package reconcile_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/donaldgifford/champs/internal/gh"
	"github.com/donaldgifford/champs/internal/ghtest"
	"github.com/donaldgifford/champs/internal/reconcile"
	"github.com/donaldgifford/champs/internal/stringset"
)

var runTeam = gh.TeamSettings{
	Slug:        "security_champions",
	Description: "Security champions",
	Privacy:     "closed",
}

func runApp(t *testing.T, srv *ghtest.Server) *gh.App {
	t.Helper()
	app, err := gh.NewApp(1, ghtest.AppKey(t), gh.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("NewApp() error = %v, want nil", err)
	}
	return app
}

// seedFleet registers three installed orgs with overlapping members.
func seedFleet(srv *ghtest.Server) {
	srv.AddOrg("acme", 7, "alice", "bob")
	srv.AddOrg("beta", 8, "alice", "carol")
	srv.AddOrg("gamma", 9, "dave")
}

func TestRunEmptyRosterPruneGuardFailsBeforeAnyWork(t *testing.T) {
	srv := ghtest.New(t)
	seedFleet(srv)

	_, err := reconcile.Run(context.Background(), &reconcile.Options{
		Source: runApp(t, srv),
		Team:   runTeam,
		Orgs:   []string{"acme", "beta", "gamma"},
		Roster: stringset.New(),
		Prune:  true,
	})

	if !errors.Is(err, reconcile.ErrEmptyRosterPrune) {
		t.Fatalf("Run() error = %v, want ErrEmptyRosterPrune", err)
	}
	if got := srv.TokenMints(); got != 0 {
		t.Errorf("TokenMints() = %d, want 0 — guard must fire before any org work", got)
	}
}

func TestRunResultDeterministicAcrossParallelism(t *testing.T) {
	roster := stringset.New("alice", "bob", "carol", "dave")
	results := make([]*reconcile.Result, 0, 2)

	for _, par := range []int{1, 5} {
		srv := ghtest.New(t)
		seedFleet(srv)
		res, err := reconcile.Run(context.Background(), &reconcile.Options{
			Source:      runApp(t, srv),
			Team:        runTeam,
			Orgs:        []string{"gamma", "acme", "beta"}, // deliberately unsorted
			Roster:      roster,
			Parallelism: par,
		})
		if err != nil {
			t.Fatalf("Run(parallelism=%d) error = %v, want nil", par, err)
		}
		results = append(results, res)
	}

	if !reflect.DeepEqual(results[0], results[1]) {
		t.Errorf("Run() results differ across parallelism:\n1: %+v\n5: %+v",
			results[0], results[1])
	}

	gotOrgs := make([]string, 0, len(results[0].Orgs))
	for _, o := range results[0].Orgs {
		gotOrgs = append(gotOrgs, o.Org)
	}
	if want := []string{"acme", "beta", "gamma"}; !slices.Equal(gotOrgs, want) {
		t.Errorf("Result.Orgs order = %v, want sorted %v", gotOrgs, want)
	}
}

func TestRunOrgErrorDoesNotAbortOthers(t *testing.T) {
	srv := ghtest.New(t)
	seedFleet(srv)
	srv.FailMembershipPut("bob") // fails acme mid-apply

	res, err := reconcile.Run(context.Background(), &reconcile.Options{
		Source: runApp(t, srv),
		Team:   runTeam,
		Orgs:   []string{"acme", "beta", "gamma", "ghost"}, // ghost: no installation
		Roster: stringset.New("alice", "bob", "carol", "dave"),
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil — per-org errors are data", err)
	}

	byOrg := make(map[string]reconcile.OrgResult, len(res.Orgs))
	for _, o := range res.Orgs {
		byOrg[o.Org] = o
	}
	if byOrg["acme"].Err == nil {
		t.Error("acme Err = nil, want the failed PUT surfaced")
	}
	if want := []string{"alice"}; !slices.Equal(byOrg["acme"].Added, want) {
		t.Errorf("acme Added = %v, want partial %v", byOrg["acme"].Added, want)
	}
	for _, org := range []string{"beta", "gamma"} {
		if byOrg[org].Err != nil {
			t.Errorf("%s Err = %v, want nil — a failing org must not abort others", org, byOrg[org].Err)
		}
	}
	if want := []string{"alice", "carol"}; !slices.Equal(byOrg["beta"].Added, want) {
		t.Errorf("beta Added = %v, want %v", byOrg["beta"].Added, want)
	}
	wantGhost := []reconcile.Skip{{Org: "ghost", Reason: reconcile.SkipNoInstallation}}
	if !slices.Equal(byOrg["ghost"].Skips, wantGhost) {
		t.Errorf("ghost Skips = %v, want %v", byOrg["ghost"].Skips, wantGhost)
	}
	if !res.HasErrors() {
		t.Error("HasErrors() = false, want true with a failed org")
	}
}

func TestRunReclassifiesUnknownUsers(t *testing.T) {
	srv := ghtest.New(t)
	seedFleet(srv)
	srv.AddUser("lonely") // real user, member of no managed org

	res, err := reconcile.Run(context.Background(), &reconcile.Options{
		Source: runApp(t, srv),
		Team:   runTeam,
		Orgs:   []string{"acme", "beta", "gamma"},
		Roster: stringset.New("alice", "lonely", "type0-ghost"),
		DryRun: true, // the residue check is a read; it runs in plan too
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if want := []string{"type0-ghost"}; !slices.Equal(res.UnknownUsers, want) {
		t.Errorf("UnknownUsers = %v, want %v", res.UnknownUsers, want)
	}
	if len(res.ResidueErrs) != 0 {
		t.Errorf("ResidueErrs = %v, want none", res.ResidueErrs)
	}
	for _, o := range res.Orgs {
		for _, s := range o.Skips {
			if s.User == "type0-ghost" {
				t.Errorf("%s still holds a %s skip for type0-ghost, want it reclassified", o.Org, s.Reason)
			}
		}
		var lonelySkipped bool
		for _, s := range o.Skips {
			if s.User == "lonely" && s.Reason == reconcile.SkipNotOrgMember {
				lonelySkipped = true
			}
		}
		if !lonelySkipped {
			t.Errorf("%s Skips = %v, want lonely kept as not_org_member", o.Org, o.Skips)
		}
	}

	// One lookup per residue login; logins seen in any org are never
	// looked up.
	for path, want := range map[string]int{
		"/users/type0-ghost": 1,
		"/users/lonely":      1,
		"/users/alice":       0,
	} {
		if got := srv.Hits(path); got != want {
			t.Errorf("Hits(%s) = %d, want %d", path, got, want)
		}
	}
}

func TestRunResidueSkippedWithoutAnyClient(t *testing.T) {
	srv := ghtest.New(t) // no orgs registered: every org is no_installation

	res, err := reconcile.Run(context.Background(), &reconcile.Options{
		Source: runApp(t, srv),
		Team:   runTeam,
		Orgs:   []string{"ghost"},
		Roster: stringset.New("type0-ghost"),
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if len(res.UnknownUsers) != 0 {
		t.Errorf("UnknownUsers = %v, want none without a residue client", res.UnknownUsers)
	}
	if got := srv.Hits("/users/type0-ghost"); got != 0 {
		t.Errorf("Hits(/users/type0-ghost) = %d, want 0", got)
	}
}
