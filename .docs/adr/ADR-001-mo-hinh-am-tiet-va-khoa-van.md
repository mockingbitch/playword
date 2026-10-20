# ADR-001 — Mô hình âm tiết, khóa vần, phương ngữ

- Trạng thái: **Đề xuất (M0) — chờ duyệt**
- Ngày: 2026-10-08
- Liên quan: [ADR-002](ADR-002-index-rcu-va-cache.md), [ADR-003](ADR-003-schema-va-pipeline.md),
  [configs/rhyme_families.yaml](../../configs/rhyme_families.yaml), [configs/dialects.yaml](../../configs/dialects.yaml),
  câu hỏi mở trong [.docs/m0-review.md](../m0-review.md) (ký hiệu `Qn`).

## Bối cảnh

Rhyme engine là lõi sản phẩm và phải là package thuần (không DB/HTTP). Quy tắc vần là gu nghệ
thuật nên phần "chính tả → cấu trúc" phải **cố định, kiểm chứng được**, còn phần "cấu trúc → thế
nào là vần" phải **cấu hình được** (YAML) mà không phải sửa code hay sửa dữ liệu trong DB.

Vì vậy tách thành 3 tầng, mỗi tầng một package, phụ thuộc một chiều:

```
pkg/vnphon            chuỗi → []Syllable (chính tả chuẩn, không phụ thuộc YAML)   ← cố định
internal/domain/rhyme Syllable --DialectMapper(YAML)--> Syllable --Keyer(YAML)--> key  ← cấu hình
                      Ranker
```

## Quyết định

### 1. Tách âm tiết (`pkg/vnphon`)

Một cụm được tách thành âm tiết theo khoảng trắng và dấu `-`; dấu câu bị bỏ. Mỗi âm tiết:

1. **Chuẩn hóa**: lowercase → NFD.
2. **Tách thanh**: bỏ các dấu kết hợp U+0300 (huyền=2), U+0301 (sắc=3), U+0309 (hỏi=4),
   U+0303 (ngã=5), U+0323 (nặng=6) và ghi lại `Tone`. Không có dấu thì tone=1. Gặp từ 2 dấu thanh
   trở lên thì trả lỗi. Các dấu chỉ chất lượng nguyên âm (U+0302 mũ, U+0306 trăng, U+031B móc)
   được giữ, rồi chuyển lại về NFC. Vì xét dấu thanh **không phụ thuộc vị trí đặt** nên `hoà`
   và `hòa` cho kết quả giống hệt, không cần bảng riêng cho kiểu đặt dấu cũ/mới.
3. **Âm đầu (onset)**: longest-match trên bảng
   `ngh ng nh ch gh gi kh ph th tr qu b c d đ g h k l m n p r s t v x`.
   Chuẩn hóa `k, q → c`, `gh → g`, `ngh → ng`. Riêng `qu` thì cho onset `c`, medial `w`.
   Trường hợp đặc biệt của `gi`:
   - phần còn lại không có nguyên âm (`gì`, `gìn`, `gịt`) → onset `gi`, chữ `i` là nucleus `I`;
   - phần còn lại bắt đầu bằng `ê` (`giết`, `giếng`) → hiểu là `gi` + `iê`, nucleus `IE`
     (vì không có vần "êt/êng" sau gi; đây là chữ i của iê bị viết gộp). **Quy tắc này do tôi tự
     suy ra, đặc tả chưa nêu.**
4. **Vần (rime)**: tra **bảng vần tường minh** (mục 2), không suy ra bằng thuật toán.
   - Lý do: tiếng Việt chỉ có khoảng 160 vần, một tập hữu hạn. Các ca nhập nhằng như
     `ui`/`uy`, `ua`/`qua`, `oo`/`o`, `ai`/`ay`, `uơ`/`ươ` được quyết định từng dòng, người
     không viết code cũng đọc và duyệt được. Bảng này đồng thời là oracle cho test.
     5 bước trong đặc tả §1.1 vẫn là quy tắc dùng để *viết ra* bảng.
   - Sau `qu`, phần còn lại tra trong bảng con có medial `w` (`quy` → `y` → I, `quyên` → `yên`
     → IE, `qua` → `a` → A, không phải UO).
