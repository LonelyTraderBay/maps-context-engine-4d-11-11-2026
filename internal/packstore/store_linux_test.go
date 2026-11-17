//go:build linux

package packstore

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

const fixtureWorktree = "11111111-1111-4111-8111-111111111111"
const resultOne = "22222222-2222-4222-8222-222222222222"
const resultTwo = "33333333-3333-4333-8333-333333333333"
const resultThree = "44444444-4444-4444-8444-444444444444"

type fixtureReservation struct{ revoked bool }

func (r *fixtureReservation) Check(context.Context) error {
	if r.revoked {
		return ErrUnsafe
	}
	return nil
}
func (*fixtureReservation) Close() error { return nil }

func fixtureStoreAt(t *testing.T, statePath, targetPath string, maxCount int, authorize func(context.Context) error, reserve ReserveBudget) (*Store, context.Context) {
	t.Helper()
	root, err := paths.OpenRoot(statePath)
	if err != nil {
		t.Fatal(err)
	}
	target, err := paths.OpenRoot(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	lease, err := runtime.AcquireLifecycle(ctx, root, fixtureWorktree, ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lease.Close(); _ = target.Close(); _ = root.Close() })
	if authorize == nil {
		authorize = func(context.Context) error { return nil }
	}
	if reserve == nil {
		reserve = func(context.Context, int64) (BudgetReservation, error) { return &fixtureReservation{}, nil }
	}
	store, err := New(root, []*paths.Root{target}, lease, fixtureWorktree, 1024*1024, 1024*1024, maxCount, authorize, reserve)
	if err != nil {
		t.Fatal(err)
	}
	return store, ctx
}

func fixtureStore(t *testing.T, maxCount int, authorize func(context.Context) error, reserve ReserveBudget) (*Store, context.Context, string, string) {
	t.Helper()
	parent := t.TempDir()
	state, target := filepath.Join(parent, "state"), filepath.Join(parent, "target")
	for _, path := range []string{state, target, filepath.Join(state, "scratch"), filepath.Join(state, "scratch", "pack")} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	store, ctx := fixtureStoreAt(t, state, target, maxCount, authorize, reserve)
	return store, ctx, state, target
}

func exact(expected []byte) VerifyBytes {
	return func(_ context.Context, raw []byte) error {
		if !bytes.Equal(raw, expected) {
			return errors.New("private fixture detail")
		}
		return nil
	}
}

func TestPackStoreRejectsLeaseFromDifferentRoot(t *testing.T) {
	store, ctx, state, _ := fixtureStore(t, 2, nil, nil)
	other := filepath.Join(filepath.Dir(state), "other-state")
	for _, path := range []string{other, filepath.Join(other, "scratch"), filepath.Join(other, "scratch", "pack")} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	root, err := paths.OpenRoot(other)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	misbound, err := New(root, store.protected, store.lease, fixtureWorktree, store.maxResult, store.quota, store.maxCount, store.authorize, store.reserve)
	if err != nil {
		t.Fatal(err)
	}
	if err := misbound.Create(ctx, resultOne, []byte("fixture"), exact([]byte("fixture"))); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("accepted lifecycle from another root: %v", err)
	}
	if entries, err := os.ReadDir(filepath.Join(other, "scratch", "pack")); err != nil || len(entries) != 0 {
		t.Fatal("wrong-root lifecycle created spool files")
	}
}

func TestPackStoreGCRejectsWriterFromDifferentRoot(t *testing.T) {
	store, ctx, state, _ := fixtureStore(t, 2, nil, nil)
	if err := store.Create(ctx, resultOne, []byte("expired"), exact([]byte("expired"))); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(filepath.Dir(state), "other-state")
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(other)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := runtime.AcquireLifecycle(ctx, root, fixtureWorktree, ports.SharedLease, time.Second)
	if err != nil {
		_ = root.Close()
		t.Fatal(err)
	}
	writer, err := runtime.AcquireWriter(ctx, lease, resultThree, time.Second)
	if err != nil {
		_ = lease.Close()
		_ = root.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close(); _ = lease.Close(); _ = root.Close() })
	count, err := store.GCExpired(ctx, writer, resultThree, func(context.Context, string, []byte) (bool, error) { return true, nil })
	if count != 0 || !errors.Is(err, ErrUnsafe) {
		t.Fatalf("GC accepted writer from another root: count=%d err=%v", count, err)
	}
	if _, err := os.Stat(filepath.Join(state, "scratch", "pack", resultOne+".pack")); err != nil {
		t.Fatal("wrong-root writer removed retained result")
	}
}

