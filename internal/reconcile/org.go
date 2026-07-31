package reconcile

import (
	"github.com/donaldgifford/champs/internal/stringset"
)

// computeDiff is the pure set math of DESIGN-0001 steps 4–6:
// desired = roster ∩ orgMembers, adds = desired − teamMembers,
// removes = teamMembers − desired (only when pruning), and one
// not_org_member skip per roster login absent from the org. All outputs
// are sorted ascending.
func computeDiff(org string, roster, orgMembers, teamMembers stringset.Set, prune bool) (adds, removes []string, skips []Skip) {
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
