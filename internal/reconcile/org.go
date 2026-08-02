package reconcile

import (
	"context"
	"errors"

	"github.com/donaldgifford/champs/internal/gh"
	"github.com/donaldgifford/champs/internal/stringset"
)

// orgOutcome is one org's reconcile plus what the run-level residue pass
// needs: the roster logins seen in this org and a working client.
type orgOutcome struct {
	res OrgResult
	// seen is roster ∩ org_members — nil when member listing never
	// succeeded, so those logins stay candidates for unknown_user.
	seen stringset.Set
	// client is non-nil iff installation resolution succeeded.
	client *gh.OrgClient
}

// reconcileOrg runs DESIGN-0001 steps 1–7 for one org, sequentially. All
// errors land in the returned outcome — a missing installation becomes a
// no_installation skip, and a write error ends this org immediately with
// partial progress preserved: write failures within an org are almost
// never per-user, and after a guard breach further writes are
// indefensible. The idempotent rerun picks up the remainder.
func reconcileOrg(ctx context.Context, opts *Options, org string) orgOutcome {
	out := orgOutcome{res: OrgResult{Org: org}}

	client, err := opts.Source.OrgClient(ctx, org)
	if err != nil {
		if errors.Is(err, gh.ErrNoInstallation) {
			out.res.Skips = []Skip{{Org: org, Reason: SkipNoInstallation}}
			return out
		}
		out.res.Err = err
		return out
	}
	out.client = client

	orgMembers, err := client.ListOrgMembers(ctx)
	if err != nil {
		out.res.Err = err
		return out
	}

	teamMembers, err := currentTeamMembers(ctx, client, opts)
	if err != nil {
		out.res.Err = err
		return out
	}

	adds, removes, skips := computeDiff(org, opts.Roster, orgMembers, teamMembers, opts.Prune)
	out.seen = opts.Roster.Intersect(orgMembers)
	out.res.Skips = skips

	if opts.DryRun {
		out.res.Added = adds
		out.res.Removed = removes
		return out
	}

	// gh's errors already name the user, team, and org — no rewrapping.
	for _, user := range adds {
		if err := client.AddTeamMember(ctx, opts.Team.Slug, user); err != nil {
			out.res.Err = err
			return out
		}
		out.res.Added = append(out.res.Added, user)
	}
	for _, user := range removes {
		if err := client.RemoveTeamMember(ctx, opts.Team.Slug, user); err != nil {
			out.res.Err = err
			return out
		}
		out.res.Removed = append(out.res.Removed, user)
	}
	return out
}

// currentTeamMembers reads current team membership. Apply ensures the
// team exists first; dry-run must not create it (EnsureTeam is a write),
// so a 404 maps to the empty set the apply would start from.
func currentTeamMembers(
	ctx context.Context,
	client *gh.OrgClient,
	opts *Options,
) (stringset.Set, error) {
	if opts.DryRun {
		members, err := client.ListTeamMembers(ctx, opts.Team.Slug)
		if gh.IsNotFound(err) {
			return stringset.Set{}, nil
		}
		return members, err
	}
	if err := client.EnsureTeam(ctx, opts.Team); err != nil {
		return nil, err
	}
	return client.ListTeamMembers(ctx, opts.Team.Slug)
}

// computeDiff is the pure set math of DESIGN-0001 steps 4–6:
// desired = roster ∩ orgMembers, adds = desired − teamMembers,
// removes = teamMembers − desired (only when pruning), and one
// not_org_member skip per roster login absent from the org. All outputs
// are sorted ascending.
func computeDiff(
	org string,
	roster, orgMembers, teamMembers stringset.Set,
	prune bool,
) (adds, removes []string, skips []Skip) {
	desired := roster.Intersect(orgMembers)
	adds = desired.Diff(teamMembers).Sorted()
	if prune {
		removes = teamMembers.Diff(desired).Sorted()
	}
	notMembers := roster.Diff(orgMembers).Sorted()
	if len(notMembers) > 0 {
		skips = make([]Skip, 0, len(notMembers))
		for _, user := range notMembers {
			skips = append(skips, Skip{User: user, Org: org, Reason: SkipNotOrgMember})
		}
	}
	return adds, removes, skips
}
