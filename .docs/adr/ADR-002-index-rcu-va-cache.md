# ADR-002 — Index in-memory, RCU, cache

- Trạng thái: **Đề xuất (M0) — chờ duyệt**
- Ngày: 2026-10-08

## Bối cảnh

Mục tiêu: p99 < 50ms, 1.000 RPS/node, đọc không bị tranh khóa, dưới 1GB RAM cho 500k entry.
Postgres là source of truth. Truy vấn nóng (tìm vần, tra từ) phải đi qua index trong RAM.

## Quyết định

### 1. Snapshot bất biến + RCU

```go
type Snapshot struct {
    Gen      uint64           // tăng mỗi lần swap
    BuiltAt  time.Time
    ConfigID string           // hash của rhyme_families.yaml + dialects.yaml đã dùng
    Entries  []Entry          // EntryID = vị trí (uint32), dùng chung cho mọi bảng
    Rhyme    [3][5]*KeyTable  // [dialect][level]; level: L0 L1 L2 L25 L3
    Lookup   *lookup.Index    // tra từ / autocomplete (fold, telex, gõ sai), tái dùng code hiện có
    Cache    *ResultCache     // gắn với snapshot nên swap là tự invalidate
}

var current atomic.Pointer[Snapshot]   // global duy nhất, đúng như đặc tả §5
```

- Reader chỉ `Load()` một lần ở đầu request rồi dùng snapshot đó đến hết request, không lấy khóa.
- Builder chạy trong goroutine riêng, dựng snapshot mới hoàn chỉnh rồi `Store()`. Nếu build lỗi thì
  giữ snapshot cũ và log lỗi kèm metric.
- Cache nằm **bên trong** snapshot. Vì vậy "invalidate toàn bộ khi swap" xảy ra tự động, không có
  khoảng thời gian cache cũ trả kết quả của index mới hay ngược lại.
- `/readyz` trả 503 cho đến khi snapshot đầu tiên sẵn sàng.

### 2. Bảng khóa dạng CSR thay cho `map[string][]EntryID`

> **Trạng thái thực hiện (M2):** đang dùng `map[string][]int32` đúng như spec §4, vì kho 29k mục
> chỉ tốn khoảng 1,3 triệu khóa. Sẽ chuyển sang CSR khi benchmark trên kho khoảng 500k mục cho
> thấy cần (M3/M4). Ở M2, ứng viên lấy qua index rồi được xếp hạng bằng `rhyme.Engine.Search`,
> tức chính bản tham chiếu. Nhờ vậy index chỉ có thể nhanh hơn, không thể cho kết quả khác.

```go
type KeyTable struct {
    hashes   []uint64   // sorted: hash(n | rhymekey), phân biệt n trong hash
    offsets  []uint32   // len(hashes)+1; postings[offsets[i]:offsets[i+1]]
    postings []Posting  // trong một khóa: xếp theo staticScore giảm dần
}
type Posting struct { Entry uint32; Start, End uint8 } // cửa sổ [Start,End) trong ứng viên (ADR-001 §6)
```

- Lý do: `map[string][]uint32` với hàng triệu khóa tốn khoảng 50–80 B/khóa cho overhead của map,
  slice header và string. Layout CSR chỉ tốn 12 B/khóa, lookup bằng binary search mất khoảng 1µs,
  và GC gần như không phải quét vì không có con trỏ.
- **Va chạm hash 64-bit** không ảnh hưởng tính đúng: sau khi lấy posting, bước phân loại mức cho
  từng âm tiết (ADR-001 §4) đằng nào cũng tính lại khóa từ `Syllable` của ứng viên, nên ứng viên
  va chạm bị loại ở bước đó.
- `staticScore` = freq_bonus + trend_bonus, tính lúc build. Mỗi danh sách posting đã được sắp theo
  điểm này nên có thể lấy top-K mà không cần sort toàn bộ.

### 3. Thuật toán truy vấn

```
q → vnphon.Parse → DialectMapper(dialect) → lấy tối đa 5 âm tiết cuối
for n := min(|q|, max_syl) down to min_syl:
    k := Key(level, q[|q|-n:])                 // 1 lookup nhờ các mức lồng nhau (ADR-001 §4)
    for p in table[dialect][level].Get(n, k):
        lọc theo alignment (suffix/prefix/any), slang, status
        dedupe theo Entry, giữ n lớn nhất
        phân loại mức thực của từng âm → level kết quả → score
    if số đã gom ≥ offset+limit: break        // đúng vì 100·n trội hơn mọi số hạng phụ (ADR-001 §7)
partial sort (heap) top offset+limit
```

- **Cursor**: base64 của `{gen, offset}`. Nếu `gen` khác snapshot hiện tại thì vẫn tính tiếp theo
  `offset` trên snapshot mới, và response có thêm cờ `index_changed: true` để UI biết danh sách có
  thể bị xô lệch.
- Ước lượng chi phí: trường hợp xấu nhất là khóa 1 âm tiết ở L3 (ví dụ `A*.SON`) có thể khớp
  khoảng 10% kho, tức 50k posting. Tính điểm 50k posting mất khoảng 0,5–1ms trên một core.
  Mọi trường hợp khác đều nhỏ hơn nhiều.

### 4. Cache

