"""Packaged Explorer and a synthetic SDK on separate private loopback origins."""

from datetime import datetime, timedelta, timezone
from hashlib import sha256
import html
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import re
import selectors
import signal
import subprocess
from threading import Thread


def exercise(root, command, setup, worktree, profile, artifact, active, *, interact=False, pick=False, reverse=False,
             hmr_maps=(), records_mode=False, context_mode=False, benchmark=False):
    path, build = artifact
    imported = command("/mce4d", ["explorer-map", "import", "--worktree-id", worktree, "--manifest", path])
    assert imported.returncode == 0 and json.loads(imported.stdout)["status"] == "bound"
    fixture_js = Path(os.environ["MCE4D_T087_PEER"]).read_bytes()
    assert 0 < len(fixture_js) <= 2 << 20
    explorer_origin = ""
    current_build = build
    build_manifest = json.loads((root / path.lstrip("/")).read_bytes())
    anchor = next(item["anchor_id"] for item in build_manifest["anchors"] if item["kind"] == "element_site")
    expected = None
    if benchmark:
        source = (root / "target/entry.tsx").read_bytes()
        start = source.index(b"<button")
        assert source.count(b"<button") == 1
        expected = {"path": "entry.tsx", "content_hash": sha256(source).hexdigest(),
                    "range": {"start_byte": start, "end_byte": start + len(b"<button")}, "text": "<button"}
    anchors = ','.join(item["anchor_id"] for item in build_manifest["anchors"])
    scroll = reverse and os.environ.get("MCE4D_T090_SCROLL") == "1"
    if reverse:
        assert sum(item["kind"] == "component_render_site" for item in build_manifest["anchors"]) == 105
    buttons = "".join(f'<button id="fixture-action{index or ""}" data-mce4d-anchor="{anchor}" type="submit">'
                      f'{index + 1 if pick else "Fixture action"}</button>' for index in range(100 if pick else 1))
    layout = ('<style>form{display:grid;grid-template-columns:repeat(10,32px);gap:2px}button{height:18px}'
              + ('#fixture-action99{margin-top:420px}' if reverse else '') + '</style>' if pick else "")

    action_requests = []

    class Peer(BaseHTTPRequestHandler):
        def do_POST(self):
            if self.path != "/fixture-action":
                self.send_error(404)
                return
            action_requests.append(self.path)
            self.send_response(204)
            self.end_headers()

        def do_GET(self):
            if (self.path == "/app" or hmr_maps and self.path == "/app/next") and explorer_origin:
                body = (f'<!doctype html><html data-parent-origin="{html.escape(explorer_origin)}" data-build-map="{current_build}"'
                        f' data-anchor="{anchor}" data-anchors="{anchors}" data-scroll="{str(reverse).lower()}">'
                        '<meta charset="utf-8"><title>Synthetic Preview</title><main>SDK fixture</main>'
                        f'{layout}<form id="fixture-form">{buttons}</form>'
                        '<script type="module" src="/peer.js"></script></html>').encode()
                mime = "text/html; charset=utf-8"
            elif self.path == "/peer.js":
                body, mime = fixture_js, "text/javascript"
            else:
                self.send_error(404)
                return
            self.send_response(200)
            self.send_header("Content-Type", mime)
            self.send_header("Cache-Control", "no-store")
            self.send_header("Content-Security-Policy",
                             f"default-src 'none'; script-src 'self'; style-src 'unsafe-inline'; connect-src 'self'; frame-ancestors {explorer_origin}")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, *_):
            pass

    server = ThreadingHTTPServer(("127.0.0.1", 0), Peer)
    server.daemon_threads = True
    thread = Thread(target=server.serve_forever, daemon=True)
    thread.start()
    peer_origin = f"http://127.0.0.1:{server.server_port}"
    scenario = "fixture-local-preview"
    install = json.loads((root / "profiles/runtime/profile.json").read_bytes())
    now = datetime.now(timezone.utc)
    permit = {
        "schema_version": 1, "profile_id": scenario, "approved_origin": peer_origin, "approved_path_prefix": "/app",
        "target_environment": "isolated_test" if interact else "local_mock", "source_worktree_ids": [worktree], "allow_attach": True,
        "allow_interact": interact, "allow_scroll": scroll, "allowed_dev_destinations": [],
        "attestation_id": "synthetic-local-preview", "attested_by": "installation:" + install["profile_id"],
        "attested_at": (now - timedelta(seconds=2)).isoformat(), "expires_at": (now + timedelta(minutes=3)).isoformat(), "version": 1,
    }
    console = None
    driver = None
    try:
        create = command("/mce4d", ["preview-permission", "put", "--input-json", "-"],
                         payload={"action": "put", "profile": permit, "expected_version": 0})
        assert create.returncode == 0, create.stderr
        assert json.loads(create.stdout)["data"]["profiles"][0]["effective_status"] == "approved"
        conflict = command("/mce4d", ["preview-permission", "put", "--input-json", "-"],
                           payload={"action": "put", "profile": permit, "expected_version": 0})
        assert conflict.returncode == 3 and json.loads(conflict.stdout)["error"]["code"] == "RECORD_CONFLICT"
        listed = command("/mce4d", ["preview-permission", "list", "--input-json", "-"], payload={"action": "list"})
        assert listed.returncode == 0 and json.loads(listed.stdout)["data"]["profiles"][0]["version"] == 1
        console = subprocess.Popen([
            "/mce4d", "explorer", "--worktree-id", worktree, "--profile-id", profile, "--listen", "127.0.0.1:0",
        ], preexec_fn=setup(1000), env={"PATH": "/toolchain/bin", "HOME": "/profiles/runtime/home",
                                        "MODERNC_SQLITE_OFD_LOCK": "0", "GOMAXPROCS": "4", "GOTELEMETRY": "off"},
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, bufsize=0)
        active.append(console)
        with selectors.DefaultSelector() as select:
            select.register(console.stdout, selectors.EVENT_READ)
            raw = bytearray()
            while raw.count(b"\n") < 2:
                assert select.select(15), "Explorer bootstrap timeout"
                piece = os.read(console.stdout.fileno(), 256)
                assert piece and len(raw) + len(piece) < 512, "Explorer failed before bootstrap"
                raw.extend(piece)
        origin_line, code_line = raw.decode().splitlines()
        assert re.fullmatch(r"Explorer http://127\.0\.0\.1:\d+/", origin_line)
        assert re.fullmatch(r"Bootstrap code: [A-Za-z0-9_-]{43}", code_line)
        explorer_origin = origin_line.removeprefix("Explorer ").rstrip("/")
        script = Path(__file__).with_name("preview_pairing.browser.mjs")
        browser_tmp = os.environ["MCE4D_T087_TMP"]
        driver = subprocess.Popen([os.environ["MCE4D_T085_NODE"], script],
                                  user=1000, group=1000, extra_groups=[], bufsize=0,
                                  stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                  env={"PATH": "/usr/bin:/bin", "HOME": browser_tmp, "TMPDIR": browser_tmp,
                                       "LD_LIBRARY_PATH": os.environ["MCE4D_T086_LIBS"],
                                       "MCE4D_T086_CHROMIUM": os.environ["MCE4D_T086_CHROMIUM"]})
        active.append(driver)
        driver.stdin.write((json.dumps({"explorer": explorer_origin, "peer": peer_origin + "/app", "build": build, "interact": interact, "pick": pick, "anchor": anchor,
                                        "reverse": reverse, "scroll": scroll, "match_count": len(build_manifest["anchors"]),
                                        "hmr": hmr_maps[-1][1] if hmr_maps else None,
                                        "records": records_mode,
                                        "context_case": context_mode, "benchmark": expected,
                                        "permission": scenario, "code": code_line.removeprefix("Bootstrap code: ")}) + "\n").encode())
        with selectors.DefaultSelector() as select:
            select.register(driver.stdout, selectors.EVENT_READ)
            assert select.select(80 if benchmark or interact else 50 if reverse else 30), "Browser did not complete preview exercise"
            line = driver.stdout.readline()
        if not line:
            raise AssertionError(driver.stderr.read(8192).decode())
        first = json.loads(line)
        if context_mode:
            assert first["case"] == "T093_CONTEXT_PAGE" and first["status"] == "PASS", first
            print(json.dumps(first), flush=True)
            expire_fixture_generation(command, worktree, profile)
            driver.stdin.write(b"context-gc\n")
            with selectors.DefaultSelector() as select:
                select.register(driver.stdout, selectors.EVENT_READ)
                assert select.select(20), "Missing context expiry result"
                line = driver.stdout.readline()
            if not line:
                raise AssertionError(driver.stderr.read(8192).decode())
            first = json.loads(line)
        if records_mode:
            exercise_records(root, command, worktree, profile, driver, first)
            return
        if hmr_maps:
            assert first["case"] == "T091_AWAITING_MAP" and first["status"] == "PASS", first
            print(json.dumps(first), flush=True)
            import native_ui_watch
            native_ui_watch.exercise(root, setup, worktree, profile, hmr_maps)
            current_build = hmr_maps[-1][1]
            driver.stdin.write(b"map-ready\n")
            with selectors.DefaultSelector() as select:
                select.register(driver.stdout, selectors.EVENT_READ)
                assert select.select(30), "Replacement preview did not complete"
                line = driver.stdout.readline()
            if not line:
                raise AssertionError(driver.stderr.read(8192).decode())
            first = json.loads(line)
        case = "T094_PREVIEW_LATENCY" if benchmark else "T093_CONTEXT_EXPIRED" if context_mode else "T091_BROWSER_HMR" if hmr_maps else "T090_BROWSER_REVERSE" if reverse else "T089_BROWSER_PICK" if pick else "T088_BROWSER_INTERACT" if interact else "T087_BROWSER_BOUND"
        assert first["case"] == case and first["status"] == "PASS", first
        print(json.dumps(first), flush=True)
        if benchmark:
            status = (root / "proc" / str(console.pid) / "status").read_text()
            memory = re.search(r"(?m)^VmHWM:\s+(\d+) kB$", status)
            assert memory is not None and int(memory[1]) > 0
            print(json.dumps({"case": "T094_EXPLORER_MEMORY", "peak_rss_kib": int(memory[1]),
                              "scope": "packaged Explorer engine process"}), flush=True)
        revoke = command("/mce4d", ["preview-permission", "revoke", "--input-json", "-"], payload={
            "action": "revoke", "profile_id": scenario, "expected_version": 1, "reason": "Synthetic owner ended this fixture"})
        assert revoke.returncode == 0 and json.loads(revoke.stdout)["data"]["profiles"][0]["effective_status"] == "revoked"
        driver.stdin.write(b"revoked\n")
        tail, errors = driver.communicate(timeout=22)
        assert driver.returncode == 0, errors.decode()[:8192]
        assert json.loads(tail)["case"] == ("T089_BROWSER_REVOKED" if pick else "T088_BROWSER_REVOKED" if interact else "T087_BROWSER_REVOKED")
        print(tail.decode().strip(), flush=True)
        read = command("/mce4d", ["preview-permission", "get", "--input-json", "-"], payload={"action": "get", "profile_id": scenario})
        assert read.returncode == 0 and json.loads(read.stdout)["data"]["profiles"][0]["version"] == 2
    finally:
        if driver is not None and driver.poll() is None:
            driver.stdin.close()
            driver.terminate()
            try:
                driver.wait(timeout=4)
            except subprocess.TimeoutExpired:
                driver.kill()
                driver.wait(timeout=3)
        if console is not None and console.poll() is None:
            console.send_signal(signal.SIGINT)
            console.wait(timeout=8)
        server.shutdown()
        server.server_close()
        thread.join(timeout=4)
    assert console is not None and console.returncode == 0
    assert len(action_requests) == int(interact)
    print(json.dumps({"case": "T089_PREVIEW_PRODUCT" if pick else "T088_PREVIEW_PRODUCT" if interact else "T087_PREVIEW_CLI_AND_PRODUCT",
                      "status": "PASS", "target_executions": 0, "fixture_action_network_requests": len(action_requests)}), flush=True)


