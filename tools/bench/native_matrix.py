"""Local benchmark workloads for the protected Linux acceptance broker."""

from contextlib import closing
from hashlib import sha256
import json
import sqlite3
import subprocess
import time


ANCHOR = "Value000000"
SOURCE = "p0000/x000000.go"
WARM_REPEATS = 5
TEST_PACK_TTL = 12
TEST_RENAME_TTL = 60


def exercise(root, command, worktree, profile, authored, config, scale):
    target = root / "target"
    state = root / "profiles/runtime/home/.local/state/mce4d/worktrees" / worktree
    index = ["index", "--worktree-id", worktree, "--profile-id", profile]
    query = ["--worktree-id", worktree, "--profile-id", profile, "query"]
    xl_boundary = authored["cohort"] == "XL150K"
    case = "T077_XL_BOUNDARY" if xl_boundary else "T039_NATIVE_MATRIX"
    evidence = {"case": case, **authored, "config": config, "index": {}, "query": [],
                "source_sha256": scale.source_digest(target), "usage": None, "proxy": {}}

    def run_index(phase, full=False, input_bytes=None):
        start = time.monotonic()
        result = command("/mce4d", index + (["--full"] if full else []), measure=True, timeout=330)
        evidence["index"][phase] = {"elapsed_ms": round((time.monotonic() - start) * 1000, 3),
                                    "exit": result.returncode, "diagnostic": result.stderr.strip(),
                                    "state": scale.disk_usage(state), **result.metrics}
        observed = result.metrics["sampled_write_bytes_floor"]
        if observed is not None and input_bytes:
            evidence["index"][phase]["sampled_writes_per_workload_byte"] = observed / input_bytes
            evidence["index"][phase]["workload_bytes"] = input_bytes
        return result

    def read(action, payload, temperature):
        start = time.monotonic()
        result = command("/mce4d", query + [action, "--input-json", "-"], payload=payload, measure=True, timeout=35)
        elapsed = round((time.monotonic() - start) * 1000, 3)
        body = json.loads(result.stdout)
        row = {"operation": action, "temperature": temperature, "elapsed_ms": elapsed,
               "exit": result.returncode, "status": body["status"], "error": (body["error"] or {}).get("code"),
               "freshness": body["freshness"]["status"], "pending": body["pagination"]["required_pending"],
               "serialized_bytes": body["budget"]["serialized_bytes"], "peak_tree_rss_bytes": result.metrics["peak_tree_rss_bytes"]}
        evidence["query"].append(row)
        assert body["budget"]["token_count"] is None, "fixture expects unavailable engine token telemetry"
        return body, row, result.stdout.encode("utf-8")

    try:
        full = run_index("full", True, authored["source_bytes"])
        if full.returncode != 0:
            status, row, _ = read("status", {}, "after_index_refusal")
            assert row["exit"] == 0 and status["data"]["index_state"] == "missing" and status["data"]["scopes"] == []
            assert full.returncode == 4 and "limit reached" in full.stderr, full.stderr
            evidence["refusal_state"] = scale.unpublished_oracle(state)
            evidence["outcome"] = "unsupported_at_measured_cap"
            if xl_boundary:
                assert evidence["index"]["full"]["stop_reason"] is None, "outer guard stopped the engine"
                evidence["after_source_sha256"] = scale.source_digest(target)
                assert evidence["after_source_sha256"] == evidence["source_sha256"], "engine changed fixture source"
                evidence["read_after_refusal"] = "status_only"
                evidence["workload_query_update"] = "NOT_RUN"
            return

        if xl_boundary:
            raise AssertionError("XL boundary exceeded the configured inventory cap without refusal")

        status, _, _ = read("status", {}, "setup")
        active, = status["data"]["scopes"]
        evidence["coverage"] = status["coverage"]["status"]
        with closing(sqlite3.connect((state / "index.sqlite").as_uri() + "?mode=ro&immutable=1", uri=True)) as db:
            files, logical_bytes = db.execute("SELECT count(*), SUM(byte_length) FROM files WHERE generation_id=?",
                                              (active["generation_id"],)).fetchone()
        assert files == authored["files"] and logical_bytes == authored["source_bytes"], (files, logical_bytes)
        evidence["captured_logical_bytes"] = logical_bytes

        sources = (target / SOURCE).read_bytes()
        expected_hash = sha256(sources).hexdigest()
        request = {"mode": "symbol", "query": ANCHOR, "limit": 5}
        samples = []
        for temperature in ["cold"] + ["warm"] * WARM_REPEATS:
            body, result, wire = read("locate", request, temperature)
            assert result["exit"] == 0 and result["pending"] == 0, result
            candidate, = [item for item in body["data"]["candidates"] if item["name"] == ANCHOR]
            ref = candidate["ref"]
            assert any(source["path"] == SOURCE and source["content_hash"] == expected_hash
                       for source in candidate["source_refs"])
            samples.append(wire)

        pack = {"selector": {"kind": "ref", "ref": ref, "service_id": None}, "task_kind": "explore",
                "freshness_mode": "analysis_snapshot", "max_output_bytes": 24576}
        pages, valid_token = [], None
        for temperature in ["cold"] + ["warm"] * WARM_REPEATS:
            body, result, wire = read("context", pack, temperature)
            pages.append(wire)
            if body["data"] is None:
                assert result["error"] in ("TIMEOUT", "CONTEXT_INCOMPLETE", "RESOURCE_LIMIT"), result
                continue
            assert body["freshness"]["status"] == "not_checked"
            assert not body["data"]["edit_precondition_validated"]
            hits = [source for item in body["data"]["items"] for source in item["source_refs"]
                    if source["path"] == SOURCE and source["content_hash"] == expected_hash]
            assert hits, "pack missed the authored target"
            valid_token = body["data"]["pack_token"]

        validation_budget = ["cold", "warm", "warm"]
        for temperature in validation_budget:
            if valid_token:
                validation, result, _ = read("validate", {"pack_token": valid_token, "verify": "workspace"}, temperature)
                if validation["data"] is None:
                    assert result["error"] in ("TIMEOUT", "FRESHNESS_UNKNOWN", "RESOURCE_LIMIT"), result
                else:
                    assert validation["data"]["edit_precondition_validated"], validation["freshness"]
            else:
                read("status", {"verify": "workspace"}, temperature)

        # Same source selector and oracle in all arms. Use actual grep/read for
        # A; B also exchanges locate; C exchanges locate and a context page.
        grep = subprocess.run(["/usr/bin/grep", "-n", "-R", "-F", "--include=*.go", "--exclude-dir=.git",
                               "--", f"func {ANCHOR}(", "."], cwd=target, capture_output=True, check=True, timeout=20).stdout
        assert len(grep.splitlines()) == 1 and grep.startswith(f"./{SOURCE}:".encode()), grep
        evidence["proxy"] = {"method": "fixed independent-oracle retrieval, not host/LLM usage",
                             "anchor_verified": True, "A": proxy(grep, sources),
                             "B": [proxy(wire, (target / SOURCE).read_bytes()) for wire in samples],
                             "C": [proxy(lookup, page) for lookup, page in zip(samples, pages)]}
        evidence["oracle"] = {"expected_path": SOURCE, "expected_content_sha256": expected_hash,
                              "locate_verified": len(samples), "valid_snapshot_pack_observed": bool(valid_token)}

        noop = run_index("noop")
        assert noop.returncode == 0 and "unchanged" in noop.stderr and active["generation_id"] in noop.stderr
        assert scale.source_digest(target) == evidence["source_sha256"], "query modified authored sources"
        updated = sources.replace(b"return 0", b"return 1000000")
        assert updated != sources
        (target / SOURCE).write_bytes(updated)  # Fixture-controller change.
        edited = scale.source_digest(target)
        change = run_index("change", input_bytes=len(updated))
        assert change.returncode == 0 and "indexed" in change.stderr
        body, _, _ = read("locate", request, "after_update")
        new, = [item for item in body["data"]["candidates"] if item["name"] == ANCHOR]
        assert new["ref"]["generation_id"] != active["generation_id"]
        assert any(source["path"] == SOURCE and source["content_hash"] == sha256(updated).hexdigest()
                   for source in new["source_refs"])
        assert scale.source_digest(target) == edited
        evidence["changed_source_sha256"] = edited
        evidence["outcome"] = "completed"
    finally:
        print(json.dumps(evidence), flush=True)


