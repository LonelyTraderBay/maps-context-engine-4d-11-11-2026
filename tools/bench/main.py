"""Plan and prepare S18 fixtures, then report locally supplied observations."""

import argparse
from hashlib import sha256
import json
import os
from pathlib import Path
import re
import subprocess
import sys

import corpus
from report import render_report


ARMS = {
    "A": "Common host search/read and available semantic tools; MCE4D disabled.",
    "B": "Same common tools plus MCE4D map and locate queries; no context pack.",
    "C": "Same common tools plus MCE4D map, locate and optimized context pack.",
}
SETTING_TEXT = ("host", "model", "model_revision", "reasoning", "permission_profile",
                "goos", "goarch", "toolchain", "machine", "filesystem")
LIMIT = 8 << 20


def canonical(value):
    return json.dumps(value, sort_keys=True, ensure_ascii=True, allow_nan=False, separators=(",", ":")).encode("ascii")


def unique_object(pairs):
    result = {}
    for name, value in pairs:
        if name in result:
            raise ValueError(f"duplicate JSON member {name}")
        result[name] = value
    return result


def read_json(path):
    with path.open("rb") as source:
        raw = source.read(LIMIT + 1)
    if len(raw) > LIMIT:
        raise ValueError("input JSON exceeds 8 MiB")
    return json.loads(raw.decode("utf-8"), object_pairs_hook=unique_object)


def checked_settings(value):
    if not isinstance(value, dict) or set(value) != {*SETTING_TEXT, "completion_budget_ms", "common_tools"}:
        raise ValueError("invalid comparison settings fields")
    if any(not isinstance(value[k], str) or not 1 <= len(value[k]) <= 160
           or not value[k].isprintable() or not value[k].strip() for k in SETTING_TEXT):
        raise ValueError("invalid comparison settings text")
    if type(value["completion_budget_ms"]) is not int or not 1 <= value["completion_budget_ms"] <= 86_400_000:
        raise ValueError("completion_budget_ms must be between 1 and 86400000")
    if value["goos"] not in ("linux", "windows") or value["goarch"] != "amd64":
        raise ValueError("plan requires a supported linux/windows amd64 profile")
    tools = value["common_tools"]
    if (not isinstance(tools, list) or not 2 <= len(tools) <= 32
            or any(not isinstance(tool, str) or not re.fullmatch(r"[a-z0-9_.-]{1,40}", tool) for tool in tools)
            or len(set(tools)) != len(tools) or not {"read", "search"} <= set(tools)
            or any("mce4d" in tool for tool in tools)):
        raise ValueError("common_tools must list search, read and any available baseline tools")
    return value


def make_plan(seed, repeats, settings):
    if type(seed) is not int or not 0 <= seed <= 2**32 - 1 or type(repeats) is not int or not 1 <= repeats <= 9:
        raise ValueError("invalid seed or repeat count")
    checked_settings(settings)
    tasks = list(corpus.tasks())
    runs = [
        {"id": f"{task['id']}-{arm}{repeat}", "task": task["id"], "arm": arm, "repeat": repeat}
        for task in tasks for repeat in range(1, repeats + 1) for arm in ARMS
    ]
    runs.sort(key=lambda run: sha256(f"{seed}/{run['id']}".encode("ascii")).digest())
    plan = {
        "schema_version": 1, "corpus": "mce4d-go-synthetic-v1", "corpus_kind": "synthetic",
        "authority": "fixtures authored for local benchmarking", "seed": seed, "repeats": repeats,
        "settings": settings, "arms": ARMS.copy(), "tasks": tasks, "runs": runs,
        "repositories": [
            {"id": name, "language": "go", "source": "tools/bench/corpus.py",
             "rationale": rationale, "starting_tree_sha256": corpus.fingerprint(corpus.files(name))}
            for name, rationale in corpus.RATIONALE.items()
        ],
    }
    return {"plan_sha256": sha256(canonical(plan)).hexdigest(), **plan}


def checked_plan(path):
    supplied = read_json(path)
    if not isinstance(supplied, dict) or not {"seed", "repeats", "settings"} <= supplied.keys():
        raise ValueError("invalid plan")
    expected = make_plan(supplied["seed"], supplied["repeats"], supplied["settings"])
    if canonical(supplied) != canonical(expected):
        raise ValueError("plan does not match its pinned corpus and settings")
    return expected


def write_json(path, value):
    with path.open("xb") as output:
        output.write(json.dumps(value, sort_keys=True, indent=2, ensure_ascii=True, allow_nan=False).encode("ascii") + b"\n")


def git_environment(home, executable):
    env = {key: os.environ[key] for key in ("SYSTEMROOT", "WINDIR", "COMSPEC") if key in os.environ}
    env.update(HOME=str(home), USERPROFILE=str(home), PATH=str(executable.parent), LANG="C",
               GIT_CONFIG_NOSYSTEM="1", GIT_CONFIG_GLOBAL=os.devnull, GIT_TERMINAL_PROMPT="0",
               GIT_AUTHOR_DATE="2001-01-01T00:00:00+00:00", GIT_COMMITTER_DATE="2001-01-01T00:00:00+00:00")
    return env


