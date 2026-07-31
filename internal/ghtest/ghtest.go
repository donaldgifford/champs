// Package ghtest provides a fake GitHub API server for champs tests: REST
// with Link-header pagination, the GraphQL membersWithRole query,
// installation token minting, and GitHub-realistic team membership
// semantics — adding a non-member to a team returns state "pending" and
// creates an org invitation, exactly the behavior the membership guard
// exists to prevent.
package ghtest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Team records the settings a team was created with.
type Team struct {
	Slug        string
	Description string
	Privacy     string
}

// Invite is a pending org invitation.
type Invite struct {
	ID    int64
	Login string
}

// Server is a fake GitHub API backed by one httptest server. Configure
// state with the Add* helpers; inspect what happened with the recorder
// methods. All methods are safe for concurrent use.
type Server struct {
	// URL is the base URL for gh.WithBaseURL.
	URL string

	mu            sync.Mutex
	installations map[string]int64
	orgMembers    map[string][]string
	teams         map[string]map[string]Team
	teamMembers   map[string]map[string][]string
	users         map[string]bool
	invites       map[string][]Invite
	nextInviteID  int64
	inviteOnPend  bool

	pageSize  int
	limitOnce map[string]bool
	hits      map[string]int

	membershipPuts []string
	cancelled      []int64
	tokenMints     int
}

// New starts a fake GitHub server that stops with the test. Lists paginate
// with pages of two so any listing of three or more exercises pagination.
func New(t *testing.T) *Server {
	t.Helper()

	s := &Server{
		installations: make(map[string]int64),
		orgMembers:    make(map[string][]string),
		teams:         make(map[string]map[string]Team),
		teamMembers:   make(map[string]map[string][]string),
		users:         make(map[string]bool),
		invites:       make(map[string][]Invite),
		nextInviteID:  100,
		inviteOnPend:  true,
		pageSize:      2,
		limitOnce:     make(map[string]bool),
		hits:          make(map[string]int),
	}

	srv := httptest.NewServer(s.handler())
	t.Cleanup(srv.Close)
	s.URL = srv.URL
	return s
}

// AddOrg registers an org with an app installation and its member logins.
func (s *Server) AddOrg(org string, installationID int64, members ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.installations[org] = installationID
	s.orgMembers[org] = append(s.orgMembers[org], members...)
	for _, m := range members {
		s.users[strings.ToLower(m)] = true
	}
}

// AddTeam registers an existing team and its current members.
func (s *Server) AddTeam(org string, team Team, members ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.teams[org] == nil {
		s.teams[org] = make(map[string]Team)
		s.teamMembers[org] = make(map[string][]string)
	}
	s.teams[org][team.Slug] = team
	s.teamMembers[org][team.Slug] = append(s.teamMembers[org][team.Slug], members...)
}

// AddUser registers logins that resolve on GET /users/{login}.
func (s *Server) AddUser(logins ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, l := range logins {
		s.users[strings.ToLower(l)] = true
	}
}

// DisableInviteCreation makes a pending membership PUT leave no invitation
// behind, simulating the cancel path failing to find one.
func (s *Server) DisableInviteCreation() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inviteOnPend = false
}

// SecondaryLimitOnce makes the next request to path fail with a GitHub
// secondary-rate-limit response (403 + Retry-After) exactly once.
func (s *Server) SecondaryLimitOnce(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.limitOnce[path] = true
}

// MembershipPuts returns every team membership PUT as "org/slug/user".
func (s *Server) MembershipPuts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.membershipPuts...)
}

// CancelledInvites returns the IDs of cancelled org invitations.
func (s *Server) CancelledInvites() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.cancelled...)
}

// PendingInvites returns the org's pending invitations.
func (s *Server) PendingInvites(org string) []Invite {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Invite(nil), s.invites[org]...)
}

// Team returns the team as currently stored.
func (s *Server) Team(org, slug string) (Team, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	team, ok := s.teams[org][slug]
	return team, ok
}