def proxy(*parts):
    return {"payload_bytes": sum(map(len, parts)), "payload_lines": sum(p.count(b"\n") for p in parts),
            "tool_responses": len(parts)}


def exercise_context(root, setup, worktree, profile, helpers):
    """Measure the independent small source-and-contract fixture over persistent MCP."""
    target = root / "target"
    originals = helpers.tree(target)
    raw = (target / "a.go").read_bytes()
    expected = sha256(raw).hexdigest()
    declaration = raw.index(b"type Inner ") + len(b"type ")
    client = helpers.MCP(setup, worktree, profile, {})
    runs = []

    def sample(action, params, heat):
        start = time.monotonic()
        body = client.tool(action, params)
        runs.append({"operation": action, "temperature": heat, "elapsed_ms": round((time.monotonic() - start) * 1000, 3),
                     "status": body["status"], "delivered": True,
                     "freshness": body["freshness"]["status"], "pending": body["pagination"]["required_pending"],
                     "serialized_bytes": body["budget"]["serialized_bytes"]})
        return body

    try:
        for heat in ["cold"] + ["warm"] * WARM_REPEATS:
            body = sample("mce4d_locate", {"mode": "symbol", "query": "Inner", "limit": 8}, heat)
            inner, = [
                candidate for candidate in body["data"]["candidates"] if candidate["name"] == "Inner"
                and any(s["path"] == "a.go" and s["content_hash"] == expected
                        and s["range"]["start_byte"] == declaration
                        and s["range"]["end_byte"] == declaration + len("Inner") for s in candidate["source_refs"])
            ]
        pack = {"selector": {"kind": "ref", "ref": inner["ref"], "service_id": None},
                "task_kind": "explore", "freshness_mode": "analysis_snapshot", "max_output_bytes": 24576}
        for heat in ["cold"] + ["warm"] * WARM_REPEATS:
            body = sample("mce4d_context_pack", pack, heat)
            assert body["freshness"]["status"] == "not_checked" and not body["data"]["edit_precondition_validated"]
            assert inner["ref"] in body["data"]["target_refs"]
        token = body["data"]["pack_token"]
        for heat in ("cold", "warm", "warm", "warm"):
            body = sample("mce4d_validate_pack", {"pack_token": token, "verify": "workspace"}, heat)
            assert body["data"]["edit_precondition_validated"]
        assert helpers.tree(target) == originals
        print(json.dumps({"case": "T039_SMALL_PERSISTENT_MCP", "source_sha256": expected, "observations": runs}), flush=True)
    finally:
        client.close()


