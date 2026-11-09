package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dakriy/doorcode/internal/connector"
)

// doorColumns lists the doors columns, the ids of each door's zones, its
// controller's columns, all null without one, and
// its configuration overrides, in the order doorFields reads them, from
// doorTables.
const (
	doorColumns = `doors.id, doors.name, doors.location, doors.notes, doors.public, doors.primary_picture_id, doors.deleted_at,
	doors.created_at,
	array(select zone_id from zone_doors where door_id = doors.id order by zone_id),
	c.hardware_id, c.firmware_version, c.signal, c.online, c.last_seen_at, c.door_state, c.opened_at, c.held_open, c.unresponsive,
	c.telemetry, doors.config, doors.firmware_id`
	doorTables = `doors left join door_controllers c on c.door_id = doors.id`
)

// scannedDoor is a door as a row holds it, its controller's columns
// nullable.
type scannedDoor struct {
	connector.StoredDoor
	controller struct {
		hardwareID, firmwareVersion, doorState *string
		signal                                 *int
		online, heldOpen, unresponsive         *bool
		lastSeenAt, openedAt                   *time.Time
		telemetry                              *connector.StoredTelemetry
	}
}

func doorFields(d *scannedDoor) []any {
	c := &d.controller
	return []any{&d.ID, &d.Name, &d.Location, &d.Notes, &d.Public, &d.PrimaryPictureID, &d.DeletedAt, &d.CreatedAt,
		&d.ZoneIDs, &c.hardwareID, &c.firmwareVersion, &c.signal, &c.online, &c.lastSeenAt,
		&c.doorState, &c.openedAt, &c.heldOpen, &c.unresponsive, &c.telemetry, &d.Config, &d.FirmwareID}
}

// door is the door, with its controller when the row has one.
func (d scannedDoor) door() connector.StoredDoor {
	c := d.controller
	if c.hardwareID != nil {
		d.Controller = &connector.StoredController{HardwareID: *c.hardwareID, Online: *c.online, StoredDoorState: connector.StoredDoorState{
			FirmwareVersion: *c.firmwareVersion, Signal: *c.signal, LastSeenAt: *c.lastSeenAt,
			DoorState: *c.doorState, OpenedAt: c.openedAt, HeldOpen: *c.heldOpen, Unresponsive: *c.unresponsive, Telemetry: c.telemetry,
		}}
	}
	return d.StoredDoor
}

// Doors returns every door in name order.
func (s *Store) Doors(ctx context.Context) ([]connector.StoredDoor, error) {
	rows, err := s.pool.Query(ctx, `select `+doorColumns+` from `+doorTables+` order by lower(doors.name) collate "C", doors.id`)
	if err != nil {
		return nil, err
	}
	scanned, err := pgx.CollectRows(rows, scanned(doorFields))
	doors := []connector.StoredDoor{}
	for _, d := range scanned {
		doors = append(doors, d.door())
	}
	return doors, err
}

// Door returns the door with id.
func (s *Store) Door(ctx context.Context, id int64) (connector.StoredDoor, bool, error) {
	var d scannedDoor
	err := s.pool.QueryRow(ctx, `select `+doorColumns+` from `+doorTables+` where doors.id = $1`, id).Scan(doorFields(&d)...)
	return found(d.door(), err)
}

// SetController stores c as the controller of the door with id, or removes
// its controller when c is nil.
func (s *Store) SetController(ctx context.Context, id int64, c *connector.StoredController) (bool, error) {
	var updated bool
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `select 1 from doors where id = $1 for update`, id)
		updated = tag.RowsAffected() == 1
		if err != nil || !updated {
			return err
		}
		return replaceController(ctx, tx, id, c)
	})
	return updated, err
}

// SetDoorState stores st over the state columns of the door's controller,
// leaving its hardware id and online status as they are.
func (s *Store) SetDoorState(ctx context.Context, id int64, st connector.StoredDoorState) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		update door_controllers set firmware_version = $2, signal = $3, last_seen_at = $4, door_state = $5, opened_at = $6,
			held_open = $7, unresponsive = $8, telemetry = $9
		where door_id = $1`, id, st.FirmwareVersion, st.Signal, st.LastSeenAt, st.DoorState, st.OpenedAt, st.HeldOpen, st.Unresponsive, st.Telemetry)
	return tag.RowsAffected() == 1, err
}

// SetDoorFirmware stores firmwareID as the firmware the door with id runs
// instead of the default; the foreign key refuses firmware there is not.
func (s *Store) SetDoorFirmware(ctx context.Context, id int64, firmwareID *int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `update doors set firmware_id = $2 where id = $1`, id, firmwareID)
	return tag.RowsAffected() == 1, err
}

// SetDoorConfig stores config as the configuration overrides of the door
// with id.
func (s *Store) SetDoorConfig(ctx context.Context, id int64, config map[string]string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `update doors set config = $2 where id = $1`, id, config)
	return tag.RowsAffected() == 1, err
}

// replaceController makes c the controller of the door with id within tx.
func replaceController(ctx context.Context, tx pgx.Tx, id int64, c *connector.StoredController) error {
	if _, err := tx.Exec(ctx, `delete from door_controllers where door_id = $1`, id); err != nil || c == nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		insert into door_controllers (door_id, hardware_id, firmware_version, signal, online, last_seen_at,
			door_state, opened_at, held_open, unresponsive, telemetry)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`, id, c.HardwareID, c.FirmwareVersion, c.Signal, c.Online, c.LastSeenAt,
		c.DoorState, c.OpenedAt, c.HeldOpen, c.Unresponsive, c.Telemetry)
	return err
}

