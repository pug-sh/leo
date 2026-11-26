// Package ghtest serves recorded GitHub API responses to a gh.Client.
package ghtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/pug-sh/leo/internal/gh"
)

type Server struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	rest    map[string]response
	graphql []graphqlRoute
}

type response struct {
	status int
	body   string
}

type graphqlRoute struct {
	match string
	vars  map[string]string
	data  string
}

func New(t *testing.T) *Server {
	s := &Server{t: t, rest: map[string]response{}}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

// REST answers method and path, without its query string, with status and body.
func (s *Server) REST(method, path string, status int, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rest[method+" "+path] = response{status, body}
}

// GraphQL answers the first query containing match whose variables include vars with data, the
// value of the response's data member.
func (s *Server) GraphQL(match string, vars map[string]string, data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.graphql = append(s.graphql, graphqlRoute{match, vars, data})
}

// Client returns a client of the server and the buffer it logs to.
func (s *Server) Client(dryRun bool) (*gh.Client, *bytes.Buffer) {
	var out bytes.Buffer
	c, err := gh.NewForServer(s.srv.URL, dryRun, &out)
	if err != nil {
		s.t.Fatal(err)
	}
	return c, &out
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/graphql" {
		s.serveGraphQL(w, r)
		return
	}
	resp, ok := s.rest[r.Method+" "+r.URL.Path]
	if !ok {
		s.t.Errorf("unexpected request %s %s", r.Method, r.URL)
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		return
	}
	w.WriteHeader(resp.status)
	io.WriteString(w, resp.body)
}

func (s *Server) serveGraphQL(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.t.Errorf("decode GraphQL request: %v", err)
		return
	}
	for _, route := range s.graphql {
		if strings.Contains(req.Query, route.match) && varsMatch(req.Variables, route.vars) {
			fmt.Fprintf(w, `{"data":%s}`, route.data)
			return
		}
	}
	s.t.Errorf("unexpected GraphQL query %s with %v", req.Query, req.Variables)
	io.WriteString(w, `{"errors":[{"message":"unexpected query"}]}`)
}

func varsMatch(got map[string]any, want map[string]string) bool {
	for k, v := range want {
		if fmt.Sprint(got[k]) != v {
			return false
		}
	}
	return true
}
