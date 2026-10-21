# ADR-004 — Xếp hạng kết quả tìm vần theo độ gần

- Trạng thái: **Đã chấp nhận**
- Ngày: 2026-10-21
- Liên quan:
  - [ADR-001](ADR-001-mo-hinh-am-tiet-va-khoa-van.md) §7 (khớp thêm một âm luôn xếp trên);
  - [kế hoạch](../plans/rhyme-ranking.md), [prompt](../prompts/rhyme-ranking.md);
  - mã nguồn `internal/domain/rhyme/closeness.go`, `engine.go` (`better`, `match`, `refine`).

## Bối cảnh

Trước đây thứ tự trong cùng số âm khớp do một điểm cộng dồn quyết định. Mức vần chỉ được cộng 6 điểm mỗi bậc, trong khi
tần suất được tới 35 điểm và độ hot tới 20 điểm. Vì vậy một từ thông dụng khớp lỏng thường đứng trên một từ hiếm khớp
chặt.

- Ví dụ: với "ngày tàn", "dày dạn" (trùng phần vần, chỉ khác thanh) đứng thứ 17, sau 16 kết quả Rap-flex.
- Đo trên 63 truy vấn thật: có **1.462 lần đảo thứ tự** (kết quả chặt hơn đứng dưới kết quả lỏng hơn khi cùng số âm
  khớp), ở 57 truy vấn.
- Trong cùng một mức cũng không có khái niệm "gần hơn": "tài sản" (lệch ai/ay) và "hải quân" (lệch ai/ay và ân/an) được
  coi là ngang nhau.

## Quyết định

1. **Bộ lọc là ràng buộc cứng.** Mức chặt, số âm tối thiểu và tối đa, vị trí vần, giọng, slang và tên riêng quyết định
   *tập* kết quả. Xếp hạng chỉ sắp thứ tự trong tập đó.
2. **Thứ tự so sánh theo từng bậc.** Bậc sau chỉ được xét khi các bậc trước hoà nhau:
   1. số âm khớp (nhiều hơn trước; ADR-001 §7);
   2. từ thường trước tên riêng (quyết định 2026-10-08);
   3. số âm tiết **trùng chính từ của truy vấn** (ít hơn trước). Ví dụ "ngày càng" khi tìm "ngày tàn" xếp sau mọi kết
      quả cùng độ dài không trùng. *Chủ sản phẩm chọn 2026-10-21*, trả lời một phần Q14.
   4. mức vần (chặt hơn trước);
   5. **khoảng cách âm** (nhỏ hơn trước);
   6. điểm `Score` (tần suất, độ hot, chênh độ dài): chỉ để phân định phần còn hoà;
   7. chữ, rồi id: thứ tự toàn phần, phân trang ổn định.
3. **Khoảng cách âm của một âm tiết**, tính trên mã đã áp giọng:

   | Thành phần | Giống | Lệch nhẹ | Lệch hơn |
   |---|---|---|---|
   | thanh | 0 | cùng bằng/trắc: 1 | khác nhóm: 2 |
   | âm cuối | 0 | cặp chéo (n~ng, w~m): 3 | cùng họ L3: 5 |
   | nguyên âm | 0 | — | cùng họ: 4 |
   | âm đệm | 0 | lệch: 1 | — |

   - **Lệch nguyên âm xa hơn lệch âm cuối** (*chủ sản phẩm chọn 2026-10-21*): với "tàn", "tàng" đứng trên "tần".
   - **Âm đầu không tính**, không ưu tiên láy âm (*chủ sản phẩm chọn 2026-10-21*).
   - Khoảng cách của cả cụm bằng tổng khoảng cách các âm tiết. Vì khoảng cách chỉ so giữa các kết quả cùng mức, không bao
     giờ có một mức lỏng hơn vượt lên trên mức chặt hơn.
4. **Tiếng Anh dùng cùng cách** (*chủ sản phẩm chọn 2026-10-21*). Mỗi đoạn vần:
   - nguyên âm khác nhưng cùng nhóm: 3;
   - **lệch trọng âm: 3**, vì vần rơi vào âm không trọng âm thì yếu ("money ~ tummy" đứng trên "money ~ company");
   - phụ âm cùng nhóm: 2; phụ âm khác nhóm hoặc thiếu: 3.

   Tiếng Anh chưa tính bậc "trùng từ".
5. **Hiệu năng:** khoảng cách chỉ được tính cho kết quả còn có thể vào top K. Kết quả được so trước với phần tử tệ nhất
   của heap, giả định nó có khoảng cách 0; nếu ngay cả khi đó cũng không vào được thì bỏ qua. Phép này cho kết quả giống
   hệt cách tính đầy đủ (fuzz `FuzzRankingFilters` kiểm điều đó).

## Hệ quả

- Trên 63 truy vấn thật: số lần đảo thứ tự trong cùng bậc giảm từ **1.462 xuống 0** (`.docs/ranking/`).
- Trường `score` của API không còn là khoá sắp xếp, chỉ dùng để phân định. API có thêm `distance` và `repeats_query`
  (chỉ thêm, không đổi tên hay xoá trường cũ).
- Không đổi khoá vần, không cần `rebuild-keys`.
- Đổi một trọng số ở bảng trên là đổi quyết định sản phẩm, phải sửa ADR này và các test ở `ranking_test.go`.

## Các phương án đã cân nhắc

- **Cộng điểm có trọng số** (giữ một con số duy nhất, tăng chênh lệch giữa các mức): đơn giản nhưng vẫn để tần suất lấn
  độ gần ở vùng biên, và khó giải thích. Không chọn.
- **Học máy hay mô hình nhúng:** không giải thích được, trái nguyên tắc "mọi kết quả phải giải thích được". Không chọn.
- **Loại hẳn kết quả trùng từ:** làm thay đổi tập kết quả, không chỉ thứ tự. Chủ sản phẩm chọn xếp sau thay vì loại.

## Câu hỏi mở

- **R1:** cụm dài mà vần nằm ở cuối, ví dụ "hương đăng bày án" (khoảng cách 2) cho "ngày tàn", đứng trên một từ hai âm
  tiết chỉ kém gần hơn một bậc, ví dụ "lạy van" (khoảng cách 3). Khi khoảng cách bằng nhau thì từ ngắn đã thắng, vì
  `Score` phạt chênh độ dài. Có nên thêm bậc "khớp trọn cả từ trước, khớp một phần sau", đặt trước khoảng cách âm không?
