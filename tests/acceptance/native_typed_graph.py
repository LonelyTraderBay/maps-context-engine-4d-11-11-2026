"""Check C006 and C007 through the packaged index command on authored Go source."""
from contextlib import closing
import hashlib
import json
import os
import random
import re
import sqlite3


SOURCE = {
    "go.mod": "module example.invalid/typedfixture\n\ngo 1.27\n",
    "calls.go": """package fixture
import (
    "example.invalid/typedfixture/lib"
    "reflect"
)
type Runner interface { Run() int }
type One struct{}
func (One) Run() int { return 1 }
type Two struct{}
func (Two) Run() int { return 2 }
func Use(r Runner, callback func() int) int {
    return lib.Direct() + r.Run() + callback()
}
func Invoke(value any) {
    reflect.ValueOf(value).MethodByName("Run").Call(nil)
}
""",
    "lib/direct.go": "package lib\nfunc Direct() int { return 3 }\n",
    "notes.sql": "-- independentsearchmarker\nSELECT 1;\n",
    "ui.ts": "export const marker = 'independentsearchmarker';\n",
    "settings.json": '{"marker":"independentsearchmarker"}\n',
    "bindings.go": """package fixture
func Shadow(parameter int) (result int) {
    outside := parameter
    if outside > 0 {
        inside := 1
        result = outside + inside
    }
    return
}
type Box[T any] struct { Value T }
type Wrap[T any] struct { Box[T] }
func Read(w Wrap[int]) int { return w.Value }
""",
}


def prepare(target):
    for name, body in SOURCE.items():
        path = target / name
        path.parent.mkdir(exist_ok=True)
        path.write_bytes(body.encode())


def prepare_profiles(target):
    files = {
        "go.mod": "module example.invalid/profilefixture\n\ngo 1.27\n",
        "main.go": "package profile\nfunc Dispatch() int { return selected() }\n",
        "selected_linux_amd64.go": "package profile\nfunc selected() int { return 1 }\n",
        "selected_linux_arm64.go": "package profile\nfunc selected() int { return 2 }\n",
        "selected_windows_amd64.go": "//go:build preview\n\npackage profile\nfunc selected() int { return 3 }\n",
    }
    for name, data in files.items():
        (target / name).write_bytes(data.encode())


def check_profiles(root, command, worktree, first_profile, profile):
    path = root / "profiles/runtime/home/.local/state/mce4d/worktrees" / worktree / "index.sqlite"
    cases = [
        ({}, "selected_linux_amd64.go"),
        ({"goarch": "arm64"}, "selected_linux_arm64.go"),
        ({"goos": "windows", "build_tags": ["preview"]}, "selected_windows_amd64.go"),
        ({"cgo_enabled": True}, None),
    ]
    observations = {}
    for revision, (override, expected) in enumerate(cases):
        profile_id = first_profile
        if override:
            set_result = command("/mce4d", ["profile", "set", "--worktree-id", worktree,
                                 "--input-json", "-"], payload={"worktree_id": worktree,
                                  "profile_name": "typed", "expected_revision": revision,
                                  "profile": {**profile, **override}})
            assert set_result.returncode == 0
            profile_id = json.loads(set_result.stdout)["data"]["profile_id"]
            result = command("/mce4d", ["index", "--worktree-id", worktree, "--profile-id", profile_id])
            assert result.returncode == 0, result.stderr
            if expected is None:
                assert "syntax partial" in result.stderr
        wal = path.with_name("index.sqlite-wal")
        assert not wal.exists() or wal.stat().st_size == 0
        with closing(sqlite3.connect(path.as_uri() + "?mode=ro&immutable=1", uri=True)) as db:
            actual = db.execute("""
                SELECT t.path,r.evidence_kind FROM active_generations a JOIN relations r USING(generation_id)
                  JOIN symbols s ON s.generation_id=r.generation_id AND s.symbol_id=r.source_symbol_id
                  JOIN symbols t ON t.generation_id=r.generation_id AND t.symbol_id=r.target_symbol_id
                 WHERE a.profile_id=? AND r.kind='CALLS_DIRECT' AND s.name='Dispatch'
                """, (profile_id,)).fetchall()
            assert actual == ([(expected, "type_resolved")] if expected else []), (override, actual)
            assert profile_id not in observations
            observations[profile_id] = {"override": override, "expected_callee": expected}
            live_ids = {r[0] for r in db.execute("SELECT profile_id FROM active_generations")}
            assert set(observations) <= live_ids
    print(json.dumps({"case": "C009_AUTHORED_BUILD_PROFILES", "profiles": observations}), flush=True)


