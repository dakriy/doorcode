create table api_tokens (
	id bigint generated always as identity primary key,
	user_id bigint not null references users (id),
	name text not null,
	token_hash bytea not null unique,
	created_at timestamptz not null,
	expires_at timestamptz
);

create index api_tokens_user_id on api_tokens (user_id);
