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
	"slices"

	"golang.org/x/sync/errgroup"

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

// Run reconciles every org and returns the collected Result. The
// returned error is fatal-only — a guard trip before any org work.
// Per-org failures are data in Result: one org's error never aborts the
// others, and the Result is identical at any parallelism.
func Run(ctx context.Context, opts *Options) (*Result, error) {
	if opts.Prune && opts.Roster.Len() == 0 {
		return nil, ErrEmptyRosterPrune
	}

	par := opts.Parallelism
	if par <= 0 {
		par = DefaultParallelism
	}
	orgs := slices.Clone(opts.Orgs)
	slices.Sort(orgs)

	// Plain errgroup.Group, deliberately not WithContext: every worker
	// returns nil — per-org errors are data in its outcome — so a
	// derived context would never fire, while inviting a refactor where
	// one org's error silently cancels its siblings. Each goroutine
	// writes only its own outcomes index, so collection needs no mutex
	// and inherits the sorted org order.
	outcomes := make([]orgOutcome, len(orgs))
	var g errgroup.Group
	g.SetLimit(par)
	for i, org := range orgs {
		g.Go(func() error {
			outcomes[i] = reconcileOrg(ctx, opts, org)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err // unreachable: workers always return nil
	}

	res := &Result{
		DryRun: opts.DryRun,
		Prune:  opts.Prune,
		Orgs:   make([]OrgResult, 0, len(orgs)),
	}
	for _, o := range outcomes {
		res.Orgs = append(res.Orgs, o.res)
	}
	return res, nil
}
