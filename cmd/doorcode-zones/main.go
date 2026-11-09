// Command doorcode-zones writes the name of every zone in a tz database
// zip, such as the one Go ships at $GOROOT/lib/time/zoneinfo.zip, one per
// line in ascending byte order. `make zones` checks the list in as the one
// the tzdb driver embeds.
package main

import (
	"archive/zip"
	"log"
	"os"
	"slices"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: doorcode-zones <zoneinfo.zip>")
	}
	archive, err := zip.OpenReader(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	var names []string
	for _, f := range archive.File {
		names = append(names, f.Name+"\n")
	}
	slices.Sort(names)
	if _, err := os.Stdout.WriteString(strings.Join(names, "")); err != nil {
		log.Fatal(err)
	}
}
