"""Broker-owned CLI process and browser driver for the packaged AppShell."""
from hashlib import sha256
from pathlib import Path
import json
import os
import re
import selectors
import signal
import subprocess


def prepare_real_scope(root, implementation):
    commit = "e17afb4e710cc0a469315e3b1e499d5028c80cfb"
    tree = "0d4be79bd4381792765abb04c9947368d9980881"
    git = ["/usr/bin/git", "-c", "safe.directory=" + str(implementation), "-C", str(implementation)]
    assert subprocess.check_output(git + ["rev-parse", commit + "^{tree}"], timeout=5).decode().strip() == tree
    paths = ["go.mod", "go.sum", "internal/model/canonical.go", "internal/model/canonical_test.go",
             "web/explorer/src/shell/App.tsx"]
    hashes = {}
    for path in paths:
        raw = subprocess.check_output(git + ["cat-file", "blob", commit + ":" + path], timeout=5)
        local = root / "target" / path
        local.parent.mkdir(parents=True, exist_ok=True)
        local.write_bytes(raw)
        hashes[path] = sha256(raw).hexdigest()
    proof = {"case": "T094_REAL_SCOPE", "commit": commit, "tree": tree, "paths_sha256": hashes,
             "approved_scope": "read-only static index and source", "live_dev": "NOT_RUN"}
    print(json.dumps(proof), flush=True)


def exercise_shell(root, setup, worktree, profile, active, scenario, *, gu0=False):
    console = subprocess.Popen([
        "/mce4d", "explorer", "--worktree-id", worktree, "--profile-id", profile, "--listen", "127.0.0.1:0",
    ], preexec_fn=setup(1000), env={
        "PATH": "/toolchain/bin", "HOME": "/profiles/runtime/home", "GOMAXPROCS": "4", "GOTELEMETRY": "off",
        "MODERNC_SQLITE_OFD_LOCK": "0", "GOTOOLCHAIN": "local",
    }, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, bufsize=0)
    active.append(console)
    try:
        with selectors.DefaultSelector() as selector:
            selector.register(console.stdout, selectors.EVENT_READ)
            raw = bytearray()
            while raw.count(b"\n") < 2:
                assert selector.select(15), "Explorer bootstrap timeout"
                piece = os.read(console.stdout.fileno(), 256)
                assert piece and len(raw)+len(piece) < 512, "Explorer startup failed"
                raw.extend(piece)
        origin_line, code_line = raw.decode().splitlines()
        assert re.fullmatch(r"Explorer http://127\.0\.0\.1:\d+/", origin_line)
        assert re.fullmatch(r"Bootstrap code: [A-Za-z0-9_-]{43}", code_line)
        engine = origin_line.removeprefix("Explorer ")
        code = code_line.removeprefix("Bootstrap code: ")
        expected = {}
        symbols = [("Show", "a.go"), ("Read", "b.go")]
        if scenario == "benchmark":
            symbols = [(name, "internal/model/canonical.go") for name in ("CanonicalNull", "CanonicalBool")]
        for symbol, path in symbols:
            raw = (root/"target"/path).read_bytes()
            start = raw.index(("func "+symbol).encode())+len(b"func ")
            expected[symbol] = {"path": path, "hash": sha256(raw).hexdigest(), "start": start,
                                "end": start+len(symbol.encode()), "text": symbol}
        node = os.environ["MCE4D_T083_NODE"]
        tmp = os.environ["MCE4D_T083_BROWSER_TMP"]
        cmd = [node,str(Path(__file__).with_name("explorer_shell.mjs")),scenario]
        data = json.dumps({"origin": engine, "code": code, "expected": expected, "gu0": gu0})
        result = subprocess.run(cmd, input=data, text=True, capture_output=True, timeout=80,
                                user=1000, group=1000, extra_groups=[], env={
            "HOME": tmp, "TMPDIR": tmp, "PATH": "/usr/bin:/bin",
            "LD_LIBRARY_PATH": os.environ["MCE4D_T083_BROWSER_LIBS"],
            "MCE4D_T083_CHROMIUM": os.environ["MCE4D_T083_CHROMIUM"],
            "MCE4D_T083_EVIDENCE": os.environ["MCE4D_T083_EVIDENCE"],
        })
        if result.stdout.strip():
            print(result.stdout.strip(), flush=True)
        if result.returncode != 0:
            print(result.stderr, flush=True)
            raise AssertionError("Packaged AppShell browser check failed")
    finally:
        if console.poll() is None:
            console.send_signal(signal.SIGINT)
        console.wait(timeout=8)
    assert console.returncode == 0, "Explorer did not stop cleanly"