5. **Kiểm tra hợp lệ**, lỗi đều có kiểu `ErrInvalidSyllable{Reason}`:
   `no_vowel`, `unknown_rime`, `multiple_tones`, `stop_coda_tone` (coda p/t/c/ch chỉ đi với
   sắc hoặc nặng; `àt` là lỗi). Chính tả âm đầu được chấp nhận lỏng (`ki`/`ky`, `ghi`/`gi`),
   còn vần thì kiểm tra chặt.

Hai sửa đổi so với đặc tả §1.1, tôi ghi lại ở đây để anh xác nhận:
- §1.1 bước 3 viết rằng "`u` trước … `i` → medial". Điều này mâu thuẫn với bước 4 (`mui` = U + j).
  Tôi chọn: `ui` = U + j, ngoại trừ sau `q` (`qui` = c + w + I).
- Bổ sung vần `uơ` = medial w + OW (`thuở`, `huơ`, `quơ`), vì đặc tả thiếu.

### 2. Bảng vần (rime inventory)

Ký hiệu: `—` là không tồn tại; `?` là hiếm, vẫn đưa vào bảng nhưng cần đối chiếu corpus ở M2.
Hàng `w+` là có âm đệm (chữ o/u, hoặc u sau q).

| Nucleus | Ø | j | w | m | n | ng | nh | p | t | k (c) | ch |
|---|---|---|---|---|---|---|---|---|---|---|---|
| **A** | a | ai | ao | am | an | ang | anh | ap | at | ac | ach |
| w+A | oa | oai | oao (ngoáo, bổ sung ở M2) | oam | oan | oang | oanh | oap? | oat | oac | oach |
| **AW** | — | ay | au | ăm | ăn | ăng | — | ăp | ăt | ăc | — |
| w+AW | — | oay / uay | uau? (quạu) | oăm | oăn | oăng | — | — | oăt | oăc | — |
| **AA** | — | ây | âu | âm | ân | âng | — | âp | ât | âc | — |
| w+AA | — | uây | — | — | uân | uâng | — | — | uât | — | — |
| **E** | e | — | eo | em | en | eng | — | ep | et | ec | — |
| w+E | oe / que | — | oeo / queo | oem (ngoém, bổ sung ở M2) | oen / quen | oeng? | — | — | oet / quet | — | — |
| **EE** | ê | — | êu | êm | ên | — | ênh | êp | êt | — | êch |
| w+EE | uê | — | — | — | uên | — | uênh | — | uêt? | — | uêch |
| **I** | i / y | — | iu | im | in | — | inh | ip | it | — | ich |
| w+I | uy | — | uyu | — | uyn? | — | uynh | uyp? | uyt | — | uych |
| **O** | o | oi | — | om | on | ong, oong | — | op | ot | oc, ooc | — |
| **OO** | ô | ôi | — | ôm | ôn | ông | — | ôp | ôt | ôc | — |
| **OW** | ơ | ơi | — | ơm | ơn | — | — | ơp | ơt | — | — |
| w+OW | uơ | — | — | — | — | — | — | — | — | — | — |
| **U** | u | ui | — | um | un | ung | — | up | ut | uc | — |
| **UW** | ư | ưi | ưu | ưm? | ưn? | ưng | — | — | ưt | ưc | — |
| **IE** | ia / ya | — | iêu / yêu | iêm / yêm | iên / yên | iêng | — | iêp | iêt / yêt | iêc | — |
| w+IE | uya | — | — | — | uyên | — | — | — | uyêt | — | — |
| **UO** | ua | uôi | — | uôm | uôn | uông | — | — | uôt | uôc | — |
| **UOW** | ưa | ươi | ươu | ươm | ươn | ương | — | ươp | ươt | ươc | — |

Ghi chú:
- `oong`/`ooc` cho cùng kết quả như `ong`/`oc` (O + ng/k), nên `boong` và `bong` trùng khóa ở mọi
  mức. Đây là mức "hỗ trợ tối thiểu" như đặc tả cho phép.
- Ở M2 sẽ đối chiếu bảng với từ điển thật: vần nào có trong kho mà không có trong bảng sẽ được liệt
  kê trong báo cáo để bổ sung, không âm thầm bỏ qua.

### 3. Render: Syllable → chữ viết chuẩn (để có round-trip)

