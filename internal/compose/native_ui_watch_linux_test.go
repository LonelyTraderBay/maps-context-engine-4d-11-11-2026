//go:build linux

package compose

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	sqliteowner "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
)

func TestUIMapWatchCoalescesBusyRetry(t *testing.T) {
	a := sqliteowner.WatchedUIBuild{ID: strings.Repeat("a", 64), Modified: time.Now()}
	b := sqliteowner.WatchedUIBuild{ID: strings.Repeat("b", 64), Modified: time.Now()}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for _, keepBusy := range []bool{false, true} {
		var calls, states []string
		applied, err := watchUIMapApply(ctx, a, func(_ context.Context, id string) (UIMapImport, error) {
			calls = append(calls, id)
			if id == a.ID || keepBusy {
				return UIMapImport{}, runtime.ErrWorktreeBusy
			}
			return UIMapImport{BuildMapID: id, Status: "bound"}, nil
		}, func(event UIMapWatchEvent) error {
			states = append(states, event.Status)
			if event.Attempt != len(states) || event.BuildMapID != calls[len(calls)-1] {
				t.Fatalf("wrong observed notification: %+v", event)
			}
			return nil
		}, time.Millisecond, func(context.Context) (sqliteowner.WatchedUIBuild, error) { return b, nil })
		final := "bound"
		if keepBusy {
			final = "unavailable"
		}
		if err != nil || applied != b.ID || !reflect.DeepEqual(calls, []string{a.ID, b.ID}) ||
			!reflect.DeepEqual(states, []string{"busy_rebind", final}) {
			t.Fatalf("calls=%v states=%v applied=%q err=%v", calls, states, applied, err)
		}
	}
}