def exercise(root, command, worktree, profile):
    target = root / "target"
    path = root / "profiles/runtime/home/.local/state/mce4d/worktrees" / worktree / "index.sqlite"
    wal = path.with_name("index.sqlite-wal")
    assert not wal.exists() or wal.stat().st_size == 0
    with closing(sqlite3.connect(path.as_uri() + "?mode=ro&immutable=1", uri=True)) as db:
        head = db.execute("""
            SELECT a.generation_id, g.state FROM active_generations a
              JOIN generations g USING (generation_id) WHERE a.profile_id=?
            """, (profile,)).fetchone()
        assert head is not None
        rows = db.execute("""
            SELECT r.kind,r.evidence_kind,s.name,t.name,t.path,r.start_byte,r.end_byte
            FROM relations r JOIN symbols s
              ON s.generation_id=r.generation_id AND s.symbol_id=r.source_symbol_id
            LEFT JOIN symbols t
              ON t.generation_id=r.generation_id AND t.symbol_id=r.target_symbol_id
            WHERE r.generation_id=? AND r.site_path='calls.go'
            """, (head[0],)).fetchall()
        source_id = db.execute("SELECT symbol_id FROM symbols WHERE generation_id=? AND name='Use'", (head[0],)).fetchone()[0]
        adjacency = db.execute("""
            EXPLAIN QUERY PLAN SELECT relation_id FROM relations
             WHERE generation_id=? AND source_symbol_id=? AND kind='CALLS_DIRECT'
            """, (head[0], source_id)).fetchall()
    body = SOURCE["calls.go"].encode()
    direct = [row for row in rows if body[row[-2]:row[-1]] == b"lib.Direct"]
    dispatch = [row for row in rows if body[row[-2]:row[-1]] == b"r.Run"]
    callback = [row for row in rows if body[row[-2]:row[-1]] == b"callback"]
    reflection = [row for row in rows if row[2] == "Invoke"]
    reflective_call = b'reflect.ValueOf(value).MethodByName("Run").Call'
    direct_match = [r for r in direct if r[:5] ==
                    ("CALLS_DIRECT", "type_resolved", "Use", "Direct", "lib/direct.go")]
    interface_match = [r for r in dispatch if r[:5] ==
                       ("CALLS_INTERFACE", "type_resolved", "Use", "Run", "calls.go")]
    candidates = [r for r in dispatch if r[:5] ==
                  ("MAY_DISPATCH_TO", "type_possible", "Run", "Run", "calls.go")]
    verified = {
        "C006": len(direct_match) == 1 and any(r[0] == "UNRESOLVED_CALL" for r in callback)
        and not any(r[0] == "CALLS_DIRECT" for r in callback),
        "C007": len(interface_match) == 1 and len(candidates) == 2
        and not any(r[0] == "CALLS_DIRECT" for r in dispatch),
        "C008": head[1] == "READY_PARTIAL"
        and any(r[:5] == ("UNRESOLVED_CALL", "unresolved", "Invoke", None, None)
                and body[r[-2]:r[-1]] == reflective_call for r in reflection)
        and not any(r[0] == "CALLS_DIRECT" and r[3] == "Run" for r in reflection),
    }
    status = command("/mce4d", ["--worktree-id", worktree, "--profile-id", profile,
                                "query", "status", "--input-json", "-"], payload={})
    assert status.returncode == 0 and json.loads(status.stdout)["data"]["index_state"] == "published"
    assert all((target / name).read_bytes() == source.encode() for name, source in SOURCE.items())
    print(json.dumps({"case": "C006_C007_C008_PACKAGED_GRAPH", "provider_status": head[1],
                      "source_hash": hashlib.sha256(body).hexdigest(),
                      "direct": direct, "interface": dispatch, "callback": callback, "reflection": reflection,
                      "adjacency_plan": adjacency, "verified": verified}), flush=True)
    assert all(verified.values()), "packaged indexing omitted required typed relations"


