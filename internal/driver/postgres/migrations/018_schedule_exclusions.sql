-- A schedule's exclusions are ranges of days, from the first to the last
-- day, both included; a single day is a range whose first and last days are
-- the same. They replace the list of excluded dates.
create table schedule_exclusions (
	schedule_id bigint not null references schedules (id) on delete cascade,
	position integer not null,
	first_day date not null,
	last_day date not null,
	primary key (schedule_id, position),
	check (last_day >= first_day)
);

alter table schedules drop column excluded_dates;
