-- A door's controller is the hardware enrolled as it, at most one per
-- door and per hardware id, with what it last reported and whether it is
-- connected. Each set of broker credentials issued to a door's controllers
-- is kept by username (the broker holds the password) until it is revoked,
-- and stays as history after.
create table door_controllers (
	door_id bigint primary key references doors (id) on delete cascade,
	hardware_id text not null unique,
	firmware_version text not null,
	signal integer not null,
	online boolean not null,
	last_seen_at timestamptz not null
);

create table door_credentials (
	id bigint generated always as identity primary key,
	door_id bigint not null references doors (id) on delete cascade,
	username text not null unique,
	issued_at timestamptz not null,
	used_at timestamptz,
	revoked_at timestamptz
);

create index door_credentials_door on door_credentials (door_id, id);
create index door_credentials_live on door_credentials (door_id, id) where revoked_at is null;
