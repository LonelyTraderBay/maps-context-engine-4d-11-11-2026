# Protected Linux profile bootstrap

This offline administrator tool creates a **new** protected profile and verifies
its real R1 catalog against a separate append-only head. It does not install a
service, create OS accounts, edit sudoers/Codex configuration, migrate an old
profile, or start the runtime CLI/MCP owners.

Prerequisites, in order:

1. Use a locally verified Linux/ext4 environment for `create` and
   `worktree-create`. Choose separate non-root numeric engine and client UIDs
   and a non-root engine GID. No account is created.
2. Explicitly approve the target root directories. They must already exist and
   be disjoint from the administrator-controlled profile parent on the verified
   filesystem. Provisioning examines directory identities, not source contents.
3. Build with the repository's pinned offline Go toolchain and `CGO_ENABLED=0`:
   `go build -o state_profile ./tools/state_profile`.
4. Review the binary and place it in an administrator-owned, non-writable-by-user
   path. The tool verifies its executable and full ancestor chain before spawning
   a worker. Prepare a separate root-owned profile parent; do not change an
   existing user's home or state permissions to satisfy the checks.

Example syntax (replace every example path/ID with explicitly approved values):

```text
state_profile create --root /var/lib/mce4d-profiles/new-profile --engine-uid 1001 --engine-gid 1001 --client-uid 1000 --target-root /home/client/approved-project
state_profile check --root /var/lib/mce4d-profiles/new-profile
state_profile worktree-create --root /var/lib/mce4d-profiles/new-profile --worktree-id <registered-worktree-uuid>
```

These commands run as administrator. Only fixed internal workers run under the
engine UID, with supplementary groups cleared and `no_new_privs` applied to all
Go threads. There is no generic command execution option. The profile's default
state path is `<profile-root>/home/.local/state/mce4d`; the manifest and control
metadata are outside it. The engine account is trusted under S14; this tool does
not authenticate a remote peer or protect against an administrator restoring
an older whole-filesystem snapshot.

`READY` means the new catalog, its incarnation and authenticated head passed
bootstrap and a separate read-only verification process. It is not G3 acceptance
or a claim that index/records/CLI integration is ready. Verification does not
repair or discard WAL; unverified recovery state is refused.

An existing destination is refused. A failure/crash can leave an incomplete
profile; it is retained, never automatically removed, resumed or reset. Preserve
its artifacts for explicit recovery review. The JSON output omits keys and
target paths. Exit 0 means verified success, 2 invalid arguments, 3 blocked or
incomplete, and 5 worker/storage failure. Process success is not a power-loss
test; directory/file sync ordering still relies on the verified filesystem.

`worktree-create` creates only a new owned slot, with the frozen R1 index and
records schemas, separate keys/incarnations/heads and root-owned READY metadata.
It does not register a target, grant a query binding, index source, or adopt an
existing directory. Existing and partially created slots are preserved and
refused. Runtime native owners use shared lifecycle leases, transaction receipt
gates and a separate read-only capture process; target authorization remains a
required caller boundary. Pending recovery can abort only an independently
verified unchanged acknowledged image. A changed/unproven outcome stays blocked.

To use the runtime binary as the installed worker, supply
`--worker-executable /administrator/approved/bin/mce4d` to `create`. Both binaries
must be verified, root-owned and under root-managed ancestors. The selected
path and SHA-256 are sealed in installation metadata; replacing or upgrading it
requires an explicit installation procedure. No target-controlled binary is used.

Launch the runtime under the provisioned engine account with `HOME` pointing to
the profile's `home`, or `MCE4D_HOME` pointing to its exact state directory.
`mce4d repo add --root <approved-root>` registers a local Git checkout
without executing Git/Go or indexing; its initial profile is syntax-only and
fingerprints the embedded parser/runtime and installed binary. Provision the
returned worktree ID with `worktree-create`. CLI query selection is explicit:
`mce4d --worktree-id <id> --profile-id <hash> query status --input-json <file>`.
MCP keeps its existing binding flags. Selection never grants additional roots.

For a linked worktree, include both the linked checkout and the checkout that
owns the common `.git` directory among the installation's `--target-root`
values. Register each checkout with its own `repo add --root` and provision
each returned worktree ID. Checkouts sharing the common directory use one repo
ID and keep separate worktree IDs and indexes.

The Linux development runtime wires syntax indexing, foreground watch, retained
CLI packs and checked-workspace validation. See the specification's S21 for the
separate packaged CLI/MCP and Codex fixture evidence.
The default syntax profile does not enable typed loading or rename preview.
An explicitly approved `typed_when_available` profile and Go/gopls policy can
enable the native rename owner described in `docs/RENAME_PREVIEW.md`; unavailable
closure remains an explicit failure. External dependency roots are not enabled
by these commands. Index facts remain explicitly partial.

