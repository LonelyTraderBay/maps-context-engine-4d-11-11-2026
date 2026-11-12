package index

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/policy"
)

func TestCheckModuleAcyclicDetectsCyclesAndHonorsBudget(t *testing.T) {
	_, db, stage := stageFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for _, id := range []string{"root", "child"} {
		if _, err := db.Exec(`INSERT INTO modules(generation_id,module_id,label,kind,plane) VALUES(?,?,?,'module','observed_static')`, fixtureGeneration, id, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`UPDATE modules SET parent_module_id='root' WHERE generation_id=? AND module_id='child'`, fixtureGeneration); err != nil {
		t.Fatal(err)
	}
	if err := checkModuleAcyclic(ctx, db, fixtureGeneration, 10, stage.Capture.Scanner); err != nil {
		t.Fatal(err)
	}
	if err := checkModuleAcyclic(ctx, db, fixtureGeneration, 1, stage.Capture.Scanner); !errors.Is(err, ErrCaptureLimit) {
		t.Fatalf("module budget ignored: %v", err)
	}
	if _, err := db.Exec(`UPDATE modules SET label='api_key = abcdefghijklmnopqrstuvwxyz' WHERE generation_id=? AND module_id='root'`, fixtureGeneration); err != nil {
		t.Fatal(err)
	}
	if err := checkModuleAcyclic(ctx, db, fixtureGeneration, 10, stage.Capture.Scanner); !errors.Is(err, policy.ErrSecretBlocked) {
		t.Fatalf("secret module label accepted: %v", err)
	}
	if _, err := db.Exec(`UPDATE modules SET label='root' WHERE generation_id=? AND module_id='root'`, fixtureGeneration); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE modules SET parent_module_id='child' WHERE generation_id=? AND module_id='root'`, fixtureGeneration); err != nil {
		t.Fatal(err)
	}
	if err := checkModuleAcyclic(ctx, db, fixtureGeneration, 10, stage.Capture.Scanner); !errors.Is(err, ErrPublishIntegrity) {
		t.Fatalf("cycle accepted: %v", err)
	}
}
