# Explorer loopback adapter

The foreground CLI binds a single authorized worktree/profile and gives the local operator an ephemeral IPv4 loopback origin and a one-time bootstrap code. The code belongs in the form body sent to `POST /api/explorer/v1/session/bootstrap`. It expires in 60 seconds. Use the returned bearer in the `Authorization: Bearer …` header and keep it in browser memory. Reload or expiry requires a fresh foreground session.

For personal use, append `--session-until-exit` to `mce4d explorer --worktree-id <id> --profile-id <id>`. The authenticated tab stays connected while the foreground process and source authorization remain active. When the response has no timed authorization deadline, `expires_at` is `null`. The default browser session is limited to 30 minutes. Any attached preview keeps its own expiry.

The HTTP server requires the bound Host and exact Origin. Same-origin browser GET requests may use Fetch Metadata when Origin is absent. Each POST also requires `X-MCE4D-Intent: explorer` and JSON. The adapter delivers scope and source only after rechecking the current bound store and policy. The response body uses the S43 envelope. The bearer has no cookie or URL transport.

| Operation | Behavior |
|---|---|
| `GET /api/explorer/v1/workspace` | No body. Optional `verify=none` or `verify=workspace`. |
| `POST /api/explorer/v1/graph/query` | Published Go static facts. Workspace, symbol and typed symbol/file roots. Repeat the same selection and pass the returned cursor to continue the pinned result. Edge pages include both endpoint nodes. Clients merge by stable node and edge IDs. |
| `POST /api/explorer/v1/source/read` | Exact captured bytes for the complete SourceRef and UTF-8 range. The envelope accounts for UTF-8 and JSON bytes. A range that exceeds the budget returns `RESULT_TOO_LARGE` with a range descriptor. |

The implementation opens the protected Linux catalog, index and records owner. Snapshot reads report `not_checked` for workspace freshness. Graph pages retain up to four projections with a 4 MiB per-projection cap and the configured result TTL bounded to 15 minutes. They are released with the process. The U1 preview session, permission mutation and SDK routes belong to their later gate.

A core build reports `EXPLORER_ASSETS_UNAVAILABLE` when the command is invoked without bundled assets. The asset build uses `mce4d_explorer_assets` and reads only the embedded `cmd/mce4d/explorer_dist` bundle. T082 exercises this path with a synthetic HTML asset and an actual SQLite fixture. T083 supplies the AppShell assets.