Before indexing, use `mce4d policy set --input-json <file>` with the registered
`worktree_id`, current `expected_epoch` and an explicitly reviewed Git executable in
`allowed_executables` (`path` and `sha256`). Git's complete path must be controlled
by the administrator; finding Git on PATH does not grant permission. Metadata
configuration comes from `/etc/gitconfig` and this profile's home. Source tools,
hooks and project validation commands are never executed.

```text
mce4d index --worktree-id <id> --profile-id <hash>
mce4d watch --worktree-id <id> --profile-id <hash>
mce4d --worktree-id <id> --profile-id <hash> query locate --input-json locate.json
mce4d --worktree-id <id> --profile-id <hash> query context --input-json context.json
mce4d --worktree-id <id> --profile-id <hash> query expand --input-json expand.json
mce4d --worktree-id <id> --profile-id <hash> query validate --input-json validate.json
```

Index/watch write progress to stderr and leave stdout empty. Watch stays in the
foreground until cancelled. Use a located candidate's exact `ref` in a context
selector: `{"kind":"ref","ref":<candidate.ref>,"service_id":null}`. A context
request also supplies `task_kind` and `freshness_mode`; overview/plan uses an
explicit workspace selector and `view:"overview"`. Only policy-approved trusted
rule paths become constraints; other documents do not acquire approval.

For continuation, copy `data.pack_token` and `pagination.next_cursor` into
`{"pack_token":"<token>","next_cursor":"<cursor>"}`. Repeat until no cursor
remains and inspect pending/coverage flags. Validate with
`{"pack_token":"<token>","verify":"workspace"}` immediately before an edit.
A successful observation is not a lock on the target or permission to apply.
Source changes, expiry, missing result storage or changed authority refuse use
of the old result. Query timeout defaults to ten seconds; a small output budget
can fail explicitly. Configurable timeout/output limits remain bounded.

The temporary pack store shares the total worktree-cache growth gate with index
writes. It does not store a live freshness verdict, silently rebuild a missing
result or automatically delete pending/unproven files. These files remain
charged until an explicitly authorized cleanup can prove ownership and expiry.

Codex configuration is a separate manual step. Preserve existing servers and
review an additional server entry; this tool never edits the user's config. A
Windows Codex host can launch the installed Linux executable through WSL, using
the provisioned engine account and the exact profile home, for example:

```toml
[mcp_servers.mce4d]
command = "C:\\Windows\\System32\\wsl.exe"
args = ["-d", "Ubuntu", "-u", "<engine-account>", "--", "/usr/bin/env", "HOME=<profile-root>/home", "<administrator-owned-mce4d>", "mcp", "--worktree-id", "<id>", "--profile-id", "<hash>"]
```

This is a template, not an installation command or a permission grant. The
specification records the actual host version, isolated fixture and observed
tool roundtrip separately; Windows-native runtime support is a different gate.

## Windows profile preparation

On a fixed local NTFS volume, use the packaged `state-profile.exe` to prepare
a fresh directory in an existing, controller-owned parent with a protected,
write-restricted DACL. Supply explicit existing NTFS target roots disjoint from
that parent and distinct Windows SIDs for the intended engine and client.

```powershell
state-profile.exe prepare --root 'C:\MCE4D-Control\new-profile' --engine-sid '<engine-sid>' --client-sid '<client-sid>' --target-root 'C:\ApprovedProjects\example'
state-profile.exe verify-roles --root 'C:\MCE4D-Control\new-profile' --engine-pid '<engine-pid>' --client-pid '<client-pid>'
state-profile.exe bootstrap-catalog --root 'C:\MCE4D-Control\new-profile'
state-profile.exe bootstrap-wal --root 'C:\MCE4D-Control\new-profile' --engine-pid '<engine-pid>' --client-pid '<client-pid>'
state-profile.exe check --root 'C:\MCE4D-Control\new-profile'
$env:MCE4D_HOME = 'C:\MCE4D-Control\new-profile'
mce4d.exe store recover --target catalog --report-only
```

Preparation records the proposed role SIDs and reports `CREATING`.
Optional `prepare --git-ignore <absolute-file>` captures the controller's
chosen global Git ignore rules into the protected profile. Indexing uses that
captured file for this profile.
`verify-roles` reads two live process tokens and compares their role
identities with the protected manifest. AppContainer roles must belong to the
controller's Windows account and match the configured package SIDs. The
result attests the observed process identities for this invocation.
`bootstrap-catalog` builds a fresh catalog using the frozen schema and seals
its closed image and verification record under the controller's `control`
directory. It refuses an existing seed or maintenance journal. `check`
reopens the sealed image read-only, verifies its digest, schema and
incarnation, and refuses incomplete or damaged images. Incomplete artifacts
are retained for review.

