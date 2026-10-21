# Prompt: xếp hạng độ khớp ở màn tìm vần

> Dán toàn bộ phần dưới đây vào một phiên Claude Code mở tại thư mục gốc repo (đã có `CLAUDE.md` và `.claude/`).

---

Bạn là **kỹ sư search ranking kiêm chuyên gia ngữ âm tiếng Việt** cho Playword. Nhiệm vụ: làm lại cách **xếp hạng kết
quả tìm vần** để hai điều luôn đúng cùng lúc:

1. **Mọi kết quả thoả đủ bộ lọc** người dùng chọn. Bộ lọc là ràng buộc cứng, không bao giờ bị nới để "có thêm kết quả".
2. **Kết quả càng gần vần càng đứng trên.** Độ gần về âm quyết định thứ tự trước; tần suất, độ hot và độ dài chỉ dùng để
   phân định giữa các kết quả gần ngang nhau.

Đọc `CLAUDE.md`, `.claude/rules/rhyme.md`, `.claude/rules/performance.md`, `.docs/adr/ADR-001…` trước khi làm.

## 1. Hiện trạng (đã kiểm chứng trên DB dev, 169.593 mục)

- Luồng chạy: `internal/adapters/index/rhymeindex` lấy tập ứng viên theo khoá vần → `rhyme.Engine.SearchIn` (file
  `internal/domain/rhyme/engine.go`):
  - `match` tìm đoạn khớp dài nhất thoả `Level`, `MinSyl`/`MaxSyl`, `Match` (`suffix` / `prefix` / `any`);
  - `Score` tính điểm; `topK` theo hàm so sánh `better`.

  Sau đó `internal/app/service.go` gộp lemma trùng, phân trang bằng cursor (tối đa `MaxDepth` = 500).
- **Thứ tự hiện tại** (`better`): số âm khớp (nhiều hơn trước) → từ thường trước tên riêng → `Score` → chữ → id.
- **Công thức điểm** (`Score`): `100·matched + levelBonus + min(5·zipf, 35) + min(15·trend, 20) − 5·|chênh số âm|`.
  - `levelBonus`: L0 = 30, L1 = 24, L2 = 18, L25 = 12, L3 = 6.
  - Mỗi bậc mức chỉ chênh 6 điểm, trong khi tần suất cộng tới 35 và độ hot tới 20. Vì vậy **trong cùng số âm khớp, một
    từ thông dụng khớp lỏng thường xếp trên một từ hiếm khớp chặt.**
- **Ví dụ thật** — `go run ./cmd/playword-cli search -json -limit 20 "ngày tàn"` (mức Rap-flex, giọng Bắc):
  - 16 kết quả đầu đều ở mức **L25**: hải quân, giai đoạn, ngày càng, tài sản…
  - **"dày dạn" (L2, trùng phần vần ay-an, chỉ khác thanh) đứng thứ 17.**
  - Trong nhóm L25 không có khái niệm "gần hơn". "tài sản" (chỉ lệch ai/ay) gần hơn "hải quân" (lệch ai/ay *và* ân/an),
    nhưng xếp sau.
  - "ngày càng" đứng thứ 3 dù có một âm tiết **giống hệt** truy vấn ("ngày"); câu hỏi Q14 về identity rhyme còn mở.

## 2. Ràng buộc cứng — không được phá

1. **Bộ lọc là hợp đồng.** Kết quả phải thoả cả sáu bộ lọc:
   - mức chặt tối đa (không có kết quả nào lỏng hơn mức đã chọn);
   - `min_syl` / `max_syl`;
   - vị trí vần (`match`);
   - giọng (so sau khi áp `Engine.Map`);
   - `slang`, `proper`.

   Viết **property test và fuzz** để khẳng định điều này với truy vấn và bộ lọc ngẫu nhiên.
2. **Không bịa từ**, mỗi kết quả vẫn có nghĩa và mức. Engine giữ **thuần** (không I/O, không đồng hồ).
3. **Giải thích được:** với mỗi kết quả, tính được *vì sao* nó đứng ở vị trí đó (các thành phần độ gần), để UI hiển thị.
4. **Hiệu năng:**
   - p99 `GET /api/v1/rhymes` < 50 ms ở 500 rps (hiện khoảng 6 ms);
   - benchmark tìm vần không chậm hơn 10% (CI chặn ở 30%);
   - không cấp phát thêm trong vòng lặp nóng: so sánh bằng `Code` (uint16) và số nguyên.
