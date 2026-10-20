# ADR-003 — Schema PostgreSQL & pipeline nạp dữ liệu

- Trạng thái: **Đề xuất (M0) — chờ duyệt**
- Ngày: 2026-10-08

## Bối cảnh

Schema trong đặc tả §3 (cùng phần bổ sung) là điểm xuất phát. ADR này giữ nguyên phần lớn, chỉ
nêu các **điểm sửa (D1–D9)** kèm lý do. Mỗi điểm cần anh đồng ý hoặc bác.

Yêu cầu: Postgres ≥ 16, các extension `citext`, `pg_trgm`. Hàm `gen_random_uuid()` đã có sẵn từ
PG13. Migration dùng goose, truy vấn dùng sqlc + pgx/v5.

## Các điểm sửa đề xuất

**D1 — Lưu bảng `rhyme_keys` (Q4 = C, chủ sản phẩm chốt 2026-10-08).**
Bảng được giữ như đặc tả. Vì khóa vần phụ thuộc vào 2 file YAML, bảng cần thêm các ràng buộc sau
để DB không bao giờ lệch với cấu hình đang chạy:

```sql
create table rhyme_configs (
  id text primary key,                -- sha256(rhyme_families.yaml || dialects.yaml)
  families_yaml text not null, dialects_yaml text not null,
  created_at timestamptz default now());
create table rhyme_keys (
  entry_id  bigint references entries on delete cascade,
  dialect   text not null,            -- bac|trung|nam
  level     text not null,            -- L0|L1|L2|L25|L3|perfect|family|assonance
  start     smallint not null,        -- cửa sổ [start, start+n) trong entry, cho match=prefix|any
  n         smallint not null,
  key       text not null,
  primary key (entry_id, dialect, level, start, n));
create index rhyme_keys_lookup on rhyme_keys (dialect, level, n, key);
create table rhyme_keys_meta (one boolean primary key default true check (one),
  config_id text not null references rhyme_configs, built_at timestamptz not null);
```

- **Ghi khi nào:** ingest ghi khóa cho các entry mới hoặc vừa đổi, trong cùng transaction với
  entry. Khi YAML đổi thì chạy `playword-cli rebuild-keys`: lệnh này COPY toàn bộ vào
  `rhyme_keys_new`, tạo index, rồi `DROP` bảng cũ và `RENAME` bảng mới trong một transaction, và
  cập nhật `rhyme_keys_meta.config_id`. Reader không bao giờ thấy bảng đang dở.
- **Kiểm tra lệch:** index in-memory (ADR-002) được build **từ `rhyme_keys`**, đúng như đặc tả.
  Nhưng server tính khóa cho query bằng YAML của chính nó, nên khi build server so
  `sha256(YAML)` với `rhyme_keys_meta.config_id`. Nếu khác nhau thì không swap index, `/readyz`
  báo lý do, và log nhắc chạy `rebuild-keys`.
- **Chi phí đã chấp nhận:** khoảng 25 triệu dòng (vài GB tính cả index) cho 500k entry; mỗi lần
  chỉnh YAML tốn vài phút rebuild. Đổi lại có thể truy vấn và phân tích khóa vần bằng SQL.
- Cột `start` là bổ sung so với đặc tả, để phục vụ `match=prefix|any` (ADR-001 §6).

