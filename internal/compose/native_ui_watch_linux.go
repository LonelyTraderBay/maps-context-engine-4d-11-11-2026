//go:build linux

package compose

import (
	"context"
	"errors"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
	sqliteowner "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/store/sqlite"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/uimap"
)

type UIMapWatchEvent struct {
	BuildMapID   string `json:"build_map_id"`
	BindingID    string `json:"binding_id,omitempty"`
	Status       string `json:"status"`
	QueueAgeMS   int64  `json:"queue_age_ms"`
	WriterWaitMS int64  `json:"writer_wait_ms"`
	RebindMS     int64  `json:"rebind_ms"`
	Attempt      int    `json:"attempt"`
}

func RunNativeUIMapWatch(ctx context.Context, installation, config, worktree, profile, directory string, cpus int,
	emit func(UIMapWatchEvent) error) (resultErr error) {
	if emit == nil || !sqliteowner.NativeUIMapsEnabled() {
		return app.ErrCoreUnsupported
	}
	starting, stopStart := context.WithTimeout(ctx, 15*time.Second)
	defer stopStart()
	state, err := sqliteowner.OpenProtectedState(starting, installation)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, state.Close()) }()
	service, err := protectedConfig(starting, state, config, cpus)
	if err != nil {
		return err
	}
	stopStart()
	startup, _, err := service.StartupConfig()
	if err != nil {
		return err
	}
	poll, err := nativeMilliseconds(startup.Explorer.MapWatchPollMs)
	if err != nil {
		return err
	}
	debounce, err := nativeMilliseconds(startup.Explorer.MapWatchDebounceMs)
	if err != nil {
		return err
	}
	apply := func(ctx context.Context, id string) (UIMapImport, error) {
		artifact, err := state.ReadWatchedUIBuild(ctx, directory, worktree, id, startup.Explorer.MapArtifactMaxBytes)
		if err != nil {
			return UIMapImport{}, err
		}
		return importNativeUIMap(ctx, installation, config, worktree, profile, id, artifact, cpus)
	}
	newest := func(parent context.Context) (sqliteowner.WatchedUIBuild, error) {
		bounded, cancel := context.WithTimeout(parent, 5*time.Second)
		defer cancel()
		candidate, err := state.NewestUIBuild(bounded, directory)
		if parent.Err() != nil {
			return sqliteowner.WatchedUIBuild{}, parent.Err()
		}
		return candidate, err
	}
	last := ""
	for {
		candidate, err := newest(ctx)
		if err != nil {
			return err
		}
		if candidate.ID != "" && candidate.ID != last {
			if err := waitUIMap(ctx, debounce); err != nil {
				return err
			}
			candidate, err = newest(ctx)
			if err != nil {
				return err
			}
			if candidate.ID != "" && candidate.ID != last {
				iteration, cancel := context.WithTimeout(ctx, 45*time.Second)
				last, err = watchUIMapApply(iteration, candidate, apply, emit, debounce, newest)
				cancel()
				if err != nil {
					return err
				}
			}
		}
		if err := waitUIMap(ctx, poll); err != nil {
			return err
		}
	}
}

func watchUIMapApply(ctx context.Context, candidate sqliteowner.WatchedUIBuild, apply func(context.Context, string) (UIMapImport, error),
	emit func(UIMapWatchEvent) error, debounce time.Duration, newest func(context.Context) (sqliteowner.WatchedUIBuild, error)) (string, error) {
	for attempt := 1; attempt <= 2; attempt++ {
		started := time.Now()
		out, err := apply(ctx, candidate.ID)
		event := UIMapWatchEvent{BuildMapID: candidate.ID, BindingID: out.BindingID,
			QueueAgeMS: max(0, started.Sub(candidate.Modified).Milliseconds()), WriterWaitMS: out.WriterWait.Milliseconds(),
			RebindMS: time.Since(started).Milliseconds(), Attempt: attempt}
		switch {
		case ctx.Err() != nil:
			return "", ctx.Err()
		case err == nil:
			event.Status = out.Status
		case errors.Is(err, runtime.ErrWorktreeBusy):
			event.Status = "busy_rebind"
			if attempt == 2 {
				event.Status = "unavailable"
			}
		case errors.Is(err, uimap.ErrBuildArtifact):
			event.Status = "invalid_artifact"
		default:
			return "", err
		}
		if err := emit(event); err != nil {
			return "", err
		}
		if event.Status != "busy_rebind" {
			return candidate.ID, nil
		}
		if err := waitUIMap(ctx, debounce); err != nil {
			return "", err
		}
		candidate, err = newest(ctx)
		if err != nil || candidate.ID == "" {
			return candidate.ID, err
		}
	}
	return candidate.ID, nil
}

func waitUIMap(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}
