# Bảo trì store trên Linux

Nguồn hợp đồng là S15 và S44 DB-02/03/05/06/07 trong đặc tả. Tài liệu
này ghi phạm vi triển khai; không thay migration, đường dẫn mặc định hoặc quyền
của profile đã cài. Các lệnh GC, backup, restore và recovery dùng profile
Linux đã được cài đặt theo hướng dẫn của repository.

GC R1 có `PlanGC` và `CollectGeneration` trong index adapter. Caller phải giữ
shared lifecycle và writer lock của đúng worktree, đồng thời cấp callback quyền
bảo trì đúng profile. Retention gồm active trước, rồi các generation terminal
mới hơn theo completed_at/created_at và ID; BUILDING thuộc recovery. Mỗi lần
collect xác nhận lại retention trong transaction và chỉ xóa một generation.
Giữ tối thiểu một generation; runtime caller dùng keep_generations đã cấu hình.
RuntimeConfig R1 yêu cầu keep_generations từ 2 đến 128. Native CLI dùng đúng
giá trị này và không nhận override retention từ argv:

```text
mce4d store gc --worktree-id <id> --profile-id <id> [--report-only]
mce4d store gc --worktree-id <id> --profile-id <id> --apply --confirm
```

Mặc định là report-only. Một lệnh xử lý tối đa 16 generation, có `more` nếu còn
candidate, và deadline 60 giây gồm startup/chờ khóa. Mỗi generation apply là
một transaction riêng; lỗi/timeout hoặc mất output cần đọc lại plan/trạng thái
trước khi retry. Không diễn giải lỗi cuối như rollback mọi generation trước đó.
Các pack/rename result ghim generation đã mất trả SNAPSHOT_EXPIRED, không đổi
sang source của generation active. Không thêm tool MCP hoặc endpoint HTTP.

Dry-run chỉ SELECT, không retire rồi rollback để giả lập. Kết quả đếm logical
rows/blob bytes của chính lần collect đó; các blob chung giữa nhiều candidate
phải tính lại sau mỗi lần apply, không cộng các dry-run độc lập thành tổng giả.

R1 thực hiện relations → module_members → search_documents/diagnostics/
capability_results → symbols → modules từ lá lên cha → files → packages →
generations → blobs không còn reference. Schema mới hơn hoặc khác baseline bị
từ chối trước ghi; không giả đã hỗ trợ domain/UI khi migration của chúng chưa
được triển khai. Catalog, records, source và artifact chứng cứ không thuộc GC.
Xóa rows không là secure erase hay bảo đảm ổ đĩa thu hồi ngay số byte đã báo.

Backup native dùng owner offline giữ exclusive lifecycle; không
nâng lease khi còn pool shared, không copy riêng main đang có WAL. Backup dùng
API của SQLite trên connection đã xác minh, có snapshot và giới hạn byte trước
khi sao chép. Cặp index/records cần chung cửa sổ quiescence. Source handles và
mọi pool phải đóng trước khi root controller kiểm file main/authority bằng FD
thông thường, để không làm mất POSIX locks của process SQLite.

```text
mce4d store backup --target catalog --output <new-private-backup-directory>
mce4d store backup --target worktree --worktree-id <id> --output <new-private-backup-directory>
mce4d store recover --target catalog [--report-only]
mce4d store recover --target worktree --worktree-id <id> [--report-only]
mce4d store restore --manifest <approved-backup.json-path> --confirm --wait-ms 5000
mce4d store recover --target catalog --apply --confirm --wait-ms 5000
mce4d store recover --target worktree --worktree-id <id> --apply --confirm --wait-ms 5000
```

Output backup phải là thư mục mới ngay dưới `backups/` của catalog hoặc đúng
worktree. Lệnh có deadline 60 giây; chờ khóa dùng maintenance_wait_ms hiện hành.
Manifest ghi installation ID, incarnation/schema, hash/byte length, binding/
policy/profile revisions và record epoch. Manifest được ghi cuối; file SQLite
tồn tại riêng không chứng minh backup đã hoàn tất. Chạy lại cùng output bị từ
chối và giữ mọi artifact. Không copy key, session, bearer hoặc runtime.json.
Giới hạn CLI là 4 GiB cho toàn bộ DB, tối đa 2 GiB mỗi DB; kiểm dung lượng chỉ
là preflight, không là quota OS hay bảo đảm nguồn điện/đĩa không hỏng.

