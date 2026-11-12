//go:build linux

package paths

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestConfinedOpenRejectsEscapesAndNonRegular(t *testing.T) {
	base := t.TempDir()
	rootPath := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(rootPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootPath, "allowed.txt"), []byte("allowed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("outside marker"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(rootPath, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(rootPath, "allowed.txt"), filepath.Join(rootPath, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("allowed.txt", filepath.Join(rootPath, "relative-link")); err != nil {
		t.Fatal(err)
	}
	root, err := OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	f, err := root.OpenRegular("allowed.txt")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(f)
	f.Close()
	if err != nil || string(data) != "allowed" {
		t.Fatalf("read %q %v", data, err)
	}
	for _, path := range []string{"../outside/secret.txt", "/etc/passwd", "escape/secret.txt", "link", "relative-link", ".", "a/../allowed.txt", "bad\\path", "bad\x00path"} {
		f, err := root.OpenRegular(path)
		if err == nil {
			f.Close()
			t.Fatalf("unsafe path %q accepted", path)
		}
	}
	t.Run("fifo", func(t *testing.T) {
		fifo := filepath.Join(rootPath, "pipe")
		if err := unix.Mkfifo(fifo, 0600); err != nil {
			if errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.ENOSYS) {
				t.Skip("filesystem cannot create FIFO fixture")
			}
			t.Fatal(err)
		}
		if f, err := root.OpenRegular("pipe"); !errors.Is(err, ErrNotRegular) {
			if f != nil {
				f.Close()
			}
			t.Fatalf("fifo error %v", err)
		}
	})
	if f, err := root.OpenRegular("escape"); err == nil {
		f.Close()
		t.Fatal("symlink accepted")
	}
	if linked, err := OpenRoot(filepath.Join(rootPath, "escape")); err == nil {
		linked.Close()
		t.Fatal("symlink root accepted")
	}
}

func TestVerifyRegularFileDetectsNewHardLink(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repo, "file.go")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := OpenRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	f, err := root.OpenRegular("file.go")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := os.Link(path, filepath.Join(base, "outside-link")); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRegularFile(f); !errors.Is(err, ErrHardLinked) {
		t.Fatalf("new hard link was not detected: %v", err)
	}
}

func TestOpenDirectoryConfinesAndPinsChild(t *testing.T) {
	base := t.TempDir()
	rootPath := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	for _, dir := range []string{filepath.Join(rootPath, "a", "b"), outside} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(rootPath, "escape")); err != nil {
		t.Fatal(err)
	}
	root, err := OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, name := range []string{"../outside", "/outside", "escape", "escape/sub", "a/../a"} {
		if child, err := root.OpenDirectory(name); err == nil {
			child.Close()
			t.Fatalf("unsafe directory accepted: %q", name)
		}
	}
	child, err := root.OpenDirectory("a/b")
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	if path, err := child.CanonicalPath(); err != nil || path != filepath.Join(rootPath, "a", "b") {
		t.Fatalf("child path %q: %v", path, err)
	}
	if err := os.Rename(filepath.Join(rootPath, "a", "b"), filepath.Join(rootPath, "a", "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(rootPath, "a", "b")); err != nil {
		t.Fatal(err)
	}
	if err := child.Verify(); !errors.Is(err, ErrRootChanged) {
		t.Fatalf("replaced child accepted: %v", err)
	}
}

