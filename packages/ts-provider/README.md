# Provider TypeScript/JavaScript

T043 chọn Compiler API từ `typescript@5.9.3` trong worker Node `24.22.3`. Quyết định và phạm vi P07 nằm ở ADR A047 trong S21. [Compiler API](https://github.com/microsoft/TypeScript/wiki/Using-the-Compiler-API) · [TypeScript 5.9.3](https://github.com/microsoft/TypeScript/releases/tag/v5.9.3)

`toolchain-pin.json` khóa tarball theo SRI SHA-512, SHA-256, manifest tệp giải nén và SHA-256 của Node Linux x64 đã kiểm. `file_manifest_sha256` băm bản đồ tên tệp tương đối → SHA-256. Bản đồ được serialize dưới dạng JSON UTF-8, khóa sắp xếp, không khoảng trắng giữa trường và một LF kết thúc.

## Profile và cách gọi

| Trường TSProfile của S43 | Quy tắc |
| --- | --- |
| `schema_version`, `language` | `1`, `typescript_javascript` |
| `tsconfig_paths` | 1–64 đường dẫn tương đối được chọn rõ và nằm trong snapshot. Chuẩn hóa và sort qua model hiện có. |
| `compiler_digest`, `node_digest` | SHA-256 tarball đã xác minh kèm manifest giải nén và SHA-256 executable Node đã kiểm theo platform từ pin. |
| `provider_digest` | SHA-256 của `worker.cjs` đã pin cùng package provider. |
| `allow_project_plugins`, `analysis_mode` | `false`, `typed_when_available` khi closure đã xác minh. `syntax` là chế độ cú pháp riêng. |

`ProfileDigest` của Go giữ recipe `profile-v1` của S43. Manifest snapshot phải bind byte của toàn bộ config, declaration và source thực sự dùng. Hai build unit có cấu hình khác nhau giữ provenance riêng. Thay compiler, Node, worker hoặc đường dẫn config đã chọn dẫn tới profile khác. Thay byte config dẫn tới snapshot khác.

`tsprovider.New` nhận đường dẫn vật liệu do installation owner quản lý và danh sách tsconfig đã duyệt. Hàm trả `LanguageProvider` cùng profile để caller bind vào capture. Adapter đối chiếu Node, archive compiler, manifest và từng file compiler trước khi mở worker. Caller giữ quyền job qua callback `Recheck`, stage batch vào generation BUILDING, seal terminal và publish bằng API index chung. Provider đã được kiểm qua SQLite WAL và context pack trên WSL2. Lệnh đăng ký profile và index CLI hiện phục vụ profile Go.

Worker dùng JSONC, `extends` cục bộ, `include/exclude`, project references và declaration đã capture. C055 kiểm alias/re-export, hai symbol cùng tên, lexical scopes, binding đến declaration của project reference và byte range UTF-8. Thiếu binding, JS `any`, dynamic import hoặc compiler diagnostics cho trạng thái partial và gap. Project plugins, config ngoài snapshot và type acquisition bị từ chối. Phạm vi fixture dương dùng `strict`, ESNext/Bundler, ES2022, JSX preserve và allowJs/checkJs.

## Kiểm thử discovery

`discovery.mjs` dùng `getParsedCommandLineOfConfigFile`, `createProgram` và `TypeChecker` với host chỉ đọc tập byte của fixture và thư viện chuẩn đã pin. `worker.cjs` dùng cùng Compiler API qua adapter S43 với framing, deadline và hủy tiến trình.

Runner của T043 dựng compiler từ npm cache theo lockfile hiện có, đối chiếu nội dung với pin và chạy ở WSL2 với network namespace riêng. Node giới hạn đọc vào chương trình thử và compiler, chặn ghi và child process. Dữ liệu thử có script `prepare` và tên plugin. Admission từ chối cấu hình plugin trực tiếp hoặc qua `extends`. Kiểm tra hash cây synthetic trước/sau có kết quả ghi trong E873.

Giữ nguyên `LICENSE.txt` và `ThirdPartyNoticeText.txt` của package. Thông báo đi kèm liệt kê DefinitelyTyped, dữ liệu Unicode và tài liệu/trích dẫn thông số Web API. Hồ sơ nghiệm thu ghi phân loại cho dùng nội bộ và điều kiện kiểm lại nếu tạo artifact phân phối.

## Frontend tĩnh (T056)

`tsprovider.NewFrontend` chọn artifact ghép đã embed và trả profile bind đúng digest. Dùng cùng profile và snapshot cho `Analyze` và `Scan`. `Analyze` trả fact chung S43. `Scan` trả route/client cùng SourceRef sau khi adapter xác minh phản hồi, capture và quyền job. Cấu hình backend phải chỉ rõ service, environment, namespace và version được duyệt. Ghép method và path từ call đã kiểm vào identifier trước khi dùng `MatchFrontendHandler`.

| Capability | Phạm vi đã kiểm |
| --- | --- |
| React/TSX route-to-component | Route JSX trực tiếp dưới Routes, import tên từ `react-router-dom`, đường dẫn cố định, component function hoặc arrow trong capture. |
| HTTP client | `fetch` bind vào lib chuẩn đã pin, path cùng origin và method literal hoặc GET mặc định. |
| Client-to-handler | Cùng path ở hai service chỉ chọn theo binding đầy đủ cùng SourceRef đúng scope. |
| Cú pháp khác | Computed path, nested route và fetch bị shadow giữ gap. SSR, RSC, framework router khác, Vite runtime inspection và deployment chưa xác minh ở T056. |
