//go:build linux

package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/ports"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

func stateObservationFixture(t *testing.T) (context.Context, *paths.Root, *paths.Root, *runtime.Lifecycle, string, string) {
	t.Helper()
	parent := t.TempDir()
	state, target := filepath.Join(parent, "state"), filepath.Join(parent, "target")
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "catalog.sqlite"), []byte("fixture database"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenRoot(state)
	if err != nil {
		t.Fatal(err)
	}
	protected, err := paths.OpenRoot(target)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	lease, err := runtime.AcquireLifecycle(ctx, root, "catalog", ports.SharedLease, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lease.Close(); _ = protected.Close(); _ = root.Close() })
	return ctx, root, protected, lease, state, target
}

func allowState(context.Context) error { return nil }

func TestStateObservationCanceledBeforeHold(t *testing.T) {
	parent, root, target, lease, _, _ := stateObservationFixture(t)
	ctx, cancel := context.WithCancel(parent)
	cancel()
	obs, err := ObserveStateFiles(ctx, root, []*paths.Root{target}, lease, "catalog", "catalog.sqlite", 1024, allowState)
	if obs != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was lost or observation created: %v", err)
	}
}

func TestStateObservationRejectsLeaseFromDifferentRoot(t *testing.T) {
	ctx, _, protected, lease, state, _ := stateObservationFixture(t)
	otherState := filepath.Join(filepath.Dir(state), "other-state")
	if err := os.Mkdir(otherState, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherState, "catalog.sqlite"), []byte("other fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	otherRoot, err := paths.OpenRoot(otherState)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = otherRoot.Close() })
	obs, err := ObserveStateFiles(ctx, otherRoot, []*paths.Root{protected}, lease, "catalog", "catalog.sqlite", 1024, allowState)
	if obs != nil {
		_ = obs.Close()
	}
	if !errors.Is(err, ErrStateUnsafe) || obs != nil {
		t.Fatalf("accepted a catalog lease from a different state root: %v", err)
	}
}

func TestStateObservationPinsFilesAndDetectsChanges(t *testing.T) {
	ctx, root, target, lease, state, _ := stateObservationFixture(t)
	if err := os.WriteFile(filepath.Join(state, "catalog.sqlite-wal"), []byte("fixture wal"), 0600); err != nil {
		t.Fatal(err)
	}
	obs, err := ObserveStateFiles(ctx, root, []*paths.Root{target}, lease, "catalog", "catalog.sqlite", 1024, allowState)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = obs.Close() })
	if err := lease.Close(); !errors.Is(err, runtime.ErrLifecycleInUse) {
		t.Fatalf("observation did not hold lifecycle: %v", err)
	}
	if err := obs.Recheck(ctx); err != nil {
		t.Fatal(err)
	}
	shm := filepath.Join(state, "catalog.sqlite-shm")
	if err := os.WriteFile(shm, []byte("appeared"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := obs.Recheck(ctx); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("new sidecar was not detected: %v", err)
	}
	if err := os.Remove(shm); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(state, "catalog.sqlite")
	if err := os.Rename(database, filepath.Join(state, "old.sqlite")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(database, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := obs.Recheck(ctx); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("database replacement was not detected: %v", err)
	}
}

func TestStateObservationRejectsDatabaseAndSidecarLinks(t *testing.T) {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		for _, hard := range []bool{false, true} {
			t.Run(suffix+map[bool]string{true: "hard", false: "sym"}[hard], func(t *testing.T) {
				ctx, root, target, lease, state, protected := stateObservationFixture(t)
				victim := filepath.Join(protected, "canary")
				if err := os.WriteFile(victim, []byte("unchanged fixture"), 0600); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(state, "catalog.sqlite"+suffix)
				if suffix == "" {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				}
				var err error
				if hard {
					err = os.Link(victim, path)
				} else {
					err = os.Symlink(victim, path)
				}
				if err != nil {
					t.Fatal(err)
				}
				if obs, err := ObserveStateFiles(ctx, root, []*paths.Root{target}, lease, "catalog", "catalog.sqlite", 1024, allowState); !errors.Is(err, ErrStateUnsafe) || obs != nil {
					t.Fatalf("unsafe database or sidecar accepted: %v", err)
				}
				raw, err := os.ReadFile(victim)
				if err != nil || string(raw) != "unchanged fixture" {
					t.Fatal("protected fixture was changed")
				}
			})
		}
	}
}

