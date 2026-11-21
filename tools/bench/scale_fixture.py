"""Deterministic Go workloads for the local C053 scale fixtures."""
from pathlib import Path

COHORT_FILES = {"SQuota": 16, "S16": 16, "S": 128, "S800": 800, "M": 10_000, "L": 100_000, "XL150K": 150_000}
FANOUT = 1025


def write_fixture(target: Path, cohort: str) -> dict:
    count = COHORT_FILES[cohort]
    files = source_bytes = 0

    def write(relative, contents):
        nonlocal files, source_bytes
        body = contents.encode("ascii")
        path = target / relative
        path.parent.mkdir(exist_ok=True)
        with path.open("xb") as output:
            output.write(body)
        files += 1
        source_bytes += len(body)

    write("go.mod", "module example.invalid/scalefixture\n\ngo 1.27\n")
    declarations = ["package scale", "func FanoutAnchor() int { return 7 }"]
    declarations.extend(f"func Caller{i:04d}() int {{ return FanoutAnchor() }}" for i in range(FANOUT))
    write("fanout.go", "\n".join(declarations) + "\n")
    for i in range(count - 2):
        package = f"p{i // 25:04d}"
        write(f"{package}/x{i:06d}.go", f"package {package}\nfunc Value{i:06d}() int {{ return {i} }}\n")
    return {"cohort": cohort, "files": files, "source_bytes": source_bytes,
            "packages": (count - 3) // 25 + 2, "fanout_expected": FANOUT}
