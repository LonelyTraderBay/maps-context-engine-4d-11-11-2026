//go:build linux

package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"golang.org/x/sys/unix"
)

const logLock = "telemetry.lock"

type logHeader struct {
	Format     string `json:"format"`
	WorktreeID string `json:"worktree_id"`
	Files      int    `json:"files"`
	Sequence   uint64 `json:"sequence"`
}

func headerBytes(worktree string, files int, sequence uint64) []byte {
	raw, _ := json.Marshal(logHeader{"mce-log-v1", worktree, files, sequence})
	return append(raw, '\n')
}

func slotName(i int) string { return fmt.Sprintf("events-%03d.jsonl", i) }

func owned(file *os.File, directory bool) error {
	var st unix.Stat_t
	if file == nil || unix.Fstat(int(file.Fd()), &st) != nil {
		return ErrUnavailable
	}
	want := uint32(unix.S_IFREG)
	if directory {
		want = unix.S_IFDIR
	}
	if st.Mode&unix.S_IFMT != want || st.Uid != uint32(unix.Geteuid()) || st.Mode&0077 != 0 || !directory && st.Nlink != 1 {
		return ErrUnavailable
	}
	return nil
}

func ownedRoot(root *paths.Root) error {
	if root == nil || root.Verify() != nil {
		return ErrUnavailable
	}
	dir, err := root.DirectoryHandle()
	if err != nil {
		return ErrUnavailable
	}
	err = errors.Join(owned(dir, true), dir.Close(), root.Verify())
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

// writableExisting first pins and validates a regular file before opening it
// for writing through a verified procfs descriptor. Neither the path nor a
// device/symlink at that path is opened for writing before validation.
func writableExisting(dir *paths.Root, name string) (*os.File, error) {
	read, err := dir.OpenRegular(name)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer read.Close()
	if owned(read, false) != nil {
		return nil, ErrUnavailable
	}
	proc, err := unix.Open("/proc/self/fd", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrUnsupported
	}
	defer unix.Close(proc)
	var fs unix.Statfs_t
	if unix.Fstatfs(proc, &fs) != nil || uint64(fs.Type) != uint64(unix.PROC_SUPER_MAGIC) {
		return nil, ErrUnsupported
	}
	fd, err := unix.Openat(proc, strconv.Itoa(int(read.Fd())), unix.O_RDWR|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, ErrUnavailable
	}
	write := os.NewFile(uintptr(fd), "telemetry-owned-file")
	a, ea := read.Stat()
	b, eb := write.Stat()
	if ea != nil || eb != nil || owned(write, false) != nil || !os.SameFile(a, b) || sameCurrent(dir, name, write) != nil {
		write.Close()
		return nil, ErrUnavailable
	}
	return write, nil
}

func sameCurrent(dir *paths.Root, name string, file *os.File) error {
	current, err := dir.OpenRegular(name)
	if err != nil {
		return ErrUnavailable
	}
	a, ea := current.Stat()
	b, eb := file.Stat()
	if ea != nil || eb != nil || owned(current, false) != nil || owned(file, false) != nil || !os.SameFile(a, b) {
		current.Close()
		return ErrUnavailable
	}
	if current.Close() != nil {
		return ErrUnavailable
	}
	return nil
}

func openOrCreate(dir *paths.Root, anchor *os.File, name string) (*os.File, bool, error) {
	fd, err := unix.Openat(int(anchor.Fd()), name, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err == nil {
		file := os.NewFile(uintptr(fd), "telemetry-new-file")
		if owned(file, false) != nil || sameCurrent(dir, name, file) != nil {
			file.Close()
			return nil, false, ErrUnavailable
		}
		return file, true, nil
	}
	if !errors.Is(err, unix.EEXIST) {
		return nil, false, ErrUnavailable
	}
	file, err := writableExisting(dir, name)
	return file, false, err
}

func (s *FileSink) check(ctx context.Context, dir *paths.Root, lock *os.File) error {
	if ctx.Err() != nil || s.authorize(ctx) != nil || s.lease.CheckRoot(ctx, s.root, s.worktree, ports.SharedLease) != nil || ownedRoot(s.root) != nil || ownedRoot(dir) != nil || sameCurrent(dir, logLock, lock) != nil {
		return ErrUnavailable
	}
	return nil
}

// appendEvent uses a nonblocking per-worktree lock; failures only affect the
// collector's degradation counters. It never removes unknown or unrelated data.
func (s *FileSink) appendEvent(ctx context.Context, raw []byte) error {
	if s == nil || ctx == nil || len(raw) < 2 || len(raw) > 8192 || raw[len(raw)-1] != '\n' || ctx.Err() != nil {
		return ErrUnavailable
	}
	if s.authorize(ctx) != nil || s.lease.CheckRoot(ctx, s.root, s.worktree, ports.SharedLease) != nil {
		return ErrUnavailable
	}
	hold, err := s.lease.Hold(ctx)
	if err != nil {
		return ErrUnavailable
	}
	defer hold.Close()
	if ownedRoot(s.root) != nil {
		return ErrUnavailable
	}
	rootfd, err := s.root.DirectoryHandle()
	if err != nil {
		return ErrUnavailable
	}
	defer rootfd.Close()
	if err = unix.Mkdirat(int(rootfd.Fd()), "logs", 0700); err != nil && !errors.Is(err, unix.EEXIST) {
		return ErrUnavailable
	}
	dir, err := s.root.OpenDirectory("logs")
	if err != nil {
		return ErrUnavailable
	}
	defer dir.Close()
	if ownedRoot(dir) != nil || ownedRoot(s.root) != nil {
		return ErrUnavailable
	}
	anchor, err := dir.DirectoryHandle()
	if err != nil {
		return ErrUnavailable
	}
	defer anchor.Close()
	lock, _, err := openOrCreate(dir, anchor, logLock)
	if err != nil {
		return err
	}
	defer lock.Close()
	if unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB) != nil {
		return ErrUnavailable
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	if s.check(ctx, dir, lock) != nil {
		return ErrUnavailable
	}

	var latest uint64
	latestSlot := -1
	for i := 0; i < 128; i++ {
		var st unix.Stat_t
		err := unix.Fstatat(int(anchor.Fd()), slotName(i), &st, unix.AT_SYMLINK_NOFOLLOW)
		if errors.Is(err, unix.ENOENT) {
			continue
		}
		if err != nil || i >= s.files {
			return ErrUnavailable
		}
		file, err := writableExisting(dir, slotName(i))
		if err != nil {
			return err
		}
		seq, err := s.verifySlot(file, i)
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			return ErrUnavailable
		}
		if seq > latest {
			latest, latestSlot = seq, i
		}
	}
	if latest == ^uint64(0) {
		return ErrUnavailable
	}
	if latestSlot < 0 {
		return s.writeSlot(ctx, dir, anchor, lock, 0, 1, raw, false)
	}
	current, err := writableExisting(dir, slotName(latestSlot))
	if err != nil {
		return err
	}
	info, statErr := current.Stat()
	closeErr := current.Close()
	if statErr != nil || closeErr != nil {
		return ErrUnavailable
	}
	if info.Size()+int64(len(raw)) <= s.bytes {
		return s.writeSlot(ctx, dir, anchor, lock, latestSlot, latest, raw, true)
	}
	next := latest + 1
	return s.writeSlot(ctx, dir, anchor, lock, int((next-1)%uint64(s.files)), next, raw, false)
}

func (s *FileSink) verifySlot(file *os.File, slot int) (uint64, error) {
	if owned(file, false) != nil {
		return 0, ErrUnavailable
	}
	info, err := file.Stat()
	if err != nil || info.Size() < 1 || info.Size() > s.bytes {
		return 0, ErrUnavailable
	}
	buf := make([]byte, 512)
	n, err := file.ReadAt(buf, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, ErrUnavailable
	}
	end := bytes.IndexByte(buf[:n], '\n')
	if end < 0 {
		return 0, ErrUnavailable
	}
	var h logHeader
	if json.Unmarshal(buf[:end], &h) != nil || h.Format != "mce-log-v1" || h.WorktreeID != s.worktree || h.Files != s.files || h.Sequence == 0 || int((h.Sequence-1)%uint64(s.files)) != slot || !bytes.Equal(buf[:end+1], headerBytes(s.worktree, s.files, h.Sequence)) {
		return 0, ErrUnavailable
	}
	var last [1]byte
	if _, err := file.ReadAt(last[:], info.Size()-1); err != nil || last[0] != '\n' {
		return 0, ErrUnavailable
	}
	return h.Sequence, nil
}

func (s *FileSink) writeSlot(ctx context.Context, dir *paths.Root, anchor, lock *os.File, slot int, sequence uint64, raw []byte, appendCurrent bool) error {
	if s.check(ctx, dir, lock) != nil {
		return ErrUnavailable
	}
	name := slotName(slot)
	if !appendCurrent && int64(len(headerBytes(s.worktree, s.files, sequence))+len(raw)) > s.bytes {
		return ErrUnavailable
	}
	var file *os.File
	var created bool
	var err error
	if appendCurrent {
		file, err = writableExisting(dir, name)
	} else {
		file, created, err = openOrCreate(dir, anchor, name)
	}
	if err != nil {
		return err
	}
	defer file.Close()
	var offset int64
	if !created {
		old, err := s.verifySlot(file, slot)
		if err != nil || (appendCurrent && old != sequence) || (!appendCurrent && old >= sequence) {
			return ErrUnavailable
		}
	}
	if appendCurrent {
		info, err := file.Stat()
		if err != nil {
			return ErrUnavailable
		}
		offset = info.Size()
	} else {
		header := headerBytes(s.worktree, s.files, sequence)
		if int64(len(header)+len(raw)) > s.bytes || s.check(ctx, dir, lock) != nil || sameCurrent(dir, name, file) != nil {
			return ErrUnavailable
		}
		if !created && file.Truncate(0) != nil {
			return ErrUnavailable
		}
		raw = append(header, raw...)
	}
	if offset+int64(len(raw)) > s.bytes || s.check(ctx, dir, lock) != nil || sameCurrent(dir, name, file) != nil {
		return ErrUnavailable
	}
	n, err := file.WriteAt(raw, offset)
	if err != nil || n != len(raw) {
		return ErrUnavailable
	}
	if s.check(ctx, dir, lock) != nil || sameCurrent(dir, name, file) != nil {
		return ErrUnavailable
	}
	return nil
}
