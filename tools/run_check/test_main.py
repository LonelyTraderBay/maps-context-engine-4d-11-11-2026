"""Small controlled tests. Retain artifacts under an explicitly supplied root."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location('guard_under_test',Path(__file__).with_name('main.py'))
guard = importlib.util.module_from_spec(spec); spec.loader.exec_module(guard)


class GuardTest(unittest.TestCase):
    def setUp(self):
        value = os.environ.get('MCE4D_CHECK_TEST_ROOT')
        if not value: self.skipTest('requires explicit private local synthetic root')
        self.base = Path(tempfile.mkdtemp(prefix='case-',dir=guard.directory(value,True)))
        self.root = self.base/'output'; self.root.mkdir(mode=0o700)
        self.cwd = self.base/'source'; self.cwd.mkdir(mode=0o700)
        (self.cwd/'canary.txt').write_text('synthetic source, preserve\n')

    def args(self,code,**patch):
        result=argparse.Namespace(root=self.root,cwd=self.cwd,peak_bytes=4*1024**2,reserve_bytes=0,block_bytes=0,warning_bytes=0,seconds=5,log_bytes=4096,retained_bytes=64*1024**2,max_runs=16,max_age_seconds=86400,module_cache=None,backing_root=Path('/mnt/c') if 'microsoft' in Path('/proc/sys/kernel/osrelease').read_text().lower() else None,env=[],report_only=False,command=[sys.executable,'-c',code])
        for key,value in patch.items():setattr(result,key,value)
        return result

    def cli(self,args):
        flags=[sys.executable,str(Path(__file__).with_name('main.py')),'--root',str(args.root),'--cwd',str(args.cwd),'--peak-bytes',str(args.peak_bytes),'--reserve-bytes','0','--block-bytes','0','--warning-bytes','0','--seconds',str(args.seconds),'--log-bytes',str(args.log_bytes),'--retained-bytes',str(args.retained_bytes)]
        if args.backing_root:flags+=['--backing-root',str(args.backing_root)]
        return flags+['--']+args.command

    def test_budget_boundaries_and_overflow(self):
        self.assertEqual(guard.admission(100,40,30,30,10,100),(True,False))
        self.assertEqual(guard.admission(99,40,30,30,10,100),(False,True))
        for values in ((100,-1,0,0,0,0),(100,1,0,0,2,1),(100,guard.MAX,1,0,0,0),(None,1,0,0,0,0)):
            with self.assertRaises(ValueError):guard.admission(*values)

    def test_low_and_unreadable_capacity_precede_mutation(self):
        descriptors=len(os.listdir('/proc/self/fd'))
        with mock.patch.object(guard.os,'statvfs',return_value=argparse.Namespace(f_bavail=0,f_frsize=4096)):
            for _ in range(8):
                with self.assertRaises(ValueError):guard.run(self.args('raise Exception("must not start")'))
        self.assertEqual(list(self.root.iterdir()),[])
        self.assertEqual(len(os.listdir('/proc/self/fd')),descriptors)
        with mock.patch.object(guard.os,'statvfs',side_effect=OSError('synthetic unreadable meter')):
            with self.assertRaises(OSError):guard.run(self.args('raise Exception("must not start")'))
        self.assertEqual(list(self.root.iterdir()),[])

    def test_report_only_creates_nothing_and_does_not_execute(self):
        result=guard.run(self.args('open("workload-started","w").write("bad")',report_only=True))
        self.assertEqual(result['status'],'REPORT_ONLY');self.assertFalse(result['workload_started'])
        self.assertEqual(list(self.root.iterdir()),[]);self.assertFalse((self.cwd/'workload-started').exists())

    def test_check_name_does_not_authorize_a_writer(self):
        script = self.cwd / "check"
        script.write_text('#!/bin/sh\nprintf written > created-by-check\n')
        script.chmod(0o700)
        args = self.args("pass", command=["./check"])
        with self.assertRaisesRegex(ValueError, "absolute reviewed executable"):
            guard.run(args)
        args.command = [str(script), "--dry-run"]
        args.report_only = True
        self.assertEqual(guard.run(args)["status"], "REPORT_ONLY")
        self.assertFalse((self.cwd / "created-by-check").exists())
        self.assertEqual(list(self.root.iterdir()), [])

    def test_alias_volume_is_not_double_charged(self):
        args=self.args('pass',backing_root=self.root)
        volumes=guard.plan(args,{'runs':[]})
        self.assertEqual(len(volumes),1);self.assertEqual(volumes[0]['peak'],args.peak_bytes)
        if Path('/mnt/c').exists() and self.root.stat().st_dev!=Path('/mnt/c').stat().st_dev:
            args.backing_root=Path('/mnt/c');volumes=guard.plan(args,{'runs':[]})
            self.assertEqual(len(volumes),2);self.assertTrue(all(v['peak']==args.peak_bytes for v in volumes))

    def test_streaming_meter_bounds_entries_and_never_follows_link(self):
        link=self.root/'outside-link';link.symlink_to(self.cwd,target_is_directory=True)
        info=link.lstat();self.assertEqual(guard.measure(self.root),max(info.st_size,info.st_blocks*512))
        descriptors=len(os.listdir('/proc/self/fd'))
        with self.assertRaises(ValueError):guard.measure(self.root,limit=0)
        self.assertEqual(len(os.listdir('/proc/self/fd')),descriptors)

    def test_original_nonzero_and_retained_deliverable(self):
        result=guard.run(self.args('import os,sys;open(os.environ["HOME"]+"/golden.expected","w").write("unique fixture");sys.exit(7)'))
        self.assertEqual(result['status'],'FAIL');self.assertEqual(result['command_exit_status'],7)
        self.assertEqual(result['finalization_status'],'PASS')
        self.assertEqual((Path(result['output'])/'home/golden.expected').read_text(),'unique fixture')

    def test_cli_propagates_original_nonzero(self):
        result=subprocess.run(self.cli(self.args('import sys;sys.exit(7)')),stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=10)
        self.assertEqual(result.returncode,7);self.assertEqual(json.loads(result.stdout)['command_exit_status'],7)

    def test_log_cap_does_not_return_false_success(self):
        result=guard.run(self.args('import os;os.write(1,b"x"*2000000)',log_bytes=1024))
        self.assertEqual(result['status'],'FAIL');self.assertEqual(result['termination_reason'],'log_cap')
        self.assertEqual((Path(result['output'])/'command.log').stat().st_size,1024)
        self.assertEqual(result['finalization_status'],'PASS')

    def test_fast_growth_is_checked_after_child_exit(self):
        result=guard.run(self.args('import os;open(os.environ["HOME"]+"/small-output","wb").write(b"x"*200000)',peak_bytes=65536))
        self.assertEqual(result['status'],'FAIL');self.assertEqual(result['termination_reason'],'output_growth')
        self.assertEqual(result['finalization_status'],'PASS')

    def test_command_failure_and_finalization_failure_preserve_primary(self):
        original=guard.atomic;calls=[]
        def fail_late(path,value):
            calls.append(path)
            if len(calls)==3:raise PermissionError('controlled finalization refusal')
            return original(path,value)
        with mock.patch.object(guard,'atomic',side_effect=fail_late):result=guard.run(self.args('import sys;sys.exit(9)'))
        self.assertEqual(result['command_exit_status'],9);self.assertEqual(result['finalization_status'],'FAIL')
        self.assertEqual(result['status'],'FAIL');self.assertEqual(result['finalization_error'],'PermissionError')

    def test_success_cannot_hide_finalization_failure(self):
        original=guard.atomic;calls=[]
        def fail_late(path,value):
            calls.append(path)
            if len(calls)==3:raise OSError('controlled finalization failure')
            return original(path,value)
        with mock.patch.object(guard,'atomic',side_effect=fail_late):result=guard.run(self.args('pass'))
        self.assertEqual(result['command_exit_status'],0);self.assertEqual(result['status'],'FAIL')
        self.assertEqual(result['finalization_status'],'FAIL')

    def test_deadline_stops_owned_child(self):
        result=guard.run(self.args('import time;time.sleep(60)',seconds=.15))
        self.assertEqual(result['termination_reason'],'deadline');self.assertEqual(result['finalization_status'],'PASS')

    def test_count_limit_retains_prior_output(self):
        result=guard.run(self.args('pass',max_runs=1))
        with self.assertRaises(ValueError):guard.run(self.args('pass',max_runs=1))
        self.assertTrue(Path(result['output']).exists())

    def test_killed_guard_does_not_claim_recovery_or_delete_leftover(self):
        code='import os,time;open(os.environ["HOME"]+"/started","w").write("owned fake bytes");time.sleep(.4)'
        command=subprocess.Popen(self.cli(self.args(code)),stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
        try:
            until=time.monotonic()+5;started=None
            while time.monotonic()<until:
                found=list(self.root.glob('run-*/home/started'))
                if found:started=found[0];break
                time.sleep(.01)
            self.assertIsNotNone(started);command.kill();command.communicate(timeout=3)
            time.sleep(.5)  # authored child has its own short finite lifetime
            report=guard.run(self.args('pass',report_only=True))
            self.assertIn('uncertain prior run',report['admission_blocker'])
            self.assertEqual(report['retained'][0]['status'],'RUNNING');self.assertTrue(started.exists())
            self.assertFalse(report['retained'][0]['removal_authorized'])
        finally:
            if command.poll() is None:command.kill();command.wait(timeout=3)

    def test_link_root_and_unknown_contents_refused(self):
        link=self.base/'link';link.symlink_to(self.root,target_is_directory=True)
        with self.assertRaises(ValueError):guard.run(self.args('pass',root=link))
        (self.root/'unique-record.json').write_text('preserve')
        with self.assertRaises(ValueError):guard.run(self.args('pass'))
        self.assertEqual((self.root/'unique-record.json').read_text(),'preserve')

    def test_run_replacement_cannot_redirect_final_record_into_source(self):
        code='import os,pathlib;p=pathlib.Path(os.environ["HOME"]).parent;p.rename(str(p)+".moved");p.symlink_to(pathlib.Path.cwd(),target_is_directory=True)'
        result=guard.run(self.args(code))
        self.assertEqual(result['status'],'FAIL');self.assertEqual(result['finalization_status'],'FAIL')
        self.assertFalse((self.cwd/'record.json').exists())
        self.assertEqual((self.cwd/'canary.txt').read_text(),'synthetic source, preserve\n')

    def test_cancel_stops_detached_descendant_writer(self):
        code='import os,time\np=os.fork()\nif p==0:\n os.setsid()\n for i in range(500):\n  with open(os.environ["HOME"]+"/writer","ab",buffering=0) as f:f.write(b"x")\n  time.sleep(.01)\nelse:time.sleep(60)\n'
        command=subprocess.Popen(self.cli(self.args(code,seconds=10)),stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
        try:
            until=time.monotonic()+5;writer=None
            while time.monotonic()<until:
                found=list(self.root.glob('run-*/home/writer'))
                if found:writer=found[0];break
                time.sleep(.02)
            self.assertIsNotNone(writer)
            command.send_signal(signal.SIGINT);out,err=command.communicate(timeout=10)
            result=json.loads(out);self.assertEqual(result['termination_reason'],'cancelled')
            self.assertEqual(result['finalization_status'],'PASS');self.assertGreaterEqual(result['max_owned_processes_observed'],2)
            length=writer.stat().st_size;time.sleep(.2);self.assertEqual(writer.stat().st_size,length)
        finally:
            if command.poll() is None:command.kill();command.wait(timeout=3)

    def test_concurrent_jobs_have_scoped_remaining_reservations(self):
        args=self.args('import time;time.sleep(1)')
        jobs=[subprocess.Popen(self.cli(args),stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True) for _ in range(2)]
        try:
            results=[]
            for job in jobs:
                out,err=job.communicate(timeout=10);self.assertEqual(job.returncode,0,err+out);results.append(json.loads(out))
            self.assertNotEqual(results[0]['output'],results[1]['output'])
            self.assertTrue(any(v['other_remaining']>0 for result in results for v in result['volumes']))
        finally:
            for job in jobs:
                if job.poll() is None:job.kill();job.wait(timeout=3)

    def test_output_collision_never_overwrites_existing_job(self):
        fixed=guard.uuid.UUID('aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa')
        with mock.patch.object(guard.uuid,'uuid4',return_value=fixed):
            first=guard.run(self.args('print("original result")'))
            original=(Path(first['output'])/'record.json').read_bytes()
            with self.assertRaises(FileExistsError):guard.run(self.args('print("must not start")'))
        self.assertEqual((Path(first['output'])/'record.json').read_bytes(),original)

    def test_age_limit_reports_retained_output_without_deleting(self):
        result=guard.run(self.args('pass'));state=guard.load_state(self.root)
        state['runs'][0]['created_at']=0;guard.atomic(self.root/'.guard-state.json',state)
        report=guard.run(self.args('pass',report_only=True))
        self.assertIn('retention age',report['admission_blocker']);self.assertTrue(Path(result['output']).exists())
        self.assertFalse(report['retained'][0]['removal_authorized'])


if __name__=='__main__':unittest.main(verbosity=2)
