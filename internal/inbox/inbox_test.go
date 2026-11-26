package inbox

import (
	"context"
	"strings"
	"testing"

	"github.com/pug-sh/leo/internal/gh/ghtest"
)

const fieldsJSON = `[
  {"id": 101, "node_id": "F_STATUS", "name": "Status", "data_type": "single_select", "options": [
    {"id": "s1", "name": {"raw": "Todo"}, "color": "green"},
    {"id": "s2", "name": {"raw": "In Progress"}, "color": "yellow"},
    {"id": "s3", "name": {"raw": "Done"}, "color": "purple"}]},
  {"id": 102, "node_id": "F_PRIORITY", "name": "Priority", "data_type": "single_select", "options": [
    {"id": "p1", "name": {"raw": "High"}}, {"id": "p2", "name": {"raw": "Normal"}}]},
  {"id": 103, "node_id": "F_KIND", "name": "Kind", "data_type": "single_select", "options": [
    {"id": "k1", "name": {"raw": "Issue"}}, {"id": "k2", "name": {"raw": "PR"}},
    {"id": "k3", "name": {"raw": "Dependabot"}}, {"id": "k4", "name": {"raw": "Security"}},
    {"id": "k5", "name": {"raw": "Release"}}, {"id": "k6", "name": {"raw": "Protos"}}]},
  {"id": 104, "node_id": "F_CI", "name": "CI", "data_type": "single_select", "options": [
    {"id": "c1", "name": {"raw": "Passing"}}, {"id": "c2", "name": {"raw": "Failing"}}, {"id": "c3", "name": {"raw": "Pending"}}]},
  {"id": 105, "node_id": "F_TITLE", "name": "Title", "data_type": "title"}
]`

func newServer(t *testing.T) *ghtest.Server {
	s := ghtest.New(t)
	s.REST("GET", "/orgs/pug-sh/repos", 200, `[
	  {"name": "app", "visibility": "public", "default_branch": "main"},
	  {"name": "sdk-go", "visibility": "public", "default_branch": "main", "topics": ["pug-sdk"]},
	  {"name": "attic", "visibility": "public", "archived": true},
	  {"name": "fork", "visibility": "public", "fork": true}
	]`)
	s.GraphQL("projectsV2(", nil, `{"organization": {"projectsV2": {"nodes": [
	  {"id": "PVT_other", "number": 2, "title": "Inbox archive"},
	  {"id": "PVT_inbox", "number": 1, "title": "Inbox", "public": true, "url": "https://github.com/orgs/pug-sh/projects/1"}
	]}}}`)
	s.REST("GET", "/orgs/pug-sh/projectsV2/1/fields", 200, fieldsJSON)
	return s
}

func TestRunDryRun(t *testing.T) {
	s := newServer(t)
	s.GraphQL("items(", nil, `{"node": {"items": {"nodes": [
	  {"id": "PVTI_a", "fullDatabaseId": "201", "isArchived": false, "content": {"id": "I_app_1"}, "fieldValues": {"nodes": []}},
	  {"id": "PVTI_b", "fullDatabaseId": "202", "isArchived": true, "content": {"id": "PR_go_5"}, "fieldValues": {"nodes": [
	    {"name": "PR", "field": {"name": "Kind"}}, {"name": "Normal", "field": {"name": "Priority"}}, {"name": "Passing", "field": {"name": "CI"}}]}},
	  {"id": "PVTI_c", "fullDatabaseId": "203", "isArchived": true, "content": {"id": "I_go_6"}, "fieldValues": {"nodes": []}},
	  {"id": "PVTI_d", "fullDatabaseId": "204", "isArchived": false, "content": {"id": "PR_go_7"}, "fieldValues": {"nodes": [
	    {"name": "In Progress", "field": {"name": "Status"}}, {"name": "PR", "field": {"name": "Kind"}},
	    {"name": "Normal", "field": {"name": "Priority"}}, {"name": "Passing", "field": {"name": "CI"}}, {}]}},
	  {"id": "PVTI_e", "fullDatabaseId": "205", "isArchived": false, "content": {}, "fieldValues": {"nodes": []}}
	], "pageInfo": {"hasNextPage": false}}}}`)

	s.GraphQL("issues(states: OPEN", map[string]string{"name": "app"}, `{"repository": {"issues": {"nodes": [
	  {"id": "I_app_1", "number": 1, "title": "Crash", "author": {"login": "stranger", "__typename": "User"},
	   "authorAssociation": "NONE", "timelineItems": {"nodes": []}}
	], "pageInfo": {"hasNextPage": false}}}}`)
	s.GraphQL("pullRequests(states: OPEN", map[string]string{"name": "app"}, `{"repository": {"pullRequests": {"nodes": [
	  {"id": "PR_app_2", "number": 2, "isDraft": true, "author": {"login": "member", "__typename": "User"}, "authorAssociation": "MEMBER", "timelineItems": {"nodes": []}},
	  {"id": "PR_app_3", "number": 3, "author": {"login": "dependabot", "__typename": "Bot"}, "authorAssociation": "CONTRIBUTOR",
	   "statusCheckRollup": {"state": "SUCCESS"}, "timelineItems": {"nodes": []}}
	], "pageInfo": {"hasNextPage": false}}}}`)
	s.GraphQL("vulnerabilityAlerts(", map[string]string{"name": "app"}, `{"repository": {"vulnerabilityAlerts": {"nodes": [
	  {"number": 1, "dependabotUpdate": {"pullRequest": {"number": 3, "state": "OPEN"}}},
	  {"number": 2, "dependabotUpdate": {"pullRequest": {"number": 9, "state": "CLOSED"}}},
	  {"number": 4, "dependabotUpdate": null}
	], "pageInfo": {"hasNextPage": false}}}}`)
	s.REST("GET", "/orgs/pug-sh/members/stranger", 404, `{"message": "Not Found"}`)
	s.REST("GET", "/repos/pug-sh/app/collaborators/stranger", 404, `{"message": "Not Found"}`)

	s.GraphQL("issues(states: OPEN", map[string]string{"name": "sdk-go"}, `{"repository": {"issues": {"nodes": [
	  {"id": "I_go_6", "number": 6, "author": {"login": "member", "__typename": "User"}, "authorAssociation": "MEMBER", "timelineItems": {"nodes": []}}
	], "pageInfo": {"hasNextPage": false}}}}`)
	s.GraphQL("pullRequests(states: OPEN", map[string]string{"name": "sdk-go"}, `{"repository": {"pullRequests": {"nodes": [
	  {"id": "PR_go_5", "number": 5, "author": {"login": "member", "__typename": "User"}, "authorAssociation": "MEMBER",
	   "statusCheckRollup": {"state": "SUCCESS"}, "timelineItems": {"nodes": [{"createdAt": "2026-11-20T00:00:00Z"}]}},
	  {"id": "PR_go_7", "number": 7, "author": {"login": "member", "__typename": "User"}, "authorAssociation": "MEMBER",
	   "statusCheckRollup": {"state": "FAILURE"}, "timelineItems": {"nodes": []}}
	], "pageInfo": {"hasNextPage": false}}}}`)
	s.REST("GET", "/orgs/pug-sh/projectsV2/1/items/202", 200, `{"id": 202, "archived_at": "2026-11-10T00:00:00Z"}`)

	c, out := s.Client(true)
	if err := Run(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"dry-run: app#1: set Status Todo, Kind Issue, Priority High",
		"dry-run: app#3: add to Inbox",
		"dry-run: app#3: set Status Todo, Kind Security, Priority High, CI Passing",
		"dry-run: sdk-go#5: unarchive on Inbox",
		"dry-run: sdk-go#5: set Status Todo",
		"dry-run: sdk-go#7: set CI Failing",
	}
	if got := strings.Split(strings.TrimSpace(out.String()), "\n"); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("output:\n%s\nwant:\n%s", out, strings.Join(want, "\n"))
	}
}

