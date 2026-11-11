-- Each report says how the controller's links fared since its previous
-- one: the code checks it sent and those whose reply never came, the TCP
-- segments it sent for the first time, sent again, and dropped, and the
-- times its Wi-Fi station lost its access point and moved to another. It
-- also says the reason code of its latest loss of the access point since
-- it started and the BSSID and channel of the access point it has joined.
-- Each hour's rollup sums the counts and keeps the reason, BSSID, and
-- channel of its last report. Reports from before this read as zero and
-- empty.
alter table telemetry_samples
	add column checks_sent integer not null default 0,
	add column checks_lost integer not null default 0,
	add column tcp_sent bigint not null default 0,
	add column tcp_retransmits bigint not null default 0,
	add column tcp_drops bigint not null default 0,
	add column wifi_drops integer not null default 0,
	add column wifi_drop_reason integer not null default 0,
	add column roams integer not null default 0,
	add column bssid text not null default '',
	add column channel integer not null default 0;

alter table telemetry_rollups
	add column checks_sent integer not null default 0,
	add column checks_lost integer not null default 0,
	add column tcp_sent bigint not null default 0,
	add column tcp_retransmits bigint not null default 0,
	add column tcp_drops bigint not null default 0,
	add column wifi_drops integer not null default 0,
	add column wifi_drop_reason integer not null default 0,
	add column roams integer not null default 0,
	add column bssid text not null default '',
	add column channel integer not null default 0;
