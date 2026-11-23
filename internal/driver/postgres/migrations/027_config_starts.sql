-- A controller keeps how often it said it started since the configuration
-- its door was last sent went out with no report naming that
-- configuration since, and when the first of those starts was.
alter table door_controllers
	add column config_starts integer not null default 0,
	add column config_starts_since timestamptz;
