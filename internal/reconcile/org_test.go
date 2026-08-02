package reconcile

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/donaldgifford/champs/internal/gh"
	"github.com/donaldgifford/champs/internal/ghtest"
	"github.com/donaldgifford/champs/internal/stringset"
)

var testTeam = gh.TeamSettings{
	Slug:        "security_champions",
	Description: "Security champions",
	Privacy:     "closed",
}

func testApp(t *testing.T, srv *ghtest.Server) *gh.App {
	t.Helper()
	app, err := gh.NewApp(1, ghtest.AppKey(t), gh.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("NewApp() error = %v, want nil", err)
	}
	return app
}

func TestComputeDiff(t *testing.T) {
	tests := []struct {
		name        string
		roster      []string
		orgMembers  []string
		teamMembers []string
		prune       bool
		wantAdds    []string
		wantRemoves []string
		wantSkips   []Skip
	}{
		{
			name:       "all roster in org, empty team",
			roster:     []string{"alice", "bob"},
			orgMembers: []string{"alice", "bob", "carol"},
			wantAdds:   []string{"alice", "bob"},
		},
		{
			name:       "roster login not in org is skipped, never added",
			roster:     []string{"alice", "outsider"},
			orgMembers: []string{"alice"},
			wantAdds:   []string{"alice"},
			wantSkips:  []Skip{{User: "outsider", Org: "acme", Reason: SkipNotOrgMember}},
		},
		{
			name:        "team extras kept without prune",
			roster:      []string{"alice"},
			orgMembers:  []string{"alice", "bob"},
			teamMembers: []string{"alice", "bob"},
		},
		{
			name:        "team extras removed with prune",
			roster:      []string{"alice"},
			orgMembers:  []string{"alice", "bob"},
			teamMembers: []string{"alice", "bob"},
			prune:       true,
			wantRemoves: []string{"bob"},
		},
		{
			name:        "team member who left the org is pruned",
			roster:      []string{"alice", "departed"},
			orgMembers:  []string{"alice"},
			teamMembers: []string{"alice", "departed"},
			prune:       true,
			wantRemoves: []string{"departed"},
			wantSkips:   []Skip{{User: "departed", Org: "acme", Reason: SkipNotOrgMember}},
		},
		{
			name:        "reconciled state is a no-op",
			roster:      []string{"alice", "bob"},
			orgMembers:  []string{"alice", "bob"},
			teamMembers: []string{"alice", "bob"},
			prune:       true,
		},
		{
			name:        "empty roster with prune empties the team",
			orgMembers:  []string{"alice"},
			teamMembers: []string{"alice", "bob"},
			prune:       true,
			wantRemoves: []string{"alice", "bob"},
		},
		{
			name:   "empty org skips every roster login",
			roster: []string{"alice", "bob"},
			wantSkips: []Skip{
				{User: "alice", Org: "acme", Reason: SkipNotOrgMember},
				{User: "bob", Org: "acme", Reason: SkipNotOrgMember},
			},
		},
		{
			name: "everything empty",
		},
		{
			name:       "outputs sorted regardless of input order",
			roster:     []string{"zoe", "bob", "alice"},
			orgMembers: []string{"zoe", "bob", "alice"},
			wantAdds:   []string{"alice", "bob", "zoe"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adds, removes, skips := computeDiff("acme",
				stringset.New(tt.roster...),
				stringset.New(tt.orgMembers...),
				stringset.New(tt.teamMembers...),
				tt.prune)
			if !slices.Equal(adds, tt.wantAdds) {
				t.Errorf("computeDiff() adds = %v, want %v", adds, tt.wantAdds)
			}
			if !slices.Equal(removes, tt.wantRemoves) {
				t.Errorf("computeDiff() removes = %v, want %v", removes, tt.wantRemoves)
			}
			if !slices.Equal(skips, tt.wantSkips) {
				t.Errorf("computeDiff() skips = %v, want %v", skips, tt.wantSkips)
			}
		})
	}
}

func TestResultTotalsAndHasErrors(t *testing.T) {
	clean := &Result{
		Orgs: []OrgResult{
			{Org: "acme", Added: []string{"alice", "bob"}, Removed: []string{"eve"}},
			{Org: "beta", Skips: []Skip{{Org: "beta", Reason: SkipNoInstallation}}},
		},
		UnknownUsers: []string{"type0-ghost"},
	}
	want := Totals{Added: 2, Removed: 1, Skipped: 2}
	if got := clean.Totals(); got != want {
		t.Errorf("Totals() = %+v, want %+v", got, want)
	}
	if clean.HasErrors() {
		t.Error("HasErrors() = true, want false for skips-only result")
	}

	failed := &Result{
		Orgs:        []OrgResult{{Org: "acme", Err: errors.New("boom")}},
		ResidueErrs: []error{errors.New("lookup failed")},
	}
	if got := failed.Totals().Errors; got != 2 {
		t.Errorf("Totals().Errors = %d, want 2", got)
	}
	if !failed.HasErrors() {
		t.Error("HasErrors() = false, want true")
	}
}