**D2 — Nguồn có loại (`Kind`), không phải nguồn nào cũng sinh entry.**
Wordfreq, wordlist tần suất và CMUdict **không có định nghĩa**. Nếu cho các nguồn này `emit(Entry)`
thì kho sẽ có entry không có nghĩa, vi phạm quy tắc 2. Đề xuất:
```go
type Kind string // "dictionary" | "frequency" | "pronunciation" | "slang"
type Source interface { Name() string; Kind() Kind; Lang() Lang; License() string; Fetch(...); Parse(ctx, r, emit func(Record) error) error }
// Record là sum type: Entry | FreqRecord{Lang, Normalized, Zipf} | PronRecord{Lang, Normalized, Phonemes, Confidence}
```
Thêm 2 bảng: `word_freq(lang, normalized, zipf, source_id, pk(lang, normalized, source_id))` và
`pronunciations(lang, normalized, phonemes text[], confidence real, source_id, pk(...))`.
Lúc build index, `freq` của entry được ghép từ `word_freq` theo `(lang, normalized)`.
**Đã làm (trước M3, chủ sản phẩm chọn "tự đếm"):** `playword-cli freq` đếm mọi headword 1–5 âm
tiết trên Wikipedia tiếng Việt (233 triệu âm tiết, khoảng 50 giây). n-gram không vượt qua dấu câu
hay chữ số. Zipf = log10(số lần / tỉ âm tiết). Mỗi lần đếm sẽ thay toàn bộ dữ liệu của nguồn đó
trong một transaction.
Bỏ cột `entries.freq` và `syllables.phonemes`; phát âm tiếng Anh nằm ở `pronunciations`.

**D3 — Gộp entry trùng giữa các nguồn ở tầng index.** *(Đã làm khi thêm `wiktionary-vi`: thứ tự ưu tiên
hnd/oewn → wiktionary-vi → community, hiện đang cố định trong `rhymeindex.sourceRank`.)*
Ví dụ "may mắn" có cả trong từ điển Hồ Ngọc Đức và Wiktionary. Theo `unique (lang, normalized,
source_id)` thì đây là 2 dòng, và nếu không gộp thì kết quả vần sẽ hiện 2 lần. DB vẫn giữ riêng
từng nguồn để truy vết, còn index gộp theo `(lang, normalized)` thành một lemma: senses xếp theo độ
ưu tiên nguồn khai trong `configs/sources.yaml`, mỗi sense ghi rõ nguồn. API trả `id` của entry
thuộc nguồn ưu tiên nhất và `sources[]`.

**D4 — Entry không parse được vẫn giữ để tra từ, nhưng loại khỏi index vần.**
Từ vay mượn (`wifi`, `karaoke`) và teen code (`j`, `z`, `ko`) sẽ trả `ErrInvalidSyllable`. Thêm cột
`entries.rhyme_error text` (null nghĩa là parse được). Ingest ghi lý do vào cột này, không bỏ entry.
Câu hỏi kèm theo (**Q8**): có thêm trường `respelling` để admin nhập cách đọc theo âm Việt cho slang
và từ vay mượn hay không, ví dụ `flex → "phờ-léc"`, `chill → "chiu"`. Rapper Việt gieo vần với từ
tiếng Anh rất nhiều, nên tôi nghĩ trường này đáng có ở M6.

**D5 — Không xóa cứng entry khi nguồn bỏ từ.**
`rhyme_book_items.entry_id` tham chiếu tới `entries`, nên nếu xóa cứng thì sổ vần của user sẽ hỏng.
Thêm `entries.last_seen_run bigint`. Sau một lần full run, entry nào không còn thấy trong nguồn sẽ
được chuyển sang `status='stale'` (bị loại khỏi index nhưng sổ vần vẫn hiển thị được).
`status` có các giá trị: `approved | pending | rejected | stale`.

**D6 — Idempotent bằng `content_hash`.**
Thêm cột `entries.content_hash bytea`, là sha256 của (headword, pos, senses, tags, is_slang) sau
khi normalize. Upsert dùng `ON CONFLICT (lang, normalized, source_id) DO UPDATE ... WHERE
entries.content_hash IS DISTINCT FROM excluded.content_hash`. Chạy lại lần hai với cùng dữ liệu
cho kết quả `updated=0`; đây sẽ là một test tích hợp.

**D7 — Session lưu hash của token.**
`sessions.id` = sha256 của token trong cookie. Nếu DB bị lộ thì kẻ tấn công cũng không dùng được
session. Cookie chứa 32 byte ngẫu nhiên, mã hóa base64url.

