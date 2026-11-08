-- Roles move from users onto groups: a user holds a role when one of their
-- groups carries it. Every user who held admin joins the seeded
-- administrators group, which carries it.
create table groups (
	id bigint generated always as identity primary key,
	slug text not null unique,
	name text not null,
	description text not null,
	roles text[] not null,
	preassigned_user_ids text[] not null
);

create table memberships (
	group_id bigint not null references groups (id) on delete cascade,
	user_id bigint not null references users (id),
	primary key (group_id, user_id)
);

create index memberships_user_id on memberships (user_id);

insert into groups (slug, name, description, roles, preassigned_user_ids)
values ('administrators', 'Administrators', 'Members may do everything.', '{admin}', '{}');

insert into memberships (group_id, user_id)
select groups.id, users.id from groups, users
where groups.slug = 'administrators' and 'admin' = any (users.roles);

alter table users drop column roles;
