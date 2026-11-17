# Rename preview và consumer

Rename chỉ tạo đề xuất. MCE4D không áp dụng edit, chạy test hay build dự án
được đọc. Quy trình này dùng sáu tool MCP hiện có; xác nhận kết quả rename
qua `mce4d_validate_pack`, không thêm tool apply.

Lớp composition cần `RenameOwner` có binary/provider được phê duyệt, đúng
toolchain và chứng minh đầy đủ các input typed. Owner native chỉ được nối khi
profile chọn `typed_when_available` và policy có đúng một đường dẫn tuyệt đối
được phê duyệt cho mỗi executable `go`, `gopls`. Profile syntax-only mặc định
không cấp quyền rename. Mỗi request vẫn kiểm tra quyền, fingerprint và toàn bộ
build inputs trước khi mở gopls; có owner không có nghĩa mọi request đều hợp lệ.

## Chuẩn bị native Linux

1. Dùng protected profile/engine account đã provision theo
   `tools/state_profile/README.md`. Administrator chuẩn bị bản Go và gopls đã
   review ở vị trí riêng do root quản lý. Cả cây Go, executable gopls và mọi
   ancestor phải thuộc root, không cho group/other ghi, không symlink/hardlink.
   Không thay quyền cây Go cá nhân hoặc tự tải executable trong runtime.
2. Build utility phát triển `./tools/go_toolchain` vào output được quản lý.
   Chạy `go-toolchain --root <approved-GOROOT>` để đọc fingerprint cả cây.
   Utility chỉ đọc; không chạy Go/gopls hay tự phê duyệt một profile. Đưa giá trị
   `toolchain_digest` vào GoProfile đã được quản trị phê duyệt. Policy cũng cần
   path/SHA-256 riêng cho `go`, `gopls` và Git. `provider_digest` của native index
   là fingerprint binary MCE4D đã cài, không phải fingerprint gopls.
3. Tạo/chỉnh profile bằng CLI admin với revision mong đợi, chọn profile đó rõ
   ràng rồi index lại. Index native vẫn công bố facts syntax ở trạng thái
   partial; việc kiểm chứng typed cho rename được thực hiện riêng bằng loader
   trên captured workspace. Không đổi analysis mode của một bản sao profile để
   giả chứng minh snapshot cũ.
4. Khởi động CLI/MCP bằng absolute path với **PATH chỉ có approved-GOROOT/bin**.
   Loader kiểm điều kiện này vì go/packages tìm Go ở tiến trình cha trước khi
   áp dụng env con. Owner không sửa PATH toàn process khi đang phục vụ request.
   CLI Linux đặt `umask 077` một lần ở entrypoint để cache của các child tool
   có đúng quyền owner-only. Host nhúng native API phải thiết lập mask này
   trước khi phục vụ đồng thời; owner chỉ đọc kiểm tra mask, không đổi theo request.
   Ví dụ hình thức trên WSL/Linux, sau khi thay bằng các giá trị được phê duyệt:

   ```sh
   PATH=/approved/go/bin /approved/bin/mce4d --worktree-id <id> --profile-id <hash> query rename-preview --input-json preview.json
   PATH=/approved/go/bin /approved/bin/mce4d mcp --worktree-id <id> --profile-id <hash>
   ```

Native owner hiện hỗ trợ một local module ở `.`, readonly module mode, cgo tắt,
test variants tắt, không external dependency roots/replace/require/tool modules.
Nó từ chối capture có test files, input bị ignore/deny/missing hoặc package/type
closure chưa chứng minh được. Giới hạn mỗi capture 128 files, 64 KiB/file, tổng
2 MiB và 32 package directories. Mọi local package được kiểm, không chỉ package
chứa selector; stdlib được ràng buộc vào cây toolchain đã xác minh. Những trường
hợp vượt phạm vi trả lỗi rõ và không fallback sang text replacement.

Scratch riêng của request nằm dưới worktree state, được kiểm containment và
giữ reservation quota hiện có. Reservation là phối hợp các writer của engine,
không phải filesystem quota của OS. Sau khi loader/LSP kết thúc chắc chắn,
engine dọn đúng scratch do request tạo và nhả reservation trước khi lưu spool.
Nếu hủy hoặc chưa chứng minh writer đã dừng thì giữ scratch và tính tiếp vào
quota; không đoán để xóa. Thư mục `scratch/rename` còn thiếu trong một slot đã
provision có thể được tạo có kiểm chứng; thư mục lạ không được chmod/adopt và
không có migration database tự động. Các `rename_result_*` và `rename_max_edits`
trong runtime config vẫn là giới hạn của kết quả, tách khỏi pack retention.
Startup và thao tác rename trong LSP dùng deadline cấu hình riêng; thời hạn
của request/caller vẫn có thể kết thúc sớm hơn. Continuation không khởi động LSP.

