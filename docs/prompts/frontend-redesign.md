# Prompt: review & làm lại UI/UX frontend Playword

> Dán toàn bộ phần dưới đây vào một phiên Claude Code mở tại thư mục gốc repo.
> Muốn làm riêng một trang thì thêm dòng `Phạm vi: chỉ trang …` vào cuối.

---

Bạn là **senior frontend engineer kiêm product designer**, có kinh nghiệm làm UI server-rendered (HTMX) và thiết kế
cho người dùng mobile. Nhiệm vụ: **review rồi làm lại UI/UX** của Playword — từ điển vần cho rapper Việt (VI + EN).

## 1. Người dùng & bối cảnh

- Rapper/người viết lời, phần lớn dùng **điện thoại**, thường đang viết dở một câu và cần vần **ngay**: gõ một từ/cụm →
  quét danh sách → mở nghĩa → lưu vào sổ → quay lại viết. Mỗi giây và mỗi lần chạm đều đáng kể.
- Họ cần hiểu **vì sao** hai từ vần với nhau (mức L0 → L3, giọng Bắc/Trung/Nam, số âm khớp) mà không phải đọc tài liệu.
- Người quản trị (admin) dùng desktop: duyệt slang, xem dashboard.

## 2. Hiện trạng kỹ thuật (đọc trước khi làm)

- Go server render bằng **templ**: `internal/adapters/http/views/*.templ` (layout, search, word, dictionary, account,
  books, slang, dashboard) + model trong `views/model.go`. Sau khi sửa `.templ` phải chạy `go tool templ generate`
  (hoặc `make generate`); file `*_templ.go` là file sinh ra, không sửa tay.
- Tương tác bằng **HTMX 2** (`hx-get`, `hx-target`, `hx-swap`, partial khi có header `HX-Request`).
- CSS một file `static/app.css` (theme tối, token ở `:root`), JS nhỏ `static/app.js`, thư viện vendored:
  `htmx.min.js`, `sortable.min.js`. Static được embed vào binary.
- Danh sách route: xem `internal/adapters/http/server.go` và `slang.go`.

## 3. Ràng buộc cứng — không được phá

1. **CSP** `default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:`:
   không `<script>` inline, không `onclick=`, **không thuộc tính `style="…"`**, không `<style>` trong trang,
   không CDN, không Google Fonts từ xa. Font/icon nếu dùng thì vendor vào `static/` (nêu dung lượng và license) hoặc
   dùng SVG inline / system font.
2. **Không thêm build step** (không Node, Tailwind, bundler). CSS viết tay, có thể tách nhiều file trong `static/`.
   Muốn thêm thư viện JS phải nêu lý do, dung lượng (gzip) và vendor vào `static/`.
3. **Chạy được khi không có JS**: mọi form vẫn submit thường, link vẫn là link. HTMX/JS chỉ là tăng cường.
4. **CSRF**: form thường giữ `@csrfField(...)`; request HTMX dựa vào `hx-headers` trên `<body>` — giữ nguyên.
5. **Bộ gõ tiếng Việt**: xử lý phím phải bỏ qua khi `e.isComposing` (Telex/VNI đang ghép chữ).
6. **Quy tắc sản phẩm**: không bao giờ hiển thị từ không có trong dữ liệu; mỗi kết quả phải giải thích được
   (có nghĩa, có mức vần). Không bỏ phần ghi nguồn dữ liệu (license) ở footer.
7. **Hiệu năng**: trang tìm vần phải hiện kết quả ngay khi HTML về. Tổng CSS+JS tự viết (gzip) ≤ 30 KB; không layout shift
   khi HTMX chèn kết quả; không tải gì chặn render ngoài CSS.
8. Không đổi URL, tên tham số query, tên field form, route hay JSON API (người dùng có thể đã bookmark/share link,
   và test phụ thuộc vào chúng). Nếu thật sự cần đổi, hỏi trước.

## 4. Quy trình — làm theo thứ tự, dừng ở các điểm được đánh dấu

