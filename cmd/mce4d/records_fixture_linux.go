//go:build linux && mce4d_records_fixture

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/compose"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/model"
)

// Only fixture builds contain this driver. It runs in the broker's synthetic
// installed profile under the same engine identity as the production CLI.
func init() {
	if len(os.Args) < 2 || os.Args[1] != "__records-fixture" {
		return
	}
	if err := runRecordsFixture(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func runRecordsFixture() error {
	if len(os.Args) != 5 || os.Geteuid() != 1000 || os.Getenv("HOME") != "/profiles/runtime/home" {
		return fmt.Errorf("fixture invocation refused")
	}
	prepareNativeProcess()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 32769))
	if err != nil || len(raw) > 32768 {
		return fmt.Errorf("fixture input refused")
	}
	var request model.Request
	switch os.Args[4] {
	case "propose":
		request, err = model.DecodeMappingProposeRequestJSON(raw, 32768)
	case "review":
		request, err = model.DecodeMappingReviewRequestJSON(raw, 32768)
	default:
		return fmt.Errorf("fixture action refused")
	}
	if err != nil {
		return err
	}
	service, closeService, err := compose.OpenNativeRecords(ctx, "/profiles/runtime", "", os.Args[2], os.Args[3], 4)
	if err != nil {
		return err
	}
	result, err := service.ApplyMapping(ctx, request, func(ctx context.Context) error { return ctx.Err() })
	closeErr := closeService()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
