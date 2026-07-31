// Package reconcile is the champs engine: it computes and applies
// desired = roster ∩ org_members for the security-champions team in every
// configured organization (DESIGN-0001 steps 1–8), under the invariant
// that the tool never expands organization access.
//
// Per-org failures are data on the [Result], never a reason to abort the
// other orgs; the only fatal errors are the pre-flight guards.
package reconcile