`Syllable` không giữ đủ thông tin để khôi phục đúng chữ viết gốc: c/k/q, i/y, kiểu đặt dấu cũ/mới
đều bị chuẩn hóa. Vì vậy property test được định nghĩa trên **cấu trúc**:
`Parse(Render(s)) == s` với mọi `s` trong kho, và `Parse(Render(Parse(w))) == Parse(w)` với mọi
từ `w`. Render dùng chính tả chuẩn: `k`/`gh`/`ngh` đứng trước I/E/EE/IE; `qu` khi có medial và
onset c; medial viết là `o` trước A/AW/E, là `u` trong các trường hợp còn lại; IE viết thành
`ia`/`ya`/`iê`/`yê` tùy có coda, có onset hay có medial; UO viết `ua`/`uô`; UOW viết `ưa`/`ươ`;
dấu thanh đặt trên nguyên âm chính: nguyên âm có dấu phụ đứng sau cùng (`ươ` thì đặt trên ơ), nếu không có thì đặt trên nguyên âm đầu tiên sau âm đệm. Vì vậy Render ra `hoà`, `thuỷ`, `mía`. Parser không phụ thuộc vào vị trí đặt dấu.

### 4. Khóa vần

Khóa của một âm tiết ghép các thành phần bằng `.`; coda rỗng viết là `_`. Khóa của cụm ghép khóa
các âm tiết bằng dấu cách. Khóa đầy đủ trong index có dạng `dialect|level|n|key`.
**Onset không tham gia khóa ở bất kỳ mức nào.**

| Mức | Thành phần | `tàn` | `ngày tàn` (n=2) |
|---|---|---|---|
| L0 | nucleus.coda.tone | `A.n.2` | `AW.j.2 A.n.2` |
| L1 | nucleus.coda.nhóm thanh (B: 1,2 · T: 3,4,5,6) | `A.n.B` | `AW.j.B A.n.B` |
| L2 | nucleus.coda | `A.n` | `AW.j A.n` |
| L2.5 | họ nucleus . lớp coda chéo | `A*.n` | `A*.j A*.n` |
| L3 | họ nucleus . lớp coda L3 | `A*.N*+W*` | `A*.J* A*.N*+W*` |

(Ví dụ tính với YAML đang dùng, tức Q1 = A: chỉ còn cặp chéo `[w,m]`.)

**Lớp coda chéo (L2.5).** Lấy union-find trên `cross_coda_equivalents`; mỗi coda thuộc đúng một
lớp, coda không nằm trong cặp nào thì tự là một lớp. Lớp được đặt tên bằng các thành viên xếp theo
thứ tự, nối bằng `+`. Union-find có tính bắc cầu: nếu anh khai `[j,n]` và `[n,ng]` thì `j~ng`.
Loader sẽ in các lớp đã gộp lúc khởi động để anh thấy được hệ quả này.

**Các mức phải lồng nhau: L0 ⊂ L1 ⊂ L2 ⊂ L2.5 ⊂ L3.** Theo đặc tả hiện tại thì L2.5 ⊄ L3: cặp chéo
`j~n` khớp ở L2.5 nhưng không khớp ở L3, vì J* và N* là hai họ khác nhau. Hậu quả của việc không
lồng nhau:
1. Tìm ở `level=L3` sẽ bỏ sót kết quả L2.5.
2. Với cụm nhiều âm tiết, cụm có âm 1 chỉ khớp ở L2.5 và âm 2 chỉ khớp ở L3 sẽ không khớp ở mức
   nào cả.

Đề xuất (**Q2**): lớp coda ở L3 = các họ coda **gộp thêm** các cạnh chéo. Với YAML hiện tại, kết
quả là L3 còn 3 lớp: `OPEN {_}`, `STOP {p,t,k,ch}`, `SON {m,n,ng,nh,j,w}`. Như vậy L3 khá lỏng,
gần với vần ép chỉ dựa trên nguyên âm (assonance).

Khi đã lồng nhau, mỗi lần tìm ở mức X chỉ cần **một** lookup bằng khóa mức X cho mỗi độ dài n.
Sau đó, với từng âm tiết khớp, tính mức chặt nhất mà âm đó đạt. **Mức của kết quả = mức của âm
khớp yếu nhất** (**Q3**); `level_bonus` tính theo mức này.

### 5. Phương ngữ

