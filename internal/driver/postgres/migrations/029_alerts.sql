-- Alerts: which kinds of alert an administrator enabled (a kind without a
-- row is disabled), the state of each condition an alert is raised for,
-- and the outbox of mail waiting to be sent, sent, or given up on.
create table alert_kinds (
	kind text primary key,
	enabled boolean not null
);

-- One row per kind of alert and what it is about, such as a door: whether
-- it is raised now, whether its latest raise was mailed, and when it was
-- last raised or resolved.
create table alert_conditions (
	kind text not null,
	subject text not null,
	active boolean not null,
	mailed boolean not null,
	changed_at timestamptz not null,
	primary key (kind, subject)
);

create table mail_outbox (
	id bigint generated always as identity primary key,
	kind text not null,
	subject text not null,
	body text not null,
	recipients text[] not null,
	status text not null,
	attempts integer not null,
	created_at timestamptz not null,
	next_attempt_at timestamptz not null,
	last_attempt_at timestamptz,
	sent_at timestamptz,
	last_error text not null
);

create index mail_outbox_status on mail_outbox (status, id);
