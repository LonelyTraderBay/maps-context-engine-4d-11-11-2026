"""Check a fresh S18 run or the bundled Windows native usage receipts."""

import argparse
import hashlib
import json
from pathlib import Path
from statistics import median

from edit_cases import CASES

FIXTURE = Path(__file__).resolve().parent / "fixture"


def read(path):
    return json.loads(path.read_bytes())


def summarize(measurements):
    rows = {(row["case"], row["arm"]): row for row in measurements}
    expected = {(case["id"], arm) for case in CASES for arm in ("A", "C")}
    if len(measurements) != len(expected) or set(rows) != expected:
        raise ValueError("expected exactly one A and one C receipt for all 30 cases")

    arms = {}
    for arm in ("A", "C"):
        samples = [rows[case["id"], arm] for case in CASES]
        usage = [sample["usage"] for sample in samples]
        tokens = sum(item["input_tokens"] for item in usage) if all(usage) else None
        accepted = sum(sample["outcome"] == "accepted" for sample in samples)
        arms[arm] = {
            "accepted": accepted,
            "input_tokens": tokens,
            "output_tokens": sum(item["output_tokens"] for item in usage) if all(usage) else None,
            "cached_input_tokens": sum(item["cached_input_tokens"] for item in usage) if all(usage) else None,
            "input_tokens_per_accepted": tokens / accepted if tokens is not None and accepted else None,
            "false_edit_runs": sum(bool(sample["false_edits"]) for sample in samples),
            "shell_calls": sum(sample["shell_count"] for sample in samples),
            "tool_calls": sum(len(sample["mcp_calls"]) for sample in samples),
            "usage_receipts": sum(item is not None for item in usage),
        }

    pairs = []
    for case in CASES:
        a, c = (rows[case["id"], arm] for arm in ("A", "C"))
        usage_a, usage_c = a["usage"], c["usage"]
        ratio = total_ratio = None
        if usage_a and usage_c and usage_a["input_tokens"] > 0:
            ratio = usage_c["input_tokens"] / usage_a["input_tokens"]
            total_ratio = (usage_c["input_tokens"] + usage_c["output_tokens"]) / (usage_a["input_tokens"] + usage_a["output_tokens"])
        comparable = a["outcome"] == c["outcome"] == "accepted" and ratio is not None
        pairs.append({
            "case": case["id"],
            "A": a["outcome"], "C": c["outcome"],
            "C_over_A_input": ratio, "C_over_A_total": total_ratio,
            "A_tool_calls": len(a["mcp_calls"]), "C_tool_calls": len(c["mcp_calls"]),
            "accepted_under_85": comparable and ratio < .85 and total_ratio < .85,
        })
    ratios = [pair["C_over_A_input"] for pair in pairs if pair["A"] == pair["C"] == "accepted" and pair["C_over_A_input"] is not None]
    acceptable = sum(pair["accepted_under_85"] for pair in pairs)
    retrieval_failures = sum(
        row["arm"] == "C" and not (row["discovery"] or {}).get("ok")
        for row in measurements
    )
    return {
        "pairs_under_85": acceptable, "attempted_pairs": len(CASES),
        "median_C_over_A_input": median(ratios) if ratios else None,
        "max_C_over_A_input": max(ratios) if ratios else None,
        "retrieval_failures": retrieval_failures,
        "passed": acceptable == len(CASES) and not retrieval_failures
            and all(arm["shell_calls"] == 0 and arm["false_edit_runs"] == 0 for arm in arms.values()),
        "arms": arms, "pairs": pairs,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    selection = parser.add_mutually_exclusive_group(required=True)
    selection.add_argument("--recorded", action="store_true")
    selection.add_argument("--output", type=Path)
    opts = parser.parse_args()
    if opts.recorded:
        observed = read(FIXTURE / "observed-2026-11-25.json")
        for name, key in (("sources.json", "fixture_lock_sha256"), ("maps-evidence.json", "captured_excerpts_sha256")):
            if hashlib.sha256((FIXTURE / name).read_bytes()).hexdigest() != observed[key]:
                parser.error(f"Recorded fixture digest mismatch: {name}")
        mode = "recorded_live"
        measurements = observed["measurements"]
    else:
        mode = read(opts.output / "run-config.json")["mode"]
        measurements = [read(path) for path in sorted(opts.output.glob("runs/*/measurement.json"))]
    result = summarize(measurements)
    print(json.dumps({"mode": mode, **result}, indent=2))
    return 0 if result["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
