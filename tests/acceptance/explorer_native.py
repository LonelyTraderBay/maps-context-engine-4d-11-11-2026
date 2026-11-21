"""Exercise the real packaged Explorer on a broker-owned synthetic index."""

import hashlib
import http.client
import json
import os
import re
import selectors
import signal
import subprocess
import time
from datetime import datetime, timezone
from urllib.parse import urlsplit

API = "/api/explorer/v1"


def exercise(root, setup, worktree, profile_id, active, *, gu0=False, canary_only=False):
    args = ["/mce4d", "explorer", "--worktree-id", worktree, "--profile-id", profile_id, "--listen", "127.0.0.1:0"]
    child = subprocess.Popen(args, preexec_fn=setup(1000), env={
        "PATH": "/toolchain/bin", "HOME": "/profiles/runtime/home", "GOMAXPROCS": "4",
        "GOTELEMETRY": "off", "MODERNC_SQLITE_OFD_LOCK": "0", "GOTOOLCHAIN": "local",
    }, stdout=subprocess.PIPE, stderr=subprocess.PIPE, bufsize=0)
    active.append(child)
    with selectors.DefaultSelector() as ready:
        ready.register(child.stdout, selectors.EVENT_READ)
        console = bytearray()
        while console.count(b"\n") < 2:
            assert ready.select(15), "Explorer startup deadline"
            received = os.read(child.stdout.fileno(), 256)
            assert received and len(console) + len(received) < 512, "Explorer startup failed"
            console.extend(received)
    origin_line, code_line = console.decode().splitlines()
    assert re.fullmatch(r"Explorer http://127\.0\.0\.1:\d+/", origin_line)
    assert re.fullmatch(r"Bootstrap code: [A-Za-z0-9_-]{43}", code_line)
    origin = origin_line.removeprefix("Explorer ").rstrip("/")
    code = code_line.removeprefix("Bootstrap code: ")
    port = urlsplit(origin).port
    assert port and port > 0
    denied_file = root / "target/gu0-denied.txt"
    denied_bytes = denied_file.read_bytes() if canary_only else b""

    def query(method, path, body=None, bearer=None, status=200, amend=None, json_response=True):
        headers = {"Origin": origin}
        if method == "POST":
            headers.update({"Content-Type": "application/json", "X-MCE4D-Intent": "explorer"})
        if bearer is not None:
            headers["Authorization"] = "Bearer " + bearer
        if amend is not None:
            amend(headers)
        conn = http.client.HTTPConnection("127.0.0.1", port, timeout=14)
        try:
            conn.request(method, path, json.dumps(body) if body is not None else None, headers)
            response = conn.getresponse()
            raw = response.read(1048577)
            assert response.status == status, (path, response.status, status, json.loads(raw).get("error") if raw.startswith(b"{") else None)
            assert response.getheader("Cache-Control") == "no-store"
            assert not response.getheader("Access-Control-Allow-Origin") and not response.getheader("Set-Cookie")
            if gu0:
                assert b"synthetic existing host config" not in raw
            if canary_only:
                assert denied_bytes.strip() not in raw
            if not json_response:
                assert "frame-ancestors 'none'" in response.getheader("Content-Security-Policy")
                return raw
            envelope = json.loads(raw)
            assert envelope["budget"]["serialized_bytes"] == len(raw)
            assert envelope["budget"]["delivery_mode"] == "http"
            if status != 200:
                assert envelope["data"] is None and code.encode() not in raw
                if bearer is not None:
                    assert bearer.encode() not in raw
            return envelope
        finally:
            conn.close()

    try:
        asset = query("GET", "/", json_response=False)
        expected_asset = b"Explorer \xc2\xb7 Maps Context Engine" if gu0 else b"MCE4D T082 synthetic test assets"
        assert expected_asset in asset
        query("GET", API + "/workspace", status=401)
        query("POST", API + "/session/bootstrap", {"code": code}, status=403,
              amend=lambda h: h.pop("X-MCE4D-Intent"))
        bootstrap = query("POST", API + "/session/bootstrap", {"code": code})
        bearer = bootstrap["data"]["bearer"]
        assert bootstrap["scope"] is None and bootstrap["readiness"] == "blocked"
        query("POST", API + "/session/bootstrap", {"code": code}, status=401)
        query("GET", API + "/workspace", bearer=bearer, status=403,
              amend=lambda h: h.update({"Host": f"localhost:{port}"}))
        query("GET", API + "/workspace", bearer=bearer, status=403,
              amend=lambda h: h.update({"Origin": "null"}))
        preview_ready = "preview_status" in bootstrap["data"]["capabilities"]
        preview = query("POST", API + "/preview/status", {"session_id": worktree}, bearer,
                        status=403 if preview_ready else 404)
        if preview_ready:
            assert preview["error"]["code"] == "PREVIEW_NOT_APPROVED" and preview["scope"] is None
        query("POST", API + "/preview-permission", {"action": "list"}, bearer, status=404)
        workspace = query("GET", API + "/workspace?verify=none", bearer=bearer)
        scope = workspace["scope"]
        assert workspace["data"]["index_state"] == "published"
        assert scope["members"][0]["worktree_id"] == worktree and scope["members"][0]["profile_id"] == profile_id

        if canary_only:
            verify_canary(query, bearer, scope, denied_file.name, denied_bytes)
        else:
            verify_index(query, root, bearer, scope, bootstrap["data"]["expires_at"], gu0)
    finally:
        if child.poll() is None:
            child.send_signal(signal.SIGINT)
        child.wait(timeout=8)
    assert child.returncode == 0, "Explorer did not shut down cleanly"
    conn = http.client.HTTPConnection("127.0.0.1", port, timeout=2)
    try:
        conn.connect()
    except ConnectionRefusedError:
        pass
    else:
        raise AssertionError("Explorer listener remained open")
    finally:
        conn.close()


