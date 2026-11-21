"""Small Linux-only guard for explicitly authorized, reviewed local commands.

Not an execution sandbox or OS quota. It never deletes retained output and
cannot coordinate unrelated writers. See README before supplying a command.
"""
import argparse
import contextlib
import ctypes
import fcntl
import json
import math
import os
from pathlib import Path
import signal
import stat
import subprocess
import sys
import threading
import time
import uuid

MAX = (1 << 63) - 1


def admission(free, peak, others, reserve, block, warning):
    values = (free, peak, others, reserve, block, warning)
    if any(type(x) is not int or x < 0 or x > MAX for x in values) or warning < block:
        raise ValueError('invalid or unknown budget measurement')
    if peak + others + reserve > MAX:
        raise ValueError('budget overflow')
    return free >= max(block, peak + others + reserve), free < warning


def directory(path, private=False):
    """Existing canonical path, no links or writable/untrusted ancestors."""
    path = Path(path)
    if not path.is_absolute() or str(path) != os.path.normpath(path):
        raise ValueError('absolute canonical directory required')
    for p in (path, *path.parents):
        info = p.lstat()
        if not stat.S_ISDIR(info.st_mode) or info.st_uid not in (0, os.geteuid()) or info.st_mode & 0o022:
            raise ValueError('untrusted directory ancestor')
    if private and (path.stat().st_uid != os.geteuid() or path.stat().st_mode & 0o077):
        raise ValueError('private owned output root required')
    return path


def identity(path):
    s = path.lstat()
    return s.st_dev, s.st_ino, s.st_uid, stat.S_IMODE(s.st_mode)


def meter_directory(path):
    path = Path(path)
    if not path.is_absolute() or str(path) != os.path.normpath(path):
        raise ValueError('canonical capacity-meter path required')
    if any(not stat.S_ISDIR(p.lstat().st_mode) for p in (path, *path.parents)):
        raise ValueError('capacity-meter link or non-directory')
    return path


def measure(path, limit=200000):
    """Charge logical or allocated bytes, once per inode; no link traversal."""
    anchor = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    device = os.fstat(anchor).st_dev
    seen = set(); count = 0; charged = 0
    def walk(fd, depth):
        nonlocal count, charged
        if depth > 64:
            raise ValueError('measurement depth exceeded')
        with os.scandir(fd) as listing:
            for entry in listing:
                name = entry.name
                try:
                    s = os.stat(name, dir_fd=fd, follow_symlinks=False)
                except FileNotFoundError:
                    continue
                count += 1
                if count > limit or s.st_dev != device:
                    raise ValueError('measurement limit or mount boundary')
                key = s.st_dev, s.st_ino
                if key in seen:
                    continue
                seen.add(key)
                charged += max(s.st_size, s.st_blocks * 512)
                if charged > MAX:
                    raise ValueError('measurement overflow')
                if stat.S_ISDIR(s.st_mode):
                    try:
                        child = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=fd)
                    except FileNotFoundError:
                        continue
                    try:
                        now = os.fstat(child)
                        if (now.st_dev, now.st_ino) != key:
                            raise ValueError('measurement directory changed')
                        walk(child, depth + 1)
                    finally:
                        os.close(child)
    try:
        walk(anchor, 0)
        return charged
    finally:
        os.close(anchor)


def proc(pid):
    try:
        s = Path('/proc', str(pid), 'stat').read_text()
        fields = s[s.rfind(')') + 2:].split()
        return fields[0], int(fields[1]), int(fields[19])
    except (OSError, ValueError, IndexError):
        return None


class Children:
    def __init__(self):
        self.known = {}

    def live(self, leader):
        snapshot = {int(p): proc(int(p)) for p in os.listdir('/proc') if p.isdigit()}
        roots = {os.getpid()}
        roots.update(pid for pid, (start, _) in self.known.items() if snapshot.get(pid) and snapshot[pid][2] == start)
        for _ in range(len(snapshot) + 1):
            more = {pid for pid, info in snapshot.items() if info and info[1] in roots and pid != os.getpid()}
            if more <= roots: break
            roots.update(more)
        for pid in roots - {os.getpid()}:
            info = snapshot.get(pid)
            if info is None or pid in self.known: continue
            try:
                fd = os.pidfd_open(pid)
                again = proc(pid)
                if again is None or again[2] != info[2]: os.close(fd); continue
                self.known[pid] = info[2], fd
            except ProcessLookupError:
                pass
        live = []
        for pid, (start, _) in self.known.items():
            info = proc(pid)
            if info and info[2] == start and info[0] != 'Z': live.append(pid)
            if info and info[0] == 'Z' and info[1] == os.getpid() and pid != leader:
                with contextlib.suppress(ChildProcessError): os.waitpid(pid, os.WNOHANG)
        return live

    def stop(self):
        for _, fd in self.known.values():
            with contextlib.suppress(ProcessLookupError): signal.pidfd_send_signal(fd, signal.SIGKILL)

    def close(self):
        for _, fd in self.known.values(): os.close(fd)