def exercise_records(root, command, worktree, profile, driver, first):
    assert first["case"] == "T092_RECORDS_REVIEWED" and first["status"] == "PASS", first
    print(json.dumps(first), flush=True)
    changed = root / "target/b.go"
    original = changed.read_bytes()
    try:
        changed.write_bytes(original + b"\n// intentional synthetic source drift\n")
        driver.stdin.write(b"source-drift\n")
        with selectors.DefaultSelector() as select:
            select.register(driver.stdout, selectors.EVENT_READ)
            assert select.select(20), "Missing source drift result"
            line = driver.stdout.readline()
        if not line:
            raise AssertionError(driver.stderr.read(8192).decode())
        drift = json.loads(line)
        assert drift["case"] == "T092_RECORDS_DRIFT" and drift["status"] == "PASS", drift
        print(json.dumps(drift), flush=True)
        expire_fixture_generation(command, worktree, profile)
    finally:
        changed.write_bytes(original)
    driver.stdin.write(b"gc-complete\n")
    tail, errors = driver.communicate(timeout=25)
    assert driver.returncode == 0, errors.decode()[:8192]
    assert json.loads(tail)["case"] == "T092_RECORDS_GC"
    print(tail.decode().strip(), flush=True)


def expire_fixture_generation(command, worktree, profile):
    binding = ["--worktree-id", worktree, "--profile-id", profile]
    for _ in range(3):
        assert command("/mce4d", ["index", *binding, "--full"]).returncode == 0
    report = command("/mce4d", ["store", "gc", *binding, "--apply", "--confirm"])
    assert report.returncode == 0 and any(item["applied"] for item in json.loads(report.stdout)["generations"])
