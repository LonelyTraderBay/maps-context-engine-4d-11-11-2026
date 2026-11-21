import argparse
import hashlib
import json
import os
from pathlib import Path
import queue
import subprocess
import sys
import threading
import time
import uuid

parser = argparse.ArgumentParser(description="Exercise an installed Windows Codex against the sealed MCP fixture.")
parser.add_argument("--codex", type=Path, required=True)
parser.add_argument("--codex-sha256", required=True)
parser.add_argument("--mcp-fixture", type=Path, required=True)
parser.add_argument("--mcp-sha256", required=True)
parser.add_argument("--output", type=Path, required=True)
parser.add_argument("--profile-root", type=Path, help="Disposable protected profile prepared by the packaged test")
parser.add_argument("--worktree-id")
parser.add_argument("--profile-id")
parser.add_argument("--go-root", type=Path)
args = parser.parse_args()
if os.name != "nt" or not all(path.is_absolute() for path in (args.codex, args.mcp_fixture, args.output)):
    parser.error("Windows and explicit absolute material and output paths are required")
codex = args.codex.resolve(strict=True)
server = args.mcp_fixture.resolve(strict=True)

def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()

codex_hash, server_hash = sha(codex), sha(server)
if codex_hash != args.codex_sha256 or server_hash != args.mcp_sha256:
    parser.error("fixture material hash differs from the supplied approval")
profile = (args.profile_root, args.worktree_id, args.profile_id, args.go_root)
product = any(profile)
if product and (not all(profile) or not args.profile_root.is_absolute() or not args.go_root.is_absolute()):
    parser.error("the packaged flow requires all four scope arguments and absolute profile and Go roots")
if product:
    request = json.loads(sys.stdin.buffer.read(32769))
    if request["selector"]["path"] != "second.go":
        parser.error("expected the synthetic packaged rename fixture")
    server_args = ["mcp", "--worktree-id", args.worktree_id, "--profile-id", args.profile_id]
    server_env = {name: os.environ[name] for name in ("USERPROFILE", "APPDATA", "LOCALAPPDATA")}
    server_env.update(MCE4D_HOME=str(args.profile_root.resolve(strict=True)), MCE4D_WINDOWS_ROLE_IPC="1",
                      PATH=str(args.go_root / "bin"), PATHEXT=".EXE")
    server_setting = (',args=' + json.dumps(server_args) + ',env={' +
                      ','.join(name + '=' + json.dumps(value) for name, value in server_env.items()) +
                      '},startup_timeout_sec=15,tool_timeout_sec=80}')
else:
    server_setting = (',args=["-test.run=^TestStdioProcessUsesOnlyProtocolFrames$"],'
                      'env_vars=["MCE4D_TEST_STDIO_HELPER"],startup_timeout_sec=15,tool_timeout_sec=15}')
run = args.output.resolve(strict=True) / ("codex-" + uuid.uuid4().hex)
run.mkdir()
home, cwd = run / "home", run / "workspace"
home.mkdir()
cwd.mkdir()
real_config = Path(os.environ["USERPROFILE"]) / ".codex/config.toml"
config_before = sha(real_config) if real_config.exists() else None
env = {name: os.environ[name] for name in ("SystemRoot", "SystemDrive", "ComSpec", "OS", "PATH", "PATHEXT") if name in os.environ}
env.update(CODEX_HOME=str(home), USERPROFILE=str(home), APPDATA=str(home), LOCALAPPDATA=str(home), TEMP=str(run), TMP=str(run),
           MCE4D_TEST_STDIO_HELPER="1", CODEX_OTEL_DISABLE="1")
workspace_canary = cwd / "canary.txt"
workspace_canary.write_text("disposable Windows native MCP canary\n")
canary = sha(workspace_canary)
cmd = [str(codex), "--no-daemon",
       "-c", "analytics.enabled=false", "-c", 'model_provider="fixture"', "-c", 'model="fixture"',
       "-c", 'model_providers.fixture={name="Local fixture only",base_url="http://127.0.0.1:9/v1",wire_api="responses"}',
       "-c", "mcp_servers.mce4d_fixture={command=" + json.dumps(str(server)) +
       server_setting,
       "app-server", "--listen", "stdio://"]
record = {"scope": "installed Windows Codex with production MCP framing and synthetic backend", "status": "FAIL",
          "codex_sha256": codex_hash, "server_sha256": server_hash, "model_turns": 0, "protocol_calls": []}
if product:
    record["scope"] = "installed Windows Codex and packaged MCP with protected SQLite using synthetic source"
events = queue.Queue(maxsize=128)
process = subprocess.Popen(cmd, cwd=cwd, env=env, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                           creationflags=subprocess.CREATE_NO_WINDOW)

