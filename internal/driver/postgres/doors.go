package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dakriy/doorcode/internal/connector"
)

// doorColumns lists the doors columns, and the ids of each door's open
// schedules and zones, in the order doorFields reads them.
const doorColumns = `id, name, location, notes, public, primary_picture_id, deleted_at, created_at,
	array(select schedule_id from door_open_schedules where door_id = doors.id order by schedule_id),
	array(select zone_id from zone_doors where door_id = doors.id order by zone_id)`

func doorFields(d *connector.StoredDoor) []any {
	return []any{&d.ID, &d.Name, &d.Location, &d.Notes, &d.Public, &d.PrimaryPictureID, &d.DeletedAt, &d.CreatedAt,
		&d.OpenScheduleIDs, &d.ZoneIDs}
}

// Doors returns every door in name order.
func (s *Store) Doors(ctx context.Context) ([]connector.StoredDoor, error) {
	rows, err := s.pool.Query(ctx, `select `+doorColumns+` from doors order by lower(name) collate "C", id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanned(doorFields))
}

// Door returns the door with id.
func (s *Store) Door(ctx context.Context, id int64) (connector.StoredDoor, bool, error) {
	var d connector.StoredDoor
	err := s.pool.QueryRow(ctx, `select `+doorColumns+` from doors where id = $1`, id).Scan(doorFields(&d)...)
	return found(d, err)
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

// UpdateDoor stores d over the door with d.ID and replaces its open
// schedules and zones.
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

// linkDoor replaces the open schedules and zones of the door with d.ID
// within tx.
func linkDoor(ctx context.Context, tx pgx.Tx, d connector.StoredDoor) error {
	_, err := tx.Exec(ctx, `delete from door_open_schedules where door_id = $1`, d.ID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `insert into door_open_schedules (door_id, schedule_id) select $1, unnest($2::bigint[])`, d.ID, d.OpenScheduleIDs)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `delete from zone_doors where door_id = $1`, d.ID)
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

// zoneColumns lists the zones columns, and the ids of each zone's doors,
// in the order zoneFields reads them.
const zoneColumns = `id, name, description,
	array(select door_id from zone_doors where zone_id = zones.id order by door_id)`

func zoneFields(z *connector.StoredZone) []any {
	return []any{&z.ID, &z.Name, &z.Description, &z.DoorIDs}
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

// CreateZone inserts z with its doors and returns its id.
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

// UpdateZone stores z over the zone with z.ID and replaces its doors.
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

// fillZone replaces the doors of the zone with z.ID within tx.
func fillZone(ctx context.Context, tx pgx.Tx, z connector.StoredZone) error {
	if _, err := tx.Exec(ctx, `delete from zone_doors where zone_id = $1`, z.ID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `insert into zone_doors (zone_id, door_id) select $1, unnest($2::bigint[])`, z.ID, z.DoorIDs)
	return err
}

// DeleteZone removes the zone with id; its doors stay.
func (s *Store) DeleteZone(ctx context.Context, id int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `delete from zones where id = $1`, id)
	return tag.RowsAffected() == 1, err
}
