package connector_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/driver/cryptoentropy"
	"github.com/dakriy/doorcode/internal/driver/dnsresolver"
	"github.com/dakriy/doorcode/internal/driver/osenv"
	"github.com/dakriy/doorcode/internal/driver/slogjson"
	"github.com/dakriy/doorcode/internal/driver/systemclock"
	"github.com/dakriy/doorcode/internal/driver/tzdb"
	"github.com/dakriy/doorcode/internal/testsupport"
)

// clockImpl pairs a clock with the time it should agree with.
type clockImpl struct {
	name      string
	clock     func() connector.Clock
	reference func() time.Time
}

var clockImpls = []clockImpl{
	{
		name: "stub",
		clock: func() connector.Clock {
			return testsupport.NewFactory().ClockStub
		},
		reference: func() time.Time { return testsupport.NewFactory().ClockStub.Now() },
	},
	{
		name:      "system",
		clock:     func() connector.Clock { return systemclock.Clock{} },
		reference: time.Now,
	},
}

type entropyImpl struct {
	name   string
	source func() connector.Entropy
}

var entropyImpls = []entropyImpl{
	{name: "stub", source: func() connector.Entropy { return testsupport.NewFactory().EntropyStub }},
	{name: "crypto", source: func() connector.Entropy { return cryptoentropy.Source{} }},
}

type zonesImpl struct {
	name  string
	zones func() connector.TimeZones
}

var zonesImpls = []zonesImpl{
	{name: "stub", zones: func() connector.TimeZones { return testsupport.NewFactory().ZonesStub }},
	{name: "tzdb", zones: func() connector.TimeZones { return tzdb.Zones{} }},
}

// resolverImpls are the resolvers, the stub told what the host's own
// resolver says of localhost.
var resolverImpls = []struct {
	name     string
	resolver func() connector.Resolver
}{
	{name: "stub", resolver: func() connector.Resolver {
		r := &testsupport.Resolver{}
		r.Resolve("localhost", "127.0.0.1", "::1")
		return r
	}},
	{name: "dns", resolver: func() connector.Resolver { return dnsresolver.Resolver{} }},
}

// envImpl builds an environment where name is set to value, or is unset.
type envImpl struct {
	name    string
	with    func(t *testing.T, name, value string) connector.Environment
	without func(t *testing.T, name string) connector.Environment
}

var envImpls = []envImpl{
	{
		name: "stub",
		with: func(_ *testing.T, name, value string) connector.Environment {
			return testsupport.Environment{name: value}
		},
		without: func(*testing.T, string) connector.Environment {
			return testsupport.Environment{}
		},
	},
	{
		name: "os",
		with: func(t *testing.T, name, value string) connector.Environment {
			t.Setenv(name, value)
			return osenv.Environment{}
		},
		without: func(t *testing.T, name string) connector.Environment {
			t.Setenv(name, "")
			if err := os.Unsetenv(name); err != nil {
				t.Fatal(err)
			}
			return osenv.Environment{}
		},
	},
}

// logImpl builds a log and a reader of what it has written so far.
type logImpl struct {
	name   string
	newLog func(t *testing.T) (connector.Log, func() []testsupport.LogEntry)
}

var logImpls = []logImpl{
	{
		name: "stub",
		newLog: func(*testing.T) (connector.Log, func() []testsupport.LogEntry) {
			l := &testsupport.Log{}
			return l, func() []testsupport.LogEntry { return l.Entries }
		},
	},
	{
		name: "slog json",
		newLog: func(t *testing.T) (connector.Log, func() []testsupport.LogEntry) {
			var buf bytes.Buffer
			return slogjson.New(&buf), func() []testsupport.LogEntry { return parseJSONLog(t, buf.String()) }
		},
	},
}

// parseJSONLog reads slog JSON lines back as entries, dropping the timestamp.
func parseJSONLog(t *testing.T, out string) []testsupport.LogEntry {
	t.Helper()
	var entries []testsupport.LogEntry
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var record map[string]string
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		entry := testsupport.LogEntry{Level: strings.ToLower(record["level"]), Message: record["msg"]}
		delete(record, "time")
		delete(record, "level")
		delete(record, "msg")
		if len(record) > 0 {
			entry.Fields = record
		}
		entries = append(entries, entry)
	}
	return entries
}
