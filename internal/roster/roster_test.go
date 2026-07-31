package roster_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/donaldgifford/champs/internal/roster"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"plain list", "alice\nbob\ncarol\n", []string{"alice", "bob", "carol"}},
		{"login header skipped", "login\nalice\nbob\n", []string{"alice", "bob"}},
		{"username header skipped", "Username\nalice\n", []string{"alice"}},
		{"mixed casing dedupes", "Alice\naLICE\nbob\n", []string{"alice", "bob"}},
		{"whitespace trimmed", "  alice  \n\tbob\n", []string{"alice", "bob"}},
		{"empty lines dropped", "\n\nalice\n\n\nbob\n", []string{"alice", "bob"}},
		{"header after blank lines", "\n\nlogin\nalice\n", []string{"alice"}},
		{"header only", "login\n", nil},
		{"empty input", "", nil},
		{"no trailing newline", "alice\nbob", []string{"alice", "bob"}},
		{"hyphenated login", "octo-cat\n", []string{"octo-cat"}},
		{"digits allowed", "type0\nuser42\n", []string{"type0", "user42"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := roster.Parse(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("Parse(%q) error = %v, want nil", tt.input, err)
			}
			if !slices.Equal(got.Sorted(), tt.want) {
				t.Errorf("Parse(%q) = %v, want %v", tt.input, got.Sorted(), tt.want)
			}
		})
	}
}

// TestParseMessyFixturesNormalize pins the Phase 1 success criterion: messy
// exports (casing, whitespace, duplicates, with/without header) all
// normalize to the same set.
func TestParseMessyFixturesNormalize(t *testing.T) {
	fixtures := []string{
		"alice\nbob\n",
		"login\nAlice\nBOB\n",
		"  alice \n\nbob\nalice\n",
		"username\n\tALICE\nbob\nBob\n",
	}
	want := []string{"alice", "bob"}

	for i, fixture := range fixtures {
		got, err := roster.Parse(strings.NewReader(fixture))
		if err != nil {
			t.Fatalf("fixture %d: Parse(%q) error = %v, want nil", i, fixture, err)
		}
		if !slices.Equal(got.Sorted(), want) {
			t.Errorf("fixture %d: Parse(%q) = %v, want %v", i, fixture, got.Sorted(), want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantIs   error    // nil means any error
		wantMsgs []string // substrings that must appear in the error
	}{
		{"multiple columns", "alice,admin\n", nil, []string{"line 1", "multiple columns"}},
		{"leading hyphen", "-alice\n", roster.ErrInvalidLogin, []string{`line 1`, `"-alice"`}},
		{"trailing hyphen", "alice-\n", roster.ErrInvalidLogin, nil},
		{"consecutive hyphens", "a--b\n", roster.ErrInvalidLogin, nil},
		{"illegal character", "al_ice\n", roster.ErrInvalidLogin, nil},
		{"too long", strings.Repeat("a", 40) + "\n", roster.ErrInvalidLogin, nil},
		{
			"aggregates all bad lines", "-a\nok\nb--c\n",
			roster.ErrInvalidLogin, []string{"line 1", "line 3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := roster.Parse(strings.NewReader(tt.input))
			if err == nil {
				t.Fatalf("Parse(%q) error = nil, want error", tt.input)
			}
			if got != nil {
				t.Errorf("Parse(%q) set = %v, want nil on error", tt.input, got.Sorted())
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("Parse(%q) error = %v, want errors.Is %v", tt.input, err, tt.wantIs)
			}
			for _, want := range tt.wantMsgs {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Parse(%q) error = %q, want it to contain %q", tt.input, err, want)
				}
			}
		})
	}
}

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "champions.csv")
	if err := os.WriteFile(path, []byte("login\nAlice\nbob\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := roster.Load(path)
	if err != nil {
		t.Fatalf("Load(%q) error = %v, want nil", path, err)
	}
	want := []string{"alice", "bob"}
	if !slices.Equal(got.Sorted(), want) {
		t.Errorf("Load(%q) = %v, want %v", path, got.Sorted(), want)
	}
}

func TestLoadMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.csv")

	_, err := roster.Load(path)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load(%q) error = %v, want errors.Is os.ErrNotExist", path, err)
	}
	if !strings.Contains(err.Error(), "reading roster") {
		t.Errorf("Load(%q) error = %q, want it to mention reading roster", path, err)
	}
}

func TestLoadParseErrorNamesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.csv")
	if err := os.WriteFile(path, []byte("-nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := roster.Load(path)
	if !errors.Is(err, roster.ErrInvalidLogin) {
		t.Fatalf("Load(%q) error = %v, want errors.Is ErrInvalidLogin", path, err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("Load(%q) error = %q, want it to name the file", path, err)
	}
}
