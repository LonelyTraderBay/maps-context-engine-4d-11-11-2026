"""Watch a private synthetic map inbox while the protected writer is busy."""

import fcntl
import json
import os
import selectors
import signal
import subprocess


def publish(folder, root, artifact, *, commit):
    source, build = artifact
    raw = (root / source.lstrip("/")).read_bytes()
    final = folder / (build + ".json")
    temporary = final.with_suffix(".json.tmp")
    with temporary.open("xb") as file:
        os.fchown(file.fileno(), 1000, 1000)
        os.fchmod(file.fileno(), 0o600)
        file.write(raw)
        file.flush()
        os.fsync(file.fileno())
    if commit:
        temporary.rename(final)
        fd = os.open(folder, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)


def exercise(root, setup, worktree, profile, variants):
    inbox = root / "result/map-inbox"
    inbox.mkdir(mode=0o700)
    os.chown(inbox, 1000, 1000)
    (inbox / ("f" * 64 + ".json")).symlink_to("/target/entry.tsx")
    publish(inbox, root, variants[1], commit=False)
    # Use a distinct temporary filename so the committed variant is published by
    # one atomic rename below.
    (inbox / (variants[1][1] + ".json.tmp")).rename(inbox / ("e" * 64 + ".json.tmp"))
    env = {"PATH": "/toolchain/bin", "HOME": "/profiles/runtime/home",
           "MODERNC_SQLITE_OFD_LOCK": "0", "GOMAXPROCS": "4", "GOTELEMETRY": "off"}
    args = ["/mce4d", "explorer-map", "watch", "--worktree-id", worktree, "--profile-id", profile, "--directory"]
    denied = subprocess.run(args + ["/target"], preexec_fn=setup(1000), env=env, capture_output=True, timeout=10)
    assert denied.returncode == 3 and b"ACCESS_DENIED" in denied.stderr
    lock = root / "profiles/runtime/home/.local/state/mce4d/worktrees" / worktree / "writer.lock"
    with lock.open("rb+") as writer:
        fcntl.flock(writer, fcntl.LOCK_EX | fcntl.LOCK_NB)
        watch = subprocess.Popen(args + ["/result/map-inbox"], preexec_fn=setup(1000), env=env,
                                 stdout=subprocess.PIPE, stderr=subprocess.PIPE, bufsize=0)
        try:
            with selectors.DefaultSelector() as select:
                select.register(watch.stdout, selectors.EVENT_READ)
                assert not select.select(0.4), "Watch reported a temporary or linked map"
                for item in variants:
                    publish(inbox, root, item, commit=True)
                assert select.select(10), "No busy map notification"
                pending = json.loads(watch.stdout.readline())
                assert pending["status"] == "busy_rebind" and pending["attempt"] == 1, pending
                assert pending["build_map_id"] == variants[-1][1]
                assert pending["writer_wait_ms"] >= 100
                fcntl.flock(writer, fcntl.LOCK_UN)
                assert select.select(10), "Watcher did not retry the map"
                bound = json.loads(watch.stdout.readline())
                assert bound["status"] == "bound" and bound["attempt"] == 2, bound
                assert bound["build_map_id"] == variants[-1][1] and len(bound["binding_id"]) == 64
                assert not select.select(0.3), "Watcher replayed the binding"
        finally:
            watch.send_signal(signal.SIGINT)
            tail, stderr = watch.communicate(timeout=8)
            assert watch.returncode == 0, stderr.decode()[:1000]
            assert not tail
    print(json.dumps({"case": "T091_WATCH", "status": "PASS", "pending": pending, "bound": bound,
                      "superseded_skipped": True, "temporary_ignored": True, "directory_rejected": True}), flush=True)
