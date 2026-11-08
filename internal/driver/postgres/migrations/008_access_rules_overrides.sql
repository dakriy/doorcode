-- Access rules let the members of a group enter doors, named directly or
-- through zones, while a schedule is active. A rule goes with its group; a
-- schedule a rule uses cannot be deleted. Overrides force one door open or
-- locked from a start up to an end, and name the user who created them.
create table access_rules (
	id bigint generated always as identity primary key,
	group_id bigint not null references groups (id) on delete cascade,
	schedule_id bigint not null references schedules (id)
);

create index access_rules_group on access_rules (group_id);

create table access_rule_doors (
	rule_id bigint not null references access_rules (id) on delete cascade,
	door_id bigint not null references doors (id) on delete cascade,
	primary key (rule_id, door_id)
);

create table access_rule_zones (
	rule_id bigint not null references access_rules (id) on delete cascade,
	zone_id bigint not null references zones (id) on delete cascade,
	primary key (rule_id, zone_id)
);

create table overrides (
	id bigint generated always as identity primary key,
	door_id bigint not null references doors (id) on delete cascade,
	kind text not null check (kind in ('open', 'locked')),
	starts_at timestamptz not null,
	ends_at timestamptz not null,
	reason text not null,
	created_by bigint not null references users (id),
	created_at timestamptz not null
);

create index overrides_door_end on overrides (door_id, ends_at);