5. **Xác định và ổn định:** cùng truy vấn thì cùng thứ tự; cursor phân trang không lặp hay sót kết quả giữa các trang.
6. **Không đổi khoá vần:**
   - Xếp hạng chỉ thay thứ tự, không thay *tập* kết quả, nên không cần `rebuild-keys`.
   - Nếu buộc phải đổi `KeyerVersion` hay `configs/*.yaml`, dừng lại và hỏi.
   - Nhớ rằng mọi byte của YAML, kể cả comment, đều nằm trong `ConfigID`.
7. **Các quyết định sản phẩm đã có thì giữ, trừ khi được duyệt đổi:**
   - "khớp thêm một âm luôn xếp trên" (ADR-001 §7, Q7);
   - "tên riêng xếp sau từ thường".

## 3. Quy trình — dừng ở các điểm có dấu ⏸

### Bước 1 — Phân tích và đề xuất (chưa sửa code)

1. **Đo hiện trạng.** Chọn 40–60 truy vấn thật:
   - `.scripts/k6/queries.txt`;
   - các truy vấn tra nhiều trong analytics (`select query, count(*) from events where type = 'search' group by 1 order
     by 2 desc limit 50`);
   - vài câu tự chọn có vần khó (âm cuối -n/-ng, -t/-c, nguyên âm đôi iê/uô/ươ, thanh ngã/hỏi).

   Lưu top 20 của từng câu vào `.docs/ranking/baseline/`.
2. **Định nghĩa độ gần ở mức âm tiết**, dùng các mã sẵn có (`Code`: nucleus, coda, tone đã map theo giọng; cộng medial
   và onset từ `vnphon.Syllable`). Đề xuất một **khoảng cách có thứ bậc** thay vì cộng điểm lẫn lộn, ví dụ:
   - phần vần trùng hẳn; khác thanh nhưng cùng nhóm bằng/trắc; khác nhóm thanh;
   - nguyên âm khác nhưng cùng họ;
   - âm cuối cùng lớp chéo (n/ng, w/m), rồi cùng họ L3;
   - âm đệm (o/u) trùng hay lệch;
   - âm đầu trùng (láy âm) hay khác.

   Cụm nhiều âm tiết tổng hợp theo âm tiết. Cần cân nhắc: âm tiết *cuối* (nơi vần rơi) nặng hơn các âm tiết trước không;
   lấy âm tiết yếu nhất hay tổng.
3. **Đề xuất thứ tự so sánh từng bậc** (lexicographic), để tần suất không bao giờ vượt được độ gần. Ví dụ để bàn:
   - số âm khớp;
   - tên riêng;
   - độ gần (khoảng cách nhỏ hơn trước);
   - khớp trọn cả từ (số âm tiết của từ = số âm khớp) hơn khớp một phần;
   - tần suất và độ hot, gộp thành một điểm "hữu ích" có trần;
   - chữ, rồi id.

   So sánh với một phương án có trọng số, nêu ưu nhược của từng cách.
4. **Mô phỏng phương án trên tập truy vấn ở bước 1** (một hàm thuần trong test, chưa thay code thật). Với mỗi câu, cho xem
   top 10 trước và sau, đánh dấu những thay đổi lớn.
5. Gọi agent **`rhyme-linguist`** thẩm định đề xuất trên ví dụ thật ở cả ba giọng.
6. Viết `.docs/plans/rhyme-ranking.md`: mô hình, các bậc, ví dụ trước/sau, rủi ro, và **danh sách câu hỏi**. Mỗi câu kèm
   phương án khuyên chọn và ví dụ một rapper nhận ra ngay. Các câu tối thiểu phải hỏi:
   - Từ có một âm tiết **giống hệt** truy vấn ("ngày càng" cho "ngày tàn") xếp sau, bị loại, hay giữ nguyên? (Q14)
   - Thanh, nguyên âm, âm cuối: lệch cái nào thì "xa vần" hơn? Ví dụ: "tàn ~ tán" (khác thanh) so với "tàn ~ tàng"
     (khác âm cuối) so với "tàn ~ tần" (khác nguyên âm).
   - Âm đầu trùng (láy) có được cộng điểm không?
   - Khớp ít âm hơn nhưng *rất* gần có được đứng trên khớp nhiều âm hơn nhưng lỏng không? Nếu đổi thì phải sửa ADR-001
     §7.
   - Tần suất và độ hot (slang đang lên) được phép đẩy kết quả lên tới đâu?
   - Có áp mô hình tương tự cho tiếng Anh (perfect/family/assonance, trọng âm) không?

