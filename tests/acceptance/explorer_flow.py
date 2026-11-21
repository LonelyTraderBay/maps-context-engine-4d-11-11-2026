"""Check the packaged static flow against an authored, protected fixture."""
from hashlib import sha256
import json
import os
from pathlib import Path
import re
import selectors
import signal
import subprocess

SOURCE = """package demo
import ("database/sql"; "net/http")
var Database *sql.DB
func Routes(m *http.ServeMux) {
 m.HandleFunc("POST /orders", PlaceOrders)
}
func PlaceOrders(http.ResponseWriter, *http.Request) {
 if denied() { return }
 SaveOrders()
}
func denied() bool { return false }
func SaveOrders() {
 tx, err := Database.Begin()
 if err != nil { return }
 tx.Exec("INSERT INTO orders (quantity) VALUES (?)", 1)
 tx.Exec("UPDATE orders SET quantity = ?", 2)
 tx.Rollback()
}
"""
SCHEMA = "CREATE TABLE orders (quantity INT NOT NULL) ENGINE=InnoDB;\n"
PROFILE = {"schema_version": 1, "sql_profile": {"dialect": "mysql", "version": "8.4", "engine": "innodb",
           "sql_mode": "default", "environment": "fixture", "namespace": "orders", "schema": "sales"},
           "schema_paths": ["schema.sql"]}

def prepare():
    return {"flow.go": SOURCE, "schema.sql": SCHEMA, "mce4d-flow.json": json.dumps(PROFILE) + "\n"}

def exercise(root, setup, worktree, profile, active):
    subprocess.run(["/usr/sbin/ip", "link", "set", "lo", "up"], check=True, timeout=5)
    console = subprocess.Popen(["/mce4d", "explorer", "--worktree-id", worktree, "--profile-id", profile,
                                "--listen", "127.0.0.1:0"], preexec_fn=setup(1000), env={
        "PATH": "/toolchain/bin", "HOME": "/profiles/runtime/home", "GOMAXPROCS": "4", "GOTELEMETRY": "off",
        "MODERNC_SQLITE_OFD_LOCK": "0", "GOTOOLCHAIN": "local",
    }, stdout=subprocess.PIPE, stderr=subprocess.PIPE, bufsize=0)
    active.append(console)
    try:
        with selectors.DefaultSelector() as selector:
            selector.register(console.stdout, selectors.EVENT_READ)
            raw = bytearray()
            while raw.count(b"\n") < 2:
                assert selector.select(15), "Explorer startup timed out"
                part = os.read(console.stdout.fileno(), 256)
                assert part and len(raw) + len(part) < 512, "Explorer startup failed"
                raw.extend(part)
        origin_line, code_line = raw.decode().splitlines()
        assert re.fullmatch(r"Explorer http://127\.0\.0\.1:\d+/", origin_line)
        assert re.fullmatch(r"Bootstrap code: [A-Za-z0-9_-]{43}", code_line)
        tmp = os.environ["MCE4D_T083_BROWSER_TMP"]
        data = {"origin": origin_line.removeprefix("Explorer "), "code": code_line.removeprefix("Bootstrap code: "),
                "schema": SCHEMA.strip().removesuffix(";"), "schema_hash": sha256(SCHEMA.encode()).hexdigest()}
        child = subprocess.run([os.environ["MCE4D_T083_NODE"], str(Path(__file__).with_suffix(".browser.mjs"))],
            input=json.dumps(data), text=True, capture_output=True, timeout=100, user=1000, group=1000, extra_groups=[], env={
                "PATH": "/usr/bin:/bin", "HOME": tmp, "TMPDIR": tmp,
                "LD_LIBRARY_PATH": os.environ["MCE4D_T083_BROWSER_LIBS"],
                "MCE4D_T083_CHROMIUM": os.environ["MCE4D_T083_CHROMIUM"],
            })
        assert child.returncode == 0, child.stderr
        proof = json.loads(child.stdout)
        assert proof["status"] == "PASS" and proof["case"] == "T097_NATIVE_BROWSER_FLOW"
        print(json.dumps(proof), flush=True)
    finally:
        if console.poll() is None:
            console.send_signal(signal.SIGINT)
        console.wait(timeout=8)
    assert console.returncode == 0
    for name, raw in prepare().items():
        assert (root / "target" / name).read_text() == raw
