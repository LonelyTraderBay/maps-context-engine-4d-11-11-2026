"""Root-only synthetic broker. Requires the owned guard/namespace setup in README.
Never invoke against an existing state or target directory.
"""
import os,pathlib,subprocess,hashlib,fcntl,struct,stat,json,socket,time,re,shutil
run=pathlib.Path(os.environ['MCE4D_ACCEPTANCE_RUN']).resolve(strict=True);root=run/'rootfs';built=pathlib.Path(os.environ['MCE4D_ACCEPTANCE_BUILD']).resolve(strict=True)
approved=json.loads(pathlib.Path(os.environ['MCE4D_ACCEPTANCE_APPROVALS']).read_text())
assert set(approved['binaries'])=={'mce4d','state-profile','go-toolchain'}
assert os.getpid()==1, 'run broker in a fresh private PID/mount/network namespace'
import importlib.util,sys
sys.dont_write_bytecode=True
module_path=pathlib.Path(__file__).resolve().parent/'native_security.py'
spec=importlib.util.spec_from_file_location('mce_local_security',module_path)
security=importlib.util.module_from_spec(spec);spec.loader.exec_module(security)
fixture=json.loads(module_path.parents[2].joinpath('testdata/malicious/security.json').read_text())
fixture['expected']=json.loads(module_path.parents[2].joinpath('testdata/malicious/expected.json').read_text())
assert os.getuid()==0 and not root.exists()
assert [n for _,n in socket.if_nameindex()]==['lo']
subprocess.run(['/usr/bin/mount','--make-rprivate','/'],check=True,timeout=5)
root.mkdir(mode=0o755)
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
for name,body in sources.items():(root/'target'/name).write_text(body)
for p in [root/'target']+list((root/'target').rglob('*')):os.chown(p,1001,1001)
(root/'target/execute-me.sh').chmod(0o755)
(root/'host-config/config.toml').write_bytes(b'synthetic existing host config\n')
def tree(p):return {str(f.relative_to(p)):hashlib.sha256(f.read_bytes()).hexdigest() for f in p.rglob('*') if f.is_file()}
before=tree(root/'target');before_config=tree(root/'host-config');active=[]
def setup(uid):
 def enter():os.chroot(root);os.chdir('/');os.setgroups([]);os.setgid(uid);os.setuid(uid)
 return enter
def command(binary,args,uid=1000,payload=None,timeout=90,extra=None):
 env={'PATH':'/toolchain/bin','HOME':'/profiles/runtime/home','GOMAXPROCS':'4','MODERNC_SQLITE_OFD_LOCK':'0','GOTOOLCHAIN':'local','GOTELEMETRY':'off'}
 env.update(extra or {})
 start=time.monotonic();p=subprocess.run([binary]+args,preexec_fn=setup(uid),env=env,input=None if payload is None else json.dumps(payload),stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=timeout)
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
 p=command('/state-profile',['create','--root','/profiles/runtime','--engine-uid','1000','--engine-gid','1000','--client-uid','1001','--target-root','/target','--worker-executable','/mce4d'],0);assert p.returncode==0
 p=command('/mce4d',['config','set','--input-json','-'],payload={'action':'set','config':{'schema_version':1,'core':{}},'expected_digest':None});assert p.returncode==0
 p=command('/mce4d',['repo','add','--root','/target']);assert p.returncode==0;worktree=re.search(r'Registered worktree ([0-9a-f-]{36})',p.stdout).group(1)
 policy={'schema_version':1,'allowed_roots':['/target'],'denied_globs':[],'trusted_rule_paths':['AGENTS.md'],'dependency_roots':[],'allowed_executables':[{'path':'/git','sha256':hashlib.sha256((root/'git').read_bytes()).hexdigest()},{'path':'/toolchain/bin/go','sha256':hashlib.sha256((root/'toolchain/bin/go').read_bytes()).hexdigest()},{'path':'/bin/gopls','sha256':hashlib.sha256((root/'bin/gopls').read_bytes()).hexdigest()}],'allow_target_execution':False,'allow_runtime_download':False,'max_file_bytes':65536,'max_source_bytes':2097152,'max_output_bytes':65536,'secret_rules_version':'builtin-v1'}
 p=command('/mce4d',['policy','set','--input-json','-'],payload={'worktree_id':worktree,'expected_epoch':1,'policy':policy});assert p.returncode==0
 profile={'schema_version':1,'language':'go','goos':'linux','goarch':'amd64','build_tags':[],'cgo_enabled':False,'tests_enabled':False,'module_roots':['.'],'toolchain_digest':toolchain_digest,'provider_digest':approved['binaries']['mce4d'],'module_mode':'readonly','analysis_mode':'typed_when_available','dependency_roots':[]}
 p=command('/mce4d',['profile','set','--worktree-id',worktree,'--input-json','-'],payload={'worktree_id':worktree,'profile_name':'typed','expected_revision':0,'profile':profile});assert p.returncode==0;profile_id=json.loads(p.stdout)['data']['profile_id']
 p=command('/state-profile',['worktree-create','--root','/profiles/runtime','--worktree-id',worktree],0);assert p.returncode==0
 p=command('/mce4d',['index','--worktree-id',worktree,'--profile-id',profile_id]);assert p.returncode==0
 prefix=['--worktree-id',worktree,'--profile-id',profile_id,'query']
 request={'selector':{'kind':'source_position','path':'a.go','content_hash':hashlib.sha256(sources['a.go'].encode()).hexdigest(),'byte_offset':sources['a.go'].index('Value')},'new_name':'Count','preserve_contracts':True,'max_output_bytes':65536}
 security.exercise(root,command,setup,worktree,profile_id,request,fixture)

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