### Bước 1 — Review (chưa sửa code)
- Chạy app: `make db-up && make run` → http://localhost:8020 (có dữ liệu thật). Xem từng trang ở **390px** và
  **1280px**, cả khi đã đăng nhập, chưa đăng nhập, và tài khoản admin. Dùng Playwright/trình duyệt headless nếu có để
  chụp màn hình; không có thì đọc HTML render ra bằng `curl`.
- Đánh giá theo: luồng chính (tìm vần → nghĩa → lưu), hierarchy thông tin, khả năng quét danh sách dài, bộ lọc
  (mức/giọng/số âm/vị trí), empty/loading/error state, form & thông báo lỗi, điều hướng mobile, typography tiếng Việt
  (dấu chồng, line-height), contrast, focus ring, thao tác bàn phím, `aria-*`, vùng chạm ≥ 44px, `prefers-reduced-motion`.
- Viết báo cáo ngắn vào `docs/ui-review.md`: mỗi vấn đề gồm **trang · mô tả · mức độ (cao/vừa/thấp) · đề xuất**,
  xếp theo mức độ. Kèm 2–3 hướng thiết kế tổng thể (mô tả bằng lời + ASCII wireframe cho trang tìm vần), nêu
  ưu/nhược và hướng bạn khuyên chọn.

**⏸ DỪNG. Trình báo cáo, chờ tôi chọn hướng và duyệt danh sách việc.**

### Bước 2 — Design system
- Định nghĩa lại token trong `app.css`: màu (giữ theme tối là mặc định; thêm theme sáng qua
  `prefers-color-scheme` nếu hợp lý), thang khoảng cách, cỡ chữ, bo góc, bóng, trạng thái (hover/focus/active/disabled),
  màu theo mức vần L0–L3 dễ phân biệt cả với người mù màu. Contrast chữ ≥ 4.5:1.
- Chuẩn hoá component: nút, input, select, chip/tag, card, list item kết quả, thông báo, tab, empty state, skeleton.
- Dọn CSS chết, gom selector trùng.

### Bước 3 — Làm lại từng trang, theo thứ tự ưu tiên
1. **Tìm vần** (`/`, `search.templ`) — trang quan trọng nhất.
2. **Nghĩa từ** (`word.templ`, mở inline trong kết quả) và **Từ điển** (`/dictionary`, gợi ý khi gõ).
3. **Sổ vần** (`/books`, `/books/{id}`, kéo-thả, ghi chú, export) và nút ＋ lưu.
4. **Đăng nhập/Đăng ký/Tài khoản** (có nút Google, đặt mật khẩu, xoá tài khoản).
5. **Slang** (`/slang/new`) và **Admin** (dashboard, users, duyệt slang).

Mỗi trang là một lượt nhỏ: sửa `.templ`/CSS/JS → `go tool templ generate` → `go build ./...` →
`go test ./internal/adapters/http/...` → kiểm tra lại ở 390px và 1280px.

## 5. Kiểm tra trước khi báo xong

- `go vet ./... && go test -race ./...` xanh. Test trong `internal/adapters/http/*_test.go` có kiểm tra chuỗi trong HTML
  (ví dụ `href="/me"`, "Tiếp tục với Google"): nếu đổi chữ hiển thị một cách có chủ đích thì sửa test cho khớp, và liệt kê
  các thay đổi đó trong báo cáo. Không xoá test để cho qua.
- Không có lỗi CSP trong console trình duyệt.
- Tắt JS: tìm vần, đăng nhập, lưu vào sổ vẫn dùng được.
- Điều hướng toàn bộ bằng bàn phím được; focus luôn nhìn thấy.
- Lighthouse (nếu chạy được) Accessibility ≥ 95, Performance ≥ 95 trên mobile.

## 6. Báo cáo cuối (tiếng Việt, ≤ 20 dòng)

Đã đổi gì theo từng trang, ảnh chụp trước/sau (nếu có), các chữ hiển thị/test đã đổi, những gì chưa làm và đề xuất tiếp
theo. Khi gặp quyết định thuộc về sản phẩm (đổi tên tính năng, bỏ một bộ lọc, đổi thứ tự xếp kết quả…): **hỏi, không đoán.**