def materialize(plan, run_id, attempt, output, git, approved_hash):
    entry = next((run for run in plan["runs"] if run["id"] == run_id), None)
    if entry is None:
        raise ValueError("run is not in this plan")
    if not 1 <= attempt <= 32:
        raise ValueError("attempt ordinal must be between 1 and 32")
    if not git.is_absolute() or not re.fullmatch(r"[0-9a-f]{64}", approved_hash):
        raise ValueError("Git requires an absolute path and approved SHA-256")
    git = git.resolve(strict=True)
    if not git.is_file() or git.stat().st_size > 64 << 20 or sha256(git.read_bytes()).hexdigest() != approved_hash:
        raise ValueError("Git executable differs from the supplied approval")
    if not output.is_absolute() or not output.name or output.name == "..":
        raise ValueError("output must name a fresh absolute directory")
    output = output.parent.resolve(strict=True) / output.name
    output.mkdir()
    workspace = output / "worktree"
    home = output / "git-home"
    template = output / "empty-template"
    for directory in (workspace, home, template):
        directory.mkdir()
    env = git_environment(home, git)

    def run_git(*args):
        command = [str(git), "-c", "core.autocrlf=false", "-c", "core.filemode=false",
                   "-c", f"core.hooksPath={template}", "-c", "user.name=MCE4D fixture",
                   "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgSign=false", *args]
        return subprocess.run(command, cwd=workspace, env=env, capture_output=True, check=True, timeout=15).stdout

    task = next(task for task in plan["tasks"] if task["id"] == entry["task"])
    clean = corpus.files(task["repo"])
    start = corpus.files(task["repo"], task["variant"])
    run_git("init", "--quiet", "--object-format=sha1", "--initial-branch=main", "--template", str(template))
    for relative, body in clean.items():
        with (workspace / relative).open("xb") as source:
            source.write(body)
    run_git("add", "--all", "--")
    run_git("commit", "--quiet", "--no-gpg-sign", "-m", "Authored benchmark fixture")
    revision = run_git("rev-parse", "HEAD").decode("ascii").strip()
    for relative, body in start.items():
        if clean.get(relative) != body:
            (workspace / relative).write_bytes(body)
    state = run_git("status", "--porcelain=v1", "--untracked-files=all")
    changed = [f" M {name}\n" for name in sorted(clean) if clean[name] != start[name]]
    untracked = [f"?? {name}\n" for name in sorted(start.keys() - clean.keys())]
    expected = "".join(changed + untracked).encode("ascii")
    observed = {file.name: file.read_bytes() for file in workspace.iterdir() if file.is_file()}
    if state != expected or corpus.fingerprint(observed) != task["input_sha256"]:
        raise ValueError("fixture does not match the planned starting state")
    write_json(output / "task.json", {
        "prompt": task["prompt"], "settings": plan["settings"], "build_tags": task["build_tags"],
        "dependency_state": task["dependency_state"], "arm": entry["arm"], "tools": ARMS[entry["arm"]],
        "workspace": "worktree",
    })
    receipt = {"plan_sha256": plan["plan_sha256"], "run_id": run_id, "attempt": attempt,
               "starting_revision": revision, "input_sha256": task["input_sha256"],
               "git_sha256": approved_hash}
    write_json(output / "receipt.json", receipt)
    return receipt


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    plan = commands.add_parser("plan")
    plan.add_argument("--seed", type=int, required=True)
    plan.add_argument("--repeats", type=int, default=3)
    plan.add_argument("--settings", type=Path, required=True)
    prepare = commands.add_parser("prepare")
    prepare.add_argument("--plan", type=Path, required=True)
    prepare.add_argument("--run-id", required=True)
    prepare.add_argument("--attempt", type=int, default=1)
    prepare.add_argument("--output", type=Path, required=True)
    prepare.add_argument("--git", type=Path, required=True)
    prepare.add_argument("--git-sha256", required=True)
    report = commands.add_parser("report")
    report.add_argument("--plan", type=Path, required=True)
    report.add_argument("--observations", type=Path, required=True)
    args = parser.parse_args()
    try:
        if args.command == "plan":
            result = make_plan(args.seed, args.repeats, read_json(args.settings))
        elif args.command == "prepare":
            result = materialize(checked_plan(args.plan), args.run_id, args.attempt,
                                 args.output, args.git, args.git_sha256)
        else:
            result = render_report(checked_plan(args.plan), read_json(args.observations))
        print(json.dumps(result, sort_keys=True, indent=2, ensure_ascii=True, allow_nan=False))
    except (ValueError, OSError, OverflowError, RecursionError, subprocess.SubprocessError) as error:
        parser.exit(2, f"bench: {error}\n")


if __name__ == "__main__":
    main()
