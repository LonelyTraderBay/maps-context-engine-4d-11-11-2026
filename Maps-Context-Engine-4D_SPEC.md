---
document_id: MCE4D-SPEC
file_name: Maps-Context-Engine-4D_SPEC.md
spec_version: 1.6.0
spec_date: 2026-11-11
language: vi
primary_reader: AI_CODING_AGENT
project_label: Maps-Context-Engine-4D
project_id: maps-context-engine-4d
binary_name: mce4d
project_name_status: INTERNAL_WORKING_NAME_NOT_TRADEMARK_CLEARED
spec_status: AUDIT_A01_A08_CLOSED_IN_SPEC_WITH_MANDATORY_PREFLIGHT_SEALS
implementation_status: NOT_STARTED
product_verification_status: NOT_RUN
document_structural_validation: PASS_75_DOCUMENT_AND_SYNTHETIC_GIT_CHECKS_PRODUCT_NOT_RUN
source_of_truth: THIS_FILE
policy_reference_directory: docs
policy_adoption: ACCEPTED_SCOPED_S50
policy_installation_in_project: NOT_VERIFIED
policy_runtime_enforcement: NOT_IMPLEMENTED
approved_audit_findings: 8
approved_audit_findings_integrated: 8
git_batch_policy: S53
git_batch_size: 10
git_checkpoint_workflow_status: SPECIFIED_NOT_IMPLEMENTED
git_project_checkpoint_status: NOT_VERIFIED
previous_spec_sha256: a4995cdab7d3fa8a4e850e84e62a496ea811def9c8089dd1a3c7bdaac0e98321
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
| Go toolchain | Stable supported được xác minh ở T002 | UNRESOLVED_AT_SPEC | REF01 | NOT_RUN | T002 |
| SQLite driver | modernc.org/sqlite | UNRESOLVED | REVIEW_REQUIRED; REF11 | NOT_RUN | T002 |
| MCP SDK | github.com/modelcontextprotocol/go-sdk | UNRESOLVED | MULTI_LICENSE_REVIEW; REF12 | NOT_RUN | T002 |
| Semantic Go loader | golang.org/x/tools/go/packages | UNRESOLVED | REVIEW_REQUIRED; REF07 | NOT_RUN | T002 |
| Go language server | golang.org/x/tools/gopls | UNRESOLVED | REVIEW_REQUIRED; REF13 | NOT_RUN | T029 |
| OS adapters | golang.org/x/sys nếu cần | UNRESOLVED | REVIEW_REQUIRED | NOT_RUN | T010 |
| Exact tokenizer | Không bắt buộc R1 | NOT_SELECTED | N/A | N/A | T025 |

MUST T002 điền pin/checksum/license scope sau khi kiểm tra, không đoán số version trong file này. Không blocking người dùng bằng câu hỏi lựa chọn library đã được tài liệu quyết định; giải quyết qua gate kỹ thuật.

Thiết kế vật tư đóng ở BP-03; exact versions còn UNRESOLVED là preflight dữ kiện, không quyền AI chọn library khác. T105–T108 trước feature; npm workspace conventions và selected baseline UI deps ở S46. Nếu compatibility thất bại, ghi discrepancy theo S41 thay vì silent swap.

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

## S19 — Bảng triển khai duy nhất

### Protocol trạng thái

Status enum: TODO, IN_PROGRESS, BLOCKED, VERIFY, DONE, DEFERRED, CANCELLED. Track U0–U2 là scope được yêu cầu ở v1.2.0; U3 optional. R2 prerequisites được kéo vào đường ưu tiên Explorer theo S39 sau G5, không chờ đóng toàn G6. DONE chỉ sau code/test/evidence + acceptance tương ứng; tài liệu thiết kế không làm task implementation DONE. VERIFY không tính hoàn thành. DEFERRED R2 không tính mẫu số R1. CANCELLED cần lý do/ADR và không tự giảm mẫu số baseline để tăng progress.

Luồng: TODO → IN_PROGRESS → VERIFY → DONE. IN_PROGRESS/VERIFY → BLOCKED khi thiếu prerequisite; BLOCKED → TODO/IN_PROGRESS sau resolve có evidence. DONE → TODO khi regression hoặc contract thay đổi; lưu lý do trong changelog. Chỉ claim task khi dependencies DONE và gate trước đã PASS. G1 có dependencies không cùng thứ tự số, nên chọn topological ready task, không chỉ T008 trước T012.

