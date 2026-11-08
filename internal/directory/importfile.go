package directory

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/dakriy/doorcode/internal/check"
)

// The columns of an import file. Every file has ColumnUserID and
// ColumnGroups; the others are optional and used only to create users.
const (
	ColumnUserID    = "user_id"
	ColumnGroups    = "groups"
	ColumnEmail     = "email"
	ColumnFirstName = "first_name"
	ColumnLastName  = "last_name"
	ColumnExpiresAt = "expires_at"
)

const (
	// MaxImportKiB and MaxImportBytes are the largest import file, and
	// MaxImportRows the most rows below its header.
	MaxImportKiB   = 1024
	MaxImportBytes = 1_048_576
	MaxImportRows  = 5000
	// ImportFileField is the problem field of an import file that cannot
	// be read at all.
	ImportFileField = "file"
	// GroupSeparator separates the slugs in the groups column.
	GroupSeparator = ";"
)

// byteOrderMark starts files some spreadsheets save as UTF-8.
const byteOrderMark = "\uFEFF"

// importColumns is every column an import file may have, in the order
// they are described.
var importColumns = []string{ColumnUserID, ColumnGroups, ColumnEmail, ColumnFirstName, ColumnLastName, ColumnExpiresAt}

// ErrImportTooBig reports an import file over MaxImportBytes.
var ErrImportTooBig = fmt.Errorf("the file is larger than %d KiB", MaxImportKiB)

// ImportColumnsHint says what columns an import file has.
const ImportColumnsHint = "The first line names the columns: user_id and groups, and optionally email, first_name, last_name, and expires_at."

// record is one row of an import file: the file's line it starts on, its
// value under each column the row reaches, and what is wrong with its
// shape.
type record struct {
	line     int
	values   map[string]string
	problems check.List
}

// fileProblem is message as the problem with a whole import file.
func fileProblem(message string) error {
	return check.Problems{{Field: ImportFileField, Message: message}}
}

// readImport reads the rows of an import file, or reports what keeps the
// whole file from being read.
func readImport(file []byte) ([]record, error) {
	if len(file) > MaxImportBytes {
		return nil, ErrImportTooBig
	}
	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(file, []byte(byteOrderMark))))
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true
	header, err := reader.Read()
	if errors.Is(err, io.EOF) {
		return nil, fileProblem("The file is empty. " + ImportColumnsHint)
	}
	if err != nil {
		return nil, unreadable(err)
	}
	columns, err := readHeader(header)
	if err != nil {
		return nil, err
	}
	return readRecords(reader, columns)
}

// unreadable is the problem with a file that is not CSV, as err says.
func unreadable(err error) error {
	return fileProblem("The file is not valid CSV: " + err.Error() + ".")
}

// readHeader returns the columns header names, lowercase, or what is wrong
// with them.
func readHeader(header []string) ([]string, error) {
	columns := []string{}
	for _, name := range header {
		name = strings.ToLower(strings.TrimSpace(name))
		if !slices.Contains(importColumns, name) {
			return nil, fileProblem(fmt.Sprintf("The header names a column “%s” that imports do not have. %s", name, ImportColumnsHint))
		}
		if slices.Contains(columns, name) {
			return nil, fileProblem(fmt.Sprintf("The header names the column %s twice.", name))
		}
		columns = append(columns, name)
	}
	for _, required := range []string{ColumnUserID, ColumnGroups} {
		if !slices.Contains(columns, required) {
			return nil, fileProblem(fmt.Sprintf("The header has no %s column. %s", required, ImportColumnsHint))
		}
	}
	return columns, nil
}

// readRecords reads every row below the header, whose columns are columns.
func readRecords(reader *csv.Reader, columns []string) ([]record, error) {
	var records []record
	for fields, err := reader.Read(); !errors.Is(err, io.EOF); fields, err = reader.Read() {
		if err != nil {
			return nil, unreadable(err)
		}
		if len(records) == MaxImportRows {
			return nil, fileProblem(fmt.Sprintf("The file has more than %d rows. Split it and import each part.", MaxImportRows))
		}
		line, _ := reader.FieldPos(0)
		records = append(records, recordOf(line, fields, columns))
	}
	if len(records) == 0 {
		return nil, fileProblem("The file has no rows below its header, so there is nothing to import.")
	}
	return records, nil
}

// recordOf is the row on line with fields under columns.
func recordOf(line int, fields, columns []string) record {
	rec := record{line: line, values: map[string]string{}}
	for i, value := range fields[:min(len(fields), len(columns))] {
		rec.values[columns[i]] = strings.TrimSpace(value)
	}
	if len(fields) < len(columns) {
		rec.problems.Add(columns[len(fields)], fmt.Sprintf("The row ends before the %s column.", columns[len(fields)]))
	}
	if len(fields) > len(columns) {
		rec.problems.Add("", "The row has more values than the header has columns.")
	}
	return rec
}

// slugsOf is the group slugs a groups value lists, trimmed, without blanks
// or repeats, in order.
func slugsOf(value string) []string {
	slugs := []string{}
	for _, slug := range strings.Split(value, GroupSeparator) {
		slug = strings.TrimSpace(slug)
		if slug != "" && !slices.Contains(slugs, slug) {
			slugs = append(slugs, slug)
		}
	}
	return slugs
}
