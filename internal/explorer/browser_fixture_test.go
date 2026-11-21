package explorer

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"
)

// The Windows browser harness owns the fixture subprocess and consumes its
// bootstrap material through the private pipe. Ordinary test runs skip it.
func TestExplorerBrowserFixture(t *testing.T) {
	if os.Getenv("MCE4D_T082_BROWSER_FIXTURE") != "1" {
		t.Skip("requires the local Explorer browser harness")
	}
	s, _, code := fixtureServer(t, time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := json.NewEncoder(os.Stdout).Encode(map[string]string{"origin": s.Origin(), "code": code}); err != nil {
		t.Fatal("fixture pipe is unavailable")
	}
	go func() {
		var stop [1]byte
		_, _ = io.ReadFull(os.Stdin, stop[:])
		cancel()
	}()
	if err := s.Serve(ctx); err != nil {
		t.Fatal("fixture shutdown failed")
	}
}