def atomic(path, value):
    parent = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    temporary = path.name + '.pending-' + str(uuid.uuid4())
    try:
        fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=parent)
        with os.fdopen(fd, 'w', encoding='utf-8') as stream:
            json.dump(value, stream, indent=2); stream.write('\n'); stream.flush(); os.fsync(stream.fileno())
        os.rename(temporary, path.name, src_dir_fd=parent, dst_dir_fd=parent)
        os.fsync(parent)
    finally:
        os.close(parent)


def load_state(root):
    path = root/'.guard-state.json'
    if not path.exists():
        if any(root.iterdir()): raise ValueError('output root has unknown provenance')
        return {'schema_version': 1, 'root': list(identity(root)), 'runs': []}
    s = path.lstat()
    if not stat.S_ISREG(s.st_mode) or s.st_nlink != 1 or s.st_uid != os.geteuid() or s.st_mode & 0o077 or s.st_size > 1048576:
        raise ValueError('invalid guard state')
    data = json.loads(path.read_text())
    if data['schema_version'] != 1 or data['root'] != list(identity(root)) or len(data['runs']) > 128:
        raise ValueError('guard state identity mismatch')
    names = set()
    for row in data['runs']:
        name = row['name']
        if not name.startswith('run-') or str(uuid.UUID(name[4:])) != name[4:] or name in names:
            raise ValueError('invalid run identity')
        names.add(name)
        owned = directory(root/name, True)
        if row['identity'] != list(identity(owned)) or row['status'] not in ('RUNNING','PASS','FAIL') or any(type(row[k]) is not int or not 0 < row[k] <= MAX for k in ('pid','start','peak')) or not isinstance(row['created_at'],(int,float)) or not math.isfinite(row['created_at']):
            raise ValueError('invalid or replaced retained run')
    known = {'.guard-state.json'} | {r['name'] for r in data['runs']}
    if any(p.name not in known for p in root.iterdir()): raise ValueError('unexpected root contents')
    return data


def lock(fd):
    deadline = time.monotonic()+5
    while True:
        try:
            fcntl.flock(fd,fcntl.LOCK_EX | fcntl.LOCK_NB);return
        except BlockingIOError:
            if time.monotonic() >= deadline:raise ValueError('scoped coordinator lock busy')
            time.sleep(.01)


def plan(args, state):
    if len(state['runs']) >= args.max_runs or measure(args.root) > args.retained_bytes:
        raise ValueError('retention limit reached; retained data requires inspection')
    other = 0
    for row in state['runs']:
        age = time.time()-row['created_at']
        if age < 0 or age > args.max_age_seconds:
            raise ValueError('retention age or clock uncertain; inspect retained data, no automatic removal')
        if row['status'] == 'RUNNING':
            current = proc(row['pid'])
            if current is None or current[2] != row['start']:
                raise ValueError('uncertain prior run; report-only inspection required')
            used = measure(args.root/row['name'])
            other += max(0, row['peak'] - used)
    # All generated paths belong to root's device. Cwd/module cache are explicit
    # read inputs: measure their distinct volume too, but assign no output peak.
    volumes = {}
    for path, demand in ((args.root, args.peak_bytes), (args.cwd, 0), *([(args.module_cache, 0)] if args.module_cache else [])):
        device = path.stat().st_dev
        item = volumes.setdefault(device, {'path': str(path), 'peak': 0})
        item['peak'] += demand
    output_device = args.root.stat().st_dev
    # Explicitly verified layered backing storage (e.g. WSL VHDX on Windows C).
    # Checking both capacity domains does not charge aliases of one device twice.
    backing_device = None
    if args.backing_root is not None:
        backing_device = args.backing_root.stat().st_dev
        if backing_device != output_device:
            item = volumes.setdefault(backing_device, {'path': str(args.backing_root), 'peak': 0})
            item['peak'] += args.peak_bytes
    for device, item in volumes.items():
        fs = os.statvfs(item['path'])
        item['free'] = fs.f_bavail * fs.f_frsize
        item['other_remaining'] = other if device in (output_device, backing_device) else 0
        allowed, warning = admission(item['free'], item['peak'], item['other_remaining'], args.reserve_bytes, args.block_bytes, args.warning_bytes)
        item.update(allowed=allowed, warning=warning)
        if not allowed: raise ValueError('insufficient measured capacity')
    return list(volumes.values())


