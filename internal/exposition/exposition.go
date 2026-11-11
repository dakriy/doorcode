// Package exposition writes metrics in the Prometheus text exposition
// format, version 0.0.4: each family's HELP and TYPE lines followed by its
// samples, one per line, in the order they are given.
package exposition

import (
	"slices"
	"strconv"
	"strings"
)

// ContentType is the media type of the text Text writes.
const ContentType = "text/plain; version=0.0.4; charset=utf-8"

// The types of a family.
const (
	Gauge     = "gauge"
	Counter   = "counter"
	Histogram = "histogram"
)

// Label is one label of a sample: its name and value.
type Label struct {
	Name  string
	Value string
}

// Sample is one value of a family, told apart from its others by its
// labels. Suffix follows the family's name in the sample's line, as
// _bucket, _sum, and _count do in a histogram's (see HistogramSamples).
type Sample struct {
	Suffix string
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
			b.WriteString(f.Name + s.Suffix + labels(s.Labels) + " " + number(s.Value) + "\n")
		}
	}
	return b.String()
}

// HistogramSamples is a histogram's samples, labelled labels: for each of
// bounds a bucket counting the inBucket observations no larger than it,
// the +Inf bucket counting all count of them, then their sum and count.
func HistogramSamples(labels []Label, bounds []float64, inBucket []int, sum float64, count int) []Sample {
	var out []Sample
	for i, bound := range bounds {
		out = append(out, bucket(labels, number(bound), inBucket[i]))
	}
	return append(out,
		bucket(labels, "+Inf", count),
		Sample{Suffix: "_sum", Labels: labels, Value: sum},
		Sample{Suffix: "_count", Labels: labels, Value: float64(count)})
}

// bucket is the sample of a histogram's bucket whose bound is le,
// labelled labels and le, holding count observations.
func bucket(labels []Label, le string, count int) Sample {
	withBound := append(slices.Clone(labels), Label{Name: "le", Value: le})
	return Sample{Suffix: "_bucket", Labels: withBound, Value: float64(count)}
}

// number is v as the text format writes a value or a bucket's bound.
func number(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
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
