"""Broker-owned CLI process and browser driver for the packaged AppShell."""
from hashlib import sha256
from pathlib import Path
import json
import os
import re
import selectors
import signal
import subprocess


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
        for symbol, path in [("Show", "a.go"), ("Read", "b.go")]:
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
