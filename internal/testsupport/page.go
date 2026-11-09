package testsupport

import (
	"strings"

	"golang.org/x/net/html"
)

// Page is what a person sees on an HTML page, read from its markup.
type Page struct {
	// Title is the document title, not that of a mark on a chart.
	Title string
	// Heading is the text of the first h1.
	Heading string
	// Nav is the text of each link in navigation, in order; a link in a
	// labelled group of links is prefixed with the group's label and ": ",
	// and a link marked as the current page is suffixed with " (current)".
	Nav []string
	// NavTargets is the address of each link in navigation, in order.
	NavTargets []string
	// Messages is the text of each alert and status message not hidden,
	// in order.
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
	// Details maps the term of each entry of a description list to its
	// description.
	Details map[string]string
	// Images is every image the page shows, in order.
	Images []Image
	// LinkTargets maps the label of each link that has one, such as a
	// button made of a symbol, to its address.
	LinkTargets map[string]string
	// Charts maps the label of each chart, an image drawn in SVG, to what
	// it draws.
	Charts map[string]Chart
	// Bars maps the label of each labelled table to the style of each
	// element in its rows that sets one, such as a bar's "width: 40%", in
	// order.
	Bars map[string][]string
	// Lists maps the label of each labelled list to the text of its items,
	// in order.
	Lists map[string][]string
	// Assets is every stylesheet and script the page loads.
	Assets []string
	// Comboboxes is the name of each select and list of checkboxes the
	// combobox script makes searchable, in order.
	Comboboxes []string
	// Pickers maps the name of each list of checkboxes the search script
	// makes a picker, one that adds choices by search and removes them
	// from the list of those chosen, to the id of that list.
	Pickers map[string]string
	// UserSearches maps the name of each text input the search script
	// makes ask the server for users as you type to the address it asks.
	UserSearches map[string]string
	// Styles is the text of each style sheet the page holds itself.
	Styles []string
	// Inputs maps each named control a person fills in, every one but a
	// hidden input, to its kind: an input's type, select, or textarea.
	Inputs map[string]string
	// Disclosures maps the heading (second or third level) in the summary of each disclosure that
	// has one, a part of the page that folds away, to whether it is open.
	Disclosures map[string]bool
}

// Image is an image a page shows: where it is served from and its
// alternative text.
type Image struct {
	Src string
	Alt string
}

// Chart is what a chart draws: the points of each line, the class that
// colors each line that has one, each rectangle, and each piece of text
// on it, in order.
type Chart struct {
	Lines  []string
	Series []string
	Marks  []Mark
	Labels []string
}

// Mark is a rectangle a chart draws: where, how large, its class, which
// colors it, and the title a pointer over it shows.
type Mark struct {
	X, Y, Width, Height string
	Class               string
	Title               string
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
		p.readScripted(n)
		p.readControl(n)
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

// readScripted reads a control the search script enhances.
func (p *Page) readScripted(n *html.Node) {
	if hasAttr(n, "data-combobox") {
		p.Comboboxes = append(p.Comboboxes, attr(n, "name"))
	}
	if hasAttr(n, "data-picker") {
		if p.Pickers == nil {
			p.Pickers = map[string]string{}
		}
		p.Pickers[attr(n, "name")] = attr(n, "data-picker")
	}
	if hasAttr(n, "data-user-search") {
		if p.UserSearches == nil {
			p.UserSearches = map[string]string{}
		}
		p.UserSearches[attr(n, "name")] = attr(n, "data-user-search")
	}
}

func (p *Page) read(n *html.Node) {
	switch {
	case n.Data == "title" && !within(n, "svg"):
		p.Title = text(n)
	case n.Data == "h1" && p.Heading == "":
		p.Heading = text(n)
	case n.Data == "a" && within(n, "nav"):
		p.Nav = append(p.Nav, navText(n))
		p.NavTargets = append(p.NavTargets, attr(n, "href"))
	case (attr(n, "role") == "alert" || attr(n, "role") == "status") && !hasAttr(n, "hidden"):
		p.Messages = append(p.Messages, text(n))
	case attr(n, "role") == "note":
		p.Notes = append(p.Notes, text(n))
	case n.Data == "tr":
		p.readRow(n)
	case n.Data == "dt":
		p.readDetail(n)
	case n.Data == "svg" && attr(n, "role") == "img":
		p.readChart(n)
	default:
		p.readResource(n)
	}
}

// readDetail reads a description list's term and the description after
// it.
func (p *Page) readDetail(dt *html.Node) {
	dd := dt.NextSibling
	for dd != nil && dd.Type != html.ElementNode {
		dd = dd.NextSibling
	}
	if p.Details == nil {
		p.Details = map[string]string{}
	}
	p.Details[text(dt)] = text(dd)
}

// readChart reads what a chart draws.
func (p *Page) readChart(svg *html.Node) {
	var c Chart
	for d := range svg.Descendants() {
		switch {
		case d.Type != html.ElementNode:
		case d.Data == "polyline":
			c.Lines = append(c.Lines, attr(d, "points"))
			if hasAttr(d, "class") {
				c.Series = append(c.Series, attr(d, "class"))
			}
		case d.Data == "rect":
			c.Marks = append(c.Marks, markOf(d))
		case d.Data == "text":
			c.Labels = append(c.Labels, text(d))
		}
	}
	if p.Charts == nil {
		p.Charts = map[string]Chart{}
	}
	p.Charts[attr(svg, "aria-label")] = c
}

// markOf is the rectangle rect, with the text of its title.
func markOf(rect *html.Node) Mark {
	m := Mark{X: attr(rect, "x"), Y: attr(rect, "y"), Width: attr(rect, "width"), Height: attr(rect, "height"), Class: attr(rect, "class")}
	for d := range rect.Descendants() {
		if d.Data == "title" {
			m.Title = text(d)
		}
	}
	return m
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
		p.readBars(label, tr)
	case table == firstUnlabelledTable(tr):
		p.Table = append(p.Table, rowText(tr))
	}
}

