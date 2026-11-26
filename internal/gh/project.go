package gh

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/v93/github"
	"github.com/shurcooL/githubv4"
)

type Project struct {
	ID     string
	Number int
	Title  string
	Public bool
	Closed bool
	URL    string
}

type Field struct {
	ID       int64
	NodeID   string
	Name     string
	DataType string
	Options  []Option
}

func (f Field) Option(name string) (Option, bool) {
	for _, o := range f.Options {
		if strings.EqualFold(o.Name, name) {
			return o, true
		}
	}
	return Option{}, false
}

type Option struct {
	ID          string
	Name        string
	Color       string
	Description string
}

type View struct {
	Name   string
	Layout string
	Filter string
}

type Workflow struct {
	Name    string
	Enabled bool
}

type Item struct {
	ID        string
	DBID      int64
	ContentID string
	Archived  bool
	// Values holds the item's single-select values, by field name.
	Values map[string]string
}

// FieldValue sets one single-select field. An empty OptionID clears it.
type FieldValue struct {
	FieldID    int64
	FieldName  string
	OptionID   string
	OptionName string
}

func (c *Client) FindProject(ctx context.Context, title string) (*Project, error) {
	var q struct {
		Organization struct {
			ProjectsV2 struct {
				Nodes []struct {
					ID     string
					Number int
					Title  string
					Public bool
					Closed bool
					URL    string
				}
			} `graphql:"projectsV2(first: 100, query: $query)"`
		} `graphql:"organization(login: $org)"`
	}
	vars := map[string]any{"org": githubv4.String(Org), "query": githubv4.String(title)}
	if err := c.gql.Query(ctx, &q, vars); err != nil {
		return nil, fmt.Errorf("find project %q: %w", title, err)
	}
	for _, p := range q.Organization.ProjectsV2.Nodes {
		if p.Title == title {
			return &Project{ID: p.ID, Number: p.Number, Title: p.Title, Public: p.Public, Closed: p.Closed, URL: p.URL}, nil
		}
	}
	return nil, nil
}

func (c *Client) CreateProject(ctx context.Context, title string) (*Project, error) {
	if !c.write("create project %q in %s", title, Org) {
		return &Project{Title: title}, nil
	}
	var q struct {
		Organization struct{ ID string } `graphql:"organization(login: $org)"`
	}
	if err := c.gql.Query(ctx, &q, map[string]any{"org": githubv4.String(Org)}); err != nil {
		return nil, err
	}
	var m struct {
		CreateProjectV2 struct {
			ProjectV2 struct {
				ID     string
				Number int
				Title  string
				Public bool
				URL    string
			}
		} `graphql:"createProjectV2(input: $input)"`
	}
	input := githubv4.CreateProjectV2Input{OwnerID: q.Organization.ID, Title: githubv4.String(title)}
	if err := c.gql.Mutate(ctx, &m, input, nil); err != nil {
		return nil, fmt.Errorf("create project %q: %w", title, err)
	}
	p := m.CreateProjectV2.ProjectV2
	return &Project{ID: p.ID, Number: p.Number, Title: p.Title, Public: p.Public, URL: p.URL}, nil
}

func (c *Client) MakeProjectPublic(ctx context.Context, p *Project) error {
	if !c.write("make project %q public", p.Title) {
		return nil
	}
	var m struct {
		UpdateProjectV2 struct {
			ProjectV2 struct{ Public bool }
		} `graphql:"updateProjectV2(input: $input)"`
	}
	public := githubv4.Boolean(true)
	input := githubv4.UpdateProjectV2Input{ProjectID: p.ID, Public: &public}
	if err := c.gql.Mutate(ctx, &m, input, nil); err != nil {
		return fmt.Errorf("make project %q public: %w", p.Title, err)
	}
	return nil
}

func (c *Client) ProjectFields(ctx context.Context, p *Project) ([]Field, error) {
	opts := &github.ListProjectsOptions{PerPage: 100}
	var fields []Field
	for {
		page, resp, err := c.rest.Projects.ListOrganizationProjectFields(ctx, Org, p.Number, opts)
		if err != nil {
			return nil, fmt.Errorf("list fields of %q: %w", p.Title, err)
		}
		for _, f := range page {
			fields = append(fields, toField(f))
		}
		if resp.After == "" {
			return fields, nil
		}
		opts.After = resp.After
	}
}

func toField(f *github.ProjectV2Field) Field {
	field := Field{ID: f.GetID(), NodeID: f.GetNodeID(), Name: f.GetName(), DataType: f.GetDataType()}
	for _, o := range f.Options {
		field.Options = append(field.Options, Option{
			ID:          o.GetID(),
			Name:        o.GetName().GetRaw(),
			Color:       strings.ToUpper(o.GetColor()),
			Description: o.GetDescription().GetRaw(),
		})
	}
	return field
}

