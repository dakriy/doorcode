-- Zones carry open mode and access: a door is in open mode while a zone
-- it is in has an active open-mode schedule, and access rules reach doors
-- only through zones. The door-level open-mode schedules and the doors
-- rules named directly are dropped.
create table zone_open_schedules (
	zone_id bigint not null references zones (id) on delete cascade,
	schedule_id bigint not null references schedules (id) on delete cascade,
	primary key (zone_id, schedule_id)
);

drop table door_open_schedules;

drop table access_rule_doors;
