//go:build linux && mce4d_gc_fixture

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/compose"
)

func init() {
	if len(os.Args) < 2 || os.Args[1] != "__gc-rollback-fixture" {
		return
	}
	if len(os.Args) != 5 {
		os.Exit(2)
	}
	prepareNativeProcess()
	ctx, cancel := context.WithTimeout(context.Background(), 95*time.Second)
	checks, err := compose.ExerciseProtectedGCRollbackFixture(ctx, os.Args[2], os.Args[3], os.Args[4])
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("C150_PROTECTED_GC_CHECKS=%d\n", checks)
	os.Exit(0)
}
