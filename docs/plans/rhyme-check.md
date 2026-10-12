# Plan: Rhyme check ở Bàn viết

Trạng thái: **đã làm xong bước 1–6** (bước 7: sự kiện đã ghi, chưa có biểu đồ trên dashboard). Ngày 12/10/2026. Quyết định của anh ở mục 9; mục 3 đã sửa theo đó.

## 1. Mục tiêu

Ở Bàn viết, bật **Kiểm tra vần** thì các âm tiết vần với nhau được tô **cùng một màu**, như cách rapper tự tô
màu rhyme scheme trên giấy. Người viết thấy ngay:

- **Vần cuối câu**, theo sơ đồ AABB, ABAB…
- **Vần trong câu** (internal rhyme).
- **Vần đa âm** (multi), ví dụ "ngày tàn" ~ "may mắn": hai âm tiết liền nhau vần với hai âm tiết liền nhau.
- Câu nào chưa có vần.

Không làm: chấm điểm "hay/dở", gợi ý sửa lời. Mọi phần tô màu phải giải thích được: bấm vào một nhóm thì thấy phần vần
chung ("-an", "-ay") và mức khớp.

## 2. Nghiên cứu

| Nguồn | Ý chính dùng được |
|---|---|
| Hirjee & Brown, *Automatic Detection of Internal and Imperfect Rhymes in Rap Lyrics* (ISMIR 2009); *Using Automated Rhyme Detection to Characterize Rhyming Style in Rap Music* (EMR 2011) | Phát hiện cả vần cuối câu lẫn vần trong câu, vần không hoàn hảo; dùng **căn chỉnh chuỗi âm** (sequence alignment) để tìm vần đa âm. Đặc trưng vần đủ phân biệt phong cách từng rapper. |
| Malmi et al., *DopeLearning* (KDD 2016) | **Rhyme density**: với mỗi từ, độ dài chuỗi nguyên âm khớp dài nhất với khoảng hai câu trước, rồi lấy trung bình. Thước đo đơn giản và tương quan với cảm nhận "kỹ thuật" của rapper. |
| Highlight trong `<textarea>` | **CSS Custom Highlight API** (Baseline 2025) *không* tô được chữ trong `<textarea>`, chỉ dùng được với `contenteditable`. Với textarea, cách chuẩn là **lớp phản chiếu (backdrop mirror)**: một `div` nằm sau textarea trong suốt, chứa cùng nội dung và cùng font, các span tô nền ở đúng vị trí. |

**Áp vào tiếng Việt và vào Playword:**

- Mỗi âm tiết là một đơn vị vần, không cần chia âm tiết như tiếng Anh. Phần "nguyên âm + âm cuối + thanh" chính là phần vần
  mà engine đã mã hoá.
- Engine đã có đủ nguyên liệu. `Keyer.Key(level, syllable)` cho ra **lớp tương đương thật** ở mọi mức: cặp chéo n/ng,
  w/m được gộp bằng union-find, và các mức lồng nhau L0 ⊂ L1 ⊂ L2 ⊂ L25 ⊂ L3. Vì vậy *nhóm theo key là phân hoạch chuẩn*:
  không có trường hợp A vần B, B vần C mà A không vần C. Không cần thuật toán gom cụm.
- `Engine.Map(dialect, …)` áp luật giọng trước khi lấy key, nên tô theo giọng Bắc, Trung, Nam có sẵn.
- **Giữ nguyên tắc "không bịa từ":** rhyme check chỉ phân tích chữ của người viết, không sinh ra từ nào.
- Khác với tra cứu, ở đây tô cả âm tiết *không* có trong từ điển (tên riêng, slang mới), miễn `vnphon` phân tích được.
  Âm tiết không phân tích được (ví dụ "wifi") thì để trơn.

## 3. Thuật toán (gói thuần `internal/domain/rhyme`, file `check.go`)

Toàn bộ là **hàm thuần**, không I/O; giữ đúng quy tắc "rhyme engine là pure package". Đầu vào là văn bản, mức, giọng
và một hàm phát âm tiếng Anh (truyền vào; snapshot đã có `PronounceEN`). Đầu ra là danh sách token kèm nhóm.

