-- Uploaded firmware images, each with a unique version. At most one is the
-- site's default, which every door runs unless it names other firmware.
create table firmware (
	id bigint generated always as identity primary key,
	version text not null unique,
	notes text not null,
	sha256 text not null,
	size integer not null,
	image bytea not null,
	uploaded_by bigint not null references users (id),
	uploaded_at timestamptz not null,
	is_default boolean not null default false
);

create unique index firmware_one_default on firmware (is_default) where is_default;

-- The firmware a door runs instead of the default; firmware a door names
-- cannot be deleted.
alter table doors add column firmware_id bigint references firmware (id);

-- The latest firmware update the server sent each door's controller.
create table door_updates (
	door_id bigint primary key references doors (id),
	version text not null,
	command_id text not null,
	token_hash bytea not null,
	sent_at timestamptz not null,
	expires_at timestamptz not null,
	state text not null check (state in ('updating', 'succeeded', 'failed')),
	finished_at timestamptz,
	detail text not null
);

-- Events that neither let anyone in nor raise an alarm, such as a firmware
-- update sent, are logged with the outcome info.
alter table events
	drop constraint events_outcome_check,
	add constraint events_outcome_check check (outcome in ('allowed', 'denied', 'alert', 'info'));
