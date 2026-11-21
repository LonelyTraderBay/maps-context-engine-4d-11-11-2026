"""Validate imported observations and report complete-cohort metrics."""

import re
from statistics import median


PROXIES = ("tool_payload_bytes", "lines", "files", "read_calls")
INDEX = ("full_ms", "noop_ms", "change_ms", "bytes_hashed", "storage_bytes", "peak_tree_rss_bytes")
USAGE = ("input_tokens", "cached_input_tokens", "output_tokens")
OUTCOMES = ("accepted", "failed", "timeout", "cancelled")
QUERY_OPS = ("locate", "context", "expand", "validate", "rename", "read", "search", "semantic")
QUERY_OUTCOMES = ("ok", "failed", "timeout", "cancelled")
COUNT_MAX = (1 << 53) - 1


def fields(value, *names):
    if not isinstance(value, dict) or set(value) != set(names):
        raise ValueError(f"expected fields: {', '.join(names)}")


def count(value, nullable=False):
    if nullable and value is None:
        return
    if type(value) is not int or not 0 <= value <= COUNT_MAX:
        raise ValueError("expected a nonnegative integer within interoperable range")


def hex_digest(value, width):
    return isinstance(value, str) and re.fullmatch(r"[a-f0-9]{" + str(width) + "}", value) is not None


def attempt_data(attempt, usage_kind):
    fields(attempt, "outcome", "elapsed_ms", "review", "usage", "context_proxy", "index", "queries")
    outcome = attempt["outcome"]
    if outcome not in OUTCOMES:
        raise ValueError("invalid attempt outcome")
    count(attempt["elapsed_ms"])
    review = attempt["review"]
    if review is not None:
        fields(review, "review_id", "critical_omissions", "false_edits")
        tag = review["review_id"]
        if not isinstance(tag, str) or not re.fullmatch(r"[a-zA-Z0-9_.-]{1,80}", tag):
            raise ValueError("review_id must identify an independent review record")
        count(review["critical_omissions"])
        count(review["false_edits"])
    if outcome == "accepted" and (review is None or review["critical_omissions"] or review["false_edits"]):
        raise ValueError("acceptance requires a review with zero omissions and false edits")

    usage = attempt["usage"]
    if usage is not None:
        if usage_kind == "unavailable":
            raise ValueError("unavailable usage cannot contain token counters")
        fields(usage, *USAGE)
        for name in USAGE:
            count(usage[name], nullable=name == "cached_input_tokens")
        if usage["cached_input_tokens"] is not None and usage["cached_input_tokens"] > usage["input_tokens"]:
            raise ValueError("cached input exceeds total input")
    for group, names in (("context_proxy", PROXIES), ("index", INDEX)):
        if attempt[group] is not None:
            fields(attempt[group], *names)
            for value in attempt[group].values():
                count(value, nullable=True)

    queries = attempt["queries"]
    if not isinstance(queries, list) or len(queries) > 256:
        raise ValueError("queries must be a list with at most 256 observations")
    for query in queries:
        fields(query, "operation", "temperature", "outcome", "elapsed_ms", "freshness_ms", "required_pending")
        if (query["operation"] not in QUERY_OPS or query["temperature"] not in ("cold", "warm")
                or query["outcome"] not in QUERY_OUTCOMES):
            raise ValueError("invalid query operation, temperature or outcome")
        count(query["elapsed_ms"])
        count(query["freshness_ms"], nullable=True)
        count(query["required_pending"], nullable=True)
        if query["freshness_ms"] is not None and query["freshness_ms"] > query["elapsed_ms"]:
            raise ValueError("freshness time exceeds query duration")