func TestStateObservationRejectsWrongRootsAndPermissions(t *testing.T) {
	t.Run("protected root", func(t *testing.T) {
		ctx, root, _, lease, _, _ := stateObservationFixture(t)
		if obs, err := ObserveStateFiles(ctx, root, []*paths.Root{root}, lease, "catalog", "catalog.sqlite", 1024, allowState); !errors.Is(err, ErrStateUnsafe) || obs != nil {
			t.Fatalf("protected state root accepted: %v", err)
		}
	})
	t.Run("permissions and size", func(t *testing.T) {
		ctx, root, target, lease, state, _ := stateObservationFixture(t)
		file := filepath.Join(state, "catalog.sqlite")
		if err := os.Chmod(file, 0644); err != nil {
			t.Fatal(err)
		}
		if obs, err := ObserveStateFiles(ctx, root, []*paths.Root{target}, lease, "catalog", "catalog.sqlite", 1024, allowState); !errors.Is(err, ErrStateUnsafe) || obs != nil {
			t.Fatalf("non-owner-only file accepted: %v", err)
		}
		if err := os.Chmod(file, 0600); err != nil {
			t.Fatal(err)
		}
		if obs, err := ObserveStateFiles(ctx, root, []*paths.Root{target}, lease, "catalog", "catalog.sqlite", 2, allowState); !errors.Is(err, ErrStateLimit) || obs != nil {
			t.Fatalf("oversized file accepted: %v", err)
		}
	})
	t.Run("owner cannot read", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root can bypass ordinary file read permissions")
		}
		ctx, root, target, lease, state, _ := stateObservationFixture(t)
		file := filepath.Join(state, "catalog.sqlite")
		if err := os.Chmod(file, 0000); err != nil {
			t.Fatal(err)
		}
		obs, err := ObserveStateFiles(ctx, root, []*paths.Root{target}, lease, "catalog", "catalog.sqlite", 1024, allowState)
		if obs != nil {
			_ = obs.Close()
		}
		if !errors.Is(err, ErrStateUnsafe) || obs != nil {
			t.Fatalf("unreadable state file was accepted: %v", err)
		}
	})
	t.Run("read permission revoked", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root can bypass ordinary file read permissions")
		}
		ctx, root, target, lease, state, _ := stateObservationFixture(t)
		obs, err := ObserveStateFiles(ctx, root, []*paths.Root{target}, lease, "catalog", "catalog.sqlite", 1024, allowState)
		if err != nil {
			t.Fatal(err)
		}
		defer obs.Close()
		if err := os.Chmod(filepath.Join(state, "catalog.sqlite"), 0000); err != nil {
			t.Fatal(err)
		}
		if err := obs.Recheck(ctx); err == nil {
			t.Fatal("revoked read permission was accepted")
		}
	})
	t.Run("root rename and revoke", func(t *testing.T) {
		ctx, root, target, lease, state, _ := stateObservationFixture(t)
		revoked := false
		guard := func(context.Context) error {
			if revoked {
				return errors.New("sensitive authorization detail")
			}
			return nil
		}
		obs, err := ObserveStateFiles(ctx, root, []*paths.Root{target}, lease, "catalog", "catalog.sqlite", 1024, guard)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = obs.Close() })
		revoked = true
		if err := obs.Recheck(ctx); !errors.Is(err, ErrStateUnsafe) {
			t.Fatalf("revoked authority accepted: %v", err)
		}
		revoked = false
		if err := os.Rename(state, state+"-moved"); err != nil {
			t.Fatal(err)
		}
		if err := obs.Recheck(ctx); !errors.Is(err, ErrStateUnsafe) {
			t.Fatalf("renamed root accepted: %v", err)
		}
	})
}
