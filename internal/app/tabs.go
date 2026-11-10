package app

import "github.com/dakriy/doorcode/internal/settings"

// tabStrip is the row of tabs under a page's header, named by Label for
// readers of the page.
type tabStrip struct {
	Label string
	Links []tabLink
}

// tabLink is a link to a tab of a page, Current when the page shows that
// tab.
type tabLink struct {
	Name    string
	Href    string
	Current bool
}

// stripOf is the strip, named label, of a link to each of tabs on the page
// at path, current the one it shows.
func stripOf(label, path string, tabs []settings.Tab, current settings.Tab) tabStrip {
	links := make([]tabLink, 0, len(tabs))
	for _, t := range tabs {
		links = append(links, tabLink{Name: t.Name, Href: tabPath(path, t), Current: t == current})
	}
	return tabStrip{Label: label, Links: links}
}

// tabPath is the address of the page at path showing tab.
func tabPath(path string, tab settings.Tab) string {
	return path + "?tab=" + tab.Slug
}

// tabNamed is the tab of tabs whose slug is slug, or the first when none is.
func tabNamed(tabs []settings.Tab, slug string) settings.Tab {
	for _, t := range tabs {
		if t.Slug == slug {
			return t
		}
	}
	return tabs[0]
}
