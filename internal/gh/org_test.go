package gh_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/donaldgifford/champs/internal/gh"
	"github.com/donaldgifford/champs/internal/ghtest"
)

const teamSlug = "security_champions"

var testTeam = gh.TeamSettings{
	Slug:        teamSlug,
	Description: "Security champions",
	Privacy:     "closed",
}

// orgClient builds a client for the "acme" org (installation 7) with the
// given member logins registered.
func orgClient(t *testing.T, srv *ghtest.Server, members ...string) *gh.OrgClient {
	t.Helper()
	srv.AddOrg("acme", 7, members...)
	client, err := newApp(t, srv).OrgClient(context.Background(), "acme")
	if err != nil {
		t.Fatalf("OrgClient(acme) error = %v, want nil", err)
	}
	return client
}

func TestListOrgMembersPaginatesAndLowercases(t *testing.T) {
	srv := ghtest.New(t)
	// Five members across three GraphQL pages of two, mixed casing.
	client := orgClient(t, srv, "Alice", "BOB", "carol", "Dave", "eve")

	got, err := client.ListOrgMembers(context.Background())
	if err != nil {
		t.Fatalf("ListOrgMembers() error = %v, want nil", err)
	}
	want := []string{"alice", "bob", "carol", "dave", "eve"}
	if !slices.Equal(got.Sorted(), want) {
		t.Errorf("ListOrgMembers() = %v, want %v", got.Sorted(), want)
	}
	if hits := srv.Hits("/graphql"); hits != 3 {
		t.Errorf("Hits(/graphql) = %d, want 3 pages", hits)
	}
}

func TestEnsureTeamCreatesWithSettings(t *testing.T) {
	srv := ghtest.New(t)
	client := orgClient(t, srv, "alice")

	if err := client.EnsureTeam(context.Background(), testTeam); err != nil {
		t.Fatalf("EnsureTeam() error = %v, want nil", err)
	}

	team, ok := srv.Team("acme", teamSlug)
	if !ok {
		t.Fatal("EnsureTeam() did not create the team")
	}
	if team.Description != testTeam.Description || team.Privacy != testTeam.Privacy {
		t.Errorf("created team = %+v, want settings %+v", team, testTeam)
	}
}

func TestEnsureTeamLeavesExistingUntouched(t *testing.T) {
	srv := ghtest.New(t)
	existing := ghtest.Team{Slug: teamSlug, Description: "pre-existing", Privacy: "secret"}
	srv.AddTeam("acme", existing)
	client := orgClient(t, srv, "alice")

	if err := client.EnsureTeam(context.Background(), testTeam); err != nil {
		t.Fatalf("EnsureTeam() error = %v, want nil", err)
	}

	team, _ := srv.Team("acme", teamSlug)
	if team != existing {
		t.Errorf("existing team mutated to %+v, want untouched %+v", team, existing)
	}
}

func TestListTeamMembersPaginatesAndLowercases(t *testing.T) {
	srv := ghtest.New(t)
	srv.AddTeam("acme", ghtest.Team{Slug: teamSlug}, "Alice", "BOB", "carol", "dave", "Eve")
	client := orgClient(t, srv, "alice")

	got, err := client.ListTeamMembers(context.Background(), teamSlug)
	if err != nil {
		t.Fatalf("ListTeamMembers() error = %v, want nil", err)
	}
	want := []string{"alice", "bob", "carol", "dave", "eve"}
	if !slices.Equal(got.Sorted(), want) {
		t.Errorf("ListTeamMembers() = %v, want %v", got.Sorted(), want)
	}
	if hits := srv.Hits("/orgs/acme/teams/" + teamSlug + "/members"); hits != 3 {
		t.Errorf("Hits(team members) = %d, want 3 pages", hits)
	}
}

func TestAddTeamMemberActive(t *testing.T) {
	srv := ghtest.New(t)
	srv.AddTeam("acme", ghtest.Team{Slug: teamSlug})
	client := orgClient(t, srv, "alice")

	if err := client.AddTeamMember(context.Background(), teamSlug, "alice"); err != nil {
		t.Fatalf("AddTeamMember(alice) error = %v, want nil", err)
	}
	if got := srv.TeamMembers("acme", teamSlug); !slices.Contains(got, "alice") {
		t.Errorf("team members = %v, want alice added", got)
	}
	if got := srv.MembershipPuts(); !slices.Contains(got, "acme/"+teamSlug+"/alice") {
		t.Errorf("MembershipPuts() = %v, want the PUT recorded", got)
	}
}

func TestAddTeamMemberGuardBreachCancelsInvitation(t *testing.T) {
	srv := ghtest.New(t)
	srv.AddTeam("acme", ghtest.Team{Slug: teamSlug})
	client := orgClient(t, srv, "alice")

	err := client.AddTeamMember(context.Background(), teamSlug, "mallory")

	var breach *gh.GuardBreachError
	if !errors.As(err, &breach) {
		t.Fatalf("AddTeamMember(non-member) error = %v, want *GuardBreachError", err)
	}
	if breach.Org != "acme" || breach.User != "mallory" {
		t.Errorf("GuardBreachError = %+v, want Org=acme User=mallory", breach)
	}
	if breach.CancelErr != nil {
		t.Errorf("CancelErr = %v, want nil (invitation cancelled)", breach.CancelErr)
	}
	if got := len(srv.CancelledInvites()); got != 1 {
		t.Errorf("CancelledInvites() count = %d, want 1", got)
	}
	if got := srv.PendingInvites("acme"); len(got) != 0 {
		t.Errorf("PendingInvites() = %v, want none after cancellation", got)
	}
}

func TestAddTeamMemberGuardBreachCancelFailure(t *testing.T) {
	srv := ghtest.New(t)
	srv.AddTeam("acme", ghtest.Team{Slug: teamSlug})
	srv.DisableInviteCreation()
	client := orgClient(t, srv, "alice")

	err := client.AddTeamMember(context.Background(), teamSlug, "mallory")

	var breach *gh.GuardBreachError
	if !errors.As(err, &breach) {
		t.Fatalf("AddTeamMember(non-member) error = %v, want *GuardBreachError", err)
	}
	if breach.CancelErr == nil {
		t.Error("CancelErr = nil, want the missing-invitation failure recorded")
	}
}

func TestRemoveTeamMember(t *testing.T) {
	srv := ghtest.New(t)
	srv.AddTeam("acme", ghtest.Team{Slug: teamSlug}, "alice", "bob")
	client := orgClient(t, srv, "alice", "bob")

	if err := client.RemoveTeamMember(context.Background(), teamSlug, "bob"); err != nil {
		t.Fatalf("RemoveTeamMember(bob) error = %v, want nil", err)
	}
	want := []string{"alice"}
	if got := srv.TeamMembers("acme", teamSlug); !slices.Equal(got, want) {
		t.Errorf("team members = %v, want %v", got, want)
	}
}

func TestUserExists(t *testing.T) {
	srv := ghtest.New(t)
	srv.AddUser("real-user")
	client := orgClient(t, srv, "alice")

	tests := []struct {
		login string
		want  bool
	}{
		{"real-user", true},
		{"alice", true}, // org members are known users
		{"type0-ghost", false},
	}
	for _, tt := range tests {
		got, err := client.UserExists(context.Background(), tt.login)
		if err != nil {
			t.Fatalf("UserExists(%q) error = %v, want nil", tt.login, err)
		}
		if got != tt.want {
			t.Errorf("UserExists(%q) = %t, want %t", tt.login, got, tt.want)
		}
	}
}