**D8 — Partition `events` tự quản lý.**
Không dùng `pg_partman`. Một job (`playword-cli maintain`, chạy theo cron) tạo trước partition cho
2 tháng tới và `DROP` các partition cũ hơn 180 ngày. Ẩn danh hóa khi user xóa tài khoản:
`update events set user_id = null, anon_id = 'deleted:'||md5(random()::text) where user_id = $1`.
Để `REFRESH MATERIALIZED VIEW CONCURRENTLY` chạy được, cần unique index trên
`mv_daily_activity(d)` và `mv_query_trends(lang, query)`.

**D9 — Scheduler: CLI + cron là chính.** *(Đã làm ở M6: `PLAYWORD_INGEST_SCHEDULE` trong server, mỗi job
giữ `pg_try_advisory_lock`; vẫn chạy được bằng CLI + cron.)*
Một scheduler chạy bên trong server (robfig/cron) sẽ chạy trùng job khi có nhiều node. Đề xuất mặc
định: `playword-cli ingest <source>` được gọi bởi cron hệ thống hoặc CronJob của K8s. Nếu vẫn bật
scheduler nội bộ (`INGEST_SCHEDULE`), mỗi job phải giữ `pg_try_advisory_lock(hash(source))` để chỉ
một node chạy.

## Pipeline

```
Fetch ──► Parse ──► Normalize ──► Validate ──► Phonemize ──► Upsert (batch 1000) ──► Finish ──► NOTIFY
 │         │          │             │             │              │                     │
 │         │          NFC, trim,    ≥1 gloss?     vnphon/CMU     COPY → temp table     mark stale (full run),
 │         │          gộp senses    ≤5 âm tiết?   lỗi → cột      → upsert theo hash    ingest_runs.status,
 │         │          trùng khóa    (lỗi → skip    rhyme_error   → thay senses của     counts, errors
 │         stream,    tự nhiên      + lý do)                     entry đã đổi
 │         emit từng record
 cache file dump cục bộ (ETag/Last-Modified, theo `since`)
```

- Mỗi batch chạy trong một transaction riêng. Lỗi ở một batch không hủy cả run: batch đó được ghi
  vào `errors` và run có trạng thái `partial`.
- `ingest_runs.errors` chỉ lưu 1.000 lỗi đầu tiên cùng bộ đếm theo lý do
  (`{"too_long": 812, "no_definition": 40, ...}`), để jsonb không phình to.
- `--dry-run`: chạy toàn bộ pipeline trong một transaction rồi `ROLLBACK`, sau đó in ra các bộ đếm.
- Phê duyệt một slang (`slang_submissions` → approve) sẽ tạo entry từ nguồn `community` với
  `status='approved'`, rồi phát `NOTIFY`.

## License (cần xác minh ở M2, sẽ ghi vào README)

| Nguồn | License (theo hiểu biết hiện tại, **chưa xác minh**) | Hệ quả |
|---|---|---|
| Từ điển Hồ Ngọc Đức | GPL | Phục vụ qua web không phải là "phân phối". Không nhúng dữ liệu vào binary/image công khai. |
| Wiktionary vi/en | CC BY-SA | Phải ghi nguồn trên mỗi định nghĩa lấy từ Wiktionary (đã có trường `source`). |
| WordNet | WordNet License (kiểu BSD) | Ghi nguồn. |
| CMUdict | BSD-2 | Ghi nguồn. |
| wordfreq | Dữ liệu CC BY-SA | Ghi nguồn. |
| underthesea / OSCAR | Cần kiểm tra từng bộ | Có thể thay bằng tần suất tự đếm từ Wiktionary nếu vướng. |

## Hệ quả

- Schema giữ `rhyme_keys` (thêm cột `start`, `dialect`, cùng 2 bảng `rhyme_configs` và
  `rhyme_keys_meta`), thêm 2 bảng enrichment và 3 cột vào `entries`
  (`rhyme_error`, `last_seen_run`, `content_hash`).
- Khóa vần trong DB luôn gắn với một `config_id`; server từ chối dùng khóa sinh ra từ một YAML khác.
