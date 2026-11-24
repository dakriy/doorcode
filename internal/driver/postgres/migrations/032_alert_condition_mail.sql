-- What a condition's raise mail says, kept so that a raise that could not
-- be mailed when it happened can be mailed later: the name its mail calls
-- the subject by, the detail it gives, and whether that mail is owed.
-- An inactive condition is mailed when the raise it cleared was mailed or
-- held back by the hold-down, so that the hold-down after it applies.
alter table alert_conditions
	add column owed boolean not null default false,
	add column name text not null default '',
	add column detail text not null default '';

update alert_conditions set name = subject;
update alert_conditions set mailed = true where not active;
