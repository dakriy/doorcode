-- Each report names the configuration the controller runs by its digest
-- (empty when it runs its defaults or does not say). A door keeps the
-- digest of the configuration the server last sent it, over the payload
-- as published, and when it was first sent, both null until it has been
-- sent one since. Its controller keeps since when its reports have named
-- another configuration, and the digest of the configuration it last said
-- it gave up and when.
alter table telemetry_samples
	add column config_digest text not null default '';

alter table doors
	add column sent_config_digest text,
	add column sent_config_at timestamptz;

alter table door_controllers
	add column config_mismatch_since timestamptz,
	add column config_reverted_digest text not null default '',
	add column config_reverted_at timestamptz;
