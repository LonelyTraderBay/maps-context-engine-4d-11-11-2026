"""Root-only synthetic broker. Requires the owned guard/namespace setup in README.
Never invoke against an existing state or target directory.
"""
import os,pathlib,subprocess,hashlib,fcntl,struct,stat,json,socket,time,re,shutil
run=pathlib.Path(os.environ['MCE4D_ACCEPTANCE_RUN']).resolve(strict=True);root=run/'rootfs';built=pathlib.Path(os.environ['MCE4D_ACCEPTANCE_BUILD']).resolve(strict=True)
approved=json.loads(pathlib.Path(os.environ['MCE4D_ACCEPTANCE_APPROVALS']).read_text())
assert set(approved['binaries'])=={'mce4d','state-profile','go-toolchain'}
suite=os.environ.get('MCE4D_ACCEPTANCE_SUITE','security');assert suite in ('security','gc','backup','restore','scale','bench','bench-context','bench-retention','spec-only','self-index','typed-graph','typed-mutations','typed-dependencies','typed-profiles','explorer','explorer-shell','explorer-gu0','records','ui_map','ui_preview','ui_interact','ui_pick','ui_reverse','ui_hmr','ui_records')
assert os.getpid()==1, 'run broker in a fresh private PID/mount/network namespace'
import importlib.util,sys
sys.dont_write_bytecode=True
module_path=pathlib.Path(__file__).resolve().parent/'native_security.py'
spec=importlib.util.spec_from_file_location('mce_local_security',module_path)
security=importlib.util.module_from_spec(spec);spec.loader.exec_module(security)
if suite in ('scale','bench'):
 scale_spec=importlib.util.spec_from_file_location('mce_native_scale',module_path.parent/'native_scale.py')
 scale=importlib.util.module_from_spec(scale_spec);scale_spec.loader.exec_module(scale)
if suite in ('bench','bench-context','bench-retention'):
 sys.path.insert(0,str(module_path.parents[2]/'tools/bench'))
 import native_matrix
if suite in ('spec-only','self-index'):
 self_spec=importlib.util.spec_from_file_location('mce_self_index_native',module_path.parent/'self_index_native.py')
 self_index=importlib.util.module_from_spec(self_spec);self_spec.loader.exec_module(self_index)
if suite in ('typed-graph','typed-mutations','typed-dependencies','typed-profiles'):
 graph_spec=importlib.util.spec_from_file_location('mce_typed_graph',module_path.parent/'native_typed_graph.py')
 typed_graph=importlib.util.module_from_spec(graph_spec);graph_spec.loader.exec_module(typed_graph)
fixture=json.loads(module_path.parents[2].joinpath('testdata/malicious/security.json').read_text())
fixture['expected']=json.loads(module_path.parents[2].joinpath('testdata/malicious/expected.json').read_text())
assert os.getuid()==0 and not root.exists()
assert [n for _,n in socket.if_nameindex()]==['lo']
subprocess.run(['/usr/bin/mount','--make-rprivate','/'],check=True,timeout=5)
root.mkdir(mode=0o755);root.chmod(0o755)
for name,uid,mode in [('profiles',0,0o755),('target',1001,0o755),('result',1000,0o700),('host-config',1000,0o700),('proc',0,0o755),('tmp',0,0o1777),('dev',0,0o755),('etc',0,0o755),('bin',0,0o755)]:
 p=root/name;p.mkdir();os.chown(p,uid,uid);p.chmod(mode)
for name,expected in approved['binaries'].items():
 src=built/name;assert hashlib.sha256(src.read_bytes()).hexdigest()==expected
 (root/name).write_bytes(src.read_bytes());(root/name).chmod(0o755)
toolchain=pathlib.Path(os.environ['MCE4D_ACCEPTANCE_GO_ROOT']).resolve(strict=True)
assert not any(p.is_symlink() for p in toolchain.rglob('*'))
shutil.copytree(toolchain,root/'toolchain')
gopls=pathlib.Path(os.environ['MCE4D_ACCEPTANCE_GOPLS']).resolve(strict=True)
assert hashlib.sha256(gopls.read_bytes()).hexdigest()=='b94804c7a4f74bcf0765263cb022b78459fc61258fe30c67607106af96fa5029'
(root/'bin/gopls').write_bytes(gopls.read_bytes());(root/'bin/gopls').chmod(0o755)
git_binary=pathlib.Path('/usr/bin/git');(root/'git').write_bytes(git_binary.read_bytes());(root/'git').chmod(0o755)
ldd=subprocess.check_output(['/usr/bin/ldd',str(git_binary)],text=True,timeout=5)
for name in set(re.findall(r'(/[A-Za-z0-9_./+-]+)',ldd)):
 p=pathlib.Path(name);assert p.is_file();dest=root/name.lstrip('/');assert dest.resolve().is_relative_to(root.resolve());dest.parent.mkdir(parents=True,exist_ok=True);dest.write_bytes(p.read_bytes());dest.chmod(0o755)
 for parent in dest.parents:
  if parent==root:break
  parent.chmod(0o755)
