package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// logoSrc is where pages show a logo stored at the stub clock's time.
const logoSrc = "/api/v1/logo?v=1767603600"

func TestTheHeaderAndTheSignInPageShowTheLogoUploadedOnTheSettingsPage(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, b := adminSession(t, f)

	uploaded := b.Upload("/admin/settings/logo", "picture", "logo.png", testsupport.PNG(1200, 400))
	settings := testsupport.ParsePage(b.Get(uploaded.Location).Body)
	signIn := testsupport.ParsePage(testsupport.Get(h, "/sign-in").Body)
	anyone := testsupport.NewBrowser(h)

	want := []any{
		303, "/admin/settings?tab=look", []string{"The site shows the new logo."},
		[]testsupport.Image{{Src: logoSrc, Alt: ""}, {Src: logoSrc, Alt: "The logo"}},
		[]testsupport.Image{{Src: logoSrc, Alt: ""}, {Src: logoSrc, Alt: ""}},
		"png 512x171", "image/png", "public, max-age=31536000, immutable",
	}
	got := []any{
		uploaded.Status, uploaded.Location, settings.Messages, settings.Images, signIn.Images,
		testsupport.ImageInfo(anyone.Get(logoSrc).Body), anyone.Get(logoSrc).ContentType, anyone.Header(logoSrc, "Cache-Control"),
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("upload, where it goes, flash, settings images, sign-in images, logo, type, caching (-want +got):\n%s", diff)
	}
}

func TestTheLogoKeepsItsTransparentParts(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	b.UploadWith("PUT", "/api/v1/logo", "picture", "logo.png", testsupport.PNG(40, 20))

	if diff := cmp.Diff("png 40x20 transparent other other other", testsupport.Quadrants(b.Get("/api/v1/logo").Body)); diff != "" {
		t.Errorf("logo (-want +got):\n%s", diff)
	}
}

func TestTheLogoIsSetThroughTheAPIAndTurnedUpright(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	set := b.UploadWith("PUT", "/api/v1/logo", "picture", "logo.jpg", testsupport.Oriented(6))

	want := []any{204, "png 64x32 red green blue white", []testsupport.Image{{Src: logoSrc, Alt: ""}}}
	got := []any{set.Status, testsupport.Quadrants(b.Get("/api/v1/logo").Body), testsupport.ParsePage(b.Get("/").Body).Images}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("status, logo, header images (-want +got):\n%s", diff)
	}
}

func TestUploadingTheLogoWithPUTReplacesIt(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	b.UploadWith("PUT", "/api/v1/logo", "picture", "a.png", testsupport.PNG(30, 20))
	f.ClockStub.Set("2026-01-05T10:00:00Z")

	replaced := b.Send("PUT", "/api/v1/logo", "")
	b.UploadWith("PUT", "/api/v1/logo", "picture", "b.png", testsupport.PNG(20, 30))

	got := []any{replaced.Status, testsupport.ImageInfo(b.Get("/api/v1/logo").Body), testsupport.ParsePage(b.Get("/").Body).Images}
	want := []any{400, "png 20x30", []testsupport.Image{{Src: "/api/v1/logo?v=1767607200", Alt: ""}}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("an empty body, the logo, header images (-want +got):\n%s", diff)
	}
}

func TestRemovingTheLogoOnTheSettingsPageBringsBackTheKeypadMark(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, b := adminSession(t, f)
	b.Upload("/admin/settings/logo", "picture", "logo.png", testsupport.PNG(30, 20))
	b.Get("/admin/settings")

	removed := b.Post("/admin/settings/logo/delete")
	settings := testsupport.ParsePage(b.Get(removed.Location).Body)
	again := b.Post("/admin/settings/logo/delete")

	want := []any{
		303, "/admin/settings?tab=look", []string{"The site shows the keypad mark in place of a logo."}, []testsupport.Image(nil),
		[]testsupport.Image(nil), 404,
		[]string{"The site has no logo any more. It may have been removed already."},
		testsupport.JSON(`{"error":"the site has no logo"}`),
	}
	got := []any{
		removed.Status, removed.Location, settings.Messages, settings.Images, testsupport.ParsePage(testsupport.Get(h, "/sign-in").Body).Images,
		again.Status, testsupport.ParsePage(again.Body).Messages, testsupport.JSON(b.Get("/api/v1/logo").Body),
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("removed, where it goes, flash, settings images, sign-in images, again, its alert, logo body (-want +got):\n%s", diff)
	}
}

func TestDeletingTheLogoThroughTheAPI(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	b.UploadWith("PUT", "/api/v1/logo", "picture", "logo.png", testsupport.PNG(30, 20))

	got := []int{b.Delete("/api/v1/logo").Status, b.Delete("/api/v1/logo").Status, b.Get("/api/v1/logo").Status}

	if diff := cmp.Diff([]int{204, 404, 404}, got); diff != "" {
		t.Errorf("delete, again, read (-want +got):\n%s", diff)
	}
}

func TestARefusedLogoSaysWhatIsWrong(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		content   []byte
		status    int
		problem   string
		apiStatus int
	}{
		"not an image":    {[]byte("not an image"), 422, "The file is not a JPEG, PNG, GIF, or WebP image that can be read.", 400},
		"too large":       {testsupport.Padded(testsupport.PNG(2, 2), 10_485_761), 413, "The picture is larger than 10 MB. Choose a smaller file.", 413},
		"too many pixels": {testsupport.PNGHeader(8000, 6000), 422, "The picture has more than 40 million pixels. Choose a smaller one.", 400},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := signedInAdmin(t, testsupport.NewFactory())
			b.Get("/")

			page := b.Upload("/admin/settings/logo", "picture", "logo.png", c.content)
			api := b.UploadWith("PUT", "/api/v1/logo", "picture", "logo.png", c.content)

			want := []any{c.status, map[string]string{"picture": c.problem}, []testsupport.Image(nil), c.apiStatus}
			got := []any{page.Status, testsupport.ParsePage(page.Body).FieldErrors, testsupport.ParsePage(page.Body).Images, api.Status}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("page status, problems, images, API status (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPagesAndTheLogoFailWhenTheLogoCannotBeRead(t *testing.T) {
	t.Parallel()
	targets := map[string]string{"shell": "/", "sign-in": "/sign-in", "logo": "/api/v1/logo"}
	for name, target := range targets {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			f.StoreStub.FailLogoReads()

			got := b.Get(target)

			if diff := cmp.Diff(500, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
		})
	}
}
