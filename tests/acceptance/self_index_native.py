"""C052/C054 native onboarding and pinned source checks in the owned broker."""
from contextlib import closing
import hashlib
import json
import os
from pathlib import Path
import re
import sqlite3


DESIGN = b"# CheckoutService\n\nImplement CheckoutService and its order validation in a later task.\n"
SOURCE_BASE = "5a7893a112f8651ba18141450f92c06abc623155"
SOURCE_MANIFEST = "e7e054fde601b902d2e7947acac25fd2c073a91c952fa586382a9e107b5874a4"
SELF_PATHS = (
    "go.mod", "go.sum", "cmd/mce4d/main.go", "cmd/mce4d/native_linux.go",
    "cmd/mce4d/native_unsupported.go", "internal/store/sqlite/restore.go",
    "internal/store/sqlite/restore_worker_linux.go",
)


def sha(body):
    return hashlib.sha256(body).hexdigest()


def prepare(root, suite):
    target = root / "target"
    if suite == "spec-only":
        (target / "design.md").write_bytes(DESIGN)
        return None
    artifact = Path(os.environ["MCE4D_SELF_SNAPSHOT"]).resolve(strict=True)
    manifest = (artifact / "manifest.sha256").read_bytes()
    assert sha(manifest) == SOURCE_MANIFEST
    expected = {}
    for line in manifest.decode().splitlines():
        digest, name = line.split("  ", 1)
        assert name not in expected
        expected[name] = digest
    files = {}
    approved_source = artifact / "source"
    for name in SELF_PATHS:
        source = approved_source / name
        assert source.resolve(strict=True).is_relative_to(approved_source)
        raw = source.read_bytes()
        assert sha(raw) == expected[name]
        result = target / name
        result.parent.mkdir(parents=True, exist_ok=True)
        result.write_bytes(raw)
        files[name] = expected[name]
    return {"source_base": SOURCE_BASE, "source_manifest": SOURCE_MANIFEST, "selected_paths": files}


def query(command, worktree, profile, action, request):
    result = command("/mce4d", ["--worktree-id", worktree, "--profile-id", profile,
                               "query", action, "--input-json", "-"], payload=request)
    return result, json.loads(result.stdout)


def database(root, worktree):
    path = root / "profiles/runtime/home/.local/state/mce4d/worktrees" / worktree / "index.sqlite"
    wal = path.with_name("index.sqlite-wal")
    assert not wal.exists() or wal.stat().st_size == 0
    return closing(sqlite3.connect(path.as_uri()+"?mode=ro&immutable=1", uri=True))


def check_spec_only(root, command, worktree, profile_id):
    target = root / "target"
    git = target / ".git"
    assert (git / "HEAD").read_text().startswith("ref: refs/heads/")
    assert not any((git / "refs/heads").iterdir()) and not (git / "packed-refs").exists()
    assert not (git / "index").exists()
    _, result = query(command, worktree, profile_id, "context", {
        "selector": {"kind": "workspace"}, "task_kind": "plan",
        "intent": "Plan the CheckoutService described in the design",
        "freshness_mode": "analysis_snapshot", "max_output_bytes": 24576,
    })
    data = result["data"]
    assert data and result["readiness"] == "needs_context"
    assert not data["target_refs"] and not data["edit_precondition_validated"]
    items = data["items"]
    meta = json.loads(next(item["content"] for item in items if item["why_included"] == "plan_summary"))
    assert meta["project_stage"] == "spec_only" and meta["implementation_evidence"] == "none"
    intended = [item for item in items if item["why_included"] == "intended_constraint"]
    assert len(intended) == 1 and intended[0]["content"].encode() == DESIGN
    assert len(intended[0]["source_refs"]) == 1
    ref = intended[0]["source_refs"][0]
    assert ref["path"] == "design.md" and ref["content_hash"] == sha(DESIGN)
    assert ref["range"] == {"start_byte": 0, "end_byte": len(DESIGN)}
    assert not any(item["why_included"].startswith("observed_") for item in items)
    with database(root, worktree) as index:
        for table in ("packages", "symbols", "relations"):
            assert index.execute(f"SELECT count(*) FROM {table}").fetchone() == (0,)
    assert (target / "design.md").read_bytes() == DESIGN
    assert not (target / ".git/index").exists()
    print(json.dumps({"case": "C052_NATIVE", "project_stage": meta["project_stage"],
                      "implementation_evidence": meta["implementation_evidence"],
                      "source_hash": sha(DESIGN), "typed_or_call_facts": 0}), flush=True)


