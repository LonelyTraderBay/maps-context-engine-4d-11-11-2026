"""Exercise durable record service and the normal records CLI on synthetic state."""

import hashlib
import json
import uuid


def exercise(root, command, worktree, profile, policy, helpers, *, change_policy):
    """Use one fresh protected state for each provenance transition."""
    binding = ["--worktree-id", worktree, "--profile-id", profile]
    def run(args, payload=None):
        process = command("/mce4d", args, payload=payload)
        assert process.returncode == 0, (args, process.returncode, process.stderr, process.stdout)
        return process

    def active_member():
        status = run(binding + ["query", "status", "--input-json", "-"], payload={})
        return json.loads(status.stdout)["data"]["scopes"][0]

    member = active_member()
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

    def check_unavailable(provenance):
        reading = query("read", {"record_id": first["record_id"]}, (6,))["data"]
        projected = reading["record"]
        assert projected["record_id"] == first["record_id"] and projected["version"] == 2
        assert projected["declared_status"] == "approved" and projected["effective_status"] == "needs_review"
        assert projected["provenance_status"] == provenance, projected
        assert reading["ui_mapping"] is None and projected["source_refs"] == []
        history = query("read", {"record_id": first["record_id"], "version": 1}, (6,))["data"]
        assert history["record"]["record_id"] == first["record_id"] and history["record"]["version"] == 1
        assert history["record"]["declared_status"] == history["record"]["effective_status"] == "proposed"
        assert history["record"]["provenance_status"] == provenance
        assert history["ui_mapping"] is None and history["record"]["source_refs"] == []
        listing = query("list", {"limit": 20})
        assert listing["pagination"]["next_cursor"] is None
        assert [row for row in listing["data"]["records"] if row["record_id"] == first["record_id"]] == [projected]

    index_args = ["index", *binding, "--full"]
    gc_args = ["store", "gc", *binding]
    original_generation = member["generation_id"]
    before = helpers.tree(target)
    config_before = helpers.tree(root / "host-config")
    collected_count = 0
    if change_policy:
        changed_policy = dict(policy, denied_globs=[*policy["denied_globs"], "a.go"])
        changed = run(["policy", "set", "--input-json", "-"], {
            "worktree_id": worktree, "expected_epoch": member["policy_epoch"], "policy": changed_policy,
        })
        assert json.loads(changed.stdout)["data"]["policy_epoch"] == member["policy_epoch"] + 1
        run(index_args)
        current = active_member()
        assert current["generation_id"] != original_generation
        assert current["policy_epoch"] == member["policy_epoch"] + 1
        assert current["policy_digest"] != member["policy_digest"]
        check_unavailable("access_denied")
        # Publish enough recent generations to list the old one in a GC
        # report. This demonstrates that its bytes were withheld by policy.
        for _ in range(2):
            run(index_args)
        plan = json.loads(run(gc_args + ["--report-only"]).stdout)
        assert plan["dry_run"]
        historical = next(row for row in plan["generations"] if row["generation_id"] == original_generation)
        assert not historical["applied"] and historical["rows"]["generations"] == 1 and historical["rows"]["files"] > 0
        check_unavailable("access_denied")
        provenance = "access_denied"
    else:
        # Edits here belong to the disposable fixture controller. The engine
        # reads source under the normal read-only role.
        try:
            source.write_bytes(original + b"\n// synthetic source drift\n")
            check_unavailable("source_changed")
        finally:
            source.write_bytes(original)
        for _ in range(3):
            run(index_args)
        active_generation = active_member()["generation_id"]
        plan = json.loads(run(gc_args + ["--report-only"]).stdout)
        candidates = {row["generation_id"]: row for row in plan["generations"]}
        assert plan["dry_run"] and not plan["more"]
        assert original_generation in candidates and active_generation not in candidates
        assert all(not row["applied"] and row["rows"]["generations"] == 1 for row in candidates.values())
        retained = query("read", {"record_id": first["record_id"]})["data"]
        assert retained["record"]["provenance_status"] == "retained_snapshot"
        applied = json.loads(run(gc_args + ["--apply", "--confirm"]).stdout)
        assert not applied["dry_run"]
        assert [row["generation_id"] for row in applied["generations"]] == list(candidates)
        assert all(row["applied"] and row["rows"] == candidates[row["generation_id"]]["rows"] for row in applied["generations"])
        assert active_member()["generation_id"] == active_generation
        check_unavailable("snapshot_expired")
        attempt = dict(approval, record_id=second["record_id"], client_mutation_id=str(uuid.uuid4()))
        mutate("review", attempt, "pinned snapshot expired")
        collected_count = len(applied["generations"])
        provenance = "snapshot_expired"
    replay = mutate("review", approval)
    assert replay["record_id"] == first["record_id"] and replay["version"] == 2
    assert replay["declared_status"] == "approved" and replay["effective_status"] == "needs_review"
    check_unavailable(provenance)
    assert helpers.tree(target) == before and helpers.tree(root / "host-config") == config_before
    assert not (root / "result/repo-executed").exists()
    print(json.dumps({"case": "C170_NATIVE_RECORDS_POLICY" if change_policy else "T047_NATIVE_RECORDS_GC",
                      "status": "PASS", "provenance_status": provenance,
                      "gc_collected": collected_count, "same_receipt": True, "list_matches_head": True,
                      "record_version_retained": True, "after_gc": None if change_policy else "needs_review",
                      "target_executions": 0}), flush=True)