def import_runs(plan, supplied):
    fields(supplied, "schema_version", "plan_sha256", "usage_source", "runs")
    if type(supplied["schema_version"]) is not int or supplied["schema_version"] != 1 or supplied["plan_sha256"] != plan["plan_sha256"]:
        raise ValueError("observations are not bound to this plan")
    provenance = supplied["usage_source"]
    fields(provenance, "kind", "artifact_sha256")
    kind, digest = provenance["kind"], provenance["artifact_sha256"]
    if kind not in ("host_export", "synthetic_fixture", "unavailable"):
        raise ValueError("unrecognized usage source")
    if (kind == "unavailable" and digest is not None) or (kind != "unavailable" and not hex_digest(digest, 64)):
        raise ValueError("usage source must identify its supporting artifact")
    runs = supplied["runs"]
    if not isinstance(runs, list) or len(runs) > len(plan["runs"]):
        raise ValueError("excessive or invalid observations")
    tasks = {task["id"]: task for task in plan["tasks"]}
    planned = {entry["id"]: entry for entry in plan["runs"]}
    imported, revisions = {}, {}
    for observed in runs:
        fields(observed, "run_id", "starting_revision", "input_sha256", "attempts")
        run_id = observed["run_id"]
        if not isinstance(run_id, str) or run_id not in planned or run_id in imported:
            raise ValueError("unknown or duplicate run ID")
        task = tasks[planned[run_id]["task"]]
        revision = observed["starting_revision"]
        if not hex_digest(revision, 40) or observed["input_sha256"] != task["input_sha256"]:
            raise ValueError(f"starting state mismatch for {run_id}")
        if revisions.setdefault(task["repo"], revision) != revision:
            raise ValueError(f"mixed starting revisions for {task['repo']}")
        attempts = observed["attempts"]
        if not isinstance(attempts, list) or not 1 <= len(attempts) <= 32:
            raise ValueError("executed run requires 1 to 32 chronological attempts")
        for number, attempt in enumerate(attempts, 1):
            attempt_data(attempt, kind)
            if attempt["outcome"] == "accepted" and number != len(attempts):
                raise ValueError("retry after acceptance is not a comparable run")
        imported[run_id] = observed
    return imported


def measures(items, group, metric_names):
    output = {}
    for name in metric_names:
        vals = [attempt[group][name] for attempt in items
                if attempt[group] is not None and attempt[group][name] is not None]
        output[name] = {"observed": len(vals), "sum": sum(vals) if vals else None}
    return output


def query_stats(attempts):
    stats = {}
    for temperature in ("cold", "warm"):
        buckets = {}
        for attempt in attempts:
            for query in attempt["queries"]:
                if query["temperature"] == temperature:
                    buckets.setdefault(query["operation"], []).append(query)
        stats[temperature] = {}
        for operation, records in buckets.items():
            times = (q["elapsed_ms"] for q in records if q["outcome"] == "ok")
            fresh = total_latency(q["freshness_ms"] for q in records if q["outcome"] == "ok" and q["freshness_ms"] is not None)
            pending = [q["required_pending"] for q in records if q["required_pending"] is not None]
            stats[temperature][operation] = {
                "outcomes": {status: sum(q["outcome"] == status for q in records) for status in QUERY_OUTCOMES},
                "latency_ms": total_latency(times), "freshness_ms": fresh,
                "pending_observed": len(pending), "pending_nonzero": sum(n > 0 for n in pending),
            }
    return stats