Recovery mặc định là báo cáo metadata, deadline 10 giây. Lệnh không mở SQLite,
không lấy lifecycle/writer lease, không khởi động worker, không tạo/xóa file.
`actual_state_verified=false` luôn được giữ: phase trong journal chưa chứng minh
các database thực tế khớp với nó. Không nhận runtime-config override. Journal
dang dở hoặc sai cấu trúc chặn mở store; bound CLI/MCP kiểm metadata này trước
khi mở catalog pool. Không tự gỡ journal, tự chuyển VERIFIED hay tự retry apply.

Profile hiện tại có manifest và thư mục chứa trusted head do root quản lý.
Engine UID không được tự sửa chúng để restore. Thiết kế offline dùng root
administrator đã có, cùng worker executable đã pin chạy dưới engine UID; không
thêm daemon hay setuid helper. Worker giữ exclusive lifecycle/writer, xác minh
head hiện tại, tạo bản sao/staging bằng SQLite và đóng SQL handles trước khi báo
sẵn sàng. Controller kiểm bytes/IDs rồi thay file theo journal ngoài các DB;
worker giữ lease đến khi controller hoàn tất hoặc pipe đóng. Không chạy SQL
hay source của target bằng root.

Restore và recovery apply yêu cầu quản trị viên hệ điều hành của profile.
`--confirm` xác nhận đã chọn đúng backup, xem lại quyền và chấp nhận trạng
thái dữ liệu tại thời điểm backup. Đường dẫn manifest là `backup.json` trong
thư mục con trực tiếp của `backups/` thuộc store đã chọn. Giá trị chờ khóa
từ 1 đến 30.000 ms và deadline toàn lệnh là 120 giây.

Worker kiểm staging bằng schema đầy đủ, integrity, FK và worktree/record
epoch. Worker đổi incarnation trong transaction, checkpoint, đóng pool rồi
tạo WAL receipt ban đầu và head append-only cho đúng incarnation. Controller
đối chiếu hash và đổi chỗ từng thành phần bằng `RENAME_EXCHANGE`, thay manifest
sau cùng. SQLite database và journal cũ nằm trong `backups/.restore-<operation-id>/`
của store. Manifest và head cũ nằm trong `control[/worktrees/<id>]/restore-<operation-id>/`.
Các bản sao này vẫn thuộc phạm vi dữ liệu riêng và không được tự dọn.

Recovery xác minh cả file đang dùng lẫn bản giữ lại theo từng bước. Journal
`PREPARED`, `REPLACING` hoặc `VERIFYING` chặn mở store cho tới khi recovery
xác minh đủ các bước và ghi `VERIFIED`. Khi hash lệch, recovery dừng và giữ
nguyên trạng thái để kiểm tra. Catalog restore ảnh hưởng mọi binding dùng
catalog. Worktree restore chỉ giữ khóa ghi worktree được chọn. Incarnation mới
làm mất hiệu lực các token đã cấp cho store cũ. Hai key của profile không được nhập từ backup.

API driver đã đối chiếu đúng module modernc.org/sqlite v1.62.0 trong go.sum.
Tham khảo [SQLite Online Backup API](https://www.sqlite.org/backup.html).
Step/Finish không nhận context; kiểm cancel giữa step không phải bảo đảm ngắt
được kernel I/O đang chặn. Quy trình native Windows và quyền tương ứng nằm trong
[hướng dẫn Windows của state-profile](../tools/state_profile/README.md#windows-backup-and-recovery).
S21 E829 ghi kết quả khôi phục sau khi dừng tiến trình trên NTFS. Lỗi do mất
nguồn vật lý vẫn chưa được kiểm thử.