1. **Tách token.** Tách văn bản thành dòng, rồi thành âm tiết, bằng cùng luật với `vnphon.SplitWords`. Mỗi token giữ:
   - dòng, vị trí trong dòng;
   - **offset theo UTF-16** (vì chuỗi trong JavaScript đánh chỉ số theo UTF-16, emoji sẽ lệch nếu dùng byte hay rune);
   - kết quả phân tích âm tiết.

   Khổ thơ ngăn cách bởi dòng trống.
2. **Lấy key.** Mỗi âm tiết qua `Map(dialect)` rồi `Key(level)`. Từ tiếng Anh dùng key của gói `en` ở mức tương ứng, và chỉ
   so với từ tiếng Anh (xem Q4).
3. **Chọn ứng viên.** Không phải âm tiết nào trùng key cũng đáng tô: tiếng Việt có rất nhiều hư từ trùng vần một cách tình
   cờ ("là", "và", "mà"). Ứng viên gồm:
   - **cuối câu:** 1–3 âm tiết cuối của mỗi dòng (bỏ dấu câu);
   - **trong câu:** âm tiết nằm trong một *chuỗi đa âm* (bước 4), hoặc trùng key với một âm tiết cuối câu trong cửa sổ;
   - hư từ trong danh sách dừng ngắn (Q3) không được làm ứng viên đơn lẻ, nhưng vẫn được tính khi nằm trong chuỗi đa âm.
4. **Chuỗi đa âm.** Với mỗi cặp dòng trong cửa sổ (mặc định trong cùng khổ, cách nhau ≤ 4 dòng), tìm các đoạn **liền
   nhau dài nhất** mà từng cặp âm tiết trùng key, theo kiểu "longest common substring" trên dãy key, O(n·m) mỗi cặp
   dòng. Đoạn dài ≥ 2 thành một chuỗi; ví dụ "ngày tàn" ~ "may mắn" ở L25. Đây là ý của Hirjee & Brown và Malmi, đơn
   giản hoá vì mỗi âm tiết tiếng Việt đã là một đơn vị.
5. **Nhóm và màu.**
   - Gom ứng viên theo key.
   - Một nhóm chỉ được tô khi có **≥ 2 lần xuất hiện của ít nhất 2 từ khác nhau** trong cùng cửa sổ. Lặp đúng một từ
     ("ngày… ngày…") không tính là vần (Q2).
   - Nhóm được đặt tên **A, B, C…** theo thứ tự xuất hiện đầu tiên, giống ký hiệu rhyme scheme.
   - Một âm tiết thuộc nhiều nhóm (ở các cửa sổ khác nhau) thì lấy nhóm gần nhất.
6. **Sơ đồ vần.** Chữ cái của nhóm chứa âm tiết cuối mỗi dòng (ví dụ `A A B B`); dòng không có vần ghi "·".
7. **Số liệu.**
   - **Mật độ vần** theo kiểu Malmi: trung bình, trên mỗi dòng, của độ dài chuỗi khớp dài nhất với hai dòng trước.
   - Số nhóm vần.
   - Tỉ lệ âm tiết có vần.

**Hiệu năng.** Lời tối đa 20.000 ký tự, khoảng 5.000 âm tiết. Parse cộng key là O(n). Phần chuỗi đa âm là O(cửa sổ × độ
dài dòng²) mỗi dòng, cỡ 5 × 15² × 300 dòng ≈ 340k phép so sánh. Mục tiêu dưới 10 ms trên server. Có benchmark và đưa vào
benchgate CI.

## 4. API và luồng

- `POST /api/v1/rhymecheck`
  - Body JSON: `{text, lang, dialect, level, scope}`.
  - Trả về: `{tokens:[{start,end,line,group,chain}], groups:[{id:"A", key, rime:"an", level, count}], scheme:["A","A","B","B"], density, stats}`.
  - Giới hạn 20.000 ký tự, dùng chung bộ giới hạn tần suất với tìm vần, timeout 2 giây.
  - Không cần đăng nhập: khách cũng có Bàn viết.
  - Không ghi lời vào analytics; chỉ đếm sự kiện `rhyme_check`, không kèm nội dung (Q6).
