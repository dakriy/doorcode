-- Nothing of the forced-open alarm is kept: its events, every mail of its
-- kind in the outbox, and any condition or switch of the kind. No table
-- refers to an event, and the event counts are kept in memory only.
delete from events where reason = 'forced open';
delete from mail_outbox where kind = 'forced_open';
delete from alert_conditions where kind = 'forced_open';
delete from alert_kinds where kind = 'forced_open';
