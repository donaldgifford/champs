// Package reconcile is the champs engine: it computes and applies
// desired = roster ∩ org_members for the security-champions team in every
// configured organization (DESIGN-0001 steps 1–8), under the invariant
// that the tool never expands organization access.
//
// Per-org failures are data on the [Result], never a reason to abort the
// other orgs; the only fatal errors are the pre-flight guards.
package reconcile

import (
	"context"
	"errors"

	"github.com/donaldgifford/champs/internal/gh"
	"github.com/donaldgifford/champs/internal/stringset"
)

// DefaultParallelism bounds concurrent org reconciles when
// [Options.Parallelism] is unset (DESIGN-0001 OQ-5).
const DefaultParallelism = 5

// ErrEmptyRosterPrune reports a roster that parsed to zero logins while
// prune is enabled. Failing here — before any org work — is the guard
// that stops a truncated CSV from emptying every team in every org.
var ErrEmptyRosterPrune = errors.New(
	"empty roster with prune enabled: refusing to empty all teams")

// ClientSource resolves the per-org installation client. *gh.App
// satisfies it directly; tests satisfy it with a real gh.App pointed at a
// ghtest server. The method returns the concrete *gh.OrgClient
// deliberately: Go has no covariant returns, so an interface-returning
// factory would force adapter closures on every caller for zero test
// benefit — the guard regression test requires the real gh write path
// anyway.
type ClientSource interface {
	OrgClient(ctx context.Context, org string) (*gh.OrgClient, error)
}

var _ ClientSource = (*gh.App)(nil)

// Options configures one reconcile run. The CLI validates --orgs against
// config and normalizes the roster before building this.
type Options struct {
	Source ClientSource
	// Team is the creation-time team settings (config.Team mapped on).
	Team gh.TeamSettings
	// Orgs are the org names to reconcile, already filtered/validated.
	Orgs []string
	// Roster is the normalized lowercase login set.
	Roster stringset.Set
	Prune  bool
	// DryRun computes everything and writes nothing — no team creation,
	// no membership PUT/DELETE. Reads still happen, including the
	// residue check.
	DryRun bool
	// Parallelism bounds concurrent orgs; values <= 0 mean
	// DefaultParallelism.
	Parallelism int
}