- `POST /write/check` (form): cùng phân tích nhưng trả **HTML** là bản lời đã tô màu, chỉ đọc. Đây là đường dự phòng khi
  không có JS, và cũng là chế độ xem/in.
- **Luồng có JS:**
  - Bật "Kiểm tra vần" → gửi request ngay; khi đang gõ thì debounce 500 ms.
  - Request cũ bị huỷ khi có request mới (`AbortController`).
  - Kết quả chỉ được áp nếu văn bản chưa đổi so với lúc gửi. Nếu đã đổi, lớp phản chiếu tạm hiện chữ trơn cho tới lần
    phân tích sau, để màu không bao giờ lệch khỏi chữ.

## 5. Giao diện

**Bàn viết**
- Nút **Kiểm tra vần** (công tắc) ở thanh dưới ô lời.
- Mức và giọng dùng chung bộ lọc của cột vần (mặc định Rap-flex, Bắc).

**Tô màu: lớp phản chiếu**
- Một `div.lyrics-mirror` nằm *dưới* textarea; textarea có nền trong suốt.
- Hai lớp dùng cùng font, cỡ chữ, line-height, padding, `white-space: pre-wrap`, `overflow-wrap: break-word` và chiều
  rộng thanh cuộn, để ngắt dòng giống hệt.
- Cuộn được đồng bộ; nội dung lớp phản chiếu được cập nhật ở mỗi `input`.
- Textarea vẫn là textarea thật, nên **bộ gõ Telex/VNI, undo, chọn chữ, autosave không bị ảnh hưởng**. Đây là lý do không
  chuyển sang `contenteditable`, vốn hay vỡ với bộ gõ tiếng Việt.

**Màu**
- Bảng màu phân loại 8 sắc, tô nền vừa đủ để chữ trên đó vẫn đạt độ tương phản.
- Kiểm bằng `validate_palette.js` của skill dataviz cho cả nền tối và nền sáng. Theme vàng–đen hiện tại đã chiếm màu
  vàng, nên bảng vần tránh trùng nó.
- Thứ tự màu cố định theo chữ cái A, B, C…, không xoay vòng. Từ nhóm thứ 9 trở đi dùng gạch chân kèm chữ cái (kiểu
  "Khác"), không sinh thêm màu.
- Không chỉ dựa vào màu: mỗi nhóm có chữ cái ở chú thích và ở cột sơ đồ vần, đọc được khi in đen trắng hoặc với người
  mù màu.

**Chú thích (legend)**
- Danh sách chip "A · -an · 6", "B · -ay · 4"…
- Bấm một chip để **chỉ hiện nhóm đó**, các nhóm khác mờ đi; bấm lại để bỏ.
- Bấm vào một từ được tô thì mở panel vần của từ đó, đúng luồng hiện có.

**Cột sơ đồ vần**
- Desktop: chữ cái A/B… bên trái mỗi dòng.
- Mobile: ẩn cột, sơ đồ hiện thành một dòng "AABB CDCD" phía trên chú thích.

**Số liệu:** "Mật độ vần 1,6 · 5 nhóm · 62% âm tiết có vần". Trang Giới thiệu sẽ có một câu giải thích các số này.

**Trợ năng**
- Lớp phản chiếu có `aria-hidden`.
- Một dòng `role=status` đọc tóm tắt cho trình đọc màn hình ("5 nhóm vần, sơ đồ A A B B").
- Có `prefers-reduced-motion`.

**Trang sổ vần:** phần "Lời" có nút **Xem vần**, hiện bản tô màu chỉ đọc, dùng cùng HTML với `/write/check`.

## 6. Kế hoạch triển khai