def check_text_query(root, command, worktree, profile):
    prefix = ["--worktree-id", worktree, "--profile-id", profile, "query"]
    result = command("/mce4d", prefix + ["locate", "--input-json", "-"], payload={
        "mode": "text", "query": "independentsearchmarker", "limit": 10,
    })
    response = json.loads(result.stdout)
    candidates = (response["data"] or {}).get("candidates", [])
    paths = ("notes.sql", "settings.json", "ui.ts")
    expected = {path: hashlib.sha256(SOURCE[path].encode()).hexdigest() for path in paths}
    actual = {c["ref"]["path"]: c["ref"]["content_hash"] for c in candidates
              if c["ref"]["kind"] == "file" and not c["evidence_refs"]}
    verified = result.returncode == 0 and len(candidates) == len(expected) and actual == expected
    state = root / "profiles/runtime/home/.local/state/mce4d/worktrees" / worktree / "index.sqlite"
    wal = state.with_name("index.sqlite-wal")
    assert not wal.exists() or wal.stat().st_size == 0
    with closing(sqlite3.connect(state.as_uri() + "?mode=ro&immutable=1", uri=True)) as db:
        full_text = db.execute("""
            EXPLAIN QUERY PLAN SELECT rowid FROM search_fts
             WHERE search_fts MATCH 'independentsearchmarker'
            """).fetchall()
        docs = db.execute("""
            SELECT d.path FROM search_fts JOIN search_documents d ON d.rowid=search_fts.rowid
             WHERE search_fts MATCH 'independentsearchmarker'
             ORDER BY d.path
            """).fetchall()
        assert db.execute("SELECT count(*) FROM relations WHERE site_path IN (?,?,?)", paths).fetchone() == (0,)
    verified = verified and docs == [(path,) for path in paths] and any("VIRTUAL TABLE INDEX" in row[-1] for row in full_text)
    print(json.dumps({"case": "C042_C045_PACKAGED_TEXT_QUERY", "status": response["status"],
                      "hits": len(candidates), "fts_plan": full_text, "verified": verified}), flush=True)
    assert verified, "packaged text query disagrees with the captured SQL, TypeScript or config files"


def check_rename_bindings(root, command, worktree, profile):
    prefix = ["--worktree-id", worktree, "--profile-id", profile, "query", "rename-preview", "--input-json", "-"]
    body = SOURCE["bindings.go"].encode()
    content_hash = hashlib.sha256(body).hexdigest()

    def preview(old_name, new_name):
        occurrences = {m.span() for m in re.finditer(rb"\b" + old_name + rb"\b", body)}
        response = command("/mce4d", prefix, payload={
            "selector": {"kind": "source_position", "path": "bindings.go", "content_hash": content_hash,
                         "byte_offset": min(occurrences)[0]},
            "new_name": new_name, "preserve_contracts": True, "max_output_bytes": 65536,
        })
        assert (root / "target/bindings.go").read_bytes() == body
        return response.returncode, json.loads(response.stdout), occurrences

    for old, new in ((b"outside", "other"), (b"Value", "Count")):
        exit_code, response, expected = preview(old, new)
        assert exit_code == 0 and response["status"] == "ok", response["error"]
        data = response["data"]
        assert data["preview_complete"] and data["applied"] is False and data["source_mutation_count"] == 0
        edits = data["edits"]
        assert len(edits) == len(expected)
        actual = set()
        for edit in edits:
            ref = edit["source_ref"]
            assert ref["path"] == "bindings.go" and ref["content_hash"] == content_hash and edit["new_text"] == new
            actual.add((ref["range"]["start_byte"], ref["range"]["end_byte"]))
        assert actual == expected
    exit_code, conflict, _ = preview(b"outside", "inside")
    assert exit_code != 0 and conflict["status"] == "error" and conflict["data"] is None, conflict
    assert conflict["error"]["code"] not in ("TIMEOUT", "CANCELLED", "UNSUPPORTED_CAPABILITY")
    print(json.dumps({"case": "C003_C004_RENAME_BINDINGS", "scoped_rename_ranges": 3,
                      "promoted_field_ranges": 2, "conflict_error": conflict["error"]["code"],
                      "source_hash": content_hash}), flush=True)


def check_unproved_toolchain(root, command, worktree, profile_id, profile):
    override = "a" * 64
    assert profile["toolchain_digest"] != override
    changed = command("/mce4d", ["profile", "set", "--worktree-id", worktree, "--input-json", "-"], payload={
        "worktree_id": worktree, "profile_name": "typed", "expected_revision": 1,
        "profile": {**profile, "toolchain_digest": override},
    })
    assert changed.returncode == 0
    fallback = json.loads(changed.stdout)["data"]["profile_id"]
    assert fallback != profile_id
    indexed = command("/mce4d", ["index", "--worktree-id", worktree, "--profile-id", fallback])
    assert indexed.returncode == 0 and "syntax partial" in indexed.stderr
    state = root / "profiles/runtime/home/.local/state/mce4d/worktrees" / worktree / "index.sqlite"
    wal = state.with_name("index.sqlite-wal")
    assert not wal.exists() or wal.stat().st_size == 0
    with closing(sqlite3.connect(state.as_uri() + "?mode=ro&immutable=1", uri=True)) as db:
        state, facts = db.execute("""
            SELECT g.state,(SELECT count(*) FROM relations r WHERE r.generation_id=g.generation_id
                              AND r.evidence_kind IN ('type_resolved','type_possible'))
             FROM active_generations a JOIN generations g USING (generation_id) WHERE a.profile_id=?
            """, (fallback,)).fetchone()
    assert state == "READY_PARTIAL" and facts == 0
    print(json.dumps({"case": "C012_UNVERIFIED_TOOLCHAIN_SYNTAX_FALLBACK", "typed_facts": facts}), flush=True)


