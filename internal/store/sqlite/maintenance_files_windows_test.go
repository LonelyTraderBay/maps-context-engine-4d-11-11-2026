package sqlite

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/paths"
	"golang.org/x/sys/windows"
)

func windowsTestControl(t *testing.T, dir string) windowsMaintenanceFiles {
	t.Helper()
	token := windows.GetCurrentProcessToken()
	identity, err := token.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	acl := windowsControlACL{owner: identity.User.Sid}
	descriptor, err := acl.descriptor()
	if err != nil {
		t.Fatal(err)
	}
	if dir == "" {
		dir = filepath.Join(t.TempDir(), "control")
		attr := windows.SecurityAttributes{SecurityDescriptor: descriptor}
		attr.Length = uint32(unsafe.Sizeof(attr))
		if err := windows.CreateDirectory(windows.StringToUTF16Ptr(dir), &attr); err != nil {
			t.Fatal(err)
		}
	}
	root, err := paths.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	return windowsMaintenanceFiles{control: root, acl: acl, profile: maintenanceJournalFixture().ProfileInstallationID, scope: "catalog"}
}

func windowsTestPreparedJournal() MaintenanceJournal {
	next := maintenanceJournalFixture()
	next.Phase = "PREPARED"
	next.NewStores[0].LogicalSHA256 = strings.Repeat("c", 64)
	for _, step := range maintenanceSteps(next.Target) {
		next.Files = append(next.Files, MaintenanceFileProof{
			Step: step, OldSHA256: strings.Repeat("b", 64), NewSHA256: strings.Repeat("c", 64),
		})
	}
	return next
}