Một agent chỉ một task IN_PROGRESS mặc định. Nhiều agent được làm task độc lập khi paths ownership không chồng; declaration `owner/task/paths/base_commit` trước sửa. File đặc tả do coordinator merge ngắn theo hash/revision, không giữ khóa file trong build/test. Không áp dụng mutex này lên mọi project của máy.

Mỗi cập nhật tracker phải read current revision, patch đúng hàng, kiểm tra không mất cập nhật khác; conflict → merge có review, không last-write-wins. Log build/test và artifacts có thể ngoài file, nhưng ID/hash/kết quả/evidence pointers phải trong S21. Task DONE được ghi ngay với tree/diff evidence, KHÔNG đồng nghĩa phải commit ngay. S53 gom 10 task DONE thành một commit; checkpoint/restore/push có metadata riêng tại S21, không thay task status. Trước task thứ 11 phải đóng local checkpoint của nhóm đầy theo quyền; không bỏ gate để đủ số.

### Progress ban đầu — derived từ các hàng task

- R1: 52 task; DONE=0; IN_PROGRESS=0; BLOCKED=0; VERIFY=0; TODO=52; completion=0/52=0%.
- R2: 20 task DEFERRED; roadmap đa stack, không thuộc mẫu số R1.
- R3: 12 task DEFERRED; mở capability theo gate.
- Explorer U0: 4 TODO; U1: 10 TODO; U2: 6 TODO; U3: 4 DEFERRED. TODO không đồng nghĩa prerequisites đã đủ hoặc permission runtime đã có.
- Toàn roadmap: 108 tasks; 72 TODO + 36 DEFERRED; implementation DONE=0. R1/R2/R3/U0/U1/U2/U3 tính riêng; không gọi task-count là % effort.
- Gate hiện tại: G0 chưa PASS. Task tiếp theo: T001. Product build/tests/benchmarks: NOT_RUN.

Tỷ lệ trên là task-count completion, không phải % effort hoặc % độ đúng của sản phẩm. Validator MUST tính lại khi cập nhật task, không sửa phần trăm thủ công.

### Tracker

Cột Cases là truy vết tới oracle nghiệm thu, không có nghĩa một task nền tảng phải hoàn thành toàn bộ E2E case thuộc feature chưa được triển khai. Task phải chứng minh các subcase thuộc chính deliverable của nó, ghi scope evidence; phần E2E chưa chạy giữ NOT_RUN. T034 và G4 tổng hợp oracle đầy đủ. Không dùng quy tắc này để bỏ integration test nằm ngay trong phạm vi task.

