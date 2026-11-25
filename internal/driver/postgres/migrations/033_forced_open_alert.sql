-- The kind of alert forced_open does not exist: its switch, its
-- conditions, and its mail still waiting in the outbox are removed. Mail
-- of it already sent or given up on stays in the outbox as it was, and
-- the events of doors are not touched.
delete from alert_conditions where kind = 'forced_open';
delete from alert_kinds where kind = 'forced_open';
delete from mail_outbox where kind = 'forced_open' and status = 'pending';