Fingerprint nội bộ `MCE4D-NATIVE-GO-TOOLCHAIN1` dùng SHA-256 trên prefix có byte
NUL cuối, tiếp theo mọi entry (kể cả root `.`) sort theo path UTF-8. Mỗi entry là
uint64 big-endian độ dài path, path bytes, byte `d`/`f`, uint64 permission bits,
uint64 file size và 32 byte SHA-256 nội dung. Directory dùng size 0/hash zero.
Nó không đổi các digest recipe của wire. Sau lần hash đầy đủ, owner kiểm lại
mọi inode/change stamp, kể cả membership của directory; proof dựa trên quyền
root quản lý, không bảo vệ trước administrator/kernel rollback độc hại.

## Thu thập và kiểm tra kết quả

1. Chọn worktree/profile rõ ràng, đọc status và locate để lấy đúng vị trí byte
   và hash từ snapshot. Đọc context, rule và gap liên quan trước khi sửa.
2. Gọi `mce4d_rename_preview` với initial request, ví dụ sau khi thay các giá trị
   bằng dữ liệu đã kiểm chứng:

   ```json
   {
     "selector": {
       "kind": "source_position",
       "path": "example.go",
       "content_hash": "<hash từ captured source>",
       "byte_offset": 42
     },
     "new_name": "NewName",
     "preserve_contracts": true
   }
   ```

3. Giữ `result_token`, `result_id`, `result_digest` và `total_edits` của trang
   đầu. `computation_complete=true` nghĩa engine đã tính xong toàn bộ kết quả;
   consumer vẫn phải đọc các trang còn lại. Mọi trang phải giữ cùng những giá
   trị này, có `applied=false`, `source_mutation_count=0` và
   `validation_required=true`.
4. Nếu có `pagination.next_cursor`, gọi tiếp cùng tool bằng **chỉ**
   `{"result_token":"...","cursor":"..."}` và tùy chọn `max_output_bytes`.
   Không gửi lại selector/new_name. Giữ mọi edit và conflict; từ chối cursor lặp,
   kết quả lẫn ID/digest/token, đứt trang hoặc số edit không khớp. CLI dùng
   `query rename-preview --input-json <file>` cho cả hai dạng request;
   exit 6 là trang partial cần đọc tiếp, không phải cho phép áp dụng.
5. Chỉ coi preview được thu thập đầy đủ khi không còn cursor/pending,
   `preview_complete=true`, tổng số edit khớp và không có edit overlap mâu thuẫn.
   Giữ lại cảnh báo implicit JSON field name, reflection và serialization để
   review; typed rename không chứng minh hành vi runtime.
6. Ngay trước khi host/editor sửa, gọi `mce4d_validate_pack` với
   `{"result_token":"..."}`; CLI tương ứng là `query validate`. Không gửi đồng
   thời `pack_token`. Phải có `edit_precondition_validated=true` và freshness
   `observed_current` ở scope `workspace`. Sau đó host kiểm lại đúng hash/range
   của **mọi** file nhận edit trước khi ghi. Engine không cấp quyền apply từ
   việc consumer mới đọc được trang đầu, hay từ riêng chữ ký của token.
7. Nếu source, membership, config, rules, policy, record hoặc incarnation đổi,
   token hết hạn/mất spool, quyền bị thu hồi hoặc timeout: dừng dùng kết quả cũ.
   Lấy snapshot/context/preview mới. Validation và continuation không kéo dài
   thời hạn, không chạy lại LSP và không rebase edit sang source mới.
8. Sau thay đổi đã được người dùng cho phép, host chọn build/test/lint từ cấu
   hình và tài liệu đúng repo, ghi kết quả thực tế rồi index lại khi có quyền.
   Khi chưa có bằng chứng về lệnh kiểm tra, giữ trạng thái unknown và khảo sát
   config; không suy ra một lệnh chung cho mọi dự án.

Result token và page cursor có mục đích khác pack token. Không dùng lẫn, không
ghi token/source vào log chẩn đoán và không retry vô hạn khi gặp lỗi. Thời hạn
cấu hình là giới hạn tối đa; retention có thể kết thúc sớm hơn nếu dữ liệu bị
thu hồi hoặc không còn được giữ.