| Task | Gate | Dependencies | Deliverable / định nghĩa hoàn thành | Req | Cases | Status | Owner | Evidence |
|---|---|---|---|---|---|---|---|---|
| T001 | G0 | NONE | Site survey + safe private module bootstrap only; preserve existing workspace; no feature code trước T108. Resolve source-policy docs/mode theo S50; ghi actual path/hash, không sửa bản gốc. Khảo sát batch baseline/history/quyền remote và S53; không commit task khởi tạo riêng. | R001,R105,R106,R125,R129 | C041,C137,C138,C181,C182,C185,C189,C191 | TODO | UNASSIGNED | NONE |
| T002 | G0 | T001 | Pin exact toolchain/dependencies, license/API ledger và smoke fixtures; không tự chọn technology ngoài S41. Áp command preflight S51 trước smoke, output volumes/budgets từ bounded evidence. | R001,R036,R108,R109 | C039,C042,C046,C140,C141,C142 | TODO | UNASSIGNED | NONE |
| T003 | G0 | T001 | Stdlib spec_check: IDs/DAG/counts/plan/contract authority/evidence rules; tests chống fake DONE và backward task order. Kiểm policy ledger/mode/claim metadata, không nhận docs PASS là guard enforced. Validator kiểm audit closure/registry transport/default/hash vector counts; no fake evidence. Kiểm batch 10 DONE/trailers/tail/receipt theo S53; validator không tự commit. | R030,R093,R102,R103,R105,R106,R112,R124,R125,R128 | C043,C133,C135,C137,C138,C148,C180,C181,C182,C183,C187,C190,C192 | TODO | UNASSIGNED | NONE |
| T004 | G1 | T005,T106 | Catalog use cases trên schema frozen: register/profile/rebind/remove; transactional revisions/identity; no source mutation. Runtime config/policy/profile/group metadata foundations và incarnation initialization; controller CLI wiring ở đúng task sau. | R002,R118,R119 | C001,C016,C167,C168,C157,C169,C170 | TODO | UNASSIGNED | NONE |
| T005 | G1 | T108 | Secure path open, deny policy, secret pre-persistence controls; OS adapters baseline. Guard containment cũng cho build-output cleanup khi áp dụng, không nới quyền. | R013,R014,R110 | C024,C025,C026,C145 | TODO | UNASSIGNED | NONE |
| T006 | G1 | T005 | Captured bytes/hash/UTF-8 ranges/manifest encoding deterministic. C14N1/capture-vs-parse input contract; shared vectors. | R005,R121 | C015,C023,C027,C174,C175,C176 | TODO | UNASSIGNED | NONE |
| T007 | G1 | T004,T006,T106 | Implement index store adapters/migrations từ S44, same-generation FKs/FTS/capture guards, not redesign schema. Store incarnation/lifecycle lease port được wired với T010 trước public access; no hidden independent pool. | R004,R010,R120 | C019,C037,C042,C171,C172,C173 | TODO | UNASSIGNED | NONE |
| T008 | G1 | T007,T012,T010 | Staging + atomic publish + active profile pointer + consistent reader. | R007,R092,R093 | C019,C020,C117,C136 | TODO | UNASSIGNED | NONE |
| T009 | G1 | T008,T010 | Crash/cancel recovery generation BUILDING; last published vẫn đọc được. | R007 | C019,C022,C036 | TODO | UNASSIGNED | NONE |
| T010 | G1 | T004,T007 | OS writer locks per worktree; bounded wait; không TTL steal/global build lock. Shared/exclusive lifecycle pool lease theo DB-05, maintenance scoped, không nâng lock tại chỗ. | R003,R120 | C021,C022,C171,C172,C173 | TODO | UNASSIGNED | NONE |
| T011 | G1 | T008,T010,T012 | Multi-process concurrency test: A/B độc lập, reader/writer và profiles. | R003 | C001,C020,C021 | TODO | UNASSIGNED | NONE |
| T012 | G1 | T004,T005,T006 | Git inventory tracked/untracked/exclusions/nested/sparse + file map. | R019 | C011,C013,C014,C016 | TODO | UNASSIGNED | NONE |
| T013 | G2 | T006,T012 | Go AST declarations/scopes/package variants trên captured bytes. | R008 | C002,C003,C004,C010 | TODO | UNASSIGNED | NONE |
| T014 | G2 | T013,T020 | Typed Defs/Uses/Selections; local fields/params/locals không mất references. | R008,R009,R099 | C003,C004,C005,C009,C129 | TODO | UNASSIGNED | NONE |
| T015 | G2 | T014,T007 | Directed multirelation graph, direct/interface/candidate/unresolved edges + source sites. | R005,R010,R011 | C006,C007,C008,C045 | TODO | UNASSIGNED | NONE |
| T016 | G2 | T015 | Coverage/diagnostics/test refs/boundaries; no zero-result overclaim. | R011,R015,R025 | C008,C010,C011,C045 | TODO | UNASSIGNED | NONE |
| T017 | G2 | T007,T012,T013 | Exact/prefix symbol lookup + bounded FTS5 text search, index plans. | R019 | C002,C042,C045 | TODO | UNASSIGNED | NONE |
| T018 | G2 | T008,T012,T015 | Dirty/add/delete/rename/config changes; manifest diff/invalidation. | R006,R020 | C013,C014,C015,C016,C017 | TODO | UNASSIGNED | NONE |
| T019 | G2 | T018,T016 | Strict full-workspace freshness + source preconditions; unknown/failure explicit. | R006,R017,R097 | C013,C015,C023,C122 | TODO | UNASSIGNED | NONE |
| T020 | G2 | T002,T005,T012 | Offline trusted package loader/profile, controlled env/tool paths; partial fallback. | R014,R032,R099,R121 | C009,C012,C046,C047,C129,C132,C174,C175,C176 | TODO | UNASSIGNED | NONE |
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
| T105 | G0 | T002,T003 | Canonical model/ports/closed schemas+registry, semantic validators và import-boundary tests trước feature code. Ghi Change Budget/Complexity gate cho delta ngoài baseline. | R089,R090,R097,R100,R107,R115,R116,R117,R118,R119,R120,R121 | C113,C114,C122,C131,C134,C139,C154,C155,C156,C157,C158,C159,C160,C161,C162,C163,C164,C165,C166,C167,C168,C169,C170,C171,C172,C173,C174,C175,C176 | TODO | UNASSIGNED | NONE |
| T106 | G0 | T105 | Trích physical DDL/catalog/index/records, migration/FK/FTS/publish/CAS proof fixtures trên pinned driver; không target data. SQL counterexamples GC/resolver fixed và restore incarnation/permission storage proof. | R091,R094,R113,R114,R120 | C115,C116,C117,C118,C119,C120,C121,C149,C150,C151,C152,C153,C171,C172,C173 | TODO | UNASSIGNED | NONE |
| T107 | G0 | T105,T106 | Contract-level flow/serialization/provider/source/state proof, MCP delivery budgets, codegen/extraction parity fixtures. Bổ sung pure command/storage/evidence contract fixtures S51; chưa là OS wrapper proof. New status/paging/scope/config/codec/state positive/negative proofs; no claim OS/browser tested. Thêm Git workflow fixture cô lập cho staged completeness/restore/retry/backup theo S53; không repo người dùng. | R090,R092,R098,R099,R108,R109,R110,R111,R115,R116,R117,R118,R119,R120,R121,R126,R127,R129 | C114,C117,C128,C129,C132,C136,C140,C141,C142,C143,C144,C145,C146,C154,C155,C156,C157,C158,C159,C160,C161,C162,C163,C164,C165,C166,C167,C168,C169,C170,C171,C172,C173,C174,C175,C176,C184,C185,C186,C187,C188,C189,C190,C191,C192 | TODO | UNASSIGNED | NONE |
| T108 | G0 | T106,T107 | Design seal review: authority/schema/DDL/ownership/order/pins/oracles verified; work cards ready, no unresolved architectural gaps for core. Seal mode/policy/command-safety readiness theo S50/S51, không fake enforcement. Audit A01–A08 core closure+owner gates verified; schema changes traced, product tests vẫn independent. Seal nhịp batch và fixture quyền/đếm/task order; no actual product checkpoint từ proof. | R089,R092,R093,R100,R102,R103,R104,R106,R107,R112,R124,R125 | C113,C133,C134,C135,C138,C139,C148,C180,C181,C182,C183,C192 | TODO | UNASSIGNED | NONE |