def read(pipe, path, protocol):
    size = 0
    try:
        with path.open("wb") as log:
            while line := pipe.readline(262145):
                size += len(line)
                if size > 2097152 or len(line) > 262144:
                    raise RuntimeError("output cap reached")
                log.write(line)
                if protocol:
                    events.put(json.loads(line), timeout=1)
    except BaseException as error:
        if protocol:
            try:
                events.put(error, timeout=1)
            except queue.Full:
                pass

threads = [threading.Thread(target=read, args=(pipe, run / name, protocol), daemon=True)
           for pipe, name, protocol in ((process.stdout, "stdout.jsonl", True), (process.stderr, "stderr.log", False))]
for thread in threads:
    thread.start()
deadline = time.monotonic() + 60

def send(message):
    process.stdin.write((json.dumps(message) + "\n").encode())
    process.stdin.flush()

def rpc(method, params):
    identifier = len(record["protocol_calls"]) + 1
    record["protocol_calls"].append(method)
    send({"id": identifier, "method": method, "params": params})
    while True:
        wait = deadline - time.monotonic()
        if wait <= 0 or process.poll() is not None and events.empty():
            raise RuntimeError("host timeout or exit in " + method)
        try:
            response = events.get(timeout=min(wait, .2))
        except queue.Empty:
            continue
        if isinstance(response, BaseException):
            raise response
        if response.get("id") == identifier and ("result" in response or "error" in response):
            if "error" in response:
                raise RuntimeError(method + ": " + json.dumps(response["error"]))
            return response["result"]
        if "id" in response and "method" in response:
            send({"id": response["id"], "error": {"code": -32601, "message": "Fixture disallows interactive actions"}})

def call_tool(thread, tool, arguments):
    response = rpc("mcpServer/tool/call", {"threadId": thread, "server": "mce4d_fixture",
                                         "tool": tool, "arguments": arguments})
    body = response.get("structuredContent")
    if body is None:
        body = json.loads(response["content"][0]["text"])
    assert not response.get("isError"), (tool, body.get("error"))
    return body

try:
    init = rpc("initialize", {"clientInfo": {"name": "mce4d_win_fixture", "version": "1"}, "capabilities": {"experimentalApi": True}})
    send({"method": "initialized"})
    thread = rpc("thread/start", {"cwd": str(cwd), "ephemeral": True, "approvalPolicy": "never",
                                  "modelProvider": "fixture", "model": "fixture", "sandbox": "read-only"})
    thread_id = thread["thread"]["id"]
    inventory = rpc("mcpServerStatus/list", {"threadId": thread_id, "serverName": "mce4d_fixture"})
    listing, = inventory["data"]
    assert listing["name"] == "mce4d_fixture"
    tools = sorted(listing["tools"])
    assert tools == ["mce4d_context_pack", "mce4d_expand_pack", "mce4d_locate", "mce4d_rename_preview",
                     "mce4d_validate_pack", "mce4d_workspace_status"]
    body = call_tool(thread_id, "mce4d_workspace_status", {})
    assert body["status"] == "ok" and body["schema_version"] == 1
    if product:
        scoped, = body["data"]["registered_scopes"]
        assert scoped == {"worktree_id": args.worktree_id, "profile_id": args.profile_id,
                          "root_status": "registered", "index_state": "published"}
        preview = call_tool(thread_id, "mce4d_rename_preview", request)["data"]
        assert preview["preview_complete"] and not preview["applied"] and preview["source_mutation_count"] == 0
        edits = preview["edits"]
        assert len(edits) == 2 and all(edit["new_text"] == request["new_name"] for edit in edits)
        verified = call_tool(thread_id, "mce4d_validate_pack", {"result_token": preview["result_token"]})
        assert verified["status"] == "ok" and verified["data"]["edit_precondition_validated"]
        record.update(registered_scope=scoped, rename_edits=len(edits), edit_precondition_validated=True)
    record.update(status="PASS", user_agent=init.get("userAgent"), tools=tools, result_status=body["status"])
except BaseException as error:
    record["diagnostic"] = str(error)
    raise
finally:
    process.stdin.close()
    try:
        process.wait(timeout=8)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=4)
        record["status"] = "FAIL"
        record["diagnostic"] = "host did not exit after stdin close"
    for thread in threads:
        thread.join(timeout=2)
    record["config_unchanged"] = config_before == (sha(real_config) if real_config.exists() else None)
    record["workspace_unchanged"] = canary == sha(workspace_canary)
    record["host_exit"] = process.returncode
    if record["host_exit"] != 0 or not record["config_unchanged"] or not record["workspace_unchanged"]:
        record["status"] = "FAIL"
    (run / "record.json").write_text(json.dumps(record, indent=2) + "\n", encoding="utf-8")
    print(run)
    print(json.dumps(record), flush=True)
if record["status"] != "PASS":
    raise SystemExit(1)
