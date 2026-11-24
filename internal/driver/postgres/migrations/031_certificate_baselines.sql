-- How many checks in a row of each endpoint reached no server, and the
-- shape of the chain it last served verified as JSON (each certificate's
-- subject and issuer, leaf first, then the anchor, and since when).
alter table certificate_checks
	add column failures integer not null default 0,
	add column baseline jsonb;
