---
document_id: MCE4D-SPEC
file_name: Maps-Context-Engine-4D_SPEC.md
spec_version: 1.6.9
spec_date: 2026-11-11
language: vi
primary_reader: AI_CODING_AGENT
project_label: Maps-Context-Engine-4D
project_id: maps-context-engine-4d
binary_name: mce4d
project_name_status: INTERNAL_WORKING_NAME_NOT_TRADEMARK_CLEARED
spec_status: AUDIT_A01_A08_CLOSED_IN_SPEC_WITH_MANDATORY_PREFLIGHT_SEALS
implementation_status: IN_PROGRESS
product_verification_status: NOT_RUN
document_structural_validation: PARTIAL_1_6_2_SELECTED_VALIDATOR_PASS
source_of_truth: THIS_FILE
policy_reference_directory: docs
policy_adoption: ACCEPTED_SCOPED_S50
policy_installation_in_project: VERIFIED_MANUAL_HASH
policy_runtime_enforcement: NOT_IMPLEMENTED
approved_audit_findings: 8
approved_audit_findings_integrated: 8
git_batch_policy: S53
git_batch_size: 10
git_checkpoint_workflow_status: SPECIFIED_NOT_IMPLEMENTED
git_project_checkpoint_status: NOT_VERIFIED
previous_spec_sha256: 935538be6adcbda1d23e7c64b2a1963e5a08cee2b7895b324f19cc0788f5efbf
initial_task: T001
current_gate: G0
---

# Maps-Context-Engine-4D — đặc tả và sổ triển khai duy nhất

## S00 — BOOT: hợp đồng thực thi dành cho AI

MUST đọc S00 và resume S22 trước; lấy hàng task hiện tại trong S19, rồi chỉ đọc mục/requirement/case mà task tham chiếu. T001/T002 cần S01–S03; không load toàn bộ tài liệu vào mỗi phiên. S23 chỉ là nguồn kiểm chứng. S26–S33 là phần mở rộng lõi v1.1.0; S34–S40 là Code↔UI Explorer v1.2.0 trong cùng đặc tả. T105–T108 đóng model/schema/DDL/flow proof trước feature; T026 kiểm conformance/wiring; T081 đóng UI adapter seal trước T082. S41–S49 là bản vẽ thi công v1.3.0; authority cụ thể S41, không duplicate engine. S50 quy định vai trò/nguồn/version/mode của ba file nguyên bản trong docs; S51 là command/storage policy cho hoạt động phát triển. V1.5.0 sửa trực tiếp S43/S44/S45 để khép audit A01–A08; S52 là bảng đối chiếu audit và giới hạn, không authority hay tracker thứ hai. S53 chốt một commit đầy đủ cho mỗi 10 task DONE theo S47, tag/restore/backup và ngoại lệ phần dư; không đổi quyền runtime của engine. Các section/task ID ổn định để agent tìm đúng đoạn bằng heading, không phải nhớ số dòng.

MUST xây một sản phẩm độc lập từ đặc tả này; không fork, dịch, chuyển đổi, tái tạo từng module hoặc dùng source/test/prompt/schema nội bộ của sản phẩm khác làm đầu vào triển khai. MUST NOT truy xuất GitNexus/Graphify để lấy cách triển khai khi thực hiện tài liệu này. Tên hai sản phẩm chỉ dùng để xác định ranh giới không kế thừa; không phải dependency.

MUST hiểu đây là đặc tả cần triển khai, không phải mô tả tính năng đã tồn tại. Mọi task ban đầu là TODO hoặc DEFERRED; không có build/test/benchmark sản phẩm đã chạy. Các giá trị hiệu năng là mục tiêu kiểm thử, không phải số đo.

MUST khảo sát thư mục thực tế trước khi tạo/sửa file. Nếu repository rỗng, bootstrap theo T001. Nếu đã có code, đối chiếu từng task với code và bằng chứng; không ghi đè, xóa hoặc tái tạo repository. Không giả định repository cũ nào còn tồn tại; không phụ thuộc `memory-for-ai`.

MUST chỉ sửa phạm vi task đã nhận. Trình tự: đọc invariant liên quan → xác định hiện trạng → test hồi quy/fixture → thay đổi tối thiểu → build/test → kiểm tra diff → cập nhật tracker và bằng chứng trong file này. Trong assignment implement/integrate có quyền, tích lũy đủ 10 task DONE thì đóng MỘT batch commit đầy đủ theo S53 trước task kế; không commit mỗi task/file/phiên. Mỗi task vẫn được test và cập nhật tracker ngay, không đợi đến lúc commit.

MUST dùng nguồn gốc code, contract và test làm bằng chứng. Không đoán API thư viện, tên module Go, version tương thích hoặc cấu hình máy người dùng. Discovery gate được phép kiểm tra tài liệu chính thức và môi trường; lựa chọn thu được phải được pin, ghi lại và kiểm thử.

MUST giữ tài liệu thiết kế/quyết định/tracker trong đúng file này. Không tách PRD, ARCHITECTURE, ROADMAP, TODO, AI_RULES riêng dự án hay CHANGELOG thành tài liệu cạnh tranh. Ba file quy tắc nguồn nguyên bản trong docs theo S50 được phép và phải giữ nguyên; chúng không giữ dữ kiện/kiến trúc/tracker riêng của MCE4D. Code, migration, test, lockfile, cấu hình CI, NOTICE/LICENSE bên thứ ba và log kiểm thử máy sinh vẫn được phép tạo khi cần. Chỉ tạo `AGENTS.md` tối đa 15 dòng trỏ về S00 nếu host cần và không có file sẵn; không sao chép toàn đặc tả vào đó; không ghi đè hướng dẫn hiện hữu.

MUST phân biệt: “AI đang xây MCE4D” được sửa code của MCE4D theo task đã được giao; “MCE4D đang phân tích một repository” mặc định không được sửa source/config/AGENTS của repository đó. Xác định task_mode theo POL-03 trước ghi: file bootstrap nằm trong docs không tự bật chế độ docs-only hay tự cho phép triển khai. Nhiệm vụ chỉ cập nhật bản vẽ không tự chuyển thành coding.

Thứ tự ưu tiên: chỉ dẫn hệ thống và quyền của người dùng → yêu cầu mới được xác nhận → invariant bảo mật/tính đúng → ADR ACCEPTED_USER/BASELINE_SPEC trong file này → yêu cầu/tracker → source/test hiện có. Phát hiện mâu thuẫn: ghi BLOCKED cho đúng phạm vi; không tự bỏ gate hoặc hạ tiêu chuẩn để qua test. Các default thiết kế trong tài liệu là quyết định của bản đặc tả, không gán chúng thành lựa chọn đã được người dùng xác nhận riêng.

Từ khóa: MUST/MUST NOT = bắt buộc; SHOULD = mặc định phải làm, ngoại lệ phải có ADR; MAY = tùy chọn không thuộc cam kết release. Không dùng lời hứa “hiểu toàn bộ code”, “không bao giờ bỏ sót”, “không có rủi ro bản quyền” hoặc “tiết kiệm X%” khi chưa có bằng chứng tương ứng.

### Bản đồ đọc theo tác vụ

| Loại việc | Mục cần đọc |
|---|---|
| Bootstrap, license, dependency | S00,S22,S41–S44,S46–S50; thêm S01–S03 và S51 khi command có output |
| Repo/worktree/path/security | S04, S06–S08, S14–S15, S17 |
| Schema/index/freshness | S06–S09, S14–S17 |
| Phân tích symbol/ngôn ngữ | S04, S08–S10, S12, S17 |
| Context/token/MCP | S06, S10–S13, S17–S18 |
| Release/benchmark | S14–S22, S27, S32 |
| Overview/greenfield/self-index | S26, S27, S32; T049,T051,T054 |
| Scale/freshness modes | S07–S08, S27; T050,T052,T053 |
| Polyglot/contracts/domain | S24, S28–S30, S32; các task R2 |
| Runtime/test/editor/advanced | S31–S32; các task R3 |
| Code↔UI Explorer / click hai chiều | S34–S35, S37–S40; T081–T094 |
| Frontend→API→backend→DB / flow | S28, S36–S40; T095–T104 |
| Tiếp nhận ba file docs / mode / policy conflict | S50; source headings theo POL-05; S41 |
| Build/test/generator/benchmark có output | S46 CODE-09, S51; POL-SRC-02; S48 QA-04/QA-05 |
| Audit v1.4.0 / runtime config / restore / continuation | S52 → section owner S43 CT-04–CT-08, S44 DB-05–DB-07, S45 AL-08–AL-10 |
| Gom commit, tag/checkpoint, push và lấy lại source | S53; S19/S21/S22; S47 thứ tự; S51 cho command/output |

Mỗi phiên phải kết thúc bằng cập nhật hàng task, bằng chứng đã chạy, rủi ro còn lại và task tiếp theo; không viết lại toàn tài liệu. Không dùng việc đã đọc trong phiên trước làm bằng chứng rằng phiên hiện tại vẫn giữ đủ context.

MUST theo S47 cho thứ tự và phiếu thi công. T001/T002/T003/T105–T108 chỉ foundation/admin/contract proofs; chưa seal G0 không feature implementation. S43/schema và S44/DDL v1.5.0 đã đóng baseline cho các sửa đổi audit đã chấp thuận; API upstream/version chỉ được xác minh qua preflight, không đoán. Thiếu dữ kiện chặn đúng scope theo S41, không cấm sửa lỗi thật để giữ lời hứa zero rework.

## S01 — Mục tiêu, phạm vi và tiêu chí sản phẩm

### Mục tiêu được giữ cố định

Xây công cụ local cho một người dùng có nhiều repository. Công cụ lưu bản đồ cấu trúc ngoài model, cung cấp ngữ cảnh có bằng chứng cho từng thay đổi code và thêm Code↔UI Explorer: graph bên trái, frontend preview hoặc backend flow bên phải, chọn đồng bộ hai chiều. Tối ưu tổng token để hoàn thành một nhiệm vụ đúng, không tối ưu riêng kích thước phản hồi đầu tiên. Explorer dùng cùng facts với AI; không phải trình tạo giao diện từ graph.

MUST giải quyết đồng thời: không khảo sát lại toàn dự án bằng LLM; phân biệt đúng symbol trùng tên; phát hiện các quan hệ đã biết có liên quan; không trộn worktree/phiên bản; cập nhật thay đổi chưa commit; trả source hiện tại cần thiết; công khai phần chưa biết; giữ các dự án độc lập không bị khóa chéo.

Thứ tự đánh đổi: correctness/security → freshness/provenance → completeness trong phạm vi hỗ trợ → reliability → token/latency → tính năng bổ sung. Một thay đổi có ảnh hưởng rộng được phép cần nhiều context hơn.

### Release boundary

| Release | Bắt buộc | Không được tuyên bố |
|---|---|---|
| R1: local Go context engine | Nhiều Git repo/worktree; file map cho các text file cho phép; Go syntax + typed index theo profile; source evidence; hierarchy/overview; spec-only planning; context packs; MCP/CLI; rename preview Go; incremental invalidation; snapshot analysis mode; recovery; scale/self-index fixtures | Không phải hỗ trợ ngữ nghĩa mọi ngôn ngữ; không phải công cụ tự sửa source; không phải phân tích runtime đầy đủ |
| R2: web/trading/commerce/AI | TS/JS + Python providers, frontend/API/SQL/event contracts, nhóm repo, approved state/invariants, semantic diff, AI metadata lineage | Roadmap cần thiết để đáp ứng đa stack; không bắt đầu trước G5 và mở scope; không tính vào R1 |
| R3: evidence và scale nâng cao | Runtime/test evidence import, editor buffers, optional semantic retrieval, storage/deployment/language extensions | Gated theo nhu cầu sau R2; không cài mọi addon mặc định |
| U0–U2: Code↔UI Explorer | GUI local 2D: graph trái, source/preview/flow phải; Inspect hai chiều, React+Vite bridge, frontend→Go/API/DB theo capability | Scope người dùng yêu cầu; sau core G5 và các prerequisite R2 riêng, không chờ toàn G6/G7; xem S39 |
| U3: observed run overlay | Runtime trace overlay + tương tác có phép trong môi trường test; tái dùng R3 runtime importer | Tùy chọn có gate, không cần để chọn UI↔source hoặc xem flow tĩnh |
| FUTURE | 3D/4D graphics, distributed indexing, browser extension tổng quát | Ngoài baseline Explorer; cần ADR và task riêng |

R1 không cần: web frontend, account/auth cloud, SaaS, billing, microservices, queue/cache server, graph database server, Docker/Kubernetes bắt buộc, LLM API, embeddings/vector DB, auto-agent, tự chạy test của repo đang index, tự apply rename, marketplace plugin, hỗ trợ PDF/audio/video.

Frontend của core R1 = không có; CLI JSON/MCP STDIO vẫn dùng độc lập. Product track U0–U2 bổ sung frontend Explorer và loopback HTTP adapter opt-in theo S34–S40; không sửa definition of done core R1 để che tiến độ UI. Không nhúng business analysis vào frontend. React/Vite của Explorer không bắt target repo phải đổi framework.

Tác vụ hỗ trợ: `explore`, `plan`, `rename`, `type_change`, `behavior_change`, `contract_change`, `debug`. “Sửa biến” không được tự diễn giải thành đổi tên hay đổi hành vi; khi chỉ tên không đủ định danh, công cụ trả ứng viên và `AMBIGUOUS_SYMBOL`, không chọn tùy ý.

### Chọn đối tượng thử đầu tiên và giới hạn kỳ vọng

R1 Go-first theo A006/A015 và thứ tự S47 vẫn là baseline; việc chưa chọn repo dùng thử không đổi scope, ngăn T001 hay tự mở R2. T001 ghi vào S21 dưới dạng thông tin discovery, không tạo tracker thứ hai: mục tiêu sử dụng dự kiến; ngôn ngữ/framework và loại tác vụ ưu tiên nếu đã biết; repo/worktree hoặc fixture đại diện và nguồn quyền; OS/filesystem, host dự kiến; mức hỗ trợ kỳ vọng đối chiếu S04/S20/S39; dữ liệu còn thiếu. `CHƯA XÁC ĐỊNH` là giá trị hợp lệ, không tự chọn repo riêng tư từ máy hoặc suy quyền từ tên repo. Không đưa source, đường dẫn riêng tư hoặc secret vào log công khai.

Nếu mục tiêu thực tế ưu tiên TS/React, Python, Flutter hoặc stack khác, ghi khác biệt so với capability R1 và tác động tới mốc dùng được; R1 không được gọi là semantic support cho stack chưa có provider. Không tự thay Go-first, gate hoặc mở provider. Nếu chủ sở hữu muốn đổi ưu tiên, lập discrepancy/ADR theo BP-04; trong khi chờ, tiếp tục các task độc lập đã được giao và còn phù hợp. Khi chưa có repo thật được phép, dùng fixture do dự án tự tạo và giữ nhãn fixture-validated theo S20.

### Ranh giới dữ liệu

R1 phân tích bytes đã lưu trên đĩa, kể cả thay đổi chưa commit và file untracked được phép. Buffer chưa lưu trong editor không được biết; MUST trả `buffer_scope="disk_only"`. Không giả vờ đã thấy buffer. Nested repo/submodule, external module và ngôn ngữ chưa hỗ trợ là boundary được liệt kê, không phải vùng “không có dependency”.

Giảm token không đồng nghĩa giảm số lần chương trình local đọc file. Hash/parse local được phép đọc nhiều file; phần được gửi vào model mới là đối tượng giảm. Host AI vẫn có thể truyền tool output tới nhà cung cấp model; MCE4D local không bảo đảm toàn bộ workflow nằm offline.

### Danh mục yêu cầu có truy vết

| ID | Mức | Điều kiện bắt buộc | Đặc tả | Task |
|---|---|---|---|---|
| R001 | P0 | Triển khai độc lập; lưu provenance và license dependency. | S02 | T001,T002 |
| R002 | P0 | Định danh riêng repo/worktree; không dùng tên folder hoặc remote URL làm ID. | S06 | T004 |
| R003 | P0 | Không có khóa build/index toàn máy do dự án khác sở hữu. | S09,S15 | T010,T011 |
| R004 | P0 | Mỗi response ràng buộc snapshot/profile/policy/version và source view. | S06,S11 | T007,T026 |
| R005 | P0 | Source bytes và vị trí có hash; không trộn source mới với edge cũ. | S07,S08,S10 | T006,T015 |
| R006 | P0 | Freshness phân biệt đã kiểm tra/đã đổi/chưa biết; HEAD không đủ. | S08 | T018,T019 |
| R007 | P0 | Index mới chỉ hiện ra bằng publish transaction; crash giữ bản đã publish. | S07,S08 | T008,T009 |
| R008 | P0 | Định danh symbol theo khai báo/scope; tên chỉ là điều kiện tìm ứng viên. | S06,S09 | T013,T014 |
| R009 | P0 | Không loại local-variable references cần cho truy vấn symbol Go. | S09 | T014 |
| R010 | P0 | Quan hệ có hướng, loại và callsite; giữ đa cạnh có ý nghĩa. | S07,S09 | T007,T015 |
| R011 | P0 | Tách typed/syntax/candidate/text evidence; không biến heuristic thành chắc chắn. | S09,S10 | T015,T016 |
| R012 | P0 | Không sửa target repo; rename chỉ preview từ LSP. | S12,S14 | T029,T030 |
| R013 | P0 | Cấm path escape, symlink escape và truy cập repo chưa đăng ký/cho phép. | S06,S14 | T005,T032 |
| R014 | P0 | Không gửi secrets/log source; không tự network/install/execute target code. | S14 | T005,T020,T032 |
| R015 | P0 | Kết quả rỗng không chứng minh không có phụ thuộc. | S10,S11 | T016,T024 |
| R016 | P0 | Không cắt JSON hoặc âm thầm bỏ cảnh báo/mandatory evidence vì budget. | S10,S11 | T024,T025 |
| R017 | P0 | Xác minh source/preconditions ngay trước khi consumer sửa. | S08,S12 | T019,T030 |
| R018 | P0 | API có schema version, error codes, timeout/cancel và pagination ổn định. | S11,S13 | T026,T027,T028 |
| R019 | P1 | File map/text search cho file được cho phép; khai báo capability từng ngôn ngữ. | S04,S09 | T012,T017 |
| R020 | P1 | Incremental dựa nội dung; thêm/xóa/đổi tên/config/profile được invalidation đúng. | S08 | T018,T021 |
| R021 | P1 | Có fallback semantic full rebuild khi dependency invalidation chưa được chứng minh. | S08 | T021 |
| R022 | P1 | Context pack theo loại task, provenance, safety gaps, expansion. | S10 | T022,T023,T024 |
| R023 | P1 | Dedup trong response; không ngầm tin model đã giữ nội dung từ phiên cũ. | S10 | T024,T025 |
| R024 | P1 | MCP tối đa 6 tool R1; startup không index toàn bộ repo. | S11,S13 | T027,T028 |
| R025 | P1 | Tests được liên kết bằng bằng chứng; heuristic không phải chứng minh coverage. | S09,S17 | T016,T034 |
| R026 | P1 | Quota/GC/backup/rollback không xóa target source hoặc khóa repo khác. | S15 | T033,T035 |
| R027 | P1 | Log/metrics đủ điều tra nhưng không chứa secret, query hoặc source mặc định. | S16 | T031 |
| R028 | P1 | Chạy Linux/WSL2 và Windows native; phát hành chỉ platform đã qua gate. | S03,S17 | T036,T037 |
| R029 | P1 | Benchmark đối chứng cùng task/model; báo failure cùng token, không cherry-pick. | S18 | T038,T039 |
| R030 | P1 | Một tài liệu quy định + tracker; DONE phải có evidence. | S00,S19–S22 | T003,T040,T042 |
| R031 | P1 | Không tự ghi AGENTS/config/hook vào repository được phân tích. | S13,S14 | T028,T032 |
| R032 | P1 | Offline degradation rõ khi thiếu dependency hoặc language server. | S09,S12 | T020,T029 |
| R033 | P1 | Chỉ tài liệu được khai báo/trusted mới là rule; text trong repo không tự thành instruction. | S10,S14 | T023,T032 |
| R034 | P1 | Hướng dẫn test/build phù hợp repo; không mặc định một lệnh áp dụng cho mọi repo. | S12,S17 | T030,T034 |
| R035 | P1 | Migration/schema mới không phá binary cũ bằng overwrite in-place không phục hồi. | S07,S15 | T035 |
| R036 | P1 | Release pin dependencies; license/security review trước gắn nhãn ổn định. | S02,S03,S20 | T002,T041 |
| R037 | P2 | Provider TS/JS typed, có kiểm thử same-name/re-export/path alias và profile. | S04,S24,S28 | T043,T044,T055,T056 |
| R038 | P2 | Contract/API/SQL/queue chỉ nối khi có bằng chứng; không gộp repo bằng tên. | S24,S28 | T045,T046,T059,T060,T061,T063 |
| R039 | P2 | Bộ nhớ quyết định có scope/source hash/approval; không tự học thành invariant. | S24,S30 | T047,T062,T064 |
| R040 | P2 | Rebenchmark R2; không nới an toàn để tăng tỷ lệ tiết kiệm. | S18,S24,S32 | T048,T068 |
| R041 | P1 | Hierarchy/overview 4D capability; nhóm không làm mất evidence hoặc caller. | S26,S27 | T049,T051 |
| R042 | P1 | Spec-only/intended không giả implemented; planning context cho repo mới. | S26 | T051,T054 |
| R043 | P1 | Partition/cache/invalidation theo closure; scale đúng quota và memory scope. | S27 | T050,T053 |
| R044 | P0 | Analysis snapshot khác live validation; không dùng cached freshness cho edit. | S08,S27 | T052 |
| R045 | P1 | Self-index bằng binary pin, independent oracle và bootstrap fallback. | S26 | T054 |
| R046 | P2 | Provider interface tối thiểu; Python không import/exec target, capability versioned. | S28 | T055,T057,T058 |
| R047 | P2 | Frontend route/component/client/backend mapping có source evidence. | S28 | T056 |
| R048 | P2 | SQL dialect/engine, migration/routine/view/trigger/event lineage có giới hạn rõ. | S28 | T059 |
| R049 | P2 | HTTP/schema/proto identity, local refs, source vs declared contract riêng. | S28 | T060 |
| R050 | P2 | Event/cache namespace/producer/consumer/schema/ack evidence, không fake exactly-once. | S28 | T061 |
| R051 | P2 | Approved business state/invariant/flow và failure obligations; không bịa policy. | S29,S30 | T062,T065 |
| R052 | P2 | Federation theo declared groups và vector snapshot/policy từng member. | S28 | T063 |
| R053 | P2 | Temporal semantic diff và source-bound decision invalidation. | S30 | T064 |
| R054 | P2 | AI metadata lineage/prompt/tokenizer/eval/data/schema; không load model/dataset payload. | S29 | T066 |
| R055 | P2 | Architecture intended-vs-observed với witness/gaps, không refactor tự động. | S30 | T067 |
| R056 | P2 | Đa domain/polyglot benchmark không lấy engine output làm oracle. | S18,S32 | T068 |
| R057 | P3 | Runtime observation có revision/environment/sampling, không suy exhaustive call graph. | S31 | T069,T070 |
| R058 | P3 | Test evidence theo revision, selection không quyền bỏ regression hoặc chạy target. | S31 | T071,T072 |
| R059 | P3 | Editor buffer session version/hash riêng; không trộn disk hoặc auto-save. | S31 | T073 |
| R060 | P3 | Semantic retrieval chỉ candidate, privacy/version/license và exact fallback. | S31 | T074 |
| R061 | P3 | Storage fact sets/XL scale chỉ sau profiling, atomic manifest và safe GC. | S27 | T075,T077 |
| R062 | P3 | Static deployment/config mapping và read-only machine reports, không chạy IaC. | S31 | T076,T078 |
| R063 | P3 | Provider bổ sung theo nhu cầu có capability fixtures trước nhãn hỗ trợ. | S28 | T079 |
| R064 | P3 | Advanced capability release review/benchmark; không giả DONE phần chưa mở. | S32 | T080 |
| R065 | P1 | [U0] Hai pane co giãn: graph cố định trái; source/preview/flow theo selection ở phải. | S34,S38 | T081,T083,T084 |
| R066 | P0 | [U1] Inspect click button hoặc chọn block/component ở preview phải resolve đúng graph trái; không dispatch action nghiệp vụ. | S34,S35,S37 | T088,T089,T094 |
| R067 | P1 | [U1] Chọn node trái highlight instance phải/ancestor; nhiều instance hoặc chưa render phải trả trạng thái, không click/navigate ngầm. | S34,S35 | T090,T091,T094 |
| R068 | P0 | [U1] Phân biệt definition/render-site/event-site/DOM instance, many-to-many, build/profile/generation/session identities. | S35,S38 | T085,T086,T089,T091 |
| R069 | P0 | [U1/U2] Evidence source-static/build-map/runtime/manual/proposal riêng; mapping không có bằng chứng không được vẽ thành exact edge. | S35,S36 | T085,T092,T095,T096 |
| R070 | P0 | [U1/U3] Inspect khác Interact; chặn input gây action, không hứa ngăn mount effects/network; môi trường test và quyền tách rõ. | S34,S37 | T087,T088,T094,T102 |
| R071 | P0 | [U1] Adapter/SDK chỉ development/test opt-in, không auto sửa source/config/khởi động repo; production build không chứa mapping SDK/metadata. | S35,S37 | T085,T086,T094 |
| R072 | P0 | [U1] Bridge verify exact origin/source/session/nonce/schema/sequence; không expose engine auth hoặc quyền đọc file cho iframe. | S35,S37,S38 | T087,T088,T094 |
| R073 | P0 | [U1] HMR/navigation/remount/build mismatch/policy revoke invalidate binding; không map preview cũ lên HEAD mới. | S35,S38 | T091,T094 |
| R074 | P1 | [U2] Chọn backend thì panel phải thành flow API→middleware/handler/service→data ops có evidence; static path không giả execution. | S36 | T095,T097,T100 |
| R075 | P0 | [U2] Liên kết UI/FE client với BE operation bằng namespace/method/path/profile/contract và member snapshot vector. | S28,S36 | T096,T097,T100 |
| R076 | P0 | [U2/U3] READ/WRITE/query attempt/success/commit/rollback tách riêng; không khẳng định đã lưu DB từ static call hoặc span thành công. | S36 | T095,T098,T101,T103 |
| R077 | P0 | [U0/U1] Mở source tại anchor/hash đã kiểm tra; không HTTP shell/editor command tùy ý, không để iframe yêu cầu source. | S34,S37,S38 | T083,T090,T093 |
| R078 | P1 | [U2] Lazy subgraph, bounded visible nodes/edges, progressive hierarchy; preview updates không reload toàn graph. | S34,S38,S39 | T083,T099,T100 |
| R079 | P1 | [U0/U2] Splitter/selection keyboard, semantic list thay canvas-only, focus/reduced motion; labels không chỉ dựa màu. | S34,S39 | T083,T099 |
| R080 | P0 | [U0–U2] Index và preview từ repo thực; fixtures để test có nhãn; không hardcode graph/demo UI thay target. | S34,S39 | T081,T084,T094,T100 |
| R081 | P0 | [U0] HTTP loopback opt-in, session auth/host/origin/CSRF; không CORS rộng, proxy URL hay tự chạy target app. | S37,S38 | T082,T084 |
| R082 | P0 | [U1] Session/DOM instances ephemeral bounded; manifests immutable scoped; cache GC/record ownership không mất approved mappings. | S35,S38 | T086,T087,T092 |
| R083 | P1 | [U1] Selection → typed source/entity locator → context pack cùng engine; không gửi screenshot/full graph hoặc gọi model tự động. | S38,S40 | T093,T094 |
| R084 | P1 | [U1/U2] Verified matrix React+Vite và Go net/http+database/sql subset trước; framework/DOM loại khác có unsupported gaps. | S35,S36,S39 | T081,T085,T095,T100 |
| R085 | P0 | [U3] Runtime path cần trace/span/request/build lineage; không last-click/timestamp correlation làm proof; CORS propagation opt-in. | S36,S37 | T101,T102,T103 |
| R086 | P0 | [U0–U3] E2E roundtrip UI↔graph↔flow/source, no Inspect actions, no production leakage, HMR/ambiguity/security và measured latency. | S39 | T084,T094,T100,T104 |
| R087 | P0 | [U1] Manual correction có approval/version/provenance, stale/revoked lifecycle; không đổi static facts hoặc giả runtime truth. | S35,S38 | T092 |
| R088 | P1 | [U0–U3] Track Explorer theo capability prerequisites, không block bởi toàn Python/ML/R3; một spec/tracker/engine, progress tách. | S19,S20,S39,S40 | T081,T084,T094,T100,T104 |
| R089 | P0 | Khóa thiết kế trước feature code; không tự đổi stack/contract/permission; discrepancy có authority. | S41,S46,S49 | T105,T108 |
| R090 | P0 | Một canonical model/schema/operation registry; scope vector và source bytes không bị định nghĩa hai nơi. | S43 | T105,T107,T026 |
| R091 | P0 | Physical migrations có FK/state/FTS guards và proof trước store use cases. | S44 | T106,T007 |
| R092 | P0 | Mọi luồng có owner, input/output, write boundary, retry/cancel và failure paths. | S42,S45 | T107,T108 |
| R093 | P0 | Thứ tự task topo/gate, publish sau lock, tổng nghiệm thu sau GC; contract trước adapter. | S47 | T003,T108,T040 |
| R094 | P0 | Approved records durable ngoài index GC, version/head CAS, mutation-id replay, effective stale state. | S42,S44 | T106,T047,T092 |
| R095 | P0 | Build producer map không phụ thuộc generation; binding immutable độc lập, server không giả DOM instances. | S43,S45 | T081,T085,T087,T089 |
| R096 | P0 | Pairing bind/BOUND_ACK và arming MODE_ACK mới cho Interact; fail closed trên stale epoch. | S42,S45 | T081,T087,T088,T094 |
| R097 | P0 | Scope/cache/pack bao gồm policy/binding/record/group revisions và optimistic postcheck. | S42,S43 | T105,T019,T025,T047 |
| R098 | P0 | MCP delivery compatibility và actual duplicate bytes budget; payload luôn parseable. | S43 | T107,T024,T027 |
| R099 | P0 | Provider không lách capture policy; types từ Load khác nhau không trộn identity; AST src khớp bytes. | S45 | T107,T014,T020 |
| R100 | P0 | Coding boundaries/error/cancel/config/testing từ bootstrap; mocks không thành runtime. | S46 | T105,T108,T040 |
| R101 | P0 | SQL GC/purge không giả secure erase; record retention/source privacy kiểm độc lập. | S44 | T035,T092 |
| R102 | P0 | Bản vẽ có document-level evidence riêng product verification; không fake gate/test/rework-free. | S48 | T003,T108,T040 |
| R103 | P1 | Phiếu task có read sections/owned paths/flow/acceptance/conditions, dùng tracker duy nhất. | S47 | T003,T108 |
| R104 | P0 | Contract/schema/DDL thay đổi theo revision; incomplete addon chặn đúng scope, không sửa bừa core. | S41,S43,S49 | T108,T081,T045 |
| R105 | P0 | Một spec chính, ba nguồn docs nguyên bản có version/hash/activation; không tự nâng/copy thành policy dự án thứ hai. | S00,S50 | T001,T003,T040 |
| R106 | P0 | task_mode theo assignment; docs-only onboarding khác foundation implement, không kích hoạt prompt chỉ vì file tồn tại. | S41,S46,S50 | T001,T003,T108 |
| R107 | P1 | Complexity Gate/Change Budget bảo vệ baseline và scope hành vi; không tùy tiện thêm hoặc bỏ bảo vệ bắt buộc. | S46,S47 | T105,T108,T040 |
| R108 | P0 | Command contract có cwd/argv/version/writes/network/quyền/bằng chứng; không chạy placeholder hoặc tin tên dry-run. | S46,S51 | T002,T107,T081 |
| R109 | P0 | Preflight/growth budget trên mọi volume, gộp shared volume/reservation remaining; không global build lock hoặc đoán đủ chỗ. | S51 | T002,T107,T033 |
| R110 | P0 | Owned-output/finalization giữ lỗi gốc; dry-run thực sự chỉ đọc; cleanup cần containment/approval, không source/durable records. | S15,S51 | T005,T107,T033,T035 |
| R111 | P1 | V0–V4 là loại bằng chứng, mutation có chọn lọc và kiểm lại bản kết hợp; evidence cũ phải còn đúng input. | S46,S48 | T107,T034,T040,T094,T100 |
| R112 | P0 | Không làm yếu test/validator/gate để xanh; phân biệt policy documented/enforced/verified và claim sản phẩm có evidence/quyền riêng. | S46,S48,S50 | T003,T108,T040,T041,T042,T094,T100 |
| R113 | P0 | GC theo FK leaf-before-parent, rollback toàn generation, không disable FK hoặc xóa active/durable records. | S44 DB-03 | T106,T035,T034 |
| R114 | P0 | Resolver digest đồng nhất binding ID/natural key/cache; nhiều resolver cùng source/map hợp lệ, immutable. | S43 CT-04,S44 DB-03/DB-07 | T106,T081,T085 |
| R115 | P0 | Preview status read không gia hạn quyền; SDK liveness riêng; revoke/expired/stale disarm, no old ACK escalation. | S43 CT-08,S45 AL-04 | T105,T107,T082,T087,T088,T094 |
| R116 | P0 | Mapping/rename/source có bounded continuation đầy đủ, same result/vector, no partial apply/repeated LSP/cursor loop. | S43 CT-05,S45 AL-02 | T105,T107,T025,T029,T030,T090,T093 |
| R117 | P0 | Group input giữ pinned vector+typed ref/member, expected-unavailable riêng, không fake generation hoặc latest fallback. | S43 CT-06,S42 DF19 | T105,T107,T045,T046,T063,T093,T096 |
| R118 | P1 | RuntimeConfig/defaults/precedence/CAS/restart rõ, không lẫn policy/build budget hoặc vượt hard wire caps. | S43 CT-07,S13,S15 | T105,T107,T004,T028,T033 |
| R119 | P0 | Group/permission/record lifecycle có get/list/mutation CAS theo authority, phân trang; CLI-only không expose MCP/HTTP control. | S43 CT-07,S44 | T105,T107,T004,T046,T047,T087,T092 |
| R120 | P0 | Shared lifecycle pool lease/exclusive maintenance, scoped restore journal+new incarnations, không replace khi reader còn mở. | S44 DB-05/DB-06,S42 DF20 | T105,T106,T107,T007,T010,T035,T036,T037 |
| R121 | P0 | MCE-C14N1 byte codec+recipe+cross-language vectors; capture manifest bất biến độc lập parse/type result; portable path validator. | S43 CT-04,S45 AL-01 | T006,T105,T107,T020,T081,T085 |
| R122 | P1 | HMR export/import/watch local opt-in, immutable artifacts/coalesce/busy/status; không source exec hoặc auto Interact. | S45 AL-08,S38 | T085,T087,T091,T094 |
| R123 | P1 | Retention/records continuity/lock wait đo thực; GC không auto reapprove source, không thêm unlimited snapshot leases. | S44 DB-07,S16,S27 | T025,T035,T039,T047,T092,T099 |
| R124 | P0 | Đóng audit A01–A08 ở đúng schema/DDL/flow/task/case; document proof khác product test, không claim universal 100%. | S48,S52 | T003,T108,T040,T042,T081,T094,T100 |
| R125 | P0 | Mỗi 10 task DONE duy nhất theo S47 tạo một batch commit; không theo ID số, file, phiên hoặc task giả; tail/recovery có điều kiện. | S53 GIT-01/GIT-02 | T001,T003,T108,T040 |
| R126 | P0 | Commit đủ source/tests/contracts/migrations/lockfiles/docs và materials cần tái dựng; không secrets/output/rác hoặc thay đổi ngoài quyền. | S53 GIT-03/GIT-04 | T107,T040,T041 |
| R127 | P0 | Mỗi checkpoint thử checkout/restore riêng từ đúng commit; source rollback khác runtime/data restore và không phá working tree hiện tại. | S53 GIT-06 | T107,T034,T042 |
| R128 | P1 | Batch ID/trailers/tag bất biến, resume theo Git evidence; không own-hash vòng lặp hoặc commit phụ chỉ để ghi receipt. | S53 GIT-05 | T003,T040 |
| R129 | P0 | Push nguyên nhóm đúng remote/branch có quyền; retry xác minh refs không lặp commit/force; backup độc lập và giới hạn rõ. | S53 GIT-01/GIT-07 | T001,T107,T041 |
| R130 | P1 | Giữa nhóm giữ tiến độ/snapshot có quyền, không WIP commit; kiểm bản kết hợp và workflow riêng với product verification. | S53 GIT-04/GIT-06/GIT-08 | T034,T040,T042 |

## S02 — Độc lập triển khai, license và nguồn gốc

IP-01 MUST NOT nhập/copy/biến đổi source, test, comment, prompt, fixture, schema đặc thù hoặc tài liệu nội bộ từ GitNexus/Graphify. Không đổi ngôn ngữ để che nguồn gốc. Không phụ thuộc engine của chúng qua CLI/API/package.

IP-02 MUST dùng đặc tả này, yêu cầu người dùng, quy tắc đã tiếp nhận theo S50 và tài liệu công khai chính thức của chuẩn/thư viện. Fixtures tự viết từ invariant; không chuyển test sản phẩm khác sang test mới. Khi cần thuật toán phổ biến, tự hiện thực từ mô tả độc lập hoặc dùng thư viện có license phù hợp.

IP-03 Các trao đổi trước đây đã có tiếp xúc source của sản phẩm tham khảo. File này không chứng nhận clean-room pháp lý. SHOULD bắt đầu phiên coding mới chỉ với file này và repository mới; không đính kèm source tham khảo. Rà soát nguồn gốc vẫn bắt buộc. Phân biệt ý tưởng/chức năng với cách thể hiện theo nguyên tắc tham khảo REF15; luật áp dụng và các quyền khác không được kết luận từ đặc tả này.

IP-04 Code riêng mặc định PRIVATE; chưa chọn giấy phép phát hành. Không tự gán Apache/MIT cho quyền của người khác. Trước phát hành công khai/thương mại phải có quyết định của chủ sở hữu, kiểm tra tên sản phẩm, notices, dependency và quyền liên quan. Tên `Maps-Context-Engine-4D/mce4d` chỉ là nhãn nội bộ.

IP-05 Allowlist mặc định cho dependency code: MIT, Apache-2.0, BSD-2-Clause, BSD-3-Clause, ISC, 0BSD; thành phần public-domain/CC0 phải có bằng chứng cụ thể. License chưa biết, noncommercial, source-available hạn chế hoặc copyleft ngoài policy → BLOCKED đến khi đánh giá. Đây là policy thận trọng của dự án, không kết luận rằng mọi license ngoài danh sách đều cấm dùng.

IP-05a **Ngoại lệ người dùng duyệt ngày 2026-11-11:** `github.com/hashicorp/golang-lru/v2 v2.0.7` theo MPL-2.0 được hiện diện và tải trong đồ thị dependency/test của `modernc.org/sqlite v1.62.0` cho fixture/kiểm thử cô lập. Ngoại lệ không mở allowlist chung, không cho gắn module đó vào product binary, vendoring hoặc phân phối source/module của nó. Trước release hoặc khi dependency/build target thay đổi, kiểm lại package graph của từng artifact/platform; nếu module xuất hiện trong product artifact hoặc cần phân phối source, BLOCKED đến khi có đánh giá và authority riêng. Go `go.sum` có checksum module test không đồng nghĩa source module đã được phân phối. Fixture T002 kiểm CLI package graph hiện tại; không suy các platform/artifact tương lai đã được kiểm. Xem A041 và evidence T002.

IP-06 Kiểm tra đúng artifact và version, cả transitive dependencies, generated/vendored code, grammar, binary phụ trợ, assets và tài liệu phân phối. Metadata package có nhiều license không được rút gọn tùy tiện thành một license. Giữ NOTICE/copyright theo điều kiện áp dụng; REF11/REF12/REF16 là điểm bắt đầu kiểm chứng, không thay thế audit release.

IP-07 Mỗi dependency mới cần: lý do không dùng stdlib/dependency sẵn có; version/hash/source; license artifact; API đã đọc; test tương thích; rủi ro bảo trì. AI không tạo dependency chỉ để theo framework mẫu.

IP-08 Không copy tài liệu tham khảo dài vào file này. Chỉ lưu quyết định và URL nguồn ở S23. Nguồn kỹ thuật không trở thành nội dung gửi vào model ở mọi task.

## S03 — Quyết định công nghệ và môi trường

### Stack R1

| Thành phần | Quyết định | Ràng buộc |
|---|---|---|
| Application/core/CLI | Go; chọn stable supported và pin tại T002 từ release history chính thức [REF01] | T002 xác minh compatibility và pin; không tải toolchain lúc runtime |
| API client AI | MCP STDIO qua official Go SDK [REF02,REF03] | Không tự viết lại protocol; không bắt buộc HTTP |
| Storage | SQLite embedded; `database/sql`; `modernc.org/sqlite` baseline [REF04,REF11] | Không ORM; kiểm tra FTS5, pragmas, license và platform trước pin |
| Go syntax | `go/parser`, `go/ast`, `go/token` | Parse bytes đã capture, không chạy code |
| Go semantics | `go/types` qua `golang.org/x/tools/go/packages` [REF07] | Chỉ profile được phép; thiếu type info → partial |
| Go rename/semantic query bổ sung | `gopls` local qua LSP [REF08,REF09,REF10] | Binary đã kiểm chứng; preview-only; không code action tùy ý |
| Search | Exact symbol/path trước; FTS5 cho metadata/text được phép [REF06] | Parameter binding; không coi lexical hit là semantic reference |
| Watch | Poll inventory/stat theo interval; full reconciliation định kỳ | Không thêm watcher dependency trước khi chứng minh cần |
| Logging/testing/config | `log/slog`, `testing`, JSON stdlib | Không framework logger/config/test bổ sung mặc định |
| OS lock | Adapter nhỏ dùng primitive OS phù hợp | Linux và Windows test thực; cho phép `x/sys` khi cần |

Metadata/release notes không chứng minh dependency bundle đã được thử trong môi trường triển khai. `go.mod/go.sum` chưa tồn tại ở lúc soạn. T002 phải khóa một bộ phiên bản tương thích đã test; không dùng `@latest` trong build/release script. Có thể pin bản ổn định không phải mới nhất sau khi kiểm tra security/compatibility và ghi quyết định.

Git là executable ngoài phục vụ thao tác đọc metadata; mặc định không bundle Git. Dependency executable không bị xem là thư viện liên kết tự động; nếu phân phối kèm phải rà soát riêng.

### Môi trường đích

R1 primary: Linux amd64; WSL2 chạy Linux binary với repo và state trên Linux filesystem. R1 secondary bắt buộc kiểm thử trước nhãn hỗ trợ: Windows amd64 native. macOS/arm64 chưa cam kết.

MUST không chia sẻ cùng state SQLite giữa Windows binary và WSL binary; không đặt state trên NFS/SMB/OneDrive/Dropbox/network share. WAL có yêu cầu/giới hạn filesystem cần kiểm chứng [REF04]. Repo target có thể ở đường dẫn khác nhưng cấu hình đó phải có benchmark/path tests; không tự di chuyển repo người dùng.

Cấu trúc runtime là modular monolith. Explorer là optional adapter/UI theo S38; `mce4d explorer` chỉ phục vụ tài sản của engine và API local khi được chạy explicit, không khởi động target app. `mce4d mcp` phục vụ truy vấn, không là daemon toàn máy. `mce4d index` chạy job hữu hạn; `mce4d watch` là tiến trình foreground chỉ tồn tại khi chủ máy khởi chạy. Không tự đăng ký service, startup task, scheduled task hay Git hook.

Không tái viết Go compiler/language server. Không viết parser đa ngôn ngữ ở R1; R2 ưu tiên TS/JS và Python qua provider/adapter có gate S28. Nếu stack lựa chọn thất bại gate, ghi chứng cứ và ADR thay đổi; không tự thay toàn kiến trúc giữa task.

### Ledger dependency — snapshot ban đầu

| Artifact | Lựa chọn | Pin | License review | Compatibility | Task |
|---|---|---|---|---|---|
| Go toolchain | Stable supported được xác minh ở T002 | 1.27.4 | BSD-3-Clause; archive SHA-256 tại T001 | Linux fixture PASS E003/E004; Windows linker FAIL E002 | T002 |
| SQLite driver | modernc.org/sqlite | v1.62.0 | BSD-3-Clause + SQLite public domain + third-party notices; MPL test graph ngoại lệ IP-05a | FTS/FK/rollback + SQLite 3.54.1 Linux fixture PASS E004 | T002 |
| MCP SDK | github.com/modelcontextprotocol/go-sdk | v1.8.0 | Apache-2.0 và MIT; docs CC-BY-4.0, phân phối xét riêng | JSON-RPC pipe fixture PASS E004; host/STDIO thực NOT_RUN | T002 |
| Semantic Go loader | golang.org/x/tools/go/packages | v0.52.0 | BSD-3-Clause | typed fixture Linux PASS E004; target policy/runtime NOT_RUN | T002 |
| Go language server | golang.org/x/tools/gopls | UNRESOLVED | REVIEW_REQUIRED; REF13 | NOT_RUN | T029 |
| OS adapters | golang.org/x/sys | v0.49.0 (pin đã có từ T002; dùng trực tiếp T005) | BSD-3-Clause, LICENSE upstream đã kiểm; review phân phối tại T041 | Linux/WSL fixture E102–E109; Windows native NOT_RUN | T005,T010 |
| Exact tokenizer | Không bắt buộc R1 | NOT_SELECTED | N/A | N/A | T025 |

T002 đã ghi pin, checksum `go.sum` và license scope sau khi kiểm tra; không suy bằng chứng Linux fixture thành hỗ trợ mọi platform. Các module sau T002 vẫn pin/checksum/license theo task tương ứng. Không blocking người dùng bằng câu hỏi lựa chọn library đã được tài liệu quyết định; giải quyết qua gate kỹ thuật.

Thiết kế vật tư đóng ở BP-03; exact versions còn UNRESOLVED là preflight dữ kiện, không quyền AI chọn library khác. T105–T108 trước feature; npm workspace conventions và selected baseline UI deps ở S46. Nếu compatibility thất bại, ghi discrepancy theo S41 thay vì silent swap.

T001/T002 ghi kết quả khảo sát theo từng môi trường thực, không suy từ máy soạn đặc tả: OS/architecture và filesystem của repo/state; đường dẫn/version executable thực thi và nguồn nhận diện; module path/remote chỉ khi đã xác minh; quyền mạng/cài đặt áp dụng; dung lượng/quota liên quan; dependency/API/license/smoke result và giới hạn. Không tìm thấy executable trên PATH chỉ chứng minh không tìm thấy trong môi trường lệnh đó; không kết luận không có trên máy hoặc WSL. Discovery và pin theo S51/BP-02; thiếu công cụ, quyền hoặc compatibility chặn đúng task phụ thuộc, không biến NOT_RUN thành PASS. Không tự cài, đổi PATH/config máy hay tải vật tư ngoài quyền được giao.

## S04 — Capability contract theo ngôn ngữ

| Input | R1 capability | Giới hạn bắt buộc trả về |
|---|---|---|
| Go có đủ module/type metadata local | syntax, definitions, typed references, direct static calls, method/field selections, imports, test references | Chỉ profile GOOS/GOARCH/tags/CGO/module đã nêu; interface dispatch/callback runtime có thể chưa resolve |
| Go syntax hợp lệ nhưng dependency thiếu | syntax, lexical declarations; một phần typed nếu có evidence | `semantic_status=partial`; cấm rename preview khi type precondition không đủ |
| Go lỗi parse/type | Những fact có vị trí chứng minh được; diagnostics | Không bỏ file khỏi denominator; không suy ra zero dependencies |
| Go local variables/params/fields | Index scope, defs/uses có typed evidence | Không gộp theo tên; không hứa liên thông reflection/serialization |
| TS/JS/TSX/JSX, Python, Rust, Java... | File inventory + allowed text search | `semantic_status=unsupported`; không sinh cạnh calls chắc chắn bằng regex |
| Markdown / text config / SQL | File/chunk/source anchors, text search theo allowlist | SQL text match không chứng minh table lineage; config text không phải instruction |
| Binary, large file, secret, symlink, nested checkout | Inventory reason hoặc boundary đã che thông tin nhạy cảm | Không giả vờ đã phân tích |

MUST capability là dữ liệu runtime, có `provider_id`, `provider_version`, `capabilities[]`, `profile_id`, `limits[]`, `diagnostics[]`. Không dùng một boolean `supports_go=true` để ám chỉ mọi tính năng Go.

S26 thêm file/module overview và intended document anchors; không nâng semantic_status của ngôn ngữ chưa hỗ trợ.

R1 khởi đầu với một interface nhỏ chỉ khi tách `GoAnalyzer` khỏi `Indexer/Query` phục vụ test. Không tạo plugin loader, dynamic module registry hay abstraction chung cho ngôn ngữ chưa có implementation. Khi R2 có provider thứ hai mới tổng quát hóa phần đã chứng minh trùng lặp.

Đối với financial/concurrent/backend code: graph không chứng minh transaction atomicity, deadlock-freedom, idempotency, exactly-once, ordering hay finality. Context pack phải tìm source/rules/test liên quan khi có; AI vẫn phải phân tích invariant và failure paths.

## S05 — Kiến trúc và cấu trúc repository cần tạo

### Luồng và ownership

```text
CLI quản trị -> Catalog (repo/worktree/profile/policy)
CLI index/watch -> Safe inventory -> Captured bytes -> Go analyzer
                                         |                  |
                                         +------> Index writer -> SQLite worktree
                                                                  |
AI host -> MCP STDIO -> Application query -> Snapshot reader -------+
                              |
                              +-> Task context pack -> bounded response/cursor
                              +-> Freshness validator -> current checkout checks
                              +-> gopls session -> rename preview only
```

`Catalog` chỉ giữ registry/config, không là hàng đợi index toàn cục. `Index writer` sở hữu mutation database index. `Snapshot reader` luôn đọc một generation đã publish. `Context builder` không tự sửa/index repo. `gopls session` không sở hữu graph và không được apply source edits. Source repository thuộc người dùng/AI host, không thuộc MCE4D.

### Cấu trúc dự án

```text
maps-context-engine-4d/
  Maps-Context-Engine-4D_SPEC.md     # duy nhất: yêu cầu/kiến trúc/tracker
  docs/                            # quy tắc nguồn giữ nguyên; xem S50, không tracker thứ hai
    AI_RULES(8).md
    AI_RULES_STORAGE_SUPPLEMENT(1).md
    PROJECT_BOOTSTRAP_PROMPT(4).txt
  contracts/                        # schema/registry trích từ S43; không spec cạnh tranh
  go.mod
  go.sum
  .gitignore
  .gitattributes
  cmd/
    mce4d/main.go                     # wiring, exit codes; không business logic
  internal/
    model/                          # pure validated IDs/value types/errors; S43
    ports/                          # small cross-module contracts only when needed; S46
    app/                            # use cases; DTO từ canonical contracts, không tự tạo schema khác
    catalog/                        # registration, profile/policy, rebind/remove
    paths/                          # canonical identity, secure open, ranges
    inventory/                      # Git read-only listing, ignores, hashes
    snapshot/                       # manifests, generation state, freshness
    indexer/                        # pipeline, invalidation, publish
    analysis/golang/                # AST/type facts, call/ref extraction
    store/sqlite/                   # repositories, transactions, migrations
      migrations/                   # SQL; embed vào binary
    search/                         # symbol/path/FTS, graph traversal
    contextpack/                    # task slicing, evidence, budget, pagination
    overview/                       # T049/T051: hierarchy, intended vs observed summaries
    analysis/providers/             # R2 T055: minimal adapters, chỉ tạo khi provider thứ hai có thật
    contracts/                      # R2: explicit HTTP/SQL/event/schema facts
    history/                        # R2: snapshot comparison, source-bound decisions
    domain/                         # R2: approved state/constraint/context profiles; không business execution
    evidence/                       # R3: sanitized artifact imports, không live collector
    lsp/                            # lifecycle, encoding, Go rename preview
    mcp/                            # tool schemas/adapters only
    cli/                            # flag parsing/JSON adapters
    policy/                         # trust, secrets, source allowlists
    runtime/                        # OS lock, exec limits, process lifecycle
    telemetry/                      # redaction, operation metrics
  testdata/
    fixtures/                       # tự viết; mỗi fixture có expected.json
    malicious/                      # path/secret/injection cases tự viết
  tests/
    integration/
    contract/
    acceptance/
  tools/
    spec_check/                     # Go validator cho tracker/IDs/gates
    bench/                          # deterministic harness; không tự gọi LLM
  .github/workflows/                # chỉ khi repository dùng GitHub/CI được phép
  artifacts/                        # output máy sinh, gitignored mặc định
```

Tạo module khi task cần; không tạo hàng chục file rỗng/TODO stub rồi đánh dấu hoàn thành. Không package `utils` tổng hợp không trách nhiệm. Interface đặt tại consumer, nhỏ và có implementation/test; không repository abstraction cho database chưa dùng.

Module path Go: lấy remote thực tế đã được xác nhận khi có. Repo local chưa có remote dùng `maps-context-engine-4d` để phát triển private; MUST NOT bịa GitHub owner/path hoặc xuất bản module. Thay module path về sau bằng task riêng có build/test.

Explorer extension cấu trúc `web/explorer`, `packages/preview-sdk`, `packages/vite-inspector`, `packages/react-inspector` và internal adapters nằm ở S38; không scaffold trước U0/U1.

### State ngoài source

```text
<MCE4D_HOME>/
  catalog.sqlite
  catalog.lock                      # chỉ transaction registry ngắn
  cursor.key                        # random secret; quyền owner-only
  runtime.json                      # RuntimeConfig optional; CT-07, không policy hay bản vẽ khác
  lifecycle.lock                    # catalog store shared/exclusive pool lease; DB-05
  .maintenance/                     # bounded operation journals; không chứa source
  worktrees/<worktree_id>/
    index.sqlite                    # derived generations/facts/build bindings; cache
    records.sqlite                  # durable decisions/manual links/imported evidence; NO cache GC
    lifecycle.lock                  # maintenance lease riêng worktree, không global build lock
    writer.lock                     # primitive OS; không stale TTL tự xóa
    scratch/                        # bounded, owner-only, disposable; rename result spools CT-05
    backups/                        # durable backups tách index quota; records/catalog retention
    logs/                           # rotated; không raw source
```

`MCE4D_HOME` mặc định: Linux `$XDG_STATE_HOME/mce4d`, fallback `~/.local/state/mce4d`; Windows `%LOCALAPPDATA%\mce4d`. Resolve/canonicalize một lần; cho phép override explicit tới local filesystem. Không đặt cache vào target repo và không âm thầm sửa `.gitignore` của nó.

Index là dữ liệu dẫn xuất, có thể rebuild; catalog chứa đăng ký/profile/policy, records.sqlite chứa decision/manual mappings/evidence durable. Cả catalog lẫn records không được xóa theo cache GC. Dữ liệu target repo không nằm trong phạm vi GC.

## S06 — Identity, invariant và trạng thái

### Identity

| Key | Quy tắc |
|---|---|
| `repo_id` | UUID sinh khi đăng ký Git common-dir identity đã canonicalize; không suy từ remote/name. Clone khác là repo khác trừ thao tác link explicit. |
| `worktree_id` | UUID riêng cho checkout; bind root canonical + filesystem identity kiểm chứng. Hai worktree cùng HEAD vẫn khác ID. |
| `profile_id` | SHA-256 của build/module/tool policy canonical: GOOS/GOARCH/tags/CGO, workspace roots, toolchain/provider versions, nonsecret effective config. |
| `generation_id` | UUID cho một lần publish index; không đổi nội dung sau publish. |
| `snapshot_digest` | SHA-256 manifest nội dung + profile/config/policy/provider fingerprint; không chỉ HEAD/mtime. |
| `symbol_id` | Hash trong generation từ language/package-variant/path/kind/lexical-owner/declaration byte range; tên không đủ. Không bảo đảm ID sống qua rename/move. |
| `content_hash` | SHA-256 bytes gốc của một file được phép; không normalize CRLF/Unicode trước hash. |
| `scope_vector` | catalog_incarnation + resolved members (index/records incarnation, epochs) + unavailable_members + group revision; S43 là canonical wire; không tạo generation giả. |
| `binding_id` | engine-bound hash nối producer build_map tới source generation/profile/policy/resolver_digest; recipe CT-04, natural unique key phải cùng đầu vào. |
| `pack_token` | Opaque signed descriptor chứa scope/generation/query/policy/ranking-version/expiry; không chứa source bytes. |

Canonical hashing duy nhất: MCE-C14N1 tại CT-04, kèm recipe/normalization/test vectors. Không dùng JSON.stringify hoặc fmt.Sprint/map iteration thay codec. content_hash luôn SHA-256 bytes gốc, không qua codec. Snapshot identity tách analysis result; metadata HEAD/branch không đủ. Runtime observations/expiry/TTL không vào content fingerprint trừ trường thời gian là nội dung có nghĩa của record cần hash theo recipe.

Path: nội bộ lưu relative path dùng `/`, so sánh theo filesystem thực tế; giữ nguyên case trong hiển thị. Không tự lowercase toàn đường dẫn. Trên Windows kiểm tra case-fold collision, drive/UNC/junction/ADS/device path; trên Linux filename không phải UTF-8 được ghi `UNSUPPORTED_PATH_ENCODING`, không silently drop. Root move/delete/reclone tại cùng path → `ROOT_IDENTITY_CHANGED`, cần `repo rebind` explicit.

Mọi symbol/reference trả ra MUST kèm `worktree_id`, `generation_id`, `profile_id`, `path`, `content_hash`, byte range. Byte offsets `[start,end)` là nguồn chuẩn; hiển thị line dùng 1-based, end_line là dòng chứa byte cuối nếu range không rỗng. LSP position được convert theo encoding đã negotiate; không truyền byte offset trực tiếp thành UTF-16 character.

### Invariant

| ID | Bất biến |
|---|---|
| I01 | Core single-scope đọc một worktree/profile/generation; federation dùng explicit ScopeVector đầy đủ; không fallback theo tên hoặc trộn member phiên bản. |
| I02 | Source snippet và graph evidence có cùng captured content hash/generation. |
| I03 | Generation đã publish bất biến; active pointer chỉ trỏ READY hoặc READY_PARTIAL. |
| I04 | Chỉ writer giữ OS lock của worktree đó mới mutation index; không lock toàn máy qua catalog. |
| I05 | Unknown/partial/unsupported không được chuyển thành complete/no-risk. |
| I06 | Payload parse được đầy đủ; budget không xóa safety metadata hoặc cắt giữa JSON/string. |
| I07 | Không có source mutation, arbitrary command, network fetch hoặc trust escalation qua MCP. |
| I08 | Path/ID/cursor không vượt worktree allowlist; không redirect sang snapshot khác khi cursor hết hạn. |
| I09 | Dependency invalidation bảo thủ; index incremental không được ít đúng hơn full rebuild trên fixture. |
| I10 | Cache retention chỉ xóa derived owned data, không source/catalog/policy/records.sqlite hoặc durable backups. |
| I11 | Cache/session chỉ tối ưu tính toán; không giả định LLM vẫn nhớ nội dung trước. |
| I12 | DONE cần code + tests + evidence trên revision xác định; không đồng nhất có file với có tính năng. |

### State machines

`Generation.state`: BUILDING → READY | READY_PARTIAL | FAILED | CANCELLED. READY_PARTIAL chỉ khi dữ liệu đã capture nhất quán và lỗi phân tích đã được ghi đầy đủ; inconsistent capture không được publish kể cả partial. READY/READY_PARTIAL → RETIRED chỉ bằng GC policy; reader không đọc BUILDING/FAILED.

`Freshness.status`: `observed_current` | `changed` | `unknown` | `not_checked`.
`Freshness.scope`: `workspace` | `returned_files` | `none`.
`Freshness.buffer_scope`: luôn `disk_only` trong R1.
`Freshness.checked_at`: RFC3339 UTC hoặc null.

`observed_current` chỉ là kết quả tại khoảng kiểm tra, không bảo đảm filesystem đứng yên sau đó và không chứng nhận một atomic snapshot của editor. MUST trả `consistency="captured_bytes_with_observed_stability"`. Workspace đang bị chỉnh liên tục có thể không đạt strict verification; trả bounded error, không giữ source lock.

`Coverage.status`: `supported_scope_complete` | `partial` | `unsupported` | `unknown`. Complete chỉ trong scope/capability/profile nêu rõ, không là mọi hành vi runtime. `Readiness`: `reviewable` | `needs_context` | `blocked`; tuyệt đối không có trường `safe_to_edit=true` được hiểu như bảo đảm.

`Job.state`: QUEUED → RUNNING → SUCCEEDED | PARTIAL | FAILED | CANCELLED. Job index có scope worktree, không điều khiển build/test của repository khác. R1 không cần durable job queue; generation BUILDING và log đủ để recovery.

S43 SourceRef canonical byte-only và ScopeVector epochs bắt buộc cho transport; line numbers trong UI/CLI là derived display, không field identity thứ hai. BuildMap và Binding theo S45, record effective state theo S44; browser state transition S45.

## S07 — Mô hình dữ liệu và transaction

### Ownership database

`catalog.sqlite`: repositories, worktrees, profiles, policies, schema metadata. Chỉ transaction registry ngắn; không giữ connection/transaction catalog trong parse, hash, LSP, index, build hay test.

`worktrees/<id>/index.sqlite`: generation, captured bytes, graph facts, diagnostics, search. Một DB/worktree; các profile khác nhau cùng worktree chia sẻ writer này nhưng không chia sẻ active pointer. Giữ FK bật trên mọi connection; không chỉ bật lúc migrate [REF05].

Thiết lập đề xuất: WAL, `foreign_keys=ON`, `synchronous=FULL`, `busy_timeout=5000` ms; read transaction ngắn. SQLite WAL cho phép các read snapshot cùng writer nhưng vẫn chỉ một writer một lúc [REF04]. Chỉ số tối ưu phải đo trước khi thay synchronous/transaction policy.

### Schema logic chuẩn R1

Các bảng dưới đây là logical overview. S44 chứa physical DDL authority: T106 chứng minh migrations trước feature, T007 implement adapters theo DDL đó. Không tự thiết kế lại columns/FKs. Những field bổ sung (epochs/owner/search_documents/records) đọc trực tiếp S44, không suy từ bảng tóm tắt.

| Table | PK / dữ liệu chính | Ràng buộc / index |
|---|---|---|
| `schema_meta` | singleton=1; schema_version; min_reader_version | CHECK singleton; version tăng theo migration |
| `generations` | generation_id; profile_id; snapshot_digest; state; created_at; completed_at; manifest_json; provider_fingerprint; policy_digest | state enum; immutable khi publish; index(profile_id,completed_at) |
| `active_generations` | profile_id → generation_id | FK generation; unique generation; publish cùng transaction kiểm tra state/profile |
| `content_blobs` | content_hash → raw bytes; byte_length | hash unique; byte_length=length(bytes); chỉ bytes được phép |
| `files` | (generation_id,path); content_hash nullable; kind; status; byte_length; package_key nullable | FK generation/blob; index(generation,status); excluded/error có reason, không fake empty bytes |
| `packages` | (generation_id,package_key); import_path; module_key; variant; input_digest; semantic_status | package variant gồm production/test/profile; index(generation,import_path) |
| `symbols` | (generation_id,symbol_id); path nullable; package_key nullable; kind; name; qualified_name; scope_key; start_byte; end_byte; signature; exported | FK(gen,path) files khi local; FK(gen,package) packages; check ranges; index(gen,name); index(gen,qualified_name); index(gen,path,start_byte) |
| `relations` | (generation_id,relation_id); source_symbol_id; target_symbol_id nullable; kind; evidence_kind; site_path; start_byte; end_byte; reason; unresolved_label nullable | Composite FKs tới symbols cùng gen; FK(gen,site_path) files; index(gen,source,kind); index(gen,target,kind); unique semantic tuple+callsite |
| `diagnostics` | (generation_id,diagnostic_id); path nullable; package_key nullable; code; severity; sanitized_message; scope_json | index(gen,severity,code); không raw secret/source trong message |
| `capability_results` | (generation_id,capability_key,scope_key); status; summary_json | status enum; số lượng files/packages skipped/error/unsupported được lưu |
| `search_documents` + `search_fts` | Relational documents scoped generation; external-content FTS5 rowid liên kết documents | Trigger sync; query luôn join/filter authorized generation; physical definition S44 |

Giá trị enum/string phải có schema validation và CHECK nơi phù hợp; JSON phải được parse/validate trước insert. Timestamp dùng UTC; không dùng floating point cho IDs/counts/byte positions. Signature/source text có quota; mandatory source lớn chia fragment có SourceRef exact theo CT-05, không trả cùng item bất tận. `files.status`: indexed, metadata_only, excluded, missing, read_error, parse_error; capability semantic lưu riêng, không lẫn file read thành công với semantic đầy đủ.

Nodes boundary cho external symbols được phép path=null; phải có package/qualified identity và `kind=external`. Không đọc raw source ngoài allowlist chỉ để hiển thị external definition. Relation target=null chỉ cho unresolved reference kèm reason, không là edge chắc chắn tới một tên đoán.

### Chiến lược lưu snapshot

R1 cho phép lặp metadata/facts giữa generations để đơn giản correctness; raw file bytes dedup bằng `content_hash` trong DB của worktree. Không global dedup cross-repo ở R1. Parse cache persistent chưa bắt buộc; no-op update và file-byte cache là ưu tiên trước.

Không tái sử dụng semantic facts chỉ vì file không đổi: profile/imported type/package membership có thể đổi. Chỉ reuse semantic facts khi input fingerprint gồm dependencies đúng scope đã được chứng minh tương đương; nếu không, rebuild semantic graph.

### Publish transaction

Pha scan/parse ngoài SQL write transaction dài. Insert generation BUILDING và facts theo batch hữu hạn; mọi query public lọc theo generation đã publish. Trước publish: kiểm tra hash/range/FK/số lượng/diagnostics/FTS consistency + revalidate manifest. Transaction publish nhỏ: xác nhận owner/job/profile → set READY/READY_PARTIAL → đổi active pointer → commit. Nếu fail, không đổi active pointer. Không query `latest created_at` để chọn index.

Read: bắt đầu transaction → lấy active generation đúng profile hoặc explicit generation → đọc facts/snippets/coverage cùng snapshot → kết thúc transaction trước gọi LSP/stream nhiều trang. Không giữ read transaction qua lượt hội thoại; page sau dùng generation token. Nếu generation đã GC → `SNAPSHOT_EXPIRED`.

Integrity verification khi publish: mọi local range nằm trong captured bytes; mọi edge endpoint cùng generation; mọi unresolved có reason; FTS không xuất generation chưa publish; capabilities tổng hợp từ manifest không bỏ sót lỗi. Parameterized SQL bắt buộc; không cung cấp raw SQL MCP.

Migration: backup đúng cách trước migration của catalog; index có thể rebuild vào DB mới nếu incompatible. Không copy một file SQLite đang WAL hoạt động rồi coi là backup đầy đủ; dùng backup API/controlled offline checkpoint theo REF14. Older binary phát hiện schema mới → `SCHEMA_TOO_NEW`, không “sửa cho chạy”.

Durable approved/mapping versions ở records.sqlite theo S44, không embed vào index cache. Database/sql transaction ownership và policy/record optimistic checks theo DF-19/DF-20; không cross-file atomic claim.

## S08 — Inventory, capture, index và cập nhật

### Pipeline chuẩn

1. Validate authorization/root/worktree/profile/config/version. Resolve công cụ allowed; đọc active metadata không làm side effect lên target repo.
2. Acquire writer lock của worktree cho index job; job khác cùng worktree chờ hữu hạn hoặc nhận `WORKTREE_BUSY`. Reader không cần writer lock.
3. Enumerate candidate files: Git tracked + untracked được phép; xử lý deletion, unmerged state, sparse/nested boundaries. Git NUL-delimited tránh lỗi tên file có whitespace/newline [REF17].
4. Áp dụng deny policy trước đọc bytes. File riêng tư bị từ chối không được đưa vào blob/FTS/log. Theo dõi reason/count; không coi `.gitignore` là security policy.
5. Capture bytes của regular file bằng secure open; record filesystem identity/stat trước/sau; hash bytes gốc. File bị đổi lúc đọc → retry tối đa 2 lần rồi lỗi instability. Không normalize line endings.
6. Tạo CaptureManifest bất biến: capture status chỉ captured/metadata_only/excluded/missing/read_error; membership/path/hash hoặc reason; profile/tool/policy/config và dependency fingerprints. Tính snapshot_digest theo CT-04, không chứa parse/type result. Freeze manifest trước provider start.
7. Parse/resolve trên captured bytes. Go typed analyzer dùng overlay/snapshot inputs khớp bytes; package loader đọc metadata theo cấu hình explicit [REF07]. Nếu provider không thể chứng minh nó phân tích đúng captured inputs, không publish semantic-complete.
8. Tạo symbols/relations/diagnostics/capabilities; projection files.status có thể indexed hoặc parse_error từ captured input, nhưng không sửa CaptureManifest/snapshot_digest. Lexical/text index không thay thế typed facts. Validate toàn bộ ranges/identity.
9. Re-enumerate membership và kiểm tra full content/config digest trước publish. Có thay đổi → hủy candidate/retry job tối đa 1 lần với snapshot mới; vẫn thay đổi → `WORKTREE_CHANGED_DURING_INDEX`.
10. Publish theo S07. Release lock và tài nguyên trong mọi exit path. Lưu metrics/evidence không source.

Bước 5/9 cung cấp observed stability, không atomic snapshot filesystem. Tính đúng của source snippet được bảo đảm bằng captured bytes bất biến; không hứa mọi file từng đồng thời có nội dung đó trong một instant nếu writer ngoài MCE4D đang thay đổi. Operation cần metadata/type coherence mà không kiểm chứng được phải degrade hoặc fail rõ.

### Git/file handling

Git chỉ đọc metadata/inventory. Không checkout/fetch/reset/clean/submodule update, không hook, pager, external diff, textconv hoặc fsmonitor command từ target config. Exec bằng argv, cwd/root cố định; validate path bắt đầu `-` và dùng delimiter khi áp dụng. Không build shell string. Test bảo đảm environment/config không cho repo chèn command.

Include tracked source và tests mặc định nếu không vi phạm policy; untracked được phép theo effective ignore. Generated source được index với `generated=true` khi thuộc build; không tự đề xuất sửa generated output. Vendor/dependencies/.git/build artifacts mặc định boundary/excluded; include cần explicit scope. Nếu build dùng vendor hay replace local mà input bị exclude, semantic completeness phải giảm hoặc profile được cấu hình cho phép.

Nested Git root/submodule: không tự traverse; báo boundary và yêu cầu đăng ký riêng. Symlink/junction bị từ chối mặc định; không chỉ kiểm tra string prefix root. Sparse checkout và inaccessible file phải xuất hiện trong coverage.

Git include/ignore behavior và global ignores phải được pin trong effective policy. Khi chạy Git với config sanitized, không được vô tình hứa giống đầy đủ config cá nhân bị vô hiệu; ghi rõ rule effective và fingerprint. HEAD có thể null cho repo chưa commit; vẫn index files được phép.

### Incremental correctness

| Thay đổi | Invalidation bắt buộc |
|---|---|
| Không đổi manifest/profile/policy/provider | NOOP; không rebuild graph và không gọi LLM |
| Sửa nội dung file | Recapture/parse file; semantic scope tối thiểu package + reverse dependents nếu đã chứng minh đủ |
| Thêm/xóa file | Re-enumerate package membership, remove edges cũ khỏi generation mới, re-resolve references |
| Rename/move file | Xử lý delete+add an toàn; không yêu cầu đoán identity qua rename |
| go.mod/go.sum/go.work, build tags, GOOS/GOARCH/CGO, local replacement | Invalidate semantic scope đầy đủ của profile; fingerprint input/config |
| Exported type/signature, interface, embedding | Re-resolve importers và candidate dispatch; fallback semantic full rebuild |
| Provider/toolchain/schema/policy/ignore rule | Rebuild phần bị ảnh hưởng; nếu không chứng minh boundary → full |
| Branch checkout không đổi HEAD nhưng dirty khác | Manifest phát hiện; không dùng HEAD làm cache key đủ |
| External allowed dependency thay đổi | Invalidate consumers; external unverified dependency → coverage partial |

R1 baseline: reuse captured blobs/không parse lại bytes không đổi khi có cache phù hợp, nhưng semantic full rebuild sau thay đổi được chấp nhận đến khi T021 chứng minh selective invalidation. Phải báo `incremental_mode=content_only|semantic_selective|full`, không gọi content-only là toàn pipeline incremental.

### Freshness trước truy vấn/sửa

`explore/locate`: MAY kiểm tra returned_files hoặc trả not_checked; luôn ghi scope/limitations.
`context_pack` cho rename/type/behavior/contract/debug: default `freshness_mode=checked_workspace`, kiểm tra workspace manifest đầy đủ, hash nội dung theo membership hiện tại, kể cả config và file mới; quá deadline → `FRESHNESS_UNKNOWN`, không tự hạ về HEAD-only. Chế độ explicit `analysis_snapshot` theo S27 được phép cho điều tra trên captured snapshot nhưng luôn needs_context/not_checked và không thay validation trước sửa.
`expand_pack`: đọc đúng generation và policy. Checked-workspace pack kiểm tra returned files; mismatch → edit preparation blocked. Analysis-snapshot pack trả historical captured source với not_checked/needs_context, không full-scan mỗi trang, không quảng cáo current source. Trước apply vẫn bắt buộc validate_pack/full workspace và host source preconditions.
`validate_pack`: full workspace verification default; checked range precondition + membership/profile; output không phải khóa filesystem. AI consumer kiểm tra lại hash ngay trước khi apply patch qua công cụ sửa riêng.

Watch/stat cache chỉ là tín hiệu tối ưu. Mtime/size bằng nhau không chứng minh bytes bằng nhau. R1 strict validation không được skip full hash dựa duy nhất mtime, Git clean, hoặc watcher không có event.

Sau AI sửa source: `validate_pack` của bản trước phải phát hiện mismatch; gọi `mce4d index` khi được phép rồi tạo pack mới. Không tự relabel edge cũ là current. Crash/timeout index không khiến source cũ được coi là mới.

## S09 — Go facts, reference resolution và concurrency

### Phân tích Go

Syntax facts lấy từ AST captured bytes: file/package/declaration, lexical scope, source range, import text, struct tags; syntax-only relation phải ghi evidence_kind đúng. Typed facts lấy từ `types.Info`/package metadata trong profile: Defs/Uses/Selections/Types và method sets. Local variable, parameter, receiver, named result, field phải có identity riêng khi được hỗ trợ.

Phân biệt package production, internal-test variant, external-test package; dedup cùng source occurrence với variant provenance, không nhân số caller vì test variant. Closure/anonymous function có scope ID theo location; không gộp mọi closure cùng tên rỗng. Package identifier không chỉ là folder basename.

Semantic loader: module-aware, explicit workspace roots; không gọi arbitrary GOPACKAGESDRIVER. Environment whitelist; `GOTOOLCHAIN=local`, `GOPROXY=off`, `GOSUMDB=off`, `GOPACKAGESDRIVER=off`; hiệu lực Go flags được kiểm tra, không kế thừa `GOFLAGS` tùy ý. `-mod=readonly` khi hợp lệ để không sửa go.mod/go.sum. `CGO_ENABLED=0` là default an toàn cho profile R1, không đồng nghĩa đã hỗ trợ code yêu cầu CGO. Profile khác cần test/quyền tương ứng.

Typed preflight MUST kiểm tra package-input closure: file bị deny, ignored nhưng cần cho build, local replace hoặc dependency root chưa được cho phép không được âm thầm đọc lại qua loader để lách policy capture. Không xác minh được closure an toàn → chỉ syntax/partial cho scope đó. Thư viện/LSP không phải security sandbox; chỉ khởi chạy trên roots và toolchain đã được user policy cho phép. Analyzer không được đưa snippet/diagnostic chứa raw source từ dependency ngoài payload allowlist.

Missing module/toolchain → diagnostics và syntax fallback, không tự `go get`, tải network, chạy generator/build/install. Trusted local cache/stdlib chỉ đọc metadata cần thiết; dữ liệu source ngoài registered allowlist không đưa vào payload. Local replace ngoài root cần explicit allowlist/fingerprint; nếu không, boundary.

### Relation kinds và bằng chứng

| Relation | Hướng | Điều kiện |
|---|---|---|
| CONTAINS | lexical owner → declaration | AST location |
| IMPORTS | package/file owner → package symbol | import đã resolve; unresolved phải riêng |
| REFERS_TO | enclosing symbol/scope → declaration | Typed binding; mỗi occurrence có site |
| READS / WRITES | enclosing scope → binding | AST/type đủ xác định thao tác; compound update có cả hai; alias không chắc chỉ REFERS_TO |
| CALLS_DIRECT | callable owner → concrete function/method | Static target chứng minh bằng type info |
| CALLS_INTERFACE | caller → interface method | Không suy thành concrete runtime callee |
| MAY_DISPATCH_TO | interface/callsite owner → candidate implementation | Type-compatible candidate, `evidence_kind=type_possible`; không là call đã xảy ra |
| UNRESOLVED_REF / UNRESOLVED_CALL | owner → null | Giữ reason/site/label; không bỏ cạnh im lặng |
| TEST_REF | test scope → symbol | Có static reference; không chứng minh test kiểm tra mọi hành vi |
| DOC_LINK | documented owner → source/document symbol | Link explicit resolve được; text co-occurrence chỉ search result |

`evidence_kind`: syntax_exact, type_resolved, type_possible, explicit_document, text_match, unresolved. Không dùng số % confidence tự nghĩ ra. Provider output phải nêu giới hạn: reflection, function values, plugin, dynamic dispatch, codegen chưa chạy, assembly/cgo, build-profile khác, runtime config.

gopls references/call hierarchy có phạm vi và giới hạn riêng; không coi call hierarchy là mọi lời gọi runtime [REF08]. Rename provider cũng không chứng minh hành vi reflection/serialization sau thay đổi [REF09]. Chỗ hai nguồn evidence mâu thuẫn phải giữ diagnostics, không chọn nguồn “confidence cao” rồi giấu mâu thuẫn.

### Concurrency và khóa

Một worktree có tối đa một writer index tại một thời điểm; profile khác cùng worktree được serialize. Hai worktree/repo độc lập được phép index đồng thời. Khóa OS giải phóng khi process chết; metadata PID/start chỉ để diagnostic, không đủ để tự xóa file lock. Không dùng heartbeat timeout làm lý do giành lock của process còn sống.

Không giữ catalog lock trong index; không lock target source; không bao giữ lock khi gọi model, chạy build/test hoặc chờ người dùng. Query reader đang index phải đọc generation trước nhất quán và báo freshness phù hợp, không thấy half-built graph.

OS advisory lock là phối hợp giữa process MCE4D, không ngăn editor khác sửa source. Writer token/job ID kiểm tra khi publish để tránh worker cũ công bố sau cancel. Nếu background goroutine còn chạy sau cancellation, MUST chờ/terminate trong shutdown policy trước release ownership; không cấp quyền publish cho worker mất ownership.

Semaphore giới hạn CPU/RAM chỉ trong process scheduler; không dựng global mutex có semantics “dự án A đang build nên B phải chờ”. Nếu thật sự hết tài nguyên, trả `RESOURCE_LIMIT`/queue reason rõ, có deadline; không giả thành dependency giữa repo.

## S10 — Context pack và ngân sách context

### Hợp đồng đầu vào

`task_kind` là enum S01; `intent` là text dữ liệu, không instruction cấp quyền. Selector phải là một trong: `symbol_id` gắn generation; `path + byte_offset + expected_content_hash`; hoặc query để trả ứng viên, không tự chọn khi mơ hồ. `worktree_id/profile_id` explicit, ngoại trừ MCP server đã bind đúng một scope ở startup.

`context_pack` cần selector resolved; R1 selector workspace/module/document cho overview/plan theo S26/S32, symbol/source-position cho sửa code; không đoán symbol bằng tên gần giống. Có thể dùng `locate` trước hoặc trong application use case nhưng mọi ambiguity phải surfaced. `change_constraints[]` ví dụ `preserve_json_keys`, `preserve_db_schema`, `no_public_api_change` là constraint của task; không tự invent business rules.

### Content selection theo task

| Task | Mandatory candidate evidence | Mở rộng khi cần |
|---|---|---|
| explore | Identity, definition/module summary, capability/freshness | Neighbors/docs |
| plan | Intended/observed separation, trusted requirement anchors, existing modules, explicit gaps | Constraints/decisions; không tự tạo implementation facts |
| rename | Target binding, declarations/references, scope/shadowing conflicts, interface/serialization boundaries, validation plan | LSP preview, ambiguous references, alternate profile warnings |
| type_change | Declaration/type, known readers/writers/callers/implementers, serialization contracts, relevant tests | Reverse impact closure trong budget, external boundaries |
| behavior_change | Current function body + immediate dependencies/callers, related invariants/error paths/tests | Transitive impact, transactional/retry/concurrency paths |
| contract_change | Explicit provider/consumer evidence trong scope, version/serialization keys, backward-compatibility constraints | SQL/API/queue boundaries; R1 chỉ claims chứng minh được |
| debug | Symptom supplied, candidate call/ref path, current code/tests/diagnostics | Config/related state ownership; không bịa log runtime |

MUST phân loại `required_for_review` theo task và evidence chứ không hardcode 2-hop là đủ. Start từ target, expand incoming/outgoing relevant kinds với cycle detection, bounded nodes/edges/depth/time. Khi chạm boundary hoặc limit, thêm gap/continuation; không tuyên bố đã có toàn blast radius.

### Pipeline chọn ngữ cảnh

1. Resolve identity, capture request scope, chọn published generation.
2. Freshness/policy/capability check; edit preparation không đạt → blocked/needs_context; analysis_snapshot chỉ cung cấp historical evidence, không cấp readiness thay validation.
3. Lấy declaration, typed references, các boundary và tests đã có; explicit query được parameterized.
4. Gắn từng evidence với why_included, source hash/range, relation kind, evidence kind.
5. Hợp nhất overlapping ranges trong cùng file/hash; không gộp across files hoặc across snapshots.
6. Sắp xếp deterministic: safety/identity → target → required constraints/refs → related tests → optional neighbors/docs. Tie-break bằng kind/path/start/id.
7. Đóng gói budget theo item/range có ý nghĩa; không cắt giữa codepoint, token JSON, signature hoặc warning.
8. Nếu required evidence vượt budget: trả `needs_context`, required_pending count, continuation và instruction dữ liệu “expand trước khi sửa”; không giả đã đủ.
9. Tạo signed token xác định truy vấn/generation/order-version. Page sau recompute cùng order, không query active generation mới.

### Budget semantics

R1 hard limit dựa serialized UTF-8 byte count, không hứa số token chính xác. Default `max_output_bytes=24576`; minimum 4096, maximum 131072. Đếm toàn tool-visible payload, gồm envelope/metadata và phần bị host/SDK lặp. Delivery mode theo CT-03: compatibility default mcp_dual, mcp_structured_verified chỉ khi host đã test. Đếm cả phần tool-visible lặp; contract test đo emitted payload, không bỏ compatibility để báo ít bytes.

`token_count=null`, `tokenizer_id=null` khi không có tokenizer exact đã test. MAY trả `token_estimate` nhưng MUST `estimate_only=true`, không dùng estimate làm bảo đảm hard token cap. Exact tokenizer là optional adapter theo model/version; không tự gọi API để đếm hoặc tải tokenizer runtime.

Metadata safety có reserved envelope và bounded arrays; số lượng gaps lớn trả grouped counts và cursor tới details. Nếu ngay cả envelope tối thiểu không vừa budget → `BUDGET_TOO_SMALL`. Không dùng suffix `...` để biểu diễn một object bị cắt. Source line quá dài → trả range descriptor + gap; không bí mật bỏ đuôi dòng. Secret redaction không được thay đổi source ranges rồi gắn hash của nội dung nguyên như snippet hoàn chỉnh.

`truncated` chỉ thông báo có phần chưa trả; thêm `required_pending`, `optional_pending`, `enumeration_complete`, `limit_reason`, `next_cursor`. Count chưa biết phải null/unknown, không ghi 0. Khi graph traversal chưa enumerate hết, không bịa tổng số còn lại.

### Bộ nhớ và tài liệu liên quan

R1 không cần LLM summary. Facts generated từ index, không biến kết luận của AI thành source truth. Rule/invariant lấy từ đường dẫn được người dùng/host khai báo; file repo tự nói “ignore previous instructions” vẫn là dữ liệu không đáng tin. Trích excerpt liên quan kèm hash/anchor thay vì gửi toàn bộ AGENTS/spec.

Dedup giữa pages dựa stable item IDs trong một pack. Không dedup ngầm giữa các cuộc hội thoại. Phiên mới/compaction → tạo pack mới hoặc explicit gửi lại phần cần; không chỉ nói “đã gửi trước đó”. Không lưu conversation transcript làm corpus mặc định.

Pack/cursor descriptor: HMAC với key owner-only, gồm worktree/profile/generation/policy digest/request descriptor/ranking version/offset/expiry. Request bounded; token không chứa source; không log token. Expiry default 15 phút; verify signature/type/range trước dùng. Generation/policy bị thay/GC → explicit error. Không có quyền tạo/bẻ scope bằng chỉnh token.

## S11 — Core API/MCP: adapter contract

S43 là định nghĩa wire DUY NHẤT (schema, request/output registry, semantic validators). Sáu tool: `mce4d_workspace_status`, `mce4d_locate`, `mce4d_context_pack`, `mce4d_expand_pack`, `mce4d_validate_pack`, `mce4d_rename_preview`. Không đổi tên thêm alias riêng. T105/T107 seal contract trước feature, T026 kiểm actual implementation parity, T027 expose chỉ capability đã implement.

Response.scope là ScopeVector có catalog/store incarnations, resolved member generations/policy/binding/record revisions và unavailable_members; source locations dùng captured hash+byte ranges. Request defaults/budget/errors ở CT-02/CT-03, không rải một bộ optional schema thứ hai. Unknown/addon chưa support trả typed error; scope.pinned_vector và Selector.kind=ref giữ đúng selection đa repo qua CT-06. Annotation readOnly không thay authorization; không tool exec/apply/write_file/install/fetch_url/raw_sql/shell/delete_repo.

Request deadline/cancel propagate app/store/provider. Domain tool failure map SDK isError; MCP framing/log stdout riêng. No auto index/LLM/network/spend. SDK/server version được pin và contract-tested; schema meta-validation không chứng minh host compatibility.

Read convenience fallback không được cấp lại quyền: missing/partial/unsupported khác zero dependency; safety fields không bị cắt để vừa token budget. Source data từ published generation cùng scope only.

## S12 — Quy trình sửa code và rename preview

### Hợp đồng với AI coding consumer

1. Xác định đúng worktree/profile, gọi status và locate khi cần.
2. Lấy pack theo task; đọc source/rules/test evidence, không chỉ danh sách filename.
3. Resolve `required_pending`, ambiguity và những gap có thể thay đổi quyết định. Gap runtime không thể resolve phải được ghi vào risk review, không xóa khỏi response.
4. Lập thay đổi nhỏ nhất, nêu invariant cần giữ; không suy từ tên field thành thay đổi JSON/SQL/API/message key.
5. `validate_pack` và so expected file hashes ngay trước sửa. Nếu bất kỳ precondition không khớp: dừng apply, lấy source mới; không fuzzy-apply patch cũ.
6. Thực hiện sửa bằng editor/host đã được người dùng cho phép, không bằng MCE4D R1.
7. Build/test/lint phù hợp project thực tế; kiểm tra diff và lỗi cùng pattern. Ghi PASS/FAIL/NOT_RUN với nguyên nhân, không fake test.
8. Index lại khi được phép, kiểm tra impacted references và regression; không commit/push/deploy nếu không thuộc quyền/task.

Số liệu token bao gồm cả bước sửa lại khi fail. Graph không thay code review. Transaction/retry/idempotency/concurrency chỉ được kết luận từ code/contract/test, không vì node cùng cluster.

### Rename preview Go

MUST gọi LSP `prepareRename` rồi `textDocument/rename` theo capability negotiated; dùng session có đúng build profile và source bytes. LSP response là đề xuất edit, không tự apply. Client MCE4D MUST từ chối `workspace/applyEdit` và arbitrary `workspace/executeCommand` nếu server gửi; không chạy code action tùy ý.

Gopls khởi chạy từ absolute binary path được allowlist, sanitized env, cwd scoped. Không tải/cài binary runtime. R1 cho phép session theo request và shutdown sau idle ngắn; không chạy LSP cho mọi repo khi MCP start. Startup/read/rename deadline tách nhau; stdout LSP framing không được trộn log.

MUST hỗ trợ UTF-16 mặc định của LSP và encoding được negotiate theo protocol; convert bằng bytes captured, test Unicode nhiều byte/surrogate pairs/CRLF. Reject position giữa UTF-8 codepoint, out-of-range và mismatched version/hash. Không round vị trí sang symbol lân cận.

Preview post-validation: mọi edit nằm trong allowed worktree, file hash/version khớp; không overlap edit mâu thuẫn; ghi tất cả edits, không chỉ một occurrence/file; không phát sinh resource rename/create/delete ở R1. Package move/rename directory trả unsupported thay vì tự di chuyển file.

Rename pagination CT-05: complete LSP computation/postvalidation trước page1, immutable bounded temporary result; continuation không gọi lại LSP; final host validation nhận result_token tại tool validate hiện có. Chỉ initial request nhận selector/new_name, continuation nhận result_token/cursor. Không áp page1 khi các edits/conflicts còn pending.

Default `preserve_contracts=true`: không chấp nhận chỉnh literal/tag contract mà chưa explicit task constraint. Field rename có thể làm thay đổi default JSON field name ngay cả khi không có json tag; MUST flag `IMPLICIT_SERIALIZATION_NAME_CHANGE` khi applicable evidence, không coi không sửa string là giữ contract. Reflection/runtime string lookup phải là warning/boundary nếu chưa chứng minh không liên quan.

Typed rename không phải guarantee runtime behavior; gopls mô tả các trường hợp reflection/type assertions cần review/test [REF09]. Preview phải có `validation_required=true`, `applied=false`, `source_mutation_count=0`.

`new_name` validate theo ngôn ngữ và provider; không regex global replacement. Missing type info/conflicts → typed error. Không fallback rename thành text replace khi provider fail. Không viết riêng full rename engine trong R1.

Build/test suggestion lấy từ config/build files của đúng repo khi có bằng chứng. Nếu chưa biết command: `validation_plan.status=unknown`, gợi ý khảo sát đúng files; không tự bịa `npm test`/`go test ./...` cho mọi repo. Chạy test target có thể thực thi code, nên nằm ngoài read-only permission của MCE4D.

Multi-file apply không được quảng cáo atomic qua filesystem nếu host không có transaction. Khi host apply một phần rồi lỗi: báo partial, kiểm tra diff từng file, không tự reset toàn worktree hoặc xóa thay đổi của người khác.

## S13 — CLI, cấu hình và tích hợp Codex

### CLI contract

```text
mce4d version --json
mce4d doctor --json
mce4d repo add --root <absolute-path>
mce4d repo list --json
mce4d repo rebind --worktree-id <id> --root <absolute-path> --confirm
mce4d repo remove --worktree-id <id> --confirm [--purge-index]
mce4d profile set --worktree-id <id> --input-json <profile-file>
mce4d index --worktree-id <id> --profile-id <id> [--full]
mce4d watch --worktree-id <id> --profile-id <id>
mce4d query status|locate|context|expand|validate|rename-preview --input-json <file-or-dash>
mce4d mcp --worktree-id <id> --profile-id <id>
mce4d cache inspect --worktree-id <id> --json
mce4d cache gc --worktree-id <id> --dry-run
mce4d cache gc --worktree-id <id> --confirm
mce4d bench --suite <local-suite-path> --output <artifact-dir>
```

CLI flags là spec cần implement. T028 validate help/docs/examples đồng bộ. JSON stdout chỉ kết quả; progress/log stderr. `--input-json -` đọc stdin; MCP STDIO dùng stdin/stdout riêng, không multiplex với CLI input. Exit: 0=ok; 2=argument; 3=blocked/precondition; 4=timeout/busy/resource; 5=internal/storage; 6=partial có data cần xử lý. SIGINT cancel child processes trước thoát.

`repo remove` bỏ đăng ký; không xóa source. `--purge-index` chỉ xóa owned cache sau root-marker validation và confirm; bảo toàn catalog entry history tối thiểu cần audit nếu policy yêu cầu. `doctor` phát hiện binaries/version/platform/filesystem/FTS/config; không install/fix/pull hay đổi setting hệ thống.

### Cấu hình effective

Source precedence: built-in defaults → user MCE4D config → registered worktree profile → validated request subset. Request không được nới secret deny/network/exec/root allowlist. Target `.mce4d.json` không tự trusted; chỉ import sau command explicit và validate. Unknown field là lỗi, không ignore typo.

Profile gồm schema_version, worktree_id, language, analysis_mode, GOOS, GOARCH, build_tags[], cgo_enabled, module_roots[], tests_enabled, generated_policy, dependency_allowlist[], trusted_rule_paths[], exclusions[], budgets. Secret values không nằm trong profile hoặc fingerprint. Environment input chỉ whitelist nonsecret effective values. Không lưu remote Git credentials.

### Codex integration

Codex có đường kết nối MCP local qua STDIO [REF18]. T028 phải xác minh version/khả năng của client được cài và test thực; không mặc định cloud session nhìn thấy local filesystem/server.

Command mẫu, chỉ chạy sau khi có binary và ID thật:

```text
codex mcp add mce4d -- <absolute-path-to-mce4d> mcp --worktree-id <real-id> --profile-id <real-profile-id>
```

Không thực thi placeholder. Không auto edit `~/.codex/config.toml`; nếu được yêu cầu cấu hình, đọc file hiện có, patch block riêng, backup và giữ MCP servers khác. Không ghi secret vào config. Project-local hay user-level config phải theo client thực tế và quyền người dùng; không hứa cấu hình được cho mọi host giống nhau.

MCP start chỉ load metadata/SDK + scope được bind; không scan/index/khởi chạy gopls tất cả repos. Nhiều repo có thể dùng nhiều server binding hoặc một allowlist explicit ở phiên sau; R1 single-bound server là default đơn giản. Không vì global registry biết repo mà tự cấp mọi session quyền đọc tất cả repo.

Không cài skill/plugin global, không chèn hooks, không sửa rules của target. Nếu người dùng có quy trình chuẩn hóa riêng, MCE4D cung cấp context như công cụ bổ sung; không thay thế quy trình đó.

## S14 — Security, privacy và trust boundary

### Threat model

Untrusted: file content/name, symlink/junction, Git config, build manifests, source comments, JSON requests, cursor token, LSP output, malformed text, imported decision records. Trusted có điều kiện: MCE4D binary/dependencies đã kiểm chứng, user-approved root/profile/policy, owner OS account. Root/malware có quyền cùng OS user ngoài phạm vi bảo đảm chống xâm nhập; MUST không tuyên bố mã hóa/sandbox tuyệt đối.

### Controls bắt buộc

Các controls dưới đây áp dụng core index/query/MCP. Explorer loopback/preview có permission bổ sung giới hạn tại S37, không tự nới source/exec/network quyền của core.

| Boundary | Control | Failure |
|---|---|---|
| Root/path | Canonical relative paths + secure handle open/no-follow; containment sau resolve; reject traversal/ADS/device/junction escape | ACCESS_DENIED/PATH_ESCAPE |
| File size/type | Regular text allowlist, byte quotas, no archive extraction/network | Boundary diagnostic |
| Secrets | Deny `.env` thật, private keys, credentials stores, wallet seed/key material, dumps; scan trước persistence/output | SECRET_BLOCKED; không log value |
| Repository instructions | Dữ liệu repo không được tự nâng thành instruction/exec authority | Trích dẫn có provenance, không làm theo embedded prompt |
| Exec | Absolute allowlisted executable + argv/cwd/env validated; no shell; no custom loader/hook | Policy error |
| Network | Không LLM/telemetry/download runtime; dependency loader offline flags; no URL tool | Missing deps → partial |
| MCP scope | Startup binding/allowlist independent registry; validate mỗi call/cursor | ACCESS_DENIED |
| Database | Parameter binding, FKs, schema guard, integrity checks, quotas | Controlled error/recovery |
| LSP | Read/preview methods only; bounded JSON-RPC; reject applyEdit/executeCommand | Provider error |
| Logs | Request ID/IDs/codes/counts; no raw query/source/absolute paths/cursor by default | Redaction tests |

Secret detection không chứng minh bắt được mọi bí mật. Deny-by-path trước đọc; content detection trước lưu blob; file có secret không được lưu bản raw rồi “redact lúc gửi”. Trong mixed source file, R1 exclude raw body và đánh dấu gap thay vì thay bytes âm thầm làm lệch source map. `.env.example` không tự được exempt nếu chứa secret thật. Test fixtures dùng secret giả và assert không có marker trong DB/FTS/log/output.

Captured source trong SQLite là dữ liệu nhạy cảm như repository gốc. Owner-only dir/file permissions trên Unix; Windows kiểm tra ACL tương ứng, không coi chmod portable là đủ. Không bật HTTP listener mặc định. Ngoại lệ có chủ ý: `mce4d explorer` opt-in, chỉ loopback có session auth và kiểm tra origin/host theo S37; MCP/index mặc định vẫn không listener. Không index secret files bằng “force” request MCP.

Không bundle sandbox hứa hẹn trong R1. Offline flags là ứng xử của các tool đã allowlist, không thay thế OS firewall với executable độc hại. Runtime network-negative tests kiểm tra fixture/local dependencies; untrusted binary không được cho chạy chỉ vì tên `gopls`.

Output escaping: JSON encoder; CLI text strip/control-escape ANSI và control chars; không in filename như command. Tool output là untrusted context đối với host; hints/annotations không cấp quyền. Rule excerpt phải ghi origin/trust class; document tự nhận “system prompt” không có hiệu lực.

DoS: limit request/body/intent/path lengths, file size/count, traversal nodes/edges/depth, source bytes, query/LSP runtime, stdout/stderr của child. `context.Context` cancel toàn pipeline, no unbounded recursion/queue/read. Malformed graph/cursor/schema không panic process.

TOCTOU: string-check path rồi mở lại bằng đường dẫn không đủ cho protected read. T005 cần adapter secure open theo OS và adversarial rename/symlink tests. Không xác nhận I07/I08 chỉ bằng unit test string normalization.

## S15 — Resource, recovery, retention và vận hành

Phạm vi mục này là runtime engine và owned index. Build/test/benchmark/generator do AI thực hiện để phát triển MCE4D dùng thêm S51; không lấy runtime quota làm budget build, không suy quyền dọn output từ đặc tả GC.

### Default thiết kế ban đầu

| Setting | Default R1 | Ý nghĩa / không được hiểu sai |
|---|---|---|
| max_file_bytes | 2 MiB | Vượt → metadata_only/gap; không tự đọc hết |
| max_indexed_files | 100000 | Vượt → fail rõ hoặc profile đã explicit nâng |
| max_source_bytes_per_snapshot | 512 MiB | Tránh full repo quá lớn; coverage báo giới hạn |
| max_parallel_index_jobs | 2 mỗi watch/scheduler process | Không là quota toàn máy; các CLI riêng cần report resource pressure |
| max_parse_workers | min(4, số CPU khả dụng), ít nhất 1 | Bounded workers, không goroutine/file không giới hạn |
| max_lsp_sessions | 1 mỗi MCP process | Giới hạn resource, không lock repo toàn máy |
| sqlite_busy_timeout_ms | 5000 | Không retry vô hạn |
| writer_lock_wait_ms | 5000 | Hết → WORKTREE_BUSY với worktree/job metadata |
| query_timeout_ms | 10000 | Bao gồm strict check; quá hạn trả unknown/timeout |
| lsp_startup_timeout_ms | 30000 | Không startup gopls trong MCP handshake |
| rename_timeout_ms | 30000 sau startup | Cancel và child teardown |
| index_timeout_ms | 600000 | Mặc định hữu hạn, user tăng explicit theo workload |
| watch_poll_interval_ms | 2000 | Hint stat; không bảo đảm freshness |
| full_reconcile_interval_ms | 60000 | Hash/inventory background trong process watch được user chạy |
| max_output_bytes | 24576 | S10; không đồng nghĩa 6144 token |
| max_graph_nodes / edges / depth | 2000 / 10000 / 8 | Trả limits và continuations; không gọi full impact |
| keep_generations | 2/profile | Token có thể expire sớm do GC; trả lỗi, không rebase |
| pack_ttl_seconds | 900 | Stateless signed descriptor; không bảo đảm retention |
| max_worktree_cache_bytes | 2 GiB | Chạm ngưỡng → GC owned retired data, nếu vẫn thiếu → fail |
| log_retention | 5 file × 10 MiB/worktree | Không log source/request raw |

Các giá trị là default đề xuất phải kiểm tra T033/T039, không benchmark đã đạt. Limits chỉ tăng qua RuntimeConfig trong hard caps schema/capability đã kiểm chứng; vượt hard cap cần revision/fixtures, không bypass bằng flag. Thay đổi default cần đo và update ADR. `GOMEMLIMIT` nếu dùng là soft Go heap limit, không phải hard RSS limit của toàn cây process. Không hứa giới hạn RAM cứng khi chưa có OS enforcement.

### Failure handling

| Sự cố | Bắt buộc |
|---|---|
| Crash giữa parse/insert | Active generation cũ giữ nguyên; BUILDING trở thành abandoned khi lấy lại OS ownership |
| Crash ngay publish | Transaction quyết định old hoặc new, không half pointer; verify lại state |
| Kill/timeout LSP | Cancel request, terminate process tree trong deadline, đóng handles/pipes; không orphan |
| Disk full | Abort job; không prune source; last published vẫn query nếu DB còn đọc được |
| DB corrupt | Quarantine owned index, giữ catalog/profile; rebuild explicit; không xóa âm thầm |
| Writer busy | Reader vẫn trả old snapshot + freshness; writer timeout rõ |
| Branch/file đổi liên tục | Bounded retry rồi WORKTREE_CHANGED_DURING_INDEX; không lock editor |
| Policy tightening | Invalidate old tokens; không phục vụ forbidden blobs từ generation cũ; purge theo policy an toàn |
| Repo bị xóa/move | Mark unavailable; không remap theo tên/remote; confirm rebind |
| Schema upgrade fail | Restore backup/catalog an toàn hoặc rebuild index; giữ binary tương thích |

### GC/backup/rollback

GC chỉ chạy với writer lock worktree, không catalog lock dài; chỉ derived index, không records.sqlite/durable backups. Chỉ delete generations không active và owned artifacts; mọi profile phải giữ active pointer hợp lệ. Reader đang trong transaction phải có snapshot nhất quán; page/cursor lần sau có thể SNAPSHOT_EXPIRED. Không cần lease hội thoại trong R1.

Delete FTS rows/facts/generation theo FK order hoặc cascade đã test; sau đó xóa blob không còn reference từ mọi generation còn giữ. Không xóa blob chỉ vì không thuộc generation mới nhất. Catalog backup tách cache quota. Validate marker/ID/realpath trước xóa thư mục; dry-run mặc định cho command GC thủ công.

Không phát hành task “dọn ổ toàn máy” hoặc global build lock. Không xóa cache của công cụ khác. State folder thuộc từng worktree, index A thất bại không làm index B bị khóa.

Backup SQLite theo REF14; copy source code không nằm trong trách nhiệm backup MCE4D. Rollback binary cần schema compatibility; derived index schema không tương thích → rebuild riêng sau khi giữ registration/policy. Không down-migrate destructive để làm test xanh.

Physical GC order và privacy residual WAL/FTS/deleted pages theo DB-03/DB-04; xóa row không là secure erase. Records/cursor/policy mutations theo DF15, không được gom vào cache cleanup.

## S16 — Observability

Structured event fields: timestamp UTC, level, request_id/job_id, worktree_id, profile_id, generation_id, phase, duration_ms, outcome, error_code, counts, budget, provider_version. Không source, query text, secret, full local path hoặc pack_token mặc định. Debug log opt-in vẫn redact; không tự upload.

Metric bắt buộc: inventory_files, files_excluded_by_reason, bytes_hashed, cache_hit/noop, files_reparsed, semantic_rebuild_mode, nodes/edges/unresolved, freshness_duration/result, query_latency, output_bytes, required_pending, cursor_errors, lock_wait_ms, busy_count, index_duration, cancellation_latency, child_process_count, disk_bytes, recovery_count, hierarchy_query_scope, partition_cache_hit, invalidation_closure_size, snapshot_analysis_count, live_validation_count, self_index_analyzer_revision.

Nơi lưu: bounded JSONL log + CLI doctor/stats; chưa cần Prometheus/Grafana/HTTP metrics server. Errors có context phase/IDs, không dump SQL binds/source. Instrumentation không giữ lock lâu hơn hoặc khiến operation fail vì log sink đầy; phải báo logging degradation qua stderr sanitized khi phù hợp.

MUST benchmark trả đủ profile/tool versions, fixture revision, filesystem class, OS/CPU/RAM, warm/cold classification và commit của MCE4D. Không trộn data user private vào report public.

## S17 — Chiến lược kiểm thử và bộ nghiệm thu

Fixtures MUST tự tạo từ case dưới đây; `expected.json` ghi binding/edge/source positions/limitations mong đợi. Expected results do kiểm tra độc lập và source language semantics, không dùng chính output indexer làm oracle. Golden update phải review diff, không auto bless.

Unit: canonical hash, byte/line/LSP positions, path, schema, selector, ranking, budget, cursor, status transitions. Integration: SQLite/WAL, Git worktrees, go/packages offline, gopls, child lifecycle. Acceptance: end-to-end CLI/MCP/canary source, target OS. Fault injection: kill/reorder/read/write/disk/network/timeout. Property/fuzz: malformed inputs, cursor, Unicode, graph traversal; bounded resources.

Tổng quát code coverage chỉ là chỉ số hỗ trợ; không ép 100% toàn code. Critical modules paths/publish/freshness/budget/rename/policy MUST có positive + negative + failure + concurrency cases tương ứng. Test pass phải gắn commit/toolchain/platform; NOT_RUN không tính pass.

### Acceptance case catalog


| Case | Mức | Tình huống | Oracle bắt buộc |
|---|---|---|---|
| C001 | P0 | Cùng folder basename/remote, clone khác, hai worktree cùng HEAD có dirty khác | IDs/snapshot/scopes khác đúng; không fallback theo tên |
| C002 | P0 | Hai function/field cùng tên ở package khác | locate trả ambiguity; positional selector resolve đúng |
| C003 | P0 | Nested scopes, variable shadowing, receiver/named result | Defs/Uses không lẫn; rename conflict không text replace |
| C004 | P0 | Closures/lambda có local cùng tên, generic method/embedded fields | Identity/ranges riêng; unsupported cases có diagnostic |
| C005 | P1 | Read, assignment, +=, pointer alias | READS/WRITES chỉ khi chứng minh; alias unresolved không fake data flow |
| C006 | P0 | Direct calls xuyên package + function value callback | Direct edge đúng; callback không được gọi là complete runtime call graph |
| C007 | P0 | Interface có nhiều implementation cùng method name | CALLS_INTERFACE khác MAY_DISPATCH_TO; không gán một callee tùy ý |
| C008 | P0 | Reflection/string lookup/dynamic invocation | Limitations/boundary tồn tại; không kết luận zero impact |
| C009 | P0 | GOOS/GOARCH/tags/CGO khác; cùng repo/profile đổi config | Index/profile phân biệt; alternate build không tự included |
| C010 | P1 | Internal/external test packages và production variants | Không đếm trùng reference; tests không biến thành coverage guarantee |
| C011 | P1 | Generated source, vendor/sparse/submodule/nested repo | Metadata/coverage đúng; không tự generate/traverse/mutate |
| C012 | P0 | Dependency/toolchain missing; offline workspace | Syntax fallback đúng; không network/install; no semantic-complete |
| C013 | P0 | File untracked mới thêm sau index tạo caller mới | Strict validation phát hiện membership change, không chỉ returned files |
| C014 | P0 | Delete/rename/move file hoặc đổi tên symbol | Generation mới không có edge cũ mồ côi; no fuzzy ID reuse |
| C015 | P0 | Thay bytes nhưng giữ mtime/size; Git assume-unchanged | Full hash phát hiện; cache không coi clean là proof |
| C016 | P0 | Switch branch, reset về commit cũ, detached HEAD, repo chưa commit | Freshness/identity đúng theo bytes; HEAD null không crash |
| C017 | P0 | Đổi go.mod/go.work/local replace/exported interface | Invalidation consumers hoặc full rebuild; no stale edge |
| C018 | P0 | Chuỗi mutation random trên fixture, full vs incremental | Facts normalized tương đương; replay deterministic |
| C019 | P0 | Fault injection ở capture/batch/publish/commit | Reader chỉ thấy old hoặc new published generation; FK/index integrity |
| C020 | P0 | Readers liên tục khi writer publish nhiều lần | Mỗi response source/facts cùng generation; no BUILDING exposure |
| C021 | P0 | Index A và B đồng thời; A giữ writer lock | B vẫn tiến triển; reader A phục vụ old snapshot; không global build lock |
| C022 | P0 | Process chết/lock metadata cũ/PID reuse/owner còn sống | Chỉ OS ownership quyết định; không steal bằng TTL |
| C023 | P0 | Source/membership đổi trong scan/parse/validation | Bounded retry hoặc explicit instability; không chứng nhận atomic filesystem |
| C024 | P0 | Traversal/symlink race/junction/UNC/ADS/control chars/invalid UTF-8 path | No root escape/source leak; unknown platform case BLOCKED |
| C025 | P0 | Secret giả trong .env, key file, source mixed-content, .env.example | Marker không xuất hiện DB blobs/FTS/log/MCP; gap preserved |
| C026 | P0 | Repo prompt injection + Git config fsmonitor/custom command | Không exec/nâng quyền; nội dung chỉ là data |
| C027 | P0 | Vietnamese/Lao/emoji, UTF-16 surrogate, CRLF, multibyte positions | Range roundtrip exact; invalid byte offset rejected |
| C028 | P0 | Tiny/large budget, metadata nhiều, source line rất dài | JSON valid trong cap; safety không mất; BUDGET_TOO_SMALL đúng |
| C029 | P0 | Hàng nghìn refs/cycle/traversal limit/required evidence quá lớn | required_pending hoặc unknown + continuation; không complete giả |
| C030 | P0 | Cursor tamper/cross-repo/policy change/TTL/GC | Error đúng; không rebase sang snapshot hiện tại |
| C031 | P1 | Phiên AI mới và context compaction | Không hidden skip vì đã gửi trước; có thể nhận lại source cần thiết |
| C032 | P0 | Rename nhiều occurrences/file và nhiều file | Preview liệt kê mọi edit; source hash before=after |
| C033 | P0 | Rename Go exported field có/không json tag | Phát hiện implicit serialization key risk; preserve constraint không bị bỏ |
| C034 | P0 | LSP yêu cầu applyEdit/executeCommand/create/delete/move file | MCE4D không apply; reject unsupported resource edits |
| C035 | P0 | Timeout/cancel/repeat start-stop LSP/MCP/index | Không goroutine/connection/child leak sau settling window |
| C036 | P0 | Disk full/permission denied/corrupt DB | Không xóa source; old published hoặc controlled recovery |
| C037 | P0 | Schema upgrade fail và chạy binary cũ trên schema mới | Backup/rebuild path đúng; SCHEMA_TOO_NEW không mutate |
| C038 | P0 | GC nhiều profiles/generation dùng chung blobs | Không xóa blob còn reference/active/catalog/source; dry-run chính xác |
| C039 | P0 | Malformed JSON, unknown args, stdout log leak, protocol cancel | Schema/MCP framing pass; bounded output và errors đúng |
| C040 | P1 | CLI help/example và Codex thực tế trên version ghi nhận | Roundtrip tool call; không auto ghi đè config hoặc server khác |
| C041 | P0 | Chạy mọi public tool trên canary repo | Toàn target tree content/config/AGENTS bất biến |
| C042 | P1 | FTS/symbol/adjacency queries trên fixture scale | EXPLAIN QUERY PLAN dùng index dự kiến; bounded traversal/no N+1 bất hợp lý |
| C043 | P1 | Hai agent update tracker; DONE không evidence; dangling ID | Validator fail hoặc merge đúng; không mất hàng/progress giả |
| C044 | P1 | Benchmark có task fail, warm/cold và cached-token reports | Không bỏ failure; report reproducible, baseline không bị giảm khả năng |
| C045 | P0 | TS/SQL/config text match và unknown external boundary trong R1 | Không gọi lexical hit semantic relation; no fabricated contract |
| C046 | P0 | GOPACKAGESDRIVER/GOFLAGS/env poison, binary thay đổi | Policy từ chối/chặn; tool fingerprint thay làm invalidation |
| C047 | P0 | Network monitoring ở runtime với modules local/missing | Không kết nối model/telemetry/download; thiếu dữ liệu báo rõ |
| C048 | P1 | Log error paths, huge diagnostics, secret-bearing filenames | Redacted logs/counts useful; no secrets/stack/source dump |
| C049 | P1 | [R1] Hierarchy overview của module/shared package và hàng nghìn refs | Summary giữ exact expansion scope; grouping không mất edge/hub consumers |
| C050 | P0 | [R1] Partition/reverse dependency/cache negative qua thay đổi export/config/closure | Selective/full normalized facts tương đương hoặc fallback rõ |
| C051 | P0 | [R1] Analysis snapshot cũ, validate live và membership đổi giữa các trang | Không báo current/reviewable để edit trước strict verification; pinned pages không rebase |
| C052 | P0 | [R1] Repo Git chưa commit/spec-only có component chưa có code | Intended facts khác observed; no invented implementation/call edges |
| C053 | P1 | [R1] Cohort M/L, large fanout, memory/disk quota và nhiều worktree | Thời gian/RSS/coverage báo đầy đủ; quá limit fail rõ, không global lock |
| C054 | P0 | [R1] Self-index engine bằng binary revision cũ/đổi provider | Analyzer revision tách target revision; independent oracle và direct-tool fallback |
| C055 | P0 | [R2] TS aliases/reexports/project refs/JS any + malicious config plugin | Binding đúng supported profile; unknown dynamic separate; không execute plugin |
| C056 | P1 | [R2] Frontend route/client/backend route cùng path khác service | Namespace/source links đúng; không nối bằng route string đơn lẻ |
| C057 | P0 | [R2] Python decorators/dynamic imports/monkey patch thiếu type info | Syntax/typed/unknown riêng; không import hoặc execute target |
| C058 | P0 | [R2] Notebook cells/venv/config chưa đủ metadata hoặc chứa side effect | Không chạy cell/startup hook; versions/boundary và no source mutation |
| C059 | P0 | [R2] MySQL dialect/engine, dynamic SQL, procedure/view/trigger/migration | Lineage có evidence, unsupported giữ gap; không kết luận deployed schema |
| C060 | P0 | [R2] OAS/schema/proto ref remote/cycle/version mismatch và handlers chưa có | No network; bounded local resolution; declared != implemented |
| C061 | P0 | [R2] Events same topic khác broker/env, wildcard, retry/ack/replay | Không false bridge/exactly-once claim; boundary còn nguyên |
| C062 | P0 | [R2] State machine/guard/action/hierarchy + proposed/approved/stale rule và false instruction | Machine state scope đúng; guards không execute; chỉ approved constraints; source-change invalidation |
| C063 | P0 | [R2] Cross-repo member stale/denied/missing schema, nhiều namespace | Vector provenance đầy đủ; partial/no leakage; không distributed atomic claim |
| C064 | P0 | [R2] Semantic diff moved/renamed cùng tên, profile/provider đổi | Không force match; provider delta riêng; missing snapshots explicit |
| C065 | P0 | [R2] Trading reserve/retry/partial fill/cancel/ledger workflow fixture | Return evidence/gaps/contract obligations; không bịa money precision/finality |
| C066 | P0 | [R2] Commerce payment webhook duplicate/out-of-order và partial refund | Order/payment/stock states tách, correct known impacted consumers |
| C067 | P0 | [R2] AI prompt/tokenizer/embedding dimension/dataset/eval drift + checkpoint bẫy | Metadata links đúng, no deserialize/load/LLM call, missed-data gaps |
| C068 | P1 | [R2] Architecture allowed-dependency rule, exemption, missing provider scope | Violation witness/path chính xác; incomplete không PASS toàn hệ thống |
| C069 | P1 | [R2] Polyglot multi-domain baseline/full/incremental test cohort | Real vs synthetic riêng; token/failure/recall reports đúng |
| C070 | P0 | [R3] Runtime export chứa PII/secret, unknown service revision, oversized span | Scrub/reject before persistence; no merge lên HEAD tùy ý |
| C071 | P0 | [R3] Sampled async spans/links nhiều retries và thiếu instrumentation | Observed-only semantics; missing trace không chứng minh missing edge |
| C072 | P0 | [R3] Coverage/test report từ revision khác hoặc test pass không assert invariant | Historical/static/executed/asserted tách riêng |
| C073 | P1 | [R3] Test selection qua change global config/schema/shared package | Rationale/gaps; không auto skip required/full suite |
| C074 | P0 | [R3] Unsaved buffer divergence, Unicode positions, save/discard và secrets | Buffer session/version/hash nhất quán, no auto-save or mix with disk |
| C075 | P0 | [R3] Embedding candidate sai binding, tokenizer/index version và policy đổi | Exact resolver bảo vệ; cache invalidated; no secret/model network leak |
| C076 | P0 | [R3] Fact-set publish/GC crash và multiple partition generation | Atomic manifest pointers; no mixed incompatible facts/orphan references |
| C077 | P0 | [R3] IaC/config scripts/secret variables và deployed manifest cũ | Static data only, no provider execution/credentials/live-state fiction |
| C078 | P1 | [R3] XL scale fixture hoặc module vượt profile budget | Bounded memory/disk/time, honest supported envelope và partial labels |
| C079 | P1 | [R3] Language addon unsupported feature/profile hoặc parser error | Capability verified matrix; no advertise scaffold as support |
| C080 | P1 | [R3] Extension API/docs/schema/naming/release labels consistency | Only enabled+verified capabilities exposed; no fake tests/gates |
| C081 | P1 | [U0] Resize hai pane, chọn source/backend/FE node và keyboard focus | Graph luôn trái; right mode đúng; accessible splitter/list; selection không bị mất bởi layout |
| C082 | P0 | [U0] Registry không có index, index partial và graph real fixture | Không sinh demo graph/UI; empty/partial/error có provenance; source đúng raw bytes |
| C083 | P0 | [U0] HTTP forged Host/Origin, CSRF, expired token, iframe thử đọc source | Không leak source/registry, không arbitrary URL proxy/shell; loopback only và auth hết hạn đúng |
| C084 | P0 | [U1] JSX native button có handler local: Inspect click | Anchor đúng binding/file/range; graph trái selected; counter action/fetch/form submit do click =0 |
| C085 | P0 | [U1] Chọn block/component lồng nhiều tầng | Phân biệt element/render-site/definition; chọn parent có evidence; không gộp Button common thành business owner |
| C086 | P0 | [U1] 100 instances cùng component, chọn instance thứ 37 | Instance selection giữ riêng; graph definition chung có instance count; không lưu business key/props |
| C087 | P0 | [U1] Graph→preview→graph liên tục, messages duplicate/out-of-order | Một selection transaction, không echo loop; request cũ không ghi đè selection mới; view ổn định |
| C088 | P0 | [U1] Graph node chưa mount, virtualized offscreen, route khác, permission khác | Trả not_rendered/not_visible/ambiguous; không auto-click/navigate/login/replay hoặc tạo instance giả |
| C089 | P0 | [U1] HMR source đổi không commit, reorder source, remount/page reload | Build+anchor epoch đổi; stale mapping blocked; còn graph snapshot cũ chỉ historical |
| C090 | P0 | [U1] Inspect pointerdown/up/click/dblclick/context menu/touch/Enter/Space/submit và frame focus | Không chuyển input target thuộc supported shield contract; test business counter=0; không gọi synthetic DOM events |
| C091 | P0 | [U1] Mount effect/polling/WS background và preview trỏ môi trường không được phép | Không gắn nhãn runtime read-only; attach bị chặn khi policy không đạt; chỉ fixture isolated dùng network test |
| C092 | P0 | [U1] Sai origin/source/nonce, message quá lớn, replay, iframe navigation | Bridge reject; session/bearer không leak; navigation resets pairing; không eval HTML/message |
| C093 | P0 | [U1] Fragments/multiple DOM roots/portals/HOC/third-party không forward attributes | Phần supported mapping đúng; unsupported grouping có reason; không đọc private React fiber hoặc đoán theo label |
| C094 | P0 | [U1] CSS transform/zoom/scroll/resize/Shadow DOM closed/canvas | Hit test tọa độ đúng trong supported matrix; unsupported elements không làm false exact mapping |
| C095 | P0 | [U1] Production bundle/SSR hydration fixture và target source canary | SDK/data attrs/maps private không leak release; development transform bảo toàn semantics đã test; target tree không auto sửa |
| C096 | P0 | [U1] Manual mapping proposed/approved/revoked, source/profile thay đổi | Lifecycle/approval đúng; nguồn human riêng, không nâng thành type-resolved/observed runtime |
| C097 | P0 | [U1] Select UI rồi yêu cầu context cho AI, snapshot GC/policy tightening | Pack same source/profile; pending gaps preserved; stale/expired explicit; không auto LLM/API spend |
| C098 | P1 | [U1] Repo thực được cho phép với build dev và matching index | Report click→source accuracy/bidirectional coverage/latency, artifact source-based; thiếu repo thật ghi NOT_RUN |
| C099 | P0 | [U2] Click backend route/handler trong graph và click step flow ngược lại | Right flow API/method/source đúng; left graph selection đồng bộ; panel không tự gửi API request |
| C100 | P0 | [U2] Hai service cùng POST /orders, proxy prefix và missing config | Contract match theo scope, không theo path đơn lẻ; unknown adapter/prefix trả candidate/unresolved |
| C101 | P0 | [U2] Conditional handler/retry/early return/parallel goroutine | Static flow không giả một chuỗi thực thi duy nhất; branch/call/candidate labels và gap đúng |
| C102 | P0 | [U2] database/sql literals BEGIN→INSERT→UPDATE→ROLLBACK và dynamic SQL/ORM | Đúng table/op trong supported subset; static chưa phải persisted; unknown SQL giữ boundary |
| C103 | P0 | [U2] Frontend→API→BE→SQL và click bảng tìm upstream UI | Đúng join IDs/source vectors; one-to-many danh mục; partial instrumentation/unsupported không biến no impact |
| C104 | P0 | [U2] Queue/outbox/late consumer/transaction boundary | Không nối sync theo timestamp; ack khác commit, code-observed publish khác delivery; no broker/DB execute |
| C105 | P1 | [U2] 50k indexed nodes nhưng viewport giới hạn + hover 60Hz, cancel navigation | Response/render trong cap; bounded RPC/DOM listeners; stale responses ignored; index không chạy lại mỗi click |
| C106 | P0 | [U2] Cross-repo member stale/denied + real FE/Go/SQL flow release | Không leak member/source; composite mapping partial; real vs synthetic label và source-mutation=0 |
| C107 | P0 | [U3] Explicit interaction→fetch traceparent→BE spans có cùng build provenance | Observed overlay chỉ các spans có causality; không biến every function thành observed |
| C108 | P0 | [U3] Hai click gần nhau, concurrent fetch/cache hit/retry/background request | Không dùng latestClick/time heuristic như proof; unbound request giữ riêng, retry attempts không gộp |
| C109 | P0 | [U3] SQL span success nhưng transaction rollback hoặc commit thiếu span | Persisted status unknown/rolled_back phù hợp; không khẳng định saved từ SQL success |
| C110 | P0 | [U3] Cross-origin CORS không allow trace header, CSP frame refusal/COOP opener missing | Không bypass browser policy; explicit unsupported/fallback static/import; không gửi bearer/source tới target |
| C111 | P0 | [U3] Trace secrets/request body/SQL literals/DOM values và corrupted export | Redact trước persistence, no raw user data; bounded importer; no trace replay/target execution |
| C112 | P1 | [U3] Session detach/abort/reload nhiều lần, imported build cũ và source mới | Không observer/child/session leak; observed legacy stays historical; report correctness/latency với version evidence |
| C113 | P0 | [G0] Actor tự đổi architecture/unknown prerequisite hoặc tạo feature trước seal | Ready validator reject; T001–T108 administrative exceptions explicit; no guessed API |
| C114 | P0 | [G0] API scalar/vector drift, duplicate JSON/unknown fields/op-data mismatch | One schema authority; positive/negative fixtures và operation-bound response |
| C115 | P0 | [G0/R1] Physical catalog/index/records migration và FK connection settings | Fresh install correct; every pooled connection FK1; target driver proof tại T106 |
| C116 | P0 | [G0/R1] Child fact tham chiếu symbol của generation khác | Composite FK hoặc authoritative typed validator reject; no mixed evidence |
| C117 | P0 | [G0/R1] Publish BUILDING/profile mismatch/cancel/rollback | Only legal READY pointer; old active survives failed publication; exact owner CAS |
| C118 | P0 | [G0/R1] Sửa published facts hoặc append relation vào READY | DDL guard/application tests reject; no quiet mutation |
| C119 | P0 | [G0/R1] Scope query FTS và GC old generation | Search never leaks generation; triggers sync documents/FTS; active protected |
| C120 | P0 | [R2/U1] Concurrent record approvals same expected_version | One commit, other conflict; head/history/epoch/receipt atomic; no caller-forged actor |
| C121 | P0 | [R2/U1] Retry mutation ID same body và khác body/principal | Same returns original result, mismatch rejects, no duplicate version |
| C122 | P0 | [R1/R2] Policy/record/group changes mid-query or between pages | Postcheck error/retry bounded; old pack invalid, no stale approved constraint |
| C123 | P0 | [U1] Build map same bytes/new index generation; source changed/HMR lazy map | Producer map ID stable by content, new binding explicit; mismatch no exact graph/source |
| C124 | P0 | [U1] SDK reports generation/SymbolRef hoặc API giả instance list | Wire rejects forbidden fields; server anchors vs browser instances separate |
| C125 | P0 | [U1] READY spoofed/stale nonce hoặc no imported build manifest | No bound/ready; no source capability before handshake+engine binding |
| C126 | P0 | [U1] Interact MODE_ACK chậm, sau reload/disarm, old command | Shield stays/returns; server ack CAS deadline; old ack cannot arm |
| C127 | P0 | [U1] Flow/preview selection responses out of order | Current seq+scope only; no echo loop, no reroot from render |
| C128 | P0 | [G0/R1] MCP text+structured duplication và near-cap envelope | Actual emitted payload counted; valid JSON under limit; no dropping required safety |
| C129 | P0 | [G0/R1] Go Load type identities và source ParseFile disagreement | No object identity mixing across loads; source mismatch explicit partial/error |
| C130 | P0 | [R1/U1] GC/quarantine index với approved mappings/records | Records/catalog/source unchanged; historical bindings expire clearly |
| C131 | P0 | [G0/R1] Source range UTF8/CRLF/empty/no trailing newline | Byte roundtrip correct; invalid boundary/range rejected, display derived |
| C132 | P0 | [G0/R1] Missing API version, unsupported platform/binary permission | No guessing/download/false support; scoped BLOCKED with evidence |
| C133 | P0 | [G0/CI] Task schedule missing/duplicate/backward dependency/gate | Validator reject; canonical order covers108 tasks exactly |
| C134 | P0 | [G0/CI] Cross-layer SQL/HTTP/provider calls or mutable global current_repo | Import-boundary/regression tests fail; composition root only wiring |
| C135 | P0 | [G0/CI] Document audit PASS being used as product evidence | Product gates remain NOT_RUN until actual code+tests+environment revision evidence |
| C136 | P0 | [R1/U1] COMMIT result uncertain, process killed, session cancellation | Resolve by job/receipt ID, bounded cleanup, no duplicate publish/permission or source loss |
| C137 | P0 | [G0/CI] Main spec + docs source aliases, missing/version mismatch/duplicate/line-ending-only | Một spec/ledger; đúng nguồn và trạng thái, không auto replace/delete/nâng bản hoặc false installed |
| C138 | P0 | [G0/CI] Bootstrap prompt hiện diện khi implement; docs_only được giao nhưng task coding đã có trong tracker | Mode từ assignment; không tự chạy prompt/đổi mode/start coding; chưa có quyền không DONE feature |
| C139 | P1 | [G0/CI] Tăng dependency/contract ngoài scope hoặc muốn xóa guard để đơn giản; local helper không đổi behavior | Gate đúng phạm vi; giữ baseline/guard, không xin duyệt thừa cho helper hoặc chia đổi contract không nhất quán |
| C140 | P0 | [G0/Build] Lệnh tên check/dry-run gọi child --write/download; cwd/argv/output chưa rõ | Kiểm tác động/quyền, reject placeholder/unsafe command; không dùng tên làm read-only proof |
| C141 | P0 | [G0/Build] Low-space, unreadable measurement, negative/overflow/invalid budget và đúng boundary | Fail closed đúng volume; fake meter/disposable fixtures nhỏ, không làm đầy disk thật |
| C142 | P0 | [G0/Build] Repo/temp/cache khác volume hoặc nhiều paths cùng volume; job khác đã ghi một phần budget | Gộp backing volume; remaining reservation không double-count; unavailable destination chặn workload |
| C143 | P0 | [R1/Build] Hai job output riêng/cùng output, concurrent admission và unknown external writer | Không global mutex; isolation hoặc lock đúng resource, aggregate chỉ scope đã kiểm chứng; uncertainty rõ |
| C144 | P0 | [G0/R1/Build] Growth/deadline/cancel, command fail rồi cleanup fail, success rồi finalization fail | Owned writers dừng trước cleanup; giữ exit/result gốc và lỗi phụ, không false success hoặc orphan trong scope đã test |
| C145 | P0 | [R1/Build] Cleanup gặp fixtures/goldens/deliverable/records/source hoặc path link/replace/unexpected file | Refuse xóa ngoài proven-disposable+authority; marker/gitignore không đủ; platform unsupported báo rõ |
| C146 | P0 | [G0/R1/Build] Dry-run, uncatchable crash leftover, cleanup thiếu action-time approval | Report-only không tạo/chỉnh/xóa state hoặc chạy workload; crash không fake cleanup/TTL ownership |
| C147 | P0 | [R1/UI/Integrate] Input/lockfile/contract/rules đổi sau test hoặc merge; mutation bỏ critical guard | Evidence invalidated theo tác động, test bản kết hợp; mutation non-equivalent phải bị phát hiện, không auto bless expected |
| C148 | P0 | [G0/Release] Documented policy/fixture PASS/absent reviewer được khai báo thành enforcement hoặc production-ready | Bốn trạng thái đúng scope; required unknown chặn claim, không fake independent review/product DONE/deploy permission |
| C149 | P0 | [v1.5.0] GC generation có module cha-con 3 tầng, source/package/symbol/FTS/domain/UI binding | Leaf-before-parent→files→packages; generation khác/active/shared blob được giữ; no FK disable |
| C150 | P0 | [v1.5.0] Inject fault/cancel ở mỗi stage GC trước commit | Rollback cả retired state/deletions; independent profile và records nguyên; rerun idempotent |
| C151 | P0 | [v1.5.0] Module cycle và report-only GC | Publish reject cycle; gặp corrupt cycle rollback không xóa partial; dry-run không mutate |
| C152 | P0 | [v1.5.0] Cùng producer/source/profile/policy với hai resolver digest | Binding IDs khác và cả hai insert được; same natural tuple không được tạo binding thứ ba |
| C153 | P0 | [v1.5.0] Reimport map/binding same ID nhưng changed content và wrong source range | Idempotent exact bytes; mismatch rejected; no replace/update old binding |
| C154 | P0 | [v1.5.0] POST preview/status nhiều lần, session TTL và permission rev không đổi | Read không renew/bump/approve; payload không nonce/bearer; actor khác denied |
| C155 | P0 | [v1.5.0] API server sống nhưng SDK mất PONG hoặc wrong origin/replayed reply | SDK riêng bị disarm; server status không substitute target liveness; no event echo |
| C156 | P0 | [v1.5.0] Permission expiry/revoke, browser hidden/resume, clock rollback, MODE_ACK trễ | Shield/disarm; no TTL extension; rearm explicit; real browser gate riêng reference model |
| C157 | P0 | [v1.5.0] Permission put/revoke CAS, sai version/actor/attestation và CLI-only routing | Immutable versions + heads, current version checked; invalid refused, no MCP escalation |
| C158 | P0 | [v1.5.0] Một component có101 hoặc hơn render sites source và nhiều targets | MappingMatch paging giữ đúng cặp anchor/target, enumerate đủ không duplicate, no false unique first page |
| C159 | P0 | [v1.5.0] Rename1025 edits/đa file, CLI cross-process continuation | LSP computation một lần, immutable spool/result digest, pages đủ; no apply trước full set + validation |
| C160 | P0 | [v1.5.0] Mapping/rename cursor đổi filters/scope/binding/expiry/evicted spool | Typed error không fallback/recompute; six MCP tools unchanged; nguồn mới không ghép page cũ |
| C161 | P0 | [v1.5.0] Source mandatory có một dòng dài, tiếng Lào/emoji/CRLF vượt ngân sách | Fragments SourceRef exact liên tiếp; concatenated bytes=parent; invalid UTF8 boundary reject |
| C162 | P0 | [v1.5.0] Budget minimum/metadata lớn/result cap/empty-progress continuation | Wire cap kể repeated text, required_pending đúng; error có hành động không endless cursor |
| C163 | P0 | [v1.5.0] Hai member cùng symbol_id, một worktree xuất hiện ở hai services | RefSelector+service+pinned vector chọn đúng; unqualified group selector reject/ambiguous |
| C164 | P0 | [v1.5.0] Group member chưa index/missing/denied và tất cả unavailable | No dummy generation/hash, availability explicit; denied identities theo policy, no source leak |
| C165 | P0 | [v1.5.0] Active generation đổi/new member index giữa graph selection và context pages | Pinned source giữ nguyên hoặc expired; không tự upgrade missing member; live query mới explicit |
| C166 | P0 | [v1.5.0] Pinned vector digest sai/incarnation cũ/group revision mismatch | No cache hit or authorized source from mismatched scope; fail closed |
| C167 | P1 | [v1.5.0] RuntimeConfig unknown field/null/ranges/defaults/sentinel/permission conflict | Reject invalid; auto workers resolved bounded; runtime cannot widen policy/wire cap; no schema defaults drift |
| C168 | P0 | [v1.5.0] Hai setter config cùng expected digest, process đang dùng config cũ | CAS một writer thắng, atomic file; process immutable tới restart; validate không write |
| C169 | P0 | [v1.5.0] Group list >1 page, put/remove/recreate, stale expected revision | Stable listing/version; tombstone no ID/revision reset; mutation không xóa member source |
| C170 | P0 | [v1.5.0] Record list/read historical source đã GC hoặc changed policy | Metadata durable, needs_review/snapshot_expired; no imaginary source hoặc auto reapproval |
| C171 | P0 | [v1.5.0] Nhiều process/pool reader và maintenance scoped A, normal operation B | Exclusive gate không vào khi còn shared handles; bounded BUSY; B tiến triển; real OS matrix bắt buộc |
| C172 | P0 | [v1.5.0] Crash tại PREPARED/REPLACING/VERIFYING restore journal | Không serve half replaced stores; report-only không mutate; authorized recovery verified |
| C173 | P0 | [v1.5.0] Restore catalog/worktree hoặc rebuild index, numeric epochs trùng trước đó | New incarnation invalidates old tokens/results; records giữ nguyên với index rebuild; no old secret key restore |
| C174 | P0 | [v1.5.0] MCE-C14N1 Unicode/null/missing/ints/key order/array order/EOL và Go/Node | Exact encoded bytes + SHA256 trùng vectors; reject surrogate/fraction/overflow/duplicate |
| C175 | P0 | [v1.5.0] Cùng source capture parse-error hay parser kết quả khác | Capture snapshot_digest không đổi bởi diagnostic; files result status đúng; ProviderDone matches original |
| C176 | P0 | [v1.5.0] Path validator portable/Go regexp, API fixtures mới và closed schema | Traversal/invalid UTF8/unknown fields rejected; no guessed root hoặc cross-transport CLI action |
| C177 | P0 | [v1.5.0] Map watcher thấy tmp/partial/oversize/symlink/same ID different body | Chỉ committed immutable artifact đúng hash được import; no network/target exec/source writes |
| C178 | P0 | [v1.5.0] HMR storm/lazy source map và index writer busy | Queue bounded, latest notification coalesced; shield+pending tới đúng map/source; no auto Interact |
| C179 | P1 | [v1.5.0] Index rapid generations, pack expiry, long index và mapping review | Record actual expiry/repeated reads/lock wait; no unlimited pin hoặc false speed claim |
| C180 | P0 | [v1.5.0] Toàn bộ audit closure và implementation chưa bắt đầu | Schema/DDL/registry/task/cards/cases đồng bộ; document PASS không đổi tasks DONE hoặc product NOT_RUN |
| C181 | P0 | [workflow] 9/10/11 task DONE theo batch, phiên đổi và gate chuyển | Chưa10 không commit thường; đủ10 chặn task11 đến local checkpoint; không reset count vì phiên/gate |
| C182 | P0 | [workflow] 10 task đầu theo S47 khác T001–T010; repo đã có lịch sử | B001 đúng thứ tự; không tự gom theo ID, không rewrite history hoặc giả task đã checkpoint |
| C183 | P0 | [workflow] Task reopen/VERIFY/BLOCKED/DEFERRED/cancel và bugfix task đã commit | Chỉ DONE mới duy nhất được đếm; không đếm trùng/fake task; prerequisite/gate giữ nguyên |
| C184 | P0 | [workflow] File mới/ignored bắt buộc, deletion/mode, lockfile/schema/docs bị thiếu stage | Chặn incomplete snapshot; positive stage đầy đủ phục hồi được; không dựa commit -a |
| C185 | P0 | [workflow] Dirty baseline ngoài scope, secret, hook sửa content hoặc thiếu Git identity | Không stage hộ/leak/bypass hook; chặn hoặc cô lập đúng quyền, không reset/clean/stash |
| C186 | P0 | [workflow] Các task pass riêng nhưng combined tree fail hoặc đổi sau stage | Chặn commit trước verify; tree mismatch sau commit không tag/push checkpoint giả; recheck đúng input |
| C187 | P0 | [workflow] Mất phản hồi commit/tag, retry, tag collision và phiên resume | Đọc OID/tree/trailers; không duplicate/empty/amend/retag; reconcile prepared record |
| C188 | P0 | [workflow] Checkout mốc vào workspace sạch, dependency ngoài Git thiếu hoặc DB schema mới | Source/tests vật tư đủ trong phạm vi; missing material không claim restore; không sửa DB/working tree gốc |
| C189 | P0 | [workflow] Chưa remote/push fail/remote ref đã cập nhật/remote diverged | Không bịa đã push; retry đúng ref không commit lại/force; backup status riêng local |
| C190 | P1 | [workflow] Kết thúc thật 108 hoặc52 task so với tạm ngắt sau 3 task | Tail8/tail2 chỉ khi scope đóng; tạm ngắt không tail; không bật optional để đủ10 |
| C191 | P0 | [workflow] Chưa10, snapshot interim có untracked/deleted files hoặc thiếu quyền backup | Snapshot đủ metadata/files nếu được phép; không WIP commit; thiếu backup báo rõ, không cloud/delete ngầm |
| C192 | P0 | [workflow] Metadata own commit/tree hash và postcommit evidence; source policy/schema parity | Không self-hash/extra metadata commit; Git receipt xác minh đúng lớp;108 task/order và canonical contracts giữ nguyên |

C149–C180 thuộc v1.5.0, chia theo task/core/UI/R2 ở S52; foundation chỉ contract/SQL/reference proof, platform/browser/integration phải chạy ở gate thực. Không lấy model proof thay real E2E.

Case applicability: C001–C054 thuộc R1; C055–C069 thuộc R2; C070–C080 thuộc R3; C081–C112 thuộc Explorer U0–U3; C113–C136 theo subcase G0/core/R2/UI ghi trong case. C137–C148 là policy/build-process cases v1.4.0 theo task/subcase S51; synthetic contract proof khác kiểm chứng OS/tooling thực. Foundation chỉ contract proof, không giả đã chạy UI E2E. Case ngoài release đang nghiệm thu là NOT_APPLICABLE_RELEASE, không PASS; khi mở capability phải chạy cả regression R1 có liên quan.

### Verification commands của chính MCE4D

Sau khi bootstrap/code tồn tại, lệnh baseline là `gofmt` cho file đổi, `go test ./...`, `go vet ./...`, `go build ./cmd/mce4d`. Chạy `go test -race ./...` trên platform/toolchain hỗ trợ và ghi yêu cầu cgo/toolchain liên quan nếu có; không tuyên bố pass khi môi trường không chạy được. Fuzz dùng target đã implement, hữu hạn thời gian và không network.

T003 xây `go run ./tools/spec_check` kiểm tra ID/task deps/gate/evidence/tracker. Đây là command dự kiến sau implementation, không command đã chạy khi soạn tài liệu. Không chạy test/build của target repo từ MCP để thỏa verification của MCE4D.

C040 cần client Codex đã cài và root local thật được phép. Nếu môi trường chỉ có MCP test client thì ghi MCP conformance PASS nhưng Codex integration NOT_RUN, không đổi thành “đã hỗ trợ thực tế” toàn host. Release platform chưa test phải loại khỏi supported matrix.

## S18 — Benchmark và tiêu chí tiết kiệm token

### So sánh công bằng

A = cùng AI host/model với công cụ tìm kiếm/đọc/LSP hiện có, không MCE4D. B = A + MCE4D map/query trước tối ưu context. C = A + MCE4D context pack tối ưu. Không dùng source sản phẩm khác làm benchmark oracle; không cần chạy GitNexus/Graphify.

Mỗi task dùng checkout/worktree độc lập cùng starting revision và dependency state. Cùng model/version/reasoning settings/prompt/tool permission/budget hoàn thành; cho baseline dùng công cụ semantic nó vốn có, không cố làm baseline kém. Xáo trộn thứ tự, tối thiểu 3 repeat nếu khả thi; ghi seed khi host hỗ trợ, không giả model deterministic.

Suite khởi đầu: ít nhất 30 task trên ít nhất 3 repo Go đại diện được người dùng cho phép hoặc fixtures tự tạo, gồm local rename, interface/field/type change, behavior bug, contract preservation, dirty worktree, missing dependency, large fanout. Ghi rõ suite synthetic hay real; chưa có user repo thì synthetic gate không được gọi production benchmark.

Không có gọi LLM tự động trong R1 engine. Harness chuẩn bị input/task/evidence và import usage do host cung cấp. Không tự dùng API key/spend tiền để đánh giá; chỉ chạy host/LLM khi được phép. Thiếu token telemetry → báo proxy bytes/reads, không gọi đó là số token tiết kiệm thật.

### Chỉ số

| Metric | Công thức / cách báo |
|---|---|
| Task success | Correct implementation + required tests + no unrelated changes; reviewer/oracle independent |
| Critical omissions | Số reference/contract/invariant bắt buộc bị bỏ qua trong scope tested |
| False edits | Số thay đổi nhầm binding/scope hoặc vượt constraints |
| Total input/output tokens | Từ host usage, gồm tool schemas/context/cached input/thử lại; phân loại cached vs uncached nếu có |
| Tokens per accepted task | Tổng tokens tất cả attempts/failures trong cohort / số task accepted; kèm completion rate, không loại failure |
| Per-task savings | 1 - C_tokens/A_tokens cho task comparable; median và distribution, không chỉ best case |
| Context proxy | Tool payload bytes/lines/files/read calls; báo riêng với token measurements |
| Index cost | Cold/full/no-op/change update time, bytes hashed, storage, peak process-tree memory |
| Query cost | p50/p95, freshness time riêng, warm/cold, output pending flags |
| Lifetime amortization | Index/update CPU/disk + các nhiệm vụ, không gộp local reads thành LLM token |

### Gates hiệu quả

Correctness gate: zero known critical omissions/false edits trên acceptance suite được định nghĩa; không có P0 failure; production real-repo success không thấp hơn baseline trong mẫu đã chạy. Đây là điều kiện hữu hạn trên suite, không chứng minh mọi chương trình đều đúng.

Mục tiêu thử nghiệm ban đầu: median total-input-token reduction >=25% cho cohort phù hợp, không tăng false edits/completion failures. Không đạt → tối ưu slicing hoặc giữ optional adoption; không hạ correctness/gaps để đạt số. Sample nhỏ không dùng để tuyên bố ý nghĩa thống kê mạnh.

Performance targets tham khảo cho medium fixture Go <=1000 files và <=20 MiB source trên máy benchmark đã ghi cấu hình: warm locate p95<=500 ms, graph-only pack p95<=1500 ms, full freshness validation p95<=5000 ms. Full index <=120s; update báo content-only/full/selective, không áp một mốc giả cho mọi build/dependency. Mọi số là target chưa đo; T039 phải bổ sung result cùng hardware/filesystem. Nếu phần correctness đạt nhưng performance không đạt, ghi FAIL target hoặc ADR điều chỉnh có dữ liệu, không đổi tên phép đo.

Chọn corpus trước khi đo: T038 ghi nguồn và quyền cho từng repo/fixture, revision hoặc content digest, ngôn ngữ/profile, số file/bytes, đặc điểm dirty/worktree/dependencies và lý do đại diện; định nghĩa oracle/required references và task trước khi xem kết quả engine. Repo thật và fixture được báo riêng; không đưa source hoặc token/credential vào báo cáo dùng chung. Bộ đo R1 vẫn theo 30 task/3 repo hoặc fixtures ở trên; nếu không đủ quyền, host usage hoặc điều kiện, giữ phần tương ứng NOT_RUN và áp dụng đúng nhãn S20. T039 báo sample/completion/failures và baseline; không biến proxy bytes thành token hay kết quả fixture thành real-repo savings.

## S19 — Bảng triển khai duy nhất

### Protocol trạng thái

Status enum: TODO, IN_PROGRESS, BLOCKED, VERIFY, DONE, DEFERRED, CANCELLED. Track U0–U2 là scope được yêu cầu ở v1.2.0; U3 optional. R2 prerequisites được kéo vào đường ưu tiên Explorer theo S39 sau G5, không chờ đóng toàn G6. DONE chỉ sau code/test/evidence + acceptance tương ứng; tài liệu thiết kế không làm task implementation DONE. VERIFY không tính hoàn thành. DEFERRED R2 không tính mẫu số R1. CANCELLED cần lý do/ADR và không tự giảm mẫu số baseline để tăng progress.

Luồng: TODO → IN_PROGRESS → VERIFY → DONE. IN_PROGRESS/VERIFY → BLOCKED khi thiếu prerequisite; BLOCKED → TODO/IN_PROGRESS sau resolve có evidence. DONE → TODO khi regression hoặc contract thay đổi; lưu lý do trong changelog. Chỉ claim task khi dependencies DONE và gate trước đã PASS. G1 có dependencies không cùng thứ tự số, nên chọn topological ready task, không chỉ T008 trước T012.

Một agent chỉ một task IN_PROGRESS mặc định. Nhiều agent được làm task độc lập khi paths ownership không chồng; declaration `owner/task/paths/base_commit` trước sửa. File đặc tả do coordinator merge ngắn theo hash/revision, không giữ khóa file trong build/test. Không áp dụng mutex này lên mọi project của máy.

Mỗi cập nhật tracker phải read current revision, patch đúng hàng, kiểm tra không mất cập nhật khác; conflict → merge có review, không last-write-wins. Log build/test và artifacts có thể ngoài file, nhưng ID/hash/kết quả/evidence pointers phải trong S21. Task DONE được ghi ngay với tree/diff evidence, KHÔNG đồng nghĩa phải commit ngay. S53 gom 10 task DONE thành một commit; checkpoint/restore/push có metadata riêng tại S21, không thay task status. Trước task thứ 11 phải đóng local checkpoint của nhóm đầy theo quyền; không bỏ gate để đủ số.

### Progress ban đầu — derived từ các hàng task

- R1: 52 task; DONE=20; IN_PROGRESS=0; BLOCKED=0; VERIFY=0; TODO=32; completion=20/52≈38.5%.
- R2: 20 task DEFERRED; roadmap đa stack, không thuộc mẫu số R1.
- R3: 12 task DEFERRED; mở capability theo gate.
- Explorer U0: 4 TODO; U1: 10 TODO; U2: 6 TODO; U3: 4 DEFERRED. TODO không đồng nghĩa prerequisites đã đủ hoặc permission runtime đã có.
- Toàn roadmap: 108 tasks; 52 TODO + 20 DONE + 36 DEFERRED; implementation DONE=20. R1/R2/R3/U0/U1/U2/U3 tính riêng; không gọi task-count là % effort.
- Gate hiện tại: G0 PASS; G1 PASS phạm vi Linux filesystem/fixture theo S21; G2 NOT_RUN. B001 checkpoint local PASS; B002 đủ 10, phải checkpoint trước T015. Product build/tests/benchmarks: NOT_RUN.

Tỷ lệ trên là task-count completion, không phải % effort hoặc % độ đúng của sản phẩm. Validator MUST tính lại khi cập nhật task, không sửa phần trăm thủ công.

### Tracker

Cột Cases là truy vết tới oracle nghiệm thu, không có nghĩa một task nền tảng phải hoàn thành toàn bộ E2E case thuộc feature chưa được triển khai. Task phải chứng minh các subcase thuộc chính deliverable của nó, ghi scope evidence; phần E2E chưa chạy giữ NOT_RUN. T034 và G4 tổng hợp oracle đầy đủ. Không dùng quy tắc này để bỏ integration test nằm ngay trong phạm vi task.

| Task | Gate | Dependencies | Deliverable / định nghĩa hoàn thành | Req | Cases | Status | Owner | Evidence |
|---|---|---|---|---|---|---|---|---|
| T001 | G0 | NONE | Site survey + safe private module bootstrap only; preserve existing workspace; no feature code trước T108. Resolve source-policy docs/mode theo S50; ghi actual path/hash, không sửa bản gốc. Khảo sát batch baseline/history/quyền remote và S53; không commit task khởi tạo riêng. | R001,R105,R106,R125,R129 | C041,C137,C138,C181,C182,C185,C189,C191 | DONE | codex | E001,E002,E003 |
| T002 | G0 | T001 | Pin exact toolchain/dependencies, license/API ledger và smoke fixtures; không tự chọn technology ngoài S41. Áp command preflight S51 trước smoke, output volumes/budgets từ bounded evidence. | R001,R036,R108,R109 | C039,C042,C046,C140,C141,C142 | DONE | codex | E004 |
| T003 | G0 | T001 | Stdlib spec_check: IDs/DAG/counts/plan/contract authority/evidence rules; tests chống fake DONE và backward task order. Kiểm policy ledger/mode/claim metadata, không nhận docs PASS là guard enforced. Validator kiểm audit closure/registry transport/default/hash vector counts; no fake evidence. Kiểm batch 10 DONE/trailers/tail/receipt theo S53; validator không tự commit. | R030,R093,R102,R103,R105,R106,R112,R124,R125,R128 | C043,C133,C135,C137,C138,C148,C180,C181,C182,C183,C187,C190,C192 | DONE | codex | E005,E020,E098,E099 |
| T004 | G1 | T005,T106 | Catalog use cases trên schema frozen: register/profile/rebind/remove; transactional revisions/identity; no source mutation. Runtime config/policy/profile/group metadata foundations và incarnation initialization; controller CLI wiring ở đúng task sau. | R002,R118,R119 | C001,C016,C167,C168,C157,C169,C170 | DONE | codex | E127,E128,E129,E130,E131 |
| T005 | G1 | T108 | Secure path open, deny policy, secret pre-persistence controls; OS adapters baseline. Guard containment cũng cho build-output cleanup khi áp dụng, không nới quyền. | R013,R014,R110 | C024,C025,C026,C145 | DONE | codex | E102,E103,E104,E105,E106,E107,E108,E109,E110,E111,E112,E113,E114,E115,E116,E117,E118,E119,E120,E121,E122,E123,E124,E125,E126 |
| T006 | G1 | T005 | Captured bytes/hash/UTF-8 ranges/manifest encoding deterministic. C14N1/capture-vs-parse input contract; shared vectors. | R005,R121 | C015,C023,C027,C174,C175,C176 | DONE | codex | E132,E133,E134,E135,E136,E137 |
| T007 | G1 | T004,T006,T106 | Implement index store adapters/migrations từ S44, same-generation FKs/FTS/capture guards, not redesign schema. Store incarnation/lifecycle lease port được wired với T010 trước public access; no hidden independent pool. | R004,R010,R120 | C019,C037,C042,C171,C172,C173 | DONE | codex | E138,E139 |
| T008 | G1 | T007,T012,T010 | Staging + atomic publish + active profile pointer + consistent reader. | R007,R092,R093 | C019,C020,C117,C136 | DONE | codex | E150,E151,E152,E153,E154,E155,E156,E157 |
| T009 | G1 | T008,T010 | Crash/cancel recovery generation BUILDING; last published vẫn đọc được. | R007 | C019,C022,C036 | DONE | codex | E159,E160 |
| T010 | G1 | T004,T007 | OS writer locks per worktree; bounded wait; không TTL steal/global build lock. Shared/exclusive lifecycle pool lease theo DB-05, maintenance scoped, không nâng lock tại chỗ. | R003,R120 | C021,C022,C171,C172,C173 | DONE | codex | E140,E141 |
| T011 | G1 | T008,T010,T012 | Multi-process concurrency test: A/B độc lập, reader/writer và profiles. | R003 | C001,C020,C021 | DONE | codex | E162,E163 |
| T012 | G1 | T004,T005,T006 | Git inventory tracked/untracked/exclusions/nested/sparse + file map. | R019 | C011,C013,C014,C016 | DONE | codex | E142,E143,E144,E145,E146,E148 |
| T013 | G2 | T006,T012 | Go AST declarations/scopes/package variants trên captured bytes. | R008 | C002,C003,C004,C010 | DONE | codex | E165,E166,E167 |
| T014 | G2 | T013,T020 | Typed Defs/Uses/Selections; local fields/params/locals không mất references. | R008,R009,R099 | C003,C004,C005,C009,C129 | DONE | codex | E182,E183,E184,E185 |
| T015 | G2 | T014,T007 | Directed multirelation graph, direct/interface/candidate/unresolved edges + source sites. | R005,R010,R011 | C006,C007,C008,C045 | TODO | UNASSIGNED | NONE |
| T016 | G2 | T015 | Coverage/diagnostics/test refs/boundaries; no zero-result overclaim. | R011,R015,R025 | C008,C010,C011,C045 | TODO | UNASSIGNED | NONE |
| T017 | G2 | T007,T012,T013 | Exact/prefix symbol lookup + bounded FTS5 text search, index plans. | R019 | C002,C042,C045 | DONE | codex | E168,E169,E170,E171,E172,E173 |
| T018 | G2 | T008,T012,T015 | Dirty/add/delete/rename/config changes; manifest diff/invalidation. | R006,R020 | C013,C014,C015,C016,C017 | TODO | UNASSIGNED | NONE |
| T019 | G2 | T018,T016 | Strict full-workspace freshness + source preconditions; unknown/failure explicit. | R006,R017,R097 | C013,C015,C023,C122 | TODO | UNASSIGNED | NONE |
| T020 | G2 | T002,T005,T012 | Offline trusted package loader/profile, controlled env/tool paths; partial fallback. | R014,R032,R099,R121 | C009,C012,C046,C047,C129,C132,C174,C175,C176 | DONE | codex | E174,E175,E176,E177,E178,E179,E180,E181 |
| T021 | G2 | T018,T020,T019 | No-op/content reuse; full semantic fallback; selective only sau differential tests. | R020,R021 | C017,C018 | TODO | UNASSIGNED | NONE |
| T022 | G3 | T017,T019,T016 | Context pack target/source/ref/boundary selection deterministic. | R022 | C002,C006,C029 | TODO | UNASSIGNED | NONE |
| T023 | G3 | T022 | Task slices + trusted rules/constraints + validation-plan data; no invented contracts. | R022,R033 | C025,C026,C033,C045 | TODO | UNASSIGNED | NONE |
| T024 | G3 | T023 | Mandatory evidence budget, grouped gaps, valid serialization, bounded traversal. | R015,R016,R022,R023 | C028,C029 | TODO | UNASSIGNED | NONE |
| T025 | G3 | T024 | Signed cursors, page order/expiry/GC behavior, dedup và honest token accounting. CT-05 continuation/result cache/spool primitives, source fragments và cursor progress. | R016,R023,R097,R116,R123 | C028,C030,C031,C122,C158,C159,C160,C161,C162,C170,C179 | TODO | UNASSIGNED | NONE |
| T026 | G3 | T024,T025,T051,T052,T107 | Conformance/wiring một schema S43 đã seal: actual DTO/service/envelope parity/cancellation/error mappings; không thiết kế API lần đầu. | R004,R018 | C028,C039 | TODO | UNASSIGNED | NONE |
| T027 | G3 | T026 | Official SDK MCP adapter; 6 tools only; conformance/stdio framing. | R018,R024,R098 | C039,C041,C128 | TODO | UNASSIGNED | NONE |
| T028 | G3 | T027 | CLI/doctor/help/example + Codex local integration; preserve existing config. CLI runtime config/policy/profile administration typed, commands only when capability ready. | R018,R024,R031,R118 | C040,C041,C167,C168 | TODO | UNASSIGNED | NONE |
| T029 | G3 | T014,T019,T020,T025 | gopls version gate/lifecycle/LSP encoding/prepare+rename preview; no apply. RenameInitial/Continue: compute once→bounded spool→pages; final full-scope validate, no rerun LSP per page. | R012,R032,R116 | C027,C032,C034,C035,C158,C159,C160,C161,C162 | TODO | UNASSIGNED | NONE |
| T030 | G3 | T029,T026 | Preview preconditions/contract constraints/all edits; consumer safe workflow. | R012,R017,R034,R116 | C032,C033,C034,C041,C158,C159,C160,C161,C162 | TODO | UNASSIGNED | NONE |
| T031 | G3 | T011,T019,T024 | Hoàn thiện telemetry/measurement và redaction; core error/cancel/log primitives đã có từ foundation, không retro-fit behavior. | R027 | C035,C048 | TODO | UNASSIGNED | NONE |
| T032 | G4 | T027,T030,T031,T051,T052 | Adversarial security suite và network/source-mutation negative tests. | R013,R014,R031,R033 | C024,C025,C026,C034,C041,C046,C047 | TODO | UNASSIGNED | NONE |
| T033 | G4 | T032,T009 | Fault/resource hardening + measured quotas/cancel/process-tree; primitive limits phải hoạt động từ đầu mỗi use case. Harden build/test output guards bằng tooling hiện có; tools/run_check nhỏ chỉ khi cần; không global cleanup. | R026,R109,R110,R118 | C029,C035,C036,C141,C142,C143,C144,C145,C146,C167,C168 | TODO | UNASSIGNED | NONE |
| T034 | G4 | T021,T030,T032,T053,T054,T035 | Chạy acceptance R1 C001–C054; oracle independent; release applicability và fixture/repro evidence. Tổng hợp policy/build checks và targeted mutation/evidence reuse đúng applicability. Kiểm các batch workflow cases áp dụng trong fixture và bản kết hợp. | R025,R034,R111,R113,R127,R130 | C001..C054,C137..C148,C149,C150,C151,C181,C182,C183,C184,C185,C186,C187,C188,C189,C190,C191,C192 | TODO | UNASSIGNED | NONE |
| T035 | G4 | T033,T007 | GC ownership/active snapshots, backups, migration rollback, schema guards. Giữ source/durable/evidence và quyền cleanup, dry-run không side effect. GC exact FK order/leaf cycle; quiescent backup+restore journal/new incarnations; actual OS proof. | R026,R035,R101,R110,R113,R120,R123 | C036,C037,C038,C119,C130,C145,C146,C149,C150,C151,C171,C172,C173,C160,C170,C179 | TODO | UNASSIGNED | NONE |
| T036 | G4 | T028,T034,T035 | Linux amd64 + WSL2 test matrix, filesystem/version evidence. | R028,R120 | C019,C020,C021,C024,C035,C040,C171,C172,C173 | TODO | UNASSIGNED | NONE |
| T037 | G4 | T028,T034,T035 | Windows amd64 native paths/locks/process/ACL/MCP smoke + acceptance. | R028,R120 | C021,C024,C027,C035,C040,C171,C172,C173 | TODO | UNASSIGNED | NONE |
| T038 | G4 | T026,T034 | Benchmark harness/corpus/usage import; no API spend; baseline fair. | R029 | C044 | TODO | UNASSIGNED | NONE |
| T039 | G4 | T038,T036,T037,T053,T054 | Measure correctness/token/latency/storage tại S/M/L allowed profiles; phân biệt synthetic/real và byte proxies. | R029,R123 | C018,C042,C044,C160,C170,C179 | TODO | UNASSIGNED | NONE |
| T040 | G4 | T003,T039,T035 | Spec↔code/API/CLI consistency; tracker/evidence review; record remaining gaps. Spec/docs integration không lệch authority; command/mode/evidence/anti-bypass checks. Kiểm S53/commit inventory/task-to-batch/receipt và không self-hash metadata commit. | R030,R093,R100,R102,R105,R107,R111,R112,R124,R125,R126,R128,R130 | C039,C040,C043,C133,C134,C135,C137,C138,C139,C147,C148,C180,C181,C182,C183,C184,C185,C186,C187,C188,C189,C190,C191,C192 | TODO | UNASSIGNED | NONE |
| T041 | G5 | T040,T002 | License/notices/SBOM/security audit + reproducible release candidate manifest. Production Claim Gate và proof thực của controls, không cấp quyền publish. Xác minh checkpoint materials/remote-backup status đúng phạm vi, không publish ngoài quyền. | R036,R112,R126,R129 | C046,C047,C048,C148,C184,C185,C186,C188,C189 | TODO | UNASSIGNED | NONE |
| T042 | G5 | T041 | Final gate review: R1 supported matrix, correctness+benchmark, handover/resume state. Handover đúng Verification Ladder/applicability và permission riêng. Handover checkpoint hoặc tail scope_final theo S53; source restore khác data restore. | R030,R112,R124,R127,R130 | C001..C054,C148,C180,C186,C188,C189,C190 | TODO | UNASSIGNED | NONE |
| T043 | R2 | T042 | Discovery TS/JS provider độc lập, compiler/language-service license/pin/profile và ADR; không execute scripts. | R037 | C055 | DEFERRED | UNASSIGNED | NONE |
| T044 | R2 | T043,T055 | TS/JS typed provider + aliases/reexports/scopes/project refs; shared context contract. | R037 | C055 | DEFERRED | UNASSIGNED | NONE |
| T045 | R2 | T042 | Chốt domain entity/link/contract schema tối thiểu S28; identity/evidence/FK/migration và adapter contracts. | R038,R104,R117 | C059,C060,C061,C114,C163,C164,C165,C166 | DEFERRED | UNASSIGNED | NONE |
| T046 | R2 | T045 | Declared repo groups + member scopes/policies/snapshot vectors; không same-name auto link. Group get/list/put/remove CAS+tombstones và unavailable member ledger, pinned scope resolver. | R038,R117,R119 | C063,C163,C164,C165,C166,C157,C169,C170 | DEFERRED | UNASSIGNED | NONE |
| T047 | R2 | T042 | RecordService trên records.sqlite: immutable versions/head CAS/mutation receipts/epoch, approved-vs-effective needs_review; no index-GC loss. Record list/read query/historical source-expired state, no reapproval after GC. | R039,R094,R097,R119,R123 | C062,C064,C120,C121,C122,C157,C169,C170,C160,C179 | DEFERRED | UNASSIGNED | NONE |
| T048 | R2 | T044,T046,T047,T068 | R2 release review: Go/TS/Python/contracts/domain/AI metadata matrix; regression/token/limitations và handover. | R040 | C055..C069 | DEFERRED | UNASSIGNED | NONE |
| T049 | G2 | T012,T013,T017 | Hierarchical workspace/module/package overview; deterministic source-backed summaries và module IDs. | R041 | C049 | TODO | UNASSIGNED | NONE |
| T050 | G2 | T018,T019,T021 | Partition/input cache và reverse-closure invalidation; worker/memory profiling; full fallback. | R043 | C050,C053 | TODO | UNASSIGNED | NONE |
| T051 | G3 | T022,T024,T025,T049 | Overview/plan/selectors/topic aliases + task obligations/grouping/cursor progress; spec-only plane. | R041,R042 | C049,C052 | TODO | UNASSIGNED | NONE |
| T052 | G3 | T019,T025,T050 | analysis_snapshot vs checked_workspace và pre-edit validate; cache không lưu live freshness truth. | R044 | C051 | TODO | UNASSIGNED | NONE |
| T053 | G4 | T032,T050,T051,T052 | Scale fixture S/M/L, large fanout/multi-worktree + memory/quota measurements; unsupported envelope explicit. | R043 | C053 | TODO | UNASSIGNED | NONE |
| T054 | G4 | T003,T028,T030,T051,T052 | Self-index pinned binary, independent oracle/direct-tool fallback và spec-only onboarding acceptance. | R042,R045 | C052,C054 | TODO | UNASSIGNED | NONE |
| T055 | R2 | T043 | Minimal LanguageProvider/schema/process adapter + license/version/timeout/no-script conformance. | R037,R046 | C055,C057,C058 | DEFERRED | UNASSIGNED | NONE |
| T056 | R2 | T044,T055,T045 | Frontend TSX/routes/components/client-to-handler mappings; framework capabilities rõ. | R047 | C056 | DEFERRED | UNASSIGNED | NONE |
| T057 | R2 | T042 | Python safe AST/import/scope provider discovery+implementation; local profile; không execute target. | R046 | C057,C058 | DEFERRED | UNASSIGNED | NONE |
| T058 | R2 | T057,T055 | Python typed enrichment adapter khi verified, diagnostics/dynamic gaps + notebook metadata boundaries. | R046 | C057,C058 | DEFERRED | UNASSIGNED | NONE |
| T059 | R2 | T045 | MySQL-first SQL static lineage/migrations/views/routines/triggers/events, dialect/engine profile. | R048 | C059 | DEFERRED | UNASSIGNED | NONE |
| T060 | R2 | T045 | OpenAPI/JSON-schema/proto supported subset, versioned fields/local refs và code binding evidence. | R049 | C060 | DEFERRED | UNASSIGNED | NONE |
| T061 | R2 | T045 | AsyncAPI/event/cache contract producer/consumer/schema/namespace và delivery-control evidence. | R050 | C061 | DEFERRED | UNASSIGNED | NONE |
| T062 | R2 | T047,T059,T060,T061 | Approved machine/state/transition/guard/action/invariant/vocabulary và failure obligations; supported format gate. | R051 | C062 | DEFERRED | UNASSIGNED | NONE |
| T063 | R2 | T046,T056,T058,T059,T060,T061 | Federated cross-language/cross-repo flow queries bằng explicit bridge + snapshot vectors/gaps. | R052,R117 | C063,C163,C164,C165,C166 | DEFERRED | UNASSIGNED | NONE |
| T064 | R2 | T044,T047,T062 | Semantic snapshot diff + source-bound decision drift; ambiguity mapping và provider deltas. | R053 | C064 | DEFERRED | UNASSIGNED | NONE |
| T065 | R2 | T063,T062 | Trading/commerce context profiles, transactional/state/retry obligations có bằng chứng; no business execution. | R051 | C065,C066 | DEFERRED | UNASSIGNED | NONE |
| T066 | R2 | T058,T060,T062 | AI/ML/RAG prompt/model/data/eval/serving metadata lineage; no artifact execution/weights ingestion. | R054 | C067 | DEFERRED | UNASSIGNED | NONE |
| T067 | R2 | T056,T058,T062 | Approved architecture constraints + intended/observed drift report với witness/coverage. | R055 | C068 | DEFERRED | UNASSIGNED | NONE |
| T068 | R2 | T063,T064,T065,T066,T067 | Polyglot multi-domain acceptance/regression/benchmarks và capability matrices trước T048. | R056 | C055..C069 | DEFERRED | UNASSIGNED | NONE |
| T069 | R3 | T048 | Secure local runtime export import; redaction/revision/env/sampling/schema controls. | R057 | C070 | DEFERRED | UNASSIGNED | NONE |
| T070 | R3 | T069 | Trace/span-link graph enrichment giữ observed-only semantics và partial instrumentation. | R057 | C071 | DEFERRED | UNASSIGNED | NONE |
| T071 | R3 | T048 | Local test/coverage evidence import theo revision/profile/artifact hash; không chạy target tests. | R058 | C072 | DEFERRED | UNASSIGNED | NONE |
| T072 | R3 | T071 | Test selection rationale/uncertainty/full-regression fallback; independent recall fixtures. | R058 | C073 | DEFERRED | UNASSIGNED | NONE |
| T073 | R3 | T048 | Editor buffer bridge session/version/hash + unsaved snapshot isolation/no source mutation. | R059 | C074 | DEFERRED | UNASSIGNED | NONE |
| T074 | R3 | T048 | Optional semantic retrieval candidate-only + privacy/license/model/version cache rules. | R060 | C075 | DEFERRED | UNASSIGNED | NONE |
| T075 | R3 | T048,T050,T053 | Profile-driven immutable partition fact sets nếu bottleneck đã đo; publish/GC/migration invariants. | R061 | C076 | DEFERRED | UNASSIGNED | NONE |
| T076 | R3 | T048 | Static deployment/config/feature-flag ownership map, source vs observed deploy riêng; no IaC execution. | R062 | C077 | DEFERRED | UNASSIGNED | NONE |
| T077 | R3 | T075 | XL synthetic/real-permission scale, failure/latency/RSS/write-amplification measurements; no unlimited claim. | R061 | C078 | DEFERRED | UNASSIGNED | NONE |
| T078 | R3 | T064,T076 | Bounded machine-readable change/deployment/architecture reports qua existing application services. | R062 | C077,C080 | DEFERRED | UNASSIGNED | NONE |
| T079 | R3 | T048 | Demand-driven additional language provider discovery/fixtures/verified scope, no mass scaffolding. | R063 | C079 | DEFERRED | UNASSIGNED | NONE |
| T080 | R3 | T070,T072,T073,T074,T077,T078,T079 | Advanced release review/reporting: extension schema, tests, names, benchmark và exact enabled capability matrix. | R064 | C070..C080 | DEFERRED | UNASSIGNED | NONE |
| T081 | U0 | T042 | Explorer design seal trước UI: pinned deps/browser/tool API; S43 bridge/HTTP schema; permission/state/build binding tests và fixture plan. Chốt command/output/browser artifact budgets S51; không áp core quota thay UI workload budget. Seal v1.5 session-status/permission/pagination/pinned-scope/hash codec before UI feature. | R065,R080,R084,R088,R095,R096,R104,R108,R114,R121,R124 | C081,C082,C123,C124,C125,C126,C140,C141,C142,C152,C153,C174,C175,C176,C180 | TODO | UNASSIGNED | NONE |
| T082 | U0 | T081 | Loopback Explorer adapter: auth/bootstrap/Host/Origin/CSRF, bounded graph/source API qua app services; không start target. | R081,R115 | C083,C154,C155,C156,C157 | TODO | UNASSIGNED | NONE |
| T083 | U0 | T082 | React/TS/Vite AppShell: graph trái, source/detail phải, splitter + selection store + keyboard; dùng indexed fixture thật. | R065,R077,R078,R079 | C081,C082 | TODO | UNASSIGNED | NONE |
| T084 | U0 | T083 | GU0 review: shell+transport+source/scoping/canary, empty states đúng và no simulated graph path. | R065,R080,R081,R086,R088 | C081..C083 | TODO | UNASSIGNED | NONE |
| T085 | U1 | T084,T044,T056 | Dev AST transform + producer BuildManifest không generation; atomic local import + immutable generation binding; production strip proof. | R068,R069,R071,R084,R095,R114,R121,R122 | C084,C085,C089,C095,C123,C152,C153,C174,C175,C176,C177,C178 | TODO | UNASSIGNED | NONE |
| T086 | U1 | T085 | Preview SDK DOM instance registry/explicit component boundaries, bounded hit-test/rect lifecycle; không private fiber. | R068,R071,R082 | C085,C086,C093,C094 | TODO | UNASSIGNED | NONE |
| T087 | U1 | T082,T086 | Attach/CHALLENGE/READY/server-bind/BOUND_ACK bằng strict session/nonce/epochs; no source bearer to SDK, fail-closed readiness. POST preview/status + CLI permission lifecycle/CAS; current head/expiry; PING/PONG SDK riêng. | R070,R072,R082,R095,R096,R115,R119,R122 | C091,C092,C123,C125,C154,C155,C156,C157,C169,C170,C177,C178 | TODO | UNASSIGNED | NONE |
| T088 | U1 | T087 | Inspect shield/focus; Interact ARMING→MODE_ACK→server CAS trước unshield; disarm shield-first, stale/timeout stays inspect. | R066,R070,R072,R096,R115 | C084,C090,C091,C094,C126,C154,C155,C156,C157 | TODO | UNASSIGNED | NONE |
| T089 | U1 | T088 | Preview→graph: SDK opaque anchor/instance; server source binding only; browser joins runtime list; selection seq and scoped provenance. | R066,R068,R069,R095 | C084,C085,C086,C087,C124,C127 | TODO | UNASSIGNED | NONE |
| T090 | U1 | T089 | Graph→preview highlight/instance chooser/scroll permission + source drawer; no auto navigate/execute; event-loop guards. Server MappingMatch continuation khác SDK instance continuation; >100 sites supported by paging. | R067,R077,R116 | C087,C088,C158,C159,C160,C161,C162 | TODO | UNASSIGNED | NONE |
| T091 | U1 | T090 | Many-instance/ancestor/portal unsupported matrix; route/navigation/HMR build epochs, stale/unmounted transitions. Opt-in local explorer-map watch/atomic artifact import/coalesced HMR and busy-rebind status. | R067,R068,R073,R122 | C086,C088,C089,C093,C094,C177,C178 | TODO | UNASSIGNED | NONE |
| T092 | U1 | T091,T047 | Manual UI mapping via RecordService/CAS/receipt actor from auth; source drift effective review; owned durable data survives cache GC. | R069,R082,R087,R094,R101,R119,R123 | C096,C120,C121,C130,C157,C169,C170,C160,C179 | TODO | UNASSIGNED | NONE |
| T093 | U1 | T092,T027 | Selection→existing typed source/entity context pack + copy/export locator; server revalidation, no new write MCP or auto model call. Use selection preserves pinned_vector + RefSelector member/service, no active re-resolution. | R077,R083,R116,R117 | C097,C158,C159,C160,C161,C162,C163,C164,C165,C166 | TODO | UNASSIGNED | NONE |
| T094 | U1 | T093 | GU1 review: bidirectional frontend E2E, Inspect safety, HMR/prod strip/security + real repository scoped benchmark. Check evidence freshness/claim và output controls trên actual UI tooling. | R066,R067,R070,R071,R072,R073,R080,R083,R086,R088,R096,R111,R112,R115,R122,R124 | C084..C098,C123,C124,C125,C126,C127,C147,C148,C154,C155,C156,C157,C177,C178,C180 | TODO | UNASSIGNED | NONE |
| T095 | U2 | T094,T045,T059,T060 | Static backend flow projection: verified Go net/http route/binding subset + database/sql literal operations; ordered vs call-only labels. | R069,R074,R076,R084 | C099,C101,C102 | TODO | UNASSIGNED | NONE |
| T096 | U2 | T095,T046 | Scoped FE request→operation→BE contract bridge và reverse reachability; reuse domain/group services; vector of available members. | R069,R075,R117 | C100,C103,C106,C163,C164,C165,C166 | TODO | UNASSIGNED | NONE |
| T097 | U2 | T096 | Backend flow pane + SQL tables/op cards; graph↔flow/source interactions, return-to-preview bookmark không tự load route. | R074,R075 | C099,C103 | TODO | UNASSIGNED | NONE |
| T098 | U2 | T097,T061 | Transaction/branch/async edges và unknown persistence; no live DB access; supported event evidence integration. | R076 | C101,C102,C104 | TODO | UNASSIGNED | NONE |
| T099 | U2 | T098 | Hierarchy/filter/progressive rendering, deterministic layout workers/cancellation + accessibility and per-selection metrics. | R078,R079,R123 | C081,C105,C160,C170,C179 | TODO | UNASSIGNED | NONE |
| T100 | U2 | T099 | GU2 review: real-repo FE↔BE↔DB roundtrip, contracts/scoping/security/canary/performance/core regression and capability release. Kiểm bản kết hợp, evidence applicability và scoped production claims. | R074,R075,R080,R084,R086,R088,R111,R112,R124 | C099..C106,C147,C148,C180 | TODO | UNASSIGNED | NONE |
| T101 | U3 | T100,T069,T070 | Observed trace overlay adapter tái dùng runtime importer; exact build/trace/span provenance và gaps. | R076,R085 | C107,C108,C109 | DEFERRED | UNASSIGNED | NONE |
| T102 | U3 | T101 | Bổ sung Observe arming vào basic Interact của U1; explicit dev action/request correlation; export/import only, trace propagation allowlist; no target start. | R070,R085 | C107,C108,C110 | DEFERRED | UNASSIGNED | NONE |
| T103 | U3 | T102 | Concurrency/CORS/partial trace/rollback/redaction/session lifecycle fault suite và observer caps. | R076,R085,R086 | C108..C112 | DEFERRED | UNASSIGNED | NONE |
| T104 | U3 | T103 | GU3 review: observed vs static correct labels, real test-environment evidence và no unapproved action/production claim. | R085,R086,R088 | C107..C112 | DEFERRED | UNASSIGNED | NONE |
| T105 | G0 | T002,T003 | Canonical model/ports/closed schemas+registry, semantic validators và import-boundary tests trước feature code. Ghi Change Budget/Complexity gate cho delta ngoài baseline. | R089,R090,R097,R100,R107,R115,R116,R117,R118,R119,R120,R121 | C113,C114,C122,C131,C134,C139,C154,C155,C156,C157,C158,C159,C160,C161,C162,C163,C164,C165,C166,C167,C168,C169,C170,C171,C172,C173,C174,C175,C176 | DONE | codex | E090,E091 |
| T106 | G0 | T105 | Trích physical DDL/catalog/index/records, migration/FK/FTS/publish/CAS proof fixtures trên pinned driver; không target data. SQL counterexamples GC/resolver fixed và restore incarnation/permission storage proof. | R091,R094,R113,R114,R120 | C115,C116,C117,C118,C119,C120,C121,C149,C150,C151,C152,C153,C171,C172,C173 | DONE | codex | E092,E093 |
| T107 | G0 | T105,T106 | Contract-level flow/serialization/provider/source/state proof, MCP delivery budgets, codegen/extraction parity fixtures. Bổ sung pure command/storage/evidence contract fixtures S51; chưa là OS wrapper proof. New status/paging/scope/config/codec/state positive/negative proofs; no claim OS/browser tested. Thêm Git workflow fixture cô lập cho staged completeness/restore/retry/backup theo S53; không repo người dùng. | R090,R092,R098,R099,R108,R109,R110,R111,R115,R116,R117,R118,R119,R120,R121,R126,R127,R129 | C114,C117,C128,C129,C132,C136,C140,C141,C142,C143,C144,C145,C146,C154,C155,C156,C157,C158,C159,C160,C161,C162,C163,C164,C165,C166,C167,C168,C169,C170,C171,C172,C173,C174,C175,C176,C184,C185,C186,C187,C188,C189,C190,C191,C192 | DONE | codex | E094,E095,E096,E097,E100 |
| T108 | G0 | T106,T107 | Design seal review: authority/schema/DDL/ownership/order/pins/oracles verified; work cards ready, no unresolved architectural gaps for core. Seal mode/policy/command-safety readiness theo S50/S51, không fake enforcement. Audit A01–A08 core closure+owner gates verified; schema changes traced, product tests vẫn independent. Seal nhịp batch và fixture quyền/đếm/task order; no actual product checkpoint từ proof. | R089,R092,R093,R100,R102,R103,R104,R106,R107,R112,R124,R125 | C113,C133,C134,C135,C138,C139,C148,C180,C181,C182,C183,C192 | DONE | codex | E101 |

Thứ tự thi công không theo số ID; theo S47. Trạng thái chỉ ở bảng này. T105–T108 ở G0 nên thực hiện trước T004 trở đi; task R2/UI chưa đủ prerequisite không được claim dù TODO.

## S20 — Gates và Definition of Done

| Gate | Phạm vi task | Điều kiện PASS | Status ban đầu |
|---|---|---|---|
| G0 — Foundation/design seal | T001–T003,T105–T108 | Site survey, pinned materials, canonical contracts/DDL/flow proofs/import boundaries/order validation; T108 PASS trước feature; mode/nguồn quy tắc/command preflight S50–S51 | PASS |
| G1 — Safe storage | T004–T012 | Identity/isolation/path safety/capture/SQLite/publish/recovery; A không khóa B | PASS |
| G2 — Correct map | T013–T021,T049,T050 | Go scoped semantics; hierarchy/partition boundaries; strict freshness; full-vs-incremental equivalence | NOT_RUN |
| G3 — AI context interface | T022–T031,T051,T052 | Context/overview/plan/budgets/cursors; snapshot analysis mode; MCP/CLI; preview-only rename; telemetry | NOT_RUN |
| G4 — Verification | T032–T040,T053,T054 | C001–C054 + core-applicable C113–C136/fault/security/platform/scale/self-index tests; fair benchmark; spec/code đồng bộ; applicable C137–C148 và no bypass; không P0 failure | NOT_RUN |
| G5 — R1 release | T041–T042 | License/security/reproducibility + R1 handover; evidence và hạn chế rõ; claims đúng mức | NOT_RUN |
| G6 — R2 release | T043–T048,T055–T068 | C055–C069 + regression; TS/Python/contracts/domain/AI lineage matrix; T048 review | NOT_RUN |
| G7 — R3 review | T069–T080 | C070–C080 + regressions áp dụng; verified addon matrix; T080 dependencies DONE trước closure | NOT_RUN |
| GU0 — Explorer base | T081–T084 | Loopback safety + graph/source split thực từ index; C081–C083 | NOT_RUN |
| GU1 — Frontend inspect | T085–T094 | C084–C098; click preview↔graph + no Inspect actions + build provenance; framework matrix rõ | NOT_RUN |
| GU2 — Full-stack static | T095–T100 | C099–C106; FE→operation→BE→DB và reverse links, no execution/persistence overclaim | NOT_RUN |
| GU3 — Observed overlay | T101–T104 | C107–C112; trace causality/revision/privacy và explicit interaction permission | NOT_RUN |

G0–G5 là gate R1 tuần tự; G6/G7 áp dụng roadmap R2/R3. GU0–GU3 là nhánh Explorer, không là G8 sau toàn bộ R3. GU0 sau G5; GU1 cần GU0 + TS frontend capability tasks; GU2 cần GU1 + contracts/SQL/events/groups subset; GU3 cần GU2 + runtime importer T069/T070. R2 subset được mở scope để phục vụ Explorer theo S39; không mark các phần R2/R3 khác DONE/IN_PROGRESS tự động. Không có GUI preview trước quyền execution riêng. Các task cùng gate có thể song song theo dependencies/ownership.

Definition of Done một task: invariant giữ; source/API/schema rõ; test tương ứng PASS trong environment recorded; negative/failure cases không bị bỏ; lint/build phù hợp đã chạy; diff không scope creep/secret; requirement+case traceability; evidence ở revision xác định; còn gap phải BLOCKED hoặc scoped limitation được chấp nhận. Không mark DONE chỉ vì unit test mock pass mà thiếu integration thuộc task.

R1 nhãn phát hành:
- `EXPERIMENTAL_LOCAL`: engine chạy nhưng còn gate chưa pass; capability/platform chưa test phải nêu NOT_VERIFIED.
- `R1_VALIDATED_ON_FIXTURES`: mọi engineering gate liên quan pass trên fixtures, chưa đủ real-repo/usage evidence; không tuyên bố tiết kiệm token thực cho người dùng.
- `R1_VALIDATED_FOR_PERSONAL_USE`: có real-repo acceptance và host usage evidence theo S18 trên môi trường được nêu; không suy thành mọi project đều đúng.

G5 có thể hoàn thành engineering handover với nhãn fixture-validated khi thiếu quyền/data benchmark thực, nhưng T039/T042 chỉ DONE nếu deliverable ghi đúng nhãn, lý do NOT_RUN cho real-repo metrics và không tuyên bố đạt personal-use token gate. Nhãn personal-use chỉ khi các evidence thiếu đã bổ sung; phải mở task follow-up tracked, không sửa nhãn bằng lời nói.

### Mốc thử sử dụng và nghiệm thu thực tế

Các mốc dưới chỉ diễn giải kết quả của task/gate hiện có, không thêm release, thay lịch S47 hay miễn case. Mỗi kết quả ghi revision/binary, OS/filesystem, fixture hay repo thật, permission, profile/capability và PASS/FAIL/NOT_RUN ở S21; dùng expected facts do fixture/reviewer xác lập độc lập. Không có quyền hoặc môi trường thì phần liên quan NOT_RUN, không chặn khảo sát/fixture độc lập và không suy thành user acceptance.

| Mốc | Luồng quan sát được khi task liên quan đã hoàn thành | Bằng chứng tối thiểu và giới hạn |
|---|---|---|
| G0 | Chứng minh contract/DDL/flow bằng artifacts và fixture foundation; chưa có chức năng sản phẩm | T001–T003,T105–T108 và G0 theo S20; không demo như engine hoạt động |
| G2 | Capture/index một fixture Go có shadowing, thay đổi dirty và caller mới; tìm và kiểm facts theo snapshot | C002–C004,C015,C018,C023 và case task liên quan; phân biệt syntax/typed/partial; chưa gọi đây là context pack hoặc MCP hoàn chỉnh |
| G3 | Trên fixture đã index, host dùng CLI/MCP locate → context pack → expand nếu còn pending → validate trước sửa; rename chỉ preview | C028–C034,C039–C041,C051; output gắn source hash/scope, cảnh báo gaps và không sửa target. Host thực chưa thử thì integration tương ứng NOT_RUN |
| G4–G5 | Chạy acceptance, platform matrix và benchmark trong phạm vi được phép; bàn giao cùng nhãn | C001–C054 và các case áp dụng S20, T036–T042/T053/T054; report failures/unsupported và real-repo/token NOT_RUN nếu thiếu evidence; nhãn theo S20 |
| GU0–GU2 | Graph/source, sau đó React preview↔graph và static frontend→backend flow theo đúng capability | C081–C106 theo từng gate; GUI/preview/runtime quyền riêng theo S37; fixture không thành bằng chứng repo thật |

Kịch bản người dùng đầu tiên cho R1 sau G3: chọn worktree/profile đã đăng ký và được phép → index bytes trên đĩa → locate symbol bằng selector đủ định danh → đọc context pack cùng source evidence/gaps → expand khi còn nghĩa vụ chưa trả → validate workspace trước khi host tự quyết định sửa → chạy kiểm tra bằng host theo quyền riêng. Nếu thiếu dependency, scope không chắc, budget không đủ hoặc workspace đổi, phải hiển thị trạng thái/lỗi theo S08/S10/S43; không mô tả luồng như đã hoàn thành. Ví dụ là trình tự nghiệp vụ, không thay schema hoặc tạo CLI flags mới; T028 đồng bộ ví dụ thực thi với CLI thực có tại C040. Nghiệm thu người dùng trên repo thật cần ghi tác vụ/revision/profile, expected facts hoặc reviewer độc lập, kết quả/gaps và permission; không yêu cầu target repo để hoàn thành nhãn fixture theo quy định hiện hành.

Không cần reviewer thứ hai cho mọi task của công cụ cá nhân; critical paths cần self-review độc lập với code-generation và acceptance evidence. Khi không có reviewer độc lập thì ghi rõ; không giả “đã được audit”. Không xóa test hoặc đổi expected để hợp thức implementation sai.

Release artifacts chỉ tạo theo quyền: binary/checksum/version/schema/dep manifest/notices. Không tự publish GitHub release, package registry hoặc deploy service. Target repo source không được engine commit/push. Với chính MCE4D, local commit theo nhóm 10 task được yêu cầu tại S53 trong assignment implement/integrate phù hợp; push chỉ remote/branch đã được phép. Gate không bắt commit mỗi task; release/handover thực sự đóng scope có thể dùng tail đúng GIT-02.

## S21 — ADR, evidence, gaps và lịch sử thực thi

### ADR baseline

Status BASELINE_SPEC = quyết định mặc định của bản đặc tả, được AI dùng để triển khai; không khẳng định từng công nghệ đã được người dùng chọn riêng. ACCEPTED_USER chỉ dùng khi có xác nhận cụ thể; SUPERSEDED cần ADR thay thế.

| ADR | Quyết định | Lý do / trade-off | Status |
|---|---|---|---|
| A001 | Sản phẩm độc lập, không kế thừa engine/source tham khảo | Quyền chủ động; phải tự chịu kiểm chứng/bảo trì | ACCEPTED_USER |
| A002 | Một file đặc tả/tracker cho AI | Không phân mảnh quyết định; cần section routing để tiết kiệm đọc | ACCEPTED_USER |
| A003 | Go modular monolith | Dùng Go tooling và một binary; không đáp ứng mọi language tự động | BASELINE_SPEC |
| A004 | SQLite per-worktree, blobs dedup local, generations publish | Không cần graph server; metadata có thể lặp và cần quota/GC | BASELINE_SPEC |
| A005 | Core CLI + MCP STDIO, không UI/HTTP mặc định R1 | Giữ CLI độc lập; product GUI opt-in được bổ sung bằng A020/A021 | BASELINE_SPEC |
| A006 | Go typed trước; unsupported languages vẫn có file/text map | Làm chính xác một stack trước; không quảng cáo universal semantics | BASELINE_SPEC |
| A007 | MCE4D read-only target; rename preview bằng LSP | Giảm rủi ro mutation; host phải apply/verify riêng | BASELINE_SPEC |
| A008 | Full workspace freshness cho edit tasks | Local I/O cao hơn nhưng không mù với file mới/caller mới | BASELINE_SPEC |
| A009 | Budget bytes thật + optional tokenizer exact | Không đánh tráo byte estimate thành token; host usage mới đo hiệu quả | BASELINE_SPEC |
| A010 | Content incremental trước, semantic full fallback | Hy sinh update speed khi chưa chứng minh invalidation đúng | BASELINE_SPEC |
| A011 | Không global build/index mutex; per-worktree writer | Repo độc lập chạy được; OS vẫn có giới hạn tài nguyên thật | BASELINE_SPEC |
| A012 | Private first; dependency license gate trước public distribution | Không hứa sạch mọi quyền pháp lý; giữ notices và nguồn gốc | BASELINE_SPEC |
| A013 | Tên Maps-Context-Engine-4D; mce4d/slug/path đồng bộ | Yêu cầu đổi tên người dùng; chưa có code/release được quan sát | ACCEPTED_USER |
| A014 | 4D = structure/interactions/behavior/evolution | Tổ chức thông tin có evidence; không là đồ họa 4D hoặc chuẩn bên ngoài | BASELINE_SPEC |
| A015 | Giữ R1 Go nhưng R2 thêm TS/JS/Python/contracts/domain/AI metadata | Đáp ứng dự án đa dạng; không trì hoãn capability cần thiết như chỉ ý tưởng mơ hồ | BASELINE_SPEC |
| A016 | Hierarchy/partition + analysis snapshot tách live validation | Giảm lặp I/O/context nhưng giữ full pre-edit check | BASELINE_SPEC |
| A017 | Intended/observed/approved/runtime evidence tách plane | Dùng cho greenfield và brownfield không bịa code/rule | BASELINE_SPEC |
| A018 | Self-index pin binary, independent oracle | Dùng engine phát triển engine nhưng không bootstrap circular/self-proof | BASELINE_SPEC |
| A019 | R3 evidence/editor/search/advanced storage gated | Không over-engineer core; không block R1/R2 vì addon | BASELINE_SPEC |
| A020 | Code↔UI Explorer: graph trái; preview/flow phải; chọn button/block đồng bộ hai chiều | Yêu cầu mới người dùng; UI là scope thật, không còn FUTURE | ACCEPTED_USER |
| A021 | Tách index/query core khỏi loopback Explorer và approved target preview | Source read-only không đồng nghĩa runtime không side effect; auth/permission tách | BASELINE_SPEC |
| A022 | Build anchor + ephemeral instance + evidence resolver; không dựa private React internals hoặc screenshot guessing | Đúng many-to-many, HMR/profile provenance; framework coverage có gate | BASELINE_SPEC |
| A023 | React/TypeScript/Vite + React Flow và Dagre cho viewport giới hạn | Một renderer cho graph/flow; pin/license/compatibility ở T081; không đổi framework target | BASELINE_SPEC |
| A024 | U0→U1→U2 theo capability subset trước toàn R2/R3; U3 runtime tùy chọn | Có utility sớm, không chờ Python/ML để chọn React UI; vẫn giữ gates core | BASELINE_SPEC |
| A025 | Foundation design seal và task work orders trước feature | Người dùng yêu cầu bản vẽ, không autonomous architecture; no zero-defect promise | ACCEPTED_USER |
| A026 | Canonical schema/physical DDL authority trong cùng spec | Giảm contract drift; generated artifacts parity tests bắt buộc | BASELINE_SPEC |
| A027 | Durable records.sqlite tách derived index cache | CAS/idempotency/epoch; thêm DB local, không cross-DB atomic claim | BASELINE_SPEC |
| A028 | Build producer map khác engine generation binding | Cho HMR/lazy modules và multiple identical-source index generations; SDK không claim source IDs | BASELINE_SPEC |
| A029 | Server bind + BOUND_ACK + arming MODE_ACK | Explicit readiness và permission sequencing; Inspect không sandbox target | BASELINE_SPEC |
| A030 | MCP delivery compatibility/actual bytes | Không bỏ serialized text mặc định khi host chưa được verify; không giả token savings | BASELINE_SPEC |
| A031 | Giữ một spec chính; ba nguồn nguyên bản trong docs, tiếp nhận theo vai trò S50 | Người dùng chấp thuận tích hợp và sẽ copy file; không sửa Universal, không tracker thứ hai | ACCEPTED_USER |
| A032 | Command/storage controls cho phát triển, không global build lock hay quyền xóa ngầm | Bổ sung S51 theo supplement; runtime schema/DDL và thứ tự task giữ nguyên | BASELINE_SPEC |
| A033 | Verification Ladder + evidence freshness + anti-bypass cụ thể hóa vào task/gate hiện có | Không nhầm docs-only/proof với product test/enforcement/release; không tăng task chỉ để đếm | BASELINE_SPEC |
| A034 | Khép audit A01–A08 trong file chính, không thay architecture stack hoặc thêm tracker | Người dùng chấp thuận hoàn thiện tài liệu sau báo cáo audit; product chưa chạy | ACCEPTED_USER |
| A035 | Binding resolver fingerprint riêng và key đồng nhất; capture input không chứa parser output | Sửa counterexample và tách nguồn bằng chứng | BASELINE_SPEC |
| A036 | Bounded paging/result spool+pinned typed scope+status read/quyền có lifecycle | Đường dùng đầy đủ, six MCP tools giữ nguyên, không partial apply | BASELINE_SPEC |
| A037 | Store lifecycle shared/exclusive + restore incarnation/journal | Phối hợp reader/writer đúng resource, no global build mutex, restore không hồi sinh token | BASELINE_SPEC |
| A038 | MCE-C14N1 byte codec/defaults/admin contracts và HMR watch opt-in | Interop không đoán, không tự spawn target hoặc claim runtime proof | BASELINE_SPEC |
| A039 | Một commit đầy đủ sau mỗi10 task DONE, không commit rời rạc; checkpoint có đường lấy lại | Yêu cầu mới người dùng; kiểm mỗi task vẫn giữ, nhịp commit không thay gate | ACCEPTED_USER |
| A040 | Tag/restore receipt, scope_final và push có quyền; source khác runtime/data backup | Cụ thể hóa khả năng khôi phục và tránh self-hash/metadata commit; không bảo đảm mọi thời điểm khi chưa có snapshot | BASELINE_SPEC |
| A041 | Ngoại lệ đúng `github.com/hashicorp/golang-lru/v2 v2.0.7` MPL-2.0 trong đồ thị dependency/test SQLite, theo IP-05a | User trực tiếp chọn cho phép ngoại lệ giới hạn test; không mở rộng license cho product artifact/phân phối | ACCEPTED_USER |

Template ADR mới: `id; problem/evidence; options; decision; constraints preserved; migration/rollback; affected R/T/C; status; date`. Không rewrite ADR cũ mất lịch sử; append supersession gọn.

### Evidence log

Evidence thực thi giữ phân biệt từng kiểm và phạm vi; kiểm bootstrap không chứng minh product capabilities hoặc toàn bộ acceptance case.

| Evidence ID | Task | Code revision / tree digest | Environment + versions | Command / case | Result | Artifact path + SHA-256 | Checked at UTC |
|---|---|---|---|---|---|---|---|
| E001 | T001 | `ba72ae4` + working tree; hashes source trong ghi chú T001 | Windows amd64; Go 1.27.4 archive chính thức, CGO=0, offline flags | `gofmt -d cmd/mce4d/main.go` không diff; `go test ./...` exit 0, không có test files; `go vet ./...` exit 0; scope bootstrap | PASS | `cmd/mce4d/main.go` SHA-256 `cb38c0fe62815899c415c10392d37872d8ea041bde4888f910d75c070862b6e2` | 2026-11-11 02:53 UTC |
| E002 | T001 | Như E001 | Windows amd64; Go 1.27.4 | `go build -o artifacts/preflight/_bootstrap-build/bin/mce4d.exe ./cmd/mce4d` exit 1: Windows Application Control chặn `link.exe`; binary smoke NOT_RUN | FAIL | NONE | 2026-11-11 02:53 UTC |
| E003 | T001 | `ba72ae4` + working tree; `go.mod`/`cmd/mce4d/main.go` hashes đối chiếu Windows và snapshot Linux như ghi chú T001 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; CGO=0; offline flags | `go test ./...`, `go vet ./...`, `go build` exit 0 trên snapshot; smoke binary exit 3, stdout rỗng, stderr báo bootstrap; không có test files | PASS | `/home/mce4d/.local/share/mce4d/preflight-20261111-t001/bin/mce4d` SHA-256 `f1ccbcf5c6107ddcfea5f2a274f2f1e05a3103385e91341c9d52e914b511e275` | 2026-11-11 03:05 UTC |
| E004 | T002 | `ba72ae4` + working tree; go.mod/go.sum/test hashes ở T002 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; CGO=0; isolated caches | `go mod verify`, `gofmt -d`, `go test -v -count=1 ./...`, `go vet ./...`, `go build` exit 0 trên snapshot byte-identical; 4 fixture tests PASS | PASS | `tests/preflight/dependencies_test.go` SHA-256 `36733717356ac919a73f7d23c31e250e4622e261a8018ae86afd551f9fb5ef8a` | 2026-11-11 03:14 UTC |
| E005 | T003 | `ba72ae4` + working tree; hashes code T003 trong ghi chú | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Git fixture cô lập | `go test ./...`, `go vet ./...`, `go build ./cmd/mce4d`, `go run ./tools/spec_check` trên snapshot; mutation/status/contract/policy/batch/Git fixture tests | PASS | `tools/spec_check/main.go` SHA-256 `efbd3ecdc85a94c0727d70e41849ee03499bf1ba55d058039cfb82423620ee47` | 2026-11-11 03:25 UTC |
| E006 | T105 | `ba72ae4` + working tree; hashes ở ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; CGO=0; Python stdlib; offline snapshot | `python3 tests/contract/canonical_reference.py`, `go test -count=1 ./...`, `go vet ./...`, `go build ./cmd/mce4d`, `go run ./tools/spec_check` exit 0; proof từng phần, chưa đủ T105 DONE | PASS | `contracts/mce4d.v1.schema.json` SHA-256 `baff99b21026031e3bae13c01952755d39309b14155403918779ed55a97c575a` | 2026-11-11 03:32 UTC |
| E007 | T105 | `ba72ae4` + working tree; hashes phần thay đổi trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python 14 vectors, `go test -count=1 ./...`, `go vet ./...`, `go build ./cmd/mce4d`, `go run ./tools/spec_check` exit 0; proof từng phần | PASS | `internal/model/binding.go` SHA-256 `59240ea7d5f5c99459a23a9bba97f47a21ddcf46f8c39b7aac795f7f1db4b26f` | 2026-11-11 03:32 UTC |
| E008 | T105,T003 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot; spec trước revision 1.6.3 | `go test -count=1 ./...`, `go vet ./...`, `go build ./cmd/mce4d`, `go run ./tools/spec_check`, Python vectors exit 0; rework tracker và ScopeVector shape, chưa kiểm digest recipe mới | PASS | `internal/model/scope.go` SHA-256 `efe6ae682a5cc229b70e2d3fe932e65caa63c298e8028cd99842433771b29a54` | 2026-11-11 05:27 UTC |
| E009 | T105,T003 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot, spec 1.6.3 | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap `go build`, `go run ./tools/spec_check` exit 0; approved unavailable sort và scope selection partial | PASS | `internal/model/scope_normalize.go` SHA-256 `ac5dacb200e2ee2828ec222854a2fdbfa064cb4e63b1d177d7dcdbd85a0dfc3d` | 2026-11-11 05:29 UTC |
| E010 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python 14 vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap `go build`, `go run ./tools/spec_check` exit 0; GraphRef/RefSelector và member-match fixture, partial | PASS | `internal/model/graph_ref.go` SHA-256 `122bb923248de335a29e91b7ee612792ace067d7dca740bf8d15b4a556b9f6b8` | 2026-11-11 05:30 UTC |
| E011 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot; spec 1.6.4 | Python 14 vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap `go build`, `go run ./tools/spec_check` exit 0; EvidenceRef origin matrix và Pagination partial | PASS | `internal/model/evidence.go` SHA-256 `a66748936c4348e9cd9c04b789f10b10433909abbda61a60b4a5fce7d306e77d` | 2026-11-11 05:32 UTC |
| E012 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python 14 vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap `go build`, `go run ./tools/spec_check` exit 0; Status/Locate request shape/default/XOR partial | PASS | `internal/model/requests.go` SHA-256 `0bd524e258e9c677dbd548230697015e889976c0d9ccd37d6c5c47f372ca4ce1` | 2026-11-11 05:33 UTC |
| E013 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python 14 vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap `go build`, `go run ./tools/spec_check` exit 0; selector/pack/expand/validate/rename request fixtures partial | PASS | `internal/model/pack_request.go` SHA-256 `595ffce3f78b456d920b8f36587bcef43de1d267e65a6428f1ea6a723c942d8f` | 2026-11-11 05:35 UTC |
| E014 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python 14 vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap `go build`, `go run ./tools/spec_check` exit 0; scope digest, lexical path, preview status partial | PASS | `internal/model/scope_digest.go` SHA-256 `3301cf9eee27013d5d77544589ce4b7aeadc173a337f70ab95bb25b957b2c80c` | 2026-11-11 05:36 UTC |
| E015 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python 14 vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap `go build`, `go run ./tools/spec_check` exit 0; budget và runtime config partial, schema bound/default parity | PASS | `internal/model/runtime_config.go` SHA-256 `b0e1c29874f687f37e0be572555bfc848b41dd120846ed3fb6c12c27e8b9c103` | 2026-11-11 05:38 UTC |
| E016 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Go `encoding/json` chấp nhận MAX_FILE_BYTES dù schema đóng: regression ban đầu FAIL; sau exact-key validation, Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0 | PASS | `internal/model/runtime_config.go` SHA-256 `38baabcf3e2f6c552802cd22cb9db2b59d58258c425d12b69243682c6e663168` | 2026-11-11 05:39 UTC |
| E017 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; profile/policy digest và operation registry partial | PASS | `internal/model/profile.go` SHA-256 `9007c8bda0379c9b931630acf2eb0a67f5e38b4b4c5905312e748db794ac4eb8` | 2026-11-11 05:41 UTC |
| E018 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; capture manifest identity fixture partial | PASS | `internal/model/capture.go` SHA-256 `cf3bc3994571399ee2c940360ccff700d75dd74e62391b14fa88128117a6e9ac` | 2026-11-11 05:42 UTC |
| E019 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; source anchor và effective runtime config digest partial | PASS | `internal/model/source_anchor.go` SHA-256 `60648af991678ae7223ff9a6fd823a7eba361b36ec545cafddec0d9e7a0fd974` | 2026-11-11 05:44 UTC |
| E020 | T003 | `ba72ae4` + working tree; current rework hashes trong ghi chú T003 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; offline snapshot | `go test -count=1 ./tools/spec_check`, `go run ./tools/spec_check` exit 0; includes misplaced evidence/unknown last ID mutation tests | PASS | `tools/spec_check/main.go` SHA-256 `cdda8352e826c42e5425176eae34e2fbfdb570686752f8e813e97bb354afc20c` | 2026-11-11 05:44 UTC |
| E021 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; freshness/coverage/gap/error wire contracts partial | PASS | `internal/model/coverage.go` SHA-256 `22402c857cf99ad27297772848a94fbcb68e918ff95da0d3ee66519cc48951c0` | 2026-11-11 05:45 UTC |
| E022 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; BuildManifest closed links/order/cycle/digest partial | PASS | `internal/model/build_manifest.go` SHA-256 `76c217e5c4b6790f9197b85cdc59570d80c0c6fc7b0bf1507b4fb3baa547c135` | 2026-11-11 05:47 UTC |
| E023 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; BuildManifest kiểm source bytes/anchor IDs trên fixture | PASS | `internal/model/build_manifest_capture.go` SHA-256 `c501c71a7204c980319a7b72bf0057bcf268bb0ed010e49885d84b5d36ff4ee0` | 2026-11-11 05:48 UTC |
| E024 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; SymbolFact/RelationFact boundary fixtures partial | PASS | `internal/model/provider_fact.go` SHA-256 `518738c43d6005a1ef0801c55aa3272632655c60e5eae90ca8427a0367ee1ff4` | 2026-11-11 05:49 UTC |
| E025 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; provider message shape/profile/input digest partial | PASS | `internal/model/provider_message.go` SHA-256 `2105cf1c6b5b97bc6646df30f7d389900f4735b6854d234a31063e9b6eac9172` | 2026-11-11 05:50 UTC |
| E026 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; admin request/CAS semantics và provider fact DDL parity partial | PASS | `internal/model/runtime_config_request.go` SHA-256 `4522510a347c608808b5e0032a5eb802369a61bfedf976871d1c1d534852c371` | 2026-11-11 05:52 UTC |
| E027 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; layer-import boundary partial | PASS | `tests/contract/layer_boundary_test.go` SHA-256 `e6531ada3d0504eed4cd407713d81fdbbcf41421725f030dfcf8faabe0ccb20e` | 2026-11-11 05:53 UTC |
| E028 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; source-read exact bytes và graph/flow request shapes partial | PASS | `internal/model/source_read.go` SHA-256 `3b803c1e8868acb5cb48213d3e42afa99c12b439eb7677ff03afedb0460598fe` | 2026-11-11 05:54 UTC |
| E029 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; ref selector symbol/module defaults | PASS | `internal/model/pack_request.go` SHA-256 `6cf96a00c75abb6596ffb146d1c9b0f2d9d7b3b5c3f6eb23c27c4cacd21c3c46` | 2026-11-11 05:55 UTC |
| E030 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; group admin/record request partial | PASS | `internal/model/group_admin.go` SHA-256 `54475212e8f63bb4a7f1331b72150b5b50e724c7ccc2759bfa4f4adc74e44086` | 2026-11-11 05:56 UTC |
| E031 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; rename read-only envelope và source fragment partial | PASS | `internal/model/source_fragment.go` SHA-256 `16c82f6e22727474d639f2bafc227368e0b4ace15f5b55d24e96cda119610307` | 2026-11-11 06:00 UTC |
| E032 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; wire JSON/bridge primitive partial | PASS | `internal/model/wire_json.go` SHA-256 `791b40fb7e199ef348be126ed79f2780b8b19ff45a204a645721b9073e72dee5` | 2026-11-11 06:02 UTC |
| E033 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; 23 bridge variants schema fixture/roundtrip, nested negative cases partial | PASS | `internal/model/bridge_message.go` SHA-256 `48b9fb3974e41432a6c93dafc8e25bc5b81c282116fc6d4fcd19028785e16db6` | 2026-11-11 06:04 UTC |
| E034 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; explicit bridge pick coordinate bound partial | PASS | `internal/model/bridge_payloads.go` SHA-256 `763aa549b82f3617e8f32108d4335974d3f3942f8d24501bfcb257b063b6e660` | 2026-11-11 06:05 UTC |
| E035 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; mapping pair/binding schema fixtures partial | PASS | `internal/model/mapping.go` SHA-256 `21e56a26e1e2e60dad29e6238eafbdda7298315f32edb48ecd806ddb785f79e8` | 2026-11-11 06:06 UTC |
| E036 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; graph page endpoints and schema fixture partial | PASS | `internal/model/graph_data.go` SHA-256 `3437296153f677ec35240f68792f6144953d74b364d8ccf42d43989c85169d37` | 2026-11-11 06:07 UTC |
| E037 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; flow page and static/observed labels partial | PASS | `internal/model/flow_data.go` SHA-256 `e1431198a513e5ff8d68258889d739cd534f24e060ca74504dd22b21fd693dde` | 2026-11-11 06:08 UTC |
| E038 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; core response and source fragment partial | PASS | `internal/model/pack_data.go` SHA-256 `5e5e8e1ea09b63f1288433b7f23b7e24228788c11c1b372c3102568cc695ca26` | 2026-11-11 06:10 UTC |
| E039 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; record and preview status shapes partial | PASS | `internal/model/record_data.go` SHA-256 `cfd7412435b4f5300d77d02173c59fce1a6087294e0dc011eb774b1bc44d06fc` | 2026-11-11 06:11 UTC |
| E040 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; admin and bootstrap shapes partial | PASS | `internal/model/admin_data.go` SHA-256 `202b1e898465e8d830abf55f984bdc864cb3186115759efff5c66ebe7d5eac83` | 2026-11-11 06:12 UTC |
| E041 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; mapping record and preview request shapes partial | PASS | `internal/model/preview_requests.go` SHA-256 `d492b3fae618a6e0f3d0a400a2e19ab6dbaae9259338c29bdf5ed3ad2d217005` | 2026-11-11 06:13 UTC |
| E042 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors và preview permission digest oracle, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; partial | PASS | `internal/model/preview_permission.go` SHA-256 `81b2879e8ebcebb1ba49ea2b2d283c4b39660fe8e9e6bf59217a5e9119749cf2` | 2026-11-11 06:14 UTC |
| E043 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; permission admin shape/CAS fields/digest partial | PASS | `internal/model/preview_permission_admin.go` SHA-256 `24e396406ee7a56dd9a668fecb0420b9de8a1cbfa18da5e26cc5e0e899f658ce` | 2026-11-11 06:14 UTC |
| E044 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; response union/SV08 and config digest partial | PASS | `internal/model/response.go` SHA-256 `a49243f9514dc4b02c478346e1466b7cca1198dcc5cbcd6a9eaecfb1e1b55eb8` | 2026-11-11 06:15 UTC |
| E045 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors/reference digests, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; registry request union and response checks partial | PASS | `internal/model/reference_digests.go` SHA-256 `0fa79051aeffefdc12b2207087d677f91cf022c5f125b4b21fbc96883de0df3d` | 2026-11-11 06:17 UTC |
| E046 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; registry response bindings partial | PASS | `contracts/response.go` SHA-256 `afb1b9d8a88eea6216bbd28f155bbaa1bdbb7d9e1abfe4f69cc0eca708a53aa2` | 2026-11-11 06:17 UTC |
| E047 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; source length invariant partial | PASS | `internal/model/source_read.go` SHA-256 `18b5970ba4042c2397788c88d33d6a58354e3f9f8d564eac6dbbaf008a959134` | 2026-11-11 06:18 UTC |
| E048 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; 20 response and 27 request schema fixtures | PASS | `tests/contract/response_variants_schema_test.go` SHA-256 `5c76529a34443c544f18620c27000af0a0aa6c42adb4dacc7cfb0b25c4825924` | 2026-11-11 06:19 UTC |
| E049 | T105 | `ba72ae4` + working tree; isolated WSL snapshot mutant, no workspace mutation | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; offline | disable captured hash comparison: `go test -count=1 ./internal/model -run ^TestSourceRefCapturedBytes/wrong_hash$` FAIL expected; restore bytes and rerun PASS | PASS | `internal/model/source_ref.go` restored SHA-256 `0aef15540f38f3253984eaff89441df24c8f2917aed3daa0ffaff21224c45240` | 2026-11-11 06:20 UTC |
| E050 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors + request/record digest oracles, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; partial | PASS | `internal/model/request_record_digest.go` SHA-256 `a62fb88f5d2780e43fe7251e436f9d0dfbf8fb987201cb3f429859e9de0ee9a6` | 2026-11-11 06:21 UTC |
| E051 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors/subgraph digest oracle, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; partial | PASS | `internal/model/subgraph_digest.go` SHA-256 `20123060e825f165be6091d16f817f813a945a1a5bf45887ae2f4a45968be66b` | 2026-11-11 06:22 UTC |
| E052 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; registry-bound request digest partial | PASS | `contracts/request_digest.go` SHA-256 `995767a947c342322c14fad6e4d0148d3c91ee0d8418f7669654f45904dbb091` | 2026-11-11 06:22 UTC |
| E053 | T105 | `ba72ae4` + working tree; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; revoked permission fail-closed | PASS | `internal/model/preview_permission_admin.go` SHA-256 `8aba437237c3fcbfb3f7be623f066e36f1fa5d001664184449975472bc8c3e76` | 2026-11-11 06:23 UTC |
| E054 | T105 | `ba72ae4` + working tree; spec 1.6.5; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors/rename digest oracle, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; partial | PASS | `internal/model/rename_result_digest.go` SHA-256 `236a19e8e55f2f23abfcbf589432043fdb56b465e8f084bd19dd1a804cf97016` | 2026-11-11 06:24 UTC |
| E055 | T105 | `ba72ae4` + working tree; spec 1.6.5; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; pinned scope postcheck partial | PASS | `internal/model/scope_postcheck.go` SHA-256 `6631d5ca30f66951f9387cc46cd306d8bd5e51fcfe3be3b8b3acb770ef691108` | 2026-11-11 06:25 UTC |
| E056 | T105 | `ba72ae4` + working tree; spec 1.6.6; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors/rename tie oracle, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; partial | PASS | `internal/model/rename_result_digest.go` SHA-256 `74081c9a2fac657df2021a2d7603820d2ec8cd0f6cdb5ecad4a4ca88161c2ef0` | 2026-11-11 06:25 UTC |
| E057 | T105 | `ba72ae4` + working tree; spec 1.6.6; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; pinned source identity partial | PASS | `internal/model/source_scope.go` SHA-256 `652fe6b026c68c659e30f339ee08359e4e29eeceab3be807ba733ef3e6d8c60a` | 2026-11-11 06:26 UTC |
| E058 | T105 | `ba72ae4` + working tree; spec 1.6.6; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; record stale approval fail-closed | PASS | `internal/model/record_data.go` SHA-256 `705d22d3055bc17c7aeec04a289329378a98e8ea8e89c2e2f4c690fa5402cd02` | 2026-11-11 06:27 UTC |
| E059 | T105 | `ba72ae4` + working tree; spec 1.6.7; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors/subgraph oracle, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; nested scope normalization partial | PASS | `internal/model/scope_vector_normalize.go` SHA-256 `b72444836c238a56180acfa6f80056354c936c6c0239ad68ba3e1e5c182c5c4a` | 2026-11-11 06:28 UTC |
| E060 | T105 | `ba72ae4` + working tree; spec 1.6.7; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; nested scope selector validation | PASS | `internal/model/scope_request.go` SHA-256 `4b8468a81a124217ec9ad4d6465e6ca3a8d4099a794d4f2fa216244163eab177` | 2026-11-11 06:28 UTC |
| E061 | T105 | `ba72ae4` + working tree; spec 1.6.7; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; delivery byte accounting fixture | PASS | `internal/model/delivery_budget.go` SHA-256 `21edc8a551de9cc4b08b72b7f0db7864bee32920dce1d4becc643a82d577963e` | 2026-11-11 06:29 UTC |
| E062 | T105 | `ba72ae4` + working tree; spec 1.6.7; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; bounded byte-count fixed point fixture | PASS | `internal/model/delivery_budget.go` SHA-256 `c9441e40685845d12cc3596f166a173f11a603a6d4cbc4010ddb599b73166c01` | 2026-11-11 06:29 UTC |
| E063 | T105 | `ba72ae4` + working tree; spec 1.6.7; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; contiguous source fragments | PASS | `internal/model/fragment_sequence.go` SHA-256 `cf83829eccac6d15e1b55766f503c72febfdd290e8e4523cce2dd637fefeb7e5` | 2026-11-11 06:30 UTC |
| E064 | T105 | `ba72ae4` + working tree; spec 1.6.7; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; continuation shape partial | PASS | `internal/model/continuation.go` SHA-256 `448f569e0ff49a3f0c7841076a65428b64372b67cbe956d90cabff70fa6fc344` | 2026-11-11 06:31 UTC |
| E065 | T105 | `ba72ae4` + working tree; spec 1.6.7; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; stale mode ACK correlation partial | PASS | `internal/model/mode_ack.go` SHA-256 `a5ce92be1d9c9f205aa7a101735c16106a5a4be532c7ed89b09626f00db086d4` | 2026-11-11 06:31 UTC |
| E066 | T105 | `ba72ae4` + working tree; spec 1.6.7; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; SDK PONG correlation partial | PASS | `internal/model/sdk_probe.go` SHA-256 `d67c04b6c862f8af15bcc5c0a4387e9a1b46911ea59e68da06f145b257d9d4e2` | 2026-11-11 06:32 UTC |
| E067 | T105 | `ba72ae4` + working tree; spec 1.6.7; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; expanded ACK stale/permission cases | PASS | `internal/model/mode_ack_test.go` SHA-256 `b645ec1b49a6ce5742b64cdcea727c47a3c00d860e93059695dc6c0d2f49a578` | 2026-11-11 06:33 UTC |
| E068 | T105 | `ba72ae4` + working tree; spec 1.6.7; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; ambiguous service in rename result | PASS | `internal/model/rename_result_digest.go` SHA-256 `2996e8d6574c55402bdd388ae44a3bac32f73389a9d203a48a4a21a514f57bc2` | 2026-11-11 06:33 UTC |
| E069 | T105 | `ba72ae4` + working tree; spec 1.6.7; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; analysis snapshot edit readiness | PASS | `internal/model/pack_response.go` SHA-256 `d9464db568cf20a18f0b0ab49727e2377097e118dc31a9606077be54cac6c704` | 2026-11-11 06:34 UTC |
| E070 | T105 | `ba72ae4` + working tree; spec 1.6.7; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; nested request scope digest normalization | PASS | `internal/model/rename_result_digest.go` SHA-256 `4916294ba7388a30a099412f46d31975743e7d70acdfc0298aaf719b42c74a32` | 2026-11-11 06:35 UTC |
| E071 | T105 | `ba72ae4` + working tree; spec 1.6.7; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; exact browser event origin fixture | PASS | `internal/model/preview_origin.go` SHA-256 `d05114348cf3c3ed88eca0ee634cf3efa8a4958d0feb608651482ee07cf2b164` | 2026-11-11 06:36 UTC |
| E072 | T105 | `ba72ae4` + working tree; spec 1.6.7; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; layer boundary/global-name guards partial | PASS | `tests/contract/layer_boundary_test.go` SHA-256 `e2bdbaf9f2fca30213e724a6358f58153fac38428ebd566acf42e2090dbbc5cc` | 2026-11-11 06:36 UTC |
| E073 | T105 | `ba72ae4` + working tree; spec 1.6.7; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; long Unicode line fragment fixture | PASS | `internal/model/fragment_long_test.go` SHA-256 `cee76e1629bc0e19c267b707cf5c2fbe86bb2181492fd64347741b4de1ab6db0` | 2026-11-11 06:37 UTC |
| E074 | T105 | `ba72ae4` + working tree; spec 1.6.7; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; status freshness scope | PASS | `internal/model/response.go` SHA-256 `537aa86659845e40b823bd11a301c5d6b592fc3b42af38c7d1eaefbf93b1fc2d` | 2026-11-11 06:37 UTC |
| E075 | T105 | `ba72ae4` + working tree; spec 1.6.7; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; provider stream sequence/digest reference | PASS | `internal/model/provider_stream.go` SHA-256 `b362a6c6470a89f7aeb03222126fcff27251e5e3f3ed0ec295d2e222482c2c81` | 2026-11-11 06:38 UTC |
| E076 | T105 | `ba72ae4` + working tree; spec 1.6.7; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; 1025-edit digest fixture | PASS | `internal/model/rename_large_test.go` SHA-256 `e6b4ea7c8d2df4e03efefb81369a254dce2ddb86cc6b20f5679c58c65e95eb5c` | 2026-11-11 06:39 UTC |
| E077 | T105 | `ba72ae4` + working tree; spec 1.6.7; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; provider terminal status reference | PASS | `internal/model/provider_stream.go` SHA-256 `b3b7599d14a3a390eca48d38b15c78233074351b8689a3893c517588b09abaf9` | 2026-11-11 06:39 UTC |
| E078 | T105 | `ba72ae4` + working tree; spec 1.6.7; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; runtime/policy cap arithmetic | PASS | `internal/model/runtime_policy_caps.go` SHA-256 `5e7fcb350c6904d206000d60d43ebc0f2e4355cec4c213d2e90cedb0058a72a3` | 2026-11-11 06:40 UTC |
| E079 | T105 | `ba72ae4` + working tree; spec 1.6.7; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; enabled provider keys partial | PASS | `internal/model/enabled_keys.go` SHA-256 `feff475328fff484404e8906ae8607fc725a480a08bf6256b89c316b1eb73831` | 2026-11-11 06:40 UTC |
| E080 | T105 | `ba72ae4` + working tree; spec 1.6.7; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; 101 mapping pairs/unique label | PASS | `internal/model/mapping_completeness.go` SHA-256 `1364f7f8059a25061fa9cbed1a5f1d3da6b9748a26ca5d186656045b66a0d56e` | 2026-11-11 06:41 UTC |
| E081 | T105 | `ba72ae4` + working tree; spec 1.6.7; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; provider 16 MiB hard cap | PASS | `internal/model/provider_frame_limit.go` SHA-256 `ec655b07e9a3a0b97751af5a9dc9c9d256da9bf6e81206c4c9ab231043875b9f` | 2026-11-11 06:42 UTC |
| E082 | T105 | `ba72ae4` + working tree; spec 1.6.7; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; registry request/response hard caps | PASS | `contracts/limits.go` SHA-256 `518e068802ee361bce27986ea73e946a42cfee94e6693ecf108498e071ded303` | 2026-11-11 06:43 UTC |
| E083 | T105 | `ba72ae4` + working tree; spec 1.6.7; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; core response budget hard cap | PASS | `contracts/response.go` SHA-256 `19b2d0fb3c962e93768c59f77a146036e569f6a15aefab0088bc6de82ef37c50` | 2026-11-11 06:43 UTC |
| E084 | T105 | `ba72ae4` + working tree; spec 1.6.8; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; preview path boundary and resolver version | PASS | `internal/model/preview_path.go` SHA-256 `5142dbcfa35819c196b9612db944e3cbec37f4b5b1107e29852da5e9dc7de3d5` | 2026-11-11 06:45 UTC |
| E085 | T105 | `ba72ae4` + working tree; spec 1.6.8; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; preview shield decision reference | PASS | `internal/model/interaction_guard.go` SHA-256 `207364bfa7c5b82ea81c97dffee886cb7ff33af2c3f08fde917afaae0eb6cf20` | 2026-11-11 06:45 UTC |
| E086 | T105 | `ba72ae4` + working tree; spec 1.6.8; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; config CAS precondition | PASS | `internal/model/config_cas.go` SHA-256 `244afce261376390fc810f83ce707c41a8158697fa62b4d619e75f983a5b7396` | 2026-11-11 06:46 UTC |
| E087 | T105 | `ba72ae4` + working tree; spec 1.6.8; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; mutation replay identity | PASS | `internal/model/mutation_replay.go` SHA-256 `fe62b29be34831c509ff59b54a530fc9b51bafd31e3e48e529a1d9c5fad3c3bf` | 2026-11-11 06:47 UTC |
| E088 | T105 | `ba72ae4` + working tree; spec 1.6.9; hashes trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check exit 0; all 23 bridge directions | PASS | `internal/model/bridge_direction.go` SHA-256 `867c4c78a49a7eca0f61fa44b1a4165a563aae94bb6188d7c74828fbbebe9606` | 2026-11-11 06:48 UTC |
| E089 | T105 | `ba72ae4` + working tree; source manifest digest trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | gofmt, go mod verify, Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check, registry-pack fixture exit 0 | PASS | `tests/contract/registry_flow_test.go` SHA-256 `4019acc85f50dc08d0daa15c71945e221fafadbbb9c267df66e10d478a4eaf29` | 2026-11-11 06:49 UTC |
| E090 | T105 | `ba72ae4` + working tree; source manifest digest trong ghi chú T105 | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Python; offline snapshot | gofmt, go mod verify, Python vectors, `go test -count=1 ./...`, `go vet ./...`, bootstrap build, spec_check; multi-file rename/scope/incarnation fixtures | PASS | `internal/model/rename_large_test.go` SHA-256 `3ec6b9ca2378105747f02081149d39f8e4957964f38b47379f402ddc4518c566` | 2026-11-11 06:50 UTC |
| E091 | T105 | `ba72ae4` + working tree; source manifest như E090; spec sau chuyển DONE | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; offline snapshot | `go run ./tools/spec_check` exit 0 sau cập nhật tracker/batch; G0 chưa PASS | PASS | `tools/spec_check/main.go` SHA-256 `cdda8352e826c42e5425176eae34e2fbfdb570686752f8e813e97bb354afc20c` | 2026-11-11 06:51 UTC |
| E092 | T106 | `ba72ae4` + working tree; 243 source files, manifest SHA-256 `2ee2ae6eb7f08d33889dc63821663bc0da152fe1f8c44c41a91144675fda51b6` (không gồm spec); so byte workspace/snapshot | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; modernc.org/sqlite v1.62.0/SQLite 3.54.1; fixture DB tạm, offline | migration parity, storage SQL fixtures, `go test -count=1 ./...`, vet, bootstrap build, spec_check, 14 Python vectors, go mod verify | PASS | `tests/contract/storage_gc_test.go` SHA-256 `71e29caadbac4915a62418464199f9dacc63c16e2ac4305c4aa42d8cfe8f10bd`; `tests/contract/storage_migrations_test.go` SHA-256 `a6afaea5b9eecf8cde22d9c7ef5b54aef4beb9a8e1fd8936a0698bae00dd568d` | 2026-11-11 06:56 UTC |
| E093 | T106 | `ba72ae4` + working tree; 244 source files manifest SHA-256 `388c1f62021667870091f1ddb0ca6563551c7def908c466c352bf65ba9e5fa8f`, excluding spec; workspace/snapshot bytes matched | Ubuntu 26.04 WSL2 x86_64, Go 1.27.4, pinned modernc SQLite 3.54.1, offline | Full go test/vet/bootstrap build/spec_check/mod verify and 14 Python vectors PASS; SQL fixture replay, concurrent CAS (10 runs), incarnation, cycle/report and metadata guards PASS | PASS | `tests/contract/storage_gc_test.go` SHA-256 `144f0f1a6b7af90ba51cfbd9bdf645330b281589681114f8ad7c144a9bba60c6`; `tests/contract/storage_identity_test.go` SHA-256 `e7e008e4ac21b37d3cef5b0990e90deb1c04b251ff150a62f756569c05a3ce8b`; `tests/contract/storage_migrations_test.go` SHA-256 `495b302127c7d841ae19f5bf06f1779766d921355883a11d25f445e2a64bd60f` | 2026-11-11 06:57 UTC |
| E094 | T107 | `ba72ae4` + working tree; 247 files manifest `00bdcdd09a67286186384a8527ee7c49678345d6e4f29dce7e023efe67ddf000`, không gồm spec; workspace/snapshot bytes khớp | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; Git isolated temp HOME/template; offline | Contract fixture budget/cleanup/finalization, Git stage/ignored/secret-marker/tree mutation/retry/local bare remote/clone; full test, vet, bootstrap build, spec_check, Python vectors, mod verify | PASS | `tests/contract/git_workflow_test.go` SHA-256 `881d745e1ef3b1ca58b61bf40ff0926f3f960edbd16756036e8d297cdacf5d5a`; `run_budget_test.go` `c9f57204d2fe3c0d9e10aa81fe43c1a86d0930107d27fd4532da09b712318125`; `run_finalization_test.go` `b95878a9e854769670af0753f92861ccc50dc98eabb085bf5cbc875da03a31a1` | 2026-11-11 06:59 UTC |
| E095 | T107 | `ba72ae4` + working tree; 249 files manifest `a78a3426ab30d42011c8ec87aaa0cb5b8b181e9ef1be22d5d0d7db0c9f47907c`, không gồm spec; workspace/snapshot byte parity | Ubuntu 26.04 WSL2 x86_64; Go 1.27.4; pinned MCP SDK 1.8.0; local Git fixture; offline | Full test, vet, bootstrap build, spec_check, Python reference, mod verify; MCP dual near-cap SDK roundtrip, Go load identity/source disagreement, Git deletion/mode/hook/retry/restore/diverged remote | PASS | `tests/contract/mcp_budget_test.go` `0efc10f8e5b8cf172b1e4c4fbf85fad081c2d37b2542653d644b62d12ae991a7`; `provider_load_test.go` `5a4d82c8b6da5dbcffd8e18fafc357ae1c704cb7e0fd41ac7e6573b899c4bdea`; `git_workflow_test.go` `d992272ed2cd130d1f800f861e39b7b837b6779ed957545f168ec1044d2342ee`; `run_finalization_test.go` `2dbd3cf222d477d72ff831058840061800f41597ea3dce190f68b5533584c94b` | 2026-11-11 07:01 UTC |
| E096 | T107 | `ba72ae4` + working tree; 250 files manifest `99b81772b634796a385231401c5ba79398684fdfe190ce21c3da3bb49954ba71`, không gồm spec; Windows/WSL snapshot bytes khớp | Go 1.27.4 Ubuntu WSL2; Python reference; Node 24.22.1 Windows; offline | Full Go test/vet/bootstrap build/spec_check/mod verify; 14 vectors Python và Node exact bytes+SHA256; fake volume aggregation | PASS | `tests/contract/canonical_reference.cjs` SHA-256 `acbf0926b69e87b12593e13ec446bc304028eed515c2f3ea0de0d1641403bc56`; `run_budget_test.go` SHA-256 `be9e9acd1dbd541753e1bb0cc52e8ff8469eae514d2208b21140c058074e2c66` | 2026-11-11 07:02 UTC |
| E097 | T107 | `ba72ae4` + working tree; 251 files manifest `47d65446744057d9933bb8545d5831c2b2e7add589bb74d97df4cec102b65d80`, không gồm spec; workspace/snapshot byte parity | Ubuntu 26.04 WSL2, Go 1.27.4, Node 24.22.1 Windows, local Git fixture | Full Go test/vet/bootstrap build/spec_check/mod verify, 14 Python/Node vectors; provider version/sequence/input, Git missing material, scope-limited admission | PASS | `tests/contract/provider_flow_test.go` `6db9c42a4d23702b86e850a7e60c14332c6ad77b8a87595c9c4ea7cc184312c4`; `git_workflow_test.go` `11017ef30d511be20493fe2712c5e085dfb59e7a7e4fefa10ee3636ecf17f9b4`; `run_budget_test.go` `c9332d62abb251956b7ba47891469e6102981bf259cbd5ead806204f0a27a6d4` | 2026-11-11 07:03 UTC |
| E098 | T003 | Rework tại T108; 251 files manifest `fc17466ec4cb165fbb9d6e2d24937a896bcf9faede1d088443549feb23501277`, không gồm spec; workspace/snapshot byte parity | Ubuntu 26.04 WSL2 Go 1.27.4; Node 24.22.1 Windows; offline | Validator trước-gate/dependency và prefix batch B001; mutation bỏ readiness call làm test FAIL đúng rồi restore exact; full test/vet/build/spec_check, Python+Node vectors, mod verify PASS | PASS | `tools/spec_check/main.go` SHA-256 `9ae48dc03e8fd35474fe8be2294b95712fc5f54a9aae985459a0152f6d32d9d2`; `main_test.go` SHA-256 `d5e8bf8976d83e6bed256de648d1f116f56bd41ebdbc73cd26c310f066204b35` | 2026-11-11 07:06 UTC |
| E099 | T003 | Rework T108; source manifest `2413ce003af843e575c63f233383879c4613baed5b4bb75e4e212a9a7b5b1185`, 252 files excluding spec, byte parity | Ubuntu 26.04 WSL2 Go 1.27.4 offline | Validate previous gates/dependencies and B001 prefix/next claimed task; full combined Go test/vet/bootstrap build/spec_check/mod verify PASS | PASS | `tools/spec_check/main.go` `271a537e1beb035478a3a1d600f51a876e5f5f39733652ef5c79315d4805039d`; `main_test.go` `f78d6edaab7f7e922313cc99962470bb38c999a6f22335f704927b087705d10e` | 2026-11-11 07:08 UTC |
| E100 | T107 | Reopened T107; cùng manifest E099 | Ubuntu 26.04 WSL2 Go 1.27.4; Python; Node 24.22.1 Windows | Deterministic pairing/arming reducer, invalid/late ack + shield/invalidation; full combined Go test/vet/bootstrap build/spec_check/mod verify, 14 Python/Node vectors PASS | PASS | `tests/contract/preview_pairing_reducer_test.go` SHA-256 `84f76e934b45a8b86c63297722af797908482c391cc4e8e19c604cdb94e22f27` | 2026-11-11 07:08 UTC |
| E101 | T108 | `ba72ae4` + working tree; 252 files manifest `e529c8287c717d98e30d9b984910d88fdedc6adbc517a6f46611d18ae68fd3fb` excluding spec; workspace/snapshot parity | Ubuntu 26.04 WSL2 Go 1.27.4; Node 24.22.1 Windows; Git local baseline; offline | Self-review S41–S53, policy hashes/pins/license scope, 108 task/cards/deps/registry/DDL via checker; G0 rejects each unfinished foundation task, B001 and predecessor gate tests; combined test/vet/gofmt/bootstrap build, vectors, mod verify PASS | PASS | `tools/spec_check/main.go` `271a537e1beb035478a3a1d600f51a876e5f5f39733652ef5c79315d4805039d`; `main_test.go` `e4c31020b61bb0b746794a03c5d75404ce4036251f85890498ce84075628d5ef` | 2026-11-11 07:09 UTC |
| E102 | T005 | `ba72ae4` + working tree; 261 files manifest `fa19b498bc052e84012bda278094d3f5cbc28ef5382efa933002cf36af8a7619`, excluding spec; workspace/snapshot byte parity | Ubuntu 26.04 WSL2 x86_64 kernel 6.18.58.1; Go 1.27.4; isolated synthetic fixture; offline | Linux openat2/O_PATH+verified procfs regular-file open, symlink/traversal/fifo/swap race, deny+bounded scanner and clean/secret fixture; full test/vet/gofmt/bootstrap build/spec_check, Python+Node vectors, mod verify | PASS | `internal/paths/open_linux.go` `9fd8004c0e8de59b4a0406f607f0a527facdd8d6803e1645cd7a0a2e9fbd4401`; `internal/policy/source.go` `65b7020f8f07776560530c30bcfb7917ecfacb43c533aeea03135eff9fa022af`; `internal/policy/read.go` `c735982a1da06b02b90dc2cf57632bc2ab30053aa3ad8b2f0181959c7f896df1` | 2026-11-11 07:12 UTC |
| E103 | T005 | 262 files manifest `7bd461803666e49d37d28b3865825378c38f92a0b45fdcf129a743edce7ffbff`, excluding spec; workspace/snapshot byte parity | Ubuntu 26.04 WSL2 Go 1.27.4; SQLite synthetic sinks; Node Windows 24.22.1 | Full Go tests/vet/bootstrap build/spec_check/mod verify, Python/Node vectors; synthetic blob/FTS/log/output secret marker check; Windows paths test cross-compiled but not run | PASS | `internal/policy/sinks_linux_test.go` SHA-256 `64687b7bf9b5676516af5426bb6b64eced0e479096f671adf9d9266e7bc81f43` | 2026-11-11 07:14 UTC |
| E104 | T005 | 262 files manifest `b69bb04c521d746f751118f28ea6e125e973ef9883805a359819e5b2f3b58000`, excluding spec, workspace/snapshot parity | Ubuntu 26.04 WSL2 Go 1.27.4; Node Windows; isolated fixture | Full test/vet/gofmt/bootstrap build/spec_check/mod verify, Python/Node vectors; relative-symlink and content-guard mutations detected after test enhancement, exact source restored | PASS | `internal/paths/open_linux_test.go` `691a0aa47c667f981c4c03cd4bb9bf563080d91263b1d00cdb0951480b746835`; `internal/policy/read_linux_test.go` `db97a1b3c20067819f1aaed8bc9c7a786c4bd88af15f2a83f67eb7fc225dce48` | 2026-11-11 07:15 UTC |
| E105 | T005 | Source manifest E104, synthetic fixtures only | Ubuntu WSL2 kernel 6.18.58.1 x86_64; fixture TMPDIR trên Windows-mounted DrvFS; Go 1.27.4 | `go test -count=1 -v ./internal/paths ./internal/policy`: policy fixtures PASS; paths suite FAIL vì FIFO creation operation not supported và concurrent rename permission denied | FAIL | `artifacts/preflight/_t005_drvfs_fixture/` owned fixture root; raw source/user data không được dùng | 2026-11-11 07:16 UTC |
| E106 | T005 | 262 files manifest `99a36e2000d0ed4883d0515469858a9bec88f68040dadf07ab9d2c283f8be17c`, excluding spec; workspace/snapshot parity | Ubuntu 26.04 WSL2 Go 1.27.4; Linux filesystem và Windows-mounted DrvFS, synthetic fixtures | Linux combined tests/vet/gofmt/build/spec_check/vectors/mod verify PASS; DrvFS symlink race và policy tests PASS sau bounded rename retry, FIFO fixture SKIP do filesystem không hỗ trợ | PASS | `internal/paths/open_linux_test.go` SHA-256 `d7a412e3181f2c3a3469d13ca5b5d5ffa12c181253389c99eb0ccac6834a4beb` | 2026-11-11 07:17 UTC |
| E107 | T005 | 263 files manifest `90aac2db43921349b0ba2679ed41008869e87baeebd2970c8035abe19650422b`, excluding spec; byte parity | Ubuntu 26.04 WSL2, Go 1.27.4, isolated Git fixture | Synthetic repository fsmonitor canary fires with unsanitized config and does not fire with explicit override; full Go tests/vet/gofmt/bootstrap build/spec_check/mod verify and Python/Node vectors PASS | PASS | `internal/policy/git_fixture_linux_test.go` SHA-256 `85dc117c841dad1a2010130cb83cc07284188dcb40784d78e6e0c08e701c6096` | 2026-11-11 07:18 UTC |
| E108 | T005 | Linux Git inventory adapter in WSL fixture before root-identity fix; repository entirely synthetic | Ubuntu 26.04 WSL2 Go 1.27.4; Windows-mounted DrvFS | `go test -count=1 -v ./internal/runtime`: fsmonitor/budget case PASS; anchored directory replacement returned empty inventory instead of expected original content | FAIL | `internal/runtime/fs_git_linux_test.go` fixture; product/root replacement semantics chưa verified | 2026-11-11 07:20 UTC |
| E109 | T005 | 267 files manifest `7c8a9291363a52806b73aba0806daa7d2e2592a8eb9ed76c12240810be57f185`, excluding spec; workspace/snapshot byte parity | Ubuntu 26.04 WSL2 kernel 6.18.58.1, Linux FS and DrvFS; Go 1.27.4; fixture Git | Root symlink/recreated inode and change during Git command rejected; sanitized Git inventory/limits/fsmonitor; targeted mutant removing post-check detected and restored; combined test/vet/gofmt/bootstrap build/spec_check, cross-compile Windows stub, vectors/mod verify | PASS | `internal/runtime/fs_git_linux.go` `a117385ff960952ed0d3dd88b19831ff5bc6896eb9cbb1b6de1e6b219eb58b4c`; `internal/paths/open_linux.go` `72df199e7b20ff026e61d2a7446e7c196768e4a5f015065439d9023a4b45116a` | 2026-11-11 07:22 UTC |
| E110 | T005 | 267 files manifest `62ac4ec103c28758a0a7cf10f03edad9ced08b6e3f76c474d61c91528fe1aded`, excluding spec; workspace/snapshot bytes matched | Ubuntu 26.04 WSL2 Linux FS + DrvFS; Go 1.27.4; Node 24.22.1 Windows; offline | Combined tests/vet/gofmt/bootstrap build/spec_check/mod verify and Python/Node vectors PASS after direct dependency declaration; Windows stub cross-compile only; DrvFS FIFO SKIP | PASS | `go.mod` SHA-256 `40a4a1b2cf8df1f9134c7f9bd91374e5ad8807a38dc0ad77988ab5440578dca6`; `internal/runtime/fs_git_linux_test.go` `b789805a0d1310759670b1f44f632d63f97a54dde105fd42312d79f4fdd3d398` | 2026-11-11 07:24 UTC |
| E111 | T005 | Linux Git adapter `internal/runtime/fs_git_linux.go` SHA-256 `a117385ff960952ed0d3dd88b19831ff5bc6896eb9cbb1b6de1e6b219eb58b4c`; fixture hoàn toàn tự tạo | Ubuntu WSL2 Go 1.27.4, Git 2.53.0, offline | `TestGitInventoryRejectsWorktreeRedirection`: core.worktree trỏ ngoài root làm adapter trả tên file ngoài root | FAIL | `internal/runtime/fs_git_worktree_linux_test.go`; chưa chạy trên target user | 2026-11-11 10:19 UTC |
| E112 | T005 | Fixture Git độc lập sau chặn core.worktree; target user không dùng | Ubuntu WSL2 Go 1.27.4, Git 2.53.0, offline | `TestGitInventoryRejectsSymlinkedMetadata`: .git symlink ngoài root làm lộ tên file tracked từ index ngoài | FAIL | `internal/runtime/fs_git_worktree_linux_test.go` | 2026-11-11 10:21 UTC |
| E113 | T005 | Git inventory fixture, no user target; core.worktree guard applied | Ubuntu WSL2 Go 1.27.4/Git 2.53.0, offline | `TestGitInventoryRejectsSymlinkedIndex`: .git/index symlink tới index ngoài làm lộ tên file tracked | FAIL | `internal/runtime/fs_git_worktree_linux_test.go` | 2026-11-11 10:21 UTC |

| E114 | T005 | 268 files manifest `c395c51675a7ab45b44954148f3b54d761f131da2c50bfd58d13c642896463aa`, selected internal/contracts/tests/cmd/tools/docs/go.mod/go.sum, workspace và WSL snapshot byte parity; không gồm spec | Ubuntu WSL2 Linux FS, Go 1.27.4, Git fixture synthetic; Node Windows; offline | Root thay trong lúc đọc bị từ chối; metadata Git chưa verified từ chối và không thực thi fixture binary; tests/vet/build/spec_check/mod verify, Python/Node vectors PASS; mutation post-read check và fail-closed bị phát hiện và restore; 2 Git functional tests SKIP | PASS | `internal/policy/read.go` SHA-256 `4122d7778605db11df7b05f5e5d0d496cfb6aae3c8619464be1cbbc4378a768a`; `internal/runtime/fs_git_linux.go` `1902c63895a36069bf51f72d37599cbf995bfde33dba73679c6dc85a2d814bcc`; scope chỉ fail-closed, không chứng minh Git inventory hoạt động | 2026-11-11 10:24 UTC |

| E115 | T005 | 274 files manifest `5eb087161364782057254da820bfe1d786a8f07763f8d1aef7afa093f715a975` (internal/contracts/tests/cmd/tools/docs/go.mod/go.sum), workspace và WSL snapshot byte parity; không gồm spec | Ubuntu WSL2 Linux FS, Go 1.27.4, Git fixtures riêng; Node Windows; offline | Bounded index read qua metadata root explicit; fixture linked worktree Git thực, reject symlink/root changed/hardlink và oversized; cleanup file inspection chỉ đọc, reject traversal/symlink; targeted hardlink mutant bị phát hiện và restored; Go tests/vet/build/spec_check/mod verify, Python/Node vectors PASS; Git inventory functional tests 2 SKIP; Windows runtime cross-compile only | PASS | `internal/runtime/fs_git_metadata.go` SHA-256 `1d0b851ad205d45180ee6f9933c250cac51bc36c80d2c1a58568da6fd7c64fd3`; `internal/runtime/fs_cleanup.go` `b6f4afe6f0c36c6baf447979956d05eb3d7a0b2b268e316c12ba5d3fb4689fda` | 2026-11-11 12:12 UTC |

| E116 | T005 | 274 files manifest `1fac227dcd8583134b9b73e16d9fb4dfd1917b52510acdc04580a57b4633a7c0`, workspace/snapshot byte parity, selected paths như E115 | Ubuntu WSL2 Linux FS, Go 1.27.4; isolated fixture | Focused cleanup report tests PASS; thay file bằng file khác cùng path/size/mode tạo report giống nhau, cả hai nội dung fixture được giữ; đây là counterexample cho dùng report làm authority | PASS | `internal/runtime/fs_cleanup_linux_test.go` SHA-256 `5cc153a74891c66f0ad828697a95aad22230aca580374c4b84120938ef57600d`; không kiểm action-time deletion | 2026-11-11 12:12 UTC |

| E117 | T005 | Linux adapter trước hardlink guard; fixture tạo hardlink giữa synthetic outside file và source path trong root | Ubuntu WSL2 Linux filesystem, Go 1.27.4; isolated fixture | `TestReadSourceRejectsHardLinkToOutside` trước sửa trả bytes và nil error | FAIL | `internal/policy/read_linux_test.go`; không sử dụng source người dùng | 2026-11-11 12:53 UTC |
| E118 | T005 | 274 files manifest `1989185b1528c19373db863f96258ae141618ee27da9bf2ad89594e70c3bb1e6`; workspace/WSL snapshot parity, selected paths như E115 | Ubuntu WSL2 Linux FS, Go 1.27.4; fixture synthetic; Node Windows; offline | Reject hardlink khi open và sau đọc; test hardlink tạo trong read ở boundary có điều khiển; post-read guard mutant bị phát hiện và source restored; full Go tests/vet/build/spec_check, Python/Node vectors, mod verify PASS; 2 Git inventory tests SKIP; Windows cross-compile only | PASS | `internal/paths/open_linux.go` SHA-256 `f6271c7f90da26eaac0884a9f24b854c9428aba819cf824bbaa937e27ae8d146`; `internal/policy/read.go` `cb392fc900bd668121f7e0559a6b77f7ecda6d2f6ab0fc53899482b84e620128` | 2026-11-11 12:53 UTC |

| E119 | T005 | Intermediate metadata index read với check inode/size/mtime, trước check inode change time | Ubuntu WSL2 Linux FS, Go 1.27.4; fixture synthetic | Sửa index cùng length và khôi phục mtime trong lúc đọc khiến bytes được trả | FAIL | `internal/runtime/fs_git_metadata_linux_test.go` `TestReadGitIndexRejectsChangeDuringRead/modify-restore-mtime` | 2026-11-11 13:18 UTC |
| E120 | T005 | 274 files manifest `cb4be5b09a57cd9c1ec329ea8f1fa0e37f2cb112063cca33f26ef06730b87570`; workspace/WSL snapshot parity, selected paths như E115 | Ubuntu WSL2 Linux FS + DrvFS, Go 1.27.4; fixture Git; Node Windows; offline | Index bị replace, modify và modify-restore-mtime trong lúc đọc đều rejected trên Linux FS và DrvFS; mutation bỏ ba guard bị tests phát hiện và restored; Go full test/vet/build/spec_check/mod verify, Python/Node vectors PASS; Git functional tests 2 SKIP; Windows cross-compile only | PASS | `internal/runtime/fs_git_metadata.go` SHA-256 `edbac96da05dee44b38735b6a31518ed8d33ed49d486e5a192601407488b622e`; `internal/runtime/fs_git_metadata_linux_test.go` `5fa779e07505969ae5adb609e7740c7dfdc7dfda1d2abc5bea5e1325ae3dbb33` | 2026-11-11 13:18 UTC |

| E121 | T005 | Guard cleanup intermediate dùng ancestor handles/mount IDs, trước kiểm mount lồng | Ubuntu WSL2 Linux FS; synthetic bind mount trong user+mount namespace riêng, mount propagation private | Output root tách source theo đường dẫn nhưng chứa bind mount tới source; guard trả nil | FAIL | `internal/runtime/fs_cleanup_guard_linux_test.go` `TestCleanupRootRejectsNestedBindMount`; không sửa mount namespace của host | 2026-11-11 13:39 UTC |
| E122 | T005 | 279 files manifest `ce155a4396359a2d7bcaf9fe3ea912cfe8b6e8a2b0bfc456fa2e8d2b2c03126b`; workspace/WSL snapshot parity, selected paths như E115 | Ubuntu WSL2 Linux FS + DrvFS; Go 1.27.4, real linked Git worktree fixture; Node Windows; offline | Open child dir anchored/no symlink; reciprocal gitfile/gitdir/commondir, tamper/symlink rejection; cleanup overlap, replaced root, different mount, nested bind trong namespace riêng, space-escaped mount path; 7 targeted guard mutations detected/restored; full Go test/vet/build/spec_check/mod verify, Python/Node vectors PASS; Linux concurrent dir exchange PASS, DrvFS exchange SKIP; Git inventory 2 SKIP; Windows cross-compile only | PASS | `internal/runtime/fs_git_binding.go` SHA-256 `0fa300bb160c454ffc3d1ad4210698d057da52737e9f74e0367ec9d762de5386`; `internal/runtime/fs_cleanup_linux.go` `acaae79a177706208398761f7b6a3c56d809ed15782325fa1f8ecfb2730c5b83` | 2026-11-11 13:39 UTC |

| E123 | T005 | Source snapshot trước khi ghi E123; base `ba72ae4`, spec tại E122 SHA-256 `559b77d00fa38d9aef6d28a3ee17ee12a06e81ad75f990f483f226bb069e5e68` | WSL Linux owned snapshot/restore riêng; Go 1.27.4 offline; không target repo khác | 282 files, 1729463 bytes; SHA-256 từng file khớp source/snapshot/restore; restore bootstrap build/spec_check và paths/policy/runtime tests PASS; 2 Git inventory tests SKIP | PASS | WSL state `snapshots/t005-20261111-1339-e122/`, manifest SHA-256 `8a5bf6eca923134f336c01dcc80374f3907bfeb8e16341d4f6347597bd254610`; không gồm `.git`, `.vscode`, artifacts hoặc runtime DB; không phải Git checkpoint | 2026-11-11 13:40 UTC |

| E124 | T005 | 279 files manifest `9932dc6f63d0a703afeaff4137c7f8e141dcc692b0797dab7c12d1ad12a104d4`; workspace/WSL snapshot parity, selected paths như E115 | Ubuntu WSL2 Linux FS, Go 1.27.4; namespace fixture synthetic, offline | Mount helper refuses original namespace before mount; distinct namespace bind fixture PASS; full Go test/vet/build/spec_check/mod verify và Python vectors PASS; Git inventory functional tests 2 SKIP; Windows cross-compile only | PASS | `internal/runtime/fs_cleanup_guard_linux_test.go` SHA-256 `2663c42fa383c3a66f736438c566e9ddb9028aa05eb0272982ea10c2867c1e8d`; snapshot E123 trước thay đổi test này | 2026-11-11 13:41 UTC |

| E125 | T005 | Source snapshot trước E125; base `ba72ae4`, spec tại E124 SHA-256 `48b1b24f6e0938684fb3e4d022bc8b2f2e5bdd9f5309b9664a11f8b405f4c9d8` | WSL Linux owned snapshot/restore riêng; Go 1.27.4 offline | 282 files, 1733073 bytes; source/snapshot/restore SHA-256 từng file khớp; restore bootstrap build/spec_check, paths/policy/runtime tests PASS; Git inventory 2 SKIP | PASS | WSL state `snapshots/t005-20261111-1341-e124/`, manifest SHA-256 `b0922d5125b8e8ee9c6fd51cdb326a9ba7395905d99e8206f81d8df719e476ac`; không gồm `.git`, `.vscode`, artifacts, runtime DB; không phải Git checkpoint | 2026-11-11 13:42 UTC |

| E126 | T005 | 280 files manifest `77bff0aacb922f8093634123b3413411c6c997e27382723c6c4429c9b604a80e`; workspace/WSL snapshot parity, selected paths như E115 | Ubuntu WSL2 Linux FS + DrvFS; Go 1.27.4; synthetic owned fixture; Node Windows; offline | Combined real filesystem integration: local Git metadata bounded read, clean source, deny secrets and outside hardlink, protected-root cleanup guard, fail-closed unverified Git; source/records remain unchanged; full Go test/vet/build/spec_check/mod verify, Python/Node vectors PASS; 2 Git inventory functional tests SKIP; Windows cross-compile only | PASS | `internal/runtime/fs_guard_integration_linux_test.go` SHA-256 `a8c98aceeac28d93ab17fb664d1ae8876122b4961796c05a3230791893353c0c`; scope T005 primitives only | 2026-11-11 15:27 UTC |

| E127 | T004 | Rebind fixture trước khi đóng old root/common directory handles | Ubuntu WSL2 Go 1.27.4; Windows-mounted DrvFS; synthetic Git repo | Rename báo thành công, mở root mới trả ENOENT khi old handles vẫn mở; tái hiện lần hai; probe độc lập cho thấy stat mới có thể ENOENT trong khi open thành công | FAIL | `internal/catalog/registration_linux_test.go` `TestRebindMovedWorktreeWithoutChangingSource`; chưa phân loại rõ product/filesystem | 2026-11-11 15:38 UTC |
| E128 | T004 | 294 files manifest `ff14b82ecfe0bbaf809762f0890c3c8634f3ab25ad50ce807a6201aba37b4ae2`; workspace/WSL snapshot parity, selected paths như E115 | Ubuntu WSL2 Linux FS + DrvFS; Go 1.27.4, Git fixtures, SQLite; Node Windows; offline | Frozen migration embed; catalog incarnation/schema guard, unknown/newer/tampered schema, concurrent connections, rollback; repo+linked registration, policy/profile CAS, remove and same-repo rebind; full Go test/vet/build/spec_check/mod verify, Python/Node vectors PASS; DrvFS tests PASS khi đóng old handles trước rename; 9 guard mutants detected/restored; Git inventory 2 SKIP; Windows cross-compile only | PASS | `internal/catalog/registration.go` SHA-256 `5236698f8aa49ed9811ff1477e64d8b4888a06d1c81ae3a43365516bb094b41b`; `internal/catalog/rebind.go` `00d048e632956c502a288989b1ec6e1a01e9b5ddbb9c57bdee0e0f54aa2f8c0d` | 2026-11-11 15:38 UTC |

| E129 | T004 | Source snapshot trước E129; base `ba72ae4`, spec tại E128 SHA-256 `d9ce08c4abf77c5fae2d995d8567a713bf8c8f7c3c5ce71557b8241d2b184595` | WSL Linux owned snapshot/restore riêng; Go 1.27.4 offline | 297 files, 1804061 bytes; SHA-256 từng file khớp source/snapshot/restore; restore bootstrap build/spec_check, catalog/paths/policy/runtime tests PASS; Git inventory 2 SKIP | PASS | WSL state `snapshots/t004-20261111-1538-e128/`, manifest SHA-256 `9d9caf2021229a041c479840731939e72f0d103c365a0a777e62e723452e4462`; không gồm `.git`, `.vscode`, artifacts, runtime DB; không phải Git checkpoint | 2026-11-11 15:39 UTC |

| E130 | T004 | Config fixture đầu tiên sau khi thêm yêu cầu state root owner-only | Ubuntu WSL2 Linux filesystem; Go 1.27.4; fixture riêng | `TestRuntimeConfigShowAndValidateAreReadOnly` FAIL vì `testing.TempDir` tạo child 0755; fixture được sửa tạo state con 0700 rồi chạy lại, không nới permission guard | FAIL | `internal/app/config_linux_test.go`; thất bại fixture, không là kết quả của revision E131 | 2026-11-11 15:50 UTC |
| E131 | T004 | 307 files manifest `d0fb756bfc551ea0ed9b6f0a0780212a80b4d11d2fa1712ae38823fda6c2c696`; workspace/WSL snapshot parity, paths như E115 | Ubuntu WSL2 Linux FS + DrvFS; Go 1.27.4, SQLite/Git fixtures; Node Windows; offline | Config show/validate không ghi, file CAS/lock và startup immutable, hai process một winner/one conflict 10 lần; linked rebind confirmed + exact revision/affected-worktree list; group read bounded; independent SQLite connections một winner và loser có thể BUSY; 8 mutants detected/restored; full Go test/vet/bootstrap build/spec_check/mod verify, Python/Node vectors PASS; Git inventory 2 SKIP; Windows cross-compile only | PASS | `internal/catalog/rebind.go` SHA-256 `fe81d4606c6fb75ea78055953176662ee5bdd912053441de2d9de0f65c6e19cd`; `internal/runtime/config_set_linux.go` `c3436e0541b1f84098215063597a66bd5d19040c58e6ed77945e96d0cb800639`; `internal/app/config.go` `6564ad2f07fc535ce765c61d912ae8ea25179e5a74ab837e0b2499d4b5d68326` | 2026-11-11 16:06 UTC |

| E132 | T006 | Fixture đầu tiên kiểm giữ nguyên mtime/size trên DrvFS | Ubuntu WSL2 Go 1.27.4; Windows-mounted DrvFS | Không tái lập được mtime bằng timestamp ban đầu, nên oracle fixture FAIL; sau đó fixture đặt timestamp nguyên giây trước lần chụp và xác minh chính xác trước/sau | FAIL | `internal/inventory/capture_linux_test.go` `TestCaptureHashesActualBytesEvenWhenMtimeAndSizeMatch`; chưa kết luận nguyên nhân filesystem | 2026-11-11 16:10 UTC |
| E133 | T006 | Fixture thay đường dẫn trong khi đang đọc, trước sửa phân loại inode đã unlink | Ubuntu WSL2 Linux FS; Go 1.27.4 | Bytes không bị trả ra nhưng adapter báo hard-link error thay vì changed/retry; thêm nhận diện link-count 0 trước/sau read, vẫn từ chối hard-link thật | FAIL | `internal/inventory/capture_linux_test.go` `TestCaptureRejectsPathReplacementDuringRead` | 2026-11-11 16:12 UTC |
| E134 | T006 | 317 files manifest `2c2cefb37f4944b5a052d251a57c0149430929c6ade5591c5147b70feacafdd8`; workspace/WSL snapshot parity, paths như E115 | Ubuntu WSL2 Linux FS + DrvFS; Go 1.27.4; file fixtures; Python/Node reference; offline | Secure capture/deny/secret/current-authority callback, unchanged mtime+size, changed/replaced/unlinked read, tối đa 2 retry; snapshot input immutable/aggregate cap/raw SHA256, canonical digest khớp Python độc lập, parse outcome không đổi digest; UTF-8/16/32 range+CRLF/surrogate; 9 targeted mutants detected/restored; full Go test/vet/bootstrap build/spec_check/mod verify và 14 Python/Node vectors PASS; Git inventory 2 SKIP; Windows cross-compile only | PASS | `internal/inventory/capture_linux.go` SHA-256 `69aac300247e314f0858928f2c277492257c65763b2f43a3355ae45f1a1cc81d`; `internal/snapshot/manifest.go` `77d34e40328fe4bb6a8cdcdcee55392e4fc3b8e9736873abb31b6ecd2a4dd533`; `internal/snapshot/position.go` `4eb73b00265472523b159873592fad3b614822f37dc79651323b6349b051b1a8` | 2026-11-11 16:14 UTC |
| E135 | T006 | 317 files manifest `a5fdf7c08bbed833f40ba6062472e3b109cede9abba54d43f1dcfc9784f67e78`, paths như E115; không gồm spec | Ubuntu WSL2 Go 1.27.4, Node Windows; fixture/offline | Chuẩn hóa duy nhất CRLF cuối cùng thành LF ở hai script tham chiếu T107 để staged bytes khớp bản test; Python/Node 14 vectors, full Go test/vet/bootstrap build/spec_check/mod verify PASS; Git inventory 2 SKIP; Windows cross-compile only | PASS | `tests/contract/canonical_reference.py` SHA-256 `af95b951dae36026d457644cd402c325cbec1d51d26d2bf0cb36ce4e69a5bd7a`; `.cjs` `1a35c7e4beb0b4944732fce65399f3fa4f214a7c93f4f112e5ec6b65bf3674a6` | 2026-11-11 16:18 UTC |
| E136 | T006 | Interim source snapshot tại spec E135, SHA-256 `bfa880d66e0a6fed73b21e5be14f320c7e83f1259e5905617e5d28728aec8c47`; base `ba72ae4` | WSL Linux snapshot/restore riêng, Go 1.27.4 offline | 320 file, 1919482 bytes; workspace/snapshot/restore hash và staged blobs khớp; restore `go test ./...`, vet, bootstrap build, spec_check và Python vectors PASS; 2 Git inventory SKIP | PASS | WSL `snapshots/b001-20261111-1618-precheckpoint/`, manifest SHA-256 `b48cd0eda8843fa177b5d19a3de338b2f2e2bf28c432fef682b42dcbca43b170`; chứa staged/unstaged diffs, không `.git`, `.vscode`, artifacts, runtime DB; không phải commit/tag checkpoint | 2026-11-11 16:18 UTC |
| E137 | T006 | B001 commit `9458db6f082637284c828b46ac0a50507562e848`, parent `ba72ae4c59e65708812a9f964e30ae0fdbbc90d6`, tree `02e9949a6fa81037536d3a5e0268c78c72b30f3a` | Ubuntu WSL2 Go 1.27.4; detached worktree từ đúng commit; Node Windows; offline | 320 file blobs khớp commit; full Go test/vet/bootstrap build/spec_check/mod verify, Python+Node vectors PASS; annotated tag B001 trỏ đúng commit và ghi restore; Git inventory 2 SKIP; Windows cross-compile only; local checkpoint không là independent backup | PASS | tag `mce4d/checkpoints/B001`, tag object `b6edf38a6e293947a0a728768c161c19abddb639`; local restore `checkpoint-restores/b001-9458db6-20261112/` | 2026-11-12 00:35 UTC |
| E138 | T007 | Test source fixture tệp rỗng có slice nil, trước sửa | Ubuntu WSL2, SQLite 3.54.1, Go 1.27.4 | Driver bind nil thành SQL NULL nên vi phạm NOT NULL của content_blobs.bytes; đã sửa và thử lại ở E139 | FAIL | `internal/store/sqlite/index/capture_test.go` TestStoreCapturedBlobsAcceptsEmptyFileAsBlob | 2026-11-12 00:41 UTC |
| E139 | T007 | 329 source files không gồm spec, manifest SHA-256 `62ddb969f7af14c9aa1fd08d7df2959abf64f5657ad4ec22016e4b0791f09a56`; workspace/WSL test worktree byte parity | Ubuntu 26.04 WSL2 Linux FS + DrvFS, Go 1.27.4, SQLite 3.54.1; Node Windows; offline | V1→V2 migration/rollback, incarnation/schema fail-closed, 4 borrowed connections và caps, cùng generation FK, FTS trigger/plan, capture authorization/secret/size/owner/policy và rollback; nil→empty BLOB, existing-content verification; 10 targeted mutants detected/restored; full Go test/vet/bootstrap build/spec_check/mod verify và Python/Node vectors PASS | PASS | `internal/store/sqlite/index/capture.go` SHA-256 `bce25f6e18c25a7e2563812fc263e00a3e89d6aa695d81576d5f126fc6a0a54c`; `initialize.go` `067416327cad97cb58ee7b3db2a98b5ad14af10a14490b00317dd9d0b2dad9e0`; Git inventory 2 SKIP; Windows cross-compile only; OS leases fake | 2026-11-12 00:43 UTC |
| E140 | T010 | Fixture lock directory trên DrvFS hiện hành, không nới permission | Ubuntu WSL2 Go 1.27.4, DrvFS `/mnt/c` | Không xác minh được mode owner-only; adapter trả `ErrLockUnsafe` và không cấp lease; test lifecycle trên filesystem này FAIL, không suy hỗ trợ từ SQLite test E139 | FAIL | `internal/runtime/lock_linux_test.go` TestLifecycleAttachmentsAndWriterScope; WSL Linux FS được kiểm riêng tại E141 | 2026-11-12 00:52 UTC |
| E141 | T010 | 341 source files không gồm spec, manifest SHA-256 `90ec6d8b79d4b705437a874396670f54b9963d67e0ff49cd93286681b18a189f`; workspace/WSL test worktree byte parity | Ubuntu 26.04 WSL2 Linux FS, Go 1.27.4, SQLite 3.54.1; Node Windows; offline | Real flock shared/exclusive, bounded busy, process exit, A/B độc lập, idle pool/borrowed handle, catalog/index owner, writer guard trong transaction, schema/lease rollback và lock inode/permission replacement; 9 mutants detected/restored; full Go test/vet/bootstrap build/spec_check/mod verify và Python/Node vectors PASS | PASS | `internal/runtime/lock_linux.go` SHA-256 `176d9b941fda4ff567e9dae7d7128ccb66993d1d75895393e62a1b2ec5ac592b`; `internal/runtime/lifecycle.go` `9ce4458014b8508b54c3918904c61f1fbdd07315c1ea204cabc49b4969ae4bcf`; DrvFS lock FAIL E140; Windows cross-compile only; race NOT_RUN | 2026-11-12 00:53 UTC |
| E142 | T012 | Fixture thay root khi enumerate trước sửa phân loại | Ubuntu WSL2 Linux FS, Go 1.27.4 | Không trả dữ liệu nhưng phát hiện ctime trước root identity nên trả `ErrWalkChanged` thay vì `ErrRootChanged`; đã sửa thứ tự và kiểm lại E143 | FAIL | `internal/inventory/walk_linux_test.go` TestWalkCandidatesRejectsRootReplacement | 2026-11-12 00:59 UTC |
| E143 | T012 | 349 source files không gồm spec, manifest SHA-256 `0bede505331cc1a9fd576fd838e0f72e02b92d82e371b71abc95adb358907e63`; workspace/WSL test worktree parity | Ubuntu 26.04 WSL2 Linux FS; targeted local/linked index và walk tests trên DrvFS; Go 1.27.4; Git fixture; Node Windows | Git index v2/v3/v4 SHA-1/SHA-256 đối chiếu `git ls-files`, unmerged, sparse-index boundary, checksum và budget; local/linked metadata và authorization recheck; handle-based candidate walk, deny/nested/symlink/hardlink/root+directory replacement; 9 targeted mutants detected/restored; full Go test/vet/bootstrap build/spec_check/mod verify, Python/Node vectors PASS | PASS | `internal/inventory/git_index.go` SHA-256 `f5c8b576750a9228832b8adfe9141f0cb7cbc13060acb559b64ee6893f29ffb3`; `git_source.go` `eee8c3cc3f6e1ac1b87c21d87a54d836230ff983d80725a3463e9e78c5491d01`; `walk_linux.go` `6611f6495f8063fdd67b19db76546784d0b049d0a32a7d0b755e8a055828a6e5`; T012 partial, GitInventory 2 SKIP, Windows cross-compile only | 2026-11-12 01:01 UTC |
| E144 | T012 | Interim snapshot source/spec tại E143, base `9458db6f082637284c828b46ac0a50507562e848`; 350 files, manifest SHA-256 `ce576003e89cd85de92eef20bd82633e23bcbce302b5d656964c3eaf85a93990` | WSL Linux snapshot + detached restore, Go 1.27.4 offline | Hash restore từng file, full Go test/vet/bootstrap build/spec_check và Python vectors PASS; scan chỉ báo ba marker giả trong tests; 2 GitInventory SKIP | PASS | `/home/mce4d/.local/share/mce4d/snapshots/b002-t012-20261112-0102/`; không chứa .git/.vscode/artifacts/runtime DB; restore dùng Git objects/tag và toolchain/cache hiện có, không là backup độc lập | 2026-11-12 01:03 UTC |
| E145 | T012 | Git fixture nested ignore pattern `foo/  ` trước sửa | Ubuntu WSL2 Linux FS; Git fixture synthetic | Quy tắc nested kết thúc bằng khoảng trắng không escape được neo sai so với `git check-ignore`; đã sửa trim theo Git và kiểm lại E146 | FAIL | `internal/inventory/ignore_rules.go`, `TestEvaluateNestedPatternsMatchGit` | 2026-11-12 01:24 UTC |
| E146 | T012 | 367 source files không gồm spec, manifest SHA-256 `9c967996ea097236d5b067b8521605d9fe2c925dc2a2cacc8087eaca666b228e`; workspace/WSL byte parity | Ubuntu 26.04 WSL2 Linux FS; Git synthetic, Go 1.27.4 offline; Node Windows | Real index v2/v3/v4 SHA-1/SHA-256, local/linked worktree, unborn/rename/detached/reset, Git config precedence và ignore đối chiếu Git, sparse/gitlink/nested/exclusion, file map và thay membership/quyền/config; 6 targeted mutants detected/restored; full Go tests/vet/bootstrap build/spec_check/mod verify, Python/Node vectors PASS | PASS | `git_config_gate.go` SHA-256 `0171095305d88cc3e63f882c0fe3fb4ce503863aec488ea2b29c1207672a37eb`; `local_inventory.go` `0f4455c428ad0d44d1f0ce72ced4cb71ef1e09e83e85f6bff2d9f43e66b571f8`; Windows cross-compile only; legacy GitInventory 2 SKIP; runtime discovery/authority chưa wired | 2026-11-12 01:24 UTC |
| E147 | T012 | Tracker ở trạng thái VERIFY, resume vẫn ghi active_task T012 | Ubuntu WSL2; `tools/spec_check` | Validator từ chối active_task không IN_PROGRESS; chỉnh resume về null trong lúc VERIFY và kiểm lại | FAIL | `tools/spec_check/main.go` resume active-task rule | 2026-11-12 01:25 UTC |
| E148 | T012 | Tracker VERIFY sau sửa resume E147, source manifest E146 | Ubuntu WSL2; `tools/spec_check` offline | Kiểm selected tracker/spec PASS; không là product hoặc release verification | PASS | `tools/spec_check/main.go` | 2026-11-12 01:25 UTC |
| E149 | T012 | Interim snapshot source/spec tại E148, B001 `9458db6f082637284c828b46ac0a50507562e848`; 370 files, manifest SHA-256 `a0abd273911c184a533778afcee419b64233dd01f44a9a8d84047af61cf9520a` | WSL Linux snapshot + detached worktree restore; Go 1.27.4 offline | Hash/mode snapshot và hash restore từng file, full Go test/vet/spec_check và Python vectors PASS; source/spec tại thời điểm trước E149 | PASS | `/home/mce4d/.local/share/mce4d/snapshots/b002-t012-20261112-0126/`; không chứa `.git` trong source snapshot, `.vscode`, artifacts hay runtime DB; restore dùng Git objects và cache hiện có, không là backup độc lập | 2026-11-12 01:26 UTC |
| E150 | T008 | Test fixture BeginGeneration đầu tiên dùng DELETE trên generation BUILDING | Ubuntu WSL2 Linux FS, SQLite 3.54.1, Go 1.27.4 | DDL guard từ chối xóa BUILDING (`GENERATION_NOT_RETIRED`); sửa fixture chuyển FAILED hợp lệ trước khi xóa, không thay migration. Shell lần đầu tiếp tục spec_check sau test FAIL nên exit cuối không đại diện test; lần sau dùng `set -e` | FAIL | `internal/store/sqlite/index/generation_test.go` | 2026-11-12 01:35 UTC |
| E151 | T008 | 378 source files không gồm spec, manifest SHA-256 `fed17627cbf5cf7693b5ce8a46c6a44e39b719d8b37fa5b1b4d423fabf0b52fe`; workspace/WSL byte parity | Ubuntu WSL2 Linux FS, Git/SQLite synthetic, Go 1.27.4 offline; Node Windows | BUILDING owner/digest, staging tối đa256 file, rollback khi mất quyền/lease, secret rejection; re-inventory và hash hai lượt; chỉ publish captured-only READY_PARTIAL cùng active pointer, FTS/FK/integrity, reader transaction old/new; integration với OS locks thật và workspace đổi; 6 targeted mutants detected/restored; full Go test/vet/bootstrap build/spec_check/mod verify, Python/Node vectors PASS | PASS | `publish_captured.go` SHA-256 `eb3fc639d66ab0fdcd54d6194b44eae57c363b714103692de3166a62f929ba77`; `read_published.go` `167f2a3281806e7f04078b15d507f0af5f2822bba9deb8cbf5d08a9366baa6c0`; `publish_revalidate.go` `98f15539b1ee5a6df763eacf962244fb894ef6d89124f1416afdcbf1b384a860`; Windows cross-compile only; semantic-fact publish/recovery chưa làm | 2026-11-12 01:35 UTC |
| E152 | T008 | 384 source files không gồm spec, manifest SHA-256 `7215f3132aacfbd17794e12fda04f949d081d551cb82f2295e6f00902f13e531`; workspace/WSL byte parity | Ubuntu WSL2 Linux FS, synthetic SQLite/provider frames, Go 1.27.4 offline; Node Windows | Job/generation/digest binding, thứ tự stream, bounded frame, source range/endpoint/secret/quyền và rollback; Done frame kiểm digest/coverage/source refs; registry bounded và quarantine commit uncertain; reconcile outcome theo job, range validation không copy; full Go test/vet/bootstrap build/spec_check/mod verify, Python/Node vectors PASS | PASS | `stage_provider.go` SHA-256 `dbdf841fb386a61709ae4c3d0abe425d4e42f1010abdd84a08d1aaf0ff3bee3e`; `seal_provider.go` `26f491aaa010564d50fef3cd01b0d3f6a9dd62ed8cddb27bfd63ce94b3b3df62`; Windows cross-compile only; registry mỗi Store, semantic publish chưa triển khai | 2026-11-12 01:43 UTC |
| E153 | T008 | Interim snapshot source/spec tại E152, B001 `9458db6f082637284c828b46ac0a50507562e848`; 387 files, manifest SHA-256 `bbacaac3c54d31ab4cdf3deb619777b10c02e41ce75c6365901524f97fe826a0` | WSL Linux snapshot + detached restore; Go 1.27.4 offline | Hash/mode snapshot và hash restore từng file, full Go test/vet/spec_check và Python vectors PASS; source/spec trước E153 | PASS | `/home/mce4d/.local/share/mce4d/snapshots/b002-t008-20261112-0144/`; không chứa `.git` trong source snapshot, `.vscode`, artifacts hay runtime DB; dựa Git objects/cache hiện có, không là backup độc lập | 2026-11-12 01:44 UTC |
| E154 | T008 | Fixture test rollback publisher dùng sai kiểu argument `StageProviderRequest` cho `insertPreviousActive` | Ubuntu WSL2, Go 1.27.4 | Compile FAIL; sửa fixture truyền `StageFilesRequest{Capture: stage.Capture}`, sau đó chạy lại test và bộ kết hợp | FAIL | `internal/store/sqlite/index/publish_provider_test.go` | 2026-11-12 01:50 UTC |
| E155 | T008 | 398 source files không gồm spec, manifest SHA-256 `d475bb81a4ac5f9f5f7dfa9908878810035f2b2fcf033140fdfd475dbecc0176`; workspace/WSL byte parity | Ubuntu WSL2 Linux FS; Git/SQLite/provider synthetic, Go 1.27.4 offline; Node Windows | Staging/Done/facts + coverage, range/module-cycle/FK/FTS; exact owner/profile; concurrent SQLite change reject; source+symbol cùng snapshot khi publish; revoke/cancel; process kill trước transaction, trước commit và sau commit; driver-injected commit unknown đã rollback hoặc committed, reconcile theo job; 6 targeted mutants detected/restored; full Go test/vet/bootstrap build/spec_check/mod verify và Python/Node vectors PASS | PASS | `publish_captured.go` `da7273ae9499631aaaa214080bdcd462aa05ad0aa89066d5409bd9d301fab7d0`; `publish_provider.go` `56b8173e46c80e7bb079e439544311e8b8170c39fe9c6898826ba5c21b9c3d4a`; `read_published_symbols.go` `7308a886e73070921a679d7073ea1833bef123c2b7248356d0efe4e99d3a9a60`; native Windows/race/product E2E NOT_RUN | 2026-11-12 01:59 UTC |
| E156 | T008 | Tracker chuyển VERIFY nhưng resume vẫn ghi `active_task: T008` | Ubuntu WSL2, Go 1.27.4 | `tools/spec_check` FAIL `resume active task is not in progress`; cập nhật resume active task/owner thành null để khớp VERIFY, giữ next T008 chờ review | FAIL | `tools/spec_check/main.go` rule resume | 2026-11-12 02:00 UTC |
| E157 | T008 | Tracker VERIFY sau sửa resume E156, source manifest E155 | Ubuntu WSL2, Go 1.27.4 offline | `tools/spec_check` và tests của checker PASS; self-review deliverable staging/publish/reader và failure boundaries theo E155; không là product hay platform verification | PASS | `tools/spec_check/main.go`; T008 scoped Linux foundation | 2026-11-12 02:00 UTC |
| E158 | T008 | Interim snapshot source/spec tại E157, B001 `9458db6f082637284c828b46ac0a50507562e848`; 401 files, manifest SHA-256 `bf04dab012e0c7fc176ecfc23ecbd6facf49744a5ec602ddfe029dd1717527e4` | WSL Linux snapshot + detached B001 restore; Go 1.27.4 offline | Hash/mode snapshot và hash restore từng file, full Go test/vet/spec_check và Python vectors PASS; spec trước E158 | PASS | `/home/mce4d/.local/share/mce4d/snapshots/b002-t008-20261112-0201/`; không chứa `.git` trong source snapshot, `.vscode`, artifacts hay runtime DB; dựa Git objects/cache hiện có, không là backup độc lập | 2026-11-12 02:01 UTC |
| E159 | T009 | 408 source files không gồm spec, manifest SHA-256 `a94594ad76a64ac685c1a2214357bc39b84a917fe80ca8fab1d71affb30f7439`; workspace/WSL byte parity | Ubuntu WSL2 Linux FS, SQLite synthetic, Go 1.27.4 offline | Real child writer bị kill: recovery chỉ lấy OS lock sau khi child chết, old active/source vẫn đọc; bounded pages/CAS, revoke/lease loss/invalid identity/tampered schema, cancel stream gate, driver-injected unknown commit được reconcile, 7 targeted mutants detected/restored; full Go test/vet/bootstrap build/spec_check/mod verify và Python vectors PASS | PASS | `recovery.go` adapter `ff7f1f582a1f9b480e46d716d0d3b2fb665c055dbd97d57c22f5feb3c37f1c0e`; indexer `49a4e2557bcdd1f050fc4ad6669fd62268f17cbbfa05107098614780ca560a91`; real disk-full, native Windows, race, provider process-tree cancellation và product E2E NOT_RUN | 2026-11-12 02:07 UTC |
| E160 | T009 | Tracker VERIFY và source manifest E159 | Ubuntu WSL2, Go 1.27.4 offline | `tools/spec_check` PASS; self-review recovery/cancel scoped fixture và giới hạn quyền/môi trường E159; không là product hay platform verification | PASS | `tools/spec_check/main.go` | 2026-11-12 02:07 UTC |
| E161 | T009 | Interim snapshot source/spec tại E160, B001 `9458db6f082637284c828b46ac0a50507562e848`; 411 files, manifest SHA-256 `2e420784b171939ef0aee2ce38524620e312a6f5591c013c3dd82534e72a62bd` | WSL Linux snapshot + detached B001 restore; Go 1.27.4 offline | Hash/mode snapshot và hash restore từng file, full Go test/vet/spec_check và Python vectors PASS; spec trước E161 | PASS | `/home/mce4d/.local/share/mce4d/snapshots/b002-t009-20261112-0208/`; không chứa `.git` trong source snapshot, `.vscode`, artifacts hay runtime DB; dựa Git objects/cache hiện có, không là backup độc lập | 2026-11-12 02:08 UTC |
| E162 | T011 | 409 source files không gồm spec, manifest SHA-256 `0572651671b2f15f9f5df08643696814c4e796a2b67ebbd8fff10720183e49de`; workspace/WSL byte parity | Ubuntu WSL2 Linux FS; Git/SQLite synthetic, Go 1.27.4 offline | Hai clone cùng basename/remote/HEAD và linked worktree dirty có catalog identities riêng; A BUILDING giữ OS writer ở child process, B publish 8 lần qua 2 profiles, 3 reader liên tục đều quan sát generation đổi và source/symbol/profile nhất quán; A reader thấy old rồi new; 5 lần lặp PASS; 3 targeted mutants profile/generation/identity detected/restored; full Go test/vet/bootstrap build/spec_check/mod verify và Python vectors PASS | PASS | `tests/integration/concurrency_linux_test.go` SHA-256 `c1b0d817459481434ddd16e82af26e66e31310f64f335eeb137b08fd10d544af`; catalog registration và provider/authority dùng fixture, không là product E2E; Windows native/race NOT_RUN | 2026-11-12 02:12 UTC |
| E163 | T011 | Tracker VERIFY và source manifest E162 | Ubuntu WSL2, Go 1.27.4 offline | `tools/spec_check` PASS; review task evidence T004–T012 và G1 theo R1 primary Linux filesystem/fixture; G2 mở, giới hạn platform/authority/product E2E giữ nguyên | PASS | `tools/spec_check/main.go`; E102–E162 theo task | 2026-11-12 02:12 UTC |
| E164 | T011 | Interim snapshot source/spec tại E163, B001 `9458db6f082637284c828b46ac0a50507562e848`; 412 files, manifest SHA-256 `6311dbe94d0e6ef45c0d97717ddbfa34d97e92a1bc647a704086316856c50022` | WSL Linux snapshot + detached B001 restore; Go 1.27.4 offline | Hash/mode snapshot và hash restore từng file, full Go test/vet/spec_check và Python vectors PASS; spec trước E164 | PASS | `/home/mce4d/.local/share/mce4d/snapshots/b002-t011-20261112-0213/`; không chứa `.git` trong source snapshot, `.vscode`, artifacts hay runtime DB; dựa Git objects/cache hiện có, không là backup độc lập | 2026-11-12 02:13 UTC |
| E165 | T013 | 415 source files không gồm spec, manifest SHA-256 `441d8d24819b4a48e62a54798966c03e4db0c0343c49f16c96fbe680aa1e4524`; workspace/WSL byte parity | Ubuntu WSL2 Linux FS, Go 1.27.4; SQLite/OS locks và source synthetic | AST từ captured bytes, scope/declaration/UTF-8 byte range, receiver/generic/type switch/closure/embedded fields, import/tag, production/internal/external variants và occurrence provenance; `go/types` oracle độc lập; parse-error partial được stage/publish/read qua SQLite và OS lock; budgets/secret/cancel; 9 targeted mutants detected/restored; full Go test/vet/bootstrap build/spec_check/mod verify và Python vectors PASS; Windows cross-compile only | PASS | `syntax.go` `74d894338cf1fa1a29b7658298a8b1173a9d774a3f5032ff3ce6921cf221b366`; `syntax_collect.go` `0fe7b91ab7c16be595843fa3ffb79496ee188644de0f281d90646fa6f6b424ac`; build-selection/provider composition, typed Uses/Selections và product E2E NOT_RUN | 2026-11-12 02:23 UTC |
| E166 | T013 | Tracker VERIFY và source manifest E165 | Ubuntu WSL2, Go 1.27.4 offline | `tools/spec_check` PASS; self-review AST/captured-input fixture và giới hạn package selection/typed semantics E165; không là G2 hay product verification | PASS | `tools/spec_check/main.go` | 2026-11-12 02:23 UTC |
| E167 | T013 | Interim snapshot source/spec sau DONE, B001 `9458db6f082637284c828b46ac0a50507562e848`; 418 files, manifest SHA-256 `1ac22a6fa69ae485e96e3cc1ece49f6a3521e702098193a074f7ba6fff5510fb` | WSL Linux snapshot + detached B001 restore; Go 1.27.4 offline | Hash từng file của source, restore và git-restore PASS; full Go test/vet/spec_check và Python vectors PASS; lệnh kiểm hash theo helper không tồn tại FAIL, đã kiểm lại trực tiếp từng file; spec trước E167 | PASS | `/home/mce4d/.local/share/mce4d/snapshots/b002-t013-20261112-0224/`; dựa Git objects/cache hiện có, không là backup độc lập | 2026-11-12 02:25 UTC |
| E168 | T017 | Search snapshot isolation fixture và targeted mutation kiểm quyền | Ubuntu WSL2, Go 1.27.4 | Fixture publish generation mới FAIL do provider stream cũ chưa được release; đã release sau terminal. Mutant bỏ reauthorization lần đầu không compile vì unused variable; thay bằng mutant compile được, test bắt và khôi phục | FAIL | `internal/store/sqlite/index/search_read_test.go`; mutant chỉ tại bản sao WSL | 2026-11-12 02:34 UTC |
| E169 | T017 | 421 source files không gồm spec, manifest SHA-256 `653f44a627f4f1b1f11edeec57b2dba4b3745ca363df133f74a4f3c215916367`; workspace/WSL byte parity | Ubuntu WSL2 Linux FS, Go 1.27.4 offline; SQLite/source synthetic; Node Windows | Staging/publish chỉ docs từ captured bytes; exact/prefix/position ambiguity, Unicode, lexical TS/SQL, SQL/FTS literal injection, bounds/revoke/cancel; readers giữ snapshot qua publish, BUILDING không xuất và pinned generation không trộn; EXPLAIN dùng index trên 1.200 symbols/300 docs; 5 lần lặp và 6 targeted mutants detected/restored; full Go test/vet/bootstrap build/spec_check/mod verify và Python/Node vectors PASS; Windows cross-compile only | PASS | `search_read.go` `6dfcc12939f939e2faad3cc3df9c5a37f1f32f8083b97694b65e54c44f60e82b`; `search_stage.go` `e82d68171f3194ff7a95c705a3f052532d64e421d98311f973f38ee85b0829a8`; runtime authority/provider composition, native Windows, race và product E2E NOT_RUN | 2026-11-12 02:34 UTC |
| E170 | T017 | Review sau E169: batch metadata vượt ngân sách | Ubuntu WSL2, Go 1.27.4 | Fixture path Unicode 4.403 bytes, body nhỏ, `MaxBatchBytes=1024` vẫn được stage; test mới FAIL với `<nil>`. T017 trở lại IN_PROGRESS để sửa; E169 historical trước thay đổi | FAIL | `internal/store/sqlite/index/search_read_test.go` `TestSearchSupportsLongUnicodePortablePath` | 2026-11-12 02:35 UTC |
| E171 | T017 | 421 source files không gồm spec, manifest SHA-256 `07d66127663eaa6612ac63ceb4e9aa5f4a3d3887285eb15c1377d904f28ac873`; workspace/WSL byte parity | Ubuntu WSL2 Linux FS, Go 1.27.4 offline; SQLite synthetic | Fix E170: batch budget tính mọi text columns/body và source cap; long Unicode path và source-budget negative PASS, 2 targeted mutants detected/restored; full Go test/vet/bootstrap build/spec_check/mod verify/Python vectors PASS, Windows cross-compile only. E169 còn áp dụng cho subcases không đổi; 5 lần search suite PASS | PASS | `search_stage.go` `67e861dd9a48e7ecc18fd79f3c5766b15ab1216f471626f96ff7addaacd32574`; authority composition/Windows native/race/product E2E NOT_RUN | 2026-11-12 02:36 UTC |
| E172 | T017 | Tracker VERIFY và source manifest E171 | Ubuntu WSL2, Go 1.27.4 offline | `tools/spec_check` PASS; self-review lookup/publish, output budget và fixture E169/E171; không là independent review, G2, product hay platform acceptance | PASS | `tools/spec_check/main.go` | 2026-11-12 02:36 UTC |
| E173 | T017 | Interim snapshot source/spec sau DONE, B001 `9458db6f082637284c828b46ac0a50507562e848`; 424 files, manifest SHA-256 `fc18f0ce9e896ae4204ffe92e6beecaddd7fcf3220e919a29e3dae403f465e92` | WSL Linux snapshot + detached B001 restore; Go 1.27.4 offline | Hash từng file source/restore/git-restore, full Go test/vet/spec_check và Python vectors PASS; spec trước E173 | PASS | `/home/mce4d/.local/share/mce4d/snapshots/b002-t017-20261112-0237/`; dựa Git objects/cache hiện có, không là backup độc lập | 2026-11-12 02:37 UTC |
| E174 | T020 | 426 source files không gồm spec, manifest SHA-256 `a2a2cf924760a9cc83151614bc1e54f92580b5a88a1782d4e061012131d798cb`; workspace/WSL byte parity | Ubuntu WSL2 Linux FS, Go 1.27.4 offline; synthetic files/process | Preflight kiểm capture/profile/policy, bounded bytes và go.mod; thiếu proof, dependencies/replace/cgo trả partial. Overlay trả bản sao, ParseFile từ chối source/path sai và revoke/cancel; 5 lần targeted, full Go test/vet/spec_check PASS. Fixture riêng chứng minh `packages.Config.Env` không khống chế `exec.Command("go")` lookup: parent PATH giả vẫn được gọi | PASS | `loader_preflight.go` `6df47de849f985b41b4ea97239b53c8a8cb843ddaf8eace3fac51d92ecaddf99`; `loader_overlay.go` `79517014ab7b19d5107c659432816ff655723206078490d155442ef7a761e030`; chưa có guarded loader, closure/toolchain proof thật, typed fact integration hoặc network/platform acceptance | 2026-11-12 02:43 UTC |
| E175 | T020 | Scratch fixture trước kiểm `.git` và trước chmod fixture | Ubuntu WSL2 Linux FS | Fixture root 0755 bị owner-only guard từ chối; sau chmod 0700 phát hiện `WalkCandidates` cố ý bỏ qua `.git`, khiến scratch verifier nhận workspace có `.git`. Đã thêm kiểm `.git` ở root/package ancestors. Một mutant byte-compare đầu tiên không compile; mutant compile được bị test bắt và hoàn nguyên | FAIL | `internal/analysis/golang/loader_scratch_linux_test.go` | 2026-11-12 02:58 UTC |
| E176 | T020 | 442 source files không gồm spec, manifest SHA-256 `5746a2addbf549d4f0af2294476762030a9b112fd28e3920b717b872c987d0d1`; workspace/WSL byte parity | Ubuntu WSL2 Linux FS, Go 1.27.4 offline; synthetic toolchain/source; network namespace không có egress | Go env whitelist + `-buildvcs=false`, hash/inode/PATH trước-sau; scratch 0700, exact captured files, `.git`/extra/symlink bị chặn; loader thật cho no-import và local-module import, source overlay mismatch fixture, policy/cancel/postcheck, external import/missing closure trả partial; fixture pass trong network namespace; 11 targeted mutants detected/restored; full Go test/vet/bootstrap build/spec_check/mod verify/Python vectors PASS, Windows cross-compile only | PASS | `loader.go` `fcbd7a3f8c2adec602d9552ab80b23206fde4bd85d2331b0a551b497458efec5`; `loader_scratch_linux.go` `c20212b7dae4f473db3f5e091d8574a8e832da6ae697b8878caa99cc9cd3c1ea`; không chứng minh không có network attempt, same-user atomicity, stdlib/external deps/tests, runtime authority, race/native Windows hay product E2E | 2026-11-12 02:58 UTC |
| E177 | T020 | Interim snapshot source/spec IN_PROGRESS, B001 `9458db6f082637284c828b46ac0a50507562e848`; 445 files, manifest SHA-256 `4a2e406d6e04cc346a5edb38fea8806c4dd3be4b39a4e97467be38690f8fe3fa` | WSL Linux snapshot + detached B001 restore; Go 1.27.4 offline | Hash từng file source/restore/git-restore và full Go test/vet/spec_check/Python vectors PASS; spec trước E177 | PASS | `/home/mce4d/.local/share/mce4d/snapshots/b002-t020-inprogress-20261112-0259/`; không là backup độc lập hoặc T020 completion | 2026-11-12 02:59 UTC |
| E178 | T020 | Runner/trace/mutation fixture trung gian | WSL2 x86_64, synthetic processes | Lệnh đầu truyền sai argv `-test.run`, một lần tracer mất process khi đọc stop, và mutant bỏ final auth đầu tiên không compile; các kết quả đó không được tính PASS. Tracer được sửa để đếm lost stops/events và fail khi mất; mutant compile được bị test bắt | FAIL | `/home/mce4d/.local/share/mce4d/t007-build/t020_network_trace_20261112.py`; `/home/mce4d/.local/share/mce4d/t007-build/t020_mutations_20261112.py` | 2026-11-12 03:16 UTC |
| E179 | T020 | 447 source files không gồm spec, manifest SHA-256 `9e69e1f4bd472fc43e85d6e14edefbe6b377bffea9b6a7143ddb396f4778bae5`; workspace/WSL byte parity | Ubuntu WSL2 Linux FS, Go 1.27.4 offline; synthetic source/toolchain authority | Stdlib `fmt` qua root/fingerprint/package closure do fixture chứng nhận; path/bytes/revoke/cancel/budget; profile Linux+tag và Windows selection; missing tool/dependency → syntax partial; 6 mutants detected/restored; full Go test/vet/mod verify/Python vectors PASS, Windows cross-compile only | PASS | `loader.go` `35163c582243d196d03ac43b993a932b8fbc26dcaf3af2f3cebdb0c534bf2b0b`; `loader_stdlib.go` `0a4a153ae842825860ca5499925886ee88bd9440837e9c91718f3f75a09ee0a3`; `loader_fallback.go` `f1a2929f03a384cca5ad91ddc0ba0b038965f9c3ca40c1509dbbe4334169dc12`; callbacks authority là test doubles, spec trong lần full test trước E177 | 2026-11-12 03:16 UTC |
| E180 | T020 | Test binary SHA-256 `1726ccacaa1771e548251317c578f182860706fca585422d13f083e666d6d28a`; tracer SHA-256 `f26ae519189435cf1ad0de4b877d2d92d586918de8c46b668eb9aec3cd447b33` | WSL2 x86_64, user+network namespace cô lập, ptrace toàn process tree synthetic | Positive probe socket/connect IPv4 trong child được phát hiện; guarded loader pass, 364 tasks seen/exited, 447299 syscall stops, lost=0, unknown architecture=0, không ghi nhận socket/connect/send; missing-dependency fallback pass tương tự. Lần trước mất 2 stops bị fail và giữ là incomplete | PASS | `/home/mce4d/.local/share/mce4d/t007-build/t020_network_trace_20261112.py`; chỉ syscall/fixture quan sát, không là runtime sandbox hoặc proof không có network trên mọi môi trường | 2026-11-12 03:16 UTC |
| E181 | T020 | Source manifest E179; spec VERIFY SHA-256 `31c5a899a059254d2838a29a77745939c83738857ab3f10d5d54dfa1710d88ba` | Ubuntu WSL2 Linux FS, Go 1.27.4 offline | `go run ./tools/spec_check`, `go test ./tools/spec_check -count=1`, bootstrap `go build` PASS; parity sau mutation; tác giả tự review phạm vi và trust boundaries, không có independent review | PASS | `/home/mce4d/.local/share/mce4d/t007-build/t020_bootstrap_20261112`; spec là bản VERIFY trước cập nhật DONE; không là product build/E2E | 2026-11-12 03:17 UTC |
| E182 | T014 | Test/mutation/ptrace trung gian | WSL2 x86_64, fixture synthetic | Mutant type-switch đầu không compile; một marker mutation sai; các kết quả không được tính phát hiện. Ptrace các lần sau mất 1–4 stops do process kết thúc, helper fail closed; không tính là proof không có network attempt | FAIL | `/home/mce4d/.local/share/mce4d/t007-build/t014_mutations_20261112.py`; `/home/mce4d/.local/share/mce4d/t007-build/t020_network_trace_20261112.py` | 2026-11-12 03:42 UTC |
| E183 | T014 | 450 source files không gồm spec, manifest SHA-256 `c9f8642a3bcfcfb62594e981d0aa9b8001cd57bb7d41bd10585c4dd0a93407d0`; Windows/WSL byte parity | Ubuntu WSL2 Linux FS, Go 1.27.4 offline; synthetic fixtures | Same-load Defs/Uses/Selections gắn symbol/range/scope, shadowing/type-switch/generic/method/field/receiver/params/result, UTF-8+CRLF, cross-file/local-package và profile selection; missing proof/revoke/budget không trả typed facts; profile/inventory copy và parse-worker cap. 11 mutants detected/restored; full Go test/vet/build/spec_check/mod verify/Python vectors PASS; Windows cross-compile only | PASS | `typed.go` `a5d738d4e54913e9d7dcb384eee2d1b49762b345fa3b66534071edba50dcb190`; `loader.go` `c7580ed4e295a9a00953b0f6886044b96fbb8d0082e0df398dcb9e0d3b7d5d47`; syntax/typed status vẫn partial, graph/publication/product authority chưa xong | 2026-11-12 03:42 UTC |
| E184 | T014 | Test binary SHA-256 `a954fb72159b07c5396d9cbb3d24899c94b4b9bb62e71635fb5bcc27469117ea`; seccomp helper SHA-256 `8817e9f92cb0f7085f29c989a262eaa51ea8af23dfcc888bdf57be838ff159c0` | WSL2 x86_64, user+network namespace, seccomp fixture kế thừa qua fork/exec | Positive child socket bị SIGSYS; loader + typed + stdlib fixture PASS với filter chặn các socket/connect/send/receive syscall x86-64 đã liệt kê. Ptrace mất stop vẫn giữ E182 | PASS | `/home/mce4d/.local/share/mce4d/t007-build/t014_network_guard_20261112.py`; không chứng minh không có network attempt, mọi syscall/kernel, hoặc production sandbox | 2026-11-12 03:42 UTC |
| E185 | T014 | Source manifest E183; spec VERIFY SHA-256 `f980749462a90dec8166f8ea32700049c168790b3fb18f92572eec7ab560f8f5` | Ubuntu WSL2 Linux FS, Go 1.27.4 offline | `go run ./tools/spec_check` PASS; parity sau mutations; tác giả tự review đối chiếu captured bytes, per-load identity, scope, budget, revocation và boundary | PASS | Self-review không là independent audit; tracker/spec hash là bản VERIFY trước cập nhật DONE; production integration và G2 chưa được chứng minh | 2026-11-12 03:43 UTC |
| E186 | T014 | Interim B002 snapshot trước E186, parent `9458db6f082637284c828b46ac0a50507562e848`, 453 files, manifest SHA-256 `eda13a5f2d5a5cd46a2004b574fc3bb426d8482e21fdcedb9e9ca92d3a2b4b40` | WSL Linux snapshot + detached B001 restore; Go 1.27.4 offline | Đối chiếu file set/hash/mode của source, copy và git-restore; full Go test/vet/spec_check/Python vectors PASS | PASS | `/home/mce4d/.local/share/mce4d/snapshots/b002-precheckpoint-20261112-0345/`; không là checkpoint đã commit hoặc backup độc lập; spec snapshot trước E186 | 2026-11-12 03:45 UTC |

Format result: PASS, FAIL, NOT_RUN. NOT_RUN phải có lý do trong record. Hash tree dùng khi chưa commit; không bịa commit SHA. Absolute local paths chỉ trong private evidence khi cần, không publish; log sanitized. File artifact không tồn tại thì không tạo pointer giả. DONE task MUST trỏ >=1 evidence record PASS bao phủ test scope; gate reviewer kiểm tra không chỉ string “PASS”.

### Metadata áp dụng cho task/lệnh — không thêm tracker

Khi có thực thi, ghi trong evidence/ghi chú S21 gắn task S19: `task_mode`, `assignment_ref`, `spec_revision`, `policy_source_refs` (actual path/version/digest/status), `change_budget` (objective/paths/behavior/contracts/exclusions), `verification_plan` (V0–V4 áp dụng + required checks), và `command_record_refs` theo S51. Chỉ lưu delta/locator cần thiết, không sao chép spec hoặc raw logs. Chưa có run thì không tạo record giả. Các metadata này phục vụ governance, không đổi S43/S44 hay mạo nhận policy được thực thi.

**T001 — assignment đang thực hiện, 2026-11-11:** `task_mode=implement`, nguồn quyền là yêu cầu người dùng bắt đầu triển khai từ T001 theo spec; `spec_revision=1.6.1`, baseline Git `ba72ae4c59e65708812a9f964e30ae0fdbbc90d6` cộng thay đổi spec 1.6.1 từ phiên trước được người dùng yêu cầu. Workspace hiện tại có `origin` và branch `main`; quyền push/backup bên ngoài chưa được cấp. Không có checkpoint `mce4d/checkpoints/*` được quan sát. Ba nguồn thực ở `docs/AI_RULES.md` (3.1, SHA-256 `1662406d9ac9e63646130516ad6811934b2bd7f0a6b7d240403749200777f9e3`), `docs/AI_RULES_STORAGE_SUPPLEMENT.md` (1.0.0, `eea81c9ac63344efde5f1eccb30e43d06146875aabaf8396fec3b46e5e878dac`) và `docs/PROJECT_BOOTSTRAP_PROMPT.txt` (3.1, `b1d692184f9b4bac87f6b55b25e1c49bab90af5d5eb1706a428a09d85212b686`) khớp raw hash ledger S50; prompt không được thực thi. Đây là kiểm thủ công, không là guard tự động.

`change_budget`: khảo sát và bootstrap module riêng ở `go.mod`, `.gitignore`, `.gitattributes`, `cmd/mce4d/` cùng bằng chứng thuộc task; cập nhật tracker/evidence/resume trong spec. Không tính năng engine, target repo, thay policy, push hay install toàn máy. `verification_plan`: V0 Git/policy/permission; V1 bootstrap format/build/test/vet khi toolchain sẵn, kiểm tracker thủ công trước T003; C041 và các phần Git/guard chưa triển khai chỉ ghi đúng phạm vi, không đánh dấu E2E PASS. Các công cụ trên PowerShell: Git 2.56.0.windows.2, Node v24.22.1; `go` không trên PATH và `C:\\Go\\bin\\go.exe` không tồn tại; `wsl --list --quiet` báo WSL chưa cài. Không suy ra mọi Go trên máy đều không tồn tại; Linux/WSL và target OS acceptance chưa kiểm. Remote repo và tác vụ thực đầu tiên chưa được chỉ định để làm corpus; dùng fixture theo S01/S18. Forecast: CHƯA ĐỦ DỮ LIỆU.

**T001 preflight bootstrap toolchain:** nguồn metadata chính thức `https://go.dev/dl/?mode=json` trả `go1.27.4.windows-amd64.zip`, 79.077.744 bytes, SHA-256 `178ab98950ac69ab25069307dc6ade79f3816e414cce95b4da166a115cde1e7e`; chỉ dùng toolchain tạm dưới `artifacts/preflight/`, không sửa PATH/config máy. Module path dùng remote origin Git đã xác minh, bỏ `.git`, không publish. Network chỉ download archive chính thức; không gửi source/credential. Download phải giới hạn đúng bytes và xác minh SHA-256 trước giải nén; kiểm tổng uncompressed/đường dẫn archive và admission trước extraction. Repo/artifact/cache dự kiến cùng volume C:; lúc khảo sát free 724.515.680.256 bytes. Giới hạn riêng run 1 GiB gồm archive/extraction/cache, cộng reserve 1 GiB; chặn khi free không đủ, deadline và giám sát ghi của process trong phạm vi run; không nhận biết writers khác. Chưa có bằng chứng chạy hay pin T002; nếu không thực thi được trong giới hạn thì giữ T001 chưa hoàn tất và báo rõ, không dùng kết quả giả. Artifacts do run sở hữu được giữ, không tự dọn dữ liệu cũ.

Lần thử download đầu dừng trước khi ghi bytes vì HTTP `Content-Length=76744255` khác metadata. HEAD kiểm tra tiếp cho thấy server trả `Content-Encoding: gzip` và `x-identity-content-length: 79077744`; retry vào path mới, kiểm chính bytes đã giải mã và SHA-256 từ metadata. File lần đầu dài 0 bytes được giữ nguyên; không coi response compressed length là hash của archive hay bỏ kiểm integrity.

Archive retry đã khớp 79.077.744 bytes/SHA-256; kiểm 15.659 entries, tổng 247.362.647 bytes, không có symlink/path escape, rồi giải nén trong giới hạn. `go version` trả `go1.27.4 windows/amd64`; `gofmt -d cmd/mce4d/main.go` không có diff. Lần đầu `go test ./...` FAIL vì source test của toolchain nằm trong `artifacts/preflight/toolchains/`, bị Go quét như package thuộc module; không phải product test PASS. Cô lập thư mục toolchain và build dưới tên bắt đầu bằng `_` để pattern `./...` không đi vào, giữ nguyên artifacts của run; chạy lại các kiểm bị ảnh hưởng. Bộ Go này là bootstrap toolchain tạm, T002 vẫn phải kiểm/pin dependency bundle, license và compatibility riêng.

**T001 kết quả/blocker:** sau cô lập, `go test ./...` và `go vet ./...` exit 0 nhưng package hiện chưa có test; `go build` FAIL do Windows Application Control chặn thực thi Go linker (`link.exe`), nên binary/smoke, T001 DONE và G0 PASS chưa có. Không vô hiệu hoặc lách chính sách máy. Cần toolchain/môi trường được chính sách cho phép, rồi chạy lại format/test/vet/build và smoke của đúng revision; T002 chờ T001 DONE. C041 public-tool canary, platform matrix, validator T003 và các fixture Git đầy đủ vẫn NOT_RUN. Go archive và build/cache giữ trong `artifacts/preflight/_toolchains/` và `artifacts/preflight/_bootstrap-build/` (gitignored); không có quyền xóa dữ liệu tích lũy. Không commit dưới 10 task DONE hoặc push. Source hash SHA-256: `go.mod=ad51b3cb1cd30d8989f06379e63163a81460299b170c5f9b5371c57cad280254`, `.gitignore=d1ecbac700945443ca0deb386015e417ac3e2b1e383a623bfc1c0dc9b1b89546`, `.gitattributes=50d6e0b355ea3957ecd810d40aa69b1e6f7dc8c78026fec69552dd14c390e370`, `cmd/mce4d/main.go=cb38c0fe62815899c415c10392d37872d8ea041bde4888f910d75c070862b6e2`. Test/build dùng `GOENV=off`, `GOWORK=off`, `GOTOOLCHAIN=local`, `GOPROXY=off`, `GOSUMDB=off`, `GOFLAGS=-mod=readonly`, cache/temp riêng; không target repo/network runtime.

**T001 — chuẩn bị môi trường WSL, 2026-11-11 02:57 UTC:** theo yêu cầu trực tiếp của người dùng cài WSL2, `wsl.exe --install --no-distribution` được khởi chạy qua Windows elevation, exit 0; `wsl.exe --install Ubuntu --no-launch` trả thành công nhưng báo thay đổi chỉ có hiệu lực sau reboot. Trước reboot, `wsl --version` báo 3.0.5.0, kernel 6.18.58.1-1; `wsl --status` báo default version 2 nhưng WSL2 chưa khởi chạy và nêu điều kiện virtualization/Virtual Machine Platform; `wsl --list --verbose` chưa thấy distribution, Windows báo pending reboot. Không suy đã cài hoặc chạy Ubuntu, không sửa firmware hay tự reboot. Sau khi người dùng khởi động lại, kiểm lại status/list; nếu Ubuntu chưa đăng ký thì tiếp tục cài theo quyền đã giao, hoàn tất initial user setup với người dùng khi cần. Nếu virtualization vẫn báo lỗi sau reboot thì chẩn đoán tiếp; chỉ dùng Linux filesystem cho repo/state theo S03 và không làm mất diff hiện tại. T001 vẫn BLOCKED, G0 chưa PASS; WSL chưa là bằng chứng Linux test.

**T001 — WSL sau reboot, 2026-11-11 03:04 UTC:** `wsl --install Ubuntu --no-launch` exit 0; `wsl --list --verbose` xác nhận Ubuntu VERSION 2; `uname` xác nhận kernel WSL2 x86_64 và `/etc/os-release` là Ubuntu 26.04. Distro ban đầu chỉ có root; tạo user không đặc quyền `mce4d` (uid 1000, không sudo/password) và `wsl --manage Ubuntu --set-default-user mce4d`; `wsl -d Ubuntu --exec id` xác nhận uid 1000. Không suy Linux build đã PASS. Dự kiến snapshot source riêng trên Linux filesystem để kiểm đúng bytes, giữ checkout Windows là source làm việc; trước dùng so hashes và không ghi sang checkout nguồn khi test.

**T001 — preflight Linux:** Go archive bootstrap từ metadata `https://go.dev/dl/?mode=json`: `go1.27.4.linux-amd64.tar.gz`, 70.680.823 bytes, SHA-256 `7226736c2339719cda28fbf3a54d71aa63bcefe376e98cef00034268dab24840`. Linux `/home` available khoảng 1.024.729.096.192 bytes theo `df`, backing C: khoảng 718 GB tại quan sát; giới hạn dự kiến 1 GiB cho download/extraction/build/cache/snapshot và reserve 1 GiB trên mỗi volume, kiểm cả filesystem/VHD backing. Download giới hạn bytes/deadline và hash, kiểm members/size trước extraction; cache/temp dưới `/home/mce4d/.local/share/mce4d/` riêng, không dùng chung cache người dùng/target. Không gửi source/network ngoài fetch archive chính thức; chưa pin T002. Chưa có bằng chứng lệnh build Linux tại thời điểm record này.

**T001 — kết quả Linux và đóng scope bootstrap:** archive đúng size/hash; kiểm 17.375 entries, 244.338.457 bytes extraction, không symlink/hardlink/path traversal, bounded extraction PASS. Copy `go.mod` và `cmd/mce4d/main.go` sang `source-snapshot` trên Linux, so SHA-256 trước/sau với source Windows; không sao chép `.git` hay giả snapshot này là checkout thực. Go version/test/vet/build và smoke E003 PASS; các lệnh có timeout, giới hạn cache/log và kiểm free space Linux lẫn backing C:, Go offline và GOCACHE/TMPDIR riêng. `go test` chưa có test case; full C041/product tests, Linux acceptance và Windows build vẫn NOT_RUN/FAIL theo scope tương ứng. C137/C138 kiểm thủ công hash ba policy/mode; C181/C182/C185/C189/C191 kiểm baseline/quyền theo scope khảo sát, Git fixture/restore thực vẫn NOT_RUN. Tracker/IDs/order được kiểm thủ công trong khi validator T003 chưa tồn tại. T001 DONE chỉ cho bootstrap trên Linux; không nhận G0 PASS hay Windows native supported. Tiếp tục T002, giữ E002 như bằng chứng lỗi môi trường Windows.

**T002 — assignment/plan:** `task_mode=implement`, cùng yêu cầu của người dùng; baseline `ba72ae4` + working tree gồm spec 1.6.1 và T001, `owner=codex`. Change budget: discovery official release/API/license, pin Go/dependency bundle tại `go.mod/go.sum`, fixture và artifacts trong phạm vi T002, ledger/evidence trong S03/S21, không runtime feature, không sửa policy hay target. V0–V2: kiểm exact module artifacts/transitive license và checksum, compatibility Go/SQLite FTS/MCP/go/packages bằng fixture nhỏ; kiểm source/config/command preflight S51; không nhận MCP host hoặc Windows platform PASS từ fixture Linux. Đầu ra/download/cache/temp riêng trên filesystem Linux và backing C:, network chỉ tới nguồn Go/module chính thức, không gửi source dự án; bắt đầu bằng metadata nhỏ, đặt budget/giới hạn thực trước smoke. Chưa kết luận T002 DONE hoặc dependency an toàn chỉ theo metadata.

**T002 — preflight bundle:** metadata Go module proxy ngày 2026-11-11 cho candidates `modernc.org/sqlite v1.62.0` (go 1.26), `github.com/modelcontextprotocol/go-sdk v1.8.0` (go 1.25), `golang.org/x/tools v0.52.0` (go 1.26); bootstrap Go 1.27.4 đáp ứng mức tối thiểu được khai báo, chưa chứng minh compatibility. MCP SDK có MIT/Apache-2.0 theo LICENSE upstream, không rút gọn thành một license; SQLite có third-party license inventory, phải kiểm artifact/transitive trước DONE. Fixture kiểm SQLite FTS5/FK/rollback, MCP in-memory roundtrip và typed Go fixture; đây không phải host/Windows/semantic conformance đầy đủ. T002 dùng root riêng `/home/mce4d/.local/share/mce4d/preflight-20261111-t002`, GOCACHE/GOMODCACHE/TMPDIR riêng; download module qua `proxy.golang.org` và `sum.golang.org`, không gửi source riêng. Preflight kiểm Linux filesystem và backing C:, stop nếu free <1 GiB, cap 2 GiB tổng output mới, thời hạn 300 giây/lệnh, log 1 MiB, theo dõi process group; không kiểm writers ngoài scope. Artifact/source fixture được bảo toàn; không tự cleanup. Module metadata/fixture chưa là evidence PASS hoặc permission runtime cho target.

**T002 — kết quả và license scope:** `go.mod` SHA-256 `5190b2474f1e2d114b47fffd0b20d32b6c5dac111b1788e4fd94ddd767b580d8`, `go.sum` `36be2e1223b9ede98aaf680199ba7cfca2fade35bd5c873db7131efb7c655fae`; checksums của từng module nằm trong `go.sum`. E004 kiểm chính các bytes này và test hash bằng snapshot Linux. Bốn tests: SQLite version 3.54.1/FTS/FK/rollback, MCP JSON-RPC qua IO pipes + unknown tool, `go/packages` typed fixture, và `go list -deps ./cmd/mce4d` không link module được ngoại lệ trên Linux. C039/C042/C046 và C140–C142 chỉ có subcase thuộc preflight; actual MCP STDIO/host, EXPLAIN ở scale, policy poisoning/runtime guard và OS enforcement vẫn NOT_RUN. Linux source snapshot không có `.git`, không chứng minh real repo acceptance; Windows native build vẫn FAIL E002.

Kiểm `go list -m all` có 38 module và `go list -deps -test` có 18 module dùng trong fixture Linux; `go mod verify` PASS. Đã thu thập LICENSE và các notices ở module graph vào `/home/mce4d/.local/share/mce4d/preflight-20261111-t002/license-inventory.json` SHA-256 `50f47aaab9c4a48d0e7cd7554d52dc6aa41c97c0dca55e5ee24d6a98511cfba6`; phân loại tự động là hỗ trợ review, không là license clearance phát hành. SQLite upstream inventory nêu public-domain SQLite, BSD/MIT components, MPL-2.0 `github.com/hashicorp/golang-lru/v2` trong module graph/test của upstream, không nằm trong package set fixture Linux; `go mod why -m` xác định đường qua `modernc.org/libc.test` và `modernc.org/ccgo/v4/lib` đến `modernc.org/gc/v3`. User trực tiếp chấp thuận scope test IP-05a/A041; chưa duyệt phát hành nguồn chứa MPL hoặc nhúng nó vào sản phẩm. MCP LICENSE nêu cả MIT/Apache-2.0 và CC-BY-4.0 cho tài liệu. Notices, generated/vendored content và mọi platform/artifact cần rà soát lại tại T041. Candidate cũ `modernc.org/sqlite v1.40.0` có SQLite 3.50.4, thuộc khoảng version SQLite mô tả có WAL-reset bug trong điều kiện concurrency; không tự hạ về bản đó để né vấn đề license: https://sqlite.org/wal.html#the_wal_reset_bug . T002 DONE trong scope fixture/ledger, không tuyên bố release/legal audit hoàn tất.

**T003 — assignment/plan:** `task_mode=implement`, user yêu cầu thực hiện theo S47; `spec_revision=1.6.2`, baseline `ba72ae4` + working tree T001/T002. Change budget: chỉ `tools/spec_check/` và tests cùng package, cập nhật S19/S21/S22; không mutation target, network, commit hay product feature. Validator dùng Go stdlib, chỉ đọc spec/policy và optional generated artifacts; test mutation có oracle độc lập cho missing/duplicate IDs, dependencies, evidence, contract refs và batch metadata. G0/product/guard chưa PASS chỉ vì validator chạy được. Build/test/temporary outputs trong Linux filesystem với Go/cache đã pin, giới hạn S51 như preflight T002; so bytes source trước/sau snapshot. Nếu chưa phủ đủ oracle S48/S53 thì giữ IN_PROGRESS/VERIFY, không fake DONE.

**T003 — phạm vi kết quả:** checker xác minh section/ID/task/case references, schedule và work cards, trạng thái/tiến độ/evidence, gate chưa đủ task, batch metadata/đuôi/authority record, policy hashes/EOL, JSON duplicate/local refs, registry/defaults/hash vectors, thứ tự DDL và parity generated artifacts khi có. Nếu metadata khai báo verified checkpoint, checker chỉ đọc Git objects, xác minh annotated tag, OID/tree/trailers/task list; fixture Git cô lập kiểm đúng/sai. Mutation tests kiểm phản ví dụ riêng; source `main.go=efbd3ecdc85a94c0727d70e41849ee03499bf1ba55d058039cfb82423620ee47`, `main_test.go=0e138fd807b1eef4c6338d57968a384c10264ec341b4ca9d420db0eb73953587`, `checkpoint.go=e6e658cb71d9a7d124c60a8aaf95a25f80570556a4e479cecc7f0a6ee0093794`, `checkpoint_test.go=91e9fbc615a8d440146803fc50c9b913196842e7a93f302f9c600d3003f17822`. `go run ./tools/spec_check` chỉ báo selected structural checks: JSON Schema meta-validation/SQL execution, authority thật của tail, Git restore/push và product controls vẫn cần bằng chứng T105–T108/T107/T040; record authority không tự cấp quyền. T003 DONE cho validator và fixture đã mô tả, G0 chưa seal.

**T105 — assignment/plan:** `task_mode=implement`, `spec_revision=1.6.2`, baseline `ba72ae4` + working tree T001–T003, `owner=codex`; S43 là schema/registry authority, S42/S45 cho semantic flow, S46 import boundary. Change budget `contracts/`, `internal/model/`, `internal/ports/`, `tests/contract/` cùng tests/fixture và tracker/evidence; không store/app/transport feature, không target source mutation và không thêm dependency/schema ngoài baseline. Trích schema/registry từ marked JSON, kiểm parity; implement value types/semantic validators có positive/negative/independent oracle và import-boundary tests theo task. Không expose capability chưa verified hoặc tạo stub success; SV cần actual runtime state được phân biệt proof thuần và owner task, không gọi fake adapter là production evidence. Chỉ mark DONE sau đầy đủ case/subcase T105 và build/verification, G0 vẫn chờ T106–T108.

**T105 — tiến độ từng phần, 2026-11-11 03:32 UTC:** trích hai artifact S43; codec Go và oracle Python độc lập khớp 14 vector bytes/SHA và từ chối các giá trị sai đã kiểm; decoder identity từ chối duplicate, surrogate lỗi, số thập phân/exponent, UTF-8 sai, trailing và giới hạn. SourceRef kiểm closed shape, ID/hash/path, raw SHA-256, UTF-8 boundary và range nhưng chưa chứng minh membership/policy/secure open. Test import boundary kiểm phần model hiện có; ports chưa tạo. `jsonschema-go v0.4.3` đã pin gián tiếp được dùng trong fixture meta-resolution và positive/negative SourceRef; thư viện bỏ qua `format`, nên không dùng đó làm proof timestamp hoặc semantic CT-02. Hash source: `contracts/mce4d.v1.schema.json`=baff99b21026031e3bae13c01952755d39309b14155403918779ed55a97c575a; `contracts/operations.v1.json`=c9e08d5babf1f254c5fb1f1a96f9075df099c5ff6d6ef2f170cca86825685bd0; `internal/model/canonical.go`=333ea303ccf0dfb82925416c63399d52475f1acec85f5bba19c50431e148b31e; `internal/model/identity_json.go`=f6fa599d2431f872ee9c99cfbf2327374f199d7ac9fc2be9164bfde977cc56af; `internal/model/canonical_test.go`=8bea77c6e4a06e3652b0065a3934ed49370eb8ff40f1f5a76d7ca938ea4fbf8e; `internal/model/source_ref.go`=0aef15540f38f3253984eaff89441df24c8f2917aed3daa0ffaff21224c45240; `internal/model/source_ref_test.go`=f3084e23db3b60fc9643a378cc1428dcf37d5d2b7294dc0e543b04f582a4df5b; `tests/contract/import_boundary_test.go`=dd3efacb7c034f340e944adba8906e6f666f4fadd546a09799cdddbc58a5b854; `tests/contract/schema_test.go`=40dde964b5fc36136ef816fc1635aa22bc26fcb000acd9c967f27ccc8d944896; `tests/contract/canonical_reference.py`=054ab582bd62d42ca0634ba77e5de32e125d0086cfa606fb06696ebc9caa2ede. Snapshot Linux kiểm hash khớp trước/sau, Python/Go tests, vet, bootstrap build, spec checker PASS theo E006. Còn thiếu các types/ports/validators và fixtures theo S43/CT-02 cùng các subcase T105; chưa gọi task DONE hay G0 PASS. Không phát sinh delta ngoài baseline nên Complexity Gate không kích hoạt.

**T105 — bổ sung, 2026-11-11 03:32 UTC:** triển khai recipe `binding-v1` đúng 5 trường, khớp vector chuẩn và kiểm sai định dạng/đổi resolver; digest không chứng minh binding tồn tại hay quyền. Import-boundary test hiện kiểm allowlist stdlib cho model, stdlib/model cho ports và cấm SQL/HTTP/exec; test ports thực tế chỉ áp dụng khi có consumer và code ports. Hash `internal/model/binding.go`=59240ea7d5f5c99459a23a9bba97f47a21ddcf46f8c39b7aac795f7f1db4b26f; `internal/model/binding_test.go`=4a13f22dcf3bac90f42e89cdf72c57441ce8b141d3e1b2b5fde8f86474ecab65; `tests/contract/import_boundary_test.go`=d2bb27902fae73fe0f90015f249b6b8b3d41a40fe20a927bbb5c8ee775e25e73. E007 xác minh bản kết hợp; E006 là lịch sử trước khi test import-boundary được tăng cường. Task vẫn IN_PROGRESS; không mở T106/G0 tiếp khi chưa hoàn tất T105.

**T105 — tiếp tục và T003 rework, 2026-11-11 05:27 UTC:** E006/E007 đã chuyển về đúng bảng evidence S21; `tools/spec_check` kiểm evidence nằm ngoài S21 và last_evidence_id không tồn tại, mutation tests xác nhận lỗi bị bắt. ScopeVector decoder kiểm closed shape, giới hạn, nullable fields và single/federated; chưa verify digest, membership, quyền hoặc current epochs. Schema positive/negative fixtures và counterexample D009 PASS trên snapshot. Hash: `tools/spec_check/main.go`=cdda8352e826c42e5425176eae34e2fbfdb570686752f8e813e97bb354afc20c; `tools/spec_check/main_test.go`=c0d1234efef98f7347018dba504afa25a4918075dd602884ccc7473340d22576; `internal/model/scope.go`=efe6ae682a5cc229b70e2d3fe932e65caa63c298e8028cd99842433771b29a54; `internal/model/scope_test.go`=44a81a4603f03661b619e35e959fe432e8f133d4d6288d4cf4f2b851425975c7; `tests/contract/scope_schema_test.go`=e75ec9163171f54b9ecee482aa1a78f157a3244ed6e280c876ad46adbe7ca5a6; `tests/contract/scope_tie_test.go`=49ac6652110d5f7ed8fa1a69d6d7be5756eae5b83f4e1e3d25e6c5c4ca59ad56. T105 IN_PROGRESS, tiếp tục implementation và proof rule D009 đã được user chọn.

**T105 — D009 và scope selection, 2026-11-11 05:29 UTC:** user đã chọn khóa phụ E(MemberUnavailable) và revision 1.6.3 ghi ở CT-04; `NormalizeUnavailableMembers` không thay đổi input và có budget, test ngược thứ tự xác nhận cùng digest oracle Python. `ScopeRequest` kiểm single/group/pinned XOR và empty chỉ khi đúng một explicit binding; không chứng minh authorization/membership/digest/freshness. Test T003 từng FAIL vì cố định `last_evidence_id=E007` khi tracker chuyển E008; đã sửa test lấy giá trị hiện tại và chạy bản kết hợp E009 PASS. Hash: `tools/spec_check/main_test.go`=a84a47257aa106c140510f9c1ba98a48e73603170dedc4772cacf8af1a02f546; `internal/model/scope.go`=a1986e1bc6089fbdf3290c94e97068ebfa99e92e7e035c76cd72710ad7d3be23; `internal/model/scope_normalize.go`=ac5dacb200e2ee2828ec222854a2fdbfa064cb4e63b1d177d7dcdbd85a0dfc3d; `internal/model/scope_normalize_test.go`=157781bf0258084e30231f0581c4d283ee12ba614e9e07d2d8a79d9decd02af3; `internal/model/scope_request.go`=eb812275faaaed78bec360102d3a081a1e4c85c9a813081e0f0b449c6f501bb1; `internal/model/scope_request_test.go`=ccbc561f07219bb1711e7d3c65d0ece78e0ee6a618b96ba246310cdc01479d7b; `tests/contract/scope_tie_test.go`=ef1e3f89d958fc87e374f5b5c4a89b0457e408d29521d36c53f7af5af183d005. Task T105 chưa DONE; còn các kiểu, schema/semantic validators, recipe và subcase chưa hoàn tất.

**T105 — GraphRef và exact member, 2026-11-11 05:30 UTC:** năm GraphRef variants dùng closed-shape decoder, `RefSelector` giữ service_id|null; schema fixture và roundtrip PASS. `MatchPinnedMember` đối chiếu worktree/profile/generation/service, từ chối ambiguous hoặc missing; không xác thực vector digest, policy, DB hay quyền. Case cùng identity ở hai services không tự chọn member. Hash: `internal/model/graph_ref.go`=122bb923248de335a29e91b7ee612792ace067d7dca740bf8d15b4a556b9f6b8; `internal/model/graph_ref_test.go`=1b2f203c4f6388106e6a7695a8ce2dbf781b7e88e368be539d4c8176e8c6ed92; `internal/model/ref_selector.go`=bf294808979b8413e8fa728c70cd571d6299dae85d518d418761018c7d2a39c1; `internal/model/ref_selector_test.go`=e5226c3cbebc8799ff132593aba2cc57916507c498b9b4fb489431ffad658251; `tests/contract/graph_ref_schema_test.go`=83a3c0c96c8ab69ca3dda8f0887eea2c7675f55ac2d822c0bf47090d1db19dfd. T105 IN_PROGRESS; chưa khẳng định các acceptance cases tương ứng đã đủ end-to-end.

**T105 — evidence và pagination, 2026-11-11 05:32 UTC:** D010 được user chấp thuận, revision 1.6.4 cho phép `static_analyzer` + `explicit_document` có tham chiếu nguồn; decoder không tự chứng minh tài liệu thực, quyền hay nguồn đáng tin. EvidenceRef kiểm origin/kind và closed shape; Pagination kiểm count/null/truncated/enumeration, không giả proof continuation/cursor. Schema và semantic tests phân biệt schema-valid với origin sai. Hash: `internal/model/evidence.go`=a66748936c4348e9cd9c04b789f10b10433909abbda61a60b4a5fce7d306e77d; `internal/model/evidence_test.go`=5053221c42a1f5215ae3d1114ad56f862769fbe0cc16f722732aa8980be352c0; `internal/model/pagination.go`=30cef10f303233aa15ae7aed36c9514893880222b4d6847a23da6546616f7c54; `internal/model/pagination_test.go`=fb52c4cf862e976e99961be176daabb029c4eba81618f1d83495e05039dbed73; `tests/contract/evidence_schema_test.go`=92cae266efdb2c33d45d6ab1d756c3307df69a2da93199dce1440d3e9e29b5a6. Task T105 vẫn IN_PROGRESS.

**T105 — Status/Locate requests, 2026-11-11 05:33 UTC:** decoder kiểm unknown/null, verify modes, Locate position XOR query, mode=symbol/limit=20, giới hạn và source-position identity; scope selection, source membership/offset thực và cursor continuity cần owner kiểm tiếp. Test đối chiếu schema phân biệt request `{}` hợp schema nhưng không qua semantic Locate. Hash: `internal/model/requests.go`=0bd524e258e9c677dbd548230697015e889976c0d9ccd37d6c5c47f372ca4ce1; `internal/model/requests_test.go`=d27a1a8806b3780978829b0d8d046533e564dacf6e466b03f63d1fa04aa21ef1; `tests/contract/requests_schema_test.go`=70c8309732677e26ef7ac2e7a3e2ccc660fc14c5d79f358ac6c330a0cd3f58c0. T105 chưa DONE; G0 chưa seal.

**T105 — request contract, 2026-11-11 05:35 UTC:** Selector closed union, Pack selector/default freshness/view khi contract nêu rõ, Expand/Validate token shape và Rename initial/continuation không trộn; default preserve_contracts=true. Schema fixture kiểm bảy selector và roundtrip, kiểm product authority, token MAC, LSP, cursor continuity, scope/state và actual source vẫn ở owner tasks. Hash: `internal/model/requests.go`=c96a3581059bb43557f785632edbbaaae65b82115f4a50a4915eb3058c5a278f; `internal/model/pack_requests.go`=407fd94e803c5dbbb48e91d640862ffeca169c298b651892bed8184be0e65047; `internal/model/pack_requests_test.go`=1f489ae40a755a6493eb6eb53091a87b6e70916d5faa21c5508121fa342665b0; `internal/model/rename_request.go`=9e93c2720d76e9040e10ad2d9de840724a5f969f44f77da9b90352990ace8ab0; `internal/model/rename_request_test.go`=e7e2ab50b6e051fd4e4c7fa5cb463f9a4d0a657dc9675fe5b491ca3fd60c98ae; `internal/model/selector.go`=5c4a9f4d8d267d8dd7e3693c9aeb52bd92845df513c62c063786e0a83ccfe7fa; `internal/model/pack_request.go`=595ffce3f78b456d920b8f36587bcef43de1d267e65a6428f1ea6a723c942d8f; `internal/model/pack_request_test.go`=87423e6bc40c0f37e8dbb3d4527af91beb54e8087c193479e3e519585a35cd62; `tests/contract/pack_schema_test.go`=e3014afde9aa63e75c995dfdf5b224fd4bede5c1280823ea39a9aedace7d8f17. T105 IN_PROGRESS.

**T105 — digest, path và preview status, 2026-11-11 05:36 UTC:** scope-v1 tính digest theo sorted preimage, unavailable tie dùng rule D009, reject resolved member cùng identity có state mâu thuẫn; kiểm hash không thay authorization/store epoch. Portable path chỉ kiểm lexical, module_root được `.`; secure OS open/case/device thuộc T005. PreviewStatusData kiểm closed shape, UTC time và không cho interaction_allowed khi revoke/expired/stale/không bound; không suy liveness/permission head thực. Hash: `internal/model/scope.go`=1cb3db932e85820f3f7acdac8f08e063afdd32f26cc88877668f2cf144821ba0; `internal/model/scope_digest.go`=3301cf9eee27013d5d77544589ce4b7aeadc173a337f70ab95bb25b957b2c80c; `internal/model/scope_digest_test.go`=768c3c7b03649074df2ccb5852e22432361ad4560be7ece76a604cddec38a4f7; `internal/model/relative_path.go`=5443e6ec38991aa196e3bdbc10d15585bd141d898bb48bd336c070cdc9ac3177; `internal/model/relative_path_test.go`=a901c835c30e30a279b095adf8a6a7beefcf78b45663fda997cff1990b6fa1ce; `internal/model/preview_status.go`=9a4f780f52ea0ae03b6c51cf5fa75abe81d40a1d923370e294d1a3ab293af1c6; `internal/model/preview_status_test.go`=ff1cc3f62ca8221102300f2b1441adc6504592133fcbd4d700aba7846253d8a0; `tests/contract/preview_status_schema_test.go`=452e4110f6562b744c5f0599f8dceda65c29d8b86b9e6cfe307254f9fd03c9ad. T105 IN_PROGRESS.

**T105 — budget/config, 2026-11-11 05:38 UTC:** Budget kiểm caps/labels/tokenizer/estimate; chưa đo serialized payload hay chứng minh host MCP. RuntimeConfig merge defaults từ block S43, kiểm tất cả min/max và các tương quan được nêu, resolve CPU sentinel; test so toàn bộ defaults và negative ngoài schema cho từng field. Chưa đọc/ghi file config, xử lý CLI override/CAS/restart hoặc cấp quyền. Hash: `internal/model/budget.go`=fa8b5adb07577b3add0a0d8ade91d7e2cf8f19a838af319eb100cbb41a5840b3; `internal/model/budget_test.go`=7c59e27460ff65182ff1fca4ce7d4db91b32dd0278d82292517d27ec3bff1f96; `internal/model/runtime_config.go`=b0e1c29874f687f37e0be572555bfc848b41dd120846ed3fb6c12c27e8b9c103; `internal/model/runtime_config_test.go`=6a39fa59410659ed984f75ddd2c91531bf35ff03e4d8dcaef788d4d9feb0a0a9; `tests/contract/runtime_config_schema_test.go`=0ec86c887e7975084bac24c659c873af7c18942181d8d945a747c4a2a8f6d686. T105 IN_PROGRESS.

**T105 — runtime config closed fields, 2026-11-11 05:39 UTC:** regression tái hiện `encoding/json` nhận `MAX_FILE_BYTES` thay field canonical; thêm kiểm exact JSON tag trước unmarshal và giữ test âm. E015 là evidence lịch sử trước sửa; bản kết hợp E016 PASS. Hash mới `internal/model/runtime_config.go`=38baabcf3e2f6c552802cd22cb9db2b59d58258c425d12b69243682c6e663168; `internal/model/runtime_config_test.go`=db44d0f035ee05247fd4d2d29b6338c3e3e5ed7d65f3550553045d49eeca6837. T105 chưa DONE.

**T105 — profile/policy/registry, 2026-11-11 05:41 UTC:** Go/TS effective profile và policy-v1 kiểm closed shapes, chuẩn hóa mảng theo CT-04, reject duplicate nơi quy định, giữ trusted_rule_paths theo thứ tự; profile/policy digest khớp Python reference riêng. Registry đọc artifact nhúng, kiểm 6 MCP/18 HTTP/7 CLI và schema refs, lookup từ chối transport sai; chưa có dispatcher, auth hay policy enforcement. Hash: `contracts/registry.go`=f7abed1816fc70b64d021b12f9166c2c227c321d97f0c8d833ba1f8422a3392d; `contracts/registry_test.go`=915991b68ef6ac8c20ff008e576494add8728ff754c5fea9ebc8f8b88167dd31; `internal/model/profile.go`=9007c8bda0379c9b931630acf2eb0a67f5e38b4b4c5905312e748db794ac4eb8; `internal/model/profile_test.go`=d003698782e4ad36310f18af875b77da3e74ebbf0d11b78b3d908b1e94142f8b; `internal/model/policy.go`=03b9588835d3be7dbaddcea14fb7f89f46d9bc625afeab6a48b947ea93070e7b; `internal/model/policy_test.go`=85a90a9b67b3ad678c0643ec2c108e9d1d5e4ff1dc384ff77c7b47be364ed39f; `tests/contract/profile_policy_schema_test.go`=b4699bf5cc8bc0c02fbe1645657eb877c3512ca75b803dface24d1bf8ed8b62e. T105 IN_PROGRESS.

**T105 — capture identity, 2026-11-11 05:42 UTC:** CaptureManifest closed shape, file capture statuses, path unique/sorted, dependency digests sorted giữ multiplicity; preimage bỏ snapshot_digest và không chứa parse/type result. Full-shape fixture digest khớp Python independent; budget kiểm trước aggregate marshal. Chưa đọc bytes thực, secure-open, snapshot ổn định hay ProviderDone matching. Hash: `internal/model/capture.go`=cf3bc3994571399ee2c940360ccff700d75dd74e62391b14fa88128117a6e9ac; `internal/model/capture_test.go`=c8e81c5e5476b9fe24d65ae0c137d2339a4129f3c7273c14c47915c1fada2dec. T105 IN_PROGRESS.

**T105 — anchor/config digest, 2026-11-11 05:44 UTC:** anchor-v1 kiểm hash byte gốc, UTF-8 boundary, kind và provider digest; runtime-config-v1 yêu cầu CPU sentinel đã resolve. Cả hai fixture digest so với Python reference độc lập; chưa chứng minh secure-open, binding thực, persisted config hay authorization. Hash: `internal/model/source_anchor.go`=60648af991678ae7223ff9a6fd823a7eba361b36ec545cafddec0d9e7a0fd974; `internal/model/source_anchor_test.go`=9881d4cbe804149376c866704f322e3c8ecad1c0338371f5d7fb13271c63691d; `internal/model/runtime_config_digest.go`=e7e4875a6d5f68ff0f04d58bd18a0164d74f5b51b6846c1477a4d2a963d76b6e; `internal/model/runtime_config_digest_test.go`=4b46cb206b56676376bc96a9cacdb830b02cd09f7f5a04b7ff176e53e25f9075. T105 IN_PROGRESS.

**T003 — rework verification, 2026-11-11 05:44 UTC:** E020 gắn lại evidence hiện tại cho tracker T003 sau sửa detection vị trí evidence và last_evidence_id; `tools/spec_check/main.go`=cdda8352e826c42e5425176eae34e2fbfdb570686752f8e813e97bb354afc20c; `tools/spec_check/main_test.go`=a84a47257aa106c140510f9c1ba98a48e73603170dedc4772cacf8af1a02f546. E005 vẫn historical; E020 scope validator fixture, không chứng minh sản phẩm/gate.

**T105 — freshness/coverage/error, 2026-11-11 05:45 UTC:** closed shape cho Freshness, Coverage, Capability, Gap và Error; kiểm UTC, count nullable, enum; observed_current yêu cầu observed byte stability. Error codes/actions được test từng enum schema. Đây là dữ liệu hợp đồng, chưa chứng minh provider completeness, observed freshness hay source authorization. Hash: `internal/model/coverage.go`=22402c857cf99ad27297772848a94fbcb68e918ff95da0d3ee66519cc48951c0; `internal/model/coverage_test.go`=6fa7c8a9ceadaa50752e4d6f610b5f05917b136b4e318fd33623af0d68c7cb60; `internal/model/api_error.go`=3eb6a786fff8414613e47717ce5deec6f4f3259af9874e8be7180c28a1f65a15; `internal/model/api_error_test.go`=0a8b9c0d04e87bb809a6ce3213ead3e68d82f593de57be8e66a0f2f26241390c; `tests/contract/error_schema_test.go`=d21662600c6aeca36025da8ad9d63b44b2892fc349d9bb10be20684dd247c154. T105 IN_PROGRESS.

**T105 — build manifest, 2026-11-11 05:47 UTC:** BuildManifest closed shape, unique source path/anchor ID, source-hash link, owner tồn tại và không vòng, deterministic sort, `build-map-v1` khớp Python reference. Chưa xác minh anchor_id theo bytes/range thực, quyền source hoặc import/build artifact. Hash: `internal/model/build_manifest.go`=76c217e5c4b6790f9197b85cdc59570d80c0c6fc7b0bf1507b4fb3baa547c135; `internal/model/build_manifest_test.go`=a3fd3480804d5145f2ff0bd11908b0127a62d53aaf68a77093bafb100db49361. T105 IN_PROGRESS.

**T105 — build manifest/captured bytes, 2026-11-11 05:48 UTC:** pure verifier nhận captured bytes từ caller, kiểm build-map digest, raw hashes, source budget, UTF-8/range và anchor-v1 ID; hash mỗi source một lần. Fixture sai bytes, giả anchor ID, split UTF-8 và budget nhỏ bị từ chối. Không chứng minh secure-open, freshness, authority hay import thật. Hash: `internal/model/source_anchor.go`=7d225a2b5f76f3beda50bc4bc2172fdff038a3486628a1ca93387d4ae2727a92; `internal/model/build_manifest_capture.go`=c501c71a7204c980319a7b72bf0057bcf268bb0ed010e49885d84b5d36ff4ee0; `internal/model/build_manifest_capture_test.go`=8cbeac0f07cade298c34d07c47de202021d683efe972d06843b153b673831c39. E019 là historical trước refactor source_anchor; T105 IN_PROGRESS.

**T105 — provider fact boundaries, 2026-11-11 05:49 UTC:** SymbolFact/RelationFact kiểm closed shape, external thiếu path phải có package/qualified identity, target null chỉ cho unresolved có reason; byte source, endpoint cùng generation, analyzer và provider framing vẫn chưa được chứng minh. Hash: `internal/model/provider_fact.go`=518738c43d6005a1ef0801c55aa3272632655c60e5eae90ca8427a0367ee1ff4; `internal/model/provider_fact_test.go`=1a78f528f858c4cdc7ee4a274e3d4515b5b6469db09391fd7becbc352bf0a126. T105 IN_PROGRESS.

**T105 — provider messages, 2026-11-11 05:50 UTC:** Start/Batch/Done/Cancel typed union kiểm closed frame shape, bounded fact arrays, profile/provider binding và Done input_digest; package/diagnostic shape. Không thực thi provider, frame I/O, sequence/job state, captured membership, authenticity hay timeout. Hash: `internal/model/provider_diagnostics.go`=06a53839794d039f2d649dd531757780450df7c1edf70974df076c6093e7f01d; `internal/model/provider_message.go`=2105cf1c6b5b97bc6646df30f7d389900f4735b6854d234a31063e9b6eac9172; `internal/model/provider_message_test.go`=4611f5636b77f4972d7252d4f708f73dd3ac80d6282b7718f2f90211af528d22; `internal/model/provider_start.go`=90d63f7d2dbc5b7e9f958eb1d80c6c913f06377a098ff6ffde1e8e906e4aa77b; `internal/model/provider_start_test.go`=23635ef9a1ec45d68c3a24c96348b3165abea8425b79af17537317dd43fb5a19; `internal/model/provider_union.go`=92e8cf5c2ea3bda5704741e61565ebb910e6d5498e956334fef0752f594053a0. T105 IN_PROGRESS.

**T105 — admin và fact parity, 2026-11-11 05:52 UTC:** profile/policy admin yêu cầu typed body + expected revision/epoch; runtime config set phân biệt expected_digest vắng mặt/null, giữ sentinel0 trong patch và effective resolved riêng. SymbolFact path/range và unresolved relation/evidence/label đối chiếu S44; không thực hiện CAS, persist hay dispatch. Hash: `internal/model/provider_fact.go`=016ee92f0917cab38d7982c981ec399417e41fa9e50a9c85cf65dbc3bad43314; `internal/model/provider_fact_test.go`=e74dbbcf10cb81cfdf9180a0c9f412d37f6999eea5b686fbb60466db31599ba3; `internal/model/admin_request.go`=92a77ee95589212910e122dfb7e374f7a9b4a0c7560af831c88447f57262acf2; `internal/model/admin_request_test.go`=6e6bfd4c88266b9196fa8c9b039b24605776e4a4ae5eb66fecb64390b660efdf; `internal/model/runtime_config_patch.go`=8a4a8035286442276d572b49bcdfdcb611de2370e814a222eab90277dd1116ae; `internal/model/runtime_config_request.go`=4522510a347c608808b5e0032a5eb802369a61bfedf976871d1c1d534852c371; `internal/model/runtime_config_request_test.go`=9d25bc0625cea86db2f9f74d484c7194a0680bc4c735a8d93e04fd0af71f877c; `tests/contract/runtime_request_schema_test.go`=8f7c7b7d6502187b6c6672289c524141f22882bb40c3164771f8850f84627e3e. T105 IN_PROGRESS.

**T105 — layer boundaries, 2026-11-11 05:53 UTC:** test bổ sung cấm app truy cập trực tiếp store/SQL, store import app/transport, và CLI/MCP/Explorer import SQL/store/parser trực tiếp; kiểm module/layer prefix và đọc import source khi các package xuất hiện. Các package tương lai chưa tồn tại nên chưa là bằng chứng implementation. Hash `tests/contract/layer_boundary_test.go`=e6531ada3d0504eed4cd407713d81fdbbcf41421725f030dfcf8faabe0ccb20e. T105 IN_PROGRESS.

**T105 — source/graph/flow request, 2026-11-11 05:54 UTC:** SourceData so exact content với captured byte range, không silently truncate; Graph/Flow request giữ typed selector/ref và hard schema limits, observed mode yêu cầu artifact ID. Chưa kiểm actual bytes qua secure open, authorization, runtime limits, artifact provenance, graph traversal hay cursor. Hash: `internal/model/source_read.go`=3b803c1e8868acb5cb48213d3e42afa99c12b439eb7677ff03afedb0460598fe; `internal/model/source_read_test.go`=bc089faed5a9d7037414f1f9c7496e863e8f2d03caef4e514f2827fb4a0f56ac; `internal/model/query_requests.go`=37c28ea77a2e215bbb6c57f7408f725a27130c2514c0b284fd68cf19fca66a69; `internal/model/query_requests_test.go`=19bc491987243fd7a47f7fdd70787f12567c942c58fd50d87b3041f305a9bca6. T105 IN_PROGRESS.

**T105 — ref selector defaults, 2026-11-11 05:55 UTC:** RefSelector chứa SymbolRef/ModuleRef nhận view default tương ứng symbol/overview; kinds khác không tự đoán. `internal/model/pack_request.go`=6cf96a00c75abb6596ffb146d1c9b0f2d9d7b3b5c3f6eb23c27c4cacd21c3c46; `internal/model/pack_request_test.go`=5e45138f05d10e090885df807fb33f4cccfd3c4d7af60d7e0ff75cf942cdaac1. E013 historical trước sửa; T105 IN_PROGRESS.

**T105 — group/record requests, 2026-11-11 05:56 UTC:** typed group config/request kiểm shape, action và expected revision; record list/read kiểm request, list default20. Không dispatch, thay catalog, kiểm CAS thực, resolve current head, authority, cursor hay support của record kind. Hash: `internal/model/group_admin.go`=54475212e8f63bb4a7f1331b72150b5b50e724c7ccc2759bfa4f4adc74e44086; `internal/model/group_admin_test.go`=7319cdb60366ac5d75117583b81135c9a22ff7786cab5f921a292980e6935381; `internal/model/record_request.go`=2ccf3b64c471b3518342371d2abd3750e22c1ee824a7dd5447ef879541c760ef; `internal/model/record_request_test.go`=ee3c30e434071e45084d6e9a4f6fbff792e680305f094b07bd950884c338bdec. T105 IN_PROGRESS.

**T105 — rename/source fragments, 2026-11-11 06:00 UTC:** RenameData chỉ chấp nhận applied=false/source_mutation_count=0/validation_required và computation_complete; source fragment kiểm UTF-8 boundary, exact content, nonempty progress và source-fragment-v1 ID khớp Python oracle. Chưa chứng minh toàn bộ pages, token MAC, LSP, continuity, actual source policy hay host apply. Hash: `internal/model/rename_data.go`=167150c1948f5747af996ecb5bc224d2bc785d0143ee24186bacee58cec88906; `internal/model/rename_data_test.go`=df413ab5416812fbe3b8aa63a0f99092686a386f5d2ac8511feb107891126b88; `internal/model/source_fragment.go`=16c82f6e22727474d639f2bafc227368e0b4ace15f5b55d24e96cda119610307; `internal/model/source_fragment_test.go`=fdbcc60494444b960b1b7c3d108ae286effcb7d54d11e46345dd068ff9d466ee. T105 IN_PROGRESS.

**T105 — wire JSON/bridge primitives, 2026-11-11 06:02 UTC:** generic JSON kiểm byte/depth/key cap, duplicates, Unicode và số finite; không giới hạn display data thành integer identity. DOMAnchorRef/InstanceRef closed shapes không nhận engine source field; Rect kiểm finite/nonnegative và có hàm bound do caller cung cấp. Chưa có bridge message union/state/actual browser test hay giá trị bound vận hành. Hash: `internal/model/wire_json.go`=791b40fb7e199ef348be126ed79f2780b8b19ff45a204a645721b9073e72dee5; `internal/model/wire_json_test.go`=80ade27fb7dbbebaca50b70c4bcbad8658733071ab9a63721ee5f027c87cec04; `internal/model/bridge_primitives.go`=066bbd1e7deaff0707dd550fdbfd25c6c29d05be94ce37bf12d1d1c4780280bf; `internal/model/bridge_primitives_test.go`=a9ca82ce8a54ab4f80ab4794b2686f5f17a108e3ec8d87c9832105548fcf608e. T105 IN_PROGRESS.

**T105 — bridge union, 2026-11-11 06:04 UTC:** typed bridge message/payload union, max16KiB, closed fields, nested refs/rects, epoch mismatch trong PICK_RESULT và không nhận DOM text; 23 variants có schema fixture + roundtrip. `scopeObject` sửa reject null object rỗng. Chưa chứng minh origin/source window, direction/nonce/replay/state machine, viewport bound thực, TS parity hay browser. Hash: `internal/model/scope.go`=d88ef58725059eb499f5fad9986181593487150f4c0849bdaad5261f84f6fd9b; `internal/model/bridge_payloads.go`=bde86f12117cc7c6aec153c2f1b808060d3d9bdf49ccc4dcd9a696ee66bbbfdc; `internal/model/bridge_message.go`=48b9fb3974e41432a6c93dafc8e25bc5b81c282116fc6d4fcd19028785e16db6; `internal/model/bridge_payload_decode.go`=66f17e78e1640979b3a99ad6b01f4db1b5df0d4a4edb4ce77d22e80715573765; `internal/model/bridge_message_test.go`=abc432b01911a5fccdf15655ec24cbb9c96950f0afd6930000dce19b1de421ce; `tests/contract/bridge_schema_test.go`=4bf1b7c286e8dbefe3855bb2be28b466dbc51480e088fc81345b912795989f6f. T105 IN_PROGRESS.

**T105 — bridge pick bound, 2026-11-11 06:05 UTC:** pick coordinates có kiểm finite/magnitude theo bound explicit của caller; chưa chọn hay chứng minh policy viewport, session origin hoặc browser runtime. Hash mới `internal/model/bridge_payloads.go`=763aa549b82f3617e8f32108d4335974d3f3942f8d24501bfcb257b063b6e660; `internal/model/bridge_message_test.go`=3399a51fe72d6bc4fea348abe29550c5e22f38c81b64ea679b9cb7e50ac667ea. E033 historical trước sửa; T105 IN_PROGRESS.

**T105 — mapping pair/binding, 2026-11-11 06:06 UTC:** typed MappingResolveRequest/MappingData/MappingMatch giữ anchor-target pairs, giới hạn và kiểm build map/binding; fixture nhiều cặp, schema-valid requested binding sai bị reject sau regression. Không chứng minh nguồn, resolver, session auth, pagination hay DOM. Hash: `internal/model/mapping.go`=21e56a26e1e2e60dad29e6238eafbdda7298315f32edb48ecd806ddb785f79e8; `internal/model/mapping_test.go`=b6c1ec918b12b3ab8b1aa10a91db7de6aaf8476b39851acc897d27ca21d78b29; `tests/contract/mapping_schema_test.go`=18428b48ece4c7fa8ac300a469ac09b5493300d3a9caf90a5def53ae4f0867e5. T105 IN_PROGRESS.

**T105 — graph data, 2026-11-11 06:07 UTC:** typed graph node/edge/page kiểm closed shape, ID unique, page-local endpoints; schema-valid edge thiếu endpoint bị semantic validator reject. Chưa xác minh digest, boundary evidence, capability registry hay quyền nguồn thực. Hash: `internal/model/graph_data.go`=3437296153f677ec35240f68792f6144953d74b364d8ccf42d43989c85169d37; `internal/model/graph_data_test.go`=89175267f6c46cfad0444a615249f04ad76dd938215794aec3a99af6fd7a29ba; `tests/contract/graph_schema_test.go`=e62af1eec80208a13027689728dbd250840469f787c10eea6bb57555d59e794f. T105 IN_PROGRESS.

**T105 — flow data, 2026-11-11 06:08 UTC:** typed flow step/edge/page kiểm refs, ID/endpoint và không cho static mode tự claim causal trace hoặc observed outcome; schema fixture cho thấy schema một mình không xác minh semantic đó. Không chứng minh trace provenance, source truth, execution, auth hay pagination. Hash: `internal/model/flow_data.go`=e1431198a513e5ff8d68258889d739cd534f24e060ca74504dd22b21fd693dde; `internal/model/flow_data_test.go`=372762523264b643a16ce0a34bcc9468cba06ef07412539875766b26a32eae95; `tests/contract/flow_schema_test.go`=76cc622b1167d687c3783b61a2f9b2fa9a24fa65a30f8d7da253c34f449f4192. T105 IN_PROGRESS.

**T105 — core response và source fragment, 2026-11-11 06:10 UTC:** E038 kiểm closed shape của status/locate/validate/pack và ràng buộc fragment với source bytes; chưa xác minh authority, freshness, pagination hoặc quyền. Hash: `internal/model/core_data.go`=babf5b439ac3ab03b5747ae82d2e5344f4fd752c57a5d927f8d7bd1ed5014c74; `internal/model/core_data_test.go`=44a7eeb3f7f5f08c3d056529e7da73159af7d59e5302b797516fda268f4a9a4e; `internal/model/pack_data.go`=5e5e8e1ea09b63f1288433b7f23b7e24228788c11c1b372c3102568cc695ca26; `internal/model/pack_data_test.go`=ce319881c43fb3d5ab78fa7d86bf646a811a7867194a250ce19f2598d1bd45cb; `internal/model/scope.go`=c8f6a5afe5a5ec793ef053bac426c6718c5090e12aacfbedd7d7c1b5d88f9f41.

**T105 — record và preview status, 2026-11-11 06:11 UTC:** E039 bổ sung shape record list/read, UI mapping typed refs, preview status request và test đối chiếu schema; không chứng minh quyền, tình trạng nguồn, trạng thái record thực tế hoặc SDK liveness. Hash: `internal/model/record_data.go`=cfd7412435b4f5300d77d02173c59fce1a6087294e0dc011eb774b1bc44d06fc; `internal/model/record_data_test.go`=8d443c218b9d1a65d36300e025f44c889b0d9e50886f93dbf19caf49ab4f6912; `internal/model/preview_status_request.go`=92686a05cf74614193692e9ce12dc74385f47b140b207c9d462da9b3183b9034; `internal/model/preview_status_request_test.go`=4592f6d12d99844e2f43fad2bf9b6ed15554f94925e935abc9dc8ad8f224da99; `tests/contract/record_schema_test.go`=3ce15cac31e13b92eccc2919137e1ada0cf7bf48bef11f8f346792dffc8af038.

**T105 — admin và bootstrap shape, 2026-11-11 06:12 UTC:** E040 kiểm group/profile/policy response, bootstrap request/response; không cấp hoặc kiểm credential, expiry theo đồng hồ, quyền hay trạng thái catalog. Hash: `internal/model/admin_data.go`=202b1e898465e8d830abf55f984bdc864cb3186115759efff5c66ebe7d5eac83; `internal/model/admin_data_test.go`=e858c258f3b78b4e3bfbcbaee807cb0e498a32bb6ae285b564612b3322bdd8d5; `internal/model/bootstrap_data.go`=d8522d0df906e0ebd225c6791e3b869adc37850d67d35c4e743e3bdc5faecdd5; `internal/model/bootstrap_data_test.go`=0e88a4e1f901b4806ffc9115ad5d24a6f0f2f3491c5011f221556ccdbcc4cc12.

**T105 — mapping và preview request, 2026-11-11 06:13 UTC:** E041 kiểm closed shape, bounds, enum, revision/epoch của mapping propose/review và preview attach/bind/mode/ack/invalidate/detach. Không xác thực origin, nonce, CAS, quyền, transitions hoặc anti-replay. Hash: `internal/model/mapping_record_request.go`=e8fb8c1b375533aafe42761bfea40672c40c34b2a834de92bd12b22097f719cd; `internal/model/mapping_record_request_test.go`=fb1f4681d1a56855ebe1c95bdb23e20297fd29dca8bd977e7799d020fe7d94c5; `internal/model/preview_requests.go`=d492b3fae618a6e0f3d0a400a2e19ab6dbaae9259338c29bdf5ed3ad2d217005; `internal/model/preview_requests_test.go`=c2881c7317cd15888bfd3060ef5f8356a84a8752d7c20a77fa9b058012db0f75.

**T105 — preview permission recipe và response, 2026-11-11 06:14 UTC:** E042 kiểm preview/record response shapes và `preview-permission-v1` với digest Python độc lập `b873da0467ce3939ccaf4cb176567a20eece3ade2568fb62d597b89409eafc31`, sort-unique. Không chứng minh browser origin, attestation, thời hạn thực tế hoặc enforcement. Hash: `internal/model/preview_record_data.go`=01a34520a212d8354c29e38c09477d81e0fc3e882284d04ab8dda6a60f34c2ed; `internal/model/preview_record_data_test.go`=3165bcfbac07f20ef3e5614101a0a20b0af0a0410c89ef5a5d97ead39b23e97b; `internal/model/preview_permission.go`=81b2879e8ebcebb1ba49ea2b2d283c4b39660fe8e9e6bf59217a5e9119749cf2; `internal/model/preview_permission_test.go`=b8f9e549e53016dd53e0345510c372471434481ccf05bf4a623a77f15baa76bc.

**T105 — permission admin, 2026-11-11 06:14 UTC:** E043 kiểm request get/list/put/revoke, expected_version, reason, profile/digest summary; không thực hiện transaction hoặc xác thực actor/attestation. Hash: `internal/model/preview_permission_admin.go`=24e396406ee7a56dd9a668fecb0420b9de8a1cbfa18da5e26cc5e0e899f658ce; `internal/model/preview_permission_admin_test.go`=679982ce3ca4ebf3e5b5e690c397e3ae32e1cc54d5d7fad5e3c8366cf5146405.

**T105 — response union, 2026-11-11 06:15 UTC:** E044 có union cho 20 loại ResponseData, đối chiếu operation caller, SV08 status/readiness/error/failure data và effective config digest. Partial-data authority phải do operation contract cung cấp; chưa kiểm runtime authority, số byte thật, cursor hoặc freshness. Hash: `internal/model/response.go`=a49243f9514dc4b02c478346e1466b7cca1198dcc5cbcd6a9eaecfb1e1b55eb8; `internal/model/response_test.go`=175e17d5bfd66aead3de95c9d719b37a9d5f7474c5dc8f474beb9395d42ba05e; `internal/model/runtime_config_data.go`=bc050be5f5b83d8f7d373d20bd0bfc0d2c318d38075efe7711535955e8029092; `internal/model/runtime_config_data_test.go`=ec4c1bfff607b29e053f3d79e0567d200a26773f9ed3c01c2e8b88a7028738bd.

**T105 — request union, bootstrap context và reference digests, 2026-11-11 06:17 UTC:** E045 nối registry transport tới decoder mọi request; reject trả nil, kiểm bootstrap context và hai digest `graph-ref-v1`/`resolver-v1` với oracle Python độc lập. Không chứng minh artifact bytes/options, authorization hoặc transport chạy thật. Hash: `internal/model/request_union.go`=6ac6c63cacf87d4a592220557b647b70cdd5a580baecf7c6474842fc37b6f1b1; `contracts/request.go`=28a87f14404c8f7edfb16e9f19ec81b9541a33596690fdef60eb695a80e11155; `contracts/request_test.go`=309d6b87509cdfe2f4d4c0e8b97c675ff9bedba561b7703d27addea24cf58f78; `internal/model/response.go`=9af2c99d8b88fef3292ca4739d72416e4f8d6b5293bc59887941c5ac555abb88; `internal/model/response_error_test.go`=c078c047370168a1979a796e406c6aa1814aa606cee0b2968142a3da390c7066; `internal/model/response_bootstrap_test.go`=4b5efcb7a2794708268d34168680ee3dc3c3e436466a7c17318ac39894e4e8a5; `internal/model/reference_digests.go`=0fa79051aeffefdc12b2207087d677f91cf022c5f125b4b21fbc96883de0df3d; `internal/model/reference_digests_test.go`=53cede9b2112e0a7ce435469fd6061de815eca196317ed2b94e51d46c48276bf.

**T105 — registry response binding, 2026-11-11 06:17 UTC:** E046 kiểm nhãn response operation theo từng endpoint và schema; failure data mặc định reject cho tới khi contract cụ thể cho phép. Test đầu FAIL do parser test ép mọi `const` về string, sau sửa chỉ đọc operation thì PASS; chưa là transport/runtime proof. Hash: `contracts/response.go`=afb1b9d8a88eea6216bbd28f155bbaa1bdbb7d9e1abfe4f69cc0eca708a53aa2; `contracts/response_test.go`=1e352bc20db64ecabc251a87c1229a76981a05d884cb3e6d7df6f34eaa0df81f. Race NOT_RUN: snapshot CGO=0, WSL chưa có gcc/clang; không suy ra product/platform verification.

**T105 — source length invariant, 2026-11-11 06:18 UTC:** E047 reject content có byte length khác SourceRef range (kể cả fragment); xác thực hash/source vẫn cần captured bytes và quyền ở owner. Hash: `internal/model/source_read.go`=18b5970ba4042c2397788c88d33d6a58354e3f9f8d564eac6dbbaf008a959134; `internal/model/pack_data.go`=f240c7309a8db440fe7c49fa4e487c46471ba0bd09fbf58993ab6b956c8eaabf; `internal/model/source_length_test.go`=df80430cc5bbb83722addddbae921fca2bd1993f548ffb7f4b9b835e68382b5e.

**T105 — fixture parity registry, 2026-11-11 06:19 UTC:** E048 tạo fixture minimal từ schema cho 20 ResponseData và 27 request types trong registry, kiểm positive + reject unknown field ở cả schema và Go; không thay thế toàn bộ semantic, nested variant, state/cross-page hoặc actual runtime tests. Hash: `tests/contract/response_variants_schema_test.go`=5c76529a34443c544f18620c27000af0a0aa6c42adb4dacc7cfb0b25c4825924.

**T105 — mutation guard, 2026-11-11 06:20 UTC:** E049 vô hiệu so sánh SHA-256 chỉ trên snapshot cô lập: wrong_hash test FAIL đúng kỳ vọng; khôi phục đúng bytes theo SHA-256 rồi test PASS. Đây là một invariant test, không đại diện mutation score hay proof security toàn hệ thống.

**T105 — request/record digest, 2026-11-11 06:21 UTC:** E050 kiểm `request-v1` với principal, kiểu request/schema và `record-payload-v1` cho UI mapping, giữ raw rationale; oracle Python độc lập. Continuation fingerprint còn cần loại controls theo contract operation; principal authority và mutation replay chưa implement. Compile test đầu FAIL do dùng map thay CanonicalValue; đã sửa dùng CanonicalObject và toàn bộ test PASS. Hash: `internal/model/request_record_digest.go`=a62fb88f5d2780e43fe7251e436f9d0dfbf8fb987201cb3f429859e9de0ee9a6; `internal/model/request_record_digest_test.go`=68d93bce78f473e5785e8449abe66a3c094f9178973eeb5ba7d2552e149267b9.

**T105 — subgraph digest, 2026-11-11 06:22 UTC:** E051 kiểm `subgraph-v1`, sort node/edge ID, unique/endpoints, >1 page và oracle Python. Scope, authorization, sanitization và enumeration completeness vẫn do caller xác minh. `rename-result-v1` còn thiếu file_preconditions shape và conflict order; đã hỏi user, chưa tự đổi authority. Hash: `internal/model/subgraph_digest.go`=20123060e825f165be6091d16f817f813a945a1a5bf45887ae2f4a45968be66b; `internal/model/subgraph_digest_test.go`=3d640c35aea75a6bfe7cceabedfb0069ec9a32b9d99c7aa18e693a5df8edc4d0.

**T105 — registry-bound request digest, 2026-11-11 06:22 UTC:** E052 ràng request hash với registered transport, operation và schema; principal vẫn phải được xác thực ở runtime. Hash: `contracts/request_digest.go`=995767a947c342322c14fad6e4d0148d3c91ee0d8418f7669654f45904dbb091; `contracts/request_digest_test.go`=41755219569d8145e2c19b95bfe104e5c6e2accc60904cbf7d9a83c9ef9d5b19.

**T105 — permission status fail-closed, 2026-11-11 06:23 UTC:** E053 reject summary khai báo revoked nhưng effective approved; không thay kiểm head/expiry/actor. Hash: `internal/model/preview_permission_admin.go`=8aba437237c3fcbfb3f7be623f066e36f1fa5d001664184449975472bc8c3e76; `internal/model/preview_permission_admin_test.go`=44ac27805c307503db0ad12d711c809795b96af3abf84febdfc1556f230b2a09.

**T105 — rename result digest/D011, 2026-11-11 06:24 UTC:** E054 kiểm precondition sort, conflict sort, duplicate/overlap và oracle Python theo lựa chọn user. Không chạy LSP, apply hoặc xác thực source bytes. Edit cùng (path,start,end,new_text) nhưng khác identity hiện reject fail-closed; đã hỏi user về quy tắc đồng hạng, chưa tự định nghĩa. Hash: `internal/model/rename_result_digest.go`=236a19e8e55f2f23abfcbf589432043fdb56b465e8f084bd19dd1a804cf97016; `internal/model/rename_result_digest_test.go`=d8705aebbdb777c49ac4f8bffa703a0f29d5f5f2bc15307b2b531ee485aca09f.

**T105 — scope postcheck, 2026-11-11 06:25 UTC:** E055 so digest vectors sau xác minh cả hai preimages và phát hiện record_epoch đổi hoặc digest giả; caller vẫn phải đọc trạng thái tin cậy từ stores. Hash: `internal/model/scope_postcheck.go`=6631d5ca30f66951f9387cc46cd306d8bd5e51fcfe3be3b8b3acb770ef691108; `internal/model/scope_postcheck_test.go`=5edfa71b60d4fa3db317d607853b64c9dc7ba6d156274a281064b4a470cf46e0.

**T105 — rename edit tie/D012, 2026-11-11 06:25 UTC:** E056 áp dụng user choice sort phụ E(toàn edit) cho edit đồng hạng; hai worktree, đảo thứ tự/dedup và digest oracle Python PASS. Không chạy provider/apply hoặc chứng minh quyền/captured bytes. Hash: `internal/model/rename_result_digest.go`=74081c9a2fac657df2021a2d7603820d2ec8cd0f6cdb5ecad4a4ca88161c2ef0; `internal/model/rename_result_ties_test.go`=6cf3fb617fa835d5ef9a6c7832f7e422edc8e171e36a9bdb452695684eb589d2.

**T105 — pinned source identity, 2026-11-11 06:26 UTC:** E057 so SourceRef với vector digest hợp lệ, reject service ambiguity và sai identity; chưa thay captured byte, policy, store epoch hoặc authorization. Hash: `internal/model/source_scope.go`=652fe6b026c68c659e30f339ee08359e4e29eeceab3be807ba733ef3e6d8c60a; `internal/model/source_scope_test.go`=b236c6db14422888eaaf807568867e7adba38fb341cccd24315b8ef071001f46.

**T105 — record status fail-closed, 2026-11-11 06:27 UTC:** E058 reject effective approved khi declared chưa approved hoặc snapshot đã expired; không tự khôi phục snapshot hay kiểm store thực. Hash: `internal/model/record_data.go`=705d22d3055bc17c7aeec04a289329378a98e8ea8e89c2e2f4c690fa5402cd02; `internal/model/preview_record_data.go`=3266be3f5cf93af05bceb71555c4f71a52100291bcc9c6d76454eb152bc18951; `internal/model/record_state_test.go`=bdfe158f3ed551b68607c31c4623c0f3d09fcb581747f7baaf35f484d8575a03.

**T105 — nested scope normalization/D013, 2026-11-11 06:28 UTC:** E059 chuẩn hóa scope trong subgraph/rename theo scope-v1 sau user choice, test đảo thứ tự và Python oracle PASS, không mutate input; digest không chứng minh quyền/state. Hash: `internal/model/scope_vector_normalize.go`=b72444836c238a56180acfa6f80056354c936c6c0239ad68ba3e1e5c182c5c4a; `internal/model/subgraph_digest.go`=65431851df55bd6ce9dc11797369c1fa16daf9d8440356a4790b8a364bbba948; `internal/model/subgraph_digest_test.go`=72b6174f9ddaa68df3401e9e26722d5018bc6278a01229a6672dbd9a085f6a49; `internal/model/rename_result_digest.go`=47c093da77b14eb906b420206e32da10e81fe8b5f4e988288fb7ec9f5305355b; `internal/model/rename_result_ties_test.go`=7b106843efbdc62c07c3f3ee9d0c7c4963e088c1c2335effeb466394a8bc3c77.

**T105 — nested scope selector, 2026-11-11 06:28 UTC:** E060 sửa decoder trước đó chỉ kiểm SelectionMode nếu caller gọi riêng: scope lồng reject group/single/pinned trộn hoặc thiếu identity. Empty selector vẫn cần caller kiểm số explicit bindings thực; digest/authority không tự xác minh. Hash: `internal/model/scope_request.go`=4b8468a81a124217ec9ad4d6465e6ca3a8d4099a794d4f2fa216244163eab177; `internal/model/nested_scope_test.go`=52376ea2f5c051208c7185d39242ed2dd1aa728c0929cf933d1ad086e42f4687.

**T105 — delivery accounting, 2026-11-11 06:29 UTC:** E061 kiểm tổng byte các block caller cung cấp, gồm hai bản mcp_dual và UTF-8; reject thiếu block, sai serialized_bytes hoặc vượt cap. Chưa chứng minh MCP adapter truyền block thật, fixed-point serializer hoặc host compatibility. Hash: `internal/model/delivery_budget.go`=21edc8a551de9cc4b08b72b7f0db7864bee32920dce1d4becc643a82d577963e; `internal/model/delivery_budget_test.go`=f884fb27db43ec8b34b0d72690a6e6d785a50872785e4bdd6455ae84e47f45a0.

**T105 — bounded serialization fixed point, 2026-11-11 06:29 UTC:** E062 kiểm hội tụ serialized_bytes từ zero trong tối đa 8 vòng, tính MCP dual và reject vòng không hội tụ; callback phải serialize đúng count, actual transport/host chưa kiểm. Hash: `internal/model/delivery_budget.go`=c9441e40685845d12cc3596f166a173f11a603a6d4cbc4010ddb599b73166c01; `internal/model/budget_fixedpoint_test.go`=48187007db35b653c91dea62b9efc6f47e4d7093e1a98ad08389fb13e9395608.

**T105 — fragment continuity, 2026-11-11 06:30 UTC:** E063 kiểm coverage liên tục, UTF-8/Lào/emoji/CRLF, identity và byte exact; source hash kiểm một lần cho chuỗi. Không chứng minh cursor, pagination runtime hoặc quyền. Hash: `internal/model/source_fragment.go`=4cefafaad3902ce938c7b0eee0531dcb0c90c776499d6ee725e5487319955fb4; `internal/model/fragment_sequence.go`=cf83829eccac6d15e1b55766f503c72febfdd290e8e4523cce2dd637fefeb7e5; `internal/model/fragment_sequence_test.go`=a6a36a41f24dc0f5f2abd50913d19765aed3801c5fde078ff02c3e6ed3000ecb.

**T105 — continuation shape, 2026-11-11 06:31 UTC:** E064 reject cursor lặp nguyên văn, pending page không cursor khi operation hỗ trợ, hoặc cursor không có request schema; token authentication/decoded progress/scope/expiry chưa chứng minh. Hash: `internal/model/continuation.go`=448f569e0ff49a3f0c7841076a65428b64372b67cbe956d90cabff70fa6fc344; `internal/model/continuation_test.go`=170be9eaeeb8bd702655e79e431c397173851fb3e5148ea5b46fecce8fd5bce1.

**T105 — MODE_ACK correlation, 2026-11-11 06:31 UTC:** E065 test ACK đúng và từ chối epoch/sequence cũ, timeout, revoked; caller phải xác thực origin/source/direction/replay/liveness, trạng thái tin cậy, monotonic clock, server CAS và shield. Không là browser/runtime proof. Hash: `internal/model/mode_ack.go`=a5ce92be1d9c9f205aa7a101735c16106a5a4be532c7ed89b09626f00db086d4; `internal/model/mode_ack_test.go`=a245aa7c86c8801168f8dd929c46921b01882101abe76be9fcc4989c274c0355.

**T105 — SDK PONG correlation, 2026-11-11 06:32 UTC:** E066 kiểm reply_to/probe/session/epoch/sequence/deadline và reject PONG unsolicited; không gia hạn lease, không xác thực origin/source/replay hoặc chứng minh SDK liveness thực. Hash: `internal/model/sdk_probe.go`=d67c04b6c862f8af15bcc5c0a4387e9a1b46911ea59e68da06f145b257d9d4e2; `internal/model/sdk_probe_test.go`=887aec7e45bd43b76bd1c4746de12e942570591daffc3d4dc8b547ac66575773.

**T105 — ACK negative cases, 2026-11-11 06:33 UTC:** E067 thêm fixture revision/authorization epoch, permission version, state stale, binding/build khác, expiry và ACK rejected; vẫn chỉ contract test, không browser hay CAS runtime. Hash: `internal/model/mode_ack_test.go`=b645ec1b49a6ce5742b64cdcea727c47a3c00d860e93059695dc6c0d2f49a578.

**T105 — rename service ambiguity, 2026-11-11 06:33 UTC:** E068 fail-closed nếu precondition match nhiều service trong vector; không giả chọn member. Khi có authority chọn service rõ, owner cần xử lý đúng contract, không suy support từ hash helper này. Hash: `internal/model/rename_result_digest.go`=2996e8d6574c55402bdd388ae44a3bac32f73389a9d203a48a4a21a514f57bc2; `internal/model/rename_result_ambiguity_test.go`=b941200b0e5d622c2de1180496890709bbdeb147348de48364f60d544e46c6a8.

**T105 — analysis snapshot readiness, 2026-11-11 06:34 UTC:** E069 reject pack analysis_snapshot claim reviewable, observed_current hoặc edit_precondition_validated; live validation và runtime wiring chưa chứng minh. Hash: `internal/model/pack_response.go`=d9464db568cf20a18f0b0ab49727e2377097e118dc31a9606077be54cac6c704; `internal/model/pack_response_test.go`=0e0ce0ea4627ca7a70e3f5319e29e9a19a75c0e37585ea8239fa8a89cff7d3e0.

**T105 — nested pinned request vector, 2026-11-11 06:35 UTC:** E070 áp dụng D013 cho vector trong initial_request, đối chiếu request/result vector và reject record_epoch mismatch. Test đầu FAIL do assertion tái dùng error của negative case; sửa assertion rồi full run PASS. Hash: `internal/model/rename_result_digest.go`=4916294ba7388a30a099412f46d31975743e7d70acdfc0298aaf719b42c74a32; `internal/model/rename_result_ties_test.go`=6aa594dcfa5d9a23e0db33868aa9407d7459a4deb261422a858540437e9dae6b.

**T105 — exact event origin, 2026-11-11 06:36 UTC:** E071 so origin được duyệt với origin canonical từ browser event đã xác minh; reject credentials/path/query/fragment, localhost khác 127.0.0.1. Không xác thực event.source, normalize tùy ý theo WHATWG, kiểm path/attestation hoặc browser thực. Hash: `internal/model/preview_origin.go`=d05114348cf3c3ed88eca0ee634cf3efa8a4958d0feb608651482ee07cf2b164; `internal/model/preview_origin_test.go`=d2ea36285d3ca907a7ff8c76de2f8c11ee01acbe2aecd11d4a6a1646f5e5e649.

**T105 — import/global guards, 2026-11-11 06:36 UTC:** E072 tăng guard app→HTTP/provider/adapter và bắt mutable globals tên current_repo/current_session; packages tương lai chưa tồn tại và alias/global state gián tiếp chưa được chứng minh. Hash: `tests/contract/layer_boundary_test.go`=e2bdbaf9f2fca30213e724a6358f58153fac38428ebd566acf42e2090dbbc5cc; `tests/contract/global_state_test.go`=a77de10d6f2d67db906a83f52d7d9ba60b685d901df3fa63346726f2567171fa.

**T105 — long source fragment fixture, 2026-11-11 06:37 UTC:** E073 chia một dòng Lào/emoji dài hơn 4096 bytes tại UTF-8 boundary, kiểm ghép byte và toàn sequence; fixture tự tạo, chưa test actual paginator/output envelope. Hash: `internal/model/fragment_long_test.go`=cee76e1629bc0e19c267b707cf5c2fbe86bb2181492fd64347741b4de1ab6db0.

**T105 — status freshness scope, 2026-11-11 06:37 UTC:** E074 reject status response claim returned_files khi operation không có result files; không chứng minh workspace thực được kiểm. Hash: `internal/model/response.go`=537aa86659845e40b823bd11a301c5d6b592fc3b42af38c7d1eaefbf93b1fc2d; `internal/model/status_response_test.go`=9565a4e1a833316db54e35f937ee5cfb1324fbf2eab6ea2d7c8c878d0ed864da.

**T105 — provider stream reference, 2026-11-11 06:38 UTC:** E075 kiểm job/sequence liên tục, terminal và input_digest; không chạy worker, xác minh bytes/permissions, frame, deadline/cancel hay publish. Hash: `internal/model/provider_stream.go`=b362a6c6470a89f7aeb03222126fcff27251e5e3f3ed0ec295d2e222482c2c81; `internal/model/provider_stream_test.go`=4a8e3e3dceb259497ca6f15ac49a0cb1e6c9764a9857424040c3edcba80c446a.

**T105 — 1025 edit fixture, 2026-11-11 06:39 UTC:** E076 kiểm digest ổn định khi đảo 1025 edits và không mutate input; chưa chứng minh LSP chạy một lần, spool/pagination cross-process hoặc apply safety thực. Hash: `internal/model/rename_large_test.go`=e6b4ea7c8d2df4e03efefb81369a254dce2ddb86cc6b20f5679c58c65e95eb5c.

**T105 — provider Done required, 2026-11-11 06:39 UTC:** E077 chỉ trả terminal status sau Done hợp lệ và giữ nguyên status provider; không coi claim complete là quyền publish hay chứng minh facts. Hash: `internal/model/provider_stream.go`=b3b7599d14a3a390eca48d38b15c78233074351b8689a3893c517588b09abaf9; `internal/model/provider_stream_test.go`=09d6b3d1f6a0970a91fc45aa57e51af0bd1f50cef810e62fd9caf2ad87e46ee8.

**T105 — runtime/policy caps, 2026-11-11 06:40 UTC:** E078 tính core limits không lớn hơn policy/runtime/wire cap và không mutate config; chưa thực thi policy, đọc file hoặc CAS. Hash: `internal/model/runtime_policy_caps.go`=5e7fcb350c6904d206000d60d43ebc0f2e4355cec4c213d2e90cedb0058a72a3; `internal/model/runtime_policy_caps_test.go`=2e437d2d72c503c36643a71c860c7b3e6be8122aea6ca2e2a7afc493177a1069.

**T105 — enabled provider keys, 2026-11-11 06:40 UTC:** E079 reject key ngoài explicit allowlist và duplicate; nguồn allowlist/profile provider phải do runtime trusted cung cấp, chưa enable capability. Hash: `internal/model/enabled_keys.go`=feff475328fff484404e8906ae8607fc725a480a08bf6256b89c316b1eb73831; `internal/model/enabled_keys_test.go`=fbce5387d7ea7ad0eed817fe4633410a19983990fd373f2b59a17d9a128b8b14.

**T105 — mapping cardinality fixture, 2026-11-11 06:41 UTC:** E080 kiểm 101 anchor-target pairs trong hai slice và reject nhãn exact khi enumeration chưa hoàn tất hoặc total khác1; total phải từ enumeration tin cậy, chưa chứng minh cursor/store runtime. Hash: `internal/model/mapping.go`=09d6b0fb96417f529604d99ba6c3d72e38eadb3debfa0fec88ca1a8ef52a4a7a; `internal/model/mapping_completeness.go`=1364f7f8059a25061fa9cbed1a5f1d3da6b9748a26ca5d186656045b66a0d56e; `internal/model/mapping_101_test.go`=f99be8179f06d56bd4d21ceb1003c61c88f3c5f72a807b3ba3bc590daee99376.

**T105 — provider frame hard cap, 2026-11-11 06:42 UTC:** E081 khóa max16MiB tại union và từng decoder, vẫn tôn trọng limit thấp hơn; chưa kiểm header, frame transport hoặc negotiated limit runtime. Hash: `internal/model/provider_frame_limit.go`=ec655b07e9a3a0b97751af5a9dc9c9d256da9bf6e81206c4c9ab231043875b9f; `internal/model/provider_frame_limit_test.go`=516aa1b9ee384ae4657b43d7a55a65a19179484294264d24d49988e2cc76390f; `internal/model/provider_message.go`=2d9c2e29924534bb150500937cb91a1584a4ab5031c39456fd01257399d5dd83; `internal/model/provider_start.go`=afc7fb6bca4a127cdcc8c5950ec856920d47cecc12fe707d610967b420e2e84c; `internal/model/provider_union.go`=adf33f60c39943c7d472755ce0e6dd4a8309a8770429479b54ba7d1a17cead8c.

**T105 — registry wire caps, 2026-11-11 06:43 UTC:** E082 giới hạn request 16/32/64KiB theo nhóm, response core128KiB/UI graph-flow1MiB, vẫn tôn trọng cap caller thấp hơn; transport phải chặn trước khi đọc/allocate. Compile lần đầu FAIL do trùng tên biến operation/label, đã sửa và full checks PASS. Hash: `contracts/limits.go`=518e068802ee361bce27986ea73e946a42cfee94e6693ecf108498e071ded303; `contracts/limits_test.go`=3b41cf64987de7c59b5465bab6d7f106ca9b6e68e70ee629578f016a9b9cb8e5; `contracts/request.go`=3dc6f81fa7bbb193fd21d300eb1929d91d1d031e5d04e0e571bbe3c14e1dda07; `contracts/request_digest.go`=5b79eb099ec4327ff58741b65882c40a4f5fd1fa77c15d9c87d16a8fe3a5b732; `contracts/response.go`=030cbda8fd5335a660de5c399296c2c242ec1d7f3a84f1fad5ff3c0e3d239bdb; `contracts/response_limit_test.go`=97a6589686ea1adf3335080e2bb858d553808ff3e2162a86ad181f03ab5d4a4f.

**T105 — core response budget, 2026-11-11 06:43 UTC:** E083 reject budget khai báo vượt hard cap của operation dù body nhỏ; không chứng minh byte tool-visible thực/SDK host. Hash: `contracts/response.go`=19b2d0fb3c962e93768c59f77a146036e569f6a15aefab0088bc6de82ef37c50; `contracts/core_budget_limit_test.go`=a407e094c2ad3674c1821afd1e195acbc6563cac5dabafee48c0f505adee48a8.

**T105 — preview path/D014 và resolver version, 2026-11-11 06:45 UTC:** E084 kiểm ranh giới segment pathname theo user choice và reject branch name/phiên bản không hợp lệ theo [SemVer 2.0.0](https://semver.org/spec/v2.0.0.html). Path helper đòi caller cung cấp browser-canonical pathname đáng tin; chưa chứng minh URL parser, authorization, query/fragment policy hoặc browser. Hash: `internal/model/preview_path.go`=5142dbcfa35819c196b9612db944e3cbec37f4b5b1107e29852da5e9dc7de3d5; `internal/model/preview_path_test.go`=1a261e4eae88abf098e01fee693a3c7fd626dff7073d67ff7ddb73ad97c40809; `internal/model/semantic_version.go`=0810d13e806d4beec2e317886979ba51a99fa2fe7686e578d2cfbb7da1328c87; `internal/model/semantic_version_test.go`=5331d9e99d20fe16eb23e2e676856d00d549bac435532cae7c2b1b7082e99734; `internal/model/reference_digests.go`=693d560556d4769b77b5d9d955b835f8797c929ecdf7a8e97c59fd7039e6d852.

**T105 — interaction shield decision, 2026-11-11 06:45 UTC:** E085 fail-closed với hidden, PONG timeout, input clock lùi, revoked/expired, epoch/revision/binding mismatch; caller phải xác thực status/PONG và áp shield-first thực. Clock/browser/network proof chưa chạy. Hash: `internal/model/interaction_guard.go`=207364bfa7c5b82ea81c97dffee886cb7ff33af2c3f08fde917afaae0eb6cf20; `internal/model/interaction_guard_test.go`=f2020e97fb2350b2ac085a722b425caf5c6aa8d6249e07ed723e27cca50a6f95.

**T105 — config CAS precondition, 2026-11-11 06:46 UTC:** E086 phân biệt expected_digest absent/null/hash và reject stale head; atomic file lock/write, concurrency và quyền thuộc task sau. Hash: `internal/model/config_cas.go`=244afce261376390fc810f83ce707c41a8158697fa62b4d619e75f983a5b7396; `internal/model/config_cas_test.go`=9022e9a93a9673befff8febe55206bcf3dad3d1cd230036b4e1e8fe07f3c7691.

**T105 — mutation replay identity, 2026-11-11 06:47 UTC:** E087 test new/replay và reject mutation ID dùng với principal/request hash khác; lookup/mutation atomic và principal authentication thuộc storage/runtime. Hash: `internal/model/mutation_replay.go`=fe62b29be34831c509ff59b54a530fc9b51bafd31e3e48e529a1d9c5fad3c3bf; `internal/model/mutation_replay_test.go`=1b68872388d64719eb003d4f0fce86370ffee57c41cca853218dc232f6ad36cc.

**T105 — bridge direction/D015, 2026-11-11 06:48 UTC:** E088 test 23 variants theo schema và direction user chọn, reject sai chiều, ERROR hai chiều; origin/source/replay/nonce/state/browser vẫn cần runtime. Hash: `internal/model/bridge_direction.go`=867c4c78a49a7eca0f61fa44b1a4165a563aae94bb6188d7c74828fbbebe9606; `internal/model/bridge_direction_test.go`=d505d5b998506a2b8626c12c7cf9fa364f25241bb2f169c06adefe186e1c091d; `tests/contract/bridge_schema_test.go`=28af2a8cc846ad350aa1283bbbb583d17d727779cb68e1e90af120d4fbf15f45.

**T105 — VERIFY foundation, 2026-11-11 06:49 UTC:** E089 kiểm bản kết hợp 233 files, manifest SHA-256 (đường dẫn relative + NUL + SHA-256 file, sort path; không gồm spec) `336615ef6f72b646a9ba586b591a70b7d6639342cee198327c16add24c9ce07a`; gofmt 224 Go files, module verify, Python vectors, tests/vet/build/spec_check PASS. Registry→pack request/response + semantic snapshot fixture PASS; mutation hash E049. Race NOT_RUN do toolchain WSL thiếu gcc/clang; Windows build trước đó FAIL E002. Đây chỉ foundation contract, không là actual authorization, OS/storage/transport/browser hay product E2E. Đang kiểm đối chiếu case và gate trước DONE.

**T105 — VERIFY bổ sung, 2026-11-11 06:50 UTC:** E090 kiểm bản kết hợp 235 files, source manifest SHA-256 `5ddcab6123036b399c4ff82b71a9ca58f5eb4e520e84ddbfaaa0a8d7eb58ee7d`; 226 Go files gofmt, module verify, Python vectors, tests/vet/build/spec_check PASS. Fixture 1025 edits hai file, unavailable→indexed, và catalog/index/records incarnation thay đổi; thực thi LSP/pagination, lifecycle/restore thật còn thuộc owner sau. Hash: `internal/model/rename_large_test.go`=3ec6b9ca2378105747f02081149d39f8e4957964f38b47379f402ddc4518c566; `internal/model/scope_unavailable_transition_test.go`=cdbf2e0243bc1486c765f763a5ccdb84605e961abe29ed40d1ba37e09f6c68f7; `internal/model/scope_incarnation_test.go`=51a3244b0e0f45ca3b285bd4749e3c9232d7d08c3b0e4f761c6523715edcdc8f.

**T105 — DONE trong phạm vi foundation, 2026-11-11 06:50 UTC:** E090 và E006–E088 chứng minh model/schema/registry, validator và fixture theo owner T105; đối chiếu C113/C114 bằng spec checker, registry và schema fixtures; C122/C131/C134/C139 bằng scope postcheck, source bytes, import guards và change budget; C154–C162 bằng status/permission/ACK/PONG models, 101 mapping pairs, 1025 edits hai file, cursor shape, fragment và budget; C163–C170 bằng scope/service, incarnation, config/CAS và group/record shapes; C171–C173 chỉ phần identity/incarnation trong model; C174–C176 bằng codec Go/Python, capture/provider và path validators. Đây là contract/model proof: mọi subcase SQL lifecycle/restore, actual paging/LSP, transport auth, browser/OS và product E2E chưa triển khai vẫn NOT_RUN ở T106/T107 hoặc owner feature; không suy gate sản phẩm PASS. Port chưa tạo vì chưa có consumer thật theo CODE-01. Race NOT_RUN vì WSL thiếu C compiler; Windows build E002 FAIL. G0 vẫn chưa seal; task tiếp theo T106.

**T106 — assignment/plan, 2026-11-11 06:51 UTC:** `task_mode=implement`, owner codex, baseline `ba72ae4` + working tree T001–T105/spec 1.6.9; dependency T105 DONE theo E090/E091. Change budget: trích chính xác bảy DDL S44 vào `internal/store/sqlite/migrations/`, fixture/proof `tests/contract/storage*` và evidence/preflight riêng, không app/store runtime use case, không target data, không thay migration đã phát hành. Chạy pinned `modernc.org/sqlite v1.62.0` SQLite 3.54.1 trong disposable fixture Linux WSL2, verify source/DDL hashes, FKs/FTS/rollback/publish/CAS/GC/resolver/incarnation theo case; lưu bounds, lỗi và giới hạn. Native Windows, OS lifecycle lease/restore và product E2E không được suy PASS từ fixture Linux. Trước mọi output kiểm free space hai filesystem, dùng cache/temp riêng, bounded execution, không dọn/xóa dữ liệu người dùng; không mở T107 trước T106 DONE.

**T106 — proof đang triển khai, 2026-11-11 06:56 UTC:** E092 chạy bảy DDL đúng S44, R1 index dừng ở schema 2 và addon chỉ trong fixture riêng; thử FK trên bốn pooled connection, WAL/FULL/busy timeout, reader query_only, cross-generation FK, owner CAS và publish rollback/commit, FTS scope/trigger, immutable records/permissions, stale head CAS, receipt uniqueness và resolver natural key. GC fixture có modules ba tầng, symbol/relation/domain/binding/anchor, blob dùng chung và active generation; 13 điểm fault đều rollback, chu trình trả lỗi và rollback. Incarnation bắt buộc và ổn định qua transaction; model stale-incarnation đã test E090. SQL fixture không chứng minh runtime adapter, concurrent service, import idempotence/source validation, actual publish cycle check, report-only GC, OS lease, restore journal/crash recovery, Windows native hoặc product E2E. Các case còn thiếu/owner runtime không được ghi PASS toàn cục; T106 tiếp tục IN_PROGRESS, G0 chưa seal và T107 chưa mở. Snapshot Linux `/home/mce4d/.local/share/mce4d/preflight-20261111-t106/source`, disk free trước chạy >1 GiB cả WSL và C:, fixture disposable, không target data.

**T106 — DONE phạm vi foundation, 2026-11-11 06:57 UTC:** E092/E093 chứng minh migration S44 trên pinned driver Linux/WSL và SQL proof fixtures: bốn connection, isolation/FK/FTS, owner CAS/publish, record CAS đồng thời/replay, rollback GC đủ stages/cycle, resolver/natural key/immutability và incarnation/permission storage. Recursive cycle precheck/report, identity replacement và GC chỉ là fixture contract, không phải runtime enforcement. Source manifest/hash, validator và build/test đã kiểm; C115–C121/C149–C153/C171–C173 còn các subcase thực tế thuộc T007/T010/T035/T036/T037/T047/T081/T085/T092 và giữ NOT_RUN, bao gồm Windows native, OS lease, journal/crash restore, actual map import/source check, publish validation và report-only GC. T107 tiếp theo; G0 chưa seal.

**T107 — assignment/plan, 2026-11-11 06:58 UTC:** `task_mode=implement`, owner codex; baseline `ba72ae4` + tree T001–T106/spec 1.6.9; prerequisites T105/T106 DONE E090–E093. Change budget `tests/contract/`, `testdata/fixtures/contracts/`, `tools/spec_check/` và evidence/preflight; chỉ contract/flow/serialization/provider/source/state và command/storage/evidence/Git workflow fixtures cô lập, không feature adapter, không dùng target repo để mutation, không commit/push thật. S51/S53: preflight volumes, bounded disposable fixture, không xóa dữ liệu người dùng; phân biệt pure proof với OS/transport/browser/runtime thực, giữ NOT_RUN cho subcase owner tương ứng. T108 chỉ mở sau T107 DONE.

**T107 — proof từng phần, 2026-11-11 06:59 UTC:** E094 kiểm RUN-03 boundary/overflow/unknown/remaining reservations, finalization/cleanup state thuần; fixture Git local-only thử ignored file, lockfile, secret-marker ngoài allowlist, thay bytes sau stage, hook sửa tree, retry bằng HEAD, tag collision, local bare push và clone restore. Hook fixture lúc đầu FAIL vì empty template không có thư mục hooks; chỉ tạo hooks trong repo fixture và chạy lại PASS. Test tạo/commit/push chỉ fixture tự sở hữu, không tác động Git repo người dùng; byte equality được kiểm cả staged và file. Đây là oracle/fixture, không phải wrapper/enforcement, secret scanner, OS path containment hay remote backup thật. T107 IN_PROGRESS, G0 chưa seal; tiếp tục các cases/flow còn thiếu.

**T107 — bổ sung proof, 2026-11-11 07:01 UTC:** E095 kiểm MCP SDK roundtrip thực bằng JSON text + structured, hai representation hợp lệ gần 4096 byte và test vượt cap; Go packages hai load độc lập có object identity khác và captured bytes thay đổi; Git fixtures bao gồm deletion/mode, thiếu identity, interim snapshot staged+unstaged+untracked+deletion, local bare remote phân kỳ. Command fixture phân biệt tên dry-run với child write/network/placeholder; không claim introspection hay wrapper thật. T107 vẫn IN_PROGRESS; transport adapter, codego provider, restore snapshot của workspace thật, OS/process/permission enforcement và remote backup thật chưa được chứng minh.

**T107 — codec/volume proof, 2026-11-11 07:02 UTC:** E096 thêm oracle Node độc lập, 14 canonical preimages/digests trùng Go/Python; invalid surrogate/fraction/unsafe integer bị từ chối ở primitive encoder, duplicate token và số JSON 1.0 cần parser token-level của owner, không suy Node JSON.parse đã chứng minh. Fixture nhóm paths theo volume ID và reject measurement thiếu; chưa chứng minh OS resolver, quotas, reservations hay external-writer coordination thật. T107 IN_PROGRESS.

**T107 — VERIFY, 2026-11-11 07:03 UTC:** E097 bổ sung flow provider thiếu/sai version, thứ tự, input digest và partial terminal; fixture clone có missing material, admission phân biệt shared-output lock với output độc lập; combined tests PASS. Đối chiếu E094–E097 với T105 model fixtures và T106 SQL fixtures, giữ giới hạn contract/oracle: app flow, source/provider runtime, transport integration sản phẩm, process/OS/browser/lease/restore và Git checkpoint/backup thực vẫn NOT_RUN tại owner task. Không thực thi xóa target hoặc commit repo này.

**T107 — DONE phạm vi foundation, 2026-11-11 07:03 UTC:** E094–E097 cùng T105/T106 proofs cung cấp schema/flow/codec/provider/state, MCP SDK near-cap, S51 budget/finalization/ownership và S53 Git fixtures; validator xác nhận tracker/order. Các mô hình/fixtures không phải enforcement runtime; app/E2E, transport product, host/browser, OS lease/restore, actual command wrapper, Windows native, real Git checkpoint và independent backup vẫn NOT_RUN ở task/gate owner. T108 seal review tiếp theo, G0 chưa PASS.

**T108 — assignment/plan, 2026-11-11 07:04 UTC:** `task_mode=implement`, owner codex; baseline `ba72ae4` + tree T001–T107/spec 1.6.9, dependencies T106/T107 DONE E092–E097. Change budget `tools/spec_check/` và `artifacts/preflight/` cùng evidence/tracker; audit contract authority/schema/DDL/ownership/order/pins/oracles, S50/S51 policy readiness, A01–A08 closure/owner gates và S53 batch, xác minh foundation evidence + combined check; không mở feature trước seal, không giả runtime guard hay product readiness. Điều tra và ghi discrepancy nếu có; T108 chỉ DONE/G0 PASS theo scope foundation khi seal thực sự đủ, không dùng design seal làm bằng chứng native OS/browser/target.

**T108 — seal audit/rework T003, 2026-11-11 07:06 UTC:** rà S19/S20/S47 cho thấy spec_check chưa từ chối task được claim khi gate trước chưa PASS và batch B001 có thể bỏ qua task đã DONE. E098 thêm check dependency+predecessor gate theo core/R2/Explorer, vẫn cho R2 subset sau G5 và U1 sau GU0, và kiểm prefix B001 theo S47; integration mutation bỏ check cho FAIL dự kiến rồi hoàn nguyên đúng hash. Đã sửa số đếm cũ ở S40 thành tham chiếu S19/S22 để tránh tracker phụ sai; không đổi cấu trúc/quyết định. T003 vẫn DONE với rework evidence E098, không đếm lại task hay commit. T108 vẫn IN_PROGRESS; tự rà soát không là independent review.

**T108 — phát hiện thiếu proof và mở lại T107, 2026-11-11 07:07 UTC:** S41 yêu cầu fake adapters/deterministic reducer tests cho flow S42–S45; E094–E097 và model tests kiểm từng guard/codec, nhưng chưa có reducer fixture chứng minh toàn chuỗi pairing/shield và negative transitions. DONE T107 trước đó chưa đủ bằng chứng phần này; reopen T107 về IN_PROGRESS, T108 về TODO, rút T107 khỏi batch chưa commit, giữ nguyên E094–E098 làm lịch sử. Bổ sung fixture trong ownership T107, chạy test/bằng chứng mới rồi mới nhận lại T108; G0 vẫn NOT_RUN. Không giả browser/transport runtime.

**T107 — rework VERIFY, 2026-11-11 07:08 UTC:** E100 thêm reducer fixture pairing→challenge→READY→bind→BOUND_ACK→arming→MODE_ACK→server ACK; thử origin/nonce/reply_to/epoch, thứ tự, timeout, wrong/late ACK và invalidation; shield giữ cho đến server ACK và bật trước invalidate. Đây là deterministic fixture, không chứng minh browser, transport hay runtime server. E099 cập nhật T003: batch B001 phải claim đúng task kế tiếp trong S47, ngoài các kiểm tra gate/prefix E098. Combined suite PASS; T108 chưa mở lại.

**T107 — rework DONE, 2026-11-11 07:08 UTC:** E100 khép thiếu sót pairing reducer thuộc foundation; E094–E097 giữ historical scope và E100 bổ sung vào combined proof. Browser/SDK sản phẩm, actual server auth, OS/runtime và target tests vẫn NOT_RUN theo owner. T108 trở lại là task tiếp theo, G0 chưa PASS.

**T108 — tiếp tục seal review, 2026-11-11 07:09 UTC:** prerequisites T106/T107 DONE theo E092/E093/E100; quyền, change budget và giới hạn giữ nguyên assignment trước. Rà schema/registry/DDL, pins/license, policy hashes, S47/S53, A01–A08 và proof owner; nếu phát hiện thiếu sót liên quan phải reopen đúng task, không đóng G0 bằng báo cáo tài liệu.

**T108 — forecast sau discovery, 2026-11-11 07:09 UTC:** P00–P05 cần hoàn thiện Go local engine, an toàn paths/SQLite/analysis, CLI/MCP, acceptance và release; P06–P10 cần Explorer và các R2 prerequisites đã xác định; P12–P14 vẫn optional, chưa có quyền mở. Nguồn lực thực được quan sát: một agent đang triển khai trong workspace; Go 1.27.4/pinned dependencies và WSL2 Linux fixture đã chạy, Windows native Go linker vẫn bị Application Control chặn, Node 24.22.1 chỉ mới dùng oracle. Chưa có browser/target preview/repo đại diện được duyệt, benchmarks, throughput hay môi trường khôi phục OS đầy đủ; không suy giấy phép preview/target từ quyền code. Effort phát triển: CHƯA ĐỦ DỮ LIỆU; kiểm thử/platform: CHƯA ĐỦ DỮ LIỆU; thời gian chờ môi trường/quyền: CHƯA ĐỦ DỮ LIỆU; real-repo acceptance: CHƯA ĐỦ DỮ LIỆU. Đây là phạm vi/điểm chưa biết, không phải deadline, ngân sách hay bằng chứng nghiệm thu.

**T108 — VERIFY seal, 2026-11-11 07:09 UTC:** E101 tự rà S41–S53, 54 sections/130 requirements/108 tasks/192 cases, 108 work cards và dependency order, schema+registry parity, bảy DDL, 14 codec vectors, pin/toolchain/license exception, policy raw hashes, batch prefix/guard và trace A01–A08 tới owners. Negative test lần lượt giữ một trong bảy foundation tasks VERIFY đều chặn G0; rework T003 E098/E099 và T107 E100 khắc phục gaps được phát hiện. Kiểm combined source/test/vet/build/format và source parity PASS; Windows native linker, race thiếu C compiler, actual product/OS/browser/target và independent review vẫn NOT_RUN. E101 là self-review foundation, không khẳng định product/guard runtime hay quyền deploy. G0 hiện NOT_RUN cho đến khi T108 thực sự DONE.

**T108/G0 — DONE/PASS foundation, 2026-11-11 07:09 UTC:** E101 và E001–E100 sau rework đáp ứng blueprint/contract/proof seal trong scope S20. G0 PASS là bằng chứng nền tảng ở WSL, không là G1, sản phẩm, Windows/browser/real-repo, production readiness, independent review hay quyền thực thi target. Task kế tiếp T005 theo S47; batch B001 có 7 task DONE, chưa commit/checkpoint và chưa push.

**T005 — assignment/plan, 2026-11-11 07:10 UTC:** `task_mode=implement`, owner codex; prerequisite T108 DONE và G0 PASS E101, baseline `ba72ae4` + working tree, batch B001 7/10. Change budget `internal/paths/`, `internal/policy/`, `internal/runtime/fs*` và targeted tests/fixtures/evidence; không đọc repo user ngoài fixture đã được phép, không chạy commands/target scripts, không persist source hoặc cleanup. Implement secure handle-based path open và fail-closed OS support, deny path + bounded secret pre-persistence control, test symlink/traversal/race/secret marker trong owned disposable fixtures; unknown platform báo unsupported. Native Windows tests, ACL/junction/race chỉ PASS khi thực chạy, không suy từ WSL; các controls được wire/kiểm persistence tại task owner sau. Command/output offline bounded, free-space checks và temp/cache riêng theo S51. T004 chỉ claim khi T005 DONE.

**T005 — bổ sung change budget tối thiểu:** `go.mod` chuyển `golang.org/x/sys v0.49.0` từ indirect thành direct vì Linux OS adapter import trực tiếp. Đây là pin đã có từ T002, không thêm module/version hay tải mạng; LICENSE upstream BSD-3-Clause đã đọc và kiểm fixtures/compatibility theo E102–E109, phát hành vẫn theo T041. Ranh giới code/target giữ nguyên.

**T005 — Linux partial, 2026-11-11 07:12 UTC:** E102 mở root/relative bằng Linux openat2 anchored, không symlink/magic/mount crossing, O_PATH kiểm inode regular trước mở nội dung qua procfs đã xác minh rồi so inode/device; `SyscallConn.Control` giữ root FD khi open. 500 lần đổi thư mục↔symlink và 2000 lượt open fixture không đọc marker ngoài root; FIFO bị từ chối. CheckPath chặn secret paths trước open; scanner bounded, từ chối raw bytes khi phát hiện indicator hoặc invalid text và không log value; đây là scanner hữu hạn, không bắt mọi secret. API ReadSource phối hợp hai bước, chưa nối catalog/index/blob/FTS/log/MCP. Nền tảng chưa có adapter (bao gồm Windows) fail closed `ErrUnsupported`; native Windows, cleanup containment, root identity/permissions, Git config sanitization, persistence/E2E và Go race vẫn NOT_RUN. T005 vẫn IN_PROGRESS; không mở T004. API semantics đối chiếu Linux man-pages chính thức https://kernel.googlesource.com/pub/scm/docs/man-pages/man-pages/+/master/man/man2/openat2.2 ; không suy hỗ trợ OS khác.

**T005 — thêm sink fixture, 2026-11-11 07:14 UTC:** E103 dùng fixture SQLite blobs/FTS cùng log/output JSON giả lập để kiểm marker trong .env, mixed source và .env.example không đi qua khi ReadSource trả lỗi; chỉ clean source tới sink. Full combined checks PASS. Cross-compile paths test cho Windows thành công nhưng không chạy, chỉ kiểm compile stub fail-closed, KHÔNG là Windows adapter hay OS acceptance. Native Windows, cleanup ownership/containment thực, Git config safety và product sink integration còn NOT_RUN; T005 IN_PROGRESS.

**T005 — targeted mutation, 2026-11-11 07:15 UTC:** ban đầu bỏ RESOLVE_NO_SYMLINKS không làm test fail vì symlink tuyệt đối còn bị RESOLVE_BENEATH chặn; bổ sung fixture relative symlink trong root, thử lại mutant bị test phát hiện như kỳ vọng, hoàn nguyên hash. Bỏ scanner khỏi ReadSource cũng làm test báo rò bytes như kỳ vọng; source được hoàn nguyên, combined tests PASS E104. Thêm fixture nội dung AGENTS.md chỉ được trả như data; không chứng minh Git-config/fsmonitor adapter hoặc actual product không thực thi. T005 vẫn IN_PROGRESS.

**T005 — thử DrvFS, 2026-11-11 07:16 UTC:** E105 FAIL ở fixture paths trên Windows-mounted filesystem: `mkfifo` trả operation not supported và concurrent directory rename trả permission denied; policy suite cùng filesystem PASS. Chưa phân loại là lỗi product hay limitation filesystem/fixture; không suy race/containment đã đạt trên DrvFS hoặc Windows native. Linux filesystem E104 vẫn PASS; giữ T005 IN_PROGRESS, không mở T004. Fixture root chỉ chứa output do lượt thử tạo, không dọn dữ liệu khác.

**T005 — kiểm lại DrvFS, 2026-11-11 07:17 UTC:** E105 vẫn là lịch sử FAIL; test tách FIFO thành subtest, chỉ SKIP khi filesystem báo không hỗ trợ, và retry rename transient có giới hạn thay vì coi lần va chạm đầu là lỗi cuối. E106 đạt các paths/policy tests còn lại trên DrvFS, yêu cầu có cả open thành công và swap trong lượt đọc; FIFO trên DrvFS NOT_RUN, FIFO trên Linux filesystem PASS. Không suy Windows native PASS hay mọi mount/race đã xác minh. T005 còn IN_PROGRESS.

**T005 — Git config fixture, 2026-11-11 07:18 UTC:** E107 tạo repository/script canary do fixture sở hữu; Git status không override thực sự gọi fsmonitor canary, còn `-c core.fsmonitor=false` không gọi. Chỉ chứng minh nguyên tắc cấu hình trong fixture, không chứng minh sản phẩm đã có Git executor, root-cwd chống TOCTOU hoặc vô hiệu mọi config độc hại. Giữ T005 IN_PROGRESS; không chạy script/command của repository target.

**T005 — DrvFS root replacement FAIL, 2026-11-11 07:20 UTC:** E108 phát hiện Git inventory trả rỗng khi root fixture được rename/thay thế trên DrvFS; không coi empty là valid inventory. Bổ sung kiểm identity root theo handle trước/sau mỗi read/inventory và trả ROOT_IDENTITY_CHANGED khi path không còn khớp, sau đó kiểm lại cả Linux FS và DrvFS. Root registration/version authority vẫn thuộc T004; không claim xử lý mọi race/native Windows từ fixture.

**T005 — root identity và Git adapter, 2026-11-11 07:22 UTC:** E109 sau fix kiểm root path/device/inode trước/sau read và Git inventory; symlink, root tái tạo cùng path, thay root trong lúc Git fixture chạy đều trả root-changed và không trả data trên Linux FS/DrvFS. Git adapter Linux dùng fixed argv/env, vô hiệu fsmonitor, output cap/deadline, cwd handle neo procfs; mutant bỏ fsmonitor override hoặc postcheck bị test phát hiện. E108 vẫn là lịch sử FAIL trước sửa. Kiểm trước/sau không chứng minh mọi thay đổi thoáng qua/atomic snapshot, registration/policy/root revisions và revalidation thuộc owner T004/T006; không cho target executable mặc định. Windows native, Go race, cleanup containment và integration thực chưa verified; T005 IN_PROGRESS.

**T005 — combined recheck, 2026-11-11 07:24 UTC:** E110 là combined proof mới sau chuyển pin x/sys sẵn có thành dependency trực tiếp. Product chưa wire guard vào index/storage; native Windows, OS process-tree/race và cleanup ownership thực vẫn NOT_RUN. DrvFS FIFO fixture không hỗ trợ, Linux FS FIFO PASS; không gọi compile Windows stub là platform support. T005 tiếp tục IN_PROGRESS.

**T005 — lỗi Git worktree, 2026-11-11 10:19 UTC:** E111 tái hiện cấu hình `core.worktree` không tin cậy làm Git inventory liệt kê tên file ngoài root. Chặn chuyển task; sửa và chứng minh ranh giới Git worktree/gitdir trên fixture riêng, gồm worktree liên kết theo authority root/metadata, không dùng target user. Nguồn API chính thức: https://git-scm.com/docs/git-config#Documentation/git-config.txt-coreworktree . T005 IN_PROGRESS; E111 giữ lịch sử FAIL.

**T005 — lỗi metadata Git, 2026-11-11 10:21 UTC:** E112 cho thấy .git symlink tới repository khác có thể đưa tên file từ Git index ngoài vào inventory. Không nhận patch core.worktree là đủ: cần chặn metadata không được chứng minh hoặc sandbox phù hợp, kiểm symlink/thay thế cả bên trong metadata và giữ hỗ trợ worktree theo verified registration. T005 IN_PROGRESS, không mở T004 khi safety chưa thỏa.

**T005 — index symlink, 2026-11-11 10:21 UTC:** E113 tái hiện thêm lộ tên từ .git/index symlink dù .git là thư mục thường. Kiểm root hoặc chỉ chặn .git symlink không đủ vì metadata bên trong và thay thế theo thời gian cũng có thể trỏ ngoài. Không để adapter chưa chứng minh trả inventory; linked worktree cần registration/identity verified, không bịa hỗ trợ.

**T005 — cô lập lỗi Git và kiểm sau đọc, 2026-11-11 10:24 UTC:** E114 bổ sung kiểm root identity sau khi đọc và trước khi trả bytes; fixture thay root trong lúc đọc bị từ chối. Git inventory tạm fail closed với `ErrGitMetadataUnverified`, không chạy Git hay trả inventory khi chưa có proof containment cho gitdir/index và linked worktree. Ba fixture E111–E113 được từ chối, nhưng điều này không xác nhận inventory hoạt động; hai test Git success/output limit và thay root khi command chạy SKIP. Mutation tests xác nhận các guard được kiểm. T005 vẫn IN_PROGRESS; Git metadata an toàn, cleanup ownership, integration và các proof platform còn lại chưa hoàn tất. Không mở T004 hay đánh dấu DONE dựa trên E114.

**T005 — metadata index và cleanup report, 2026-11-11 12:12 UTC:** E115 đọc index bounded qua metadata root handle explicit, không discover hoặc tin `.git` pointer; fixture Git linked worktree thật và index symlink/hardlink/root replacement được kiểm. Caller vẫn phải xác nhận registration/binding/authority; check link count trước/sau không chứng minh mọi race, index split/config/includes và capture stability chưa được triển khai. GitInventory tiếp tục fail closed; không xem index read như inventory hoạt động. `InspectCleanupFile` chỉ báo file regular đã resolve trong root, không đọc content hay xóa; report không chứng minh disposable, quyền xóa hoặc writers dừng và không authorize cleanup. Action-time deletion/containment, protected-set, authority, Windows native và race vẫn NOT_RUN; E115 chỉ PASS phần fixture đã nêu, T005 IN_PROGRESS và không mở T004.

**T005 — cleanup replacement counterexample, 2026-11-11 12:12 UTC:** E116 cho thấy sau khi candidate bị thay bởi file khác cùng path/size/mode, report không phân biệt được; report không phải authority để xóa. Đây là test fixture chỉ đọc/rename/write trong temp root riêng, không chạy cleanup và không chứng minh atomic deletion. T005 tiếp tục IN_PROGRESS.

**T005 — hardlink source boundary, 2026-11-11 12:53 UTC:** E117 tái hiện fixture source path là hardlink tới file ngoài root và bị trả bytes. E118 từ chối file có link count khác 1 khi mở và kiểm lại handle trước trả bytes; hardlink tạo khi đọc bị test boundary có điều khiển phát hiện, mutant guard sau đọc bị phát hiện rồi hoàn nguyên. Chính sách này cũng từ chối hardlinks hợp lệ trong root; quan sát trước/sau không chứng minh absence của hardlink thoáng qua hay snapshot atomic. Git inventory, binding/authority, cleanup action-time và platform/race còn như E115/E116; không coi E118 là T005 DONE và không mở T004.

**T005 — phát hiện thay index, 2026-11-11 13:18 UTC:** E119 cho thấy inode/size/mtime không đủ để phát hiện file bị sửa rồi khôi phục mtime. E120 so metadata trước/sau gồm ctime Linux, mở lại `index` trong root và so identity trước trả bytes; fixtures thay path hoặc sửa tại chỗ bị từ chối, bao gồm khôi phục mtime. Ba mutation guard cho expected test failure rồi được restore; Linux FS/DrvFS fixture PASS. Đây là kiểm tra quan sát, không đảm bảo snapshot nguyên tử, không chứng minh metadata authority/binding và không chứng minh mọi filesystem có timestamp phù hợp. Git inventory vẫn fail-closed; cleanup action-time chưa triển khai; T005 IN_PROGRESS.

**T005 — linked Git và cleanup containment, 2026-11-11 13:39 UTC:** E122 mở child directory bằng openat2 anchored/no-link/no-mount và kiểm identity; linked fixture Git thực kiểm ba pointer hai chiều trên metadata root explicit, chặn pointer bị đổi/symlink và recheck trước/sau đọc index. Đây không chứng minh đăng ký/quyền, Git config an toàn, inventory hay snapshot nguyên tử. Cleanup guard kiểm tổ tiên hai chiều bằng handle+unique mount ID, từ chối mount khác/unknown và mount lồng trong output đọc từ procfs đã kiểm; E121 chứng minh ancestor checks đơn thuần thiếu nested bind mount. Test mount chạy trong namespace riêng trên fixture, không đổi host; trên DrvFS rename exchange SKIP. Guard là quan sát có giới hạn, không chứng minh protected set đầy đủ, disposable/provenance, writer quiescence, authority hoặc action-time deletion; không xóa. Giữ T005 IN_PROGRESS và không mở T004, Git inventory vẫn fail-closed; Windows native và Go race NOT_RUN. Semantics statx theo Linux man-pages https://man7.org/linux/man-pages/man2/statx.2.html .

**T005 — snapshot giữa batch, 2026-11-11 13:40 UTC:** E123 lưu toàn source được phép tại E122, cả docs/spec/go modules và staged/unstaged tracked diffs; source mới được đưa vào theo manifest, `.git`, `.vscode`, artifacts và dữ liệu runtime bị loại. Mẫu heuristic chỉ tìm marker private-key giả trong test, không coi đó là bảo đảm phát hiện mọi secret. Bản restore riêng được so hash đầy đủ và build/chạy tests mục tiêu; đây không phải checkpoint commit/tag hay kiểm chứng toàn product. E123 được ghi sau snapshot để tránh vòng self-hash; snapshot khôi phục tracker về E122. T005 vẫn IN_PROGRESS.

**T005 — bảo vệ fixture mount, 2026-11-11 13:41 UTC:** E124 yêu cầu namespace mount của helper khác parent trước mọi thao tác mount; test gọi helper trong namespace gốc bị từ chối và test bind mount trong namespace riêng vẫn PASS. Full combined checks chạy lại. E123 là snapshot trước thay đổi test, không được coi là bản E124. T005 vẫn IN_PROGRESS.

**T005 — snapshot cập nhật, 2026-11-11 13:42 UTC:** E125 lưu và thử restore lại bản nguồn gồm sửa an toàn fixture E124. Snapshot ghi spec tại E124; E125 viết sau đó để tránh self-hash. Đây chỉ là snapshot source ngoài Git history, không thay checkpoint B001 hoặc product acceptance. T005 tiếp tục IN_PROGRESS.

**T005 — DONE phạm vi nền tảng, 2026-11-11 15:27 UTC:** E122/E124/E126 và negative cases E102–E120 xác minh secure-open/policy/secret scan và cleanup containment guard trên fixture Linux/WSL; E126 chạy kết hợp trên Linux FS và DrvFS. Đối chiếu lại S19/S47: đăng ký/authority thuộc T004, Git inventory thật thuộc T012 và cleanup thực có approval/quiescence/action-time revalidation thuộc T033/T035; các ghi chú trước yêu cầu hoàn thành các task đó trước T004 đã tạo vòng phụ thuộc, nay được điều chỉnh theo owner gốc mà không miễn kiểm hoặc thay quyết định. GitInventory vẫn fail-closed, 2 functional tests T012 SKIP, không có action deletion; C025/C026/C145 E2E, Windows native, race, bind-mount/rename-exchange trên mọi FS và product integration vẫn NOT_RUN cho owner/gate tương ứng. T005 DONE chỉ là primitives nền tảng, không là G1 PASS, product-ready hoặc quyền cleanup. B001 8/10, chưa commit.

**T004 — assignment/plan, 2026-11-11 15:27 UTC:** `task_mode=implement`, owner codex; dependencies T005 DONE, T106 DONE, G0 PASS; baseline `ba72ae4` + working tree E126, B001 8/10. Vùng chính `internal/catalog/`, `internal/app/registration*`, `internal/app/config*`, `internal/runtime/config*`, tests/fixtures colocated và evidence; chỉ thêm adapter đọc migration frozen nếu cần, không thay schema. Thiết lập transactional registration/profile/policy/config/group foundations và incarnation theo S43/S44; không sửa target source, không chạy target script, không tạo CLI trước owner task, không claim DONE nếu thiếu safety/integration. Fixture SQLite riêng, offline, budgets S51.

**T004 — catalog partial, 2026-11-11 15:38 UTC:** E128 triển khai `internal/catalog` dùng migration catalog frozen qua adapter embed, không sửa SQL nguồn: random incarnation trong transaction, schema comparison/fail-closed, registration gồm repo/worktree + policy/profile heads trong cùng transaction, CAS policy/profile, remove giữ lịch sử, và rebind đã confirm trong cùng Git common identity. Fixture Git thật gồm uncommitted/linked, fsmonitor canary không chạy; không sửa source. Tests check concurrent pool/SQLite connections, stale revision, rollback, root/policy mismatch, foreign incarnation, schema changes và mutations. E127 ghi FAIL DrvFS khi old handles còn mở; fixture đóng handles trước rename rồi PASS, không chứng minh tình huống open-handle. T004 IN_PROGRESS: rebind clone khác còn bị từ chối, runtime config CAS, group metadata foundations, path/actor authority và durable pool/lifecycle/CLI integration chưa hoàn tất; index Git, cleanup deletion và Windows native vẫn theo owner tasks. DB handle hiện được caller cung cấp; caller phải cấu hình WAL/FULL/busy timeout, quyền và lifecycle lease, chưa public wiring.

**T004 — snapshot giữa batch, 2026-11-11 15:39 UTC:** E129 lưu source tại E128 và thử restore riêng, so hash và chạy build/kiểm thử mục tiêu. Source snapshot gồm catalog mới, migration embed, source/tests/docs/spec/lockfiles; không gồm `.git`, `.vscode`, artifacts hoặc DB người dùng. E129 viết sau snapshot để tránh self-hash; snapshot khôi phục tracker về E128. Không phải Git checkpoint hay product acceptance. T004 IN_PROGRESS.

**T004 — DONE phạm vi use case nền tảng, 2026-11-11 16:06 UTC:** E131 bổ sung config service qua port có consumer thật, file được caller authorize, read/validate không side effect, config khởi động bất biến, CAS dưới lock và atomic rename; pending bất thường được giữ và chặn xử lý. Rebind clone khác ở đường dẫn mới có repo ID riêng; clone tại cùng đường dẫn chỉ giữ repo ID khi có xác nhận link riêng. Với common-dir dùng chung, preview trả tối đa 1000 worktree, xác nhận đủ ID/status/revision, transaction tăng binding revision và đánh dấu các worktree còn sống unavailable; removed giữ nguyên, stale/thiếu/duplicate rollback. Đây là link explicit theo S06, không silent match, không đổi frozen DDL, không sửa source/records. E130 ghi lỗi fixture 0755 rồi sửa đúng fixture; một mutant pending đầu tiên không compile, sau đó thay bằng mutant compile được và bị test bắt. T004 DONE chỉ cho use case/fixture đã nêu: caller còn phải authenticate/authorize từng root/actor/affected worktree, quản lý catalog/lifecycle lock và SQLite WAL/FULL/busy timeout trước public access theo T010/T028; SQLite hai connection có thể trả BUSY, không giả đó là conflict. Recovery điều khiển cho pending sau crash, finalization, target action và OS matrix tiếp tục thuộc T033/T035–T037; DrvFS tại fixture này báo mode 0777 nên config file owner-only bị từ chối, không khẳng định config CAS chạy được trên đó. Git inventory thực T012 còn fail-closed/2 SKIP; CLI/group mutation/records/Windows native, race và independent review chưa thực hiện. G1 chưa PASS. B001 9/10; T006 tiếp theo.

**T006 — assignment/plan, 2026-11-11 16:06 UTC:** `task_mode=implement`, owner codex; dependencies T005 DONE và G0 PASS; baseline `ba72ae4` + tree E131, B001 9/10. Phạm vi `internal/snapshot/`, `internal/inventory/capture*` và test cùng owner; chỉ thêm port nếu có consumer thật. Secure read/policy trước bytes, bounded capture retry, raw content SHA-256, range UTF-8/CRLF và manifest canonical `capture-v1` không chứa parse result; fixture tự tạo, offline, không Git inventory thật trước T012, không chạy hay sửa source target. Kiểm positive/negative/failure và combined checks theo S46/S51; không claim stable toàn filesystem hoặc G1. Trước task kế sau T006 DONE phải đóng B001 theo S53.

**T006 — DONE phạm vi capture, 2026-11-11 16:14 UTC:** E134 nối secure-open và scanner T005 với kiểm quyền do caller cung cấp trước/sau, kiểm identity/size/mode/mtime/ctime khi đọc, xác minh path hiện hành và retry hữu hạn; bytes không thoát khi deny/secret/size/revocation/unstable. Snapshot sao chép nguồn và manifest, kiểm hash/length/membership được cung cấp, giới hạn tổng bytes, sort/digest đúng CT-04; parse kết quả khác không thay capture input. Position helper reject offset giữa UTF-8, surrogate hoặc CRLF và hỗ trợ ba encoding; quy tắc line endings tham chiếu LSP chính thức `https://github.com/Microsoft/language-server-protocol/blob/gh-pages/_specifications/specification-3-14.md`. E132/E133 giữ các thất bại phát hiện thật, fixture và code đã sửa rồi kiểm lại. Phần này không tự enumerate tracked/untracked, không chứng minh input closure hay full-workspace freshness; Git membership/revalidation/provider/publish và C013/C023/C175 đầy đủ thuộc T012/T020/T008/T018. Không phải atomic filesystem snapshot; callback không tự cấp quyền, secret scanner không chứng minh không có mọi secret. Windows native/race chưa chạy; G1 vẫn NOT_RUN. B001 đủ 10 task, phải hoàn tất Git checkpoint trước T007.

**B001 — checkpoint bị chặn, 2026-11-11:** nhánh hiện tại `main`, base `ba72ae4c59e65708812a9f964e30ae0fdbbc90d6`, không thấy conflict, batch đúng 10 task theo S47. `git var GIT_AUTHOR_IDENT` và `git var GIT_COMMITTER_IDENT` đều báo identity unknown trên Windows; WSL cũng không có user.name/user.email. GIT-04 cấm tự điền identity để vượt lỗi, nên không tạo commit/tag hoặc mở T007. Danh mục 320 file đã rà và stage có chủ đích, chỉ 2 marker giả trong test; đối chiếu staged blob với bytes đã test, không có mismatch. `.vscode/` là thay đổi ngoài batch, giữ nguyên và loại khỏi inventory; artifacts/runtime state cũng bị loại. E135 chuẩn hóa đúng newline cuối hai script T107, chạy lại vectors và kiểm bản kết hợp. Metadata evidence/spec sau lượt đối chiếu được stage lại, phải kiểm tree lần cuối trước commit. Sau khi chủ repository tự cấu hình danh tính Git phù hợp, reconcile trạng thái, kiểm staged tree/secret/format và bản kết hợp, rồi commit một lần + restore + annotated tag theo S53; không suy quyền push từ remote hiện có.

**B001 — interim snapshot, 2026-11-11 16:18 UTC:** E136 lưu full source và staged/unstaged diffs ngoài Git, có manifest từng file và thử restore riêng; không chứa dữ liệu runtime hoặc `.vscode`. Snapshot lưu spec tại E135, E136 ghi sau snapshot để tránh vòng self-hash. Chưa có commit, tag, checkpoint restore hay backup độc lập; Git identity vẫn thiếu và T007 chờ S53.

**B001 — checkpoint local, 2026-11-12 00:35 UTC:** user chọn rõ danh tính của commit trước và cho cấu hình riêng repository; commit `9458db6f082637284c828b46ac0a50507562e848` có parent/tree/trailers đúng B001, không gồm `.vscode`/artifacts. E137 kiểm checkout detached từ chính commit, đối chiếu toàn bộ 320 blob và chạy lại các kiểm áp dụng; annotated tag `mce4d/checkpoints/B001` trỏ đúng commit. Không push; không có bằng chứng backup độc lập hay Windows native. Sang B002/T007 theo S47, không tạo commit phụ chỉ để ghi receipt.

**T007 — assignment/plan, 2026-11-12 00:35 UTC:** `task_mode=implement`, owner codex; dependencies T004/T006/T106 DONE, G0 PASS, B001 verified tại E137; baseline `9458db6` và B002 0/10. Phạm vi `internal/store/sqlite/`, port lifecycle có consumer thật và tests/fixtures liên quan. Dùng DDL index 001→002 frozen, transactional incarnation/migration, FK/FTS/capture guard theo S44; unknown/newer DB fail closed, mọi borrowed connection kiểm pragma, caller sở hữu pool/quyền và lease, không mở public access trước T010. Không tự sửa source target, không mở DB người dùng; kiểm fixtures Linux/WSL và ghi rõ Windows native, OS lifecycle, publish/recovery chưa tới owner tasks.

**T007 — DONE phạm vi nền tảng, 2026-11-12 00:43 UTC:** E138 ghi lỗi nil BLOB được phát hiện trước khi sửa; E139 kiểm adapter migration, schema/incarnation, pool caps, per-connection pragma, FK/FTS và capture guards với SQLite thật trên hai filesystem. Lifecycle/writer probes là giả, caller phải giữ lease và pool đúng lifetime; chưa mở public access. Publish, recovery, search API/plan sản phẩm, GC/restore, real OS locking, native Windows và race chưa được chứng minh, theo T008/T009/T017/T035/T010/T037; không suy G1 PASS. B002 đạt 1/10, không commit lẻ.

**T010 — assignment/plan, 2026-11-12 00:43 UTC:** `task_mode=implement`, owner codex; T004/T007 DONE, G0 PASS, B002 1/10. Phạm vi `internal/runtime/lock*,internal/runtime/lifecycle*,internal/store/sqlite/ownership*` và fixture; theo DB-05/DF02/DF08/DF17, writer lock riêng từng worktree và lifecycle shared/exclusive bounded, không TTL steal, không lock toàn máy, lease giữ đến khi đóng mọi pool/handle; không upgrade tại chỗ. Kiểm multiprocess thật trên fixture, ghi riêng WSL/Linux, DrvFS, Windows native và integration chưa có.

**T010 — DONE phạm vi Linux foundation, 2026-11-12 00:53 UTC:** E141 dùng OS flock và SQLite thật, chứng minh lease shared/exclusive theo scope, writer pin theo job và giữ trong transaction, cấm nhả lifecycle khi còn writer/pool/borrowed handles, catalog reader không giữ mutation lock; exclusive maintenance được gắn pool owner trước migration. Bổ sung wiring hẹp `internal/catalog/{initialize,store,leased,group,rebind}*` và index initialization/capture (related T004/T007) để kiểm authority trước commit; không thay migration. E140 cho thấy DrvFS hiện hành bị từ chối do không xác minh được owner-only; không nới guard. Windows native còn unsupported/NOT_RUN, race NOT_RUN, arbitrary noncooperating cùng user không được ngăn bởi flock; Windows paths/locks/ACL và ma trận thật thuộc T037, restore/journal thuộc T035. Không mở public transport và không suy toàn C171–C173/G1 PASS. B002 2/10.

**T012 — assignment/plan, 2026-11-12 00:53 UTC:** `task_mode=implement`, owner codex; dependencies T004/T005/T006 DONE, G0 PASS, B002 2/10. Phạm vi chính `internal/inventory/`, fixture Git và các thay đổi hẹp cần thiết ở `internal/runtime/fs_git*` khi nối containment đã có. Implement tracked/untracked/exclusion/nested/sparse và file map theo S42/S44; không thực thi hook/fsmonitor hoặc sửa target, fail closed khi chưa chứng minh metadata/root. Oracle Git repository synthetic riêng, kiểm quyền/path/secret/cancel/limits; không giả unsupported thành empty.

**T012 — partial, 2026-11-12 01:01 UTC:** E142 ghi phân loại lỗi root được phát hiện và sửa; E143 kiểm bộ đọc index không chạy Git trên target và bộ liệt kê candidate qua handle confinement, cả hai giới hạn resources và không trả dữ liệu nếu index/root/authority đổi. Format theo tài liệu chính thức `https://git-scm.com/docs/gitformat-index`; split-index và required extension lạ trả unsupported, không diễn giải thiếu entry thành empty. Candidate walk chưa là file map: effective ignore, hợp nhất tracked/untracked, trạng thái delete/unmerged/nested, source capture và revalidation membership chưa wired; hai test GitInventory cũ vẫn SKIP. Windows native/race chưa chạy. T012 giữ IN_PROGRESS, không mở T008 hoặc claim C011/C013/C014/C016 hoàn tất.

**B002 — interim snapshot, 2026-11-12 01:03 UTC:** E144 lưu source/spec, tracked diff, status và manifest/mode ngoài Git, thử restore riêng từ B001 rồi phủ snapshot; spec trong snapshot dừng tại E143 để tránh self-hash. Không tạo WIP commit khi mới 2/10, không push hoặc tuyên bố backup độc lập. Giữ snapshot đến checkpoint B002 và review, không tự xóa dữ liệu. T012 tiếp tục IN_PROGRESS.

**T012 — kiểm chứng file map, 2026-11-12 01:24 UTC:** E146 nối index và walk có giới hạn với ignore sources đã capture/authorize, phân loại tracked/untracked/missing/unmerged/sparse/nested/excluded, hai quan sát và recheck root/config/quyền. Effective config đọc có giới hạn qua Git trusted và sealed memory; include/config không xác minh, split-index và required extension lạ từ chối thay vì giả kết quả. Fixture so sánh precedence system/XDG/global/repo và untracked với Git, kiểm SHA-256, linked config và sparse directory không traverse. Callback discovery, actor/root authority và resolver là trust boundary; chưa có runtime caller kết nối quyền/config thật. Chưa có full-workspace atomic snapshot hay revalidation ngay trước publish: T008/T019 phải nối capture, authorization và membership trước publish/query, không được dựa hai quan sát như proof vĩnh viễn. GitInventory cũ vẫn fail-closed với 2 SKIP; Windows native/race và C011/C013/C014/C016 E2E chưa chạy. T012 chuyển VERIFY để tự review phạm vi và gate trước khi đánh dấu DONE; G1 NOT_RUN. Lần khởi chạy combined đầu sai do PowerShell mở rộng biến shell, lệnh dừng trước gofmt/test; đã chạy lại bằng executable path explicit và E146 ghi lần chạy thành công.

**T012 — DONE phạm vi inventory Linux, 2026-11-12 01:25 UTC:** E143/E146 chứng minh deliverable index/file map trong các fixture được cấp quyền; E148 kiểm tracker sau VERIFY. Quyền và effective config production không được suy từ fixture: caller phải cung cấp và xác minh discovery/actor/root/path/resolver, kiểm lại ở mọi boundary; nếu không chứng minh được trả lỗi. T008 phải giữ ràng buộc freshness/capture trước publish, T019 chịu full-workspace strict freshness; unmerged/sparse/unsupported vẫn là coverage gap, không là semantic completeness. Legacy adapter, Windows native, race và E2E nêu trên chưa được nâng trạng thái. G1 NOT_RUN; B002 3/10; T008 tiếp theo.

**T008 — assignment/plan, 2026-11-12 01:26 UTC:** `task_mode=implement`, owner codex; baseline B001 + tree E146, dependencies T007/T010/T012 DONE, G0 PASS, B002 3/10. Change budget `internal/indexer/publish*`, `internal/store/sqlite/index/` và tests thuộc owner; không sửa frozen migrations, target source hoặc bật CLI/transport. Theo DF02/DB-03: generation BUILDING chỉ writer/job đúng; bounded staging; xác minh capture, membership/config/quyền, graph/FK/range/FTS và policy/profile/root trước publish; transaction CAS BUILDING+owner→READY/READY_PARTIAL và active pointer cùng profile; readers chỉ dùng published generation trong một read snapshot. Failure/cancel/lease loss giữ active cũ; commit outcome unknown không giả rollback. Synthetic Git/SQLite fixture, fault injection/concurrent readers, budget/cancel và full combined checks; Windows native/race và C019/C020/C117/C136 E2E không tự suy PASS từ fixture.

**T008 — partial, 2026-11-12 01:35 UTC:** E151 nối BUILDING với digest/job, batch file+blob có giới hạn và kiểm quyền trước/sau; reader đọc manifest+source cùng SQLite snapshot, không trả BUILDING, recheck policy và hash. `RevalidateLocal/Linked` đối chiếu membership và hash hai lượt, lỗi khi authority/dependency/config thay; không là atomic filesystem snapshot. `PublishCapturedOnly` kiểm staged files/FK/FTS external-content, từ chối mọi semantic facts, ghi diagnostic `PROVIDER_NOT_RUN` và capability unknown, chỉ publish `READY_PARTIAL` cùng active pointer; integration thực dùng OS lease/SQLite/Git fixture giữ active cũ khi thêm file. Callback actor/policy/discovery/dependency phải được composition root thực cung cấp, fixture không tự chứng minh quyền production. Còn thiếu publish tổng quát `READY`/facts cùng kiểm graph/range/diagnostics/cycle, orchestration và xử lý commit uncertain/recovery; không expose transport, T008 vẫn IN_PROGRESS và G1 NOT_RUN. Liên quan T007: sửa `ErrAfterCommit` khi guard đóng lỗi sau commit để caller không nhầm transaction đã rollback; tests E151 bao phủ.

**T008 — provider staging và outcome, 2026-11-12 01:43 UTC:** E152 thêm staging bounded cho package/symbol/relation/diagnostic và stream Done theo model contract; job/generation/snapshot được bind, range kiểm trên captured bytes, FK/quyền/secret và transaction rollback, stream không tự replay sau kết quả commit không rõ. `PublicationOutcomeByJob` chỉ quan sát cùng read snapshot; trạng thái unavailable không khẳng định chưa từng publish. Registry stream chỉ trong một instance Store, composition root phải bảo đảm một owner Store cho một writer/job; chưa là proof cross-process hoặc recovery journal. Bổ sung `snapshot.CheckRange` liên quan T006 để kiểm UTF-8 boundary không sao chép vùng source lớn. Publish facts, coverage tổng quát, module-cycle/range integrity, orchestration và crash recovery vẫn chưa hoàn thành; giữ T008 IN_PROGRESS, B002 3/10, G1 NOT_RUN.

**B002 — interim snapshot T008, 2026-11-12 01:44 UTC:** E153 lưu source/spec trước record này, hash và kiểm khôi phục từ B001 trong worktree riêng; giữ snapshot đến checkpoint B002, không tạo commit dưới 10 DONE hoặc tự xóa snapshot cũ. Tiếp tục T008 và không suy đây là independent backup.

**T008 — VERIFY, 2026-11-12 01:59 UTC:** E155 bổ sung publish semantic `READY`/`READY_PARTIAL` theo Done frame và coverage, validate facts/diagnostics/range/cycle/FK/FTS, projection `parse_error` chỉ khi có diagnostic; manifest capture vẫn bất biến. `ReadPublishedFileSymbols` đọc source và local symbols cùng transaction, quyền được kiểm lại trước trả. `data_version` từ chối commit từ connection khác giữa validation và transaction; active stream được giữ theo writer/job qua nhiều Store, dùng OS writer lease, không global lock. T010/ports được mở rộng đúng phần liên quan để giữ stream reservation; `snapshot.CheckRange` là rework T006 đã ghi. VerifyProvider và callback actor/discovery/dependency là trust boundaries cần composition thật; provider thật thuộc T013/T020, search documents thuộc T017, recovery job thuộc T009, concurrent nhiều worktree/profile thuộc T011. Không suy fixture thành E2E hoặc Windows/race PASS. T008 chuyển VERIFY để review điều kiện trước khi mở T009; B002 3/10, G1 NOT_RUN.

**T008 — DONE phạm vi storage Linux, 2026-11-12 02:00 UTC:** E155/E157 chứng minh bounded staging, atomic active-pointer publish và typed source/symbol snapshot reader trong fixture cùng các failure subcases thuộc deliverable; E156 là lỗi resume đã sửa. Revalidation cần caller được cấp quyền và provider proof thật, không biến fixture thành production authority; coverage/source refs đầy đủ và graph query còn task sau. Recovery BUILDING sau crash/cancel thuộc T009; matrix multi-process A/B và profile thuộc T011; Windows native, race, product E2E và G1 giữ NOT_RUN. B002 4/10, T009 tiếp theo.

**B002 — interim snapshot T008, 2026-11-12 02:01 UTC:** E158 lưu source/spec trước record này, khôi phục trong detached worktree từ B001, kiểm hash và test; giữ snapshot tới checkpoint, không coi là backup độc lập.

**T009 — assignment/plan, 2026-11-12 02:01 UTC:** `task_mode=implement`, owner codex; baseline B001 + E155, dependencies T008/T010 DONE, B002 4/10. Change budget `internal/indexer/recovery*`, adapter `internal/store/sqlite/index/recovery*` và lease/runtime/ports tối thiểu nếu cần; tests cùng owner. Recovery chỉ sau lấy lại OS writer ownership cho worktree, không dùng TTL/PID để steal; bounded tìm BUILDING, xác minh identity/quyền và CAS sang FAILED/CANCELLED trong transaction, giữ active/source/records; không tự xóa generation/blob hoặc quarantine/delete DB. Test process sống/chết, cancel, SQLite lỗi/quyền, reader old active, uncertain commit và giới hạn; các phần GC/quarantine hoàn chỉnh thuộc T035, native Windows/race chưa có môi trường.

**T009 — VERIFY, 2026-11-12 02:07 UTC:** E159 thực hiện recovery writer riêng sau OS lock thật, danh sách BUILDING có pagination/authority, CAS chính xác sang FAILED và observe trạng thái khi commit không rõ; cancel generation job hiện tại dùng stream gate và CAS sang CANCELLED. Giữ facts/blob/active; không suy PID/TTL là quyền. Controller phải dừng provider trước cancel và production phải cấp actor/recovery authority thật; process-tree provider chưa tồn tại và thuộc T020/T033. `ErrCommitUnknown` không tự retry; caller nhận pending để reconcile. Fixture corruption là sửa schema, lỗi commit do driver test tiêm, không khẳng định thử disk-full vật lý hay corruption thực. T009 chuyển VERIFY trước review; B002 4/10, G1 NOT_RUN.

**T009 — DONE phạm vi recovery Linux, 2026-11-12 02:07 UTC:** E159/E160 chứng minh recovery và cancel thuộc storage core trên fixture, gồm OS ownership thực khi process writer bị kill, rollback và outcome unknown. Không tuyên bố controller/provider process-tree, real disk-full, Windows native hoặc release E2E đã được kiểm; GC/quarantine tiếp tục thuộc T035. B002 5/10, T011 tiếp theo; G1 NOT_RUN.

**B002 — interim snapshot T009, 2026-11-12 02:08 UTC:** E161 lưu source/spec trước record này và kiểm hash/khôi phục từ B001; giữ snapshot tới checkpoint, không coi là backup độc lập.

**T011 — assignment/plan, 2026-11-12 02:08 UTC:** `task_mode=implement`, owner codex; baseline B001 + E159, dependencies T008/T010/T012 DONE, B002 5/10. Change budget `tests/integration/concurrency*` và fixture/test helpers cùng owner; multi-process thật trên Linux FS cô lập: worktree A/B cùng basename hoặc profile, A giữ writer thì B vẫn publish/read, reader A vẫn thấy generation published; nhiều profile và thay active liên tục không trộn source/facts; bounded wait, no TTL/global lock và failure diagnostics. Không suy Windows native/race/product E2E từ WSL; G1 được đánh giá riêng theo S20 khi task kết thúc.

**T011 — VERIFY, 2026-11-12 02:12 UTC:** E162 kiểm clone và linked worktree cùng HEAD, thay bytes dirty; catalog fixture phân biệt identity. Process A stage BUILDING và giữ writer thật, đồng thời process B tiếp tục publish 8 generation qua 2 profile; ba reader thấy thay đổi và kiểm nội dung cùng symbol/profile trong snapshot, A old vẫn đọc trước publish. Assertions giữ nguồn fixture đúng nội dung test. Provider/authorizer dùng fixture, catalog không wired public transport, không là C001/C020/C021 E2E của sản phẩm hoặc Windows proof. Chuyển VERIFY trước review G1, B002 5/10.

**T011 và G1 — DONE/PASS phạm vi Linux filesystem fixture, 2026-11-12 02:12 UTC:** E102–E162 bao phủ các deliverable T004–T012 về identity, containment, capture, SQLite, OS lock, inventory, publish, recovery và A/B concurrency; E163 kiểm tracker VERIFY. G1 này chỉ mở P02 trên R1 primary, không là nhãn phát hành hoặc xác nhận production actor/discovery/provider wiring. Windows native vẫn unsupported/NOT_RUN theo T037; race thiếu C compiler, DrvFS lock fail-closed, hai test GitInventory legacy SKIP, real disk-full và product/real-repo E2E chưa kiểm; các giới hạn phải được giữ tại gate/release tương ứng. B002 6/10; T013 tiếp theo.

**B002 — interim snapshot T011, 2026-11-12 02:13 UTC:** E164 lưu source/spec trước record này và kiểm hash/khôi phục từ B001; giữ snapshot tới checkpoint, không coi là backup độc lập.

**T013 — assignment/plan, 2026-11-12 02:13 UTC:** `task_mode=implement`, owner codex; G1 PASS phạm vi primary Linux fixture, T006/T012 DONE, B002 6/10. Change budget `internal/analysis/golang/` và tests/fixture cùng owner; dùng `go/parser`, `go/ast`, `go/token` trên captured bytes immutable, khai báo/range/scope/package variant xác định, không đọc filesystem target hoặc chạy build. Kiểm shadowing, receiver/named result, closure, generic, embedded fields, package/test variants, parse error và budgets/cancel; không suy AST là typed Defs/Uses hoặc coverage semantic, owner T014/T020/T016 xử lý. Dùng oracle fixture độc lập và tests kết hợp trước khi đánh DONE.

**T013 — VERIFY, 2026-11-12 02:23 UTC:** E165 phân tích từng file từ `Snapshot`, kiểm profile/provider digest, scanner, package/test variant, budgets và phạm vi byte gốc; IDs variant-specific dùng canonical preimage gồm language/package/path/kind/owner/range, occurrence provenance nhận cùng vị trí trong production/internal-test mà không đồng nhất typed semantics. Parse lỗi chỉ trả diagnostic sanitized và partial; field signature giữ struct tag. Integration stage/publish/read dùng callback fixture, không chứng minh build selection/closure hoặc actor production. Kết quả scopes/imports/occurrence hiện ở bộ nhớ; T020/T014 phải nối provider/typed output và dedup reference thực. `parser.ParseFile` bị chặn bởi file-size cap nhưng không nhận cancellation trong chính lời gọi. Giữ G2 NOT_RUN, B002 6/10; review trước DONE.

**T013 — DONE phạm vi syntax fixture, 2026-11-12 02:23 UTC:** E165/E166 chứng minh parser/declaration/scope/variant và adapter fixture trên captured bytes. Package selection là đầu vào do caller cung cấp, chưa được coi là build closure đã xác minh; typed Defs/Uses/Selections, persisted occurrence dedup và provider composition vẫn thuộc T020/T014. G2 NOT_RUN; B002 7/10; T017 tiếp theo.

**B002 — interim snapshot T013, 2026-11-12 02:25 UTC:** E167 xác minh nội dung và khôi phục source/spec từ B001 trong worktree riêng; snapshot không phải backup độc lập, không commit lẻ.

**T017 — assignment/plan, 2026-11-12 02:25 UTC:** `task_mode=implement`, owner codex; G1 PASS phạm vi Linux fixture, T007/T012/T013 DONE, B002 7/10. Change budget `internal/search/`, `internal/store/sqlite/index/` và tests/fixture liên quan để nối lookup với storage; giữ DDL/schema frozen. Exact/prefix symbols và FTS5 lexical search chỉ trên generation published, source bytes đã capture được phép; validate bounds, authorization, SQL parameter binding, deterministic output, FTS integrity và query plans. Không gọi lexical match là semantic reference, không đọc source target trực tiếp hoặc chạy target; real authority/provider composition, Windows native và G2 vẫn cần evidence tương ứng. Kiểm fixture độc lập, failure cases và bản kết hợp trước DONE.

**T017 — VERIFY, 2026-11-12 02:34 UTC:** E169 chứng minh lookup bounded và FTS trên adapter fixture, không phải public locate hoặc live freshness. Tài liệu tìm kiếm chỉ từ captured UTF-8 đã cho phép, publish kiểm byte tương ứng và FTS integrity; mỗi response kiểm lại generation/path, ghi coverage `unknown` và freshness `not_checked`; `text_match` không tạo relation. FTS dùng token phrase `unicode61`, không hứa substring byte-exact; path-level hit không giả vị trí. Output có truncation nhưng cursor thuộc T025; caller chịu trách nhiệm composition policy/provider và chọn tập files, không tự nhận coverage đầy đủ. E168 giữ lỗi đã sửa; independent reviewer, native Windows/race và product E2E chưa xác minh. B002 vẫn 7/10 đến sau review.

**T017 — review phát hiện lỗi, 2026-11-12 02:35 UTC:** E170 cho thấy batch budget mới tính body, thiếu path/metadata nên không chốt task. Sửa giới hạn, giữ test negative và chạy lại bản kết hợp; E169 là snapshot lịch sử.

**T017 — VERIFY lại, 2026-11-12 02:36 UTC:** E171 sửa E170, cộng toàn bộ persisted text/body vào batch budget và kiểm source cap; targeted mutation và combined checks PASS. Giữ giới hạn lexical, authority, platform, freshness và pagination ở trên; đợi self-review/checker trước DONE.

**T017 — DONE phạm vi adapter/fixture, 2026-11-12 02:36 UTC:** E169/E171/E172 kiểm exact/prefix/positional candidates và bounded FTS trên published snapshot sau fix E170. Lexical coverage và live freshness giữ unknown/not_checked, cursor, service/authority composition và acceptance nằm ở task sau; không suy G2 PASS. B002 8/10; T020 tiếp theo.

**B002 — interim snapshot T017, 2026-11-12 02:37 UTC:** E173 kiểm khôi phục source/spec từ B001 và từng hash, không là independent backup hoặc commit lẻ.

**T020 — assignment/plan, 2026-11-12 02:37 UTC:** `task_mode=implement`, owner codex; G1 PASS phạm vi Linux fixture; T002/T005/T012 DONE, B002 8/10. Change budget `internal/analysis/golang/loader*`, `internal/runtime/exec*` và fixture cùng owner; dùng Go package loader đã pin, executable fingerprint/allowlist, môi trường offline whitelist, profile/capture digest, kiểm package-input closure và callback ParseFile/overlay với bytes capture. Files bị deny/ignored, local replace/dependency chưa đủ authority hoặc toolchain không xác minh phải partial/fail closed; không chạy source target/generator/test hay network. Kiểm fixture sạch và poisoned env, file thay, missing dependency, scope/limits/cancel và self-review trước DONE; không suy Windows native, network isolation production hoặc typed fact integration T014.

**T020 — preflight đang triển khai, 2026-11-12 02:43 UTC:** E174 chỉ chứng minh guards thuần/fixture, chưa chạy trusted loader trên target. `x/tools/go/packages` v0.52.0 gọi `exec.Command("go")` với executable lookup theo PATH của process, dù child env dùng `Config.Env`; không được coi whitelist child env là proof binary. Inventory chỉ báo tổng denied/ignored và walker bỏ qua `.git` riêng; các count không chứng minh không có hidden Go inputs. Callback closure phải được coordinator độc lập chứng minh; E175 kiểm riêng `.git` trong scratch. Tiếp tục ranh giới executable, scratch/closure, runtime budgets và parser contract trước khi chuyển VERIFY.

**T020 — loader và scratch fixture, 2026-11-12 02:58 UTC:** E175 lưu lỗi và E176 kiểm guarded `go/packages` thực trên subset no-import/local-module, không `go generate`; lexical source lấy từ capture, scratch exact được kiểm trước/sau, chưa là OS sandbox chống same-user mutation. `GOPACKAGESDEBUG` không được ghi raw logs, `GOPACKAGESDRIVER/GOFLAGS` inherited bị loại. Chưa hỗ trợ stdlib imports, external dependency, Go tests variants và closure/toolchain proof thực; cache/output growth, network attempts, runtime authority, Windows/race vẫn cần kiểm. T020 vẫn IN_PROGRESS, G2 NOT_RUN; không chuyển T014.

**T020 — VERIFY phạm vi adapter/fixture, 2026-11-12 03:16 UTC:** E178–E180 bổ sung source stdlib dưới root và package closure có authority/fingerprint callback, kiểm input digest trong capture và per-path trước/sau; fixture `fmt` chạy thật. Thiếu toolchain/dependency trả syntax partial trên file caller yêu cầu; selection chỉ gắn file đã được chứng minh; test variants, external require/replace, cgo và multi-module chưa hỗ trợ, trả partial. Proof callback/actor/closure/toolchain trong fixture không phải authority production; PATH và filesystem checks không atomic chống same-user mutation; process-tree/resource enforcement thực, native Windows/race, semantic integration và product E2E vẫn NOT_RUN. Network trace là fixture x86_64 cô lập; các lần mất stop không tính PASS. T020 giữ VERIFY để tự rà diff, validate tracker và đánh giá giới hạn trước khi chuyển task.

**T020 — DONE phạm vi loader adapter/fixture, 2026-11-12 03:17 UTC:** E179–E181 chứng minh controlled loader thực, syntax fallback, negative cases và tracker trên phạm vi đã nêu; tác giả tự review, không nhận independent audit. T014/T016 phải hoàn thiện typed semantics/package variants và coverage; T018/T019 phải nối closure/freshness thực; T032/T033 phải xác minh runtime security/process-resource enforcement. Authorized external dependency/replace, cgo, multi-module và tests chưa có proof vẫn partial, không được quảng cáo semantic complete; nếu case G2 cần thêm loader support thì reopen đúng owner. B002 9/10, G2 NOT_RUN; T014 theo S47.

**T014 — assignment/plan, 2026-11-12 03:18 UTC:** `task_mode=implement`, owner codex; prerequisites T013/T020 DONE theo phạm vi fixture, G2 NOT_RUN, B002 9/10. Change budget `internal/analysis/golang/` và fixtures/tests cùng owner; rework loader T020 tối thiểu để trích `types.Info` trong chính lần load đã xác minh, không để `go/types` object thoát hoặc trộn giữa các lần load. Kết nối declaration ID/range từ syntax capture với Defs/Uses/Selections, phân biệt shadowing/receiver/params/results/fields/generic/implicit switch; unresolved/external giữ boundary và partial, không giả data flow hay semantic coverage. Kiểm oracle source độc lập, build profile, source mismatch, cancellation/budget/revocation và integration thật; test variants hoặc closure chưa chứng minh không được nhận complete. Khi T014 DONE, dừng mở task kế và thực hiện checkpoint B002 đúng S53.

**T014 — rework budget:** mở tối thiểu `internal/runtime/exec_go_environment*` cùng tests để giới hạn Go child/parse workers theo default S15 trong từng load; không thêm dependency hay đổi wire contract. Giới hạn mỗi load không chứng minh scheduler/global CPU, hard RSS hoặc process-tree enforcement của T033.

**T014 — VERIFY phạm vi typed adapter/fixture, 2026-11-12 03:42 UTC:** E182–E184 kiểm Defs/Uses/Selections trong cùng load, compiler objects không thoát; khai báo và use liên kết theo captured range và package identity caller cung cấp, cross-package chỉ khi dependency cùng load có identity; thiếu identity giữ boundary. C005 chưa suy READS/WRITES/pointer data flow, thuộc T015; test variants, external dependencies, runtime authority/freshness, Windows native/race và product E2E vẫn NOT_RUN hoặc partial theo owner. Source/profile mismatch, cancel, giới hạn và revoke sau extraction không trả facts; author self-review chưa là independent audit. G2 NOT_RUN, B002 vẫn 9/10 chờ review/tracker; không mở T015 trước khi T014 DONE và checkpoint.

**T014 — DONE phạm vi typed adapter/fixture, 2026-11-12 03:43 UTC:** E183–E185 chứng minh bindings cho production packages đã chọn và dependencies nội bộ cùng load, giữ explicit boundary/partial cho phần chưa có proof. Package test variants, external dependency, typed graph/READS/WRITES, actor/closure thực và freshness vẫn thuộc T015/T016/T018/T019 và G2; không tuyên bố semantic complete hoặc product E2E. T020 được rework có kiểm lại loader/worker limit trong cùng batch. B002 đủ 10 task, dừng mở T015 và đóng checkpoint theo S53.

**B002 — chuẩn bị checkpoint, 2026-11-12 03:43 UTC:** base `9458db6f082637284c828b46ac0a50507562e848`, branch `main`; 10 task theo S47: T007,T010,T012,T008,T009,T011,T013,T017,T020,T014. Danh mục gồm spec/tracker, source và tests thuộc `internal/{analysis,catalog,indexer,inventory,ports,runtime,search,snapshot,store/sqlite}` và `tests/integration`; Go module/lock và policy docs đã có trong parent. Loại `.vscode/` của người dùng, `artifacts/` cache/toolchain/output, binaries và helper tạm WSL; không chứa source target, runtime DB hay secrets. Kiểm Linux offline dùng Go 1.27.4 đã pin, cache vật tư hiện có và E138–E185; Windows native/race/product E2E chưa xác minh. User đã chỉ định identity Git repository-local; chưa push, chưa có backup độc lập. Commit/tree và restore B002 chưa thực hiện ở thời điểm record này.

**Phạm vi roadmap do user yêu cầu, 2026-11-11:** user yêu cầu hoàn thiện cả 108 task và cho phép tự quyết định kỹ thuật. Phạm vi này bao gồm cả P12–P14 khi đạt prerequisites và gate của chúng; không đổi thứ tự, không chuyển task DEFERRED sang DONE/IN_PROGRESS trước gate, và không suy quyền chạy target, cấp preview hoặc xóa dữ liệu từ yêu cầu triển khai.

### Metadata checkpoint Git — không phải tracker task thứ hai

S53 điều hành nhịp commit; các task vẫn chỉ có trạng thái ở S19. Record sau là trạng thái khởi đầu, chưa quan sát repository/Git thật. Khi có run, ghi batch metadata vào S21, derive summary S22, reconcile actual log/trailers/tag; không tự ghi đã commit/push/restore khi chưa làm.

```yaml
git_batch_control:
  policy: S53
  batch_size: 10
  counting_order: S47
  active_batch_id: B002
  batch_type: full
  assignment_scope_final: false
  authority_ref: null
  active_task_ids: [T007, T010, T012, T008, T009, T011, T013, T017, T020, T014]
  active_done_count: 10
  base_commit_oid: 9458db6f082637284c828b46ac0a50507562e848
  branch: main
  commit_binding: containing_commit
  current_commit_oid: null
  current_tree_oid: null
  planned_checkpoint_tag: mce4d/checkpoints/B002
  last_verified_checkpoint_oid: 9458db6f082637284c828b46ac0a50507562e848
  last_verified_checkpoint_tag: mce4d/checkpoints/B001
  remote_alias: null
  remote_branch: null
  push_status: NOT_CONFIGURED
  independent_backup_status: NOT_VERIFIED
  interim_snapshot_ref: /home/mce4d/.local/share/mce4d/snapshots/b002-precheckpoint-20261112-0345
  project_restore_verification: NOT_RUN
```

Commit hiện hành được nhận diện từ containing commit/trailers và tag theo GIT-05, không tự lưu own SHA trong source đã commit. OID/tag của batch trước chỉ đi vào spec ở lần batch tiếp theo khi cần; không commit phụ để cập nhật receipt. Không đưa bản ghi này vào wire/schema/SQLite của engine.

`batch_type` chỉ điền khi chuẩn bị checkpoint; `scope_final` cần `assignment_scope_final=true` và `authority_ref` dẫn tới quyết định/phạm vi thật; `authorized_recovery` cần `authority_ref` riêng. Validator chỉ kiểm record có cấu trúc hợp lệ, người thực hiện vẫn phải kiểm nguồn authority và scope thực trước commit; một chuỗi metadata không tự cấp quyền.

### Known gaps / discovery register

| Gap | Tác động | Cách resolve | Owner task | Ban đầu |
|---|---|---|---|---|
| K001 | Chưa có repository/module path/toolchain thực tế | Khảo sát workspace, pin chỉ sau xác minh | T001,T002 | OPEN |
| K002 | Chưa pin/test dependency bundle | Smoke compatibility + license artifact review | T002 | OPEN |
| K003 | Chưa biết bộ repo đại diện và host token telemetry | Dùng synthetic trước; chỉ dùng real roots sau quyền; ghi measurement limits | T038,T039 | OPEN |
| K004 | Chưa verify Windows/WSL filesystem/ACL/LSP thực | Matrix tests; giới hạn supported label theo evidence | T036,T037 | OPEN |
| K005 | Không có editor unsaved-buffer ingestion R1 | Disk-only warning; R3 editor bridge có gate, không fake freshness | T073 | KNOWN_LIMIT |
| K006 | Dynamic runtime/cross-service/unsupported languages chưa đầy đủ | Boundary diagnostics; R2 provider/contract gates | T043–T048 | KNOWN_LIMIT |
| K007 | Chưa kiểm tra nhãn hiệu/sáng chế/quyền phát hành riêng | Không public claim; legal/license review trước distribution | T041 | OPEN |
| K008 | Chưa có bằng chứng scale M/L/XL hay token savings thực | Fixture scope/config/resources rõ; không tăng supported label trước đo | T053,T039,T077 | OPEN |
| K009 | Chưa có concrete TS/Python/contract provider API/schema pins | Discovery chính thức/license/fixtures; không lấy implementation tham khảo | T043,T045,T055,T057 | OPEN |
| K010 | Chưa có target frontend/URL/build metadata được kiểm tra | T081 khảo sát repo thật được phép; fixtures tự viết; không bịa framework/route thật | T081 | OPEN |
| K011 | Chưa kiểm chứng effect isolation khi preview app chạy | Dedicated dev/test profile + no production creds/endpoints; trước attach/interact phải thỏa S37 | T087,T088,T102 | OPEN |
| K012 | Không có mapping phổ quát cho RSC/SSR/closed shadow/canvas/third-party runtime | Capability matrix/unresolved; chỉ thêm adapter sau test, không Fiber heuristic | T085,T091,T095 | KNOWN_LIMIT |
| K013 | Ba nguồn docs mới được kiểm ở attachment; chưa xác minh đã copy vào repository | T001 verify exact path/version/hash theo S50, ghi S21; không suy từ tên upload | T001,T003 | OPEN |
| K014 | Chưa có actual command/output-volume budget hoặc build guard đã kiểm trong project | Preflight nhỏ T002, proof T107, tooling/OS integration T033; chặn workload thiếu điều kiện | T002,T107,T033 | OPEN |
| K015 | Chưa xác minh Git branch/history/remote/quyền backup và checkpoint thực của MCE4D | T001 xác minh trong workspace được giao, S53 batch/restore; proof synthetic không thay actual repo | T001,T003,T107,T040 | OPEN |
| K016 | Chưa xác định repo/stack/tác vụ ưu tiên cho dùng thử thật và quyền liên quan | T001 ghi discovery S01; T038 chọn corpus theo S18; dùng fixture nếu chưa có quyền, không đổi Go-first | T001,T038,T042 | OPEN |
| K017 | Chưa có ước lượng effort/nguồn lực dựa trên dữ kiện triển khai | Lập và cập nhật forecast theo S47 từ phạm vi và quan sát; nêu unknown, không đổi gate hoặc hứa deadline | T001,T108,T040 | OPEN |
| K018 | Gói proof lịch sử được nhắc tại S48 chưa thấy trong checkout khảo sát ở revision `ba72ae4` | Xác minh manifest/hash nếu có nguồn hợp lệ; nếu thiếu, tái tạo kiểm tra cần thiết trong task owner, giữ lịch sử và status chưa xác minh | T003,T106,T107,T108,T040 | OPEN |

OPEN không mặc định là câu hỏi bắt người dùng trả lời; agent tự resolve bằng tài liệu chính thức/môi trường khi có thể. Chỉ yêu cầu thông tin/quyền khi không thể suy ra an toàn; không tự chọn private repos để thử hoặc gọi API trả phí.

### Change log của đặc tả

| Revision | Date | Change | Product evidence |
|---|---|---|---|
| 1.0.0 | 2026-10-13 | Baseline trước đổi tên; requirements, architecture, schema, API, tasks, acceptance | NONE; implementation NOT_STARTED |
| 1.1.0 | 2026-10-13 | Đổi tên toàn diện; S26–S33; overview/scale/analysis mode/spec-only/self-index; R2 đa stack/contracts/state machines/domain/AI; R3 evidence/editor/storage; toolchain/protocol pin tại discovery; 80 tasks/64 requirements/80 cases | DOC_ONLY; implementation NOT_STARTED, product tests NOT_RUN |
| 1.2.0 | 2026-10-13 | Tích hợp Code↔UI Explorer S34–S40; two-way selection, build/instance mapping, static backend flow, permission boundary, loopback API; U0–U3 track; 104 tasks/88 requirements/112 cases; core chưa triển khai | DOC_ONLY; product build/tests/benchmarks NOT_RUN |
| 1.3.0 | 2026-10-13 | Construction blueprint S41–S49; early design seals; normalized contract/DDL; catalog-record-index ownership; build binding and session ACK flows; precise schedule/work cards; 108 tasks/104 requirements/136 cases | DOC_ONLY; product NOT_RUN |
| 1.4.0 | 2026-10-13 | Tiếp nhận có chọn lọc ba nguồn docs; S50 authority/version/mode, S51 command/storage/cleanup; Change Budget/Verification Ladder/evidence guard; 112 requirements/148 cases, giữ 108 task và contracts/DDL/order | DOC_ONLY; nguồn attachment giữ nguyên; project installation/enforcement/product NOT_VERIFIED/NOT_RUN |
| 1.5.0 | 2026-10-14 | Khép audit A01–A08; GC FK tree, resolver identity, status+permission admin, paging/source fragments, pinned group scope, RuntimeConfig, lifecycle/restore incarnation, codec vectors/capture separation; HMR/record retention follow-through; 53 sections,124 requirements,108 tasks,180 cases | DOC_ONLY; current document proofs S48/S52; product/driver/browser/real-repo NOT_RUN |
| 1.6.0 | 2026-11-11 | S53 commit một nhóm10 task DONE; staged completeness, verified checkpoint/tag, source restore, scope_final, interim snapshot và push/backup có quyền;54 sections,130 requirements,108 tasks,192 cases; giữ canonical schema/DDL/order | DOC_ONLY; kiểm tài liệu và Git fixture riêng S48; chưa commit/push repository người dùng |
| 1.6.1 | 2026-11-11 | Làm rõ discovery repo/stack và môi trường, corpus benchmark, mốc thử sử dụng, forecast theo phase và cách xử lý proof lịch sử; giữ scope, kiến trúc, contract, 54 sections, 130 requirements, 108 tasks, 192 cases và lịch/gates | DOC_ONLY; product/build/test/benchmark NOT_RUN; không kế thừa kết quả validation cũ cho revision mới |
| 1.6.2 | 2026-11-11 | User duyệt ngoại lệ IP-05a/A041 giới hạn dependency MPL trong đồ thị test SQLite; không mở allowlist chung hay quyền phân phối | T001/T002 fixture evidence E001–E004; product/Windows acceptance chưa đạt |
| 1.6.3 | 2026-11-11 | User chấp thuận khóa phụ byte MCE-C14N1 cho unavailable_members đồng hạng trong scope-v1; giữ các khóa sort đã chốt và wire schema | D009; implementation/proof tiếp tục trong T105, G0 chưa seal |
| 1.6.4 | 2026-11-11 | User cho phép static_analyzer + explicit_document khi có nguồn tài liệu rõ ràng; không coi nhãn là bằng chứng quyền hay compiler proof | D010; implementation/proof tiếp tục T105 |
| 1.6.5 | 2026-11-11 | User chấp thuận cấu trúc và thứ tự file_preconditions cùng thứ tự conflicts cho rename-result-v1 | D011; implementation/proof tiếp tục T105 |
| 1.6.6 | 2026-11-11 | User chấp thuận khóa phụ E(toàn edit) cho rename edits đồng hạng | D012; implementation/proof tiếp tục T105 |
| 1.6.7 | 2026-11-11 | User chấp thuận chuẩn hóa ScopeVector lồng trong subgraph-v1 và rename-result-v1 theo scope-v1 | D013; implementation/proof tiếp tục T105 |
| 1.6.8 | 2026-11-11 | User chấp thuận ranh giới segment cho approved_path_prefix của preview | D014; implementation/proof tiếp tục T105 |
| 1.6.9 | 2026-11-11 | User chấp thuận bảng hướng đóng cho toàn bộ 23 thông điệp bridge | D015; implementation/proof tiếp tục T105 |

Changelog sau này chỉ ghi thay đổi có ảnh hưởng contract/scope/gate và task đã verify. Không ghi hàng dài về mỗi file nhỏ; không duplicate source diff.

Discrepancy register (điền khi có bằng chứng về docs/source/policy/environment/implementation, ghi đúng lớp):

| ID | Task | Category | Evidence | Affected contracts/tasks | Resolution/authority | Status |
|---|---|---|---|---|---|---|
| D001 | T106 | spec_conflict | Audit A01 v1.4.0; current closure/proofs S52/S48 | R113 và owner tasks S52 | User-approved revision1.5.0; sửa owner contract, không implementation | RESOLVED_IN_SPEC_PRODUCT_UNVERIFIED |
| D002 | T085 | spec_conflict | Audit A02 v1.4.0; current closure/proofs S52/S48 | R114 và owner tasks S52 | User-approved revision1.5.0; sửa owner contract, không implementation | RESOLVED_IN_SPEC_PRODUCT_UNVERIFIED |
| D003 | T087 | spec_conflict | Audit A03 v1.4.0; current closure/proofs S52/S48 | R115 và owner tasks S52 | User-approved revision1.5.0; sửa owner contract, không implementation | RESOLVED_IN_SPEC_PRODUCT_UNVERIFIED |
| D004 | T025 | spec_conflict | Audit A04 v1.4.0; current closure/proofs S52/S48 | R116 và owner tasks S52 | User-approved revision1.5.0; sửa owner contract, không implementation | RESOLVED_IN_SPEC_PRODUCT_UNVERIFIED |
| D005 | T045 | spec_conflict | Audit A05 v1.4.0; current closure/proofs S52/S48 | R117 và owner tasks S52 | User-approved revision1.5.0; sửa owner contract, không implementation | RESOLVED_IN_SPEC_PRODUCT_UNVERIFIED |
| D006 | T004 | spec_conflict | Audit A06 v1.4.0; current closure/proofs S52/S48 | R118,R119 và owner tasks S52 | User-approved revision1.5.0; sửa owner contract, không implementation | RESOLVED_IN_SPEC_PRODUCT_UNVERIFIED |
| D007 | T035 | spec_conflict | Audit A07 v1.4.0; current closure/proofs S52/S48 | R120 và owner tasks S52 | User-approved revision1.5.0; sửa owner contract, không implementation | RESOLVED_IN_SPEC_PRODUCT_UNVERIFIED |
| D008 | T105 | spec_conflict | Audit A08 v1.4.0; current closure/proofs S52/S48 | R121 và owner tasks S52 | User-approved revision1.5.0; sửa owner contract, không implementation | RESOLVED_IN_SPEC_PRODUCT_UNVERIFIED |
| D009 | T105 | spec_conflict | Hai MemberUnavailable có cùng service_id=null, worktree_id=null, status=access_denied nhưng reason_code khác; hai input order tạo hai preimage digest khác nhau, fixture `tests/contract/scope_tie_test.go` | CT-04 scope-v1, R117/R121, T105/T107 | User chọn sort phụ theo byte E(toàn phần tử) sau ba khóa hiện có; sửa revision 1.6.3, không đổi schema/quyền; kiểm implementation độc lập | RESOLVED_IN_SPEC_PRODUCT_UNVERIFIED |
| D010 | T105 | spec_conflict | S10/RelationFact có explicit_document nhưng SV21 không có origin cho kind này | SV21 EvidenceRef, R121, T105/T107 | User cho phép static_analyzer + explicit_document với nguồn tài liệu rõ ràng; revision 1.6.4, không tự xác nhận nguồn | RESOLVED_IN_SPEC_PRODUCT_UNVERIFIED |
| D011 | T105 | spec_conflict | rename-result-v1 chưa định nghĩa file_preconditions và khóa sắp conflicts, có thể tạo digest khác giữa các triển khai | CT-04 rename-result-v1, R121, T105/T107 | User chọn precondition gồm worktree_id, profile_id, generation_id, path, content_hash; sort theo thứ tự trường đó, conflicts sort theo byte E(toàn Gap); revision 1.6.5 | RESOLVED_IN_SPEC_PRODUCT_UNVERIFIED |
| D012 | T105 | spec_conflict | Rename edits khác identity có thể đồng hạng ở path/start/end/new_text và tạo digest phụ thuộc thứ tự input | CT-04 rename-result-v1, R121, T105/T107 | User chọn sort phụ theo byte E(toàn edit); revision 1.6.6, giữ các khóa hiện có | RESOLVED_IN_SPEC_PRODUCT_UNVERIFIED |
| D013 | T105 | spec_conflict | Vector cùng scope-v1 digest nhưng thứ tự members/unavailable khác có thể tạo subgraph/rename digest khác | CT-04 subgraph-v1/rename-result-v1, R121, T105/T107 | User chọn sort members/unavailable theo scope-v1 trước khi đưa ScopeVector vào hai preimage; revision 1.6.7 | RESOLVED_IN_SPEC_PRODUCT_UNVERIFIED |
| D014 | T105 | spec_conflict | approved_path_prefix không quy định `/app` có cho phép `/application` hay không | SV13 preview URL, R095, T105/T081 | User chọn `/app` chỉ match chính nó hoặc `/app/` và hậu duệ; so trên browser-canonical pathname, `/` bao phủ pathname tuyệt đối | RESOLVED_IN_SPEC_PRODUCT_UNVERIFIED |
| D015 | T105 | spec_conflict | Bridge yêu cầu direction closed nhưng chưa có bảng đầy đủ cho 23 message types | CT-02 SV14, AL-04, R096, T105/T081 | User chấp thuận bảng parent→SDK, SDK→parent và ERROR hai chiều trong CT-02; revision 1.6.9 | RESOLVED_IN_SPEC_PRODUCT_UNVERIFIED |

## S22 — Trạng thái tiếp tục cho phiên AI sau

```yaml
resume:
  spec_version: 1.6.9
  implementation_revision: null
  release_label: NOT_BUILT
  gate: G2
  design_seal: PASS
  construction_plan: S47
  policy_registry: S50
  command_storage_policy: S51
  audit_closure: S52
  git_batch_policy: S53
  git_batch_size: 10
  git_batch_current_id: B002
  git_batch_done_count: 10
  last_git_checkpoint: B001
  git_project_restore: NOT_RUN
  git_remote_status: CONFIGURED_PUSH_NOT_AUTHORIZED
  audit_baseline: 1.4.0
  canonical_codec: MCE-C14N1
  remaining_scope_gate: ACTUAL_TOOLCHAIN_OS_BROWSER_AND_REAL_REPO_VERIFICATION
  policy_adoption_status: ACCEPTED_SCOPED
  project_policy_files_status: VERIFIED_MANUAL_HASH
  policy_runtime_enforcement: NOT_IMPLEMENTED
  last_request_mode: implement
  next_task_mode: REQUIRES_CURRENT_ASSIGNMENT
  foundation_sequence: [T001, T002, T003, T105, T106, T107, T108]
  next_task: T015
  active_task: null
  active_owner: null
  done_r1_tasks: 20
  total_r1_tasks: 52
  deferred_r2_tasks: 20
  deferred_r3_tasks: 12
  explorer_requested_tracks: [U0, U1, U2]
  explorer_active_gate: null
  explorer_u0_todo: 4
  explorer_u1_todo: 10
  explorer_u2_todo: 6
  explorer_u3_deferred: 4
  explorer_implementation_status: NOT_STARTED
  preview_execution_permission: NOT_GRANTED_FOR_ANY_TARGET
  total_roadmap_tasks: 108
  product_build: NOT_RUN
  product_tests: NOT_RUN
  product_benchmark: NOT_RUN
  blocking_user_decision: null
  discovery_gaps: [K001, K002, K003, K004, K007, K008, K009, K010, K011, K013, K014, K015, K016, K017, K018]
  known_limits: [K005, K006, K012]
  last_evidence_id: E186
  last_checked_at_utc: 2026-11-12T03:45:00Z
```

MUST cập nhật resume chỉ sau khi tracker/evidence nhất quán; là summary derived, không nguồn thay thế task rows. Không lưu chain-of-thought vào tài liệu. Chỉ lưu quyết định, bằng chứng, lỗi tái hiện được và hành động tiếp theo.

Checklist mỗi phiên: validate spec → inspect git status/actual modules và reconcile batch S53 → chọn ready task → đọc sections đúng scope → nhận ownership → implement/test → review → ghi evidence → cập nhật task/gate/resume → đủ10 thì đóng checkpoint đầy đủ; chưa đủ thì giữ tiến độ/snapshot được phép, không commit lẻ. Nếu interruption hoặc thiếu quyền: ghi exact state/cmd còn NOT_RUN; không tuyên bố sẽ tiếp tục chạy nền.

Điều kiện dừng agent: người dùng yêu cầu dừng; hết quyền cần thiết; P0 invariant conflict; thiếu nguồn API/capability không thể xác minh; tài nguyên/deadline đạt limit. Không dừng chỉ vì “project lớn”; làm task nhỏ nhất hoàn chỉnh và ghi đúng trạng thái. Không chuyển TODO thành DONE để đáp ứng lời hứa tiến độ.

## S23 — Nguồn kỹ thuật và pháp lý tham khảo

Kiểm tra web khi soạn: 2026-10-13. Các URL là tài liệu chính thức/metadata package; không có source implementation sản phẩm tham khảo. Facts/version có thể thay đổi: T002/T029/T041 phải kiểm tra đúng version pin trước dùng. Các quy định thiết kế trong file là quyết định riêng của MCE4D, không phải tính năng mà nguồn dưới đây hứa cung cấp.

| Ref | Nguồn | Phạm vi fact được dùng |
|---|---|---|
| REF01 | `https://go.dev/doc/devel/release` | Release history và supported-release policy; không pin phiên bản chỉ từ kết quả tìm kiếm |
| REF02 | `https://go.sdk.modelcontextprotocol.io/` | Official Go SDK; không tự bịa API SDK |
| REF03 | `https://modelcontextprotocol.io/specification/latest/server/tools` | Tool protocol/structured output; negotiate version với SDK/client thực |
| REF04 | `https://www.sqlite.org/wal.html` | WAL/read snapshot/single-writer và filesystem constraints |
| REF05 | `https://www.sqlite.org/pragma.html` | Foreign-key/pragma configuration; xác minh trên từng connection |
| REF06 | `https://www.sqlite.org/fts5.html` | FTS5; lexical/full-text search không phải semantic binding |
| REF07 | `https://pkg.go.dev/golang.org/x/tools/go/packages` | Go package loading, build driver, test variants và overlays |
| REF08 | `https://go.dev/gopls/features/navigation` | Definitions/references/call hierarchy và giới hạn build/dynamic calls |
| REF09 | `https://go.dev/gopls/features/transformation` | Rename safeguards và giới hạn runtime/reflection |
| REF10 | `https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/` | Position encodings, text edits và LSP request contracts; verify negotiated version |
| REF11 | `https://pkg.go.dev/modernc.org/sqlite` | Driver/package/license metadata; không mặc định mọi bundled artifact cùng license |
| REF12 | `https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk` | Module/version compatibility và nhiều license thành phần |
| REF13 | `https://pkg.go.dev/golang.org/x/tools/gopls` | Module/tool version/license metadata cho gopls |
| REF14 | `https://www.sqlite.org/backup.html` | Backup API; không copy DB đang live tùy tiện |
| REF15 | `https://www.copyright.gov/help/faq/faq-protect.html` | Tham khảo nguyên tắc ý tưởng so với biểu đạt; không thay tư vấn luật áp dụng |
| REF16 | `https://www.apache.org/licenses/LICENSE-2.0` | Ví dụ nghĩa vụ license/notices; phải audit dependency cụ thể |
| REF17 | `https://git-scm.com/docs/git-ls-files` | Tracked/untracked/ignore và NUL-delimited file listing |
| REF18 | `https://developers.openai.com/codex/mcp` | Codex local STDIO và cấu hình; verify host version/environment |
| REF19 | `https://www.typescriptlang.org/docs/handbook/project-references.html` | Project references, compiler scope và module boundaries; không phải universal graph extraction |
| REF20 | `https://www.typescriptlang.org/tsconfig/incremental.html` | Incremental compiler metadata; không tự dùng internal format làm storage contract |
| REF21 | `https://docs.python.org/3/library/ast.html` | Python AST syntax và giới hạn; parsing không chứng minh runtime bindings |
| REF22 | `https://spec.openapis.org/oas/` | Versioned OpenAPI specs; chọn supported versions qua adapter fixtures |
| REF23 | `https://www.asyncapi.com/docs/concepts/asyncapi-document/structure` | Channels/messages/operations; versioned direction và namespaces |
| REF24 | `https://docs.nats.io/learn/jetstream/pull-consumers` | Consumer/ack/redelivery controls; không suy business exactly-once |
| REF25 | `https://dev.mysql.com/doc/refman/8.4/en/stored-objects.html` | Views/routines/triggers/events; dùng đúng dialect/version của target, không ép 8.4 |
| REF26 | `https://mlflow.org/docs/latest/ml/dataset/` | Metadata lineage/schema/digest/split; không bắt buộc dependency MLflow |
| REF27 | `https://docs.pytorch.org/docs/stable/generated/torch.load` | Unpickling warning; engine không load checkpoint để phân tích |
| REF28 | `https://opentelemetry.io/docs/concepts/signals/traces/` | Spans/links và observed runtime evidence; không bắt buộc telemetry service |
| REF29 | `https://www.sqlite.org/whentouse.html` | Embedded app/local storage suitability và concurrency constraints; benchmark trước thay storage |
| REF30 | `https://www.w3.org/TR/scxml/` | State/transition/guard/hierarchy/parallel concepts; không bắt buộc implement SCXML runtime |
| REF31 | `https://vite.dev/guide/api-plugin` | Plugin transform/HMR hooks; API/version pinned; không mặc định mapping DOM có sẵn |
| REF32 | `https://tc39.es/ecma426/` | Source maps generated→original locations; không chứa component runtime identity |
| REF33 | `https://developer.mozilla.org/en-US/docs/Web/API/Window/postMessage` | Cross-origin messages; exact targetOrigin/origin/source validation |
| REF34 | `https://developer.mozilla.org/en-US/docs/Web/Security/Defenses/Same-origin_policy` | Iframe origin isolation; parent không tự đọc DOM cross-origin |
| REF35 | `https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Content-Security-Policy/frame-ancestors` | Embedding policy; không bypass bằng proxy stripping |
| REF36 | `https://developer.mozilla.org/en-US/docs/Web/API/Document/elementFromPoint` | Hit test trong document/viewport; cross-origin iframe needs local bridge |
| REF37 | `https://react.dev/reference/react-dom/createPortal` | Portal DOM location khác React ownership/events |
| REF38 | `https://react.dev/reference/react/Children` | Children API không cho full rendered ownership tree |
| REF39 | `https://reactflow.dev/learn/advanced-use/performance` | Controlled rendering/performance; không chứng minh unlimited graph scale |
| REF40 | `https://github.com/xyflow/xyflow/blob/main/LICENSE` | React Flow core MIT; audit artifact/version/dependency; paid examples terms riêng |
| REF41 | `https://github.com/dagrejs/dagre` | Directed layout và MIT; dùng package hiện được maintainer chỉ định, pin ở T081 |
| REF42 | `https://www.w3.org/TR/trace-context/` | traceparent/tracestate interoperability và privacy; không tự map span→source |
| REF43 | `https://opentelemetry.io/docs/concepts/context-propagation/` | Causal cross-service context; instrumentation từng boundary |
| REF44 | `https://opentelemetry.io/docs/specs/semconv/db/database-spans/` | Database spans/table info có điều kiện; sanitized metadata; không mặc định commit proof |
| REF45 | `https://playwright.dev/docs/network` | Browser network fixtures/isolation tests; service worker visibility caveat |
| REF46 | `https://www.w3.org/WAI/ARIA/apg/patterns/windowsplitter/` | Keyboard accessible split pane; kiểm thử accessibility thực, không self-certify |
| REF47 | `https://opentelemetry.io/docs/languages/js/getting-started/browser/` | Browser instrumentation examples và status; pin runtime capability qua tests |
| REF48 | `https://go.dev/doc/database/execute-transactions` | Statement trong transaction khác Commit/Rollback; source không là bằng chứng persisted runtime |
| REF49 | `https://developer.mozilla.org/en-US/docs/Web/API/Event/stopPropagation` | Propagation/default behavior là hai vấn đề; không chỉ stopPropagation để hứa Inspect no action |

Không lấy mô tả marketing, số stars hoặc tool name làm evidence correctness. Không tái tạo license text dài trong spec; khi phân phối dependency phải ship thông báo được yêu cầu dưới file pháp lý thích hợp, dù đây vẫn là tài liệu thiết kế duy nhất.

Nguồn bổ sung khi rà soát v1.3.0 (không phải dependency đã pin/test trên target):

| Ref | Nguồn | Phạm vi |
|---|---|---|
| REF50 | `https://www.sqlite.org/foreignkeys.html` | Composite FK/connection settings; checked for physical schema design |
| REF51 | `https://www.sqlite.org/pragma.html#pragma_secure_delete` | Deletion/secure_delete limits, including FTS traces; no forensic erase guarantee |
| REF52 | `https://www.sqlite.org/fts5.html#the_secure_delete_configuration_option` | FTS secure-delete compatibility must be pin-tested, not inferred from DELETE |
| REF53 | `https://go.dev/src/os/root.go` | Root containment does not replace no-follow/device/mount policy; verify APIs |
| REF54 | `https://go.dev/doc/database/execute-transactions` | database/sql Tx ownership, Commit/Rollback and error semantics |
| REF55 | `https://json-schema.org/draft/2020-12/schema` | JSON Schema dialect for canonical closed wire records; semantic validators additional |

### Xác minh kỹ thuật cho v1.5.0 — 2026-10-14

Đối chiếu lại REF04/REF05/REF14 về WAL/FK/backup và REF33 về postMessage tại tài liệu chính thức. Quy trình bảo trì, codec và contracts ở v1.5.0 là thiết kế riêng, không tính năng do các nguồn này cung cấp. Các version dependency thực tế vẫn được pin/test tại T002/T081; không nâng version chỉ vì ngày soạn mới. Không tra hoặc kế thừa source implementation GitNexus/Graphify trong bản cập nhật này.

### Nguồn Git cho policy v1.6.0 — kiểm ngày 2026-11-11

| Ref | Nguồn chính thức | Phạm vi fact; policy nhóm10 là yêu cầu riêng, không phải Git mặc định |
|---|---|---|
| REF56 | `https://git-scm.com/docs/git-commit` | Commit từ staging, -a không thêm file mới; không dùng Git commit như backup mọi dữ liệu |
| REF57 | `https://git-scm.com/docs/git-add` | Stage đúng content lúc add; ignored/new/deleted paths cần xét rõ |
| REF58 | `https://git-scm.com/docs/git-worktree` | Checkout mốc tại worktree riêng; vẫn chia sẻ Git repository, không backup độc lập |
| REF59 | `https://git-scm.com/docs/git-push` | Push explicit refs và atomic capability; permission/network kiểm riêng |
| REF60 | `https://git-scm.com/docs/git-revert` | Revert tạo thay đổi đảo trên lịch sử, không khôi phục database/dịch vụ ngoài |
| REF61 | `https://git-scm.com/docs/git-bundle` | Bundle/prerequisites/verify; không chứa working tree/index hoặc dữ liệu ngoài Git |

## S24 — Roadmap mở rộng có gate

Explorer U0–U2 = T081–T100; scope đã mở theo yêu cầu người dùng, còn permission chạy target là riêng. U3 = T101–T104 optional. Không chờ Python/AI/R3 để làm U1; theo dependency closure S39.

R2 = T043–T048,T055–T068: TS/JS, Python, frontend, HTTP/schema/proto, SQL, events, cross-repo vectors, approved state/decision/architecture, semantic diff và AI metadata lineage. Thiết kế/contract ở S28–S30/S32; không bắt đầu trước G5 và mở scope. Task T048 chỉ review sau T068 và các dependencies DONE.

R3 = T069–T080: local runtime/test artifacts, editor buffers, optional semantic retrieval, measured storage scale, deployment metadata và provider theo nhu cầu. Chi tiết S27/S31/S32; chưa mở capability thì giữ DEFERRED. Không cần mọi addon để R1/R2 hữu ích.

Mọi extension phải có schema/DTO/capability acceptance trước code, giữ source/policy/snapshot invariants và cập nhật requirement/task/case trong cùng tài liệu. Core không gọi LLM, network, arbitrary scripts, production DB hoặc target test để tự “khám phá”. U0–U2 mở GUI local và U1 attach preview đã chạy khi người dùng cho phép; tải preview là thực thi target trong browser, không thuộc bảo đảm offline/read-only-runtime của indexer. Permission S37 tách riêng. Distributed indexing/3D vẫn FUTURE, GUI 2D không còn FUTURE.

## S25 — Lệnh khởi động dành cho AI thực hiện tài liệu

Đọc S00 và resume S22. Kiểm tra repository hiện có. Bắt đầu T001; thứ tự S47, foundation T001/T002/T003/T105/T106/T107/T108 trước feature. Không chọn task chỉ theo số ID. Dùng đặc tả này làm nguồn yêu cầu duy nhất; giữ implementation độc lập; không tải source sản phẩm tham khảo. Triển khai lát cắt nhỏ, test thật, cập nhật tracker/evidence trong cùng file. Chưa có evidence thì giữ NOT_RUN/TODO/VERIFY đúng thực tế. Không tự mở rộng ngoài track/capability đã mở, không auto-write target hay thay stack ngoài quy trình ADR. Explorer U0–U2 là scope đã được yêu cầu nhưng chỉ nhận task khi đủ prerequisites S39; U3 cần phép runtime riêng.

## S26 — 4D, bản đồ phân cấp và dự án chưa có code

### Quy ước sản phẩm

Tên sản phẩm do người dùng chọn: `Maps-Context-Engine-4D`; project/module local slug `maps-context-engine-4d`; CLI `mce4d`; prefix MCP `mce4d_`; state env `MCE4D_HOME`. Chưa có binary/repository deployment được kiểm chứng. Không mặc định tên đã được clearance nhãn hiệu. Không cần tương thích alias tên cũ khi chưa có implementation được phát hành; nếu discovery thấy bản cũ đã chạy, dừng migration tự động và lập ADR/rebind riêng.

`4D` là tổ chức thông tin do bản đặc tả đề xuất, không là thuật ngữ chuẩn, hệ tọa độ đồ họa, cam kết mô phỏng hay graph có bốn dimensions toán học:

| Chiều | Dữ liệu cần biểu diễn | Câu hỏi hỗ trợ | Release |
|---|---|---|---|
| D1 Structure | workspace/module/file/scope/symbol, ownership và boundary | Thành phần ở đâu, thuộc module nào? | R1 |
| D2 Interactions & Data | calls/refs, API/event/SQL/schema, source-to-consumer links | Thành phần nào dùng dữ liệu này; thay đổi lan tới đâu? | R1 code; R2 contracts |
| D3 Behavior & Constraints | trạng thái/chuyển trạng thái, business invariant được duyệt, failure/recovery obligations | Hành vi nào và điều kiện nào phải được giữ? | R2; runtime evidence R3 |
| D4 Evolution & Evidence | snapshot/profile, changed facts, quyết định có version, test/run evidence | Điều gì đổi, bằng chứng thuộc phiên bản nào, quyết định nào cần xem lại? | R1 snapshots; R2 diff; R3 observed runs |

Không đổi tên capability R1 thành “4D complete”. Runtime status trả coverage từng chiều. D3/D4 chưa implement phải là `not_available` hoặc `partial`, không trường rỗng mang nghĩa đầy đủ.

### Bản đồ phân cấp

H01 Hierarchy: repository → declared system/module → package/file → symbol/source range. Module discovery dùng manifest/workspace boundary được parse an toàn; folder convention chỉ là candidate. Một module có thể chứa nhiều package; shared package không được tự gán riêng cho consumer đầu tiên. Mọi child count/pagination đều scoped generation/profile/policy.

H02 Overview deterministic gồm ngôn ngữ/phạm vi đã phát hiện, entry points có bằng chứng, dependency giữa module, tài liệu trusted, test roots, known gaps, scale statistics. Tóm tắt bằng template từ facts; không cần LLM. Không bịa mô tả nghiệp vụ từ tên folder. Dùng overview để chọn scope, không thay source cần review.

H03 Mọi cạnh bị gom ở module view phải có `expand_ref` truy về các edge/source sites trong cùng generation; giữ loại hướng và evidence. Cluster/topic summary không là proof liên thông execution. Shared/hub symbol trả danh mục consumer theo nhóm + continuation; không cắt caller hiếm chỉ vì ít centrality.

H04 Vocabulary được khai báo/trusted có thể nối từ nghiệp vụ Việt/Lào/Anh với entity ID/source anchors: “số dư khả dụng”, “available balance”, “available_balance”. Alias chỉ dùng tìm ứng viên, không tự tạo CALLS/READS/WRITES hoặc gộp entity. Cùng một từ có nhiều nghĩa phải trả ambiguity. R1 dùng explicit aliases trong approved config; record lifecycle đầy đủ ở T062.

### Greenfield / spec-only / brownfield

G01 Git repository chưa commit hoặc chỉ có tài liệu được phép có overview: `project_stage=spec_only|mixed|implemented_scope`, `implementation_evidence=none|partial|observed`. Engine không tự `git init`, tạo source hay bootstrap target. Chưa có git root hợp lệ thì thông báo prerequisite; AI xây target thực hiện ngoài quyền read-only engine.

G02 Planned component lấy từ tài liệu được khai báo, giữ `plane=intended` và anchor/hash. Component từ code có `plane=observed_static`. Runtime import dùng `plane=observed_runtime`. Chỉ có tài liệu “PaymentService” không được tạo symbol implementation hoặc CALLS thật. Mapping intended↔observed cần source/explicit link; chưa map là `unmapped`, không phải missing implementation được chứng minh.

G03 `task_kind=plan` và workspace/module/document selector trả mục tiêu, constraints trích nguồn, thành phần đã có và gaps; không tự thiết kế requirement mới. ADR/state records chỉ thành ràng buộc khi approved theo S30. Repo đang phát triển giữ source/rules hiện hữu; onboarding không thay convention.

### Tự dùng công cụ để phát triển chính công cụ

Bootstrap T001–T042/T049–T054 không yêu cầu `mce4d` đã tồn tại. Sau khi có candidate binary được build/test, có thể index source của chính engine bằng binary pin tại một revision đã biết, state ở ngoài target. Binary đang chạy không bị overwrite giữa job; source revision và analyzer revision ghi riêng.

T054 so sánh thao tác self-index với oracle độc lập và công cụ nền không dùng engine. Output engine không tự làm oracle/golden. Lỗi engine phải có đường fallback đọc source/LSP/test trực tiếp; không tạo vòng phụ thuộc “phải dùng engine hỏng mới sửa được engine”. Sau nâng cấp provider phải invalidate đúng cache, không reuse fact chỉ vì source target không đổi.

## S27 — Kiến trúc tối ưu cho repository lớn

### Những chi phí phải đo riêng

`T_total = inventory + capture/hash + parse/type + graph_write + query + context_pack + consumer_verification`. Local I/O/CPU không phải LLM token. Báo warm/cold, tree size, excluded bytes, external metadata, peak memory toàn cây process và write amplification. Không dùng graph-only latency để quảng cáo edit latency đã bao gồm strict freshness.

| Rủi ro thiết kế cũ | Thiết kế giữ tính đúng | Task |
|---|---|---|
| Duyệt graph toàn repo trước mọi câu hỏi | Overview → module → symbol; SQL adjacency có index, traversal bounded | T049,T051 |
| Hash toàn workspace cho từng lượt tra cứu trong một cuộc điều tra | Cho phép truy vấn analysis trên generation bất biến; xác minh live riêng trước edit | T052 |
| Parse lại nhiều file không đổi | Cache syntax theo bytes/provider; semantic reuse phải gồm dependency/profile fingerprint | T050 |
| AST/type graph toàn repo giữ cùng lúc | Xử lý từng scope có dependency closure; giới hạn workers; provider không stream được phải có scope/resource diagnostic | T050,T053 |
| Vô số refs vượt context | Catalog refs đầy đủ phía máy, chia nhóm/obligations theo task, source quan trọng theo evidence | T051 |
| Sao chép facts giữa nhiều generations tốn đĩa | R1 đo quota/retention; R3 immutable package fact sets khi profiling chứng minh cần | T075,T077 |
| Cursor lặp graph walk từ đầu | Stable ordered ID/keyset cho lists; bounded in-memory result cache keyed snapshot/query; graph continuation có progress test | T025,T051 |

### Hai chế độ độ mới, không đánh tráo bảo đảm

F01 `freshness_mode=checked_workspace` là default cho edit-oriented pack và rename preview: giữ full manifest/hash check S08; query vượt deadline trả unknown. `freshness_mode=analysis_snapshot` là lựa chọn explicit để đọc nhanh source/facts bất biến của generation; `freshness.scope=none`, `status=not_checked`, `readiness=needs_context`, `edit_precondition_validated=false`. Có thể nêu indexed_at và drift hint, không báo live-current dựa vào cached flag.

F02 Các trang của cùng analysis pack không full-scan lại mỗi lượt. `mce4d_validate_pack` kiểm tra live workspace/config/membership trước edit, rồi host kiểm tra expected hashes ngay trước apply. Validation là quan sát tại thời điểm kiểm tra, không là khóa hoặc token cho phép sửa mãi mãi. Profile/policy thay đổi làm token invalid. Historical source phải mang `source_view=indexed_snapshot`, không hiển thị như working copy hiện tại.

F03 Watcher/stat/Merkle hierarchy chỉ tăng tốc phát hiện và tính digest; không chứng minh không có file mới/bytes bị đổi khi tín hiệu bị mất. Không bỏ full validation trong baseline vì “watch không báo”. Optimization bỏ scan chỉ được mở sau chứng minh input closure + mutation source có kiểm soát + full-rebuild equivalence; chưa có chứng minh thì S08 là fallback.

### Partition và invalidation

P01 Logical partition theo build unit/module/package variant, không tùy tiện chia N file. Reverse dependency được lưu; package membership/config/exported signature và unresolved boundary đều là input. Scope có cycle xử lý theo strongly connected component, nếu chưa chứng minh boundary → rebuild closure bảo thủ. File không đổi nhưng kiểu được import đổi vẫn phải được resolve lại.

P02 Cache key gồm source digest, language/provider/version, compiler configuration, dependency exported input fingerprints, policy, interpretation/schema version. Cache syntax không được tái sử dụng làm semantic facts. Negative lookup cache cũng bị invalidation khi thêm file/package/implementation; không cache “không có caller” qua generation.

P03 R1 vẫn SQLite/worktree và atomic active pointer. Không shard vật lý hoặc thêm graph server trước benchmark. Nếu provider phải load dependency closure lớn hơn memory budget, trả `RESOURCE_LIMIT`/partial capability, không chỉ index nửa closure rồi gọi complete. `.tsbuildinfo` hay artifact compiler chỉ được dùng qua adapter được kiểm chứng; không tự đọc format nội bộ không ổn định hoặc ghi vào target [REF19,REF20].

P04 R3 có thể dedup immutable fact sets theo partition; generation manifest tham chiếu các set/version đầy đủ và bridge edges tương ứng. Publish atomically manifest đã validate; reader không trộn partition cũ/mới không tương thích. Refcount/GC xét mọi generation/profile còn giữ. Cross-worktree/global dedup là ngoài baseline vì isolation/policy/recovery phức tạp hơn.

### Context ngân sách lớn và high fanout

B01 `required_for_review` là obligation theo thay đổi, không đồng nghĩa đưa toàn thân mọi caller vào LLM. Rename cơ học: full reference/preview inventory được máy giữ và phân trang; AI cần đúng target/type/conflict/contract evidence. Đổi hành vi/type: caller tiêu thụ semantic khác nhau và failure paths có thể bắt buộc đọc. Không dùng heuristic sampling để âm thầm tuyên bố các consumer đã review.

B02 Nếu mọi nghĩa vụ không vừa context, trả batched review plan: scope IDs, unresolved obligations, constraint boundaries, source hashes và proposed validation; không áp patch. Review các batch phải cùng baseline hoặc được revalidate sau mỗi patch. Aggregate report phân biệt `machine_checked`, `source_reviewed`, `test_evidence`, `not_checked`; engine không tự điền trạng thái của host.

B03 Cache result chỉ lưu facts/rendered items cho cùng gen/profile/policy; không cache freshness như sự thật hiện tại và không cache “AI đã nhớ”. Cache in-memory bounded trước; persistent pack cache cần ADR, privacy/expiry/GC tests. Không giữ DB read transaction suốt cuộc hội thoại. Cursor stale phải lỗi rõ, không tái trỏ live snapshot.

### Scale acceptance, không cam kết vô hạn

| Cohort | Workload định nghĩa | Điều kiện test |
|---|---|---|
| S | <=1.000 source files, <=20 MiB source | Giữ targets S18, đo đủ strict freshness |
| M | khoảng 10.000 files, <=200 MiB, nhiều build units | Báo cold/update/query/RSS/disk/pending; không tự tuyên bố đạt trước đo |
| L | khoảng 100.000 files, tối đa 1 GiB source fixture | Profile/quota tăng explicit; vượt default 512 MiB phải từ chối có lý do nếu chưa cấu hình |
| XL | từ 1.000.000 files hoặc workload vượt L | R3 research gate, có thể benchmark partitions; không là supported baseline |

Cohort là fixture workload, không giới hạn cam kết cho mọi repo cùng số file. Dependency count, fanout và compiler behavior phải báo. Không vừa scan 1% code vừa gọi throughput của whole-repo. T053/G4 kiểm tra memory/error correctness ở M/L; nếu performance chưa đạt giữ supported envelope nhỏ hơn bằng evidence. Không biến threshold số file thành lý do tự triển khai distributed database.

## S28 — Đa ngôn ngữ và bản đồ contract

### Thứ tự hỗ trợ thực tế

| Provider | Release | Fact được ưu tiên | Giới hạn |
|---|---|---|---|
| Go | R1 | Defs/Uses/Selections, scopes, direct/interface/candidate refs | S04/S09 |
| TypeScript/JavaScript/TSX/JSX | R2 ưu tiên đầu | Compiler/language-service symbol graph; imports/reexports/path alias/project references | `any`, dynamic import/runtime framework cần gaps |
| Python | R2, trước nhãn dùng cho AI projects | AST declarations/imports; typed facts khi provider/version/profile được xác minh | Không hứa full binding cho monkey patch/dynamic imports/decorators |
| HTTP/JSON Schema/Protobuf | R2 | Contract declaration, schema field, provider/consumer binding explicit | Không biến spec thành proof handler đã triển khai |
| SQL MySQL trước | R2 | Static SQL/migration/table/column/view/procedure/trigger/event; dialect/engine profile | Dynamic SQL/catalog ngoài source không được tự biết |
| AsyncAPI/message/cache contracts | R2 | Channel/message/send/receive/serialization và code binding có bằng chứng | Wildcard/env/routing/retention/ack cần cấu hình tương ứng |
| Rust/Java/C#/khác | R3 demand gate | Discovery provider từ compiler/LSP thích hợp | Không tự chọn mọi ngôn ngữ hoặc copy extractor sản phẩm tham khảo |

Ngôn ngữ của engine là Go, không giới hạn ngôn ngữ được index. Nhưng đa ngôn ngữ không tự xuất hiện nhờ parse tree hay MCP. Một `LanguageProvider` tối thiểu chỉ tổng quát hóa sau Go và TS thực tế; DTO có source bytes/ranges, profile, capabilities, symbols, relations, diagnostics. Process adapter qua bounded framed JSON có schema, timeout và whitelist; không plugin loader tùy ý.

TS parser/compiler adapter không execute project scripts, transformers/plugins hoặc auto-install dependency; config extends/path references phải resolve trong allowlist. Có thể dùng local Node interpreter được pin; “một binary Go” không được hiểu là TS/Python tooling không có runtime dependency. Python analyzer không import/exec module mục tiêu, chạy notebook, eval annotations hoặc deserialize model. AST chỉ chứng minh cú pháp, không phải toàn bộ runtime binding [REF19,REF21].

### Contract identity và liên kết xuyên tầng

Contract key bắt buộc gồm service/repo scope, environment/profile, protocol/dialect, namespace/server binding không chứa secrets, contract identifier/version và source hash. Hai endpoint cùng path ở hai service hoặc hai topic cùng tên ở hai broker không tự liên kết. Data mapping giữ type/nullability/units/currency/precision khi có khai báo; không tự gán đơn vị từ tên biến.

HTTP: OpenAPI paths/operations/schemas và handler/client binding có nguồn; route prefix/method/server/version/auth policy phải xem xét. `$ref` chỉ đọc local allowlisted; URL ngoài trả unresolved, không fetch. Không giả mọi version OAS tương thích; parser matrix pin minor version và unsupported keyword phải báo [REF22]. WebSocket events cần declaration/subscription/message codec tương ứng, không coi chúng là HTTP handler thường.

Events: AsyncAPI channel address/messages/operations riêng; `send`/`receive` direction phải theo version. Topic wildcard, queue group, stream/consumer, schema version, retry/ack/DLQ/outbox/inbox nếu code/config có bằng chứng. Ack hoặc dedup khai báo không chứng minh exactly-once business processing; link transfer và side effect là hai lớp riêng [REF23,REF24].

SQL: nguồn ưu tiên schema/migrations đã lưu và query literals/ORM mapping được provider hỗ trợ. `CREATE TABLE` không đủ mô tả trigger, view, routine, event; phải giữ những object này trong phạm vi SQL provider hoặc ghi unsupported. MySQL/NDB/InnoDB engine assumptions không được gộp. Snapshot migration source không chứng nhận database production đang cùng version. Không tự chạy migration/introspect DB/EXPLAIN/query trên target [REF25].

Frontend: route/page/component → API client → operation → handler/service links; auth guard, state store, server/client boundary và generated client origins khi có source evidence. React/TSX baseline; Vue/Svelte/Angular và framework-specific conventions chỉ gắn verified sau fixture riêng. CSS/UI style map không ưu tiên hơn API/permission/state correctness.

### Logical schema R2: phải chốt trước code qua T045/T055/T062

| Record | Identity tối thiểu | Invariant |
|---|---|---|
| `domain_entities` | generation_id, entity_id, kind, qualified_key, plane, source_ref | Same-name != same-entity; source_ref có hash/range hoặc approved record version |
| `contract_fields` | entity_id, field_path, declaration/version | Direction-sensitive compatibility; preserve units/precision nếu có |
| `entity_links` | generation_id, link_id, typed endpoints, kind, evidence/site | Endpoint XOR symbol/entity, composite FK hoặc validator transactional tương đương; không dangling refs |
| `state_transitions` | entity_id, transition_id, from/to/guard/effect refs | Declared vs statically inferred vs observed riêng |
| `approved_records` | record_id, version, scope, approval, source_refs, invalidation | Mutable review status không sửa historical source facts |
| `snapshot_comparisons` | left/right immutable scope vector, mapping_version | Không nối symbol đổi vị trí chỉ bằng name |

Đây là logical overview; S43/S44 giữ physical/schema authority. Không tạo EAV/plugin DSL. Mỗi typed payload extension phải có schema+oracle và blueprint revision tại provider seal trước feature, không để implementer tự bổ sung columns/FKs ngoài bản vẽ. UUID/entity kinds bổ sung không được ép vào `symbols.kind=Function` giả. Migration giữ S07/S15. Cross-repo federation dùng member snapshot vector; không giữ long transaction/lock nhiều DB hoặc hứa atomic distributed snapshot. Policy mỗi member được kiểm tra trước tạo cầu nối.

## S29 — Hồ sơ nghiệp vụ: trading, thương mại điện tử và AI

Hồ sơ là read-only context policy và bộ câu hỏi kiểm tra; không là rule engine đặt lệnh, thanh toán, quản trị hàng hay đào tạo model. Không kết luận repo người dùng đã có những thành phần dưới đây. Template chỉ kích hoạt khi task/scope đã xác định; marker từ text đơn lẻ là candidate.

### Máy trạng thái và workflow engine

Cần model definition có bằng chứng gồm: state identity theo machine/scope, initial/final state, event, transition, guard, action/side effect, entry/exit hook, timer/timeout, retry/cancel/compensation; hierarchical hoặc parallel states chỉ khi provider/schema được hỗ trợ. Tên `pending` ở hai machine không được gộp. Một status enum không đủ chứng minh transition graph. Machine definition và machine instance state/runtime observation là entity khác nhau [REF30].

T062 phải chốt supported declarative format/source patterns qua discovery và fixture trước parser; không tự triển khai simulator hoặc SCXML interpreter. Guard expression lưu source/ref và analysis status, không execute/eval. Structural checks trong subset được hỗ trợ có thể phát hiện target state không khai báo, không có initial state, guard ref thiếu; reachability phụ thuộc guard/runtime inputs thì phải báo unknown, không kết luận dead state bằng graph walk đơn giản.

Với yêu cầu đổi một transition, context cần incoming/outgoing transition, event producers/consumers, guard/action source, persistence/status contract, timeout/retry path và tests liên quan. Không được đổi field/status value trong database hoặc message contract chỉ vì tên state nội bộ đổi. Trace một lần chỉ cho biết transition đã quan sát, không chứng minh mọi state/event đều đã được phủ.

### Trading / tài khoản / wallet

Cần tìm khi có evidence: order/trade/position/account/ledger, balance available/locked/pending, fee/commission, price/quantity precision, risk checks, market data, settlement, websocket/API, retry/reconcile và custody/blockchain adapter. Mọi tiền tệ, token decimals, chain/network, atomic unit, rounding và status enum phải lấy từ đúng code/schema/profile; không hardcode số lẻ/giới hạn tiền/độ finality.

Ví dụ review obligations (không phải business rule đã được duyệt): lệnh retry có tạo side effect lặp; idempotency key thuộc owner/scope nào; reserve/release tiền hoặc hàng có transaction boundary nào; partial fill/cancel/timeout/late receipt có đường xử lý nào; ledger invariant lấy từ đâu; reconcile chạy khi crash ở bước nào; permission và audit có ở boundary nào. Mỗi obligation trả `evidence|unknown|not_applicable_with_reason`; `found_lock` không là bằng chứng race-free.

Luồng minh họa: request → validation/auth → order/risk → reserve → execution → ledger/fees → event → projection/UI. Đối với blockchain, broadcast/receipt/confirmation/reorg là các state có nguồn riêng; không suy finality từ tx_hash. Các nhánh lỗi/cancel/rollback/retry phải có khoảng trống được nêu, không chỉ happy path. Engine không kết nối ví/sàn hoặc gửi transaction.

### Thương mại điện tử

Cần tìm khi có evidence: product/variant/SKU, price/tax/promotion, stock/reservation, cart/order/payment, fulfillment/shipping, cancellation/refund/return, seller/tenant/warehouse, invoice/accounting và notification. Phân biệt product stock với reservation/available, trạng thái order với payment, partial refund với full refund, currency/unit với số lượng.

Ví dụ obligations: hai request cạnh tranh cùng SKU; webhook bị gửi lại/đến sai thứ tự; thanh toán thành công nhưng response timeout; hoàn tiền từng phần; giao hàng trước khi event projection đồng bộ; access boundary tenant/shop. Schema và source phải xác định policy oversell, stock decrement timing, retry và compensation; template không tự chọn business rule.

### Module AI / ML / RAG / agent

Cần lập chuỗi metadata khi có nguồn: dataset/schema/split/version → preprocessing/features → training config → model artifact reference → inference/pre/postprocessing → evaluation → serving; hoặc prompt/template → tokenizer/chunker/embedding → retrieval/index schema → reranker → model invocation → tool/output validation → eval/fallback. Pipeline khai báo có thể khác runtime; thể hiện rõ.

Fields ưu tiên: model/provider identifier và revision đã pin, prompt/template hash, input/output schema, embedding dimension/distance metric, tokenizer/version/chunking config, dataset digest và train/validation/test role, metric definition/eval threshold, feature names/order, retry/timeout, approved tool scopes. Unversioned remote model chỉ có observed/requested identifier; không bịa nội dung weights hoặc kiến trúc.

Artifact policy: chỉ đọc source/config/manifest và metadata exports được phép; dataset rows, embeddings bulk, checkpoints/weights, notebook outputs không ingest mặc định. Không `torch.load`, pickle/joblib load, import training module, execute notebook, tải model hay gọi model để “hiểu nó”. Checkpoint không tin cậy có nguy cơ thực thi code qua unpickling; metadata riêng là boundary an toàn hơn nhưng vẫn validate size/path [REF26,REF27].

Thay embedding dimension phải làm nổi nghĩa vụ kiểm tra vector schema/index/query consumers nếu evidence có; đổi tokenizer/feature ordering/prompt schema cần tìm cache invalidation và eval tương ứng. Engine không chứng minh model thông minh hơn, không phát hiện mọi data leakage/bias và không bảo đảm chất lượng ML từ code graph. Theo dõi lineage không phải lưu toàn bộ dữ liệu huấn luyện.

## S30 — Evolution, quyết định và kiến trúc dự kiến/thực tế

E01 `snapshot_diff(left,right)` so sánh hai scope vectors bất biến, cùng provider semantics hoặc báo provider/profile delta riêng. Trả added/removed/changed symbols/relations/contracts, changed constraints và affected known consumers; không đồng nhất diff dòng với diff hành vi. So sánh chỉ khi snapshot còn được giữ; thiếu snapshot → explicit error, không tự checkout hoặc dựng lịch sử Git vô hạn.

E02 `symbol_id` vẫn generation-scoped S06. Cross-generation matching dùng nhiều evidence (qualified declaration, owner, signature/range/content lineage), đánh dấu exact/probable/unmatched; probable không đủ tự rename/migrate/edit. Same-name hoặc Git rename heuristic không là identity proof. Profile/provider change có thể làm graph khác dù code không đổi; báo riêng để tránh false regression.

E03 Decision/invariant record: id/version/scope/plane/statement/approval/source_refs+hashes/invalidation triggers/replaced_by. Chỉ approved record thành constraint; stale source → needs_review. R1 trusted excerpt không tự trở thành approved persistent memory. Human/user authority được giữ; không tự “học” output AI thành chân lý.

E04 Intended architecture có thể mô tả module ownership, allowed dependency direction, tenant/data boundary, contract owner và forbidden write paths. Checks chạy trên supported observed evidence và quy tắc được duyệt: mỗi violation có witness path/source, severity category + rationale, exemption ID/expiry khi có. Incomplete graph → check incomplete, không PASS toàn kiến trúc. Không tạo điểm số risk phần trăm không có calibration.

E05 `implemented`, `planned`, `observed_in_run`, `unsupported`, `not_seen` tách riêng; một intended component không thấy mapping không đồng nghĩa chắc chắn chưa tồn tại. T067 tạo drift report trong scope; không tự refactor, chuyển module, thay schema hoặc viết business requirement.

## S31 — Runtime/test evidence và boundary của AI host

RT01 R3 chỉ import file export local được user cho phép. Explorer U3 tái dùng importer này; SDK trong preview chỉ xuất metadata approved theo S36–S38, không biến core thành collector/target launcher. Runtime trace phải kèm application/service identity, deployed revision/build digest nếu có, environment không secrets, time window, sampling/collector metadata và instrumentation version. Không map trace production lên HEAD mới chỉ vì cùng tên function. Thiếu revision mapping → unbound runtime observation, không merge vào static exact graph.

RT02 OpenTelemetry spans/links có thể hỗ trợ theo dõi chuỗi asynchronous; một trace là bằng chứng một execution đã quan sát, không là danh sách mọi execution. Các lần retry, duplicate delivery, batch fan-in/out có quan hệ riêng; order thời gian không tự là data dependency. Import sai định dạng/PII/payload quá lớn bị từ chối/che trước persistence. Không bật agent instrumentation hoặc kết nối collector/prod tự động [REF28].

TE01 Test evidence record: test ID/file, source revision/digest, test environment/profile, command supplied by host, result, coverage scope, artifact hash, time. Static TEST_REF != runtime covered != assertion checked invariant. Coverage hit không chứng minh behavior đúng; test pass thuộc phiên bản cũ là historical.

TE02 Suggested test selection là danh sách có rationale/coverage gaps, không quyền bỏ regression suite. Khi thay schema/global config/shared core hoặc evidence không đủ, gợi ý broaden/full suite của repo nếu đã biết command. Engine không chạy target tests. Host permission/execution/reporting tách riêng.

ED01 Unsaved buffer bridge chỉ R3: editor session ID/version, base disk hash, buffer bytes, scope và lifecycle explicit; snapshots buffer không lẫn disk snapshot. Model/analyzer không được tự ghi buffer ra file. Multi-editor divergence, save/reload/delete, secret policy, UTF-16 conversions và cache invalidation phải có test trước bật capability.

IR01 Optional semantic search R3 chỉ hỗ trợ candidate discovery. Bằng chứng symbol/contract vẫn cần resolver/source. Không dùng vector similarity tạo edge exact hoặc bỏ lexical hit. Model/tokenizer/license/version/index dimensions được pin; embeddings cũng có thể chứa thông tin nhạy cảm, subject to same policy. Không auto network/LLM spend. Mặc định exact/FTS + vocabulary + graph đủ đường fallback.

DP01 Deployment/config map R3 đọc static manifests/config schema được phép: service/config/feature flag/version/port/permissions boundary, manifest source vs deployed state riêng. Không đọc secret values để biết variable name; không chạy Terraform providers/Helm plugins/Kubernetes commands/Docker build. Entry deployment evidence chưa có → environment unknown; không bịa production inventory.

## S32 — Hợp đồng extension và phân chia release

### Canonical contract routing

S43 hợp nhất former S11/S32/S38 DTO fields: overview/plan/selectors/topic modes và analysis_snapshot/checked_workspace có cùng requests/schema. Core sáu tools không đổi. View flow/state/contract/change/architecture/ai_lineage và observed chỉ expose sau respective capability/task/fixtures. Schema acceptance không đồng nghĩa implementation đã tồn tại. T026 conformance không tự bổ sung fields sau implementation.

### Release applicability

| Release | Task set | Gate/điều kiện | Case set |
|---|---|---|---|
| R1 | T001–T042 + T049–T054 + T105–T108 (52 tasks) | G0–G5; nền Go/local safety + hierarchy/scale baseline; không Go-only universal claim | C001–C054 |
| R2 | T043–T048 + T055–T068 (20 tasks) | Sau G5 và mở scope; web/polyglot/contracts/domain/AI metadata; T048 kết luận matrix | C055–C069 + regression R1 |
| R3 | T069–T080 (12 tasks) | Theo capability gate sau R2; runtime/tests/editor/search/scale extensions, không block R1/R2 | C070–C080 + regression áp dụng |
| U0 | T081–T084 (4 tasks) | GU0 sau G5: graph/source UI thực | C081–C083 |
| U1 | T085–T094 (10 tasks) | GU1 sau GU0 + T044/T056/T047: React preview↔graph | C084–C098 |
| U2 | T095–T100 (6 tasks) | GU2 sau GU1 + contract/SQL/event/group tasks: frontend↔backend flow | C099–C106 |
| U3 | T101–T104 (4 tasks) | GU3 optional sau GU2 + T069/T070: observed run | C107–C112 |

R2 là roadmap cần thiết cho phạm vi web/trading/AI đa dạng, không bị gọi “hoàn thiện” khi chỉ R1 Go đạt. R3 vẫn tùy nhu cầu; các addon có thể được nghiệm thu riêng, T080 chỉ DONE sau các dependencies của nó; addon chưa chọn giữ DEFERRED và không gọi cả R3 hoàn tất. Có thể báo capability đã nghiệm thu riêng bằng evidence mà không đóng T080. Scope chưa mở giữ DEFERRED; tự động làm hết task chỉ vì còn context là vi phạm S00.

## S33 — Quyết định tối ưu và giới hạn lời hứa

### Phân tích thiết kế của bản 1.1.0

| Nhận định | Quyết định | Trade-off thực tế |
|---|---|---|
| Go-only không đủ web/AI đa dạng | Giữ R1 core nhỏ; làm TS/JS + Python và contracts ở R2 | Có utility sớm nhưng personal-use đa stack cần chờ capability qua test |
| Graph hàm/file không đủ lỗi xuyên service/state | D2 contracts + D3 approved constraints/failure obligations | Phải khai báo gaps/manual links khi không có evidence |
| Snapshot cũ có thể làm AI tự tin sai | Immutable analysis mode tách live validation | Giảm repeated I/O khi tìm hiểu, vẫn tốn strict validation trước edit |
| Graph lớn không nên gửi hết model | Hierarchy + task obligations + item paging | Có nhiệm vụ cần nhiều batch; không bảo đảm số token cố định |
| Distributed infra làm tăng bảo trì cá nhân | Modular monolith, SQLite/worktree, provider adapter bounded | Một build unit rất lớn vẫn có giới hạn RAM; benchmark trước đổi storage |
| Dự án mới chỉ có ý tưởng/spec | Intended plane riêng, planning context có provenance | Không thay người dùng định nghĩa business rules |
| Decision memory có thể lỗi thời | Approved/source-bound/invalidation, temporal diff | Có bước review; không tự học vô điều kiện |
| Cần tự dùng engine phát triển engine | Pinned previous binary + independent oracle + fallback | Không có self-proof hoặc bootstrap circular dependency |

Không dùng số lượng node/edge/star/tool như quality score. Nghiệm thu theo missing-critical-dependency rate, false edits, freshness/source match, task success, tokens/accepted task, resource envelope và reproduce được.

Không triển khai ngay: 3D/4D visualization, graph server cluster, auto-fix/auto-trade, executable rules DSL, hàng chục provider scaffold, model training, embeddings mặc định, telemetry gửi cloud, scan production databases, GPU/weights inspection. GUI 2D Code↔UI Explorer được mở scope ở S34–S40, không thuộc danh sách trì hoãn này. Những mục này không làm core hiểu đúng code hơn nếu chưa giải quyết evidence/provider/freshness.

Tài liệu này vẫn là một nguồn đặc tả; thực thi bằng section/task routing. Thêm tính năng phải nối requirement→task→case→evidence; phần mở rộng không biến thành nhiều PRD/roadmap mâu thuẫn. `maps-context-engine-4d` là công cụ trợ giúp phát triển, không bảo chứng mọi hệ thống tài chính, thương mại hoặc AI đều đúng chỉ vì được lập bản đồ.

## S34 — Code↔UI Explorer: mục tiêu và hợp đồng tương tác

### UX-01 — Bố cục chuẩn, không mơ hồ trái/phải

Yêu cầu người dùng: graph nằm BÊN TRÁI; BÊN PHẢI hiển thị frontend preview khi chọn frontend, hoặc sơ đồ luồng khi chọn backend. Click nút/element hoặc chọn block/component ở preview PHẢI đồng bộ selection đến graph TRÁI. Chọn node graph TRÁI phải đồng bộ về preview/flow PHẢI. Cụm “graph bên phải” trong một câu của yêu cầu không thay bố cục tổng thể; dùng bố cục trái/phải này thống nhất.

Đây là Code↔UI Explorer cho người lập trình và AI context, không là công cụ sinh giao diện từ source summary. Cùng core facts S06–S11/S28; graph trái và flow phải là hai projection của cùng evidence, không hai nguồn chân lý. Scope GUI đã được người dùng yêu cầu; không mặc định đã có implementation.

Layout desktop: toolbar workspace/profile/generation + trạng thái map; pane trái mặc định 45%, pane phải 55%, splitter điều chỉnh 30–70% nếu viewport đủ rộng. Inspector/source drawer nằm trong vùng phải hoặc dưới pane, không chuyển graph sang phải. Trên viewport hẹp có thể switch pane giữ selection; desktop là baseline. Dark theme là baseline của Explorer; không đổi theme/CSS của app preview. Các tỉ lệ là default thiết kế, không được giữ cứng làm source/toolbar bị cắt.

Pane trái: hierarchy repository/module/frontend/backend/database/contracts; search/filter/evidence legend; focus neighborhood; expand/collapse, fit current selection, back/forward. Pane phải có modes `source`, `frontend_preview`, `backend_flow`; có tab source/evidence và static/observed switch khi supported. `AUTO` theo node loại; người dùng có thể pin mode rồi đổi selection; pin không được bỏ cảnh báo mapping khác scope.

### UX-02 — Ma trận selection hai chiều bắt buộc

| Người dùng thực hiện | Kết quả bắt buộc | Không được làm |
|---|---|---|
| Hover trong Inspect trên button/element preview | Highlight outline + tag/type/anchor status, không commit selection | Không gọi model hoặc tải lại graph mỗi pixel |
| Click button/element preview trong Inspect | Chọn runtime instance + element source-site; focus node graph trái; kèm owning component/event handler nếu có evidence | Không kích hoạt onClick/onSubmit/business API |
| Chọn block/component | Cho chọn ancestor boundary qua breadcrumb; nhóm các DOM roots đã đăng ký; graph chọn component definition/render-site tương ứng | Không coi DOM parent mặc định là React owner |
| Click frontend graph node | Resolve live instances trong session; highlight instance khi unique; nếu nhiều thì chooser/group highlight có count | Không tự gọi click, mở modal, đổi route để làm component xuất hiện |
| Click backend/operation graph node | Giữ graph trái; pane phải đổi `backend_flow` rooted tại operation hoặc caller được chọn | Không giữ một preview không liên quan mà giấu việc mode chưa đổi |
| Click bước flow phải | Select canonical graph entity/node trái + mở source evidence của bước | Không tạo node source mới để khớp layout |
| Click bảng DB | Flow cho operations/transaction boundary và upstream consumers có bằng chứng; frontend candidates danh mục theo scope | Không đọc dữ liệu bảng hoặc tự truy vấn DB |
| Mở source | Hiển thị source của indexed snapshot + hash/range; clipboard locator hoặc editor link explicit có kiểm tra | Không shell arbitrary path hoặc hiển thị source hiện tại dưới hash cũ |
| Back/forward | Khôi phục selection/view/filter; live preview target chỉ khôi phục nếu session còn valid | Không replay business actions |

Không đồng nhất `selected` (commit intent) với `hovered` (transient). Block selection không bắt buộc rectangle lasso UI lần đầu: pick element + ancestor chooser là baseline đơn giản và xác định. Lasso sau này phải khai báo multi-select union, không đoán một logical component duy nhất.

Khi đổi pane phải sang backend_flow, preview đã approved có thể giữ tối đa một iframe mounted để bảo toàn session, nhưng phải disarm Interact, duy trì shield và báo rõ `preview_running_hidden`; target có thể vẫn polling. Không gọi việc ẩn iframe là pause runtime. Người dùng có nút Detach rõ, hoặc policy chọn detach khi ẩn; không tự khởi động lại app/route khi quay về preview.

Frontend component definition có thể dùng ở nhiều page. Node hàm không render DOM có thể chỉ trả source/handler link; không ép mọi symbol có hình trên màn hình. Khi nhiều matching instances, UI giữ explicit instance đã chọn và hiển thị count, không tự lấy first DOM match. Chọn code node không cấp quyền điều khiển UI nghiệp vụ.

### UX-03 — Trạng thái UI và giới hạn

`preview_status = detached | awaiting_approval | loading | pairing | ready | stale | blocked | unsupported | disconnected | error`.
`mapping_status = exact_in_supported_scope | candidate | ambiguous | ancestor_fallback | unmapped | stale | unsupported`.
`instance_status = visible | mounted_offscreen | hidden | not_rendered | detached | unknown`.

Không đánh đồng not_rendered với missing code. Route hiện tại, auth role, responsive breakpoint, feature flags, virtualization hoặc conditional rendering có thể chưa hiển thị component; metadata chỉ giữ những điều đã khai báo/được phép. Nêu lựa chọn “mở route được duyệt” riêng, không navigation trong handler selection. Việc cuộn cũng có thể kích hoạt lazy load; auto-scroll mặc định tắt khi preview safety chưa chứng minh, cho nút `Locate on preview` explicit.

Preview chưa chạy/không có SDK/không matching build → status cụ thể + source/static graph hữu ích. Không dựng trang giống target bằng dữ liệu demo hoặc screenshot để giả live mapping. Test fixtures được phép nhưng phải index source fixture thật; render preview từ build của fixture đó; label `synthetic_fixture`, không dùng graph hand-written làm integration oracle.

Accessibility baseline: splitter focusable có keyboard controls/value; graph có tree/list representation và source links truy cập bằng bàn phím; focus không nhảy giữa pane khi hover; Enter chọn node, Escape thoát pick/clear theo scope; reduced-motion tắt pan/zoom animation. Badges/line patterns/text thể hiện evidence, không chỉ màu. Không tuyên bố WCAG conformance khi chưa test [REF46].

## S35 — Mapping frontend: build anchor, source graph và DOM instance

### MAP-01 — Các identity không được nhập làm một

| Identity | Scope và nguồn | Invariant |
|---|---|---|
| `symbol_ref` | worktree/profile/generation/symbol_id của core | Definition theo ngữ nghĩa source; không chứa DOM instance |
| `source_anchor_id` | hash versioned từ original path/content_hash/byte_range/anchor_kind/provider; nằm trong build manifest | Anchor thay khi nội dung/vị trí/provider không còn tương đương; không đoán ổn định vĩnh viễn |
| `anchor_kind` | element_site, component_definition, component_render_site, event_binding_site | JSX sử dụng `<Button>` khác definition Button và khác handler được truyền |
| `build_map_id` | Producer manifest digest theo original source hashes/compiler/provider; KHÔNG generation/symbol IDs | Same source artifact không đổi chỉ vì index mới; loaded-module coverage explicit |
| `binding_id` | Engine kết nối build_map với published generation/profile/policy sau hash/range validation | Immutable binding riêng; source mismatch không bind |
| `preview_session_id` | UUID + approved origin + worktree allowlist của một lần attach | Không tái dùng qua unapproved navigation/origin hoặc tab khác |
| `document_epoch` / `hmr_epoch` | monotonic/session-local metadata do cooperative SDK cung cấp, engine verify bindings | Epoch cũ không ghi đè epoch mới; không là bằng chứng source mới tự cập nhật index |
| `instance_id` | UUID/counter random-session trong WeakMap/registry của DOM element hoặc component boundary | Ephemeral, không dùng props/customer/order IDs; 100 instances có 100 IDs |
| `selection_id` | Explorer session + monotonic sequence và origin of selection | Một hành động commit có một transaction; chống echo/out-of-order |

BuildManifest ghi original path/hash/range và owner anchors; engine tạo SourceRef/component/event candidates lúc bind. Producer không chứa engine generation/symbol claims. DOM SDK chỉ báo anchor/instance/rect, không được tự khẳng định symbol trong repository khác. Resolver engine đối chiếu source hash/profile/build map với generation trước khi bind. Một anchor có thể trỏ nhiều candidate handlers, một definition có nhiều render sites và nhiều instances; tất cả là one-to-many/many-to-many có loại quan hệ.

### MAP-02 — Các nguồn bằng chứng và quyền suy luận

`evidence_origin = static_analyzer | build_instrumentation | runtime_observation | manual_approved | ai_proposal` bổ sung metadata cho evidence_kind của core; không thay `type_resolved` bằng cùng một mức “confidence”. `resolution_status` riêng với origin. Manual approved có giá trị điều hướng nhưng không trở thành compiler proof hoặc observed execution; ai_proposal không auto approve/không tham gia exact impact.

Link display phải nêu `relation_kind`, source/hash/range, scope vector, build/session khi có, evidence origin/status, limitation và provenance chain. Đường xuyên tầng có một cạnh candidate/unresolved phải giữ gap tương ứng; không lấy average confidence làm exact path. Build map chứng minh nguồn của phần tử được instrument trong phạm vi đã test, không chứng minh runtime handler hoặc DB persistence.

CSS selector, text label, class name, filename giống nhau và vị trí ảnh chỉ là lookup hints. Không dùng screenshot/LLM vision làm identity mặc định. Source maps mô tả vị trí generated code về original source; source maps riêng không giải quyết DOM instance→logical component→handler. Phải thêm build/SDK binding [REF31,REF32].

### MAP-03 — Instrumentation MVP React+Vite

T085 xây adapter phát triển riêng của MCE4D dựa official plugin/compiler interfaces được pin. Chỉ chạy sau người dùng chủ động cấu hình development/test build. Không copy source/extractor từ sản phẩm tham khảo. Không chạy config/build plugins của target từ indexer để “discover”; build target là tiến trình người dùng chạy đã được cho phép.

Pipeline: parse TSX/JSX original bằng provider hợp lệ → tạo anchor cho native DOM JSX sites và render/event declarations → transform development artifacts để gắn opaque `data-mce4d-anchor` trên host elements được hỗ trợ → export build manifest → SDK register live instances. Mapping private path/source nằm phía engine hoặc artifact được phép; DOM attrs chỉ opaque ID, không absolute path/raw source.

Không chèn DOM wrappers làm thay layout. Không wrap toàn handler để đổi `this`, args, equality/ref, hook order hoặc error behavior. Baseline xác định event-binding từ source/static symbols, không tự thực thi handler. Custom component không forward attributes/ref được coi là boundary; không ép prop forwarding làm thay contract. Instrument host JSX trong source component nội bộ để lấy definition/element site; muốn map callsite/owner chain chính xác qua HOC/render props cần explicit SDK boundary hoặc adapter được test. Không dùng private React Fiber fields, DevTools private hook hoặc duyệt `Children` để giả một ownership tree đầy đủ [REF37,REF38].

SDK API do dự án cần định nghĩa, không phải thư viện upstream đã có: `registerBoundary` nhận opaque anchor do build adapter tạo, DOM handles (một hoặc nhiều), parent-instance nếu caller biết; registry không tự claim engine binding; trả unregister callback. Ordinary host element instances có thể discovery lazy từ opaque attrs. Registry dùng WeakMap + cleanup/delta; không lưu raw props/React key/DOM text/input value/cookie/token. Explicit boundary là opt-in source change do người dùng/AI host áp dụng theo task riêng; engine không tự patch target.

Vite dev tải module lazily: manifest có thể chỉ bao phủ transformed modules, phải có `module_coverage` và source hashes cho các anchor được trả; chưa load module không có runtime instance. Thêm module vào manifest tạo build-map revision/epoch mới nhưng không buộc re-index source unchanged; resolver rebind chỉ phần khớp generation bằng evidence. Source map từ nhiều transform phải compose tới original bytes; unresolved/missing map không được đoán offset. Transform order/compiler version là profile đã pin.

Development guard phải kiểm tra effective build mode + explicit inspector flag; env variable tên dev nhưng build release vẫn phải strip SDK/attrs/private manifest. C095 là case bắt buộc: so source/production assets/behavior giữa bản instrument và baseline; không tuyên bố transformation semantics tương đương chỉ từ compile pass. SSR/RSC/Next.js/Vue/Svelte/Angular không kế thừa tự động compatibility của Vite React CSR; ở MVP trả unsupported hoặc static source-only.

### MAP-04 — Picker và đồng bộ selection

Mặc định Inspect dùng transparent input shield thuộc Explorer nằm trên iframe. Click không đi vào DOM của target. Shield gửi tọa độ đã quy đổi viewport cho SDK; SDK hit-test bằng DOM APIs trong document của chính nó rồi trả anchor/instance + candidate ancestor boundaries. Không dispatch `MouseEvent`, `PointerEvent`, `KeyboardEvent`, gọi `.click()` hoặc `.submit()` trên target. Parent không đọc DOM cross-origin trực tiếp [REF33,REF34,REF36].

Origin → flow của một selection:
1. Capture click/keyboard trong shield; tạo `selection_id`, source=`preview`; tách hover không commit.
2. `PICK_AT` qua bridge; SDK trả `PICK_RESULT` với session/build/epoch/request ID, instance/anchor, rects và mapping status.
3. Explorer bỏ reply cũ/out-of-order; gọi engine resolve anchor; engine kiểm tra policy/profile/source/generation.
4. Unique valid mapping → set selection canonical và focus node graph trái; ambiguity → chooser với evidence, không chọn tùy ý.
5. Các pane subscribe selection store; render không phát một selection event mới. Echo ack chứa original selection ID, không tái phát click.
6. Graph-origin selection → resolve instances trong SDK, `HIGHLIGHT` outlines; không truyền hành vi nghiệp vụ. Preview reply chỉ ack/highlight result.

Selection state S45 chứa selection_seq/session+scope+ref/instance/occurrence; fields wire của messages ở S43, không tự serialize một object tùy ý. Cùng state machine sử dụng cho graph/preview/flow/source. `occurrence_ref` giữ step/callsite hiện tại khi một symbol xuất hiện nhiều lần trong flow. Flow-origin selection chỉ highlight graph/source, không re-root/rebuild flow. Graph-origin click chỉ re-root khi người dùng chọn operation mới hoặc lệnh explicit; nếu node đã có trong flow hiện tại thì chọn đúng occurrence hoặc đưa chooser, không tự reset camera/root. `AbortController`/equivalent cancel queries cũ; server result cần request ID và generation; stale reply không repaint lựa chọn hiện tại.

Hit-test/outline cần xử lý scroll container, resize, CSS transforms được hỗ trợ, zoom, device pixel ratio và viewport offset. Dùng CSS pixels trên wire; pixel conversion chỉ adapter; range code vẫn bytes S06. Open shadow roots chỉ khi capability có test; closed shadow, cross-origin nested frame, canvas/WebGL/SVG custom widget không map arbitrary subparts bằng coordinate guessing. Trả boundary/ancestor_fallback có tên rõ. Multiple DOM roots cần nhiều rectangles, không lấy bounding-box bao trùm rồi giả toàn vùng thuộc component.

### MAP-05 — HMR, lịch sử và mapping sửa tay

HMR đổi source hash hoặc adapter/build config → build_map epoch mới; giữ graph generation bất biến. Session `stale` đến khi source/build/generation mapping khớp. UI có thể tiếp tục xem historical source riêng, không dùng vị trí mới dưới ID cũ. SDK HMR phải dispose listeners/observers và register lại; route navigation đổi document/instance state, không ép rebuild code graph khi source unchanged.

Manual mapping nhận `target_anchor_or_selector`, typed destination, source_refs+hashes, reason, actor, revision, status proposed/approved/superseded/revoked/needs_review. `manual_approved` chỉ do thao tác explicit có quyền; target content/build thay phải revalidate. Không cho kéo dây graph rồi sinh CALLS exact; dây tự nối là annotation plane. Thiếu nguồn static hoặc runtime giữ nguyên gap, dù có manual hint.

## S36 — Backend flow và cầu nối full-stack

### FLOW-01 — Hai projection, ba ngữ nghĩa

Graph trái là bản đồ quan hệ; flow phải trình bày các bước/phân nhánh gắn root operation được chọn. Không biến layout graph thành bằng chứng thứ tự thực thi.

`flow_mode = static | observed`; manual/intended có filter riêng, không nhúng mặc định vào static exact flow. `order_semantics = source_sequence | control_flow_supported | call_relationship_only | causal_trace | unknown`. Static caller→callee không đủ chứng minh tất cả các callees chạy theo thứ tự hiển thị. Một flow có thể chỉ là call/dependency view và phải gắn label đó.

Khi chọn handler không biết entry operation: hiển thị caller/route candidates và source; không bịa API. Một handler có nhiều routes → chooser. Chọn service/table cũng có thể có nhiều upstream roots; scope/kết quả phân trang không bị gọi đầy đủ khi đang truncated.

### FLOW-02 — Nội dung backend pane

Trong supported subset: `HTTP method + route/service/profile` → middleware/auth/validation khi có binding nguồn → handler → service/direct calls → data-access operation → table/view/procedure/cache/event boundary → response/known consumer. Mỗi step có source_ref, graph_ref(s), evidence, description deterministic và expand_ref. Không dùng LLM tự sinh đường happy-path cho node thiếu source.

Branches/guards/early returns/loop/cancellation/retry/compensation chỉ biểu diễn khi analyzer chứng minh pattern hoặc có approved definition. Direct static calls Go phục vụ baseline; deep interprocedural control/data flow là capability khác, không tự có chỉ vì R1 lưu CALLS. `go func`/async task không tự nối serial với câu lệnh tiếp theo. Recursive/cyclic flow collapse theo explicit container có expand, không xóa cycle để layout đẹp.

MVP T095: Go `net/http` đăng ký handler theo pattern/literal được pin với Go profile và tested parser; routes động, third-party router/framework chưa có adapter giữ unresolved. Không coi mọi URL literal là route. Database MVP: `database/sql` calls có literal SQL và source type binding, MySQL dialect profile theo T059; wrappers chỉ xuyên qua được khi resolver chứng minh target. ORM/raw query builder/dynamic SQL phải khai báo capability riêng. Không bắt user đổi backend sang net/http nếu actual repo dùng framework khác; T081/T095 chọn adapter tiếp theo sau khảo sát, giữ status chưa hỗ trợ khi chưa làm.

### FLOW-03 — Nối frontend với backend

Chuỗi minh họa, KHÔNG là graph hardcoded: element-site → component/event binding → frontend API-client callsite → operation contract → route registration/handler → service → query site → table. Có thể component chỉ đổi local state mà không gọi API; không được chèn node backend cho đủ chuỗi.

Match key tối thiểu: member/service ID, protocol, method, normalized route template theo rules của framework, environment/baseURL/proxy namespace, version/operation identity và source refs. `/orders/123` không tự suy ra `/orders/{id}` nếu không có route matcher/contract phù hợp. Query string và record IDs không lưu raw mặc định. Hai services cùng path không merge. Reverse proxy/BFF/gateway và rewrite prefix cần static config/approved mapping hoặc trace; thiếu config hiển thị boundary.

Cùng monorepo có thể nhiều service IDs; nhiều repos dùng T046 declared group + scope vector. T096 tái dùng domain/contract/group services từ T045/T046/T059/T060; chưa cần Python hoặc toàn T063, nhưng không xây một bridge store trùng lặp. T063 mở rộng generic federation sau này phải reuse. Một member stale/denied/missing → path partial; không spill source từ repo ngoài allowlist.

API success frontend không chứng minh mọi data writes backend. Chọn DB table truy ngược ra frontend trả danh mục paths/candidates phân nhóm, không khẳng định một bảng chỉ thuộc một màn hình. Không có dependency UI không có nghĩa table unused; có job/webhook/queue/CLI khác.

### FLOW-04 — Data operations và transaction

DB cards: dialect/engine/schema namespace từ profile; table/column khi chứng minh; operation READ/INSERT/UPDATE/DELETE/CALL; SQL source site, parameterized template/hash hoặc sanitized summary; transaction-group evidence; constraints/index/migration anchors khi có. Không credentials, row data, bind values, SQL literal có PII hoặc production schema lookup.

Tách `declared_operation`, `statically_possible`, `attempt_observed`, `statement_success_observed`, `commit_observed`, `rollback_observed`, `outcome_unknown`. Đây là observation statuses, không fallback boolean `saved=true`. `Tx.Exec` thành công chưa đủ cho transaction commit. Ngay cả commit span thành công cũng chỉ là kết quả client/instrumentation thấy trong run đó, không tự chứng minh business invariant hoặc replication/finality toàn hệ thống [REF44,REF48].

Query chứa nhiều table/joins hoặc trigger/procedure side effects cần parse/evidence thích hợp; thuộc tính table name đơn lẻ của trace không được suy ra tất cả bảng touched. ORM metadata declared table khác actual physical shard/runtime table. Outbox write, message publish, broker delivery, consumer ack và consumer DB commit là các sự kiện khác nhau; không nối như một transaction atomic xuyên hệ thống khi chưa có chứng cứ.

### FLOW-05 — Observed overlay U3, không là điều kiện để Inspect

U1 click Inspect không tạo request nghiệp vụ hoặc trace. U2 hiển thị static possible flow từ code/contracts. U3 dùng explicit Interact run trong isolated test app + imported export theo T069/T070. Không cần chạy app backend để xem static flow; cần instrumentation nếu muốn biết flow của một lần chạy cụ thể.

Runtime data: interaction_id khi explicit context binding có, trace_id/span_id/parent/links, service/build/revision/profile, operation/timestamps/duration/status, sampling/instrumentation gaps. W3C trace context và OTel context propagation cung cấp cách nối các boundary có instrument; chúng không tự gắn tất cả internal functions vào source graph [REF42,REF43,REF47]. Exact span→source cần build manifest/source anchor hoặc symbol mapping được verify; chỉ có span name giống method thì candidate.

Không đặt biến global `lastClickedElement` rồi gán mọi fetch tiếp theo cho nó. Hai thao tác đồng thời, debounce, cached queries, event queues, retries và background polling phá giả định đó. Context explicit ở handler/request adapter/trace boundaries hoặc known instrumentation mới được gọi causal; timestamp/window correlation chỉ `candidate`, không exact. Không wrap mọi callback tùy ý để giữ context nếu chưa kiểm thử semantics.

Chỉ thêm trace headers vào dev destinations được allowlist và user cho phép; preserve compatible context, không overwrite tùy ý `traceparent/tracestate`, không gửi engine token. Cross-origin propagation cần app/server CORS configuration tương ứng; không mở CORS=* để chữa lỗi. Request bodies/cookies/form values/header secrets và raw SQL params không đưa vào export. Runtime span missing phải nhìn thấy, không nối một đường hoàn chỉnh giả. Observed flow có thể kết thúc tại HTTP/DB spans, không có mọi service method.

## S37 — Explorer security và ranh giới execution

### SEC-UI-01 — Bốn quyền tách riêng

| Quyền | Cho phép | Không suy ra |
|---|---|---|
| Read source/index | Core inventory/query trên roots approved | Không chạy target app/test/config scripts |
| Start Explorer | Chạy server/UI của MCE4D trên loopback và đọc index đã được phép | Không mở mạng public, không attach URL bất kỳ |
| Attach Preview | Browser tải app development/test đã chạy, đúng origin/build/session được duyệt | Không được coi runtime read-only; không production credentials/endpoints |
| Interact/Observe | Thao tác thật trong test environment được duyệt riêng và thu metadata opt-in | Không đặt lệnh/giao dịch/thanh toán production; không tự chạy lại action để tìm trace |

Mặc định CLI/MCP/index giữ safety R1. Explorer command không `npm install`, `npm run dev`, Docker compose up, chạy Go server hoặc eval config target. Người dùng/AI host có task và quyền riêng mới cài integration/build/start app. Go core không spawn Node để phục vụ production assets của Explorer; web assets đã build của chính MCE4D được bundle/serve. Node chỉ cần lúc build Explorer/instrument target khi được phép.

### SEC-UI-02 — Inspect chỉ kiểm soát input, không sandbox target

Input shield phải active trước iframe có thể nhận pointer. Inspect giữ focus ở Explorer, target iframe không tham gia tab order và SDK hỗ trợ focus/input guard cho supported cooperative app. Pointer/keyboard/submit không được forward; unsupported input modality phải disable interactive picker thay vì hứa an toàn. Không dựa duy nhất `stopPropagation()` trong target: nó không tự chặn mọi default action/handler; exact prevention do shield + supported SDK + tests [REF49].

Tải trang có thể chạy mount effects, auth refresh, timers, websockets, analytics, hoặc gửi request không cần click. Cuộn/navigate có thể kích hoạt lazy load. `GET` cũng không phải cơ chế sandbox. Bởi vậy UI phải nói rõ “Inspect không kích hoạt action từ lựa chọn; app preview vẫn đang chạy”. Không dùng CSS overlay, monkey-patch fetch hoặc Service Worker làm bảo đảm không có mutation tuyệt đối.

Trước attach yêu cầu dev/test profile với dữ liệu/tài khoản riêng, backend/db/broker/payment/trading endpoints test-only hoặc mock của app, không keys production. Graph vẫn dựa code thật; mock test data phải có nhãn `mock_runtime`, không giả execution backend thật. Khi không thể xác minh môi trường cho phép, không auto-load preview; cho graph/source/static flow. Browser/OS network isolation hoặc app safe-preview mode là cơ chế riêng có evidence; MCE4D không chứng nhận toàn sandbox. Local app vẫn có thể proxy tới prod; loopback URL một mình không chứng minh safety.

Baseline U1 có Inspect và explicit approved navigation để vào trạng thái UI mong muốn; Interact phải có nút arm rõ, badge liên tục, disarm khi reload/origin/build đổi, không bật từ graph selection/MCP. U3 thêm observed context/export, không tự tăng permission tương tác. Source mutation của engine vẫn zero dù target application có thể thay test data trong Interact; đo hai loại side effect riêng.

### SEC-UI-03 — Iframe, origin và message

Explorer assets/API và target preview phải khác origin; parent không đòi DOM access cross-origin. SDK trong iframe dùng `postMessage` với exact targetOrigin; parent verify event.origin, event.source===current iframe.contentWindow, session, nonce, protocol schema, sequence và payload size. MessagePort sau handshake là tùy chọn, không bỏ initial origin verification. Nonce chỉ chống stale/unpaired messages, không chứng minh target app không độc hại. Frame navigation detach session và yêu cầu pairing lại [REF33,REF34].

Iframe không nhận full source graph, engine API bearer, filesystem paths, session bootstrap token hoặc tool quyền ghi. Payload allowed chỉ IDs, rectangles, capability/status của preview và các bounded control messages cho inspect/highlight. Cấm `eval`, arbitrary JS selectors executable, HTML injection, open URL/file command hoặc API-call relay trong bridge. Schema explicit allowlist, rate cap, timeout, sanitized diagnostics.

`frame-ancestors`/X-Frame-Options có thể chặn embed; CORS không cấp quyền iframe DOM. Không reverse proxy hoặc strip CSP/XFO để bypass. User có thể cấu hình dev server của mình allow exact Explorer origin bằng task riêng. External-tab mode chỉ là fallback khi cooperative opener/pairing có thể verify và được cho phép; nếu COOP/opener hoặc browser policy không hỗ trợ thì source/static view, không tự cài browser extension [REF35].

Iframe sandbox dùng quyền tối thiểu sau capability tests; không trình bày `sandbox` attribute như bảo đảm an toàn cho target đã cho scripts/origin. Không ghép target và Explorer cùng origin để “dễ đọc DOM”. Không dùng engine auth cookies vì cookies không cách ly theo port; bearer riêng trong memory của Explorer, không chia qua preview. Auth browser profile/cookies của target thuộc quyền người dùng; tài khoản test biệt lập.

### SEC-UI-04 — Loopback API, secrets và data minimization

Bind explicit 127.0.0.1, port cấp bởi OS; ::1 chỉ khi adapter test. U1/U2 preview default cũng là loopback development origin đã được duyệt; remote staging cần policy/compatibility gate riêng, production preview không thuộc baseline. Không bind 0.0.0.0 hoặc remote URL mặc định. Verify actual Host/Origin allowlist chống cross-site localhost requests/DNS rebinding; không CORS rộng. API đọc source cần bearer opaque, TTL/scoped roots/profile + policy check mỗi request. Browser stores bearer trong memory, không URL/query/localStorage/log. Bootstrap code một lần do local CLI cung cấp trực tiếp cho người dùng, exchange endpoint có Origin/Host/rate/TTL; T082 test implementation, không in credential trong telemetry. UI controls thay mapping/session cần same-origin anti-CSRF validation và explicit action; GET không mutation. Process/browser reload làm bearer expired hoặc yêu cầu re-auth; malware cùng OS user ngoài bảo đảm này.

API không proxy arbitrary URL/fetch source map từ Internet, không đọc arbitrary path, không cung cấp terminal. URL preview input chỉ approved http(s) development origin/path, reject credentials/schemes/file/data/javascript; redaction query/fragment secrets. Attach tải browser document có permission, không là server-side fetch capability. TLS/profile bất thường trả lỗi không bỏ certificate validation.

DOM text, props, input values, auth tokens, cookies, headers, request/response bodies, screenshot, trace payload không thu mặc định. Button labels có thể chứa customer data; UI label lấy từ source metadata được phép, không scrape DOM để lưu. Rect/instance chỉ memory bounded. Source preview và build maps nhạy cảm như repo: auth, Cache-Control no-store cho source/API, no third-party analytics/CDN/runtime script/fonts. Export source/context chỉ khi user yêu cầu và policy cho phép. Logs giữ IDs/counts/error codes, không nguyên messages.

Exact nonce/bind/mode CAS/ack order, bearer/bootstrap/command TTL và fail-closed state theo S43/S45. Không tự chọn implementation khác với contract bằng câu “nonce chỉ là gợi ý”. DOM message là untrusted data, không authority. Parent/SDK validators cùng schema nhưng independent runtime permissions.

## S38 — Kiến trúc, storage và API Explorer

### ARCH-UI-01 — Giữ một engine, thêm adapters

```text
Source repos -> Core indexers/providers -> published source graph/snapshots
Approved target dev build -> MCE4D build map -> validated source-anchor bindings
User-started target dev app <-> Preview SDK (instance IDs/hit-test only)
                                   ^ exact-origin message bridge
Browser Explorer: [Graph left] <-> Selection store <-> [Preview or Flow right]
                 | auth read API / typed user-controlled mutations
Go Explorer adapter -> existing app/query/context/flow services -> same core stores
R3 local trace import -> observed evidence -> optional U3 overlay (not static rewrite)
```

React+TypeScript strict+Vite cho Explorer, không làm target đổi stack. Baseline graph/flow renderer dùng React Flow core `@xyflow/react`; layout `@dagrejs/dagre` cho bounded directed projection, preserve cycle edges/semantics. Core libraries có MIT source tham khảo REF40/REF41; pin đúng artifact/transitive notices ở T081; không copy paid Pro examples dưới giả định cùng license. Không thêm Cytoscape/WebGL renderer/Electron/Tauri hoặc state library thứ hai trước benchmark. Có thể đổi renderer qua ADR nếu viewport tests thất bại; graph business DTO độc lập renderer [REF39–REF41].

Selection store nhỏ bằng React reducer/external-store hợp lệ; normalize node IDs/reference store, tách selection state khỏi full nodes array. ReactFlow state không là graph database. Manual node dragging chỉ đổi layout, không sửa business relation. Layout worker bounded, deterministic input/order, cancel khi scope đổi. Đọc source bằng code drawer read-only cơ bản trước; không nhúng IDE đầy đủ chỉ để đọc 20 dòng. Editor link optional client-open theo local allowlisted scheme + escaped/validated locator; server không spawn arbitrary editor/shell.

Cấu trúc bổ sung (chỉ tạo module khi task cần):
```text
internal/explorer/          # loopback session/auth/routes; adapter, không duplicate analyzers
internal/uimap/             # build anchors, resolver, metadata lifecycle
internal/flowview/          # projections of core static/observed evidence
web/explorer/              # React/TS/Vite AppShell; graph/preview/flow/source features
  src/features/{graph,preview,flow,source}/
  src/state/selection.*
  src/api/                 # generated/validated DTO clients
packages/preview-sdk/      # dev browser bridge/DOM registry; framework neutral base
packages/vite-inspector/   # development-only TSX/JSX transform + manifest producer
packages/react-inspector/  # explicit supported component boundary adapter
 test files colocated; acceptance fixtures under testdata + browser harness
```

Không tạo `docs/UI_SPEC.md` thứ hai; contract/schema files máy đọc/máy sinh được phép nhưng phải trace tới section này. `go build ./cmd/mce4d` core phải chạy được không cài Node/GUI deps. Explorer release asset build riêng, embed hoặc bundled artifact có hash/version; core-only binary trả `EXPLORER_ASSETS_UNAVAILABLE` thay vì tải assets Internet. Các UI tests không tự chạy repo user; fixture app được kiểm soát mới thuộc test harness của MCE4D.

### ARCH-UI-02 — Storage/ownership và transport authority

S44 physical storage/S43 schemas là authority. Producer `ui_build_maps` và `ui_source_anchors` không chứa source generation/symbol claims. `ui_build_bindings` nối map/profile/policy tới published source generation; `ui_anchor_links` giữ nhiều links của binding. Mọi import/binding atomic dưới worktree writer, không sửa static published facts. Source generation GC làm binding expire, không tự rebase.

`records.sqlite` giữ approved records/manual links và sanitized evidence; không cùng lifetime index cache. Browser SDK giữ instance registry/rectangles bằng memory; server `mapping/resolve` trả các MappingMatch (anchor/target/source/evidence) và cursor, **không live DOM instances**. Explorer nối kết quả server với LIST_INSTANCES từ SDK. No server request nào đòi đọc iframe DOM hoặc source từ SDK.

API prefix `/api/explorer/v1`, operation registry đầy đủ ở S43: core graph/source/context, attach/bind/mode/ack/invalidate/detach + POST preview/status, records list/read và manual propose/review. CLI-only administrative entries tuyệt đối không dispatch qua HTTP hoặc MCP. Authenticated actor lấy từ server session, không client actor field. Closed schema/CAS/idempotency và state rules ở S43–S45. Mapping/session controls chỉ explicit user permission, MCP không tự gọi. GET workspace là no-body read.

Pairing: parent CHALLENGE → SDK READY → server preview/bind → parent BOUND → SDK BOUND_ACK. Interact: server ARMING → SDK MODE_ACK → server preview/ack → unshield; revoke/disarm shield-first. Không ready/interact từ timeout, no stale ACK. Bridge payload chỉ DOMAnchorRef opaque IDs/rects/capabilities; typed directions/version/limits S43/S45. Không copy SourceRef/engine bearer vào messages.

Manifest import CLI: `mce4d explorer-map import --worktree-id <id> --manifest <approved-local-file>`; artifacts phải local/approved và output ngoài target tree mặc định. Không server fetch map URL. Start `mce4d explorer --worktree-id <id> --profile-id <id> --listen 127.0.0.1:0`; stop own process/sessions không kill target app. No placeholder execution.

`Use selection as AI context` lấy typed GraphRef resolver đã kiểm tra → core PackRequest{scope:{pinned_vector},selector:{kind:"ref",ref,service_id},task_kind,constraints}. Với source_position dùng đúng member single scope pinned; không bỏ vector rồi resolve active mới. Không gửi full graph/DOM/screenshot, không auto model call, không thêm write MCP. Hover/highlight không dùng LLM. Copy/export source/context chỉ explicit policy permission. Core edit preconditions S12 vẫn áp dụng.

## S39 — Lộ trình khả thi, acceptance và giới hạn hỗ trợ

### PLAN-UI-01 — Ưu tiên theo một lát cắt thật, không dựng demo thay sản phẩm

1. Hoàn thành core R1 G0–G5 theo tracker; không yêu cầu UI để bootstrap engine.
2. U0 T081–T084: graph trái từ index thật, source pane phải, loopback/auth/selection; preview chưa ready phải disabled.
3. Mở R2 subset cần cho U1: T043,T055,T044,T056,T047, không phải toàn Python/ML. Làm U1 T085–T094 cho React+Vite CSR verified; đi đến click button/block phải ↔ node trái với source thật và Inspect action count=0.
4. U2 T095–T100 cùng prerequisites T045,T046,T059,T060,T061: static operation/service/SQL flow và bridge FE→BE có namespace + vectors. Không chờ toàn T063/Python để làm supported pair React↔Go; reuse service để federation sau không duplicate.
5. Sau GU2 có thể dùng Explorer static ở các repo thuộc verified matrix. Tiếp tục R2 đa domain riêng. U3 chỉ khi T069/T070 và runtime permission đủ; không block utility U1/U2 vì chưa có traces.

Số task là theo dõi công việc, không ước lượng thời gian hoặc % độ khó. User request này mở scope GUI nhưng không tự cấp quyền chạy một target repo/app/DB cụ thể. Nếu discovery không có real repo được phép, xây/test fixtures tự tạo trước và label fixture-validated; không hỏi lại quyết định bố cục đã được chốt.

### PLAN-UI-02 — Capability matrix thực tế

| Đối tượng | Baseline | Hành vi khi chưa hỗ trợ |
|---|---|---|
| React + TypeScript + Vite client-rendered, source/build/map có sẵn | U1 target đầu tiên; native elements + explicit block boundaries | Gate T085/T094; không fake support trước tests |
| Shared component nhiều instance; portal/fragment có anchors/boundaries được test | U1 scoped | Thiếu owner/render-site metadata -> candidate/ancestor_fallback |
| Frontend chỉ CSS/text/image không có source hoặc SDK | Hiển thị static source khi có, không interactive mapping | NO_BRIDGE/UNMAPPED; screenshot không thay source |
| Next.js SSR/RSC, Vue/Svelte/Angular, microfrontend | Chưa MVP; source-static subset nếu provider có | Adapter/pin/fixtures riêng, không dùng React Vite transform một cách mù |
| Canvas/WebGL/chart library/private shadow internals | Logical widget boundary explicit nếu SDK biết | Không map mỗi pixel/nút chart về source bằng đoán |
| Backend Go net/http + database/sql literals | U2 first verified subset | Dynamic registration/router/ORM cần adapter hoặc unresolved |
| Python/Java/other backend | Static provider capability đã có hoặc source-only | Không tự suy endpoint/data lineage từ method name |
| App production/minified không metadata, remote source không khớp | Không live Inspect baseline | Không bypass, không request credentials; static/source-bound report |

C082/C084/C086/C087/C088/C089/C090/C095/C099/C102/C103/C106 là critical path, không được bỏ để gọi MVP xong. C095 SSR fixture là negative support/consistency test nếu SSR chưa supported, không buộc triển khai toàn SSR. Bộ integration có ít nhất một repository fixture FE+BE+SQL do dự án tạo, index từ bytes, build frontend từ chính bytes đó; expected bindings lập độc lập. Test có fixture mock backend phải label runtime mock, không giả relation xuất phát từ request thật tới BE.

### PLAN-UI-03 — Performance objectives, chưa đo

Default graph viewport 200 nodes/500 edges; hard per-view cap 500/1500 (cấu hình chỉ tăng sau profiling); backend flow default <=100 steps/300 edges; full source graph giữ ở core, không tải toàn bộ vào browser. Khi chạm cap hiển thị counts/unknown/pagination/gap, không nói không có thêm dependency. Collapsing là projection và expand_ref giữ original edges. Không render 50.000 nodes rồi ẩn bằng CSS.

Hover picker tối đa 15 hit-tests/giây, coalesce theo frame/latest pointer, không backend RPC trên mỗi hover; selection click resolve tách, tối đa một active resolution/request-type mỗi panel, cancel previous. UI cache key scope-vector+build+policy+query; layout key subgraph digest, hover không relayout. DOM registry discovery lazy + bounded MutationObserver/ResizeObserver chỉ active/selected boundary; full document scan mỗi mousemove bị cấm.

Target test machine được ghi rõ: local outline feedback p95 <=100ms; click→matched selected node/source p95 <=500ms sau ready/warm index; graph-only initial slice p95 <=1500ms. Không tính cold app load/type index vào warm metric; report riêng. p95 fail giữ fail hoặc ADR số liệu, không quảng cáo instant. Source graph scale M/L theo S27 độc lập render cap; mới target, chưa measurement.

### PLAN-UI-04 — Verification và release labels

T084/T094/T100/T104 cần build frontend, TypeScript strict check, lint theo config đã pin, unit/component tests, browser E2E (Playwright candidate, pin/license kiểm tra), Go test/vet/build/race nơi hỗ trợ, API/schema contract, inspect input/network/canary tests và CSP/origin adversarial cases. Version/browser/OS/filesystem và artifacts hashes ghi S21. Network test routing có giới hạn service-worker coverage; kiểm soát browser context và ghi rõ policy, không gọi mock interception là OS isolation [REF45].

Đo precision/recall trên expected supported mappings, ambiguity count, not_rendered đúng, stale false match=0 trên suite, side-effect counter trong Inspect=0, source mutation=0 bởi engine, cross-scope disclosure=0, latency/RSS/leaked observer counts. Ghi denominator bao gồm unsupported/failed parse, không loại difficult cases để tăng “coverage”. Full graph business correctness vẫn dùng source/test oracle; UI edge count không là metric chất lượng.

Labels: `U0_GRAPH_SOURCE_VALIDATED`, `U1_REACT_INSPECT_FIXTURE_VALIDATED`, `U1_REACT_INSPECT_REAL_REPO_VALIDATED`, `U2_FULL_STACK_STATIC_VALIDATED`, `U3_OBSERVED_RUN_VALIDATED`, luôn kèm capability/framework/provider/browser/version và evidence scope. Fixture pass không thành user project pass. Hoàn thành core R1/G5 không hoàn thành Explorer; còn missing UI tasks thì không tuyên bố đã có click mapping.

## S40 — Hợp đồng bàn giao cho AI coding Explorer

Đọc S00/S22 và các task ready trong S19; giữ một tài liệu này là source of truth. Hạng mục mới là Code↔UI Explorer, không fork hoặc gọi engine tham khảo. Graph TRÁI; pane PHẢI là target frontend preview hoặc backend flow theo selection. Implement thật cả preview→graph và graph→preview/flow→graph. Click Inspect button/block phải map đúng source anchor và runtime instance nhưng không chạy action nghiệp vụ. Chưa có preview/mapping/provenance thì trả gap, không thay bằng demo UI hay synthetic edges trong đường chạy thực.

Trước UI code phải có shared DTO/snapshot source contract và loopback security; trước frontend picker phải có dev-only build map/SDK được duyệt; trước full-stack flow phải có route/contract/SQL evidence; observed execution cần trace/build binding riêng. Không sửa renderer code để bịa một edge mà core không trả. Không dispatch click để “tìm API mà nút gọi”. Dùng static analysis hoặc explicit test-mode observed run có permission.

MVP theo U0→U1→U2, mở đúng R2 prerequisite subset; các feature chưa verified disabled và hiện lý do. Source remains read-only bởi engine. UI reading/preview permissions không cấp target install/run/test/production write permissions. Sau mỗi task chạy test scope, kiểm tra canary/diff, ghi artifact/version/result ở S21, cập nhật tracker/resume và next ready task; không claim đã code/test khi mới có tài liệu.

Bản hiện tại 1.6.9 gồm 54 sections S00–S53, 130 requirements R001–R130, 108 tasks T001–T108, 192 acceptance cases C001–C192. R1/R2/R3 có 52/20/12 task; Explorer U0/U1/U2/U3 có 4/10/6/4. Trạng thái task hiện hành lấy từ S19/S22; bootstrap/fixture Linux có evidence tại S21, Windows build FAIL theo E002; product tests/benchmark NOT_RUN; document audit lịch sử và giới hạn revision hiện tại ở S48.

V1.6.9 construction routing: đọc đúng owner section theo S52/S50/task và S53 cho batch commit; Explorer T081 seal schema/state/proof trước T082; BuildManifest↔Binding tách S43/S44/S45; phiên làm việc theo S47, không quyền tự diễn giải thêm API.

## S41 — Bản vẽ thi công: quyền quyết định, khóa thiết kế và sai lệch

### BP-01 — Hợp đồng với AI triển khai

Đặc tả v1.6.9 là bản vẽ của sản phẩm cần xây. `DESIGN_BASELINE` nghĩa thiết kế đã được mô tả, KHÔNG nghĩa code/API dependency đã được kiểm chứng. Không có cam kết zero bug/zero rework. Mục tiêu: loại việc làm lại do đoán contract, ghép sai luồng hoặc mở scope; lỗi thật phải sửa trong phạm vi và chạy regression, không giữ lỗi để bảo vệ lời hứa “không quay lại”.

AI thực thi không được tự thay stack, module ownership, schema/wire names, permission, lifecycle, thứ tự đã chốt, provider support hoặc nghiệp vụ target. AI được chọn tên biến local, tách private helper, dùng cấu trúc điều khiển tương đương, thêm test/error context trong task; không coi đó là quyền thiết kế lại hệ thống.

`S19` là tracker mutable DUY NHẤT. S47 chỉ định thứ tự/phiếu thi công, không có trạng thái cạnh tranh. S43 là wire/schema authority; S44 là physical storage authority; S42/S45 là luồng và state authority. S50 là nơi tiếp nhận nguồn policy/mode; S51 là command/storage policy của hoạt động phát triển; S53 là policy commit/checkpoint được yêu cầu, không thêm authority về schema/kiến trúc runtime. S01–S40 giữ mục tiêu, giới hạn và cách dùng; mọi mục đã cập nhật phải dẫn về đúng authority, không để hai định nghĩa cùng tên khác nghĩa. Nếu vẫn tìm thấy mâu thuẫn: ghi discrepancy, chặn đúng task phụ thuộc; không chọn đoạn thuận tiện hoặc “section mới luôn thắng” mà không reconcile.

### BP-02 — Các cổng trước dòng feature code đầu tiên

| Mốc | Cho phép | Bắt buộc trước bước tiếp |
|---|---|---|
| T001: site survey | Read workspace, lưu digest/status và verify docs policies/mode S50; xác định empty/existing; tạo module/private bootstrap tối thiểu chỉ trong assignment implement | Không ghi đè repo cũ, không tạo parser/server/GUI sản phẩm |
| T002: vật tư | Kiểm tra official API/version/license; pin dependency tương thích; preflight command/volume/budget S51 trước bounded smoke tests có quyền | Ledger có version/hash/source/license/tool API result; thiếu mạng/dependency là BLOCKED, không bịa version |
| T003: kiểm bản vẽ | Tạo spec_check stdlib và test validator; đọc task/req/case/plan + policy/mode/claim metadata S50 | Validator phát hiện missing/duplicate/cycle/DONE không evidence; không giả code validator đã tồn tại trước bootstrap |
| T105: model/ownership | Chép contract S43 thành schema và value types có validation; dependency boundary tests | Không cho SQL, HTTP, React state trở thành domain model |
| T106: storage contract proof | Trích SQL S44, chạy migration/positive/negative/rollback fixture trên driver đã pin | Tất cả schema cần cho core đúng FK/state/ownership; chưa chạy trên target user |
| T107: protocol/flow proof | Tạo fake adapters và deterministic reducer tests cho S42–S45; validate requests/responses | Contract được kiểm tra trước implement handler thật; không dùng fake adapter trong runtime |
| T108: design seal G0 | Đối chiếu S41–S49, dependency pins, module DAG, fixtures, source tree revision | Chỉ sau PASS mới bắt đầu feature T005/T004/...; không skip vì muốn có UI nhanh |

Explorer có seal riêng tại T081 trước T082: Node/npm/compiler/React/Vite/React Flow/Dagre/browser pins + manifest/bridge/state schema + threat fixture. R2 contract/provider có seal tại T043/T045/T055/T057; chỉ đóng những capability được định nghĩa. Một version đã chọn không phải quyền thêm framework/plugin mới. Dependency install/build chỉ cho workspace MCE4D/fixture được phép; không cho indexer chạy scripts của target.

### BP-03 — Quyết định đóng và dữ kiện môi trường còn phải đo

| Chủ đề | Thiết kế cố định | Dữ kiện được discovery, không tự đổi kiến trúc |
|---|---|---|
| Runtime/core | Go modular monolith; CLI/MCP độc lập GUI | Exact Go/dependency versions, executable paths, local module path |
| Storage | Catalog + per-worktree index + per-worktree records; SQLite; không server DB/ORM | Driver pragma/FTS/backup/platform support và file permissions |
| Explorer | React/TS strict/Vite; React Flow + Dagre; native CSS/CSS Modules; một reducer selection; read-only code drawer | Versions compatible; browser/OS matrix; measured viewport envelope |
| JS workspace | npm workspaces + package-lock.json; private packages; `npm ci`, không trộn yarn/pnpm lockfiles | Node LTS đang supported và npm bundled compatible tại T081, pin exact |
| Provider TS | Official TypeScript compiler API qua local Node bounded worker, không target plugins | Supported compiler version và tsconfig features thực tế |
| Preview | Cooperative React CSR build mapping, opaque DOM anchors, exact-origin bridge; không Fiber private | Build profile/approved origin/fixture behavior; repo target không phù hợp thì unsupported |
| Backend MVP | Go net/http static supported registration, database/sql literal SQL; MySQL dialect | Actual routing syntax/DB dialect của target; không ép target đổi stack |
| Python baseline | stdlib AST parse-only trong isolated tool worker; no import/exec target | Python version và scope/type-enrichment compatibility; typed enrichment chưa proven thì partial |
| R3 optional | Không auto mở embeddings/distributed index/runtime collection | Chỉ mở qua capability approval, fixture/contract gate mới; không đoán model/library |

Bất kỳ schema record của R2/R3 còn `payload_json` phải có closed typed schema theo kind trước task feature ghi record đó. Đây là kho chứa JSON đã validate, không plugin DSL/EAV tùy ý. Capability chưa có schema+oracle không được expose, và không được làm trễ core vì addon chưa được chọn.

### BP-04 — Discrepancy procedure

Dùng record trong S21: `Dxxx; task; category=spec_conflict|upstream_api|environment|security|missing_requirement; source evidence; affected contracts/tasks; proposed bounded resolution; approval_required; status`. Không sửa contract chỉ để build xanh. Sai implementation nhưng spec rõ: sửa implementation và regression cùng task, không cần xin duyệt lại naming local. Đổi public contract/architecture/permission: chỉ sau revision đặc tả được người dùng/architect chấp thuận. Fact về dependency được xác minh theo gate có thể cập nhật ledger; không gán thành approval của người dùng.

Mode/nguồn docs/command preconditions theo S50/S51 là điều kiện áp dụng, không mặc nhiên chặn mọi task. Nếu một prerequisite không kiểm chứng được, task phụ thuộc giữ BLOCKED; task độc lập chỉ tiếp tục khi schedule/ownership cho phép. Không đặt stub trả success/empty để vượt prerequisite. Không hỏi lại bố cục, tên sản phẩm hoặc quyết định đã có. Không tự điền biến môi trường, business schema hay endpoint người dùng chưa cung cấp.

### BP-05 — Các thiếu sót v1.2.0 được khép lại ở bản này

| Audit | Điểm cần làm rõ/sửa | Quyết định v1.3.0 |
|---|---|---|
| AU01 | Schema/API được chốt sau implementation | T105–T108 foundation; T026 chỉ conformance/wiring |
| AU02 | Registration đi trước physical catalog schema; publish chưa phụ thuộc lock | Physical migrations trước feature; T004 sau T005; T008 sau T010 |
| AU03 | Tổng nghiệm thu có thể trước GC/migration code | T034 sau T035 và fault prerequisites |
| AU04 | Source scope đơn và Explorer vector chưa một contract | ScopeVector + policy/binding/record/group revisions S43 |
| AU05 | Approved records có thể lẫn cache | records.sqlite durable tách index cache; CAS/idempotency/epoch |
| AU06 | Build artifact tự phụ thuộc index generation | BuildManifest content-based; engine Binding độc lập, không producer tự claim symbol |
| AU07 | Server mapping biết DOM instances khi chưa nhận dữ liệu | Server chỉ resolve anchors/source; browser SDK giữ live registry |
| AU08 | Pairing/HMR/mode transition thiếu bước commit/ack | Attach→challenge→ready→bind→bound_ack; arming + fresh MODE_ACK trước bỏ shield |
| AU09 | Budget có thể bỏ qua phần MCP lặp | Delivery mode explicit, serialize whole tool-visible result; compatibility default |
| AU10 | Cache chưa theo đổi approved record/policy/group | Scope vector epochs + optimistic before/after checks; revoke không giả zero in-flight bytes |
| AU11 | Logical schema/invariants thiếu executable blueprint | DDL/schema registry trong cùng file, document-level tests; app validators riêng |
| AU12 | Thứ tự task và đầu vào từng task chưa tuyệt đối | S47 một thứ tự chính xác + read/write/flow cards, gate closures và no fake DONE |

## S42 — Luồng dữ liệu end-to-end và quyền sở hữu

### DF-00 — Mô hình chung

Source target là dữ liệu người dùng; chỉ CLI quản trị explicit mới đổi registration/policy, không sửa source. Core index chỉ sản xuất facts có provenance. Query, MCP và Explorer không gọi index ngầm. Browser là renderer, không analyzer; Preview SDK là observable-instance adapter, không filesystem client. Mọi input qua parse/limits/authorization trước I/O. Mọi response chứa ScopeVector đã dùng hoặc null khi chưa resolve. Build artifact, static source, approved record và runtime observation là bốn nguồn khác nhau, không nhập thành một sự thật.

| Flow | Trigger → thứ tự thực hiện | Writes / điểm công bố | Failure và nơi tiếp tục |
|---|---|---|---|
| DF01 Register/profile | CLI args → safe root identity → read Git metadata sanitized → validate profile/policy → canonical IDs/digests → catalog transaction | Catalog only; COMMIT trả IDs/revision. State dirs chỉ owner-marked, không source | Conflict/path/tool error: no half registration; orphan empty owned dir được recovery, không tự remap root |
| DF02 Index | policy/version → writer OS lock → inventory → deny → capture/hash → manifest → provider closure/type → normalize facts → BUILDING batches → full revalidate → integrity → publish | Index only; active pointer+READY trong cùng transaction. Parse/LLM/network không trong SQL transaction | Error trước commit: previous active nguyên; new generation FAILED/CANCELLED. Ownership lost: worker không publish |
| DF03 Locate/overview | bounded input → authorize → resolve vector/active once → short read snapshot → exact/scope/FTS → evidence/gaps → policy/record epochs check → serialize | Không write source/index; bounded caches memory | Ambiguous/missing/partial giữ trạng thái; không fallback repo/name hoặc bỏ lỗi provider |
| DF04 Context pack | selector→resolve → task obligations → generation facts + approved records at record_epoch → merge source ranges → deterministic rank → meaningful-item budget → cursor | Không durable write cần thiết; pack descriptor MAC riêng mục đích | Required evidence chưa đủ: needs_context+cursor; không trả safe_to_edit. Scope changed: new pack |
| DF05 Expand | verify token version/signature/expiry/purpose → exact scope/epochs → keyset traversal page → source from captured bytes → budget | Không chuyển sang active generation mới | GC → SNAPSHOT_EXPIRED; policy/record changed → explicit invalid. Cursor phải tiến triển hoặc trả limit error |
| DF06 Validate | token scope → policy/record precheck → full inventory/hash/config/profile → compare capture manifest → postcheck revisions → result | Không khóa source; chỉ quan sát kiểm tra | New/untracked/deleted file cũng mismatch; unknown không current. Host check patch hashes lại trước apply |
| DF07 Rename preview | positional selector+hash → DF06 → gopls pinned profile/captured buffers → prepareRename/rename → reject apply/commands → validate all edits/ranges/contracts → full revalidate | No source mutation. gopls scratch/cache chỉ approved state | Không type info/changed source/contract risk: blocked; không global regex fallback. Timeout kill children |
| DF08 Watch | explicit process → bounded stat hints → scheduled inventory/hash reconciliation → enqueue max một index yêu cầu mới cho worktree → DF02 | Chỉ index writer qua same use case; không daemon/global job lock | Event storm coalesce; cancel waits child workers; dropped hints không proof fresh |
| DF09 Manifest import/bind | explicit local artifact → path/secret/schema/digest validation → writer lock → verify anchors and loaded coverage → transaction import entire immutable producer map → resolve against a published source generation → publish binding | Index metadata separate from static facts; manifest không cần generation; binding cần gen/source hashes/policy | Import unchanged idempotent; same ID/different bytes reject. Stale source: imported-but-unbound, no fabricated links |
| DF10 Explorer start | explicit CLI→loopback listen→own embedded assets→one-time bootstrap exchange→scoped in-memory bearer→query app service | Runtime memory only, no target launch, no arbitrary file HTTP | Missing assets/auth/host/origin: reject; core CLI vẫn chạy. Stop server không kill user app |
| DF11 Preview attach/pair | permission profile+read scope → server attach → shield before load → exact-origin CHALLENGE → SDK READY → server bind → BOUND → SDK BOUND_ACK | Session bound state in server; browser readiness only sau matching ack. Metadata identity separate from DOM IDs | Canceled/reload/HMR/policy fail: shield, disarm, stale/detach; no ready-by-timeout fallback |
| DF12 Preview→graph | shield pick → seq/epoch → SDK hit-test opaque anchor+instance → browser stale-filter → mapping/resolve server → source/binding check → canonical selection → graph focus + source drawer | Selection browser memory; no static edges created from click | N>1 chooser; missing source fallback explicit; old reply must not repaint |
| DF13 Graph→preview | user graph selection → source/anchor resolve → LIST_INSTANCES opaque IDs → user instance choice → HIGHLIGHT → ACK | SDK/browser live memory only; server doesn't invent DOM list | Unmounted/hidden return status; no synthetic click/navigation/login. Flow selection never recursively re-roots itself |
| DF14 Static flow | root selector→same graph/contract query→bounded occurrence graph→known branch/call/SQL/transaction metadata→projection | Derived memory cache only, includes vector/record/provider revisions | Unknown route/SQL preserved boundary; layout isn't execution order; no request/database command |
| DF15 Manual mapping/decision | authenticated explicit action → request/schema/source validate → owner writer lock → CAS records transaction → version+head+receipt+epoch commit → invalidate read caches | records.sqlite only. Actor from server principal; client mutation id exact replay | Same id+same principal/hash returns original result; changed body/principal rejects. Stale version rollback and conflict |
| DF16 Observed import | approved local artifact→size/schema/redaction→source build binding or unbound→atomic artifact/items write→observed projection | records.sqlite evidence storage; immutable imported items not static graph rewrite | Secret or malformed data reject before persistence; unmatched revision remains unbound. No replay/execute |
| DF17 Retention/recovery | explicit/owned quota→worktree lock→choose non-active generations→short tx retire/delete dependents→blob orphan GC→checkpoint policy | Index/cache only; never records/catalog/source. Backup records separately | Active/source-bound reader snapshot retains semantics; next page may expire. DELETE doesn't promise forensic erase |
| DF18 Consumer change | export bounded pack→host validates+patches with own permission→host build/tests→record evidence→explicit reindex | Host owns source mutation, not MCP/Explorer | Partial filesystem patch no claim atomic; preserve user's other changes; old pack invalid |

### DF-19 — Catalog / policy / records coherence

Không SQL JOIN/transaction cross database. Một query lấy catalog binding/profile/policy/group revisions, records record_epoch và published generation trong những read transaction ngắn. Sau khi đọc data, kiểm tra lại các revisions có ảnh hưởng trước serialize. Nếu khác: discard response data, retry read tối đa một lần trong deadline, hoặc POLICY_CHANGED/RECORD_CHANGED/PROFILE_MISMATCH. Đây là optimistic stable observation, không atomic distributed snapshot và không thu hồi được bytes đã gửi trước thời điểm revoke.

Pack/query cache key = schema + operation + normalized query + complete scope vector + provider/ranking versions + build binding hoặc artifact nếu có. `record_epoch` thay khi record head thay; proposed/revoked metadata không làm mất lịch sử, effective approved phụ thuộc hash/version hiện tại. Group membership sửa phải tăng group revision cùng transaction; policy/profile head CAS tăng epoch/revision; không chỉ update JSON cùng digest.

Nếu profile hash đổi, không tự giữ old name để giả same profile. policy stricter có thể cấm cả historical blobs: kiểm tra policy hiện tại trước đọc/xuất mọi snapshot. Response bị chặn khi policy không đo được; fail-open chỉ dành hiển thị metadata đã sanitize không cần quyền source và phải ghi unknown.

### DF-20 — Chuẩn mutation, retry, timeout, cancellation

Read retries: tối đa 1 retry sau initial attempt, trong deadline gốc, chỉ cho lỗi transient đã phân loại. Bounded index capture: tối đa 2 file recaptures; snapshot instability retry toàn job tối đa 1. Không retry operation đối với lỗi semantic/authorization. `retryable=true` không tự cấp quyền thực thi.

SQL statement retry không được replay từng nửa transaction. Rollback transaction trước retry toàn use case có idempotency contract. Nếu COMMIT báo lỗi/timeout khiến kết quả chưa chắc chắn: đọc lại theo generation/job/receipt identity sau kiểm tra integrity; không insert duplicate hoặc báo success chỉ từ “không nhận error lần trước”.

Lock order: catalog transaction được đóng trước worktree lock. Một process không giữ hai worktree writer locks đồng thời. `records` mutation và `index` mutation cùng worktree dùng cùng writer lock; SQL tx mỗi lần chỉ một DB. Federation đọc từng scope vector, không distributed writer. UI sessions/hover không giữ lock. Goroutine cancellation phải join/terminate trước release writer ownership; context deadline gốc xuyên suốt provider/store/transport.

Default cancellation settling tối đa 5s cho cooperative worker; child vượt graceful 2s thì terminate/kill process tree trong tổng 5s và log sanitized. Giá trị là blueprint default cần test OS, không bằng chứng đã enforce hard memory/time với arbitrary executable. Không xóa lock file khi owner còn sống hoặc dựa PID/TTL một mình.

## S43 — Contract duy nhất: model, wire schema và registry

### CT-01 — Authority và sinh mã

Block `MCE4D_CONTRACT_SCHEMA_V1` bên dưới là JSON Schema Draft 2020-12 duy nhất cho wire/value records baseline. Block `MCE4D_OPERATION_REGISTRY_V1` ràng buộc request và `data` response. Core DTO không còn thêm “optional fields” nằm rải rác ở S11/S32/S38. Additive extension chưa implement trả UNSUPPORTED_CAPABILITY; không accept rồi trả object rỗng.

Wire schema_version=1 và bridge protocol_version=1 ở đây là baseline CHƯA PHÁT HÀNH. V1.5.0 thay fields so với bản vẽ1.4, không tuyên bố backward-compatible với consumer đã được triển khai. T001 nếu phát hiện client/store phiên bản cũ đã chạy: giữ dữ liệu/consumer, mở compatibility discrepancy và forward version/migration có test trước thay; không sửa baseline1 trực tiếp trong sản phẩm đã phát hành.

T105 trích block vào `contracts/mce4d.v1.schema.json` và `contracts/operations.v1.json`, commit như artifact được phép. `tools/spec_check` so SHA-256 normalized JSON với block; thay generated file một mình là lỗi. Go/TS validators phải implement đúng schema + CT-02; không `as any`, không `Record<string,any>` chứa domain facts. Có thể tạo Go/TS types thủ công theo schema để không thêm codegen dependency, nhưng phải có contract tests đồng thời trên fixtures positive/negative; đó là code thực thi, không tài liệu thứ hai.

Một SourceRef canonical chứa byte offsets/hash; line/column display derive từ captured bytes. ScopeVector authority CT-06: single có đúng một resolved member, group fields null; federated có group/version, resolved members và unavailable_members tách riêng; có thể zero resolved member nhưng không tạo dummy generation. Pre-index status dùng registered_scopes, scope=null. Incarnations từ trusted stores, không client tự cấp. Vector digest theo CT-04, không chứa query time.

Registry operation ràng buộc Response.data, không chỉ chấp nhận bất kỳ member ResponseData. Bootstrap/error chưa có source dùng scope=null, freshness not_checked, coverage unknown, readiness blocked. Không expose SourceRef hoặc registry ngoài authorization trước bootstrap.

### CT-02 — Semantic validation bắt buộc ngoài JSON Schema

| Rule | Kiểm tra |
|---|---|
| SV01 | UTF-8 hợp lệ, không duplicate JSON member ở mọi object; reject trailing JSON, unknown fields, non-finite numbers, size/depth > policy; decode không coercion |
| SV02 | ByteRange: 0<=start<=end<=actual captured length, position nằm trên UTF-8 boundary; SourceRef hash bằng bytes, file thuộc gen/vector/policy; line/end được derive |
| SV03 | ScopeRequest bound default chỉ khi server có đúng một explicit binding. Group selector XOR single selector; không name fallback; single lookup cần profile có thật |
| SV04 | Locate: position XOR query; cursor continuation không được đổi filters/scope/order; defaults mode=symbol, limit=20. Status chỉ verify none/workspace, không returned_files khi chưa có result files |
| SV05 | Pack: default view=symbol cho symbol/position, overview cho workspace/module/doc; entity/flow chỉ capability đã qua gate. Task rename yêu cầu resolved positional/symbol target; ambiguous không auto chọn |
| SV06 | Default checked_workspace cho rename/type/behavior/contract/debug; explore/plan default analysis_snapshot. Analysis data luôn needs_context/not_checked cho edit; validate live không đổi historical graph |
| SV07 | ScopeVector group/revision/member source consistency; policy_epoch/binding_revision/record_epoch hiện tại được so trước/sau. Records mismatch làm new pack, không giấu thay đổi rule |
| SV08 | Response status error/blocked cần error object, readiness blocked; data null trừ partial result được operation contract cho phép. status ok không có error nhưng vẫn được coverage partial+gaps; không ok khi schema invalid |
| SV09 | Pagination: truncated iff còn known/unknown pending hoặc traversal incomplete. Unknown count=null; cursor progress verified; no pending zero khi enumeration chưa hoàn tất |
| SV10 | Graph edges endpoints trong visible node slice hoặc explicit boundary node có evidence. Flow steps occurrence IDs không gộp canonical symbols; FlowEdge không tự sinh từ layout |
| SV11 | DOMAnchorRef chỉ opaque build_map_id/anchor_id. SDK không được gửi SymbolRef/SourceRef/engine generation để tự claim ownership. Bound AnchorRef của engine cần binding_id thực |
| SV12 | BuildManifest digest exclude chính build_map_id; source array unique/sorted, anchor range trong source hash, owner refs cùng manifest, không cyclic owner. Coverage lazy != toàn repo |
| SV13 | Preview URL canonical origin scheme/host/port exact, approved path prefix theo ranh giới segment: `/app` match `/app` hoặc prefix `/app/`, không match `/application`; prefix kết thúc `/` match chính prefix hoặc hậu duệ, `/` match mọi pathname tuyệt đối; so trên browser-canonical pathname và credential/query/fragment policy; no proxy/fetch. localhost/127.0.0.1 không interchangeable silently |
| SV14 | Ready/bind/mode/click results session+document+HMR+revision+nonce/command khớp. Delayed/duplicate/wrong direction messages không tăng quyền hoặc commit selection |
| SV15 | Manual actor từ authenticated principal; expected version CAS; client mutation id bound principal+request_hash; same id different request -> RECORD_CONFLICT |
| SV16 | capability/edge/node/rule kind strings là registry extensibility keys, không quyền nhận giá trị tùy ý: registry closed cho enabled provider. Unknown -> UNSUPPORTED_CAPABILITY, không generic parse |
| SV17 | Instance UUIDs ephemeral, rects finite CSS pixels và bounded; DOM text/props/values/keys/secrets không ở wire. Source read không thành arbitrary path read |
| SV18 | observed flow requires imported sanitized artifact_id và verified/unbound source status; static mode không accept trace như compiler fact. No query success→commit inference |
| SV19 | Budget tính sau envelope/warnings/cursor; no Unicode/JSON/code-range truncation. Constraints enum không được tự bổ sung business rule; explicit doc/record source cho constraint khác |
| SV21 | Evidence origin/kind matrix: static_analyzer dùng syntax/type/text/unresolved và explicit_document khi có tài liệu nguồn rõ ràng; explicit_document cần ít nhất một source_ref, record_ref hoặc artifact_id, và owner xác minh đó là tài liệu thực được phép trước khi dùng; build_instrumentation dùng build_anchored/unresolved; runtime_observation dùng runtime_observed/unresolved; manual_approved dùng manual_declared; ai_proposal dùng proposed. Không đổi origin thành compiler proof. FileRef cho inventory/text-only, hash=null chỉ metadata, không mở source hoặc rename. |
| SV20 | Profile identity hash lấy canonical effective config với tool digests; field `profile_id` không tự nằm trong preimage. Source CaptureManifest snapshot_digest tương tự; UTF-8 length-prefixed canonical encoding S06, arrays sort theo defined order |

**Bridge direction registry (D015):** parent→SDK gồm `CHALLENGE`, `BOUND`, `PICK_AT`, `LIST_INSTANCES`, `HIGHLIGHT`, `CLEAR_HIGHLIGHT`, `REQUEST_SCROLL`, `SET_MODE`, `DISPOSE`, `PING`; SDK→parent gồm `READY`, `BOUND_ACK`, `PICK_RESULT`, `INSTANCES`, `HIGHLIGHT_RESULT`, `SCROLL_RESULT`, `MODE_ACK`, `BUILD_CHANGED`, `INSTANCE_INVALIDATED`, `CAPABILITY_STATUS`, `DISPOSED`, `PONG`; `ERROR` được gửi hai chiều. Direction đúng không thay origin/source/session/nonce/replay/state validation.

GraphRef/Selector chỉ được dùng ở scope đã resolve. Runtime authorization không dựa tool annotation. Positive JSON schema match không bỏ qua SV01–SV21; không tuyên bố schema validation bằng chứng access control.

### CT-03 — Transport, lỗi và budget

HTTP `/api/explorer/v1` dùng bearer memory+Host/Origin checks; GET workspace chỉ nhận query verify=none/workspace, không GET body. Core CLI dùng typed JSON stdin/file; CLI control request schemas CT-07, không ghép shell string; MCP tên sáu tools cố định. Child/provider frame không dùng HTTP: Content-Length ASCII + CRLFCRLF + UTF-8 JSON, maximum 16MiB/frame, 64KiB header, deadline/cancel request ID, binary stdout không log. LSP dùng protocol riêng đã pin, không tái sử dụng provider method names.

`INVALID_ARGUMENT/DUPLICATE_FIELD/UNEXPECTED_FIELD` → HTTP400/CLI2; unauthenticated →401; unauthorized →403; scope/source/record/mode conflict →409; unknown scope→404 sau authorization; expired cursor/snapshot→410; quota/rate→429; timeout→504; internal/storage→500; partial có typed data→HTTP200/CLI6, không giả error thành success. MCP domain tool failure dùng isError đúng SDK; parse/protocol errors dùng đúng JSON-RPC errors của SDK, không tự sinh code tưởng tượng.

Bootstrap/session/record controls mỗi request max16KiB; normal core request max32KiB; UI graph/flow max64KiB. Response byte cap core 24KiB default, maximum128KiB; UI graph/flow maximum1MiB. Auth header/token limit trước parse. Bridge max16KiB, arrays theo schema và rate S39. Nếu scalar source dài vượt cap: descriptor+gap, không cắt chuỗi bừa.

MCP delivery default `mcp_dual` cho structured data + serialized text để compatibility theo official protocol [REF03]. Chỉ dùng `mcp_structured_verified` khi host/version fixture chứng minh consumer đọc structuredContent; `mcp_text` cho host không dùng structured output. JSON budget đo cả phần tool-visible bị lặp, không đo wire framing thành token. `serialized_bytes` fixed point: serialize với 0 → đo → cập nhật → serialize lại đến count không đổi, tối đa8 vòng; không hội tụ -> internal serializer error, không gửi vượt cap. Không exact tokenizer thì token_count/tokenizer_id null; byte cap không là token cap.

`pack_token`/cursor/result_token signed separate purpose strings (pack/list/mapping/rename-result/rename-page), version, full scope vector/incarnations, principal+allowed binding, normalized request hash/order_version, result ID/digest nếu có, last_key/position, issued_at/expires_at; HMAC-SHA256 key random32bytes owner-only. Constant-time signature check, reject unknown key/version/expired/cross-scope; key rotation invalidates old tokens. Token không secret content, nhưng không log. List paging keyset; graph paging deterministic bounded expansion với materialized-result ID/cap/TTL theo CT-05 và actual progress. No cache “model already read”.

### CT-04 — MCE-C14N1: mã hóa byte và recipe hash liên ngôn ngữ (A08)

Đây là codec nội bộ cần hiện thực bằng standard library, không thư viện crypto mới hoặc JSON canonicalization được giả định có sẵn. `H(domain,value) = lowercase_hex(SHA256(b"MCE4D\x00C14N1\x00" || E(domain) || E(value)))`. Domain là UTF-8 string chính xác trong bảng; không lowercase/normalize nội dung tùy ý. E chỉ nhận dữ liệu đã schema/semantic-validate; không hash raw serialized JSON để thay thế. Source `content_hash`/download artifact hash luôn SHA-256 raw bytes, KHÔNG E.

| Giá trị | E(value) chính xác |
|---|---|
| null | byte ASCII `n` |
| false / true | byte ASCII `f` / `t` |
| integer an toàn | ASCII `i` + decimal base10 tối giản + `;`; -0 tương đương0; tuyệt đối <=9007199254740991; không fraction/NaN/Infinity |
| string | ASCII `s` + số byte UTF-8 dạng decimal + `:` + raw UTF-8; empty là `s0:`; không Unicode normalization hoặc EOL conversion |
| array | ASCII `a` + số phần tử decimal + `:` + E từng phần tử theo thứ tự đã normalize riêng cho recipe; không delimiter khác |
| object | ASCII `o` + số member decimal + `:` + E(key),E(value) theo key UTF-8 byte lexicographic tăng; key duy nhất; missing khác key:null |

UTF-8 phải hợp lệ; JS lone surrogate bị từ chối trước Buffer/TextEncoder, Go không thay invalid rune, Python không encode replacement. Identity input chỉ nhận integer token base10, không decimal point/exponent, parse số chính xác trước chuyển sang native JS Number; không float rounding vượt53bit. Producer phải serialize integer đúng kiểu; schema integer có thể chấp nhận1.0 nhưng semantic identity decoder từ chối numeric token đó trước hash. Rectangles/durations float chỉ là dữ liệu hiển thị, không đưa vào identity. Duplicate JSON keys/trailing bytes bị reject TRƯỚC decoder làm mất duplicate. Độ sâu tối đa32, object keys tối đa4096, tổng bytes theo artifact cap; không recursion không giới hạn. E(object) không dựa thứ tự map của Go/JS/Python. Không tự áp generic sort cho mọi array.

| Digest / domain | Exact preimage value và normalize trước E |
|---|---|
| `profile_id` / `profile-v1` | EffectiveProfile sau defaults đã chốt; không field profile_id; build_tags/module_roots/dependency_roots/tsconfig_paths sort raw UTF-8, reject duplicates; compiler/tool paths được fingerprint qua digest, không runtime PID/time |
| `policy_digest` / `policy-v1` | PolicyConfig effective; allowed_roots/dependency_roots/denied_globs sort/reject duplicates; allowed_executables sort(path,sha256); trusted_rule_paths GIỮ thứ tự vì có thể ảnh hưởng rule precedence |
| `snapshot_digest` / `capture-v1` | CaptureManifest bỏ chính snapshot_digest; files sort(path), unique path; dependency_input_digests sort giữ multiplicity; input closure digest đã ràng buộc module/root identity. Không parse_error/type results/created_at |
| `source_anchor_id` / `anchor-v1` | object đúng keys `{path,content_hash,kind,start_byte,end_byte,provider_digest}`; original bytes/range, instrumenter fingerprint chứ không display version |
| `build_map_id` / `build-map-v1` | BuildManifest bỏ build_map_id; sources sort(path), anchors sort(path,range.start_byte,range.end_byte,kind,anchor_id); IDs/path duplicates reject; provider_digest là instrumenter; module_coverage có nghĩa và có trong hash |
| `binding_id` / `binding-v1` | đúng keys `{build_map_id,source_generation_id,profile_id,policy_digest,resolver_digest}`; resolver_digest gồm binary/version/options fingerprint; tự nhiên unique ở DDL cùng tuple |
| `vector_digest` / `scope-v1` | ScopeVector bỏ vector_digest; members sort(service_id null trước string,worktree_id,profile_id,generation_id); unavailable_members sort(service_id null trước,worktree_id null trước,status), nếu cùng cả ba khóa thì sort tiếp theo byte lexicographic của E(toàn MemberUnavailable) không domain prefix; catalog/store incarnations và group_revision giữ đầy đủ |
| `target_ref_digest` / `graph-ref-v1` | Toàn GraphRef typed đúng schema; không display label/layout/instance; used cho mapping keyset |
| `fragment item_id` / `source-fragment-v1` | object `{parent_ref,range}` như CT-05, SourceRef parent đầy đủ và byte range fragment |
| `resolver_digest` / `resolver-v1` | object `{implementation_artifact_hash,semantic_version,options_digest}`; artifact SHA256 thật và options theo closed adapter schema, không Git branch name |
| `runtime_config_digest` / `runtime-config-v1` | effective RuntimeConfig sau merge defaults và resolve max_parse_workers=0; runtime resource config KHÔNG tự trở thành semantic profile |
| `permission_digest` / `preview-permission-v1` | PreviewPermissionProfile đầy đủ version/attestation/expiry; source_worktree_ids/allowed_dev_destinations sort unique; timestamp ở đây là nội dung quyền, không thời điểm hash |
| `request_hash` / `request-v1` | object `{operation,principal_id,request}` sau defaults/validation; bỏ pagination controls chỉ khi so initial request với continuation; mapping/rename query fingerprint có target/scope/constraints/resolver |
| `record payload_hash` / `record-payload-v1` | payload typed theo record_kind, raw text không trim; không tự thay tác giả/thời gian khi replay |
| `result_digest` / `rename-result-v1` | object `{scope,initial_request,provider_digest,file_preconditions,edits,conflicts}`; scope và mọi pinned_vector lồng trong initial_request chuẩn hóa members/unavailable_members theo scope-v1; mỗi file_precondition là object đúng `{worktree_id,profile_id,generation_id,path,content_hash}`, sort theo thứ tự các trường đó bằng raw UTF-8; edits sort(path,start,end,new_text), nếu đồng hạng sort tiếp theo byte lexicographic E(toàn edit) không domain prefix; duplicate identical edits dedup trước digest, overlaps mâu thuẫn reject; conflicts là Gap sort theo byte lexicographic E(toàn phần tử), không domain prefix |
| `subgraph_digest` / `subgraph-v1` | object `{scope,nodes,edges}` với scope members/unavailable_members sort theo scope-v1, nodes sort(node_id), edges sort(edge_id); labels sanitized deterministic; không camera/layout/hover/time |

Các ID content-based khác MUST dùng domain literal versioned + recipe ghi tại adapter seal; không tự ghép strings bằng `|` hoặc `/`. Hash không chứng minh authorization; mọi use vẫn kiểm policy, incarnations, actual rows. Hash collision/same ID different content bị từ chối và báo integrity conflict, không last-write-wins. Do sản phẩm chưa phát hành, C14N1 là codec baseline đầu tiên; không giả tương thích digest từ các bản tài liệu cũ chưa có implementation. T105/T107 kiểm Go/Python reference; T081/T085 kiểm Node cùng vectors; code provider không được bắt đầu trao đổi mapping trước pass.

Capture vs result: ManifestFile.status=`captured` nếu bytes hợp lệ đã chụp, kể cả Go parse lỗi; `files.status` là projection kết quả (indexed/parse_error/...). Parser/diagnostic không được sửa manifest hoặc snapshot_digest. ProviderDone.input_digest phải bằng input ban đầu. NOOP chỉ tái dùng mức capability đã có; config/dependency/provider hoặc retry điều kiện resource thay đổi phải mở lại phần phân tích cần thiết, không giữ partial mãi rồi gọi complete.

Path semantic validator: đường dẫn file relative `/`, không empty, NUL/control, backslash, absolute/drive/UNC, segment `.`/`..`, trailing slash hoặc segment rỗng. `.` chỉ hợp lệ khi trường là module_root của worktree, không SourceRef.path. Regex schema không dùng lookahead mà Go regexp không hỗ trợ; SV02/path adapter kiểm containment/encoding/case theo OS. Filesystem canonicalization không đồng nghĩa normalize source bytes. Các vector cuối S43 là input/output normative cho codec; không là fixture dữ liệu người dùng.

### CT-05 — Continuation có điểm kết thúc, không cắt im lặng (A04)

**Mapping server:** initial MappingResolveRequest có session/build/doc/hmr/ref, scope pinned khi từ graph, optional limit(default20,max100), max_output_bytes(default24576,max131072). Server resolve binding+resolver một lần, đọc immutable links theo keyset `(anchor_id,target_ref_digest,match_id)`; target_ref_digest=H("graph-ref-v1", typed GraphRef). MappingData.matches là đơn vị page giữ anchor-target-source-evidence đúng cặp; không trả hai mảng độc lập làm mất pairing. Mapping data không có live DOM instances. Browser lấy anchor từ matches rồi LIST_INSTANCES riêng.

Continuation gửi cùng request target/session/scope cùng cursor, chỉ được đổi limit/budget. Cursor purpose=mapping, bound principal/full vector/binding/resolver/map/epoch/query fingerprint/last_key/expiry. Không resolve active source/binding mới khi chuyển page. Global mapping_status chỉ exact unique khi enumerate đủ và đúng một đích; page đầu có một match trong tập chưa hết không được đánh dấu unique. Ambiguity/unsupported/gap vẫn giữ khi budget giảm. Hết TTL, HMR, permission revoke, GC hoặc resolver/binding đổi → error tương ứng, không nối trang khác snapshot. Mọi list còn pending phải có next_cursor khả thi; `pending=0` chỉ khi complete. Một item metadata không vừa max budget → RESULT_TOO_LARGE + descriptor, không cursor tự lặp.

**Rename giữ đúng sáu MCP tools:** `mce4d_rename_preview` nhận RenameInitialRequest HOẶC RenameContinueRequest (oneOf), không trộn. Initial chạy toàn prepareRename/rename và postvalidation trong scope trước page1; validate tất cả edits/conflicts, cap tổng10000 edits và32MiB result (effective config có thể hạ), reject quá cap trước success. Capture `file_preconditions`, result_id(UUID), result_digest và complete edit set; không chạy lại LSP mỗi page. Edits sort(path,start,end,new_text); no partial successful set khi provider bị lỗi. Một page tối đa512 edits nhưng serialized byte budget mới là cap thực.

Kết quả tạm nằm ở `<home>/worktrees/<id>/scratch/rename/<result_id>.json`, tạo exclusive/owner-only rồi atomic rename sau flush, có format/version/hash/header/scope/principal/engine-schema và TTL900s, aggregate cap128MiB/worktree. Chỉ ghi artifact do MCE4D sở hữu; không source/records/catalog. Temp spool dùng shared worktree lifecycle lease, unique result ownership; không lấy index writer cho mỗi page. Secure-open khi đọc, kiểm hash/header/incarnations/policy trước dùng. Result có thể bị quota GC/expiry, trả RESULT_EXPIRED; không tái tính rồi giả cùng result. Snapshot mất → SNAPSHOT_EXPIRED. Page cursor và result_token khác purpose; HMAC kiểm constant-time theo CT-03. Result token không chứa nguyên source/edits. Spool là derived sensitive cache, không chứng nhận secure erase; lỗi ghi/disk full không gửi token thành công.

RenameData.computation_complete=true nghĩa engine đã tính đủ trong scope; preview_complete=true CHỈ page cuối khi edits/conflicts đã deliver đủ. Mỗi page có cùng result_id/result_digest/total_edits; pagination nói rõ remaining. Consumer phải giữ tập pages từ page1, verify continuity/digest/file preconditions và gọi mce4d_validate_pack với result_token (XOR pack_token) trước áp dụng bằng host; không apply từng page. Validate result kiểm toàn original workspace manifest + file hashes + rule/policy/incarnations, không gia hạn result TTL. Last page chỉ có nghĩa giao đủ dữ liệu, không quyền apply hoặc bảo đảm filesystem không đổi sau đó. CLI mới mỗi invocation vẫn đọc được spool cùng principal; restart/lost spool lỗi rõ. Không persist transcript hoặc đánh dấu AI đã review.

**Source lớn:** source_range bắt buộc vượt budget được chia thành `source_range_fragment`. `fragment_of` giữ SourceRef nguyên parent; từng fragment có đúng một SourceRef range nhỏ hơn và content đúng byte slice, cùng file content_hash; nonfragment có fragment_of=null. Ưu tiên newline trong giới hạn; dòng dài chia tại UTF-8 boundary, không thay bytes. Các fragment nối liên tiếp không gap/overlap bao phủ parent range; cursor giữ next byte offset, item_id=H("source-fragment-v1", {parent_ref,range}); tăng budget không đổi nghĩa prefix đã gửi. Signature/constraint cần nguyên khối để giải thích thì đánh dấu obligation chưa đủ, không đưa prefix như toàn bộ. Chỉ hết parent/mandatory evidence mới bỏ required_pending. SourceReadRequest trả range được yêu cầu nếu vừa cap, nếu quá thì RESULT_TOO_LARGE + safe range descriptor; không tự cắt content dưới cùng SourceRef. UI/AI có thể yêu cầu subrange rõ hoặc dùng pack fragment.

Graph/flow/record list dùng keyset hoặc bounded materialized result cache có ID/digest/TTL, max32MiB/process+32 entries; không nhét frontier/visited lớn vào token. Bị evict trả RESULT_EXPIRED; không retry từ đầu rồi trộn page. Graph projection mỗi page có endpoints hoặc boundary nodes, visited progress theo immutable result. Loops, budget nhỏ, page cuối và expiry phải có oracle; không một continuation nào chỉ là lời nhắc “hãy lấy thêm” mà không request được định nghĩa.

### CT-06 — Pinned scope và exact selected root (A05)

ScopeRequest hợp lệ đúng một chế độ: (a) single worktree/profile/optional generation; (b) group_id live exploration; (c) pinned_vector, không thêm các field live. Empty chỉ khi server đã bind đúng một worktree/profile. Pinned vector là dữ liệu request, KHÔNG quyền: server verify digest/incarnations/group revision/source/policy và từng member trước/sau; không thay generation hết hạn bằng active. Policy/incarnation/group mismatch → blocked error; không gửi một phần snapshot khác dưới token cũ.

Group live resolver lấy expected group members từ catalog revision. `ScopeVector.members` chỉ member có generation thật và được quyền source; `unavailable_members` mô tả member expected chưa resolve được (index_missing/unavailable/profile_missing/access_denied). Không fake zero UUID/hash hoặc bỏ silently khỏi coverage. Member không được tiết lộ identity thì worktree/profile/service fields=null, chỉ count/status được group policy cho phép; không được quyền biết group → ACCESS_DENIED trước metadata. Federated vector có thể members=[] khi tất cả không có index; no source operations, readiness blocked. Single status chưa index: scope=null và StatusData.registered_scopes thực, không tạo ScopeMember giả.

Selector.kind=ref mang GraphRef đầy đủ và service_id|null. Trong group, selector symbol/module/entity thiếu member không hợp lệ; trả INVALID_ARGUMENT hoặc AMBIGUOUS_SYMBOL khi locate thực có ứng viên, không tự chọn member đầu tiên. Ref phải khớp đúng một resolved member; nếu cùng worktree/profile xuất hiện ở nhiều services thì service_id bắt buộc. Với typed selection từ Explorer, scope.pinned_vector bắt buộc; node ref trong graph page không tự chuyển thành source_position của active HEAD. UI `Use selection as AI context` giữ nguyên vector+ref+service và task constraints. Operation liên service không dựng SQL JOIN giữa DB; join bằng authorized in-memory refs có snapshot vector.

Pinned vector có missing member sẽ GIỮ gap đó cho cả pack; member về sau có index không được tự bổ sung vào trang tiếp. Người dùng tạo live query mới để resolve vector mới. Scope/policy/root mất quyền giữa các trang → block/new pack, không trả partial lẫn generations. Definition ID không bảo đảm qua rename/move; code bị đổi phải locate lại dựa source. Partial group không được gọi full-system impact.

### CT-07 — Cấu hình và vòng đời quản trị (A06)

RuntimeConfig, PolicyConfig và build/test CommandRecord S51 khác authority. File runtime optional ở `<MCE4D_HOME>/runtime.json` hoặc `--runtime-config <approved-local-file>`; absence dùng MCE4D_RUNTIME_DEFAULTS_V1. Merge built-in defaults→explicit runtime file→CLI overrides trong allowlist resource fields; reject unknown, invalid cross-field ranges. Chỉ CPU-derived max_parse_workers=0 là sentinel đã định nghĩa: resolve min(4,available CPUs), ít nhất1. Chính sách quyền được đăng ký vẫn thắng nếu runtime đề nghị nới cap. Config resource không cấp quyền đọc/ghi/mạng/execute, không đổi semantic profile âm thầm.

RuntimeConfig sau start immutable trong process. `config set` dùng expected_digest CAS dưới config-file lock ngắn, validate trước ghi temp/atomic rename; không sửa root configs/AGENTS/dependencies. Expected digest null chỉ khi file chưa có. Giá trị mới cần restart process bị ảnh hưởng; process đang chạy không tự nóng cập nhật. `config show` hiển thị effective/source digest và restart_required; `config validate` chỉ kiểm config, không tạo file/log run/DB/khởi chạy workload. Hai setter tranh chấp trả CONFIG_CONFLICT, không last-write-wins. T028 implement CLI adapter, T004 config use case; T105/T107 schema/default/CAS proof trước đó.

| CLI có quyền explicit (không HTTP/MCP mutation) | Hợp đồng |
|---|---|
| `mce4d config show|validate|set --input-json <file-or-dash>` | RuntimeConfigRequest; validate/set cần config; set cần expected_digest, show không nhận config; pathname từ global --runtime-config đã allowlisted |
| `mce4d policy set --input-json <file-or-dash>` | PolicyAdminRequest; worktree/expected_epoch CAS, digest/epoch cập nhật transaction; không request query nào gọi thay |
| `mce4d profile set --worktree-id <id> --input-json <file>` | ProfileAdminRequest; ID trên flag/body phải khớp; name+expected_revision CAS; profile effective hashed; không dựa tên profile làm identity |
| `mce4d group get|list|put|remove --input-json <file-or-dash>` | GroupAdminRequest, R2 T046; put toàn config+expected_revision, remove cần group_id+revision; tombstone+revision tăng, không reset IDs/revisions hoặc xóa source |
| `mce4d preview-permission get|list|put|revoke --input-json <file-or-dash>` | PreviewPermissionAdminRequest, U1 T087; put cần config version=expected_version+1, revoke có reason+CAS; principal từ OS/authorized actor, không tin attested_by tùy tiện |
| `mce4d records list|read --input-json <file-or-dash>` | RecordListRequest/RecordReadRequest; historical version không overwrite; use same query service như Explorer |

Các subcommand dùng action field tương ứng; flag action và body action khác → INVALID_ARGUMENT. Request cho get cần ID; list không nhận target/config/CAS; mutating action thiếu expected version/digest → INVALID_ARGUMENT. Lists hỗ trợ limit(default20,max100)/cursor qua same request, stable catalog/record revision và incarnation; sorted ID/version, đổi filter giữa pages reject. Không thêm exec/raw_sql/URL fetch. `CLI ...` keys trong operation registry là CLI-only; dispatcher HTTP/MCP MUST reject dù payload hợp lệ. Chức năng chưa tới task/gate không expose.

Preview permission lưu immutable versions+heads trong catalog DDL, không file raw browser được tự đưa vào để cấp quyền. Put/revoke transaction gồm version mới, head CAS; đúng một actor và nguồn xác nhận test profile. Revoke giữ lịch sử, mọi process query status/attach/mode/bind đọc head hiện tại trước/sau; session pin permission_version, khi head thay disarm và yêu cầu re-attach/approve mới. Expiry đánh giá ở read, không cần background mutation và không tự gia hạn. Revoked version không được khôi phục bằng sửa JSON tại cùng version; new approved version cần xác nhận mới. Attestation của chủ môi trường không phải sandbox proof; vẫn test controls trước claims.

Record list/read: default current head, history chỉ version explicit; không đọc nguyên forbidden source vào payload. Baseline UI read body chỉ hỗ trợ UIMappingPayload; record kind khác chưa có closed payload schema ở seal R2 trả metadata+unsupported gap, không arbitrary any. Effective needs_review/provenance_status derived từ current policy/hash/store và retained snapshot. Snapshot mất thì record vẫn còn, source unavailable và needs_review, không tự approve/rebind bằng same-name. Relocation candidate dựa exact content/range trong đúng worktree/profile chỉ là gợi ý; tạo version mapping mới qua review explicit. Không cần lưu raw source lâu dài trong records để giữ tên quyết định.

Runtime caps quan trọng: `default<=hard`, max_output_bytes nằm4096..131072, graph/flow không vượt schema hard cap, timeouts/TTL/counts hữu hạn; rename_result_max_bytes<=rename_results_max_bytes và phù hợp worktree cache quota; max_file_bytes<=max_source_bytes_per_snapshot; polling>0, reconcile>=poll; script override không được vô hiệu hóa guard. default2 retained generations/profile; tăng retention chỉ theo evidence và budget, không task memory lease mới. DB pragma/pool defaults trong S44 giữ cố định nếu chưa ADR; setter không tự nới an toàn synchronous/FK.

### CT-08 — Preview status, liveness và quyền hết hạn (A03)

`POST /preview/status` là authenticated READ với PreviewStatusRequest{session_id}; mặc dù HTTP POST, không gia hạn TTL, không authorize mode, không nonce rotation, không tăng revision chỉ vì polling. PreviewStatusData không có bearer/pairing_nonce; trả version quyền hiện tại, effective permission status, binding và interaction_allowed=false khi expired/revoked/stale/maintenance. Detached session còn retention ngắn trả detached; unknown session chỉ lỗi scoped, không expose session khác. scope response là vector pin của phiên đã authorized hoặc null khi unavailable; không lấy active generations mới.

Browser poll bằng cùng session/request sequence, bỏ kết quả revision cũ; timeout2 missed theo polling interval→shield+disarm. Mất quyền hoặc permission version khác phản hồi đã arm→shield ngay khi quan sát, không đợi MODE_ACK. Page hidden→Inspect ngay; foreground chỉ được arm lại qua thao tác explicit. Clock so TTL dùng monotonic elapsed + UTC expiry kiểm tra cả hai; clock rollback không kéo dài permission/session. Offline/runtime sleep không được giữ một lease tương tác vô hạn; resumed page phải kiểm lại status.

Server status không chứng minh SDK/frame alive. Parent PING→SDK PONG dùng probe_id/reply_to/session/doc/hmr và exact origin/source, timeout riêng cùng policy polling; wrong/replayed/late PONG bị bỏ. PONG không tăng quyền/hmr, không là proof source fresh. Browser throttling khiến phát hiện chậm vẫn được ghi giới hạn; Inspect/isolated test environment không bị thay bằng một lời hứa instantaneous revoke. Read status có thể tính expiry hiệu lực, nhưng không dùng GET/POST read để approve/renew. Không gửi SDK credential engine, quyền file hoặc source paths.


<!-- BEGIN MCE4D_HASH_VECTORS_V1 -->
```json
{
  "codec": "MCE-C14N1",
  "scope": "document_reference_not_product",
  "valid": [
    {
      "case": "null",
      "domain": "codec-test-v1",
      "value": null,
      "encoding_hex": "4d43453444004331344e31007331333a636f6465632d746573742d76316e",
      "digest": "9a3a326d97dd4ca8e7648816695f8e2ae848098b19025f76d5bc717987a42ac0"
    },
    {
      "case": "booleans",
      "domain": "codec-test-v1",
      "value": [
        true,
        false
      ],
      "encoding_hex": "4d43453444004331344e31007331333a636f6465632d746573742d763161323a7466",
      "digest": "94e8da8f0b464b9826d0329d3eb4a26a6abae67cab94f8618052a02ef6526882"
    },
    {
      "case": "unicode_crlf",
      "domain": "codec-test-v1",
      "value": "Tiếng Việt | ລາວ | 😀\r\n",
      "encoding_hex": "4d43453444004331344e31007331333a636f6465632d746573742d76317333353a5469e1babf6e67205669e1bb8774207c20e0baa5e0bab2e0baa7207c20f09f98800d0a",
      "digest": "5f35687ff357cee6a88e37bf8e9e6fc7ab480cdab89b16978b2e846a94b40805"
    },
    {
      "case": "unicode_lf",
      "domain": "codec-test-v1",
      "value": "Tiếng Việt | ລາວ | 😀\n",
      "encoding_hex": "4d43453444004331344e31007331333a636f6465632d746573742d76317333343a5469e1babf6e67205669e1bb8774207c20e0baa5e0bab2e0baa7207c20f09f98800a",
      "digest": "eb18d16c7b112daad67190ad338408b35c420cc197b89b98b6dff9c96d0e2f46"
    },
    {
      "case": "key_order",
      "domain": "codec-test-v1",
      "value": {
        "😀": 1,
        "": 2,
        "z": 3,
        "a": 4
      },
      "encoding_hex": "4d43453444004331344e31007331333a636f6465632d746573742d76316f343a73313a6169343b73313a7a69333b73333aee808069323b73343af09f988069313b",
      "digest": "13acc7905d6a88da4a4d6dbf3281f7d36cf6911c820eb1e2a399215245b6b459"
    },
    {
      "case": "integers",
      "domain": "codec-test-v1",
      "value": [
        0,
        -1,
        9007199254740991,
        -9007199254740991
      ],
      "encoding_hex": "4d43453444004331344e31007331333a636f6465632d746573742d763161343a69303b692d313b69393030373139393235343734303939313b692d393030373139393235343734303939313b",
      "digest": "0c0dc14969ef529f60470b134f398710640349b0202d8f3d7bf85d80c4c811e8"
    },
    {
      "case": "order_za",
      "domain": "codec-test-v1",
      "value": [
        "z",
        "a"
      ],
      "encoding_hex": "4d43453444004331344e31007331333a636f6465632d746573742d763161323a73313a7a73313a61",
      "digest": "90c64846e965c081e8c8c2a1f5fc68fc0c7d58453a5cc4b23b4716a1dec269e3"
    },
    {
      "case": "order_az",
      "domain": "codec-test-v1",
      "value": [
        "a",
        "z"
      ],
      "encoding_hex": "4d43453444004331344e31007331333a636f6465632d746573742d763161323a73313a6173313a7a",
      "digest": "a61b3d0564f4f0c61d6a7c05192e0f881e8ba0e91d0d58aeda859e8557de8bbc"
    },
    {
      "case": "missing",
      "domain": "codec-test-v1",
      "value": {},
      "encoding_hex": "4d43453444004331344e31007331333a636f6465632d746573742d76316f303a",
      "digest": "d53de2c8b3d21324eb25451a5ebc2e72b81e7499bb7d7e181a65c6d4ba7ca0b4"
    },
    {
      "case": "explicit_null",
      "domain": "codec-test-v1",
      "value": {
        "x": null
      },
      "encoding_hex": "4d43453444004331344e31007331333a636f6465632d746573742d76316f313a73313a786e",
      "digest": "301fdf9fcecbf5c2a6d22d989a4773da38661ef44ae02fb97f4dca12e332508a"
    },
    {
      "case": "empty",
      "domain": "codec-test-v1",
      "value": [
        "",
        [],
        {}
      ],
      "encoding_hex": "4d43453444004331344e31007331333a636f6465632d746573742d763161333a73303a61303a6f303a",
      "digest": "22e351613289bcd381db83e037722a63923ef7267140664bd34e7c24806b487c"
    },
    {
      "case": "anchor",
      "domain": "anchor-v1",
      "value": {
        "path": "web/ລາວ/Button.tsx",
        "content_hash": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "kind": "element_site",
        "start_byte": 7,
        "end_byte": 35,
        "provider_digest": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
      },
      "encoding_hex": "4d43453444004331344e310073393a616e63686f722d76316f363a7331323a636f6e74656e745f686173687336343a6161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616173383a656e645f627974656933353b73343a6b696e647331323a656c656d656e745f7369746573343a706174687332343a7765622fe0baa5e0bab2e0baa72f427574746f6e2e7473787331353a70726f76696465725f6469676573747336343a626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262627331303a73746172745f6279746569373b",
      "digest": "b7f67eb9294b0a02242e0070a218c236c22b2a06e3520ff7aa2e36e96f014d49"
    },
    {
      "case": "binding",
      "domain": "binding-v1",
      "value": {
        "build_map_id": "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
        "source_generation_id": "00000000-0000-4000-8000-000000000001",
        "profile_id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "policy_digest": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
        "resolver_digest": "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
      },
      "encoding_hex": "4d43453444004331344e31007331303a62696e64696e672d76316f353a7331323a6275696c645f6d61705f69647336343a636363636363636363636363636363636363636363636363636363636363636363636363636363636363636363636363636363636363636363636363636363637331333a706f6c6963795f6469676573747336343a626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262627331303a70726f66696c655f69647336343a616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161616161617331353a7265736f6c7665725f6469676573747336343a646464646464646464646464646464646464646464646464646464646464646464646464646464646464646464646464646464646464646464646464646464647332303a736f757263655f67656e65726174696f6e5f69647333363a30303030303030302d303030302d343030302d383030302d303030303030303030303031",
      "digest": "3b352d73b1c13cbcd0ca363aa3baa59b65db0a2d725733fd4613375606cc2b49"
    },
    {
      "case": "capture",
      "domain": "capture-v1",
      "value": {
        "schema_version": 1,
        "files": [
          {
            "path": "bad.go",
            "status": "captured",
            "content_hash": "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
            "byte_length": 11,
            "reason_code": null
          }
        ],
        "dependency_input_digests": []
      },
      "encoding_hex": "4d43453444004331344e31007331303a636170747572652d76316f333a7332343a646570656e64656e63795f696e7075745f6469676573747361303a73353a66696c657361313a6f353a7331313a627974655f6c656e6774686931313b7331323a636f6e74656e745f686173687336343a6666666666666666666666666666666666666666666666666666666666666666666666666666666666666666666666666666666666666666666666666666666673343a7061746873363a6261642e676f7331313a726561736f6e5f636f64656e73363a73746174757373383a63617074757265647331343a736368656d615f76657273696f6e69313b",
      "digest": "a64832f1988e904f5536a8430409440cf4191a24823c8c7a5fb03c354724e5e2"
    }
  ],
  "reject_identity_values": [
    "fractional number",
    "integer magnitude > 9007199254740991",
    "lone surrogate",
    "duplicate object keys",
    "invalid UTF-8",
    "unknown value type"
  ]
}
```
<!-- END MCE4D_HASH_VECTORS_V1 -->

<!-- BEGIN MCE4D_RUNTIME_DEFAULTS_V1 -->
```json
{
  "schema_version": 1,
  "core": {
    "max_file_bytes": 2097152,
    "max_indexed_files": 100000,
    "max_source_bytes_per_snapshot": 536870912,
    "max_parallel_index_jobs": 2,
    "max_parse_workers": 0,
    "max_lsp_sessions": 1,
    "sqlite_busy_timeout_ms": 5000,
    "writer_lock_wait_ms": 5000,
    "query_timeout_ms": 10000,
    "lsp_startup_timeout_ms": 30000,
    "rename_timeout_ms": 30000,
    "index_timeout_ms": 600000,
    "watch_poll_interval_ms": 2000,
    "full_reconcile_interval_ms": 60000,
    "max_output_bytes": 24576,
    "max_graph_nodes": 2000,
    "max_graph_edges": 10000,
    "max_graph_depth": 8,
    "keep_generations": 2,
    "pack_ttl_seconds": 900,
    "max_worktree_cache_bytes": 2147483648,
    "log_files": 5,
    "log_file_bytes": 10485760,
    "maintenance_wait_ms": 5000,
    "rename_result_max_bytes": 33554432,
    "rename_results_max_bytes": 134217728,
    "rename_result_ttl_seconds": 900,
    "rename_max_edits": 10000,
    "result_ttl_seconds": 900,
    "query_result_cache_bytes": 33554432,
    "max_retained_query_results": 32
  },
  "explorer": {
    "graph_default_nodes": 200,
    "graph_default_edges": 500,
    "graph_hard_nodes": 500,
    "graph_hard_edges": 1500,
    "flow_max_steps": 100,
    "flow_max_edges": 300,
    "hover_hz": 15,
    "session_ttl_seconds": 1800,
    "bearer_ttl_seconds": 1800,
    "ack_timeout_ms": 3000,
    "detach_timeout_ms": 1000,
    "poll_visible_ms": 2000,
    "poll_hidden_ms": 5000,
    "poll_miss_limit": 2,
    "bridge_message_bytes": 16384,
    "max_preview_sessions": 4,
    "map_watch_poll_ms": 2000,
    "map_artifact_max_bytes": 67108864,
    "map_pending_max": 1,
    "map_watch_debounce_ms": 300
  }
}
```
<!-- END MCE4D_RUNTIME_DEFAULTS_V1 -->

<!-- BEGIN MCE4D_CONTRACT_SCHEMA_V1 -->
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "urn:mce4d:contracts:1",
  "$ref": "#/$defs/Response",
  "$defs": {
    "UUID": {"type":"string","pattern":"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"},
    "Hash": {"type":"string","pattern":"^[0-9a-f]{64}$"},
    "ID": {"type":"string","minLength":1,"maxLength":192},
    "Token": {"type":"string","minLength":1,"maxLength":32768},
    "Time": {"type":"string","format":"date-time"},
    "Count": {"type":"integer","minimum":0,"maximum":9007199254740991},
    "Path": {"type":"string","minLength":1,"maxLength":4096},
    "ByteRange": {"type":"object","properties":{"start_byte":{"type":"integer","minimum":0,"maximum":9007199254740991},"end_byte":{"type":"integer","minimum":0,"maximum":9007199254740991}},"required":["start_byte","end_byte"],"additionalProperties":false},
    "SourceRef": {"type":"object","properties":{"worktree_id":{"$ref":"#/$defs/UUID"},"profile_id":{"$ref":"#/$defs/Hash"},"generation_id":{"$ref":"#/$defs/UUID"},"path":{"$ref":"#/$defs/Path"},"content_hash":{"$ref":"#/$defs/Hash"},"range":{"$ref":"#/$defs/ByteRange"}},"required":["worktree_id","profile_id","generation_id","path","content_hash","range"],"additionalProperties":false},
    "ScopeMember": {"type":"object","properties":{"worktree_id":{"$ref":"#/$defs/UUID"},"profile_id":{"$ref":"#/$defs/Hash"},"generation_id":{"$ref":"#/$defs/UUID"},"snapshot_digest":{"$ref":"#/$defs/Hash"},"policy_digest":{"$ref":"#/$defs/Hash"},"policy_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"binding_revision":{"type":"integer","minimum":1,"maximum":9007199254740991},"record_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"service_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"index_incarnation":{"$ref":"#/$defs/UUID"},"records_incarnation":{"$ref":"#/$defs/UUID"}},"required":["worktree_id","profile_id","generation_id","snapshot_digest","policy_digest","policy_epoch","binding_revision","record_epoch","service_id","index_incarnation","records_incarnation"],"additionalProperties":false},
    "ScopeVector": {"type":"object","properties":{"kind":{"enum":["single","federated"]},"members":{"type":"array","items":{"$ref":"#/$defs/ScopeMember"},"minItems":0,"maxItems":32},"vector_digest":{"$ref":"#/$defs/Hash"},"group_id":{"anyOf":[{"$ref":"#/$defs/UUID"},{"type":"null"}]},"group_revision":{"anyOf":[{"type":"integer","minimum":1,"maximum":9007199254740991},{"type":"null"}]},"catalog_incarnation":{"$ref":"#/$defs/UUID"},"unavailable_members":{"type":"array","items":{"$ref":"#/$defs/MemberUnavailable"},"minItems":0,"maxItems":32}},"required":["kind","members","vector_digest","group_id","group_revision","catalog_incarnation","unavailable_members"],"additionalProperties":false},
    "ScopeRequest": {"type":"object","properties":{"worktree_id":{"$ref":"#/$defs/UUID"},"profile_id":{"$ref":"#/$defs/Hash"},"generation_id":{"$ref":"#/$defs/UUID"},"group_id":{"$ref":"#/$defs/UUID"},"pinned_vector":{"$ref":"#/$defs/ScopeVector"}},"required":[],"additionalProperties":false},
    "SymbolRef": {"type":"object","properties":{"kind":{"const":"symbol"},"worktree_id":{"$ref":"#/$defs/UUID"},"profile_id":{"$ref":"#/$defs/Hash"},"generation_id":{"$ref":"#/$defs/UUID"},"symbol_id":{"$ref":"#/$defs/ID"}},"required":["kind","worktree_id","profile_id","generation_id","symbol_id"],"additionalProperties":false},
    "EntityRef": {"type":"object","properties":{"kind":{"const":"entity"},"worktree_id":{"$ref":"#/$defs/UUID"},"profile_id":{"$ref":"#/$defs/Hash"},"generation_id":{"$ref":"#/$defs/UUID"},"entity_id":{"$ref":"#/$defs/ID"}},"required":["kind","worktree_id","profile_id","generation_id","entity_id"],"additionalProperties":false},
    "ModuleRef": {"type":"object","properties":{"kind":{"const":"module"},"worktree_id":{"$ref":"#/$defs/UUID"},"profile_id":{"$ref":"#/$defs/Hash"},"generation_id":{"$ref":"#/$defs/UUID"},"module_id":{"$ref":"#/$defs/ID"}},"required":["kind","worktree_id","profile_id","generation_id","module_id"],"additionalProperties":false},
    "AnchorRef": {"type":"object","properties":{"kind":{"const":"source_anchor"},"worktree_id":{"$ref":"#/$defs/UUID"},"profile_id":{"$ref":"#/$defs/Hash"},"generation_id":{"$ref":"#/$defs/UUID"},"build_map_id":{"$ref":"#/$defs/Hash"},"anchor_id":{"$ref":"#/$defs/Hash"},"binding_id":{"$ref":"#/$defs/Hash"}},"required":["kind","worktree_id","profile_id","generation_id","build_map_id","anchor_id","binding_id"],"additionalProperties":false},
    "GraphRef": {"oneOf":[{"$ref":"#/$defs/SymbolRef"},{"$ref":"#/$defs/EntityRef"},{"$ref":"#/$defs/ModuleRef"},{"$ref":"#/$defs/AnchorRef"},{"$ref":"#/$defs/FileRef"}]},
    "WorkspaceSelector": {"type":"object","properties":{"kind":{"const":"workspace"}},"required":["kind"],"additionalProperties":false},
    "SymbolSelector": {"type":"object","properties":{"kind":{"const":"symbol"},"symbol_id":{"$ref":"#/$defs/ID"}},"required":["kind","symbol_id"],"additionalProperties":false},
    "ModuleSelector": {"type":"object","properties":{"kind":{"const":"module"},"module_id":{"$ref":"#/$defs/ID"}},"required":["kind","module_id"],"additionalProperties":false},
    "EntitySelector": {"type":"object","properties":{"kind":{"const":"entity"},"entity_id":{"$ref":"#/$defs/ID"}},"required":["kind","entity_id"],"additionalProperties":false},
    "PositionSelector": {"type":"object","properties":{"kind":{"const":"source_position"},"path":{"$ref":"#/$defs/Path"},"content_hash":{"$ref":"#/$defs/Hash"},"byte_offset":{"type":"integer","minimum":0,"maximum":9007199254740991}},"required":["kind","path","content_hash","byte_offset"],"additionalProperties":false},
    "DocumentSelector": {"type":"object","properties":{"kind":{"const":"document_anchor"},"path":{"$ref":"#/$defs/Path"},"content_hash":{"$ref":"#/$defs/Hash"},"range":{"$ref":"#/$defs/ByteRange"}},"required":["kind","path","content_hash","range"],"additionalProperties":false},
    "Selector": {"oneOf":[{"$ref":"#/$defs/WorkspaceSelector"},{"$ref":"#/$defs/SymbolSelector"},{"$ref":"#/$defs/ModuleSelector"},{"$ref":"#/$defs/EntitySelector"},{"$ref":"#/$defs/PositionSelector"},{"$ref":"#/$defs/DocumentSelector"},{"$ref":"#/$defs/RefSelector"}]},
    "Gap": {"type":"object","properties":{"code":{"$ref":"#/$defs/ID"},"reason":{"type":"string","minLength":1,"maxLength":1024},"count":{"anyOf":[{"type":"integer","minimum":0,"maximum":9007199254740991},{"type":"null"}]},"source_refs":{"type":"array","items":{"$ref":"#/$defs/SourceRef"},"minItems":0,"maxItems":16}},"required":["code","reason","count","source_refs"],"additionalProperties":false},
    "EvidenceRef": {"type":"object","properties":{"evidence_id":{"$ref":"#/$defs/ID"},"origin":{"enum":["static_analyzer","build_instrumentation","runtime_observation","manual_approved","ai_proposal"]},"kind":{"enum":["syntax_exact","type_resolved","type_possible","explicit_document","text_match","unresolved","build_anchored","runtime_observed","manual_declared","proposed"]},"source_refs":{"type":"array","items":{"$ref":"#/$defs/SourceRef"},"minItems":0,"maxItems":16},"record_ref":{"anyOf":[{"type":"object","properties":{"record_id":{"$ref":"#/$defs/UUID"},"version":{"type":"integer","minimum":1,"maximum":9007199254740991}},"required":["record_id","version"],"additionalProperties":false},{"type":"null"}]},"artifact_id":{"anyOf":[{"$ref":"#/$defs/Hash"},{"type":"null"}]}},"required":["evidence_id","origin","kind","source_refs","record_ref","artifact_id"],"additionalProperties":false},
    "Freshness": {"type":"object","properties":{"status":{"enum":["observed_current","changed","unknown","not_checked"]},"scope":{"enum":["workspace","returned_files","none"]},"checked_at":{"anyOf":[{"$ref":"#/$defs/Time"},{"type":"null"}]},"buffer_scope":{"enum":["disk_only","editor_session"]},"consistency":{"enum":["captured_bytes_with_observed_stability","captured_buffer_version"]},"changed_count":{"anyOf":[{"type":"integer","minimum":0,"maximum":9007199254740991},{"type":"null"}]},"source_view":{"enum":["indexed_snapshot","editor_snapshot"]}},"required":["status","scope","checked_at","buffer_scope","consistency","changed_count","source_view"],"additionalProperties":false},
    "Capability": {"type":"object","properties":{"name":{"$ref":"#/$defs/ID"},"provider_id":{"$ref":"#/$defs/ID"},"provider_version":{"type":"string","minLength":1,"maxLength":128},"status":{"enum":["supported_scope_complete","partial","unsupported","unknown"]}},"required":["name","provider_id","provider_version","status"],"additionalProperties":false},
    "Coverage": {"type":"object","properties":{"status":{"enum":["supported_scope_complete","partial","unsupported","unknown"]},"capabilities":{"type":"array","items":{"$ref":"#/$defs/Capability"},"minItems":0,"maxItems":128},"gaps":{"type":"array","items":{"$ref":"#/$defs/Gap"},"minItems":0,"maxItems":64},"scope_description":{"type":"string","minLength":1,"maxLength":1024}},"required":["status","capabilities","gaps","scope_description"],"additionalProperties":false},
    "Pagination": {"type":"object","properties":{"truncated":{"type":"boolean"},"enumeration_complete":{"type":"boolean"},"returned_count":{"type":"integer","minimum":0,"maximum":9007199254740991},"required_pending":{"anyOf":[{"type":"integer","minimum":0,"maximum":9007199254740991},{"type":"null"}]},"optional_pending":{"anyOf":[{"type":"integer","minimum":0,"maximum":9007199254740991},{"type":"null"}]},"next_cursor":{"anyOf":[{"$ref":"#/$defs/Token"},{"type":"null"}]},"limit_reason":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]}},"required":["truncated","enumeration_complete","returned_count","required_pending","optional_pending","next_cursor","limit_reason"],"additionalProperties":false},
    "Budget": {"type":"object","properties":{"max_output_bytes":{"type":"integer","minimum":4096,"maximum":1048576},"serialized_bytes":{"type":"integer","minimum":0,"maximum":9007199254740991},"token_count":{"anyOf":[{"type":"integer","minimum":0,"maximum":9007199254740991},{"type":"null"}]},"token_estimate":{"anyOf":[{"type":"integer","minimum":0,"maximum":9007199254740991},{"type":"null"}]},"tokenizer_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"estimate_only":{"type":"boolean"},"delivery_mode":{"enum":["http","cli","mcp_text","mcp_dual","mcp_structured_verified"]}},"required":["max_output_bytes","serialized_bytes","token_count","token_estimate","tokenizer_id","estimate_only","delivery_mode"],"additionalProperties":false},
    "Error": {"type":"object","properties":{"code":{"enum":["INVALID_ARGUMENT","UNSUPPORTED_SCHEMA","UNKNOWN_WORKTREE","ROOT_IDENTITY_CHANGED","ACCESS_DENIED","SECRET_BLOCKED","PATH_ESCAPE","INDEX_MISSING","INDEX_STALE","FRESHNESS_UNKNOWN","WORKTREE_CHANGED_DURING_INDEX","AMBIGUOUS_SYMBOL","UNSUPPORTED_CAPABILITY","DEPENDENCY_UNAVAILABLE","WORKTREE_BUSY","RESOURCE_LIMIT","TIMEOUT","CANCELLED","BUDGET_TOO_SMALL","CONTEXT_INCOMPLETE","CURSOR_INVALID","CURSOR_EXPIRED","SNAPSHOT_EXPIRED","POLICY_CHANGED","PROFILE_MISMATCH","SOURCE_PRECONDITION_FAILED","SCHEMA_TOO_NEW","INDEX_CORRUPT","DISK_FULL","INTERNAL_ERROR","PREVIEW_NOT_APPROVED","PREVIEW_UNAVAILABLE","PREVIEW_EMBED_BLOCKED","PREVIEW_ORIGIN_MISMATCH","PREVIEW_BUILD_MISMATCH","BRIDGE_NOT_READY","BRIDGE_PROTOCOL_MISMATCH","STALE_INSTANCE","AMBIGUOUS_UI_MAPPING","UI_TARGET_NOT_RENDERED","BUILD_MAP_MISSING","BUILD_MAP_EXPIRED","UNSUPPORTED_FRAMEWORK","TRACE_UNBOUND","INTERACTION_NOT_APPROVED","EXPLORER_ASSETS_UNAVAILABLE","RECORD_CONFLICT","RECORD_CHANGED","MODE_ACK_TIMEOUT","AUTH_REQUIRED","AUTH_EXPIRED","AUTH_INVALID","RATE_LIMITED","CONTRACT_NOT_FROZEN","UNEXPECTED_FIELD","DUPLICATE_FIELD","SOURCE_CAPTURE_MISMATCH","RESULT_TOO_LARGE","SOURCE_RANGE_INVALID","RESULT_EXPIRED","STORE_REPLACED","STORE_MAINTENANCE","CONFIG_CONFLICT","PERMISSION_REVOKED","PERMISSION_EXPIRED","STORE_RECOVERY_REQUIRED"]},"message":{"type":"string","minLength":1,"maxLength":1024},"retryable":{"type":"boolean"},"suggested_action":{"enum":["fix_request","reauthenticate","register_scope","explicit_index","recreate_pack","expand","review_candidates","verify_dependency","retry_bounded","review_policy","recover_store","cancel","none"]},"details":{"type":"array","items":{"$ref":"#/$defs/Gap"},"minItems":0,"maxItems":16}},"required":["code","message","retryable","suggested_action","details"],"additionalProperties":false},
    "StatusRequest": {"type":"object","properties":{"scope":{"$ref":"#/$defs/ScopeRequest"},"verify":{"enum":["none","workspace"]}},"required":[],"additionalProperties":false},
    "LocateRequest": {"type":"object","properties":{"scope":{"$ref":"#/$defs/ScopeRequest"},"mode":{"enum":["symbol","file","module","topic","text"]},"query":{"type":"string","minLength":1,"maxLength":512},"position":{"$ref":"#/$defs/PositionSelector"},"kinds":{"type":"array","items":{"$ref":"#/$defs/ID"},"minItems":0,"maxItems":16},"package":{"type":"string","minLength":1,"maxLength":512},"limit":{"type":"integer","minimum":1,"maximum":100},"cursor":{"$ref":"#/$defs/Token"}},"required":[],"additionalProperties":false},
    "PackRequest": {"type":"object","properties":{"scope":{"$ref":"#/$defs/ScopeRequest"},"selector":{"$ref":"#/$defs/Selector"},"view":{"enum":["overview","symbol","flow","state","contract","change","architecture","ai_lineage"]},"task_kind":{"enum":["explore","plan","rename","type_change","behavior_change","contract_change","debug"]},"intent":{"type":"string","minLength":0,"maxLength":2048},"constraints":{"type":"array","items":{"enum":["preserve_json_keys","preserve_db_schema","no_public_api_change","preserve_message_schema","no_source_mutation"]},"minItems":0,"maxItems":16},"freshness_mode":{"enum":["checked_workspace","analysis_snapshot"]},"max_output_bytes":{"type":"integer","minimum":4096,"maximum":131072}},"required":["selector","task_kind"],"additionalProperties":false},
    "ExpandRequest": {"type":"object","properties":{"pack_token":{"$ref":"#/$defs/Token"},"next_cursor":{"$ref":"#/$defs/Token"},"max_output_bytes":{"type":"integer","minimum":4096,"maximum":131072}},"required":["pack_token","next_cursor"],"additionalProperties":false},
    "ValidateRequest": {"type":"object","properties":{"pack_token":{"$ref":"#/$defs/Token"},"verify":{"const":"workspace"},"result_token":{"$ref":"#/$defs/Token"}},"required":[],"additionalProperties":false,"oneOf":[{"required":["pack_token"],"not":{"required":["result_token"]}},{"required":["result_token"],"not":{"required":["pack_token"]}}]},
    "RenameRequest": {"oneOf":[{"$ref":"#/$defs/RenameInitialRequest"},{"$ref":"#/$defs/RenameContinueRequest"}]},
    "EvidenceItem": {"type":"object","properties":{"item_id":{"$ref":"#/$defs/ID"},"kind":{"enum":["source_range","reference_catalog","constraint_excerpt","boundary","overview","validation_obligation","source_range_fragment"]},"required_for_review":{"type":"boolean"},"why_included":{"$ref":"#/$defs/ID"},"source_refs":{"type":"array","items":{"$ref":"#/$defs/SourceRef"},"minItems":0,"maxItems":16},"content":{"anyOf":[{"type":"string","minLength":0,"maxLength":131072},{"type":"null"}]},"evidence_refs":{"type":"array","items":{"$ref":"#/$defs/EvidenceRef"},"minItems":0,"maxItems":16},"limitations":{"type":"array","items":{"$ref":"#/$defs/Gap"},"minItems":0,"maxItems":16},"fragment_of":{"anyOf":[{"$ref":"#/$defs/SourceRef"},{"type":"null"}]}},"required":["item_id","kind","required_for_review","why_included","source_refs","content","evidence_refs","limitations","fragment_of"],"additionalProperties":false},
    "Candidate": {"type":"object","properties":{"ref":{"$ref":"#/$defs/GraphRef"},"name":{"type":"string","minLength":0,"maxLength":512},"qualified_name":{"type":"string","minLength":0,"maxLength":2048},"source_refs":{"type":"array","items":{"$ref":"#/$defs/SourceRef"},"minItems":0,"maxItems":16},"evidence_refs":{"type":"array","items":{"$ref":"#/$defs/EvidenceRef"},"minItems":0,"maxItems":16}},"required":["ref","name","qualified_name","source_refs","evidence_refs"],"additionalProperties":false},
    "StatusData": {"type":"object","properties":{"operation":{"const":"status"},"scopes":{"type":"array","items":{"$ref":"#/$defs/ScopeMember"},"minItems":0,"maxItems":32},"capabilities":{"type":"array","items":{"$ref":"#/$defs/Capability"},"minItems":0,"maxItems":128},"project_stage":{"enum":["spec_only","mixed","implemented_scope","unknown"]},"index_state":{"enum":["missing","published","building_with_previous","unavailable"]},"registered_scopes":{"type":"array","items":{"$ref":"#/$defs/RegisteredScopeStatus"},"minItems":0,"maxItems":32}},"required":["operation","scopes","capabilities","project_stage","index_state","registered_scopes"],"additionalProperties":false},
    "LocateData": {"type":"object","properties":{"operation":{"const":"locate"},"candidates":{"type":"array","items":{"$ref":"#/$defs/Candidate"},"minItems":0,"maxItems":100}},"required":["operation","candidates"],"additionalProperties":false},
    "PackData": {"type":"object","properties":{"operation":{"enum":["context_pack","expand_pack"]},"pack_token":{"$ref":"#/$defs/Token"},"target_refs":{"type":"array","items":{"$ref":"#/$defs/GraphRef"},"minItems":0,"maxItems":32},"items":{"type":"array","items":{"$ref":"#/$defs/EvidenceItem"},"minItems":0,"maxItems":512},"edit_precondition_validated":{"type":"boolean"}},"required":["operation","pack_token","target_refs","items","edit_precondition_validated"],"additionalProperties":false},
    "ValidateData": {"type":"object","properties":{"operation":{"const":"validate_pack"},"edit_precondition_validated":{"type":"boolean"},"changed_sources":{"type":"array","items":{"$ref":"#/$defs/SourceRef"},"minItems":0,"maxItems":256},"membership_changed":{"type":"boolean"},"configuration_changed":{"type":"boolean"},"record_set_changed":{"type":"boolean"}},"required":["operation","edit_precondition_validated","changed_sources","membership_changed","configuration_changed","record_set_changed"],"additionalProperties":false},
    "ProposedEdit": {"type":"object","properties":{"source_ref":{"$ref":"#/$defs/SourceRef"},"new_text":{"type":"string","minLength":0,"maxLength":131072}},"required":["source_ref","new_text"],"additionalProperties":false},
    "RenameData": {"type":"object","properties":{"operation":{"const":"rename_preview"},"applied":{"const":false},"source_mutation_count":{"const":0},"validation_required":{"const":true},"edits":{"type":"array","items":{"$ref":"#/$defs/ProposedEdit"},"minItems":0,"maxItems":512},"conflicts":{"type":"array","items":{"$ref":"#/$defs/Gap"},"minItems":0,"maxItems":128},"preview_complete":{"type":"boolean"},"result_token":{"$ref":"#/$defs/Token"},"result_id":{"$ref":"#/$defs/UUID"},"result_digest":{"$ref":"#/$defs/Hash"},"total_edits":{"type":"integer","minimum":0,"maximum":10000},"computation_complete":{"const":true}},"required":["operation","applied","source_mutation_count","validation_required","edits","conflicts","preview_complete","result_token","result_id","result_digest","total_edits","computation_complete"],"additionalProperties":false},
    "GraphNode": {"type":"object","properties":{"node_id":{"$ref":"#/$defs/ID"},"ref":{"$ref":"#/$defs/GraphRef"},"kind":{"$ref":"#/$defs/ID"},"label":{"type":"string","minLength":0,"maxLength":512},"plane":{"enum":["intended","observed_static","observed_runtime","annotation"]},"evidence_refs":{"type":"array","items":{"$ref":"#/$defs/EvidenceRef"},"minItems":0,"maxItems":16},"source_refs":{"type":"array","items":{"$ref":"#/$defs/SourceRef"},"minItems":0,"maxItems":16},"expandable_count":{"anyOf":[{"type":"integer","minimum":0,"maximum":9007199254740991},{"type":"null"}]}},"required":["node_id","ref","kind","label","plane","evidence_refs","source_refs","expandable_count"],"additionalProperties":false},
    "GraphEdge": {"type":"object","properties":{"edge_id":{"$ref":"#/$defs/ID"},"from_node_id":{"$ref":"#/$defs/ID"},"to_node_id":{"$ref":"#/$defs/ID"},"relation_kind":{"$ref":"#/$defs/ID"},"resolution_status":{"enum":["exact_in_supported_scope","candidate","unresolved"]},"evidence_refs":{"type":"array","items":{"$ref":"#/$defs/EvidenceRef"},"minItems":0,"maxItems":16},"expand_ref":{"anyOf":[{"$ref":"#/$defs/Token"},{"type":"null"}]}},"required":["edge_id","from_node_id","to_node_id","relation_kind","resolution_status","evidence_refs","expand_ref"],"additionalProperties":false},
    "GraphQueryRequest": {"type":"object","properties":{"scope":{"$ref":"#/$defs/ScopeRequest"},"root":{"$ref":"#/$defs/Selector"},"depth":{"type":"integer","minimum":0,"maximum":8},"node_limit":{"type":"integer","minimum":1,"maximum":500},"edge_limit":{"type":"integer","minimum":1,"maximum":1500},"kinds":{"type":"array","items":{"$ref":"#/$defs/ID"},"minItems":0,"maxItems":32},"cursor":{"$ref":"#/$defs/Token"}},"required":["root"],"additionalProperties":false},
    "GraphData": {"type":"object","properties":{"operation":{"const":"graph_query"},"nodes":{"type":"array","items":{"$ref":"#/$defs/GraphNode"},"minItems":0,"maxItems":500},"edges":{"type":"array","items":{"$ref":"#/$defs/GraphEdge"},"minItems":0,"maxItems":1500},"subgraph_digest":{"$ref":"#/$defs/Hash"}},"required":["operation","nodes","edges","subgraph_digest"],"additionalProperties":false},
    "SourceReadRequest": {"type":"object","properties":{"source_ref":{"$ref":"#/$defs/SourceRef"},"max_output_bytes":{"type":"integer","minimum":4096,"maximum":131072}},"required":["source_ref"],"additionalProperties":false},
    "SourceData": {"type":"object","properties":{"operation":{"const":"source_read"},"source_ref":{"$ref":"#/$defs/SourceRef"},"content":{"type":"string","minLength":0,"maxLength":131072}},"required":["operation","source_ref","content"],"additionalProperties":false},
    "InstanceRef": {"type":"object","properties":{"session_id":{"$ref":"#/$defs/UUID"},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"instance_id":{"$ref":"#/$defs/ID"}},"required":["session_id","document_epoch","hmr_epoch","instance_id"],"additionalProperties":false},
    "Rect": {"type":"object","properties":{"x":{"type":"number"},"y":{"type":"number"},"width":{"type":"number","minimum":0},"height":{"type":"number","minimum":0}},"required":["x","y","width","height"],"additionalProperties":false},
    "MappingResolveRequest": {"type":"object","properties":{"scope":{"$ref":"#/$defs/ScopeRequest"},"session_id":{"$ref":"#/$defs/UUID"},"build_map_id":{"$ref":"#/$defs/Hash"},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"ref":{"oneOf":[{"$ref":"#/$defs/GraphRef"},{"$ref":"#/$defs/DOMAnchorRef"}]},"cursor":{"$ref":"#/$defs/Token"},"limit":{"type":"integer","minimum":1,"maximum":100},"max_output_bytes":{"type":"integer","minimum":4096,"maximum":131072}},"required":["session_id","build_map_id","document_epoch","hmr_epoch","ref"],"additionalProperties":false},
    "MappingData": {"type":"object","properties":{"operation":{"const":"mapping_resolve"},"requested_ref":{"oneOf":[{"$ref":"#/$defs/GraphRef"},{"$ref":"#/$defs/DOMAnchorRef"}]},"matches":{"type":"array","items":{"$ref":"#/$defs/MappingMatch"},"minItems":0,"maxItems":100},"build_map_id":{"$ref":"#/$defs/Hash"},"binding_id":{"$ref":"#/$defs/Hash"},"resolver_digest":{"$ref":"#/$defs/Hash"},"mapping_status":{"enum":["exact_in_supported_scope","candidate","ambiguous","ancestor_fallback","unmapped","stale","unsupported"]},"why_matched":{"type":"string","minLength":1,"maxLength":1024}},"required":["operation","requested_ref","matches","build_map_id","binding_id","resolver_digest","mapping_status","why_matched"],"additionalProperties":false},
    "FlowStep": {"type":"object","properties":{"step_id":{"$ref":"#/$defs/ID"},"canonical_refs":{"type":"array","items":{"$ref":"#/$defs/GraphRef"},"minItems":1,"maxItems":16},"source_refs":{"type":"array","items":{"$ref":"#/$defs/SourceRef"},"minItems":0,"maxItems":16},"step_kind":{"$ref":"#/$defs/ID"},"order_semantics":{"enum":["source_sequence","control_flow_supported","call_relationship_only","causal_trace","unknown"]},"possible_or_observed":{"enum":["statically_possible","observed"]},"outcome_status":{"enum":["declared_operation","statically_possible","attempt_observed","statement_success_observed","commit_observed","rollback_observed","outcome_unknown"]},"evidence_refs":{"type":"array","items":{"$ref":"#/$defs/EvidenceRef"},"minItems":0,"maxItems":16},"limitations":{"type":"array","items":{"$ref":"#/$defs/Gap"},"minItems":0,"maxItems":16}},"required":["step_id","canonical_refs","source_refs","step_kind","order_semantics","possible_or_observed","outcome_status","evidence_refs","limitations"],"additionalProperties":false},
    "FlowEdge": {"type":"object","properties":{"edge_id":{"$ref":"#/$defs/ID"},"from_step_id":{"$ref":"#/$defs/ID"},"to_step_id":{"$ref":"#/$defs/ID"},"kind":{"enum":["call","control","return","async","contract","transaction_link","candidate"]},"condition_source_ref":{"anyOf":[{"$ref":"#/$defs/SourceRef"},{"type":"null"}]},"evidence_refs":{"type":"array","items":{"$ref":"#/$defs/EvidenceRef"},"minItems":0,"maxItems":16}},"required":["edge_id","from_step_id","to_step_id","kind","condition_source_ref","evidence_refs"],"additionalProperties":false},
    "FlowQueryRequest": {"type":"object","properties":{"scope":{"$ref":"#/$defs/ScopeRequest"},"root_ref":{"$ref":"#/$defs/GraphRef"},"mode":{"enum":["static","observed"]},"artifact_id":{"$ref":"#/$defs/Hash"},"step_limit":{"type":"integer","minimum":1,"maximum":100},"edge_limit":{"type":"integer","minimum":1,"maximum":300},"cursor":{"$ref":"#/$defs/Token"}},"required":["root_ref"],"additionalProperties":false},
    "FlowData": {"type":"object","properties":{"operation":{"const":"flow_query"},"projection_id":{"$ref":"#/$defs/Hash"},"root_ref":{"$ref":"#/$defs/GraphRef"},"mode":{"enum":["static","observed"]},"steps":{"type":"array","items":{"$ref":"#/$defs/FlowStep"},"minItems":0,"maxItems":100},"edges":{"type":"array","items":{"$ref":"#/$defs/FlowEdge"},"minItems":0,"maxItems":300}},"required":["operation","projection_id","root_ref","mode","steps","edges"],"additionalProperties":false},
    "BootstrapRequest": {"type":"object","properties":{"code":{"type":"string","minLength":1,"maxLength":64}},"required":["code"],"additionalProperties":false},
    "BootstrapData": {"type":"object","properties":{"operation":{"const":"bootstrap"},"bearer":{"type":"string","minLength":1,"maxLength":64},"expires_at":{"$ref":"#/$defs/Time"},"capabilities":{"type":"array","items":{"$ref":"#/$defs/ID"},"minItems":0,"maxItems":64}},"required":["operation","bearer","expires_at","capabilities"],"additionalProperties":false},
    "PreviewAttachRequest": {"type":"object","properties":{"scope":{"$ref":"#/$defs/ScopeRequest"},"origin":{"type":"string","minLength":1,"maxLength":2048},"path":{"type":"string","minLength":1,"maxLength":2048},"build_map_id":{"$ref":"#/$defs/Hash"},"permission_profile_id":{"$ref":"#/$defs/ID"}},"required":["origin","path","build_map_id","permission_profile_id"],"additionalProperties":false},
    "PreviewBindRequest": {"type":"object","properties":{"session_id":{"$ref":"#/$defs/UUID"},"expected_revision":{"type":"integer","minimum":1,"maximum":9007199254740991},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"build_map_id":{"$ref":"#/$defs/Hash"},"pairing_nonce":{"type":"string","minLength":1,"maxLength":64},"capabilities":{"type":"array","items":{"$ref":"#/$defs/ID"},"minItems":0,"maxItems":64}},"required":["session_id","expected_revision","document_epoch","hmr_epoch","build_map_id","pairing_nonce","capabilities"],"additionalProperties":false},
    "PreviewModeRequest": {"type":"object","properties":{"session_id":{"$ref":"#/$defs/UUID"},"expected_revision":{"type":"integer","minimum":1,"maximum":9007199254740991},"expected_authorization_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"mode":{"enum":["inspect","interact"]},"permission_profile_id":{"$ref":"#/$defs/ID"}},"required":["session_id","expected_revision","expected_authorization_epoch","mode","permission_profile_id"],"additionalProperties":false},
    "PreviewAckRequest": {"type":"object","properties":{"session_id":{"$ref":"#/$defs/UUID"},"expected_revision":{"type":"integer","minimum":1,"maximum":9007199254740991},"authorization_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"command_id":{"$ref":"#/$defs/UUID"},"acknowledged":{"type":"boolean"}},"required":["session_id","expected_revision","authorization_epoch","command_id","acknowledged"],"additionalProperties":false},
    "PreviewInvalidateRequest": {"type":"object","properties":{"session_id":{"$ref":"#/$defs/UUID"},"expected_revision":{"type":"integer","minimum":1,"maximum":9007199254740991},"reason":{"enum":["hmr","navigation","heartbeat_lost","policy_changed","build_changed","client_error"]}},"required":["session_id","expected_revision","reason"],"additionalProperties":false},
    "PreviewDetachRequest": {"type":"object","properties":{"session_id":{"$ref":"#/$defs/UUID"}},"required":["session_id"],"additionalProperties":false},
    "PreviewData": {"type":"object","properties":{"operation":{"enum":["preview_attach","preview_bind","preview_mode","preview_ack","preview_invalidate","preview_detach"]},"session_id":{"$ref":"#/$defs/UUID"},"revision":{"type":"integer","minimum":1,"maximum":9007199254740991},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"authorization_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"state":{"enum":["pairing","bound","stale","detached","blocked"]},"mode_state":{"enum":["inspect","arming","interact","disarming"]},"pairing_nonce":{"anyOf":[{"type":"string","minLength":1,"maxLength":64},{"type":"null"}]},"command_id":{"anyOf":[{"$ref":"#/$defs/UUID"},{"type":"null"}]},"expires_at":{"$ref":"#/$defs/Time"},"binding_id":{"anyOf":[{"$ref":"#/$defs/Hash"},{"type":"null"}]},"permission_version":{"type":"integer","minimum":1,"maximum":9007199254740991},"resolver_digest":{"anyOf":[{"$ref":"#/$defs/Hash"},{"type":"null"}]}},"required":["operation","session_id","revision","document_epoch","hmr_epoch","authorization_epoch","state","mode_state","pairing_nonce","command_id","expires_at","binding_id","permission_version","resolver_digest"],"additionalProperties":false},
    "MappingProposeRequest": {"type":"object","properties":{"scope":{"$ref":"#/$defs/ScopeRequest"},"source_ref":{"$ref":"#/$defs/SourceRef"},"destination_ref":{"$ref":"#/$defs/GraphRef"},"rationale":{"type":"string","minLength":1,"maxLength":2048},"client_mutation_id":{"$ref":"#/$defs/UUID"}},"required":["source_ref","destination_ref","rationale","client_mutation_id"],"additionalProperties":false},
    "MappingReviewRequest": {"type":"object","properties":{"scope":{"$ref":"#/$defs/ScopeRequest"},"record_id":{"$ref":"#/$defs/UUID"},"expected_version":{"type":"integer","minimum":1,"maximum":9007199254740991},"action":{"enum":["approve","revoke","supersede"]},"reason":{"type":"string","minLength":1,"maxLength":2048},"client_mutation_id":{"$ref":"#/$defs/UUID"}},"required":["record_id","expected_version","action","reason","client_mutation_id"],"additionalProperties":false},
    "RecordData": {"type":"object","properties":{"operation":{"enum":["mapping_propose","mapping_review"]},"record_id":{"$ref":"#/$defs/UUID"},"version":{"type":"integer","minimum":1,"maximum":9007199254740991},"record_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"declared_status":{"enum":["proposed","approved","revoked","superseded"]},"effective_status":{"enum":["proposed","approved","revoked","superseded","needs_review"]}},"required":["operation","record_id","version","record_epoch","declared_status","effective_status"],"additionalProperties":false},
    "ResponseData": {"oneOf":[{"$ref":"#/$defs/StatusData"},{"$ref":"#/$defs/LocateData"},{"$ref":"#/$defs/PackData"},{"$ref":"#/$defs/ValidateData"},{"$ref":"#/$defs/RenameData"},{"$ref":"#/$defs/GraphData"},{"$ref":"#/$defs/SourceData"},{"$ref":"#/$defs/MappingData"},{"$ref":"#/$defs/FlowData"},{"$ref":"#/$defs/BootstrapData"},{"$ref":"#/$defs/PreviewData"},{"$ref":"#/$defs/RecordData"},{"$ref":"#/$defs/PreviewStatusData"},{"$ref":"#/$defs/PreviewPermissionAdminData"},{"$ref":"#/$defs/GroupAdminData"},{"$ref":"#/$defs/RuntimeConfigData"},{"$ref":"#/$defs/RecordListData"},{"$ref":"#/$defs/RecordReadData"},{"$ref":"#/$defs/ProfileAdminData"},{"$ref":"#/$defs/PolicyAdminData"}]},
    "Response": {"type":"object","properties":{"schema_version":{"const":1},"request_id":{"$ref":"#/$defs/UUID"},"status":{"enum":["ok","partial","blocked","error"]},"scope":{"anyOf":[{"$ref":"#/$defs/ScopeVector"},{"type":"null"}]},"freshness":{"$ref":"#/$defs/Freshness"},"coverage":{"$ref":"#/$defs/Coverage"},"readiness":{"enum":["reviewable","needs_context","blocked"]},"data":{"anyOf":[{"$ref":"#/$defs/ResponseData"},{"type":"null"}]},"pagination":{"$ref":"#/$defs/Pagination"},"budget":{"$ref":"#/$defs/Budget"},"warnings":{"type":"array","items":{"$ref":"#/$defs/Gap"},"minItems":0,"maxItems":64},"error":{"anyOf":[{"$ref":"#/$defs/Error"},{"type":"null"}]}},"required":["schema_version","request_id","status","scope","freshness","coverage","readiness","data","pagination","budget","warnings","error"],"additionalProperties":false},
    "ManifestSource": {"type":"object","properties":{"path":{"$ref":"#/$defs/Path"},"content_hash":{"$ref":"#/$defs/Hash"}},"required":["path","content_hash"],"additionalProperties":false},
    "AnchorEntry": {"type":"object","properties":{"anchor_id":{"$ref":"#/$defs/Hash"},"kind":{"enum":["element_site","component_definition","component_render_site","event_binding_site"]},"path":{"$ref":"#/$defs/Path"},"content_hash":{"$ref":"#/$defs/Hash"},"range":{"$ref":"#/$defs/ByteRange"},"owner_anchor_id":{"anyOf":[{"$ref":"#/$defs/Hash"},{"type":"null"}]}},"required":["anchor_id","kind","path","content_hash","range","owner_anchor_id"],"additionalProperties":false},
    "BuildManifest": {"type":"object","properties":{"schema_version":{"const":1},"worktree_id":{"$ref":"#/$defs/UUID"},"profile_id":{"$ref":"#/$defs/Hash"},"build_map_id":{"$ref":"#/$defs/Hash"},"provider_version":{"type":"string","minLength":1,"maxLength":128},"build_config_digest":{"$ref":"#/$defs/Hash"},"input_manifest_digest":{"$ref":"#/$defs/Hash"},"mode":{"enum":["development","test"]},"module_coverage":{"enum":["all_declared_modules","loaded_modules_only","partial"]},"sources":{"type":"array","items":{"$ref":"#/$defs/ManifestSource"},"minItems":0,"maxItems":100000},"anchors":{"type":"array","items":{"$ref":"#/$defs/AnchorEntry"},"minItems":0,"maxItems":100000},"provider_digest":{"$ref":"#/$defs/Hash"}},"required":["schema_version","worktree_id","profile_id","build_map_id","provider_version","build_config_digest","input_manifest_digest","mode","module_coverage","sources","anchors","provider_digest"],"additionalProperties":false},
    "Bridge_CHALLENGE": {"type":"object","properties":{"protocol_version":{"const":1},"session_id":{"$ref":"#/$defs/UUID"},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"message_id":{"$ref":"#/$defs/UUID"},"reply_to":{"anyOf":[{"$ref":"#/$defs/UUID"},{"type":"null"}]},"selection_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"seq":{"type":"integer","minimum":1,"maximum":9007199254740991},"type":{"const":"CHALLENGE"},"payload":{"type":"object","properties":{"nonce":{"type":"string","minLength":1,"maxLength":64},"approved_origin":{"type":"string","minLength":1,"maxLength":2048}},"required":["nonce","approved_origin"],"additionalProperties":false}},"required":["protocol_version","session_id","document_epoch","hmr_epoch","message_id","reply_to","selection_id","seq","type","payload"],"additionalProperties":false},
    "Bridge_READY": {"type":"object","properties":{"protocol_version":{"const":1},"session_id":{"$ref":"#/$defs/UUID"},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"message_id":{"$ref":"#/$defs/UUID"},"reply_to":{"anyOf":[{"$ref":"#/$defs/UUID"},{"type":"null"}]},"selection_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"seq":{"type":"integer","minimum":1,"maximum":9007199254740991},"type":{"const":"READY"},"payload":{"type":"object","properties":{"nonce":{"type":"string","minLength":1,"maxLength":64},"build_map_id":{"$ref":"#/$defs/Hash"},"capabilities":{"type":"array","items":{"$ref":"#/$defs/ID"},"minItems":0,"maxItems":64}},"required":["nonce","build_map_id","capabilities"],"additionalProperties":false}},"required":["protocol_version","session_id","document_epoch","hmr_epoch","message_id","reply_to","selection_id","seq","type","payload"],"additionalProperties":false},
    "Bridge_BOUND": {"type":"object","properties":{"protocol_version":{"const":1},"session_id":{"$ref":"#/$defs/UUID"},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"message_id":{"$ref":"#/$defs/UUID"},"reply_to":{"anyOf":[{"$ref":"#/$defs/UUID"},{"type":"null"}]},"selection_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"seq":{"type":"integer","minimum":1,"maximum":9007199254740991},"type":{"const":"BOUND"},"payload":{"type":"object","properties":{"build_map_id":{"$ref":"#/$defs/Hash"},"authorization_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991}},"required":["build_map_id","authorization_epoch"],"additionalProperties":false}},"required":["protocol_version","session_id","document_epoch","hmr_epoch","message_id","reply_to","selection_id","seq","type","payload"],"additionalProperties":false},
    "Bridge_BOUND_ACK": {"type":"object","properties":{"protocol_version":{"const":1},"session_id":{"$ref":"#/$defs/UUID"},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"message_id":{"$ref":"#/$defs/UUID"},"reply_to":{"anyOf":[{"$ref":"#/$defs/UUID"},{"type":"null"}]},"selection_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"seq":{"type":"integer","minimum":1,"maximum":9007199254740991},"type":{"const":"BOUND_ACK"},"payload":{"type":"object","properties":{"build_map_id":{"$ref":"#/$defs/Hash"},"authorization_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991}},"required":["build_map_id","authorization_epoch"],"additionalProperties":false}},"required":["protocol_version","session_id","document_epoch","hmr_epoch","message_id","reply_to","selection_id","seq","type","payload"],"additionalProperties":false},
    "Bridge_PICK_AT": {"type":"object","properties":{"protocol_version":{"const":1},"session_id":{"$ref":"#/$defs/UUID"},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"message_id":{"$ref":"#/$defs/UUID"},"reply_to":{"anyOf":[{"$ref":"#/$defs/UUID"},{"type":"null"}]},"selection_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"seq":{"type":"integer","minimum":1,"maximum":9007199254740991},"type":{"const":"PICK_AT"},"payload":{"type":"object","properties":{"x_css":{"type":"number"},"y_css":{"type":"number"},"selection_kind":{"enum":["element","component"]}},"required":["x_css","y_css","selection_kind"],"additionalProperties":false}},"required":["protocol_version","session_id","document_epoch","hmr_epoch","message_id","reply_to","selection_id","seq","type","payload"],"additionalProperties":false},
    "Bridge_PICK_RESULT": {"type":"object","properties":{"protocol_version":{"const":1},"session_id":{"$ref":"#/$defs/UUID"},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"message_id":{"$ref":"#/$defs/UUID"},"reply_to":{"anyOf":[{"$ref":"#/$defs/UUID"},{"type":"null"}]},"selection_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"seq":{"type":"integer","minimum":1,"maximum":9007199254740991},"type":{"const":"PICK_RESULT"},"payload":{"type":"object","properties":{"targets":{"type":"array","items":{"type":"object","properties":{"instance_ref":{"$ref":"#/$defs/InstanceRef"},"anchor_ref":{"$ref":"#/$defs/DOMAnchorRef"},"rects":{"type":"array","items":{"$ref":"#/$defs/Rect"},"minItems":0,"maxItems":16},"parent_instance_ref":{"anyOf":[{"$ref":"#/$defs/InstanceRef"},{"type":"null"}]}},"required":["instance_ref","anchor_ref","rects","parent_instance_ref"],"additionalProperties":false},"minItems":0,"maxItems":32},"status":{"enum":["exact_in_supported_scope","ancestor_fallback","unmapped","unsupported"]}},"required":["targets","status"],"additionalProperties":false}},"required":["protocol_version","session_id","document_epoch","hmr_epoch","message_id","reply_to","selection_id","seq","type","payload"],"additionalProperties":false},
    "Bridge_LIST_INSTANCES": {"type":"object","properties":{"protocol_version":{"const":1},"session_id":{"$ref":"#/$defs/UUID"},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"message_id":{"$ref":"#/$defs/UUID"},"reply_to":{"anyOf":[{"$ref":"#/$defs/UUID"},{"type":"null"}]},"selection_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"seq":{"type":"integer","minimum":1,"maximum":9007199254740991},"type":{"const":"LIST_INSTANCES"},"payload":{"type":"object","properties":{"anchor_refs":{"type":"array","items":{"$ref":"#/$defs/DOMAnchorRef"},"minItems":0,"maxItems":100},"limit":{"type":"integer","minimum":1,"maximum":100},"cursor":{"anyOf":[{"type":"string","minLength":1,"maxLength":1024},{"type":"null"}]}},"required":["anchor_refs","limit","cursor"],"additionalProperties":false}},"required":["protocol_version","session_id","document_epoch","hmr_epoch","message_id","reply_to","selection_id","seq","type","payload"],"additionalProperties":false},
    "Bridge_INSTANCES": {"type":"object","properties":{"protocol_version":{"const":1},"session_id":{"$ref":"#/$defs/UUID"},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"message_id":{"$ref":"#/$defs/UUID"},"reply_to":{"anyOf":[{"$ref":"#/$defs/UUID"},{"type":"null"}]},"selection_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"seq":{"type":"integer","minimum":1,"maximum":9007199254740991},"type":{"const":"INSTANCES"},"payload":{"type":"object","properties":{"items":{"type":"array","items":{"type":"object","properties":{"instance_ref":{"$ref":"#/$defs/InstanceRef"},"anchor_ref":{"$ref":"#/$defs/DOMAnchorRef"},"visibility":{"enum":["visible","mounted_offscreen","hidden","not_rendered","detached","unknown"]},"rects":{"type":"array","items":{"$ref":"#/$defs/Rect"},"minItems":0,"maxItems":16}},"required":["instance_ref","anchor_ref","visibility","rects"],"additionalProperties":false},"minItems":0,"maxItems":100},"next_cursor":{"anyOf":[{"type":"string","minLength":1,"maxLength":1024},{"type":"null"}]},"pending":{"anyOf":[{"type":"integer","minimum":0,"maximum":9007199254740991},{"type":"null"}]}},"required":["items","next_cursor","pending"],"additionalProperties":false}},"required":["protocol_version","session_id","document_epoch","hmr_epoch","message_id","reply_to","selection_id","seq","type","payload"],"additionalProperties":false},
    "Bridge_HIGHLIGHT": {"type":"object","properties":{"protocol_version":{"const":1},"session_id":{"$ref":"#/$defs/UUID"},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"message_id":{"$ref":"#/$defs/UUID"},"reply_to":{"anyOf":[{"$ref":"#/$defs/UUID"},{"type":"null"}]},"selection_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"seq":{"type":"integer","minimum":1,"maximum":9007199254740991},"type":{"const":"HIGHLIGHT"},"payload":{"type":"object","properties":{"instance_refs":{"type":"array","items":{"$ref":"#/$defs/InstanceRef"},"minItems":0,"maxItems":100}},"required":["instance_refs"],"additionalProperties":false}},"required":["protocol_version","session_id","document_epoch","hmr_epoch","message_id","reply_to","selection_id","seq","type","payload"],"additionalProperties":false},
    "Bridge_HIGHLIGHT_RESULT": {"type":"object","properties":{"protocol_version":{"const":1},"session_id":{"$ref":"#/$defs/UUID"},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"message_id":{"$ref":"#/$defs/UUID"},"reply_to":{"anyOf":[{"$ref":"#/$defs/UUID"},{"type":"null"}]},"selection_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"seq":{"type":"integer","minimum":1,"maximum":9007199254740991},"type":{"const":"HIGHLIGHT_RESULT"},"payload":{"type":"object","properties":{"matched":{"type":"array","items":{"$ref":"#/$defs/InstanceRef"},"minItems":0,"maxItems":100},"unavailable":{"type":"array","items":{"$ref":"#/$defs/InstanceRef"},"minItems":0,"maxItems":100}},"required":["matched","unavailable"],"additionalProperties":false}},"required":["protocol_version","session_id","document_epoch","hmr_epoch","message_id","reply_to","selection_id","seq","type","payload"],"additionalProperties":false},
    "Bridge_CLEAR_HIGHLIGHT": {"type":"object","properties":{"protocol_version":{"const":1},"session_id":{"$ref":"#/$defs/UUID"},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"message_id":{"$ref":"#/$defs/UUID"},"reply_to":{"anyOf":[{"$ref":"#/$defs/UUID"},{"type":"null"}]},"selection_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"seq":{"type":"integer","minimum":1,"maximum":9007199254740991},"type":{"const":"CLEAR_HIGHLIGHT"},"payload":{"type":"object","properties":{},"required":[],"additionalProperties":false}},"required":["protocol_version","session_id","document_epoch","hmr_epoch","message_id","reply_to","selection_id","seq","type","payload"],"additionalProperties":false},
    "Bridge_REQUEST_SCROLL": {"type":"object","properties":{"protocol_version":{"const":1},"session_id":{"$ref":"#/$defs/UUID"},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"message_id":{"$ref":"#/$defs/UUID"},"reply_to":{"anyOf":[{"$ref":"#/$defs/UUID"},{"type":"null"}]},"selection_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"seq":{"type":"integer","minimum":1,"maximum":9007199254740991},"type":{"const":"REQUEST_SCROLL"},"payload":{"type":"object","properties":{"instance_ref":{"$ref":"#/$defs/InstanceRef"}},"required":["instance_ref"],"additionalProperties":false}},"required":["protocol_version","session_id","document_epoch","hmr_epoch","message_id","reply_to","selection_id","seq","type","payload"],"additionalProperties":false},
    "Bridge_SCROLL_RESULT": {"type":"object","properties":{"protocol_version":{"const":1},"session_id":{"$ref":"#/$defs/UUID"},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"message_id":{"$ref":"#/$defs/UUID"},"reply_to":{"anyOf":[{"$ref":"#/$defs/UUID"},{"type":"null"}]},"selection_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"seq":{"type":"integer","minimum":1,"maximum":9007199254740991},"type":{"const":"SCROLL_RESULT"},"payload":{"type":"object","properties":{"status":{"enum":["done","not_permitted","not_rendered"]}},"required":["status"],"additionalProperties":false}},"required":["protocol_version","session_id","document_epoch","hmr_epoch","message_id","reply_to","selection_id","seq","type","payload"],"additionalProperties":false},
    "Bridge_SET_MODE": {"type":"object","properties":{"protocol_version":{"const":1},"session_id":{"$ref":"#/$defs/UUID"},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"message_id":{"$ref":"#/$defs/UUID"},"reply_to":{"anyOf":[{"$ref":"#/$defs/UUID"},{"type":"null"}]},"selection_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"seq":{"type":"integer","minimum":1,"maximum":9007199254740991},"type":{"const":"SET_MODE"},"payload":{"type":"object","properties":{"mode":{"enum":["inspect","interact"]},"authorization_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"command_id":{"$ref":"#/$defs/UUID"}},"required":["mode","authorization_epoch","command_id"],"additionalProperties":false}},"required":["protocol_version","session_id","document_epoch","hmr_epoch","message_id","reply_to","selection_id","seq","type","payload"],"additionalProperties":false},
    "Bridge_MODE_ACK": {"type":"object","properties":{"protocol_version":{"const":1},"session_id":{"$ref":"#/$defs/UUID"},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"message_id":{"$ref":"#/$defs/UUID"},"reply_to":{"anyOf":[{"$ref":"#/$defs/UUID"},{"type":"null"}]},"selection_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"seq":{"type":"integer","minimum":1,"maximum":9007199254740991},"type":{"const":"MODE_ACK"},"payload":{"type":"object","properties":{"mode":{"enum":["inspect","interact"]},"authorization_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"command_id":{"$ref":"#/$defs/UUID"},"accepted":{"type":"boolean"}},"required":["mode","authorization_epoch","command_id","accepted"],"additionalProperties":false}},"required":["protocol_version","session_id","document_epoch","hmr_epoch","message_id","reply_to","selection_id","seq","type","payload"],"additionalProperties":false},
    "Bridge_BUILD_CHANGED": {"type":"object","properties":{"protocol_version":{"const":1},"session_id":{"$ref":"#/$defs/UUID"},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"message_id":{"$ref":"#/$defs/UUID"},"reply_to":{"anyOf":[{"$ref":"#/$defs/UUID"},{"type":"null"}]},"selection_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"seq":{"type":"integer","minimum":1,"maximum":9007199254740991},"type":{"const":"BUILD_CHANGED"},"payload":{"type":"object","properties":{"build_map_id":{"$ref":"#/$defs/Hash"},"new_hmr_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991}},"required":["build_map_id","new_hmr_epoch"],"additionalProperties":false}},"required":["protocol_version","session_id","document_epoch","hmr_epoch","message_id","reply_to","selection_id","seq","type","payload"],"additionalProperties":false},
    "Bridge_INSTANCE_INVALIDATED": {"type":"object","properties":{"protocol_version":{"const":1},"session_id":{"$ref":"#/$defs/UUID"},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"message_id":{"$ref":"#/$defs/UUID"},"reply_to":{"anyOf":[{"$ref":"#/$defs/UUID"},{"type":"null"}]},"selection_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"seq":{"type":"integer","minimum":1,"maximum":9007199254740991},"type":{"const":"INSTANCE_INVALIDATED"},"payload":{"type":"object","properties":{"instance_refs":{"type":"array","items":{"$ref":"#/$defs/InstanceRef"},"minItems":0,"maxItems":100}},"required":["instance_refs"],"additionalProperties":false}},"required":["protocol_version","session_id","document_epoch","hmr_epoch","message_id","reply_to","selection_id","seq","type","payload"],"additionalProperties":false},
    "Bridge_CAPABILITY_STATUS": {"type":"object","properties":{"protocol_version":{"const":1},"session_id":{"$ref":"#/$defs/UUID"},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"message_id":{"$ref":"#/$defs/UUID"},"reply_to":{"anyOf":[{"$ref":"#/$defs/UUID"},{"type":"null"}]},"selection_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"seq":{"type":"integer","minimum":1,"maximum":9007199254740991},"type":{"const":"CAPABILITY_STATUS"},"payload":{"type":"object","properties":{"capabilities":{"type":"array","items":{"$ref":"#/$defs/ID"},"minItems":0,"maxItems":64}},"required":["capabilities"],"additionalProperties":false}},"required":["protocol_version","session_id","document_epoch","hmr_epoch","message_id","reply_to","selection_id","seq","type","payload"],"additionalProperties":false},
    "Bridge_ERROR": {"type":"object","properties":{"protocol_version":{"const":1},"session_id":{"$ref":"#/$defs/UUID"},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"message_id":{"$ref":"#/$defs/UUID"},"reply_to":{"anyOf":[{"$ref":"#/$defs/UUID"},{"type":"null"}]},"selection_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"seq":{"type":"integer","minimum":1,"maximum":9007199254740991},"type":{"const":"ERROR"},"payload":{"type":"object","properties":{"code":{"$ref":"#/$defs/ID"},"reason":{"type":"string","minLength":1,"maxLength":512}},"required":["code","reason"],"additionalProperties":false}},"required":["protocol_version","session_id","document_epoch","hmr_epoch","message_id","reply_to","selection_id","seq","type","payload"],"additionalProperties":false},
    "Bridge_DISPOSE": {"type":"object","properties":{"protocol_version":{"const":1},"session_id":{"$ref":"#/$defs/UUID"},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"message_id":{"$ref":"#/$defs/UUID"},"reply_to":{"anyOf":[{"$ref":"#/$defs/UUID"},{"type":"null"}]},"selection_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"seq":{"type":"integer","minimum":1,"maximum":9007199254740991},"type":{"const":"DISPOSE"},"payload":{"type":"object","properties":{},"required":[],"additionalProperties":false}},"required":["protocol_version","session_id","document_epoch","hmr_epoch","message_id","reply_to","selection_id","seq","type","payload"],"additionalProperties":false},
    "Bridge_DISPOSED": {"type":"object","properties":{"protocol_version":{"const":1},"session_id":{"$ref":"#/$defs/UUID"},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"message_id":{"$ref":"#/$defs/UUID"},"reply_to":{"anyOf":[{"$ref":"#/$defs/UUID"},{"type":"null"}]},"selection_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"seq":{"type":"integer","minimum":1,"maximum":9007199254740991},"type":{"const":"DISPOSED"},"payload":{"type":"object","properties":{},"required":[],"additionalProperties":false}},"required":["protocol_version","session_id","document_epoch","hmr_epoch","message_id","reply_to","selection_id","seq","type","payload"],"additionalProperties":false},
    "BridgeMessage": {"oneOf":[{"$ref":"#/$defs/Bridge_CHALLENGE"},{"$ref":"#/$defs/Bridge_READY"},{"$ref":"#/$defs/Bridge_BOUND"},{"$ref":"#/$defs/Bridge_BOUND_ACK"},{"$ref":"#/$defs/Bridge_PICK_AT"},{"$ref":"#/$defs/Bridge_PICK_RESULT"},{"$ref":"#/$defs/Bridge_LIST_INSTANCES"},{"$ref":"#/$defs/Bridge_INSTANCES"},{"$ref":"#/$defs/Bridge_HIGHLIGHT"},{"$ref":"#/$defs/Bridge_HIGHLIGHT_RESULT"},{"$ref":"#/$defs/Bridge_CLEAR_HIGHLIGHT"},{"$ref":"#/$defs/Bridge_REQUEST_SCROLL"},{"$ref":"#/$defs/Bridge_SCROLL_RESULT"},{"$ref":"#/$defs/Bridge_SET_MODE"},{"$ref":"#/$defs/Bridge_MODE_ACK"},{"$ref":"#/$defs/Bridge_BUILD_CHANGED"},{"$ref":"#/$defs/Bridge_INSTANCE_INVALIDATED"},{"$ref":"#/$defs/Bridge_CAPABILITY_STATUS"},{"$ref":"#/$defs/Bridge_ERROR"},{"$ref":"#/$defs/Bridge_DISPOSE"},{"$ref":"#/$defs/Bridge_DISPOSED"},{"$ref":"#/$defs/Bridge_PING"},{"$ref":"#/$defs/Bridge_PONG"}]},
    "PolicyConfig": {"type":"object","properties":{"schema_version":{"const":1},"allowed_roots":{"type":"array","items":{"type":"string","minLength":1,"maxLength":4096},"minItems":1,"maxItems":32},"denied_globs":{"type":"array","items":{"type":"string","minLength":1,"maxLength":512},"minItems":0,"maxItems":256},"trusted_rule_paths":{"type":"array","items":{"$ref":"#/$defs/Path"},"minItems":0,"maxItems":128},"dependency_roots":{"type":"array","items":{"type":"string","minLength":1,"maxLength":4096},"minItems":0,"maxItems":128},"allowed_executables":{"type":"array","items":{"type":"object","properties":{"path":{"type":"string","minLength":1,"maxLength":4096},"sha256":{"$ref":"#/$defs/Hash"}},"required":["path","sha256"],"additionalProperties":false},"minItems":0,"maxItems":32},"allow_target_execution":{"const":false},"allow_runtime_download":{"const":false},"max_file_bytes":{"type":"integer","minimum":1,"maximum":9007199254740991},"max_source_bytes":{"type":"integer","minimum":1,"maximum":9007199254740991},"max_output_bytes":{"type":"integer","minimum":4096,"maximum":131072},"secret_rules_version":{"$ref":"#/$defs/ID"}},"required":["schema_version","allowed_roots","denied_globs","trusted_rule_paths","dependency_roots","allowed_executables","allow_target_execution","allow_runtime_download","max_file_bytes","max_source_bytes","max_output_bytes","secret_rules_version"],"additionalProperties":false},
    "GoProfile": {"type":"object","properties":{"schema_version":{"const":1},"language":{"const":"go"},"goos":{"type":"string","minLength":1,"maxLength":32},"goarch":{"type":"string","minLength":1,"maxLength":32},"build_tags":{"type":"array","items":{"type":"string","minLength":1,"maxLength":128},"minItems":0,"maxItems":128},"cgo_enabled":{"type":"boolean"},"tests_enabled":{"type":"boolean"},"module_roots":{"type":"array","items":{"$ref":"#/$defs/Path"},"minItems":1,"maxItems":64},"toolchain_digest":{"$ref":"#/$defs/Hash"},"provider_digest":{"$ref":"#/$defs/Hash"},"module_mode":{"enum":["readonly","vendor_explicit"]},"analysis_mode":{"enum":["syntax","typed_when_available"]},"dependency_roots":{"type":"array","items":{"type":"string","minLength":1,"maxLength":4096},"minItems":0,"maxItems":128}},"required":["schema_version","language","goos","goarch","build_tags","cgo_enabled","tests_enabled","module_roots","toolchain_digest","provider_digest","module_mode","analysis_mode","dependency_roots"],"additionalProperties":false},
    "TSProfile": {"type":"object","properties":{"schema_version":{"const":1},"language":{"const":"typescript_javascript"},"tsconfig_paths":{"type":"array","items":{"$ref":"#/$defs/Path"},"minItems":1,"maxItems":64},"compiler_digest":{"$ref":"#/$defs/Hash"},"node_digest":{"$ref":"#/$defs/Hash"},"provider_digest":{"$ref":"#/$defs/Hash"},"allow_project_plugins":{"const":false},"analysis_mode":{"enum":["syntax","typed_when_available"]}},"required":["schema_version","language","tsconfig_paths","compiler_digest","node_digest","provider_digest","allow_project_plugins","analysis_mode"],"additionalProperties":false},
    "EffectiveProfile": {"oneOf":[{"$ref":"#/$defs/GoProfile"},{"$ref":"#/$defs/TSProfile"}]},
    "ManifestFile": {"type":"object","properties":{"path":{"$ref":"#/$defs/Path"},"status":{"enum":["captured","metadata_only","excluded","missing","read_error"]},"content_hash":{"anyOf":[{"$ref":"#/$defs/Hash"},{"type":"null"}]},"byte_length":{"anyOf":[{"type":"integer","minimum":0,"maximum":9007199254740991},{"type":"null"}]},"reason_code":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]}},"required":["path","status","content_hash","byte_length","reason_code"],"additionalProperties":false},
    "CaptureManifest": {"type":"object","properties":{"schema_version":{"const":1},"worktree_id":{"$ref":"#/$defs/UUID"},"profile_id":{"$ref":"#/$defs/Hash"},"policy_digest":{"$ref":"#/$defs/Hash"},"provider_digest":{"$ref":"#/$defs/Hash"},"files":{"type":"array","items":{"$ref":"#/$defs/ManifestFile"},"minItems":0,"maxItems":100000},"dependency_input_digests":{"type":"array","items":{"$ref":"#/$defs/Hash"},"minItems":0,"maxItems":100000},"snapshot_digest":{"$ref":"#/$defs/Hash"}},"required":["schema_version","worktree_id","profile_id","policy_digest","provider_digest","files","dependency_input_digests","snapshot_digest"],"additionalProperties":false},
    "DOMAnchorRef": {"type":"object","properties":{"build_map_id":{"$ref":"#/$defs/Hash"},"anchor_id":{"$ref":"#/$defs/Hash"}},"required":["build_map_id","anchor_id"],"additionalProperties":false},
    "PreviewPermissionProfile": {"type":"object","properties":{"schema_version":{"const":1},"profile_id":{"$ref":"#/$defs/ID"},"approved_origin":{"type":"string","minLength":1,"maxLength":2048},"approved_path_prefix":{"type":"string","minLength":1,"maxLength":2048},"target_environment":{"enum":["isolated_test","local_mock"]},"source_worktree_ids":{"type":"array","items":{"$ref":"#/$defs/UUID"},"minItems":1,"maxItems":32},"allow_attach":{"type":"boolean"},"allow_interact":{"type":"boolean"},"allow_scroll":{"type":"boolean"},"allowed_dev_destinations":{"type":"array","items":{"type":"string","minLength":1,"maxLength":2048},"minItems":0,"maxItems":64},"attestation_id":{"$ref":"#/$defs/ID"},"attested_by":{"$ref":"#/$defs/ID"},"attested_at":{"$ref":"#/$defs/Time"},"expires_at":{"$ref":"#/$defs/Time"},"version":{"type":"integer","minimum":1,"maximum":9007199254740991}},"required":["schema_version","profile_id","approved_origin","approved_path_prefix","target_environment","source_worktree_ids","allow_attach","allow_interact","allow_scroll","allowed_dev_destinations","attestation_id","attested_by","attested_at","expires_at","version"],"additionalProperties":false},
    "SymbolFact": {"type":"object","properties":{"symbol_id":{"$ref":"#/$defs/ID"},"path":{"anyOf":[{"$ref":"#/$defs/Path"},{"type":"null"}]},"package_key":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"kind":{"enum":["file","package","function","method","type","interface","field","variable","parameter","receiver","result","constant","closure","external"]},"name":{"type":"string","minLength":0,"maxLength":512},"qualified_name":{"type":"string","minLength":0,"maxLength":2048},"scope_key":{"$ref":"#/$defs/ID"},"range":{"anyOf":[{"$ref":"#/$defs/ByteRange"},{"type":"null"}]},"signature":{"type":"string","minLength":0,"maxLength":32768},"exported":{"type":"boolean"}},"required":["symbol_id","path","package_key","kind","name","qualified_name","scope_key","range","signature","exported"],"additionalProperties":false},
    "RelationFact": {"type":"object","properties":{"relation_id":{"$ref":"#/$defs/ID"},"source_symbol_id":{"$ref":"#/$defs/ID"},"target_symbol_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"kind":{"enum":["CONTAINS","IMPORTS","REFERS_TO","READS","WRITES","CALLS_DIRECT","CALLS_INTERFACE","MAY_DISPATCH_TO","UNRESOLVED_REF","UNRESOLVED_CALL","TEST_REF","DOC_LINK"]},"evidence_kind":{"enum":["syntax_exact","type_resolved","type_possible","explicit_document","text_match","unresolved"]},"site_path":{"$ref":"#/$defs/Path"},"range":{"$ref":"#/$defs/ByteRange"},"reason":{"type":"string","minLength":0,"maxLength":1024},"unresolved_label":{"anyOf":[{"type":"string","minLength":1,"maxLength":512},{"type":"null"}]}},"required":["relation_id","source_symbol_id","target_symbol_id","kind","evidence_kind","site_path","range","reason","unresolved_label"],"additionalProperties":false},
    "PackageFact": {"type":"object","properties":{"package_key":{"$ref":"#/$defs/ID"},"import_path":{"type":"string","minLength":1,"maxLength":2048},"module_key":{"$ref":"#/$defs/ID"},"variant":{"enum":["production","internal_test","external_test","other_supported"]},"input_digest":{"$ref":"#/$defs/Hash"},"semantic_status":{"enum":["complete_in_profile","partial","unsupported","unknown"]}},"required":["package_key","import_path","module_key","variant","input_digest","semantic_status"],"additionalProperties":false},
    "DiagnosticFact": {"type":"object","properties":{"diagnostic_id":{"$ref":"#/$defs/ID"},"path":{"anyOf":[{"$ref":"#/$defs/Path"},{"type":"null"}]},"package_key":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"code":{"$ref":"#/$defs/ID"},"severity":{"enum":["info","warning","error"]},"message":{"type":"string","minLength":1,"maxLength":1024},"scope_description":{"type":"string","minLength":1,"maxLength":1024}},"required":["diagnostic_id","path","package_key","code","severity","message","scope_description"],"additionalProperties":false},
    "ProviderStart": {"type":"object","properties":{"provider_protocol":{"const":1},"type":{"const":"analyze_start"},"job_id":{"$ref":"#/$defs/UUID"},"provider_id":{"$ref":"#/$defs/ID"},"provider_digest":{"$ref":"#/$defs/Hash"},"profile_id":{"$ref":"#/$defs/Hash"},"generation_id":{"$ref":"#/$defs/UUID"},"snapshot_root":{"type":"string","minLength":1,"maxLength":4096},"manifest_path":{"type":"string","minLength":1,"maxLength":4096},"manifest_digest":{"$ref":"#/$defs/Hash"},"profile":{"$ref":"#/$defs/EffectiveProfile"},"max_frame_bytes":{"type":"integer","minimum":65536,"maximum":16777216},"deadline_at":{"$ref":"#/$defs/Time"}},"required":["provider_protocol","type","job_id","provider_id","provider_digest","profile_id","generation_id","snapshot_root","manifest_path","manifest_digest","profile","max_frame_bytes","deadline_at"],"additionalProperties":false},
    "ProviderBatch": {"type":"object","properties":{"provider_protocol":{"const":1},"type":{"const":"analyze_batch"},"job_id":{"$ref":"#/$defs/UUID"},"sequence":{"type":"integer","minimum":1,"maximum":9007199254740991},"packages":{"type":"array","items":{"$ref":"#/$defs/PackageFact"},"minItems":0,"maxItems":512},"symbols":{"type":"array","items":{"$ref":"#/$defs/SymbolFact"},"minItems":0,"maxItems":2048},"relations":{"type":"array","items":{"$ref":"#/$defs/RelationFact"},"minItems":0,"maxItems":4096},"diagnostics":{"type":"array","items":{"$ref":"#/$defs/DiagnosticFact"},"minItems":0,"maxItems":512}},"required":["provider_protocol","type","job_id","sequence","packages","symbols","relations","diagnostics"],"additionalProperties":false},
    "ProviderDone": {"type":"object","properties":{"provider_protocol":{"const":1},"type":{"const":"analyze_done"},"job_id":{"$ref":"#/$defs/UUID"},"sequence":{"type":"integer","minimum":1,"maximum":9007199254740991},"status":{"enum":["complete_in_supported_scope","partial","failed","cancelled"]},"input_digest":{"$ref":"#/$defs/Hash"},"capabilities":{"type":"array","items":{"$ref":"#/$defs/Capability"},"minItems":0,"maxItems":128},"gaps":{"type":"array","items":{"$ref":"#/$defs/Gap"},"minItems":0,"maxItems":64},"error":{"anyOf":[{"$ref":"#/$defs/Error"},{"type":"null"}]}},"required":["provider_protocol","type","job_id","sequence","status","input_digest","capabilities","gaps","error"],"additionalProperties":false},
    "ProviderCancel": {"type":"object","properties":{"provider_protocol":{"const":1},"type":{"const":"cancel"},"job_id":{"$ref":"#/$defs/UUID"}},"required":["provider_protocol","type","job_id"],"additionalProperties":false},
    "ProviderMessage": {"oneOf":[{"$ref":"#/$defs/ProviderStart"},{"$ref":"#/$defs/ProviderBatch"},{"$ref":"#/$defs/ProviderDone"},{"$ref":"#/$defs/ProviderCancel"}]},
    "FileRef": {"type":"object","properties":{"kind":{"const":"file"},"worktree_id":{"$ref":"#/$defs/UUID"},"profile_id":{"$ref":"#/$defs/Hash"},"generation_id":{"$ref":"#/$defs/UUID"},"path":{"$ref":"#/$defs/Path"},"content_hash":{"anyOf":[{"$ref":"#/$defs/Hash"},{"type":"null"}]}},"required":["kind","worktree_id","profile_id","generation_id","path","content_hash"],"additionalProperties":false},
    "MemberUnavailable": {"type":"object","properties":{"service_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"worktree_id":{"anyOf":[{"$ref":"#/$defs/UUID"},{"type":"null"}]},"profile_id":{"anyOf":[{"$ref":"#/$defs/Hash"},{"type":"null"}]},"status":{"enum":["index_missing","unavailable","access_denied","profile_missing"]},"reason_code":{"$ref":"#/$defs/ID"}},"required":["service_id","worktree_id","profile_id","status","reason_code"],"additionalProperties":false},
    "RefSelector": {"type":"object","properties":{"kind":{"const":"ref"},"ref":{"$ref":"#/$defs/GraphRef"},"service_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]}},"required":["kind","ref","service_id"],"additionalProperties":false},
    "RegisteredScopeStatus": {"type":"object","properties":{"worktree_id":{"$ref":"#/$defs/UUID"},"profile_id":{"anyOf":[{"$ref":"#/$defs/Hash"},{"type":"null"}]},"root_status":{"enum":["registered","unavailable","removed"]},"index_state":{"enum":["missing","published","building_with_previous","unavailable"]}},"required":["worktree_id","profile_id","root_status","index_state"],"additionalProperties":false},
    "PreviewStatusRequest": {"type":"object","properties":{"session_id":{"$ref":"#/$defs/UUID"}},"required":["session_id"],"additionalProperties":false},
    "PreviewStatusData": {"type":"object","properties":{"operation":{"const":"preview_status"},"session_id":{"$ref":"#/$defs/UUID"},"revision":{"type":"integer","minimum":1,"maximum":9007199254740991},"authorization_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"state":{"enum":["pairing","bound","stale","detached","blocked"]},"mode_state":{"enum":["inspect","arming","interact","disarming"]},"binding_id":{"anyOf":[{"$ref":"#/$defs/Hash"},{"type":"null"}]},"permission_profile_id":{"$ref":"#/$defs/ID"},"permission_version":{"type":"integer","minimum":1,"maximum":9007199254740991},"permission_status":{"enum":["approved","revoked","expired","unavailable"]},"expires_at":{"$ref":"#/$defs/Time"},"server_time":{"$ref":"#/$defs/Time"},"interaction_allowed":{"type":"boolean"}},"required":["operation","session_id","revision","authorization_epoch","document_epoch","hmr_epoch","state","mode_state","binding_id","permission_profile_id","permission_version","permission_status","expires_at","server_time","interaction_allowed"],"additionalProperties":false},
    "Bridge_PING": {"type":"object","properties":{"protocol_version":{"const":1},"session_id":{"$ref":"#/$defs/UUID"},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"message_id":{"$ref":"#/$defs/UUID"},"reply_to":{"anyOf":[{"$ref":"#/$defs/UUID"},{"type":"null"}]},"selection_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"seq":{"type":"integer","minimum":1,"maximum":9007199254740991},"type":{"const":"PING"},"payload":{"type":"object","properties":{"probe_id":{"$ref":"#/$defs/UUID"}},"required":["probe_id"],"additionalProperties":false}},"required":["protocol_version","session_id","document_epoch","hmr_epoch","message_id","reply_to","selection_id","seq","type","payload"],"additionalProperties":false},
    "Bridge_PONG": {"type":"object","properties":{"protocol_version":{"const":1},"session_id":{"$ref":"#/$defs/UUID"},"document_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991},"hmr_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"message_id":{"$ref":"#/$defs/UUID"},"reply_to":{"anyOf":[{"$ref":"#/$defs/UUID"},{"type":"null"}]},"selection_id":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]},"seq":{"type":"integer","minimum":1,"maximum":9007199254740991},"type":{"const":"PONG"},"payload":{"type":"object","properties":{"probe_id":{"$ref":"#/$defs/UUID"}},"required":["probe_id"],"additionalProperties":false}},"required":["protocol_version","session_id","document_epoch","hmr_epoch","message_id","reply_to","selection_id","seq","type","payload"],"additionalProperties":false},
    "MappingMatch": {"type":"object","properties":{"match_id":{"$ref":"#/$defs/Hash"},"anchor_ref":{"$ref":"#/$defs/AnchorRef"},"target_ref":{"anyOf":[{"$ref":"#/$defs/GraphRef"},{"type":"null"}]},"source_refs":{"type":"array","items":{"$ref":"#/$defs/SourceRef"},"minItems":0,"maxItems":16},"mapping_status":{"enum":["exact_in_supported_scope","candidate","ancestor_fallback","unmapped","unsupported"]},"evidence_refs":{"type":"array","items":{"$ref":"#/$defs/EvidenceRef"},"minItems":0,"maxItems":16}},"required":["match_id","anchor_ref","target_ref","source_refs","mapping_status","evidence_refs"],"additionalProperties":false},
    "RenameInitialRequest": {"type":"object","properties":{"scope":{"$ref":"#/$defs/ScopeRequest"},"selector":{"$ref":"#/$defs/PositionSelector"},"new_name":{"type":"string","minLength":1,"maxLength":256},"preserve_contracts":{"type":"boolean"},"max_output_bytes":{"type":"integer","minimum":4096,"maximum":131072}},"required":["selector","new_name"],"additionalProperties":false},
    "RenameContinueRequest": {"type":"object","properties":{"result_token":{"$ref":"#/$defs/Token"},"cursor":{"$ref":"#/$defs/Token"},"max_output_bytes":{"type":"integer","minimum":4096,"maximum":131072}},"required":["result_token","cursor"],"additionalProperties":false},
    "PreviewPermissionAdminRequest": {"type":"object","properties":{"action":{"enum":["get","list","put","revoke"]},"profile_id":{"$ref":"#/$defs/ID"},"expected_version":{"type":"integer","minimum":0,"maximum":9007199254740991},"profile":{"$ref":"#/$defs/PreviewPermissionProfile"},"reason":{"type":"string","minLength":1,"maxLength":2048},"limit":{"type":"integer","minimum":1,"maximum":100},"cursor":{"$ref":"#/$defs/Token"}},"required":["action"],"additionalProperties":false},
    "PreviewPermissionSummary": {"type":"object","properties":{"profile_id":{"$ref":"#/$defs/ID"},"version":{"type":"integer","minimum":1,"maximum":9007199254740991},"declared_status":{"enum":["approved","revoked"]},"effective_status":{"enum":["approved","revoked","expired"]},"digest":{"$ref":"#/$defs/Hash"},"profile":{"$ref":"#/$defs/PreviewPermissionProfile"}},"required":["profile_id","version","declared_status","effective_status","digest","profile"],"additionalProperties":false},
    "PreviewPermissionAdminData": {"type":"object","properties":{"operation":{"const":"preview_permission_admin"},"profiles":{"type":"array","items":{"$ref":"#/$defs/PreviewPermissionSummary"},"minItems":0,"maxItems":100}},"required":["operation","profiles"],"additionalProperties":false},
    "ServiceNamespace": {"type":"object","properties":{"environment":{"$ref":"#/$defs/ID"},"protocol":{"enum":["http","websocket","event","sql"]},"base_origin":{"anyOf":[{"type":"string","minLength":1,"maxLength":2048},{"type":"null"}]},"route_prefix":{"anyOf":[{"type":"string","minLength":1,"maxLength":1024},{"type":"null"}]},"contract_version":{"anyOf":[{"$ref":"#/$defs/ID"},{"type":"null"}]}},"required":["environment","protocol","base_origin","route_prefix","contract_version"],"additionalProperties":false},
    "GroupMemberConfig": {"type":"object","properties":{"service_id":{"$ref":"#/$defs/ID"},"worktree_id":{"$ref":"#/$defs/UUID"},"profile_id":{"$ref":"#/$defs/Hash"},"namespace":{"$ref":"#/$defs/ServiceNamespace"}},"required":["service_id","worktree_id","profile_id","namespace"],"additionalProperties":false},
    "GroupConfig": {"type":"object","properties":{"group_id":{"$ref":"#/$defs/UUID"},"name":{"type":"string","minLength":1,"maxLength":256},"members":{"type":"array","items":{"$ref":"#/$defs/GroupMemberConfig"},"minItems":1,"maxItems":32}},"required":["group_id","name","members"],"additionalProperties":false},
    "GroupAdminRequest": {"type":"object","properties":{"action":{"enum":["get","list","put","remove"]},"group_id":{"$ref":"#/$defs/UUID"},"expected_revision":{"type":"integer","minimum":0,"maximum":9007199254740991},"config":{"$ref":"#/$defs/GroupConfig"},"limit":{"type":"integer","minimum":1,"maximum":100},"cursor":{"$ref":"#/$defs/Token"}},"required":["action"],"additionalProperties":false},
    "GroupSummary": {"type":"object","properties":{"config":{"$ref":"#/$defs/GroupConfig"},"revision":{"type":"integer","minimum":1,"maximum":9007199254740991},"status":{"enum":["active","removed"]}},"required":["config","revision","status"],"additionalProperties":false},
    "GroupAdminData": {"type":"object","properties":{"operation":{"const":"group_admin"},"groups":{"type":"array","items":{"$ref":"#/$defs/GroupSummary"},"minItems":0,"maxItems":100}},"required":["operation","groups"],"additionalProperties":false},
    "CoreRuntimeLimits": {"type":"object","properties":{"max_file_bytes":{"type":"integer","minimum":1,"maximum":9007199254740991},"max_indexed_files":{"type":"integer","minimum":1,"maximum":100000},"max_source_bytes_per_snapshot":{"type":"integer","minimum":1,"maximum":9007199254740991},"max_parallel_index_jobs":{"type":"integer","minimum":1,"maximum":16},"max_parse_workers":{"type":"integer","minimum":0,"maximum":64},"max_lsp_sessions":{"type":"integer","minimum":1,"maximum":8},"sqlite_busy_timeout_ms":{"type":"integer","minimum":1,"maximum":9007199254740991},"writer_lock_wait_ms":{"type":"integer","minimum":1,"maximum":9007199254740991},"query_timeout_ms":{"type":"integer","minimum":1,"maximum":9007199254740991},"lsp_startup_timeout_ms":{"type":"integer","minimum":1,"maximum":9007199254740991},"rename_timeout_ms":{"type":"integer","minimum":1,"maximum":9007199254740991},"index_timeout_ms":{"type":"integer","minimum":1,"maximum":9007199254740991},"watch_poll_interval_ms":{"type":"integer","minimum":1,"maximum":9007199254740991},"full_reconcile_interval_ms":{"type":"integer","minimum":1,"maximum":9007199254740991},"max_output_bytes":{"type":"integer","minimum":4096,"maximum":131072},"max_graph_nodes":{"type":"integer","minimum":1,"maximum":100000},"max_graph_edges":{"type":"integer","minimum":1,"maximum":1000000},"max_graph_depth":{"type":"integer","minimum":0,"maximum":8},"keep_generations":{"type":"integer","minimum":2,"maximum":128},"pack_ttl_seconds":{"type":"integer","minimum":1,"maximum":9007199254740991},"max_worktree_cache_bytes":{"type":"integer","minimum":1,"maximum":9007199254740991},"log_files":{"type":"integer","minimum":1,"maximum":9007199254740991},"log_file_bytes":{"type":"integer","minimum":1,"maximum":9007199254740991},"maintenance_wait_ms":{"type":"integer","minimum":1,"maximum":9007199254740991},"rename_result_max_bytes":{"type":"integer","minimum":1,"maximum":9007199254740991},"rename_results_max_bytes":{"type":"integer","minimum":1,"maximum":9007199254740991},"rename_result_ttl_seconds":{"type":"integer","minimum":1,"maximum":9007199254740991},"rename_max_edits":{"type":"integer","minimum":1,"maximum":10000},"result_ttl_seconds":{"type":"integer","minimum":1,"maximum":9007199254740991},"query_result_cache_bytes":{"type":"integer","minimum":1,"maximum":9007199254740991},"max_retained_query_results":{"type":"integer","minimum":1,"maximum":128}},"required":[],"additionalProperties":false},
    "ExplorerRuntimeLimits": {"type":"object","properties":{"graph_default_nodes":{"type":"integer","minimum":1,"maximum":500},"graph_default_edges":{"type":"integer","minimum":1,"maximum":1500},"graph_hard_nodes":{"type":"integer","minimum":1,"maximum":500},"graph_hard_edges":{"type":"integer","minimum":1,"maximum":1500},"flow_max_steps":{"type":"integer","minimum":1,"maximum":100},"flow_max_edges":{"type":"integer","minimum":1,"maximum":300},"hover_hz":{"type":"integer","minimum":1,"maximum":15},"session_ttl_seconds":{"type":"integer","minimum":1,"maximum":1800},"bearer_ttl_seconds":{"type":"integer","minimum":1,"maximum":1800},"ack_timeout_ms":{"type":"integer","minimum":1,"maximum":9007199254740991},"detach_timeout_ms":{"type":"integer","minimum":1,"maximum":9007199254740991},"poll_visible_ms":{"type":"integer","minimum":1,"maximum":9007199254740991},"poll_hidden_ms":{"type":"integer","minimum":1,"maximum":9007199254740991},"poll_miss_limit":{"type":"integer","minimum":1,"maximum":9007199254740991},"bridge_message_bytes":{"type":"integer","minimum":1024,"maximum":16384},"max_preview_sessions":{"type":"integer","minimum":1,"maximum":16},"map_watch_poll_ms":{"type":"integer","minimum":1,"maximum":9007199254740991},"map_artifact_max_bytes":{"type":"integer","minimum":1,"maximum":67108864},"map_pending_max":{"type":"integer","minimum":1,"maximum":1},"map_watch_debounce_ms":{"type":"integer","minimum":1,"maximum":9007199254740991}},"required":[],"additionalProperties":false},
    "RuntimeConfig": {"type":"object","properties":{"schema_version":{"const":1},"core":{"$ref":"#/$defs/CoreRuntimeLimits"},"explorer":{"$ref":"#/$defs/ExplorerRuntimeLimits"}},"required":["schema_version"],"additionalProperties":false},
    "RuntimeConfigRequest": {"type":"object","properties":{"action":{"enum":["show","validate","set"]},"config":{"$ref":"#/$defs/RuntimeConfig"},"expected_digest":{"anyOf":[{"$ref":"#/$defs/Hash"},{"type":"null"}]}},"required":["action"],"additionalProperties":false},
    "RuntimeConfigData": {"type":"object","properties":{"operation":{"const":"runtime_config"},"config":{"$ref":"#/$defs/RuntimeConfig"},"config_digest":{"$ref":"#/$defs/Hash"},"restart_required":{"type":"boolean"}},"required":["operation","config","config_digest","restart_required"],"additionalProperties":false},
    "UIMappingPayload": {"type":"object","properties":{"source_ref":{"$ref":"#/$defs/SourceRef"},"destination_ref":{"$ref":"#/$defs/GraphRef"},"rationale":{"type":"string","minLength":1,"maxLength":2048}},"required":["source_ref","destination_ref","rationale"],"additionalProperties":false},
    "RecordSummary": {"type":"object","properties":{"record_id":{"$ref":"#/$defs/UUID"},"version":{"type":"integer","minimum":1,"maximum":9007199254740991},"record_kind":{"$ref":"#/$defs/ID"},"declared_status":{"enum":["proposed","approved","revoked","superseded"]},"effective_status":{"enum":["proposed","approved","revoked","superseded","needs_review"]},"provenance_status":{"enum":["retained_snapshot","snapshot_expired","source_changed","access_denied","unverified"]},"source_refs":{"type":"array","items":{"$ref":"#/$defs/SourceRef"},"minItems":0,"maxItems":128}},"required":["record_id","version","record_kind","declared_status","effective_status","provenance_status","source_refs"],"additionalProperties":false},
    "RecordListRequest": {"type":"object","properties":{"scope":{"$ref":"#/$defs/ScopeRequest"},"record_kind":{"$ref":"#/$defs/ID"},"status":{"enum":["proposed","approved","revoked","superseded","needs_review"]},"limit":{"type":"integer","minimum":1,"maximum":100},"cursor":{"$ref":"#/$defs/Token"}},"required":[],"additionalProperties":false},
    "RecordListData": {"type":"object","properties":{"operation":{"const":"records_list"},"records":{"type":"array","items":{"$ref":"#/$defs/RecordSummary"},"minItems":0,"maxItems":100}},"required":["operation","records"],"additionalProperties":false},
    "RecordReadRequest": {"type":"object","properties":{"scope":{"$ref":"#/$defs/ScopeRequest"},"record_id":{"$ref":"#/$defs/UUID"},"version":{"type":"integer","minimum":1,"maximum":9007199254740991}},"required":["record_id"],"additionalProperties":false},
    "RecordReadData": {"type":"object","properties":{"operation":{"const":"records_read"},"record":{"$ref":"#/$defs/RecordSummary"},"ui_mapping":{"anyOf":[{"$ref":"#/$defs/UIMappingPayload"},{"type":"null"}]},"limitations":{"type":"array","items":{"$ref":"#/$defs/Gap"},"minItems":0,"maxItems":32}},"required":["operation","record","ui_mapping","limitations"],"additionalProperties":false},
    "ProfileAdminRequest": {"type":"object","properties":{"worktree_id":{"$ref":"#/$defs/UUID"},"profile_name":{"type":"string","minLength":1,"maxLength":128},"expected_revision":{"type":"integer","minimum":0,"maximum":9007199254740991},"profile":{"$ref":"#/$defs/EffectiveProfile"}},"required":["worktree_id","profile_name","expected_revision","profile"],"additionalProperties":false},
    "ProfileAdminData": {"type":"object","properties":{"operation":{"const":"profile_admin"},"worktree_id":{"$ref":"#/$defs/UUID"},"profile_name":{"type":"string","minLength":1,"maxLength":128},"profile_id":{"$ref":"#/$defs/Hash"},"revision":{"type":"integer","minimum":1,"maximum":9007199254740991}},"required":["operation","worktree_id","profile_name","profile_id","revision"],"additionalProperties":false},
    "PolicyAdminRequest": {"type":"object","properties":{"worktree_id":{"$ref":"#/$defs/UUID"},"expected_epoch":{"type":"integer","minimum":0,"maximum":9007199254740991},"policy":{"$ref":"#/$defs/PolicyConfig"}},"required":["worktree_id","expected_epoch","policy"],"additionalProperties":false},
    "PolicyAdminData": {"type":"object","properties":{"operation":{"const":"policy_admin"},"worktree_id":{"$ref":"#/$defs/UUID"},"policy_digest":{"$ref":"#/$defs/Hash"},"policy_epoch":{"type":"integer","minimum":1,"maximum":9007199254740991}},"required":["operation","worktree_id","policy_digest","policy_epoch"],"additionalProperties":false}
  }
}
```
<!-- END MCE4D_CONTRACT_SCHEMA_V1 -->

<!-- BEGIN MCE4D_OPERATION_REGISTRY_V1 -->
```json
{
  "mce4d_workspace_status": [
    "StatusRequest",
    "StatusData"
  ],
  "mce4d_locate": [
    "LocateRequest",
    "LocateData"
  ],
  "mce4d_context_pack": [
    "PackRequest",
    "PackData"
  ],
  "mce4d_expand_pack": [
    "ExpandRequest",
    "PackData"
  ],
  "mce4d_validate_pack": [
    "ValidateRequest",
    "ValidateData"
  ],
  "mce4d_rename_preview": [
    "RenameRequest",
    "RenameData"
  ],
  "POST /session/bootstrap": [
    "BootstrapRequest",
    "BootstrapData"
  ],
  "GET /workspace": [
    "StatusRequest",
    "StatusData"
  ],
  "POST /graph/query": [
    "GraphQueryRequest",
    "GraphData"
  ],
  "POST /source/read": [
    "SourceReadRequest",
    "SourceData"
  ],
  "POST /mapping/resolve": [
    "MappingResolveRequest",
    "MappingData"
  ],
  "POST /flow/query": [
    "FlowQueryRequest",
    "FlowData"
  ],
  "POST /context/pack": [
    "PackRequest",
    "PackData"
  ],
  "POST /preview/attach": [
    "PreviewAttachRequest",
    "PreviewData"
  ],
  "POST /preview/bind": [
    "PreviewBindRequest",
    "PreviewData"
  ],
  "POST /preview/mode": [
    "PreviewModeRequest",
    "PreviewData"
  ],
  "POST /preview/ack": [
    "PreviewAckRequest",
    "PreviewData"
  ],
  "POST /preview/invalidate": [
    "PreviewInvalidateRequest",
    "PreviewData"
  ],
  "POST /preview/detach": [
    "PreviewDetachRequest",
    "PreviewData"
  ],
  "POST /mapping/propose": [
    "MappingProposeRequest",
    "RecordData"
  ],
  "POST /mapping/review": [
    "MappingReviewRequest",
    "RecordData"
  ],
  "POST /preview/status": [
    "PreviewStatusRequest",
    "PreviewStatusData"
  ],
  "CLI preview-permission": [
    "PreviewPermissionAdminRequest",
    "PreviewPermissionAdminData"
  ],
  "CLI group": [
    "GroupAdminRequest",
    "GroupAdminData"
  ],
  "CLI config": [
    "RuntimeConfigRequest",
    "RuntimeConfigData"
  ],
  "POST /records/list": [
    "RecordListRequest",
    "RecordListData"
  ],
  "POST /records/read": [
    "RecordReadRequest",
    "RecordReadData"
  ],
  "CLI records list": [
    "RecordListRequest",
    "RecordListData"
  ],
  "CLI records read": [
    "RecordReadRequest",
    "RecordReadData"
  ],
  "CLI profile set": [
    "ProfileAdminRequest",
    "ProfileAdminData"
  ],
  "CLI policy set": [
    "PolicyAdminRequest",
    "PolicyAdminData"
  ]
}
```
<!-- END MCE4D_OPERATION_REGISTRY_V1 -->

## S44 — Bản vẽ physical storage, giao dịch và migration

### DB-01 — Database ownership cố định

| DB | Authority / lifecycle | Writer |
|---|---|---|
| catalog.sqlite | Registration, profiles/policies immutable + heads/revisions, declared groups; durable configuration | Catalog service; short catalog tx; không parse/index trong tx |
| worktrees/<id>/index.sqlite | Captured allowed bytes, immutable generations/facts/FTS/module/domain/build maps/bindings; rebuildable | Single IndexWriter giữ worktree OS lock |
| worktrees/<id>/records.sqlite | Approved/proposed decisions/mappings immutable versions + heads + mutation receipts; imported redacted evidence; **không cache** | RecordService giữ cùng worktree OS writer lock, tx riêng records |
| process/browser memory | Bearer/session, DOM instances, selection, bounded layout/query caches | Chủ process/browser session; no persistent per-hover rows |

Không `ATTACH` các DB rồi giả atomic transaction toàn hệ thống. Không schema FK qua file DB; validate cross-DB identities trước/sau operation, ghi epochs trong scope. Index reset/purge không xóa records/catalog. Remove/rebind giữ durable records và audit; khôi phục lại không tự approve record stale.

DDL dưới đây là bản vẽ có thể chạy; T106 phải chạy **trên driver/OS của sản phẩm** trước feature. Document audit dùng SQLite test riêng chỉ chứng minh script/fixtures đã chạy ở môi trường đó. SQL guards không thay OS ownership, source hash validators hoặc permissions. `*_json` có json_valid tại DB và closed kind schema ở application; không tự do chứa code/DSL.

### DB-02 — Thứ tự migration và compatibility

Files cần trích khi task bắt đầu: `internal/store/sqlite/migrations/catalog/001.sql`, `index/001.sql`, `index/002.sql`, `records/001.sql`, rồi `index/003.sql` khi R2 domain, `index/004.sql` khi UI maps, `records/002.sql` khi runtime evidence. Không tạo tất cả bảng addon ngay R1. Mỗi DB ghi schema_version/min_reader_version và incarnation; migration hash lưu release manifest, không sửa SQL migration đã phát hành. V1.5.0 được phép cập nhật 7 DDL baseline vì implementation NOT_STARTED và chưa có migration đã phát hành được quan sát. Nếu T001 tìm actual v1.4 DB/binary, MUST dừng bootstrap destructive, giữ file cũ và tạo forward migration được duyệt; không chạy lại CREATE/overwrite trên dữ liệu thật. R1 reader chỉ hỗ trợ index đến2; UI-enabled build đến4; version cao hơn capabilities đã build -> SCHEMA_TOO_NEW, không tự down-migrate.

Mọi connection bật foreign_keys trước transaction; driver initializer thực thi/kiểm chứng WAL, FULL, busy_timeout5000, FK1. Init phải áp cho **mọi pooled connection**, không chỉ db.Exec lần đầu. Read dùng read-only connection/query_only khi đúng supported driver, không cho reader tạo schema. Tests mở nhiều connection chứng minh settings. DB pool giới hạn đọc4/write1 mỗi DB/process; nhiều process vẫn cần OS writer lock. FK/relation/capture guard không dựa SQLite implicit defaults [REF04,REF05,REF50].

Migrate/restore theo shared/exclusive lifecycle gate DB-05–DB-06; writer lock một mình không chứng minh mọi reader đã đóng. Không raw-copy active WAL một file. Recovery không tự xóa target/source hay sửa toàn registry.

### DB-03 — Các transaction chuẩn

**Publish:** get writer ownership → insert BUILDING row owner_job_id → bounded transactions insert facts/FTS. Validation source/graph/FK outside publication tx. Cuối job kiểm tra policy/profile/root revisions và manifest stability; trong transaction chỉ dùng connection đó: CAS state BUILDING+owner_job_id, set READY hoặc READY_PARTIAL với manifest/digests → active pointer đúng profile → COMMIT. RowsAffected cần đúng1; conflict no publish. Cancel/crash trước commit giữ pointer trước. Existing active giữ nguyên nếu generation mới FAILED. Core read không chọn latest timestamp.

**Record edit:** validate source refs và principal → worktree lock → begin records tx → tìm mutation receipt. Same principal/hash trả version gốc; mismatch reject. Read current head, expected_version match; insert version+1 immutable → CAS head old version → update records_meta epoch+1 → insert mutation receipt → commit. New record version1 có head creation uniqueness. Ghi failure rollback tất cả. `needs_review` derive khi source/hash/policy không khớp, không update historical record_versions. Không xóa record_versions khi cache quota đầy; backup dưới durable policy.

**Map import:** parse+validate+hash entire manifest trước SQL tx; import map+anchors atomically dưới writer; same map ID phải same normalized content. Binding service separately checks anchors versus captured source and creates binding+typed links atomically. `binding_id = H("binding-v1", {build_map_id, source_generation_id, profile_id, policy_digest, resolver_digest})`; no source metadata update trong published generation. Resolver digest bao gồm implementation/config/semantic version của resolver, khác instrumentation provider. UNIQUE(build_map_id,profile_id,source_generation_id,policy_digest,resolver_digest) phải khớp recipe; hai resolver cho cùng source/map được lưu đồng thời, binding cũ không overwrite. Same natural tuple/different content → integrity conflict, không INSERT OR REPLACE. Binding target_refs validator kiểm tra XOR/same scope và actual rows. Re-import producer metadata không đọc app network.

**Retire/GC — thứ tự cố định:** acquire worktree shared lifecycle lease + writer lock → chọn generation không active → một transaction cho từng generation: state READY/READY_PARTIAL→RETIRED; FAILED/CANCELLED giữ terminal state → (1) `ui_build_bindings` của gen (cascade `ui_anchor_links`) nếu table đã migrate; (2) `entity_links`, `state_transitions`, `contract_fields` nếu domain đã migrate; (3) `relations`; (4) `module_members`; (5) `search_documents` (FTS triggers), `diagnostics`, `capability_results`; (6) `symbols`; (7) `domain_entities` nếu có; (8) `modules` theo leaf-before-parent; (9) `files`; (10) `packages`; (11) `generations`; (12) `content_blobs` không còn FK reference từ BẤT KỲ generation còn giữ; FK check → COMMIT. Không bỏ bước bằng cách disable FK, đổi cascade, xóa active pointer hoặc cập nhật source_path=NULL.

Modules leaf-before-parent: trong cùng generation, chọn tối đa256 leaf IDs không có child còn tồn tại, order UTF-8 module_id; xóa đúng các IDs đó; lặp đến hết trong deadline. Còn rows nhưng không leaf → INDEX_CORRUPT và ROLLBACK toàn generation, không xử lý một nửa cây. Module cycle phải bị từ chối ở publish dù FK cho phép cycle. Các bước không có rows là no-op; table addon chưa được migrate được skip dựa schema capability đã xác minh, không nuốt lỗi SQL. Cancellation/fault giữa các bước rollback cả trạng thái RETIRED và deletions; generation active khác/profile khác không đổi. Reader đã mở read transaction vẫn giữ snapshot của lượt đó; page sau có thể SNAPSHOT_EXPIRED.

Whole build-map GC là job riêng trong cùng writer ownership: chỉ map không binding còn giữ và ngoài retention mới được delete `ui_build_maps`; cascade anchors trong một transaction (self-FK deferred) + integrity check. Không xóa từng anchor ở map còn dùng. Không đọc/xóa records.sqlite/catalog/source trong GC derived index. Khi không đủ chỗ trả DISK_FULL/RESOURCE_LIMIT, không giảm dữ liệu active để vừa quota.

FTS dùng `search_documents` external-content + triggers đồng bộ. Query join rowid về documents, filter generation/worktree before returning; không full-text table scan ngoài policy. Regression đảm bảo phrase ở generation A không xuất qua request B. Dedupe source blobs trong cùng worktree, không toàn máy.

### DB-04 — Privacy purge và scope bảo đảm

Revoke quyền đọc có hiệu lực ở các checks trước response; output đã gửi không thu hồi được. DELETE/GC không chứng minh raw bytes biến mất khỏi WAL, FTS shadow tables, free pages, OS snapshot hoặc backup [REF51,REF52]. Không ghi “secure erase” từ rowcount=0. Khi cần purge secret thực đã lỡ lưu: block serving affected data, lập incident metadata không value, rebuild allowed index từ nguồn đã sửa, offline checkpoints/backup handling theo quyền. Đừng thực hiện destructive purge records/backups để làm privacy test pass.

Source security policy được áp trước persistence, không “save raw rồi redact lúc gửi”. Secret scanner hữu hạn có false negative; không claim mọi bí mật được tìm thấy. T001/T106 fixtures dùng marker giả và không user secret.

### DB-05 — Bảo trì nhiều process: lifecycle lease khác writer lock (A07)

Mỗi catalog và mỗi worktree có `lifecycle.lock` cố định ngoài các DB được thay. Primitive OS shared/exclusive được test tại T010/T035 trên Linux/Windows; không xóa hoặc đổi inode lockfile khi còn user. Process phải giữ SHARED lifecycle lease trong suốt thời gian còn bất kỳ pool/handle/transaction/child-owned DB handle nào tới store đó; idle pooled connection cũng tính. Đóng toàn pool/handles rồi mới nhả lease. Writer index/record thêm writer.lock EXCLUSIVE như trước; reader không lấy writer.lock. API entry authorization không thay lease. R1 foundation defines port/proof; actual store public access không mở trước wired lock adapter. Không dùng PID/TTL để chiếm lease.

Maintenance chỉ cho migrate/replace/restore đúng store, không áp cho build/test/normal index toàn máy. Trình tự: validate command/quyền/target identity→acquire EXCLUSIVE lifecycle lease trong maintenance_wait_ms→không owner DB handles→acquire writer lock nếu worktree hoặc catalog mutation lock nếu catalog→backup/checkpoint/stage/migrate/verify→close maintenance handles→release writer→release lifecycle. Không nâng shared→exclusive tại chỗ trong khi đang giữ pool; close/release trước, lấy exclusive mới rồi revalidate. Không giữ hai worktree writer locks. Không giữ catalog SQL transaction khi chờ worktree gate. Acquire nhiều lifecycle leases chỉ ở process startup bằng catalog trước rồi worktree UUID thứ tự tăng, release ngược; không khóa source.

Một CLI maintenance không ép các MCP/Explorer process khác thoát, không kill theo tên. Nếu họ đang giữ SHARED pool lease: trả STORE_MAINTENANCE/WORKTREE_BUSY với store/operation owner metadata đã sanitize và yêu cầu đóng đúng process trước; không khẳng định đã drain chỉ từ writer.lock. Deadline hữu hạn, không retry vô hạn. Worktree B không bị khóa bởi maintenance A; catalog maintenance ảnh hưởng mọi consumer catalog vì đó là shared config thật, phải báo phạm vi này rõ, không gọi dependency build giữa các dự án. Không cần daemon/poll registry mới để bảo trì.

Giữ SQLite native locks/WAL semantics, lifecycle lock chỉ điều phối các process MCE4D hợp tác. Process/DB tool ngoài giao thức hoặc filesystem không hỗ trợ lock đáng tin cậy → refuse automatic replace, offline operator gate; không claim chống arbitrary same-user tool. Reader process lâu sống giữ shared lease nên maintenance có thể yêu cầu user dừng process rõ ràng; đây là trade-off đơn giản, không fake automatic draining. Core command bắt đầu trên incomplete maintenance journal → STORE_RECOVERY_REQUIRED, không tự mở DB nửa thay.

### DB-06 — Incarnation, restore và journal phục hồi (A07)

Catalog có catalog_identity.incarnation; index store_identity.incarnation; records records_meta.incarnation. App init tạo UUID random mới cho mỗi store mới; field bắt buộc không có default cố định. Restore/rebuild/replacement sinh incarnation MỚI trước phục vụ; không lấy lại identity incarnation trong backup làm phiên hiện tại. Regular SQL transaction/atomic publish không đổi incarnation. Scoped tokens/cache/results gồm catalog+index+records incarnation; client thiếu/sai nhận STORE_REPLACED hoặc token invalidation, không chấp nhận vì revision số trùng sau rollback. SourceRef bản thân không mang quyền; khi dùng phải có current/pinned scope đã xác minh.

**Backup baseline:** mỗi command chọn catalog HOẶC một worktree set index+records; không hứa atomic backup xuyên nhiều worktrees/catalog. Backup online dùng verified SQLite backup API từng DB và metadata versions/hashes; còn consistent worktree pair cần exclusive lifecycle + all writers quiesced rồi backup cả hai cùng cửa sổ. Backup manifest có format_version, source store kinds/IDs/incarnations/schema versions, raw file hashes, timestamp, policy/binding/record revisions và `consistency=quiesced_worktree_set|single_store`. Backup không chứa cursor.key/session/bearer; permission approvals khi restore phải được review lại và sessions không tồn tại sau restart. Catalog group refs và worktree backup có thể không cùng point-in-time: sau restore phải reconcile, không auto link missing refs.

**Restore baseline offline:** approved local backup manifest→bounded checksum/schema/worktree/permission validation→EXCLUSIVE lifecycle/worktree writer→create sanitized journal ngoài files sẽ restore→build staged DBs dưới same owned filesystem bằng backup API/closed verified files→PRAGMA integrity_check/foreign_key_check + initialize NEW incarnation(s)→close staging/original handles→record PREPARED→replace each store file theo journal và adapter atomic-rename semantics đã test→verify all intended hashes/schema/IDs→record VERIFIED→resume only sau gate. Giữ rollback artifact thuộc policy/quyền, không tự xóa bản duy nhất để reclaim. Không restore cursor.key từ archive. Catalog restore cũng rotate catalog incarnation, mọi existing scope token invalid. Worktree index-only rebuild giữ records, index incarnation mới; records references cũ là historical/needs_review.

Journal record gồm format_version, operation_id, target_store_ids, expected_old_incarnations, staged hashes, replacement order, phase=`PREPARED|REPLACING|VERIFYING|VERIFIED|NEEDS_RECOVERY`, actual completed steps; không source/raw secrets. Atomic journal writes, flush và directory durability theo OS adapter được kiểm chứng; không hứa crash durability nếu filesystem/platform không bảo đảm. Process crash giữa replace nhiều DB không là atomic multi-file transaction: next command block phục vụ, recovery verify journal/state rồi hoàn tất hoặc rollback đúng owned files có quyền. Không đoán từ file tồn tại/PID cũ rằng restore hoàn tất. Test crash từng boundary; no rollback generation/profile silently để token cũ hợp lệ.

CLI cần triển khai: `mce4d store backup --target catalog|worktree --worktree-id <id-if-worktree> --output <approved-path>`; `mce4d store restore --manifest <approved-local-file> --confirm --wait-ms <bounded>`; `mce4d store recover --target ... --report-only` mặc định không mutate, apply recovery cần quyền+confirmation riêng. Không HTTP/MCP restore endpoint. Shell placeholders không chạy. T035 định nghĩa command/parser tests theo contract này; exact path allowed ownership kiểm T005; không thêm service hoặc credential.

### DB-07 — Resolver, snapshots và dữ liệu bền vững sau GC

Resolver digest nằm trong binding natural key và immutable metadata; select binding của session explicit binding_id, không lấy MAX(created_at) làm đúng. Cùng producer map/source/policy đổi resolver được lưu cạnh nhau; phiên cũ giữ resolver cũ tới expiry/rebind, không rewrite links. Import same manifest byte-equivalent idempotent; same ID/different content reject. UI metadata chỉ có hiệu lực với generation retained+policy/current incarnation đúng.

Retention cache default2 generations/profile vẫn giữ; TTL pack/result không bảo đảm snapshot được giữ. Đo snapshot_expired_count, repeated_context_bytes, map_expired_count, lock_wait_by_operation trước tăng retention. Không thêm pin/lease hội thoại vào R1. User records và permission version history không vào GC index. Khi historical SourceRef đã GC, RecordSummary báo snapshot_expired/needs_review, có thể xem statement/metadata được policy cho phép nhưng không dựng lại source từ lời nhớ. Reanchor vào current source chỉ proposed mapping mới, có version/review; không sửa record cũ.


### DDL — catalog_001

<!-- BEGIN MCE4D_DDL_CATALOG_001 -->
```sql
PRAGMA foreign_keys=ON;
CREATE TABLE schema_meta (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 schema_version INTEGER NOT NULL CHECK(schema_version>=1),
 min_reader_version INTEGER NOT NULL CHECK(min_reader_version>=1)
) STRICT;
INSERT INTO schema_meta VALUES(1,1,1);
CREATE TABLE catalog_identity (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 incarnation TEXT NOT NULL CHECK(length(incarnation)=36)
) STRICT;
CREATE TABLE repositories (
 repo_id TEXT PRIMARY KEY NOT NULL,
 common_dir TEXT NOT NULL UNIQUE,
 filesystem_identity TEXT NOT NULL UNIQUE,
 created_at TEXT NOT NULL
) STRICT;
CREATE TABLE worktrees (
 worktree_id TEXT PRIMARY KEY NOT NULL,
 repo_id TEXT NOT NULL REFERENCES repositories(repo_id) ON DELETE RESTRICT,
 canonical_root TEXT NOT NULL UNIQUE,
 filesystem_identity TEXT NOT NULL UNIQUE,
 binding_revision INTEGER NOT NULL DEFAULT 1 CHECK(binding_revision>=1),
 status TEXT NOT NULL CHECK(status IN ('registered','unavailable','removed')),
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL
) STRICT;
CREATE INDEX worktrees_repo ON worktrees(repo_id,status);
CREATE TABLE policies (
 worktree_id TEXT NOT NULL REFERENCES worktrees(worktree_id) ON DELETE RESTRICT,
 policy_digest TEXT NOT NULL CHECK(length(policy_digest)=64),
 config_json TEXT NOT NULL CHECK(json_valid(config_json)),
 created_at TEXT NOT NULL,
 PRIMARY KEY(worktree_id,policy_digest)
) STRICT;
CREATE TABLE worktree_policy_heads (
 worktree_id TEXT PRIMARY KEY NOT NULL,
 policy_digest TEXT NOT NULL,
 policy_epoch INTEGER NOT NULL CHECK(policy_epoch>=1),
 updated_at TEXT NOT NULL,
 FOREIGN KEY(worktree_id,policy_digest) REFERENCES policies(worktree_id,policy_digest) ON DELETE RESTRICT
) STRICT;
CREATE TABLE profiles (
 worktree_id TEXT NOT NULL REFERENCES worktrees(worktree_id) ON DELETE RESTRICT,
 profile_id TEXT NOT NULL CHECK(length(profile_id)=64),
 config_json TEXT NOT NULL CHECK(json_valid(config_json)),
 toolchain_digest TEXT NOT NULL CHECK(length(toolchain_digest)=64),
 provider_digest TEXT NOT NULL CHECK(length(provider_digest)=64),
 created_at TEXT NOT NULL,
 PRIMARY KEY(worktree_id,profile_id)
) STRICT;
CREATE TABLE profile_heads (
 worktree_id TEXT NOT NULL,
 profile_name TEXT NOT NULL,
 profile_id TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(revision>=1),
 PRIMARY KEY(worktree_id,profile_name),
 FOREIGN KEY(worktree_id,profile_id) REFERENCES profiles(worktree_id,profile_id) ON DELETE RESTRICT
) STRICT;
CREATE TABLE repo_groups (
 group_id TEXT PRIMARY KEY NOT NULL,
 name TEXT NOT NULL UNIQUE,
 status TEXT NOT NULL DEFAULT 'active' CHECK(status IN('active','removed')),
 revision INTEGER NOT NULL CHECK(revision>=1),
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL
) STRICT;
CREATE TABLE group_members (
 group_id TEXT NOT NULL REFERENCES repo_groups(group_id) ON DELETE CASCADE,
 service_id TEXT NOT NULL,
 worktree_id TEXT NOT NULL,
 profile_id TEXT NOT NULL,
 namespace_json TEXT NOT NULL CHECK(json_valid(namespace_json)),
 PRIMARY KEY(group_id,service_id),
 FOREIGN KEY(worktree_id,profile_id) REFERENCES profiles(worktree_id,profile_id) ON DELETE RESTRICT
) STRICT;
CREATE INDEX group_members_scope ON group_members(worktree_id,profile_id);
CREATE TRIGGER policies_immutable BEFORE UPDATE ON policies BEGIN SELECT RAISE(ABORT,'IMMUTABLE_POLICY'); END;
CREATE TRIGGER profiles_immutable BEFORE UPDATE ON profiles BEGIN SELECT RAISE(ABORT,'IMMUTABLE_PROFILE'); END;
CREATE TABLE preview_permission_versions (
 profile_id TEXT NOT NULL, version INTEGER NOT NULL CHECK(version>=1),
 declared_status TEXT NOT NULL CHECK(declared_status IN('approved','revoked')),
 config_json TEXT NOT NULL CHECK(json_valid(config_json)),
 config_digest TEXT NOT NULL CHECK(length(config_digest)=64),
 actor_id TEXT NOT NULL, reason TEXT NOT NULL, created_at TEXT NOT NULL,
 PRIMARY KEY(profile_id,version)
) STRICT;
CREATE TABLE preview_permission_heads (
 profile_id TEXT PRIMARY KEY NOT NULL, version INTEGER NOT NULL,
 FOREIGN KEY(profile_id,version) REFERENCES preview_permission_versions(profile_id,version) ON DELETE RESTRICT
) STRICT;
CREATE TRIGGER preview_permissions_immutable BEFORE UPDATE ON preview_permission_versions
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_PERMISSION_VERSION'); END;

```
<!-- END MCE4D_DDL_CATALOG_001 -->

### DDL — index_001

<!-- BEGIN MCE4D_DDL_INDEX_001 -->
```sql
PRAGMA foreign_keys=ON;
CREATE TABLE schema_meta (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 schema_version INTEGER NOT NULL CHECK(schema_version>=1),
 min_reader_version INTEGER NOT NULL CHECK(min_reader_version>=1)
) STRICT;
INSERT INTO schema_meta VALUES(1,1,1);
CREATE TABLE store_identity (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 worktree_id TEXT NOT NULL UNIQUE,
 incarnation TEXT NOT NULL CHECK(length(incarnation)=36)
) STRICT;
CREATE TABLE generations (
 generation_id TEXT PRIMARY KEY NOT NULL,
 profile_id TEXT NOT NULL CHECK(length(profile_id)=64),
 snapshot_digest TEXT CHECK(snapshot_digest IS NULL OR length(snapshot_digest)=64),
 state TEXT NOT NULL CHECK(state IN ('BUILDING','READY','READY_PARTIAL','FAILED','CANCELLED','RETIRED')),
 owner_job_id TEXT NOT NULL,
 policy_digest TEXT NOT NULL CHECK(length(policy_digest)=64),
 policy_epoch INTEGER NOT NULL CHECK(policy_epoch>=1),
 provider_fingerprint TEXT NOT NULL CHECK(length(provider_fingerprint)=64),
 manifest_json TEXT CHECK(manifest_json IS NULL OR json_valid(manifest_json)),
 created_at TEXT NOT NULL, completed_at TEXT,
 UNIQUE(profile_id,generation_id),
 CHECK(state NOT IN ('READY','READY_PARTIAL','RETIRED') OR
       (snapshot_digest IS NOT NULL AND manifest_json IS NOT NULL AND completed_at IS NOT NULL))
) STRICT;
CREATE INDEX generations_profile ON generations(profile_id,completed_at,generation_id);
CREATE TABLE active_generations (
 profile_id TEXT PRIMARY KEY NOT NULL,
 generation_id TEXT NOT NULL UNIQUE,
 FOREIGN KEY(profile_id,generation_id) REFERENCES generations(profile_id,generation_id) ON DELETE RESTRICT
) STRICT;
CREATE TABLE content_blobs (
 content_hash TEXT PRIMARY KEY NOT NULL CHECK(length(content_hash)=64),
 bytes BLOB NOT NULL,
 byte_length INTEGER NOT NULL CHECK(byte_length>=0 AND byte_length=length(bytes))
) STRICT;
CREATE TABLE packages (
 generation_id TEXT NOT NULL REFERENCES generations(generation_id) ON DELETE CASCADE,
 package_key TEXT NOT NULL,
 import_path TEXT NOT NULL, module_key TEXT NOT NULL,
 variant TEXT NOT NULL CHECK(variant IN ('production','internal_test','external_test','other_supported')),
 input_digest TEXT NOT NULL CHECK(length(input_digest)=64),
 semantic_status TEXT NOT NULL CHECK(semantic_status IN ('complete_in_profile','partial','unsupported','unknown')),
 PRIMARY KEY(generation_id,package_key)
) STRICT;
CREATE INDEX packages_import ON packages(generation_id,import_path,variant);
CREATE TABLE files (
 generation_id TEXT NOT NULL REFERENCES generations(generation_id) ON DELETE CASCADE,
 path TEXT NOT NULL,
 content_hash TEXT REFERENCES content_blobs(content_hash) ON DELETE RESTRICT,
 kind TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('indexed','metadata_only','excluded','missing','read_error','parse_error')),
 byte_length INTEGER CHECK(byte_length IS NULL OR byte_length>=0),
 package_key TEXT,
 generated INTEGER NOT NULL DEFAULT 0 CHECK(generated IN (0,1)),
 reason_code TEXT,
 PRIMARY KEY(generation_id,path),
 FOREIGN KEY(generation_id,package_key) REFERENCES packages(generation_id,package_key) ON DELETE RESTRICT,
 CHECK(status NOT IN ('indexed','parse_error') OR content_hash IS NOT NULL),
 CHECK(status NOT IN ('excluded','missing','read_error') OR content_hash IS NULL),
 CHECK(status='indexed' OR reason_code IS NOT NULL)
) STRICT;
CREATE INDEX files_status ON files(generation_id,status,path);
CREATE TABLE symbols (
 generation_id TEXT NOT NULL REFERENCES generations(generation_id) ON DELETE CASCADE,
 symbol_id TEXT NOT NULL,
 path TEXT, package_key TEXT, kind TEXT NOT NULL,
 name TEXT NOT NULL, qualified_name TEXT NOT NULL, scope_key TEXT NOT NULL,
 start_byte INTEGER, end_byte INTEGER,
 signature TEXT NOT NULL, exported INTEGER NOT NULL CHECK(exported IN(0,1)),
 PRIMARY KEY(generation_id,symbol_id),
 FOREIGN KEY(generation_id,path) REFERENCES files(generation_id,path) ON DELETE RESTRICT,
 FOREIGN KEY(generation_id,package_key) REFERENCES packages(generation_id,package_key) ON DELETE RESTRICT,
 CHECK((path IS NULL AND kind='external' AND start_byte IS NULL AND end_byte IS NULL)
   OR (path IS NOT NULL AND start_byte IS NOT NULL AND end_byte IS NOT NULL AND start_byte>=0 AND end_byte>=start_byte))
) STRICT;
CREATE INDEX symbols_name ON symbols(generation_id,name,symbol_id);
CREATE INDEX symbols_qualified ON symbols(generation_id,qualified_name,symbol_id);
CREATE INDEX symbols_range ON symbols(generation_id,path,start_byte,end_byte);
CREATE TABLE relations (
 generation_id TEXT NOT NULL REFERENCES generations(generation_id) ON DELETE CASCADE,
 relation_id TEXT NOT NULL,
 source_symbol_id TEXT NOT NULL, target_symbol_id TEXT,
 kind TEXT NOT NULL,
 evidence_kind TEXT NOT NULL CHECK(evidence_kind IN
 ('syntax_exact','type_resolved','type_possible','explicit_document','text_match','unresolved')),
 site_path TEXT NOT NULL, start_byte INTEGER NOT NULL CHECK(start_byte>=0),
 end_byte INTEGER NOT NULL CHECK(end_byte>=start_byte),
 reason TEXT NOT NULL, unresolved_label TEXT,
 PRIMARY KEY(generation_id,relation_id),
 FOREIGN KEY(generation_id,source_symbol_id) REFERENCES symbols(generation_id,symbol_id) ON DELETE RESTRICT,
 FOREIGN KEY(generation_id,target_symbol_id) REFERENCES symbols(generation_id,symbol_id) ON DELETE RESTRICT,
 FOREIGN KEY(generation_id,site_path) REFERENCES files(generation_id,path) ON DELETE RESTRICT,
 CHECK(target_symbol_id IS NOT NULL OR (evidence_kind='unresolved' AND unresolved_label IS NOT NULL))
) STRICT;
CREATE INDEX relations_out ON relations(generation_id,source_symbol_id,kind,relation_id);
CREATE INDEX relations_in ON relations(generation_id,target_symbol_id,kind,relation_id);
CREATE TABLE diagnostics (
 generation_id TEXT NOT NULL REFERENCES generations(generation_id) ON DELETE CASCADE,
 diagnostic_id TEXT NOT NULL, path TEXT, package_key TEXT,
 code TEXT NOT NULL, severity TEXT NOT NULL CHECK(severity IN('info','warning','error')),
 sanitized_message TEXT NOT NULL,
 scope_json TEXT NOT NULL CHECK(json_valid(scope_json)),
 PRIMARY KEY(generation_id,diagnostic_id),
 FOREIGN KEY(generation_id,path) REFERENCES files(generation_id,path) ON DELETE RESTRICT,
 FOREIGN KEY(generation_id,package_key) REFERENCES packages(generation_id,package_key) ON DELETE RESTRICT
) STRICT;
CREATE INDEX diagnostics_code ON diagnostics(generation_id,severity,code);
CREATE TABLE capability_results (
 generation_id TEXT NOT NULL REFERENCES generations(generation_id) ON DELETE CASCADE,
 capability_key TEXT NOT NULL, scope_key TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN('supported_scope_complete','partial','unsupported','unknown')),
 summary_json TEXT NOT NULL CHECK(json_valid(summary_json)),
 PRIMARY KEY(generation_id,capability_key,scope_key)
) STRICT;
CREATE TABLE search_documents (
 rowid INTEGER PRIMARY KEY,
 generation_id TEXT NOT NULL REFERENCES generations(generation_id) ON DELETE CASCADE,
 item_id TEXT NOT NULL, path TEXT NOT NULL, kind TEXT NOT NULL,
 name TEXT NOT NULL, qualified_name TEXT NOT NULL, body TEXT NOT NULL,
 UNIQUE(generation_id,item_id)
) STRICT;
CREATE INDEX search_scope ON search_documents(generation_id,path,item_id);
CREATE VIRTUAL TABLE search_fts USING fts5(
 name,qualified_name,body,content='search_documents',content_rowid='rowid',tokenize='unicode61'
);
CREATE TRIGGER search_documents_ai AFTER INSERT ON search_documents BEGIN
 INSERT INTO search_fts(rowid,name,qualified_name,body) VALUES(new.rowid,new.name,new.qualified_name,new.body);
END;
CREATE TRIGGER search_documents_ad AFTER DELETE ON search_documents BEGIN
 INSERT INTO search_fts(search_fts,rowid,name,qualified_name,body) VALUES('delete',old.rowid,old.name,old.qualified_name,old.body);
END;
CREATE TRIGGER search_documents_au AFTER UPDATE ON search_documents BEGIN
 INSERT INTO search_fts(search_fts,rowid,name,qualified_name,body) VALUES('delete',old.rowid,old.name,old.qualified_name,old.body);
 INSERT INTO search_fts(rowid,name,qualified_name,body) VALUES(new.rowid,new.name,new.qualified_name,new.body);
END;
CREATE TRIGGER active_insert_guard BEFORE INSERT ON active_generations BEGIN
 SELECT CASE WHEN NOT EXISTS(SELECT 1 FROM generations g WHERE g.generation_id=new.generation_id
 AND g.profile_id=new.profile_id AND g.state IN('READY','READY_PARTIAL'))
 THEN RAISE(ABORT,'GENERATION_NOT_PUBLISHED') END;
END;
CREATE TRIGGER active_update_guard BEFORE UPDATE ON active_generations BEGIN
 SELECT CASE WHEN NOT EXISTS(SELECT 1 FROM generations g WHERE g.generation_id=new.generation_id
 AND g.profile_id=new.profile_id AND g.state IN('READY','READY_PARTIAL'))
 THEN RAISE(ABORT,'GENERATION_NOT_PUBLISHED') END;
END;
CREATE TRIGGER generation_retire_guard BEFORE UPDATE OF state ON generations
WHEN new.state='RETIRED' BEGIN
 SELECT CASE WHEN EXISTS(SELECT 1 FROM active_generations WHERE generation_id=old.generation_id)
 THEN RAISE(ABORT,'ACTIVE_GENERATION_CANNOT_RETIRE') END;
END;
CREATE TRIGGER content_blob_no_update BEFORE UPDATE ON content_blobs BEGIN
 SELECT RAISE(ABORT,'IMMUTABLE_CONTENT');
END;
CREATE TRIGGER packages_insert_frozen BEFORE INSERT ON packages
WHEN (SELECT state FROM generations WHERE generation_id=new.generation_id)!='BUILDING'
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER packages_update_frozen BEFORE UPDATE ON packages
WHEN (SELECT state FROM generations WHERE generation_id=old.generation_id)!='BUILDING'
 OR (SELECT state FROM generations WHERE generation_id=new.generation_id)!='BUILDING'
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER packages_delete_frozen BEFORE DELETE ON packages
WHEN (SELECT state FROM generations WHERE generation_id=old.generation_id) IN('READY','READY_PARTIAL')
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER files_insert_frozen BEFORE INSERT ON files
WHEN (SELECT state FROM generations WHERE generation_id=new.generation_id)!='BUILDING'
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER files_update_frozen BEFORE UPDATE ON files
WHEN (SELECT state FROM generations WHERE generation_id=old.generation_id)!='BUILDING'
 OR (SELECT state FROM generations WHERE generation_id=new.generation_id)!='BUILDING'
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER files_delete_frozen BEFORE DELETE ON files
WHEN (SELECT state FROM generations WHERE generation_id=old.generation_id) IN('READY','READY_PARTIAL')
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER symbols_insert_frozen BEFORE INSERT ON symbols
WHEN (SELECT state FROM generations WHERE generation_id=new.generation_id)!='BUILDING'
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER symbols_update_frozen BEFORE UPDATE ON symbols
WHEN (SELECT state FROM generations WHERE generation_id=old.generation_id)!='BUILDING'
 OR (SELECT state FROM generations WHERE generation_id=new.generation_id)!='BUILDING'
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER symbols_delete_frozen BEFORE DELETE ON symbols
WHEN (SELECT state FROM generations WHERE generation_id=old.generation_id) IN('READY','READY_PARTIAL')
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER relations_insert_frozen BEFORE INSERT ON relations
WHEN (SELECT state FROM generations WHERE generation_id=new.generation_id)!='BUILDING'
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER relations_update_frozen BEFORE UPDATE ON relations
WHEN (SELECT state FROM generations WHERE generation_id=old.generation_id)!='BUILDING'
 OR (SELECT state FROM generations WHERE generation_id=new.generation_id)!='BUILDING'
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER relations_delete_frozen BEFORE DELETE ON relations
WHEN (SELECT state FROM generations WHERE generation_id=old.generation_id) IN('READY','READY_PARTIAL')
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER diagnostics_insert_frozen BEFORE INSERT ON diagnostics
WHEN (SELECT state FROM generations WHERE generation_id=new.generation_id)!='BUILDING'
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER diagnostics_update_frozen BEFORE UPDATE ON diagnostics
WHEN (SELECT state FROM generations WHERE generation_id=old.generation_id)!='BUILDING'
 OR (SELECT state FROM generations WHERE generation_id=new.generation_id)!='BUILDING'
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER diagnostics_delete_frozen BEFORE DELETE ON diagnostics
WHEN (SELECT state FROM generations WHERE generation_id=old.generation_id) IN('READY','READY_PARTIAL')
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER capability_results_insert_frozen BEFORE INSERT ON capability_results
WHEN (SELECT state FROM generations WHERE generation_id=new.generation_id)!='BUILDING'
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER capability_results_update_frozen BEFORE UPDATE ON capability_results
WHEN (SELECT state FROM generations WHERE generation_id=old.generation_id)!='BUILDING'
 OR (SELECT state FROM generations WHERE generation_id=new.generation_id)!='BUILDING'
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER capability_results_delete_frozen BEFORE DELETE ON capability_results
WHEN (SELECT state FROM generations WHERE generation_id=old.generation_id) IN('READY','READY_PARTIAL')
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER search_documents_insert_frozen BEFORE INSERT ON search_documents
WHEN (SELECT state FROM generations WHERE generation_id=new.generation_id)!='BUILDING'
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER search_documents_update_frozen BEFORE UPDATE ON search_documents
WHEN (SELECT state FROM generations WHERE generation_id=old.generation_id)!='BUILDING'
 OR (SELECT state FROM generations WHERE generation_id=new.generation_id)!='BUILDING'
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER search_documents_delete_frozen BEFORE DELETE ON search_documents
WHEN (SELECT state FROM generations WHERE generation_id=old.generation_id) IN('READY','READY_PARTIAL')
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER generation_state_guard BEFORE UPDATE OF state ON generations
WHEN NOT (new.state=old.state OR (old.state='BUILDING' AND new.state IN('READY','READY_PARTIAL','FAILED','CANCELLED'))
 OR (old.state IN('READY','READY_PARTIAL') AND new.state='RETIRED'))
BEGIN SELECT RAISE(ABORT,'INVALID_GENERATION_TRANSITION'); END;
CREATE TRIGGER generation_metadata_guard BEFORE UPDATE ON generations
WHEN old.state!='BUILDING' AND (new.generation_id IS NOT old.generation_id OR new.profile_id IS NOT old.profile_id
 OR new.snapshot_digest IS NOT old.snapshot_digest OR new.policy_digest IS NOT old.policy_digest
 OR new.provider_fingerprint IS NOT old.provider_fingerprint OR new.manifest_json IS NOT old.manifest_json
 OR new.created_at IS NOT old.created_at OR new.completed_at IS NOT old.completed_at
 OR new.owner_job_id IS NOT old.owner_job_id OR new.policy_epoch IS NOT old.policy_epoch)
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION_METADATA'); END;
CREATE TRIGGER generation_delete_guard BEFORE DELETE ON generations
WHEN old.state NOT IN('RETIRED','FAILED','CANCELLED')
BEGIN SELECT RAISE(ABORT,'GENERATION_NOT_RETIRED'); END;
```
<!-- END MCE4D_DDL_INDEX_001 -->

### DDL — index_002

<!-- BEGIN MCE4D_DDL_INDEX_002 -->
```sql
CREATE TABLE modules (
 generation_id TEXT NOT NULL REFERENCES generations(generation_id) ON DELETE CASCADE,
 module_id TEXT NOT NULL, parent_module_id TEXT,
 label TEXT NOT NULL, kind TEXT NOT NULL,
 plane TEXT NOT NULL CHECK(plane IN('intended','observed_static')),
 source_path TEXT, start_byte INTEGER, end_byte INTEGER,
 PRIMARY KEY(generation_id,module_id),
 FOREIGN KEY(generation_id,parent_module_id) REFERENCES modules(generation_id,module_id) ON DELETE RESTRICT,
 FOREIGN KEY(generation_id,source_path) REFERENCES files(generation_id,path) ON DELETE RESTRICT
) STRICT;
CREATE TABLE module_members (
 generation_id TEXT NOT NULL, module_id TEXT NOT NULL, package_key TEXT NOT NULL,
 PRIMARY KEY(generation_id,module_id,package_key),
 FOREIGN KEY(generation_id,module_id) REFERENCES modules(generation_id,module_id) ON DELETE CASCADE,
 FOREIGN KEY(generation_id,package_key) REFERENCES packages(generation_id,package_key) ON DELETE RESTRICT
) STRICT;
CREATE INDEX module_members_reverse ON module_members(generation_id,package_key,module_id);
UPDATE schema_meta SET schema_version=2,min_reader_version=2 WHERE singleton=1;
CREATE TRIGGER modules_insert_frozen BEFORE INSERT ON modules
WHEN (SELECT state FROM generations WHERE generation_id=new.generation_id)!='BUILDING'
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER modules_update_frozen BEFORE UPDATE ON modules
WHEN (SELECT state FROM generations WHERE generation_id=old.generation_id)!='BUILDING'
 OR (SELECT state FROM generations WHERE generation_id=new.generation_id)!='BUILDING'
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER modules_delete_frozen BEFORE DELETE ON modules
WHEN (SELECT state FROM generations WHERE generation_id=old.generation_id) IN('READY','READY_PARTIAL')
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER module_members_insert_frozen BEFORE INSERT ON module_members
WHEN (SELECT state FROM generations WHERE generation_id=new.generation_id)!='BUILDING'
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER module_members_update_frozen BEFORE UPDATE ON module_members
WHEN (SELECT state FROM generations WHERE generation_id=old.generation_id)!='BUILDING'
 OR (SELECT state FROM generations WHERE generation_id=new.generation_id)!='BUILDING'
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER module_members_delete_frozen BEFORE DELETE ON module_members
WHEN (SELECT state FROM generations WHERE generation_id=old.generation_id) IN('READY','READY_PARTIAL')
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
```
<!-- END MCE4D_DDL_INDEX_002 -->

### DDL — records_001

<!-- BEGIN MCE4D_DDL_RECORDS_001 -->
```sql
PRAGMA foreign_keys=ON;
CREATE TABLE schema_meta (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 schema_version INTEGER NOT NULL CHECK(schema_version>=1),
 min_reader_version INTEGER NOT NULL CHECK(min_reader_version>=1)
) STRICT;
INSERT INTO schema_meta VALUES(1,1,1);
CREATE TABLE records_meta (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1), worktree_id TEXT NOT NULL,
 incarnation TEXT NOT NULL CHECK(length(incarnation)=36),
 record_epoch INTEGER NOT NULL DEFAULT 0 CHECK(record_epoch>=0)
) STRICT;
CREATE TABLE record_versions (
 record_id TEXT NOT NULL, version INTEGER NOT NULL CHECK(version>=1),
 record_kind TEXT NOT NULL CHECK(record_kind IN('decision','constraint','vocabulary','ui_mapping','intended_architecture','state_model')),
 declared_status TEXT NOT NULL CHECK(declared_status IN('proposed','approved','revoked','superseded')),
 payload_json TEXT NOT NULL CHECK(json_valid(payload_json)),
 source_refs_json TEXT NOT NULL CHECK(json_valid(source_refs_json)),
 actor_id TEXT NOT NULL, reason TEXT NOT NULL,
 payload_hash TEXT NOT NULL CHECK(length(payload_hash)=64), created_at TEXT NOT NULL,
 PRIMARY KEY(record_id,version)
) STRICT;
CREATE TABLE record_heads (
 record_id TEXT PRIMARY KEY NOT NULL, version INTEGER NOT NULL,
 FOREIGN KEY(record_id,version) REFERENCES record_versions(record_id,version) ON DELETE RESTRICT
) STRICT;
CREATE INDEX records_kind ON record_versions(record_kind,record_id,version);
CREATE TRIGGER record_versions_immutable BEFORE UPDATE ON record_versions BEGIN
 SELECT RAISE(ABORT,'IMMUTABLE_RECORD_VERSION');
END;
CREATE TABLE mutation_receipts (
 client_mutation_id TEXT PRIMARY KEY NOT NULL,
 principal_id TEXT NOT NULL,
 request_hash TEXT NOT NULL CHECK(length(request_hash)=64),
 record_id TEXT NOT NULL, result_version INTEGER NOT NULL,
 created_at TEXT NOT NULL,
 FOREIGN KEY(record_id,result_version) REFERENCES record_versions(record_id,version) ON DELETE RESTRICT
) STRICT;
```
<!-- END MCE4D_DDL_RECORDS_001 -->

### DDL — index_003

<!-- BEGIN MCE4D_DDL_INDEX_003 -->
```sql
CREATE TABLE domain_entities (
 generation_id TEXT NOT NULL REFERENCES generations(generation_id) ON DELETE CASCADE,
 entity_id TEXT NOT NULL, kind TEXT NOT NULL, qualified_key TEXT NOT NULL,
 plane TEXT NOT NULL CHECK(plane IN('intended','observed_static')),
 payload_json TEXT NOT NULL CHECK(json_valid(payload_json)),
 source_refs_json TEXT NOT NULL CHECK(json_valid(source_refs_json)),
 PRIMARY KEY(generation_id,entity_id)
) STRICT;
CREATE INDEX entities_kind ON domain_entities(generation_id,kind,qualified_key,entity_id);
CREATE TABLE entity_links (
 generation_id TEXT NOT NULL REFERENCES generations(generation_id) ON DELETE CASCADE,
 link_id TEXT NOT NULL, source_symbol_id TEXT, source_entity_id TEXT,
 target_symbol_id TEXT, target_entity_id TEXT, kind TEXT NOT NULL,
 evidence_json TEXT NOT NULL CHECK(json_valid(evidence_json)),
 resolution_status TEXT NOT NULL CHECK(resolution_status IN('exact_in_supported_scope','candidate','unresolved')),
 PRIMARY KEY(generation_id,link_id),
 FOREIGN KEY(generation_id,source_symbol_id) REFERENCES symbols(generation_id,symbol_id) ON DELETE RESTRICT,
 FOREIGN KEY(generation_id,target_symbol_id) REFERENCES symbols(generation_id,symbol_id) ON DELETE RESTRICT,
 FOREIGN KEY(generation_id,source_entity_id) REFERENCES domain_entities(generation_id,entity_id) ON DELETE RESTRICT,
 FOREIGN KEY(generation_id,target_entity_id) REFERENCES domain_entities(generation_id,entity_id) ON DELETE RESTRICT,
 CHECK((source_symbol_id IS NOT NULL)+(source_entity_id IS NOT NULL)=1),
 CHECK((target_symbol_id IS NOT NULL)+(target_entity_id IS NOT NULL)=1)
) STRICT;
CREATE INDEX entity_links_out ON entity_links(generation_id,source_entity_id,kind,link_id);
CREATE INDEX entity_links_in ON entity_links(generation_id,target_entity_id,kind,link_id);
CREATE INDEX symbol_entity_out ON entity_links(generation_id,source_symbol_id,kind,link_id);
CREATE INDEX symbol_entity_in ON entity_links(generation_id,target_symbol_id,kind,link_id);
CREATE TABLE contract_fields (
 generation_id TEXT NOT NULL, entity_id TEXT NOT NULL, field_path TEXT NOT NULL,
 direction TEXT NOT NULL CHECK(direction IN('input','output','bidirectional','stored')),
 field_json TEXT NOT NULL CHECK(json_valid(field_json)),
 PRIMARY KEY(generation_id,entity_id,field_path,direction),
 FOREIGN KEY(generation_id,entity_id) REFERENCES domain_entities(generation_id,entity_id) ON DELETE CASCADE
) STRICT;
CREATE TABLE state_transitions (
 generation_id TEXT NOT NULL, machine_entity_id TEXT NOT NULL,
 transition_id TEXT NOT NULL, from_entity_id TEXT NOT NULL, to_entity_id TEXT NOT NULL,
 guard_ref_json TEXT CHECK(guard_ref_json IS NULL OR json_valid(guard_ref_json)),
 effect_refs_json TEXT NOT NULL CHECK(json_valid(effect_refs_json)),
 evidence_json TEXT NOT NULL CHECK(json_valid(evidence_json)),
 PRIMARY KEY(generation_id,machine_entity_id,transition_id),
 FOREIGN KEY(generation_id,machine_entity_id) REFERENCES domain_entities(generation_id,entity_id) ON DELETE CASCADE,
 FOREIGN KEY(generation_id,from_entity_id) REFERENCES domain_entities(generation_id,entity_id) ON DELETE RESTRICT,
 FOREIGN KEY(generation_id,to_entity_id) REFERENCES domain_entities(generation_id,entity_id) ON DELETE RESTRICT
) STRICT;
UPDATE schema_meta SET schema_version=3,min_reader_version=3 WHERE singleton=1;
CREATE TRIGGER domain_entities_insert_frozen BEFORE INSERT ON domain_entities
WHEN (SELECT state FROM generations WHERE generation_id=new.generation_id)!='BUILDING'
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER domain_entities_update_frozen BEFORE UPDATE ON domain_entities
WHEN (SELECT state FROM generations WHERE generation_id=old.generation_id)!='BUILDING'
 OR (SELECT state FROM generations WHERE generation_id=new.generation_id)!='BUILDING'
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER domain_entities_delete_frozen BEFORE DELETE ON domain_entities
WHEN (SELECT state FROM generations WHERE generation_id=old.generation_id) IN('READY','READY_PARTIAL')
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER entity_links_insert_frozen BEFORE INSERT ON entity_links
WHEN (SELECT state FROM generations WHERE generation_id=new.generation_id)!='BUILDING'
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER entity_links_update_frozen BEFORE UPDATE ON entity_links
WHEN (SELECT state FROM generations WHERE generation_id=old.generation_id)!='BUILDING'
 OR (SELECT state FROM generations WHERE generation_id=new.generation_id)!='BUILDING'
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER entity_links_delete_frozen BEFORE DELETE ON entity_links
WHEN (SELECT state FROM generations WHERE generation_id=old.generation_id) IN('READY','READY_PARTIAL')
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER contract_fields_insert_frozen BEFORE INSERT ON contract_fields
WHEN (SELECT state FROM generations WHERE generation_id=new.generation_id)!='BUILDING'
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER contract_fields_update_frozen BEFORE UPDATE ON contract_fields
WHEN (SELECT state FROM generations WHERE generation_id=old.generation_id)!='BUILDING'
 OR (SELECT state FROM generations WHERE generation_id=new.generation_id)!='BUILDING'
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER contract_fields_delete_frozen BEFORE DELETE ON contract_fields
WHEN (SELECT state FROM generations WHERE generation_id=old.generation_id) IN('READY','READY_PARTIAL')
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER state_transitions_insert_frozen BEFORE INSERT ON state_transitions
WHEN (SELECT state FROM generations WHERE generation_id=new.generation_id)!='BUILDING'
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER state_transitions_update_frozen BEFORE UPDATE ON state_transitions
WHEN (SELECT state FROM generations WHERE generation_id=old.generation_id)!='BUILDING'
 OR (SELECT state FROM generations WHERE generation_id=new.generation_id)!='BUILDING'
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
CREATE TRIGGER state_transitions_delete_frozen BEFORE DELETE ON state_transitions
WHEN (SELECT state FROM generations WHERE generation_id=old.generation_id) IN('READY','READY_PARTIAL')
BEGIN SELECT RAISE(ABORT,'IMMUTABLE_GENERATION'); END;
```
<!-- END MCE4D_DDL_INDEX_003 -->

### DDL — index_004

<!-- BEGIN MCE4D_DDL_INDEX_004 -->
```sql
CREATE TABLE ui_build_maps (
 build_map_id TEXT PRIMARY KEY NOT NULL CHECK(length(build_map_id)=64),
 schema_version INTEGER NOT NULL CHECK(schema_version=1),
 profile_id TEXT NOT NULL, provider_version TEXT NOT NULL,
 provider_digest TEXT NOT NULL CHECK(length(provider_digest)=64),
 build_config_digest TEXT NOT NULL CHECK(length(build_config_digest)=64),
 input_manifest_digest TEXT NOT NULL CHECK(length(input_manifest_digest)=64),
 mode TEXT NOT NULL CHECK(mode IN('development','test')),
 artifact_hash TEXT NOT NULL CHECK(length(artifact_hash)=64),
 metadata_json TEXT NOT NULL CHECK(json_valid(metadata_json)), created_at TEXT NOT NULL
) STRICT;
CREATE TABLE ui_source_anchors (
 build_map_id TEXT NOT NULL REFERENCES ui_build_maps(build_map_id) ON DELETE CASCADE,
 anchor_id TEXT NOT NULL CHECK(length(anchor_id)=64),
 kind TEXT NOT NULL CHECK(kind IN('element_site','component_definition','component_render_site','event_binding_site')),
 path TEXT NOT NULL, source_hash TEXT NOT NULL CHECK(length(source_hash)=64),
 start_byte INTEGER NOT NULL CHECK(start_byte>=0), end_byte INTEGER NOT NULL CHECK(end_byte>=start_byte),
 owner_anchor_id TEXT,
 PRIMARY KEY(build_map_id,anchor_id),
 FOREIGN KEY(build_map_id,owner_anchor_id) REFERENCES ui_source_anchors(build_map_id,anchor_id) DEFERRABLE INITIALLY DEFERRED
) STRICT;
CREATE INDEX anchors_source ON ui_source_anchors(build_map_id,path,start_byte,end_byte);
CREATE TABLE ui_build_bindings (
 binding_id TEXT PRIMARY KEY NOT NULL CHECK(length(binding_id)=64),
 build_map_id TEXT NOT NULL REFERENCES ui_build_maps(build_map_id) ON DELETE CASCADE,
 profile_id TEXT NOT NULL, source_generation_id TEXT NOT NULL,
 policy_digest TEXT NOT NULL CHECK(length(policy_digest)=64),
 resolver_digest TEXT NOT NULL CHECK(length(resolver_digest)=64),
 evidence_digest TEXT NOT NULL CHECK(length(evidence_digest)=64),
 created_at TEXT NOT NULL,
 UNIQUE(binding_id,build_map_id),
 UNIQUE(build_map_id,profile_id,source_generation_id,policy_digest,resolver_digest),
 FOREIGN KEY(profile_id,source_generation_id) REFERENCES generations(profile_id,generation_id) ON DELETE RESTRICT
) STRICT;
CREATE INDEX build_bindings_gen ON ui_build_bindings(source_generation_id,build_map_id);
CREATE TABLE ui_anchor_links (
 binding_id TEXT NOT NULL, build_map_id TEXT NOT NULL, link_id TEXT NOT NULL, anchor_id TEXT NOT NULL,
 target_ref_json TEXT NOT NULL CHECK(json_valid(target_ref_json)),
 kind TEXT NOT NULL, resolution_status TEXT NOT NULL,
 evidence_json TEXT NOT NULL CHECK(json_valid(evidence_json)),
 PRIMARY KEY(binding_id,link_id),
 FOREIGN KEY(binding_id,build_map_id) REFERENCES ui_build_bindings(binding_id,build_map_id) ON DELETE CASCADE,
 FOREIGN KEY(build_map_id,anchor_id) REFERENCES ui_source_anchors(build_map_id,anchor_id) ON DELETE CASCADE
) STRICT;
CREATE INDEX anchors_reverse ON ui_anchor_links(binding_id,anchor_id,link_id);
UPDATE schema_meta SET schema_version=4,min_reader_version=4 WHERE singleton=1;
CREATE TRIGGER ui_build_maps_immutable BEFORE UPDATE ON ui_build_maps BEGIN SELECT RAISE(ABORT,'IMMUTABLE_UI_METADATA'); END;
CREATE TRIGGER ui_source_anchors_immutable BEFORE UPDATE ON ui_source_anchors BEGIN SELECT RAISE(ABORT,'IMMUTABLE_UI_METADATA'); END;
CREATE TRIGGER ui_build_bindings_immutable BEFORE UPDATE ON ui_build_bindings BEGIN SELECT RAISE(ABORT,'IMMUTABLE_UI_METADATA'); END;
CREATE TRIGGER ui_anchor_links_immutable BEFORE UPDATE ON ui_anchor_links BEGIN SELECT RAISE(ABORT,'IMMUTABLE_UI_METADATA'); END;
CREATE TRIGGER ui_anchors_bound_insert BEFORE INSERT ON ui_source_anchors
WHEN EXISTS(SELECT 1 FROM ui_build_bindings WHERE build_map_id=new.build_map_id)
BEGIN SELECT RAISE(ABORT,'BUILD_MAP_ALREADY_BOUND'); END;
```
<!-- END MCE4D_DDL_INDEX_004 -->

### DDL — records_002

<!-- BEGIN MCE4D_DDL_RECORDS_002 -->
```sql
CREATE TABLE evidence_artifacts (
 artifact_id TEXT PRIMARY KEY NOT NULL CHECK(length(artifact_id)=64),
 artifact_kind TEXT NOT NULL CHECK(artifact_kind IN('runtime_trace','test_report','coverage_report','deployment_metadata')),
 input_hash TEXT NOT NULL CHECK(length(input_hash)=64),
 sanitizer_version TEXT NOT NULL,
 metadata_json TEXT NOT NULL CHECK(json_valid(metadata_json)),
 created_at TEXT NOT NULL
) STRICT;
CREATE TABLE evidence_items (
 artifact_id TEXT NOT NULL REFERENCES evidence_artifacts(artifact_id) ON DELETE CASCADE,
 item_id TEXT NOT NULL, kind TEXT NOT NULL,
 payload_json TEXT NOT NULL CHECK(json_valid(payload_json)),
 source_binding_json TEXT NOT NULL CHECK(json_valid(source_binding_json)),
 PRIMARY KEY(artifact_id,item_id)
) STRICT;
CREATE INDEX evidence_kind ON evidence_items(artifact_id,kind,item_id);
UPDATE schema_meta SET schema_version=2,min_reader_version=2 WHERE singleton=1;
```
<!-- END MCE4D_DDL_RECORDS_002 -->

## S45 — Thuật toán chuẩn và state transitions không được tự suy diễn

### AL-01 — Index/analyzer contracts

Inventory sorted raw relative path bytes được hỗ trợ; no symlink/ADS/UNC escape; enumerate tracked+allowed untracked và sparse/nested boundaries. Capture regular files qua secured handle, stat/identity before+after, hash raw bytes. Source range luôn [start,end). Invalid UTF8/path/file deny có diagnostic reason; không zero-byte substitution.

Go `go/packages.Load` profile được pin, Overlay chứa captured inputs; custom ParseFile phải dùng provided src khớp capture hoặc reject; không âm thầm đọc disk mới. Loader có thể đọc metadata/dependencies nên preflight allowed closure + no custom driver/hooks là bắt buộc; overlay không sandbox. Type values (`types.Object`, `types.Type`) của hai Load khác nhau không dùng để so identity trực tiếp: mỗi load run registry riêng; normalize facts sang stable keys đã định nghĩa rồi mới join qua source/import identity có evidence [REF07].

Parse cache key=(bytes,syntax provider/options). Semantic cache=(complete input closure,config/profile/policy/provider). Negative facts không reuse qua membership change. `x += y` read+write x chỉ khi binding chứng minh; pointer alias unknown không fake READS actual target. Interface target lưu method và candidate implementations riêng. Count declared file/test variants không tăng caller count vì cùng occurrence được type-check nhiều lần.

Provider process payload chỉ snapshots/allowed paths đúng contract; cancellation by request ID/close/terminate. AST response nhận như untrusted: validate source hash/ranges/endpoints/kinds before store. Foreign source not allowlisted không trả snippets. Layer này không execute user tests, training module, notebook, target build plugin hoặc migration.

Provider contract S43: ProviderStart/Batch/Done/Cancel. Go analyzer in-process implements `Analyze(ctx, input, sink) error` theo cùng normalized facts; method signature là API nội bộ dự án, không API thư viện upstream. Go compiler objects không ra khỏi analyzer. Node worker nhận snapshot_root + manifest_path do coordinator tạo dưới owned scratch, manifest digest/profile/source hash xác minh; không nhận URL/client-supplied arbitrary path. Snapshot materialization chỉ chứa approved captured inputs, access sanitized dependency metadata theo profile; no source reread để lách snapshot. Worker identity/config cũng allowlisted, không OS sandbox tuyệt đối.

Start một job/scope mỗi worker; batching sequence strict tăng1, symbol/package facts trước consumer relations hoặc stage relation tới endpoints ready trong BUILDING; no public reads staging. Frame <=16MiB; partition source closure lớn dùng manifest file reference, không nhét tất cả bytes vào một frame. Done input_digest phải đúng capture; thiếu Done/job wrong/digest mismatch không publish complete. Output bytes/range/hash/FK validated bởi coordinator. Cancel frame best effort rồi terminate theo DF20; no unbounded sink backpressure. Batch có unknown kind hoặc oversized diagnostic là provider error, không drop im lặng. Python provider trước enable phải bổ sung PythonProfile schema tại T057 theo same seal, không accept EffectiveProfile=any để chạy.

### AL-02 — Context selection/order

Resolve target exact → initial source/constraint obligations → reference catalog + boundary ledger → task-specific graph traversal với visited tuple(gen,node,kind) → group by module/semantic use → merge overlapping source ranges same file/hash → stable sort (required category, relation kind, path,start,id) → serialize whole items under cap. New UI selection chỉ cung cấp seed; không cắt dependencies chỉ vì UI nhỏ.

No universal depth=2 sufficiency. Bound traversal time/nodes/depth produces gap. Graph walk order không proof execution. Cycles giữ representative cycle witness + expansion, không xóa cho layout. A required item không vừa budget phải pending với descriptor; consumer chưa được mark reviewed. Repeated page sau progress token phải tiếp tục item/order trong same vector; duplicate item IDs không làm AI đọc mãi cùng page.

### AL-03 — Selection reducer (browser owner)

State gồm selected GraphRef, source hash/scope vector, optional live instance, flow occurrence, mode pin, selection_seq, active resolver request IDs, viewport state. Hover state riêng, không copy graph nodes mỗi frame. Khi commit click: seq+=1, cancel old resolver, send typed request; response chỉ apply khi selection_seq+scope+build+epoch+requestId còn khớp. Render/update highlight ACK **không** dispatch user SELECT event. Graph/flow/source đều dùng một reducer; ReactFlow local layout không được mutation domain edges.

Click flow step giữ occurrence và root; graph-origin chọn operation mới mới re-root. Multi-instance/ambiguous đưa chooser; unique anchor chưa chắc unique runtime instance. Preview not mounted vẫn source/graph view, không tự render UI giả, navigation hoặc auth.

### AL-04 — Session pairing và mode state machine

| Input / điều kiện | Transition và hành động |
|---|---|
| Attach valid permission+origin+scope | Server creates pairing, revision1, doc_epoch1, hmr0, auth_epoch0, mode inspect; browser installs shield trước iframe load |
| iframe load/reload | Browser giữ shield; old listeners/requests disposed; origin/window verify; old session detach hoặc invalidate. Document epoch chỉ theo server acknowledgement, không SDK tự mở scope |
| Parent CHALLENGE | Gửi nonce/session/exact expectedOrigin; SDK chỉ trả READY tương ứng, build_map opaque/capabilities |
| READY valid | Browser POST preview/bind với expected_revision+nonce+epochs+build; server resolve imported map/source binding và source policy |
| Bind success | Server revision tăng và state bound, returns binding_id; parent gửi BOUND build+auth_epoch; SDK BOUND_ACK khớp mới client READY. No ACK trong3s → invalidate/shield |
| Request Interact | Chỉ explicit UI action+permission test profile; server expected_revision/auth epoch CAS → arming, command_id random, auth_epoch+1, deadline3s; shield vẫn còn |
| SDK MODE_ACK accepted | Browser verify session/build/doc/hmr/command/epoch; POST preview/ack; server còn arming+not_expired mới interact; browser bỏ shield chỉ sau response khớp selection/session |
| Wrong/late/rejected ACK / timeout | Không chuyển interact; server inspect hoặc stale, auth_epoch invalidate; browser shield. Không auto retry mode escalation |
| Disarm/mode Inspect | Browser bật shield synchronously TRƯỚC request; server inspect+epoch++ ngay, sends SET_MODE inspect best effort; ack không là prerequisite safety |
| HMR/build/origin/policy/group change | Browser shield+cancel first; preview/invalidate CAS; state stale, nonce/command invalid, auth_epoch++; mới bind/source check mới ready |
| Detach/server stop | Shield/remove iframe if mounted, revoke session and bearer as scoped, cleanup observers/timers/ports/weak refs; do not kill user's app |

Preview session TTL30 phút, bearer TTL30 phút maximum không vượt source permission; bootstrap code32 randombytes base64url TTL60s single-use, max5 attempts/min/process rồi rate-limit không global lock. Command ACK deadline3s; graceful detach timeout1s rồi local cleanup. Query defaults theo S15. Bridge PING/PONG kiểm SDK liveness riêng server status; polling không gia hạn session/bearer/quyền và không bump revision. Heartbeat chỉ liveness hint, không source freshness proof; server `POST /preview/status` polling2s khi visible/5s hidden (CT-08), timeout2 missed -> local shield+disarm; in-flight race vẫn cần isolated test environment, không sandbox guarantee.

Bridge mỗi direction có increasing seq và dedupe message_id bounded256 entries/session. Reply phải match pending reply_to/type; event unsolicited chỉ allow BUILD_CHANGED/INSTANCE_INVALIDATED/CAPABILITY_STATUS/ERROR theo state; PONG chỉ reply PING pending (không unsolicited). HMR notification phải ở old authorized session với new_hmr_epoch cao hơn; invalidated epoch không dùng PICK responses. Không ACK vòng lặp, không echo full messages vào log. Parent/SDK directions closed ở CT registry.

### AL-05 — Build maps và source-generation binding

Producer transform chạy trong user-approved dev build: original TSX bytes → supported AST nodes → opaque anchors → instrumentation output + map artifact. Anchor/build-map hashes theo recipe CT-04; owner_anchor relationship không dựa DOM parent. partial/lazy coverage là dữ liệu thật; provider_digest của instrumenter có trong manifest và anchor recipe. Không dùng provider_version string đơn lẻ thay fingerprint. Native JSX host attach opaque attr, không bọc DOM/handlers làm đổi props/this/hooks.

Engine imports local artifact trước binding; build map KHÔNG chứa engine generation/symbol IDs. Engine finds candidate published generation explicit profile rồi so original hashes/ranges. Same source bytes in a newer index can bind through new binding ID; không rewrite producer ID vì HEAD khác. Source changed but graph old -> stale/unbound. Reverse graph mapping returns CT-05 paged MappingMatch rows (anchor_ref/target_ref cùng một match), browser LIST_INSTANCES obtains instances; server không đọc DOM/không lưu per-pixel/props.

Explicit `registerBoundary` API local SDK nhận DOM handles và opaque anchor/known parent, trả dispose; no private Fiber. Unknown third-party/portal/render-site preserve candidate/ancestor, not false exact. Production build inspector flag bị strip bất kể tên env; negative fixture no attrs/SDK/private mapping endpoint. Dev assets may run code: target tests/data isolation không thay được bằng Inspect shield.

### AL-06 — Static/full-stack/observed flow

Select operation -> resolve root candidates -> static call graph occurrence projection -> route/middleware bindings -> typed database/sql sites -> supported literal SQL parse -> table/column/transaction evidence -> contract bridge to FE consumers. Definition vs invocation occurrence tách. For branches only known supported AST/control patterns add conditional edges; unknown expression does not become false/unreachable. Async goroutine/send not sequential edge.

Bridge match keys include namespace/service/method/route/profile/baseURL/rewrite/version. No URL text-only joins. Symbol name giống span name chỉ candidate. Runtime import must be separate observed plane; attempt/statement_success/commit/rollback/outcome_unknown không collapsed thành saved. No DB query/EXPLAIN or request executed to draw flow. R3 optional có dữ liệu runtime thì source revision binding/check first; missing trace không no-dependency.

### AL-07 — Storage/runtime state recovery

Generation BUILDING đến READY/READY_PARTIAL/FAILED/CANCELLED; published content immutable; retirement only nonactive via GC. Process chết để BUILDING abandoned; next owner kiểm job metadata và mark FAILED, không inherit writer từ TTL. Failed attempt never becomes active. Records versions append-only, heads CAS; effective needs_review derived. UI maps immutable; deleting source generation expires its binding, not remap newest silently. Query cache evict/cancel no durable state loss; user records survive index reset.

### AL-08 — Chu trình HMR và nhập map local có kiểm soát

Baseline import một lần vẫn CLI explicit. Opt-in `mce4d explorer-map watch --worktree-id <id> --profile-id <id> --directory <approved-owned-map-dir>` là foreground process do user khởi chạy, không auto daemon/service/target build. T085 producer export file bất biến `<build_map_id>.json.tmp`→flush→atomic rename `<build_map_id>.json`; file committed không sửa. Directory ở MCE-owned state/scratch hoặc path được duyệt, không target source. Watch chỉ regular files suffix/schema đúng, deny symlink/traversal/oversize (>64MiB mặc định), kiểm lần đọc ổn định+digest; không fetch URL.

Event coalesce mỗi worktree tối đa1 pending latest *notification*, nhưng index manifests vẫn content-addressed và validation đầy đủ; poll2s/debounce300ms là hints không bằng chứng source fresh. Import qua same writer lock/use case DF09, retry busy tối đa1 trong deadline rồi báo map pending/unavailable; không giữ lock trong preview session hoặc tạo writer mới. Producer sequence/lazy module changes không thể trộn anchors của hai map. Khi BUILD_CHANGED: browser shield/disarm ngay, resolve map đã import và source hash; map chưa có → awaiting_map (UI loading/stale), không tự bind old map dưới ID mới. Sau matching import+binding mới pairing/ACK theo AL-04; Interact không tự bật lại.

Một HMR notification không cấp quyền command, import hay mở directory. Watch permission chỉ đọc directory và ghi own map metadata/cache. Lỗi watcher/map parsing không làm source index invalid; graph/source old vẫn historical. T091 đo import queue age+lock wait+rebind time; nếu writer index dài, UI báo pending và cho retry rõ, không ghost-ready. Không bypass shared writer để UI nhìn nhanh. Stop watch dừng own workers, không kill dev app; artifact cleanup cần policy, không tự dọn thư mục người dùng.

### AL-09 — Chính sách không tự mở tính năng trong audit closure

A01–A08 đã được chốt ở các owner sections/schema/DDL, không phải yêu cầu implement ngoài thứ tự. Chỉ code những operation đã tới task/gate; metadata/conformance tests có thể chuẩn bị sớm. RuntimeConfig không làm core tự hỗ trợ Python, DB engines hay frontend khác. Resolver/pagination/store-recovery tạo đúng ranh giới cần cho chức năng đã được yêu cầu, không graph cluster hoặc orchestration framework mới.

T105/T107 kiểm hash/selector/config/continuation validators và state models; T106 SQL+GC+binding/incarnation proof; T010 có lifecycle+writer adapter; T025 pagination/spool primitives; T029 rename spool provider; T081 session/admin/schema integration; T087 status/quyền/PING; T091 map watch; T035 restore/recovery. Nếu có actual version cũ đã chạy, bản này là proposed migration baseline cho dữ liệu đó cho đến khi forward migration được duyệt và test; không dùng approval sửa tài liệu để xóa/reset store.

### AL-10 — Mọi đường ra phải có câu trả lời hoặc lỗi có thể hành động

Mỗi operation test success, zero-result, partial, ambiguous, stale, too-large, timeout, cancel, denied, expired và recovery-required theo applicability. Không tạo next_cursor khi client không có request schema để dùng; không trả operation success với mandatory fields không khớp enum/shape; không có pagination loop progress0. Unknown totals null và explicit gaps. Read-only query không tạo thêm policy/permission hoặc index ngầm. Capture/provider/DB result/transport caches phải có cùng input identities; nguồn thiếu vẫn thiếu, không làm đầy bằng conventional name hoặc previous chat.


## S46 — Quy định code từ dòng đầu và Definition of Ready

### CODE-01 — Module import direction

`internal/model` chứa validated value types/errors/IDs/refs và pure normalization; không SQL/HTTP/SDK/exec/React. `internal/app` điều phối use cases qua consumer-owned small ports. `internal/store/sqlite` implements typed ports và imports model/ports; MUST NOT import app. `internal/ports` là vị trí cố định cho interface đi qua boundary app↔adapter; chỉ port đang có consumer thật, không God interface. Private helper interface không qua boundary đặt tại consumer. `cmd/mce4d` composition root tạo concrete adapters, inject app services. CLI/MCP/Explorer adapters gọi app, không trực tiếp query SQL/parse source.

Dependency order: model → pure helpers/policy/ranges → small ports → concrete store/provider adapters → app orchestration → CLI/MCP/HTTP → UI. Indexer owns jobs/publish; contextpack owns selection/budget; uimap owns binding; flowview owns projection. Analyzer không gọi transport, UI không invent relations, no global singleton mutable current_repo/session. Test import boundary tự đọc Go imports, allowlist layer edges; cycles or cross-layer SQL fail CI.

### CODE-02 — Chuẩn Go

Go identifiers/comments tiếng Anh; spec/description Vietnamese; exported names chỉ khi module boundary cần. Functions receive context.Context first khi I/O; không lưu request context vào long-lived service, không dùng Background thay cancel caller. Constructors validate config/dependencies, return error khi thiếu; không hidden side effects ở init/import. Standard library trước; không fmt.Println vào MCP stdout.

Error có typed code + op/request metadata, wrap with context, errors.Is/As; không ignore close/rollback/write errors cần xử lý. Primary error giữ nguyên, cleanup secondary log sanitized. Panic chỉ internal invariant không recoverable; boundary recover log sanitized+INTERNAL_ERROR, không biến panic thành success. Không sleep để chữa race; bounded channel/workers; close bởi owner; cancel child và wait trước return. Time/random injected interfaces nhỏ cho test, runtime UTC và monotonic duration. No timestamp/PID as UUID uniqueness, no default zero as unknown count.

SQL qua database/sql prepared/parameter binding; scan/rows.Err/close kiểm tra; một transaction dùng cùng *sql.Tx hoặc cùng connection-managed transaction adapter đã chốt, không DB.Exec xen giữa. Writer lock/revision check ở application + repository invariant guard. No raw BEGIN/COMMIT rải rác trong use case; transaction manager dùng SDK semantics đã verify. Config immutable sau startup, requests chỉ được thu hẹp quyền. Every connection policy applied/tested, no global mutex all repos.

### CODE-03 — Chuẩn frontend / SDK / TypeScript

`strict=true`, noUncheckedIndexedAccess và exactOptionalPropertyTypes bật từ bootstrap UI; nullable theo schema, không non-null assertion che lỗi. Unknown external data giữ unknown đến decoder; no implicit any, no cast để bỏ validation. npm workspaces private, một lockfile. React reducer/hooks theo supported API; no package import side effects tự attach target. AbortController cho queries, stale-response guard independent cancellation. Effects luôn cleanup; global events registered exactly once per session/epoch.

CSS Modules/native CSS theo components, UI layout2D không business graph. Không dùng dangerous HTML injection, eval/new Function, arbitrary iframe URLs, localStorage bearer hoặc public CDN scripts. SDK không import engine client/secret/source tools. Source drawer plain escaped text+range đầu tiên; không nhúng full IDE/terminal. Flow/graph labels từ sanitized source facts, tooltip không dump SQL params/DOM values. Lazy rendering bounded, no source tree copy vào browser state.

Tests frontend: built-in Node test runner cho pure SDK/provider utilities nơi đủ; React component tests Vitest + Testing Library, E2E Playwright theo version/license gate T081. Bổ sung dependency testing có lý do; không dùng screenshot-only làm oracle mapping. jsdom không chứng minh iframe/origin/HMR/real browser behavior; browser fixture bắt buộc.

### CODE-04 — Contracts/tests trước implementation

Mỗi task theo thứ tự nội bộ cố định: (1) xác định mode/assignment, read work card+source+policy/dependency evidence; (2) reproduce/define oracle; (3) add contract/negative/failure tests thuộc task; (4) implementation tối thiểu; (5) positive+negative+integration; (6) formatting/build/lint/static; (7) inspect diff/security/canary; (8) update evidence+task+resume. Test red là trạng thái có chủ ý trước implementation, không evidence completion.

Không copy output engine làm expected; fixtures tự tạo với source/reference truth lập độc lập. No fake data in runtime to satisfy UI; mocks chỉ tests, dependency injection explicit. Không tạo method TODO return nil/empty success, code commented-out scaffold hay disabling tests. Không tự update goldens/golden snapshots nếu chưa giải thích diff. Không thêm feature ngoài card. New migrations/API/permissions cần preapproved contract, không “để sau tối ưu”.

### CODE-05 — Ready checklist, dừng đúng chỗ

Task Ready iff: mode/assignment và nguồn quy tắc S50 được resolve cho task; Change Budget cùng command preconditions S51 đã xác minh khi áp dụng; dependency DONE đúng scope; previous gate PASS; versioned blueprint/schema present; target file/path ownership rõ; input/output/failure/limits fixtures known; dependency APIs verified; permissions đủ; no open blocker affecting this task. Chỉ T001/T002/T003/T105–T108 là foundation/admin/contract proof theo BP-02, không yêu cầu product đã tồn tại; các task khác không được miễn gate. Optional scope closed không Ready dù agent rảnh.

Thiếu credential/repo/build metadata không liên quan fixture thì triển khai/test fixture và label chính xác, không bịa real integration. Không được đánh dấu task yêu cầu real evidence DONE nếu thiếu evidence mà chưa có defined fixture-label handover rule. Một task kiểm conformance không được tự thay contract để implementation pass.

### CODE-06 — Verification/CI mỗi lát cắt

Core: gofmt changed Go files; go test relevant packages; go test ./...; go vet ./...; go build ./cmd/mce4d; go run ./tools/spec_check; contract/schema + import-boundary tests. Race suite khi platform/toolchain/cgo hỗ trợ; NOT_RUN ghi nguyên nhân và gate cần chờ, không giả PASS. Fuzz bounded targets, no network. Tests use temporary unique MCE4D_HOME, no user's registry.

UI sau T081 creates package scripts chính xác: `typecheck`, `test`, `build`, `test:e2e`, `test:production-strip`. npm ci theo lock, npm run <script> -w <workspace thật đã khai báo>; không chạy placeholder hoặc script chưa tạo. Production strip runs on controlled fixture, target tree content hashes before/after assertion. CI build matrix Windows native/Linux/WSL evidence như S17, không đánh đồng cross-compile với runtime PASS.

Critical continuous regressions sau mọi đổi model/schema/query/policy: C001/C003/C013/C019/C020/C021/C024/C025/C028/C030/C041 và C113–C136 theo scope. UI changes thêm bidirectional/mode/HMR/security subset. DB changes migration/rollback/FTS/CAS/GC. Security/tool version review trước release; no bypass SSL, ignore errors or infinite retries to pass network-off tests.

### CODE-07 — Definition of Done và sửa lỗi sau DONE

DONE = output contract implemented + matching tests PASS + necessary integration PASS + no remaining blocking gaps + evidence revision/digest/commands/environment + applicable V0–V4 và policy/command checks + diff reviewed + current tracker validator PASS. File tồn tại/compile pass không đủ. VERIFY không tính completion. Sau regression, reopen đúng task(s), giữ evidence cũ historical và ghi nguyên nhân; không reset toàn dự án hoặc xóa lịch sử. Một công việc có thể cần thêm test/sửa lỗi thực tế; đây là kiểm soát chất lượng, không được che giấu để giữ hình ảnh “làm một lần xong”.

### CODE-08 — Change Budget và Complexity/Abstraction Gate

Trước ghi, bổ sung phiếu S47 bằng ghi chú task S21: mục tiêu, mode/assignment, baseline, vùng file VÀ hành vi được sửa, contracts/bên tiêu thụ chung, phần loại trừ, kiểm tra bắt buộc, quyền và ngân sách có liên quan. Không đặt số dòng/file cố định; không chia một thay đổi cần nhất quán thành những mảnh thiếu an toàn. Khác thư mục không tự chứng minh độc lập hành vi, ports, DB/queue/cache/test resources.

Giữ stack/module/schema/flow S41–S45. Chỉ kích hoạt Complexity Gate cho delta tăng đáng kể bề mặt bảo trì/trạng thái/ownership/triển khai ngoài baseline: ghi nhu cầu+rủi ro có nguồn, phương án nhỏ hơn và vì sao thiếu, tác động/tương thích, cách kiểm/phục hồi và authority tại ADR/task hiện có. Chưa có quyết định thì không implement phần mở rộng. Không mở lại toàn kiến trúc mỗi phiên hoặc xin duyệt private helper tương đương trong task. Không lấy simplicity để tự viết crypto/parser khó, bỏ validation, idempotency, transaction, audit hay recovery bắt buộc. Rework do lỗi thật theo CODE-07; không che cờ/state mới ngoài bản vẽ.

### CODE-09 — Lệnh, code sinh và kiểm soát bằng chứng

Trước command xem contract S51: executable/argv/cwd/toolchain, nguồn command, ảnh hưởng output/temp/cache/network/children và quyền. Lệnh hiện có không tự an toàn; lệnh chưa tồn tại/placeholder không chạy. Output đáng kể cần budget trên các volume thật; core quota khác build budget. Không tự install, sửa host config hoặc thêm wrapper trước khi chứng minh cần và có quyền. T002 dùng preflight nhỏ trước build đầu tiên, không chờ T033 rồi bỏ guard giai đoạn đầu.

Code/schema sinh tự động phải có source→generator/version→output set; chỉ regenerate trong quyền, kiểm diff cả ngoài docs. Không sửa tay output để khác canonical source. Bảo vệ test, validator, lint/race, expected/goldens, threshold, CI và release gate: đổi chúng cần lý do yêu cầu/oracle/compatibility đúng, impact+review; không disable/auto-bless để che regression. Không cấm sửa test sai đã chứng minh, nhưng phải sửa đúng nguồn theo BP-04 và giữ negative case.

Bằng chứng tái sử dụng chỉ khi inputs liên quan còn tương đương: source/diff, contracts/rules, dependencies/config, command/fixture, environment/profile và applicability. Không dùng cached result sau merge hoặc compaction chỉ vì từng PASS. Sau tích hợp kiểm bản kết hợp và consumers bị ảnh hưởng; baseline lỗi/thiếu tool/quyền ghi riêng, không đổi thành NOT_APPLICABLE. No ours/theirs toàn bộ, no force/reset/clean/stash hoặc mutation workspace khác ngoài quyền.

### CODE-10 — Hoàn thành task khác với tạo commit

Test/review/cập nhật S19/S21/S22 sau từng task. Không commit ngay khi một task DONE hoặc cuối phiên; theo S53 đủ10 task DONE mới stage toàn bộ nhóm, kiểm bản kết hợp, tạo một commit và thử khôi phục/tag. Chưa commit có thể dùng content/tree digest và baseline+diff làm bằng chứng; không bịa SHA. Gate theo chất lượng/prerequisite không chờ commit lẻ. File mới/lockfile/test/migration/docs bắt buộc phải trong snapshot; không dựa vào working tree local để che file thiếu trong commit. Proof tests của policy nằm ở tools/spec_check/tests hiện có, không thêm hook hoặc Git-writing capability vào engine.

## S47 — Thứ tự thi công chính xác và phiếu việc

Đây là work order mặc định, không bảng progress thứ hai. S19 giữ trạng thái/evidence. Mỗi task chỉ xuất hiện một lần; thứ tự dưới đã topo-sort theo dependencies và tracks. Không chọn task chỉ vì ID nhỏ hơn hoặc vì UI nhìn dễ hơn. P00–P11 là đường sản phẩm yêu cầu; optional P12–P14 chỉ khi mở scope. R2 subtask DEFERRED chuyển TODO khi đến phase và scope được mở, không gọi DEFERRED là DONE.

Nhịp checkpoint theo S53 đi xuyên các phase/gate khi scope tiếp tục: mỗi10 task DONE theo chính lịch dưới. Không sửa thứ tự/dependency để khớp số10; mọi phiếu cũng phải kiểm batch state trước khi bắt đầu task kế.

### Forecast và mốc báo cáo — không thay work order

Trước hoặc trong T001, lập forecast nhẹ tại S21 nếu assignment cần kế hoạch: phạm vi và output theo P00–P05, P06–P10 và phần tùy chọn; người/nguồn lực có thật; giả định về toolchain, quyền, test OS/browser, repo đại diện và telemetry; các phụ thuộc bên ngoài, rủi ro và khoảng effort hoặc `CHƯA ĐỦ DỮ LIỆU`. Tách effort triển khai, kiểm thử, chờ môi trường/quyền và real-repo acceptance; nêu đơn vị và độ tin cậy khi có số. Không suy thời gian từ 108 task hoặc % task DONE, không bịa deadline/công suất. T108 cập nhật sau discovery/pins/proofs; các mốc sau điều chỉnh theo actual evidence và thay đổi phạm vi. Forecast là thông tin lập kế hoạch, không gate mới, task mới, lời hứa giao hàng hay quyền chạy target. Nếu thiếu dữ kiện, tiếp tục T001 và các việc độc lập theo S47, không yêu cầu người dùng ước lượng thay.

Mốc có thể trình bày theo S20: G0 design seal; G2 bản đồ Go trên fixture; G3 luồng context CLI/MCP; G4–G5 core fixture/release handover theo evidence; GU0/GU1/GU2 lần lượt graph/source, React Inspect, static full-stack. Kết quả của mỗi mốc chỉ được gọi như vậy sau các task/gate tương ứng; G0 không phải bản demo sản phẩm. Không rút ngắn lịch hay đổi thứ tự đã duyệt chỉ để có demo sớm.

### Lịch theo phase

| Phase | Thứ tự bắt buộc | Điểm đóng |
|---|---|---|
| P00 Foundation | T001 → T002 → T003 → T105 → T106 → T107 → T108 | G0 |
| P01 Safe storage | T005 → T004 → T006 → T007 → T010 → T012 → T008 → T009 → T011 | theo S20 / final task evidence |
| P02 Correct map | T013 → T017 → T020 → T014 → T015 → T016 → T018 → T019 → T021 → T049 → T050 | theo S20 / final task evidence |
| P03 AI contracts | T022 → T023 → T024 → T025 → T029 → T031 → T051 → T052 → T026 → T027 → T028 → T030 | theo S20 / final task evidence |
| P04 Verification | T032 → T033 → T035 → T053 → T054 → T034 → T036 → T037 → T038 → T039 → T040 | theo S20 / final task evidence |
| P05 Core release | T041 → T042 | theo S20 / final task evidence |
| P06 Explorer base | T081 → T082 → T083 → T084 | theo S20 / final task evidence |
| P07 Frontend prerequisites | T043 → T055 → T044 → T045 → T056 → T047 | theo S20 / final task evidence |
| P08 Frontend inspect | T085 → T086 → T087 → T088 → T089 → T090 → T091 → T092 → T093 → T094 | theo S20 / final task evidence |
| P09 Backend contracts | T046 → T059 → T060 → T061 | theo S20 / final task evidence |
| P10 Full-stack static | T095 → T096 → T097 → T098 → T099 → T100 | theo S20 / final task evidence |
| P11 R2 remaining | T057 → T058 → T062 → T063 → T064 → T065 → T066 → T067 → T068 → T048 | theo S20 / final task evidence |
| P12 Optional observed prerequisites | T069 → T070 | theo S20 / final task evidence |
| P13 Optional observed Explorer | T101 → T102 → T103 → T104 | theo S20 / final task evidence |
| P14 Optional R3 remaining | T071 → T072 → T073 → T074 → T075 → T076 → T077 → T078 → T079 → T080 | theo S20 / final task evidence |

P01 cần G0 PASS; P02 cần G1; P03 cần G2; P04 cần G3; P05 cần G4. P06 cần G5; P08 cần GU0 + P07 capability proofs; P10 cần GU1 + P09. P11 hoàn tất G6; P12/P14 cần G6 và explicit optional scope; P13 cần GU2+runtime capability. Scope không được mở thì dừng tại release hữu ích, không tự thực hiện addon.

### Phiếu thi công từng task

Read mặc định cho mọi hàng: S00/S22/S41/S46 và S53 GIT-01/GIT-02/batch metadata + POL-01/POL-03 và nguồn theo POL-05; S51 trước command liên quan; requirement/case exact ở S01/S17, task row S19, source hiện có trong owned paths. Contracts I/O và failure lấy S42/S43; physical rows S44; algorithm S45. Cột below chỉ bổ sung task-specific route. Input thực thi gồm evidence của Dependencies trong S19; Output chính là Deliverable của S19, không acceptance bằng file có tồn tại. Mỗi hàng lập Change Budget/verification/command metadata S21, không tracker thứ hai. Mọi hàng được thêm colocated tests/fixture/artifacts riêng task và cập nhật đúng hàng tracker/evidence/resume; không mở rộng tất cả thư mục nếu task chỉ một use case.

| Seq | Task | Đọc thêm | Owned production paths / allowed test artifacts | Flow contract | Oracle / gate evidence |
|---|---|---|---|---|---|
| 001 | T001 | S43,S44,S45,S50; S53 | `go.mod,.gitignore,.gitattributes,cmd/mce4d/main.go (bootstrap only)`; docs sources read/hash only | DF00–DF20; contract proof only where implementation unavailable | C041,C137,C138,C181,C182,C185,C189,C191 |
| 002 | T002 | S43,S44,S45,S50,S51 | `go.mod,go.sum,artifacts/preflight/` | DF00–DF20; contract proof only where implementation unavailable | C039,C042,C046,C140,C141,C142 |
| 003 | T003 | S43,S44,S45,S50,S52 (route only); owner CT/DB/AL cited there; S53 | `tools/spec_check/` | DF00–DF20; contract proof only where implementation unavailable | C043,C133,C135,C137,C138,C148,C180,C181,C182,C183,C187,C190,C192 |
| 004 | T105 | S41–S49,S50,S52 (route only); owner CT/DB/AL cited there | `internal/model/,internal/ports/,contracts/,tests/contract/` | DF00–DF20; contract proof only where implementation unavailable | C113,C114,C122,C131,C134,C139,C154,C155,C156,C157,C158,C159,C160,C161,C162,C163,C164,C165,C166,C167,C168,C169,C170,C171,C172,C173,C174,C175,C176 |
| 005 | T106 | S41–S49,S52 (route only); owner CT/DB/AL cited there | `internal/store/sqlite/migrations/,tests/contract/storage*,artifacts/preflight/` | DF00–DF20; contract proof only where implementation unavailable | C115,C116,C117,C118,C119,C120,C121,C149,C150,C151,C152,C153,C171,C172,C173 |
| 006 | T107 | S41–S49,S50,S51,S52 (route only); owner CT/DB/AL cited there; S53 | `tests/contract/,testdata/fixtures/contracts/,tools/spec_check/` | DF00–DF20; contract proof only where implementation unavailable | C114,C117,C128,C129,C132,C136,C140,C141,C142,C143,C144,C145,C146,C154,C155,C156,C157,C158,C159,C160,C161,C162,C163,C164,C165,C166,C167,C168,C169,C170,C171,C172,C173,C174,C175,C176,C184,C185,C186,C187,C188,C189,C190,C191,C192 |
| 007 | T108 | S41–S49,S50,S51,S52 (route only); owner CT/DB/AL cited there; S53 | `tools/spec_check/,artifacts/preflight/` | DF00–DF20; contract proof only where implementation unavailable | C113,C133,C134,C135,C138,C139,C148,C180,C181,C182,C183,C192 |
| 008 | T005 | S06–S08,S14–S15,S42,S44,S50,S51 | `internal/paths/,internal/policy/,internal/runtime/fs*` | DF02,DF08,DF17 | C024,C025,C026,C145 |
| 009 | T004 | S06–S08,S14–S15,S42,S44,S52 (route only); owner CT/DB/AL cited there | `internal/catalog/,internal/app/registration*,internal/app/config*,internal/runtime/config*` | DF01,DF19 | C001,C016,C167,C168,C157,C169,C170 |
| 010 | T006 | S06–S08,S14–S15,S42,S44,S52 (route only); owner CT/DB/AL cited there | `internal/snapshot/,internal/inventory/capture*` | DF02,DF08,DF17 | C015,C023,C027,C174,C175,C176 |
| 011 | T007 | S06–S08,S14–S15,S42,S44,S52 (route only); owner CT/DB/AL cited there | `internal/store/sqlite/` | DF02,DF08,DF17 | C019,C037,C042,C171,C172,C173 |
| 012 | T010 | S06–S08,S14–S15,S42,S44,S52 (route only); owner CT/DB/AL cited there | `internal/runtime/lock*,internal/runtime/lifecycle*,internal/store/sqlite/ownership*` | DF02,DF08,DF17 | C021,C022,C171,C172,C173 |
| 013 | T012 | S06–S08,S14–S15,S42,S44 | `internal/inventory/` | DF02,DF08,DF17 | C011,C013,C014,C016 |
| 014 | T008 | S06–S08,S14–S15,S42,S44 | `internal/indexer/publish*,internal/store/sqlite/` | DF02,DF08,DF17 | C019,C020,C117,C136 |
| 015 | T009 | S06–S08,S14–S15,S42,S44 | `internal/indexer/recovery*` | DF02,DF08,DF17 | C019,C022,C036 |
| 016 | T011 | S06–S08,S14–S15,S42,S44 | `tests/integration/concurrency*` | DF02,DF08,DF17 | C001,C020,C021 |
| 017 | T013 | S04,S09,S45 | `internal/analysis/golang/` | DF02,DF08,DF17 | C002,C003,C004,C010 |
| 018 | T017 | S04,S09,S45 | `internal/search/` | DF03–DF07 | C002,C042,C045 |
| 019 | T020 | S04,S09,S45,S52 (route only); owner CT/DB/AL cited there | `internal/analysis/golang/loader*,internal/runtime/exec*` | DF02,DF08,DF17 | C009,C012,C046,C047,C129,C132,C174,C175,C176 |
| 020 | T014 | S04,S09,S45 | `internal/analysis/golang/` | DF02,DF08,DF17 | C003,C004,C005,C009,C129 |
| 021 | T015 | S04,S09,S45 | `internal/analysis/golang/,internal/indexer/facts*` | DF02,DF08,DF17 | C006,C007,C008,C045 |
| 022 | T016 | S04,S09,S45 | `internal/analysis/golang/,internal/model/capability*` | DF02,DF08,DF17 | C008,C010,C011,C045 |
| 023 | T018 | S06–S08,S14–S15,S42,S44 | `internal/indexer/invalidation*,internal/snapshot/` | DF02,DF08,DF17 | C013,C014,C015,C016,C017 |
| 024 | T019 | S06–S08,S14–S15,S42,S44 | `internal/snapshot/validation*,internal/app/validate*` | DF03–DF07 | C013,C015,C023,C122 |
| 025 | T021 | S04,S09,S45 | `internal/indexer/cache*` | DF02,DF08,DF17 | C017,C018 |
| 026 | T049 | S43,S44,S45 | `internal/overview/` | DF02,DF08,DF17 | C049 |
| 027 | T050 | S43,S44,S45 | `internal/indexer/cache*,internal/indexer/partition*` | DF02,DF08,DF17 | C050,C053 |
| 028 | T022 | S10–S13,S43,S45 | `internal/contextpack/` | DF03–DF07 | C002,C006,C029 |
| 029 | T023 | S10–S13,S43,S45 | `internal/contextpack/` | DF03–DF07 | C025,C026,C033,C045 |
| 030 | T024 | S10–S13,S43,S45 | `internal/contextpack/budget*,internal/app/response*` | DF03–DF07 | C028,C029 |
| 031 | T025 | S10–S13,S43,S45,S52 (route only); owner CT/DB/AL cited there | `internal/contextpack/cursor*,internal/contextpack/fragments*,internal/search/paging*,internal/lsp/result_store*` | DF03–DF07 | C028,C030,C031,C122,C158,C159,C160,C161,C162,C170,C179 |
| 032 | T029 | S10–S13,S43,S45,S52 (route only); owner CT/DB/AL cited there | `internal/lsp/` | DF03–DF07 | C027,C032,C034,C035,C158,C159,C160,C161,C162 |
| 033 | T031 | S43,S44,S45 | `internal/telemetry/,tests/integration/telemetry*` | DF03–DF07 | C035,C048 |
| 034 | T051 | S43,S44,S45 | `internal/overview/,internal/contextpack/` | DF03–DF07 | C049,C052 |
| 035 | T052 | S43,S44,S45 | `internal/snapshot/,internal/app/validate*` | DF03–DF07 | C051 |
| 036 | T026 | S10–S13,S43,S45 | `contracts/,internal/app/,tests/contract/` | DF03–DF07 | C028,C039 |
| 037 | T027 | S10–S13,S43,S45 | `internal/mcp/` | DF03–DF07 | C039,C041,C128 |
| 038 | T028 | S10–S13,S43,S45,S52 (route only); owner CT/DB/AL cited there | `internal/cli/,cmd/mce4d/` | DF03–DF07 | C040,C041,C167,C168 |
| 039 | T030 | S10–S13,S43,S45,S52 (route only); owner CT/DB/AL cited there | `internal/lsp/,internal/app/rename*` | DF03–DF07 | C032,C033,C034,C041,C158,C159,C160,C161,C162 |
| 040 | T032 | S17–S20,S48 | `testdata/malicious/,tests/acceptance/security*` | DF03–DF07 | C024,C025,C026,C034,C041,C046,C047 |
| 041 | T033 | S17–S20,S48,S50,S51,S52 (route only); owner CT/DB/AL cited there | `internal/runtime/,tests/integration/resources*,tools/run_check/ (only if existing tooling insufficient)` | DF03–DF07 | C029,C035,C036,C141,C142,C143,C144,C145,C146,C167,C168 |
| 042 | T035 | S06–S08,S14–S15,S42,S44,S50,S51,S52 (route only); owner CT/DB/AL cited there | `internal/store/sqlite/,internal/catalog/backup*,internal/runtime/maintenance*,internal/cli/store*,tests/integration/recovery*` | DF17,DF19 | C036,C037,C038,C119,C130,C145,C146,C149,C150,C151,C171,C172,C173,C160,C170,C179 |
| 043 | T053 | S43,S44,S45 | `tools/bench/,tests/acceptance/scale*` | DF02,DF08,DF17 | C053 |
| 044 | T054 | S43,S44,S45 | `tests/acceptance/self_index*,tools/bench/` | DF02,DF08,DF17 | C052,C054 |
| 045 | T034 | S17–S20,S48,S50,S51,S52 (route only); owner CT/DB/AL cited there; S53 | `tests/acceptance/,artifacts/acceptance/` | DF03–DF07 | C001..C054,C137..C148,C149,C150,C151,C181,C182,C183,C184,C185,C186,C187,C188,C189,C190,C191,C192 |
| 046 | T036 | S17–S20,S48,S52 (route only); owner CT/DB/AL cited there | `tests/integration/platform*,.github/workflows/` | DF03–DF07 | C019,C020,C021,C024,C035,C040,C171,C172,C173 |
| 047 | T037 | S17–S20,S48,S52 (route only); owner CT/DB/AL cited there | `tests/integration/platform*,internal/runtime/windows*,.github/workflows/` | DF03–DF07 | C021,C024,C027,C035,C040,C171,C172,C173 |
| 048 | T038 | S17–S20,S48 | `tools/bench/` | DF03–DF07 | C044 |
| 049 | T039 | S17–S20,S48,S52 (route only); owner CT/DB/AL cited there | `tools/bench/,artifacts/bench/` | DF03–DF07 | C018,C042,C044,C160,C170,C179 |
| 050 | T040 | S17–S20,S48,S50,S51,S52 (route only); owner CT/DB/AL cited there; S53 | `tools/spec_check/,tests/contract/` | DF00–DF20; contract proof only where implementation unavailable | C039,C040,C043,C133,C134,C135,C137,C138,C139,C147,C148,C180,C181,C182,C183,C184,C185,C186,C187,C188,C189,C190,C191,C192 |
| 051 | T041 | S17–S20,S48,S50; S53 | `artifacts/release/,LICENSE/NOTICE material as applicable` | DF00–DF20; contract proof only where implementation unavailable | C046,C047,C048,C148,C184,C185,C186,C188,C189 |
| 052 | T042 | S17–S20,S48,S50,S52 (route only); owner CT/DB/AL cited there; S53 | `artifacts/release/` | DF00–DF20; contract proof only where implementation unavailable | C001..C054,C148,C180,C186,C188,C189,C190 |
| 053 | T081 | S34–S39,S43–S45,S50,S51,S52 (route only); owner CT/DB/AL cited there | `contracts/,package.json,package-lock.json,artifacts/preflight/,tests/contract/ui*` | DF10,DF03 | C081,C082,C123,C124,C125,C126,C140,C141,C142,C152,C153,C174,C175,C176,C180 |
| 054 | T082 | S34–S39,S43–S45,S52 (route only); owner CT/DB/AL cited there | `internal/explorer/` | DF10,DF03 | C083,C154,C155,C156,C157 |
| 055 | T083 | S34–S39,S43–S45 | `web/explorer/` | DF10,DF03 | C081,C082 |
| 056 | T084 | S34–S39,S43–S45 | `tests/acceptance/explorer*,artifacts/acceptance/` | DF10,DF03 | C081..C083 |
| 057 | T043 | S24,S28–S30,S43–S45 | `packages/ts-provider/,artifacts/preflight/` | DF03–DF07 | C055 |
| 058 | T055 | S24,S28–S30,S43–S45 | `internal/analysis/providers/,internal/ports/,contracts/` | DF03–DF07 | C055,C057,C058 |
| 059 | T044 | S24,S28–S30,S43–S45 | `packages/ts-provider/` | DF03–DF07 | C055 |
| 060 | T045 | S24,S28–S30,S43–S45,S52 (route only); owner CT/DB/AL cited there | `contracts/,internal/contracts/,internal/store/sqlite/migrations/index/` | DF14,DF15,DF19 | C059,C060,C061,C114,C163,C164,C165,C166 |
| 061 | T056 | S24,S28–S30,S43–S45 | `packages/ts-provider/frontend*,internal/contracts/frontend*` | DF03–DF07 | C056 |
| 062 | T047 | S24,S28–S30,S43–S45,S52 (route only); owner CT/DB/AL cited there | `internal/history/records*,internal/store/sqlite/records*,internal/app/records*,internal/cli/records*` | DF14,DF15,DF19 | C062,C064,C120,C121,C122,C157,C169,C170,C160,C179 |
| 063 | T085 | S34–S39,S43–S45,S52 (route only); owner CT/DB/AL cited there | `packages/vite-inspector/,internal/uimap/import*,internal/store/sqlite/ui*` | DF09,DF11 | C084,C085,C089,C095,C123,C152,C153,C174,C175,C176,C177,C178 |
| 064 | T086 | S34–S39,S43–S45 | `packages/preview-sdk/,packages/react-inspector/` | DF09,DF11 | C085,C086,C093,C094 |
| 065 | T087 | S34–S39,S43–S45,S52 (route only); owner CT/DB/AL cited there | `internal/explorer/sessions*,internal/catalog/preview_permissions*,internal/cli/preview_permission*,web/explorer/src/features/preview/,packages/preview-sdk/` | DF11,DF12,DF13,DF15 | C091,C092,C123,C125,C154,C155,C156,C157,C169,C170,C177,C178 |
| 066 | T088 | S34–S39,S43–S45,S52 (route only); owner CT/DB/AL cited there | `internal/explorer/sessions*,web/explorer/src/features/preview/,packages/preview-sdk/` | DF11,DF12,DF13,DF15 | C084,C090,C091,C094,C126,C154,C155,C156,C157 |
| 067 | T089 | S34–S39,S43–S45 | `internal/uimap/resolve*,web/explorer/src/state/,web/explorer/src/features/preview/` | DF11,DF12,DF13,DF15 | C084,C085,C086,C087,C124,C127 |
| 068 | T090 | S34–S39,S43–S45,S52 (route only); owner CT/DB/AL cited there | `web/explorer/src/features/graph/,web/explorer/src/features/source/,packages/preview-sdk/` | DF11,DF12,DF13,DF15 | C087,C088,C158,C159,C160,C161,C162 |
| 069 | T091 | S34–S39,S43–S45,S52 (route only); owner CT/DB/AL cited there | `internal/uimap/,internal/cli/explorer_map*,web/explorer/src/features/preview/,packages/preview-sdk/` | DF11,DF12,DF13,DF15 | C086,C088,C089,C093,C094,C177,C178 |
| 070 | T092 | S34–S39,S43–S45,S52 (route only); owner CT/DB/AL cited there | `internal/history/records*,internal/uimap/manual*,web/explorer/src/features/preview/` | DF11,DF12,DF13,DF15 | C096,C120,C121,C130,C157,C169,C170,C160,C179 |
| 071 | T093 | S34–S39,S43–S45,S52 (route only); owner CT/DB/AL cited there | `internal/explorer/context*,web/explorer/src/api/,web/explorer/src/features/graph/` | DF11,DF12,DF13,DF15 | C097,C158,C159,C160,C161,C162,C163,C164,C165,C166 |
| 072 | T094 | S34–S39,S43–S45,S50,S51,S52 (route only); owner CT/DB/AL cited there | `tests/acceptance/explorer*,artifacts/acceptance/` | DF11,DF12,DF13,DF15 | C084..C098,C123,C124,C125,C126,C127,C147,C148,C154,C155,C156,C157,C177,C178,C180 |
| 073 | T046 | S24,S28–S30,S43–S45,S52 (route only); owner CT/DB/AL cited there | `internal/catalog/groups*,internal/contracts/federation*,internal/cli/groups*` | DF14,DF15,DF19 | C063,C163,C164,C165,C166,C157,C169,C170 |
| 074 | T059 | S24,S28–S30,S43–S45 | `internal/contracts/sql/` | DF14,DF15,DF19 | C059 |
| 075 | T060 | S24,S28–S30,S43–S45 | `internal/contracts/http/,internal/contracts/schema/` | DF14,DF15,DF19 | C060 |
| 076 | T061 | S24,S28–S30,S43–S45 | `internal/contracts/events/` | DF14,DF15,DF19 | C061 |
| 077 | T095 | S34–S39,S43–S45 | `internal/flowview/,internal/contracts/gohttp*` | DF14,DF19 | C099,C101,C102 |
| 078 | T096 | S34–S39,S43–S45,S52 (route only); owner CT/DB/AL cited there | `internal/contracts/federation/,internal/uimap/bridge*` | DF14,DF19 | C100,C103,C106,C163,C164,C165,C166 |
| 079 | T097 | S34–S39,S43–S45 | `web/explorer/src/features/flow/` | DF14,DF19 | C099,C103 |
| 080 | T098 | S34–S39,S43–S45 | `internal/flowview/,internal/contracts/events/` | DF14,DF19 | C101,C102,C104 |
| 081 | T099 | S34–S39,S43–S45,S52 (route only); owner CT/DB/AL cited there | `web/explorer/src/features/graph/,web/explorer/src/features/flow/,tools/bench/` | DF14,DF19 | C081,C105,C160,C170,C179 |
| 082 | T100 | S34–S39,S43–S45,S50,S51,S52 (route only); owner CT/DB/AL cited there | `tests/acceptance/explorer*,artifacts/release/` | DF14,DF19 | C099..C106,C147,C148,C180 |
| 083 | T057 | S24,S28–S30,S43–S45 | `packages/python-provider/,internal/analysis/providers/` | DF03–DF07 | C057,C058 |
| 084 | T058 | S24,S28–S30,S43–S45 | `packages/python-provider/,contracts/` | DF03–DF07 | C057,C058 |
| 085 | T062 | S24,S28–S30,S43–S45 | `internal/domain/state/,internal/history/records*` | DF14,DF15,DF19 | C062 |
| 086 | T063 | S24,S28–S30,S43–S45,S52 (route only); owner CT/DB/AL cited there | `internal/contracts/federation/` | DF14,DF15,DF19 | C063,C163,C164,C165,C166 |
| 087 | T064 | S24,S28–S30,S43–S45 | `internal/history/diff*` | DF14,DF15,DF19 | C064 |
| 088 | T065 | S24,S28–S30,S43–S45 | `internal/domain/profiles/` | DF14,DF15,DF19 | C065,C066 |
| 089 | T066 | S24,S28–S30,S43–S45 | `internal/domain/ai/` | DF14,DF15,DF19 | C067 |
| 090 | T067 | S24,S28–S30,S43–S45 | `internal/domain/architecture*` | DF14,DF15,DF19 | C068 |
| 091 | T068 | S24,S28–S30,S43–S45 | `tests/acceptance/polyglot*,tools/bench/` | DF14,DF15,DF19 | C055..C069 |
| 092 | T048 | S24,S28–S30,S43–S45 | `artifacts/release/` | DF03–DF07 | C055..C069 |
| 093 | T069 | S27,S31,S43–S45 | `internal/evidence/import*,internal/store/sqlite/records*` | DF16,DF19 | C070 |
| 094 | T070 | S27,S31,S43–S45 | `internal/evidence/traces*` | DF16,DF19 | C071 |
| 095 | T101 | S34–S39,S43–S45 | `internal/flowview/observed*,web/explorer/src/features/flow/` | DF11,DF16 | C107,C108,C109 |
| 096 | T102 | S34–S39,S43–S45 | `packages/preview-sdk/observe*,internal/explorer/sessions*,web/explorer/src/features/preview/` | DF11,DF16 | C107,C108,C110 |
| 097 | T103 | S34–S39,S43–S45 | `tests/acceptance/explorer*,testdata/malicious/` | DF11,DF16 | C108..C112 |
| 098 | T104 | S34–S39,S43–S45 | `artifacts/release/` | DF11,DF16 | C107..C112 |
| 099 | T071 | S27,S31,S43–S45 | `internal/evidence/tests*` | DF16,DF19 | C072 |
| 100 | T072 | S27,S31,S43–S45 | `internal/evidence/selection*` | DF16,DF19 | C073 |
| 101 | T073 | S27,S31,S43–S45 | `internal/evidence/editor*` | DF03–DF07 | C074 |
| 102 | T074 | S27,S31,S43–S45 | `internal/search/semantic*` | DF03–DF07 | C075 |
| 103 | T075 | S27,S31,S43–S45 | `internal/store/sqlite/factsets*,internal/indexer/` | DF03–DF07 | C076 |
| 104 | T076 | S27,S31,S43–S45 | `internal/evidence/deployment*` | DF16,DF19 | C077 |
| 105 | T077 | S27,S31,S43–S45 | `tools/bench/,tests/acceptance/scale*` | DF03–DF07 | C078 |
| 106 | T078 | S27,S31,S43–S45 | `internal/history/report*` | DF16,DF19 | C077,C080 |
| 107 | T079 | S27,S31,S43–S45 | `internal/analysis/providers/,packages/new-provider/ (approved capability only)` | DF03–DF07 | C079 |
| 108 | T080 | S27,S31,S43–S45 | `artifacts/release/` | DF03–DF07 | C070..C080 |

Không cần mở toàn table mỗi phiên: tìm hàng task bằng ID rồi exact section locator. Thay owned paths vượt phiếu cần xem impact và cập nhật card theo scope-authority, không implicit refactor. Phiếu không cho phép overwrite user changes. Task conformance/testing được sửa implementation lỗi trong phạm vi qua reopen task hoặc linked fix record, không tự chỉnh blueprint để hợp thức bug.

## S48 — Kiểm chứng bản vẽ, oracle và bàn giao

### QA-01 — Validator tài liệu / contract

T003/T108/T040 phải kiểm: ID unique/resolved, counts derived, no task dependency cycles, schedule permutation và dependency-before-consumer, gate closure, required sections trong work cards, requirement→task→case, no DONE without evidence, no unknown enum/version, JSON schema local refs only, DDL migration order, fenced blocks balanced, source-policy ledger/version/mode/activation và new traceability C137–C148. Khối machine-readable authority trích theo marker và hash; code artifacts phải match source blocks.

C181–C192 là acceptance của workflow commit/checkpoint S53; synthetic Git proof không đánh dấu task implementation DONE và không thay actual project restore. T003/T107/T034/T040 chứng minh các subcase theo owner GIT-08.

Structured validation không chứng minh thuật toán đúng hay ứng dụng production an toàn. Document-level SQLite tests không tương đương modernc Go driver/Windows; JSON schema meta-validation không tương đương MCP/React conformance; reducer reference tests không tương đương browser. Khi báo kết quả phải nêu đúng lớp kiểm thử.

### QA-02 — Mandatory positive/negative/failure matrix

C113–C136 là test bổ sung cho blueprint v1.3.0, được chạy theo task/subcase của core/UI/R2, không ép bootstrap chạy browser chưa tồn tại. T107 checks contract-level reducers bằng fake components; T094/T100 mới end-to-end browser. Case có nhiều phạm vi ghi partial coverage evidence, không false universal PASS.

Fixture minimal end-to-end phải có hai repos/worktrees trùng name, Go shadowed variables/interface/callback, TS shared Button100 instances, literal API route và SQL transaction rollback, alternate build/source mismatch, untracked new caller, stale delayed mode ack. All generated fixtures synthetic label, no credential/prod endpoint. Expected mapping tạo từ source position/cases do người viết fixture xác nhận độc lập, không graph output.

Database tests bắt buộc: inactive/BUILDING invisible; cross-generation FK reject; publish old/new all-or-nothing; record CAS+idempotency; FTS scope; GC active reject+records preserve; aborted transaction state unchanged; index version too new reject. Byte/source tests: Unicode/Lao/emoji/CRLF line ranges; source hash bound; duplicate JSON and case/path collision; full manifest membership not mtime-only.

### QA-03 — Lớp evidence và release status

`document_checks` báo PASS/FAIL riêng `product_build`, `product_tests`, `browser_e2e`, `real_repo_acceptance`, `token_benchmark`. Không đưa audit doc vào E001 product bằng chứng giả. Document audit có bảng DV ở cuối mục này; E001 chỉ khi actual implementation có revision tồn tại. Actual scripts/artifact kiểm tra được phép ở artifacts/; file này giữ ID/hash/results/reference, không chứa private chain-of-thought hoặc log raw nguồn.

Không có luật “toàn bộ mọi dự án không cần quay lại sửa”. Blueprint kiểm soát assumptions và order; nếu môi trường hay requirement đổi thì lập design change ảnh hưởng có traceability. Unknown real dependencies remain preflight gate, không hợp thức hóa auto invent.

### QA-04 — Verification Ladder và Production Claim Gate

Tiếp nhận POL-SRC-01 §11.1 và §19.5–19.6. V0–V4 là loại/độ sâu bằng chứng, KHÔNG là G0–G7/GU0–GU3 mới, không pipeline cứng và không thay điều kiện test đã chốt. Một task chọn mức theo tác động và phải đáp ứng mọi case bắt buộc đã assigned; không dùng ma trận này để bỏ regression.

| Mức | Bằng chứng cho MCE4D khi áp dụng | Không được suy ra |
|---|---|---|
| V0 nguồn/hiện trạng | Workspace, assignment/mode, spec+policy versions, source/diff/constraints hiện tại | Đã build/run phần mềm |
| V1 phần đổi | Unit/property/negative/regression + format/type/build/validator phù hợp artifact | Mọi boundary hoặc target platform đã đúng |
| V2 ranh giới | Schema/ports/provider/SQLite/MCP/SDK consumers; transaction, failure/cancel và source consistency | Browser/production E2E hay scope ngoài đã được kiểm |
| V3 artifact/target | Binary/package/Explorer assets, install/startup trên target có quyền, bidirectional E2E khi UI có thật | Chỉ cross-compile hoặc jsdom đã PASS thì OS/iframe thật cũng PASS |
| V4 rủi ro vận hành | Permissions/containment/concurrency/resources/restore/migration/load/retention thuộc scope | Mọi repo cần cloud, SLA, DB cluster hoặc chứng nhận |

Foundation chỉ gồm T001/T002/T003/T105–T108; kiểm artifact/contract của foundation ở V0–V2, không gán sản phẩm V3/V4 đã đạt. T033–T042/T081/T094/T100 thực hiện mức liên quan đúng component/platform; chưa có môi trường thì giữ NOT_RUN và giới hạn nhãn như S20, không mock thành production evidence.

Ánh xạ báo cáo: ĐẠT=PASS có evidence; CHƯA ĐẠT=FAIL trong scope đã kiểm (phân biệt lỗi product/môi trường); CHƯA XÁC MINH=NOT_RUN hoặc evidence thiếu/stale/cancelled; KHÔNG ÁP DỤNG cần lý do scope đã xác minh. Applicability là thuộc tính verification plan; không thêm giá trị giả vào enum result S21/S43. Task TODO/VERIFY không phải result test. Thiếu công cụ hoặc chưa xây feature không tự là không áp dụng.

Trước claim Production-Ready/Enterprise-Grade: nêu artifact+revision/diff+environment+intended use, tiêu chí bắt buộc có nguồn, evidence hiện hành, review thực đã có, gaps và authority/ngoại lệ. Required FAIL/NOT_RUN chặn claim tương ứng; ngoại lệ thật không biến kết quả thành PASS. Document policy, manual check, configured guard, verified guard là bốn lớp khác nhau; không dùng hash/green badge thay proof. Đủ evidence không cấp merge/push/deploy hoặc quyền chạy target app.

### QA-05 — Hiệu lực guard, mutation và tích hợp

C137–C148 bổ sung kiểm chính cơ chế áp dụng quy định và build tooling. T107 dùng fixtures nhỏ/pure fake volumes/clock/outputs cho contract; T033/T035 và OS/browser tasks phải xác minh hiệu lực thật trước các claim tương ứng. Test disk-full dùng meter/fault injection, không làm đầy ổ thật. Test cleanup chỉ disposable roots có quyền, không dùng source/dữ liệu người dùng.

Chọn mutation non-equivalent cho invariant quan trọng khi implementation đã tồn tại, ví dụ bỏ source-hash check, cho old MODE_ACK arm, hoặc nuốt exit code. Làm trong bản sao/fixture cô lập theo quyền, lưu expected failing check và kết quả, loại trừ mutation tương đương; không ép một mutation score toàn repo. Test không phát hiện lỗi quan trọng → bổ sung test, rerun, giữ evidence đúng. Hoàn tác đúng mutant tạo ra và kiểm diff cuối; không giao code thử phá.

Review bắt buộc nhưng chưa có reviewer hợp lệ → chưa xác minh, chặn đúng nghiệm thu; tự rà soát không thành independent review. Sau merge/conflict/đổi source/dependency/rules/config, đánh giá invalidation và chạy lại checks bị ảnh hưởng trên bản kết hợp, không chỉ trên nhánh riêng. Case cũ có thể tái sử dụng khi equivalence inputs được chứng minh và gate không đòi chạy mới; không đưa đây thành quyền bỏ các test bắt buộc.

### DV — Audit lịch sử của tài liệu v1.3.0, 2026-10-13

Giữ nguyên báo cáo từ bản 1.3.0; không coi 94 assertions hoặc state/SQL tests lịch sử là đã được chạy lại trong lần tích hợp 1.4.0. Audit mới ở DV-1.4 bên dưới, product evidence vẫn độc lập.

| Layer đã chạy | Kết quả | Phạm vi bằng chứng |
|---|---|---|
| IDs/sections/traceability/task order | PASS | 50 sections; 104 requirements; 108 tasks; 136 cases; no dangling IDs/duplicate task/DAG cycles; corrected dependency order; no fake DONE |
| Canonical JSON schema/registry | PASS | Draft2020-12 meta-validation; 117 definitions; 21 operation request/data mappings; representative positive/negative fixtures; all local refs resolve |
| Embedded physical SQL | PASS | 7 scripts executed on disposable Python sqlite3 SQLite 3.46.1; integrity/FK checks; positive publication/negative FK/immutable/rollback/FTS/recordCAS/build binding/GC cases |
| Reference state/serialization models | PASS | Stale selection and delayed mode ACK rejection; shield-first disarm; MCP dual-body byte-count fixed point; NOT browser/MCP SDK E2E |
| Tổng document-level assertions | 94 PASS, 0 FAIL | Includes individual registry-reference checks; không đồng nghĩa 94 product acceptance cases |
| Product Go build/test; modernc driver matrix | NOT_RUN | Chưa có implementation repository/bundle đã được chạy; bắt buộc T002/T106 và các gate sản phẩm |
| Real browser Inspect/Interact/HMR/production strip | NOT_RUN | Cần actual Explorer/SDK và controlled fixture/browser tại T084/T094/T100 |
| Real repository/token/performance benchmark | NOT_RUN | Chưa dùng codebase thực của người dùng; không claim token savings hoặc scale supported |

Canonical schema semantic hash (sorted JSON): `f2506c38a2f39a19e825ee70482055f3e24b1a877cf6ee03802c601bd02b8d52`. DDL bundle hash (ordered marker+SQL text): `3efbd545e88ac80ca5e18ce43867903a2fb9f7f957529484795e7c237cf20364`. Document-audit report hash: `6148348ad6e0f7c03b04c7ef2503f269d3764221b208ed25779ed2eabb8f035e`. Các hash này nhận diện nội dung đã kiểm tra, không chứng nhận dependency, OS sandbox, code correctness hoặc quyền pháp lý. Evidence S21 vẫn NONE; không mark T105/T106/T107 DONE từ audit này vì deliverables code/pinned driver của sản phẩm chưa có.


### DV-1.4 — Kiểm tra bản cập nhật policy 2026-10-13

| Kiểm tra lần này | Kết quả | Giới hạn |
|---|---|---|
| Sections/requirements/cases/traceability, 108 task/order/dependencies/status và work cards | PASS | Chỉ cấu trúc tài liệu, không product test |
| Ba file nguồn so hash trước/sau; ledger path/version/hash | PASS | Attachment được kiểm; bản đã copy vào docs của repo vẫn NOT_VERIFIED |
| Canonical schema/operation registry/7 DDL blocks và thứ tự task so với 1.3.0 | PASS | Phải không đổi; không chạy lại toàn SQL/reducer suite lịch sử |
| JSON Schema/local refs/JSON/YAML fences và cây Markdown | PASS | Parse/meta-validation và preservation, không browser/Go SDK conformance |
| Product code, build/test, runtime storage guard, real repo/browser/benchmark | NOT_RUN | Không có implementation/repository được thay đổi trong lần này |

Kết quả lần này: 57 kiểm tra cấu trúc/parity/nguồn và parse/meta-validation PASS, 0 FAIL trong môi trường soạn. Không phải 57 product acceptance cases; không chạy lại đầy đủ 94 kiểm tra lịch sử của 1.3.0. Canonical code blocks, dependency order và trạng thái task được so sánh trực tiếp với file đầu vào 1.3.0. Ba file nguồn giữ nguyên raw bytes. Không đưa kết quả document audit vào E001 hoặc DONE; bản trong docs của repository, runtime guard, Go/browser tests và benchmark vẫn chưa xác minh.

### DV — Audit bản cập nhật v1.5.0, 2026-10-14

| Lớp đã kiểm | Kết quả | Phạm vi và giới hạn |
|---|---|---|
| Section/requirement/task/case/schedule | PASS | 53 sections,124 requirements,108 tasks,180 cases; DAG/order valid, no task DONE |
| Canonical schema/registry/defaults | PASS | 151 definitions;31 operation mappings (6 MCP,18 HTTP,7 CLI-only); local refs/schema positive-negative fixtures |
| Physical SQL và GC | PASS | 7 DDL trên SQLite 3.46.1; actual FK/tree/shared-blob/FTS/map GC và rollback tại17 vị trí lỗi |
| Resolver/permission/incarnations | PASS | SQL multiple resolver versions+natural unique key+immutable metadata; permission CAS/revoke và3 store identity fields |
| Paging/fragment/scope/state | PASS — reference models | 137 mapping matches;1025 rename edits đọc qua process riêng; Unicode fragment reassembly; pinned/unavailable scope; status/revoke/late ACK/restore models; KHÔNG app E2E |
| Codec liên ngôn ngữ | PASS | 14 positive vectors bytes+SHA256 khớp Python/Go/Node; raw source hashes riêng codec; negative primitive checks ở Python reference |
| Lifecycle OS primitive | PASS — Linux reference only | Hai process thử shared/exclusive flock và worktree B độc lập; KHÔNG Go pool adapter/Windows/full restore proof |
| Ba file policy nguồn | BYTE_UNCHANGED | Raw hashes khớp POL-02; chưa xác minh đã được copy vào repository của người dùng |
| Tổng kiểm tra | 106 PASS,0 FAIL | Có31 registry-reference checks và17 GC-fault cases; không phải106 tính năng sản phẩm |
| Product Go/modernc/MCP/browser/Windows | NOT_RUN | Chưa có implementation; code codec Go là proof độc lập, không build sản phẩm |
| Real-repo/scale/token | NOT_RUN | Chưa đọc/chạy repository thật; không công bố số tiết kiệm hoặc100% correctness |

Current canonical schema semantic SHA-256(sorted JSON): `0c738b6b47c99687ca2469076b6883e57bb4eaf998d9b90b38aa8a604ed6882b`. Current DDL bundle SHA-256(ordered marker+SQL newline): `666d2a6e38474715f5621aaeb7fcbc2c3ffb3703912da47dc88da3bab2419f25`. Codec proof toolchain: `go version go1.23.2 linux/amd64`, Node `v22.16.0`, Python `3.13.5`; không thay ledger UNRESOLVED của product. Bộ tái hiện được bàn giao riêng dưới tên `MCE4D_1.5.0_VERIFICATION.zip` (script+vectors+results+manifest), không phải spec hoặc tracker thứ hai. Main file SHA-256 và hashes của proof files ở manifest ngoài để tránh self-hash vòng lặp. Không đưa document proofs vào E001 product.

Các PASS lịch sử v1.3.0/v1.4.0 ở trên giữ đúng lớp đã chạy; không có nghĩa đã phủ các phản ví dụ phát hiện sau đó. V1.5.0 thay canonical schema/DDL baseline trước release; historical hashes ở DV cũ không là hashes hiện hành. Product task/evidence vẫn NONE, mọi product build/modernc/browser/real-repo/OS-matrix test NOT_RUN cho đến task actual. Bộ proof tạm không phải source implementation repository hoặc reviewer độc lập.

### DV — Cập nhật policy commit v1.6.0, 2026-11-11

| Lớp đã kiểm | Kết quả | Phạm vi / giới hạn |
|---|---|---|
| Cấu trúc, ID, truy vết, task order | PASS | 54 sections;130 requirements;108 tasks;192 cases. Dependencies/order/status task không đổi;72 TODO+36 DEFERRED; product DONE=0 |
| Contract/runtime parity | BYTE_UNCHANGED / schema meta-validation PASS | Toàn bộ marked machine-readable blocks và cả S43/S44/S45 không đổi so với1.5.0; không rerun mọi SQL/browser proof lịch sử |
| Ba policy nguồn | BYTE_UNCHANGED | SHA-256 raw khớp ledger nguồn; chưa xác minh cài đặt ở repo thực |
| Mô hình batching | PASS — reference only | 9 chưa commit;10 đủ;11 phải dừng; non-DONE/reopen/duplicate không đếm; S47 khác thứ tự số ID; tail thật khác tạm nghỉ |
| Repository Git synthetic cô lập | PASS | 20 task mẫu tạo2 commit; kiểm staged tree+file mới/docs/contract/lockfile/deletion; excluded .env/cache; annotated tags và checkout từng mốc; workspace dirty gốc giữ nguyên |
| Remote/backup fixture local | PASS | Local bare remote explicit refs; push lặp không commit thừa; full bundle verify+clone và tests stdlib từ checkout sạch. Chỉ cùng môi trường local, không proof remote/cloud hoặc mất ổ thật |
| Tổng | 75 PASS,0 FAIL | Bao gồm cấu trúc/traceability, mô hình reference và Git fixture; không phải 75 acceptance cases sản phẩm |
| Actual project commit/push/restore; product build/test; enforcement | NOT_RUN / NOT_IMPLEMENTED | Không có workspace implementation của người dùng được thao tác; chưa có validator/guard sản phẩm chạy theo policy mới |

Môi trường proof: `git version 2.47.3`, Python `3.13.5`, Linux. Chỉ repository disposable tự tạo; không Git init/commit/push/tag trong repository người dùng, không sửa history/remote/global Git config của họ. Main spec là artifact được sửa. Giữ scope của audit: full interim backup, hooks/signing, Git LFS/submodules, Windows và recovery dữ liệu thật chưa được fixture này xác minh; S53 yêu cầu kiểm đúng môi trường khi sử dụng. Git tests dùng dữ liệu tổng hợp, không bí mật thật. Những PASS lịch sử không được chuyển thành evidence product hoặc task DONE.

### DV — Tính tái lập bằng chứng lịch sử và revision 1.6.1

Tại checkout được khảo sát ở revision Git `ba72ae4` ngày 2026-11-11, repository chỉ có spec và ba file policy trong `docs/`; không thấy `MCE4D_1.5.0_VERIFICATION.zip` trong checkout. Đây là quan sát phạm vi checkout tại thời điểm đó, không kết luận artifact không tồn tại ở nơi khác. Tên và hash trong báo cáo lịch sử không tự chứng minh artifact hiện truy cập được hoặc kết quả áp dụng cho revision 1.6.1.

T003/T106/T107/T108 khi cần proof: tìm artifact chỉ trong nguồn được phép; nếu có, kiểm nguồn gốc, manifest/hash, version, nội dung và tác động script trước khi chạy, rồi ghi command, môi trường, input digest, kết quả và giới hạn vào S21. Không chạy script chưa kiểm hoặc lấy thêm source sản phẩm tham khảo. Nếu artifact thiếu, khác hash hoặc không tái chạy được, giữ historical result như báo cáo lịch sử và current reproduction NOT_RUN/FAIL đúng thực tế; tự tạo lại kiểm tra độc lập trong phạm vi task và authority tương ứng khi khả thi. Không chặn T001 chỉ vì chưa có ZIP, không tự tạo locator giả, không coi historical proof là product evidence hoặc thay driver/OS/browser tests bắt buộc.

Revision 1.6.1 chỉ bổ sung quy trình discovery, nghiệm thu và planning; các kết quả 1.6.0 ở trên vẫn là lịch sử, không mặc định chạy lại. Kiểm chọn lọc trên working tree 2026-11-11 bằng Node và `git diff --check`: PASS so sánh bytes S43–S45, tracker S19, danh mục requirements/cases với `ba72ae4`; ID 54 sections/130 requirements/108 tasks/192 cases; lịch chứa mỗi task một lần và dependencies trước consumer; 72 TODO/36 DEFERRED; fences cân bằng; hai block JSON contract/registry parse; version/resume/gaps và hash spec trước khớp. Đây không phải chạy lại 75 kiểm tra lịch sử, JSON Schema semantic validation, validator T003 hay kiểm product. Trạng thái `document_structural_validation` ở đầu file phản ánh phạm vi kiểm chọn lọc này; product build/tests/benchmark vẫn NOT_RUN cho đến khi có implementation và evidence.

## S49 — Chỉ thị khởi công và tiếp tục dành cho AI

Đọc S00, S22, S41 và POL-01/POL-03/POL-05 ở S50; xác minh source docs/mode của nhiệm vụ. Chỉ khi có assignment implement/integrate phù hợp mới lấy task đầu tiên chưa DONE theo S47 trong track được mở; docs-only dừng ở bàn giao bản vẽ, không tự bắt đầu code. Đọc phiếu của task, contracts/flow/storage/code rules mà phiếu chỉ ra, khảo sát source/actual git status. Không đọc toàn bộ source hay toàn spec mỗi phiên nếu locator đã rõ, nhưng không lấy trí nhớ phiên trước làm bằng chứng source hiện tại.

T001 chỉ khảo sát/bootstrap; T002 pin/smoke; T003 validator; T105→T106→T107→T108 đóng blueprint/proof trước feature code. Chưa qua G0 không viết catalog/index/API/UI implementation, ngoại trừ foundation contract/proof/stdlib tooling trong các task trên. Sau đó tuân thủ đúng thứ tự S47. Mỗi task hoàn thành cả test/error/cancel/failure scope, command/storage checks S51 khi áp dụng và cập nhật tracker trước task kế. Không cần tạo lại hồ sơ bootstrap vì ba file docs có mặt.

Không tự quyết schema, đổi stack, tạo API/provider không có contract hoặc dự đoán business rule. Facts unknown phải diagnostics/boundary; source không được phép đọc phải ACCESS_DENIED; thiếu capability trả UNSUPPORTED_CAPABILITY; không dùng fake success. Đụng dữ kiện môi trường unresolved thì kiểm bằng official docs/local tools theo gate; đụng thay đổi architecture/permission thì ghi discrepancy và chặn scope đến khi bản vẽ được sửa có authority.

Sửa code chỉ trong repository MCE4D đã được người dùng cho phép hoặc fixture của chính tests. Core chạy trên repository khác mặc định read-only. Preview build/install/runtime permission không suy từ quyền lập bản đồ. Local commit MCE4D chỉ theo batch10 hoặc ngoại lệ hữu hạn S53 trong assignment implement/integrate; không commit riêng từng task/file. Push cần đúng remote/branch được phép; không deploy/delete/cache cleanup hoặc thao tác repo ngoài quyền.

Trước kết thúc phiên: test thực có revision → evidence PASS/FAIL/NOT_RUN đúng → patch một hàng task S19, related gate và resume S22 → validate IDs/status/contracts → đối chiếu S53: đủ10 thì đóng commit+local restore/tag, chưa đủ thì lưu tiến độ/snapshot có quyền chứ không commit lẻ → ghi task tiếp theo thực sự Ready, không lời hứa chạy nền. Nếu lỗi kiểm thử, giữ VERIFY/BLOCKED hoặc reopen task; không đổi test/gate để xanh. Bản vẽ là authority về thiết kế, build/test/evidence là authority về việc đã làm xong.

## S50 — Tiếp nhận quy tắc trong docs, một bản vẽ chính

### POL-01 — Vai trò và phạm vi đã được chấp thuận

Người dùng chấp thuận tiếp nhận các quy định cần thiết và giữ ba file nguồn nguyên bản trong `docs/`. File `Maps-Context-Engine-4D_SPEC.md` vẫn là bản vẽ chính duy nhất: yêu cầu, kiến trúc, schema/DDL, luồng, quyết định riêng, thứ tự, tracker và evidence dự án ở đây. Ba file nguồn là quy tắc tham chiếu theo vai trò, không là ba bản vẽ/tracker khác. S53 ghi yêu cầu mới về commit nhóm10 và quyền có điều kiện cho chính MCE4D; không sửa quy tắc Universal, không suy quyền push/deploy từ quyền commit. Không chép toàn văn nguồn vào spec, không tạo `PROJECT_CONTEXT.md`, `AI_RULES_PROJECT.md` hoặc một ROADMAP cạnh tranh khi thông tin đã có trong file này. S19 là nơi duy nhất giữ trạng thái task; S21 giữ quyết định/evidence, S22 là summary suy ra.

Đây là tiếp nhận có chọn lọc và cụ thể hóa cho MCE4D, KHÔNG là bản sửa/nâng cấp bộ Universal 3.1. Nội dung Universal đã tiếp nhận áp dụng theo điều kiện của nó; các quyết định cụ thể đã chốt của MCE4D không bị mở lại vì Universal không ấn định stack chung. S50/S51 không hạ quyền hệ thống/người dùng/nguồn hướng dẫn thực sự có hiệu lực. Xung đột chưa được giải thích phải theo BP-04, không dùng quy tắc “file mới nhất thắng” hoặc “câu nghiêm hơn luôn thắng”.

Vị trí mặc định (đường dẫn dưới đây tương đối với project root, không phải đường dẫn của phiên tạo tài liệu):

```text
maps-context-engine-4d/
  Maps-Context-Engine-4D_SPEC.md       # bản vẽ + điều phối + tracker duy nhất
  docs/
    AI_RULES(8).md                    # giữ nội dung gốc, Universal 3.1
    AI_RULES_STORAGE_SUPPLEMENT(1).md  # giữ nội dung gốc, supplement 1.0.0
    PROJECT_BOOTSTRAP_PROMPT(4).txt    # giữ nội dung gốc, nhiệm vụ docs-only 3.1
```

Hậu tố upload không phải version. Không bắt người dùng đổi tên; tham chiếu chuẩn `AI_RULES.md`/`PROJECT_BOOTSTRAP_PROMPT.txt` bên trong Universal được resolve tới nguồn tương ứng trong ledger sau xác minh, KHÔNG tạo bản sao chỉ để khớp tên. Có thể nhận bản đã được người dùng đổi tên tại `docs/AI_RULES.md`, `docs/AI_RULES_STORAGE_SUPPLEMENT.md`, `docs/PROJECT_BOOTSTRAP_PROMPT.txt` khi đúng nội dung/version. Nhiều candidate khác nội dung → conflict; không chọn theo mtime/hậu tố. Nhiều bản byte-identical → chọn đường dẫn ưu tiên đã ghi, không sửa/xóa các bản còn lại. Không tự đi tìm chính sách thay thế ở repo khác.

Nếu dự án đã đặt chính spec này trong `docs/`, giữ duy nhất bản đang có thẩm quyền và ghi đường dẫn thật ở S21; không tạo thêm bản ở root. Cấu trúc mặc định không phải lệnh di chuyển hoặc xóa file. Bản cập nhật này không tự copy, cài đặt hoặc đồng bộ các file lên repository người dùng.

### POL-02 — Ledger nguồn đã đọc và điều kiện xác minh ở dự án

| Policy ID | Đường dẫn dự kiến | Version nội dung | SHA-256 bytes gốc được cung cấp | Vai trò/kích hoạt |
|---|---|---|---|---|
| POL-SRC-01 | `docs/AI_RULES(8).md` | 3.1 | `1662406d9ac9e63646130516ad6811934b2bd7f0a6b7d240403749200777f9e3` | Quy tắc hành vi chung; đọc các phần bắt buộc và các mục theo task, không thay bản vẽ cụ thể |
| POL-SRC-02 | `docs/AI_RULES_STORAGE_SUPPLEMENT(1).md` | 1.0.0 | `eea81c9ac63344efde5f1eccb30e43d06146875aabaf8396fec3b46e5e878dac` | Trước build/test/benchmark/generator hoặc check có khả năng sinh output đáng kể; S51 cụ thể hóa |
| POL-SRC-03 | `docs/PROJECT_BOOTSTRAP_PROMPT(4).txt` | 3.1 | `b1d692184f9b4bac87f6b55b25e1c49bab90af5d5eb1706a428a09d85212b686` | Chỉ thực hiện khi nhiệm vụ khảo sát/thiết lập hồ sơ docs-only được giao; sự tồn tại của file không tự kích hoạt |

Tại revision 1.4.0: nội dung và hash ba file đã kiểm tra trong các attachment được cung cấp; người dùng sẽ copy vào `docs/`. `project_installation=NOT_VERIFIED`, `runtime_enforcement=NOT_IMPLEMENTED`, product evidence vẫn NONE. T001 xác minh đường dẫn thực và ghi bằng chứng vào S21, không tự ghi rằng nguồn trong repo đã khớp khi mới thấy attachment. Chấp thuận chính sách ≠ đã đọc đúng file ở checkout ≠ có guard thực thi ≠ guard đã qua test.

T001/T003 kiểm file trong scope được phép, version, raw hash và nội dung. Không gọi web hoặc nâng bản “latest” để thay nguồn đã tiếp nhận. Khi raw hash khác: so sánh đầy đủ nội dung; chỉ khác CRLF↔LF thì có thể ghi `CONTENT_EQUIVALENT_EOL_ONLY`, kèm actual raw hash và phép đối chiếu, không tuyên bố byte-identical/không sửa nguồn. BOM, whitespace, từ ngữ, version hoặc quy tắc khác không được tự coi tương đương; ghi discrepancy theo BP-04. Hash chỉ nhận diện nội dung, không chứng minh chất lượng hoặc quyền thực thi.

Thiếu/mâu thuẫn nguồn → ghi thiếu đúng nguồn/phạm vi, không tái tạo file “giống như nhớ”. Cho phép khảo sát/đọc/sửa tài liệu an toàn để resolve; không nhận đã tiếp nhận đầy đủ và không bắt đầu phần implement/heavy command phụ thuộc khi chưa resolve. Không chặn mọi repository khác hoặc dùng thiếu file làm lý do thay kiến trúc. Issue của policy checker là kết quả tooling, không tự thêm enum/error mới vào MCP schema S43.

### POL-03 — Mode được xác định từ nhiệm vụ, không từ tên file

| task_mode | Cho phép theo quyền thực tế | Không tự cho phép |
|---|---|---|
| docs_only | Khảo sát, cập nhật bản vẽ/hồ sơ hiện có theo yêu cầu; kiểm tra artifact được phép có đầu ra nhỏ và cô lập | Không tự implement sản phẩm, generator, wrapper, CI, test thực thi mới trong repo, install/khởi chạy target hoặc regenerate runtime artifacts |
| implement | Thi công task Ready trong S47; foundation T001/T002/T003/T105–T108 theo BP-02 khi được giao coding | Không đổi stack/contract/gate, mở optional scope, dọn dữ liệu cũ hoặc tác động repo/production khác |
| review | Đọc, đối chiếu và chạy check có quyền/phạm vi phù hợp | Không tự sửa source hoặc tự nhận reviewer độc lập; patch cần quyền riêng |
| integrate | Kiểm bản kết hợp và tích hợp đúng phạm vi được giao, giữ các thay đổi của bên khác | Không suy ra quyền commit/merge/push/release/deploy hoặc bỏ check nếu nhiệm vụ chưa cho phép |

Prompt bootstrap là nhiệm vụ, không phải instruction tự chạy khi đọc file. Task implementation đã được giao không bị biến thành docs-only chỉ vì POL-SRC-03 nằm trong `docs/`. Ngược lại, kết thúc bootstrap docs-only không tự chuyển sang T001 implementation. T001 có thể được khảo sát trong docs-only, nhưng không mark deliverable bootstrap-code DONE nếu chưa có quyền và evidence. Các kiểm tra tài liệu tạm trong phiên chuẩn bị bản vẽ không đồng nghĩa tạo code/test runtime trong repository.

Mỗi phiên ghi mode và assignment_ref tại ghi chú task/evidence S21, gắn với yêu cầu hiện tại; phiên mới phải xác định lại quyền, không kế thừa mode thành quyền vĩnh viễn. Thiếu assignment chỉ tiếp tục phần đọc an toàn; không hỏi lại quyết định tên/bố cục/stack đã rõ. Mode không làm mất quyền an toàn thực sự của môi trường hoặc tạo thêm quyền bằng lời nói.

### POL-04 — Bảng tiếp nhận có chọn lọc, không thêm authority cạnh tranh

| Nguồn | Cách tiếp nhận cho MCE4D | Nơi điều hành/kiểm |
|---|---|---|
| POL-SRC-01 §§1–3,5–8 | Giữ no-guessing, ownership, uncertainty, no blind retry; đã được cụ thể hóa bằng source/snapshot/permission contracts | S00,S06–S12,S41–S45; không chép lại |
| POL-SRC-01 §§9–10 | Complexity Gate và Change Budget áp dụng khi tăng phạm vi/độ phức tạp ngoài baseline; không mở lại thiết kế mọi task | S46 CODE-08; phiếu S47 |
| POL-SRC-01 §§11–14,19 | Verification Ladder, mutation có chọn lọc, review đúng vai trò và Production Claim Gate | S48 QA-04/QA-05; không thay G/GU bằng V0–V4 |
| POL-SRC-01 §§16–18,21–23 | Đọc theo tác vụ, generated-output parity, không sửa chồng, evidence còn hiệu lực và test bản kết hợp | S00,S21,S46,S47,S48 |
| POL-SRC-02 | Command impact, multi-volume budget, run ownership, bounded retention, cleanup đúng quyền và bảo toàn lỗi | S51; quota runtime engine vẫn S15/S44 |
| POL-SRC-03 §§1–7,GIỚI HẠN | Chỉ onboarding docs-only khi được giao; dùng chính spec thay hồ sơ mới; không khởi chạy lại toàn bootstrap mỗi phiên | POL-03; S21/S22; sau bàn giao chờ assignment phù hợp |

Tài liệu nguồn giữ nguyên, không chèn stack/path/tiến độ MCE4D. Mọi khác biệt cần quyết định lưu ở S21 theo BP-04; không dùng entrypoint/skill/prompt phát sinh để hạ blueprint. Chi tiết sản phẩm do S41–S45 quyết định; chính sách hành vi/quyền không được xem như đề xuất schema/luồng mới của sản phẩm.

### POL-05 — Đọc theo task và entrypoint

Mỗi phiên: S00 → S22 → POL-01/POL-03 và ledger hiện hành → hàng task S19/S47 → source/phần quy định áp dụng. Đối với POL-SRC-01, đọc HỢP ĐỒNG DÙNG CHUNG và KHỞI ĐỘNG; dùng LỘ TRÌNH ĐỌC để mở thêm đúng mục. POL-SRC-02 đọc trước workload liên quan; POL-SRC-03 đọc khi onboarding docs-only hoặc cần giải quyết ranh giới mode, không “thực hiện lại” ngầm. Phiên/compaction mất context hoặc nguồn đổi phải đọc lại phần thiếu; checksum không thay việc hiểu phần quy định áp dụng.

Không auto-load toàn spec và ba file vào mọi tool schema/AGENTS. Đặt cạnh nhau không chứng minh tự nạp. Khi task cho phép chỉnh entrypoint, kiểm cơ chế host thực tế, giữ chỉ dẫn có sẵn; entrypoint nhỏ trỏ bản spec thực cùng S50, không sao chép chính sách. Không tự cấu hình toàn máy, cài skill/plugin/hooks, tạo server đồng bộ hay thêm credential. Chưa kiểm host thì ghi khả năng nạp `NOT_VERIFIED`, hướng dẫn explicit read; không nhận policy đã được cưỡng chế.

## S51 — Hợp đồng lệnh và storage khi phát triển/kiểm thử

### RUN-01 — Phạm vi, lệnh nhỏ và vòng bootstrap

S51 áp dụng cho tác vụ phát triển MCE4D và các fixture đã được cho phép, đặc biệt command sinh build/test/benchmark/generator output đáng kể. Không thay runtime quota S15, storage authority S44 hoặc thêm tính năng dọn máy cho engine. Không cấp quyền chạy target app/test, tải dependency, chỉnh config hay xóa dữ liệu. Nguồn: POL-SRC-02 và POL-SRC-01 §§2–3,10–13,16–19,22–23. Những công thức/field/điểm tích hợp dưới là quyết định triển khai riêng của bản spec, không phải phần đã có sẵn trong file nguồn.

Trước mọi command: xác minh cwd/argv/tool version, tác động và quyền; tên `check`, `verify`, `dry-run`, `--write` không là chứng nhận. Đọc script/helper khi cần; không đoán flag, không chạy placeholder. Lệnh đọc hoặc check đã biết đầu ra nhỏ không cần wrapper/budget biểu mẫu lớn; ghi căn cứ phạm vi nhỏ. Unknown-growth hoặc long/variable/heavy run cần RUN-02–RUN-05; không gắn “small” chỉ để vượt preflight.

T002 áp dụng preflight thủ công/máy đo sẵn có và thử nghiệm nhỏ được giới hạn trước smoke build. Không bắt phải build wrapper hoàn chỉnh mới được build wrapper. T107 thêm pure budget/ownership/finalization contract fixtures nhỏ; T033 harden cơ chế theo tooling thực. Ưu tiên công cụ có sẵn + wrapper nhỏ chỉ khi thiếu guard có căn cứ; không bắt thêm dependency, daemon, global lock hoặc registry dịch vụ. Khi cần wrapper, phạm vi duy nhất mặc định `tools/run_check/` thuộc T033, gọi utilities hiện có; test contract ban đầu không giả wrapper đã chạy production. Chưa kiểm guard thì báo manual/documented/untested đúng lớp và không chạy workload nặng không đủ bảo vệ.

### RUN-02 — Command contract và bằng chứng, không tracker thứ hai

Mỗi nhóm lệnh quan trọng có record tại S21 hoặc artifact máy sinh do S21 trỏ tới. Tái dùng record khi đầu vào còn đúng, không viết dài cho mỗi invocation. S19 vẫn là tracker duy nhất; record lệnh không có trạng thái task thứ hai. `command_id` chỉ cấp khi command/cwd thật đã xác minh; template rỗng không được thực thi. Nội dung tối thiểu:

| Trường | Điều kiện |
|---|---|
| task_id, task_mode, assignment_ref, owner, baseline_ref | Phải truy về task/phạm vi/quyền đang có; baseline gồm diff khi chưa commit |
| command_id, executable/argv, cwd, tool_versions, command_source_ref | Đường dẫn/command thật; quote/argv đúng; không log secret trong argv hoặc environment |
| input_refs, contracts, dependency/config/rules fingerprints | Căn cứ tái sử dụng evidence; fingerprint theo phần đầu vào thực sự ảnh hưởng, không chỉ timestamp |
| expected_writes, temporary_roots, cache_roots, log_roots, download_roots | Mọi nơi có khả năng ghi kể cả redirect, children, browser artifacts và compiler caches |
| network_scope, child_processes, side_effect_class, permission_ref | Đọc/ghi-tạm/ghi-bền-vững/dịch-vụ-ngoài được phân biệt; không tự widening |
| storage_mode, volumes, budgets, retention, deadline, monitor_interval | `bounded_small` có căn cứ hoặc `budgeted`; giá trị cho workload nặng phải xác minh trước chạy |
| run_id, output_root, outputs_created, result, artifact_refs | Một run identity; theo dõi exact owned outputs; chưa có artifact thì null, không bịa pointer |
| cleanup_plan, cleanup_permission, command_exit_status, cleanup_status, final_status | Kết quả command khác cleanup; original lỗi không bị ghi đè; crash không suy đã cleanup |

Records là dữ liệu governance/build, KHÔNG thêm field vào wire schema S43 hay bảng domain S44. Thư mục mặc định `artifacts/runs/<run_id>/` chỉ cho output được công cụ cho phép redirect; căn cứ actual root/volume có thể khác. Không reset/xóa cache của người dùng để ép theo mẫu. Run record nhỏ có retention; dữ liệu bằng chứng duy nhất/deliverable không tự trở thành disposable.

### RUN-03 — Preflight dung lượng theo volume thật

1. Resolve các destination và volume/resource quota thực của chúng, gồm temp/cache/log/download; nhiều path cùng volume phải gộp. Đo available bytes mà principal/process có thể sử dụng, không chỉ free của drive chứa repo. Nếu filesystem/quota/inode limit được môi trường công bố thì kiểm tra khi liên quan; phần không quan sát được ghi giới hạn, không giả vô hạn.
2. Lấy peak additional demand, retained bytes, safety reserve, warning/block thresholds, deadline và monitor interval từ lần chạy liên quan hoặc bounded sample có quyền. Số bytes nguyên không âm, duration/interval hợp lệ; warning >= block. Không gán một số GB cho mọi repo, không lấy quota cache engine S15 làm build budget.
3. Admission xét một snapshot dung lượng hiện tại theo từng volume v: `required_v = max(block_threshold_v, new_job_peak_remaining_v + other_jobs_peak_remaining_v + safety_reserve_v)`. Chỉ cho workload nặng bắt đầu khi `available_v >= required_v` trên MỌI destination, quyền và các điều kiện khác đều đạt. `available_v < warning_threshold_v` phải cảnh báo. Unknown/invalid measurement/budget không được chuyển thành 0 hoặc PASS.
4. `available_v` đã phản ánh bytes thực đã chiếm. Reservation của job khác là phần nhu cầu tương lai còn lại, không cộng lại toàn ngân sách ban đầu và đếm đôi bytes đã ghi. Peak không phải retained; peak gồm intermediate files/children/download/extraction khi có. Không suy remaining bằng file size khi tool's growth model chưa được xác minh.
5. Với các job cùng điều phối, kiểm tra và giữ chỗ bằng cơ chế đã có, được kiểm chứng cho các writer đó; critical section ngắn chỉ cho admission bookkeeping. Không giữ mutex của mọi repo suốt build. Output riêng + đủ tài nguyên → được song song. Shared output không thể cô lập → serialize/lock đúng tài nguyên, không tên dự án toàn máy.
6. Không thấy writer/reservation ngoài process thì ghi `coordination_scope` và giới hạn; không báo aggregate toàn máy đã kiểm soát. Nếu không thể budget an toàn workload nặng thì chặn workload đó. Không tự tạo daemon/khóa toàn máy để có cảm giác chắc chắn. Những job không liên quan vẫn theo điều kiện riêng của chúng.

Chỉ số filesystem và reservation đều có thời điểm quan sát; preflight không bảo đảm disk không thể đầy. Resource exhaustion phải có failure path. Nếu source/tool không hỗ trợ redirect/limits phù hợp, giảm/phân chia workload trong phạm vi yêu cầu hoặc ghi BLOCKED; không bỏ test bắt buộc, không nới quyền cleanup để lấy chỗ.

### RUN-04 — Theo dõi, kết thúc và phục hồi

Run record được tạo trước tác vụ ghi đáng kể tại nơi owned đã được phép; một owner và phạm vi output rõ. Long/variable run kiểm tra dung lượng/growth định kỳ hữu hạn đã xác định; output/log bị cap bằng cơ chế thực được test, không chỉ ghi limit vào JSON. Chạm blocking/resource/output/deadline limit → ngăn job mới trong scope đó, cancel owned process tree, chờ writers/handles dừng, rồi finalize. Không kill process khác chỉ vì cùng tên hoặc dùng cùng volume.

Mọi đường success/failure/cancel đi qua finalization sau writers dừng. Giữ riêng `command_exit_status`, `termination_reason`, `cleanup_status`, `final_status`. Command fail → giữ lỗi/exit gốc và failing test/seed; cleanup fail không đổi thành command success. Command success nhưng cleanup/finalization bắt buộc fail → báo tổng thể chưa thành công, vẫn giữ exit gốc 0 làm dữ kiện. Không bịa exit status khi bị kill/mất kết nối; ghi unknown và căn cứ quan sát. Không retry action có side effect mù quáng chỉ vì mất kết quả.

Uncatchable crash → để record cho lần kiểm tra report-only; không tự nhận finally đã chạy hoặc tự chiếm output theo TTL/PID đơn lẻ. Xác minh owner/process identity/current state theo cơ chế hiện có; chưa rõ thì không xóa. Không tự dọn file tích lũy trước lần chạy. Khi volume thiếu, báo nhu cầu/đường dẫn đã che thông tin nhạy cảm và lựa chọn trong quyền; không tự dọn cache chung.

### RUN-05 — Cleanup, retention và quyền xóa

Retention byte/count/age hữu hạn áp dụng cho disposable artifacts thuộc dự án; output độc nhất phục vụ nghiệm thu, test source/fixtures/baseline/golden, Git metadata, source tracked, catalog, records.sqlite, durable backups và deliverable người dùng yêu cầu không là rác. `gitignored`, tên `tmp/cache` hoặc marker alone không chứng minh disposable. Artifact unique cần giữ phải budget từ đầu hoặc bàn giao đúng quyền; không giữ unbounded dump/secrets.

Trước xóa: report-only liệt kê exact candidate, provenance/owner, size, lý do, exclusions, writer status và dữ liệu sẽ giữ. Dry-run KHÔNG chạy workload, tạo run directory/record mới, ghi log/state vào repo, install, repair hay delete; chỉ đọc và xuất báo cáo tới output channel được phép. Retention policy không biến dry-run thành cleanup.

Chỉ cleanup khi quyền hiện hành cho phép đúng candidate, writers đã dừng và containment được chứng minh. Resolve mọi ancestor liên quan và revalidate lúc mở/xóa; chặn traversal/symlink/junction/reparse/mount escape, path replacement race, unexpected content, thiếu ownership. Không đi qua link để xóa target. Nền tảng không enforce được containment → refuse automatic removal, báo giới hạn. Kiểm tra chuỗi prefix hoặc record cũ không đủ.

Dọn dữ liệu tích lũy cũ cần consent riêng, cụ thể. Xóa không thể phục hồi qua cơ chế khôi phục thông thường cần phê duyệt tại thời điểm hành động theo POL-SRC-02; standing policy/marker/việc người dùng duyệt bản vẽ không thay phê duyệt bắt buộc. Thiếu approval → report candidates và để nguyên. Recoverable removal vẫn cần quyền tương ứng; di chuyển cùng volume không giải phóng dung lượng. Không thêm cleanup tự động vào MCP, startup hooks hoặc một global cache policy.

S15/DF17 là thiết kế GC runtime cho owned index; S51 không cấp quyền để AI chạy GC/delete trong một phiên không được phép, không xóa records/catalog/source. Khi user trực tiếp dùng sản phẩm hoặc task có quyền cleanup, vẫn áp dụng đúng contract và instruction hierarchy, không thay semantics xóa để lách approval.

### RUN-06 — Kiểm chứng và điểm tích hợp không đổi thứ tự thi công

| Task hiện có | Việc tiếp nhận thêm, cùng thứ tự S47 | Bằng chứng cần có |
|---|---|---|
| T001 | Resolve main spec + docs ledger; mode và nguồn authority; không sửa ba file nguồn | C137,C138, hash/path/version observations |
| T002 | Command inventory/preflight/bounded sample trước install/build đã có quyền; giữ budget từ nguồn thật | C140–C142 theo check thủ công/fixture nhỏ, không fake enforced guard |
| T003 | spec_check kiểm ledger/mode metadata, traceability, no false DONE/claim | C137,C138,C148 bằng fixture synthetic |
| T105,T108 | Change Budget/Complexity Gate gắn contract baseline; design seal không là quyền chạy production | C139,C148; approval/authority và scope |
| T107 | Pure contract tests cho budget/destination grouping/finalization/dry-run và evidence applicability | C140–C146 thuộc proof scope; fake volumes/paths, không làm đầy/xóa ổ thật |
| T005,T033,T035 | Secure ownership/containment, teardown/output cap, retention; reuse mechanisms; wrapper nhỏ chỉ khi cần | C143–C146 integration trên disposable fixture; old source/records protected |
| T034,T040 | Consolidate policy checks + targeted mutation/evidence reuse/merge validation; no auto bless | C137–C148 theo applicability; full core behavior evidence |
| T081,T094,T100 | UI/browser artifact budgets, SDK build/prod-strip outputs, integrated verification | Same command contract; C147,C148 và applicable storage cases trên UI tooling |
| T041,T042 | Check readiness claim, actual versions/artifacts/required checks còn hiệu lực; permission khác evidence | C148; không tự publish/deploy |

Không tạo task mới chỉ để thêm nhãn quy tắc; giữ 108 task và dependencies/order, mở rộng deliverable/case đúng owner. Foundation chỉ proof/manual controls; OS/browser evidence vẫn phải chạy tại task/platform tương ứng. Trước mỗi workload nặng, guard thực tế của môi trường phải đạt điều kiện RUN-03–RUN-05, không lấy lịch “sẽ implement T033” để cho chạy thiếu bảo vệ. Thiếu điều kiện chặn workload phụ thuộc, không chỉnh thứ tự/kiến trúc tùy ý.

Báo sau run: command/check/cwd/versions, volumes+free trước/sau, generated/retained bytes, removed candidates được phép, lỗi cleanup và limitations. Chỉ ghi số đã đo; không có thì unknown/NOT_RUN. Đây là evidence của công cụ/fixture, không tự chứng minh mọi trường hợp disk-full đã được ngăn chặn.


## S52 — Đóng audit 1.4.0 và đường kiểm chứng 1.5.0

### CLOSE-01 — Phạm vi hoàn thiện

Revision này hoàn thiện các thay đổi đã chấp thuận từ audit, không tuyên bố mọi chương trình hoặc mọi môi trường đạt correctness100%. `CLOSED_IN_SPEC` = đã có contract/flow/storage/owner task/case tương ứng; khác IMPLEMENTED/PRODUCT_VERIFIED. Không còn để A01–A08 cho AI tự chọn thiết kế trong phạm vi được mô tả dưới đây. Exact dependency version, tool paths, trusted target roots, measured throughput và quyền runtime thật vẫn phải xác minh qua gate, không điền bằng phỏng đoán để có nhãn hoàn thành.

| Audit | Quyết định và owner authority | Task/case bắt buộc | Closure |
|---|---|---|---|
| A01 GC FK | DB-03 exact order + leaf tree/cycle rollback, no FK disable | T106/T035; C149–C151 | CLOSED_IN_SPEC; SQL proof S48 |
| A02 resolver | CT-04 recipe + DB-03/DDL resolver_digest natural key, immutable multiple versions | T081/T085/T106; C152–C153 | CLOSED_IN_SPEC; SQL proof S48 |
| A03 session | CT-08 status READ, permission current version, PING/PONG SDK, AL-04 disarm | T087/T088/T094; C154–C157 | CLOSED_IN_SPEC; browser/OS product NOT_RUN |
| A04 continuation | CT-05 MappingMatch pages, bounded rename spool, fragments, expiry/progress | T025/T029/T090/T093; C158–C162 | CLOSED_IN_SPEC; model proof khác product |
| A05 federation | CT-06 pinned_vector+RefSelector+service, unavailable members, incarnations | T045/T046/T063/T096; C163–C166 | CLOSED_IN_SPEC; actual cross-repo NOT_RUN |
| A06 config/lifecycle | CT-07 RuntimeConfig defaults/CAS/restart, group/permission/record reads/mutations | T004/T028/T046/T047/T087; C167–C170 | CLOSED_IN_SPEC; actual CLI integration NOT_RUN |
| A07 maintenance | DB-05 pool lifecycle lease; DB-06 scoped journal/restore/new incarnation, no global build lock | T010/T035/T036/T037; C171–C173 | CLOSED_IN_SPEC; actual Go/Windows matrix NOT_RUN |
| A08 identity | CT-04 canonical bytes/recipes/vectors; capture input separate files result; semantic path validator | T006/T105/T107/T081; C174–C176 | CLOSED_IN_SPEC; provider integration NOT_RUN |
| O01 HMR | AL-08 local atomic maps watch opt-in/coalesce/busy status, no target launch | T091/T094; C177–C178 | SPECIFIED; browser product NOT_RUN |
| O02 retention/records/lock | DB-07 metrics/continuity, no auto-approve hoặc lease conversation phức tạp | T035/T039/T047/T099; C179 | SPECIFIED; measurements NOT_RUN |
| O03 end-to-end parity | registry/schema/DDLs/work cards đúng owner, source policies nguyên bản | T003/T108/T040; C180 | DOCUMENT_PROOF_ONLY |

### CLOSE-02 — Quy trình khởi công duy nhất

S00→S22→S50 role/mode→S41→task S19/S47→owner sections trong bảng trên khi liên quan. Giữ thứ tự T001→T002→T003→T105→T106→T107→T108 và 108 task; T029 thêm prerequisite T025 vì dùng pagination/result primitives. Không implementation feature trước seal đúng scope. Đặc tả/tài liệu proof không phải bằng chứng task DONE; progress hiện hành lấy từ S19/S22, không từ audit lịch sử.

Source-of-truth vẫn một file này. Không tạo `AUDIT_SPEC.md` hoặc `PATCH_SPEC.md` thay thế; proof scripts/results ZIP nếu bàn giao chỉ là bằng chứng có hash, không chứa quy tắc cạnh tranh. Ba nguồn docs Universal giữ nguyên byte/hash. Nếu quyền/môi trường thiếu, chặn đúng operation/capability; không thêm stub, bỏ negative case hoặc xin lại các quyết định user đã chốt.

## S53 — Commit theo nhóm 10 task và mốc khôi phục đầy đủ

### GIT-01 — Phạm vi và quyền đã được yêu cầu

Người dùng yêu cầu: **cứ hoàn tất 10 task thì gom toàn bộ thay đổi liên quan thành MỘT commit; không commit rời rạc từng task/file/phiên; mỗi mốc phải đủ để lấy lại source và tái dựng phần sản phẩm đã hoàn thành ở mốc đó.** Đây là policy riêng của MCE4D; giữ nguyên ba file nguồn trong docs. Không thêm commit tool vào MCP, không thay schema runtime hoặc quyền read-only của engine đối với target repositories.

Trong assignment `implement`/`integrate` được phép trên repository MCE4D, yêu cầu này cho phép commit local đúng batch sau các kiểm tra bên dưới; không cần xin lại từng commit đúng điều kiện nếu host không yêu cầu approval bổ sung. `docs_only`/`review` không tự chạy Git commit hoặc chuyển sang implementation. Phiên chỉ sửa file đặc tả là cập nhật bản vẽ, không tính một task sản phẩm DONE. Không tự init repository, tạo remote, thay nhánh có người dùng, publish, merge, deploy hoặc gửi source lên một dịch vụ chưa được phép. Push chỉ tới đúng remote/branch đã được người dùng cho phép và xác minh theo GIT-07; cụm “commit lên” không cho phép tự đoán tài khoản, URL hoặc visibility.

### GIT-02 — Cách đếm và ranh giới nhóm

`batch_size=10`. Đếm **task ID duy nhất đã DONE có bằng chứng và chưa được đưa vào một batch commit trước**, theo thứ tự thi công S47 trong scope đã mở; không đếm số file, số lần test, commit phụ, phiên AI hoặc ID số liên tiếp. Task còn VERIFY/BLOCKED/DEFERRED/CANCELLED không được tính. Không chia nhỏ task, tạo task giả hoặc bật optional scope để đủ 10. Bugfix/rework của task cũ đi cùng batch hiện hành, có `related_task_ids`, nhưng không đếm lại task đã nằm trong checkpoint trước; yêu cầu việc mới thực sự cần được thêm vào kế hoạch theo authority hiện có.

Batch được lên danh sách từ tối đa 10 task kế tiếp hợp lệ của S47; gate/prerequisite vẫn phải đạt. Không nhảy qua prerequisite hoặc task chưa xong để lấy đủ số. Song song chỉ trong ownership được duyệt, một đầu mối tích hợp/commit; worker không tự commit từng task. Chạm đúng 10 DONE thì dừng mở task thứ 11, kiểm bản kết hợp và đóng batch. Regression làm một task trong batch mất DONE thì batch chưa đủ điều kiện commit, sửa và kiểm lại trước. Task count chỉ là nhịp lưu lịch sử, không thay Definition of Done.

Không reset batch vì qua gate, đổi phiên, hết ngày hoặc cập nhật spec. Gate có thể PASS trên revision+diff/tree evidence chưa commit; không tạo vòng “phải commit mới DONE, phải DONE mới commit”. Nếu đang làm liên tục toàn roadmap theo S47, batch đầu tiên là:

```text
B001 = T001, T002, T003, T105, T106, T107, T108, T005, T004, T006
```

Danh sách trên là ví dụ kế hoạch, KHÔNG là evidence đã thực hiện. Nhóm sau tiếp tục từ task chưa được checkpoint tiếp theo, không dùng T001–T010 theo số ID. Không có commit đầu tiên trong repo mới thì B001 là root commit và `base_commit_oid=null`; không tạo thêm commit rỗng/khởi tạo chỉ để có baseline. Repo đã có lịch sử: giữ nguyên lịch sử, xác minh mốc áp dụng cùng các task thực sự đã hoàn tất; không tự squash/rebase lịch sử cũ để ép quy định mới.

**Ngoại lệ hữu hạn:**
- `scope_final`: khi toàn bộ phạm vi được giao thực sự kết thúc mà chỉ còn 1–9 task mới DONE, gom phần dư thành một commit cuối, ghi rõ số lượng và phạm vi đóng. Kết thúc phiên, tạm nghỉ, hết context hoặc bị chặn không phải scope_final. Nếu cả 108 task được mở và làm liên tục không có tail trước đó thì có 10 nhóm đủ 10 và nhóm cuối 8; nếu chỉ giao R1 52 task thì 5 nhóm đủ 10 và một nhóm cuối 2. Không mở R3 để làm tròn nhóm.
- `authorized_recovery`: sửa khẩn cấp/rollback một checkpoint đã commit chỉ khi có yêu cầu/phê duyệt cụ thể; ghi commit bị ảnh hưởng và lý do. Không giữ lỗi nguy hiểm chỉ để chờ đủ 10, cũng không tự gọi mọi sửa nhỏ là recovery để commit lẻ. Không amend/rebase/force-push checkpoint đã tạo. Sau ngoại lệ, tính nhóm kế tiếp từ các task chưa được checkpoint, không tính trùng.

### GIT-03 — “Commit đầy đủ” là đủ tái dựng, không phải add mọi file

Git commit ghi nội dung đã stage; file mới hoặc nội dung sửa sau stage không tự được bao gồm. `git commit -a` không thay kiểm tra file mới [REF56,REF57]. MUST lập danh mục thay đổi theo batch và kiểm trạng thái staged/unstaged/untracked/ignored, kể cả add/delete/rename/file mode. “Đầy đủ” gồm toàn bộ dependency closure cần thiết của batch và phần nền đã được giữ trong parent commit, không chỉ file AI vừa chạm cuối cùng.

| Phải nằm trong commit nếu thuộc sản phẩm/phạm vi | Không được đưa vào chỉ để gọi là đầy đủ |
|---|---|
| Source; tests; fixture/golden tự tạo; scripts build/check thực sự dùng; cấu hình không secret; manifests và lockfiles | Secret, token, credential, private key, .env thật, dữ liệu người dùng/production |
| Main spec cùng tracker/resume đúng trạng thái; ba policy nguồn đang được áp dụng; schema/registry; migrations; generator và generated artifact mà build cần | Cache, node_modules, dependency download cache, scratch, build outputs tái tạo được, dump/log/raw trace nhạy cảm |
| Thay đổi API/provider+consumer cùng phạm vi, file bị xóa, license/notices và mẫu config an toàn có liên quan | Source của target repo đang phân tích, SQLite runtime stores/MCE4D_HOME, backup database hay thay đổi của người khác ngoài assignment |
| Tóm tắt test/evidence và chỉ dẫn build/restore/toolchain pins đủ tái hiện, có locator/hash cho artifact ngoài Git | Chỉ một đường dẫn log ở máy cũ mà không có nội dung/tóm tắt/nguồn lưu bền được kiểm; URL không được phép hoặc artifact tưởng tượng |

Không mặc định `git add .`/`git add -A` toàn repo. Stage bằng danh sách path đã rà soát, xử lý đúng tên có khoảng trắng/ký tự đặc biệt; bao gồm deletion hợp lệ. File bắt buộc đang ignored phải được xử lý có chủ đích trong scope, không bỏ sót hoặc `-f` toàn cây. Hash/raw bytes ba policy nguồn không đổi. File chỉ đổi line endings phải được giải thích theo policy, không trộn format toàn repo.

Baseline/status trước batch phải cho biết thay đổi sẵn có và ai sở hữu. Có thay đổi bên ngoài scope: giữ nguyên và cô lập bằng worktree/clone đã được phép hoặc phối hợp; không stash/reset/clean để tạo vẻ sạch, không stage hộ. Baseline có thay đổi cần thiết cho batch nhưng chưa thuộc lịch sử thì phải có quyền tiếp nhận chúng hoặc task bị chặn; không tạo checkpoint dựa vào dependency “chỉ có trên máy hiện tại”.

Secrets/runtime state không nằm trong Git: commit phải ghi **tham chiếu cấu hình và cách cấp lại an toàn**, không ghi giá trị secret. Submodule/Git LFS/artifact ngoài Git nếu thực sự có: pin đúng revision/object digest và xác minh bản có thể lấy lại; Git pointer không phải toàn nội dung. Không thêm các cơ chế này khi dự án chưa dùng. Lockfile/pin không bảo đảm registry tồn tại mãi; thiếu vật tư đã lưu thì ghi giới hạn tái dựng, không gắn nhãn khôi phục đầy đủ vô điều kiện.

### GIT-04 — Quy trình đóng một batch

1. **Đối chiếu:** đọc S19/S21/S22, lịch S47 và actual Git log/status. Xác minh branch/worktree/base commit, danh sách đúng 10 task hoặc ngoại lệ hợp lệ, quyền và không có conflict/merge/rebase dở. Repo đang detached HEAD hoặc branch dùng chung chưa được giao: dừng phần commit, không tự đoán nhánh.
2. **Tích hợp:** hoàn tất toàn bộ code/test/schema/docs liên quan, bảo toàn thay đổi người khác. Cập nhật tracker/evidence sau từng task dù chưa commit. Dừng writer của cùng batch trong cửa sổ stage→verify→commit, không khóa build mọi dự án.
3. **Kiểm bản kết hợp:** chạy các kiểm tra bắt buộc hiện có tương ứng phần đã triển khai theo S46/S48, không đợi đủ 10 mới test từng task. Không ép chạy command chưa tồn tại ở giai đoạn đầu. Lỗi bắt buộc/thiếu prerequisite chặn commit; fixture-only label đã được gate cho phép phải được giữ nguyên, không biến thành real-repo PASS.
4. **Soát nội dung và stage:** kiểm diff, secret/scope, tracked+untracked+ignored bắt buộc và file mode; stage đầy đủ danh mục đã duyệt. Kiểm `git diff --cached --check`, staged diff, unmerged entries và phần còn unstaged. Hook/filter/signing đang cấu hình phải được hiểu và cho phép; không `--no-verify`, không vô hiệu hook hoặc tự điền identity/signing để vượt lỗi.
5. **Khóa nội dung được kiểm:** tree chuẩn bị commit phải khớp source/config/test đã kiểm. Lấy staged tree OID bằng `git write-tree` sau khi chốt metadata. Ghi OID trong commit message/receipt bên ngoài tree, không chèn own tree/commit hash vào chính file đang tạo ra hash. Thay bytes sau kiểm (kể cả hook sửa source) làm mất tính hợp lệ: re-stage và chạy lại kiểm tra bị ảnh hưởng. Không commit selected path trực tiếp để bỏ qua staged review.
6. **Một commit:** tạo đúng một commit với message/trailers GIT-05. Xác minh HEAD mới có parent kỳ vọng (hoặc root), tree bằng staged tree đã chốt, task list đúng và không có thay đổi batch bắt buộc bị bỏ ngoài commit. Timeout/mất phản hồi phải đọc log/HEAD/tree để xác định commit đã tồn tại trước khi retry; không tạo commit trùng/empty.
7. **Thử lấy lại mốc:** checkout commit vừa tạo vào workspace sạch riêng được phép theo GIT-06, dùng source/materials của mốc đó; kiểm tối thiểu đầy đủ file, contract/spec parity, build/tests bắt buộc cho phạm vi đã có. Không dùng file chưa commit từ workspace gốc để làm test qua. Nếu bước này fail: giữ nguyên commit, đánh dấu checkpoint chưa verified, chặn publish và batch tiếp theo; sửa theo quyền recovery rõ ràng, không che bằng amend hoặc tag giả.
8. **Gắn mốc:** sau restore check PASS, tạo annotated tag bất biến `mce4d/checkpoints/B001` (ID thực theo batch). Annotation lưu commit/tree OID, task IDs/count, loại batch, evidence và kết quả khôi phục có phạm vi. Tag tồn tại phải trỏ đúng commit/evidence; không retag/force. Không tự tạo commit phụ chỉ để ghi tag/hash. Push/backup theo GIT-07; báo local checkpoint và bản sao độc lập riêng biệt.

`DONE` của từng task ≠ batch đã commit ≠ restore đã verified ≠ đã push/backup. Không mở task thứ 11 khi batch đủ 10 chưa hoàn tất commit+local restore/tag trong điều kiện có quyền. Thiếu quyền Git hoặc lỗi môi trường thì giữ changes, ghi blocker và không gọi đã có checkpoint; không làm thêm task để phình batch. Push chưa thành công được báo riêng; không xóa local checkpoint hoặc tự tạo lại commit.

### GIT-05 — Định danh, message và bằng chứng không sinh commit phụ

Message mẫu dưới chỉ là format, không lệnh thực thi. Thay mọi placeholder bằng dữ kiện thật; subject nêu kết quả của nhóm, không chỉ “update/fix”.

```text
checkpoint(B001): <mô tả ngắn kết quả của nhóm>

MCE4D-Batch: B001
MCE4D-Batch-Type: full
MCE4D-Task-Count: 10
MCE4D-Tasks: T001,T002,T003,T105,T106,T107,T108,T005,T004,T006
MCE4D-Spec-Version: 1.6.0
MCE4D-Tree: <staged-tree-oid-đã-xác-minh>
MCE4D-Checkpoint: mce4d/checkpoints/B001
MCE4D-Verification: <evidence-ids-hoặc-locator-không-secret>
```

`Bnnn` tăng tuần tự theo các checkpoint batch thực tế, không đặt lại theo phiên/branch; xác minh collision với lịch sử/tag trước khi dùng. Chỉ một coordinator giữ quyền commit/tag trên nhánh tích hợp đã giao. Nếu người dùng yêu cầu mô hình branch khác, phải giữ cùng quy tắc một batch commit và không rewrite lịch sử ngoài quyền.

S21 lưu **batch metadata**, không là tracker task thứ hai: batch_id/type; task_ids/related_task_ids; base commit/branch; scope/authority; content inventory và exclusions; evidence/restore commands/tool versions; planned tag; các giới hạn external materials. S19 vẫn giữ task status. S22 chỉ là summary derived; khi tiếp tục phải reconcile với Git log/trailers/tag thực tế, không tin số đếm trong chat.

Trước commit, record hiện tại ghi `commit_binding=containing_commit`, commit/tree OID chưa biết để null; không ghi “đã push/đã restore PASS” trước khi xảy ra. Commit message có staged tree OID vì message không nằm trong tree. Kết quả post-commit ghi vào annotated tag/receipt không chứa secret và báo cáo phiên; lần batch kế tiếp có thể bổ sung OID/receipt của batch trước trong S21. Không tạo commit metadata-only hoặc amend chỉ để nhét SHA của chính commit vào file. Khi còn receipt chưa được ghi trong spec, Git tag/trailers là bằng chứng thực; validator phải chấp nhận prepared record đã được reconcile thay vì bắt sửa file rồi commit vô hạn.

### GIT-06 — Khôi phục source, khôi phục dữ liệu và giai đoạn chưa đủ 10

**Khôi phục một checkpoint:** xác minh tag/commit/tree/evidence, tạo worktree mới bằng mẫu `git worktree add --detach <new-approved-empty-path> <verified-checkpoint-ref>` hoặc clone sạch từ bản sao được phép. Thay placeholder thực, kiểm Git version/quyền/path trước chạy; đây không là lệnh để tự thực thi trong docs-only [REF58]. Giữ workspace hiện tại và thay đổi chưa commit; không dùng `reset --hard`, `clean -fd`, checkout đè hoặc force-push để “quay lại”. Muốn tiếp tục phát triển từ mốc cũ thì tạo branch mới trong quyền; muốn hoàn tác trên nhánh chung thì cần task recovery và đánh giá dependency/data, không mặc định revert một commit giữa chuỗi là an toàn [REF60].

Mỗi mốc phải có lệnh/cwd/toolchain/lockfile/config template/schema migration của **chính mốc đó** và danh mục external materials. Restore smoke không chứng minh toàn sản phẩm hoàn thiện; checkpoint đầu có thể chỉ là foundation nhưng phải dựng lại đúng foundation đã kiểm. Commit không sao lưu database, tài khoản, queue, secret, model weights, OS hoặc dịch vụ ngoài. Runtime MCE4D stores dùng S44 backup/restore và schema compatibility riêng; không chạy binary cũ trên database mới nếu không tương thích, không tự down-migrate dữ liệu thật.

**Giữa hai checkpoint:** vẫn lưu file và cập nhật evidence/tracker sau từng task. Chưa đủ 10 thì không WIP/fixup/temp commit, không stash như backup mặc định. Khi kết thúc task/phiên hoặc trước thao tác có nguy cơ mất bản sửa, tạo/cập nhật snapshot tạm đã được phép ở vùng owned ngoài Git history theo S51: base commit, diff cả staged+unstaged, file mới cần thiết, deletion/mode, task/resume, manifest/hash; repo chưa có commit dùng full approved source snapshot. Snapshot phải lọc secrets, có retention hữu hạn và phép thử phục hồi; Git diff đơn lẻ thiếu file untracked thì không gọi đủ. Không snapshot `.git`, target repo, runtime database hoặc dữ liệu ngoài quyền.

Chưa có vị trí/quyền snapshot thì để nguyên working tree, ghi `UNCOMMITTED_NOT_BACKED_UP`; không bịa có backup, không tự tải cloud hoặc xóa output. Snapshot tạm không là checkpoint verified của 10 task. Quy định bảo đảm đường trở lại **các mốc đã commit và còn được giữ**, cộng các interim snapshot thật đã kiểm; không hứa quay về mọi lần gõ phím hay mỗi task bằng Git khi đã chọn một commit/10 task.

### GIT-07 — Push nguyên nhóm, bản sao độc lập và xử lý thất bại

Sau một checkpoint verified, push **một nhóm gồm đúng commit chain cần thiết của nhánh đã duyệt và tag checkpoint đó** khi remote/branch/quyền mạng đã rõ. Không push từng task, không `--all/--mirror/--tags` ngoài danh sách, không force. Server hỗ trợ atomic ref update thì dùng chế độ đã xác minh; nếu không hỗ trợ, đẩy branch rồi tag trong cùng bước bàn giao và kiểm từng ref, không quảng cáo nguyên tử [REF59]. Fetch/read remote khi cần cũng phải trong quyền; lịch sử remote phân kỳ thì dừng và lập kế hoạch tích hợp, không tự rebase/overwrite.

Push timeout/mất mạng: so local commit/tag với remote refs bằng công cụ được phép để biết đã ghi hay chưa. Chưa xác minh được thì `push_status=UNKNOWN|PENDING|FAILED`, giữ commit/tag nguyên vẹn và retry hữu hạn đúng ref; không lặp commit/tag để chữa lỗi push. Không tạo commit riêng chỉ để ghi push thành công. Chỉ cập nhật receipt/summary có căn cứ. Local checkpoint đã verified có thể tiếp tục được dùng; thiếu backup độc lập phải báo rõ, và chỉ chặn batch tiếp theo nếu policy/assignment thực tế yêu cầu remote backup trước khi tiếp tục.

Không có remote được duyệt: checkpoint vẫn ở local, ghi `remote_backup=NOT_CONFIGURED`; không nói đã “commit lên GitHub”. Có đích backup khác được phép thì có thể tạo full Git bundle chứa nhánh/tag cần thiết, kiểm `git bundle verify` và thử clone tại vùng riêng. Bundle/incremental phải kiểm prerequisites; bundle không tự chứa uncommitted work, Git LFS objects hoặc repo con [REF61]. Bản sao cùng ổ không bảo vệ mất ổ; một tag/worktree cùng repository không phải bản backup độc lập. Thiếu đích lưu độc lập/credentials/materials thì nêu giới hạn, không tự tạo dịch vụ.

Giữ các checkpoint tag/history và vật tư khôi phục theo chính sách đã duyệt; không tự xóa/chuyển tag, squash, amend hoặc rebase các checkpoint để làm gọn log. `git fsck`/verify phát hiện vấn đề nội bộ nhưng không thay thử clone/checkout/build. Giảm số commit không được đổi thành bỏ test, bỏ backup hoặc nhét secrets vào lịch sử.

### GIT-08 — Tích hợp vào task hiện có và kiểm tra

| Owner | Điều cần làm trong scope đã có | Cases |
|---|---|---|
| T001 | Xác minh Git root/branch/history/dirty baseline/quyền, mốc áp dụng và đích backup nếu có; không tự commit bootstrap | C181,C182,C185,C189,C191 |
| T003 | spec_check kiểm đếm task DONE duy nhất, batch metadata/trailers, tail/exception authority và không self-hash loop; tool chỉ kiểm, không tự commit/push | C181,C182,C183,C187,C190,C192 |
| T107 | Fixture workflow Git nhỏ cô lập: stage đủ/thiếu, secret/out-of-scope, commit retry, checkout/restore và local bare remote; không tác động repo người dùng | C184..C192 |
| T108 | Seal policy/ownership/permission và proof trước batch đầu, không lấy fixture proof làm checkpoint sản phẩm | C181,C182,C183,C192 |
| T034,T040 | Regression bản kết hợp, completeness của snapshot/materials, docs/code parity và resume sau gián đoạn | C181..C192 |
| T041,T042 | Kiểm mốc khôi phục/retention/backup/claim trước release; không tự kết thúc batch nonterminal chỉ vì qua gate | C186,C188,C189,C190 |

Mọi task implement/integrate đều theo nhịp S53 sau DONE; bảng trên chỉ owner của guard/proof, không miễn kiểm cho task khác. Hỗ trợ thủ công có bằng chứng khi tool guard chưa được viết; không biến policy Markdown thành claim enforced. Dùng Git/tooling hiện có; không thêm hook global, service backup, database hoặc task sản phẩm mới chỉ để thực thi nhịp commit. `108` task, dependencies và work order S47 được giữ nguyên; các case mới là tiêu chí kiểm của workflow phát triển, không API sản phẩm.
