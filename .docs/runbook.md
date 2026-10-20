# Playword — Runbook

Cho người vận hành. Thiết kế: [ADR-001](adr/ADR-001-mo-hinh-am-tiet-va-khoa-van.md) ·
[ADR-002](adr/ADR-002-index-rcu-va-cache.md) · [ADR-003](adr/ADR-003-schema-va-pipeline.md).

## Thành phần

| | |
|---|---|
| `playword` | Web + API. Giữ toàn bộ index trong RAM (khoảng 1 GB với 500k mục); tự build lại khi có NOTIFY `playword_index`, poll dự phòng 60 giây. |
| `playword-cli` | migrate, ingest, rebuild-keys, freq, maintain, user-role, user-delete, healthcheck. |
| PostgreSQL ≥ 16 | Source of truth. Extension: `pg_trgm`, `citext`. |
| Cổng | `:8020` web (public, đặt sau TLS), `:9090` `/metrics` (chỉ nội bộ). |

## Triển khai / nâng cấp

1. Build image (`docker build --build-arg VERSION=$(git rev-parse --short HEAD) .`), hoặc lấy từ GHCR (CI đẩy lên khi merge vào main).
2. `playword-cli migrate up`. Migration chỉ tiến, có thể chạy lại nhiều lần; image có sẵn `playword-cli`.
3. Nếu bản phát hành đổi `KeyerVersion` hoặc `configs/*.yaml`: `playword-cli rebuild-keys` **trước khi** chuyển traffic. Nếu bỏ qua bước này, `/readyz` của server mới trả 503 kèm lý do "different rhyme config", và server cũ vẫn phục vụ bình thường.
4. Rolling restart. Server mới chỉ nhận traffic khi `/readyz` trả 200 (khoảng 8 giây để build index với 155k mục).

Biến môi trường bắt buộc ở production: `DATABASE_URL`, `PLAYWORD_IP_SALT` (cố định), `PLAYWORD_TRUST_PROXY=true` khi đứng sau
proxy. Nên đặt: `PLAYWORD_ADMIN_TOKEN` (≥ 32 ký tự), `PLAYWORD_INGEST_SCHEDULE`, `OTEL_EXPORTER_OTLP_ENDPOINT`.
Cấu hình sai thì process thoát ngay khi khởi động và in ra lỗi.

## Đăng nhập bằng Google

1. Google Cloud Console → *APIs & Services* → *OAuth consent screen*: loại External, scope `openid`, `email`, `profile`
   (không cần Google duyệt app vì chỉ dùng các scope này).
2. *Credentials* → *Create credentials* → *OAuth client ID* → loại **Web application**.
   *Authorized redirect URIs* phải khớp từng ký tự:
   - production: `https://playword.vn/auth/google/callback`
   - dev: `http://localhost:8020/auth/google/callback` (`make run` đã đặt sẵn `PLAYWORD_PUBLIC_URL=http://localhost:8020`)
3. Đặt `PLAYWORD_GOOGLE_CLIENT_ID`, `PLAYWORD_GOOGLE_CLIENT_SECRET` và `PLAYWORD_PUBLIC_URL` (khi dev thì điền vào `.env`;
   ở production thì dùng secret của nền tảng deploy), rồi restart. Log in ra
   `google sign-in enabled redirect=…`, và trang `/login` hiện nút "Tiếp tục với Google".

Hành vi:
- Tài khoản gắn với `sub` của Google, nên vẫn đúng người khi họ đổi email bên Google.
- Lần đầu đăng nhập bằng Google mà email đã có tài khoản mật khẩu: liên kết vào tài khoản đó, **vô hiệu mật khẩu cũ và
  đăng xuất mọi phiên**. Lý do: email chưa từng được xác minh ở Playword, nên người đăng ký trước có thể không phải chủ
  email (pre-account hijacking). Chủ tài khoản đặt lại mật khẩu ở `/me` nếu muốn.
- Tài khoản chỉ có Google thì không đăng nhập bằng mật khẩu được, cho tới khi đặt mật khẩu ở `/me`. Khi xoá tài khoản,
  người dùng xác nhận bằng cách gõ lại email.
- Ai đó báo lỗi "redirect_uri_mismatch": `PLAYWORD_PUBLIC_URL` không khớp URI đã khai ở Console (http/https, cổng, dấu `/`).

## Theo dõi

