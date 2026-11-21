import copy
from hashlib import sha256
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

import corpus
from main import checked_plan, make_plan, materialize, read_json, write_json
from report import render_report


SETTINGS = {
    "host": "synthetic importer check", "model": "fixture", "model_revision": "fixture-v1",
    "reasoning": "fixture", "permission_profile": "isolated synthetic worktree",
    "goos": "linux", "goarch": "amd64", "toolchain": "go1.27.4", "machine": "fixture machine",
    "filesystem": "fixture filesystem", "completion_budget_ms": 900000,
    "common_tools": ["read", "search", "git", "gopls"],
}


def observation(outcome="accepted", tokens=100, cached=30):
    return {
        "outcome": outcome, "elapsed_ms": 200,
        "review": {"review_id": "synthetic-review-01", "critical_omissions": 0, "false_edits": 0}
        if outcome == "accepted" else None,
        "usage": {"input_tokens": tokens, "cached_input_tokens": cached, "output_tokens": 20},
        "context_proxy": {"tool_payload_bytes": 400, "lines": 6, "files": 2, "read_calls": 3},
        "index": None,
        "queries": [
            {"operation": "read", "temperature": "cold", "outcome": "ok", "elapsed_ms": 100,
             "freshness_ms": None, "required_pending": None},
            {"operation": "read", "temperature": "warm", "outcome": "ok", "elapsed_ms": 20,
             "freshness_ms": None, "required_pending": None},
        ],
    }


def observations(plan):
    rows = []
    tasks = {task["id"]: task for task in plan["tasks"]}
    for run in plan["runs"]:
        task = tasks[run["task"]]
        tokens, cached = {"A": (100, 30), "B": (90, 15), "C": (75, 10)}[run["arm"]]
        rows.append({"run_id": run["id"], "starting_revision": task["starting_tree_sha256"][:40],
                     "input_sha256": task["input_sha256"], "attempts": [observation(tokens=tokens, cached=cached)]})
    return {"schema_version": 1, "plan_sha256": plan["plan_sha256"],
            "usage_source": {"kind": "synthetic_fixture", "artifact_sha256": "a" * 64}, "runs": rows}