type writerWithoutRootProof struct{ ports.IndexWriterLease }

type writerWithRevokedCheck struct{ *runtime.Writer }

func (writerWithRevokedCheck) CheckWriter(context.Context, string, string) error { return ErrUnsafe }

type writerRevocableCheck struct {
	*runtime.Writer
	revoked bool
}

func (w *writerRevocableCheck) CheckWriter(ctx context.Context, worktree, job string) error {
	if w.revoked {
		return ErrUnsafe
	}
	return w.Writer.CheckWriter(ctx, worktree, job)
}

func TestPackStoreGCRejectsWriterWithoutRootProof(t *testing.T) {
	store, ctx, state, _ := fixtureStore(t, 2, nil, nil)
	if err := store.Create(ctx, resultOne, []byte("expired"), exact([]byte("expired"))); err != nil {
		t.Fatal(err)
	}
	writer, err := runtime.AcquireWriter(ctx, store.lease, resultThree, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	count, err := store.GCExpired(ctx, writerWithoutRootProof{writer}, resultThree, func(context.Context, string, []byte) (bool, error) { return true, nil })
	if count != 0 || !errors.Is(err, ErrUnsafe) {
		t.Fatalf("GC accepted writer without root proof: count=%d err=%v", count, err)
	}
	if _, err := os.Stat(filepath.Join(state, "scratch", "pack", resultOne+".pack")); err != nil {
		t.Fatal("unbound writer removed retained result")
	}
}

func TestPackStoreGCCancellationWithMissingRootProof(t *testing.T) {
	store, parent, state, _ := fixtureStore(t, 2, nil, nil)
	if err := store.Create(parent, resultOne, []byte("expired"), exact([]byte("expired"))); err != nil {
		t.Fatal(err)
	}
	writer, err := runtime.AcquireWriter(parent, store.lease, resultThree, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	ctx, cancel := context.WithCancel(parent)
	cancel()
	count, err := store.GCExpired(ctx, writerWithoutRootProof{writer}, resultThree, func(context.Context, string, []byte) (bool, error) { return true, nil })
	if count != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("GC hid cancellation when root proof was unavailable: count=%d err=%v", count, err)
	}
	if _, err := os.Stat(filepath.Join(state, "scratch", "pack", resultOne+".pack")); err != nil {
		t.Fatal("cancelled GC removed retained result")
	}
}

func TestPackStoreGCRejectsRevokedWriterWithValidRootProof(t *testing.T) {
	store, ctx, state, _ := fixtureStore(t, 2, nil, nil)
	if err := store.Create(ctx, resultOne, []byte("expired"), exact([]byte("expired"))); err != nil {
		t.Fatal(err)
	}
	writer, err := runtime.AcquireWriter(ctx, store.lease, resultThree, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	count, err := store.GCExpired(ctx, writerWithRevokedCheck{writer}, resultThree, func(context.Context, string, []byte) (bool, error) { return true, nil })
	if count != 0 || !errors.Is(err, ErrUnsafe) {
		t.Fatalf("GC ignored revoked writer: count=%d err=%v", count, err)
	}
	if _, err := os.Stat(filepath.Join(state, "scratch", "pack", resultOne+".pack")); err != nil {
		t.Fatal("revoked writer removed retained result")
	}
}

func TestPackStoreGCRechecksWriterBeforeRemoval(t *testing.T) {
	store, ctx, state, _ := fixtureStore(t, 2, nil, nil)
	if err := store.Create(ctx, resultOne, []byte("expired"), exact([]byte("expired"))); err != nil {
		t.Fatal(err)
	}
	writer, err := runtime.AcquireWriter(ctx, store.lease, resultThree, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	authority := &writerRevocableCheck{Writer: writer}
	count, err := store.GCExpired(ctx, authority, resultThree, func(context.Context, string, []byte) (bool, error) {
		authority.revoked = true
		return true, nil
	})
	if count != 0 || !errors.Is(err, ErrUnsafe) {
		t.Fatalf("GC failed to recheck revoked writer: count=%d err=%v", count, err)
	}
	if _, err := os.Stat(filepath.Join(state, "scratch", "pack", resultOne+".pack")); err != nil {
		t.Fatal("revoked writer removed retained result")
	}
}

func TestPackStoreCancellationPreservesOutcome(t *testing.T) {
	t.Run("already canceled", func(t *testing.T) {
		store, parent, state, _ := fixtureStore(t, 2, nil, nil)
		ctx, cancel := context.WithCancel(parent)
		cancel()
		calls := 0
		err := store.Create(ctx, resultOne, []byte("fixture"), func(context.Context, []byte) error {
			calls++
			return nil
		})
		if !errors.Is(err, context.Canceled) || calls != 0 {
			t.Fatalf("canceled request ran verifier: calls=%d err=%v", calls, err)
		}
		if _, err := os.Stat(filepath.Join(state, "scratch", "pack", resultOne+".pending")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("canceled request started a write: %v", err)
		}
	})
	for _, cancelAt := range []int{2, 3} {
		t.Run(string(rune('0'+cancelAt)), func(t *testing.T) {
			store, parent, state, _ := fixtureStore(t, 2, nil, nil)
			ctx, cancel := context.WithCancel(parent)
			defer cancel()
			calls := 0
			verify := func(context.Context, []byte) error {
				calls++
				if calls == cancelAt {
					cancel()
					return ctx.Err()
				}
				return nil
			}
			err := store.Create(ctx, resultOne, []byte("fixture"), verify)
			if !errors.Is(err, context.Canceled) || (cancelAt == 3) != errors.Is(err, ErrUncertain) {
				t.Fatalf("cancellation lost or outcome wrong: calls=%d err=%v", calls, err)
			}
			_, statErr := os.Stat(filepath.Join(state, "scratch", "pack", resultOne+".pack"))
			if cancelAt == 2 && !errors.Is(statErr, os.ErrNotExist) || cancelAt == 3 && statErr != nil {
				t.Fatalf("unexpected published state: %v", statErr)
			}
		})
	}
	t.Run("budget reservation", func(t *testing.T) {
		var cancel context.CancelFunc
		store, parent, state, _ := fixtureStore(t, 2, nil, func(ctx context.Context, _ int64) (BudgetReservation, error) {
			cancel()
			return nil, ctx.Err()
		})
		ctx, stop := context.WithCancel(parent)
		cancel = stop
		defer cancel()
		err := store.Create(ctx, resultOne, []byte("fixture"), exact([]byte("fixture")))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("reservation cancellation lost: %v", err)
		}
		if _, err := os.Stat(filepath.Join(state, "scratch", "pack", resultOne+".pending")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("reservation cancellation started a write: %v", err)
		}
	})
}

func TestPackStoreGCCancellationPreservesOutcome(t *testing.T) {
	for _, cancelAt := range []int{2, 3} {
		t.Run(string(rune('0'+cancelAt)), func(t *testing.T) {
			store, parent, state, _ := fixtureStore(t, 2, nil, nil)
			for _, id := range []string{resultOne, resultTwo} {
				if err := store.Create(parent, id, []byte("expired"), exact([]byte("expired"))); err != nil {
					t.Fatal(err)
				}
			}
			writer, err := runtime.AcquireWriter(parent, store.lease, resultThree, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = writer.Close() })
			ctx, cancel := context.WithCancel(parent)
			defer cancel()
			checks := 0
			proof := func(context.Context, string, []byte) (bool, error) {
				checks++
				if checks == cancelAt {
					cancel()
					return false, ctx.Err()
				}
				return true, nil
			}
			removed, err := store.GCExpired(ctx, writer, resultThree, proof)
			wantRemoved := 0
			if cancelAt == 3 {
				wantRemoved = 1
			}
			if removed != wantRemoved || !errors.Is(err, context.Canceled) || (removed > 0) != errors.Is(err, ErrUncertain) {
				t.Fatalf("cancellation lost or outcome wrong: count=%d err=%v", removed, err)
			}
			_, err = os.Stat(filepath.Join(state, "scratch", "pack", resultTwo+".pack"))
			if err != nil {
				t.Fatalf("unprocessed file removed: %v", err)
			}
		})
	}
}

func TestPackStoreCrossProcessAndTamper(t *testing.T) {
	store, ctx, state, target := fixtureStore(t, 2, nil, nil)
	data := []byte("fixture-only authenticated bytes")
	if err := store.Create(ctx, resultOne, data, exact(data)); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(state, "scratch", "pack", resultOne+".pack")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("result permissions: %v", err)
	}
	got, err := store.Read(ctx, resultOne, exact(data))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("read: %v", err)
	}
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPackStoreChild$")
	cmd.Env = append(os.Environ(), "MCE4D_PACKSTORE_STATE="+state, "MCE4D_PACKSTORE_TARGET="+target)
	out, err := cmd.CombinedOutput()
	if err != nil || !bytes.Equal(out, data) {
		t.Fatalf("other process could not read fixture: %v %q", err, out)
	}
	if err := os.WriteFile(path, []byte("fixture-only authenticated bytEs"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Read(ctx, resultOne, exact(data)); !errors.Is(err, ErrUnsafe) || got != nil {
		t.Fatalf("tamper returned: %q %v", got, err)
	}
}

func TestPackStoreChild(t *testing.T) {
	state, target := os.Getenv("MCE4D_PACKSTORE_STATE"), os.Getenv("MCE4D_PACKSTORE_TARGET")
	if state == "" || target == "" {
		return
	}
	store, ctx := fixtureStoreAt(t, state, target, 2, nil, nil)
	data := []byte("fixture-only authenticated bytes")
	raw, err := store.Read(ctx, resultOne, exact(data))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stdout.Write(raw); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

func TestPackStoreQuotaCollisionAndContainment(t *testing.T) {
	store, ctx, state, _ := fixtureStore(t, 2, nil, nil)
	data := []byte("small fixture")
	if err := store.Create(ctx, resultOne, data, exact(data)); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(ctx, resultOne, data, exact(data)); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("collision accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(state, "scratch", "pack", resultOne+".pending")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("collision left unnecessary pending file: %v", err)
	}
	if err := store.Create(ctx, resultTwo, data, exact(data)); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(ctx, resultThree, data, exact(data)); !errors.Is(err, ErrCapacity) {
		t.Fatalf("count quota ignored: %v", err)
	}
	store.protected = []*paths.Root{store.root}
	if raw, err := store.Read(ctx, resultOne, exact(data)); !errors.Is(err, ErrUnsafe) || raw != nil {
		t.Fatalf("protected root was used as state: %v", err)
	}
}

func TestPackStoreRejectsLinksAndLateRevocation(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		store, ctx, state, target := fixtureStore(t, 2, nil, nil)
		canary := filepath.Join(target, "canary")
		if err := os.WriteFile(canary, []byte("unchanged"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(canary, filepath.Join(state, "scratch", "pack", resultOne+".pending")); err != nil {
			t.Fatal(err)
		}
		if err := store.Create(ctx, resultOne, []byte("new"), exact([]byte("new"))); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("symlink accepted: %v", err)
		}
		after, err := os.ReadFile(canary)
		if err != nil || string(after) != "unchanged" {
			t.Fatal("protected canary changed")
		}
	})
	t.Run("hardlink", func(t *testing.T) {
		store, ctx, state, target := fixtureStore(t, 2, nil, nil)
		data := []byte("fixture")
		if err := store.Create(ctx, resultOne, data, exact(data)); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(filepath.Join(state, "scratch", "pack", resultOne+".pack"), filepath.Join(target, "linked")); err != nil {
			t.Fatal(err)
		}
		if raw, err := store.Read(ctx, resultOne, exact(data)); !errors.Is(err, ErrUnsafe) || raw != nil {
			t.Fatalf("hardlinked data returned: %v", err)
		}
	})
	t.Run("parent permissions", func(t *testing.T) {
		store, ctx, state, _ := fixtureStore(t, 2, nil, nil)
		data := []byte("fixture")
		if err := store.Create(ctx, resultOne, data, exact(data)); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(state, "scratch"), 0755); err != nil {
			t.Fatal(err)
		}
		if raw, err := store.Read(ctx, resultOne, exact(data)); !errors.Is(err, ErrUnsafe) || raw != nil {
			t.Fatalf("non-owner-only parent accepted: %v", err)
		}
	})
	t.Run("total budget denied", func(t *testing.T) {
		store, ctx, state, _ := fixtureStore(t, 2, nil, func(context.Context, int64) (BudgetReservation, error) {
			return nil, errors.New("budget unavailable")
		})
		if err := store.Create(ctx, resultOne, []byte("fixture"), exact([]byte("fixture"))); !errors.Is(err, ErrCapacity) {
			t.Fatalf("missing worktree budget accepted: %v", err)
		}
		if _, err := os.Stat(filepath.Join(state, "scratch", "pack", resultOne+".pending")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("write started before budget reservation: %v", err)
		}
	})
	for _, failAt := range []int{2, 3} {
		t.Run(string(rune('0'+failAt)), func(t *testing.T) {
			store, ctx, state, _ := fixtureStore(t, 2, nil, nil)
			calls := 0
			verify := func(context.Context, []byte) error {
				calls++
				if calls == failAt {
					return errors.New("revoked")
				}
				return nil
			}
			err := store.Create(ctx, resultOne, []byte("fixture"), verify)
			if !errors.Is(err, ErrUnsafe) || (failAt == 3) != errors.Is(err, ErrUncertain) {
				t.Fatalf("unexpected write outcome, calls=%d err=%v", calls, err)
			}
			_, statErr := os.Stat(filepath.Join(state, "scratch", "pack", resultOne+".pack"))
			if failAt == 2 && !errors.Is(statErr, os.ErrNotExist) || failAt == 3 && statErr != nil {
				t.Fatalf("unexpected published state: %v", statErr)
			}
		})
	}
}

