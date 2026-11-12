package runtime

import (
	"context"
	"errors"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
)

var ErrGoToolchain = errors.New("Go executable identity or authority is unverified")

type GoExecutableInput struct {
	BinRoot        *paths.Root // borrowed; must stay open until Close
	ExpectedSHA256 string
	MaxBytes       int64
	Authorize      func(context.Context) error
}
