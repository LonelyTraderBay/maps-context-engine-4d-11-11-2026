"""Measure the packaged engine in the C053 isolated Linux broker."""
from contextlib import closing
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import signal
import sqlite3
import subprocess
import threading
import time

generator = Path(__file__).resolve().parents[2] / "tools/bench/scale_fixture.py"
spec = importlib.util.spec_from_file_location("mce_scale_fixture", generator)
fixture = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fixture)

MEMORY_LIMIT = 2 << 30
PACK_RULE = b"Synthetic project: preserve public contract names.\n"
PACK_COHORTS = ("C029", "C029Extended")


def prepare(root, cohort):
    if cohort in PACK_COHORTS:
        target = root / "target"
        authored = fixture.write_fixture(target, "S16")
        (target / "AGENTS.md").write_bytes(PACK_RULE)
        fanout = target / "fanout.go"
        before = fanout.read_bytes()
        after = before.replace(b"return 7", b"return Caller0000()", 1)
        fanout.write_bytes(after)
        return {**authored, "cohort": cohort, "files": authored["files"] + 1,
                "source_bytes": authored["source_bytes"] + len(PACK_RULE) + len(after) - len(before)}
    return fixture.write_fixture(root / "target", cohort)


def tree_rss(proc, pid, writes=None):
    seen, stack = set(), [pid]
    total = 0
    while stack:
        child = stack.pop()
        if child in seen:
            continue
        seen.add(child)
        if len(seen) > 128:
            raise ValueError("fixture process limit exceeded")
        entry = proc / str(child)
        try:
            statm = (entry / "statm").read_text().split()
            total += int(statm[1]) * os.sysconf("SC_PAGE_SIZE")
            if writes is not None:
                try:
                    started = (entry / "stat").read_text().rsplit(")", 1)[1].split()[19]
                    io = dict(line.split(": ", 1) for line in (entry / "io").read_text().splitlines())
                    key = (child, started)
                    writes[key] = max(writes.get(key, 0), int(io["write_bytes"]))
                except (FileNotFoundError, PermissionError, IndexError, KeyError):
                    pass
            for thread in (entry / "task").iterdir():
                try:
                    stack.extend(int(p) for p in (thread / "children").read_text().split())
                except FileNotFoundError:
                    pass
        except FileNotFoundError:
            pass
    return total


