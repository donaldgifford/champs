package stringset_test

import (
	"slices"
	"testing"

	"github.com/donaldgifford/champs/internal/stringset"
)

func TestNew(t *testing.T) {
	s := stringset.New("a", "b", "a")

	if got := s.Len(); got != 2 {
		t.Errorf(`New("a", "b", "a").Len() = %d, want 2`, got)
	}
	if !s.Contains("a") || !s.Contains("b") {
		t.Errorf(`New("a", "b", "a") = %v, want it to contain "a" and "b"`, s.Sorted())
	}
	if s.Contains("c") {
		t.Error(`New("a", "b", "a").Contains("c") = true, want false`)
	}
}

func TestNilSetReads(t *testing.T) {
	var s stringset.Set

	if s.Contains("a") {
		t.Error(`nil Set: Contains("a") = true, want false`)
	}
	if got := s.Len(); got != 0 {
		t.Errorf("nil Set: Len() = %d, want 0", got)
	}
	if got := s.Sorted(); len(got) != 0 {
		t.Errorf("nil Set: Sorted() = %v, want empty", got)
	}
}

func TestIntersect(t *testing.T) {
	tests := []struct {
		name string
		a, b stringset.Set
		want []string
	}{
		{"overlap", stringset.New("a", "b", "c"), stringset.New("b", "c", "d"), []string{"b", "c"}},
		{"disjoint", stringset.New("a"), stringset.New("b"), nil},
		{"empty left", stringset.New(), stringset.New("a"), nil},
		{"empty right", stringset.New("a"), stringset.New(), nil},
		{"smaller right side", stringset.New("a", "b", "c", "x"), stringset.New("x"), []string{"x"}},
		{"identical", stringset.New("a", "b"), stringset.New("a", "b"), []string{"a", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Intersect(tt.b).Sorted(); !slices.Equal(got, tt.want) {
				t.Errorf("Intersect(%v, %v) = %v, want %v",
					tt.a.Sorted(), tt.b.Sorted(), got, tt.want)
			}
		})
	}
}

func TestDiff(t *testing.T) {
	tests := []struct {
		name string
		a, b stringset.Set
		want []string
	}{
		{"removes common", stringset.New("a", "b", "c"), stringset.New("b"), []string{"a", "c"}},
		{"disjoint keeps all", stringset.New("a", "b"), stringset.New("x"), []string{"a", "b"}},
		{"identical empties", stringset.New("a"), stringset.New("a"), nil},
		{"empty left", stringset.New(), stringset.New("a"), nil},
		{"empty right keeps all", stringset.New("a"), stringset.New(), []string{"a"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Diff(tt.b).Sorted(); !slices.Equal(got, tt.want) {
				t.Errorf("Diff(%v, %v) = %v, want %v",
					tt.a.Sorted(), tt.b.Sorted(), got, tt.want)
			}
		})
	}
}

func TestSortedIsOrdered(t *testing.T) {
	s := stringset.New("zeta", "alpha", "mike", "bravo")

	want := []string{"alpha", "bravo", "mike", "zeta"}
	if got := s.Sorted(); !slices.Equal(got, want) {
		t.Errorf("Sorted() = %v, want %v", got, want)
	}
}