Thứ tự xử lý: `Parse` (chính tả chuẩn) → `DialectMapper.Map` → `Keyer`. Mapper chạy trước khi tính
họ L3, nên `tàn` ở giọng `nam` khớp L0 với `tàng`, đúng như đặc tả yêu cầu.

Ngữ nghĩa của mapper (bảng luật nằm trong YAML):
- Ba nhóm luật áp theo thứ tự cố định: `nucleus_rules` → `coda_rules` → `tone_merge`.
- **Mọi điều kiện đều xét trên âm tiết gốc**, không xét trên kết quả trung gian. Nhờ vậy thứ tự
  khai báo trong file không làm đổi kết quả, và không có luật nào bị kích hoạt dây chuyền.
- Mapper không đụng tới onset và medial.
- **Kiểm tra idempotent lúc nạp YAML** (fail-fast): liệt kê toàn bộ (vần hợp lệ × 6 thanh), khoảng
  một nghìn âm tiết, và kiểm tra `Map(Map(s)) == Map(s)`. Nếu vi phạm thì server không khởi động
  và in ra ví dụ cụ thể. Ví dụ một luật gây vi phạm: `nh → n` đặt cạnh `n → ng` với cùng điều kiện
  nucleus, vì chạy 2 lần thì `anh → an → ang`. Như vậy property test của đặc tả §7 trở thành một
  điều kiện kiểm tra lúc chạy, không chỉ là test.
- Index tính sẵn khóa cho cả 3 phương ngữ (xem ADR-002). Giá trị khởi điểm nằm trong
  `configs/dialects.yaml`, kèm danh sách các cặp sẽ trùng vần để anh nghe thử.

### 6. Vần đa âm tiết và căn vị trí (alignment)

- `MaxSyl = 5`. Query dài hơn 5 âm tiết thì chỉ lấy 5 âm tiết cuối và thêm `warnings[]` vào
  response.
- Phía query luôn lấy **hậu tố**, vì đó là cuối câu rap. `match` quy định vị trí của đoạn khớp
  trong **ứng viên**:
  `suffix` (mặc định) là ứng viên kết thúc bằng đoạn khớp; `prefix` là ứng viên bắt đầu bằng đoạn
  khớp (để đặt vào giữa câu); `any` là đoạn khớp nằm ở bất kỳ cửa sổ liên tiếp nào.
- Để phục vụ cả 3 chế độ bằng một index, index lưu **mọi cửa sổ liên tiếp** của ứng viên
  (n ≤ 5 nên tối đa 15 cửa sổ), mỗi posting kèm `[start,end)`. Response trả `alignment` lấy từ đây
  để UI highlight đúng các âm tiết khớp.
- Lùi dần như đặc tả: n giảm từ min(|q|, max_syl) xuống `min_syl`.

### 7. Xếp hạng

Giữ công thức ở §1.7 nhưng **chặn biên** các số hạng phụ. Lý do: phép lùi theo n và việc dừng
sớm (ADR-002) chỉ đúng khi `100 × matched_syllables` luôn thắng, tức **khoảng chênh** giữa giá trị
lớn nhất và nhỏ nhất của tổng các số hạng phụ phải nhỏ hơn 100. Đặc tả dùng `10·log10(freq+1)`;
nếu `freq` là số lần xuất hiện thô (đến 10⁹) thì riêng số hạng này đã lên tới 90. Đề xuất (**Q7**):

| Số hạng | Đề xuất | Khoảng |
|---|---|---|
| level_bonus | như đặc tả | 6..30 |
| freq_bonus | `5 × zipf` (thang Zipf của wordfreq, 0–8), chặn ≤ 35 | 0..35 |
| trend_bonus | `15 × trend_score`, chặn ≤ 20 | 0..20 |
| độ dài | `−5 × |Δ số âm tiết|`, Δ ≤ 4 | −20..0 |
| **tổng phụ** | | **−14..85, khoảng chênh 99 < 100** ✓ |

Bản M0 của ADR này ghi trần 40/25 và chỉ kiểm tra "tổng phụ < 100". Như vậy là sai: khoảng chênh
khi đó là 109. Lỗi được `TestScore` phát hiện ở M1 và đã sửa thành 35/20.

Kết quả trùng query (so `normalized`) bị **lọc bỏ** thay vì trừ 1000.

