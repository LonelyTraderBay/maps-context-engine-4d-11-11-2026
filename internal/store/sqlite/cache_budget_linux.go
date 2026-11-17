//go:build linux

package sqlite

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	"golang.org/x/sys/unix"
)

var ErrCacheCapacity = errors.New("worktree cache capacity unavailable")

const defaultWorktreeCacheBytes int64 = 2 << 30

// cacheBudget serializes derived-cache growth, not readers or the long job
// writer lease. It is cooperative engine authority, not a filesystem quota.
type cacheBudget struct {
	owner *ProtectedWorktreeOwner
	gate  *stateDomain
	limit int64
}

type cacheUsage struct{ other, main, wal, shm int64 }

// No ordinary descriptor is opened for a database inode: even closing an
// innocent read descriptor could release this process's native POSIX locks.
func observeCacheUsage(ctx context.Context, root *paths.Root, limit int64) (result cacheUsage, resultErr error) {
	anchor, err := root.DirectoryHandle()
	if err != nil {
		return result, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, anchor.Close())
		if resultErr != nil {
			result = cacheUsage{}
		}
	}()
	nodes := 0
	var walk func(int, string, int) error
	walk = func(parent int, prefix string, depth int) (resultErr error) {
		if depth > 16 {
			return ErrStateLimit
		}
		fd, err := unix.Openat(parent, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return err
		}
		dir := os.NewFile(uintptr(fd), "cache-directory")
		defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
		for {
			entries, readErr := dir.ReadDir(128)
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				return readErr
			}
			for _, entry := range entries {
				if err := ctx.Err(); err != nil {
					return err
				}
				nodes++
				name := entry.Name()
				path := prefix + name
				if nodes > 8192 || len(path) > 4096 || strings.ContainsAny(name, "/\\\x00") {
					return ErrStateLimit
				}
				child, err := unix.Openat(fd, name, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
				if errors.Is(err, unix.ENOENT) && (path == "index.sqlite-wal" || path == "index.sqlite-shm") {
					continue // A native close can remove these; their floor stays charged.
				}
				if err != nil {
					return err
				}
				var st unix.Stat_t
				err = unix.Fstat(child, &st)
				if err == nil && (st.Uid != uint32(unix.Geteuid()) || st.Mode&0077 != 0 || st.Size < 0) {
					err = ErrStateUnsafe
				}
				if err == nil {
					switch st.Mode & unix.S_IFMT {
					case unix.S_IFDIR:
						if path != "backups" && path != ".maintenance/records" {
							err = walk(child, path+"/", depth+1)
						}
					case unix.S_IFREG:
						if st.Nlink != 1 {
							err = ErrStateUnsafe
							break
						}
						switch path {
						case "records.sqlite", "records.sqlite-wal", "records.sqlite-shm", "records.sqlite-journal":
							// Durable records are not derived cache and are never pruned here.
						case "index.sqlite":
							result.main = st.Size
						case "index.sqlite-wal":
							result.wal = st.Size
						case "index.sqlite-shm":
							result.shm = st.Size
						default:
							if st.Size > limit-result.other {
								err = ErrCacheCapacity
							} else {
								result.other += st.Size
							}
						}
					default:
						err = ErrStateUnsafe
					}
				}
				err = errors.Join(err, unix.Close(child))
				if err != nil {
					return err
				}
			}
			if errors.Is(readErr, io.EOF) {
				return nil
			}
		}
	}
	if err := walk(int(anchor.Fd()), "", 0); err != nil {
		return cacheUsage{}, err
	}
	return result, root.Verify()
}

