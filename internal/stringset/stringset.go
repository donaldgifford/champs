// Package stringset provides the string set used for roster membership and
// the reconcile set arithmetic (intersection and difference of login sets).
package stringset

import "slices"

// Set is a set of strings. It is a map type, so range, len, and index
// operations work directly; a nil Set is valid for reads but not writes.
type Set map[string]struct{}

// New returns a Set containing items.
func New(items ...string) Set {
	s := make(Set, len(items))
	for _, v := range items {
		s.Add(v)
	}
	return s
}

// Add inserts v into the set.
func (s Set) Add(v string) {
	s[v] = struct{}{}
}

// Contains reports whether v is in the set.
func (s Set) Contains(v string) bool {
	_, ok := s[v]
	return ok
}

// Len returns the number of elements.
func (s Set) Len() int {
	return len(s)
}

// Intersect returns a new Set with the elements present in both s and other.
func (s Set) Intersect(other Set) Set {
	small, large := s, other
	if len(large) < len(small) {
		small, large = large, small
	}
	out := make(Set)
	for v := range small {
		if large.Contains(v) {
			out.Add(v)
		}
	}
	return out
}

// Diff returns a new Set with the elements of s that are not in other.
func (s Set) Diff(other Set) Set {
	out := make(Set)
	for v := range s {
		if !other.Contains(v) {
			out.Add(v)
		}
	}
	return out
}

// Sorted returns the elements in ascending order, for deterministic output.
func (s Set) Sorted() []string {
	out := make([]string, 0, len(s))
	for v := range s {
		out = append(out, v)
	}
	slices.Sort(out)
	return out
}
