package gh

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/google/go-github/v93/github"
	"github.com/shurcooL/githubv4"
)

const (
	Org        = "pug-sh"
	BotLogin   = "leo-the-pug[bot]"
	Dependabot = "dependabot[bot]"
	SyncBranch = "leo/sync-protos"
)

type Client struct {
	rest   *github.Client
	gql    *githubv4.Client
	dryRun bool
	out    io.Writer

	members map[string]bool
	team    map[string]bool
}

func New(token string, dryRun bool, out io.Writer) (*Client, error) {
	hc := &http.Client{Transport: bearer{token: token, base: http.DefaultTransport}}
	rest, err := github.NewClient(github.WithHTTPClient(hc))
	if err != nil {
		return nil, err
	}
	return newClient(rest, githubv4.NewClient(hc), dryRun, out), nil
}

// NewForServer points both APIs at one test server: REST at its root and GraphQL at /graphql.
func NewForServer(serverURL string, dryRun bool, out io.Writer) (*Client, error) {
	base := strings.TrimSuffix(serverURL, "/") + "/"
	rest, err := github.NewClient(github.WithURLs(&base, &base))
	if err != nil {
		return nil, err
	}
	return newClient(rest, githubv4.NewEnterpriseClient(base+"graphql", http.DefaultClient), dryRun, out), nil
}

func newClient(rest *github.Client, gql *githubv4.Client, dryRun bool, out io.Writer) *Client {
	return &Client{rest: rest, gql: gql, dryRun: dryRun, out: out, members: map[string]bool{}, team: map[string]bool{}}
}

func (c *Client) DryRun() bool { return c.dryRun }

func (c *Client) Logf(format string, args ...any) {
	fmt.Fprintf(c.out, format+"\n", args...)
}

func (c *Client) Warnf(format string, args ...any) {
	fmt.Fprintf(c.out, "warning: "+format+"\n", args...)
}

// write logs a write and reports whether to make it: in a dry run it only prints.
func (c *Client) write(format string, args ...any) bool {
	if c.dryRun {
		fmt.Fprintf(c.out, "dry-run: "+format+"\n", args...)
		return false
	}
	fmt.Fprintf(c.out, format+"\n", args...)
	return true
}

type bearer struct {
	token string
	base  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(r)
}

func isStatus(err error, codes ...int) bool {
	var e *github.ErrorResponse
	return errors.As(err, &e) && e.Response != nil && slices.Contains(codes, e.Response.StatusCode)
}

// NormalizeLogin gives bots the [bot] suffix that REST uses and GraphQL leaves off.
func NormalizeLogin(login, typename string) string {
	if typename == "Bot" && !strings.HasSuffix(login, "[bot]") {
		return login + "[bot]"
	}
	return login
}

type pageInfo struct {
	HasNextPage bool
	EndCursor   githubv4.String
}

func cursor(p pageInfo) *githubv4.String {
	c := p.EndCursor
	return &c
}
