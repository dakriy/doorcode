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

func TestTheFaviconIsTheUploadedFaviconThenTheLogoThenTheKeypadMark(t *testing.T) {
	t.Parallel()
	h, b := adminSession(t, testsupport.NewFactory())
	b.UploadWith("PUT", "/api/v1/logo", "picture", "logo.png", testsupport.PNG(40, 20))
	logo := testsupport.Get(h, "/favicon")
	b.UploadWith("PUT", "/api/v1/favicon", "picture", "favicon.png", testsupport.PNG(16, 16))
	favicon := testsupport.Get(h, "/favicon")
	b.Delete("/api/v1/favicon")
	b.Delete("/api/v1/logo")
	mark := testsupport.Get(h, "/favicon")

	want := []string{"image/png", "png 40x20", "image/png", "png 16x16", "image/svg+xml"}
	got := []string{logo.ContentType, testsupport.ImageInfo(logo.Body), favicon.ContentType, testsupport.ImageInfo(favicon.Body), mark.ContentType}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("with a logo, with a favicon too, with neither (-want +got):\n%s", diff)
	}
}

func TestTheLookAndFeelTabShowsTheFaviconUploadedOnIt(t *testing.T) {
	t.Parallel()
	h, b := adminSession(t, testsupport.NewFactory())

	uploaded := b.Upload("/admin/settings/favicon", "favicon", "favicon.png", testsupport.PNG(1024, 1024))
	settings := testsupport.ParsePage(b.Get(uploaded.Location).Body)

	want := []any{
		303, "/admin/settings?tab=look", []string{"Browsers show the new favicon."},
		[]testsupport.Image{{Src: "/api/v1/favicon", Alt: "The favicon"}},
		"png 512x512", "image/png", "png 512x512",
	}
	got := []any{
		uploaded.Status, uploaded.Location, settings.Messages, settings.Images,
		testsupport.ImageInfo(testsupport.Get(h, "/api/v1/favicon").Body), testsupport.Get(h, "/api/v1/favicon").ContentType,
		testsupport.ImageInfo(testsupport.Get(h, "/favicon").Body),
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("upload, where it goes, flash, settings images, favicon, its type, what pages name (-want +got):\n%s", diff)
	}
}

func TestTheFaviconKeepsItsTransparentParts(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	b.UploadWith("PUT", "/api/v1/favicon", "picture", "favicon.png", testsupport.PNG(40, 20))

	if diff := cmp.Diff("png 40x20 transparent other other other", testsupport.Quadrants(b.Get("/api/v1/favicon").Body)); diff != "" {
		t.Errorf("favicon (-want +got):\n%s", diff)
	}
}

func TestTheFaviconIsSetThroughTheAPIAndTurnedUpright(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	set := b.UploadWith("PUT", "/api/v1/favicon", "picture", "favicon.jpg", testsupport.Oriented(6))

	if diff := cmp.Diff([]any{204, "png 64x32 red green blue white"}, []any{set.Status, testsupport.Quadrants(b.Get("/favicon").Body)}); diff != "" {
		t.Errorf("status, favicon (-want +got):\n%s", diff)
	}
}

func TestUploadingTheFaviconWithPUTReplacesItAtTheSameAddress(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	b.UploadWith("PUT", "/api/v1/favicon", "picture", "a.png", testsupport.PNG(30, 20))
	f.ClockStub.Set("2026-01-05T10:00:00Z")

	replaced := b.Send("PUT", "/api/v1/favicon", "")
	b.UploadWith("PUT", "/api/v1/favicon", "picture", "b.png", testsupport.PNG(20, 30))

	got := []any{replaced.Status, testsupport.ImageInfo(b.Get("/api/v1/favicon").Body), testsupport.ParsePage(b.Get("/admin/settings?tab=look").Body).Images}
	want := []any{400, "png 20x30", []testsupport.Image{{Src: "/api/v1/favicon", Alt: "The favicon"}}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("an empty body, the favicon, settings images (-want +got):\n%s", diff)
	}
}

