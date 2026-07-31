package reconcile

// SkipReason classifies why a roster login was not reconciled somewhere.
// Values are the DESIGN-0001 reason codes, emitted verbatim in slog
// records.
type SkipReason string

const (
	// SkipNotOrgMember marks a roster login that is not a member of the
	// org — including pending invitees, who are absent from the member
	// list. Adding them would send an org invitation, the one forbidden
	// behavior.
	SkipNotOrgMember SkipReason = "not_org_member"
	// SkipNoInstallation marks a whole org the champs app is not
	// installed in. The Skip's User field is empty.
	SkipNoInstallation SkipReason = "no_installation"
	// SkipUnknownUser marks a roster login that resolves to no GitHub
	// user at all (cross-org residue check). The Skip's Org field is
	// empty; these live on [Result.UnknownUsers], not on any OrgResult.
	SkipUnknownUser SkipReason = "unknown_user"
)

// Skip is one structured (user, org, reason) skip record. Exactly one of
// User or Org may be empty depending on Reason — see the [SkipReason]
// constants.
type Skip struct {
	User   string
	Org    string
	Reason SkipReason
}

// OrgResult is one org's reconcile outcome. In dry-run, Added and Removed
// are the planned writes; in apply they are the writes that actually
// succeeded, so a partially failed org carries its partial progress
// alongside Err.
type OrgResult struct {
	Org string
	// Added is sorted ascending. In apply mode these are membership PUTs
	// that returned state "active".
	Added []string
	// Removed is sorted ascending; always empty unless pruning.
	Removed []string
	// Skips is sorted by user and holds only not_org_member and
	// no_installation records — unknown_user is run-level on [Result].
	Skips []Skip
	// Err is this org's failure: client resolution, a list/ensure call,
	// or the first failed write — including *gh.GuardBreachError, which
	// callers can errors.As out of here. Never gh.ErrNoInstallation;
	// that becomes a Skip instead.
	Err error
}

// Result is a full run's outcome — everything the renderer needs.
type Result struct {
	DryRun bool
	Prune  bool
	// Orgs is sorted by org name, deterministic regardless of goroutine
	// completion order.
	Orgs []OrgResult
	// UnknownUsers holds roster logins (sorted) that appeared in zero
	// org member lists and failed the user-existence residue check.
	// Their per-org not_org_member skips have been reclassified away.
	UnknownUsers []string
	// ResidueErrs records residue-check lookups that errored (API
	// failure, not "user missing"); affected logins keep their
	// not_org_member skips. Counts toward HasErrors.
	ResidueErrs []error
}

// Totals are the end-of-run summary counts.
type Totals struct {
	Added   int
	Removed int
	Skipped int
	Errors  int
}

// Totals derives run totals: Skipped counts org-level skips plus unknown
// users; Errors counts org errors plus residue-check failures.
func (r *Result) Totals() Totals {
	t := Totals{
		Skipped: len(r.UnknownUsers),
		Errors:  len(r.ResidueErrs),
	}
	for _, o := range r.Orgs {
		t.Added += len(o.Added)
		t.Removed += len(o.Removed)
		t.Skipped += len(o.Skips)
		if o.Err != nil {
			t.Errors++
		}
	}
	return t
}

// HasErrors reports whether anything failed — the CLI's exit-code-1
// signal. Skips alone are not errors.
func (r *Result) HasErrors() bool {
	if len(r.ResidueErrs) > 0 {
		return true
	}
	for _, o := range r.Orgs {
		if o.Err != nil {
			return true
		}
	}
	return false
}