Thứ tự thi công không theo số ID; theo S47. Trạng thái chỉ ở bảng này. T105–T108 ở G0 nên thực hiện trước T004 trở đi; task R2/UI chưa đủ prerequisite không được claim dù TODO.

## S20 — Gates và Definition of Done

| Gate | Phạm vi task | Điều kiện PASS | Status ban đầu |
|---|---|---|---|
| G0 — Foundation/design seal | T001–T003,T105–T108 | Site survey, pinned materials, canonical contracts/DDL/flow proofs/import boundaries/order validation; T108 PASS trước feature; mode/nguồn quy tắc/command preflight S50–S51 | NOT_RUN |
| G1 — Safe storage | T004–T012 | Identity/isolation/path safety/capture/SQLite/publish/recovery; A không khóa B | NOT_RUN |
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

Template ADR mới: `id; problem/evidence; options; decision; constraints preserved; migration/rollback; affected R/T/C; status; date`. Không rewrite ADR cũ mất lịch sử; append supersession gọn.

### Evidence log

Chỉ `E001` được điền khi task code thật có evidence. Lúc soạn tài liệu: không có evidence sản phẩm; bảng trống có chủ ý.

| Evidence ID | Task | Code revision / tree digest | Environment + versions | Command / case | Result | Artifact path + SHA-256 | Checked at UTC |
|---|---|---|---|---|---|---|---|
| NONE | NONE | NONE | NONE | NONE | NOT_RUN | NONE | NONE |

Format result: PASS, FAIL, NOT_RUN. NOT_RUN phải có lý do trong record. Hash tree dùng khi chưa commit; không bịa commit SHA. Absolute local paths chỉ trong private evidence khi cần, không publish; log sanitized. File artifact không tồn tại thì không tạo pointer giả. DONE task MUST trỏ >=1 evidence record PASS bao phủ test scope; gate reviewer kiểm tra không chỉ string “PASS”.

### Metadata áp dụng cho task/lệnh — không thêm tracker

Khi có thực thi, ghi trong evidence/ghi chú S21 gắn task S19: `task_mode`, `assignment_ref`, `spec_revision`, `policy_source_refs` (actual path/version/digest/status), `change_budget` (objective/paths/behavior/contracts/exclusions), `verification_plan` (V0–V4 áp dụng + required checks), và `command_record_refs` theo S51. Chỉ lưu delta/locator cần thiết, không sao chép spec hoặc raw logs. Chưa có run thì không tạo record giả. Các metadata này phục vụ governance, không đổi S43/S44 hay mạo nhận policy được thực thi.

