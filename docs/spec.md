# doorcode: specification

doorcode is a self-hosted access-control server for keypad doors. Door
controllers hold a persistent MQTT connection to the server, which is the
single source of truth for who may enter, when doors are open or locked, how
each door is configured, and which firmware it runs. Administrators manage
everything from a web UI; users manage their own code from a self-service
portal; scripts use a documented JSON API.

This document is the product contract. AGENTS.md is the engineering contract.

## 1. Concepts

| Concept | Meaning |
| --- | --- |
| User | A person with a door code. Identified by an external id (the "user id", e.g. an employee or student number) and an email. |
| Group | A named set of people. A group carries roles and pre-assigned user ids, nothing about doors. |
| Schedule | A reusable, named time pattern: weekly windows, an optional date range, excluded date ranges. Has no type of its own. |
| Door | A physical controller, enrolled over MQTT and approved by an admin. A door is in any number of zones, which decide who may enter it and when it is in open mode. |
| Zone | A named set of doors and the schedules during which those doors are in *open mode* (any keypress unlocks). Access rules name zones. |
| Access rule | Group + schedule + zones: members of the group may enter every door in those zones with their code while the schedule is active. |
| Override | A per-door window that forces *open* or *locked*, created by an admin or from the keypad. |
| Role | A privilege (admin, manage-users, ...) carried by a group. |
| Event | Anything the door did or was told to do, with who and why. The activity log. |
| Access request | Free-text request from a user to an admin. |
| Firmware | An uploaded image with a version string, assignable to all doors by default or to one door as an override. |

A user may enter a door when an access rule names one of their groups and a
zone the door is in, and its schedule is active. Group membership alone
grants nothing. A door is in open mode while any zone it is in has an active
open-mode schedule; open mode belongs to zones, not to any group. To let a
group in at a single door, put that door in a zone of its own.

## 2. Access decision

A door submits a keypad entry of the form `<code>[*<command>]`. The server
looks the code up by hash and evaluates, in order:

1. Rate limit exceeded for this door: deny, reason `rate limited`.
2. Known user whose account is expired or deleted: deny, reason `expired`.
3. Command present: if the user holds *door-commander* and the command is
   valid, run it and allow; otherwise deny.
4. User holds *door-commander*: allow, at any door, under any override.
5. Active *locked* override: deny. Else active *open* override: allow.
6. An open-mode schedule of one of the door's zones is active: allow
   (unknown codes included).
7. An access rule naming one of the door's zones and one of the user's
   groups has an active schedule: allow.
8. Otherwise deny, reason `no rule`.

Every evaluation is logged as an event with outcome, reason, user if known,
and the rule that decided. Unknown codes are logged without a user. While a
door is in open mode it unlocks locally on any keypress and asks the server
nothing, so open-mode use is measured only by the telemetry open count.

Keypad commands (opcode `00`): `001<minutes>` creates an open override,
`002<minutes>` a locked override, `000` cancels all active overrides on this
door. Commands are logged as events naming the commander.

## 3. Users and codes

Fields: user id (unique, optional for local-only accounts), email (unique,
lowercased), first name, last name, display name, optional local password,
door code (stored as a salted hash, globally unique), expires at, soft-delete
timestamp, groups.

Codes are digits only.

- Self-service code = the user id with leading zeros stripped, followed by 4
  to 8 digits chosen by the user. A user without a user id cannot set a code
  in self-service.
- Admin-set code = 6 to 20 raw digits, no prefix.
- The keypad submit key terminates entry.

New users created through sign-in expire after a configurable period
(default 5 years). Expired or deleted users are denied everywhere and cannot
sign in. Deleting a user or a door is a soft delete; its events stay.

## 4. Groups, roles, zones, and access rules

A group has a slug (unique, URL-safe), a name, a description, members,
roles, and a list of pre-assigned user ids: a user whose user id is listed
joins the group on first sign-in (unless entitlements decide groups, see
section 10) or import, and the id then leaves the list. Groups contain
people only.

A zone has a name, a description, a set of doors, and a set of open-mode
schedules. A door may be in many zones and is in open mode during the
open-mode schedules of every one of them. Deleting a zone keeps its doors;
its access rules and open mode stop reaching them.

An access rule has a group, a schedule, and one or more zones; it reaches
every door in those zones, including doors added later. A group's page
lists its rules; a door's page derives who may enter and when from every
rule that reaches it through its zones. Renaming or deleting a group never
changes when a door is unlocked.

Roles: `admin` (everything), `manage-users`, `manage-doors`,
`manage-groups`, `manage-schedules`, `manage-firmware`, `view-logs`,
`door-commander`. A user holds a role if any of their groups carries it.
Group lists show role badges. Users cannot remove their own admin role.

## 5. Schedules

