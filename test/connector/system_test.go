//go:build !slow

package connector_test

import (
	"bytes"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/testsupport"
)

func TestClockReportsTheCurrentTime(t *testing.T) {
	for _, impl := range clockImpls {
		t.Run(impl.name, func(t *testing.T) {
			clock := impl.clock()

			before := impl.reference()
			got := clock.Now()
			after := impl.reference()

			if got.Before(before) || got.After(after) {
				t.Errorf("Now() = %v, want between %v and %v", got, before, after)
			}
		})
	}
}

func TestEntropyReturnsTheRequestedNumberOfFreshBytes(t *testing.T) {
	for _, impl := range entropyImpls {
		t.Run(impl.name, func(t *testing.T) {
			source := impl.source()

			first := source.Bytes(16)
			second := source.Bytes(16)

			if diff := cmp.Diff([]int{16, 16}, []int{len(first), len(second)}); diff != "" {
				t.Errorf("lengths (-want +got):\n%s", diff)
			}
			if bytes.Equal(first, second) {
				t.Errorf("two reads returned the same bytes %x", first)
			}
		})
	}
}

func TestEnvironmentReportsASetVariableWithItsValue(t *testing.T) {
	for _, impl := range envImpls {
		t.Run(impl.name, func(t *testing.T) {
			env := impl.with(t, "DOORCODE_CONNECTOR_TEST", "on")

			value, ok := env.Lookup("DOORCODE_CONNECTOR_TEST")

			if diff := cmp.Diff([]any{"on", true}, []any{value, ok}); diff != "" {
				t.Errorf("lookup (-want +got):\n%s", diff)
			}
		})
	}
}

func TestEnvironmentReportsAnEmptyVariableAsSet(t *testing.T) {
	for _, impl := range envImpls {
		t.Run(impl.name, func(t *testing.T) {
			env := impl.with(t, "DOORCODE_CONNECTOR_TEST", "")

			value, ok := env.Lookup("DOORCODE_CONNECTOR_TEST")

			if diff := cmp.Diff([]any{"", true}, []any{value, ok}); diff != "" {
				t.Errorf("lookup (-want +got):\n%s", diff)
			}
		})
	}
}

func TestEnvironmentReportsAnUnsetVariableAsNotSet(t *testing.T) {
	for _, impl := range envImpls {
		t.Run(impl.name, func(t *testing.T) {
			env := impl.without(t, "DOORCODE_CONNECTOR_TEST")

			value, ok := env.Lookup("DOORCODE_CONNECTOR_TEST")

			if diff := cmp.Diff([]any{"", false}, []any{value, ok}); diff != "" {
				t.Errorf("lookup (-want +got):\n%s", diff)
			}
		})
	}
}

func TestLogRecordsLevelMessageAndFieldsInOrder(t *testing.T) {
	for _, impl := range logImpls {
		t.Run(impl.name, func(t *testing.T) {
			log, written := impl.newLog(t)

			log.Info("schema migrated")
			log.Error("request failed", connector.Field{Key: "request_id", Value: "ab12"}, connector.Field{Key: "error", Value: "boom"})

			want := []testsupport.LogEntry{
				{Level: "info", Message: "schema migrated"},
				{Level: "error", Message: "request failed", Fields: map[string]string{"request_id": "ab12", "error": "boom"}},
			}
			if diff := cmp.Diff(want, written()); diff != "" {
				t.Errorf("log (-want +got):\n%s", diff)
			}
		})
	}
}
