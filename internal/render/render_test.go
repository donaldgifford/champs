package render_test

import (
	"bufio"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/donaldgifford/champs/internal/reconcile"
	"github.com/donaldgifford/champs/internal/render"
)

// fullResult exercises every rendering path: adds, removes, all three
// skip reasons, an org error with partial progress, and a residue error.
func fullResult() *reconcile.Result {
	return &reconcile.Result{
		Prune: true,
		Orgs: []reconcile.OrgResult{
			{
				Org:     "org1",
				Added:   []string{"alice", "bob"},
				Removed: []string{"mallory"},
				Skips: []reconcile.Skip{
					{User: "carol", Org: "org1", Reason: reconcile.SkipNotOrgMember},
				},
			},
			{
				Org:   "org2",
				Added: []string{"dave"},
				Err:   errors.New("adding eve to team security_champions in org2: 500"),
			},
			{
				Org:   "org3",
				Skips: []reconcile.Skip{{Org: "org3", Reason: reconcile.SkipNoInstallation}},
			},
		},
		UnknownUsers: []string{"type0-ghost"},
		ResidueErrs:  []error{errors.New("looking up user flaky: 500")},
	}
}

func TestRenderPlainFullStream(t *testing.T) {
	var out strings.Builder
	r := render.Renderer{Out: &out, Color: false}
	if err := r.Render(fullResult()); err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}

	want := `org1:
  + alice
  + bob
  - mallory

org2:
  + dave
  ! adding eve to team security_champions in org2: 500

{"level":"INFO","msg":"skip","user":"carol","org":"org1","reason":"not_org_member"}
{"level":"WARN","msg":"skip","org":"org3","reason":"no_installation"}
{"level":"INFO","msg":"skip","user":"type0-ghost","org":"","reason":"unknown_user"}
Summary:
  org1: 2 added, 1 removed, 1 skipped
  org2: error: adding eve to team security_champions in org2: 500
  org3: 0 added, 0 removed, 1 skipped
  residue check error: looking up user flaky: 500
Applied: 3 added, 1 removed, 3 skipped, 2 error(s).
`
	if got := out.String(); got != want {
		t.Errorf("Render() output:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderSkipLinesAreTimestampFreeJSON(t *testing.T) {
	var out strings.Builder
	r := render.Renderer{Out: &out, Color: false}
	if err := r.Render(fullResult()); err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}

	var skipLines int
	sc := bufio.NewScanner(strings.NewReader(out.String()))
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "{") {
			continue
		}
		skipLines++
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("skip line %q is not JSON: %v", line, err)
		}
		if _, ok := rec["time"]; ok {
			t.Errorf("skip line %q carries a timestamp, want none (diffable runs)", line)
		}
		if rec["msg"] != "skip" || rec["reason"] == "" {
			t.Errorf("skip line %q missing msg/reason fields", line)
		}
	}
	if skipLines != 3 {
		t.Errorf("skip line count = %d, want 3", skipLines)
	}
}

func TestRenderColor(t *testing.T) {
	var out strings.Builder
	r := render.Renderer{Out: &out, Color: true}
	if err := r.Render(fullResult()); err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}
	got := out.String()

	for _, want := range []string{
		"\x1b[32m  + alice\x1b[0m",
		"\x1b[31m  - mallory\x1b[0m",
		"\x1b[1mSummary:\x1b[0m",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Render() output missing %q", want)
		}
	}
	// Skip records stay machine-readable even with color on.
	if strings.Contains(got, "\x1b[32m{") || strings.Contains(got, "\x1b[31m{") {
		t.Error("Render() colorized a skip JSON line, want plain")
	}
}

func TestRenderNoChangesPrintsOnlySummary(t *testing.T) {
	var out strings.Builder
	r := render.Renderer{Out: &out, Color: false}
	res := &reconcile.Result{
		DryRun: true,
		Orgs:   []reconcile.OrgResult{{Org: "org1"}, {Org: "org2"}},
	}
	if err := r.Render(res); err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}

	want := `Summary:
  org1: 0 added, 0 removed, 0 skipped
  org2: 0 added, 0 removed, 0 skipped
Plan: 0 to add, 0 to remove, 0 skipped.
`
	if got := out.String(); got != want {
		t.Errorf("Render() output:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderDryRunWording(t *testing.T) {
	var out strings.Builder
	r := render.Renderer{Out: &out, Color: false}
	res := &reconcile.Result{
		DryRun: true,
		Orgs:   []reconcile.OrgResult{{Org: "org1", Added: []string{"alice"}}},
	}
	if err := r.Render(res); err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}
	if !strings.Contains(out.String(), "Plan: 1 to add, 0 to remove, 0 skipped.") {
		t.Errorf("Render() output = %q, want Plan wording in dry-run", out.String())
	}
}