func TestOpenDirectoryRejectsConcurrentLinkExchange(t *testing.T) {
	base := t.TempDir()
	rootPath := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	entry := filepath.Join(rootPath, "entry")
	toggle := filepath.Join(rootPath, "toggle")
	for _, dir := range []string{entry, outside} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{filepath.Join(entry, "marker"): "inside", filepath.Join(outside, "marker"): "outside"} {
		if err := os.WriteFile(name, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, toggle); err != nil {
		t.Fatal(err)
	}
	exchange := func() error {
		return unix.Renameat2(unix.AT_FDCWD, entry, unix.AT_FDCWD, toggle, unix.RENAME_EXCHANGE)
	}
	if err := exchange(); err != nil {
		if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.EINVAL) {
			t.Skip("filesystem does not support atomic exchange fixture")
		}
		t.Fatal(err)
	}
	if err := exchange(); err != nil {
		t.Fatal(err)
	}
	root, err := OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	var swaps atomic.Int64
	start := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		<-start
		for i := 0; i < 1000; i++ {
			if err := exchange(); err != nil {
				done <- err
				return
			}
			swaps.Add(1)
		}
		done <- nil
	}()
	close(start)
	successful := 0
	for i := 0; i < 5000; i++ {
		child, err := root.OpenDirectory("entry")
		if err != nil {
			continue
		}
		f, err := child.OpenRegular("marker")
		if err == nil {
			data, readErr := io.ReadAll(f)
			f.Close()
			if readErr != nil || string(data) != "inside" {
				child.Close()
				<-done
				t.Fatalf("outside marker escaped: %q %v", data, readErr)
			}
			successful++
		}
		child.Close()
	}
	overlap := swaps.Load()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if successful == 0 || overlap == 0 {
		t.Fatalf("fixture lacked overlap: reads=%d swaps=%d", successful, overlap)
	}
}
func TestConfinedOpenRejectsConcurrentSymlinkSwap(t *testing.T) {
	base := t.TempDir()
	rootPath := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	for _, p := range []string{rootPath, outside, filepath.Join(rootPath, "dir")} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(rootPath, "dir", "file"), []byte("allowed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "file"), []byte("outside marker"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	dir := filepath.Join(rootPath, "dir")
	parked := filepath.Join(rootPath, "parked")
	var wg sync.WaitGroup
	var completed atomic.Int64
	start := make(chan struct{})
	wg.Add(1)
	done := make(chan error, 1)
	rename := func(from, to string) error {
		for attempt := 0; attempt < 2000; attempt++ {
			err := os.Rename(from, to)
			if err == nil {
				return nil
			}
			if !errors.Is(err, unix.EACCES) && !errors.Is(err, unix.EBUSY) && !errors.Is(err, unix.EPERM) {
				return err
			}
			time.Sleep(2 * time.Millisecond)
		}
		return errors.New("bounded fixture rename retry exhausted")
	}
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 500; i++ {
			if err := rename(dir, parked); err != nil {
				done <- err
				return
			}
			if err := os.Symlink(outside, dir); err != nil {
				_ = rename(parked, dir)
				done <- err
				return
			}
			if err := os.Remove(dir); err != nil {
				done <- err
				return
			}
			if err := rename(parked, dir); err != nil {
				done <- err
				return
			}
			completed.Add(1)
		}
		done <- nil
	}()
	close(start)
	successfulReads := 0
	for i := 0; i < 2000; i++ {
		f, err := root.OpenRegular("dir/file")
		if err != nil {
			continue
		}
		successfulReads++
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil || string(data) != "allowed" {
			wg.Wait()
			t.Fatalf("outside data escaped: %q %v", data, err)
		}
	}
	swapsDuringReads := completed.Load()
	wg.Wait()
	if err := <-done; err != nil {
		t.Fatalf("swap fixture: %v", err)
	}
	if successfulReads == 0 || swapsDuringReads == 0 {
		t.Fatalf("race fixture lacked overlap: reads=%d swaps=%d", successfulReads, swapsDuringReads)
	}
}

func TestRootReplacementRejectsConfinedRead(t *testing.T) {
	base := t.TempDir()
	original := filepath.Join(base, "root")
	moved := filepath.Join(base, "moved")
	replacement := filepath.Join(base, "replacement")
	for _, p := range []string{original, replacement} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "file"), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := OpenRoot(original)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = os.Rename(original, moved); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(replacement, original); err != nil {
		t.Fatal(err)
	}
	file, err := root.OpenRegular("file")
	if file != nil {
		file.Close()
	}
	if !errors.Is(err, ErrRootChanged) {
		t.Fatalf("changed root accepted: %v", err)
	}
}

func TestRootRecreatedAtSamePathIsRejected(t *testing.T) {
	base := t.TempDir()
	original := filepath.Join(base, "root")
	old := filepath.Join(base, "old")
	newRoot := filepath.Join(base, "new")
	for _, p := range []string{original, newRoot} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "file"), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := OpenRoot(original)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = os.Rename(original, old); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(newRoot, original); err != nil {
		t.Fatal(err)
	}
	f, err := root.OpenRegular("file")
	if f != nil {
		f.Close()
	}
	if !errors.Is(err, ErrRootChanged) {
		t.Fatalf("new inode at old path accepted: %v", err)
	}
}