- Grafana: `docker compose --profile obs up -d` → http://localhost:3000, dashboard "Playword".
- Tín hiệu chính:
  - `histogram_quantile(0.99, playword_http_request_duration_seconds_bucket{route="GET /api/v1/rhymes"})`: dưới 50 ms (DoD của M3).
  - `rate(events_dropped_total[5m]) > 0`: buffer analytics bị đầy.
  - `playword_index_generation` không tăng sau khi ingest: index không được build lại.
  - `go_memstats_heap_inuse_bytes`: khoảng 2× kích thước snapshot trong lúc build. Cần đặt memory limit ≥ 2 GB.
- Dashboard nghiệp vụ: `/admin/dashboard` (role admin hoặc Bearer token).

## Sự cố thường gặp

| Triệu chứng | Nguyên nhân / cách xử lý |
|---|---|
| `/readyz` 503 "index not loaded yet" kéo dài | DB chậm hoặc không kết nối được, xem log `index build failed`. Server không nhận traffic cho tới khi build xong. |
| `/readyz` 503 "different rhyme config" | YAML hoặc code sinh khóa đã đổi mà chưa chạy `playword-cli rebuild-keys`. Chạy lệnh đó; các node tự build lại qua NOTIFY. |
| Kết quả tìm vần không đổi sau khi ingest | NOTIFY bị lỡ: poll 60 giây sẽ tự bắt lại. Muốn ép ngay: `select pg_notify('playword_index','manual')`. |
| `events_dropped_total` tăng | DB ghi chậm. Request không bị ảnh hưởng (theo thiết kế). Kiểm tra lock và I/O; có thể tạm tắt bằng `PLAYWORD_ANALYTICS=false`. |
| `ingest_runs.status = partial/failed` | Xem `ingest_runs.errors` (gồm `skip_reasons` và tối đa 1.000 lỗi mẫu). Chạy lại được ngay, vì ingest idempotent. |
| Ingest báo `ingest already running` | Một node khác đang giữ advisory lock `ingest:<source>`. Chờ, hoặc kiểm tra `pg_locks`. |
| Người dùng báo bị khoá đăng nhập | Sai mật khẩu 10 lần thì bị khoá 15 phút; khoá tự hết hạn. Mở khoá tay: `update users set failed_logins=0, locked_until=null where email=…`. |
| Lộ admin token | Đổi `PLAYWORD_ADMIN_TOKEN` rồi restart. Token cũ hết hiệu lực ngay. |

## Dữ liệu

- **Ingest tay:** `playword-cli ingest hnd` / `oewn` (`-dry-run` để xem trước), hoặc `POST /api/v1/admin/ingest/{source}`.
- **Ingest định kỳ:** `PLAYWORD_INGEST_SCHEDULE="hnd=168h,oewn=720h"`. Tải có điều kiện, nguồn không đổi thì bỏ qua.
- **Đổi luật vần:** sửa `configs/*.yaml` → `playword-cli rebuild-keys` → deploy YAML mới (giá trị hash phải khớp).
- **Bảo trì:** server tự chạy (partition, retention 180 ngày, refresh view mỗi 10 phút, dọn session hết hạn). Có thể chạy tay: `playword-cli maintain`.

## Backup / restore

```sh
DATABASE_URL=… .scripts/backup.sh                    # pg_dump -Fc, bỏ dữ liệu rhyme_keys (dựng lại được)
DATABASE_URL=… .scripts/restore.sh file.dump         # vào DB TRỐNG, rồi chạy rebuild-keys
```
Nên backup hằng ngày và giữ 30 bản. Mỗi tháng phải thử restore một lần trên DB tạm: chạy `restore.sh`, khởi động server,
rồi kiểm tra `/readyz` và một truy vấn tìm vần.

## Xoá tài khoản (theo kiểu GDPR)

Người dùng tự xoá tại `/me` (phải nhập lại mật khẩu) hoặc qua `DELETE /api/v1/me`. Khi nhận yêu cầu qua hỗ trợ:

```sh
playword-cli user-delete -confirm user@example.com
```

Một transaction sẽ: ẩn danh hoá mọi `events` của người đó (`user_id = null`, `anon_id = 'deleted:…'`), xoá tài khoản,
và xoá dây chuyền sang `sessions`, `rhyme_books`, `rhyme_book_items`. Slang người đó đã gửi chỉ lưu email trong
`slang_submissions.submitted_by`; nếu được yêu cầu thì xoá thêm:
`update slang_submissions set submitted_by = 'deleted' where submitted_by = 'user@example.com'`.
Các backup cũ sẽ hết hạn theo chu kỳ giữ backup.
