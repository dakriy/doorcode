package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/check"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/door"
	"github.com/dakriy/doorcode/internal/openapi"
	"github.com/dakriy/doorcode/internal/picture"
)

// uploadOverhead is how much an upload's body may hold beyond the picture:
// the multipart boundaries and part headers.
const uploadOverhead = 65_536 // 64 KiB

// pictureCache lets browsers keep a picture: its id is never reused and its
// image never changes, and it is only for people allowed to see it.
const pictureCache = "private, max-age=31536000, immutable"

type pictureView struct {
	ID           int64     `json:"id" doc:"Names the picture in the API."`
	Primary      bool      `json:"primary" doc:"True for the door's primary picture, which lists and the user portal show."`
	Width        int       `json:"width" doc:"The image's width in pixels."`
	Height       int       `json:"height" doc:"The image's height in pixels."`
	ImageURL     string    `json:"image_url" doc:"Where the image is served, as JPEG."`
	ThumbnailURL string    `json:"thumbnail_url" doc:"Where a thumbnail of the image is served, as JPEG."`
	CreatedAt    time.Time `json:"created_at" doc:"When the picture was uploaded."`
}

type pictureList struct {
	Pictures []pictureView `json:"pictures" doc:"The door's pictures, oldest first."`
}

type pictureUpload struct {
	Picture openapi.File `json:"picture" doc:"The image: JPEG, PNG, GIF, or WebP."`
}

var (
	pictureParams = []openapi.Param{doorIDParam, {Name: "picture", In: "path", Description: "The picture's id.", Example: int64(1)}}
	noPicture     = openapi.Response{Status: http.StatusNotFound, Description: "No door has this id, or the door has no picture with this id.", Body: errorBody{}}
)

func (a *App) pictureRoutes() []route {
	doors := holding(auth.RoleManageDoors)
	image := func(what string) []openapi.Response {
		return []openapi.Response{
			{Status: http.StatusOK, Description: what + " Anyone signed in may read the pictures of a door the portal shows; other doors' need the manage-doors role.", Body: openapi.File{}, Type: picture.ContentType},
			{Status: http.StatusForbidden, Description: "The door is not one the portal shows, and the caller does not hold the manage-doors role.", Body: errorBody{}},
			noPicture,
		}
	}
	return []route{
		apiRoute(http.MethodGet, "/doors/{id}/pictures", doors, a.listPictures, apiDoc{
			id: "listDoorPictures", summary: "List a door's pictures", tag: "doors", params: []openapi.Param{doorIDParam},
			responses: []openapi.Response{{Status: http.StatusOK, Description: "The door's pictures.", Body: pictureList{}}, noDoor},
		}),
		apiRoute(http.MethodPost, "/doors/{id}/pictures", doors, a.uploadPicture, apiDoc{
			id: "uploadDoorPicture", summary: "Add a picture to a door", tag: "doors", params: []openapi.Param{doorIDParam}, form: pictureUpload{},
			responses: []openapi.Response{
				{Status: http.StatusCreated, Description: fmt.Sprintf("The new picture, reduced to at most %d pixels on its longest side. A door's first picture becomes its primary one.", picture.MaxSide), Body: pictureView{}},
				{Status: http.StatusBadRequest, Description: "The body is not a multipart form with the file in the field picture, or the file is not an image that can be read.", Body: errorBody{}},
				noDoor,
				{Status: http.StatusRequestEntityTooLarge, Description: fmt.Sprintf("The file is larger than %d MB.", picture.Megabytes()), Body: errorBody{}},
			},
		}),
		apiRoute(http.MethodGet, "/doors/{id}/pictures/{picture}", signedInUser, a.servePicture(a.doors.Image), apiDoc{
			id: "getDoorPicture", summary: "Read a picture's image", tag: "doors", params: pictureParams,
			responses: image("The image."),
		}),
		apiRoute(http.MethodGet, "/doors/{id}/pictures/{picture}/thumbnail", signedInUser, a.servePicture(a.doors.Thumbnail), apiDoc{
			id: "getDoorPictureThumbnail", summary: "Read a picture's thumbnail", tag: "doors", params: pictureParams,
			responses: image(fmt.Sprintf("The image reduced to at most %d pixels on its longest side.", picture.ThumbnailSide)),
		}),
		apiRoute(http.MethodPost, "/doors/{id}/pictures/{picture}/primary", doors, a.makePrimary, apiDoc{
			id: "setPrimaryDoorPicture", summary: "Make a picture the door's primary one", tag: "doors", params: pictureParams,
			responses: []openapi.Response{{Status: http.StatusNoContent, Description: "The picture is the door's primary picture."}, noPicture},
		}),
		apiRoute(http.MethodDelete, "/doors/{id}/pictures/{picture}", doors, a.deletePicture, apiDoc{
			id: "deleteDoorPicture", summary: "Delete a picture", tag: "doors", params: pictureParams,
			responses: []openapi.Response{
				{Status: http.StatusNoContent, Description: "The picture is gone. When it was the primary picture, the oldest one left becomes primary."}, noPicture,
			},
		}),
	}
}

