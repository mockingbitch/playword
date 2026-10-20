# Review UI/UX — Playword

Ngày 08/10/2026. Xem trên dữ liệu thật (169k mục) bằng Chrome headless ở 390×844 và 1280×900, với ba vai: khách,
người dùng đã đăng nhập, admin. Ảnh chụp: [`.docs/ui-review/before/`](ui-review/before/).

**Kết luận nhanh:** nền tảng tốt. Theme tối gọn, chữ tiếng Việt dễ đọc, không bị tràn ngang, không có lỗi CSP,
contrast chữ chính và chữ phụ đều đạt (≈ 7:1). Vấn đề lớn nằm ở **luồng chính trên mobile**: kết quả đầu tiên nằm
khá thấp, danh sách khó quét, nút lưu lệch vị trí, và hai tính năng hỏng khi tắt JS.

## Vấn đề, xếp theo mức độ

| # | Trang | Vấn đề | Mức | Đề xuất |
|---|---|---|---|---|
| 1 | Tìm vần | **Nút ＋ lưu không chạy khi tắt JS.** Form `.save` chỉ có `hx-post`, không có `action`/`method`, nên trình duyệt gửi GET về trang hiện tại. | Cao | Thêm `action="/save" method="post"` cùng `@csrfField`. Server trả redirect 303 về trang kết quả khi request không phải HTMX. |
| 2 | Tìm vần | **Mở nghĩa không chạy khi tắt JS.** `.hit-head` là `<button>` chỉ có `hx-get`. | Cao | Đổi thành `<a href="/word/…">` có `hx-get`: có JS thì mở inline, không có JS thì sang trang nghĩa. |
| 3 | Tìm vần | **Lỗi console khi người dùng chưa có sổ nào:** `hx-include="#save-book"` trỏ tới phần tử không tồn tại (lỗi htmx, 3 lần mỗi trang). | Cao | Chỉ thêm `hx-include` khi có select, hoặc dùng `hx-include="[name=book_id]"`. |
| 4 | Tìm vần | **Mỗi lần dừng gõ lại thêm một mục vào lịch sử trình duyệt** (`hx-push-url` + trigger `keyup`): gõ "ngày tàn" xong, nút Back phải bấm 3–4 lần. | Cao | Dùng `hx-replace-url` khi đang gõ, chỉ push khi submit hoặc đổi bộ lọc. |
| 5 | Tìm vần | **`aria-live="polite"` bọc cả vùng kết quả**, nên trình đọc màn hình đọc lại 50 kết quả sau mỗi lần gõ. | Cao | Bỏ khỏi `#results`; thêm một dòng trạng thái ẩn, ví dụ "50+ vần, khớp 2 âm". |
| 6 | Tìm vần (mobile) | **Kết quả đầu tiên bắt đầu ở y≈390px.** Phía trên nó có nav 2 dòng, tab ngôn ngữ, ô tìm, dòng bộ lọc, "Vần cho…" và "Khớp thêm ở giọng". Màn 844px chỉ còn thấy 3–4 kết quả. | Cao | Nav một dòng; ô tìm dính đầu trang khi cuộn; gộp ngôn ngữ và bộ lọc vào một hàng chip; bỏ dòng "Vần cho X" vì đã trùng với ô tìm. |
| 7 | Tìm vần | **Nút ＋ đặt `position:absolute; top:50px`.** Khi nghĩa chỉ có 1 dòng, nút đè lên đường kẻ phân cách (rõ nhất ở bản desktop: "giai đoạn", "ngày càng"). Kích thước nút 32×32px. | Cao | Dùng grid cho mỗi kết quả: [từ + nghĩa] [＋]. Vùng chạm 44×44. |
| 8 | Tìm vần | **Tô sáng không cho biết phần nào vần.** Khi khớp cả cụm, mọi âm tiết đều tô cam nên mất tác dụng. Huy hiệu "Rap-flex" lặp lại ở mọi dòng của nhóm Rap-flex nên gây nhiễu. | Vừa | Chỉ tô phần vần (bỏ phụ âm đầu: h**ải** q**uân**). Chỉ hiện huy hiệu mức khi nó khác mức đang chọn, hoặc gom kết quả theo mức. *Tô phần vần cần biết ranh giới phụ âm đầu, vnphon đã có sẵn; cần anh duyệt.* |
| 9 | Tìm vần | **Không biết có bao nhiêu kết quả**, cũng không biết đang ở nhóm nào khi cuộn sâu. | Vừa | Hiện số lượng ở tiêu đề nhóm ("Khớp 2 âm · 50+"); tiêu đề nhóm dính đầu trang khi cuộn. |
| 10 | Tìm vần EN | Tiêu đề nhóm hiện ký hiệu ARPAbet "AH1 N IY0", người dùng không hiểu. | Vừa | Hiện phần chữ tương ứng ("-oney") hoặc chỉ ghi "khớp 2 âm". |
| 11 | Tìm vần | **Trạng thái rỗng và lỗi đều tô đỏ, không có lối đi tiếp.** Ví dụ "xqzw không phải âm tiết…". | Vừa | Màu trung tính. Kèm gợi ý: tra từ điển, chuyển sang English (nếu là chữ Latinh không dấu), nới mức chặt (một chạm). |
| 12 | Bộ lọc | Mở bộ lọc thì đẩy kết quả xuống dưới màn hình (5 nhóm chip). Mỗi lần đổi một chip là chạy lại tìm kiếm ngay; vẫn ổn nhưng không thấy kết quả thay đổi. | Vừa | Mobile: bottom sheet. Desktop: panel bên cạnh. Hàng chip tóm tắt phía trên chạm được để đổi nhanh mức chặt. |
| 13 | Nav (mobile) | Nav xuống 2 dòng ("Đăng nhập" hoặc tên người dùng rơi xuống dòng dưới); link cao 33px. | Vừa | Mobile: thanh tab dưới đáy (Tìm vần · Từ điển · Sổ · Tôi), cao ≥ 56px, có safe-area. |
| 14 | Nghĩa từ / Từ điển | Không có phát âm hay phần vần của từ (ví dụ "vần **an**, thanh huyền"). Link "Tìm vần cho…" nằm cuối, chữ nhỏ. Nghĩa của HNĐ còn ký hiệu thô ("I. tt. 1. (Hoa)…"). | Vừa | Header của từ: vần, thanh, nút "Tìm vần" nổi bật. Ẩn bớt nghĩa dài (hiện 3 nghĩa + "xem thêm"). *Làm sạch dữ liệu HNĐ thuộc phần ingest, không thuộc UI.* |
| 15 | Sổ vần | Nút xoá × chỉ 24×36px và không hỏi lại. Ô ghi chú lúc nào cũng hiện nên mục trông nặng nề. Kéo-thả chỉ có tay nắm ⋮⋮, không có cách sắp xếp bằng bàn phím. | Vừa | Xoá có "Hoàn tác" 5 giây. Ghi chú hiện dạng chữ, chạm vào mới sửa. Thêm nút lên/xuống cho bàn phím và trình đọc màn hình. |
| 16 | Sổ vần | Sổ không gom theo từ gốc ("vần với ngày tàn"), trong khi rapper thường lưu nhiều vần cho cùng một từ. | Thấp | Nhóm theo `matched_from`. *Đây là quyết định sản phẩm.* |
| 17 | Tài khoản | Trang `/me` dài, gồm 4 thẻ. Thẻ tên "Đăng nhập" nhưng nội dung là mật khẩu, dễ nhầm. "Đăng xuất" đứng một mình trong một thẻ. | Thấp | Đổi tên thành "Mật khẩu & đăng nhập"; chuyển Đăng xuất lên đầu trang hoặc vào menu; gom thành các phần rõ ràng. |
| 18 | Form | Nhãn kiểu grid làm "(tuỳ chọn)" rơi xuống dòng riêng (form slang). Ô input trống không có placeholder hay ví dụ. | Thấp | Nhãn nằm trên một dòng; thêm ví dụ trong placeholder. |
| 19 | Đăng nhập | Nút Google chỉ có chữ, không có logo, nên khó nhận ra. | Thấp | Thêm logo "G" bằng SVG inline (theo hướng dẫn thương hiệu của Google). |
| 20 | Admin | Hai hàng chip (Dashboard/Slang và 7d/30d/90d/CSV/Người dùng) trông giống nhau nên khó phân biệt. 9 ô số liệu xuống dòng, ô cuối nằm lẻ. Nhãn trục biểu đồ khoảng 9px. Duyệt slang: hai nút to xếp dọc, không có phím tắt. | Thấp | Tab admin riêng; ô số liệu chia 4/8 cột; chữ biểu đồ ≥ 11px; duyệt bằng phím (j/k, a/r). |
| 21 | Chung | Không có `prefers-reduced-motion`. Input dùng `outline:none` và chỉ đổi màu viền 1px khi focus, dễ bị bỏ qua khi dùng bàn phím. Không có theme sáng: khó đọc ngoài trời nắng. | Thấp | Thêm media query giảm chuyển động; focus ring 2px rõ ràng; theme sáng qua `prefers-color-scheme`. |

