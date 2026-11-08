-- A door's configuration overrides: each value keyed by the name of the
-- field it overrides, the site's defaults applying to the rest.
alter table doors add column config jsonb not null default '{}';