func viewOfPicture(d connector.StoredDoor, p connector.StoredPicture) pictureView {
	base := picturePath(d.ID, p.ID)
	return pictureView{
		ID: p.ID, Primary: door.IsPrimary(d, p.ID), Width: p.Width, Height: p.Height,
		ImageURL: base, ThumbnailURL: base + "/thumbnail", CreatedAt: p.CreatedAt.UTC(),
	}
}

// picturePath is where the image of the picture with id, of the door with
// doorID, is served.
func picturePath(doorID, id int64) string {
	return fmt.Sprintf("%s/doors/%d/pictures/%d", apiPrefix, doorID, id)
}

func (a *App) listPictures(w http.ResponseWriter, r *http.Request, _ auth.User) {
	d, pictures, err := a.doors.Pictures(r.Context(), pathID(r, "id"))
	if err != nil {
		a.doorError(w, err)
		return
	}
	views := []pictureView{}
	for _, p := range pictures {
		views = append(views, viewOfPicture(d, p))
	}
	writeJSON(w, http.StatusOK, pictureList{Pictures: views})
}

// notUpload is the problem with a body that is not an upload.
var notUpload = check.Problems{{Field: picture.Field, Message: "Send the picture as a multipart form with the file in the field picture."}}

// errTooBig reports an upload over picture.MaxBytes.
var errTooBig = errors.New(picture.TooBig())

// readUpload reads the file in the picture field of r's multipart form,
// reading no more of it than one byte past the limit. It reports errTooBig
// for a file over the limit and notUpload for a body that is not such a
// form. Nothing is written to disk.
func readUpload(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	data, ok := readFile(w, r, picture.Field, picture.MaxBytes)
	if !ok {
		return nil, notUpload
	}
	if len(data) > picture.MaxBytes {
		return nil, errTooBig
	}
	return data, nil
}

// readFile reads the file in field of r's multipart form, of which it
// reads no more than one byte past most, so a file longer than most is
// longer than most when read. It reports whether r's body is such a form.
// Nothing is written to disk.
func readFile(w http.ResponseWriter, r *http.Request, field string, most int) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, int64(most)+uploadOverhead)
	parts, err := r.MultipartReader()
	if err != nil {
		return nil, false
	}
	for {
		part, err := parts.NextPart()
		if err != nil {
			return nil, false
		}
		if part.FormName() != field {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(part, int64(most)+1))
		return data, err == nil
	}
}

// addPicture adds the picture r uploads to the door the path names, and
// returns the door as it is now.
func (a *App) addPicture(w http.ResponseWriter, r *http.Request) (connector.StoredDoor, connector.StoredPicture, error) {
	data, err := readUpload(w, r)
	if err != nil {
		return connector.StoredDoor{}, connector.StoredPicture{}, err
	}
	return a.doors.AddPicture(r.Context(), pathID(r, "id"), data)
}

func (a *App) uploadPicture(w http.ResponseWriter, r *http.Request, _ auth.User) {
	d, p, err := a.addPicture(w, r)
	if err != nil {
		a.doorError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, viewOfPicture(d, p))
}

// servePicture serves what read returns for the picture the path names, as
// JPEG that browsers may keep. Anyone signed in sees the pictures of a door
// the portal shows; only those who manage doors see the others.
func (a *App) servePicture(read func(ctx context.Context, doorID, id int64) ([]byte, error)) viewerHandler {
	return func(w http.ResponseWriter, r *http.Request, caller auth.User) {
		d, err := a.doors.Door(r.Context(), pathID(r, "id"))
		if err != nil {
			a.doorError(w, err)
			return
		}
		if !caller.Has(auth.RoleManageDoors) && !door.InPortal(d) {
			writeError(w, http.StatusForbidden, "the manage-doors role is required for the pictures of a door the portal does not show")
			return
		}
		data, err := read(r.Context(), d.ID, pathID(r, "picture"))
		if err != nil {
			a.doorError(w, err)
			return
		}
		w.Header().Set("Content-Type", picture.ContentType)
		w.Header().Set("Cache-Control", pictureCache)
		_, _ = w.Write(data)
	}
}

func (a *App) makePrimary(w http.ResponseWriter, r *http.Request, _ auth.User) {
	a.doorDone(w, a.doors.SetPrimaryPicture(r.Context(), pathID(r, "id"), pathID(r, "picture")))
}

func (a *App) deletePicture(w http.ResponseWriter, r *http.Request, _ auth.User) {
	a.doorDone(w, a.doors.DeletePicture(r.Context(), pathID(r, "id"), pathID(r, "picture")))
}
