package config

import (
	"errors"
	"io"

	"github.com/hashicorp/hcl/v2"
)

// Semantic validation failures, matchable with errors.Is. Load aggregates
// them with errors.Join so one run reports every problem.
var (
	ErrNoOrgs         = errors.New("no orgs configured")
	ErrEmptyTeamSlug  = errors.New("team slug is empty")
	ErrMissingAppID   = errors.New("github app_id is not set")
	ErrNoKeySource    = errors.New("no private key source")
	ErrInvalidPrivacy = errors.New("invalid team privacy")
	ErrDuplicateOrg   = errors.New("duplicate org")
	ErrInvalidPEM     = errors.New("not valid PEM data")
)

// DiagnosticsError carries every HCL diagnostic from a parse so callers can
// render all problems at once instead of surfacing one per fix-parse cycle.
type DiagnosticsError struct {
	Diagnostics hcl.Diagnostics

	files map[string]*hcl.File
}

// Error returns a one-line summary; use Render for the full report.
func (e *DiagnosticsError) Error() string {
	return "config: " + e.Diagnostics.Error()
}

// Render writes the full diagnostic set to w with source snippets. width
// bounds line wrapping (0 means no wrap); color enables ANSI highlighting.
func (e *DiagnosticsError) Render(w io.Writer, width uint, color bool) error {
	return hcl.NewDiagnosticTextWriter(w, e.files, width, color).
		WriteDiagnostics(e.Diagnostics)
}