func TestPackStoreGCOnlyProvenExpiredWithWriterLease(t *testing.T) {
	store, ctx, state, _ := fixtureStore(t, 4, nil, nil)
	old, live := []byte("authenticated expired fixture"), []byte("authenticated live fixture")
	if err := store.Create(ctx, resultOne, old, exact(old)); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(ctx, resultTwo, live, exact(live)); err != nil {
		t.Fatal(err)
	}
	pending := filepath.Join(state, "scratch", "pack", resultThree+".pending")
	if err := os.WriteFile(pending, []byte("incomplete"), 0600); err != nil {
		t.Fatal(err)
	}
	writer, err := runtime.AcquireWriter(ctx, store.lease, resultThree, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	proof := func(_ context.Context, id string, raw []byte) (bool, error) {
		switch id {
		case resultOne:
			return bytes.Equal(raw, old), nil
		case resultTwo:
			return false, nil
		default:
			return false, errors.New("unexpected file")
		}
	}
	if count, err := store.GCExpired(ctx, writer, resultTwo, proof); !errors.Is(err, ErrUnsafe) || count != 0 {
		t.Fatalf("wrong writer job accepted: count=%d err=%v", count, err)
	}
	count, err := store.GCExpired(ctx, writer, resultThree, proof)
	if err != nil || count != 1 {
		t.Fatalf("GC result count=%d err=%v", count, err)
	}
	if _, err := os.Stat(filepath.Join(state, "scratch", "pack", resultOne+".pack")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired file retained: %v", err)
	}
	if got, err := store.Read(ctx, resultTwo, exact(live)); err != nil || !bytes.Equal(got, live) {
		t.Fatalf("live file lost: %v", err)
	}
	if raw, err := os.ReadFile(pending); err != nil || string(raw) != "incomplete" {
		t.Fatalf("unproven pending file modified: %v", err)
	}
}

func TestPackStoreGCRechecksProofAndReportsPartialOutcome(t *testing.T) {
	t.Run("late revocation", func(t *testing.T) {
		store, ctx, state, _ := fixtureStore(t, 2, nil, nil)
		if err := store.Create(ctx, resultOne, []byte("expired"), exact([]byte("expired"))); err != nil {
			t.Fatal(err)
		}
		writer, err := runtime.AcquireWriter(ctx, store.lease, resultThree, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = writer.Close() })
		checks := 0
		proof := func(context.Context, string, []byte) (bool, error) {
			checks++
			return checks == 1, nil
		}
		if count, err := store.GCExpired(ctx, writer, resultThree, proof); !errors.Is(err, ErrUnsafe) || count != 0 {
			t.Fatalf("late revocation ignored: count=%d err=%v", count, err)
		}
		if _, err := os.Stat(filepath.Join(state, "scratch", "pack", resultOne+".pack")); err != nil {
			t.Fatalf("unproven file removed: %v", err)
		}
	})
	t.Run("partial outcome", func(t *testing.T) {
		store, ctx, _, _ := fixtureStore(t, 2, nil, nil)
		data := []byte("expired")
		if err := store.Create(ctx, resultOne, data, exact(data)); err != nil {
			t.Fatal(err)
		}
		if err := store.Create(ctx, resultTwo, data, exact(data)); err != nil {
			t.Fatal(err)
		}
		writer, err := runtime.AcquireWriter(ctx, store.lease, resultThree, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = writer.Close() })
		proof := func(_ context.Context, id string, _ []byte) (bool, error) {
			if id == resultTwo {
				return false, errors.New("proof unavailable")
			}
			return true, nil
		}
		if count, err := store.GCExpired(ctx, writer, resultThree, proof); count != 1 || !errors.Is(err, ErrUncertain) {
			t.Fatalf("partial deletion hidden: count=%d err=%v", count, err)
		}
	})
}

