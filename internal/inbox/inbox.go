package inbox

import (
	"context"
	"errors"

	"github.com/pug-sh/leo/internal/gh"
)

// Run adds every open issue and non-draft pull request in scope to Inbox, fills its empty fields,
// unarchives what was reopened after being archived, and keeps CI current on pull requests.
func Run(ctx context.Context, c *gh.Client) error {
	b, err := Load(ctx, c)
	if err != nil {
		return err
	}
	repos, err := c.Repos(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, r := range repos {
		if err := b.syncRepo(ctx, c, r.Name); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (b *Board) syncRepo(ctx context.Context, c *gh.Client, repo string) error {
	contents, err := c.OpenContent(ctx, repo)
	if err != nil {
		return err
	}
	alerts := &alertPRs{c: c, repo: repo}
	var errs []error
	for _, ct := range contents {
		if err := b.syncContent(ctx, c, ct, alerts); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (b *Board) syncContent(ctx context.Context, c *gh.Client, ct gh.Content, alerts *alertPRs) error {
	item := b.Items[ct.ID]
	switch {
	case item == nil:
		if ct.Draft {
			return nil
		}
		added, err := c.AddItem(ctx, b.Project, ct.Repo, ct.Number, ct.PR)
		if err != nil {
			return err
		}
		added.ContentID = ct.ID
		item = &added
		b.Items[ct.ID] = item
	case item.Archived:
		// An archived item stays dismissed unless its issue or pull request was reopened since.
		if ct.ReopenedAt.IsZero() {
			return nil
		}
		at, err := c.ItemArchivedAt(ctx, b.Project, *item)
		if err != nil {
			return err
		}
		if !ct.ReopenedAt.After(at) {
			return nil
		}
		if err := c.UnarchiveItem(ctx, b.Project, ct.Label(), *item); err != nil {
			return err
		}
		item.Archived = false
	}
	values, err := b.fill(ctx, c, ct, item, alerts)
	if err != nil {
		return err
	}
	if len(values) == 0 {
		return nil
	}
	return c.SetItemFields(ctx, b.Project, ct.Label(), *item, values)
}

// fill sets Status, Kind and Priority where they're empty, so it never overwrites a value set by
// hand, and keeps CI in step with a pull request's head commit.
func (b *Board) fill(ctx context.Context, c *gh.Client, ct gh.Content, item *gh.Item, alerts *alertPRs) ([]gh.FieldValue, error) {
	var set []gh.FieldValue
	if item.Values["Status"] == "" {
		set = append(set, b.value("Status", "Todo"))
	}
	kind := item.Values["Kind"]
	if kind == "" {
		var linked map[int]bool
		if ct.PR && ct.Author == gh.Dependabot {
			var err error
			if linked, err = alerts.get(ctx); err != nil {
				// A Dependabot pull request's Kind depends on the alerts, so it waits for a run that
				// can read them.
				c.Warnf("%s: leaving Kind and Priority empty: %v", ct.Label(), err)
				return append(set, b.ci(ct, item)...), nil
			}
		}
		kind = KindOf(ct, linked)
		set = append(set, b.value("Kind", kind))
	}
	if item.Values["Priority"] == "" {
		outsider := false
		if NeedsTeamCheck(ct) {
			team, err := c.IsTeam(ctx, ct.Repo, ct.Author)
			if err != nil {
				return nil, err
			}
			outsider = !team
		}
		set = append(set, b.value("Priority", PriorityOf(ct, kind, outsider)))
	}
	return append(set, b.ci(ct, item)...), nil
}

func (b *Board) ci(ct gh.Content, item *gh.Item) []gh.FieldValue {
	if !ct.PR {
		return nil
	}
	ci := CIOf(ct.Checks)
	if ci == item.Values["CI"] {
		return nil
	}
	if ci == "" {
		return []gh.FieldValue{b.clear("CI")}
	}
	return []gh.FieldValue{b.value("CI", ci)}
}

// alertPRs reads a repo's alert-linked pull requests once, and only if a Dependabot pull request
// needs its Kind.
type alertPRs struct {
	c      *gh.Client
	repo   string
	loaded bool
	prs    map[int]bool
	err    error
}

func (a *alertPRs) get(ctx context.Context) (map[int]bool, error) {
	if !a.loaded {
		a.loaded = true
		updates, err := a.c.DependabotUpdates(ctx, a.repo)
		a.err = err
		a.prs = map[int]bool{}
		for _, u := range updates {
			if u.OpenPR() {
				a.prs[u.PR] = true
			}
		}
	}
	return a.prs, a.err
}