def verify_index(query, root, bearer, scope, expiry, gu0):
    request = {"root": {"kind": "workspace"}, "scope": {"pinned_vector": scope}, "node_limit": 3, "edge_limit": 4}
    pages, nodes, edges, cursors = 0, {}, {}, set()
    while True:
        page = query("POST", API + "/graph/query", request, bearer)
        assert page["scope"] == scope and page["freshness"]["status"] == "not_checked"
        data, pagination = page["data"], page["pagination"]
        visible = {node["node_id"]: node for node in data["nodes"]}
        assert len(visible) <= 3 and len(data["edges"]) <= 4
        assert all(edge["from_node_id"] in visible and edge["to_node_id"] in visible for edge in data["edges"])
        nodes.update(visible)
        edges.update({edge["edge_id"]: edge for edge in data["edges"]})
        pages += 1
        cursor = pagination["next_cursor"]
        if cursor is None:
            assert pagination["enumeration_complete"] and pagination["required_pending"] == pagination["optional_pending"] == 0
            break
        assert pagination["truncated"] and cursor not in cursors and pages < 100
        cursors.add(cursor)
        if pages == 1:
            failure = query("POST", API + "/graph/query", dict(request, cursor=cursor, depth=1), bearer, status=409)
            assert failure["error"]["code"] == "CURSOR_INVALID"
        request["cursor"] = cursor
    assert len(nodes) > 3 and any(edge["relation_kind"] == "READS" for edge in edges.values()), "expected authored static relations"
    show = [node for node in nodes.values() if node["label"] == "Show"]
    assert len(show) == 1 and len(show[0]["source_refs"]) == 1
    ref = show[0]["source_refs"][0]
    source_file = root / "target/a.go"
    authored = source_file.read_bytes()
    assert ref["path"] == "a.go" and ref["content_hash"] == hashlib.sha256(authored).hexdigest()
    offsets = ref["range"]
    expected = authored[offsets["start_byte"]:offsets["end_byte"]]
    assert b"Show" in expected
    source = query("POST", API + "/source/read", {"source_ref": ref}, bearer)
    assert source["data"]["content"].encode() == expected and source["data"]["source_ref"] == ref
    assert source["freshness"]["status"] == "not_checked" and source["scope"] == scope
    source_file.write_bytes(authored.replace(b"func Show", b"func Test"))
    try:
        stale = query("POST", API + "/source/read", {"source_ref": ref}, bearer)
        assert stale["data"]["content"].encode() == expected
    finally:
        source_file.write_bytes(authored)
    foreign = dict(ref, worktree_id="01234567-89ab-cdef-0123-456789abcdef")
    query("POST", API + "/source/read", {"source_ref": foreign}, bearer, status=403)
    bad_hash = dict(ref, content_hash="a" * 64)
    query("POST", API + "/source/read", {"source_ref": bad_hash}, bearer, status=409)
    oversized_ref = dict(ref, range={"start_byte": 0, "end_byte": len(authored)})
    too_large = query("POST", API + "/source/read", {"source_ref": oversized_ref, "max_output_bytes": 4096}, bearer, status=413)
    assert too_large["error"]["code"] == "RESULT_TOO_LARGE"
    assert too_large["error"]["details"][0]["source_refs"] == [oversized_ref]
    if gu0:
        verify_gu0(query, bearer, expiry, ref)
    # No credentials, graph bodies or captured source are emitted to the evidence log.
    print(json.dumps({"case": "C083_GU0" if gu0 else "T082_EXPLORER_NATIVE", "status": "PASS",
                      "graph_pages": pages, "nodes": len(nodes), "edges": len(edges),
                      "exact_source_bytes": len(expected), "source_hash": ref["content_hash"],
                      "auth_transport": "IPv4 loopback, single-use bootstrap, scoped HTTP bearer"}), flush=True)


