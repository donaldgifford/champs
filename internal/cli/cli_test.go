package cli

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/donaldgifford/champs/internal/config"
	"github.com/donaldgifford/champs/internal/ghtest"
)

const testHCL = `team {
  slug        = "security_champions"
  description = "Security champions"
  privacy     = "closed"
}

github {
  app_id = 1
}

org "acme" {}
org "beta" {}
org "gamma" {}
`

const testCSV = "login\nalice\nbob\ncarol\ndave\n"

var testInfo = BuildInfo{Version: "1.2.3", Commit: "abc1234", Date: "2026-07-31"}

// seedFleet mirrors the reconcile tests: three installed orgs with
// overlapping members.
func seedFleet(srv *ghtest.Server) {
	srv.AddOrg("acme", 7, "alice", "bob")
	srv.AddOrg("beta", 8, "alice", "carol")
	srv.AddOrg("gamma", 9, "dave")
}

// runCLI writes the config and roster fixtures into a temp dir, points
// the real production path at srv, and drives execute end to end.
// t.Setenv forbids t.Parallel, which suits this file.
func runCLI(t *testing.T, srv *ghtest.Server, hcl, csv string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "champs.hcl")
	rosPath := filepath.Join(dir, "roster.csv")
	if err := os.WriteFile(cfgPath, []byte(hcl), 0o600); err != nil {
		t.Fatalf("writing config fixture: %v", err)
	}
	if err := os.WriteFile(rosPath, []byte(csv), 0o600); err != nil {
		t.Fatalf("writing roster fixture: %v", err)
	}
	t.Setenv(config.EnvPrivateKey, string(ghtest.AppKey(t)))
	t.Setenv(EnvBaseURL, srv.URL)

	var out, errOut strings.Builder
	full := append(slices.Clone(args), "--config", cfgPath, "--roster", rosPath)
	code = execute(context.Background(), testInfo, full, &out, &errOut)
	return out.String(), errOut.String(), code
}

func TestPlanRendersDiffAndWritesNothing(t *testing.T) {
	srv := ghtest.New(t)
	seedFleet(srv)

	stdout, stderr, code := runCLI(t, srv, testHCL, testCSV, "plan")

	if code != 0 {
		t.Fatalf("plan exit = %d (stderr %q), want 0", code, stderr)
	}
	for _, want := range []string{
		"acme:", "  + alice", "  + bob",
		"beta:", "  + carol",
		"gamma:", "  + dave",
		`"reason":"not_org_member"`,
		"Plan: 5 to add, 0 to remove, 7 skipped.",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("plan stdout missing %q:\n%s", want, stdout)
		}
	}
	if puts := srv.MembershipPuts(); len(puts) != 0 {
		t.Errorf("MembershipPuts() = %v, want none from plan", puts)
	}
	if stderr != "" {
		t.Errorf("plan stderr = %q, want empty", stderr)
	}
}

func TestPlanIsTrueAliasForApplyDryRun(t *testing.T) {
	srv := ghtest.New(t)
	seedFleet(srv)

	planOut, _, planCode := runCLI(t, srv, testHCL, testCSV, "plan")
	applyOut, _, applyCode := runCLI(t, srv, testHCL, testCSV, "apply", "--dry-run")

	if planCode != 0 || applyCode != 0 {
		t.Fatalf("exit codes plan=%d apply=%d, want 0 0", planCode, applyCode)
	}
	if planOut != applyOut {
		t.Errorf("plan output differs from apply --dry-run:\nplan:\n%s\napply:\n%s",
			planOut, applyOut)
	}
	if puts := srv.MembershipPuts(); len(puts) != 0 {
		t.Errorf("MembershipPuts() = %v, want none", puts)
	}
}

func TestApplyReconcilesAndExitsZero(t *testing.T) {
	srv := ghtest.New(t)
	seedFleet(srv)

	stdout, stderr, code := runCLI(t, srv, testHCL, testCSV, "apply")

	if code != 0 {
		t.Fatalf("apply exit = %d (stderr %q), want 0", code, stderr)
	}
	if !strings.Contains(stdout, "Applied: 5 added, 0 removed, 7 skipped.") {
		t.Errorf("apply stdout missing Applied totals:\n%s", stdout)
	}
	if got := srv.TeamMembers("acme", "security_champions"); len(got) != 2 {
		t.Errorf("acme team members = %v, want alice and bob", got)
	}
}

