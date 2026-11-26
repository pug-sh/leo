package inbox

import (
	"context"
	"fmt"

	"github.com/pug-sh/leo/internal/gh"
)

const Title = "Inbox"

type fieldSpec struct {
	name    string
	options []gh.Option
}

var fieldSpecs = []fieldSpec{
	{"Status", []gh.Option{
		{Name: "Todo", Color: "GREEN"},
		{Name: "In Progress", Color: "YELLOW"},
		{Name: "Done", Color: "PURPLE"},
	}},
	{"Priority", []gh.Option{
		{Name: PriorityHigh, Color: "RED", Description: "Security, or opened by someone outside the team"},
		{Name: PriorityNormal, Color: "GRAY"},
	}},
	{"Kind", []gh.Option{
		{Name: KindIssue, Color: "GREEN"},
		{Name: KindPR, Color: "BLUE"},
		{Name: KindDependabot, Color: "PURPLE", Description: "Dependency update"},
		{Name: KindSecurity, Color: "RED", Description: "Dependency alert"},
		{Name: KindRelease, Color: "YELLOW", Description: "Unreleased changes on main"},
		{Name: KindProtos, Color: "ORANGE", Description: "Proto sync"},
	}},
	{"CI", []gh.Option{
		{Name: "Passing", Color: "GREEN"},
		{Name: "Failing", Color: "RED"},
		{Name: "Pending", Color: "YELLOW"},
	}},
}

type Board struct {
	Project *gh.Project
	Fields  map[string]gh.Field
	// Items holds every item, archived ones included, by the node ID of its issue or pull request.
	Items map[string]*gh.Item
}

func Load(ctx context.Context, c *gh.Client) (*Board, error) {
	p, err := c.FindProject(ctx, Title)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("%s has no %s project: run leo setup", gh.Org, Title)
	}
	fields, err := c.ProjectFields(ctx, p)
	if err != nil {
		return nil, err
	}
	b := &Board{Project: p, Fields: map[string]gh.Field{}, Items: map[string]*gh.Item{}}
	for _, f := range fields {
		b.Fields[f.Name] = f
	}
	for _, spec := range fieldSpecs {
		f, ok := b.Fields[spec.name]
		if !ok {
			return nil, fmt.Errorf("%s has no %s field: run leo setup", Title, spec.name)
		}
		for _, o := range spec.options {
			if _, ok := f.Option(o.Name); !ok {
				return nil, fmt.Errorf("%s's %s field has no %s option: run leo setup", Title, spec.name, o.Name)
			}
		}
	}
	items, err := c.ProjectItems(ctx, p)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].ContentID != "" {
			b.Items[items[i].ContentID] = &items[i]
		}
	}
	return b, nil
}

func (b *Board) value(field, option string) gh.FieldValue {
	f := b.Fields[field]
	o, _ := f.Option(option)
	return gh.FieldValue{FieldID: f.ID, FieldName: f.Name, OptionID: o.ID, OptionName: o.Name}
}

func (b *Board) clear(field string) gh.FieldValue {
	f := b.Fields[field]
	return gh.FieldValue{FieldID: f.ID, FieldName: f.Name}
}
