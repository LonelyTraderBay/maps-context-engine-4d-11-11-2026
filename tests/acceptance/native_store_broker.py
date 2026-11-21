"""Root-only native quota/pending/worker fixture; run through the owned guard.
Uses no live user profile and refuses an existing synthetic root.
"""
import os,pathlib,subprocess,hashlib,fcntl,struct,stat,json,socket,resource,time,ctypes,select,signal,base64
run=pathlib.Path(os.environ['HOME']).parent;root=run/'rootfs'
build=pathlib.Path(os.environ['MCE4D_ACCEPTANCE_BUILD'])
approved=json.loads(pathlib.Path(os.environ['MCE4D_ACCEPTANCE_APPROVALS']).read_text())
assert os.getpid()==1
source=build/'sqlite.test'
assert os.getuid()==0 and not root.exists()
assert hashlib.sha256(source.read_bytes()).hexdigest()==approved['binaries']['sqlite.test']
assert [n for _,n in socket.if_nameindex()]==['lo']
subprocess.run(['/usr/bin/mount','--make-rprivate','/'],check=True,timeout=5)
root.mkdir(mode=0o755);root.chmod(0o755)
for name,uid,mode in [('profiles',0,0o755),('target',1001,0o755),('host-config',1000,0o700),('proc',0,0o755),('tmp',0,0o1777),('dev',0,0o755)]:
 p=root/name;p.mkdir();os.chown(p,uid,uid);p.chmod(mode)
(root/'state-profile').write_bytes(source.read_bytes());(root/'state-profile').chmod(0o755)
(root/'target/canary.txt').write_bytes(b'synthetic project untouched\n')
(root/'host-config/config.toml').write_bytes(b'synthetic existing configuration\n')
os.mknod(root/'dev/null',stat.S_IFCHR|0o666,os.makedev(1,3));(root/'dev/null').chmod(0o666)
subprocess.run(['/usr/bin/mount','-t','proc','-o','nosuid,nodev,noexec','proc',str(root/'proc')],check=True,timeout=5)

(root/'fixture.owner').write_text('mce4d-profile-catalog-fixture-v1\n');(root/'fixture.owner').chmod(0o644)
tool=build/'state-profile'
assert hashlib.sha256(tool.read_bytes()).hexdigest()==approved['binaries']['state-profile']
(root/'profile-tool').write_bytes(tool.read_bytes());(root/'profile-tool').chmod(0o755)
def setup(uid):
 def enter():
  os.chroot(root);os.chdir('/');os.setgroups([]);os.setgid(uid);os.setuid(uid)
 return enter
def command(binary,args,uid=0,env_extra=None):
 env={'PATH':'/usr/bin:/bin','HOME':'/','GOMAXPROCS':'4'}
 env.update(env_extra or {})
 p=subprocess.run([binary]+args,preexec_fn=setup(uid),env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,timeout=40)
 print(json.dumps({'binary':binary,'args':args,'uid':uid,'exit':p.returncode,'output':p.stdout}),flush=True)
 return p
def test(name,uid,profile="native"):
 e={'MCE4D_PROFILE_CATALOG_FIXTURE':'synthetic-v1','MCE4D_PROFILE_TEST_ROOT':'/profiles/'+profile}
 p=command('/state-profile',['-test.v','-test.run=^'+name+'$','-test.timeout=30s'],uid,e)
 assert p.returncode==0
def tree(path):
 return {str(p.relative_to(path)):hashlib.sha256(p.read_bytes()).hexdigest() for p in path.rglob('*') if p.is_file()}
def crash_restore_matrix():
 for scope,steps in (
  ('catalog',['catalog_main','catalog_journal','catalog_head','profile_manifest']),
  ('worktree',['index_main','records_main','index_journal','records_journal','index_head','records_head','worktree_manifest']),
 ):
  boundaries=['PREPARED','REPLACING','VERIFYING']
  boundaries += [marker+':'+step for step in steps for marker in ('rename','exchange','record')]
  for i,boundary in enumerate(boundaries):
   profile='restore-'+scope+'-'+str(i)
   test('TestProtectedProfileCatalogProvisionFixture',0,profile)
   if scope=='worktree':
    test('TestProtectedWorktreeProvisionFixture',0,profile)
   fixture=root/'profiles'/profile
   keys=[fixture/'control/profile.key',fixture/'home/.local/state/mce4d/cursor.key']
   original=[p.read_bytes() for p in keys]
   env={'MCE4D_PROFILE_CATALOG_FIXTURE':'synthetic-v1','MCE4D_PROFILE_TEST_ROOT':'/profiles/'+profile,'MCE4D_RESTORE_FIXTURE_TARGET':scope,'MCE4D_RESTORE_FIXTURE_BOUNDARY':boundary}
   def run_case(name,uid):
    return command('/state-profile',['-test.v','-test.run=^'+name+'$','-test.timeout=30s'],uid,env)
   assert run_case('TestProtectedRestoreCreateBackupFixture',1000).returncode==0
   crash=run_case('TestProtectedRestoreCrashFixture',0)
   assert crash.returncode==-signal.SIGKILL and 'RESTORE_FIXTURE_CONTROLLER_SIGKILL '+boundary in crash.stdout
   held=tree(fixture)
   assert run_case('TestProtectedRestoreBlockedFixture',1000).returncode==0
   assert tree(fixture)==held
   if i==0:
    state=fixture/'home/.local/state/mce4d'
    if scope=='worktree':state=state/'worktrees/11111111-1111-4111-8111-111111111111'
    stage=next((state/'backups').glob('.restore-*'))/'db'
    database=stage/('catalog.sqlite' if scope=='catalog' else 'records.sqlite')
    saved=database.read_bytes()
    database.write_bytes(saved[:-1]+bytes([saved[-1]^1]))
    inconsistent=tree(fixture)
    assert run_case('TestProtectedRestoreRefusesInconsistentPairFixture',0).returncode==0
    assert tree(fixture)==inconsistent
    database.write_bytes(saved)
    assert tree(fixture)==held
   assert run_case('TestProtectedRestoreFinishFixture',0).returncode==0
   assert run_case('TestProtectedRestoreServingFixture',1000).returncode==0
   assert [p.read_bytes() for p in keys]==original
   print('C172_C173_NATIVE_CRASH_RECOVERY_PASS',scope,boundary,flush=True)