### Metadata checkpoint Git — không phải tracker task thứ hai

S53 điều hành nhịp commit; các task vẫn chỉ có trạng thái ở S19. Record sau là trạng thái khởi đầu, chưa quan sát repository/Git thật. Khi có run, ghi batch metadata vào S21, derive summary S22, reconcile actual log/trailers/tag; không tự ghi đã commit/push/restore khi chưa làm.

```yaml
git_batch_control:
  policy: S53
  batch_size: 10
  counting_order: S47
  active_batch_id: null
  active_task_ids: []
  active_done_count: 0
  base_commit_oid: null
  branch: null
  commit_binding: null
  planned_checkpoint_tag: null
  last_verified_checkpoint_oid: null
  last_verified_checkpoint_tag: null
  remote_alias: null
  remote_branch: null
  push_status: NOT_CONFIGURED
  independent_backup_status: NOT_VERIFIED
  interim_snapshot_ref: null
  project_restore_verification: NOT_RUN
```

Commit hiện hành được nhận diện từ containing commit/trailers và tag theo GIT-05, không tự lưu own SHA trong source đã commit. OID/tag của batch trước chỉ đi vào spec ở lần batch tiếp theo khi cần; không commit phụ để cập nhật receipt. Không đưa bản ghi này vào wire/schema/SQLite của engine.

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

## S22 — Trạng thái tiếp tục cho phiên AI sau