def total_latency(times):
    times = sorted(times)
    return {"samples": len(times), "p50": median(times) if times else None,
            "p95": times[(95 * len(times) + 99) // 100 - 1] if times else None}


def cohort(entries, found):
    reports = [found[entry["id"]] for entry in entries if entry["id"] in found]
    attempts = [attempt for result in reports for attempt in result["attempts"]]
    review = [attempt["review"] for attempt in attempts if attempt["review"] is not None]
    outcomes = {status: sum(result["attempts"][-1]["outcome"] == status for result in reports)
                for status in OUTCOMES}
    attempts_by_outcome = {status: sum(attempt["outcome"] == status for attempt in attempts) for status in OUTCOMES}
    tokens = measures(attempts, "usage", USAGE)
    reporting_complete = len(reports) == len(entries)
    usage_complete = (bool(attempts) and tokens["input_tokens"]["observed"] == len(attempts)
                      and tokens["output_tokens"]["observed"] == len(attempts))
    total_input = tokens["input_tokens"]["sum"]
    total_output = tokens["output_tokens"]["sum"]
    cached = tokens["cached_input_tokens"]
    count_accepted = outcomes["accepted"]
    measured_runs = len(reports)
    return {
        "planned": len(entries), "reported": measured_runs, "unreported": len(entries) - measured_runs,
        "terminal_outcomes": outcomes, "attempt_outcomes": attempts_by_outcome,
        "reviewed_attempts": len(review), "unreviewed_attempts": len(attempts) - len(review),
        "critical_omissions": sum(r["critical_omissions"] for r in review) if review else None,
        "false_edits": sum(r["false_edits"] for r in review) if review else None,
        "completion_rate_among_reported": count_accepted / measured_runs if measured_runs else None,
        "usage": tokens, "usage_complete_for_reported": usage_complete,
        "uncached_input_tokens": total_input - cached["sum"] if usage_complete and cached["observed"] == len(attempts) else None,
        "tokens_per_accepted": ((total_input + total_output) / count_accepted
                                if reporting_complete and usage_complete and count_accepted else None),
        "context_proxy": measures(attempts, "context_proxy", PROXIES),
        "index": measures(attempts, "index", INDEX), "query": query_stats(attempts),
    }


def paired_savings(tasks, repeats, found):
    reasons = dict.fromkeys(("missing_run", "unsuccessful", "missing_usage", "zero_baseline"), 0)
    savings = []
    for task in tasks:
        task_id = task["id"]
        pairs = []
        for repeat in range(1, repeats + 1):
            baseline, context = (found.get(f"{task_id}-{arm}{repeat}") for arm in ("A", "C"))
            if baseline is None or context is None:
                reasons["missing_run"] += 1
                continue
            a, c = baseline["attempts"], context["attempts"]
            if a[-1]["outcome"] != "accepted" or c[-1]["outcome"] != "accepted":
                reasons["unsuccessful"] += 1
                continue
            if any(attempt["usage"] is None for attempt in a + c):
                reasons["missing_usage"] += 1
                continue
            a_tokens, c_tokens = (sum(t["usage"]["input_tokens"] for t in attempts) for attempts in (a, c))
            if not a_tokens:
                reasons["zero_baseline"] += 1
                continue
            pairs.append({"repeat": repeat, "a_input_tokens": a_tokens, "c_input_tokens": c_tokens})
        if pairs:
            a_total = sum(pair["a_input_tokens"] for pair in pairs)
            c_total = sum(pair["c_input_tokens"] for pair in pairs)
            savings.append({"task": task_id, "pairs": pairs, "fraction": (a_total - c_total) / a_total})
    return {"scope": "A and C both accepted with complete paired input counters including retries",
            "ineligible_pairs": reasons, "reported_tasks": len(savings),
            "median_fraction": median(row["fraction"] for row in savings) if savings else None,
            "per_task": savings}


def render_report(plan, supplied):
    found = import_runs(plan, supplied)
    provenance = supplied["usage_source"]
    return {
        "schema_version": 1, "plan_sha256": plan["plan_sha256"],
        "corpus_kind": plan["corpus_kind"], "usage_source": provenance,
        "settings": plan["settings"], "seed": plan["seed"], "repeats": plan["repeats"],
        "cohorts": {arm: cohort([entry for entry in plan["runs"] if entry["arm"] == arm], found)
                    for arm in plan["arms"]},
        "paired_input_savings": paired_savings(plan["tasks"], plan["repeats"], found),
        "runs": [{"run_id": entry["id"], "state": "reported", "arm": entry["arm"],
                  "task": entry["task"], "repeat": entry["repeat"], **found[entry["id"]]}
                 if entry["id"] in found else
                 {"run_id": entry["id"], "arm": entry["arm"], "task": entry["task"],
                  "repeat": entry["repeat"], "state": "unreported"} for entry in plan["runs"]],
    }
