-- A door closing after an opening the server timed records how many
-- milliseconds the door was open; every other event has none.
alter table events add column open_ms bigint;
