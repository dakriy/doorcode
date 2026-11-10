package app

import "github.com/dakriy/doorcode/internal/settings"

// tabStrip is the row of tabs under a page's header, named by Label for
// readers of the page.
type tabStrip struct {
	Label string
	Links []tabLink
}

// tabLink is a link to a tab of a page, Current when the page shows that
// tab, and the Count of what the tab holds for the reader's attention,
// what Counted says of each, when there are any.
type tabLink struct {
	Name    string
	Href    string
	Current bool
	Count   int
	Counted string
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

// counting is s with count, each counted as what counted says, shown on
// its link to tab.
func (s tabStrip) counting(tab settings.Tab, count int, counted string) tabStrip {
	links := make([]tabLink, 0, len(s.Links))
	for _, link := range s.Links {
		if link.Name == tab.Name {
			link.Count, link.Counted = count, counted
		}
		links = append(links, link)
	}
	return tabStrip{Label: s.Label, Links: links}
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
