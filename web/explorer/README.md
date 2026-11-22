# Explorer workspace

The Explorer AppShell is built from `index.html` and `src/main.tsx`. The browser contracts are exported from `src/index.ts`. `src/explorer.css` owns the dark theme, pane sizing, focus style and compact breakpoint. The graph and inspector share a scope-bound selection reducer in `src/shell/selection.ts`.

## Build and test

Use Node 24.22.3 and npm 11.19.1. From the project root:

```sh
npm ci --ignore-scripts --no-audit --no-fund --engine-strict
npm run lint -w @mce4d/explorer
npm run typecheck -w @mce4d/explorer
npm test -w @mce4d/explorer
npm run build -w @mce4d/explorer
```

The build places the app in `dist/`. In an owned packaging checkout, stage those files under `cmd/mce4d/explorer_dist/` and build `./cmd/mce4d` with the `mce4d_explorer_assets` tag. Launch the installed CLI against a registered worktree/profile and enter the one-time code in Explorer. The app reads the bound index over the same-origin API.

For the T081 contract smoke test, set `MCE4D_CHROME_PATH` and `MCE4D_CHROME_SHA256` to the verified Chrome executable and run `npm run test:e2e -w @mce4d/explorer`. The T083 packaged AppShell test in `tests/acceptance/explorer_shell.mjs` runs with the protected native fixture broker and exercises a real SQLite index. Use an owned workspace and the documented S51 budgets.

## Contract sources

- `contracts/mce4d.v1.schema.json` is the S43 Draft 2020-12 schema. `contracts/operations.v1.json` binds HTTP operations to their request and response data definitions.
- `contracts/explorer-wire.v1.json` gives the operation labels, byte caps and D015 bridge directions. The Go parity test checks this table against its registry and the bridge direction validator.
- `tools/generate-contracts.mjs` generates the reachable TypeScript types and standalone validators from these inputs. Generation runs before validation tests and builds. The browser validates with precompiled code under a CSP without `unsafe-eval`.
- The HTTP adapter owns the transport byte limit, authentication, request context and store checks. The browser consumes plain validated DTOs. Browser bridge peers exchange S43 JSON text and check source window, exact origin, direction and active session before applying messages. A digest or decoded DTO alone confers no permission.

The right pane shows the selected source range and its static evidence in GU0. The preview and flow capabilities follow their assigned gates. Run `npm run test:production-strip -w @mce4d/explorer` to compare production client and SSR bundles and verify the synthetic source remains unchanged.
