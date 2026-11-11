-- Every picture the site keeps at most one of, by kind: the sign-in
-- page's picture (sign_in), the logo (logo), and the favicon (favicon).
-- The sign-in picture and the logo move here from their own tables.
create table site_images (
	kind text primary key,
	image bytea not null,
	updated_at timestamptz not null
);

insert into site_images (kind, image, updated_at)
	select 'sign_in', image, updated_at from sign_in_image;
insert into site_images (kind, image, updated_at)
	select 'logo', image, updated_at from logo;

drop table sign_in_image;
drop table logo;