**⏸ DỪNG. Trình plan và câu hỏi, chờ chủ sản phẩm trả lời rồi mới code.**

### Bước 2 — Bộ đánh giá (làm trước khi đổi ranking)

- Tạo `internal/domain/rhyme/testdata/ranking_golden.tsv`: với từng truy vấn, các **cặp phán xét** "A phải đứng trên B"
  (rẻ hơn gán điểm từng kết quả). Lấy từ câu trả lời của chủ sản phẩm và từ các luật đã chốt.
- Viết test đo:
  - số phán xét bị vi phạm;
  - **số lần đảo thứ tự** (một kết quả chặt hơn đứng dưới một kết quả lỏng hơn, khi cùng số âm khớp);
  - NDCG@10 nếu có nhãn mức độ;
  - **số kết quả vi phạm bộ lọc** — luôn phải bằng 0.

  In bảng trước/sau.
- Property test và fuzz: truy vấn ngẫu nhiên × bộ lọc ngẫu nhiên → mọi kết quả thoả bộ lọc; thứ tự xác định; ghép các
  trang theo cursor ra đúng danh sách khi lấy một lần.

### Bước 3 — Triển khai

- Trong `internal/domain/rhyme`:
  - hàm thuần tính khoảng cách âm tiết trên `Code`, bằng bảng tra dựng sẵn khi tạo `Engine` (như `codeTables`), không
    cấp phát;
  - `Result` mang thêm các thành phần độ gần (gọn, kiểu số nguyên);
  - `better` so sánh theo các bậc đã chốt;
  - `Score` giữ lại phần "hữu ích" có trần, hoặc thay hẳn;
  - cập nhật comment và test về các trần điểm.
- Tiếng Anh (`english.go`, `en/`): chỉ làm nếu đã được duyệt ở bước 1.
- Benchmark trước/sau bằng agent **`perf-engineer`** (`go test ./internal/adapters/index/rhymeindex -run '^$' -bench Lookup -benchmem -count 6`; thêm một benchmark riêng cho hàm so sánh nếu cần), báo bảng ns/op, B/op,
  allocs/op.
- Cập nhật test HTTP và file golden (`-update` chỉ sau khi đọc diff). Không xoá hay nới test.

### Bước 4 — Giải thích trên UI

- Panel nghĩa (`WordPanel`) hiện **vì sao khớp**, theo từng âm tiết. Ví dụ "ngày tàn → dày dạn": `ay = ay`, `an = an`,
  khác thanh (huyền ~ nặng). Ký hiệu rõ ràng, không chỉ dựa vào màu.
- Chế độ Đủ có thể thêm một chỉ báo độ gần nhỏ. Hỏi trước khi thêm thành phần UI mới. Làm theo `/ui-change`, kiểm bằng
  `make uicheck`.
- API `GET /api/v1/rhymes` thêm trường giải thích, theo kiểu chỉ thêm (không đổi tên hay xoá trường cũ). Cập nhật
  README.

### Bước 5 — Xác nhận và báo cáo

- `make check`, `make test-integration`; chạy lại tập truy vấn ở bước 1 và lưu vào `.docs/ranking/after/`; gọi agent
  `go-reviewer` review diff.
- Báo cáo tiếng Việt, ngắn:
  - số đo đánh giá trước/sau (vi phạm phán xét, đảo thứ tự, vi phạm bộ lọc = 0);
  - 5 truy vấn tiêu biểu với top 10 trước/sau;
  - hiệu năng trước/sau;
  - các quyết định đã áp dụng và còn mở;
  - có cần ADR mới không (dùng `/adr` để ghi lại thứ tự xếp hạng nếu được duyệt).

## 4. Không làm

- Không dùng học máy hay mô hình nhúng: xếp hạng phải giải thích được bằng luật ngữ âm.
- Không đổi luật *vần hay không vần* (tập kết quả theo mức). Đó là việc của `configs/*.yaml` và cần quyết định riêng.
- Không cá nhân hoá theo người dùng ở vòng này.