| Bước | Nội dung | Kiểm tra |
|---|---|---|
| 1 | `rhyme.Check` (thuần): tách token theo UTF-16, key, ứng viên, chuỗi đa âm, nhóm, sơ đồ, mật độ | Unit test với các khổ mẫu (AABB, ABAB, vần đa âm, lặp từ, hư từ, khác giọng, emoji); fuzz không panic; benchmark < 10 ms với 20k ký tự |
| 2 | Tầng app: `Service.RhymeCheck` (tiếng Anh qua snapshot), giới hạn, lỗi | Test bảng; tiếng Anh chỉ so với tiếng Anh |
| 3 | HTTP: `POST /api/v1/rhymecheck`, `POST /write/check`, view tô màu dùng chung | Test JSON, HTML, giới hạn độ dài, CSRF, rate limit |
| 4 | Bảng màu 8 sắc cho nền tối và sáng | Chạy validator cho cả hai theme; ghi kết quả vào plan |
| 5 | JS: lớp phản chiếu, đồng bộ cuộn, debounce và huỷ request, legend, chế độ chỉ một nhóm | Chụp ảnh ở 360/768/1440, so khớp ngắt dòng (đo bằng JS rằng mọi span nằm đúng dòng); thử gõ Telex khi đang bật |
| 6 | Cột sơ đồ, số liệu, bản xem ở trang sổ, câu giải thích ở trang Giới thiệu | Test HTTP và ảnh chụp |
| 7 | Đo thực tế: thời gian phân tích, sự kiện `rhyme_check` | Dashboard và benchgate |

Ước lượng: bước 1–3 khoảng 1,5 ngày; bước 4–6 khoảng 2 ngày; bước 7 nửa ngày.

## 7. Rủi ro

- **Lớp phản chiếu lệch chữ:** do font chưa tải xong, do thanh cuộn, do `letter-spacing`… Cách giảm:
  - chỉ bật tô màu sau `document.fonts.ready`;
  - dùng cùng một khối CSS cho cả hai lớp;
  - có test đo vị trí span ở nhiều độ rộng.
- **Nhiễu do hư từ:** chưa làm thì chưa biết danh sách dừng thế nào là vừa. Cách giảm:
  - bắt đầu với danh sách ngắn;
  - có chế độ "Chỉ cuối câu";
  - chỉnh theo phản hồi trên lời thật.
- **Trộn Việt–Anh:** vần chéo ngôn ngữ đang chờ quyết định Q13 từ trước, nên giai đoạn này chỉ so Anh với Anh.

## 8. Câu hỏi cần anh quyết

1. **Phạm vi mặc định**:
   - (a) chỉ vần cuối câu;
   - (b) cuối câu + chuỗi đa âm *(em đề xuất)*;
   - (c) mọi âm tiết trùng vần.

   Hai chế độ còn lại vẫn có trong công tắc.
2. **Lặp đúng một từ** ("ngày… ngày…") có tô như vần không? Đề xuất: **không**; chỉ gạch chấm và chú thích "lặp từ".
   Câu này liên quan Q14 (identity rhyme).
3. **Hư từ**: dùng danh sách dừng ngắn, đề xuất "là, và, mà, thì, của, cho, với, những, các, một, này, đó, đã, sẽ,
   đang, cũng, rồi, nhé, à, ơi"? Anh có muốn thêm hoặc bớt từ nào không?
4. **Cửa sổ so vần**: trong cùng khổ, tối đa cách 4 dòng (đề xuất), hay cả bài?
5. **Màu sắc**: màu chỉ tô **nền** của âm tiết (đề xuất, dễ đọc), hay tô cả chữ?
6. **Analytics**: chỉ đếm số lần dùng (đề xuất), hay lưu cả sơ đồ vần ẩn danh (không kèm lời) để thống kê kiểu vần
   phổ biến?

## Nguồn

