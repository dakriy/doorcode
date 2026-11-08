package testsupport

import (
	"strings"

	"golang.org/x/net/html"
)

// Page is what a person sees on an HTML page, read from its markup.
type Page struct {
	// Title is the document title.
	Title string
	// Heading is the text of the first h1.
	Heading string
	// Nav is the text of each link in navigation, in order; a link marked
	// as the current page is suffixed with " (current)".
	Nav []string
	// Messages is the text of each alert and status message, in order.
	Messages []string
	// FieldErrors maps each input marked invalid to the text of the message
	// that describes it.
	FieldErrors map[string]string
	// Values maps each named input, other than passwords, to its value.
	Values map[string]string
	// Table is the text of every cell of the first table, row by row.
	Table [][]string
	// Assets is every stylesheet and script the page loads.
	Assets []string
}

// ParsePage reads a page from HTML.
func ParsePage(body string) Page {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		panic(err)
	}
	var p Page
	byID := map[string]*html.Node{}
	var invalid []*html.Node
	for n := range doc.Descendants() {
		if n.Type != html.ElementNode {
			continue
		}
		if id := attr(n, "id"); id != "" {
			byID[id] = n
		}
		if attr(n, "aria-invalid") == "true" {
			invalid = append(invalid, n)
		}
		p.read(n)
	}
	for _, n := range invalid {
		if p.FieldErrors == nil {
			p.FieldErrors = map[string]string{}
		}
		p.FieldErrors[attr(n, "name")] = text(byID[attr(n, "aria-describedby")])
	}
	return p
}

func (p *Page) read(n *html.Node) {
	switch {
	case n.Data == "title":
		p.Title = text(n)
	case n.Data == "h1" && p.Heading == "":
		p.Heading = text(n)
	case n.Data == "a" && within(n, "nav"):
		p.Nav = append(p.Nav, navText(n))
	case attr(n, "role") == "alert" || attr(n, "role") == "status":
		p.Messages = append(p.Messages, text(n))
	case n.Data == "tr" && firstTable(n):
		p.Table = append(p.Table, rowText(n))
	default:
		p.readResource(n)
	}
}

// readResource reads the inputs and the loaded assets.
func (p *Page) readResource(n *html.Node) {
	switch {
	case n.Data == "input" && attr(n, "name") != "" && attr(n, "type") != "password":
		if p.Values == nil {
			p.Values = map[string]string{}
		}
		p.Values[attr(n, "name")] = attr(n, "value")
	case n.Data == "link" && attr(n, "rel") == "stylesheet":
		p.Assets = append(p.Assets, attr(n, "href"))
	case n.Data == "script" && attr(n, "src") != "":
		p.Assets = append(p.Assets, attr(n, "src"))
	}
}

func navText(n *html.Node) string {
	if attr(n, "aria-current") == "page" {
		return text(n) + " (current)"
	}
	return text(n)
}

func rowText(tr *html.Node) []string {
	var cells []string
	for c := range tr.ChildNodes() {
		if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
			cells = append(cells, text(c))
		}
	}
	return cells
}

// firstTable reports whether the row belongs to the first table in the
// document.
func firstTable(tr *html.Node) bool {
	var table *html.Node
	for a := tr.Parent; a != nil; a = a.Parent {
		if a.Data == "table" {
			table = a
			break
		}
	}
	root := tr
	for root.Parent != nil {
		root = root.Parent
	}
	for n := range root.Descendants() {
		if n.Type == html.ElementNode && n.Data == "table" {
			return n == table
		}
	}
	return false
}

func within(n *html.Node, tag string) bool {
	for a := n.Parent; a != nil; a = a.Parent {
		if a.Type == html.ElementNode && a.Data == tag {
			return true
		}
	}
	return false
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

// text is the visible text inside n with runs of whitespace collapsed.
func text(n *html.Node) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	for d := range n.Descendants() {
		if d.Type == html.TextNode {
			b.WriteString(d.Data)
			b.WriteString(" ")
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