def verify_canary(query, bearer, scope, name, marker):
    member = scope["members"][0]
    assert member["policy_epoch"] == 3
    request = {"root": {"kind": "workspace"}, "scope": {"pinned_vector": scope}, "node_limit": 20, "edge_limit": 10}
    graph = query("POST", API + "/graph/query", request, bearer)
    assert graph["scope"] == scope and graph["data"]["nodes"]
    assert name not in json.dumps(graph["data"])
    ref = {field: member[field] for field in ("worktree_id", "profile_id", "generation_id")}
    ref.update(path=name, content_hash=hashlib.sha256(marker).hexdigest(),
               range={"start_byte": 0, "end_byte": len(marker)})
    denied = query("POST", API + "/source/read", {"source_ref": ref}, bearer, status=403)
    assert denied["scope"] is None and denied["error"]["code"] == "ACCESS_DENIED"
    file_ref = {key: value for key, value in ref.items() if key != "range"}
    blocked_node = query("POST", API + "/graph/query", {
        "root": {"kind": "ref", "ref": dict(file_ref, kind="file"), "service_id": None},
        "scope": {"pinned_vector": scope}, "depth": 0,
    }, bearer, status=403)
    assert blocked_node["scope"] is None and blocked_node["error"]["code"] == "ACCESS_DENIED"
    print(json.dumps({"check": "C083_POLICY_CANARY", "status": "PASS", "policy_epoch": 3,
                      "denied_path": 403, "denied_file_node": 403, "canary_disclosures": 0}), flush=True)


def verify_gu0(query, bearer, expiry, ref):
    source = API + "/source/read"
    workspace = API + "/workspace?verify=none"
    read = {"source_ref": ref}
    for headers in ({"Origin": "null"}, {"Origin": "http://127.0.0.1:1"},
                    {"Host": "fixture.invalid"}, {"X-MCE4D-Intent": "other"}):
        denied = query("POST", source, read, bearer, status=403, amend=lambda h: h.update(headers))
        assert denied["scope"] is None and denied["error"]["code"] == "ACCESS_DENIED"
    assert query("POST", source, read, status=401)["error"]["code"] == "AUTH_REQUIRED"
    assert query("POST", source, read, bearer, status=403,
                 amend=lambda h: h.pop("X-MCE4D-Intent"))["scope"] is None
    for route in ("/registry", "/proxy", "/exec"):
        assert query("GET", API + route, bearer=bearer, status=404)["scope"] is None

    inaccessible_profile = dict(ref, profile_id="a" * 64)
    blocked = query("POST", source, {"source_ref": inaccessible_profile}, bearer, status=403)
    assert blocked["scope"] is None and blocked["error"]["code"] == "ACCESS_DENIED"
    absent_generation = dict(ref, generation_id="01234567-89ab-cdef-0123-456789abcdef")
    stale = query("POST", source, {"source_ref": absent_generation}, bearer, status=410)
    assert stale["scope"] is None and stale["error"]["code"] == "SNAPSHOT_EXPIRED"
    foreign_graph = {"root": {"kind": "workspace"}, "scope": {
        "worktree_id": ref["worktree_id"], "profile_id": inaccessible_profile["profile_id"],
    }}
    blocked = query("POST", API + "/graph/query", foreign_graph, bearer, status=403)
    assert blocked["scope"] is None and blocked["error"]["code"] == "ACCESS_DENIED"

    # A successful workspace read during the configured TTL must not renew it.
    assert query("GET", workspace, bearer=bearer)["data"]["index_state"] == "published"
    expires = datetime.fromisoformat(expiry).astimezone(timezone.utc)
    remaining = (expires - datetime.now(timezone.utc)).total_seconds()
    assert 0 < remaining <= 16
    time.sleep(remaining + 0.15)
    for method, path, body in (("GET", workspace, None), ("POST", source, read)):
        expired = query(method, path, body, bearer, status=401)
        assert expired["scope"] is None and expired["error"]["code"] == "AUTH_EXPIRED"
    print(json.dumps({"check": "C083_DENIALS", "status": "PASS",
                      "host_origin_csrf": 403, "missing_bearer": 401, "expired_bearer": 401,
                      "bearer_ttl_seconds": 16, "foreign_profile": 403, "absent_generation": 410,
                      "closed_routes": 404}), flush=True)
