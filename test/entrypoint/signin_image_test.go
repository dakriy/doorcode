package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// signInImageSrc is where the sign-in page shows a picture stored at the
// stub clock's time.
const signInImageSrc = "/api/v1/sign-in-image?v=1767603600"

func TestTheSignInPageShowsThePictureUploadedOnTheSettingsPage(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, b := adminSession(t, f)

	uploaded := b.Upload("/admin/settings/sign-in-image", "picture", "lobby.png", testsupport.PNG(3000, 1000))
	settings := testsupport.ParsePage(b.Get(uploaded.Location).Body)
	signIn := testsupport.ParsePage(testsupport.Get(h, "/sign-in").Body)
	anyone := testsupport.NewBrowser(h)

	want := []any{
		303, "/admin/settings?tab=look", []string{"The sign-in page shows the new picture."},
		[]testsupport.Image{{Src: signInImageSrc, Alt: "The sign-in page picture"}},
		[]testsupport.Image{{Src: signInImageSrc, Alt: ""}},
		"jpeg 1600x533", "public, max-age=31536000, immutable",
	}
	got := []any{
		uploaded.Status, uploaded.Location, settings.Messages, settings.Images, signIn.Images,
		testsupport.ImageInfo(anyone.Get(signInImageSrc).Body), anyone.Header(signInImageSrc, "Cache-Control"),
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("upload, where it goes, flash, settings images, sign-in images, image, caching (-want +got):\n%s", diff)
	}
}

func TestTheSignInPictureIsSetThroughTheAPIAndTurnedUpright(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	set := b.UploadWith("PUT", "/api/v1/sign-in-image", "picture", "lobby.jpg", testsupport.Oriented(6))

	if diff := cmp.Diff([]any{204, testsupport.Upright}, []any{set.Status, testsupport.Quadrants(b.Get("/api/v1/sign-in-image").Body)}); diff != "" {
		t.Errorf("status, image (-want +got):\n%s", diff)
	}
}

func TestUploadingTheSignInPictureWithPUTReplacesIt(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	b.UploadWith("PUT", "/api/v1/sign-in-image", "picture", "a.png", testsupport.PNG(30, 20))
	f.ClockStub.Set("2026-01-05T10:00:00Z")

	replaced := b.Send("PUT", "/api/v1/sign-in-image", "")
	b.UploadWith("PUT", "/api/v1/sign-in-image", "picture", "b.png", testsupport.PNG(20, 30))

	got := []any{replaced.Status, testsupport.ImageInfo(b.Get("/api/v1/sign-in-image").Body),
		testsupport.ParsePage(b.Get("/admin/settings?tab=look").Body).Images}
	want := []any{400, "jpeg 20x30", []testsupport.Image{{Src: "/api/v1/sign-in-image?v=1767607200", Alt: "The sign-in page picture"}}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("an empty body, the image, settings images (-want +got):\n%s", diff)
	}
}

func TestRemovingTheSignInPictureTakesItOffTheSignInPage(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, b := adminSession(t, f)
	b.Upload("/admin/settings/sign-in-image", "picture", "lobby.png", testsupport.PNG(30, 20))
	b.Get("/admin/settings")

	removed := b.Post("/admin/settings/sign-in-image/delete")
	settings := testsupport.ParsePage(b.Get(removed.Location).Body)
	again := b.Post("/admin/settings/sign-in-image/delete")

	want := []any{
		303, "/admin/settings?tab=look", []string{"The sign-in page shows no picture."}, []testsupport.Image(nil),
		[]testsupport.Image(nil), 404, 404,
		[]string{"The sign-in page shows no picture any more. It may have been removed already."},
		testsupport.JSON(`{"error":"the sign-in page has no picture"}`),
	}
	got := []any{
		removed.Status, removed.Location, settings.Messages, settings.Images, testsupport.ParsePage(testsupport.Get(h, "/sign-in").Body).Images,
		b.Get("/api/v1/sign-in-image").Status, again.Status, testsupport.ParsePage(again.Body).Messages,
		testsupport.JSON(b.Get("/api/v1/sign-in-image").Body),
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("removed, where it goes, flash, settings images, sign-in images, image, again, its alert, image body (-want +got):\n%s", diff)
	}
}

func TestDeletingTheSignInPictureThroughTheAPI(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	b.UploadWith("PUT", "/api/v1/sign-in-image", "picture", "lobby.png", testsupport.PNG(30, 20))

	got := []int{b.Delete("/api/v1/sign-in-image").Status, b.Delete("/api/v1/sign-in-image").Status, b.Get("/api/v1/sign-in-image").Status}

	if diff := cmp.Diff([]int{204, 404, 404}, got); diff != "" {
		t.Errorf("delete, again, read (-want +got):\n%s", diff)
	}
}

func TestARefusedSignInPictureSaysWhatIsWrong(t *testing.T) {
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

			page := b.Upload("/admin/settings/sign-in-image", "picture", "lobby.png", c.content)
			api := b.UploadWith("PUT", "/api/v1/sign-in-image", "picture", "lobby.png", c.content)

			want := []any{c.status, map[string]string{"picture": c.problem}, []testsupport.Image(nil), c.apiStatus}
			got := []any{page.Status, testsupport.ParsePage(page.Body).FieldErrors, testsupport.ParsePage(page.Body).Images, api.Status}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("page status, problems, images, API status (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheSignInPageFailsWhenItsPictureCannotBeRead(t *testing.T) {
	t.Parallel()
	pages := map[string]string{"sign-in": "/sign-in", "settings": "/admin/settings?tab=look"}
	for name, target := range pages {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			f.StoreStub.FailSignInImageReads()

			got := b.Get(target)

			if diff := cmp.Diff(500, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
		})
	}
}