def check_dependency_updates(root, command, worktree, profile):
    target = root / "target"
    index = root / "profiles/runtime/home/.local/state/mce4d/worktrees" / worktree / "index.sqlite"
    argv = ["index", "--worktree-id", worktree, "--profile-id", profile]

    def observed():
        wal = index.with_name("index.sqlite-wal")
        assert not wal.exists() or wal.stat().st_size == 0
        with closing(sqlite3.connect(index.as_uri() + "?mode=ro&immutable=1", uri=True)) as db:
            generation, status = db.execute("""
                SELECT g.generation_id,g.state FROM active_generations a JOIN generations g USING(generation_id)
                 WHERE a.profile_id=?
                """, (profile,)).fetchone()
            files = db.execute("SELECT path,content_hash,status FROM files WHERE generation_id=? ORDER BY path",
                               (generation,)).fetchall()
            symbols = db.execute("SELECT path,name,qualified_name,start_byte,end_byte FROM symbols WHERE generation_id=? ORDER BY 1,2,3,4,5",
                                 (generation,)).fetchall()
            calls = db.execute("""
                SELECT r.kind,s.name,t.name,r.evidence_kind,r.site_path,r.start_byte,r.end_byte
                  FROM relations r JOIN symbols s ON r.source_symbol_id=s.symbol_id AND r.generation_id=s.generation_id
                  LEFT JOIN symbols t ON r.target_symbol_id=t.symbol_id AND r.generation_id=t.generation_id
                 WHERE r.generation_id=? AND r.kind IN ('CALLS_DIRECT','CALLS_INTERFACE','MAY_DISPATCH_TO','UNRESOLVED_CALL')
                 ORDER BY 1,2,3,4,5,6,7
                """, (generation,)).fetchall()
        return generation, (status, files, symbols, calls)

    base, (_, _, _, original_calls) = observed()
    assert any(r[:4] == ("CALLS_INTERFACE", "Use", "Run", "type_resolved") for r in original_calls)
    updates = {
        "go.mod": SOURCE["go.mod"] + "\nreplace example.invalid/unproved => ./lib\n",
        "go.work": "go 1.27\n\nuse .\n",
        "calls.go": SOURCE["calls.go"].replace("Run()", "Fetch()"),
    }
    for name, text in updates.items():
        path = target / name
        path.write_bytes(text.encode())
        if name == "go.work":
            os.chown(path, 1001, 1001)
            path.chmod(0o644)
    update = command("/mce4d", argv, timeout=120)
    assert update.returncode == 0, update.stderr
    newer, facts = observed()
    status, files, symbols, calls = facts
    for name, text in updates.items():
        assert (name, hashlib.sha256(text.encode()).hexdigest(), "indexed") in files
    assert newer != base and all(row[1] != "Run" for row in symbols)
    assert all(row[2] != "Run" for row in calls)
    typed = [row for row in calls if row[0] == "CALLS_INTERFACE"]
    if typed:
        assert len(typed) == 1 and typed[0][:4] == ("CALLS_INTERFACE", "Use", "Fetch", "type_resolved")
    else:
        assert status == "READY_PARTIAL" and "syntax partial" in update.stderr
    updates["go.work"] += "\n// fixture workspace metadata changed\n"
    (target / "go.work").write_bytes(updates["go.work"].encode())
    workspace_update = command("/mce4d", argv, timeout=120)
    assert workspace_update.returncode == 0, workspace_update.stderr
    workspace_generation, workspace_facts = observed()
    assert workspace_generation not in (base, newer)
    assert ("go.work", hashlib.sha256(updates["go.work"].encode()).hexdigest(), "indexed") in workspace_facts[1]
    assert workspace_facts[2:] == facts[2:]
    rebuilt = command("/mce4d", argv + ["--full"], timeout=120)
    assert rebuilt.returncode == 0, rebuilt.stderr
    full, comparison = observed()
    assert full not in (base, newer, workspace_generation) and comparison == workspace_facts
    assert all((target / name).read_bytes() == text.encode() for name, text in updates.items())
    print(json.dumps({"case": "C017_DEPENDENCY_INTERFACE_REBUILD", "generations": [base, newer, workspace_generation, full],
                      "updated": list(updates), "interface_calls": len(typed), "coverage": status}), flush=True)