**Thứ tự sắp xếp** (chủ sản phẩm chốt 2026-10-08):
1. Số âm tiết khớp, giảm dần.
2. Từ thường trước, tên riêng sau. Tên riêng là headword viết hoa, mang tag `proper_noun`. Chúng
   vẫn hiện nhưng luôn đứng sau từ thường có cùng số âm khớp.
3. Score giảm dần.
4. Text tăng dần, rồi đến entry id.

Bước 2 là một tầng sắp xếp, không phải điểm trừ. Muốn tên riêng luôn đứng sau bằng cách trừ điểm
thì mức trừ phải lớn hơn khoảng chênh 75 điểm của freq + trend + độ dài, và như vậy sẽ phá điều
kiện khoảng chênh < 100 ở trên. Trường `score` trong API giữ nguyên công thức §1.7.

### 8. Tiếng Anh (đã làm ở M4)

- **Phát âm**: CMUdict, lấy cách đọc chính và bỏ các biến thể `word(2)`. Từ không có trong CMUdict
  được suy ra từ gốc từ bằng luật hậu tố (`-s/-es/-'s/-ies/-ed/-ing/-er/-est/-ly/-ness`, kèm luật
  hữu thanh/vô thanh của `-s` và `-ed`), gán `phonetic_confidence = 0.7`. Không suy ra được thì mục từ
  vẫn có trong từ điển nhưng không gieo vần được. Không dùng thư viện g2p lớn.
- **Đoạn vần** (tương đương âm tiết của tiếng Việt): một nguyên âm cùng các phụ âm theo sau nó, cho
  tới nguyên âm kế tiếp. Phụ âm đứng trước nguyên âm đầu tiên không tham gia vần. Cụm nhiều từ được
  ghép phoneme của các từ lại rồi chia đoạn. Cột `syllables.phonemes` lưu mỗi đoạn một dòng.
- **Vần bắt đầu từ nguyên âm có trọng âm** (spec §1.6): hậu tố của query chỉ được bắt đầu tại một
  đoạn có trọng âm (1 hoặc 2). Vì vậy `money` (`AH1 N | IY0`) chỉ xét n=2, và các từ chỉ trùng phần
  `-y` không mang trọng âm như `tree` không được tính là vần. Query không có trọng âm nào (`the`)
  thì không áp ràng buộc này.
- **3 mức lồng nhau** `perfect ⊂ family ⊂ assonance`:
  - `perfect`: cùng nguyên âm, cùng việc có/không có trọng âm, cùng phụ âm theo sau. Ví dụ
    `money ~ honey`; còn `money ~ company` chỉ ở mức family vì `AH` của `company` không mang trọng âm.
  - `family`: nguyên âm cùng nhóm (AA/AO, IY/IH, EH/AE, UW/UH, AH/AX) và phụ âm cùng nhóm: mũi M N NG;
    tắc hữu thanh B D G; tắc vô thanh P T K; xát F V TH DH S Z SH ZH HH, gộp thêm tắc xát CH JH;
    tiếp cận L R W Y. Hai nhóm sau cùng spec chưa quy định, là đề xuất của tôi.
  - `assonance`: chỉ xét nhóm nguyên âm. Dùng *nhóm* chứ không dùng nguyên âm y hệt, để giữ tính lồng
    nhau với `family`.
- **level_bonus**: perfect 30, family 18, assonance 6. Mức mặc định là `family`, tương đương
  rap-flex của tiếng Việt.
- **Ca test §7 đã sửa theo I9**: `sunny` và `bunny` là vần perfect; `tummy` ở mức family; `mono`
  không vần.
- **Câu hỏi mở (Q14):** các cụm chứa chính từ của query (`old money`, `big money` khi tìm `money`) là
  lặp từ (identity rhyme). Có loại bỏ, hay để chúng xếp sau?

## Hệ quả

- Đổi gu vần (YAML) thì chạy `playword-cli rebuild-keys` (thay toàn bộ bảng `rhyme_keys` một
  cách nguyên tử) rồi các node build lại index (ADR-003 D1, Q4 = C).
- Bảng vần tường minh tốn công nhập liệu một lần, nhưng mọi ca nhập nhằng đều nằm ở một chỗ có
  thể duyệt.
- Rủi ro: luật cho phương ngữ Nam/Trung cần tai người nghe để chỉnh; giá trị khởi điểm chỉ là giả
  thuyết.
