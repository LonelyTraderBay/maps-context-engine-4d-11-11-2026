# Protected Linux profile bootstrap

This offline administrator tool creates a **new** protected profile and verifies
its real R1 catalog against a separate append-only head. It does not install a
service, create OS accounts, edit sudoers/Codex configuration, migrate an old
profile, or start the runtime CLI/MCP owners.

Prerequisites, in order:

1. Use a locally verified Linux/ext4 environment. Windows native and arbitrary
   filesystem/mount layouts are unsupported. Choose separate non-root numeric
   engine and client UIDs and a non-root engine GID. No account is created.
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
`mce4d repo add --root <approved-root>` registers a normal local Git worktree
without executing Git/Go or indexing; its initial profile is syntax-only and
fingerprints the embedded parser/runtime and installed binary. Provision the
returned worktree ID with `worktree-create`. CLI query selection is explicit:
`mce4d --worktree-id <id> --profile-id <hash> query status --input-json <file>`.
MCP keeps its existing binding flags. Selection never grants additional roots.

The Linux development runtime wires syntax indexing, foreground watch, retained
CLI packs and checked-workspace validation. T028 and G3 still require the evidence
recorded in the specification; a successful bootstrap does not pass either gate.
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
