package server

import (
	"fmt"
	"html/template"
	"io/fs"
)

// templateSet holds parsed page and partial templates, keyed by short name.
type templateSet struct {
	pages    map[string]*template.Template
	partials map[string]*template.Template
}

// pageTemplates maps a page name to its content template file. Each page is
// parsed alongside layout.html so layout's `content` slot is filled.
var pageTemplates = map[string]string{
	"index":   "index.html",
	"logs":    "logs.html",
	"metrics": "metrics.html",
	"traces":  "traces.html",
	"tail":    "tail.html",
}

// partialTemplates maps a partial name (the `define` name) to its file.
var partialTemplates = map[string]string{
	"logs_rows":     "logs_rows.html",
	"metrics_chart": "metrics_chart.html",
	"traces_rows":   "traces_rows.html",
}

func parseTemplates(filesystem fs.FS) (*templateSet, error) {
	set := &templateSet{
		pages:    map[string]*template.Template{},
		partials: map[string]*template.Template{},
	}
	for name, file := range pageTemplates {
		tpl, err := template.New(name).ParseFS(filesystem, "layout.html", file)
		if err != nil {
			return nil, fmt.Errorf("parsing page %s: %w", name, err)
		}
		set.pages[name] = tpl
	}
	for name, file := range partialTemplates {
		tpl, err := template.New(name).ParseFS(filesystem, file)
		if err != nil {
			return nil, fmt.Errorf("parsing partial %s: %w", name, err)
		}
		set.partials[name] = tpl
	}
	return set, nil
}
