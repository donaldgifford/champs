package gh

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v89/github"
	"github.com/shurcooL/githubv4"

	"github.com/donaldgifford/champs/internal/stringset"
)

const (
	// roleMember is the only team role champs assigns.
	roleMember = "member"
	// listPageSize is the page size for REST and GraphQL list calls.
	listPageSize = 100
	// activeState is the membership state proving no invitation was sent.
	activeState = "active"
)

// TeamSettings are the creation-time settings for the security-champions
// team. The caller maps config onto this; gh does not import
// internal/config.
type TeamSettings struct {
	Slug        string
	Description string
	Privacy     string
}

// OrgClient executes champs API operations in one organization using that
// org's installation token. Methods are called sequentially by the org's
// fan-out goroutine.
type OrgClient struct {
	org  string
	rest *github.Client
	gql  *githubv4.Client
}

// Org returns the organization this client operates on.
func (c *OrgClient) Org() string { return c.org }

// ListOrgMembers returns every org member's login, lowercased, via GraphQL
// membersWithRole — logins only, cursor-paginated. Some managed orgs have
// thousands of members against a ~100-login roster, so the query pulls just
// logins and spends the GraphQL rate budget (DESIGN-0001 OQ-3).
func (c *OrgClient) ListOrgMembers(ctx context.Context) (stringset.Set, error) {
	var q struct {
		Organization struct {
			MembersWithRole struct {
				Nodes []struct {
					Login githubv4.String
				}
				PageInfo struct {
					EndCursor   githubv4.String
					HasNextPage githubv4.Boolean
				}
			} `graphql:"membersWithRole(first: $first, after: $cursor)"`
		} `graphql:"organization(login: $org)"`
	}
	vars := map[string]any{
		"org":    githubv4.String(c.org),
		"first":  githubv4.Int(listPageSize),
		"cursor": (*githubv4.String)(nil),
	}

	members := make(stringset.Set, listPageSize)
	for {
		if err := c.gql.Query(ctx, &q, vars); err != nil {
			return nil, fmt.Errorf("listing members of %s: %w", c.org, err)
		}
		page := q.Organization.MembersWithRole
		for _, n := range page.Nodes {
			members.Add(strings.ToLower(string(n.Login)))
		}
		if !page.PageInfo.HasNextPage {
			break
		}
		vars["cursor"] = new(page.PageInfo.EndCursor)
	}
	return members, nil
}

// EnsureTeam creates the team with settings when it does not exist. An
// existing team is left untouched: settings apply only at creation
// (DESIGN-0001).
func (c *OrgClient) EnsureTeam(ctx context.Context, team TeamSettings) error {
	_, _, err := c.rest.Teams.GetTeamBySlug(ctx, c.org, team.Slug)
	if err == nil {
		return nil
	}
	if !IsNotFound(err) {
		return fmt.Errorf("getting team %s in %s: %w", team.Slug, c.org, err)
	}

	// GitHub derives the slug from the name; for security_champions the
	// two are identical (DESIGN-0001 Configuration).
	newTeam := github.NewTeam{
		Name:        team.Slug,
		Description: new(team.Description),
		Privacy:     new(team.Privacy),
	}
	if _, _, err := c.rest.Teams.CreateTeam(ctx, c.org, newTeam); err != nil {
		return fmt.Errorf("creating team %s in %s: %w", team.Slug, c.org, err)
	}
	return nil
}

// ListTeamMembers returns the team's member logins, lowercased. Maintainers
// are included — prune does not special-case them (DESIGN-0001).
func (c *OrgClient) ListTeamMembers(ctx context.Context, slug string) (stringset.Set, error) {
	members := make(stringset.Set, listPageSize)
	opts := &github.TeamListTeamMembersOptions{
		ListOptions: github.ListOptions{PerPage: listPageSize},
	}
	for {
		users, resp, err := c.rest.Teams.ListTeamMembersBySlug(ctx, c.org, slug, opts)
		if err != nil {
			return nil, fmt.Errorf("listing team %s members in %s: %w", slug, c.org, err)
		}
		for _, u := range users {
			members.Add(strings.ToLower(u.GetLogin()))
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return members, nil
}

// AddTeamMember adds user to the team as a member. The caller must have
// verified org membership first — that intersection is the load-bearing
// guard (DESIGN-0001). As a backstop, the response state must be "active":
// "pending" means GitHub just sent an org invitation, so the invitation is
// cancelled and a *GuardBreachError returned.
func (c *OrgClient) AddTeamMember(ctx context.Context, slug, user string) error {
	m, _, err := c.rest.Teams.AddTeamMembershipBySlug(ctx, c.org, slug, user,
		&github.TeamAddTeamMembershipOptions{Role: roleMember})
	if err != nil {
		return fmt.Errorf("adding %s to team %s in %s: %w", user, slug, c.org, err)
	}
	if m.GetState() != activeState {
		return &GuardBreachError{
			Org:       c.org,
			User:      user,
			CancelErr: c.cancelInvitation(ctx, user),
		}
	}
	return nil
}

// RemoveTeamMember removes user from the team. Team membership only — org
// membership is never touched, so prune cannot revoke org access.
func (c *OrgClient) RemoveTeamMember(ctx context.Context, slug, user string) error {
	if _, err := c.rest.Teams.RemoveTeamMembershipBySlug(ctx, c.org, slug, user); err != nil {
		return fmt.Errorf("removing %s from team %s in %s: %w", user, slug, c.org, err)
	}
	return nil
}

// UserExists reports whether login resolves to a GitHub user. Used only for
// the cross-org residue check that classifies unknown_user skips.
func (c *OrgClient) UserExists(ctx context.Context, login string) (bool, error) {
	_, _, err := c.rest.Users.Get(ctx, login)
	switch {
	case err == nil:
		return true, nil
	case IsNotFound(err):
		return false, nil
	default:
		return false, fmt.Errorf("looking up user %s: %w", login, err)
	}
}

// cancelInvitation finds and cancels the pending org invitation for user.
// Cancelling the org invitation also drops the queued team membership.
func (c *OrgClient) cancelInvitation(ctx context.Context, user string) error {
	opts := &github.ListOptions{PerPage: listPageSize}
	for {
		invites, resp, err := c.rest.Organizations.ListPendingOrgInvitations(ctx, c.org, opts)
		if err != nil {
			return fmt.Errorf("listing pending invitations: %w", err)
		}
		for _, inv := range invites {
			if strings.EqualFold(inv.GetLogin(), user) {
				if _, err := c.rest.Organizations.CancelInvite(ctx, c.org, inv.GetID()); err != nil {
					return fmt.Errorf("cancelling invitation %d: %w", inv.GetID(), err)
				}
				return nil
			}
		}
		if resp.NextPage == 0 {
			return fmt.Errorf("pending invitation for %s not found", user)
		}
		opts.Page = resp.NextPage
	}
}