`bootstrap-wal` checks both live roles and uses the verified seed to create a
fresh checkpointed SQLite WAL catalog under `control/catalog-wal`. It stores
the initial receipt with the catalog, then publishes a controller-owned head
and key outside that directory. `check` verifies the exact SQLite image,
receipt and head together. A missing head, modified receipt or unexpected
sidecar requires review.

The controller can explicitly set `MCE4D_WINDOWS_STAGED_ENGINE_PID` and
`MCE4D_WINDOWS_STAGED_CLIENT_PID` to the two live role process IDs and set
`MCE4D_HOME` to this profile. In that mode, `mce4d.exe repo add --root
<approved-target>` registers a local Git checkout in the staged catalog. For a
linked worktree, pass both the linked checkout and the owner of the common
`.git` directory to `prepare` as `--target-root` values.
It rechecks the role tokens while holding the catalog lease. It appends a
prepare receipt to the receipt log and independent head before SQL, then
appends the observed outcome after closing SQLite. A failed mutation can be
recorded as aborted only when the authenticated image is unchanged. A pending
or mismatched receipt blocks further registration.

For an AppContainer profile, setting `MCE4D_WINDOWS_ROLE_IPC=1` selects
controller-launched IPC for `mce4d.exe repo add`. The controller validates the
packaged image and launches each role with its own inherited stdio handles.
Each exchange checks the held process token and a fresh challenge bound to the
profile and operation. The controller grants executable read and execute
access inside the per-image role directories.

`state-profile.exe worktree-create --root '<profile-root>' --worktree-id
'<registered-worktree-id>'` uses the same role IPC and validates the
registration against the catalog head. It initializes fresh index and records
WAL stores, with separate incarnations, receipts and controller-owned heads.
The completion marker is published after both SQLite images verify. A failed
or duplicate provisioning attempt preserves the existing slot for review.

`state-profile.exe check-pools --root '<profile-root>' --worktree-id
'<registered-worktree-id>' --profile-id '<registered-profile-id>'` checks
all three read-only pools in one IPC session. It verifies the requested
profile, the worktree and Git metadata identities, and the independent WAL
head for each store. The catalog and worktree leases stay held until their
pools close. Pending maintenance and mismatched images or heads prevent a
successful report. The report preserves the installation's `CREATING` stage.

The controller can inspect the same scope through the packaged CLI or MCP
entry point. Set `MCE4D_HOME` and opt in with `MCE4D_WINDOWS_ROLE_IPC=1`.
Use the worktree and profile identifiers returned by `repo add`.

```powershell
$env:MCE4D_WINDOWS_ROLE_IPC = '1'
'{}' | mce4d.exe --worktree-id '<worktree-id>' --profile-id '<profile-id>' query status --input-json -
mce4d.exe mcp --worktree-id '<worktree-id>' --profile-id '<profile-id>'
```

An MCP host passes the same arguments and environment to its server child.
The session keeps both shared leases until the pools close. It checks its
role peers, scope and WAL heads again at each authorization boundary. A
worktree with freshly provisioned stores reports a missing index until its
first successful publication.

```powershell
mce4d.exe index --worktree-id '<worktree-id>' --profile-id '<profile-id>'
mce4d.exe watch --worktree-id '<worktree-id>' --profile-id '<profile-id>'
```

The staged Windows index captures tracked and eligible untracked files in a
supported local SHA-1 Git repository and publishes Go syntax and text search
as `READY_PARTIAL`. It applies folder `.gitignore` files, `.git/info/exclude`
and the optional captured global rules. The scanner enforces configured path
policy and file limits. Git configuration and ignore grammar outside the
implemented local subset are refused. Query status accepts
`{"verify":"workspace"}` to compare a fresh capture with the indexed snapshot.

`watch` repeats the same verified capture in the foreground on the configured
poll interval. Each pass finishes its worktree transaction before the next
poll. Readers can keep using the last acknowledged snapshot. Restoring a
store rotates its incarnation and preserves the protected cursor key. The
previously issued cursors for that scope expire.

In controller IPC mode, `profile set` and `policy set` update a registered
Windows worktree through the catalog WAL owner. They use the existing
expected revision and epoch contracts. Policy roots must be approved by the
installation. A `typed_when_available` profile bound to the current provider
can publish syntax fallback while typed analysis requires its own proof.

### Windows backup and recovery