func (c *Client) AddField(ctx context.Context, p *Project, name string, options []Option) (Field, error) {
	if !c.write("add field %s (%s) to %q", name, optionNames(options), p.Title) {
		return Field{Name: name, DataType: "single_select", Options: options}, nil
	}
	req := github.AddProjectV2FieldRequest{Name: new(name), DataType: new("single_select")}
	for _, o := range options {
		req.SingleSelectOptions = append(req.SingleSelectOptions, &github.ProjectV2FieldSingleSelectOption{
			Name:        o.Name,
			Color:       new(o.Color),
			Description: new(o.Description),
		})
	}
	f, _, err := c.rest.Projects.AddOrganizationProjectField(ctx, Org, p.Number, req)
	if err != nil {
		return Field{}, fmt.Errorf("add field %s to %q: %w", name, p.Title, err)
	}
	return toField(f), nil
}

// ProjectV2SingleSelectFieldOptionInput mirrors the GraphQL input type, which githubv4 generated
// before the schema gained id. Existing options must be sent with their IDs, or the items that
// use them lose their values.
type ProjectV2SingleSelectFieldOptionInput struct {
	ID          *githubv4.String                               `json:"id,omitempty"`
	Name        githubv4.String                                `json:"name"`
	Color       githubv4.ProjectV2SingleSelectFieldOptionColor `json:"color"`
	Description githubv4.String                                `json:"description"`
}

type UpdateProjectV2FieldInput struct {
	FieldID             githubv4.ID                              `json:"fieldId"`
	SingleSelectOptions *[]ProjectV2SingleSelectFieldOptionInput `json:"singleSelectOptions,omitempty"`
}

// SetFieldOptions replaces a field's options with options. updateProjectV2Field replaces the
// whole list, so options must include the existing ones with their IDs.
func (c *Client) SetFieldOptions(ctx context.Context, p *Project, f Field, options []Option) error {
	if !c.write("set the options of %s on %q to %s", f.Name, p.Title, optionNames(options)) {
		return nil
	}
	var inputs []ProjectV2SingleSelectFieldOptionInput
	for _, o := range options {
		in := ProjectV2SingleSelectFieldOptionInput{
			Name:        githubv4.String(o.Name),
			Color:       githubv4.ProjectV2SingleSelectFieldOptionColor(o.Color),
			Description: githubv4.String(o.Description),
		}
		if o.ID != "" {
			id := githubv4.String(o.ID)
			in.ID = &id
		}
		inputs = append(inputs, in)
	}
	var m struct {
		UpdateProjectV2Field struct {
			ClientMutationID string
		} `graphql:"updateProjectV2Field(input: $input)"`
	}
	if err := c.gql.Mutate(ctx, &m, UpdateProjectV2FieldInput{FieldID: f.NodeID, SingleSelectOptions: &inputs}, nil); err != nil {
		return fmt.Errorf("set options of %s: %w", f.Name, err)
	}
	return nil
}

func optionNames(options []Option) string {
	names := make([]string, len(options))
	for i, o := range options {
		names[i] = o.Name
	}
	return strings.Join(names, ", ")
}

func (c *Client) ProjectViewsAndWorkflows(ctx context.Context, p *Project) ([]View, []Workflow, error) {
	var q struct {
		Node struct {
			ProjectV2 struct {
				Views struct {
					Nodes []struct {
						Name   string
						Layout string
						Filter string
					}
				} `graphql:"views(first: 100)"`
				Workflows struct {
					Nodes []struct {
						Name    string
						Enabled bool
					}
				} `graphql:"workflows(first: 100)"`
			} `graphql:"... on ProjectV2"`
		} `graphql:"node(id: $id)"`
	}
	if err := c.gql.Query(ctx, &q, map[string]any{"id": githubv4.ID(p.ID)}); err != nil {
		return nil, nil, fmt.Errorf("read views and workflows of %q: %w", p.Title, err)
	}
	var views []View
	for _, v := range q.Node.ProjectV2.Views.Nodes {
		views = append(views, View{Name: v.Name, Layout: v.Layout, Filter: v.Filter})
	}
	var workflows []Workflow
	for _, w := range q.Node.ProjectV2.Workflows.Nodes {
		workflows = append(workflows, Workflow{Name: w.Name, Enabled: w.Enabled})
	}
	return views, workflows, nil
}

// CreateView creates a view with layout "board" or "table". The API can't set grouping.
func (c *Client) CreateView(ctx context.Context, p *Project, name, layout, filter string) error {
	desc := fmt.Sprintf("create %s view %q on %q", layout, name, p.Title)
	if filter != "" {
		desc += fmt.Sprintf(" filtered to %q", filter)
	}
	if !c.write("%s", desc) {
		return nil
	}
	req := github.CreateProjectV2ViewRequest{Name: name, Layout: layout}
	if filter != "" {
		req.Filter = new(filter)
	}
	if _, _, err := c.rest.Projects.CreateOrganizationProjectView(ctx, Org, p.Number, req); err != nil {
		return fmt.Errorf("create view %q: %w", name, err)
	}
	return nil
}

