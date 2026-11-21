"""Local synthetic native acceptance; called by a reviewed root fixture broker.

No target is discovered here. The broker supplies its fresh chroot, real CLI
launcher, explicit bound IDs and fake corpus. These callbacks launch the actual
packaged CLI; they do not replace engine/store/toolchain authorization.
"""
import fcntl
import hashlib
import json
import os
from pathlib import Path
import select
import socket
import stat
import struct
import subprocess
import threading
import time


def tree(root):
    result = {}
    for current, dirs, files in os.walk(root, followlinks=False):
        assert len(result) < 20000
        for leaf in dirs + files:
            p = Path(current) / leaf
            info = p.lstat()
            kind = stat.S_IFMT(info.st_mode)
            body = os.readlink(p).encode() if stat.S_ISLNK(kind) else p.read_bytes() if stat.S_ISREG(kind) else b''
            result[str(p.relative_to(root))] = (kind, stat.S_IMODE(info.st_mode), info.st_uid, info.st_gid, hashlib.sha256(body).hexdigest())
    return result


class LocalTrap:
    """Observe this test's poisoned proxy/telemetry endpoint, never an external host."""
    def __init__(self):
        ctl = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        request = struct.pack('16sH22x', b'lo', 0)
        flags = struct.unpack_from('H', fcntl.ioctl(ctl, 0x8913, request), 16)[0]
        fcntl.ioctl(ctl, 0x8914, struct.pack('16sH22x', b'lo', flags | 1))
        ctl.close()
        self.server = socket.socket()
        self.server.bind(('127.0.0.1', 0))
        self.server.listen(8)
        self.server.settimeout(.1)
        self.count = 0
        self.stopped = threading.Event()
        self.thread = threading.Thread(target=self._serve, daemon=True)
        self.thread.start()
        self.url = 'http://127.0.0.1:' + str(self.server.getsockname()[1])
        # Positive control proves this observer can see a connection.
        with socket.create_connection(self.server.getsockname(), timeout=1) as control:
            control.sendall(b'GET /control HTTP/1.0\r\n\r\n')
            control.recv(4096)
        assert self.count == 1
        self.count = 0

    def _serve(self):
        while not self.stopped.is_set():
            try:
                connection, _ = self.server.accept()
            except socket.timeout:
                continue
            except OSError:
                return
            with connection:
                self.count += 1
                connection.settimeout(.5)
                try:
                    connection.recv(4096)
                    connection.sendall(b'HTTP/1.0 404 Not Found\r\nContent-Length: 0\r\n\r\n')
                except OSError:
                    pass

    def close(self):
        self.stopped.set()
        self.thread.join(timeout=2)
        self.server.close()
        assert not self.thread.is_alive()


