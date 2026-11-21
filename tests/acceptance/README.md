# Local native defensive acceptance

For T035, the same fresh native broker accepts the closed environment choice
`MCE4D_ACCEPTANCE_SUITE=gc` (default `security`). `native_gc.py` keeps an MCP
reader open, creates four synthetic generations via the real index command,
checks the default read-only plan and confirmed apply, then checks expired
pack/rename-result validation and unchanged target/catalog/records/config/key.
Only the fixture controller edits source to create revisions. There is no
network scan or product write/execute grant for the target.

The other closed choice, `MCE4D_ACCEPTANCE_SUITE=backup`, uses
`native_backup.py`: actual catalog/worktree backup commands, independent SQLite
reads, byte/hash/manifest checks, bounded refusal while an MCP pool is alive,
and unchanged existing output on retry. Recovery reports are exercised both
with a live MCP pool and with root-seeded incomplete metadata. Bound CLI/MCP
startup must refuse the latter without changing installed state. Seeded phases
are admission/reporting proof, not actual restore or crash-recovery evidence.

`MCE4D_ACCEPTANCE_SUITE=restore` backs up and restores an installed synthetic
catalog and worktree through the packaged CLI. It checks bounded lock refusal
with an open MCP pool, new store incarnations, continued queries and writes,
and rejection of previous pack, continuation and rename-result tokens in CLI
and MCP.

`MCE4D_ACCEPTANCE_SUITE=scale` uses `tools/bench/scale_fixture.py` with
`MCE4D_SCALE_COHORT=S16|S|S800|M|L` (16, 128, 800, 10,000 and 100,000 files).
The CLI runs against newly provisioned state with the same 1,025 authored
Go callers in each workload. The fixture records cold, no-change and changed
indexing, query output and strict freshness, allocated state disk and sampled
process-tree RSS. It checks coverage via an independent read-only SQLite query
and checks rename edit ranges against the authored source. For published S
fixtures it verifies quota refusal preserves the active generation and a
second worktree can index while the first worktree's writer is busy. A
bounded refusal before publication is recorded as an unsupported workload.
The test profile explicitly allows 64 KiB pages and a 200 ms writer wait.
The checked-workspace query retains its default 10 second deadline.
`MCE4D_SCALE_COHORT=SQuota` isolates the multi-worktree and quota checks on
the same 16-file workload. It restores the original quota and verifies the
published generation remains readable.
`MCE4D_SCALE_COHORT=C029` uses the 16-file workload plus an approved rule
excerpt and adds a cycle between the anchor and the first caller. It checks
all 1,025 resolved call sites against source bytes across CLI invocations,
including source fragments, exact payload budgets and cursor termination.
The first two pages use 8 KiB; subsequent pages use the configured 128 KiB cap.
Every query retains the default 10 second deadline.
It then lowers the graph node limit and checks that the error reports
unknown remaining counts.
`MCE4D_SCALE_COHORT=C029Extended` runs the same oracle with a 20 second
configured query deadline. It records this deadline separately from C029.
RSS sampling counts shared memory in each process and can miss short peaks.
Single synthetic runs provide neither p95 nor real-repository measurements.

The WSL2 filesystem check runs
`TestStateDomainPlatformFilesystemFixture` from the Linux SQLite test binary.
Supply existing disposable directories in `MCE4D_PLATFORM_EXT4_FIXTURE`
and `MCE4D_PLATFORM_DRVFS_FIXTURE`, and run the binary as root in the
guarded local fixture. The test checks the actual filesystem type and the
protected-state admission result without creating databases. Run the native
restore and crash brokers separately for the supported ext4 case.

`MCE4D_ACCEPTANCE_SUITE=spec-only` starts with a Git repository with no commit
or index and a single approved design document. The packaged plan must report
`spec_only` and `implementation_evidence=none`. An independent SQLite read
verifies that no package, symbol or relation facts were added.

