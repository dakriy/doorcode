-- When a door's controller first downloaded the image of its latest
-- update with the update's token; null until it does.
alter table door_updates
	add column fetched_at timestamptz;
