// Package gh is the GitHub layer for champs: GitHub App authentication,
// per-org installation clients, and the REST + GraphQL operations the
// reconcile engine consumes.
//
// Every request — installation token minting, REST, and GraphQL — flows
// through one shared secondary-rate-limit retry transport, and everything is
// pointable at a single base URL so tests run against one httptest server.
package gh

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/gofri/go-github-ratelimit/v2/github_ratelimit"
	"github.com/google/go-github/v89/github"
	"github.com/shurcooL/githubv4"
)

// Option configures NewApp.
type Option func(*App)

// WithBaseURL points every request at u instead of github.com: REST calls,
// the GraphQL endpoint (u + "/graphql"), and installation token minting.
// Tests point this at one httptest server.
func WithBaseURL(u string) Option {
	return func(a *App) { a.baseURL = strings.TrimSuffix(u, "/") }
}

// WithTransport replaces the base transport underneath the shared
// secondary-rate-limit retry layer.
func WithTransport(rt http.RoundTripper) Option {
	return func(a *App) { a.base = rt }
}

// App authenticates as the champs GitHub App and builds per-org installation
// clients. It is safe for concurrent use by the org fan-out.
type App struct {
	baseURL string
	base    http.RoundTripper

	appsTransport *ghinstallation.AppsTransport
	rest          *github.Client // app-JWT client; installation lookup only
}

// NewApp builds an App from the GitHub App ID and its private key PEM.
func NewApp(appID int64, privateKeyPEM []byte, opts ...Option) (*App, error) {
	a := &App{base: http.DefaultTransport}
	for _, opt := range opts {
		opt(a)
	}

	// One retry layer for everything (DESIGN-0001 OQ-6). Known limitation,
	// accepted for v0.1.0: the limiter does not rewind request bodies on
	// retry, so a retried body-carrying write can fail oddly — that
	// surfaces as an org error and the next idempotent run heals it.
	limiter := github_ratelimit.NewSecondaryLimiter(a.base)

	at, err := ghinstallation.NewAppsTransport(limiter, appID, privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("building app transport: %w", err)
	}
	if a.baseURL != "" {
		at.BaseURL = a.baseURL
	}
	a.appsTransport = at

	rest, err := a.restClient(at)
	if err != nil {
		return nil, fmt.Errorf("building app rest client: %w", err)
	}
	a.rest = rest

	return a, nil
}

// OrgClient resolves the app installation for org and returns a client whose
// requests carry that installation's token. A missing installation wraps
// ErrNoInstallation; the reconcile engine maps it to the no_installation
// category instead of failing the run.
func (a *App) OrgClient(ctx context.Context, org string) (*OrgClient, error) {
	inst, _, err := a.rest.Apps.GetOrganizationInstallation(ctx, org)
	if err != nil {
		if IsNotFound(err) {
			err = ErrNoInstallation
		}
		return nil, fmt.Errorf("resolving installation for %s: %w", org, err)
	}

	// Shallow-copy the apps transport: ghinstallation's token refresh
	// writes back into it, which races when orgs fan out sharing one.
	cp := *a.appsTransport
	itr := ghinstallation.NewFromAppsTransport(&cp, inst.GetID())
	if a.baseURL != "" {
		itr.BaseURL = a.baseURL
	}

	rest, err := a.restClient(itr)
	if err != nil {
		return nil, fmt.Errorf("building client for %s: %w", org, err)
	}

	httpClient := &http.Client{Transport: itr}
	gql := githubv4.NewClient(httpClient)
	if a.baseURL != "" {
		gql = githubv4.NewEnterpriseClient(a.baseURL+"/graphql", httpClient)
	}

	return &OrgClient{org: org, rest: rest, gql: gql}, nil
}

// restClient builds a go-github client over rt, honoring the base URL
// override.
func (a *App) restClient(rt http.RoundTripper) (*github.Client, error) {
	opts := []github.ClientOptionsFunc{github.WithTransport(rt)}
	if a.baseURL != "" {
		opts = append(opts, github.WithURLs(&a.baseURL, &a.baseURL))
	}
	return github.NewClient(opts...)
}