def run(args):
    os.umask(0o077)
    if not sys.platform.startswith('linux') or not hasattr(os, 'pidfd_open') or not hasattr(signal, 'pidfd_send_signal'):
        raise ValueError('Linux pidfd/subreaper support required')
    args.root = directory(args.root, True); args.cwd = directory(args.cwd)
    if args.root == args.cwd or args.root in args.cwd.parents or args.cwd in args.root.parents:
        raise ValueError('output/source roots overlap')
    if args.module_cache: args.module_cache = directory(args.module_cache)
    if args.backing_root: args.backing_root = meter_directory(args.backing_root)
    if 'microsoft' in Path('/proc/sys/kernel/osrelease').read_text().lower() and args.backing_root is None:
        raise ValueError('WSL requires an independently verified VHDX backing-volume meter')
    if not args.command or not Path(args.command[0]).is_absolute(): raise ValueError('absolute reviewed executable required')
    executable = Path(args.command[0]).resolve(strict=True)
    if not executable.is_file() or not os.access(executable, os.X_OK): raise ValueError('executable unavailable')
    admission(0, args.peak_bytes, 0, args.reserve_bytes, args.block_bytes, args.warning_bytes)
    if args.peak_bytes < 1 or args.log_bytes < 1 or args.log_bytes > MAX or not math.isfinite(args.seconds) or not 0 < args.seconds <= 43200 or not 1 <= args.max_runs <= 128 or not args.peak_bytes <= args.retained_bytes <= MAX or not math.isfinite(args.max_age_seconds) or not 0 < args.max_age_seconds <= MAX:
        raise ValueError('invalid finite run/retention limits')
    extra = {}
    for value in args.env:
        key, sep, val = value.partition('=')
        if not sep or key in ('HOME','TMPDIR','GOCACHE','GOPATH','GOMODCACHE') or not key or not key.isascii() or not key.replace('_','').isalnum():
            raise ValueError('invalid/private environment override')
        extra[key] = val
    if args.report_only:
        state = load_state(args.root)
        try:
            volumes = plan(args, state); blocked = None
        except (ValueError,OSError) as error:
            volumes = []; blocked = str(error)
        retained = [{'path': str(args.root/r['name']), 'identity': r['identity'], 'status': r['status'], 'observed_bytes': measure(args.root/r['name']), 'removal_authorized': False} for r in state['runs']]
        return {'status': 'REPORT_ONLY', 'volumes': volumes, 'admission_blocker': blocked, 'retained': retained, 'workload_started': False, 'mutations': 0, 'coordination': 'this private root only; unrelated writers unknown'}
    # First initialization is serialized on the existing private directory inode;
    # no lock file is created before provenance is checked.
    anchor = os.open(args.root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    initialized = False
    try:
        lock(anchor)
        state = load_state(args.root); volumes = plan(args, state)
        name = 'run-' + str(uuid.uuid4()); output = args.root/name; output.mkdir(mode=0o700)
        for leaf in ('home','tmp','cache','gopath','modcache'): (output/leaf).mkdir(mode=0o700)
        current = proc(os.getpid())
        row = {'name': name, 'identity': list(identity(output)), 'pid': os.getpid(), 'start': current[2], 'peak': args.peak_bytes, 'created_at': time.time(), 'status': 'RUNNING'}
        state['runs'].append(row); atomic(args.root/'.guard-state.json', state)
        initialized = True
    finally:
        fcntl.flock(anchor, fcntl.LOCK_UN)
        if not initialized:os.close(anchor)
    before = identity(args.root)
    record = {'status': 'RUNNING', 'started_at_utc': time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()), 'command': args.command, 'cwd': str(args.cwd), 'output': str(output), 'volumes': volumes, 'termination_reason': None, 'command_exit_status': None, 'finalization_status': 'PENDING', 'cleanup_status': 'NOT_REQUESTED_RETAINED', 'coordination': 'private-root cooperating writers only; unrelated writers unknown; sampled growth, not OS quota'}
    record['limits'] = {'peak_bytes':args.peak_bytes,'reserve_bytes':args.reserve_bytes,'block_bytes':args.block_bytes,'warning_bytes':args.warning_bytes,'seconds':args.seconds,'log_bytes':args.log_bytes,'retained_bytes':args.retained_bytes,'max_runs':args.max_runs,'max_age_seconds':args.max_age_seconds,'observed_process_limit':128,'measurement_entries':200000}
    atomic(output/'record.json', record)
    env = {'PATH': os.defpath, 'HOME': str(output/'home'), 'TMPDIR': str(output/'tmp'), 'GOCACHE': str(output/'cache'), 'GOPATH': str(output/'gopath'), 'GOMODCACHE': str(args.module_cache or output/'modcache'), 'GOTOOLCHAIN': 'local', 'GOPROXY': 'off', 'GOSUMDB': 'off', 'GOENV': 'off', 'GOWORK': 'off', 'GOTELEMETRY': 'off', 'GOVCS': '*:off', 'GOPACKAGESDRIVER': 'off', 'CGO_ENABLED': '0', 'GOMAXPROCS': '4'}
    env.update(extra)
    run_started = time.monotonic();cancelled = threading.Event()
    old = {s: signal.signal(s, lambda *_: cancelled.set()) for s in (signal.SIGINT, signal.SIGTERM)}
    children = Children(); reason = None; child = None; code = None; peak = 0; max_processes = 0
    capped = threading.Event(); write_failed = threading.Event(); reader = None; live = []
    try:
        if ctypes.CDLL(None, use_errno=True).prctl(36, 1, 0, 0, 0) != 0: raise ValueError('subreaper unavailable')
        peak = measure(output)
        if peak > args.peak_bytes: reason = 'metadata_budget'; raise ValueError('run metadata exceeds budget')
        with (output/'command.log').open('wb', buffering=0) as log:
            child = subprocess.Popen(args.command, cwd=args.cwd, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, start_new_session=True)
            info = proc(child.pid)
            if info is None: raise ValueError('child identity unavailable')
            children.known[child.pid] = info[2], os.pidfd_open(child.pid)
            def pump():
                used = 0
                try:
                    while True:
                        data = os.read(child.stdout.fileno(), 65536)
                        if not data: break
                        take = min(len(data), max(0, args.log_bytes-used))
                        if take:
                            if log.write(data[:take]) != take: write_failed.set()
                            used += take
                        if take < len(data): capped.set()
                except OSError: write_failed.set()
            reader = threading.Thread(target=pump, daemon=True); reader.start(); start = time.monotonic(); last_growth = 0
            while child.poll() is None:
                live = children.live(child.pid); max_processes = max(max_processes,len(live))
                if cancelled.is_set(): reason = 'cancelled'
                elif capped.is_set(): reason = 'log_cap'
                elif write_failed.is_set(): reason = 'log_write_error'
                elif time.monotonic()-start >= args.seconds: reason = 'deadline'
                elif len(live) > 128: reason = 'owned_process_cap'
                elif identity(args.root) != before: reason = 'output_root_changed'
                else:
                    if time.monotonic()-last_growth >= 2:
                        peak = max(peak, measure(output)); last_growth = time.monotonic()
                    if peak > args.peak_bytes: reason = 'output_growth'
                    for volume in volumes:
                        fs = os.statvfs(volume['path'])
                        if fs.f_bavail * fs.f_frsize < max(args.reserve_bytes, args.block_bytes): reason = 'disk_reserve'
                if reason: break
                time.sleep(.1)
            if reason: children.stop()
            try: code = child.wait(timeout=5)
            except subprocess.TimeoutExpired:
                children.live(child.pid); children.stop(); code = child.wait(timeout=5); reason = reason or 'leader_stop_timeout'
            end = time.monotonic()+3
            while children.live(child.pid):
                if time.monotonic() >= end or reason: reason = reason or 'descendants_remained'; children.stop()
                if time.monotonic() >= end+5: raise ValueError('owned descendants did not stop')
                time.sleep(.05)
            reader.join(timeout=3)
            if reader.is_alive(): reason = reason or 'output_handle_open'
            if capped.is_set(): reason = reason or 'log_cap'
            if write_failed.is_set(): reason = reason or 'log_write_error'
            child.stdout.close()
        # A fast writer may finish between samples. Final growth/capacity checks
        # are required after every owned writer has stopped, before any PASS.
        retained = measure(output);peak = max(peak, retained)
        if peak > args.peak_bytes: reason = reason or 'output_growth'
        for volume in volumes:
            fs = os.statvfs(volume['path'])
            if fs.f_bavail * fs.f_frsize < max(args.reserve_bytes,args.block_bytes): reason = reason or 'disk_reserve'
        record['capacity_after'] = []
        for volume in volumes:
            fs = os.statvfs(volume['path']);free = fs.f_bavail*fs.f_frsize
            record['capacity_after'].append({'path':volume['path'],'free_bytes':free,'warning':free<args.warning_bytes})
        record['retained_observed_bytes'] = retained;record['removed_bytes'] = 0
        record.update(status='PASS' if code == 0 and reason is None else 'FAIL', termination_reason=reason, command_exit_status=code, finalization_status='PASS' if not reader.is_alive() else 'FAIL', peak_observed_bytes=peak, max_owned_processes_observed=max_processes, process_poll_seconds=.1,growth_poll_seconds=2)
    except BaseException as error:
        reason = reason or type(error).__name__
        if child is not None:
            end = time.monotonic()+5
            while True:
                live = children.live(child.pid); children.stop()
                with contextlib.suppress(subprocess.TimeoutExpired): code = child.wait(timeout=.05)
                if not live and child.poll() is not None: break
                if time.monotonic() >= end: break
            if reader is not None: reader.join(timeout=2)
            if child.stdout is not None:child.stdout.close()
        stopped = child is None or not children.live(child.pid) and child.poll() is not None
        drained = reader is None or not reader.is_alive()
        record.update(status='FAIL', termination_reason=reason, command_exit_status=code, finalization_status='PASS' if stopped and drained else 'FAIL')
    finally:
        children.close()
        for sig, handler in old.items(): signal.signal(sig, handler)
        record['seconds'] = round(time.monotonic()-run_started,3)
        try:
            if identity(args.root) != before or identity(output) != tuple(row['identity']):
                raise ValueError('refusing finalization after root replacement')
            atomic(output/'record.json',record)
            lock(anchor)
            state = load_state(args.root)
            saved = next(r for r in state['runs'] if r['name'] == name)
            saved['status'] = record['status'] if record['finalization_status'] == 'PASS' else 'RUNNING'
            atomic(args.root/'.guard-state.json',state)
        except (OSError,ValueError,KeyError,TypeError) as error:
            record.update(status='FAIL',finalization_status='FAIL',finalization_error=type(error).__name__)
        finally:
            fcntl.flock(anchor, fcntl.LOCK_UN); os.close(anchor)
    return record


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root',required=True); parser.add_argument('--cwd',required=True)
    for name in ('peak-bytes','reserve-bytes','block-bytes','warning-bytes'):
        parser.add_argument('--'+name,type=int,required=True)
    parser.add_argument('--seconds',type=float,required=True)
    parser.add_argument('--log-bytes',type=int,default=16*1024**2)
    parser.add_argument('--retained-bytes',type=int,default=8*1024**3)
    parser.add_argument('--max-runs',type=int,default=32)
    parser.add_argument('--max-age-seconds',type=float,default=30*86400)
    parser.add_argument('--module-cache');parser.add_argument('--env',action='append',default=[])
    parser.add_argument('--backing-root',help='explicit independently verified layered backing-volume meter')
    parser.add_argument('--report-only',action='store_true')
    parser.add_argument('command',nargs=argparse.REMAINDER)
    args=parser.parse_args()
    if args.command[:1]==['--']:args.command=args.command[1:]
    try:
        result=run(args);print(json.dumps(result,indent=2))
        if result['status'] in ('PASS','REPORT_ONLY'):return 0
        original=result.get('command_exit_status')
        if type(original) is int and original!=0:return original if 0<original<256 else min(255,128-original) if original<0 else 1
        return 1
    except (OSError,ValueError,KeyError,TypeError) as error:
        print(json.dumps({'status':'BLOCKED','reason':str(error),'workload_started':'inspect record if admission already occurred'}));return 2


if __name__=='__main__':sys.exit(main())
