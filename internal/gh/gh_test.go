package gh_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/donaldgifford/champs/internal/gh"
	"github.com/donaldgifford/champs/internal/ghtest"
)

func newApp(t *testing.T, srv *ghtest.Server) *gh.App {
	t.Helper()
	app, err := gh.NewApp(1, ghtest.AppKey(t), gh.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("NewApp() error = %v, want nil", err)
	}
	return app
}

func TestNewAppRejectsBadKey(t *testing.T) {
	_, err := gh.NewApp(1, []byte("not a key"))
	if err == nil {
		t.Fatal("NewApp(bad key) error = nil, want error")
	}
}

func TestOrgClientNoInstallation(t *testing.T) {
	srv := ghtest.New(t)
	app := newApp(t, srv)

	_, err := app.OrgClient(context.Background(), "ghost")
	if !errors.Is(err, gh.ErrNoInstallation) {
		t.Fatalf("OrgClient(ghost) error = %v, want errors.Is ErrNoInstallation", err)
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("OrgClient(ghost) error = %q, want it to name the org", err)
	}
}

func TestOrgClientMintsInstallationToken(t *testing.T) {
	srv := ghtest.New(t)
	srv.AddOrg("acme", 7, "alice")
	app := newApp(t, srv)

	client, err := app.OrgClient(context.Background(), "acme")
	if err != nil {
		t.Fatalf("OrgClient(acme) error = %v, want nil", err)
	}
	if got := client.Org(); got != "acme" {
		t.Errorf("Org() = %q, want %q", got, "acme")
	}

	// Any installation-authenticated call forces a token mint.
	if _, err := client.ListOrgMembers(context.Background()); err != nil {
		t.Fatalf("ListOrgMembers() error = %v, want nil", err)
	}
	if got := srv.TokenMints(); got < 1 {
		t.Errorf("TokenMints() = %d, want >= 1", got)
	}
}

func TestSecondaryRateLimitRetried(t *testing.T) {
	srv := ghtest.New(t)
	srv.AddOrg("acme", 7, "alice")
	srv.SecondaryLimitOnce("/orgs/acme/installation")
	app := newApp(t, srv)

	if _, err := app.OrgClient(context.Background(), "acme"); err != nil {
		t.Fatalf("OrgClient(acme) after rate limit error = %v, want retried success", err)
	}
	if got := srv.Hits("/orgs/acme/installation"); got != 2 {
		t.Errorf("Hits(installation) = %d, want 2 (limited once, then retried)", got)
	}
}
