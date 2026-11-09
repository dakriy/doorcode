-- The picture the sign-in page shows, at most one: the only_one column
-- can hold only true, so a second row would repeat the primary key.
create table sign_in_image (
	only_one boolean primary key default true check (only_one),
	image bytea not null,
	updated_at timestamptz not null
);