Đo vùng chạm (cần ≥ 44px): `.hit-head` 42px, `.save-btn` 32px, chip 36px, link nav 33px, nút xoá trong sổ 24×36px,
`summary` của bảng admin 21px.

## Ba hướng thiết kế tổng thể

### A. "Đánh bóng": giữ bố cục hiện tại, sửa lỗi
Sửa #1–#13 trong khuôn hiện tại: nav gọn, ô tìm dính đầu trang, grid cho kết quả, bộ lọc dạng sheet.
```
┌ Playword        Tìm vần Từ điển Sổ ☰ ┐
│ [ngày tàn                     ⟳  ]   │  ← dính khi cuộn
│ VI·Rap-flex·Bắc·Cuối cụm  [Lọc]      │
│ KHỚP 2 ÂM · 50+                      │
│ h[ải] q[uân]                    [＋] │
│ dt. Quân chủng của lực lượng…        │
│ gi[ai] đ[oạn]                   [＋] │
```
+ Ít rủi ro, làm nhanh (khoảng 1–2 ngày), test hiện có hầu như giữ nguyên.
− Mỗi màn hình vẫn chỉ thấy 5–6 kết quả, vì dòng nghĩa chiếm chỗ.

### B. "Bảng vần": dày, quét nhanh, nghĩa mở ra khi cần ★ khuyên chọn
Mobile: tab bar dưới đáy, ô tìm dính đầu trang. Kết quả hiện dạng **lưới chip theo nhóm** (khớp 3 âm → 2 âm → 1 âm;
trong mỗi nhóm xếp theo mức). Chạm vào một chip → **bottom sheet** hiện nghĩa, phần vần, nút Lưu và "Tìm vần cho từ này".
Có công tắc "Gọn / Đầy đủ" (Đầy đủ = danh sách có nghĩa như bây giờ), trình duyệt nhớ lựa chọn.
Desktop: 2 cột, bên trái là lưới vần, bên phải là panel nghĩa cố định.
```
mobile                                 desktop
┌──────────────────────────────┐      ┌───────────────────────────┬──────────────────┐
│ [ngày tàn               ⟳ ]  │      │ [ngày tàn           ] Lọc │ hải quân    [＋] │
│ Rap-flex ▾  Bắc ▾  Gọn|Đủ    │      │ KHỚP 2 ÂM · 50+           │ vần: ai · ân     │
│ KHỚP 2 ÂM · 50+              │      │ hải quân  giai đoạn       │ dt. Quân chủng…  │
│ [hải quân] [giai đoạn]       │      │ ngày càng tài sản tai nạn │ Nguồn: HNĐ, Wikt │
│ [ngày càng] [tài sản]        │      │ tài năng  đại tân  ...    │ [Tìm vần cho …]  │
│ [tai nạn] [tài năng] [+12]   │      │ KHỚP 1 ÂM · 50+           │                  │
│ KHỚP 1 ÂM · 50+              │      │ tàn  tan  bàn  đàn  ...   │                  │
│ [tàn] [bàn] [đàn] [càn] …    │      └───────────────────────────┴──────────────────┘
├──────────────────────────────┤
│ ⌕ Vần   📖 Từ điển  ★ Sổ  ☺ │
└──────────────────────────────┘
```
+ Một màn hình mobile thấy 25–40 vần thay vì 4; đúng với cách rapper dùng ("quét nhanh, chọn một"). Nghĩa vẫn
  chỉ cách một chạm, nên mỗi kết quả vẫn giải thích được.