func TestPackStoreGCCanReclaimAfterQuotaReduction(t *testing.T) {
	store, ctx, _, _ := fixtureStore(t, 2, nil, nil)
	data := bytes.Repeat([]byte("x"), 2048)
	if err := store.Create(ctx, resultOne, data, exact(data)); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(ctx, resultTwo, data, exact(data)); err != nil {
		t.Fatal(err)
	}
	// Simulate reopening under a tighter valid runtime configuration.
	store.maxResult, store.quota, store.maxCount = 1024, 1024, 1
	writer, err := runtime.AcquireWriter(ctx, store.lease, resultThree, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	proof := func(_ context.Context, _ string, raw []byte) (bool, error) { return bytes.Equal(raw, data), nil }
	if count, err := store.GCExpired(ctx, writer, resultThree, proof); err != nil || count != 2 {
		t.Fatalf("tighter quota prevented reclaim: count=%d err=%v", count, err)
	}
}

func TestPackStoreQuotaAcrossCompetingProcesses(t *testing.T) {
	_, ctx, state, target := fixtureStore(t, 1, nil, nil)
	ids := []string{resultOne, resultTwo}
	var commands []*exec.Cmd
	var outputs [2]bytes.Buffer
	var releases []*os.File
	for i, id := range ids {
		readEnd, writeEnd, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPackStoreQuotaChild$")
		cmd.Env = append(os.Environ(), "MCE4D_QUOTA_STATE="+state, "MCE4D_QUOTA_TARGET="+target, "MCE4D_QUOTA_ID="+id)
		cmd.ExtraFiles = []*os.File{readEnd}
		cmd.Stdout, cmd.Stderr = &outputs[i], &outputs[i]
		if err := cmd.Start(); err != nil {
			_ = readEnd.Close()
			_ = writeEnd.Close()
			t.Fatal(err)
		}
		_ = readEnd.Close()
		commands, releases = append(commands, cmd), append(releases, writeEnd)
	}
	for _, release := range releases {
		if _, err := release.Write([]byte{1}); err != nil {
			t.Fatal(err)
		}
		_ = release.Close()
	}
	for _, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("quota child failed: %v", err)
		}
	}
	results := []string{outputs[0].String(), outputs[1].String()}
	sort.Strings(results)
	if results[0] != "CAPACITY" || results[1] != "OK" {
		t.Fatalf("shared quota was not enforced: %q", results)
	}
}

