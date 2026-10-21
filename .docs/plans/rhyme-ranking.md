# Plan: xếp hạng độ khớp ở màn tìm vần

Trạng thái: **đã triển khai** (2026-10-21). Quyết định: [ADR-004](../adr/ADR-004-xep-hang-do-gan-van.md).
Prompt gốc: [.docs/prompts/rhyme-ranking.md](../prompts/rhyme-ranking.md).

## Bộ đánh giá

- `.docs/ranking/queries.txt`: 68 truy vấn, gồm:
  - 25 câu tra nhiều nhất trong analytics;
  - 25 câu chọn tay có vần khó (-n/-ng, -t/-c, iê/uô/ươ, hỏi/ngã);
  - 20 câu ngẫu nhiên từ `.scripts/k6/queries.txt`.
- Chạy: `go run ./cmd/playword-cli search -limit 20 -queries .docs/ranking/queries.txt > out.tsv`. Index được dựng một
  lần cho cả tập. Các cột: rank, mức, `identical`, `distance`, `proper`.
- Đo: `python3 .docs/ranking/inversions.py out.tsv` đếm số lần một kết quả chặt hơn đứng dưới một kết quả lỏng hơn
  trong cùng bậc (cùng số âm khớp, cùng cờ tên riêng, cùng số âm trùng).
- `baseline.tsv` là trước thay đổi, `after.tsv` là sau.

| | Trước | Sau |
|---|---|---|
| Lần đảo thứ tự | 1.462 (57/63 truy vấn) | **0** |

## Ví dụ (top 6)

| "ngày tàn" trước | sau |
|---|---|
| hải quân (L25) | giày ban (L1) |
| giai đoạn (L25) | dày dạn (L2) |
| ngày càng (L25, trùng "ngày") | hương đăng bày án (L2) |
| tài sản (L25) | lạy van (L2) |
| tai nạn (L25) | máy khoan (L2) |
| tài năng (L25) | chạy án (L2) |

| "tàn" trước | sau |
|---|---|
| đoàn (L0, lệch âm đệm) | bàn (L0) |
| toàn (L0, lệch âm đệm) | hàn (L0) |
| hoàn | đàn |
| bàn | màn |

"cuộc đời": "qua đời", "ra đời" (trùng "đời") xuống sau "trả lời", "tạm thời", "tuyệt vời"…

## Kiểm chứng

- `internal/domain/rhyme/ranking_test.go`, kiểm các quyết định:
  - dày dạn trên hải quân; tài sản trên hải quân;
  - ngày càng sau các kết quả không trùng;
  - bàn → toàn → tán → tàng → tần;
  - tần suất chỉ phân định (đàn trên bàn);
  - tiếng Anh: honey → tummy → company.
- `FuzzRankingFilters`, với truy vấn và bộ lọc ngẫu nhiên:
  - mọi kết quả thoả bộ lọc (mức, số âm, cửa sổ theo vị trí vần, slang, tên riêng);
  - đúng thứ tự `better`;
  - xác định;
  - top K có cắt nhánh trùng với phần đầu của bảng xếp đầy đủ.

## Hiệu năng

Khoảng cách chỉ được tính cho kết quả còn có thể vào top K (ADR-004 §5); `Result` giữ 48 byte.

Cách đo: `rhymeindex` Lookup, base (HEAD) xen kẽ head, 6 vòng, `GOMAXPROCS=8` vì máy dùng chung bị tải nặng. So sánh
bằng benchgate ở ngưỡng 10%.

| Benchmark | base ns/op | head ns/op | Δ |
|---|---|---|---|
| Lookup_1syl | 1 419 886 | 1 414 497 | −0,4% |
| Lookup_2syl | 1 617 631 | 1 671 888 | +3,4% |
| Lookup_3syl | 1 899 380 | 1 892 112 | −0,4% |
| LookupLatency_1syl | 11 013 764 | 10 182 378 | −7,5% |

Bản đầu tiên tính khoảng cách cho mọi kết quả, chậm hơn khoảng 30%. Ba thay đổi đưa nó về mức trên:
- so sánh mã số nguyên trước chuỗi;
- giữ `Result` ở 48 byte;
- cắt nhánh lười.

## Panel "vì sao khớp"

`Engine.Explain` trả từng cặp âm tiết cùng các điểm lệch (`rhyme.Diff`). Nó dùng đúng luật của khoảng cách:
`TestExplainMatchesRanking` kiểm rằng tổng trọng số bằng `Distance` và số âm trùng bằng `Identical`, với mọi kết quả ở
cả ba giọng. `Service.Explain` lấy hit từ cùng bảng xếp hạng đã cache, nên panel luôn khớp với danh sách. Ví dụ:

- ngày → hải: ay ~ ai, nguyên âm gần, khác thanh bằng – trắc;
- tàn → quân: an ~ uân, lệch âm đệm, nguyên âm gần.

Tiếng Anh chưa có phần giải thích.

## Còn mở

- **R1** (ADR-004): cụm dài khớp một phần, so với từ ngắn khớp trọn.
- **Q13**: vần chéo Việt–Anh.
- Bậc "trùng từ" cho tiếng Anh.
