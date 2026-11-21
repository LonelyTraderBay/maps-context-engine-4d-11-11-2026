"""Owned TSX fixture tested through the protected Linux Explorer-map CLI."""

import json
import os
from pathlib import PurePosixPath
import subprocess
import uuid

SOURCE = 'export function Page() { return <button onClick={() => 1}>Local fixture</button>; }\n'
SOURCE_MANY = ('function Button() { return <button onClick={() => 1}>Local fixture</button>; }\n'
               + 'export function Page() { return <>\n' + '<Button/>\n' * 105 + '</>; }\n')


def import_before_index(root, command, worktree, profile, implementation, source=SOURCE, *, hmr=False):
    def emit(name, source, config_pin="a" * 64):
        # Only the bundled producer runs. The target text is parsed as data.
        module = implementation / "packages/vite-inspector/producer.mjs"
        js = """
const { instrumentSource, createBuildManifest } = await import(process.argv[1]);
const [worktreeID, profileID, source, configPin] = process.argv.slice(2);
const pin = 'a'.repeat(64);
const entry = await instrumentSource('entry.tsx', source, pin);
const config = {worktreeID, profileID, providerDigest: pin, buildConfigDigest: configPin,
  inputManifestDigest: pin, mode: 'development'};
process.stdout.write(JSON.stringify(await createBuildManifest(config, new Map([['entry.tsx', entry]]))));
"""
        raw = subprocess.check_output([os.environ["MCE4D_T085_NODE"], "--input-type=module", "-e", js,
                                       str(module), worktree, profile, source, config_pin], timeout=15)
        path = PurePosixPath("/result") / name
        artifact = root / str(path).lstrip("/")
        artifact.write_bytes(raw)
        os.chown(artifact, 1000, 1000)
        artifact.chmod(0o600)
        return str(path), json.loads(raw)["build_map_id"]

    maps = [emit("build.json", source), emit("unbound.json", source + "// separate build\n")]
    if hmr:
        maps.extend([emit("hmr-intermediate.json", source, "b" * 64), emit("hmr-latest.json", source, "c" * 64)])
    path, digest = maps[0]
    args = ["explorer-map", "import", "--worktree-id", worktree, "--manifest", path]
    denied = command("/mce4d", args, uid=1001)
    assert denied.returncode == 3 and "ACCESS_DENIED" in denied.stderr and not denied.stdout
    wrong = command("/mce4d", args[:3] + [str(uuid.uuid4())] + args[4:])
    assert wrong.returncode == 2 and "INVALID_ARGUMENT" in wrong.stderr and not wrong.stdout
    pending = command("/mce4d", args)
    assert pending.returncode == 0 and json.loads(pending.stdout) == {"build_map_id": digest, "status": "imported_unbound"}
    return maps


def exercise(root, command, worktree, profile, maps):
    def cli(args, **options):
        process = command("/mce4d", args, **options)
        assert process.returncode == 0, (args, process.stderr, process.stdout)
        return process

    def imported(item):
        path, digest = item
        output = json.loads(cli(["explorer-map", "import", "--worktree-id", worktree, "--manifest", path]).stdout)
        assert output["build_map_id"] == digest
        return output

    first = imported(maps[0])
    assert first["status"] == "bound" and len(first["binding_id"]) == 64
    assert imported(maps[0]) == first
    assert imported(maps[1])["status"] == "imported_unbound"
    binding = ["--worktree-id", worktree, "--profile-id", profile]
    backup = f"/profiles/runtime/home/.local/state/mce4d/worktrees/{worktree}/backups/ui-map-fixture"
    cli(["store", "backup", "--target", "worktree", "--worktree-id", worktree, "--output", backup])
    backed = json.loads((root / backup.lstrip("/") / "backup.json").read_bytes())
    assert backed["stores"][0]["schema_version"] == backed["stores"][0]["min_reader_version"] == 4
    for _ in range(3):
        cli(["index", *binding, "--full"])
    gc = json.loads(cli(["store", "gc", *binding, "--apply", "--confirm"]).stdout)
    assert any(item["rows"]["ui_build_bindings"] == 1 and item["applied"] for item in gc["generations"])
    replacement = imported(maps[0])
    assert replacement["status"] == "bound" and replacement["binding_id"] != first["binding_id"]
    restore = ["store", "restore", "--manifest", backup + "/backup.json", "--confirm", "--wait-ms", "3000"]
    assert command("/mce4d", restore).returncode == 3
    restored = json.loads(cli(restore, uid=0).stdout)
    assert restored["outcome"] == "restored"
    assert imported(maps[0]) == first
    print(json.dumps({"case": "T085_NATIVE_UI_MAP", "status": "PASS", "unbound_import": True,
                      "immutable_replay": True, "gc_then_restore_v4": True, "target_executions": 0}), flush=True)
