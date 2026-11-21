"""Bounded GC acceptance on the broker's disposable installed native profile.

Only the root fixture controller changes synthetic source revisions. Product
commands retain read-only target permission and use their real WAL authority.
"""
import hashlib
import json
import time


def exercise(root, command, setup, worktree, profile, request, fixture, helpers):
    target = root/'target'
    config_before = helpers.tree(root/'host-config')
    state = root/'profiles/runtime/home/.local/state/mce4d'
    durable_paths = [state/'catalog.sqlite', state/'worktrees'/worktree/'records.sqlite', state/'cursor.key', root/'profiles/runtime/control/worktrees'/worktree/'worktree.json']
    durable_before = {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in durable_paths}
    mcp = helpers.MCP(setup, worktree, profile, {})
    try:
        located = mcp.tool('mce4d_locate', {'mode': 'symbol', 'query': 'Inner', 'limit': 10})
        candidate = next(item for item in located['data']['candidates'] if item['name']=='Inner')
        pack = mcp.tool('mce4d_context_pack', {'selector': {'kind':'ref', 'ref':candidate['ref'], 'service_id':None}, 'task_kind':'explore', 'max_output_bytes':24576})
        token, cursor = pack['data']['pack_token'], pack['pagination']['next_cursor']
        assert cursor, 'authored corpus must force continuation'
        preview = mcp.tool('mce4d_rename_preview', request)
        assert preview['data']['total_edits']==4 and not preview['data']['applied']
        result_token = preview['data']['result_token']
        for revision in range(1,4):
            # Explicit fixture-controller mutation, never an engine edit.
            with (target/'b.go').open('a') as output:
                output.write('// local synthetic GC revision %d\n'%revision)
            result = command('/mce4d', ['index','--worktree-id',worktree,'--profile-id',profile])
            assert result.returncode==0
        target_before_gc = helpers.tree(target)
        # The old pinned source still exists before GC; new active generations
        # alone must not rebase or invalidate its continuation.
        repeat_started = time.monotonic()
        repeated = mcp.tool('mce4d_expand_pack', {'pack_token':token,'next_cursor':cursor,'max_output_bytes':24576})
        repeated_elapsed_ms = round(1000 * (time.monotonic() - repeat_started), 3)
        gc = ['store','gc','--worktree-id',worktree,'--profile-id',profile]
        report = command('/mce4d', gc)
        assert report.returncode==0
        report = json.loads(report.stdout)
        assert report['dry_run'] and report['keep_generations']==2 and len(report['generations'])==2 and not report['more'], report
        assert all(not item['applied'] and item['rows']['generations']==1 for item in report['generations'])
        # The same old page is still readable after the read-only GC plan.
        mcp.tool('mce4d_expand_pack', {'pack_token':token,'next_cursor':cursor,'max_output_bytes':24576})
        refused = command('/mce4d',gc+['--apply'])
        assert refused.returncode==2
        applied = command('/mce4d',gc+['--apply','--confirm'])
        assert applied.returncode==0
        applied = json.loads(applied.stdout)
        assert not applied['dry_run'] and len(applied['generations'])==2
        assert [item['generation_id'] for item in applied['generations']]==[item['generation_id'] for item in report['generations']]
        assert all(item['applied'] and item['rows']==expected['rows'] and item['blob_bytes']==expected['blob_bytes'] for item,expected in zip(applied['generations'],report['generations']))
        # This must be a typed expired-snapshot result, never a new page or a
        # fallback to the current active generation.
        result = mcp.rpc('tools/call', {'name':'mce4d_expand_pack','arguments':{'pack_token':token,'next_cursor':cursor,'max_output_bytes':24576}})
        body = result['structuredContent']
        print(json.dumps({'case':'GC_OLD_PINNED_CONTINUATION','status':body['status'],'error':body.get('error')}),flush=True)
        assert body['status']=='error' and body['error']['code']=='SNAPSHOT_EXPIRED' and body['data'] is None, body.get('error')
        mcp.tool('mce4d_validate_pack', {'pack_token':token}, error={'SNAPSHOT_EXPIRED'})
        mcp.tool('mce4d_validate_pack', {'result_token':result_token}, error={'SNAPSHOT_EXPIRED'})
        print(json.dumps({'case':'C179_R1_RETAINED_READ_AND_GC_EXPIRY',
                          'generations_created':4, 'generations_collected':2,
                          'snapshot_expired_operations':3,
                          'repeated_context_bytes':repeated['budget']['serialized_bytes'],
                          'repeated_context_elapsed_ms':repeated_elapsed_ms}),flush=True)
        mcp.tool('mce4d_workspace_status', {})
        mcp.tool('mce4d_locate', {'mode':'symbol','query':'Inner','limit':10})
        empty = command('/mce4d',gc)
        assert empty.returncode==0 and json.loads(empty.stdout)['generations']==[]
        assert helpers.tree(target)==target_before_gc
        assert helpers.tree(root/'host-config')==config_before
        assert {str(p):hashlib.sha256(p.read_bytes()).hexdigest() for p in durable_paths}==durable_before
        print('C038_C119_C130_NATIVE_GC_TWO_GENERATIONS_DEFAULT_RETENTION_SOURCE_RECORDS_CATALOG_PRESERVED_PASS',flush=True)
    finally:
        mcp.close()