def exercise_retention(root, setup, worktree, profile, request, helpers):
    before = helpers.tree(root / "target")
    client = helpers.MCP(setup, worktree, profile, {})
    try:
        rename = client.tool("mce4d_rename_preview", request)
        assert rename["data"]["total_edits"] == 4 and not rename["data"]["applied"]
        rename_ready = time.monotonic()
        result = {"result_token": rename["data"]["result_token"]}
        client.tool("mce4d_validate_pack", result)
        names = client.tool("mce4d_locate", {"mode": "symbol", "query": "Inner", "limit": 8})
        ref = next(c["ref"] for c in names["data"]["candidates"] if c["name"] == "Inner")
        request = {"selector": {"kind": "ref", "ref": ref, "service_id": None}, "task_kind": "explore",
                   "freshness_mode": "analysis_snapshot", "max_output_bytes": 24576}
        issued = time.monotonic()
        pack = client.tool("mce4d_context_pack", request)
        page = {"pack_token": pack["data"]["pack_token"], "next_cursor": pack["pagination"]["next_cursor"],
                "max_output_bytes": 24576}
        assert page["next_cursor"]
        timings = []
        for _ in range(2):
            start = time.monotonic()
            actual = client.tool("mce4d_expand_pack", page)
            timings.append({"elapsed_ms": round((time.monotonic() - start) * 1000, 3),
                            "bytes": actual["budget"]["serialized_bytes"]})
        assert time.monotonic() - issued < TEST_PACK_TTL
        time.sleep(TEST_PACK_TTL + 0.25)
        expired = client.tool("mce4d_expand_pack", page, error={"CURSOR_EXPIRED"})
        pack_elapsed = round((time.monotonic() - issued) * 1000, 3)
        remaining = rename_ready + TEST_RENAME_TTL + 0.25 - time.monotonic()
        if remaining > 0:
            time.sleep(remaining)
        rename_expired = client.tool("mce4d_validate_pack", result, error={"CURSOR_EXPIRED", "RESULT_EXPIRED"})
        assert helpers.tree(root / "target") == before
        print(json.dumps({"case": "T039_C160_C179_WALL_CLOCK", "pack_ttl_seconds": TEST_PACK_TTL,
                          "rename_ttl_seconds": TEST_RENAME_TTL, "pack_elapsed_ms": pack_elapsed,
                          "rename_elapsed_after_preview_ms": round((time.monotonic() - rename_ready) * 1000, 3), "repeated_reads": timings,
                          "pack": expired["error"]["code"], "rename": rename_expired["error"]["code"]}), flush=True)
    finally:
        client.close()
