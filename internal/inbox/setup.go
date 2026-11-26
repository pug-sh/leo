package inbox

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/pug-sh/leo/internal/gh"
)

type viewSpec struct {
	name   string
	layout string
	filter string
}

var viewSpecs = []viewSpec{
	{"Board", "board", ""},
	{"High priority", "table", "priority:High is:open"},
	{"By repo", "table", ""},
}

// The built-in workflows Inbox relies on. The API can read them but not change them.
var workflowNames = []string{"Item closed", "Pull request merged", "Auto-close issue", "Item reopened", "Auto-archive items"}

// Setup creates Inbox if it's missing, makes it public, and adds any missing fields, options and
// views. It's safe to run repeatedly.
func Setup(ctx context.Context, c *gh.Client) error {
	p, err := c.FindProject(ctx, Title)
	if err != nil {
		return err
	}
	if p == nil {
		if p, err = c.CreateProject(ctx, Title); err != nil {
			return err
		}
	}
	if p.Closed {
		c.Warnf("%s is closed: reopen it in its settings", Title)
	}
	if !p.Public {
		// GitHub only lets a person change a project's visibility, so with Leo's token this fails
		// until someone makes Inbox public in its settings.
		if err := c.MakeProjectPublic(ctx, p); err != nil {
			c.Warnf("%v: make it public in its settings", err)
		}
	}

	// A project that a dry run only pretended to create has the Status field every new project
	// starts with, and nothing else.
	fields := []gh.Field{{Name: "Status", DataType: "single_select", Options: fieldSpecs[0].options}}
	var views []gh.View
	var workflows []gh.Workflow
	if p.ID != "" {
		if fields, err = c.ProjectFields(ctx, p); err != nil {
			return err
		}
		if views, workflows, err = c.ProjectViewsAndWorkflows(ctx, p); err != nil {
			return err
		}
	}

	for _, spec := range fieldSpecs {
		if err := ensureField(ctx, c, p, fields, spec); err != nil {
			return err
		}
	}
	for _, spec := range viewSpecs {
		if err := ensureView(ctx, c, p, views, spec); err != nil {
			return err
		}
	}
	if p.ID != "" {
		checkWorkflows(c, workflows)
	}
	c.Logf("set grouping by hand, since the API can't: Board's columns by Status and swimlanes by Priority, and By repo grouped by Repository")
	c.Logf("auto-archive's filter can't be read: it should be is:closed updated:<@today-2w")
	if p.URL != "" {
		c.Logf("%s: %s", Title, p.URL)
	}
	return nil
}

func ensureField(ctx context.Context, c *gh.Client, p *gh.Project, fields []gh.Field, spec fieldSpec) error {
	i := slices.IndexFunc(fields, func(f gh.Field) bool { return strings.EqualFold(f.Name, spec.name) })
	if i < 0 {
		_, err := c.AddField(ctx, p, spec.name, spec.options)
		return err
	}
	f := fields[i]
	if !singleSelect(f) {
		return fmt.Errorf("%s's %s field is a %s field, not a single select", Title, f.Name, f.DataType)
	}
	options := slices.Clone(f.Options)
	for i := range options {
		if options[i].Color == "" {
			options[i].Color = "GRAY"
		}
	}
	missing := false
	for _, o := range spec.options {
		if _, ok := f.Option(o.Name); !ok {
			options = append(options, o)
			missing = true
		}
	}
	if !missing {
		return nil
	}
	return c.SetFieldOptions(ctx, p, f, options)
}

func singleSelect(f gh.Field) bool {
	return f.DataType == "single_select" || f.DataType == "status" || len(f.Options) > 0
}

func ensureView(ctx context.Context, c *gh.Client, p *gh.Project, views []gh.View, spec viewSpec) error {
	i := slices.IndexFunc(views, func(v gh.View) bool { return strings.EqualFold(v.Name, spec.name) })
	if i < 0 {
		return c.CreateView(ctx, p, spec.name, spec.layout, spec.filter)
	}
	v := views[i]
	if want := strings.ToUpper(spec.layout) + "_LAYOUT"; v.Layout != want {
		c.Warnf("view %q is %s, not %s", v.Name, v.Layout, want)
	}
	if v.Filter != spec.filter {
		c.Warnf("view %q filters on %q, not %q", v.Name, v.Filter, spec.filter)
	}
	return nil
}

func checkWorkflows(c *gh.Client, workflows []gh.Workflow) {
	for _, name := range workflowNames {
		i := slices.IndexFunc(workflows, func(w gh.Workflow) bool { return strings.EqualFold(w.Name, name) })
		switch {
		case i < 0:
			c.Warnf("%s has no built-in workflow %q", Title, name)
		case !workflows[i].Enabled:
			c.Warnf("built-in workflow %q is off: turn it on in %s's workflows", name, Title)
		}
	}
}