def measure(proc, argv, timeout, setup, env, payload):
    child = subprocess.Popen(argv, start_new_session=True, preexec_fn=setup,
                             env=env, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                             stderr=subprocess.PIPE, text=True)
    stop = threading.Event()
    stats = {"sample_interval_ms": 50, "peak_tree_rss_bytes": 0,
             "samples": 0, "memory_limit_bytes": MEMORY_LIMIT, "stop_reason": None}
    writes = {}

    def stop_child(reason):
        stats["stop_reason"] = reason
        try:
            os.killpg(child.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass

    def sample():
        while not stop.is_set():
            try:
                rss = tree_rss(proc, child.pid, writes)
            except (OSError, ValueError, IndexError):
                stop_child("sampling_failed")
                return
            stats["samples"] += 1
            stats["peak_tree_rss_bytes"] = max(stats["peak_tree_rss_bytes"], rss)
            if rss > MEMORY_LIMIT:
                stop_child("rss_limit")
                return
            stop.wait(.05)

    monitor = threading.Thread(target=sample)
    monitor.start()
    try:
        try:
            stdout, stderr = child.communicate(payload, timeout=timeout)
        except subprocess.TimeoutExpired:
            stop_child("outer_deadline")
            stdout, stderr = child.communicate(timeout=5)
    finally:
        stop.set()
        monitor.join(timeout=5)
    assert not monitor.is_alive()
    stats["sampled_write_bytes_floor"] = sum(writes.values()) if writes else None
    stats["sampled_write_processes"] = len(writes)
    return subprocess.CompletedProcess(argv, child.returncode, stdout, stderr), stats


def disk_usage(root):
    logical = blocks = files = 0
    for path in root.rglob("*"):
        if path.is_file():
            info = path.stat()
            logical += info.st_size
            blocks += info.st_blocks * 512
            files += 1
    return {"logical_bytes": logical, "allocated_bytes": blocks, "files": files}


def oracle(state, profile, generation):
    wal = state / "index.sqlite-wal"
    assert not wal.exists() or wal.stat().st_size == 0
    database = sqlite3.connect((state / "index.sqlite").as_uri() + "?mode=ro&immutable=1", uri=True)
    try:
        active = database.execute("SELECT generation_id FROM active_generations WHERE profile_id=?",
                                  (profile,)).fetchone()
        assert active == (generation,)
        coverage = dict(database.execute("SELECT status,count(*) FROM files WHERE generation_id=? GROUP BY status",
                                         (generation,)))
        sources = database.execute("""
            SELECT DISTINCT s.name
            FROM relations r JOIN symbols s
              ON s.generation_id=r.generation_id AND s.symbol_id=r.source_symbol_id
            JOIN symbols t
              ON t.generation_id=r.generation_id AND t.symbol_id=r.target_symbol_id
            WHERE r.generation_id=? AND t.name='FanoutAnchor' AND r.site_path='fanout.go'
              AND r.kind='CALLS_DIRECT' AND r.evidence_kind='type_resolved'
            """, (generation,))
        callers = {row[0] for row in sources}
        capabilities = list(database.execute("""
            SELECT capability_key,status,count(*) FROM capability_results WHERE generation_id=?
            GROUP BY capability_key,status ORDER BY capability_key,status
            """, (generation,)))
        return coverage, callers, capabilities
    finally:
        database.close()


def unpublished_oracle(state):
    wal = state / "index.sqlite-wal"
    assert not wal.exists() or wal.stat().st_size == 0
    with closing(sqlite3.connect((state / "index.sqlite").as_uri() + "?mode=ro&immutable=1", uri=True)) as database:
        active = database.execute("SELECT count(*) FROM active_generations").fetchone()[0]
        generations = database.execute("SELECT state,count(*) FROM generations GROUP BY state").fetchall()
    assert active == 0 and generations == [], (active, generations)
    return {"active": active, "generations": generations}


def exercise(root, command, worktree, profile_id, authored, profile, policy, config):
    target = root / "target"
    state = root / "profiles/runtime/home/.local/state/mce4d/worktrees" / worktree
    argv = ['index', '--worktree-id', worktree, '--profile-id', profile_id]
    bind = ['--worktree-id', worktree, '--profile-id', profile_id, 'query']
    is_pack = authored["cohort"] in PACK_COHORTS
    case = "C029_PACK" if is_pack else "C053_SCALE"
    proof = {"case": case, **authored, "runs": {}}
    before = source_digest(target)
    proof["source_sha256"] = before
    if is_pack:
        proof["query_timeout_ms"] = config.get("query_timeout_ms", 10000)

    def run(label, args, payload=None, timeout=60):
        start = time.monotonic()
        result = command('/mce4d', args, payload=payload, timeout=timeout, measure=True)
        proof["runs"][label] = {"elapsed_ms": round((time.monotonic() - start) * 1000, 3),
                                "exit": result.returncode, **result.metrics}
        return result

    def read(label, action, request, timeout=75):
        result = run(label, bind + [action, '--input-json', '-'], request, timeout=timeout)
        assert result.stdout, (label, result.returncode, result.metrics)
        data = json.loads(result.stdout)
        proof["runs"][label]["status"] = data["status"]
        proof["runs"][label]["error"] = (data.get("error") or {}).get("code")
        proof["runs"][label]["pending"] = data["pagination"]["required_pending"]
        proof["runs"][label]["serialized_bytes"] = data["budget"]["serialized_bytes"]
        return result, data

    try:
        cold = run("cold", argv + ["--full"], timeout=360)
        assert source_digest(target) == before
        proof["index_disk"] = disk_usage(state)
        if cold.returncode:
            ready, status = read("after_limit", "status", {})
            assert ready.returncode == 0
            # At the deadline a fenced SQLite transaction can refuse to attest
            # its outcome. Accept the refusal only with independently empty state.
            reconciliation = (cold.returncode == 3
                              and "index outcome requires reconciliation" in cold.stderr
                              and proof["runs"]["cold"]["elapsed_ms"] >= config["index_timeout_ms"])
            assert ((cold.returncode == 4 and 'limit reached' in cold.stderr)
                    or cold.metrics["stop_reason"] == "rss_limit" or reconciliation), cold.stderr
            assert status["data"]["index_state"] == "missing" and status["data"]["scopes"] == []
            proof["refusal_state"] = unpublished_oracle(state)
            assert not is_pack and authored["cohort"] not in ("S", "S16", "SQuota"), "the correctness workload must publish"
            proof["outcome"] = "unsupported_at_measured_cap"
            return

        _, status = read("status", "status", {})
        active = status["data"]["scopes"][0]["generation_id"]
        coverage, callers, capabilities = oracle(state, profile_id, active)
        proof["coverage"] = coverage
        proof["capabilities"] = capabilities
        proof["indexed_fanout"] = len(callers)
        assert coverage.get("indexed") == authored["files"], coverage
        expected = {f"Caller{i:04d}" for i in range(fixture.FANOUT)}
        assert callers <= expected, callers - expected
        if callers != expected:
            assert "syntax partial" in cold.stderr
        proof["indexed_fanout_complete"] = callers == expected
        if is_pack:
            assert callers == expected, (len(callers), cold.stderr)
            prove_pack_fanout(read, run, target, active, config, proof)
            assert source_digest(target) == before
            proof["outcome"] = "context_pack_fanout_completed"
            return
        if authored["cohort"] == "SQuota":
            prove_scope_and_quota(root, command, worktree, profile_id, profile, policy, config, run, read, active)
            assert source_digest(target) == before
            proof["outcome"] = "scope_and_quota_completed"
            return

        locate, result = read("locate", "locate", {"mode": "symbol", "query": "FanoutAnchor", "limit": 10})
        if locate.returncode in (0, 6) and result["data"] is not None:
            anchor = next(c for c in result["data"]["candidates"] if c["name"] == "FanoutAnchor")
            _, pack = read("bounded_fanout_pack", "context",
                           {"selector":{"kind":"ref","ref":anchor["ref"],"service_id":None},
                            "task_kind":"behavior_change","freshness_mode":"analysis_snapshot",
                            "max_output_bytes":8192})
            assert pack["budget"]["serialized_bytes"] <= 8192
            proof["runs"]["bounded_fanout_pack"]["limit_reason"] = pack["pagination"]["limit_reason"]
        else:
            assert result["error"]["code"] in ("TIMEOUT", "RESOURCE_LIMIT", "FRESHNESS_UNKNOWN")
        _, strict = read("strict_freshness", "status", {"verify":"workspace"})
        proof["runs"]["strict_freshness"]["freshness"] = strict["freshness"]["status"]
        prove_fanout(read, target / "fanout.go", active, proof)

        noop = run("no_change", argv, timeout=360)
        assert noop.returncode == 0 and f"unchanged {authored['files']} files" in noop.stderr
        assert active in noop.stderr and source_digest(target) == before
        first = target / "p0000/x000000.go"
        with first.open("a") as output:
            output.write("// synthetic update\n")
        edited = source_digest(target)
        changed = run("changed_source", argv, timeout=360)
        assert changed.returncode == 0
        _, current = read("updated_status", "status", {})
        updated = current["data"]["scopes"][0]["generation_id"]
        assert updated != active and source_digest(target) == edited
        proof["updated_source_sha256"] = edited
        disk_after = disk_usage(state)
        proof["after_update_disk"] = disk_after

        if authored["cohort"] in ("S", "S16"):
            prove_scope_and_quota(root, command, worktree, profile_id, profile, policy, config, run, read, updated)
        assert source_digest(target) == edited
        proof["outcome"] = "completed"
    finally:
        print(json.dumps(proof), flush=True)


def source_digest(root):
    h = hashlib.sha256()
    for path in sorted(root.rglob("*")):
        relative = path.relative_to(root)
        if relative.parts[0] != ".git" and path.is_file():
            h.update(str(relative).encode() + b"\0")
            h.update(hashlib.sha256(path.read_bytes()).digest())
    return h.hexdigest()


def prove_pack_fanout(read, run, target, generation, config, proof):
    files = {name: (target / name).read_bytes() for name in ("fanout.go", "AGENTS.md")}
    hashes = {name: hashlib.sha256(body).hexdigest() for name, body in files.items()}
    expected = {match.span(1) for match in re.finditer(rb"return (FanoutAnchor)\(\)", files["fanout.go"])}
    assert len(expected) == fixture.FANOUT
    _, located = read("pack_target", "locate", {"mode": "symbol", "query": "FanoutAnchor", "limit": 10})
    anchor, = [item["ref"] for item in located["data"]["candidates"] if item["name"] == "FanoutAnchor"]
    request = {
        "selector": {"kind": "ref", "ref": anchor, "service_id": None},
        "task_kind": "behavior_change", "freshness_mode": "analysis_snapshot",
        "max_output_bytes": 8192,
    }
    result, page = read("pack_000", "context", request)
    item_ids, cursors, calls, rule_ranges = set(), set(), set(), set()
    target_ranges = []
    pending = []
    cycle_edge, first_caller = None, None
    first_call = min(expected)
    scope, token = None, None
    for number in range(400):
        cap = 8192 if number < 2 else config["max_output_bytes"]
        assert result.returncode in (0, 6) and page["error"] is None, (number, page["error"], result.stderr)
        if number == 0:
            scope, token = page["scope"], page["data"]["pack_token"]
            assert len(scope["members"]) == 1 and scope["members"][0]["generation_id"] == generation
        assert page["scope"] == scope and page["data"]["pack_token"] == token
        assert page["data"]["target_refs"] == [anchor] and page["data"]["edit_precondition_validated"] is False
        assert page["readiness"] == "needs_context"
        assert page["budget"]["serialized_bytes"] == len(result.stdout.removesuffix("\n").encode())
        assert page["budget"]["serialized_bytes"] <= cap
        pagination = page["pagination"]
        assert pagination["enumeration_complete"] and pagination["returned_count"] == len(page["data"]["items"])
        required, optional = pagination["required_pending"], pagination["optional_pending"]
        assert isinstance(required, int) and isinstance(optional, int)
        assert required >= 0 and optional >= 0 and (not pending or required <= pending[-1])
        pending.append(required)
        for item in page["data"]["items"]:
            assert item["item_id"] not in item_ids
            item_ids.add(item["item_id"])
            if item["why_included"] not in ("target_source", "declared_rule_path", "selected_reference"):
                continue
            ref, = item["source_refs"]
            path = ref["path"]
            assert ref["generation_id"] == generation
            start, end = ref["range"]["start_byte"], ref["range"]["end_byte"]
            if item["why_included"] == "selected_reference":
                relation = json.loads(item["content"])["relation"]
                if relation["kind"] == "CALLS_DIRECT" and relation["target_symbol_id"] == anchor["symbol_id"]:
                    assert item["required_for_review"] and relation["evidence_kind"] == "type_resolved"
                    assert path == relation["site_path"] == "fanout.go" and ref["range"] == relation["range"]
                    assert ref["content_hash"] == hashes[path]
                    assert (start, end) in expected and (start, end) not in calls
                    calls.add((start, end))
                if relation["kind"] == "CALLS_DIRECT" and relation["source_symbol_id"] == anchor["symbol_id"]:
                    assert cycle_edge is None and relation["evidence_kind"] == "type_resolved"
                    assert path == "fanout.go" and files[path][start:end] == b"Caller0000"
                    cycle_edge = relation["target_symbol_id"]
                if relation["kind"] == "CALLS_DIRECT" and (start, end) == first_call:
                    first_caller = relation["source_symbol_id"]
                continue
            expected_path = "AGENTS.md" if item["why_included"] == "declared_rule_path" else "fanout.go"
            assert item["required_for_review"] and path == expected_path and ref["content_hash"] == hashes[path]
            assert item["content"].encode() == files[path][start:end]
            if path == "AGENTS.md":
                rule_ranges.add((start, end))
            else:
                target_ranges.append((start, end))
        cursor = pagination["next_cursor"]
        if not cursor:
            assert page["status"] == "ok" and not pagination["truncated"] and required == optional == 0
            break
        assert page["status"] == "partial" and pagination["truncated"] and required + optional > 0
        assert cursor not in cursors
        cursors.add(cursor)
        result, page = read(f"pack_{number + 1:03d}", "expand", {
            "pack_token": token, "next_cursor": cursor,
            "max_output_bytes": 8192 if number == 0 else config["max_output_bytes"],
        })
    else:
        raise AssertionError("context pack did not reach the end")
    assert len(pending) > 2 and pending[0] >= fixture.FANOUT and calls == expected
    assert cycle_edge is not None and cycle_edge == first_caller
    assert rule_ranges == {(0, len(PACK_RULE))}
    end = 0
    for start, next_end in sorted(target_ranges):
        assert start == end and next_end > start, (end, start, next_end)
        end = next_end
    assert end == len(files["fanout.go"])
    proof["pack"] = {"pages": len(pending), "required_pending": pending, "item_ids": len(item_ids),
                     "verified_call_ranges": len(calls), "target_bytes": end, "rule_bytes": len(PACK_RULE),
                     "pinned_scope": scope, "verified_cycle": True}

    shown = run("pack_config", ["config", "show", "--input-json", "-"], {"action": "show"})
    assert shown.returncode == 0
    limited = run("pack_node_cap_config", ["config", "set", "--input-json", "-"], {
        "action": "set", "config": {"schema_version": 1, "core": {**config, "max_graph_nodes": 100}},
        "expected_digest": json.loads(shown.stdout)["data"]["config_digest"],
    })
    assert limited.returncode == 0
    result, page = read("pack_node_cap", "context", request)
    assert result.returncode != 0 and page["error"]["code"] == "CONTEXT_INCOMPLETE", page
    assert page["data"] is None and page["readiness"] == "blocked"
    assert page["pagination"] == {"truncated": True, "enumeration_complete": False,
                                   "required_pending": None, "optional_pending": None,
                                   "returned_count": 0, "next_cursor": None,
                                   "limit_reason": "CONTINUATION_UNAVAILABLE"}
    proof["bounded_traversal"] = page["pagination"]


def prove_fanout(read, path, generation, proof):
    source = path.read_bytes()
    expected = {match.span() for match in re.finditer(rb"\bFanoutAnchor\b", source)}
    assert len(expected) == fixture.FANOUT + 1
    digest = hashlib.sha256(source).hexdigest()
    cap = 65536
    replacement = "RenamedFanoutAnchor"
    request = {
        "selector": {"kind": "source_position", "path": path.name,
                     "content_hash": digest, "byte_offset": min(expected)[0]},
        "new_name": replacement, "preserve_contracts": True, "max_output_bytes": cap,
    }
    seen, cursors = set(), set()
    token = None
    for number in range(1, 65):
        result, response = read(f"rename_page_{number}", "rename-preview", request)
        assert response["budget"]["serialized_bytes"] <= cap
        data = response["data"]
        if number == 1 and data is None:
            assert (response["error"] or {}).get("code") in {
                "TIMEOUT", "RESOURCE_LIMIT", "FRESHNESS_UNKNOWN", "UNSUPPORTED_CAPABILITY",
            }, response
            proof["rename_fanout"] = {"outcome": "unsupported_at_measured_cap",
                                      "error": response["error"]["code"]}
            return
        assert result.returncode in (0, 6) and data is not None, response
        assert data["computation_complete"] and data["total_edits"] == len(expected), data["total_edits"]
        assert data["applied"] is False and data["source_mutation_count"] == 0
        if token is None:
            token = data["result_token"]
        assert token == data["result_token"]
        assert data["edits"]
        for edit in data["edits"]:
            ref = edit["source_ref"]
            assert ref["path"] == path.name and ref["content_hash"] == digest and ref["generation_id"] == generation
            span = (ref["range"]["start_byte"], ref["range"]["end_byte"])
            assert span in expected and span not in seen and edit["new_text"] == replacement
            seen.add(span)
        cursor = response["pagination"]["next_cursor"]
        if not cursor:
            assert data["preview_complete"] and seen == expected
            assert path.read_bytes() == source
            proof["rename_fanout"] = {"outcome": "completed", "edits": len(seen), "pages": number}
            return
        assert cursor not in cursors
        cursors.add(cursor)
        request = {"result_token": token, "cursor": cursor, "max_output_bytes": cap}
    raise AssertionError("rename pagination did not terminate")


def prove_scope_and_quota(root, command, worktree, profile_id, profile, policy, config, run, read, generation):
    def invoke(args, payload=None, uid=1000):
        result = command('/mce4d', args, uid=uid, payload=payload)
        assert result.returncode == 0, (args, result.stderr)
        return result

    peer = re.search(r"Registered worktree ([0-9a-f-]+)", invoke(['repo','add','--root','/peer']).stdout)[1]
    invoke(['policy','set','--input-json','-'], {'worktree_id':peer,'expected_epoch':1,
                                                'policy':{**policy,'allowed_roots':['/peer']}})
    response = invoke(['profile','set','--worktree-id',peer,'--input-json','-'],
                      {'worktree_id':peer,'profile_name':'scale','expected_revision':0,'profile':profile})
    peer_profile = json.loads(response.stdout)["data"]["profile_id"]
    provisioned = command('/state-profile', ['worktree-create','--root','/profiles/runtime','--worktree-id',peer], uid=0)
    assert provisioned.returncode == 0
    A = ['index','--worktree-id',worktree,'--profile-id',profile_id]
    state = root / 'profiles/runtime/home/.local/state/mce4d/worktrees' / worktree
    with (state/'writer.lock').open('rb') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        busy = run("A_writer_busy", A + ['--full'])
        assert busy.returncode == 4
        other = run("B_indexes_while_A_locked", ['index','--worktree-id',peer,'--profile-id',peer_profile])
        assert other.returncode == 0 and 'indexed 2 files' in other.stderr

    def set_config(override):
        shown = json.loads(invoke(['config','show','--input-json','-'], {'action':'show'}).stdout)
        invoke(['config','set','--input-json','-'], {
            'action': 'set', 'config': {'schema_version': 1, 'core': {**config, **override}},
            'expected_digest': shown['data']['config_digest'],
        })

    for name, override in (
        ("file_limit", {"max_indexed_files": 8}),
        ("cache_limit", {"max_worktree_cache_bytes": 524288,
                         "rename_result_max_bytes": 262144, "rename_results_max_bytes": 524288}),
    ):
        set_config(override)
        limited = run(name, A + ['--full'])
        assert limited.returncode == 4 and 'limit reached' in limited.stderr
        if name == "cache_limit":
            refused, response = read("cache_limit_read", "status", {})
            assert refused.returncode == 4 and response["error"]["code"] == "RESOURCE_LIMIT"
            assert response["data"] is None
            startup = run("cache_limit_mcp", ['mcp', '--worktree-id', worktree, '--profile-id', profile_id])
            assert startup.returncode == 4 and 'RESOURCE_LIMIT' in startup.stderr and not startup.stdout
            oracle(state, profile_id, generation)
            set_config({})
        _, current = read(name+"_active", "status", {})
        assert current["data"]["scopes"][0]["generation_id"] == generation
    print('C053_MULTI_WORKTREE_AND_QUOTA_PASS', flush=True)
