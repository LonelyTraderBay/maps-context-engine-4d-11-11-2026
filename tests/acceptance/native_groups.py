"""Local declared groups across two broker-owned SQLite worktrees."""

import json
import os
import re
import subprocess


def prepare(root, template, home):
    peer = root / "peer"
    peer.mkdir(mode=0o755)
    subprocess.run(["/usr/bin/git", "init", "--template=" + str(template), str(peer)], check=True, timeout=5,
                   env={"PATH": "/usr/bin:/bin", "HOME": str(home), "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null"},
                   stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    (peer / "go.mod").write_text("module example.invalid/grouppeer\n\ngo 1.27\n")
    (peer / "peer.go").write_text("package peer\nfunc Peer() int { return 1 }\n")
    for child in [peer, *peer.rglob("*")]:
        os.chown(child, 1001, 1001)
        child.chmod(0o755 if child.is_dir() else 0o644)


def exercise(root, command, worktree, profile, profile_config, policy):
    def cli(args, payload=None, exit=0, uid=1000):
        result = command("/mce4d", args, uid=uid, payload=payload)
        assert result.returncode == exit, (args, result.returncode, result.stdout, result.stderr)
        return result.stdout

    def group(action, body=None, *, exit=0, uid=1000):
        return json.loads(cli(["group", action, "--input-json", "-"], {"action": action, **(body or {})}, exit, uid))

    def status(scope, exit=0):
        return json.loads(cli(["query", "status", "--input-json", "-"], {"scope": scope, "verify": "none"}, exit))

    added = cli(["repo", "add", "--root", "/peer"])
    peer = re.search(r"Registered worktree ([0-9a-f-]{36})", added).group(1)
    peer_policy = {**policy, "allowed_roots": ["/peer"], "trusted_rule_paths": []}
    cli(["policy", "set", "--input-json", "-"], {"worktree_id": peer, "expected_epoch": 1, "policy": peer_policy})
    data = cli(["profile", "set", "--worktree-id", peer, "--input-json", "-"],
               {"worktree_id": peer, "profile_name": "typed", "expected_revision": 0, "profile": profile_config})
    peer_profile = json.loads(data)["data"]["profile_id"]
    created = command("/state-profile", ["worktree-create", "--root", "/profiles/runtime", "--worktree-id", peer], 0)
    assert created.returncode == 0

    first = "01111111-1111-4111-8111-111111111111"
    second = "02222222-2222-4222-8222-222222222222"
    namespace = {"environment": "local", "protocol": "http", "base_origin": None,
                 "route_prefix": None, "contract_version": None}
    member = lambda service, w, p: {"service_id": service, "worktree_id": w, "profile_id": p, "namespace": namespace}
    browser = member("ui", worktree, profile)
    other_browser = member("docs-ui", worktree, profile)
    api = member("api", peer, peer_profile)
    config = {"group_id": first, "name": "Fixture project", "members": [browser, other_browser, api]}
    pending = {"group_id": second, "name": "Fixture pending", "members": [api]}
    assert group("list")["data"]["groups"] == []
    denied = group("put", {"config": config, "expected_revision": 0}, exit=3, uid=1001)
    assert denied["error"]["code"] == "ACCESS_DENIED" and denied["data"] is None
    one = group("put", {"config": config, "expected_revision": 0})["data"]["groups"][0]
    assert one["revision"] == 1 and one["status"] == "active"
    assert group("get", {"group_id": first})["data"]["groups"] == [one]
    conflict = group("put", {"config": config, "expected_revision": 0}, exit=3)
    assert conflict["error"]["code"] == "RECORD_CONFLICT"
    group("put", {"config": pending, "expected_revision": 0})
    page = group("list", {"limit": 1}, exit=6)
    assert page["data"]["groups"] == [one] and page["pagination"]["next_cursor"]
    cursor = page["pagination"]["next_cursor"]
    continued = group("list", {"cursor": cursor, "limit": 2})
    assert continued["data"]["groups"][0]["config"]["group_id"] == second
    assert continued["pagination"]["next_cursor"] is None
    invalid = group("list", {"cursor": cursor[:6] + "!" + cursor[7:]}, exit=3)
    assert invalid["error"]["code"] == "CURSOR_INVALID"
    group("put", {"config": pending, "expected_revision": 1})
    expired = group("list", {"cursor": cursor, "limit": 1}, exit=3)
    assert expired["error"]["code"] == "CURSOR_EXPIRED" and expired["data"] is None

    live = status({"group_id": first}, exit=6)
    pin = live["scope"]
    assert pin["kind"] == "federated" and pin["group_id"] == first and pin["group_revision"] == 1
    assert set(m["service_id"] for m in pin["members"]) == {"ui", "docs-ui"}
    assert len({m["generation_id"] for m in pin["members"]}) == 1
    assert pin["unavailable_members"] == [{"service_id": "api", "worktree_id": peer, "profile_id": peer_profile,
                                           "status": "index_missing", "reason_code": "INDEX_MISSING"}]
    empty = status({"group_id": second}, exit=6)
    assert empty["scope"]["members"] == empty["data"]["scopes"] == [] and empty["readiness"] == "blocked"
    assert empty["data"]["index_state"] == "unavailable"
    cli(["index", "--worktree-id", peer, "--profile-id", peer_profile])
    cli(["index", "--worktree-id", worktree, "--profile-id", profile, "--full"])
    retained = status({"pinned_vector": pin}, exit=6)
    assert retained["scope"] == pin and retained["data"]["scopes"] == pin["members"]
    latest = status({"group_id": first})["scope"]
    assert not latest["unavailable_members"] and len(latest["members"]) == 3
    assert next(m["generation_id"] for m in latest["members"] if m["service_id"] == "ui") != pin["members"][0]["generation_id"]
    peer_git = root / "peer/.git"
    held_git = root / "peer/.fixture-git-held"
    peer_git.rename(held_git)
    peer_git.mkdir(mode=0o755)
    try:
        restricted = status({"group_id": first}, exit=6)["scope"]
        assert {item["service_id"] for item in restricted["members"]} == {"ui", "docs-ui"}
        assert restricted["unavailable_members"] == [{"service_id": None, "worktree_id": None, "profile_id": None,
                                                      "status": "access_denied", "reason_code": "ACCESS_DENIED"}]
        blocked = status({"pinned_vector": latest}, exit=3)
        assert blocked["error"]["code"] == "ACCESS_DENIED" and blocked["scope"] is None and blocked["data"] is None
    finally:
        peer_git.rmdir()
        held_git.rename(peer_git)
    wrong = status({"pinned_vector": {**pin, "vector_digest": "a" * 64}}, exit=2)
    assert wrong["error"]["code"] == "INVALID_ARGUMENT" and wrong["scope"] is None
    removed = group("remove", {"group_id": first, "expected_revision": 1})["data"]["groups"][0]
    assert removed["status"] == "removed" and removed["revision"] == 2 and removed["config"] == one["config"]
    old = status({"pinned_vector": pin}, exit=3)
    assert old["error"]["code"] == "ACCESS_DENIED" and old["scope"] is None
    print(json.dumps({"case": "T046_GROUP_NATIVE", "status": "PASS", "declared_services": 3, "worktrees": 2,
                      "unauthorized_writer": "denied", "cursor_survives_new_process": True, "stale_cursor": "expired",
                      "pinned_gap_preserved": True, "all_unavailable_blocked": True, "tombstone_revision": 2,
                      "denied_member_identity_redacted": True, "pinned_denial_has_no_source": True}), flush=True)
