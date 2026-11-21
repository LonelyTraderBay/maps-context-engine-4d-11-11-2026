# Guard cho build/test cục bộ

`main.py` là wrapper Python stdlib cho **lệnh đã được review và được phép** trên
Linux. Nó không cấp quyền chạy target code, không phải sandbox/firewall hay OS
quota. Mỗi lần chạy dùng một output riêng; guard không xóa output cũ, source,
fixture, golden, records hoặc deliverable. Không dùng thư mục source làm root.

Caller chuẩn bị một root rỗng, owner-only, nằm ngoài cwd/source. Ancestors của
root/cwd phải là directory thật, do root/current user quản lý và không cho
group/other ghi. Mọi output/cache/temp được chuyển vào run riêng. Nếu dùng
dependency cache đã xác minh ngoài run, cấp `--module-cache`; đây là input đã
review, không được xóa hay tự tải bổ sung khi thiếu. Chỉ có các biến build an
toàn mặc định; `--env KEY=VALUE` dành cho các giá trị đã review như PATH,
GOOS/GOARCH và đường dẫn fixture, không đưa credential vào argv hoặc log.

Ví dụ hình thức, thay toàn bộ path/budget bằng dữ kiện đã kiểm:

```sh
python3 tools/run_check/main.py --root /owned/private-checks --cwd /approved/source \
  --peak-bytes 3221225472 --reserve-bytes 1073741824 \
  --block-bytes 1073741824 --warning-bytes 8589934592 --seconds 480 \
  --module-cache /approved/verified-modcache --backing-root /mnt/c \
  --env PATH=/approved/go/bin:/usr/bin:/bin -- /approved/go/bin/go test ./...
```

Các số ví dụ dựa trên run MCE4D trước đó khoảng0.75GiB peak, cap3GiB/reserve1GiB;
không phải ngưỡng chung cho mọi project. Cấp peak/reserve/block/warning/duration
rõ ràng. Một command tối đa12 giờ. Log mặc định16MiB, ngưỡng quan sát128 owned processes,
retention32 run/8GiB/30 ngày; có thể thu hẹp theo nhu cầu. Chạm retention sẽ chặn
workload mới và giữ data để kiểm tra, không tự xóa hoặc suy quyền từ tuổi file.

Free space được đo trên từng capacity domain đã khai báo, gộp alias cùng device
để tránh double-count. Guard dùng lock ngắn trên inode root cho admission và
reservation; các command ở output khác nhau vẫn chạy song song. Job đang chạy
chỉ tính phần `peak - observed_used` còn lại. Phạm vi coordination chỉ là các
guard dùng cùng root; writers ngoài đó không được biết hay đảm bảo tổng quota.
Không đồng thời sửa source của cùng batch khi đang kiểm chứng.

WSL bắt buộc thêm meter cho volume chứa VHDX, đã được xác minh **độc lập**.
Trong môi trường đã kiểm, registry distro Ubuntu xác định VHDX ở Windows C,
nên `/mnt/c` nhận thêm demand của output Linux. Nếu distro đã chuyển ổ hoặc
storage topology đổi, kiểm lại mapping trước khi dùng. `statvfs` không khám phá
mọi lớp thin provisioning/physical storage. Windows-native/reparse support chưa
được thực hiện; không suy PASS từ biên dịch chéo sản phẩm. Lock admission và
finalization có thời hạn5 giây; không giữ lock qua toàn command.

Guard poll process/cancel/free-space100ms, growth2s, và luôn kiểm growth lần cuối
sau khi owned writers dừng. Pipe log được clip ở hard byte cap. pidfd và
subreaper giữ ownership khi child đổi session; deadline/cancel/failure đều phải
đóng writer trước finalization. Exit code gốc và lỗi finalization là trường
riêng; command exit0 không đủ để báo PASS. CLI giữ exit code gốc khác0; signal
được biểu diễn bằng128+signal. Command exit0 nhưng guard/finalization lỗi trả1.
Kernel I/O hang, abrupt kill, external
writers và growth giữa các samples vẫn là giới hạn, không được gọi OS quota.

Thêm `--report-only` với cùng arguments để xem volume/budget và các run được giữ;
nó không tạo state, chạy workload hoặc xóa file. `admission_blocker` khác null
nghĩa command hiện chưa được chạy. Output/root bị thay, provenance lạ, measurement
không đọc được, pending metadata hoặc prior RUNNING không rõ outcome đều chặn.
Sau SIGKILL/crash, giữ pending data/reservation và chỉ report; không tự adopt,
khôi phục, hết hạn quyền hay xóa. Việc dọn sau này cần authority riêng theo S51.

Kiểm nhỏ dùng dữ liệu giả và root riêng được caller tạo:

```sh
MCE4D_CHECK_TEST_ROOT=/owned/private-small-fixtures python3 tools/run_check/test_main.py
```

Các fixture được giữ lại, không làm đầy disk thật. Suite kiểm meter lỗi/low-space,
overflow/boundary, report-only, alias/backing domains, concurrent reservations,
log/growth/deadline/cancel, finalization lỗi, link/replace, retention và crash.
Đây là script CLI riêng, không phải API để nhúng vào process đã có child khác.

`verify_go.py --go /approved/go/bin/go` là workload cố định dùng qua guard để
chạy mod verify, full suite, vet, Linux development/test binaries, chín bước
cross-compile Windows và spec check. Mọi case dùng cùng cache **của một run**;
script dừng ở lỗi đầu tiên và ghi từng exit code vào `go-verification.json`.
Đặt workload bên trong namespace không có route ngoài khi kiểm offline. Nó
không tự cài tool, nâng dependency, mở target hay cấp quyền từ record của guard.