shell=pathlib.Path('/usr/bin/dash');(root/'bin/sh').write_bytes(shell.read_bytes());(root/'bin/sh').chmod(0o755)
os.mknod(root/'dev/null',stat.S_IFCHR|0o666,os.makedev(1,3));(root/'dev/null').chmod(0o666)
subprocess.run(['/usr/bin/mount','-t','proc','-o','nosuid,nodev,noexec','proc',str(root/'proc')],check=True,timeout=5)
template=run/'git-template';template.mkdir(mode=0o700)
subprocess.run(['/usr/bin/git','init','--template='+str(template),str(root/'target')],check=True,env={'PATH':'/usr/bin:/bin','HOME':str(run/'home'),'GIT_CONFIG_NOSYSTEM':'1','GIT_CONFIG_GLOBAL':'/dev/null'},stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=5)
sources={'go.mod':'module example.invalid/renamefixture\n\ngo 1.27\n','a.go':'package demo\nimport "fmt"\ntype Inner struct { Value int }\ntype Outer struct { Inner }\nfunc Show(x Inner) string { return fmt.Sprint(x.Value) }\n','b.go':'package demo\nvar object = Outer{Inner: Inner{Value: 1}}\nfunc Read() int { return object.Value }\n','AGENTS.md':'Synthetic project: preserve public contract names.\n'}
# Authored large type body forces real CT-05 expansion without changing edits.
sources['a.go']=sources['a.go'].replace('type Inner struct { Value int }','type Inner struct { Value int\n'+''.join('// local synthetic source evidence %04d\n'%i for i in range(650))+'}\n')
sources['untrusted.md']=fixture['untrusted_document']
sources['execute-me.sh']='#!/bin/sh\nprintf executed > /result/repo-executed\n'
sources['go.env.txt']='GOFLAGS=-mod=mod -toolexec=/target/execute-me.sh\n'
if suite in ('scale','bench'):
 authored=scale.prepare(root,os.environ['MCE4D_SCALE_COHORT'])
 peer=root/'peer';peer.mkdir();peer.chmod(0o755)
 subprocess.run(['/usr/bin/git','init','--template='+str(template),str(peer)],check=True,env={'PATH':'/usr/bin:/bin','HOME':str(run/'home'),'GIT_CONFIG_NOSYSTEM':'1','GIT_CONFIG_GLOBAL':'/dev/null'},stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=5)
 (peer/'go.mod').write_text('module example.invalid/scalepeer\n\ngo 1.27\n')
 (peer/'peer.go').write_text('package peer\nfunc Peer() int {return 1}\n')
 for p in [peer]+list(peer.rglob('*')):
  p.chmod(0o755 if p.is_dir() else 0o644);os.chown(p,1001,1001)
elif suite in ('spec-only','self-index'):
 provenance=self_index.prepare(root,suite)
elif suite in ('typed-graph','typed-mutations','typed-dependencies'):
 typed_graph.prepare(root/'target')
elif suite=='typed-profiles':
 typed_graph.prepare_profiles(root/'target')
else:
 for name,body in sources.items():(root/'target'/name).write_text(body)
if suite in ('ui_map','ui_preview','ui_interact','ui_pick','ui_reverse','ui_hmr','ui_records'):
 sys.path.insert(0,str(module_path.parent))
 import native_ui_map
 ui_source=native_ui_map.SOURCE_MANY if suite=='ui_reverse' else native_ui_map.SOURCE
 (root/'target/entry.tsx').write_text(ui_source)
for p in [root/'target']+list((root/'target').rglob('*')):
 p.chmod(0o755 if p.is_dir() else 0o644);os.chown(p,1001,1001)