class MCP:
    def __init__(self, setup, worktree, profile, extra):
        env = {'PATH': '/toolchain/bin', 'HOME': '/profiles/runtime/home', 'GOMAXPROCS': '4', 'GOTOOLCHAIN': 'local', 'GOTELEMETRY': 'off', **extra}
        self.process = subprocess.Popen(['/mce4d', 'mcp', '--worktree-id', worktree, '--profile-id', profile], preexec_fn=setup(1000), env=env, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.buffer = b''
        self.identifier = 0
        self.rpc('initialize', {'protocolVersion': '2025-11-25', 'capabilities': {}, 'clientInfo': {'name': 'local-defensive-acceptance', 'version': '1'}})
        self.process.stdin.write(b'{"jsonrpc":"2.0","method":"notifications/initialized"}\n')
        self.process.stdin.flush()
        listed = self.rpc('tools/list', {})
        assert len(listed['tools']) == 6

    def rpc(self, method, params):
        self.identifier += 1
        request = {'jsonrpc': '2.0', 'id': self.identifier, 'method': method, 'params': params}
        self.process.stdin.write((json.dumps(request) + '\n').encode())
        self.process.stdin.flush()
        deadline = time.monotonic() + 75
        while time.monotonic() < deadline:
            while b'\n' in self.buffer:
                line, self.buffer = self.buffer.split(b'\n', 1)
                if line:
                    response = json.loads(line)
                    if response.get('id') == self.identifier:
                        assert 'error' not in response
                        return response['result']
            if select.select([self.process.stdout], [], [], .1)[0]:
                data = os.read(self.process.stdout.fileno(), 65536)
                assert data, 'MCP closed before its response'
                self.buffer += data
                assert len(self.buffer) <= 1048576
        raise TimeoutError('bounded local MCP fixture')

    def tool(self, name, arguments, error=None):
        result = self.rpc('tools/call', {'name': name, 'arguments': arguments})
        body = result['structuredContent']
        assert len(result['content']) == 1 and json.loads(result['content'][0]['text']) == body
        if error is None:
            assert body['status'] in ('ok', 'partial') and not result.get('isError', False), (name, body['status'], body.get('error'))
        else:
            assert body['error']['code'] in error and body['data'] is None
        return body

    def close(self):
        self.process.stdin.close()
        try:
            self.process.wait(timeout=8)
        except subprocess.TimeoutExpired:
            self.process.kill()
            self.process.wait(timeout=5)
            raise
        assert self.process.returncode == 0


def exercise(root, command, setup, worktree, profile, request, fixture):
    """Execute real public operations, then controller-owned negative mutations."""
    assert os.getuid() == 0 and not root.is_symlink()
    assert [name for _, name in socket.if_nameindex()] == ['lo']
    baseline, config = tree(root/'target'), tree(root/'host-config')
    expected = fixture['expected']
    prefix = ['--worktree-id', worktree, '--profile-id', profile, 'query']
    def unchanged():
        assert tree(root/'target') == baseline and tree(root/'host-config') == config
    trap = LocalTrap()
    poison = {'GOPACKAGESDRIVER': '/target/execute-me.sh', 'GOFLAGS': '-mod=mod -toolexec=/target/execute-me.sh', 'GOENV': '/target/go.env.txt', 'GOPROXY': trap.url, 'GOTELEMETRY': 'on', 'OTEL_EXPORTER_OTLP_ENDPOINT': trap.url, 'HTTP_PROXY': trap.url, 'HTTPS_PROXY': trap.url}
    mcp = None
    try:
        probe = command('/mce4d', prefix+['rename-preview', '--input-json', '-'], payload=request)
        assert probe.returncode in (0, 6), ('clean native comparison', probe.returncode)
        mcp = MCP(setup, worktree, profile, poison)
        mcp.tool('mce4d_workspace_status', {}); unchanged()
        located = mcp.tool('mce4d_locate', {'mode': 'symbol', 'query': 'Inner', 'limit': 10}); unchanged()
        candidate = next(item for item in located['data']['candidates'] if item['name'] == 'Inner')
        pack = mcp.tool('mce4d_context_pack', {'selector': {'kind': 'ref', 'ref': candidate['ref'], 'service_id': None}, 'task_kind': 'explore', 'max_output_bytes': 24576}); unchanged()
        constraints = [item for item in pack['data']['items'] if item['kind'] == 'constraint_excerpt']
        token, cursor, pages, seen = pack['data']['pack_token'], pack['pagination']['next_cursor'], 1, set()
        # Large type body is authored by the controller to force real expansion.
        assert cursor is not None, 'fixture must exercise the sixth operation'
        while cursor is not None:
            assert pages < 16 and cursor not in seen
            seen.add(cursor)
            page = mcp.tool('mce4d_expand_pack', {'pack_token': token, 'next_cursor': cursor, 'max_output_bytes': 24576}); unchanged()
            assert page['data']['pack_token'] == token
            constraints.extend(item for item in page['data']['items'] if item['kind'] == 'constraint_excerpt')
            cursor = page['pagination']['next_cursor']; pages += 1
        assert pages >= expected['context_min_pages']
        assert constraints
        for item in constraints:
            assert item['source_refs'] and all(ref['path'] in expected['trusted_constraint_paths'] for ref in item['source_refs'])
            assert 'Ignore previous instructions' not in (item['content'] or '')
        valid = mcp.tool('mce4d_validate_pack', {'pack_token': token, 'verify': 'workspace'}); unchanged()
        assert valid['data']['edit_precondition_validated'] and valid['freshness']['status'] == 'observed_current'
        preview = mcp.tool('mce4d_rename_preview', request); unchanged()
        assert preview['data']['total_edits'] == expected['rename']['total_edits'] and not preview['data']['applied'] and preview['data']['source_mutation_count'] == 0
        assert sorted({edit['source_ref']['path'] for edit in preview['data']['edits']}) == expected['rename']['paths']
        valid = mcp.tool('mce4d_validate_pack', {'result_token': preview['data']['result_token']}); unchanged()
        assert valid['data']['edit_precondition_validated']
        for path in fixture['invalid_paths']:
            invalid = {**request, 'selector': {**request['selector'], 'path': path}}
            mcp.tool('mce4d_rename_preview', invalid, error=('INVALID_ARGUMENT',)); unchanged()
        mcp.close(); mcp = None
        assert not (root/'result/repo-executed').exists()
        print('C041_ALL_SIX_NATIVE_MCP_TOOLS_CANARY_PASS pages='+str(pages), flush=True)

        # The caller alone adds/restores fake secret-bearing inputs.
        marker = 'LOCALTESTONLY_' + 'Z'*32
        for name, body in fixture['secret_files'].items():
            path = root/'target'/name; path.write_text(body.replace('{MARKER}', marker));path.chmod(0o644);os.chown(path, 1001, 1001)
        mutated = tree(root/'target')
        result = command('/mce4d', ['index', '--worktree-id', worktree, '--profile-id', profile], extra=poison)
        assert result.returncode == 0 and marker not in result.stdout+result.stderr
        result = command('/mce4d', prefix+['locate', '--input-json', '-'], payload={'mode': 'symbol', 'query': 'Leaked', 'limit': 10}, extra=poison)
        assert marker not in result.stdout+result.stderr
        assert not json.loads(result.stdout)['data']['candidates']
        assert tree(root/'target') == mutated
        blocked = command('/mce4d', prefix+['rename-preview', '--input-json', '-'], payload=request, extra=poison)
        assert blocked.returncode == 3 and json.loads(blocked.stdout)['error']['code'] == 'UNSUPPORTED_CAPABILITY'
        found_gap = False; scanned = 0
        for current, _, names in os.walk(root/'profiles/runtime'):
            for name in names:
                path = root / os.path.relpath(current, root) / name
                if not path.is_file() or path.is_symlink(): continue
                assert path.stat().st_size <= 64*1024**2
                raw = path.read_bytes(); scanned += len(raw); assert scanned <= 512*1024**2
                assert marker.encode() not in raw, 'secret marker persisted in native state'
                found_gap |= b'SECRET_BLOCKED' in raw
        assert found_gap, 'blocked source classification was lost'
        for name in fixture['secret_files']: (root/'target'/name).unlink()
        unchanged()
        print('C025_NATIVE_STATE_BLOBS_FTS_WAL_LOG_AND_OUTPUT_SECRET_CANARY_PASS', flush=True)

        # Missing approved dependency data is an explicit capability failure,
        # not permission to contact the poisoned module proxy.
        mod = root/'target/go.mod'; original = mod.read_bytes()
        mod.write_bytes(original+b'\nrequire missing.invalid/localfixture v0.0.1\n')
        assert command('/mce4d', ['index', '--worktree-id', worktree, '--profile-id', profile], extra=poison).returncode == 0
        missing = command('/mce4d', prefix+['rename-preview', '--input-json', '-'], payload=request, extra=poison)
        assert missing.returncode == 3 and json.loads(missing.stdout)['error']['code'] == 'UNSUPPORTED_CAPABILITY'
        mod.write_bytes(original); unchanged()
        assert trap.count == 0 and not (root/'result/repo-executed').exists()
        print('C046_POISONED_PARENT_AND_C047_LOCAL_TRAP_ZERO_CONTACT_MISSING_DEPENDENCY_REFUSAL_PASS', flush=True)

        # Refuse a substituted approved executable before it can run.
        assert command('/mce4d', ['index', '--worktree-id', worktree, '--profile-id', profile]).returncode == 0
        server = root/'bin/gopls'; binary = server.read_bytes()
        server.write_bytes(binary+b'\n')
        try:
            replaced = command('/mce4d', prefix+['rename-preview', '--input-json', '-'], payload=request)
            assert replaced.returncode == 3 and json.loads(replaced.stdout)['error']['code'] == 'ACCESS_DENIED'
        finally:
            server.write_bytes(binary)
        unchanged()
        # Runtime config is data: unapproved repo fsmonitor/aliases must never
        # execute the synthetic script. The controller proves its marker works.
        control = command('/target/execute-me.sh', [])
        assert control.returncode == 0 and (root/'result/repo-executed').read_bytes() == b'executed'
        (root/'result/repo-executed').unlink()
        git_config = root/'target/.git/config'; original_config = git_config.read_bytes()
        git_config.write_bytes(original_config+b'\n[core]\n\tfsmonitor = /target/execute-me.sh\n[alias]\n\tstatus = !/target/execute-me.sh\n')
        mutated = tree(root/'target')
        command('/mce4d', ['index', '--worktree-id', worktree, '--profile-id', profile], extra=poison)
        assert not (root/'result/repo-executed').exists() and tree(root/'target') == mutated
        git_config.write_bytes(original_config); unchanged()
        print('C026_UNTRUSTED_DOCUMENT_AND_GIT_CONFIG_NO_EXEC_C046_REPLACED_GOPLS_DENIED_PASS', flush=True)

        outside = root/'outside';outside.mkdir();outside.chmod(0o755)
        (outside/'private.go').write_bytes(b'package private\n// OUTSIDE_CANARY_ONLY\n');(outside/'private.go').chmod(0o644)
        link = root/'target/escape.go'; link.symlink_to('/outside/private.go')
        mutated = tree(root/'target')
        escape = command('/mce4d', ['index', '--worktree-id', worktree, '--profile-id', profile])
        assert 'OUTSIDE_CANARY_ONLY' not in escape.stdout+escape.stderr and tree(root/'target') == mutated
        assert (outside/'private.go').read_bytes() == b'package private\n// OUTSIDE_CANARY_ONLY\n'
        link.unlink(); unchanged()
        assert trap.count == 0
        print('C024_NATIVE_PATH_REJECTIONS_AND_SYMLINK_ESCAPE_NO_MUTATION_PASS', flush=True)
    finally:
        if mcp is not None: mcp.close()
        trap.close()
