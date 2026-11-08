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
	// NavTargets is the address of each link in navigation, in order.
	NavTargets []string
	// Messages is the text of each alert and status message, in order.
	Messages []string
	// Notes is the text of each note, such as why an action is not
	// offered, in order.
	Notes []string
	// FieldErrors maps each input marked invalid to the text of the message
	// that describes it.
	FieldErrors map[string]string
	// Values maps each named input and text area, other than passwords and
	// checkboxes, to its value.
	Values map[string]string
	// Checked is each checked checkbox, as its name, "=", and its value.
	Checked []string
	// Choices maps each checkbox inside a label, as its name, "=", and its
	// value, to the label's text.
	Choices map[string]string
	// Options maps each select to the text of its options, in order; the
	// selected one is suffixed with " (selected)".
	Options map[string][]string
	// Table is the text of every cell of the first table without a label,
	// row by row.
	Table [][]string
	// Tables maps the label of each labelled table to the text of its
	// cells, row by row.
	Tables map[string][][]string
	// Images is every image the page shows, in order.
	Images []Image
	// Assets is every stylesheet and script the page loads.
	Assets []string
}

// Image is an image a page shows: where it is served from and its
// alternative text.
type Image struct {
	Src string
	Alt string
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
		p.NavTargets = append(p.NavTargets, attr(n, "href"))
	case attr(n, "role") == "alert" || attr(n, "role") == "status":
		p.Messages = append(p.Messages, text(n))
	case attr(n, "role") == "note":
		p.Notes = append(p.Notes, text(n))
	case n.Data == "tr":
		p.readRow(n)
	default:
		p.readResource(n)
	}
}

// readRow reads a table row into the table it belongs to.
func (p *Page) readRow(tr *html.Node) {
	table := tableOf(tr)
	label := attr(table, "aria-label")
	switch {
	case label != "":
		if p.Tables == nil {
			p.Tables = map[string][][]string{}
		}
		p.Tables[label] = append(p.Tables[label], rowText(tr))
	case table == firstUnlabelledTable(tr):
		p.Table = append(p.Table, rowText(tr))
	}
}

// readResource reads the inputs and the loaded assets.
func (p *Page) readResource(n *html.Node) {
	switch {
	case n.Data == "input" && attr(n, "type") == "checkbox":
		p.readCheckbox(n)
	case n.Data == "option":
		p.readOption(n)
	case n.Data == "input" && attr(n, "name") != "" && attr(n, "type") != "password":
		p.value(attr(n, "name"), attr(n, "value"))
	case n.Data == "textarea":
		p.value(attr(n, "name"), rawText(n))
	case n.Data == "img":
		p.Images = append(p.Images, Image{Src: attr(n, "src"), Alt: attr(n, "alt")})
	case n.Data == "link" && attr(n, "rel") == "stylesheet":
		p.Assets = append(p.Assets, attr(n, "href"))
	case n.Data == "script" && attr(n, "src") != "":
		p.Assets = append(p.Assets, attr(n, "src"))
	}
}

func (p *Page) readCheckbox(n *html.Node) {
	key := attr(n, "name") + "=" + attr(n, "value")
	if hasAttr(n, "checked") {
		p.Checked = append(p.Checked, key)
	}
	if label := enclosing(n, "label"); label != nil {
		if p.Choices == nil {
			p.Choices = map[string]string{}
		}
		p.Choices[key] = text(label)
	}
}

func (p *Page) readOption(n *html.Node) {
	label := text(n)
	if hasAttr(n, "selected") {
		label += " (selected)"
	}
	if p.Options == nil {
		p.Options = map[string][]string{}
	}
	name := attr(enclosing(n, "select"), "name")
	p.Options[name] = append(p.Options[name], label)
}

func (p *Page) value(name, value string) {
	if p.Values == nil {
		p.Values = map[string]string{}
	}
	p.Values[name] = value
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

// tableOf is the table a row belongs to.
func tableOf(tr *html.Node) *html.Node {
	for a := tr.Parent; a != nil; a = a.Parent {
		if a.Data == "table" {
			return a
		}
	}
	return nil
}

// firstUnlabelledTable is the first table without a label in the document
// that holds n.
func firstUnlabelledTable(n *html.Node) *html.Node {
	root := n
	for root.Parent != nil {
		root = root.Parent
	}
	for t := range root.Descendants() {
		if t.Type == html.ElementNode && t.Data == "table" && attr(t, "aria-label") == "" {
			return t
		}
	}
	return nil
}

func within(n *html.Node, tag string) bool {
	return enclosing(n, tag) != nil
}

// enclosing is the nearest element around n with tag, nil without one.
func enclosing(n *html.Node, tag string) *html.Node {
	for a := n.Parent; a != nil; a = a.Parent {
		if a.Type == html.ElementNode && a.Data == tag {
			return a
		}
	}
	return nil
}

func hasAttr(n *html.Node, name string) bool {
	for _, a := range n.Attr {
		if a.Key == name {
			return true
		}
	}
	return false
}

// rawText is the text inside n as written, as a text area holds it.
func rawText(n *html.Node) string {
	var b strings.Builder
	for d := range n.Descendants() {
		if d.Type == html.TextNode {
			b.WriteString(d.Data)
		}
	}
	return b.String()
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
