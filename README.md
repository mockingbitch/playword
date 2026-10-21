# Playword

Từ điển vần cho rapper: nhập một từ hoặc cụm, nhận lại những từ/cụm **có thật trong từ điển hoặc kho slang đã duyệt**,
cùng vần theo nhiều mức chặt, vần đơn đến vần 5 âm, ba giọng Bắc/Trung/Nam, tiếng Việt và tiếng Anh, kèm định nghĩa.
Go + HTMX, PostgreSQL, index trong RAM.

- Thiết kế: [.docs/adr/](.docs/adr/) · Câu hỏi và quyết định: [.docs/m0-review.md](.docs/m0-review.md) · Vận hành: [.docs/runbook.md](.docs/runbook.md)

## Chạy nhanh

```sh
make db-up && make migrate
make ingest-hnd                       # tiếng Việt: Hồ Ngọc Đức (~29k mục)
make freq-viwiki                      # tần suất từ Wikipedia tiếng Việt (~1,5 GB tải về, 1 phút đếm)
make ingest-en                        # tiếng Anh: CMUdict → Open English WordNet → tần suất phụ đề
make run                              # http://localhost:8020
go run ./cmd/playword-cli user-role you@example.com admin   # sau khi đăng ký, để vào /admin
```

Toàn bộ stack bằng Docker: `docker compose --profile app up -d --wait` (thêm `--profile obs` để có Prometheus + Grafana).

## Cấu hình (biến môi trường)

Khi phát triển: `cp .env.example .env` rồi điền giá trị. `make` và `docker compose` tự đọc `.env`; biến đã đặt sẵn trong
shell được ưu tiên hơn giá trị trong file. `.env` nằm trong `.gitignore`. Bản thân server chỉ đọc biến môi trường của process.

| Biến | Mặc định | |
|---|---|---|
| `DATABASE_URL` | (bắt buộc) | PostgreSQL ≥ 16 |
| `PLAYWORD_ADDR` / `PLAYWORD_METRICS_ADDR` | `:8020` / `:9090` | `/metrics` nằm ở cổng riêng; để trống thì tắt |
| `PLAYWORD_CONFIG_DIR` | `configs` | YAML luật vần |
| `PLAYWORD_RATE_LIMIT_RPS` / `_BURST` | `10` / `20` | giới hạn tìm vần mỗi IP; `0` để tắt |
| `PLAYWORD_TRUST_PROXY` | `false` | lấy IP từ `X-Forwarded-For` |
| `PLAYWORD_CACHE_SIZE` | `10000` | số danh sách kết quả giữ trong LRU |
| `PLAYWORD_IP_SALT` | ngẫu nhiên | **đặt cố định ở production** |
| `PLAYWORD_INSECURE_COOKIES` | `false` | `true` chỉ khi phát triển qua HTTP thường |
| `PLAYWORD_ADMIN_TOKEN` | — | Bearer token cho admin API (≥ 32 ký tự) |
| `PLAYWORD_GOOGLE_CLIENT_ID` / `_SECRET` | — | bật "Tiếp tục với Google" (đặt cả hai hoặc không đặt) |
| `PLAYWORD_PUBLIC_URL` | — | vd. `https://playword.vn` (chỉ scheme + host). Dùng cho link canonical, ảnh Open Graph, `sitemap.xml`; bắt buộc khi bật Google (redirect URI `{PUBLIC_URL}/auth/google/callback`) |
| `PLAYWORD_CONTACT_EMAIL` | — | email liên hệ hiện ở footer, Điều khoản và Quyền riêng tư |
| `PLAYWORD_INGEST_SCHEDULE` | — | vd. `hnd=168h,oewn=720h` |
| `PLAYWORD_ANALYTICS` | `true` | ghi sự kiện cho dashboard |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | — | bật tracing OTLP/HTTP |
| `PLAYWORD_LOG_LEVEL` | `INFO` | |

## Tính năng