func TestPackStoreQuotaChild(t *testing.T) {
	state, target, id := os.Getenv("MCE4D_QUOTA_STATE"), os.Getenv("MCE4D_QUOTA_TARGET"), os.Getenv("MCE4D_QUOTA_ID")
	if state == "" || target == "" || id == "" {
		return
	}
	store, ctx := fixtureStoreAt(t, state, target, 1, nil, nil)
	barrier := os.NewFile(3, "quota-fixture-barrier")
	var signal [1]byte
	if _, err := barrier.Read(signal[:]); err != nil {
		t.Fatal(err)
	}
	_ = barrier.Close()
	data := []byte("bounded fixture")
	err := store.Create(ctx, id, data, exact(data))
	result := "OK"
	if errors.Is(err, ErrCapacity) {
		result = "CAPACITY"
	} else if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stdout.WriteString(result); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

func TestPackStoreCrashBeforePublishLeavesNoResult(t *testing.T) {
	store, ctx, state, target := fixtureStore(t, 1, nil, nil)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPackStoreCrashChild$")
	cmd.Env = append(os.Environ(), "MCE4D_CRASH_STATE="+state, "MCE4D_CRASH_TARGET="+target)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ready, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || ready != "PENDING\n" {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("child did not reach pending state: %v %q %q", err, ready, stderr.String())
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("killed writer unexpectedly succeeded")
	}
	pending := filepath.Join(state, "scratch", "pack", resultOne+".pending")
	if _, err := os.Stat(pending); err != nil {
		t.Fatalf("interrupted pending file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(state, "scratch", "pack", resultOne+".pack")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("interrupted result was published: %v", err)
	}
	data := []byte("new result")
	if err := store.Create(ctx, resultTwo, data, exact(data)); !errors.Is(err, ErrCapacity) {
		t.Fatalf("pending bytes escaped quota: %v", err)
	}
	writer, err := runtime.AcquireWriter(ctx, store.lease, resultThree, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	if count, err := store.GCExpired(ctx, writer, resultThree, func(context.Context, string, []byte) (bool, error) { return true, nil }); err != nil || count != 0 {
		t.Fatalf("unproven pending file was collected: count=%d err=%v", count, err)
	}
	if _, err := os.Stat(pending); err != nil {
		t.Fatalf("pending provenance was ignored: %v", err)
	}
}

func TestPackStoreCrashChild(t *testing.T) {
	state, target := os.Getenv("MCE4D_CRASH_STATE"), os.Getenv("MCE4D_CRASH_TARGET")
	if state == "" || target == "" {
		return
	}
	store, ctx := fixtureStoreAt(t, state, target, 1, nil, nil)
	checks := 0
	verify := func(ctx context.Context, _ []byte) error {
		checks++
		if checks == 2 {
			if _, err := os.Stdout.WriteString("PENDING\n"); err != nil {
				return err
			}
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}
	if err := store.Create(ctx, resultOne, []byte("interrupted fixture"), verify); err != nil {
		t.Fatal(err)
	}
	t.Fatal("writer should have been stopped")
}