class BenchTest(unittest.TestCase):
    def setUp(self):
        self.plan = make_plan(53038, 1, SETTINGS)
        self.source = observations(self.plan)

    def row(self, run_id):
        return next(run for run in self.source["runs"] if run["run_id"] == run_id)

    def test_authored_corpus_and_fair_plan(self):
        plan = make_plan(53038, 3, SETTINGS)
        self.assertEqual(plan, make_plan(53038, 3, SETTINGS))
        self.assertNotEqual(plan["runs"], make_plan(53039, 3, SETTINGS)["runs"])
        self.assertEqual(len(plan["tasks"]), 30)
        self.assertEqual(len(plan["repositories"]), 3)
        self.assertEqual(len({run["id"] for run in plan["runs"]}), 270)
        self.assertEqual({task["kind"] for task in plan["tasks"]},
                         {"rename", "behavior_bug", "interface_change", "field_change", "type_change",
                          "contract_preservation", "dirty_worktree", "missing_dependency", "large_fanout"})
        for task in plan["tasks"]:
            materials = corpus.files(task["repo"], task["variant"])
            for ref in task["required_refs"]:
                path, name = ref.split(":", 1)
                self.assertIn(name.encode(), materials[path], ref)
            matching = [run for run in plan["runs"] if run["task"] == task["id"]]
            self.assertEqual({(r["arm"], r["repeat"]) for r in matching},
                             {(arm, n) for arm in "ABC" for n in (1, 2, 3)})
        self.assertEqual(len([ref for ref in next(t for t in plan["tasks"] if t["id"] == "relay-01")
                              ["required_refs"] if ref.startswith("fanout")]), 1025)
        self.assertNotIn(b"require", corpus.files("relay")["go.mod"])
        self.assertNotIn("optional.go", corpus.files("relay"))
        self.assertIn(b"require example.invalid/missing", corpus.files("relay", "missing_dependency")["go.mod"])

    def test_report_keeps_failed_retry_and_terminal_failure(self):
        retry = observation("failed", 50, 5)
        retry["queries"][0]["outcome"] = "failed"
        self.row("routing-01-A1")["attempts"].insert(0, retry)
        self.row("billing-01-A1")["attempts"] = [observation("timeout")]
        self.row("routing-02-B1")["attempts"][0]["usage"] = None
        self.row("routing-02-C1")["attempts"][0]["usage"]["cached_input_tokens"] = None
        result = render_report(self.plan, self.source)
        base = result["cohorts"]["A"]
        self.assertEqual((base["planned"], base["reported"], base["unreported"]), (30, 30, 0))
        self.assertEqual(base["terminal_outcomes"], {"accepted": 29, "failed": 0, "timeout": 1, "cancelled": 0})
        self.assertEqual(base["attempt_outcomes"]["failed"], 1)
        self.assertEqual(base["reviewed_attempts"], 29)
        self.assertEqual(base["unreviewed_attempts"], 2)
        self.assertEqual(base["usage"]["input_tokens"], {"observed": 31, "sum": 3050})
        self.assertEqual(base["usage"]["cached_input_tokens"], {"observed": 31, "sum": 905})
        self.assertEqual(base["uncached_input_tokens"], 2145)
        self.assertAlmostEqual(base["tokens_per_accepted"], 3670 / 29)
        self.assertEqual(base["context_proxy"]["tool_payload_bytes"]["sum"], 12400)
        self.assertEqual(base["query"]["cold"]["read"]["outcomes"]["failed"], 1)
        self.assertEqual(base["query"]["cold"]["read"]["latency_ms"], {"samples": 30, "p50": 100, "p95": 100})
        self.assertEqual(base["query"]["warm"]["read"]["latency_ms"], {"samples": 31, "p50": 20, "p95": 20})
        self.assertIsNone(result["cohorts"]["B"]["tokens_per_accepted"])
        self.assertIsNone(result["cohorts"]["C"]["uncached_input_tokens"])
        self.assertIsNotNone(result["cohorts"]["C"]["tokens_per_accepted"])
        savings = result["paired_input_savings"]
        self.assertEqual(savings["ineligible_pairs"]["unsuccessful"], 1)
        self.assertEqual(savings["reported_tasks"], 29)
        self.assertEqual(savings["median_fraction"], 0.25)
        self.assertEqual(next(s for s in savings["per_task"] if s["task"] == "routing-01")["fraction"], 0.5)

    def test_reports_missing_usage_and_unreported_runs(self):
        one = self.row("routing-01-A1")
        one["attempts"][0]["usage"] = None
        self.source["runs"] = [one]
        self.source["usage_source"] = {"kind": "unavailable", "artifact_sha256": None}
        result = render_report(self.plan, self.source)
        self.assertEqual(result["cohorts"]["A"]["unreported"], 29)
        self.assertIsNone(result["cohorts"]["A"]["tokens_per_accepted"])
        self.assertIsNone(result["cohorts"]["A"]["usage"]["input_tokens"]["sum"])
        self.assertEqual(result["cohorts"]["A"]["context_proxy"]["files"]["sum"], 2)
        self.assertEqual(result["paired_input_savings"]["ineligible_pairs"]["missing_run"], 30)
        self.assertEqual(sum(row["state"] == "unreported" for row in result["runs"]), 89)

        data = observations(self.plan)
        data["runs"].pop()
        report = render_report(self.plan, data)
        partial = next(cohort for cohort in report["cohorts"].values() if cohort["unreported"])
        self.assertTrue(partial["usage_complete_for_reported"])
        self.assertIsNone(partial["tokens_per_accepted"])

    def test_rejects_unfair_or_malformed_inputs(self):
        def first(data):
            return data["runs"][0]["attempts"][0]

        mutations = {
            "wrong plan": lambda data: data.update(plan_sha256="0" * 64),
            "wrong input": lambda data: data["runs"][0].update(input_sha256="0" * 64),
            "wrong revision": lambda data: data["runs"][0].update(starting_revision="0" * 40),
            "duplicate": lambda data: data["runs"].append(data["runs"][0]),
            "cache exceeds input": lambda data: first(data)["usage"].update(cached_input_tokens=101),
            "bool counter": lambda data: first(data)["usage"].update(output_tokens=True),
            "estimated tokens": lambda data: first(data)["usage"].update(token_estimate=10),
            "raw prompt": lambda data: first(data).update(transcript="forbidden"),
            "unreviewed acceptance": lambda data: first(data).update(review=None),
            "omission": lambda data: first(data)["review"].update(critical_omissions=1),
            "retry after accepted": lambda data: data["runs"][0]["attempts"].append(observation()),
            "bad phase": lambda data: first(data)["queries"][0].update(temperature="ambient"),
            "bad freshness": lambda data: first(data)["queries"][0].update(freshness_ms=101),
        }
        for name, mutate in mutations.items():
            with self.subTest(name=name):
                altered = copy.deepcopy(self.source)
                mutate(altered)
                with self.assertRaises(ValueError):
                    render_report(self.plan, altered)

    def test_replays_cli_and_refuses_tampered_plan(self):
        with tempfile.TemporaryDirectory(prefix="mce-bench-test-") as tmp:
            root = Path(tmp)
            plan_path = root / "plan.json"
            settings_path = root / "settings.json"
            observations_path = root / "observations.json"
            write_json(settings_path, SETTINGS)
            write_json(observations_path, self.source)
            script = [sys.executable, "-B", str(Path(__file__).with_name("main.py"))]
            generated = subprocess.check_output(script + ["plan", "--seed", "53038", "--repeats", "1",
                                                          "--settings", str(settings_path)])
            plan_path.write_bytes(generated)
            report = script + ["report", "--plan", str(plan_path), "--observations", str(observations_path)]
            first = subprocess.check_output(report)
            self.assertEqual(first, subprocess.check_output(report))
            self.assertEqual(json.loads(first)["cohorts"]["A"]["reported"], 30)
            changed = checked_plan(plan_path)
            changed["arms"]["A"] = "forbid semantic tools"
            plan_path.write_text(json.dumps(changed), encoding="utf-8")
            rejected = subprocess.run(report, capture_output=True)
            self.assertEqual(rejected.returncode, 2)
            self.assertEqual(rejected.stdout, b"")
            plan_path.write_text('{"seed":1,"seed":2}', encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "duplicate"):
                read_json(plan_path)

    def test_materializes_git_worktrees_with_independent_dirty_seed(self):
        executable = os.getenv("MCE4D_BENCH_GIT")
        approved = os.getenv("MCE4D_BENCH_GIT_SHA256")
        if not executable or not approved:
            self.skipTest("requires an explicitly pinned Git executable")
        git = Path(executable)
        with tempfile.TemporaryDirectory(prefix="mce-bench-prepare-") as tmp:
            root = Path(tmp).resolve()
            a, b, c, retry, missing = (root / name for name in ("A", "B", "C", "retry", "missing"))
            first = materialize(self.plan, "relay-02-A1", 1, a, git, approved)
            (a / "worktree" / "relay.go").write_text("fixture mutation", encoding="utf-8")
            observed = [materialize(self.plan, f"relay-02-{arm}1", 1, target, git, approved)
                        for arm, target in (("B", b), ("C", c))]
            observed.append(materialize(self.plan, "relay-02-A1", 2, retry, git, approved))
            for evidence in observed:
                self.assertEqual(evidence["starting_revision"], first["starting_revision"])
                self.assertEqual(evidence["input_sha256"], first["input_sha256"])
            for work in (b, c, retry):
                self.assertIn(b"local operator annotation", (work / "worktree" / "relay.go").read_bytes())
                self.assertIn(b"LocalCode", (work / "worktree" / "local.go").read_bytes())
                self.assertNotIn("review_oracle", read_json(work / "task.json"))
            for key in ("prompt", "settings", "dependency_state"):
                self.assertEqual(read_json(a / "task.json")[key], read_json(b / "task.json")[key])
                self.assertEqual(read_json(b / "task.json")[key], read_json(c / "task.json")[key])
            missing_proof = materialize(self.plan, "relay-03-A1", 1, missing, git, approved)
            self.assertEqual(missing_proof["starting_revision"], first["starting_revision"])
            self.assertNotEqual(missing_proof["input_sha256"], first["input_sha256"])
            self.assertEqual(read_json(missing / "task.json")["build_tags"], ["bench_missing"])
            self.assertNotIn(b"require", (b / "worktree" / "go.mod").read_bytes())
            self.assertIn(b"require example.invalid/missing", (missing / "worktree" / "go.mod").read_bytes())
            with self.assertRaises(FileExistsError):
                materialize(self.plan, "relay-02-A1", 2, a, git, approved)
            with self.assertRaises(ValueError):
                materialize(self.plan, "relay-02-A1", 2, root / "unapproved", git, "0" * 64)


if __name__ == "__main__":
    unittest.main()