if suite in ('security','gc','backup','restore','explorer-gu0','ui_map','ui_preview','ui_interact','ui_pick','ui_reverse','ui_hmr','ui_records'):(root/'target/execute-me.sh').chmod(0o755)
(root/'host-config/config.toml').write_bytes(b'synthetic existing host config\n')
def tree(p):return {str(f.relative_to(p)):hashlib.sha256(f.read_bytes()).hexdigest() for f in p.rglob('*') if f.is_file()}
before=tree(root/'target');before_config=tree(root/'host-config');active=[]
def setup(uid):
 def enter():os.chroot(root);os.chdir('/');os.setgroups([]);os.setgid(uid);os.setuid(uid)
 return enter
def command(binary,args,uid=1000,payload=None,timeout=90,extra=None,measure=False):
 env={'PATH':'/toolchain/bin','HOME':'/profiles/runtime/home','GOMAXPROCS':'4','MODERNC_SQLITE_OFD_LOCK':'0','GOTOOLCHAIN':'local','GOTELEMETRY':'off'}
 env.update(extra or {})
 start=time.monotonic();body=None if payload is None else json.dumps(payload)
 if measure:
  assert suite in ('scale','bench')
  p,p.metrics=scale.measure(root/'proc',[binary]+args,timeout,setup(uid),env,body)
 else:
  p=subprocess.run([binary]+args,preexec_fn=setup(uid),env=env,input=body,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=timeout)
 item={'binary':binary,'args':args,'uid':uid,'exit':p.returncode,'seconds':round(time.monotonic()-start,3),'stdout':p.stdout,'stderr':p.stderr};print(json.dumps(item),flush=True)
 with (run/'commands.jsonl').open('a') as f:f.write(json.dumps(item)+'\n')
 return p
