package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/dakriy/doorcode/internal/connector"
)

// firmwareColumns lists the firmware columns, without the image, and the
// display name of the uploader, in the order firmwareFields reads them.
const firmwareColumns = `firmware.id, firmware.version, firmware.notes, firmware.sha256, firmware.size, firmware.uploaded_by,
	users.display_name, firmware.uploaded_at, firmware.is_default`

// firmwareSource is the firmware table joined to its uploaders.
const firmwareSource = ` from firmware join users on users.id = firmware.uploaded_by`

func firmwareFields(f *connector.StoredFirmware) []any {
	return []any{&f.ID, &f.Version, &f.Notes, &f.SHA256, &f.Size, &f.UploadedBy, &f.UploaderName, &f.UploadedAt, &f.Default}
}

// updateColumns lists the door_updates columns in the order updateFields
// reads them.
const updateColumns = `door_id, version, command_id, token_hash, sent_at, expires_at, state, finished_at, detail`

func updateFields(u *connector.StoredUpdate) []any {
	return []any{&u.DoorID, &u.Version, &u.CommandID, &u.TokenHash, &u.SentAt, &u.ExpiresAt, &u.State, &u.FinishedAt, &u.Detail}
}

// AddFirmware inserts f with its image and returns its id.
func (s *Store) AddFirmware(ctx context.Context, f connector.StoredFirmware, image []byte) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		insert into firmware (version, notes, sha256, size, image, uploaded_by, uploaded_at) values ($1, $2, $3, $4, $5, $6, $7) returning id`,
		f.Version, f.Notes, f.SHA256, f.Size, image, f.UploadedBy, f.UploadedAt).Scan(&id)
	return id, err
}

// Firmwares returns every firmware, newest upload first.
func (s *Store) Firmwares(ctx context.Context) ([]connector.StoredFirmware, error) {
	rows, err := s.pool.Query(ctx, `select `+firmwareColumns+firmwareSource+` order by firmware.uploaded_at desc, firmware.id desc`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanned(firmwareFields))
}

// Firmware returns the firmware with id.
func (s *Store) Firmware(ctx context.Context, id int64) (connector.StoredFirmware, bool, error) {
	var f connector.StoredFirmware
	err := s.pool.QueryRow(ctx, `select `+firmwareColumns+firmwareSource+` where firmware.id = $1`, id).Scan(firmwareFields(&f)...)
	return found(f, err)
}

// FirmwareImage returns the image of the firmware with id.
func (s *Store) FirmwareImage(ctx context.Context, id int64) ([]byte, bool, error) {
	var image []byte
	err := s.pool.QueryRow(ctx, `select image from firmware where id = $1`, id).Scan(&image)
	return found(image, err)
}

// SetDefaultFirmware makes the firmware with id the only default one; the
// current default stays when no firmware has id.
func (s *Store) SetDefaultFirmware(ctx context.Context, id int64) (bool, error) {
	var held bool
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `update firmware set is_default = false
			where is_default and exists (select 1 from firmware chosen where chosen.id = $1)`, id); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `update firmware set is_default = true where id = $1`, id)
		held = tag.RowsAffected() == 1
		return err
	})
	return held, err
}

// ClearDefaultFirmware makes no firmware the default.
func (s *Store) ClearDefaultFirmware(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `update firmware set is_default = false where is_default`)
	return err
}

// DeleteFirmware removes the firmware with id; the foreign key refuses
// firmware a door names.
func (s *Store) DeleteFirmware(ctx context.Context, id int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `delete from firmware where id = $1`, id)
	return tag.RowsAffected() == 1, err
}

// DoorUpdates returns every door's latest update, by door.
func (s *Store) DoorUpdates(ctx context.Context) ([]connector.StoredUpdate, error) {
	rows, err := s.pool.Query(ctx, `select `+updateColumns+` from door_updates order by door_id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanned(updateFields))
}

// PutDoorUpdate stores u over its door's latest update.
func (s *Store) PutDoorUpdate(ctx context.Context, u connector.StoredUpdate) error {
	_, err := s.pool.Exec(ctx, `insert into door_updates (`+updateColumns+`) values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		on conflict (door_id) do update set version = excluded.version, command_id = excluded.command_id,
			token_hash = excluded.token_hash, sent_at = excluded.sent_at, expires_at = excluded.expires_at,
			state = excluded.state, finished_at = excluded.finished_at, detail = excluded.detail`, updateFields(&u)...)
	return err
}
