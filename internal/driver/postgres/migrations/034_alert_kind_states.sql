-- Each kind of alert is in one of three states: off (not raised), on
-- (raised and listed, not mailed), or mail (raised, listed, and mailed).
-- A kind that mailed is in mail; one that did not is on.
alter table alert_kinds add column state text;
update alert_kinds set state = case when enabled then 'mail' else 'on' end;
alter table alert_kinds alter column state set not null, drop column enabled;

-- A muted condition was raised while its kind was not set to mail, and
-- its raise is never mailed. Mail owed by a kind that is not in mail is
-- owed no more: a raised condition of it is muted, and a one-off event
-- of it is not mailed.
alter table alert_conditions add column muted boolean not null default false;
update alert_conditions set owed = false, muted = active
	where owed and kind not in (select kind from alert_kinds where state = 'mail');