func TestLoadNeedsSetup(t *testing.T) {
	s := ghtest.New(t)
	s.GraphQL("projectsV2(", nil, `{"organization": {"projectsV2": {"nodes": []}}}`)
	c, _ := s.Client(true)
	if _, err := Load(context.Background(), c); err == nil || !strings.Contains(err.Error(), "run leo setup") {
		t.Errorf("Load without a project: %v", err)
	}
}

func TestSetupDryRunAddsWhatIsMissing(t *testing.T) {
	s := ghtest.New(t)
	s.GraphQL("projectsV2(", nil, `{"organization": {"projectsV2": {"nodes": [
	  {"id": "PVT_inbox", "number": 1, "title": "Inbox", "public": false, "url": "https://github.com/orgs/pug-sh/projects/1"}
	]}}}`)
	s.REST("GET", "/orgs/pug-sh/projectsV2/1/fields", 200, `[
	  {"id": 101, "node_id": "F_STATUS", "name": "Status", "data_type": "single_select", "options": [
	    {"id": "s1", "name": {"raw": "Todo"}, "color": "green"}, {"id": "s3", "name": {"raw": "Done"}, "color": "purple"}]},
	  {"id": 102, "node_id": "F_PRIORITY", "name": "Priority", "data_type": "single_select", "options": [
	    {"id": "p1", "name": {"raw": "High"}}, {"id": "p2", "name": {"raw": "Normal"}}]}
	]`)
	s.GraphQL("views(", nil, `{"node": {
	  "views": {"nodes": [{"name": "View 1", "layout": "TABLE_LAYOUT", "filter": ""}, {"name": "Board", "layout": "BOARD_LAYOUT", "filter": ""}]},
	  "workflows": {"nodes": [{"name": "Item closed", "enabled": true}, {"name": "Pull request merged", "enabled": true},
	    {"name": "Auto-close issue", "enabled": true}, {"name": "Item reopened", "enabled": false}]}}}`)

	c, out := s.Client(true)
	if err := Setup(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`dry-run: make project "Inbox" public`,
		`dry-run: set the options of Status on "Inbox" to Todo, Done, In Progress`,
		`dry-run: add field Kind (Issue, PR, Dependabot, Security, Release, Protos) to "Inbox"`,
		`dry-run: add field CI (Passing, Failing, Pending) to "Inbox"`,
		`dry-run: create table view "High priority" on "Inbox" filtered to "priority:High is:open"`,
		`dry-run: create table view "By repo" on "Inbox"`,
		`warning: built-in workflow "Item reopened" is off`,
		`warning: Inbox has no built-in workflow "Auto-archive items"`,
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"add field Status", "add field Priority", `view "Board"`} {
		if strings.Contains(out.String(), unwanted) {
			t.Errorf("output has %q:\n%s", unwanted, out)
		}
	}
}
