# Nạp ngữ cảnh Maps vào Codex

Chạy với một worktree đã đăng ký và có index. Truyền câu hỏi, URL hoặc tên
màn hình. Maps dùng index cục bộ để tìm ký hiệu và trích mã trước khi mở lượt
Codex. Ví dụ trên Windows:

```powershell
python tools/codex_context/main.py --maps 'C:\tools\mce4d.exe' `
  --codex 'C:\tools\codex.exe' --repo 'C:\repos\my-project' `
  --worktree-id '<worktree-id>' --profile-id '<profile-id>' `
  --question 'Trang nào được render tại /settings/profile?'
```

Dùng đúng thư mục repo của binding đã chọn. Thiết lập `MCE4D_HOME` và tùy chọn
IPC như khi truy vấn CLI thông thường. Có thể truyền câu hỏi UTF-8 qua stdin
thay cho `--question`. Codex chạy ở chế độ đọc và trả JSONL gồm usage.

URL khớp chính xác ưu tiên component liên kết trong index. Với câu hỏi tiếng
Việt, Maps nhận các cụm quen dùng như “Tiếp tục”, “quay lại” và
“lưu bản nháp” ở cả dạng có dấu và không dấu. Tên màn hình hỗ trợ thu hẹp
ứng viên. Bước tìm ký hiệu chạy cục bộ và không gọi mô hình.

Với repo Go, Maps còn xếp hạng khai báo, đoạn kiểm tra trong thân hàm dài
và hàm trợ giúp cùng package. Có thể hỏi bằng thuật ngữ như “khóa trùng”,
“quá hạn” hoặc “số nguyên” mà không cần biết tên hàm.
Khi hỏi về TTL hay đổi tên trường, khai báo phù hợp được ưu tiên cùng
các đoạn sử dụng. Phần nạp sẵn vào Codex dùng tối đa tám đoạn Go nguyên vẹn
trong 6.000 byte JSON và giữ số dòng vật lý của mỗi đoạn.

Host có bản nguồn Go đã xác minh có thể truyền ánh xạ đường dẫn và byte nguồn
qua tham số `source_view` của `prepare_prompt`. Với yêu cầu đổi thông báo,
host nhận chuỗi hiện tại và số lần xuất hiện trong một lượt. Với yêu cầu đổi tên
trường hoặc interface, host nhận các mẫu mã nguyên văn, ngữ cảnh và số lần lặp
theo tệp. Các số đếm chỉ
áp dụng cho bản nguồn được cấp. Kết quả mơ hồ vẫn dùng các đoạn trích thông thường.

Đoạn trích thuộc snapshot đã lập chỉ mục và có thể thiếu phần liên quan.
Codex có thể đọc thêm khi host cho phép. Kiểm tra freshness của Maps trước
khi dựa vào snapshot để chỉnh sửa.
