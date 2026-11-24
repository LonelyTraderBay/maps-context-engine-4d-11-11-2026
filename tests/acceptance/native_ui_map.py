"""Owned TSX fixture tested through the protected Linux Explorer-map CLI."""

import fcntl
import hashlib
import json
import os
from pathlib import PurePosixPath
import sqlite3
import subprocess
import time
from contextlib import closing
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


def exercise_gc(root, command, worktree, profile, maps, helpers, *, fault_profile=None):
    """Check native V4 GC using detached product backups to inspect SQLite."""
    binding = ["--worktree-id", worktree, "--profile-id", profile]
    state = root / "profiles/runtime/home/.local/state/mce4d"
    owned_worktree = state / "worktrees" / worktree
    gc = ["store", "gc", *binding]
    apply = gc + ["--apply", "--confirm"]

    def cli(args, payload=None, exit_code=0):
        process = command("/mce4d", args, payload=payload, timeout=80)
        assert process.returncode == exit_code, (args, process.returncode, process.stderr, process.stdout)
        return json.loads(process.stdout) if process.stdout else None

    def status(selected=binding):
        return cli(selected + ["query", "status", "--input-json", "-"], {})["data"]["scopes"][0]

    def imported():
        artifact, build_map_id = maps[0]
        result = cli(["explorer-map", "import", "--worktree-id", worktree, "--manifest", artifact])
        assert result["build_map_id"] == build_map_id and result["status"] == "bound"
        return result["binding_id"]

    def backup(name):
        output = owned_worktree / "backups" / name
        cli(["store", "backup", "--target", "worktree", "--worktree-id", worktree,
             "--output", "/" + output.relative_to(root).as_posix()])
        manifest = json.loads((output / "backup.json").read_bytes())
        stores = {item["kind"]: item for item in manifest["stores"]}
        assert set(stores) == {"index", "records"}
        assert stores["index"]["schema_version"] == stores["index"]["min_reader_version"] == 4
        for kind, item in stores.items():
            payload = output / (kind + ".sqlite")
            assert payload.stat().st_size == item["byte_length"]
            assert hashlib.sha256(payload.read_bytes()).hexdigest() == item["raw_sha256"]
        return output, stores

    def inspect_index(folder, live):
        path = folder / "index.sqlite"
        with closing(sqlite3.connect(path.as_uri() + "?mode=ro&immutable=1", uri=True)) as db:
            assert db.execute("PRAGMA foreign_key_check").fetchone() is None
            assert db.execute("SELECT schema_version FROM schema_meta").fetchone() == (4,)
            assert db.execute("SELECT generation_id FROM active_generations WHERE profile_id=?", (profile,)).fetchone() == (live,)
            bindings = db.execute("SELECT binding_id, source_generation_id FROM ui_build_bindings ORDER BY binding_id").fetchall()
            links = db.execute("SELECT * FROM ui_anchor_links WHERE binding_id=? ORDER BY link_id", (active_binding,)).fetchall()
            build = db.execute("SELECT * FROM ui_build_maps WHERE build_map_id=?", (maps[0][1],)).fetchall()
            anchors = db.execute("SELECT * FROM ui_source_anchors WHERE build_map_id=? ORDER BY anchor_id", (maps[0][1],)).fetchall()
            active_files = db.execute(
                "SELECT f.path, f.content_hash, b.bytes FROM files f JOIN content_blobs b ON b.content_hash=f.content_hash "
                "WHERE f.generation_id=? ORDER BY f.path", (live,)).fetchall()
            assert links and build and anchors and active_files
            assert any(path == "entry.tsx" for path, _, _ in active_files)
            assert all(hashlib.sha256(data).hexdigest() == digest for _, digest, data in active_files)
            generation_ids = {row[0] for row in db.execute("SELECT generation_id FROM generations")}
            return bindings, links, build, anchors, active_files, generation_ids

    old_member = status()
    old_generation = old_member["generation_id"]
    old_binding = imported()
    source = (root / "target/a.go").read_bytes()
    start = source.index(b"Inner")
    ref = {"worktree_id": worktree, "profile_id": profile, "generation_id": old_generation,
           "path": "a.go", "content_hash": hashlib.sha256(source).hexdigest()}
    proposal = {"source_ref": dict(ref, range={"start_byte": start, "end_byte": start + 5}),
                "destination_ref": dict(ref, kind="file"), "rationale": "Local GC record fixture.",
                "client_mutation_id": str(uuid.uuid4())}
    author = ["__records-fixture", worktree, profile]
    proposed = cli(author + ["propose"], proposal)
    approved = cli(author + ["review"], {"record_id": proposed["record_id"], "expected_version": 1,
                  "action": "approve", "reason": "Reviewed against the synthetic snapshot.",
                  "client_mutation_id": str(uuid.uuid4())})
    selector = {"record_id": approved["record_id"]}
    read_record = [*binding, "records", "read", "--input-json", "-"]
    assert approved["version"] == 2 and approved["effective_status"] == "approved"
    retained = cli(read_record, selector)["data"]["record"]
    assert retained["version"] == 2 and retained["provenance_status"] == "retained_snapshot"

    for _ in range(3):
        cli(["index", *binding, "--full"])
    live = status()["generation_id"]
    active_binding = imported()
    assert live != old_generation and active_binding != old_binding
    peer_binding = None
    if fault_profile is not None:
        peer = cli(["profile", "set", "--worktree-id", worktree, "--input-json", "-"],
                   {"worktree_id": worktree, "profile_name": "c150-independent", "expected_revision": 0,
                    "profile": dict(fault_profile, analysis_mode="syntax", build_tags=["c150_fixture"])})
        peer_id = peer["data"]["profile_id"]
        assert peer_id != profile
        peer_binding = ["--worktree-id", worktree, "--profile-id", peer_id]
        cli(["index", *peer_binding, "--full"])
        peer_before = status(peer_binding)
        assert peer_before["generation_id"] not in (old_generation, live)
    before_folder, before_stores = backup("gc-v4-before")
    before_index = inspect_index(before_folder, live)
    assert set(before_index[0]) == {(old_binding, old_generation), (active_binding, live)}
    before_source = helpers.tree(root / "target")
    before_config = helpers.tree(root / "host-config")

    # Wait on the same lock a cooperating native writer uses, on this fixture only.
    with (owned_worktree / "writer.lock").open("rb") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        started = time.monotonic()
        blocked = command("/mce4d", apply, timeout=14)
        blocked_ms = (time.monotonic() - started) * 1000
        assert blocked.returncode == 4 and "GC cancelled or limited" in blocked.stderr and not blocked.stdout
        assert 250 <= blocked_ms < 14000, blocked_ms
        assert cli(read_record, selector)["data"]["record"] == retained

    started = time.monotonic()
    plan = cli(gc + ["--report-only"])
    report_ms = (time.monotonic() - started) * 1000
    assert plan["dry_run"] and not plan["more"] and plan["keep_generations"] == 2
    candidates = {item["generation_id"]: item for item in plan["generations"]}
    assert len(candidates) == 2 and old_generation in candidates and live not in candidates
    assert all(not item["applied"] and item["rows"]["generations"] == 1 for item in candidates.values())
    assert candidates[old_generation]["rows"]["ui_build_bindings"] == 1
    report_folder, report_stores = backup("gc-v4-report")
    assert inspect_index(report_folder, live) == before_index
    for kind in before_stores:
        assert before_stores[kind]["source_logical_sha256"] == report_stores[kind]["source_logical_sha256"]

    fault_checks = None
    if peer_binding is not None:
        assert peer_before["generation_id"] not in candidates
        witness = root / "result/gc-rollback-fixture"
        with witness.open("x", encoding="utf-8") as file:
            file.write(old_generation)
        witness.chmod(0o444)
        process = command("/mce4d", ["__gc-rollback-fixture", worktree, profile, old_generation], timeout=105)
        assert process.returncode == 0, (process.stdout, process.stderr)
        counter = [line for line in process.stdout.splitlines() if line.startswith("C150_PROTECTED_GC_CHECKS=")]
        assert len(counter) == 1, process.stdout
        fault_checks = int(counter[0].split("=")[1])
        assert fault_checks >= 19
        fault_folder, fault_stores = backup("gc-v4-faults")
        assert inspect_index(fault_folder, live) == before_index
        for kind in before_stores:
            assert before_stores[kind]["source_logical_sha256"] == fault_stores[kind]["source_logical_sha256"]
        assert cli(gc + ["--report-only"]) == plan
        assert status(peer_binding) == peer_before
        assert cli(read_record, selector)["data"]["record"] == retained

    outcome = cli(apply)
    assert not outcome["dry_run"]
    assert [item["generation_id"] for item in outcome["generations"]] == list(candidates)
    for item in outcome["generations"]:
        expected = candidates[item["generation_id"]]
        assert item["applied"] and item["rows"] == expected["rows"] and item["blob_bytes"] == expected["blob_bytes"]
    after_folder, after_stores = backup("gc-v4-after")
    after_index = inspect_index(after_folder, live)
    assert after_index[0] == [(active_binding, live)]
    assert after_index[1:5] == before_index[1:5]
    assert after_index[5] == before_index[5] - candidates.keys()
    assert before_stores["records"]["source_incarnation"] == after_stores["records"]["source_incarnation"]
    assert before_stores["records"]["source_logical_sha256"] == after_stores["records"]["source_logical_sha256"]
    expired = cli(read_record, selector, exit_code=6)["data"]
    summary = expired["record"]
    assert summary["record_id"] == retained["record_id"] and summary["version"] == 2
    assert summary["declared_status"] == "approved" and summary["effective_status"] == "needs_review"
    assert summary["provenance_status"] == "snapshot_expired" and not summary["source_refs"] and expired["ui_mapping"] is None
    assert imported() == active_binding
    assert not cli(gc + ["--report-only"])["generations"]
    assert helpers.tree(root / "target") == before_source and helpers.tree(root / "host-config") == before_config
    assert not (root / "result/repo-executed").exists()
    if fault_checks is not None:
        assert status(peer_binding) == peer_before
        repeated = cli(apply)
        assert not repeated["dry_run"] and repeated["generations"] == []
        print(json.dumps({"case": "C150_NATIVE_V4_PROTECTED_GC_ROLLBACK", "status": "PASS",
                          "authority_faults": fault_checks, "context_cancellations": fault_checks,
                          "index_and_records_logical_sha256_preserved_after_faults": True,
                          "independent_profile_preserved": True, "retry_exact_and_idempotent": True,
                          "target_executions": 0}), flush=True)
    print(json.dumps({"case": "C149_NATIVE_V4_PROTECTED_GC", "status": "PASS",
                      "schema_version": 4, "retired_generations": len(candidates),
                      "active_anchor_links_preserved": len(after_index[1]),
                      "records_logical_sha256_unchanged": True, "record_status_after_gc": "needs_review",
                      "writer_configured_ms": 300, "busy_exit": blocked.returncode,
                      "blocked_cli_ms": round(blocked_ms, 3), "uncontended_report_cli_ms": round(report_ms, 3),
                      "target_executions": 0}), flush=True)
