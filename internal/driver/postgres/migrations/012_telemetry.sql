-- A door's controller also holds the door's current state: whether the
-- door is open or closed as the controller last said (empty until it
-- has), since when it is open, whether it has been logged as held open
-- since, whether the controller has gone unresponsive, and its latest
-- telemetry report.
alter table door_controllers
	add column door_state text not null default '',
	add column opened_at timestamptz,
	add column held_open boolean not null default false,
	add column unresponsive boolean not null default false,
	add column telemetry jsonb;

-- Alarms a door's state raises, such as a door forced open, are logged
-- with the outcome alert.
alter table events
	drop constraint events_outcome_check,
	add constraint events_outcome_check check (outcome in ('allowed', 'denied', 'alert'));

-- Every telemetry report, kept for the telemetry retention period.
create table telemetry_samples (
	door_id bigint not null references doors (id),
	at timestamptz not null,
	signal integer not null,
	latency_ms integer not null,
	uptime_s bigint not null,
	free_bytes bigint not null,
	firmware_version text not null,
	reconnects integer not null,
	door_open boolean not null,
	opens integer not null
);

create index telemetry_samples_door_at on telemetry_samples (door_id, at);
create index telemetry_samples_at on telemetry_samples (at);

-- Each door's reports summed up an hour at a time, kept longer than the
-- reports.
create table telemetry_rollups (
	door_id bigint not null references doors (id),
	hour timestamptz not null,
	samples integer not null,
	signal_mean double precision not null,
	signal_min bigint not null,
	signal_max bigint not null,
	latency_mean double precision not null,
	latency_min bigint not null,
	latency_max bigint not null,
	free_bytes_mean double precision not null,
	free_bytes_min bigint not null,
	free_bytes_max bigint not null,
	opens integer not null,
	primary key (door_id, hour)
);

create index telemetry_rollups_hour on telemetry_rollups (hour);
