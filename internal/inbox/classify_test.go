package inbox

import (
	"testing"

	"github.com/pug-sh/leo/internal/gh"
)

func TestKindOf(t *testing.T) {
	security := "Advisory.\n\n" + gh.Marker(gh.MarkerSecurity, "GHSA-aaaa-bbbb-cccc")
	release := "Changes.\n\n" + gh.Marker(gh.MarkerRelease)
	failed := "Log.\n\n" + gh.Marker(gh.MarkerProtosFailed, "432c927edc274179ad23f1498a6ad20d")
	alertPRs := map[int]bool{7: true}

	tests := []struct {
		name string
		c    gh.Content
		want string
	}{
		{"Leo's security issue", gh.Content{Author: gh.BotLogin, Body: security}, KindSecurity},
		{"Dependabot PR fixing an alert", gh.Content{PR: true, Number: 7, Author: gh.Dependabot}, KindSecurity},
		{"Leo's sync PR", gh.Content{PR: true, Author: gh.BotLogin, HeadRef: gh.SyncBranch}, KindProtos},
		{"Leo's sync failure", gh.Content{Author: gh.BotLogin, Body: failed}, KindProtos},
		{"Leo's release reminder", gh.Content{Author: gh.BotLogin, Body: release}, KindRelease},
		{"other Dependabot PR", gh.Content{PR: true, Number: 8, Author: gh.Dependabot}, KindDependabot},
		{"person's PR", gh.Content{PR: true, Author: "someone"}, KindPR},
		{"person's issue", gh.Content{Author: "someone"}, KindIssue},
		{"pasted security marker", gh.Content{Author: "someone", Body: security}, KindIssue},
		{"pasted release marker", gh.Content{Author: "someone", Body: release}, KindIssue},
		{"someone else's sync branch", gh.Content{PR: true, Author: "someone", HeadRef: gh.SyncBranch}, KindPR},
		{"Leo's PR from another branch", gh.Content{PR: true, Author: gh.BotLogin, HeadRef: "leo/other"}, KindPR},
		{"security marker on Leo's PR", gh.Content{PR: true, Author: gh.BotLogin, Body: security}, KindPR},
		{"security comes first", gh.Content{Author: gh.BotLogin, Body: security + "\n" + release}, KindSecurity},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := KindOf(tt.c, alertPRs); got != tt.want {
				t.Errorf("KindOf = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestPriorityOf(t *testing.T) {
	failed := gh.Marker(gh.MarkerProtosFailed, "432c927e")
	tests := []struct {
		name     string
		c        gh.Content
		kind     string
		outsider bool
		want     string
	}{
		{"security item", gh.Content{PR: true, Author: gh.Dependabot}, KindSecurity, false, PriorityHigh},
		{"outsider's issue", gh.Content{Author: "stranger"}, KindIssue, true, PriorityHigh},
		{"outsider's PR", gh.Content{PR: true, Author: "stranger"}, KindPR, true, PriorityHigh},
		{"Leo's sync failure", gh.Content{Author: gh.BotLogin, Body: failed}, KindProtos, false, PriorityHigh},
		{"Leo's sync PR", gh.Content{PR: true, Author: gh.BotLogin, HeadRef: gh.SyncBranch}, KindProtos, false, PriorityNormal},
		{"team member's PR", gh.Content{PR: true, Author: "member"}, KindPR, false, PriorityNormal},
		{"Dependabot update", gh.Content{PR: true, Author: gh.Dependabot}, KindDependabot, false, PriorityNormal},
		{"pasted failure marker", gh.Content{Author: "member", Body: failed}, KindIssue, false, PriorityNormal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PriorityOf(tt.c, tt.kind, tt.outsider); got != tt.want {
				t.Errorf("PriorityOf = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestNeedsTeamCheck(t *testing.T) {
	tests := []struct {
		c    gh.Content
		want bool
	}{
		{gh.Content{Author: "owner", Association: "OWNER"}, false},
		{gh.Content{Author: "member", Association: "MEMBER"}, false},
		{gh.Content{Author: "collaborator", Association: "COLLABORATOR"}, false},
		{gh.Content{Author: gh.Dependabot, Bot: true, Association: "CONTRIBUTOR"}, false},
		{gh.Content{Author: "", Association: "NONE"}, false},
		{gh.Content{Author: "contributor", Association: "CONTRIBUTOR"}, true},
		{gh.Content{Author: "newcomer", Association: "FIRST_TIME_CONTRIBUTOR"}, true},
		{gh.Content{Author: "stranger", Association: "NONE"}, true},
	}
	for _, tt := range tests {
		if got := NeedsTeamCheck(tt.c); got != tt.want {
			t.Errorf("NeedsTeamCheck(%s, %s) = %v, want %v", tt.c.Author, tt.c.Association, got, tt.want)
		}
	}
}

func TestCIOf(t *testing.T) {
	for checks, want := range map[string]string{
		"SUCCESS":  "Passing",
		"FAILURE":  "Failing",
		"ERROR":    "Failing",
		"PENDING":  "Pending",
		"EXPECTED": "Pending",
		"":         "",
	} {
		if got := CIOf(checks); got != want {
			t.Errorf("CIOf(%q) = %q, want %q", checks, got, want)
		}
	}
}
