"""Fixed development checks, run inside main.py's approved output/environment.

No installation or target discovery. Caller supplies the approved absolute Go
binary and namespace; one job shares only its private build cache across cases.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time


def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--go',required=True)
    args=parser.parse_args();go=Path(args.go)
    if not go.is_absolute() or not go.is_file():raise ValueError('explicit approved Go executable required')
    output=Path(os.environ['HOME']).parent
    if not (output/'record.json').is_file() or output.stat().st_mode & 0o077:raise ValueError('owned guard job required')
    commands=[('module_verify',['mod','verify'],{}),('full_suite',['test','-v','-count=1','-timeout=420s','./...'],{}),('vet',['vet','./...'],{})]
    for name,package in [('mce4d','./cmd/mce4d'),('state-profile','./tools/state_profile'),('go-toolchain','./tools/go_toolchain')]:
        commands.append((name,['build','-o',str(output/name),package],{}))
    for name,package in [('sqlite','./internal/store/sqlite'),('compose','./internal/compose')]:
        commands.append((name+'-test',['test','-c','-o',str(output/(name+'.test')),package],{}))
    for name,package in [('cli','./internal/cli'),('index','./internal/store/sqlite/index'),('catalog','./internal/catalog'),('records','./internal/store/sqlite/records'),('sqlite','./internal/store/sqlite'),('compose','./internal/compose')]:
        commands.append(('windows-'+name,['test','-c','-o',str(output/(name+'-win.test.exe')),package],{'GOOS':'windows','GOARCH':'amd64'}))
    for name,package in [('mce4d','./cmd/mce4d'),('state-profile','./tools/state_profile'),('go-toolchain','./tools/go_toolchain')]:
        commands.append(('windows-'+name,['build','-o',str(output/(name+'-win.exe')),package],{'GOOS':'windows','GOARCH':'amd64'}))
    commands.append(('spec_check',['run','./tools/spec_check'],{}))
    report={'status':'RUNNING','go_sha256':hashlib.sha256(go.read_bytes()).hexdigest(),'cases':[]}
    for name,argv,extra in commands:
        print('START '+name,flush=True);env=os.environ.copy();env.update(extra);start=time.monotonic()
        result=subprocess.run([str(go),*argv],env=env)
        report['cases'].append({'case':name,'argv':argv,'exit':result.returncode,'seconds':round(time.monotonic()-start,3)})
        report['status']='FAIL' if result.returncode else 'RUNNING'
        (output/'go-verification.json').write_text(json.dumps(report,indent=2)+'\n')
        if result.returncode:return result.returncode if result.returncode>0 else 1
    report['status']='PASS';(output/'go-verification.json').write_text(json.dumps(report,indent=2)+'\n');return 0


if __name__=='__main__':sys.exit(main())