// ProjectItems lists every item on the project, archived ones included.
func (c *Client) ProjectItems(ctx context.Context, p *Project) ([]Item, error) {
	var q struct {
		Node struct {
			ProjectV2 struct {
				Items struct {
					Nodes []struct {
						ID             string
						FullDatabaseID string `graphql:"fullDatabaseId"`
						IsArchived     bool
						Content        struct {
							Issue struct {
								ID string
							} `graphql:"... on Issue"`
							PullRequest struct {
								ID string
							} `graphql:"... on PullRequest"`
						}
						FieldValues struct {
							Nodes []struct {
								SingleSelect struct {
									Name  string
									Field struct {
										SingleSelect struct {
											Name string
										} `graphql:"... on ProjectV2SingleSelectField"`
									}
								} `graphql:"... on ProjectV2ItemFieldSingleSelectValue"`
							}
						} `graphql:"fieldValues(first: 30)"`
					}
					PageInfo pageInfo
				} `graphql:"items(first: 100, after: $cursor, archivedStates: [ARCHIVED, NOT_ARCHIVED])"`
			} `graphql:"... on ProjectV2"`
		} `graphql:"node(id: $id)"`
	}
	vars := map[string]any{"id": githubv4.ID(p.ID), "cursor": (*githubv4.String)(nil)}
	var items []Item
	for {
		if err := c.gql.Query(ctx, &q, vars); err != nil {
			return nil, fmt.Errorf("list items of %q: %w", p.Title, err)
		}
		for _, n := range q.Node.ProjectV2.Items.Nodes {
			id, err := strconv.ParseInt(n.FullDatabaseID, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("item %s: bad database ID %q", n.ID, n.FullDatabaseID)
			}
			item := Item{ID: n.ID, DBID: id, Archived: n.IsArchived, Values: map[string]string{}}
			item.ContentID = n.Content.Issue.ID
			if item.ContentID == "" {
				item.ContentID = n.Content.PullRequest.ID
			}
			for _, v := range n.FieldValues.Nodes {
				if field := v.SingleSelect.Field.SingleSelect.Name; field != "" {
					item.Values[field] = v.SingleSelect.Name
				}
			}
			items = append(items, item)
		}
		if !q.Node.ProjectV2.Items.PageInfo.HasNextPage {
			return items, nil
		}
		vars["cursor"] = cursor(q.Node.ProjectV2.Items.PageInfo)
	}
}

// AddItem adds an issue or pull request to the project. In a dry run the item it returns has no IDs.
func (c *Client) AddItem(ctx context.Context, p *Project, repo string, number int, pr bool) (Item, error) {
	if !c.write("%s#%d: add to %s", repo, number, p.Title) {
		return Item{Values: map[string]string{}}, nil
	}
	kind := github.ProjectV2ItemContentTypeIssue
	if pr {
		kind = github.ProjectV2ItemContentTypePullRequest
	}
	req := github.AddProjectItemRequest{Type: kind, Owner: new(Org), Repo: new(repo), Number: new(number)}
	added, _, err := c.rest.Projects.AddOrganizationProjectItem(ctx, Org, p.Number, req)
	if err != nil {
		return Item{}, fmt.Errorf("%s#%d: add to %s: %w", repo, number, p.Title, err)
	}
	return Item{ID: added.GetNodeID(), DBID: added.GetID(), Values: map[string]string{}}, nil
}

func (c *Client) SetItemFields(ctx context.Context, p *Project, label string, item Item, values []FieldValue) error {
	var parts []string
	req := github.UpdateProjectItemRequest{}
	for _, v := range values {
		if v.OptionID == "" {
			parts = append(parts, "clear "+v.FieldName)
			req.Fields = append(req.Fields, &github.UpdateProjectV2Field{ID: v.FieldID, Value: nil})
			continue
		}
		parts = append(parts, v.FieldName+" "+v.OptionName)
		req.Fields = append(req.Fields, &github.UpdateProjectV2Field{ID: v.FieldID, Value: v.OptionID})
	}
	if !c.write("%s: set %s", label, strings.Join(parts, ", ")) {
		return nil
	}
	if _, _, err := c.rest.Projects.UpdateOrganizationProjectItem(ctx, Org, p.Number, item.DBID, req); err != nil {
		return fmt.Errorf("%s: set fields: %w", label, err)
	}
	return nil
}

func (c *Client) ItemArchivedAt(ctx context.Context, p *Project, item Item) (time.Time, error) {
	got, _, err := c.rest.Projects.GetOrganizationProjectItem(ctx, Org, p.Number, item.DBID, nil)
	if err != nil {
		return time.Time{}, fmt.Errorf("get item %d: %w", item.DBID, err)
	}
	return got.GetArchivedAt().Time, nil
}

func (c *Client) UnarchiveItem(ctx context.Context, p *Project, label string, item Item) error {
	if !c.write("%s: unarchive on %s", label, p.Title) {
		return nil
	}
	var m struct {
		UnarchiveProjectV2Item struct {
			Item struct{ ID string }
		} `graphql:"unarchiveProjectV2Item(input: $input)"`
	}
	input := githubv4.UnarchiveProjectV2ItemInput{ProjectID: p.ID, ItemID: item.ID}
	if err := c.gql.Mutate(ctx, &m, input, nil); err != nil {
		return fmt.Errorf("%s: unarchive: %w", label, err)
	}
	return nil
}