− Cần làm bottom sheet (dùng `<dialog>`, JS nhỏ, không cần thư viện). Khi tắt JS, chip là link sang `/word/…`.
  Phải sửa nhiều test hơn. Khoảng 3–4 ngày.
− **Cần anh quyết:** chế độ mặc định là Gọn hay Đầy đủ (em đề xuất Gọn trên mobile, Đầy đủ trên desktop).

### C. "Bàn viết": viết lời ngay trong app
Thêm một vùng soạn lời. Chạm vào từ cuối của một câu là ra vần ở panel bên cạnh, rồi chèn thẳng vào câu.
Sổ vần trở thành nơi lưu bài.
```
┌ Bài: "Ngày tàn" ───────────────┬ vần cho "tàn" ─────┐
│ Đêm nay phố vắng ngày [tàn]    │ bàn · đàn · càn    │
│ Bước chân lạc giữa ...|        │ hải quân · tai nạn │
└────────────────────────────────┴────────────────────┘
```
+ Khác biệt rõ với các từ điển vần khác.
− Đây là **tính năng mới** chứ không còn là redesign: cần đặc tả, lưu trữ bài viết, và rất khó chạy khi tắt JS.
  Nên để làm sau, trên nền B.

**Khuyên chọn: B**, kèm toàn bộ bản sửa lỗi #1–#5 của A, làm trước vì là lỗi thật, độc lập với hướng thiết kế.