```yaml
resume:
  spec_version: 1.6.0
  implementation_revision: null
  release_label: NOT_BUILT
  gate: G0
  design_seal: NOT_RUN
  construction_plan: S47
  policy_registry: S50
  command_storage_policy: S51
  audit_closure: S52
  git_batch_policy: S53
  git_batch_size: 10
  git_batch_current_id: null
  git_batch_done_count: 0
  last_git_checkpoint: null
  git_project_restore: NOT_RUN
  git_remote_status: NOT_CONFIGURED
  audit_baseline: 1.4.0
  canonical_codec: MCE-C14N1
  remaining_scope_gate: ACTUAL_TOOLCHAIN_OS_BROWSER_AND_REAL_REPO_VERIFICATION
  policy_adoption_status: ACCEPTED_SCOPED
  project_policy_files_status: NOT_VERIFIED
  policy_runtime_enforcement: NOT_IMPLEMENTED
  last_request_mode: docs_only
  next_task_mode: REQUIRES_CURRENT_ASSIGNMENT
  foundation_sequence: [T001, T002, T003, T105, T106, T107, T108]
  next_task: T001
  active_task: null
  active_owner: null
  done_r1_tasks: 0
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
  discovery_gaps: [K001, K002, K003, K004, K007, K008, K009, K010, K011, K013, K014, K015]
  known_limits: [K005, K006, K012]
  last_evidence_id: null
  last_checked_at_utc: null
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

Bản hiện tại 1.6.0 gồm 54 sections S00–S53, 130 requirements R001–R130, 108 tasks T001–T108, 192 acceptance cases C001–C192. R1/R2/R3 có 52/20/12 task; Explorer U0/U1/U2/U3 có 4/10/6/4. Tổng 72 TODO +36 DEFERRED, DONE=0. Product build/tests/benchmark NOT_RUN; document audit riêng S48.

V1.6.0 construction routing: đọc đúng owner section theo S52/S50/task và S53 cho batch commit; Explorer T081 seal schema/state/proof trước T082; BuildManifest↔Binding tách S43/S44/S45; phiên làm việc theo S47, không quyền tự diễn giải thêm API.

## S41 — Bản vẽ thi công: quyền quyết định, khóa thiết kế và sai lệch

### BP-01 — Hợp đồng với AI triển khai

Đặc tả v1.6.0 là bản vẽ của sản phẩm cần xây. `DESIGN_BASELINE` nghĩa thiết kế đã được mô tả, KHÔNG nghĩa code/API dependency đã được kiểm chứng. Không có cam kết zero bug/zero rework. Mục tiêu: loại việc làm lại do đoán contract, ghép sai luồng hoặc mở scope; lỗi thật phải sửa trong phạm vi và chạy regression, không giữ lỗi để bảo vệ lời hứa “không quay lại”.

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
| SV13 | Preview URL canonical origin scheme/host/port exact, approved path prefix và credential/query/fragment policy; no proxy/fetch. localhost/127.0.0.1 không interchangeable silently |
| SV14 | Ready/bind/mode/click results session+document+HMR+revision+nonce/command khớp. Delayed/duplicate/wrong direction messages không tăng quyền hoặc commit selection |
| SV15 | Manual actor từ authenticated principal; expected version CAS; client mutation id bound principal+request_hash; same id different request -> RECORD_CONFLICT |
| SV16 | capability/edge/node/rule kind strings là registry extensibility keys, không quyền nhận giá trị tùy ý: registry closed cho enabled provider. Unknown -> UNSUPPORTED_CAPABILITY, không generic parse |
| SV17 | Instance UUIDs ephemeral, rects finite CSS pixels và bounded; DOM text/props/values/keys/secrets không ở wire. Source read không thành arbitrary path read |
| SV18 | observed flow requires imported sanitized artifact_id và verified/unbound source status; static mode không accept trace như compiler fact. No query success→commit inference |
| SV19 | Budget tính sau envelope/warnings/cursor; no Unicode/JSON/code-range truncation. Constraints enum không được tự bổ sung business rule; explicit doc/record source cho constraint khác |
| SV21 | Evidence origin/kind matrix: static_analyzer dùng syntax/type/text/unresolved; build_instrumentation dùng build_anchored/unresolved; runtime_observation dùng runtime_observed/unresolved; manual_approved dùng manual_declared; ai_proposal dùng proposed. Không đổi origin thành compiler proof. FileRef cho inventory/text-only, hash=null chỉ metadata, không mở source hoặc rename. |
| SV20 | Profile identity hash lấy canonical effective config với tool digests; field `profile_id` không tự nằm trong preimage. Source CaptureManifest snapshot_digest tương tự; UTF-8 length-prefixed canonical encoding S06, arrays sort theo defined order |

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
| `vector_digest` / `scope-v1` | ScopeVector bỏ vector_digest; members sort(service_id null trước string,worktree_id,profile_id,generation_id); unavailable_members sort(service_id null trước,worktree_id null trước,status); catalog/store incarnations và group_revision giữ đầy đủ |
| `target_ref_digest` / `graph-ref-v1` | Toàn GraphRef typed đúng schema; không display label/layout/instance; used cho mapping keyset |
| `fragment item_id` / `source-fragment-v1` | object `{parent_ref,range}` như CT-05, SourceRef parent đầy đủ và byte range fragment |
| `resolver_digest` / `resolver-v1` | object `{implementation_artifact_hash,semantic_version,options_digest}`; artifact SHA256 thật và options theo closed adapter schema, không Git branch name |
| `runtime_config_digest` / `runtime-config-v1` | effective RuntimeConfig sau merge defaults và resolve max_parse_workers=0; runtime resource config KHÔNG tự trở thành semantic profile |
| `permission_digest` / `preview-permission-v1` | PreviewPermissionProfile đầy đủ version/attestation/expiry; source_worktree_ids/allowed_dev_destinations sort unique; timestamp ở đây là nội dung quyền, không thời điểm hash |
| `request_hash` / `request-v1` | object `{operation,principal_id,request}` sau defaults/validation; bỏ pagination controls chỉ khi so initial request với continuation; mapping/rename query fingerprint có target/scope/constraints/resolver |
| `record payload_hash` / `record-payload-v1` | payload typed theo record_kind, raw text không trim; không tự thay tác giả/thời gian khi replay |
| `result_digest` / `rename-result-v1` | object `{scope,initial_request,provider_digest,file_preconditions,edits,conflicts}`; edits sort(path,start,end,new_text); duplicate identical edits dedup trước digest, overlaps mâu thuẫn reject; conflicts order deterministic |
| `subgraph_digest` / `subgraph-v1` | object `{scope,nodes,edges}` với nodes sort(node_id), edges sort(edge_id); labels sanitized deterministic; không camera/layout/hover/time |

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

S00→S22→S50 role/mode→S41→task S19/S47→owner sections trong bảng trên khi liên quan. Giữ thứ tự T001→T002→T003→T105→T106→T107→T108 và 108 task; T029 thêm prerequisite T025 vì dùng pagination/result primitives. Không implementation feature trước seal đúng scope. Đặc tả/tài liệu proof không phải bằng chứng task DONE; product progress vẫn0/108.

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