try:
 p=command('/go-toolchain',['--root','/toolchain']);assert p.returncode==0
 toolchain_digest=json.loads(p.stdout)['toolchain_digest']
 # Independent serialization oracle over the exact copied administrator tree.
 h=hashlib.sha256(b'MCE4D-NATIVE-GO-TOOLCHAIN1\0');entries=[root/'toolchain']+list((root/'toolchain').rglob('*'))
 for p in sorted(entries,key=lambda p:'.' if p==root/'toolchain' else str(p.relative_to(root/'toolchain'))):
  name='.' if p==root/'toolchain' else str(p.relative_to(root/'toolchain'));st=p.stat();kind=b'd' if p.is_dir() else b'f';size=0 if p.is_dir() else st.st_size;digest=b'\0'*32 if p.is_dir() else hashlib.sha256(p.read_bytes()).digest();raw=name.encode();h.update(struct.pack('>Q',len(raw))+raw+kind+struct.pack('>QQ',stat.S_IMODE(st.st_mode),size)+digest)
 assert h.hexdigest()==toolchain_digest,'toolchain independent fingerprint mismatch'
 targets=['--target-root','/target']
 config={}
 if suite in ('scale','bench'):
  targets+=['--target-root','/peer']
  config={'writer_lock_wait_ms':200,'index_timeout_ms':300000,'max_parse_workers':4,'max_output_bytes':65536}
  if authored['cohort'] in scale.PACK_COHORTS:config['max_output_bytes']=131072
  if authored['cohort']=='C029Extended':config['query_timeout_ms']=20000
 if suite=='bench-retention':
  config={'pack_ttl_seconds':native_matrix.TEST_PACK_TTL,'rename_result_ttl_seconds':native_matrix.TEST_RENAME_TTL}
 if suite=='ui_hmr':
  config={'writer_lock_wait_ms':200}
 p=command('/state-profile',['create','--root','/profiles/runtime','--engine-uid','1000','--engine-gid','1000','--client-uid','1001',*targets,'--worker-executable','/mce4d'],0);assert p.returncode==0
 config_patch={'schema_version':1,'core':config}
 if suite=='ui_hmr':config_patch['explorer']={'map_watch_poll_ms':50,'map_watch_debounce_ms':250}
 p=command('/mce4d',['config','set','--input-json','-'],payload={'action':'set','config':config_patch,'expected_digest':None});assert p.returncode==0
 config_digest=json.loads(p.stdout)['data']['config_digest']
 p=command('/mce4d',['repo','add','--root','/target']);assert p.returncode==0;worktree=re.search(r'Registered worktree ([0-9a-f-]{36})',p.stdout).group(1)
 policy={'schema_version':1,'allowed_roots':['/target'],'denied_globs':[],'trusted_rule_paths':['AGENTS.md'],'dependency_roots':[],'allowed_executables':[{'path':'/git','sha256':hashlib.sha256((root/'git').read_bytes()).hexdigest()},{'path':'/toolchain/bin/go','sha256':hashlib.sha256((root/'toolchain/bin/go').read_bytes()).hexdigest()},{'path':'/bin/gopls','sha256':hashlib.sha256((root/'bin/gopls').read_bytes()).hexdigest()}],'allow_target_execution':False,'allow_runtime_download':False,'max_file_bytes':65536,'max_source_bytes':2097152,'max_output_bytes':65536,'secret_rules_version':'builtin-v1'}
 if suite in ('scale','bench'):policy['max_source_bytes']=32<<20
 if suite=='scale' and authored['cohort'] in scale.PACK_COHORTS:policy['max_output_bytes']=config['max_output_bytes']
 if suite in ('spec-only','self-index'):policy['trusted_rule_paths']=['design.md'] if suite=='spec-only' else []
 p=command('/mce4d',['policy','set','--input-json','-'],payload={'worktree_id':worktree,'expected_epoch':1,'policy':policy});assert p.returncode==0
 profile={'schema_version':1,'language':'go','goos':'linux','goarch':'amd64','build_tags':[],'cgo_enabled':False,'tests_enabled':False,'module_roots':['.'],'toolchain_digest':toolchain_digest,'provider_digest':approved['binaries']['mce4d'],'module_mode':'readonly','analysis_mode':'typed_when_available','dependency_roots':[]}
 p=command('/mce4d',['profile','set','--worktree-id',worktree,'--input-json','-'],payload={'worktree_id':worktree,'profile_name':'typed','expected_revision':0,'profile':profile});assert p.returncode==0;profile_id=json.loads(p.stdout)['data']['profile_id']
 p=command('/state-profile',['worktree-create','--root','/profiles/runtime','--worktree-id',worktree],0);assert p.returncode==0
 if suite in ('ui_map','ui_preview','ui_interact','ui_pick','ui_reverse','ui_hmr','ui_records'):
  maps=native_ui_map.import_before_index(root,command,worktree,profile_id,module_path.parents[2],ui_source,hmr=suite=='ui_hmr')
 if suite in ('explorer-shell','explorer-gu0'):
  subprocess.run(['/usr/sbin/ip','link','set','lo','up'],check=True,timeout=5)
  sys.path.insert(0,str(module_path.parent))
  from explorer_shell import exercise_shell
  exercise_shell(root,setup,worktree,profile_id,active,"empty")
 if suite not in ('scale','bench'):
  p=command('/mce4d',['index','--worktree-id',worktree,'--profile-id',profile_id]);assert p.returncode==0
 prefix=['--worktree-id',worktree,'--profile-id',profile_id,'query']
 request={'selector':{'kind':'source_position','path':'a.go','content_hash':hashlib.sha256(sources['a.go'].encode()).hexdigest(),'byte_offset':sources['a.go'].index('Value')},'new_name':'Count','preserve_contracts':True,'max_output_bytes':65536}
 if suite=='security':
  security.exercise(root,command,setup,worktree,profile_id,request,fixture)
 elif suite=='scale':
  scale.exercise(root,command,worktree,profile_id,authored,profile,policy,config)
 elif suite=='bench':
  native_matrix.exercise(root,command,worktree,profile_id,authored,config,scale)
 elif suite=='bench-context':
  native_matrix.exercise_context(root,setup,worktree,profile_id,security)
 elif suite=='bench-retention':
  native_matrix.exercise_retention(root,setup,worktree,profile_id,request,security)
 elif suite=='explorer':
  subprocess.run(['/usr/sbin/ip','link','set','lo','up'],check=True,timeout=5)
  sys.path.insert(0,str(module_path.parent))
  from explorer_native import exercise
  exercise(root,setup,worktree,profile_id,active)
  assert tree(root/'target')==before and tree(root/'host-config')==before_config
 elif suite in ('explorer-shell','explorer-gu0'):
  exercise_shell(root,setup,worktree,profile_id,active,"indexed", gu0=suite=='explorer-gu0')
  if suite=='explorer-gu0':
   from explorer_native import exercise
   config_patch={'schema_version':1,'core':config,'explorer':{'bearer_ttl_seconds':16}}
   p=command('/mce4d',['config','set','--input-json','-'],payload={'action':'set','config':config_patch,'expected_digest':config_digest})
   assert p.returncode==0
   exercise(root,setup,worktree,profile_id,active,gu0=True)
  assert tree(root/'target')==before and tree(root/'host-config')==before_config
  if suite=='explorer-gu0':
   canary=root/'target/gu0-denied.txt'
   canary.write_bytes(b'T084_SYNTHETIC_DENIED_CANARY\n')
   canary.chmod(0o644);os.chown(canary,1001,1001)
   before[canary.name]=hashlib.sha256(canary.read_bytes()).hexdigest()
   policy['denied_globs']=[canary.name]
   p=command('/mce4d',['policy','set','--input-json','-'],payload={'worktree_id':worktree,'expected_epoch':2,'policy':policy})
   assert p.returncode==0
   p=command('/mce4d',['index','--worktree-id',worktree,'--profile-id',profile_id]);assert p.returncode==0
   exercise(root,setup,worktree,profile_id,active,gu0=True,canary_only=True)
   assert tree(root/'target')==before and tree(root/'host-config')==before_config
   assert not (root/'result/repo-executed').exists()
   print(json.dumps({'check':'GU0_CANARY','status':'PASS','target_files':len(before),'host_config_files':len(before_config),'source_mutations':0,'target_executions':0}),flush=True)
 elif suite=='spec-only':
  self_index.check_spec_only(root,command,worktree,profile_id)
  assert tree(root/'target')==before
 elif suite=='self-index':
  self_index.check_self_index(root,command,worktree,profile_id,profile,provenance,approved)
  assert tree(root/'target')==before
 elif suite=='typed-graph':
  typed_graph.exercise(root,command,worktree,profile_id)
  typed_graph.check_text_query(root,command,worktree,profile_id)
  typed_graph.check_rename_bindings(root,command,worktree,profile_id)
  typed_graph.check_unproved_toolchain(root,command,worktree,profile_id,profile)
  assert tree(root/'target')==before
 elif suite=='typed-mutations':
  typed_graph.check_mutations(root,command,worktree,profile_id)
 elif suite=='typed-dependencies':
  typed_graph.check_dependency_updates(root,command,worktree,profile_id)
 elif suite=='typed-profiles':
  typed_graph.check_profiles(root,command,worktree,profile_id,profile)
  assert tree(root/'target')==before
 elif suite=='ui_map':
  native_ui_map.exercise(root,command,worktree,profile_id,maps)
  assert tree(root/'target')==before and tree(root/'host-config')==before_config
  assert not (root/'result/repo-executed').exists()
 elif suite in ('ui_preview','ui_interact','ui_pick','ui_reverse','ui_hmr','ui_records'):
  subprocess.run(['/usr/sbin/ip','link','set','lo','up'],check=True,timeout=5)
  import native_ui_preview
  native_ui_preview.exercise(root,command,setup,worktree,profile_id,maps[0],active,interact=suite=='ui_interact',
                             pick=suite in ('ui_pick','ui_reverse','ui_hmr'),reverse=suite=='ui_reverse',
                             hmr_maps=maps[2:] if suite=='ui_hmr' else (),records_mode=suite=='ui_records')
  assert tree(root/'target')==before and tree(root/'host-config')==before_config
  assert not (root/'result/repo-executed').exists()
 else:
  scenario_path=module_path.parent/('native_'+suite+'.py')
  scenario_spec=importlib.util.spec_from_file_location('mce_local_'+suite,scenario_path)
  scenario=importlib.util.module_from_spec(scenario_spec);scenario_spec.loader.exec_module(scenario)
  scenario.exercise(root,command,setup,worktree,profile_id,request,fixture,security)

finally:
 for child in active:
  if child.poll() is None:child.kill()
  child.wait(timeout=5)
 for head in (root/'profiles').rglob('wal-head.log'):
  fd=os.open(head,os.O_RDONLY)
  try:
   flags=struct.unpack('I',fcntl.ioctl(fd,0x80086601,struct.pack('I',0))[:4])[0]
   if flags&0x20:fcntl.ioctl(fd,0x40086602,struct.pack('I',flags&~0x20));os.fsync(fd)
  finally:os.close(fd)
 subprocess.run(['/usr/bin/umount',str(root/'proc')],check=True,timeout=5)
 print('PRIVATE_FLAGS_AND_MOUNT_CLEANED',flush=True)