func TestRemovingTheFaviconOnTheSettingsPageBringsBackTheLogo(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	b.UploadWith("PUT", "/api/v1/logo", "picture", "logo.png", testsupport.PNG(40, 20))
	b.Upload("/admin/settings/favicon", "favicon", "favicon.png", testsupport.PNG(16, 16))
	b.Get("/admin/settings")

	removed := b.Post("/admin/settings/favicon/delete")
	settings := testsupport.ParsePage(b.Get(removed.Location).Body)
	again := b.Post("/admin/settings/favicon/delete")

	want := []any{
		303, "/admin/settings?tab=look",
		[]string{"Browsers show the logo, or the keypad mark when the site has no logo, in place of a favicon."},
		[]testsupport.Image{{Src: logoSrc, Alt: ""}, {Src: logoSrc, Alt: "The logo"}}, "png 40x20", 404,
		[]string{"The site has no favicon any more. It may have been removed already."},
		testsupport.JSON(`{"error":"the site has no favicon"}`),
	}
	got := []any{
		removed.Status, removed.Location, settings.Messages, settings.Images, testsupport.ImageInfo(b.Get("/favicon").Body),
		again.Status, testsupport.ParsePage(again.Body).Messages, testsupport.JSON(b.Get("/api/v1/favicon").Body),
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("removed, where it goes, flash, settings images, favicon, again, its alert, favicon body (-want +got):\n%s", diff)
	}
}

func TestDeletingTheFaviconThroughTheAPI(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	b.UploadWith("PUT", "/api/v1/favicon", "picture", "favicon.png", testsupport.PNG(30, 20))

	got := []int{b.Delete("/api/v1/favicon").Status, b.Delete("/api/v1/favicon").Status, b.Get("/api/v1/favicon").Status}

	if diff := cmp.Diff([]int{204, 404, 404}, got); diff != "" {
		t.Errorf("delete, again, read (-want +got):\n%s", diff)
	}
}

func TestARefusedFaviconSaysWhatIsWrongOnItsOwnCard(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		content   []byte
		status    int
		problem   string
		apiStatus int
	}{
		"not an image": {[]byte("not an image"), 422, "The file is not a JPEG, PNG, GIF, or WebP image that can be read.", 400},
		"too large":    {testsupport.Padded(testsupport.PNG(2, 2), 10_485_761), 413, "The picture is larger than 10 MB. Choose a smaller file.", 413},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := signedInAdmin(t, testsupport.NewFactory())
			b.Get("/")

			page := b.Upload("/admin/settings/favicon", "favicon", "favicon.png", c.content)
			api := b.UploadWith("PUT", "/api/v1/favicon", "picture", "favicon.png", c.content)

			want := []any{c.status, map[string]string{"favicon": c.problem}, []testsupport.Image(nil), c.apiStatus}
			got := []any{page.Status, testsupport.ParsePage(page.Body).FieldErrors, testsupport.ParsePage(page.Body).Images, api.Status}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("page status, problems, images, API status (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheFaviconAndTheLookAndFeelTabFailWhenTheFaviconCannotBeRead(t *testing.T) {
	t.Parallel()
	targets := map[string]string{"favicon": "/favicon", "API": "/api/v1/favicon", "settings": "/admin/settings?tab=look"}
	for name, target := range targets {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			f.StoreStub.FailFaviconReads()

			got := b.Get(target)

			if diff := cmp.Diff(500, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheFaviconFailsWhenItFallsBackToALogoThatCannotBeRead(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	f.StoreStub.FailLogoReads()

	got := b.Get("/favicon")

	if diff := cmp.Diff(500, got.Status); diff != "" {
		t.Errorf("status (-want +got):\n%s", diff)
	}
}

func TestOnlyTheSignInPictureAndTheLogoAreAddressedByWhenTheyWereStored(t *testing.T) {
	t.Parallel()
	doc := openAPIDocument(t)
	version := `[{"name": "v", "in": "query", "required": false, "description": "Ignored. Pages add when the picture was stored, so browsers fetch a new picture at a new address.", "schema": {"type": "string"}, "example": "1767603600"}]`

	got := []any{
		testsupport.JSONAt(doc, "/paths/~1sign-in-image/get/parameters"),
		testsupport.JSONAt(doc, "/paths/~1logo/get/parameters"),
		testsupport.JSONAt(doc, "/paths/~1favicon/get"),
	}

	want := []any{testsupport.JSON(version), testsupport.JSON(version), testsupport.JSON(`{
		"operationId": "getFavicon", "summary": "Read the site's favicon", "tags": ["settings"], "security": [],
		"responses": {
			"200": {"description": "The favicon, as PNG.", "content": {"image/png": {"schema": {"type": "string", "format": "binary"}}}},
			"404": {"description": "The site has no favicon.", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/ErrorBody"}}}},
			"500": {"description": "The server failed; the body names a request id to give an administrator.", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/ErrorBody"}}}}
		}
	}`)}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("sign-in picture's parameters, logo's parameters, favicon's operation (-want +got):\n%s", diff)
	}
}
