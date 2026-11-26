package inbox

import "github.com/pug-sh/leo/internal/gh"

const (
	KindIssue      = "Issue"
	KindPR         = "PR"
	KindDependabot = "Dependabot"
	KindSecurity   = "Security"
	KindRelease    = "Release"
	KindProtos     = "Protos"

	PriorityHigh   = "High"
	PriorityNormal = "Normal"
)

// KindOf applies the first rule that matches. alertPRs holds the pull requests that fix a
// Dependabot alert.
func KindOf(c gh.Content, alertPRs map[int]bool) string {
	leo := c.Author == gh.BotLogin
	leoIssue := leo && !c.PR
	switch {
	case leoIssue && gh.HasMarker(c.Body, gh.MarkerSecurity),
		c.PR && c.Author == gh.Dependabot && alertPRs[c.Number]:
		return KindSecurity
	case leo && c.PR && c.HeadRef == gh.SyncBranch,
		leoIssue && gh.HasMarker(c.Body, gh.MarkerProtosFailed):
		return KindProtos
	case leoIssue && gh.HasMarker(c.Body, gh.MarkerRelease):
		return KindRelease
	case c.PR && c.Author == gh.Dependabot:
		return KindDependabot
	case c.PR:
		return KindPR
	}
	return KindIssue
}

// PriorityOf is High for security items, Leo's proto sync failures, and issues and pull requests
// opened by someone outside the team.
func PriorityOf(c gh.Content, kind string, outsider bool) string {
	failedSync := c.Author == gh.BotLogin && !c.PR && gh.HasMarker(c.Body, gh.MarkerProtosFailed)
	if kind == KindSecurity || failedSync || outsider {
		return PriorityHigh
	}
	return PriorityNormal
}

// NeedsTeamCheck reports whether the author could be outside the team: a bot never is, and
// GitHub already marks owners, members and collaborators.
func NeedsTeamCheck(c gh.Content) bool {
	if c.Bot || c.Author == "" {
		return false
	}
	switch c.Association {
	case "OWNER", "MEMBER", "COLLABORATOR":
		return false
	}
	return true
}

// CIOf maps a commit's combined check state to the CI field. It's empty when the commit has no
// checks or statuses.
func CIOf(checks string) string {
	switch checks {
	case "SUCCESS":
		return "Passing"
	case "FAILURE", "ERROR":
		return "Failing"
	case "PENDING", "EXPECTED":
		return "Pending"
	}
	return ""
}
