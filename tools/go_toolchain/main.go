// go_toolchain is an offline, read-only administrator fingerprint utility.
// It does not approve a profile, execute Go/gopls, install or change any files.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/runtime"
)

func main() {
	flags := flag.NewFlagSet("go_toolchain", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", "", "")
	if flags.Parse(os.Args[1:]) != nil || *root == "" || flags.NArg() != 0 {
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	ctx, deadline := context.WithTimeout(ctx, 60*time.Second)
	defer deadline()
	proof, err := runtime.InspectGoToolchain(ctx, *root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "approved toolchain fingerprint unavailable")
		os.Exit(3)
	}
	digest := proof.Digest()
	err = proof.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, "approved toolchain verification failed")
		os.Exit(3)
	}
	if json.NewEncoder(os.Stdout).Encode(struct {
		Digest string `json:"toolchain_digest"`
	}{digest}) != nil {
		os.Exit(5)
	}
}
