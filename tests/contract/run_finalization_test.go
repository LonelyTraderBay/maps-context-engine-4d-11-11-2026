package contract

import "testing"

// These fixtures specify the state contract only. OS process, path containment,
// authorization and cleanup enforcement are separate owner-task proofs.
type finishFixture struct {
	exitKnown                                              bool
	exit                                                   int
	writersStopped, cleanupRequired, cleanupOK, finalizeOK bool
}

func (f finishFixture) result() string {
	if !f.writersStopped {
		return "writers_not_stopped"
	}
	if !f.exitKnown {
		return "unknown_command_result"
	}
	if f.exit != 0 {
		return "command_failed"
	}
	if (f.cleanupRequired && !f.cleanupOK) || !f.finalizeOK {
		return "finalization_failed"
	}
	return "success"
}
func TestRunFinalizationContract(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state finishFixture
		want  string
	}{
		{"command fail and cleanup fail", finishFixture{true, 7, true, true, false, false}, "command_failed"},
		{"success but cleanup fail", finishFixture{true, 0, true, true, false, true}, "finalization_failed"},
		{"success but finalization fail", finishFixture{true, 0, true, false, true, false}, "finalization_failed"},
		{"unknown after crash", finishFixture{false, 0, true, false, false, true}, "unknown_command_result"},
		{"writers still running", finishFixture{true, 0, false, true, true, true}, "writers_not_stopped"},
		{"success", finishFixture{true, 0, true, true, true, true}, "success"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.state.result(); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

type cleanupFixture struct{ authorized, owned, disposable, containmentVerified, writersStopped, contentExpected bool }

func (f cleanupFixture) allowed() bool {
	return f.authorized && f.owned && f.disposable && f.containmentVerified && f.writersStopped && f.contentExpected
}
func TestRunCleanupContractFailClosed(t *testing.T) {
	valid := cleanupFixture{true, true, true, true, true, true}
	if !valid.allowed() {
		t.Fatal("valid cleanup proof rejected")
	}
	cases := []cleanupFixture{{false, true, true, true, true, true}, {true, false, true, true, true, true}, {true, true, false, true, true, true}, {true, true, true, false, true, true}, {true, true, true, true, false, true}, {true, true, true, true, true, false}}
	for _, c := range cases {
		if c.allowed() {
			t.Fatal("missing cleanup prerequisite accepted")
		}
	}
}

// commandContractFixture models reviewed metadata, never inspects or runs user commands.
type commandContractFixture struct {
	cwd, executable                      string
	argv                                 []string
	sourceReviewed, childWrites, network bool
}

func (c commandContractFixture) readOnlyAllowed() bool {
	if c.cwd == "" || c.executable == "" || !c.sourceReviewed || c.childWrites || c.network {
		return false
	}
	for _, arg := range c.argv {
		if arg == "<path>" || arg == "<command>" {
			return false
		}
	}
	return true
}
func TestRunCommandContractRejectsUnreviewedEffects(t *testing.T) {
	good := commandContractFixture{cwd: "fixture", executable: "git", argv: []string{"status", "--short"}, sourceReviewed: true}
	if !good.readOnlyAllowed() {
		t.Fatal("known read-only fixture rejected")
	}
	for _, bad := range []commandContractFixture{
		{cwd: "fixture", executable: "check", argv: []string{"--dry-run"}, sourceReviewed: true, childWrites: true},
		{cwd: "fixture", executable: "check", argv: []string{"--dry-run"}, sourceReviewed: true, network: true},
		{cwd: "fixture", executable: "check", argv: []string{"<path>"}, sourceReviewed: true},
		{cwd: "fixture", executable: "check", argv: []string{"--dry-run"}},
		{cwd: "", executable: "check", sourceReviewed: true},
	} {
		if bad.readOnlyAllowed() {
			t.Fatal("unknown, placeholder or write effect accepted")
		}
	}
}