## Danh sách việc đề xuất (chờ duyệt)

1. **Sửa lỗi #1–#5** (không phụ thuộc hướng thiết kế).
2. Design system: token màu, khoảng cách, cỡ chữ; màu cho từng mức vần L0–L3 dễ phân biệt cả với người mù màu
   (kèm nhãn chữ, không chỉ dựa vào màu); focus ring; giảm chuyển động; theme sáng.
3. Trang tìm vần theo hướng đã chọn (#6–#13).
4. Nghĩa từ / Từ điển (#14).
5. Sổ vần (#15; #16 nếu anh đồng ý).
6. Tài khoản / form / đăng nhập (#17–#19).
7. Admin (#20).

## Câu hỏi cho anh

1. Chọn hướng **A, B hay C**?
2. Với B: mặc định **Gọn** (lưới chip) hay **Đầy đủ** (danh sách có nghĩa)?
3. #8: chỉ tô **phần vần** (h**ải** q**uân**) thay vì cả âm tiết, được không?
4. #16: gom sổ vần theo từ gốc, được không?
5. Thêm **theme sáng** (theo cài đặt hệ thống) không?

Ghi chú: trong lúc review, em tạo tài khoản `ui-review@example.test` để chụp trang đăng nhập và trang admin, rồi đã xoá.
Sự kiện của tài khoản này đã được ẩn danh, nhưng khoảng 20 lượt tìm thử vẫn nằm trong số liệu dashboard 7 ngày.

---

## Kết quả (12/10/2026)

Anh chọn **B + C**: mỗi sổ vần là một bài; mặc định Gọn trên điện thoại, Đủ trên máy tính; chỉ tô phần vần; thêm theme sáng
theo hệ thống; không gom sổ theo từ gốc. Ảnh chụp sau khi làm: [`.docs/ui-review/after/`](ui-review/after/).

| # | Trạng thái | Ghi chú |
|---|---|---|
| 1–3 | Đã sửa | Lưu và mở nghĩa chạy khi tắt JS (đã thử bằng Chrome tắt script); không còn lỗi `#save-book`. |
| 4 | Đã sửa | Gõ thì *thay* URL (`hx-replace-url`), không thêm mục lịch sử. |
| 5 | Đã sửa | Một dòng trạng thái ẩn (`#rstatus`) đọc "500+ vần cho …"; vùng kết quả không còn `aria-live`. |
| 6 | Đã sửa | Nav lên tab bar dưới đáy; ô tìm dính đầu trang; một hàng: ngôn ngữ · Lọc · mức chặt (cuộn ngang). Mobile thấy khoảng 25 vần mỗi màn thay vì 4. |
| 7 | Đã sửa | Nút lưu chuyển vào panel nghĩa (một chạm từ mọi kết quả); vùng chạm ≥ 44px. |
| 8 | Đã sửa | Chỉ tô phần vần (`vnphon.OnsetLen`). Mức thể hiện bằng nền + kiểu viền (đậm → nhạt → nét đứt) kèm chú thích, không chỉ bằng màu. |
| 9 | Một phần | Có tổng số ("500+ vần"), tiêu đề nhóm dính khi cuộn. Không có số đếm riêng từng nhóm (engine không trả). |
| 10 | Đã sửa | Tiêu đề nhóm EN ghi "Khớp N âm"; ký hiệu ARPAbet chỉ còn trong tooltip. |
| 11 | Đã sửa | Trạng thái rỗng/lỗi trung tính, có nút: tra từ điển, tìm vần tiếng Anh, nới mức chặt. |
| 12 | Đã sửa | Bộ lọc là bottom sheet trên điện thoại, dropdown trên máy tính; chạy không cần JS. |
| 13 | Đã sửa | Tab bar 4 mục (Tìm vần, Từ điển, Viết, Sổ vần); Admin ở nav máy tính và trong `/me`. |
| 14 | Đã sửa | Đầu mục từ ghi phần vần và thanh ("Vần **ay** huyền · **an** huyền"), nút Tìm vần nổi bật; quá 3 nghĩa thì gập lại. Làm sạch ký hiệu HNĐ: chưa (thuộc ingest). |
| 15 | Đã sửa | Xoá có "Hoàn tác" 5 giây; ghi chú hiện như chữ, chạm mới sửa; tay nắm ⋮⋮ sắp xếp được bằng phím ↑ ↓. |
| 16 | Không làm | Theo quyết định của anh. |
| 17–19 | Đã sửa | `/me`: Đăng xuất lên đầu, "Mật khẩu & đăng nhập", Xoá tài khoản gập lại. Nhãn "(tuỳ chọn)" cùng dòng. Nút Google có logo. |
| 20 | Đã sửa | Tab admin riêng, khoảng thời gian dạng segmented, 9 ô số liệu chia 3×3, chữ biểu đồ ≈ 11px trở lên, phím tắt duyệt slang j/k/a/r. |
| 21 | Đã sửa | Theme sáng, `prefers-reduced-motion`, focus ring 2px cho mọi phần tử. |

**Bàn viết (C):** `/write` (khách: nháp trong trình duyệt), `/books/{id}/write` (tự lưu sau 1,2 giây, nút Lưu khi không có JS).
Con trỏ ở câu nào thì tìm vần cho hai từ cuối câu đó (hoặc câu trước nếu đang ở dòng trống; hoặc đoạn đang bôi đen).
Chạm một vần → panel có "Chèn vào bài" và "＋ Lưu vào sổ" (lưu vào chính bài đó). Lời bài có trong trang sổ, export
txt/md/json và API (`lyrics` trong `GET/PATCH /api/v1/books/{id}`). Migration `00008_lyrics`. Bản nháp viết lúc chưa đăng nhập
được đề nghị chèn vào bài khi mở bàn viết sau khi đăng nhập.

Kích thước: CSS + JS tự viết ≈ 11 KB gzip (ngân sách 30 KB). Không có lỗi console/CSP trên mọi trang đã chụp.
