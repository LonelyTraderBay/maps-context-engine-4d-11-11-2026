package compose

import (
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
)

func nativeMilliseconds(value int64) (time.Duration, error) {
	if value < 1 || value > int64((1<<63-1)/time.Millisecond) {
		return 0, app.ErrCoreInput
	}
	return time.Duration(value) * time.Millisecond, nil
}
