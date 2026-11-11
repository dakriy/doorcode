package app

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/openapi"
	"github.com/dakriy/doorcode/internal/picture"
	"github.com/dakriy/doorcode/internal/settings"
)

// siteImage is a picture an administrator uploads for the whole site: the
// API reads, sets, and deletes it, and the settings page's Look and feel
// tab shows it in a card of its own with a form to upload it and one to
// remove it.
type siteImage struct {
	// kind is the picture as the store keeps it.
	kind connector.ImageKind
	// name is where it is, below the API and below the settings page; the
	// file input of its card's upload form, so a refused upload's problem
	// shows on its own card; and what its flash keys start with.
	name string
	// contentType is the type it is served as, which prepare makes an
	// upload into.
	contentType string
	prepare     func([]byte) ([]byte, error)
	// missing is the refusal when the site has none, answered with 404.
	missing error
	// versioned is true when pages add when it was stored to its address,
	// so a new one has a new address and browsers keep each for good;
	// otherwise its address stays fixed.
	versioned bool
	// gone is what the settings page says when removing it finds none.
	gone string
	doc  imageDoc
}

// imageDoc is what the API document says of a site picture: what its
// operation ids end in, the summaries of reading, setting, and deleting
// it, and the descriptions of the picture served, of it set, of it
// deleted, and of the site having none.
type imageDoc struct {
	id                              string
	read, set, remove               string
	served, shown, removed, missing string
}

// signInImage is the picture the sign-in page shows beside its form.
var signInImage = siteImage{
	kind: connector.SignInImage, name: "sign-in-image", contentType: picture.ContentType, prepare: picture.PrepareImage,
	missing: errors.New("the sign-in page has no picture"), versioned: true,
	gone: "The sign-in page shows no picture any more. It may have been removed already.",
	doc: imageDoc{
		id: "SignInImage", read: "Read the sign-in page's picture", set: "Set the sign-in page's picture", remove: "Take the picture off the sign-in page",
		served:  "The picture, as JPEG.",
		shown:   fmt.Sprintf("The sign-in page shows the picture, reduced to at most %d pixels on its longest side.", picture.MaxSide),
		removed: "The sign-in page shows no picture.", missing: "The sign-in page shows no picture.",
	},
}

// logoImage is the site's logo, which the page header and the sign-in
// page show in place of the keypad mark.
var logoImage = siteImage{
	kind: connector.LogoImage, name: "logo", contentType: picture.LogoType, prepare: picture.PrepareLogo,
	missing: errors.New("the site has no logo"), versioned: true,
	gone: "The site has no logo any more. It may have been removed already.",
	doc: imageDoc{
		id: "Logo", read: "Read the site's logo", set: "Set the site's logo", remove: "Take the site's logo away",
		served:  "The logo, as PNG.",
		shown:   fmt.Sprintf("The page header and the sign-in page show the logo in place of the keypad mark, reduced to at most %d pixels on its longest side. Its transparent parts stay transparent.", picture.LogoSide),
		removed: "The pages show the keypad mark again.", missing: "The site has no logo.",
	},
}

// faviconImage is the site's favicon. Its address stays fixed, since a
// favicon is set once when a site is set up.
var faviconImage = siteImage{
	kind: connector.FaviconImage, name: "favicon", contentType: picture.LogoType, prepare: picture.PrepareLogo,
	missing: errors.New("the site has no favicon"),
	gone:    "The site has no favicon any more. It may have been removed already.",
	doc: imageDoc{
		id: "Favicon", read: "Read the site's favicon", set: "Set the site's favicon", remove: "Take the site's favicon away",
		served:  "The favicon, as PNG.",
		shown:   fmt.Sprintf("Browsers show the favicon for the site's pages, reduced to at most %d pixels on its longest side. Its transparent parts stay transparent.", picture.LogoSide),
		removed: "Browsers show the logo for the site's pages, or the keypad mark when the site has no logo.", missing: "The site has no favicon.",
	},
}

// siteImages is every site picture, in the order the API lists them.
var siteImages = []siteImage{signInImage, logoImage, faviconImage}

// imageCache lets browsers keep a versioned site picture, whose address
// changes with every new one.
const imageCache = "public, max-age=31536000, immutable"

// imageVersion is the query parameter pages add to a versioned site
// picture's address.
var imageVersion = openapi.Param{Name: "v", In: "query", Description: "Ignored. Pages add when the picture was stored, so browsers fetch a new picture at a new address.", Example: "1767603600"}

// imageRoutes is the API operations on img and its settings page forms.
func (a *App) imageRoutes(img siteImage) []route {
	admin := holding(auth.RoleAdmin)
	read, set, remove := img.apiDocs()
	page := settingsPath + "/" + img.name
	return []route{
		apiRoute(http.MethodGet, "/"+img.name, anyone, a.serveImage(img), read),
		apiRoute(http.MethodPut, "/"+img.name, admin, a.putImage(img), set),
		apiRoute(http.MethodDelete, "/"+img.name, admin, a.deleteImage(img), remove),
		pageRoute(http.MethodPost, page, admin, a.uploadImageFromForm(img)),
		pageRoute(http.MethodPost, page+"/delete", admin, a.deleteImageFromForm(img)),
	}
}