func TestReconcileOrgDryRunWritesNothing(t *testing.T) {
	srv := ghtest.New(t)
	srv.AddOrg("acme", 7, "alice", "bob", "carol")
	opts := Options{
		Source: testApp(t, srv),
		Team:   testTeam,
		Roster: stringset.New("alice", "bob", "outsider"),
		DryRun: true,
	}

	out := reconcileOrg(context.Background(), &opts, "acme")

	if out.res.Err != nil {
		t.Fatalf("reconcileOrg() err = %v, want nil", out.res.Err)
	}
	if want := []string{"alice", "bob"}; !slices.Equal(out.res.Added, want) {
		t.Errorf("Added = %v, want %v", out.res.Added, want)
	}
	wantSkips := []Skip{{User: "outsider", Org: "acme", Reason: SkipNotOrgMember}}
	if !slices.Equal(out.res.Skips, wantSkips) {
		t.Errorf("Skips = %v, want %v", out.res.Skips, wantSkips)
	}
	if want := []string{"alice", "bob"}; !slices.Equal(out.seen.Sorted(), want) {
		t.Errorf("seen = %v, want %v", out.seen.Sorted(), want)
	}
	if puts := srv.MembershipPuts(); len(puts) != 0 {
		t.Errorf("MembershipPuts() = %v, want none in dry-run", puts)
	}
	if _, ok := srv.Team("acme", testTeam.Slug); ok {
		t.Error("dry-run created the team, want no writes")
	}
}

func TestReconcileOrgApplyCreatesTeamAndAdds(t *testing.T) {
	srv := ghtest.New(t)
	srv.AddOrg("acme", 7, "alice", "bob", "carol")
	opts := Options{
		Source: testApp(t, srv),
		Team:   testTeam,
		Roster: stringset.New("alice", "bob"),
	}

	out := reconcileOrg(context.Background(), &opts, "acme")

	if out.res.Err != nil {
		t.Fatalf("reconcileOrg() err = %v, want nil", out.res.Err)
	}
	if want := []string{"alice", "bob"}; !slices.Equal(out.res.Added, want) {
		t.Errorf("Added = %v, want %v", out.res.Added, want)
	}
	team, ok := srv.Team("acme", testTeam.Slug)
	if !ok {
		t.Fatal("apply did not create the team")
	}
	if team.Privacy != testTeam.Privacy {
		t.Errorf("created team = %+v, want settings %+v", team, testTeam)
	}
	got := srv.TeamMembers("acme", testTeam.Slug)
	slices.Sort(got)
	if want := []string{"alice", "bob"}; !slices.Equal(got, want) {
		t.Errorf("team members = %v, want %v", got, want)
	}
}

func TestReconcileOrgApplyPrunesExtras(t *testing.T) {
	srv := ghtest.New(t)
	srv.AddOrg("acme", 7, "alice", "bob")
	srv.AddTeam("acme", ghtest.Team{Slug: testTeam.Slug}, "alice", "departed")
	opts := Options{
		Source: testApp(t, srv),
		Team:   testTeam,
		Roster: stringset.New("alice", "bob"),
		Prune:  true,
	}

	out := reconcileOrg(context.Background(), &opts, "acme")

	if out.res.Err != nil {
		t.Fatalf("reconcileOrg() err = %v, want nil", out.res.Err)
	}
	if want := []string{"bob"}; !slices.Equal(out.res.Added, want) {
		t.Errorf("Added = %v, want %v", out.res.Added, want)
	}
	if want := []string{"departed"}; !slices.Equal(out.res.Removed, want) {
		t.Errorf("Removed = %v, want %v", out.res.Removed, want)
	}
	got := srv.TeamMembers("acme", testTeam.Slug)
	slices.Sort(got)
	if want := []string{"alice", "bob"}; !slices.Equal(got, want) {
		t.Errorf("team members after prune = %v, want %v", got, want)
	}
}

func TestReconcileOrgNoInstallationIsSkipNotError(t *testing.T) {
	srv := ghtest.New(t)
	opts := Options{
		Source: testApp(t, srv),
		Team:   testTeam,
		Roster: stringset.New("alice"),
	}

	out := reconcileOrg(context.Background(), &opts, "ghost")

	if out.res.Err != nil {
		t.Fatalf("reconcileOrg() err = %v, want nil for missing installation", out.res.Err)
	}
	wantSkips := []Skip{{Org: "ghost", Reason: SkipNoInstallation}}
	if !slices.Equal(out.res.Skips, wantSkips) {
		t.Errorf("Skips = %v, want %v", out.res.Skips, wantSkips)
	}
	if out.client != nil {
		t.Error("client = non-nil, want nil when installation is missing")
	}
}

func TestReconcileOrgStopsOnFirstWriteError(t *testing.T) {
	srv := ghtest.New(t)
	srv.AddOrg("acme", 7, "alice", "bob", "carol")
	srv.FailMembershipPut("bob")
	opts := Options{
		Source: testApp(t, srv),
		Team:   testTeam,
		Roster: stringset.New("alice", "bob", "carol"),
	}

	out := reconcileOrg(context.Background(), &opts, "acme")

	if out.res.Err == nil {
		t.Fatal("reconcileOrg() err = nil, want the failed PUT surfaced")
	}
	if want := []string{"alice"}; !slices.Equal(out.res.Added, want) {
		t.Errorf("Added = %v, want partial progress %v", out.res.Added, want)
	}
	for _, put := range srv.MembershipPuts() {
		if put == "acme/"+testTeam.Slug+"/carol" {
			t.Error("PUT issued for carol after bob failed, want the org stopped")
		}
	}
}