A schedule has a name, an optional effective date range, one or more weekly
windows (set of weekdays, start time, end time, in the site timezone), and a
list of excluded date ranges, each a first and a last day, both included (a
single day is a range of one day). No window opens on a day of an excluded
range; one that opened the day before still runs past midnight. It is
entered through a form, never as recurrence text. A window may cross
midnight. The same schedule can drive the open mode
of any number of zones and any number of access rules. A schedule named
*Always* is seeded and every access rule requires a schedule. A schedule a
zone's open mode or an access rule uses cannot be deleted.

The server expands the open-mode schedules of a door's zones and its
overrides into absolute intervals: the door's *timeline*. Locked overrides win over open
overrides, which win over schedules. Calendars in the UI render the same
timeline, and a user's access calendar overlays the intervals of the access
rules that reach them.

## 6. Overrides

Per door: type (open or locked), start, end, reason, creator. Admins create,
edit, cancel, and delete them. Keypad commands create and cancel them.

## 7. Doors

Fields: name (unique), location, notes, public flag (visible in the user
portal), pictures, zones, firmware override, hardware id, credentials,
config overrides, current state (online, door open or closed, firmware
version, last seen, last telemetry), soft-delete timestamp.

### Enrollment

A controller boots with shared enrollment credentials, connects, and
announces its hardware id, firmware version, and signal strength. It appears
in the admin UI as pending for as long as it stays connected. On approval the
admin sets name, location, pictures, zones, and optional firmware override;
the server creates per-door broker credentials and sends them to the door,
which reconnects as itself.

Credentials rotate automatically at a configurable interval and on demand
from the door page. A door has at most two live credentials: the server
issues the new one over the existing connection and keeps the old one valid
until the door connects with the new one, which revokes the old. While a
door has not yet connected with its newest credential, rotation issues
nothing more: the periodic rotation waits, and rotating from the door page
or the API is refused. Revocation from the door page disconnects the door.

### Pictures

A door has any number of pictures, uploaded from the door page or the API.
One is the primary picture, which the admin chooses on the door page and
which the admin UI shows in door lists and on the door page. The user
portal shows it only while the setting *door pictures in the portal* is on
(off by default); while it is off, only those who see doors in the admin UI
can load a door's pictures. Pictures are downsized on upload, capped in
size, and stored in the database.

### Configuration

Door configuration has site defaults editable in settings and per-door
overrides. Fields: strike pulse duration, keypad idle clear time, minimum and
maximum code length, submit key, reply timeout, telemetry interval, timeline
refresh interval, credential rotation interval, rate limit (checks per minute
and burst), NTP server, log level. The server pushes the effective config on connect and on change. Pin
mappings are firmware concerns and are not here.

### Timeline cache

The server pushes the next 25 hours of the door's timeline on connect,
whenever its zones, the open-mode schedules of its zones, those schedules,
or its overrides change, and at least every 6 hours. A disconnected door follows its cached timeline. Codes are never
stored on the door; without a connection, code entry is denied and nothing is
logged until the door reconnects.

### Rate limiting

A token bucket per door protects against code enumeration. Over the limit the
server does not evaluate the code and logs a denied event. The default is
generous enough that a person typing codes is never affected.

### Open from the UI

An admin with *manage-doors* or *door-commander* can pulse a door's strike
from the door page. It is logged as an event naming the admin.

### Telemetry and state

Doors report, at the configured interval: signal strength, round-trip
latency, uptime, free memory, firmware version, reconnect count, door open or
closed, and the number of opens since the last report. Outside open mode,
doors also send door open and close transitions as events. The server derives
*forced open* (opened without a preceding unlock) and *held open* (open longer
than a configurable duration) and shows them live. A door with no contact for
a configurable period is *unresponsive*.

## 8. Firmware

Admins with *manage-firmware* upload images with a version string through
the UI or the JSON API (CI uploads releases). Signature verification is a
later addition; today any file is accepted. One version is the *default*; a
door may carry a per-door override. A door whose reported version differs
from its target is *pending update*.

Updates roll out inside an *update window* (daily or weekly, with start
time and duration, in the site timezone), at most N doors at a time. A door
can also be updated immediately from its page. The server sends an update
command over MQTT with an authenticated HTTPS download URL; the door fetches,
installs, reboots, reconnects, and acknowledges. If it cannot reconnect it
reverts on its own and reappears with the old version. Rollout continues in
the next window until every door matches its target.

## 9. MQTT protocol

Payloads are CBOR maps with a `v` protocol version field. Topic prefix
`doorcode/`. Each door's broker ACL allows only its own topics.

| Direction | Topic | Content |
| --- | --- | --- |
| door → server | `enroll/<hwid>` | hardware id, firmware version, signal |
| server → door | `enroll/<hwid>/credentials` | door id, username, token |
| door → server | `doors/<id>/check` | request id, digits entered |
| server → door | `doors/<id>/reply` | request id, allow or deny, strike duration |
| door → server | `doors/<id>/event` | door opened, door closed, command result, update result |
| door → server | `doors/<id>/telemetry` | the telemetry fields above |
| door → server | `doors/<id>/online` | retained, with a last-will of offline |
| server → door | `doors/<id>/timeline` | retained merged intervals |
| server → door | `doors/<id>/config` | retained effective config |
| server → door | `doors/<id>/command` | open, update (url, version, hash), reboot, new credentials |
| door → server | `doors/<id>/ack` | command id, result |