- **Tìm vần** `/` · `GET /api/v1/rhymes?q=&lang=&dialect=&level=&min_syl=&max_syl=&match=&slang=&proper=&limit=&cursor=`
  - Tiếng Việt: mức `L0` (chuẩn) → `L1` (cùng bằng/trắc) → `L2` (khác thanh) → `L25` (rap-flex, mặc định) → `L3` (ép vần); ba giọng.
  - Tiếng Anh: `perfect` → `family` (mặc định) → `assonance`, vần tính từ nguyên âm có trọng âm.
  - Bộ lọc quyết định tập kết quả; xếp hạng theo từng bậc ([ADR-004](.docs/adr/ADR-004-xep-hang-do-gan-van.md)):
    1. khớp nhiều âm hơn;
    2. từ thường trước tên riêng;
    3. ít âm trùng chính từ đã tra;
    4. mức chặt hơn;
    5. khoảng cách âm nhỏ hơn (thanh, âm cuối, nguyên âm, âm đệm);
    6. cuối cùng mới đến tần suất và trend.
  - API trả thêm `distance` (khoảng cách âm) và `repeats_query` (số âm tiết trùng từ đã tra). `score` chỉ để phân định khi các bậc trên hoà.
- **Từ điển** `/dictionary` · `GET /api/v1/words/{lang}/{text}` · gợi ý khi gõ không dấu, Telex hoặc gõ sai.
- **Đăng nhập bằng Google** `/auth/google` (OAuth 2.0 + PKCE, chỉ nhận email đã xác minh). Xem cách cấu hình trong [runbook](.docs/runbook.md#đăng-nhập-bằng-google).
- **Website**: trang chủ `/`, `/about` (giới thiệu, các mức vần, ba giọng — ví dụ lấy trực tiếp từ index), `/sources` (nguồn và giấy phép, số mục từ theo nguồn), `/terms`, `/privacy`, trang 404, `robots.txt`, `sitemap.xml`, manifest + icon + ảnh Open Graph. `/?q=…` vẫn là trang tìm vần như trước. Phông Be Vietnam Pro (OFL) đóng gói trong `static/fonts`.
- **Bàn viết** `/write`: viết lời bên trái, vần cho từ cuối của câu đang viết hiện bên phải; chạm một vần để xem nghĩa, chèn vào bài hoặc lưu. Mỗi sổ vần là một bài (`/books/{id}/write`); khách viết nháp trong trình duyệt.
- **Kiểm tra vần** (ở Bàn viết): tô cùng màu các chuỗi từ ≥ 2 âm tiết vần với nhau trong cùng khổ, mỗi bộ vần một màu, ưu tiên cặp khớp chặt nhất; lặp từ được gạch chấm; kèm sơ đồ vần và mật độ vần. `POST /api/v1/rhymecheck`, bản chỉ đọc ở `POST /write/check` và `/books/{id}/check`. Thiết kế: [.docs/plans/rhyme-check.md](.docs/plans/rhyme-check.md).
- **Sổ vần** `/books` (đăng ký/đăng nhập, nút ＋ trên kết quả, kéo-thả sắp xếp, export txt/md/json, sổ public). Khách có sổ tạm 20 mục.
- **Slang** `/slang/new` → admin duyệt ở `/admin/slang`; `respelling` (cách đọc theo âm Việt) cho từ mượn; trend giảm nửa sau 90 ngày.
- **Dashboard** `/admin/dashboard`: tra gì nhiều lên, tra không ra kết quả, mức/giọng được dùng, slang được lưu nhiều.
- Lỗi API theo RFC 7807 `application/problem+json`. API dùng cookie phiên thì phải lấy CSRF ở `GET /api/v1/auth/csrf`
  và gửi lại qua header `X-CSRF-Token`.

## Tinh chỉnh giọng Nam/Trung

`configs/dialects.yaml` là gu nghệ thuật, chỉnh tay được. Mỗi lần chỉnh xong:

```sh
go run ./cmd/playword-cli dialect-check -file raw-wiktextract-data.jsonl.gz   # so với IPA Sài Gòn/Huế của Wiktionary
go run ./cmd/playword-cli rebuild-keys
```
Bộ luật hiện tại khớp IPA 96,2% (Nam) và 95,8% (Trung). Phần còn lệch là `anh`/`ách`: nếu biểu diễn chúng thì luật sẽ
không còn idempotent (ADR-001 §5).

## CLI

```
playword-cli migrate | ingest <hnd|oewn|cmudict|slang> [-file] [-dry-run] [-since] | rebuild-keys | freq
             parse | rhymes | search | user-role | user-delete | maintain | healthcheck
```
Slang từ file: CSV có dòng tiêu đề `headword,lang,gloss,example,respelling,tags` (tags cách nhau bởi `;`); mọi dòng vào hàng chờ duyệt.

## Phát triển và kiểm thử

```sh
make test               # unit, -race
make test-integration   # cần make db-up; mỗi test tạo rồi xoá DB riêng
make lint               # golangci-lint
make fuzz
make loadtest           # k6, cần server chạy với PLAYWORD_RATE_LIMIT_RPS=0
make generate sqlc      # sau khi sửa *.templ hoặc *.sql
```
CI (`.github/workflows/ci.yml`): lint, test (kèm Postgres và kiểm tra code sinh tự động có khớp không), benchmark nhánh gốc so với PR
trên cùng runner (fail nếu chậm hơn 30%), build và đẩy image lên GHCR.

## Số đo

| | |
|---|---|
| Tìm vần, kho thật (155k mục vi+en), 500 RPS | p99 5,5 ms (bật analytics 5,7 ms) |
| Index 500k mục tổng hợp | heap khoảng 660 MB, truy vấn xấu nhất không trúng cache khoảng 10 ms |
| Ingest lần đầu, 29k mục | 17 giây |

## Nguồn dữ liệu và giấy phép

| Nguồn | Dùng cho | Giấy phép | Ghi chú |
|---|---|---|---|
| Từ điển Tiếng Việt — Hồ Ngọc Đức, Free Vietnamese Dictionary Project ([gốc](https://www.informatik.uni-leipzig.de/~duc/Dict/)) | `hnd`: mục từ, định nghĩa | GPL | Qua bản StarDict của [catusf/tudien](https://github.com/catusf/tudien). Không nhúng dữ liệu vào image; mỗi định nghĩa đều ghi nguồn. |
| Wikipedia tiếng Việt, [wikimedia/wikipedia](https://huggingface.co/datasets/wikimedia/wikipedia) 20231101.vi | `viwiki-freq`: tần suất cụm 1–5 âm | CC BY-SA 4.0 | Chỉ lưu số đếm. Văn phong thiên về báo chí và bách khoa. |
| [CMU Pronouncing Dictionary](https://github.com/cmusphinx/cmudict) | `cmudict`: phát âm ARPAbet | BSD-2-Clause | Nguồn phát âm, không tạo mục từ. |
| [Open English WordNet 2025](https://github.com/globalwordnet/english-wordnet) | `oewn`: mục từ tiếng Anh | CC BY 4.0 (gốc Princeton WordNet) | |
| [FrequencyWords](https://github.com/hermitdave/FrequencyWords) `en_50k` (phụ đề OpenSubtitles 2018) | `en-subtitles-freq` | dữ liệu CC BY-SA 4.0, code MIT | Cụm nhiều từ được ước lượng. |
| Wiktionary tiếng Việt, bản trích của [kaikki.org](https://kaikki.org/viwiktionary/) | `wiktionary-vi`: mục từ (41k), IPA theo vùng; nghĩa gắn tag slang được đưa vào hàng chờ duyệt | CC BY-SA 4.0 | Ghi nguồn. Mục trùng với `hnd` được gộp lại, `hnd` đứng trước. |
| Đóng góp của người dùng | `community`: slang đã duyệt | — | Mọi slang đều được admin duyệt trước khi lên. |