`MCE4D_ACCEPTANCE_SUITE=self-index` reads an explicit set of seven engine
files from the verified T035 source snapshot supplied by
`MCE4D_SELF_SNAPSHOT`. It checks the snapshot manifest and each selected
file hash before copying. The broker pins the analyzer executable separately
and compares locate results against independently read declaration ranges.
`MCE4D_SELF_OTHER_PROVIDER` supplies the digest of the other reviewed binary.
Changing the profile fingerprint for the unchanged source must leave its
index missing and refuse indexing with the previous executable. Direct
source lookup remains available. This case covers the selected declarations
and the profile boundary. It makes no whole-repository or typed-index claim.

`MCE4D_ACCEPTANCE_SUITE=typed-graph` checks a real cross-package direct call,
an interface call with two candidates, and an unresolved function-value call.
An independent read of the published SQLite rows verifies their evidence
kind and callable byte range. The suite also checks an FTS5 lookup against
authored SQL text and confirms it produces a file reference with no semantic
relationship. A second profile with a mismatched toolchain fingerprint must
publish syntax partial with zero typed facts.

`native_store_broker.py` can additionally run the private maintenance-owner
fixture with `MCE4D_STORE_MAINTENANCE=1`: separate reader process exclusion,
another worktree's actual writer progress, authenticated pair/catalog backup,
future-schema refusal and raw-capture refusal while a source SQL pool is alive.
It also exercises strict pre-SQL restore input checks, unserved staging with
new identities, and all four incomplete journal phases for both scopes.
With `MCE4D_STORE_RESTORE=1`, the broker also kills the real restore controller
at 39 points across the catalog and worktree flows. It checks side effects,
recovery, preserved old files and subsequent native owner admission. These
cases verify Linux process-crash recovery. Windows and physical power loss
require separate tests.

`native_security.py` runs only inside a fresh root-owned synthetic Linux
fixture supplied by a reviewed broker. It never discovers a project, account,
network range or target to test. `testdata/malicious/security.json` contains fake
inputs; secret markers and sensitive filenames are created only inside that
fixture, outside the checkout.

The broker must install exact hashed development binaries, provision the real
protected state/profile and separate engine/client UIDs, approve the complete
Go toolchain and gopls, bind the fake worktree, index it, and supply a real
process launcher. All six MCP operations go through the packaged CLI/SDK and
native storage owners. Canary comparisons include Git/config/AGENTS bytes,
permissions, ownership and symlink identity.

The local HTTP trap has a positive control and observes the poisoned proxy and
telemetry endpoint. The namespace has only loopback and no external route.
Zero trap contacts proves only that endpoint was not contacted: this is not a
system-wide packet or syscall trace. Keep that limitation in C047 evidence.
Native Windows junction/ACL behavior and broader platform acceptance belong to
T036/T037. The suite does not certify all secrets, all attacks or production
security.

`native_broker.py` is the versioned provisioning/workload script. Invoke it
only through an owned output/process/disk guard, in a new
`unshare --mount --pid --fork --net` namespace as root. It verifies PID 1,
loopback-only networking, a nonexistent `rootfs`, the explicitly approved
binary digests, and the reviewed gopls digest before provisioning fake state.
The broker does not install tools, change personal config, or select a real
repository. Set these explicit absolute paths in the guard's child environment:

| Variable | Meaning |
| --- | --- |
| `MCE4D_ACCEPTANCE_RUN` | Fresh private output directory; guard has created its `home` directory |
| `MCE4D_ACCEPTANCE_BUILD` | Development build containing `mce4d`, `state-profile`, `go-toolchain` |
| `MCE4D_ACCEPTANCE_APPROVALS` | Reviewed JSON `{"binaries":{"mce4d":"sha256","state-profile":"sha256","go-toolchain":"sha256"}}` for those exact builds |
| `MCE4D_ACCEPTANCE_GO_ROOT` | Existing pinned Go distribution to copy into the fake root |
| `MCE4D_ACCEPTANCE_GOPLS` | Existing reviewed gopls binary matching the broker's pin |