The broker is Mosquitto with its dynamic-security plugin; the server manages
clients and ACLs over `$CONTROL`. All connections use TLS.

Every controller enrolls with the same shared username and password, and
must connect with its hardware id as its MQTT client id. The shared
client's ACLs use the plugin's `%c` substitution: a connection may publish
only `doorcode/enroll/%c` and subscribe only to
`doorcode/enroll/%c/credentials`, so a controller can neither announce
itself as another nor read another's credentials. When the enrollment
password setting is empty on first start, the server generates one, stores
it, and logs it.

The server publishes a door's config and timeline retained to every door a
controller is enrolled as that is not deleted, connected or not, so a
controller that reconnects finds them current before the server notices it.
The timeline holds `from` and `to` and the spans between them, each a
`start` and `end` in Unix seconds and a `state` of `open` or `locked`.

## 10. Identity and sign-in

Local sign-in with email and password is always available to accounts that
have a password. OIDC sign-in can be enabled in settings with issuer, client
id and secret, scopes, and the claim names that supply the user id, email,
first name, last name, and entitlements. Users are matched by user id first,
then email; a new user is created on first sign-in with the default expiry.

When the entitlements claim name is set and a sign-in's ID token or userinfo
carries that claim, the user's group memberships become exactly the existing
groups whose slugs appear among its values: missing memberships are added and
all others removed. Values naming no group are ignored, groups are never
created, and nothing records where a membership came from. A sign-in that does
not carry the claim leaves memberships unchanged. Because exact matching would
undo them, the default groups for new users and the pre-assigned joins apply
only while the entitlements claim name is empty; then sign-in leaves
memberships alone apart from those joins on first sign-in.

When *auto sign-in* is on, visiting the sign-in page redirects to the
provider unless `auto_sign_in=false` is in the query string, and the
self-service password form is hidden.

Sessions are server-side with a cookie. Personal API tokens (created by the
user or an admin, shown once, optional expiry, revocable) and door tokens are
stored hashed. The JSON API accepts a token as a bearer header or as the
password of HTTP Basic auth.

On first start with no users, the server creates a local admin and prints
its password to the log with instructions to change it.

## 11. Admin UI

Pages: dashboard, doors (list, pending enrollments, detail with live state,
timeline calendar, pictures, zones and the open mode they give it,
who-can-enter, config, firmware, open button, events), zones (doors and
open-mode schedules), users (list, detail, groups, access
calendar per door, events), groups (list with role badges, detail with
members, roles, access rules, pre-assigned user ids), access rules,
schedules (list, form, calendar, where used), overrides,
firmware (versions, default, update window, rollout progress), events
(filters by door, user, outcome, reason, type, date), access requests
(reply, resolve), bulk import (CSV of user id and group slugs, preview
before apply), settings (sign-in, identity claims, defaults, door config
defaults, theme, login image, timezone, with env-provided values shown
read-only), API docs.

Lists are searchable, filterable, server-paginated, and update live over
Server-Sent Events where state changes without a page action (door status,
enrollments, dashboard). Every destructive action confirms. Forms validate
inline. Light and dark themes follow the browser; theme colors and the
sign-in page image are settings.

## 12. User portal

Home, set door code, set password (hidden under auto sign-in), access
calendar per public door (with its primary picture while door pictures in
the portal is on) showing when it is unlocked (its zones' open mode
and its overrides) and when the user's code works (the access rules that
reach it through its zones), my recent entries, personal API tokens, access requests (create,
edit while unresolved, delete), sign out.

## 13. Dashboard and analytics

Time range selectable. Entries per day, entries by hour of day and weekday,
unique users, allowed versus denied with denial reasons, most and least
active doors, door uptime, latency and signal trends per door, firmware
version distribution, open access requests, doors currently open,
unresponsive doors, pending enrollments, pending updates.

## 14. JSON API and documentation

Every admin and self-service capability is available under `/api/v1` with
bearer-token auth, including firmware upload. The OpenAPI document is
generated from the same route table that registers the routes, served at
`/api/v1/openapi.json`, checked in, and verified not to drift. Swagger UI is
served from the app.

## 15. Operations

A single binary plus Postgres and Mosquitto, run with docker compose.
Migrations apply automatically on startup. Configuration comes from settings
in the database, with any setting overridable by environment variable (then
read-only in the UI). The environment also supplies the database URL, the
broker address and admin credentials, the listen address, and the key that
encrypts stored secrets.

`doorcode-demo` is a second binary that runs the same application over
seeded in-memory stand-ins with simulated doors, so the UI can be explored
without Postgres, a broker, or data entry.

Not in scope: SCIM, email or push notifications, codes stored on doors,
compatibility with the previous HTTP door protocol.
