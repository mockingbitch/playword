# M0 — Tổng hợp để duyệt

Tài liệu: [ADR-001](adr/ADR-001-mo-hinh-am-tiet-va-khoa-van.md) (âm tiết, khóa vần, phương ngữ) ·
[ADR-002](adr/ADR-002-index-rcu-va-cache.md) (index, RCU, cache) ·
[ADR-003](adr/ADR-003-schema-va-pipeline.md) (schema, pipeline) ·
[rhyme_families.yaml](../configs/rhyme_families.yaml) · [dialects.yaml](../configs/dialects.yaml)

Cách trả lời: ghi `Q1: B` hoặc `Q1: B, nhưng ...`. Phương án đứng đầu trong mỗi câu là phương án
tôi đề xuất. **Q1 đến Q4 chặn M1**, các câu còn lại có thể trả lời muộn hơn.

## Mâu thuẫn và lỗ hổng phát hiện trong đặc tả

| # | Vấn đề | Xử lý đề xuất |
|---|---|---|
| I1 | Có cặp chéo `[j,n]` thì `mai`(A.j) ~ `tàn`(A.n) ở L2.5, nên "ngày mai" **sẽ** khớp "ngày tàn". Điều này trái với ca test §7. | Q1 |
| I2 | Chú thích YAML nói "may mắn" cần `[j,n]`, nhưng thực ra không cần: may=AW.j trùng ngày, mắn=AW.n khớp tàn nhờ họ A*. | Đã sửa chú thích |
| I3 | L2.5 ⊄ L3, nên tìm ở L3 bỏ sót kết quả L2.5, và cụm nhiều âm tiết khớp lẫn mức sẽ không khớp ở mức nào. | Q2 |
| I4 | §1.1 bước 3 ("u trước i → medial") mâu thuẫn với bước 4 (`mui` = U+j). | Chọn: `ui` = U+j, trừ sau q |
| I5 | Thiếu vần `uơ` (thuở, huơ, quơ). | Thêm: w + OW |
| I6 | `freq` không có thang đo. Nếu là số đếm thô thì `10·log10` lên tới 90 điểm, phá thứ tự "khớp nhiều âm tiết luôn đứng trước". | Q7 |
| I7 | Nguồn tần suất và CMUdict không có định nghĩa. Nếu cho chúng tạo entry thì vi phạm quy tắc 2. | ADR-003 D2 |
| I8 | Cùng một từ từ 2 nguồn sẽ thành 2 kết quả vần trùng nhau. | ADR-003 D3 |
| I9 | Ca test EN: `sunny`, `bunny` đã là **perfect** rhyme của `money`, không phải chỉ `family`. | Đổi ví dụ family thành `tummy` |
| I10 | Tên họ coda `O*` trùng với họ nucleus `O*`. | Đổi thành `Ø*` |
| I11 | Xóa cứng entry khi re-ingest sẽ làm hỏng `rhyme_book_items.entry_id`. | ADR-003 D5 (`stale`) |
| I12 | Chạy scheduler trong server với nhiều node sẽ chạy trùng job. | ADR-003 D9 |

## Câu hỏi

**Q1 — (đang dùng A; thêm cặp `[n, ng]` đã chốt 2026-10-08)** "ngày mai" có được tính là vần với "ngày tàn" không? (chặn M1)
- **A.** Bỏ cặp `[j,n]`, giữ `[w,m]`. Ca test giữ nguyên.
- B. Giữ `[j,n]`, thêm luật "loại ứng viên có âm tiết **giống hệt** query ở cùng vị trí khớp". Luật
  này cũng loại cả "chạy tàn", "ngày xanh".
- C. Giữ `[j,n]` và sửa ca test.

**Q2 — Cách dựng L3** (chặn M1)
- **A.** `merged`: L3 = họ coda gộp thêm cặp chéo, còn 3 lớp OPEN / STOP / SON, gần với vần ép chỉ
  dựa trên nguyên âm.
- B. Đúng đặc tả, chấp nhận bỏ sót như mô tả ở I3.
- (Nếu Q1 = A thì với phương án A, lớp SON còn `{m,n,ng,nh,w}`, còn `j` thành lớp riêng.)

**Q3 — Mức của kết quả nhiều âm tiết** (chặn M1)
- **A.** Lấy mức của âm khớp yếu nhất.
- B. Lấy trung bình điểm level_bonus của các âm.