before_target=tree(root/'target');before_config=tree(root/'host-config')
try:
 test('TestProtectedProfileCatalogProvisionFixture',0)
 test('TestProtectedWorktreeProvisionFixture',0)
 test('TestProtectedWorktreeRoundtripFixture',1000)
 if os.environ.get('MCE4D_STORE_RESTORE')=='1':
  crash_restore_matrix()
 if os.environ.get('MCE4D_STORE_MAINTENANCE')=='1':
  test('TestProtectedMaintenanceSecondWorktreeProvisionFixture',0)
  test('TestProtectedQuiescentBackupFixture',1000)
  test('TestProtectedCreateBackupCatalogFixture',1000)
  test('TestProtectedRestoreStageCatalogFixture',1000)
  test('TestProtectedRestoreStageWorktreePairFixture',1000)
  test('TestProtectedRestoreInputPreflightFixture',0)
  test('TestProtectedProfileCatalogProvisionFixture',0,'schema')
  test('TestProtectedWorktreeProvisionFixture',0,'schema')
  test('TestProtectedBackupFutureSchemaRefusalFixture',1000,'schema')
  for scope in ['catalog','worktree']:
   for phase in ['NEEDS_RECOVERY','PREPARED','REPLACING','VERIFYING']:
    profile='gate-'+scope+'-'+phase.lower()
    test('TestProtectedProfileCatalogProvisionFixture',0,profile)
    test('TestProtectedWorktreeProvisionFixture',0,profile)
    test('TestProtectedMaintenanceSecondWorktreeProvisionFixture',0,profile)
    env={'MCE4D_PROFILE_CATALOG_FIXTURE':'synthetic-v1','MCE4D_PROFILE_TEST_ROOT':'/profiles/'+profile,'MCE4D_MAINTENANCE_TARGET':scope,'MCE4D_MAINTENANCE_PHASE':phase}
    assert command('/state-profile',['-test.v','-test.run=^TestProtectedMaintenanceJournalSeedFixture$'],0,env).returncode==0
    before=tree(root/'profiles'/profile)
    assert command('/state-profile',['-test.v','-test.run=^TestProtectedMaintenanceJournalServingGateFixture$'],1000,env).returncode==0
    # Worktree B deliberately writes; all scoped A metadata and DBs stay equal.
    after=tree(root/'profiles'/profile)
    ignored=('home/.local/state/mce4d/worktrees/22222222-2222-4222-8222-222222222222/','control/worktrees/22222222-2222-4222-8222-222222222222/')
    assert {k:v for k,v in before.items() if not k.startswith(ignored)}=={k:v for k,v in after.items() if not k.startswith(ignored)}
 test('TestProtectedProfileCatalogProvisionFixture',0,'pending')
 test('TestProtectedWorktreeProvisionFixture',0,'pending')
 test('TestProtectedPendingRecoveryFixture',1000,'pending')
 test('TestProtectedProfileCatalogProvisionFixture',0,'quota')
 test('TestProtectedWorktreeProvisionFixture',0,'quota')
 test('TestProtectedCacheBudgetFixture',1000,'quota')
 wt=root/'profiles/native/control/worktrees/11111111-1111-4111-8111-111111111111/worktree.json'
 m=json.loads(wt.read_bytes());assert m['stage']=='READY' and m['index_incarnation']!=m['records_incarnation']
 base_args=['create','--root','/profiles/tool','--engine-uid','1000','--engine-gid','1000','--client-uid','1001','--target-root','/target']
 assert command('/profile-tool',base_args).returncode==0
 make=['worktree-create','--root','/profiles/tool','--worktree-id','11111111-1111-4111-8111-111111111111']
 made=command('/profile-tool',make);assert made.returncode==0 and json.loads(made.stdout)['verified']
 before=tree(root/'profiles/tool')
 assert command('/profile-tool',make).returncode==3 and tree(root/'profiles/tool')==before
 assert command('/profile-tool',['check','--root','/profiles/tool']).returncode==0
 assert tree(root/'target')==before_target and tree(root/'host-config')==before_config
 print('WORKTREE_PROFILE_AND_PACKAGED_PROVISIONER_PASS',flush=True)
finally:
 for head in (root/'profiles').rglob('wal-head.log'):
  fd=os.open(head,os.O_RDONLY)
  try:
   flags=struct.unpack('I',fcntl.ioctl(fd,0x80086601,struct.pack('I',0))[:4])[0]
   fcntl.ioctl(fd,0x40086602,struct.pack('I',flags&~0x20));os.fsync(fd)
  finally:os.close(fd)
 subprocess.run(['/usr/bin/umount',str(root/'proc')],check=True,timeout=5)
 print('PRIVATE_FLAGS_AND_MOUNT_CLEANED',flush=True)
