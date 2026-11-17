//go:build linux

package lsp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/packstore"
)

type resultReservation struct{ checks, failAt, closes int }

func (r *resultReservation) Check(context.Context) error {
	r.checks++
	if r.failAt == r.checks {
		return errors.New("fixture reservation revoked")
	}
	return nil
}
func (r *resultReservation) Close() error { r.closes++; return nil }

func TestBudgetedResultLifetimeCountAndOwnerRecheck(t *testing.T) {
	dir := resultFixtureDir(t)
	legacy, _ := makeResultStore(t, dir, 0)
	if legacy.WithinRetentionLimits(fixtureWorktree, resultHardQuota, 32, 900) {
		t.Fatal("unbudgeted component enabled public retention")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	authorized := true
	reservation := &resultReservation{}
	store, err := NewBudgetedResultStore(legacy.root, legacy.lease, fixtureWorktree, legacy.scanner, 1<<20, 2<<20, 5, 1,
		func(context.Context) error {
			if !authorized {
				return ErrLSPAuthority
			}
			return nil
		},
		func(_ context.Context, n int64) (packstore.BudgetReservation, error) {
			if n < 1 {
				t.Fatal("empty growth reservation")
			}
			return reservation, nil
		})
	if err != nil || !store.WithinRetentionLimits(fixtureWorktree, 2<<20, 1, 5) || store.WithinRetentionLimits(fixtureWorktree, 2<<20, 1, 4) {
		t.Fatalf("limits: %v", err)
	}
	clock := time.Now().Truncate(time.Second)
	store.now = func() time.Time { return clock }
	scope, payload, principal := resultFixture(t, 2)
	header, err := store.Create(ctx, scope, principal, payload, allowResult)
	if err != nil || header.ExpiresAt-header.IssuedAt != 5 || reservation.closes != 1 || reservation.checks < 3 {
		t.Fatalf("create/lifetime/reservation: %+v %+v %v", header, reservation, err)
	}
	if _, err := store.Create(ctx, scope, principal, payload, allowResult); !errors.Is(err, ErrResultQuota) {
		t.Fatalf("count cap: %v", err)
	}
	authorized = false
	if _, err := store.Read(ctx, header.ResultID, principal, scope, allowResult); !errors.Is(err, ErrResultStoreUnsafe) {
		t.Fatalf("owner revoked: %v", err)
	}
	authorized = true
	clock = clock.Add(5 * time.Second)
	if _, err := store.Read(ctx, header.ResultID, principal, scope, allowResult); !errors.Is(err, ErrResultExpired) {
		t.Fatalf("configured expiry: %v", err)
	}
}

func TestBudgetedResultRefusesGrowthAndLateReservationLoss(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(map[bool]string{false: "capacity", true: "late_revocation"}[late], func(t *testing.T) {
			dir := resultFixtureDir(t)
			legacy, _ := makeResultStore(t, dir, 0)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			reservation := &resultReservation{failAt: 2}
			store, err := NewBudgetedResultStore(legacy.root, legacy.lease, fixtureWorktree, legacy.scanner, 1<<20, 2<<20, 900, 2, func(context.Context) error { return nil }, func(context.Context, int64) (packstore.BudgetReservation, error) {
				if !late {
					return nil, ErrResultQuota
				}
				return reservation, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			scope, payload, principal := resultFixture(t, 2)
			header, err := store.Create(ctx, scope, principal, payload, allowResult)
			if err == nil || header.ResultID != "" {
				t.Fatal("returned usable result after lost reservation")
			}
			if late && reservation.closes != 1 {
				t.Fatal("reservation leaked")
			}
			entries, err := os.ReadDir(filepath.Join(dir, "scratch", "rename"))
			if err != nil {
				t.Fatal(err)
			}
			pending := 0
			for _, entry := range entries {
				if filepath.Ext(entry.Name()) == ".json" {
					t.Fatal("published result after lost reservation")
				}
				if filepath.Ext(entry.Name()) == ".pending" {
					pending++
				}
			}
			if late && pending != 1 || !late && pending != 0 {
				t.Fatal("pending growth accounting")
			}
		})
	}
}
