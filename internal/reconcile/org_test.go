package reconcile

import (
	"errors"
	"slices"
	"testing"

	"github.com/donaldgifford/champs/internal/stringset"
)

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
