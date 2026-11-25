-- A telemetry report may carry no latency, which is stored as NULL. A
-- rollup counts the reports of its hour that carried one, and its latency
-- spread is NULL when none did. The rollups stored before count every
-- report of their hour, as their spread does.
alter table telemetry_samples alter column latency_ms drop not null;

alter table telemetry_rollups
	alter column latency_mean drop not null,
	alter column latency_min drop not null,
	alter column latency_max drop not null,
	add column latency_samples integer;
update telemetry_rollups set latency_samples = samples;
alter table telemetry_rollups alter column latency_samples set not null;
