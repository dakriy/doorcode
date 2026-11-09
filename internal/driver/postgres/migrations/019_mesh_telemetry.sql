-- Each report says where the controller sits in the site's Wi-Fi mesh: its
-- layer (0 when it reaches the building's access point itself), the
-- hardware id of the controller it hops through (empty at layer 0), and how
-- many controllers hop through it. Each hour's rollup keeps the layer and
-- children of its last report.
alter table telemetry_samples
	add column mesh_layer integer not null default 0,
	add column mesh_parent text not null default '',
	add column mesh_children integer not null default 0;

alter table telemetry_rollups
	add column mesh_layer integer not null default 0,
	add column mesh_children integer not null default 0;
