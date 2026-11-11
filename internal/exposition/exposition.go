// Package exposition writes metrics in the Prometheus text exposition
// format, version 0.0.4: each family's HELP and TYPE lines followed by its
// samples, one per line, in the order they are given.
package exposition

import (
	"strconv"
	"strings"
)

// ContentType is the media type of the text Text writes.
const ContentType = "text/plain; version=0.0.4; charset=utf-8"

// The types of a family.
const (
	Gauge   = "gauge"
	Counter = "counter"
)

// Label is one label of a sample: its name and value.
type Label struct {
	Name  string
	Value string
}

// Sample is one value of a family, told apart from its others by its
// labels.
type Sample struct {
	Labels []Label
	Value  float64
}

// Family is a metric: its name, what it measures, its type, and its
// samples.
type Family struct {
	Name    string
	Help    string
	Type    string
	Samples []Sample
}

var (
	helpEscaper  = strings.NewReplacer(`\`, `\\`, "\n", `\n`)
	valueEscaper = strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`)
)

// Text is families in the text format, in the order given.
func Text(families []Family) string {
	var b strings.Builder
	for _, f := range families {
		b.WriteString("# HELP " + f.Name + " " + helpEscaper.Replace(f.Help) + "\n")
		b.WriteString("# TYPE " + f.Name + " " + f.Type + "\n")
		for _, s := range f.Samples {
			b.WriteString(f.Name + labels(s.Labels) + " " + strconv.FormatFloat(s.Value, 'f', -1, 64) + "\n")
		}
	}
	return b.String()
}

// labels is a sample's labels in braces, nothing when it has none.
func labels(ls []Label) string {
	if len(ls) == 0 {
		return ""
	}
	pairs := make([]string, 0, len(ls))
	for _, l := range ls {
		pairs = append(pairs, l.Name+`="`+valueEscaper.Replace(l.Value)+`"`)
	}
	return "{" + strings.Join(pairs, ",") + "}"
}
