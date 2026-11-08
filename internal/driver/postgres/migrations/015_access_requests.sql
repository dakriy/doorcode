-- Access requests: what a user asks an administrator in their own words,
-- and the administrator's reply. A request is resolved once resolved_at is
-- set; until then its user may change or withdraw it.
create table access_requests (
	id bigint generated always as identity primary key,
	user_id bigint not null references users (id),
	title text not null,
	body text not null,
	reply text not null,
	resolved_at timestamptz,
	created_at timestamptz not null,
	updated_at timestamptz not null
);

create index access_requests_user on access_requests (user_id, created_at desc, id desc);