func TestApplyOrgErrorExitsOneOthersStillReconcile(t *testing.T) {
	srv := ghtest.New(t)
	seedFleet(srv)
	srv.FailMembershipPut("bob")

	stdout, stderr, code := runCLI(t, srv, testHCL, testCSV, "apply")

	if code != 1 {
		t.Fatalf("apply exit = %d, want 1 with a failed org", code)
	}
	if !strings.Contains(stdout, "acme: error:") {
		t.Errorf("apply stdout missing acme error line:\n%s", stdout)
	}
	if got := srv.TeamMembers("beta", "security_champions"); len(got) != 2 {
		t.Errorf("beta team members = %v, want reconciled despite acme failure", got)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty — per-org errors live in the summary", stderr)
	}
}

func TestEmptyRosterPruneGuardIsFatal(t *testing.T) {
	srv := ghtest.New(t)
	seedFleet(srv)

	_, stderr, code := runCLI(t, srv, testHCL, "login\n", "apply", "--prune")

	if code != 1 {
		t.Fatalf("apply exit = %d, want 1 on guard trip", code)
	}
	if !strings.Contains(stderr, "empty roster with prune enabled") {
		t.Errorf("stderr = %q, want the prune guard named", stderr)
	}
	if got := srv.TokenMints(); got != 0 {
		t.Errorf("TokenMints() = %d, want 0 — guard fires before any API work", got)
	}
}

func TestUnknownOrgsFlagFailsBeforeAnyAPICall(t *testing.T) {
	srv := ghtest.New(t)
	seedFleet(srv)

	_, stderr, code := runCLI(t, srv, testHCL, testCSV, "plan", "--orgs", "acme,bogus")

	if code != 1 {
		t.Fatalf("plan exit = %d, want 1 for unknown --orgs", code)
	}
	if !strings.Contains(stderr, `"bogus"`) || !strings.Contains(stderr, "acme, beta, gamma") {
		t.Errorf("stderr = %q, want the bogus org named and configured orgs listed", stderr)
	}
	if got := srv.TokenMints(); got != 0 {
		t.Errorf("TokenMints() = %d, want 0 — validation precedes any API call", got)
	}
}

func TestOrgsFlagLimitsTheRun(t *testing.T) {
	srv := ghtest.New(t)
	seedFleet(srv)

	stdout, _, code := runCLI(t, srv, testHCL, testCSV, "apply", "--orgs", "beta")

	if code != 0 {
		t.Fatalf("apply exit = %d, want 0", code)
	}
	if strings.Contains(stdout, "acme") {
		t.Errorf("stdout mentions acme on an --orgs beta run:\n%s", stdout)
	}
	if got := srv.Hits("/orgs/acme/installation"); got != 0 {
		t.Errorf("Hits(acme installation) = %d, want 0", got)
	}
	if got := srv.TeamMembers("beta", "security_champions"); len(got) != 2 {
		t.Errorf("beta team members = %v, want alice and carol", got)
	}
}

func TestPipedOutputCarriesNoANSI(t *testing.T) {
	srv := ghtest.New(t)
	seedFleet(srv)

	stdout, stderr, code := runCLI(t, srv, testHCL, testCSV, "apply")

	if code != 0 {
		t.Fatalf("apply exit = %d, want 0", code)
	}
	if ansi := regexp.MustCompile(`\x1b\[`); ansi.MatchString(stdout) || ansi.MatchString(stderr) {
		t.Error("output contains ANSI escapes, want none for a non-TTY writer")
	}
}

func TestInvalidConfigRendersDiagnostics(t *testing.T) {
	srv := ghtest.New(t)

	_, stderr, code := runCLI(t, srv, "team {\n", testCSV, "plan")

	if code != 1 {
		t.Fatalf("plan exit = %d, want 1 on invalid HCL", code)
	}
	if !strings.Contains(stderr, "champs.hcl") {
		t.Errorf("stderr = %q, want rendered diagnostics naming the file", stderr)
	}
}

func TestVersionOutputs(t *testing.T) {
	want := "champs 1.2.3 (commit: abc1234, built: 2026-07-31)\n"
	for _, args := range [][]string{{"version"}, {"--version"}} {
		var out, errOut strings.Builder
		code := execute(context.Background(), testInfo, args, &out, &errOut)
		if code != 0 {
			t.Fatalf("%v exit = %d, want 0", args, code)
		}
		if out.String() != want {
			t.Errorf("%v output = %q, want %q", args, out.String(), want)
		}
	}
}

func TestValidateOrgsDedupes(t *testing.T) {
	cfg := &config.Config{Orgs: []config.Org{{Name: "acme"}, {Name: "beta"}}}
	got, err := validateOrgs(cfg, []string{"beta", "beta", "acme"})
	if err != nil {
		t.Fatalf("validateOrgs() error = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Errorf("validateOrgs() = %v, want deduped [acme beta]", got)
	}
}