**Q4 — ✅ Đã chốt: C (giữ `rhyme_keys`), xem ADR-003 D1.** (Câu hỏi gốc: có bỏ bảng `rhyme_keys` không?)
- **A.** Bỏ, debug bằng `playword-cli keys`.
- B. Giữ dưới dạng bảng export do CLI sinh ra.
- C. Giữ đúng đặc tả.

**Q5 — `gi` + `ê` → IE** (`giết`, `giếng` thành gi + iê). Đặc tả chưa nêu, tôi tự suy ra.
**A.** Đồng ý · B. Khác (anh nêu).

**Q6 — Âm tiết có coda p/t/c/ch đi với thanh ngang, huyền, hỏi hoặc ngã** (`àt`): **A.** Coi là lỗi
`ErrInvalidSyllable` · B. Chấp nhận.

**Q7 — Thang đo freq và chặn biên điểm phụ.**
**A.** freq_bonus = 5×zipf, chặn ở 35; trend_bonus chặn ở 20 (ADR-001 §7) · B. Giữ công thức
`10·log10` nhưng chuẩn hóa freq về số lần trên mỗi triệu từ · C. Khác.

**Q8 — Trường `respelling`** cho slang và từ vay mượn (`flex → phờ-léc`) để chúng tham gia gieo vần.
**A.** Có, làm ở M6 · B. Không.

**Q9 — ✅ Dữ liệu trả lời: có** (IPA Sài Gòn của Wiktionary, `playword-cli dialect-check`; ngoài ra `ên` cũng thành
`êng`, còn `anh`/`ách` đọc [an]/[at] nên KHÔNG trùng `ăn`/`ắc` như spec ghi — đã sửa `dialects.yaml`). Câu gốc: giọng Nam: `en` có đổi thành `eng` không (`đen ≈ đeng`)? Đặc tả giữ `e` trong danh sách
ngoại lệ. Tôi đề xuất bỏ `e` khỏi ngoại lệ, tức là có đổi. Ngoài ra, anh nghe giúp danh sách cặp
trong `dialects.yaml` và gạch những cặp sai.

**Q10 — ✅ Dữ liệu (IPA Huế): hỏi = ngã, nặng khác** → `tone_merge {5: 4}`, không phải `{4:6, 5:6}`;
thêm `inh/ênh → in`, `ich/êch → ít`. Khớp IPA 95,8%. Nghệ–Tĩnh chưa có (Wiktionary có IPA Vinh/Hà Tĩnh nếu cần). Câu gốc: giọng Trung. Mặc định Huế. Tôi **không đủ chắc** về cách gộp thanh ở Huế (`{4:6, 5:6}` gộp
hỏi/ngã vào nặng là giá trị tạm lấy từ đặc tả). Đề xuất: ở M1 làm cơ chế đầy đủ, giá trị để anh
nghe rồi chốt. Có cần thêm chế độ Nghệ–Tĩnh riêng không?

**Q11 — Sổ vần dạng unlisted** (ai có link thì xem được): **A.** Có. Chi phí nhỏ: thêm
`visibility = private | unlisted | public` và slug ngẫu nhiên.

**Q12 — Role `analyst`** (xem dashboard, không được duyệt slang): **A.** Có, ở M7. `role` đã là text
nên chỉ cần thêm giá trị mới.

**Q13 — Vần chéo Việt–Anh** (`flow` ~ `lâu`): ngoài phạm vi M1–M4. **A.** Để sau, có thể dựa trên
Q8 · B. Cần sớm.

**Q14 — Lặp từ (identity rhyme).** Khi tìm `money`, có nên ẩn hoặc xếp sau các cụm chứa chính từ đó
(`old money`, `big money`) không? Áp dụng cho cả tiếng Việt (`ngày tàn` → `hoang tàn`)? **A.** Xếp sau
(như tên riêng) · B. Ẩn · C. Giữ nguyên.

## Kế hoạch M1 (sau khi duyệt)

Tạo cấu trúc thư mục theo §5 và chuyển `internal/dict` hiện có sang `internal/adapters/index/lookup`.
Làm `pkg/vnphon` (bảng vần, Parse, Render, fuzz) và `internal/domain/rhyme` (Keyer 5 mức, đọc YAML
và kiểm tra hợp lệ, DialectMapper có kiểm tra idempotent, Ranker). Dựng bộ từ fixture khoảng 300 cụm
để chạy các ca test vần ở §7 và §1.8. Thêm `playword-cli parse`. Tiêu chí hoàn thành:
`go test -race ./...` xanh, coverage domain > 90%.
