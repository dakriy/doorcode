-- Whether a session began with a sign-in at the identity provider, so
-- signing out can end the provider's session too.
alter table sessions add column via_identity boolean not null default false;
