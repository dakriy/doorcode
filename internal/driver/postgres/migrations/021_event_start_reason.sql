-- A controller's start records why the controller said it started, as it
-- said it; every other event, and a start recorded before this, has none.
alter table events add column start_reason text not null default '';