// credentialColumns lists the door_credentials columns in the order
// credentialFields reads them.
const credentialColumns = `id, door_id, username, issued_at, used_at, revoked_at`

func credentialFields(c *connector.StoredCredential) []any {
	return []any{&c.ID, &c.DoorID, &c.Username, &c.IssuedAt, &c.UsedAt, &c.RevokedAt}
}

// CreateCredential inserts c and returns its id.
func (s *Store) CreateCredential(ctx context.Context, c connector.StoredCredential) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		insert into door_credentials (door_id, username, issued_at, used_at, revoked_at) values ($1, $2, $3, $4, $5) returning id`,
		c.DoorID, c.Username, c.IssuedAt, c.UsedAt, c.RevokedAt).Scan(&id)
	return id, err
}

// DoorCredentials returns the door's credentials in id order.
func (s *Store) DoorCredentials(ctx context.Context, doorID int64) ([]connector.StoredCredential, error) {
	rows, err := s.pool.Query(ctx, `select `+credentialColumns+` from door_credentials where door_id = $1 order by id`, doorID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanned(credentialFields))
}

// LiveCredentials returns the credentials not revoked, by door and id.
func (s *Store) LiveCredentials(ctx context.Context) ([]connector.StoredCredential, error) {
	rows, err := s.pool.Query(ctx, `select `+credentialColumns+` from door_credentials where revoked_at is null order by door_id, id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanned(credentialFields))
}

// UseCredential stores at as when the credential with id was first used.
func (s *Store) UseCredential(ctx context.Context, id int64, at time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx, `update door_credentials set used_at = $2 where id = $1`, id, at)
	return tag.RowsAffected() == 1, err
}

// RevokeCredential stores at as when the credential with id was revoked.
func (s *Store) RevokeCredential(ctx context.Context, id int64, at time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx, `update door_credentials set revoked_at = $2 where id = $1`, id, at)
	return tag.RowsAffected() == 1, err
}

// CreateDoor inserts d with its open schedules and zones and returns its
// id.
func (s *Store) CreateDoor(ctx context.Context, d connector.StoredDoor) (int64, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			insert into doors (name, location, notes, public, deleted_at, created_at) values ($1, $2, $3, $4, $5, $6) returning id`,
			d.Name, d.Location, d.Notes, d.Public, d.DeletedAt, d.CreatedAt).Scan(&d.ID)
		if err != nil {
			return err
		}
		return linkDoor(ctx, tx, d)
	})
	return d.ID, err
}

// UpdateDoor stores d over the door with d.ID and replaces its zones.
func (s *Store) UpdateDoor(ctx context.Context, d connector.StoredDoor) (bool, error) {
	var updated bool
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `update doors set name = $2, location = $3, notes = $4, public = $5 where id = $1`,
			d.ID, d.Name, d.Location, d.Notes, d.Public)
		updated = tag.RowsAffected() == 1
		if err != nil || !updated {
			return err
		}
		return linkDoor(ctx, tx, d)
	})
	return updated, err
}

// linkDoor replaces the zones of the door with d.ID within tx.
func linkDoor(ctx context.Context, tx pgx.Tx, d connector.StoredDoor) error {
	_, err := tx.Exec(ctx, `delete from zone_doors where door_id = $1`, d.ID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `insert into zone_doors (door_id, zone_id) select $1, unnest($2::bigint[])`, d.ID, d.ZoneIDs)
	return err
}

// SetDoorDeleted stores at as the deletion time of the door with id.
func (s *Store) SetDoorDeleted(ctx context.Context, id int64, at *time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx, `update doors set deleted_at = $2 where id = $1`, id, at)
	return tag.RowsAffected() == 1, err
}

// AddPicture inserts p with its image and thumbnail and returns its id.
func (s *Store) AddPicture(ctx context.Context, p connector.StoredPicture, image, thumbnail []byte) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		insert into door_pictures (door_id, width, height, image, thumbnail, created_at) values ($1, $2, $3, $4, $5, $6) returning id`,
		p.DoorID, p.Width, p.Height, image, thumbnail, p.CreatedAt).Scan(&id)
	return id, err
}

