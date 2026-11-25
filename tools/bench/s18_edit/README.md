# Nghiệm thu sửa mã S18 trên Windows native

Bộ này có 30 câu hỏi tiếng Việt, 10 ca mỗi repo. Nguồn Maps được lấy từ
commit đầu vào ghi trong fixture, với ba nhóm internal/model, internal/search và
internal/contextpack. Hai nguồn ngoài là google/uuid v1.6.0 và go-humanize v1.1.0.
Danh sách tệp và SHA-256 được khóa trong [fixture/sources.json](fixture/sources.json).
Đề bài, đáp án và fixture Go tự viết nằm trong [edit_cases.py](edit_cases.py).
Harness chỉ gửi câu hỏi cho Codex. Nhánh C nhận thêm bằng chứng Maps.

## Chạy trên Windows

Cần Windows amd64, Git, CPython 3.10 trở lên, Go và gofmt trên PATH. Lần đo
gốc dùng Go 1.27.4. Bản sao Git Maps phải chứa commit đầu vào. Nếu cache Go
còn thiếu, lấy module đã pin từ repo Maps bằng các lệnh sau.

```powershell
go mod download
go mod download github.com/google/uuid@v1.6.0 github.com/dustin/go-humanize@v1.1.0
```

Runner kiểm hash trước khi dùng nguồn. Fixture dùng Go với cache cục bộ và
GOTOOLCHAIN=local. Lệnh chuẩn bị nguồn của Git và Go không gọi script repo mẫu.

Từ repo Maps, đặt đường dẫn CPython và đầu ra mới trên ổ cục bộ.

```powershell
$Python = 'C:\path\to\python.exe'
$Suite = Join-Path (Get-Location) 'tools\bench\s18_edit'
$Run = Join-Path $env:LOCALAPPDATA ('MCE4D-S18\' + (Get-Date -Format yyyyMMdd-HHmmss))
& $Python -B "$Suite\runner.py" prepare --output $Run
& $Python -B "$Suite\runner.py" replay-check --output $Run
& $Python -B "$Suite\runner.py" preflight --output $Run
```

- **prepare** dựng ba bản nguồn trong thư mục source để dùng làm đầu vào nếu
  cần index live. Hash từng tệp phải khớp trước khi làm việc.
- **replay-check** dựng lại 30 prompt có bằng chứng và đối chiếu hash với lần
  nghiệm thu đã ghi. Bước này không mở Codex.
- **preflight** chạy 30 bản sửa tham chiếu và 23 đối chứng hành vi. Suite tự
  tạo test Go và loại tệp kiểm thử của repo mẫu khỏi thư mục biên dịch.

Chỉ mở bước đo khi đã có quyền sử dụng Codex CLI và model gpt-6.2-sol. Lệnh
run mở 60 lượt với cùng công cụ đọc, quyền và mức suy luận. Mỗi lượt dùng
bản sao riêng. Codex chạy ở chế độ chỉ đọc và đề xuất JSON. Runner đặt cờ
tắt shell và tra web, đồng thời report kiểm số sự kiện gọi shell bằng 0.
Harness áp dụng đề xuất và đối chiếu vùng sửa, rồi chạy fixture khi bản sửa
khớp đáp án sau gofmt. Bản đóng gói thêm cờ hạn chế công cụ so với lượt gốc,
nên token của lượt mới được đo lại.

```powershell
$Codex = 'C:\path\to\codex.exe'
& $Python -B "$Suite\runner.py" run --output $Run --codex $Codex
& $Python -B "$Suite\report.py" --output $Run
```

Mặc định runner nạp các đoạn trích Maps đã lưu trong
[fixture/maps-evidence.json](fixture/maps-evidence.json). Để đo cả bước truy
xuất từ index mới, dùng đường live. Mỗi bộ kết quả ghi rõ replay hoặc live.
Mỗi lượt mới dùng thư mục output riêng.

### Đo truy xuất từ index Maps hiện tại

Tạo ba checkout fixture từ thư mục source của bước prepare, rồi đăng ký
và index theo mục Windows của [hướng dẫn protected profile](../../state_profile/README.md).
Dùng Go profile cú pháp và binary Maps hỗ trợ locate Go với mode=evidence,
match=question. CLI cần trả coverage GO_QUESTION_CANDIDATES. Ghi worktree,
profile, generation và thư mục protected profile cho từng fixture trong
bindings.json cục bộ. Dùng cấu trúc sau cho maps, uuid và humanize.

```json
{
  "maps": {
    "worktree": "<worktree-id>",
    "profile": "<profile-id>",
    "generation": "<generation-id>",
    "profile_home": "C:\\MCE4D-Control\\s18-profile"
  }
}
```

Dùng output mới đã qua prepare và preflight.

```powershell
& $Python -B "$Suite\runner.py" run --output $Run --codex $Codex --maps 'C:\path\to\mce4d.exe' --bindings 'C:\path\to\bindings.json'
& $Python -B "$Suite\report.py" --output $Run
```

Runner kiểm phạm vi trả về và đối chiếu đoạn trích với byte nguồn đã khóa.
Thiếu bằng chứng ở một ca làm bước kiểm đầu vào dừng trước khi mở Codex.
Nếu một truy vấn lỗi trong lượt đo, report giữ ca đó và đánh dấu chưa đạt.

## Kiểm lại kết quả đã lưu

Job **S18 replay (windows-2025)** trong [CI](../../../.github/workflows/ci.yml)
tái tạo 30 prompt từ gói đã lưu, kiểm 30 bản sửa tham chiếu và 23 đối chứng.
Job cũng tạo ba worktree và index trong profile cô lập của tài khoản Windows
thường, mở lại ảnh index Maps lớn hơn 4 MiB rồi truy vấn 30 prompt live. Bước
live đối chiếu từng prompt với hash đã lưu và kiểm ảnh SQLite cùng WAL head
khớp trước và sau truy vấn. Ngưỡng 30/30 cặp dưới 85% lấy từ biên nhận đã lưu.
Job không gọi Codex hay chạy script repo mẫu.

```powershell
& $Python -B "$Suite\report.py" --recorded
```

[fixture/observed-2026-11-25.json](fixture/observed-2026-11-25.json) lưu 60
biên nhận đã xác minh của lượt live ngày 25/11/2026. A và C đều đúng 30/30,
không có sửa ngoài phạm vi. C dùng dưới 85% token của A ở cả 30 cặp, kể cả
khi cộng token đầu ra. Trung vị đầu vào C/A là 24,89%, mức cao nhất 78,74%.
Token cache, phần nạp trước và mọi lượt đọc thêm đã nằm trong usage Codex.
M03 và M09 có 0 lượt tìm hoặc đọc thêm ở C. Kết quả áp dụng cho bộ sửa mã có
kiểm soát và một lần lặp trên mỗi cặp của lượt này.

Bản quyền đi kèm các đoạn trích fixture nằm tại
[google/uuid](fixture/licenses/google-uuid.txt) và
[go-humanize](fixture/licenses/dustin-go-humanize.txt).
