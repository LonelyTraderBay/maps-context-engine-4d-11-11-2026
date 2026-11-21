# Vite Inspector

This opt-in Vite plugin generates source anchors for JSX and TSX in the developer's own dev server. The indexer reads the exported JSON and matches it against captured source bytes. It does not load the project's Vite configuration or start its dev server.

Call `viteInspector({ enabled: true, root, worktreeID, profileID, providerDigest, buildConfigDigest, inputManifestDigest })` from an approved Vite configuration. Supply the canonical Vite root, the registered worktree and profile IDs, and the SHA-256 pins for the producer, build configuration and declared build inputs. The plugin instruments loaded source modules during development or test serve. Production builds and SSR transforms keep the original source.

`plugin.api.exportManifest(directory)` writes the current loaded-module manifest to a canonical, owner-only Linux directory outside the target tree. It returns the committed file path. The published name contains the immutable map ID and the JSON carries UTF-8 source offsets. Publication opens one exclusive temporary file per map, flushes it and renames it atomically. Repeated exports of identical bytes return the existing path. If a producer stops before publishing, the temporary file remains outside the import namespace.

Use a Linux engine built with `mce4d_ui_maps` and a worktree provisioned by that build. The owner can import an approved local JSON file from a private directory outside the target.

```sh
mce4d explorer-map import --worktree-id <id> --manifest <absolute-local-json>
```

The CLI returns `imported_unbound` when no published generation matches the captured files. Import the same file after indexing the matching source to create the binding. A successful binding returns `bound` and its immutable ID. Import alone grants no runtime preview permission.

The owner may run a foreground watcher for this worktree and profile.

```sh
mce4d explorer-map watch --worktree-id <id> --profile-id <id> --directory <absolute-private-map-directory>
```

The watcher accepts committed map IDs from the private directory. It coalesces a burst to the latest committed notification and retries a busy worktree writer once. Watch events report queue age, writer wait and binding time. Stop the watcher with Ctrl+C. To bind an artifact that arrived before its matching index generation, run the explicit import again after indexing.
