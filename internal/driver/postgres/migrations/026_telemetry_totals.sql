-- A controller now reports its counts as totals since it started rather
-- than since its report before. Each sample keeps those totals as
-- reported, and its other count columns keep what it counted since its
-- report before, which the server works out from the totals. Samples
-- from before this read as totals of zero.
alter table telemetry_samples
	add column total_reconnects integer not null default 0,
	add column total_opens integer not null default 0,
	add column total_checks_sent integer not null default 0,
	add column total_checks_lost integer not null default 0,
	add column total_tcp_sent bigint not null default 0,
	add column total_tcp_retransmits bigint not null default 0,
	add column total_tcp_drops bigint not null default 0,
	add column total_wifi_drops integer not null default 0,
	add column total_roams integer not null default 0;