// Pictures returns the door's pictures in id order, without their data.
func (s *Store) Pictures(ctx context.Context, doorID int64) ([]connector.StoredPicture, error) {
	rows, err := s.pool.Query(ctx, `
		select id, door_id, width, height, created_at from door_pictures where door_id = $1 order by id`, doorID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (connector.StoredPicture, error) {
		var p connector.StoredPicture
		err := row.Scan(&p.ID, &p.DoorID, &p.Width, &p.Height, &p.CreatedAt)
		return p, err
	})
}

// PictureImage returns the image of the door's picture with id.
func (s *Store) PictureImage(ctx context.Context, doorID, id int64) ([]byte, bool, error) {
	var data []byte
	err := s.pool.QueryRow(ctx, `select image from door_pictures where door_id = $1 and id = $2`, doorID, id).Scan(&data)
	return found(data, err)
}

// PictureThumbnail returns the thumbnail of the door's picture with id.
func (s *Store) PictureThumbnail(ctx context.Context, doorID, id int64) ([]byte, bool, error) {
	var data []byte
	err := s.pool.QueryRow(ctx, `select thumbnail from door_pictures where door_id = $1 and id = $2`, doorID, id).Scan(&data)
	return found(data, err)
}

// SetPrimaryPicture makes the picture with id the door's primary picture;
// the foreign key refuses a picture of another door.
func (s *Store) SetPrimaryPicture(ctx context.Context, doorID, id int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `update doors set primary_picture_id = $2 where id = $1`, doorID, id)
	return tag.RowsAffected() == 1, err
}

// DeletePicture removes the door's picture with id; the foreign key clears
// the door's primary picture when it was this one.
func (s *Store) DeletePicture(ctx context.Context, doorID, id int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `delete from door_pictures where door_id = $1 and id = $2`, doorID, id)
	return tag.RowsAffected() == 1, err
}

// zoneColumns lists the zones columns, and the ids of each zone's doors
// and open-mode schedules, in the order zoneFields reads them.
const zoneColumns = `id, name, description,
	array(select door_id from zone_doors where zone_id = zones.id order by door_id),
	array(select schedule_id from zone_open_schedules where zone_id = zones.id order by schedule_id)`

func zoneFields(z *connector.StoredZone) []any {
	return []any{&z.ID, &z.Name, &z.Description, &z.DoorIDs, &z.OpenScheduleIDs}
}

// Zones returns every zone in name order.
func (s *Store) Zones(ctx context.Context) ([]connector.StoredZone, error) {
	rows, err := s.pool.Query(ctx, `select `+zoneColumns+` from zones order by lower(name) collate "C", id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanned(zoneFields))
}

// Zone returns the zone with id.
func (s *Store) Zone(ctx context.Context, id int64) (connector.StoredZone, bool, error) {
	var z connector.StoredZone
	err := s.pool.QueryRow(ctx, `select `+zoneColumns+` from zones where id = $1`, id).Scan(zoneFields(&z)...)
	return found(z, err)
}

// CreateZone inserts z with its doors and open-mode schedules and returns
// its id.
func (s *Store) CreateZone(ctx context.Context, z connector.StoredZone) (int64, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `insert into zones (name, description) values ($1, $2) returning id`, z.Name, z.Description).Scan(&z.ID)
		if err != nil {
			return err
		}
		return fillZone(ctx, tx, z)
	})
	return z.ID, err
}

// UpdateZone stores z over the zone with z.ID and replaces its doors and
// open-mode schedules.
func (s *Store) UpdateZone(ctx context.Context, z connector.StoredZone) (bool, error) {
	var updated bool
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `update zones set name = $2, description = $3 where id = $1`, z.ID, z.Name, z.Description)
		updated = tag.RowsAffected() == 1
		if err != nil || !updated {
			return err
		}
		return fillZone(ctx, tx, z)
	})
	return updated, err
}

// fillZone replaces the doors and open-mode schedules of the zone with
// z.ID within tx.
func fillZone(ctx context.Context, tx pgx.Tx, z connector.StoredZone) error {
	if _, err := tx.Exec(ctx, `delete from zone_doors where zone_id = $1`, z.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `insert into zone_doors (zone_id, door_id) select $1, unnest($2::bigint[])`, z.ID, z.DoorIDs); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from zone_open_schedules where zone_id = $1`, z.ID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `insert into zone_open_schedules (zone_id, schedule_id) select $1, unnest($2::bigint[])`, z.ID, z.OpenScheduleIDs)
	return err
}

// DeleteZone removes the zone with id; its doors stay.
func (s *Store) DeleteZone(ctx context.Context, id int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `delete from zones where id = $1`, id)
	return tag.RowsAffected() == 1, err
}
