"""Exercise durable record service and the normal records CLI on synthetic state."""

import hashlib
import json
import uuid


def exercise(root, command, setup, worktree, profile, request, fixture, helpers):
    binding = ["--worktree-id", worktree, "--profile-id", profile]
    status = command("/mce4d", binding + ["query", "status", "--input-json", "-"], payload={})
    assert status.returncode == 0
    envelope = json.loads(status.stdout)
    member = envelope["data"]["scopes"][0]
    target = root / "target"
    source = target / "a.go"
    original = source.read_bytes()
    digest = hashlib.sha256(original).hexdigest()
    start = original.index(b"Inner")
    ref = {"worktree_id": worktree, "profile_id": profile, "generation_id": member["generation_id"],
           "path": "a.go", "content_hash": digest, "range": {"start_byte": start, "end_byte": start + 5}}
    destination = {key: value for key, value in ref.items() if key != "range"}
    destination["kind"] = "file"

    def mutate(action, payload, failed=None):
        process = command("/mce4d", ["__records-fixture", worktree, profile, action], payload=payload, timeout=40)
        if failed is not None:
            assert process.returncode != 0 and failed in process.stderr, (action, process.returncode, process.stderr)
            return None
        assert process.returncode == 0, (action, process.stderr)
        return json.loads(process.stdout)

    def query(action, payload, exits=(0,), error=None, uid=1000):
        process = command("/mce4d", binding + ["records", action, "--input-json", "-"], payload=payload, uid=uid)
        assert process.returncode in exits, (action, process.returncode, process.stderr, process.stdout)
        response = json.loads(process.stdout)
        assert response["budget"]["serialized_bytes"] == len(process.stdout.rstrip("\n").encode())
        if error:
            assert response["error"]["code"] == error and response["data"] is None
        else:
            assert response["error"] is None
        return response

    proposal = {"source_ref": ref, "destination_ref": destination, "rationale": "Synthetic static source mapping.",
                "client_mutation_id": str(uuid.uuid4())}
    first = mutate("propose", proposal)
    assert first["version"] == 1 and first["declared_status"] == "proposed"
    assert mutate("propose", proposal) == first
    change = dict(proposal, rationale="Different authored request")
    mutate("propose", change, "record mutation conflict")
    approval = {"record_id": first["record_id"], "expected_version": 1, "action": "approve",
                "reason": "Fixture principal checked the captured declaration.", "client_mutation_id": str(uuid.uuid4())}
    approved = mutate("review", approval)
    assert approved["declared_status"] == approved["effective_status"] == "approved" and approved["version"] == 2
    retained = query("read", {"record_id": first["record_id"]})["data"]
    assert retained["record"]["provenance_status"] == "retained_snapshot" and retained["ui_mapping"]["source_ref"] == ref
    historical = query("read", {"record_id": first["record_id"], "version": 1})["data"]
    assert historical["record"]["declared_status"] == "proposed"
    second = mutate("propose", dict(proposal, client_mutation_id=str(uuid.uuid4())))

    # Page across separate processes and verify the stable owner key.
    page = query("list", {"limit": 1}, (6,))
    token = page["pagination"]["next_cursor"]
    page_two = query("list", {"limit": 1, "cursor": token})
    ids = {page["data"]["records"][0]["record_id"], page_two["data"]["records"][0]["record_id"]}
    assert ids == {first["record_id"], second["record_id"]}
    mutate("propose", dict(proposal, client_mutation_id=str(uuid.uuid4())))
    query("list", {"limit": 1, "cursor": token}, (3,), "CURSOR_EXPIRED")
    query("read", {"record_id": first["record_id"]}, (3,), "ACCESS_DENIED", uid=1001)

    # All source edits here are on the disposable target performed by the
    # independent fixture controller. CLI/engine continue with read-only access.
    try:
        source.write_bytes(original + b"\n// synthetic source drift\n")
        stale = query("read", {"record_id": first["record_id"]}, (6,))["data"]
        assert stale["record"]["provenance_status"] == "source_changed"
        assert stale["record"]["effective_status"] == "needs_review" and stale["ui_mapping"] is None
    finally:
        source.write_bytes(original)
    before_gc = helpers.tree(target)
    for _ in range(3):
        indexed = command("/mce4d", ["index", *binding, "--full"])
        assert indexed.returncode == 0
    gc = command("/mce4d", ["store", "gc", *binding, "--apply", "--confirm"])
    assert gc.returncode == 0
    collected = json.loads(gc.stdout)["generations"]
    assert any(item["generation_id"] == member["generation_id"] and item["applied"] for item in collected)
    expired = query("read", {"record_id": first["record_id"]}, (6,))["data"]
    assert expired["record"]["version"] == 2 and expired["record"]["declared_status"] == "approved"
    assert expired["record"]["effective_status"] == "needs_review" and expired["record"]["provenance_status"] == "snapshot_expired"
    assert expired["record"]["source_refs"] == [] and expired["ui_mapping"] is None
    historical = query("read", {"record_id": first["record_id"], "version": 1}, (6,))["data"]
    assert historical["record"]["declared_status"] == "proposed" and historical["ui_mapping"] is None
    attempt = dict(approval, record_id=second["record_id"], client_mutation_id=str(uuid.uuid4()))
    mutate("review", attempt, "pinned snapshot expired")
    replay = mutate("review", approval)
    assert replay["record_id"] == first["record_id"] and replay["version"] == 2 and replay["effective_status"] == "needs_review"
    assert helpers.tree(target) == before_gc
    assert not (root / "result/repo-executed").exists()
    print(json.dumps({"case": "T047_NATIVE_RECORDS_GC", "status": "PASS",
                      "gc_collected": len(collected), "same_receipt": True,
                      "record_version_retained": True, "after_gc": "needs_review", "target_executions": 0}), flush=True)