// apiDocs is what the API document says of reading, setting, and deleting
// img.
func (img siteImage) apiDocs() (apiDoc, apiDoc, apiDoc) {
	missing := openapi.Response{Status: http.StatusNotFound, Description: img.doc.missing, Body: errorBody{}}
	read := apiDoc{
		id: "get" + img.doc.id, summary: img.doc.read, tag: "settings",
		responses: []openapi.Response{
			{Status: http.StatusOK, Description: img.doc.served, Body: openapi.File{}, Type: img.contentType},
			missing,
			{Status: http.StatusInternalServerError, Description: "The server failed; the body names a request id to give an administrator.", Body: errorBody{}},
		},
	}
	if img.versioned {
		read.params = []openapi.Param{imageVersion}
	}
	set := apiDoc{
		id: "set" + img.doc.id, summary: img.doc.set, tag: "settings", form: pictureUpload{},
		responses: []openapi.Response{
			{Status: http.StatusNoContent, Description: img.doc.shown},
			{Status: http.StatusBadRequest, Description: "The body is not a multipart form with the file in the field picture, or the file is not an image that can be read.", Body: errorBody{}},
			{Status: http.StatusRequestEntityTooLarge, Description: fmt.Sprintf("The file is larger than %d MB.", picture.Megabytes()), Body: errorBody{}},
		},
	}
	remove := apiDoc{
		id: "delete" + img.doc.id, summary: img.doc.remove, tag: "settings",
		responses: []openapi.Response{{Status: http.StatusNoContent, Description: img.doc.removed}, missing},
	}
	return read, set, remove
}

// imageURL is where img is served, with when it was stored when it is
// versioned; empty when the site has none.
func (a *App) imageURL(r *http.Request, img siteImage) (string, error) {
	at, found, err := a.settings.ImageStoredAt(r.Context(), img.kind)
	if !found {
		return "", err
	}
	url := apiPrefix + "/" + img.name
	if img.versioned {
		url += "?v=" + strconv.FormatInt(at.Unix(), 10)
	}
	return url, err
}

// writeImage writes the picture of img the site has, kept for good when
// its address is versioned.
func writeImage(w http.ResponseWriter, img siteImage, data []byte) {
	w.Header().Set("Content-Type", img.contentType)
	if img.versioned {
		w.Header().Set("Cache-Control", imageCache)
	}
	_, _ = w.Write(data)
}

func (a *App) serveImage(img siteImage) viewerHandler {
	return func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		stored, found, err := a.settings.Image(r.Context(), img.kind)
		if err != nil {
			a.internalError(w, r, err)
			return
		}
		if !found {
			writeError(w, http.StatusNotFound, img.missing.Error())
			return
		}
		writeImage(w, img, stored.Data)
	}
}

// setImage makes the picture r uploads in field the site's img.
func (a *App) setImage(w http.ResponseWriter, r *http.Request, img siteImage, field string) error {
	data, err := readUpload(w, r, field)
	if err != nil {
		return err
	}
	return a.settings.SetImage(r.Context(), img.kind, img.prepare, data)
}

// removeImage takes img away, reporting img.missing when the site has
// none.
func (a *App) removeImage(r *http.Request, img siteImage) error {
	removed, err := a.settings.RemoveImage(r.Context(), img.kind)
	if err == nil && !removed {
		return img.missing
	}
	return err
}

func (a *App) putImage(img siteImage) viewerHandler {
	return func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		a.imageDone(w, r, img, a.setImage(w, r, img, picture.Field))
	}
}

func (a *App) deleteImage(img siteImage) viewerHandler {
	return func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		a.imageDone(w, r, img, a.removeImage(r, img))
	}
}

// imageDone answers an API request that changed img with no content, or
// with what err means.
func (a *App) imageDone(w http.ResponseWriter, r *http.Request, img siteImage, err error) {
	if err != nil {
		a.refuse(w, r, err, []refusalStatus{{img.missing, http.StatusNotFound}, {errTooBig, http.StatusRequestEntityTooLarge}})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) uploadImageFromForm(img siteImage) viewerHandler {
	return func(w http.ResponseWriter, r *http.Request, viewer auth.User) {
		err := a.setImage(w, r, img, img.name)
		if status, refused := uploadRefusal(err); refused {
			a.showSettings(w, r, status, viewer, settings.TabLook, nil, map[string]string{img.name: err.Error()}, "")
			return
		}
		a.changed(w, r, err, settingsTabPath(settings.TabLook), img.name+"-saved")
	}
}

func (a *App) deleteImageFromForm(img siteImage) viewerHandler {
	return func(w http.ResponseWriter, r *http.Request, viewer auth.User) {
		err := a.removeImage(r, img)
		if errors.Is(err, img.missing) {
			a.showSettings(w, r, http.StatusNotFound, viewer, settings.TabLook, nil, nil, img.gone)
			return
		}
		a.changed(w, r, err, settingsTabPath(settings.TabLook), img.name+"-removed")
	}
}
