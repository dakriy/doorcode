package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

func TestEveryPageNamesTheFavicon(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, b := adminSession(t, f)
	signIn := testsupport.ParsePage(testsupport.Get(h, "/sign-in").Body).Icon
	home := testsupport.ParsePage(b.Get("/").Body).Icon
	f.StoreStub.FailSettings()
	failed := testsupport.ParsePage(b.Get("/admin/settings").Body).Icon

	if diff := cmp.Diff([]string{"/favicon", "/favicon", "/favicon"}, []string{signIn, home, failed}); diff != "" {
		t.Errorf("sign-in page, home page, error page (-want +got):\n%s", diff)
	}
}

func TestTheFaviconIsTheKeypadMarkInTheDefaultAccentColor(t *testing.T) {
	t.Parallel()
	got := testsupport.Get(start(t, testsupport.NewFactory()).Handler(), "/favicon")

	want := testsupport.Response{Status: 200, ContentType: "image/svg+xml", Body: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">
  <style>.accent { fill: #4f39f6; }</style>
  <rect width="32" height="32" rx="8" class="accent"/>
  <g fill="#fff">
    <circle cx="10" cy="9" r="2"/><circle cx="16" cy="9" r="2"/><circle cx="22" cy="9" r="2"/>
    <circle cx="10" cy="15" r="2"/><circle cx="16" cy="15" r="2"/><circle cx="22" cy="15" r="2"/>
    <circle cx="10" cy="21" r="2"/><circle cx="16" cy="21" r="2"/><circle cx="22" cy="21" r="2"/>
  </g>
</svg>`}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestTheFaviconIsDrawnInTheAccentColorsTheSettingsChoose(t *testing.T) {
	t.Parallel()
	h, b := adminSession(t, testsupport.NewFactory())
	send(t, b, "PUT", "/api/v1/settings/theme_accent", `{"value":"#0F766E"}`, 200)
	light := testsupport.ParsePage(testsupport.Get(h, "/favicon").Body).Styles
	send(t, b, "PUT", "/api/v1/settings/theme_accent_dark", `{"value":"#2dd4bf"}`, 200)
	both := testsupport.ParsePage(testsupport.Get(h, "/favicon").Body).Styles

	want := []any{
		[]string{".accent { fill: #0f766e; }"},
		[]string{".accent { fill: #0f766e; } @media (prefers-color-scheme: dark) { .accent { fill: #2dd4bf; } }"},
	}
	if diff := cmp.Diff(want, []any{light, both}); diff != "" {
		t.Errorf("accent, accent and dark accent (-want +got):\n%s", diff)
	}
}