def declaration(target, path, name, keyword):
    """Independent direct-source oracle for these known top-level declarations."""
    raw = (target / path).read_bytes()
    matches = list(re.finditer(rb"(?m)^"+keyword.encode()+rb" ("+name.encode()+rb")\b", raw))
    assert len(matches) == 1, (path, name)
    start, end = matches[0].span(1)
    return {"path": path, "content_hash": sha(raw), "range": {"start_byte": start, "end_byte": end}}


def check_self_index(root, command, worktree, profile_id, profile, provenance, approved):
    target = root / "target"
    analyzer = approved["binaries"]["mce4d"]
    upgraded = os.environ["MCE4D_SELF_OTHER_PROVIDER"]
    assert re.fullmatch(r"[0-9a-f]{64}", upgraded) and upgraded != analyzer
    declarations = (("cmd/mce4d/main.go", "main", "func"),
                    ("internal/store/sqlite/restore.go", "ProtectedRestoreSummary", "type"))
    _, status = query(command, worktree, profile_id, "status", {})
    active = status["data"]["scopes"][0]["generation_id"]
    observed = {}
    for path, name, keyword in declarations:
        expected = declaration(target, path, name, keyword)
        _, result = query(command, worktree, profile_id, "locate", {"mode": "symbol", "query": name, "limit": 10})
        matches = [row for row in result["data"]["candidates"] if row["name"] == name
                   and row["source_refs"] and row["source_refs"][0]["path"] == path
                   and row["source_refs"][0]["range"] == expected["range"]]
        assert len(matches) == 1, (name, result)
        ref = matches[0]["source_refs"][0]
        assert all(ref[key] == value for key, value in expected.items()), (ref, expected)
        assert ref["generation_id"] == active
        observed[name] = expected
    with database(root, worktree) as index:
        assert index.execute("SELECT provider_fingerprint FROM generations WHERE generation_id=?",
                             (active,)).fetchone() == (analyzer,)
        files = dict(index.execute("SELECT path, content_hash FROM files WHERE generation_id=? AND status='indexed'",
                                   (active,)))
        assert files == provenance["selected_paths"]
    changed = command("/mce4d", ["profile", "set", "--worktree-id", worktree, "--input-json", "-"], payload={
        "worktree_id": worktree, "profile_name": "typed", "expected_revision": 1,
        "profile": {**profile, "provider_digest": upgraded},
    })
    assert changed.returncode == 0, changed.stderr
    new_profile = json.loads(changed.stdout)["data"]["profile_id"]
    assert new_profile != profile_id
    _, missing = query(command, worktree, new_profile, "status", {})
    assert missing["data"]["index_state"] == "missing" and missing["data"]["scopes"] == []
    refused = command("/mce4d", ["index", "--worktree-id", worktree, "--profile-id", new_profile])
    assert refused.returncode == 3 and "capability" in refused.stderr
    fallback = {name: declaration(target, path, name, kind) for path, name, kind in declarations}
    assert fallback == observed
    assert {name: sha((target/name).read_bytes()) for name in SELF_PATHS} == provenance["selected_paths"]
    print(json.dumps({"case": "C054_NATIVE", **provenance, "analyzer_sha256": analyzer,
                      "profile_before": profile_id, "profile_after": new_profile,
                      "unserved_after_provider_change": True, "direct_source": fallback}), flush=True)