The guard must retain commands, manifests, failures and cleanup status. The
S21 evidence index records the actual guarded invocation and hashes; raw logs
stay outside Git. `expected.json` is an authored semantic oracle, not output
copied from an engine golden update. Build/toolchain changes require new
approval digests and a fresh fixture. No older profile is silently adopted.

`native_store_broker.py` reuses the protected SQLite fixture roles for real
worktree provisioning, WAL/owner roundtrips, pending recovery, shared-cache
quota refusal, captured-worker stop and packaged provisioner behavior. Run it
as root in the same private PID/mount/network setup through `tools/run_check`;
its fake root is under the guard job's private HOME parent. Supply
`MCE4D_ACCEPTANCE_BUILD` and `MCE4D_ACCEPTANCE_APPROVALS` with pinned
`sqlite.test` and `state-profile` digests. The broker establishes explicit fake
UID/modes, clears only its synthetic append flags, and leaves artifacts for
inspection. No live user database/profile is opened or migrated.

### Windows native fixtures

Build the Windows amd64 integration test executable with the pinned Go
toolchain. Run `TestWindowsAppContainerSQLiteWAL` on local NTFS. It creates two
temporary AppContainer profiles with distinct package SIDs and removes them at
teardown. The controller owns the trusted fixture head. The engine creates
and recovers a real SQLite WAL. The cursor codec verifies that an offline
fixture replacement invalidates old tokens at the same numeric epoch.

With the packaged `mce4d.exe`, `state-profile.exe` and SQLite test binary
`journal.test.exe` in `MCE4D_WIN_PACKAGE_FIXTURE`, run
`TestWindowsPackagedProfileRoleVerification` for the real Windows backup and
restore commands. Set `MCE4D_WIN_RESTORE_CRASH_FIXTURE=1` to terminate the
test-only controller at PREPARED, during file replacement, and at VERIFYING
for each restore scope. The packaged recovery command resumes the journal.
Set `MCE4D_WIN_INDEX_CRASH_FIXTURE=1` to run index recovery with a retained
WAL at each injected boundary.
All profiles and repositories in this test are temporary.

`TestWindowsJobGopls` checks native gopls against a generated module and
validates its edits against the captured source bytes. Supply absolute paths
through `MCE4D_GOPLS_WIN_FIXTURE` and `MCE4D_GOPLS_WIN_GOROOT`, and approved
executable SHA-256 values through `MCE4D_GOPLS_WIN_SHA256` and
`MCE4D_GO_WIN_SHA256`. `TestWindowsAppContainerGopls` uses the same materials
to check the restricted process variant. Keep the results of the two
variants distinct.

Set `MCE4D_WIN_RENAME_PUBLIC=1` and supply the same pinned toolchain and
gopls values to include activation, public rename preview, validation, and
post-restore token expiry in the packaged test. The value `only` runs the
activation and rename workflow without the separate crash matrix.

Add the following environment values to the packaged test to exercise the
installed Codex host against the real, temporary SQLite scope.

| Environment variable | Value |
|---|---|
| `MCE4D_WIN_CODEX_RENAME_PROBE` | Absolute path of `tests/integration/platform_windows_codex.py` |
| `MCE4D_WIN_PROBE_PYTHON` | Absolute path of the local Python interpreter |
| `MCE4D_WIN_PROBE_CODEX` | Absolute path of the installed `codex.exe` |
| `MCE4D_WIN_PROBE_CODEX_SHA256` | Reviewed SHA-256 of that exact executable |
| `MCE4D_WIN_CODEX_OUTPUT` | Optional existing private directory for the host record |

The probe uses a disposable Codex home, six MCP tools and zero model turns.
It checks the personal config and fixture canary before and after. The same
runner also supports the separate MCP framing fixture from
`go test -c ./internal/mcp`. See `platform_windows_codex.py --help`.
