-- Events are the activity log: every code check a door asked about, every
-- keypad command, and every opening from the admin UI. They are only ever
-- added. Doors and users are soft-deleted, so the events naming them stay;
-- the deciding access rule is kept by id alone, since rules are removed.
create table events (
	id bigint generated always as identity primary key,
	at timestamptz not null,
	door_id bigint not null references doors (id),
	type text not null,
	outcome text not null check (outcome in ('allowed', 'denied')),
	reason text not null,
	user_id bigint references users (id),
	rule_id bigint,
	request_id text not null,
	detail text not null
);

create index events_at on events (at desc, id desc);
create index events_door_at on events (door_id, at desc, id desc);
create index events_user_at on events (user_id, at desc, id desc);
