// Package firmware keeps the firmware images the doors run and rolls
// updates out to them. One version is the site's default; a door may run
// another instead. A door whose controller reports a version other than
// its target is pending an update, which the rollout sends inside the
// update window, a few doors at a time, or an administrator sends at once.
package firmware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/dakriy/doorcode/internal/check"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/controller"
	"github.com/dakriy/doorcode/internal/door"
	"github.com/dakriy/doorcode/internal/event"
	"github.com/dakriy/doorcode/internal/settings"
)

var (
	// ErrNoFirmware reports an id that names no firmware.
	ErrNoFirmware = errors.New("no firmware has this id")
	// ErrInUse reports firmware that cannot be deleted while it is the
	// default or a door runs it instead of the default.
	ErrInUse = errors.New("the firmware is in use")
)

const (
	// MaxBytes is the largest image an upload may hold, MaxMiB mebibytes.
	MaxBytes = 16_777_216
	MaxMiB   = 16
	// Field is the multipart form field an upload carries the image in.
	Field            = "image"
	maxVersionLength = 64
	maxNotesLength   = 2000
)

// versionPattern is what a version may be made of, so it can stand in a
// URL as it is.
var versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)

// Store is the part of the store this package uses.
type Store interface {
	connector.FirmwareStore
	connector.DoorsStore
}

// Service keeps the firmware and rolls updates out.
type Service struct {
	Store Store
	// Doors finds the doors, and Controllers sends them update commands.
	Doors       door.Service
	Controllers controller.Service
	Events      event.Service
	Settings    settings.Service
	Clock       connector.Clock
	Entropy     connector.Entropy
	// DownloadPath is the path, below the server's public address, a door
	// downloads version from.
	DownloadPath func(doorID int64, version string) string
	// Changed is called whenever what a door should run or how far its
	// update is changes.
	Changed func()
}

// Fields is what an uploader says about firmware. Text is trimmed before
// it is checked.
type Fields struct {
	Version string
	Notes   string
}

// InUseError is the refusal to delete firmware that is the site's default
// or that the doors named run instead of it.
type InUseError struct {
	Default bool
	Doors   []string
}

func (e InUseError) Error() string {
	if e.Default {
		return "this is the default version; choose another default before deleting it"
	}
	return "these doors run this version instead of the default: " + strings.Join(e.Doors, ", ") + "; choose other firmware for them first"
}

// Is reports whether target is ErrInUse.
func (e InUseError) Is(target error) bool {
	return target == ErrInUse
}

// verify says why image cannot be offered to the doors, empty when it can.
// It is the one place an image is judged before it is stored: today any
// file is accepted, and checking the image's signature belongs here.
func verify(image []byte) string {
	if len(image) == 0 {
		return "Choose the firmware image to upload."
	}
	return ""
}

// Upload stores image as the firmware f describes, uploaded by the user
// whose id is by.
func (s Service) Upload(ctx context.Context, f Fields, image []byte, by int64) (connector.StoredFirmware, error) {
	all, err := s.Store.Firmwares(ctx)
	if err != nil {
		return connector.StoredFirmware{}, err
	}
	version, notes := strings.TrimSpace(f.Version), strings.TrimSpace(f.Notes)
	var found check.List
	found.Add("version", versionProblem(all, version))
	found.Add("notes", check.TooLong(notes, maxNotesLength, check.LongText))
	found.Add(Field, verify(image))
	if err := found.Err(); err != nil {
		return connector.StoredFirmware{}, err
	}
	sum := sha256.Sum256(image)
	stored := connector.StoredFirmware{
		Version: version, Notes: notes, SHA256: hex.EncodeToString(sum[:]), Size: len(image), UploadedBy: by, UploadedAt: s.Clock.Now(),
	}
	id, err := s.Store.AddFirmware(ctx, stored, image)
	if err != nil {
		return connector.StoredFirmware{}, err
	}
	return s.Get(ctx, id)
}

// versionProblem is what is wrong with version among the firmware all,
// empty when nothing is.
func versionProblem(all []connector.StoredFirmware, version string) string {
	if version == "" {
		return "Enter the version, such as 1.4.3."
	}
	if problem := check.TooLong(version, maxVersionLength, check.LongText); problem != "" {
		return problem
	}
	if !versionPattern.MatchString(version) {
		return "Use letters, digits, dots, dashes, underscores, and plus signs, starting with a letter or digit."
	}
	for _, f := range all {
		if f.Version == version {
			return "This version is uploaded already."
		}
	}
	return ""
}

// Get returns the firmware whose id is id.
func (s Service) Get(ctx context.Context, id int64) (connector.StoredFirmware, error) {
	f, found, err := s.Store.Firmware(ctx, id)
	if err != nil {
		return connector.StoredFirmware{}, err
	}
	if !found {
		return connector.StoredFirmware{}, ErrNoFirmware
	}
	return f, nil
}

// Delete removes the firmware whose id is id, unless it is the default or
// a door runs it instead.
func (s Service) Delete(ctx context.Context, id int64) error {
	plan, err := s.Plan(ctx)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(plan.Firmware, func(f connector.StoredFirmware) bool { return f.ID == id })
	if i < 0 {
		return ErrNoFirmware
	}
	if err := plan.InUse(plan.Firmware[i]); err != nil {
		return err
	}
	_, err = s.Store.DeleteFirmware(ctx, id)
	return err
}

// InUse reports, as an InUseError, why f cannot be deleted as plan
// stands, and nil when it can.
func (p Plan) InUse(f connector.StoredFirmware) error {
	if f.Default {
		return InUseError{Default: true}
	}
	var running []string
	for _, df := range p.Doors {
		if df.Override != nil && df.Override.ID == f.ID {
			running = append(running, df.Door.Name)
		}
	}
	if running != nil {
		return InUseError{Doors: running}
	}
	return nil
}

// SetDefault makes the firmware whose id is id the site's default, nil
// leaving the site without one.
func (s Service) SetDefault(ctx context.Context, id *int64) error {
	if err := s.exists(ctx, id); err != nil {
		return err
	}
	if err := s.storeDefault(ctx, id); err != nil {
		return err
	}
	s.Changed()
	return nil
}

// storeDefault stores the firmware whose id is id as the default, nil
// clearing it.
func (s Service) storeDefault(ctx context.Context, id *int64) error {
	if id == nil {
		return s.Store.ClearDefaultFirmware(ctx)
	}
	_, err := s.Store.SetDefaultFirmware(ctx, *id)
	return err
}

// SetOverride makes the firmware whose id is id the one the door whose id
// is doorID runs instead of the default, nil making it follow the default,
// and returns the door as it is now.
func (s Service) SetOverride(ctx context.Context, doorID int64, id *int64) (connector.StoredDoor, error) {
	if _, err := s.Doors.Door(ctx, doorID); err != nil {
		return connector.StoredDoor{}, err
	}
	if err := s.exists(ctx, id); err != nil {
		return connector.StoredDoor{}, err
	}
	if _, err := s.Store.SetDoorFirmware(ctx, doorID, id); err != nil {
		return connector.StoredDoor{}, err
	}
	s.Changed()
	return s.Doors.Door(ctx, doorID)
}

// exists reports ErrNoFirmware unless id is nil or names firmware.
func (s Service) exists(ctx context.Context, id *int64) error {
	if id == nil {
		return nil
	}
	_, err := s.Get(ctx, *id)
	return err
}

// versionDetail is what an update event says of version.
func versionDetail(version, more string) string {
	return fmt.Sprintf("Version %s.%s", version, more)
}