func TestWindowsMaintenanceTransitionsAndArchive(t *testing.T) {
	j := windowsTestControl(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, present, err := j.read(ctx); present || err != nil {
		t.Fatalf("fresh journal read: present=%v %v", present, err)
	}
	if err := j.write(ctx, maintenanceJournalFixture(), false, nil); err != nil {
		t.Fatal(err)
	}
	if err := j.checkServe(ctx, ""); !errors.Is(err, ErrStoreRecoveryRequired) {
		t.Fatalf("public serving gate did not hold: %v", err)
	}
	if err := j.checkServe(ctx, "99999999-9999-4999-8999-999999999999"); !errors.Is(err, ErrStoreRecoveryRequired) {
		t.Fatalf("another recovery bypassed the gate: %v", err)
	}
	verb := windowsTestPreparedJournal()
	if err := j.write(ctx, verb, true, nil); err != nil {
		t.Fatal(err)
	}
	verb.Phase = "REPLACING"
	if err := j.write(ctx, verb, true, nil); err != nil {
		t.Fatal(err)
	}
	mutant := verb
	mutant.CompletedSteps = maintenanceSteps("catalog")
	if err := j.write(ctx, mutant, true, nil); !errors.Is(err, ErrStoreRecoveryRequired) {
		t.Fatalf("out-of-order completion accepted: %v", err)
	}
	for _, step := range maintenanceSteps(verb.Target) {
		verb.CompletedSteps = append(verb.CompletedSteps, step)
		if err := j.write(ctx, verb, true, nil); err != nil {
			t.Fatal(err)
		}
	}
	verb.Phase = "VERIFYING"
	if err := j.write(ctx, verb, true, nil); err != nil {
		t.Fatal(err)
	}
	verb.Phase, verb.Outcome = "VERIFIED", "restored"
	if err := j.write(ctx, verb, true, nil); err != nil {
		t.Fatal(err)
	}
	if err := j.checkServe(ctx, ""); err != nil {
		t.Fatal(err)
	}
	dir, err := j.control.CanonicalPath()
	if err != nil {
		t.Fatal(err)
	}
	read := func(name string) []byte {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	expected := read(windowsMaintenanceName)
	if report, ok, err := j.read(ctx); err != nil || !ok || report.Phase != "VERIFIED" || report.Outcome != "restored" {
		t.Fatalf("completed journal did not survive reopening: %v", err)
	}
	if !bytes.Equal(expected, read(windowsMaintenanceName)) {
		t.Fatal("read operation modified the journal")
	}
	next := maintenanceJournalFixture()
	next.OperationID = "33333333-3333-4333-8333-333333333333"
	next.NewStores[0].Incarnation = next.OperationID
	if err := j.write(ctx, next, false, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(expected, read("maintenance.completed."+verb.OperationID+".json")) {
		t.Fatal("previous verified journal was not preserved")
	}
	if report, ok, err := j.read(ctx); err != nil || !ok || report.OperationID != next.OperationID || report.Phase != "NEEDS_RECOVERY" {
		t.Fatalf("next installation-scoped operation not published: %v", err)
	}
}

func TestWindowsMaintenanceJournalRefusesUntrustedACL(t *testing.T) {
	j := windowsTestControl(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := j.write(ctx, maintenanceJournalFixture(), false, nil); err != nil {
		t.Fatal(err)
	}
	root, err := j.control.CanonicalPath()
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := windows.SecurityDescriptorFromString(
		"D:P(A;;FA;;;" + j.acl.owner.String() + ")(A;;FW;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(filepath.Join(root, windowsMaintenanceName), windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := j.read(ctx); !errors.Is(err, ErrStoreRecoveryRequired) || !errors.Is(err, ErrProfileDenied) {
		t.Fatalf("unsafe current journal was not rejected: %v", err)
	}
	if err := j.write(ctx, windowsTestPreparedJournal(), true, nil); !errors.Is(err, ErrStoreRecoveryRequired) {
		t.Fatalf("unsafe journal was adopted during write: %v", err)
	}
}

func TestWindowsMaintenanceJournalCrashWorker(t *testing.T) {
	root, point := os.Getenv("MCE4D_WIN_JOURNAL_TEST_ROOT"), os.Getenv("MCE4D_WIN_JOURNAL_TEST_POINT")
	if root == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	j := windowsTestControl(t, root)
	err := j.write(ctx, windowsTestPreparedJournal(), true, func(boundary string) {
		if boundary == point {
			fmt.Println("JOURNAL_BOUNDARY " + boundary)
			<-ctx.Done()
		}
	})
	t.Fatalf("crash worker exited without being stopped: %v", err)
}

func TestWindowsMaintenanceJournalProcessCrash(t *testing.T) {
	for _, tc := range []struct{ point, phase string }{
		{"prepared-file", "NEEDS_RECOVERY"},
		{"published-name", "PREPARED"},
	} {
		t.Run(tc.point, func(t *testing.T) {
			j := windowsTestControl(t, "")
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := j.write(ctx, maintenanceJournalFixture(), false, nil); err != nil {
				t.Fatal(err)
			}
			dir, err := j.control.CanonicalPath()
			if err != nil {
				t.Fatal(err)
			}
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(ctx, binary, "-test.run=^TestWindowsMaintenanceJournalCrashWorker$", "-test.timeout=18s")
			cmd.Env = append(os.Environ(), "MCE4D_WIN_JOURNAL_TEST_ROOT="+dir, "MCE4D_WIN_JOURNAL_TEST_POINT="+tc.point)
			output, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if cmd.ProcessState == nil {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
			}()
			reader := bufio.NewReader(io.LimitReader(output, 8192))
			line, err := reader.ReadString('\n')
			if err != nil || strings.TrimSpace(line) != "JOURNAL_BOUNDARY "+tc.point {
				more, _ := io.ReadAll(reader)
				t.Fatalf("did not reach boundary: %q %q %v", line, more, err)
			}
			if err := j.write(ctx, windowsTestPreparedJournal(), true, nil); !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
				t.Fatalf("a second writer advanced the held journal: %v", err)
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err == nil {
				t.Fatal("expected abrupt termination")
			}
			report, ok, err := j.read(ctx)
			if err != nil || !ok || report.Phase != tc.phase {
				t.Fatalf("observed journal at restart: present=%v phase=%q err=%v", ok, report.Phase, err)
			}
			if err := j.checkServe(ctx, ""); !errors.Is(err, ErrStoreRecoveryRequired) {
				t.Fatalf("serving was not fenced after the crash: %v", err)
			}
			if tc.phase == "NEEDS_RECOVERY" {
				if err := j.write(ctx, windowsTestPreparedJournal(), true, nil); err != nil {
					t.Fatal(err)
				}
			}
			next := windowsTestPreparedJournal()
			next.Phase = "REPLACING"
			if err := j.write(ctx, next, true, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}
