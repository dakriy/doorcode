-- The latest certificate check of each TLS endpoint, by host:port: what
-- the connection reported when nothing was served, whether what was
-- served verified and why not, the certificates of the path (or of what
-- was served) and the trust anchor as JSON, and the days left.
create table certificate_checks (
	address text primary key,
	checked_at timestamptz not null,
	error text not null,
	verified boolean not null,
	problem text not null,
	chain jsonb not null,
	anchor jsonb,
	runway_days integer,
	leaf_days integer
);
