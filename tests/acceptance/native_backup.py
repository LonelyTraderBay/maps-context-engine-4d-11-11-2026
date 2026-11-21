"""Real backup CLI on a fresh synthetic installed profile, no live user data."""
import hashlib
import json
from pathlib import Path
import sqlite3
import os
import uuid


def exercise(root,command,setup,worktree,profile,request,fixture,helpers):
    state=root/'profiles/runtime/home/.local/state/mce4d'
    state_inside=Path('/profiles/runtime/home/.local/state/mce4d')
    before_target=helpers.tree(root/'target');before_config=helpers.tree(root/'host-config')
    durable=[state/'catalog.sqlite',state/'worktrees'/worktree/'records.sqlite',state/'cursor.key',root/'profiles/runtime/control/worktrees'/worktree/'worktree.json']
    before={str(p):hashlib.sha256(p.read_bytes()).hexdigest() for p in durable}
    client=helpers.MCP(setup,worktree,profile,{})
    try:
        status=client.tool('mce4d_workspace_status',{})
        for target in ('catalog','worktree'):
            args=['store','recover','--target',target]+(['--worktree-id',worktree] if target=='worktree' else [])+['--report-only']
            result=command('/mce4d',args,timeout=5)
            assert result.returncode==0, (result.returncode,result.stderr)
            observed=json.loads(result.stdout)
            assert observed['phase']=='none' and not observed['journal_present'] and not observed['actual_state_verified']
            default=command('/mce4d',args[:-1],timeout=5)
            assert default.returncode==0 and json.loads(default.stdout)==observed
        generation=status['data']['scopes'][0]['generation_id']
        blocked=state_inside/'worktrees'/worktree/'backups/blocked'
        result=command('/mce4d',['store','backup','--target','worktree','--worktree-id',worktree,'--output',str(blocked)],timeout=15)
        assert result.returncode==4, (result.returncode,result.stderr)
        assert not (root/str(blocked).lstrip('/')).exists()
        assert client.tool('mce4d_workspace_status',{})['data']['scopes'][0]['generation_id']==generation
    finally:
        client.close()
    reports={}
    for target in ('worktree','catalog'):
        parent=state_inside/'worktrees'/worktree if target=='worktree' else state_inside
        output=parent/'backups'/('cli-'+target)
        args=['store','backup','--target',target]
        if target=='worktree':args+=['--worktree-id',worktree]
        args+=['--output',str(output)]
        result=command('/mce4d',args)
        assert result.returncode==0, (result.returncode,result.stderr)
        report=json.loads(result.stdout);directory=root/str(output).lstrip('/')
        raw=(directory/'backup.json').read_bytes();manifest=json.loads(raw)
        assert hashlib.sha256(raw).hexdigest()==report['manifest_sha256']
        assert manifest['format_version']==1 and manifest['target']==target
        assert manifest['profile_installation_id']==json.loads((root/'profiles/runtime/profile.json').read_bytes())['profile_id']
        assert manifest['consistency']==('quiesced_worktree_set' if target=='worktree' else 'single_store')
        expected={'index.sqlite','records.sqlite'} if target=='worktree' else {'catalog.sqlite'}
        assert {item['file_name'] for item in manifest['stores']}==expected
        assert {p.name for p in directory.iterdir()}==expected|{'backup.json'}
        assert manifest['revisions']['worktrees'][0]['worktree_id']==worktree
        assert len(manifest['revisions']['worktrees'][0]['profiles'])==2
        bytes_total=len(raw)
        for item in manifest['stores']:
            path=directory/item['file_name'];data=path.read_bytes()
            assert len(data)==item['byte_length'] and hashlib.sha256(data).hexdigest()==item['raw_sha256']
            bytes_total+=len(data)
            db=sqlite3.connect(path.as_uri()+'?mode=ro&immutable=1',uri=True)
            try:
                assert db.execute('PRAGMA integrity_check').fetchone()==('ok',)
                assert db.execute('PRAGMA foreign_key_check').fetchall()==[]
                identity={'catalog':'catalog_identity','index':'store_identity','records':'records_meta'}[item['kind']]
                assert db.execute('SELECT incarnation FROM '+identity+' WHERE singleton=1').fetchone()==(item['source_incarnation'],)
                if item['kind']=='index':
                    assert db.execute('SELECT generation_id FROM active_generations WHERE profile_id=?',(profile,)).fetchone()==(generation,)
                if item['kind']=='records':
                    assert db.execute('SELECT record_epoch FROM records_meta WHERE singleton=1').fetchone()==(item['record_epoch'],)
            finally:db.close()
            assert hashlib.sha256(path.read_bytes()).hexdigest()==item['raw_sha256']
        assert report['total_bytes']==bytes_total and report['file_count']==len(expected)+1
        preserved={p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in directory.iterdir()}
        again=command('/mce4d',args)
        assert again.returncode==3 and not again.stdout
        assert preserved=={p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in directory.iterdir()}
        reports[target]=report
    denied=command('/mce4d',['store','backup','--target','catalog','--output','/target/forbidden-backup'])
    assert denied.returncode!=0 and not (root/'target/forbidden-backup').exists()
    assert helpers.tree(root/'target')==before_target
    assert helpers.tree(root/'host-config')==before_config
    assert {str(p):hashlib.sha256(p.read_bytes()).hexdigest() for p in durable}==before
    print(json.dumps({'case':'C171_NATIVE_BACKUP_CLI','result':'PASS','reports':reports,'independent_reader_sqlite_version':sqlite3.sqlite_version}),flush=True)
    # Seed protected metadata only, to prove public startup refusals. This is
    # not an actual replacement, crash simulation or restore/recovery proof.
    installed=json.loads((root/'profiles/runtime/profile.json').read_bytes())
    for target in ('worktree','catalog'):
        selected=state/'worktrees'/worktree if target=='worktree' else state
        manifest=json.loads((selected/'backups'/('cli-'+target)/'backup.json').read_bytes())
        steps=['index_main','records_main','index_journal','records_journal','index_head','records_head','worktree_manifest'] if target=='worktree' else ['catalog_main','catalog_journal','catalog_head','profile_manifest']
        journal={'format_version':1,'operation_id':str(uuid.uuid4()),'profile_installation_id':installed['profile_id'],'target':target,'worktree_id':worktree if target=='worktree' else '', 'backup_directory':'cli-'+target,'backup_manifest_sha256':reports[target]['manifest_sha256'],'phase':'PREPARED','outcome':'','permission_review_confirmed':True,'old_stores':[{'kind':item['kind'],'incarnation':item['source_incarnation'],'logical_sha256':item['source_logical_sha256']} for item in manifest['stores']],'new_stores':[{'kind':item['kind'],'incarnation':str(uuid.uuid4()),'logical_sha256':'d'*64} for item in manifest['stores']],'files':[{'step':step,'old_sha256':'a'*64,'new_sha256':'b'*64} for step in steps],'completed_steps':[]}
        control=root/'profiles/runtime/control'/('worktrees/'+worktree if target=='worktree' else 'catalog')
        path=control/'maintenance.json'
        fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o444)
        try:os.fchmod(fd,0o444);os.write(fd,(json.dumps(journal)+'\n').encode());os.fsync(fd)
        finally:os.close(fd)
        prior=helpers.tree(root/'profiles/runtime')
        args=['store','recover','--target',target]+(['--worktree-id',worktree] if target=='worktree' else [])+['--report-only']
        for uid in (1000,0):
            result=command('/mce4d',args,uid=uid)
            assert result.returncode==0,(result.returncode,result.stderr)
            report=json.loads(result.stdout)
            assert report['phase']=='PREPARED' and report['operation_id']==journal['operation_id'] and not report['actual_state_verified']
        assert helpers.tree(root/'profiles/runtime')==prior, 'report-only changed installed state'
        result=command('/mce4d',['--worktree-id',worktree,'--profile-id',profile,'query','status','--input-json','-'],payload={})
        assert result.returncode!=0 and 'STORE_RECOVERY_REQUIRED' in result.stdout,(result.returncode,result.stdout,result.stderr)
        result=command('/mce4d',['mcp','--worktree-id',worktree,'--profile-id',profile])
        assert result.returncode==3 and 'STORE_RECOVERY_REQUIRED' in result.stderr
        after=helpers.tree(root/'profiles/runtime')
        changed=[name for name in sorted(set(prior)|set(after)) if prior.get(name)!=after.get(name)]
        assert not changed, ('refused startup changed state',changed)
    print(json.dumps({'case':'C172_NATIVE_JOURNAL_REPORT_AND_CLI_MCP_REFUSAL','result':'PASS','scope':'seeded metadata only, no database replacement'}),flush=True)
