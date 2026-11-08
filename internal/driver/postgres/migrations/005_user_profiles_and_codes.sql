-- A user gains an optional external user id, first and last names, and a
-- door code kept only as a hash. The hash is deterministic under the site's
-- code hashing parameters, stored once, so codes are unique and can be
-- looked up.
alter table users
	add column user_id text unique,
	add column first_name text not null default '',
	add column last_name text not null default '',
	add column code_hash text unique;

create table code_hashing (
	singleton boolean primary key default true check (singleton),
	parameters text not null
);
