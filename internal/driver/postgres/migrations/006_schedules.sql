-- Schedules are named weekly patterns: windows that open on chosen weekdays
-- at a start time and close at an end time (on the next day when the end is
-- not after the start), an optional effective date range, and excluded
-- dates. Times are minutes after midnight in the site timezone. The Always
-- schedule is seeded as built in: every day, all day.
create table schedules (
	id bigint generated always as identity primary key,
	name text not null,
	starts_on date,
	ends_on date,
	excluded_dates date[] not null,
	builtin boolean not null default false
);

create unique index schedules_name on schedules (lower(name));

create table schedule_windows (
	schedule_id bigint not null references schedules (id) on delete cascade,
	position integer not null,
	weekdays smallint[] not null,
	starts_at smallint not null,
	ends_at smallint not null,
	primary key (schedule_id, position)
);

insert into schedules (name, excluded_dates, builtin) values ('Always', '{}', true);

insert into schedule_windows (schedule_id, position, weekdays, starts_at, ends_at)
select id, 0, '{0,1,2,3,4,5,6}', 0, 0 from schedules where builtin;
