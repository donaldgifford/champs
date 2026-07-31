package gh

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/google/go-github/v89/github"
)

// ErrNoInstallation reports that the champs GitHub App is not installed in
// an organization. Matched with errors.Is and mapped to the no_installation
// skip category — never a crash.
var ErrNoInstallation = errors.New("github app not installed for org")

// GuardBreachError reports the membership-guard backstop firing: a team
// membership PUT returned state "pending", meaning GitHub just sent an org
// invitation — the one thing champs must never do. The invitation is
// cancelled immediately; CancelErr records that outcome.
type GuardBreachError struct {
	Org  string
	User string
	// CancelErr is nil when the invitation was found and cancelled.
	CancelErr error
}

func (e *GuardBreachError) Error() string {
	msg := fmt.Sprintf(
		"membership guard breach: adding %s to team in %s sent an org invitation",
		e.User, e.Org)
	if e.CancelErr != nil {
		return msg + "; cancelling it FAILED: " + e.CancelErr.Error()
	}
	return msg + "; invitation cancelled"
}

func (e *GuardBreachError) Unwrap() error { return e.CancelErr }

// is404 reports whether err is a GitHub API 404 response.
func is404(err error) bool {
	var ger *github.ErrorResponse
	return errors.As(err, &ger) && ger.Response != nil &&
		ger.Response.StatusCode == http.StatusNotFound
}