// Charge both the current WAL and a future checkpointed main. A commit with
// spilling disabled writes at most one frame per dirty database page, plus
// conservative sector padding. The pinned native VFS and this bound are tested
// together; this is not a bound for arbitrary SQL/VFS/attached databases.
func cacheCharge(usage cacheUsage, image walLogicalImage, commitPages, extra, limit int64) (int64, error) {
	if !validAuthorityImage(image) || extra < 0 || limit < 1 || commitPages < 0 {
		return 0, ErrStateUnsafe
	}
	pageSize := int64(image.PageSize)
	pages := max(int64(image.PageCount), commitPages)
	if pages > walLogicalImageMaxBytes/pageSize {
		return 0, ErrCacheCapacity
	}
	main := max(usage.main, pages*pageSize)
	wal := max(usage.wal, int64(32))
	if commitPages != 0 {
		wal += (pages+128)*(pageSize+24) + 32
	}
	if main > walLogicalImageMaxBytes-wal {
		return 0, ErrCacheCapacity
	}
	// WAL-index blocks hold 4062 frames first, then 4096. This deliberately
	// rounds up more generously and retains two blocks even after close.
	shm := max(usage.shm, (2+wal/(pageSize+24)/4062)*32768)
	total := usage.other
	for _, size := range []int64{main, wal, shm, extra} {
		if size < 0 || size > limit-total {
			return 0, ErrCacheCapacity
		}
		total += size
	}
	return total, nil
}

func (q *cacheBudget) check(ctx context.Context, image walLogicalImage, commitPages, extra int64) error {
	if q == nil || q.owner == nil || q.gate == nil || q.limit < 1 {
		return ErrStateUnsafe
	}
	if err := q.owner.check(ctx); err != nil {
		return err
	}
	usage, err := observeCacheUsage(ctx, q.owner.domain.root, q.limit)
	if err != nil {
		return err
	}
	_, err = cacheCharge(usage, image, commitPages, extra, q.limit)
	return err
}

func (q *cacheBudget) acquire(ctx context.Context) (*runtime.Lifecycle, error) {
	if err := q.owner.check(ctx); err != nil {
		return nil, err
	}
	if err := q.gate.Check(ctx); err != nil {
		return nil, err
	}
	return runtime.AcquireLifecycle(ctx, q.gate.root, q.owner.manifest.WorktreeID, ports.ExclusiveLease, 5*time.Second)
}

// CacheReservation keeps the cross-process growth gate and the DB lifecycle
// alive through publication. Closing releases only the reservation; published
// files remain charged by the next full usage observation.
type CacheReservation struct {
	mu     sync.Mutex
	quota  *cacheBudget
	gate   *runtime.Lifecycle
	hold   *runtime.LifecycleHold
	image  walLogicalImage
	closed bool
}

func (r *CacheReservation) Check(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrOwnerClosed
	}
	if err := r.gate.Check(ctx, r.quota.owner.manifest.WorktreeID, ports.ExclusiveLease); err != nil {
		return err
	}
	return r.quota.check(ctx, r.image, 0, 0)
}

func (r *CacheReservation) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrOwnerClosed
	}
	if err := r.gate.Close(); err != nil {
		return err
	}
	r.closed = true
	return r.hold.Close()
}

func (o *ProtectedWorktreeOwner) ReserveCache(ctx context.Context, bytes int64) (_ *CacheReservation, resultErr error) {
	if o == nil || o.quota == nil || bytes < 1 || bytes > o.quota.limit {
		return nil, ErrCacheCapacity
	}
	gate, err := o.quota.acquire(ctx)
	if err != nil {
		return nil, err
	}
	r := &CacheReservation{quota: o.quota, gate: gate}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, gate.Close())
			if r.hold != nil {
				resultErr = errors.Join(resultErr, r.hold.Close())
			}
		}
	}()
	r.hold, err = o.lease.Hold(ctx)
	if err != nil {
		return nil, err
	}
	a, err := o.stores[0].admit(ctx, false)
	if err != nil {
		return nil, err
	}
	r.image = a.receipt.Image
	err = errors.Join(o.quota.check(ctx, r.image, 0, bytes), a.close())
	if err != nil {
		return nil, err
	}
	return r, nil
}

// Borrowed resources remain owned by o and require o.Check on every operation.
func (o *ProtectedWorktreeOwner) CacheResources(ctx context.Context) (*paths.Root, *runtime.Lifecycle, []*paths.Root, error) {
	if err := o.check(ctx); err != nil {
		return nil, nil, nil, err
	}
	return o.domain.root, o.lease, append([]*paths.Root(nil), o.profile.targets...), nil
}
