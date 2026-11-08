package door

import (
	"context"
	"slices"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/picture"
)

// Pictures returns the door whose id is doorID and its pictures, oldest
// first.
func (s Service) Pictures(ctx context.Context, doorID int64) (connector.StoredDoor, []connector.StoredPicture, error) {
	d, err := s.Door(ctx, doorID)
	if err != nil {
		return d, nil, err
	}
	pictures, err := s.Store.Pictures(ctx, doorID)
	return d, pictures, err
}

// AddPicture adds data, an uploaded image, as a picture of the door whose
// id is doorID, or returns the problem with it, and returns the door as it
// is now. A door's first picture becomes its primary picture.
func (s Service) AddPicture(ctx context.Context, doorID int64, data []byte) (connector.StoredDoor, connector.StoredPicture, error) {
	d, err := s.Door(ctx, doorID)
	if err != nil {
		return d, connector.StoredPicture{}, err
	}
	prepared, err := picture.Prepare(data)
	if err != nil {
		return d, connector.StoredPicture{}, err
	}
	p := connector.StoredPicture{DoorID: doorID, Width: prepared.Width, Height: prepared.Height, CreatedAt: s.Clock.Now()}
	p.ID, err = s.Store.AddPicture(ctx, p, prepared.Image, prepared.Thumbnail)
	if err != nil || d.PrimaryPictureID != nil {
		return d, p, err
	}
	d.PrimaryPictureID = &p.ID
	_, err = s.Store.SetPrimaryPicture(ctx, doorID, p.ID)
	return d, p, err
}

// picture returns the door whose id is doorID, after checking that the
// picture whose id is id is one of its own, and the door's pictures.
func (s Service) picture(ctx context.Context, doorID, id int64) (connector.StoredDoor, []connector.StoredPicture, error) {
	d, pictures, err := s.Pictures(ctx, doorID)
	if err != nil {
		return d, nil, err
	}
	if !slices.ContainsFunc(pictures, func(p connector.StoredPicture) bool { return p.ID == id }) {
		return d, nil, ErrNoPicture
	}
	return d, pictures, nil
}

// SetPrimaryPicture makes the picture whose id is id the primary picture of
// the door whose id is doorID.
func (s Service) SetPrimaryPicture(ctx context.Context, doorID, id int64) error {
	if _, _, err := s.picture(ctx, doorID, id); err != nil {
		return err
	}
	// The door was just read and doors are never removed.
	_, err := s.Store.SetPrimaryPicture(ctx, doorID, id)
	return err
}

// DeletePicture removes the picture whose id is id from the door whose id
// is doorID. When it was the primary picture, the oldest picture left
// becomes the primary one.
func (s Service) DeletePicture(ctx context.Context, doorID, id int64) error {
	d, pictures, err := s.picture(ctx, doorID, id)
	if err != nil {
		return err
	}
	if _, err := s.Store.DeletePicture(ctx, doorID, id); err != nil {
		return err
	}
	left := slices.DeleteFunc(slices.Clone(pictures), func(p connector.StoredPicture) bool { return p.ID == id })
	if !IsPrimary(d, id) || len(left) == 0 {
		return nil
	}
	_, err = s.Store.SetPrimaryPicture(ctx, doorID, left[0].ID)
	return err
}

// Image returns the image of the picture whose id is id, of the door whose
// id is doorID.
func (s Service) Image(ctx context.Context, doorID, id int64) ([]byte, error) {
	return found(s.Store.PictureImage(ctx, doorID, id))
}

// Thumbnail returns the thumbnail of the picture whose id is id, of the
// door whose id is doorID.
func (s Service) Thumbnail(ctx context.Context, doorID, id int64) ([]byte, error) {
	return found(s.Store.PictureThumbnail(ctx, doorID, id))
}

// found turns a store's report of whether a picture was there into an
// error.
func found(data []byte, there bool, err error) ([]byte, error) {
	if err == nil && !there {
		return nil, ErrNoPicture
	}
	return data, err
}

// IsPrimary reports whether the picture whose id is id is d's primary
// picture.
func IsPrimary(d connector.StoredDoor, id int64) bool {
	return d.PrimaryPictureID != nil && *d.PrimaryPictureID == id
}
