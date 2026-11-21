"""Actual packaged backup/restore and stale token checks on synthetic state."""
import json
import time
from pathlib import Path


def exercise(root, command, setup, worktree, profile, request, fixture, helpers):
    install = root / 'profiles/runtime'
    state = install / 'home/.local/state/mce4d'
    state_inside = Path('/profiles/runtime/home/.local/state/mce4d')
    slot = install / 'control/worktrees' / worktree / 'worktree.json'
    profile_file = install / 'profile.json'
    keys = (install / 'control/profile.key', state / 'cursor.key')
    key_bytes = [p.read_bytes() for p in keys]
    target_before = helpers.tree(root / 'target')
    config_before = helpers.tree(root / 'host-config')
    prefix = ['--worktree-id', worktree, '--profile-id', profile, 'query']

    def cli(args, **kwargs):
        result = command('/mce4d', args, **kwargs)
        assert result.returncode == 0, (args, result.returncode, result.stderr)
        return json.loads(result.stdout)

    def tokens(mcp):
        before = mcp.tool('mce4d_workspace_status', {})
        candidate = next(item for item in mcp.tool('mce4d_locate', {
            'mode': 'symbol', 'query': 'Inner', 'limit': 10,
        })['data']['candidates'] if item['name'] == 'Inner')
        pack = mcp.tool('mce4d_context_pack', {
            'selector': {'kind': 'ref', 'ref': candidate['ref'], 'service_id': None},
            'task_kind': 'explore', 'max_output_bytes': 24576,
        })
        rename = mcp.tool('mce4d_rename_preview', request)
        assert rename['data']['total_edits'] == 4 and not rename['data']['applied']
        pack_input = {'pack_token': pack['data']['pack_token']}
        result_input = {'result_token': rename['data']['result_token']}
        cursor = pack['pagination']['next_cursor']
        assert cursor
        expand_input = {**pack_input, 'next_cursor': cursor, 'max_output_bytes': 24576}
        for payload in (pack_input, result_input):
            mcp.tool('mce4d_validate_pack', payload)
        mcp.tool('mce4d_expand_pack', expand_input)
        return before['data']['scopes'][0], (pack_input, result_input, expand_input)

    previous = None
    for target in ('worktree', 'catalog'):
        scope_args = ['--target', target]
        base = state_inside
        if target == 'worktree':
            scope_args += ['--worktree-id', worktree]
            base /= Path('worktrees') / worktree
        backup = base / 'backups' / ('restore-' + target)
        cli(['store', 'backup'] + scope_args + ['--output', str(backup)])
        backup_on_disk = root / str(backup).lstrip('/')
        backup_before = helpers.tree(backup_on_disk)
        catalog_before = json.loads(profile_file.read_bytes())
        slot_before = json.loads(slot.read_bytes())
        args = ['store', 'restore', '--manifest', str(backup / 'backup.json'),
                '--confirm', '--wait-ms', '2500']
        mcp = helpers.MCP(setup, worktree, profile, {})
        try:
            status, old = previous or tokens(mcp)
            wait_started = time.monotonic()
            blocked = command('/mce4d', args[:-1] + ['50'], uid=0)
            wait_ms = round(1000 * (time.monotonic() - wait_started), 3)
            assert blocked.returncode == 4 and 'STORE_MAINTENANCE' in blocked.stderr
            print(json.dumps({'case': 'C179_RESTORE_WITH_SHARED_READER', 'scope': target,
                              'configured_wait_ms': 50, 'command_elapsed_ms': wait_ms}), flush=True)
            assert not list(backup_on_disk.parent.glob('.restore-*'))
            assert not cli(['store', 'recover'] + scope_args)['journal_present']
        finally:
            mcp.close()
        assert command('/mce4d', args, uid=1000).returncode == 3
        assert command('/mce4d', args[:4] + args[5:], uid=0).returncode == 2
        restored = cli(args, uid=0)
        assert restored['outcome'] == 'restored' and restored['target'] == target
        report = cli(['store', 'recover'] + scope_args)
        assert report['phase'] == 'VERIFIED' and report['operation_id'] == restored['operation_id']
        same = cli(['store', 'recover'] + scope_args + ['--apply', '--confirm', '--wait-ms', '2500'], uid=0)
        assert same == restored
        current_catalog = json.loads(profile_file.read_bytes())
        current_slot = json.loads(slot.read_bytes())
        if target == 'catalog':
            assert current_catalog.pop('catalog_incarnation') != catalog_before.pop('catalog_incarnation')
            assert current_catalog == catalog_before and current_slot == slot_before
        else:
            for key in ('index_incarnation', 'records_incarnation'):
                assert current_slot.pop(key) != slot_before.pop(key)
            assert current_slot == slot_before and current_catalog == catalog_before
        assert helpers.tree(backup_on_disk) == backup_before
        assert [p.read_bytes() for p in keys] == key_bytes
        mcp = helpers.MCP(setup, worktree, profile, {})
        try:
            now = mcp.tool('mce4d_workspace_status', {})['data']['scopes'][0]
            assert now['generation_id'] == status['generation_id']
            for tool, action, payload in (
                ('mce4d_validate_pack', 'validate', old[0]),
                ('mce4d_validate_pack', 'validate', old[1]),
                ('mce4d_expand_pack', 'expand', old[2]),
            ):
                body = mcp.tool(tool, payload, error={'STORE_REPLACED', 'CURSOR_INVALID'})
                result = command('/mce4d', prefix + [action, '--input-json', '-'], payload=payload)
                decoded = json.loads(result.stdout)
                assert result.returncode != 0 and decoded['error']['code'] == body['error']['code'] and decoded['data'] is None
                print(json.dumps({'case': 'C173_OLD_TOKEN_REJECTED', 'scope': target,
                                  'operation': tool, 'code': body['error']['code']}), flush=True)
            previous = tokens(mcp)
        finally:
            mcp.close()
    head = install / 'control/worktrees' / worktree / 'index/wal-head.log'
    before_size = head.stat().st_size
    rebuilt = command('/mce4d', ['index', '--worktree-id', worktree, '--profile-id', profile, '--full'])
    assert rebuilt.returncode == 0 and head.stat().st_size > before_size
    now = cli(prefix + ['status', '--input-json', '-'], payload={})
    assert now['data']['scopes'][0]['generation_id'] != previous[0]['generation_id']
    assert helpers.tree(root / 'target') == target_before
    assert helpers.tree(root / 'host-config') == config_before
    print('C173_NATIVE_RESTORE_CLI_AND_STALE_TOKENS_PASS', flush=True)
