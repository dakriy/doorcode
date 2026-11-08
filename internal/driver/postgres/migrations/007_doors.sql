-- Doors are the controllers the site manages. A door exists once an
-- administrator creates it; enrollment attaches its hardware later. Doors
-- are soft-deleted, so their names stay taken. Each door has any number of
-- pictures, kept as JPEG with a thumbnail, and at most one primary picture,
-- which must be its own. A door is in open mode during each of its open
-- schedules, and is in any number of zones.
create table doors (
	id bigint generated always as identity primary key,
	name text not null,
	location text not null,
	notes text not null,
	public boolean not null,
	primary_picture_id bigint,
	deleted_at timestamptz,
	created_at timestamptz not null
);

create unique index doors_name on doors (lower(name));

create table door_pictures (
	id bigint generated always as identity primary key,
	door_id bigint not null references doors (id) on delete cascade,
	width integer not null,
	height integer not null,
	image bytea not null,
	thumbnail bytea not null,
	created_at timestamptz not null,
	unique (id, door_id)
);

create index door_pictures_door on door_pictures (door_id, id);

alter table doors add constraint doors_primary_picture
	foreign key (primary_picture_id, id) references door_pictures (id, door_id)
	on delete set null (primary_picture_id);

create table door_open_schedules (
	door_id bigint not null references doors (id) on delete cascade,
	schedule_id bigint not null references schedules (id) on delete cascade,
	primary key (door_id, schedule_id)
);

create table zones (
	id bigint generated always as identity primary key,
	name text not null,
	description text not null
);

create unique index zones_name on zones (lower(name));

create table zone_doors (
	zone_id bigint not null references zones (id) on delete cascade,
	door_id bigint not null references doors (id) on delete cascade,
	primary key (zone_id, door_id)
);

create index zone_doors_door on zone_doors (door_id);
