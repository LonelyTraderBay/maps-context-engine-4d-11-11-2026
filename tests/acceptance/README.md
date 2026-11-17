# Local native defensive acceptance

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
