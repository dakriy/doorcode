create table users (
	id bigint generated always as identity primary key,
	email text not null unique,
	display_name text not null,
	password_hash text,
	roles text[] not null,
	expires_at timestamptz,
	deleted_at timestamptz,
	created_at timestamptz not null
);

create table sessions (
	token_hash bytea primary key,
	user_id bigint not null references users (id),
	created_at timestamptz not null,
	expires_at timestamptz not null
);

create index sessions_user_id on sessions (user_id);
