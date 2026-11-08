package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/app"
	"github.com/dakriy/doorcode/internal/testsupport"
)

func check(f testsupport.Factory, code string) testsupport.Response {
	return testsupport.PostForm(app.Handler(f), "/api/check", map[string]string{"code": code})
}

func TestKnownCodeOpensTheDoor(t *testing.T) {
	got := check(testsupport.WithCodes("1111\n 2468 \n"), "2468")

	want := testsupport.Response{Status: 200, Body: "open\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestUnknownCodeIsDenied(t *testing.T) {
	got := check(testsupport.WithCodes("1111\n2468\n"), "1357")

	want := testsupport.Response{Status: 403, Body: "denied\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestEmptyCodeIsDeniedEvenWhenTheListHasABlankLine(t *testing.T) {
	got := check(testsupport.WithCodes("1111\n\n2468\n"), "")

	want := testsupport.Response{Status: 403, Body: "denied\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestUnreadableCodeListMakesTheServiceUnavailable(t *testing.T) {
	got := check(testsupport.WithUnavailableCodes(), "2468")

	want := testsupport.Response{Status: 503, Body: "unavailable\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}