In controller IPC mode, `mce4d.exe store backup` accepts one catalog or one
registered worktree. Choose a fresh directory immediately below
`control\backups` for a catalog or `control\worktrees\<worktree-id>\backups`
for a worktree. The controller holds the selected scope's exclusive lifecycle
and writer leases. SQLite creates standalone backup images and the verified
`backup.json` is published last. Catalog and records images are limited to
1 MiB each. A worktree index image can reach 256 MiB. The effective write
limit also depends on the configured worktree cache budget. With the default
2 GiB cache budget and the required reserve, the effective cap is 128 MiB.
Use a build that supports the larger image to read or restore it.
The backup contains store data and revisions. Cursor and WAL head
keys stay with the protected installation.

`mce4d.exe store restore --manifest <absolute-backup.json> --confirm
--wait-ms <milliseconds>` applies a backup to its original installation and
scope. It verifies the backup, stages new incarnations and receipts, and
retains the previous files in a private `restore-<operation-id>` directory.
If interrupted, `store recover --target catalog --report-only` or
`store recover --target worktree --worktree-id <id> --report-only` gives the
recorded progress. Use `--apply --confirm --wait-ms <milliseconds>` in place
of `--report-only` to resume a verified journal plan. The result is available
only after all files and WAL heads verify. An active reader lease can prevent
maintenance from starting.

An installation starts at `CREATING` with `trusted_head: false`. After
registering and provisioning a worktree, publish its first index in controller
IPC mode. Activate the installation with the exact worktree UUID and profile
digest used for indexing.

```powershell
$env:MCE4D_WINDOWS_ROLE_IPC = '1'
state-profile.exe activate --root 'C:\MCE4D-Control\new-profile' --worktree-id '<worktree-id>' --profile-id '<profile-digest>'
state-profile.exe check --root 'C:\MCE4D-Control\new-profile'
```

Activation checks the authenticated catalog, index and records owners, all
three independent WAL heads, the published generation and the protected
signing key. Subsequent opens check the live scope and heads. An interrupted
restore still requires recovery before that scope can serve requests.

Normal index uses a shared worktree lifecycle and one exclusive writer per
worktree. Each reader transaction opens an
immutable image verified against the current independent head. While the
writer runs, readers use the previous checkpoint authenticated by PREPARE.
The short index control lock covers image capture and receipt publication.
Maintenance restore still requires the exclusive worktree lifecycle.

Retrying index can append an abort for a pending PREPARE when the quiesced
image still matches. If a complete authenticated COMMIT or ABORT survived
before the independent head append, retry advances the head after verifying
the exact checkpoint and the operation's PREPARE.

Use `store recover --target worktree --worktree-id <id> --apply --confirm
--wait-ms <milliseconds>` to restore the last independently acknowledged
index when interrupted work changed its files. Recovery requires the
authenticated PREPARE and its matching protected checkpoint. A short
unacknowledged receipt tail can be retained with the interrupted files.
Recovery stages a new index incarnation, keeps records intact, and resumes
through PREPARED, REPLACING and VERIFYING. It retires up to 512 MiB per
owner-bound sidecar under the original protected directory. Missing authority,
an incomplete independent head or unsafe files still block mutation.

For Windows rename preview, pass the SID of a dedicated provisioned LSP
AppContainer in `prepare --lsp-sid <package-sid>`. The engine, client and LSP
roles have distinct SIDs. Approve the exact paths and SHA-256 values for
`go.exe` and `gopls.exe` through `policy set`. Use the pinned Windows
compatibility build from `tools/gopls_windows/build.py`, an
administrator-owned Go distribution, and a `typed_when_available` profile
with the verified toolchain digest. For the CLI or MCP process that loads Go,
set `PATH` to the distribution's `bin` directory and `PATHEXT` to `.EXE`.
The native adapter accepts a single local readonly Go module with verified
standard library imports. External dependency roots, cgo and test variants
remain outside the supported typed scope.

Windows Go indexing also uses this scratch role with a
`typed_when_available` profile. For indexing, approve `go.exe` and set the
index process's `PATH` to the verified distribution's `bin` and `PATHEXT`
to `.EXE`. The analyzer uses a protected capture and local standard library.
It needs no gopls server. Explorer and context packs read the published
call facts, including exact direct calls and separately labeled interface
and unresolved calls. An unavailable typed closure leaves syntax coverage.

Use the CLI `query rename-preview` and `query validate`, or the MCP tools
`mce4d_rename_preview` and `mce4d_validate_pack`, with the same worktree and
profile binding. The managed Job receives a protected read-only copy of the
verified sources and disposable tool-output directories inside the LSP
package's local cache. Scratch, staged gopls and retained rename results
share the worktree cache budget. The owner verifies edits against the captured
bytes and stores the complete result before issuing its token. Worktree
restore changes the store incarnation and makes earlier tokens unusable.