// readBars reads the styles set in tr, a row of the table labelled label.
func (p *Page) readBars(label string, tr *html.Node) {
	for d := range tr.Descendants() {
		if d.Type == html.ElementNode && hasAttr(d, "style") {
			if p.Bars == nil {
				p.Bars = map[string][]string{}
			}
			p.Bars[label] = append(p.Bars[label], attr(d, "style"))
		}
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
	case n.Data == "a" && attr(n, "aria-label") != "":
		p.readLink(n)
	case n.Data == "img":
		p.Images = append(p.Images, Image{Src: attr(n, "src"), Alt: attr(n, "alt")})
	case n.Data == "link" && attr(n, "rel") == "stylesheet":
		p.Assets = append(p.Assets, attr(n, "href"))
	case n.Data == "script" && attr(n, "src") != "":
		p.Assets = append(p.Assets, attr(n, "src"))
	default:
		p.readText(n)
	}
}

// readText reads a labelled list's item, a style sheet's text, and a
// disclosure.
func (p *Page) readText(n *html.Node) {
	switch n.Data {
	case "li":
		p.readItem(n)
	case "style":
		p.Styles = append(p.Styles, rawText(n))
	case "details":
		p.readDisclosure(n)
	}
}

// readControl reads the kind of a named control a person fills in.
func (p *Page) readControl(n *html.Node) {
	kind := n.Data
	if kind == "input" {
		kind = attr(n, "type")
	}
	named := attr(n, "name") != ""
	if named && kind != "hidden" && (n.Data == "input" || n.Data == "select" || n.Data == "textarea") {
		if p.Inputs == nil {
			p.Inputs = map[string]string{}
		}
		p.Inputs[attr(n, "name")] = kind
	}
}

// readDisclosure reads whether a disclosure whose summary holds a second
// or third level heading is open.
func (p *Page) readDisclosure(details *html.Node) {
	for d := range details.Descendants() {
		if (d.Data == "h2" || d.Data == "h3") && within(d, "summary") {
			if p.Disclosures == nil {
				p.Disclosures = map[string]bool{}
			}
			p.Disclosures[text(d)] = hasAttr(details, "open")
			return
		}
	}
}

// readItem reads an item of a labelled list.
func (p *Page) readItem(li *html.Node) {
	label := attr(li.Parent, "aria-label")
	if label == "" {
		return
	}
	if p.Lists == nil {
		p.Lists = map[string][]string{}
	}
	p.Lists[label] = append(p.Lists[label], text(li))
}

// readLink reads where a labelled link goes.
func (p *Page) readLink(a *html.Node) {
	if p.LinkTargets == nil {
		p.LinkTargets = map[string]string{}
	}
	p.LinkTargets[attr(a, "aria-label")] = attr(a, "href")
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
	label := text(n)
	for a := n.Parent; a != nil; a = a.Parent {
		if attr(a, "role") == "group" {
			label = attr(a, "aria-label") + ": " + label
			break
		}
	}
	if attr(n, "aria-current") == "page" {
		return label + " (current)"
	}
	return label
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

// Confirmations maps the address each form on the page posts to, when
// the form asks before it is sent, to the label of its button and the
// question it asks, as "label: question".
func Confirmations(body string) map[string]string {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		panic(err)
	}
	found := map[string]string{}
	for n := range doc.Descendants() {
		if n.Type == html.ElementNode && n.Data == "form" && hasAttr(n, "hx-confirm") {
			found[attr(n, "action")] = buttonText(n) + ": " + attr(n, "hx-confirm")
		}
	}
	return found
}

// buttonText is the text of the first button in form.
func buttonText(form *html.Node) string {
	for d := range form.Descendants() {
		if d.Type == html.ElementNode && d.Data == "button" {
			return text(d)
		}
	}
	return ""
}

// Headings is the text of each second-level heading on the page, in the
// order the page shows them.
func Headings(body string) []string {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		panic(err)
	}
	var found []string
	for n := range doc.Descendants() {
		if n.Type == html.ElementNode && n.Data == "h2" {
			found = append(found, text(n))
		}
	}
	return found
}