- `hashicorp/golang-lru/v2`: thư viện nhỏ, không có dependency, generic. Dùng cùng
  `golang.org/x/sync/singleflight`.
- Khóa cache = hash của `(q đã normalize, lang, dialect, level, min_syl, max_syl, match, slang)`.
  Giá trị là danh sách đã xếp hạng, tối đa 200 kết quả, để phân trang cũng trúng cache.
- Dung lượng 10.000 mục; ước tính 200 kết quả × 8 B ≈ 1,6 KB/mục, tổng khoảng 16 MB.
- Ghi chú: index đã ở mức micro giây nên cache chủ yếu có ích cho các query L3 ngắn và phổ biến.
  Vẫn làm theo đặc tả; benchmark ở M3 sẽ có số đo hit-rate và độ trễ khi có và không có cache.

### 5. Build và đồng bộ nhiều node

- Đọc dữ liệu từ Postgres bằng một lượt quét stream (pgx), gồm entries, syllables, senses,
  word_freq, pronunciations và **`rhyme_keys`** (Q4 = C). Sắp xếp và đóng gói thành CSR.
  Trước khi swap, kiểm tra `rhyme_keys_meta.config_id == sha256(YAML)` (ADR-003 D1).
- Thời điểm build: lúc khởi động; khi nhận `NOTIFY playword_index` (phát ra sau mỗi ingest run
  thành công hoặc sau lệnh `playword-cli rebuild-index`); và mỗi 60 giây poll
  `max(ingest_runs.id where status='ok')` để dự phòng trường hợp lỡ mất NOTIFY.
- Đổi YAML thì phải chạy `playword-cli rebuild-keys` trước, sau đó các node tự rebuild index
  nhờ NOTIFY (ADR-003 D1).

### 6. Ước lượng bộ nhớ (500k entry)

Giả định: trung bình 2,2 âm tiết/entry, tức khoảng 3,5 cửa sổ/entry.

| Thành phần | Tính | Ước lượng |
|---|---|---|
| Posting | 500k × 3,5 cửa sổ × 3 phương ngữ × 5 mức × 8 B | ~210 MB |
| Khóa CSR | khóa riêng biệt ≤ 30% số posting × 12 B | ~95 MB |
| Entry + senses | 500k × khoảng 400 B (text, gloss, ví dụ) | ~200 MB |
| Lookup (tra từ) | khóa thường/không dấu/telex + delete-map | ~80 MB |
| **Tổng** | | **~590 MB < 1 GB** ✓ |

Nếu cần giảm: (a) bỏ chế độ `any` và chỉ index cửa sổ prefix/suffix, giảm khoảng 35% posting;
(b) dùng chung bảng giữa các phương ngữ khi khóa giống nhau, phần lớn khóa của `bac` trùng `nam`
ở L2.5/L3. Chưa làm cho đến khi số đo thật cần đến.

### 7. Tái dùng code hiện có

Package `internal/dict` hiện có (chuẩn hóa, bỏ dấu, telex, gợi ý gõ sai, đo 0,1–3µs trên 200k từ)
sẽ được chuyển ở M1/M3 thành `internal/adapters/index/lookup`, phục vụ `/dictionary` và
autocomplete. Index `pg_trgm` trong đặc tả vẫn giữ cho trang admin và các truy vấn ad-hoc, không
nằm trên đường xử lý nóng. Web layer cũ (html/template) sẽ được thay bằng `templ` ở M3.

### 8. Benchmark và hồi quy

`BenchmarkLookup_{1,2,3}syl` chạy trên kho tổng hợp 500k entry sinh từ bảng vần thật, có
`-race` ở CI. Baseline lưu ở `bench/baseline.txt`; CI so bằng `benchstat` và fail nếu chậm hơn
30%. k6 đặt trong `.scripts/k6/`.
Lưu ý: benchmark Go đo thời gian trung bình, không đo p99. Vì vậy p99 lấy từ k6 cộng với histogram
Prometheus, còn ngưỡng 30% áp lên ns/op.

## Số đo (M3, kho thật 29k mục, máy 56 core)

- k6 với mix API, partial HTMX và trang đầy đủ; truy vấn ngẫu nhiên từ 400 từ thật × mức × giọng × số âm:
  500 RPS cho p99 35,6ms; 1.000 RPS cho p99 9,3ms; 0 lỗi.
- Heap sống sau khi build: khoảng 168MB (đỉnh lúc build khoảng 420MB). Nếu tăng tuyến tính thì
  500k mục cần khoảng 2,9GB, **vượt mục tiêu 1GB**. Phải chuyển `byKey` sang CSR (mục 2) và nén lookup
  index trước khi nạp kho lớn ở M4.

## Hệ quả

- Không có lock trên đường đọc; global duy nhất là `atomic.Pointer[Snapshot]`.
- Bộ nhớ cao hơn kiểu map đơn giản về độ phức tạp code nhưng thấp hơn khoảng 3 lần về RAM thực tế.
- Rủi ro: build 500k entry × 3 phương ngữ trong lúc vẫn đang phục vụ request sẽ có lúc bộ nhớ gấp
  đôi (snapshot cũ cùng mới), khoảng 1,2 GB trong vài giây. Cần đặt memory limit cho container
  ≥ 1,5 GB, hoặc build trên node riêng rồi phân phối file snapshot. Phương án sau để dành về sau.