def check_mutations(root, command, worktree, profile_id):
    """Compare default updates and forced rebuilds against authored call sites."""
    target = root / "target"
    state = root / "profiles/runtime/home/.local/state/mce4d/worktrees" / worktree / "index.sqlite"
    wal = state.with_name("index.sqlite-wal")
    args = ["/mce4d", ["index", "--worktree-id", worktree, "--profile-id", profile_id]]
    library = target / "lib/direct.go"
    call_source = target / "calls.go"
    seed = 34018
    values = random.Random(seed).sample(range(10, 100), 4)

    def snapshot():
        assert not wal.exists() or wal.stat().st_size == 0
        with closing(sqlite3.connect(state.as_uri() + "?mode=ro&immutable=1", uri=True)) as db:
            generation = db.execute("SELECT generation_id FROM active_generations WHERE profile_id=?",
                                    (profile_id,)).fetchone()[0]
            files = db.execute("""
                SELECT path,content_hash,status FROM files WHERE generation_id=? ORDER BY path
                """, (generation,)).fetchall()
            symbols = db.execute("""
                SELECT path,name,qualified_name,kind,start_byte,end_byte FROM symbols
                 WHERE generation_id=? ORDER BY path,name,qualified_name,kind,start_byte,end_byte
                """, (generation,)).fetchall()
            relations = db.execute("""
                SELECT r.site_path,r.start_byte,r.end_byte,r.kind,r.evidence_kind,
                       s.path,s.qualified_name,s.start_byte,t.path,t.qualified_name,t.start_byte
                  FROM relations r JOIN symbols s
                    ON s.generation_id=r.generation_id AND s.symbol_id=r.source_symbol_id
                  LEFT JOIN symbols t
                    ON t.generation_id=r.generation_id AND t.symbol_id=r.target_symbol_id
                 WHERE r.generation_id=? ORDER BY 1,2,3,4,5,6,7,8,9,10,11
                """, (generation,)).fetchall()
        return generation, files, symbols, relations

    def capture():
        return {str(path.relative_to(target)): hashlib.sha256(path.read_bytes()).hexdigest()
                for path in target.rglob("*") if path.is_file() and ".git" not in path.relative_to(target).parts}

    old, *_ = snapshot()
    reports = []
    for number, value in enumerate(values, 1):
        library.write_text(f"package lib\nfunc Alternate() int {{ return {value} }}\n", encoding="ascii")
        call_source.write_text(SOURCE["calls.go"].replace("lib.Direct()", "lib.Alternate()"), encoding="ascii")
        if number == 2:
            added = target / "extra.go"
            added.write_bytes(
                b'package fixture\nimport "example.invalid/typedfixture/lib"\nfunc Extra() int { return lib.Alternate() }\n')
            os.chown(added, 1001, 1001)
            added.chmod(0o644)
        elif number == 3:
            (target / "extra.go").rename(target / "relocated.go")
        elif number == 4:
            (target / "relocated.go").unlink()
        expected = ["calls.go"] + {2: ["extra.go"], 3: ["relocated.go"]}.get(number, [])
        digest = capture()
        updated = command(*args, timeout=120)
        assert updated.returncode == 0, updated.stderr
        update_generation, files, symbols, relations = snapshot()
        rebuilt = command(args[0], args[1] + ["--full"], timeout=120)
        assert rebuilt.returncode == 0, rebuilt.stderr
        full_generation, *full_facts = snapshot()
        assert old != update_generation != full_generation and old != full_generation
        assert full_facts == [files, symbols, relations]
        assert dict((path, content_hash) for path, content_hash, status in files if status == "indexed") == digest
        observed = []
        for site, start, end, kind, evidence, _, _, _, destination, target_name, _ in relations:
            if kind != "CALLS_DIRECT":
                continue
            assert evidence == "type_resolved" and destination == "lib/direct.go" and target_name.endswith(".Alternate")
            assert (target / site).read_bytes()[start:end] == b"lib.Alternate"
            observed.append(site)
        assert sorted(observed) == sorted(expected)
        assert capture() == digest
        reports.append({"step": number, "call_sites": observed, "relations": len(relations),
                        "files": len(files), "updated_generation": update_generation, "full_generation": full_generation})
        old = full_generation
    print(json.dumps({"case": "C014_C018_NATIVE_MUTATION_REBUILD", "seed": seed, "steps": reports}), flush=True)