- [Hirjee & Brown 2009, ISMIR](https://ir.webis.de/anthology/2009.ismir_conference-2009.116), [DOI](https://doi.org/10.5281/zenodo.1416160)
- [Hirjee & Brown 2011, Empirical Musicology Review](https://doaj.org/article/daf65bcc29124bba9d26603c6d4e1d3c)
- [Malmi et al., DopeLearning (arXiv 1505.04771)](https://arxiv.org/pdf/1505.04771), [bản KDD 2016](https://www.cs.helsinki.fi/u/htoivone/pubs/kdd_2016_malmi_et_al.pdf), [MIT Technology Review](https://www.technologyreview.com/2015/05/20/248788/machine-learning-algorithm-mines-rap-lyrics-then-writes-its-own/amp/)
- [MDN: CSS Custom Highlight API](https://developer.mozilla.org/en-US/docs/Web/API/CSS_Custom_Highlight_API), [CSS-Tricks: highlight trong textarea](https://css-tricks.com/?p=363724), [web.dev: contenteditable plaintext-only](https://web.dev/blog/contenteditable-plaintext-only-baseline?hl=en)

## 9. Quyết định đã chốt (12/10/2026)

1. **Chỉ tô chuỗi từ 2 âm trở lên.** Vần một âm tiết ("tàn" – "bàn") không tô.
2. **Gom nhóm theo cặp khớp chặt nhất.** Các mức được xét lần lượt từ chặt tới lỏng (tới mức đang chọn):
   - Ở mỗi mức, các cụm chưa có nhóm mà nối được với một nhóm đã có từ mức chặt hơn sẽ nhập vào **đúng một** nhóm đó.
     Ưu tiên nhóm có nhiều cặp nối nhất, rồi nhóm gần hơn tính theo số dòng.
   - Các cụm còn lại nối với nhau ở mức này thì thành nhóm mới.
   - Các nhóm đã có **không bao giờ gộp với nhau**.

   Ví dụ: "hải quân" – "hai tuần" (Khác thanh) là một màu, "ngày tàn" – "chạy án" (Khác thanh) là màu khác, dù hai
   nhóm vẫn vần Rap-flex với nhau.
3. **Lặp đúng một từ** không tính là vần, chỉ đánh dấu "lặp từ". Hai chỗ được đánh dấu:
   - từ cuối câu trùng nhau;
   - đoạn ≥ 2 từ giống hệt nhau.

   Một cặp âm tiết là cùng một từ thì cắt chuỗi vần tại đó.
4. **Danh sách hư từ** như đề xuất. Hư từ vẫn được nằm trong chuỗi, nhưng chuỗi phải có ít nhất một từ không phải hư từ.
5. **Cửa sổ so vần**: trong cùng khổ (khổ ngăn cách bởi dòng trống), không giới hạn khoảng cách dòng.
6. **Chỉ tô nền**, chữ giữ màu thường.
7. **Analytics lưu sơ đồ vần** (chuỗi chữ cái theo dòng), số nhóm và mật độ; không bao giờ lưu lời. Trang Quyền riêng tư
   phải ghi thêm điều này.
8. **Tiếng Anh** để sau: cần tách âm tiết tiếng Anh, và còn chờ Q13 (vần chéo Việt–Anh). Từ không phải âm tiết tiếng Việt
   sẽ cắt chuỗi.

## 10. Kết quả triển khai (12/10/2026)

- **Thuật toán:** `internal/domain/rhyme/check.go`, gói thuần, có test, fuzz và benchmark.
  - Bài 16 dòng: 0,7 ms.
  - Bài 20.000 ký tự dày vần nhất có thể: 14 ms. Mục tiêu 10 ms không đạt cho trường hợp xấu nhất, nhưng không ảnh
    hưởng người dùng vì yêu cầu đã debounce 500 ms.
- **Một sửa đổi so với plan:** khi hai chuỗi chồng nhau, *nhóm chặt hơn* thắng, rồi mới tới chuỗi dài hơn. Chuỗi bị mất
  một âm thì mất màu cả chuỗi, nên không bao giờ còn mảnh một âm tiết được tô (có test và fuzz kiểm tra).
- **API:** `POST /api/v1/rhymecheck`.
  - Không có JS: `POST /write/check`, ra bản tô màu chỉ đọc có cột sơ đồ.
  - Trang sổ: `GET /books/{id}/check`.
  - Sự kiện `rhyme_check` lưu sơ đồ, số nhóm, số dòng, mật độ; không lưu lời; tối đa một lần mỗi phút. Trang Quyền riêng
    tư đã ghi điều này.
- **Màu:** 5 màu nền, kiểm bằng validator của skill dataviz với `--pairs all`.

  | Theme | CVD (≥ 8) | Mắt thường (≥ 15) | Tương phản chữ |
  |---|---|---|---|
  | Tối | 10,1 | 15,4 | 4,6–10:1 |
  | Sáng | 12,8 | 20,5 | 5,1–13:1 |

  - Màu được tìm sao cho cũng khác rõ với nền chưa tô.
  - Validator báo FAIL ở hai kiểm tra *dải độ sáng* và *chroma*. Hai kiểm tra đó dành cho nét vẽ biểu đồ trên nền; ở đây
    màu nằm sau chữ nên bắt buộc phải tối hoặc nhạt hơn dải đó.
  - Bộ 8 màu gốc chỉ đạt 4,6 / 1,0 khi pha tối, nên không dùng.
  - Từ nhóm thứ 6 trở đi, các màu được dùng lại kèm gạch chân nét đứt, rồi nét chấm.
- **Lớp phản chiếu:** đã đo bằng Chrome ở 390/1024/1440px; chữ, chiều cao và chiều rộng trùng khớp với textarea. Gõ ở đầu
  bài trong lúc đang bật thì màu vẫn bám đúng chữ (offset được dời theo chỗ sửa).
- **Chưa làm:**
  - cột sơ đồ bên lề ở chế độ trực tiếp (hiện ghi "Sơ đồ: …" trong khung chú thích; bản chỉ đọc có cột);
  - tiếng Anh;
  - biểu đồ sơ đồ vần phổ biến trên dashboard.

## 11. Sửa UI/UX sau phản hồi (12/10/2026)

**Phản hồi:** "các bộ vần khác nhau vẫn bị bôi chung một màu".

**Nguyên nhân:** bài thử có 11 nhóm, mà bảng chỉ có 5 màu nền tối. Nhóm F–J dùng lại màu của A–E, chỉ khác gạch chân rất
mờ. Thêm vào đó, các màu nền tối (xanh rêu, tím than, nâu) đục và khó phân biệt.

**Đã sửa:**
- **Chữ đen trên nền sáng.** Khi bật kiểm tra, lớp phản chiếu *vẽ chữ*; textarea chỉ còn con trỏ và vùng chọn (vẫn là
  textarea thật). Nhờ vậy dùng được nền sáng với chữ đen.
- **7 màu**, cùng một bộ cho cả hai theme: `#41bdd1 #fe77a6 #efe345 #35f3b5 #f68c11 #9690f1 #d9c6fe`.
  - Validator, `--pairs all`: CVD 8,0, mắt thường 15,8 (PASS ở cả tối lẫn sáng).
  - Chữ đen trên nền tô tương phản 7–15:1.
  - Theme sáng: tương phản với nền giấy dưới 3:1, được bù bằng viền mảnh và nhãn chữ cái.
- **Gán màu theo khổ** (`views.CheckStyles`):
  - các nhóm trong một khổ không bao giờ trùng màu;
  - mỗi khổ lấy các màu lâu chưa dùng nhất, nên hai khổ liền nhau khác màu khi đủ 7 màu;
  - chỉ khi một khổ có hơn 7 nhóm mới dùng gạch chân nét đứt, rồi nét chấm.
- **Nhãn chữ cái (A, B, C…)** trên đầu mỗi chuỗi, ngay trong lời.
- **Ô viết tự giãn** theo bài khi đang kiểm tra: thấy cả bài đã tô, không phải cuộn bên trong ô.
- **Chip chú thích gọn:** chữ cái, phần vần, số chuỗi. Danh sách cụm nằm trong tooltip và nhãn cho trình đọc màn hình.
- **Analytics:** server chặn còn tối đa một sự kiện mỗi phút mỗi người. Trước đó, khi người dùng tải lại trang, các lần
  ghi chỉ cách nhau khoảng 15 giây.