// TeamMembers returns the team's current member logins.
func (s *Server) TeamMembers(org, slug string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.teamMembers[org][slug]...)
}

// Hits returns how many requests reached path.
func (s *Server) Hits(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[path]
}

// TokenMints returns how many installation tokens were minted.
func (s *Server) TokenMints() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokenMints
}

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /orgs/{org}/installation", s.getInstallation)
	mux.HandleFunc("POST /app/installations/{id}/access_tokens", s.mintToken)
	mux.HandleFunc("POST /graphql", s.graphql)
	mux.HandleFunc("GET /orgs/{org}/teams/{slug}", s.getTeam)
	mux.HandleFunc("POST /orgs/{org}/teams", s.createTeam)
	mux.HandleFunc("GET /orgs/{org}/teams/{slug}/members", s.listTeamMembers)
	mux.HandleFunc("PUT /orgs/{org}/teams/{slug}/memberships/{user}", s.putMembership)
	mux.HandleFunc("DELETE /orgs/{org}/teams/{slug}/memberships/{user}", s.deleteMembership)
	mux.HandleFunc("GET /users/{login}", s.getUser)
	mux.HandleFunc("GET /orgs/{org}/invitations", s.listInvites)
	mux.HandleFunc("DELETE /orgs/{org}/invitations/{id}", s.cancelInvite)

	// Count hits and serve one secondary-rate-limit rejection when armed.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.hits[r.URL.Path]++
		limited := s.limitOnce[r.URL.Path]
		if limited {
			delete(s.limitOnce, r.URL.Path)
		}
		s.mu.Unlock()

		if limited {
			// Must be >= 1: the limiter treats Retry-After <= 0 as
			// "not a rate limit" and passes the 403 through.
			w.Header().Set("Retry-After", "1")
			writeJSON(w, http.StatusForbidden, map[string]string{
				"message":           "You have exceeded a secondary rate limit. Please wait.",
				"documentation_url": "https://docs.github.com/rest#about-secondary-rate-limits",
			})
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) getInstallation(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	id, ok := s.installations[r.PathValue("org")]
	s.mu.Unlock()
	if !ok {
		notFound(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (s *Server) mintToken(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.tokenMints++
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, map[string]any{
		"token":      "tok-" + r.PathValue("id"),
		"expires_at": time.Now().Add(time.Hour).Format(time.RFC3339),
	})
}

func (s *Server) graphql(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Variables struct {
			Org    string  `json:"org"`
			Cursor *string `json:"cursor"`
		} `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	members := append([]string(nil), s.orgMembers[req.Variables.Org]...)
	pageSize := s.pageSize
	s.mu.Unlock()

	start := 0
	if req.Variables.Cursor != nil {
		start = atoi(*req.Variables.Cursor)
	}
	end := min(start+pageSize, len(members))

	nodes := make([]map[string]string, 0, end-start)
	for _, login := range members[start:end] {
		nodes = append(nodes, map[string]string{"login": login})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"organization": map[string]any{
				"membersWithRole": map[string]any{
					"nodes": nodes,
					"pageInfo": map[string]any{
						"endCursor":   strconv.Itoa(end),
						"hasNextPage": end < len(members),
					},
				},
			},
		},
	})
}

func (s *Server) getTeam(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	team, ok := s.teams[r.PathValue("org")][r.PathValue("slug")]
	s.mu.Unlock()
	if !ok {
		notFound(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"slug": team.Slug, "name": team.Slug})
}

func (s *Server) createTeam(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Privacy     string `json:"privacy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	org := r.PathValue("org")
	s.mu.Lock()
	if s.teams[org] == nil {
		s.teams[org] = make(map[string]Team)
		s.teamMembers[org] = make(map[string][]string)
	}
	s.teams[org][body.Name] = Team{
		Slug:        body.Name,
		Description: body.Description,
		Privacy:     body.Privacy,
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, map[string]any{"slug": body.Name, "name": body.Name})
}

func (s *Server) listTeamMembers(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	members := append([]string(nil), s.teamMembers[r.PathValue("org")][r.PathValue("slug")]...)
	pageSize := s.pageSize
	s.mu.Unlock()

	page := paginate(w, r, s.URL, pageSize, members)
	out := make([]map[string]string, 0, len(page))
	for _, login := range page {
		out = append(out, map[string]string{"login": login})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) putMembership(w http.ResponseWriter, r *http.Request) {
	org, slug, user := r.PathValue("org"), r.PathValue("slug"), r.PathValue("user")

	s.mu.Lock()
	defer s.mu.Unlock()
	s.membershipPuts = append(s.membershipPuts, org+"/"+slug+"/"+user)

	// GitHub semantics: an org member becomes an active team member; a
	// non-member is sent an org invitation and the membership is pending.
	for _, m := range s.orgMembers[org] {
		if strings.EqualFold(m, user) {
			if s.teamMembers[org] == nil {
				s.teamMembers[org] = make(map[string][]string)
			}
			s.teamMembers[org][slug] = append(s.teamMembers[org][slug], user)
			writeJSON(w, http.StatusOK, map[string]string{"state": "active", "role": "member"})
			return
		}
	}
	if s.inviteOnPend {
		s.nextInviteID++
		s.invites[org] = append(s.invites[org], Invite{ID: s.nextInviteID, Login: user})
	}
	writeJSON(w, http.StatusOK, map[string]string{"state": "pending", "role": "member"})
}

func (s *Server) deleteMembership(w http.ResponseWriter, r *http.Request) {
	org, slug, user := r.PathValue("org"), r.PathValue("slug"), r.PathValue("user")

	s.mu.Lock()
	defer s.mu.Unlock()
	members := s.teamMembers[org][slug]
	kept := members[:0]
	for _, m := range members {
		if !strings.EqualFold(m, user) {
			kept = append(kept, m)
		}
	}
	s.teamMembers[org][slug] = kept
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	known := s.users[strings.ToLower(r.PathValue("login"))]
	s.mu.Unlock()
	if !known {
		notFound(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"login": r.PathValue("login")})
}

func (s *Server) listInvites(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	invites := append([]Invite(nil), s.invites[r.PathValue("org")]...)
	pageSize := s.pageSize
	s.mu.Unlock()

	page := paginate(w, r, s.URL, pageSize, invites)
	out := make([]map[string]any, 0, len(page))
	for _, inv := range page {
		out = append(out, map[string]any{"id": inv.ID, "login": inv.Login})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) cancelInvite(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	org := r.PathValue("org")
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.invites[org][:0]
	for _, inv := range s.invites[org] {
		if inv.ID != id {
			kept = append(kept, inv)
		}
	}
	s.invites[org] = kept
	s.cancelled = append(s.cancelled, id)
	w.WriteHeader(http.StatusNoContent)
}

// paginate slices items for the request's page and emits a Link rel="next"
// header when more pages remain, mirroring GitHub's REST pagination.
func paginate[T any](w http.ResponseWriter, r *http.Request, baseURL string, pageSize int, items []T) []T {
	page := atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	start := (page - 1) * pageSize
	if start >= len(items) {
		return nil
	}
	end := min(start+pageSize, len(items))
	if end < len(items) {
		next := *r.URL
		q := next.Query()
		q.Set("page", strconv.Itoa(page+1))
		next.RawQuery = q.Encode()
		w.Header().Set("Link", fmt.Sprintf(`<%s%s>; rel="next"`, baseURL, next.String()))
	}
	return items[start:end]
}

// atoi parses s, treating anything unparsable as zero — fine for a fake
// whose cursors and page numbers it minted itself.
func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(data); err != nil {
		return // client hung up; nothing useful to do in a fake
	}
}

func notFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
}
