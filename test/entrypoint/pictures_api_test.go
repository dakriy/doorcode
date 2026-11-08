package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// tooBigPicture is the API's refusal of an upload over the size limit.
const tooBigPicture = `{"error":"The picture is larger than 10 MB. Choose a smaller file."}` + "\n"

// uploadPicture uploads content as a picture of the door with id through
// the API as c's user. Pictures are numbered from 1 across every door.
func uploadPicture(t *testing.T, c *testsupport.Browser, door string, content []byte) {
	t.Helper()
	got := c.Upload("/api/v1/doors/"+door+"/pictures", "picture", "door.png", content)
	if got.Status != 201 {
		t.Fatalf("upload answered %d, want 201:\n%s", got.Status, got.Body)
	}
}

// doorWithPictures is a browser signed in as the administrator of a site
// whose front door (1) has a wide picture (1) and a tall one (2).
func doorWithPictures(t *testing.T, f *testsupport.Factory) *testsupport.Browser {
	t.Helper()
	b := signedInAdmin(t, f)
	createDoor(t, b, frontDoor)
	uploadPicture(t, b, "1", testsupport.PNG(30, 20))
	uploadPicture(t, b, "1", testsupport.PNG(20, 30))
	return b
}

func TestUploadingAPictureAnswersWithIt(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, frontDoor)

	got := b.Upload("/api/v1/doors/1/pictures", "picture", "door.png", testsupport.PNG(30, 20))

	want := testsupport.Response{Status: 201, ContentType: jsonType, Body: `{"id":1,"primary":true,"width":30,"height":20,` +
		`"image_url":"/api/v1/doors/1/pictures/1","thumbnail_url":"/api/v1/doors/1/pictures/1/thumbnail","created_at":"2026-01-05T09:00:00Z"}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestDoorsFirstPictureIsPrimaryAndLaterOnesAreListedAfterIt(t *testing.T) {
	t.Parallel()
	b := doorWithPictures(t, testsupport.NewFactory())

	door := testsupport.JSONAt(b.Get("/api/v1/doors/1").Body, "/primary_picture_id")
	got := testsupport.JSON(b.Get("/api/v1/doors/1/pictures").Body)

	want := testsupport.JSON(`{"pictures":[` +
		`{"id":1,"primary":true,"width":30,"height":20,"image_url":"/api/v1/doors/1/pictures/1",` +
		`"thumbnail_url":"/api/v1/doors/1/pictures/1/thumbnail","created_at":"2026-01-05T09:00:00Z"},` +
		`{"id":2,"primary":false,"width":20,"height":30,"image_url":"/api/v1/doors/1/pictures/2",` +
		`"thumbnail_url":"/api/v1/doors/1/pictures/2/thumbnail","created_at":"2026-01-05T09:00:00Z"}]}`)
	if diff := cmp.Diff([]any{1.0, want}, []any{door, got}); diff != "" {
		t.Errorf("primary picture, pictures (-want +got):\n%s", diff)
	}
}

func TestPicturesAreServedAsJPEGThatBrowsersMayKeep(t *testing.T) {
	t.Parallel()
	b := doorWithPictures(t, testsupport.NewFactory())

	image := b.Get("/api/v1/doors/1/pictures/2")
	thumbnail := b.Get("/api/v1/doors/1/pictures/2/thumbnail")

	want := []any{200, "image/jpeg", "jpeg 20x30", 200, "image/jpeg", "jpeg 20x30"}
	got := []any{image.Status, image.ContentType, testsupport.ImageInfo(image.Body),
		thumbnail.Status, thumbnail.ContentType, testsupport.ImageInfo(thumbnail.Body)}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("image, thumbnail (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff("private, max-age=31536000, immutable", b.Header("/api/v1/doors/1/pictures/2", "Cache-Control")); diff != "" {
		t.Errorf("cache control (-want +got):\n%s", diff)
	}
}

func TestLargePicturesAreReducedKeepingTheirProportions(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		width, height int
		want          []string
	}{
		"a wide picture":           {3000, 3, []string{"jpeg 1600x2", "jpeg 320x1"}},
		"a tall picture":           {3, 3000, []string{"jpeg 2x1600", "jpeg 1x320"}},
		"a picture at the limit":   {1600, 4, []string{"jpeg 1600x4", "jpeg 320x1"}},
		"a thin picture":           {4000, 1, []string{"jpeg 1600x1", "jpeg 320x1"}},
		"a thumbnail at the limit": {320, 7, []string{"jpeg 320x7", "jpeg 320x7"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			createDoor(t, b, frontDoor)
			uploadPicture(t, b, "1", testsupport.PNG(c.width, c.height))

			got := []string{
				testsupport.ImageInfo(b.Get("/api/v1/doors/1/pictures/1").Body),
				testsupport.ImageInfo(b.Get("/api/v1/doors/1/pictures/1/thumbnail").Body),
			}

			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Errorf("image, thumbnail (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSettingTheSecondPictureAsPrimary(t *testing.T) {
	t.Parallel()
	b := doorWithPictures(t, testsupport.NewFactory())

	set := b.PostJSON("/api/v1/doors/1/pictures/2/primary", "")
	door := testsupport.JSONAt(b.Get("/api/v1/doors/1").Body, "/primary_picture_id")
	flags := []any{
		testsupport.JSONAt(b.Get("/api/v1/doors/1/pictures").Body, "/pictures/0/primary"),
		testsupport.JSONAt(b.Get("/api/v1/doors/1/pictures").Body, "/pictures/1/primary"),
	}

	if diff := cmp.Diff([]any{204, 2.0, []any{false, true}}, []any{set.Status, door, flags}); diff != "" {
		t.Errorf("status, primary picture, flags (-want +got):\n%s", diff)
	}
}

func TestDeletingThePrimaryPictureMakesTheOldestLeftPrimary(t *testing.T) {
	t.Parallel()
	b := doorWithPictures(t, testsupport.NewFactory())
	uploadPicture(t, b, "1", testsupport.PNG(10, 10))
	send(t, b, "POST", "/api/v1/doors/1/pictures/3/primary", "", 204)

	deleted := b.Delete("/api/v1/doors/1/pictures/3")
	door := testsupport.JSONAt(b.Get("/api/v1/doors/1").Body, "/primary_picture_id")
	gone := b.Get("/api/v1/doors/1/pictures/3")

	want := []any{204, 1.0, 404, `{"error":"the door has no picture with this id"}` + "\n"}
	if diff := cmp.Diff(want, []any{deleted.Status, door, gone.Status, gone.Body}); diff != "" {
		t.Errorf("delete, primary picture, read back (-want +got):\n%s", diff)
	}
}

func TestDeletingAnotherPictureKeepsThePrimaryOne(t *testing.T) {
	t.Parallel()
	b := doorWithPictures(t, testsupport.NewFactory())

	send(t, b, "DELETE", "/api/v1/doors/1/pictures/2", "", 204)

	if diff := cmp.Diff(1.0, testsupport.JSONAt(b.Get("/api/v1/doors/1").Body, "/primary_picture_id")); diff != "" {
		t.Errorf("primary picture (-want +got):\n%s", diff)
	}
}

func TestDeletingTheLastPictureLeavesNoPrimary(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, frontDoor)
	uploadPicture(t, b, "1", testsupport.PNG(10, 10))

	send(t, b, "DELETE", "/api/v1/doors/1/pictures/1", "", 204)
	got := b.Get("/api/v1/doors/1")

	want := []any{frontDoorJSON + "\n", `{"pictures":[]}` + "\n"}
	if diff := cmp.Diff(want, []any{got.Body, b.Get("/api/v1/doors/1/pictures").Body}); diff != "" {
		t.Errorf("door, pictures (-want +got):\n%s", diff)
	}
}

func TestPicturesOfAnotherDoorAreNotFound(t *testing.T) {
	t.Parallel()
	b := doorWithPictures(t, testsupport.NewFactory())
	createDoor(t, b, `{"name":"Back door"}`)
	cases := map[string]func() testsupport.Response{
		"image":       func() testsupport.Response { return b.Get("/api/v1/doors/2/pictures/1") },
		"thumbnail":   func() testsupport.Response { return b.Get("/api/v1/doors/2/pictures/1/thumbnail") },
		"set primary": func() testsupport.Response { return b.PostJSON("/api/v1/doors/2/pictures/1/primary", "") },
		"delete":      func() testsupport.Response { return b.Delete("/api/v1/doors/2/pictures/1") },
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			got := request()

			want := testsupport.Response{Status: 404, ContentType: jsonType, Body: `{"error":"the door has no picture with this id"}` + "\n"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPicturesOfADoorThatIsNotThereAreNotFound(t *testing.T) {
	t.Parallel()
	cases := map[string]func(b *testsupport.Browser) testsupport.Response{
		"list": func(b *testsupport.Browser) testsupport.Response { return b.Get("/api/v1/doors/9/pictures") },
		"upload": func(b *testsupport.Browser) testsupport.Response {
			return b.Upload("/api/v1/doors/9/pictures", "picture", "a.png", testsupport.PNG(2, 2))
		},
		"set primary": func(b *testsupport.Browser) testsupport.Response {
			return b.PostJSON("/api/v1/doors/9/pictures/1/primary", "")
		},
		"delete": func(b *testsupport.Browser) testsupport.Response { return b.Delete("/api/v1/doors/9/pictures/1") },
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			got := request(signedInAdmin(t, testsupport.NewFactory()))

			want := testsupport.Response{Status: 404, ContentType: jsonType, Body: `{"error":"no door has this id"}` + "\n"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestATooLargeUploadIsRefused(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, frontDoor)

	got := b.Upload("/api/v1/doors/1/pictures", "picture", "huge.png", testsupport.Padded(testsupport.PNG(4, 4), 10<<20+1))
	pictures := b.Get("/api/v1/doors/1/pictures").Body

	want := []any{testsupport.Response{Status: 413, ContentType: jsonType, Body: tooBigPicture}, `{"pictures":[]}` + "\n"}
	if diff := cmp.Diff(want, []any{got, pictures}); diff != "" {
		t.Errorf("response, pictures (-want +got):\n%s", diff)
	}
}

func TestAnUploadAtTheSizeLimitIsAccepted(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, frontDoor)

	got := b.Upload("/api/v1/doors/1/pictures", "picture", "big.png", testsupport.Padded(testsupport.PNG(4, 4), 10<<20), "note", "first")

	if diff := cmp.Diff(201, got.Status); diff != "" {
		t.Errorf("status (-want +got):\n%s\n%s", diff, got.Body)
	}
}

func TestUploadsThatAreNotPicturesAreRefused(t *testing.T) {
	t.Parallel()
	unreadable := "The file is not a JPEG, PNG, GIF, or WebP image that can be read."
	notUpload := "Send the picture as a multipart form with the file in the field picture."
	png := testsupport.PNG(8, 8)
	cases := map[string]struct {
		upload func(b *testsupport.Browser) testsupport.Response
		want   string
	}{
		"text": {func(b *testsupport.Browser) testsupport.Response {
			return b.Upload("/api/v1/doors/1/pictures", "picture", "notes.txt", []byte("not a picture"))
		}, unreadable},
		"a damaged image": {func(b *testsupport.Browser) testsupport.Response {
			return b.Upload("/api/v1/doors/1/pictures", "picture", "cut.png", png[:len(png)-20])
		}, unreadable},
		"an image at the pixel limit with no pixels": {func(b *testsupport.Browser) testsupport.Response {
			return b.Upload("/api/v1/doors/1/pictures", "picture", "empty.png", testsupport.PNGHeader(8000, 5000))
		}, unreadable},
		"an image over the pixel limit": {func(b *testsupport.Browser) testsupport.Response {
			return b.Upload("/api/v1/doors/1/pictures", "picture", "bomb.png", testsupport.PNGHeader(8000, 5001))
		}, "The picture has more than 40 million pixels. Choose a smaller one."},
		"a file in another field": {func(b *testsupport.Browser) testsupport.Response {
			return b.Upload("/api/v1/doors/1/pictures", "photo", "door.png", png)
		}, notUpload},
		"json": {func(b *testsupport.Browser) testsupport.Response {
			return b.PostJSON("/api/v1/doors/1/pictures", `{"picture":"door.png"}`)
		}, notUpload},
		"a broken form": {func(b *testsupport.Browser) testsupport.Response {
			return b.SendForm("/api/v1/doors/1/pictures", "multipart/form-data; boundary=x",
				"--x\r\nContent-Disposition: form-data; name=\"picture\"; filename=\"a.png\"\r\n\r\nunfinished")
		}, notUpload},
		"a form whose other fields are too large": {func(b *testsupport.Browser) testsupport.Response {
			return b.Upload("/api/v1/doors/1/pictures", "picture", "door.png", png, "padding", string(testsupport.Padded(nil, 64<<10+10<<20)))
		}, notUpload},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			createDoor(t, b, frontDoor)

			got := c.upload(b)

			want := testsupport.Response{Status: 400, ContentType: jsonType, Body: `{"error":"` + c.want + `"}` + "\n"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPicturesAnswerWithARequestIDWhenTheStoreFails(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		fail   func(s *testsupport.Store)
		method string
		target string
	}{
		"listing pictures":          {(*testsupport.Store).FailDoors, "GET", "/api/v1/doors/1/pictures"},
		"serving an image":          {(*testsupport.Store).FailDoors, "GET", "/api/v1/doors/1/pictures/1"},
		"setting the primary":       {(*testsupport.Store).FailDoorWrites, "POST", "/api/v1/doors/1/pictures/2/primary"},
		"deleting a picture":        {(*testsupport.Store).FailDoorWrites, "DELETE", "/api/v1/doors/1/pictures/2"},
		"promoting the oldest left": {nil, "DELETE", "/api/v1/doors/1/pictures/1"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := doorWithPictures(t, f)
			if c.fail == nil {
				f.StoreStub.FailPrimaryPictures()
			} else {
				c.fail(f.StoreStub)
			}

			got := b.Send(c.method, c.target, "")

			want := testsupport.Response{Status: 500, ContentType: jsonType, Body: `{"error":"internal error","request_id":"4243444546474849"}` + "\n"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUploadAnswersWithARequestIDWhenThePictureCannotBeStored(t *testing.T) {
	t.Parallel()
	cases := map[string]func(s *testsupport.Store){
		"storing the picture":       (*testsupport.Store).FailDoorWrites,
		"making it the primary one": (*testsupport.Store).FailPrimaryPictures,
	}
	for name, fail := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			createDoor(t, b, frontDoor)
			fail(f.StoreStub)

			got := b.Upload("/api/v1/doors/1/pictures", "picture", "door.png", testsupport.PNG(4, 4))

			want := testsupport.Response{Status: 500, ContentType: jsonType, Body: `{"error":"internal error","request_id":"4243444546474849"}` + "\n"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}
