// Package roster parses the champion roster: a single-column CSV of GitHub
// logins, one per line, header optional.
//
// Logins are normalized to lowercase (GitHub logins are case-insensitive but
// case-preserving), surrounding whitespace is trimmed, empty lines are
// dropped, and duplicates collapse into the returned set. An empty result is
// not an error; the caller decides what an empty roster means.
package roster

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/donaldgifford/champs/internal/stringset"
)

// ErrInvalidLogin marks a roster line that cannot be a GitHub login. Line
// problems are aggregated with errors.Join so one run reports them all.
var ErrInvalidLogin = errors.New("invalid github login")

// Load reads and parses the roster file at path.
func Load(path string) (stringset.Set, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading roster: %w", err)
	}
	set, err := Parse(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("roster %s: %w", path, err)
	}
	return set, nil
}

// Parse reads a single-column roster from r.
//
// Header rule: the first non-empty line is skipped if and only if it
// normalizes to exactly "login" or "username". A roster whose first real
// entry is a user actually named "login" would need a header line; that
// collision is accepted as unrealistic.
func Parse(r io.Reader) (stringset.Set, error) {
	set := make(stringset.Set)
	var errs []error
	first := true

	sc := bufio.NewScanner(r)
	for line := 1; sc.Scan(); line++ {
		v := strings.ToLower(strings.TrimSpace(sc.Text()))
		if v == "" {
			continue
		}
		if first {
			first = false
			if v == "login" || v == "username" {
				continue
			}
		}
		switch {
		case strings.Contains(v, ","):
			errs = append(errs, fmt.Errorf(
				"line %d: multiple columns, expected single-column roster", line))
		case !validLogin(v):
			errs = append(errs, fmt.Errorf("line %d: %q: %w", line, v, ErrInvalidLogin))
		default:
			set.Add(v)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading roster: %w", err)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return set, nil
}

// validLogin reports whether v (already lowercased) matches GitHub's login
// shape: 1-39 alphanumerics and hyphens, with no leading, trailing, or
// consecutive hyphens.
func validLogin(v string) bool {
	if v == "" || len(v) > 39 {
		return false
	}
	if v[0] == '-' || v[len(v)-1] == '-' {
		return false
	}
	prevHyphen := false
	for i := range len(v) {
		switch c := v[i]; {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			prevHyphen = false
		case c == '-':
			if prevHyphen {
				return false
			}
			prevHyphen = true
		default:
			return false
		}
	}
	return true
}
