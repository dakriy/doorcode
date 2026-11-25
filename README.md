# doorcode

doorcode is a self-hosted access-control server for keypad doors. Door
controllers hold a persistent MQTT connection to the server, which is the
single source of truth for who may enter, when doors are open or locked, how
each door is configured, and which firmware it runs. Administrators manage
everything from a web UI, people manage their own door code from a
self-service portal, and scripts use a documented JSON API.

It is a general-purpose, public project: nothing in it is tied to a
particular organization. The product contract is [docs/spec.md](docs/spec.md);
the engineering contract is [AGENTS.md](AGENTS.md).

## Features

- **Access model.** Users belong to groups; groups carry roles and
  pre-assigned user ids. Doors are put in zones. Access rules name a group,
  a schedule, and zones: members may enter every door in those zones with
  their code while the schedule is active. Zones also carry open-mode
  schedules: a door is in open mode (any keypress unlocks) while any zone it
  is in has one active. Doors have per-door open or locked overrides, also
  creatable from the keypad by door commanders.
- **Schedules.** Weekly windows (crossing midnight if need be), an optional
  date range, and excluded date ranges (a holiday, a week's closure),
  entered through a form and shown on calendars. Every door's timeline of
  open and locked time is pushed to it, so a disconnected door keeps
  following it.
- **Codes.** Stored only as salted hashes, never on doors, at a fixed cost
  far below a password's (argon2id, 2 MiB, one pass) so code checks stay
  fast; the password hashing setting does not apply. Administrators set
  codes of 6 to 20 digits; people choose their own as their user id followed
  by 4 to 8 digits. A per-door rate limit protects against guessing.
- **Doors.** Enrollment over MQTT with administrator approval, per-door
  broker credentials that rotate automatically, pictures, zones, per-door
  configuration overrides over site defaults, opening from the UI, and live
  state: online, open or closed, held open, unresponsive.
- **Telemetry.** Signal, latency, uptime, free memory, reconnects, opens,
  and where the controller sits in the site's Wi-Fi mesh, kept as samples
  and hourly summaries, charted on each door's page; a door that hops
  through another controller carries a mesh badge in the door list, and a
  mesh page draws the whole mesh by layer, with relays and each door's path
  to the access point.
- **Firmware.** Uploads through the UI or the API, a default version and
  per-door overrides, rollout inside an update window a few doors at a time,
  update-now from a door's page, and provisioning files to flash new
  controllers with, for the site or one door.
- **Events.** Every code check, keypad command, UI opening and reboot,
  alarm, opening and closing of a door (in open mode too, each closing
  with how long the door was open), controller start, and update is
  logged with who and why, and searchable by door,
  user, type, outcome, reason, and date. The user is picked by searching
  the server as you type, so the page stays fast with thousands of users.
- **Dashboard.** Entries per day, a weekday by hour heatmap, denial reasons,
  the busiest and quietest doors, door uptime, signal and latency trends,
  firmware versions, and live counts.
- **People.** Local passwords and OpenID Connect single sign-on (matching by
  user id then email, an entitlements claim setting group membership exactly), a user portal
  (door code, password, access calendars, recent entries, access requests,
  API tokens), administrators setting or removing a user's password
  (which ends that user's sessions), bulk import of users from CSV with a
  preview, and access requests answered by administrators.
- **API.** Everything the UI does is under `/api/v1`, described by an
  OpenAPI 3.1 document at `/api/v1/openapi.json` and browsable at
  `/api-docs`. Personal API tokens are accepted as a bearer token or as the
  HTTP Basic password. Deleting a user or a door keeps its history and can
  be undone; `DELETE /api/v1/users/{id}?force=true` and
  `DELETE /api/v1/doors/{id}?force=true` instead remove it for good with
  everything that refers to it (its events, telemetry, sessions, tokens,
  and so on) and free its name, email, or user id. That is meant for
  integrations that make temporary users and doors for testing, needs the
  `admin` role, and is offered by no page.
- **Alerts.** Mail to a site-wide list of recipients when a door is held
  open, offline, or unresponsive, runs a configuration other
  than the one it was sent, or fails a firmware update, or when the
  certificates the broker or the device address serve stop verifying,
  run short, or cannot be reached, once when raised and once more when it
  clears; and once when such a chain changes shape. Each
  kind of alert is off, on (raised and listed, not mailed), or set to mail,
  and is on until an administrator sets it otherwise; a condition that
  comes back soon after clearing mails nothing, and mail waits in a durable
  outbox that is retried with growing pauses, so a slow or failing mail
  server never holds up the doors.
- **Settings.** Edited in the UI, each overridable by an environment
  variable (and then read-only in the UI). Secret settings are encrypted at
  rest. Light and dark themes follow the browser; the accent color, the
  logo, the favicon, and the sign-in page picture are settings.

## Try the demo

The demo runs the real application over seeded in-memory data with simulated
doors. It needs neither Postgres nor a broker, and nothing it does outlives
it.

```sh
docker build --target doorcode-demo -t doorcode-demo .
docker run --rm -p 8080:8080 --add-host=host.docker.internal:host-gateway doorcode-demo
```

or, with Go installed, `go run ./cmd/doorcode-demo`. Open
<http://localhost:8080> and sign in with one of the two accounts the demo
prints at start: the administrator (`admin@example.com` and
`doorcode-demo`), or a member of staff with a door code and no role
(`user@example.com` and `doorcode-demo`), who sees the user portal: their
access to the public doors, with pictures, and their own entries. It comes
with users, groups, zones, schedules, access rules, overrides, doors with
pictures (and three dozen office doors in a zone of their own), a few weeks of events and telemetry, firmware versions, and access
requests. One simulated controller waits for approval under Enrollments; the
others are online (three of them reaching the server through others over
the mesh), report telemetry, open and close, answer open commands,
reboot and connect again when told to, and check codes now and then, so the
live pages move. The demo listens on
`-listen`, which defaults to `DOORCODE_LISTEN_ADDR` and else `:8080`.
Settings work as in the server: a `DOORCODE_` variable such as
`DOORCODE_SITE_NAME` provides its setting, and the demo then keeps that
value instead of seeding its own, logging each setting it leaves alone. With
`DOORCODE_ENROLLMENT_PASSWORD` set, the simulated controllers enroll with
it.

The demo sends its mail to a [Mailpit](https://mailpit.axllent.org) on the
same machine, which catches every message and delivers none: it presets
`DOORCODE_MAIL_HOST` (`localhost`, or `host.docker.internal` in the image,
the Docker host), `DOORCODE_MAIL_PORT` (`1025`), `DOORCODE_MAIL_SECURITY`
(`none`), and `DOORCODE_MAIL_FROM` (`doorcode demo <doorcode@example.com>`),
which the Mail tab shows as set by the deployment, and a variable the demo
is started with wins over its preset. Start one first with
`docker compose -f docker-compose.dev.yml up -d --wait mailpit` (or
`docker run -d -p 1025:1025 -p 8025:8025 axllent/mailpit:v1.29.6`); Send
test email on the Mail tab, and alert mails once alerts are set to mail, then
show at <http://localhost:8025>.

## Deploy with docker compose

`docker-compose.example.yml` runs the published server (`app`) with
PostgreSQL 18 and Mosquitto 2 with its dynamic-security plugin. Migrations
apply on start. Each release is published as `ghcr.io/dakriy/doorcode`,
tagged with its version (`1.2.3`), its minor line (`1.2`), and `latest`; the
example follows a minor line, and `docker compose pull` followed by `docker
compose up -d --wait` upgrades within it. It expects a proxy on the same
host that terminates TLS: the web server listens on `127.0.0.1:8080` and
the broker on `127.0.0.1:1883`, and the proxy serves the broker over TLS
(usually on 8883) with a certificate from the CA the doors trust.

1. In a checkout of this repository (the broker mounts files from
   `deploy/mosquitto`), copy the example to the file compose reads. Git
   ignores `docker-compose.yml` and `.env`, so a local deployment
   never shows as a change:

   ```sh
   cp docker-compose.example.yml docker-compose.yml
   ```

2. Generate the key that encrypts stored secrets and the database and broker
   passwords, and put them in a `.env` file next to `docker-compose.yml`,
   which compose reads every time:

   ```sh
   cat > .env <<EOF
   DOORCODE_SECRET_KEY=$(openssl rand -base64 32)
   DOORCODE_POSTGRES_PASSWORD=$(openssl rand -hex 24)
   DOORCODE_BROKER_ADMIN_PASSWORD=$(openssl rand -hex 24)
   EOF
   ```

   Keep the file. The passwords go into URLs, so they keep to URL-safe
   characters, and take effect only when the volumes are new. The server
   refuses to start without a valid key. On its first start the server also
   makes the key it signs firmware with and stores it encrypted under this
   one, so keep the key: secrets stored under one key cannot be read under
   another, and a server started with another refuses to start rather than
   make a new signing key. The firmware page shows the signing key's
   fingerprint, and every door is sent its public half to check firmware
   with.

3. Start everything and wait for it to be healthy:

   ```sh
   docker compose up -d --wait
   ```

4. Read the first administrator's password, and the generated enrollment
   password controllers enroll with, from the log:

   ```sh
   docker compose logs app
   ```

   Sign in at <http://localhost:8080> as `admin@example.com` (the
   `admin_email` setting) and change the password from the portal. The
   password is generated and shown once, in the log of the first start.
   `DOORCODE_ADMIN_PASSWORD` gives it instead, for development; a
   production deployment leaves that unset.

5. In Settings, set `public_url` to the `https://` address people's
   browsers reach the server at (such as `https://doorcode.example.com`).
   Single sign-on needs it. An `http://` address is accepted for
   development only.

   Doors verify the server, when they download firmware, against the same
   certificates as the broker (`door_broker_ca_pem`), and trust no others.
   Set `device_url` to the address doors download from (such as
   `https://devices.doorcode.example.com`). It may be the entrypoint people
   use, when `door_broker_ca_pem` holds the root of that entrypoint's CA,
   or a second entrypoint wherever TLS is terminated, such as a second port
   or host name on the reverse proxy forwarding to the same server, serving
   a chain from an internal CA the doors trust. No firmware update is sent
   while `device_url` is empty; `public_url` is never used for doors.

   Set `door_broker_ca_pem` to the root CA certificates of the broker and
   of `device_url` (see
   [Choosing the certificates doors trust](#choosing-the-certificates-doors-trust)),
   and `door_broker_host` to the host name
   doors reach the broker at. List the proxy in `trusted_proxies` (see below); a
   proxy on the host reaches the server from the compose network's gateway
   address.

Keep the `postgres` and `mosquitto` volumes backed up. Each door holds one
broker connection open, so the compose files raise the broker's open file
limit to 65536; a broker run elsewhere, and the proxy in front of it, need
the same, since the usual default of 1024 refuses connections short of 1000
doors.

The server answers two unauthenticated probes, which log nothing:

- `GET /healthz` is the liveness probe. It answers 200 `ok` while the
  process serves; restart the server when it stops answering.
- `GET /readyz` is the readiness probe. It answers 200 `ok` when the
  database answers a ping (within two seconds) and the connection to the
  broker is up, and otherwise 503 with what is not ready, one per line
  (`store: <error>`, `broker: not connected`); send no traffic to the
  server while it answers 503.

The `app` service's healthcheck asks `/readyz`; the image's own
`HEALTHCHECK` asks `/healthz`.

Behind a reverse proxy that terminates TLS, list the proxy in the
`trusted_proxies` setting (such as `DOORCODE_TRUSTED_PROXIES=10.0.0.0/8` or
the proxy's host name). The server believes `X-Forwarded-For` (the client
address it logs for sign-ins, refused sign-ins, and failed requests) and
`X-Forwarded-Proto` (whether cookies are marked `Secure`) only from a
connection whose address is on that list, and ignores both from anyone else.

### Choosing the certificates doors trust

Doors trust exactly the certificates in `door_broker_ca_pem`, for the
broker and for `device_url`, and nothing else: no list of public CAs is
built in. Paste in, one after another, the root CA certificate that each
of those hosts' certificate chains leads to. A host's certificate from
your own CA needs that CA's certificate; one from a public CA needs that
CA's root. Every certificate takes a door's memory on each connection, so
give only the ones the hosts need, never a whole bundle of public roots.

To find the root a host's chain leads to, connect to it and look at the
chain it sends:

```sh
openssl s_client -connect broker.example.com:8883 -showcerts </dev/null
```

Each certificate is listed with its subject (`s:`) and issuer (`i:`). A
server seldom sends its root, so the root is usually the issuer of the
last certificate listed; when that certificate's subject and issuer are
the same, it is the root itself. Get the root's PEM from its CA's website,
or from Mozilla's root list, which curl publishes as one file at
<https://curl.se/docs/caextract.html> with each certificate under its name.
Copy everything from its `-----BEGIN CERTIFICATE-----` line through its
`-----END CERTIFICATE-----` line.

Before saving, check that the certificates verify each host:

```sh
openssl s_client -connect broker.example.com:8883 -CAfile roots.pem </dev/null
```

where `roots.pem` holds what you are about to paste. The output ends in
`Verify return code: 0 (ok)` when a door would accept the host. When a CA
announces new roots, add them alongside the ones in use before its
certificates move to them, and remove an old one only once no host's chain
leads to it.

## Monitoring with Prometheus

`GET /metrics` answers a Prometheus scrape in the text exposition format
0.0.4 (`Content-Type: text/plain; version=0.0.4; charset=utf-8`). Unlike
the probes it needs an API token, sent as a bearer token (or as the HTTP
Basic password), whose user holds the `view-metrics` role; `admin` holds
it too. Without a token it answers 401, and with the token of a user
lacking the role 403. A successful scrape logs nothing. It is not part of
the JSON API and not in the OpenAPI document.

To make the token Prometheus scrapes with:

1. Create a user for Prometheus (Users, or `POST /api/v1/users`).
2. Create a group carrying the `view-metrics` role and no other (Groups, or
   `POST /api/v1/groups` with `"roles":["view-metrics"]`) and add the user
   to it.
3. On the user's page, under API tokens, create a token (or
   `POST /api/v1/users/<id>/tokens` as an administrator), and copy the
   secret: it is shown once.
4. Save the secret in a file Prometheus reads, and name that file in the
   job's `authorization.credentials_file`.

[`deploy/prometheus/`](deploy/prometheus) holds an example scrape
configuration (`prometheus.yml`) and example alert rules (`alerts.yml`):
a door offline or unresponsive, held open, a weak signal, lost
code checks, controller restarts by panic or watchdog, a spike of denied
or rate-limited checks, a failed firmware update, a lasting configuration
mismatch, and the server, its broker, or its store down.

What a scrape holds:

- Every metric is named `doorcode_`, in base units (seconds, bytes), and
  every counter ends `_total`. Families and series come in a stable
  order, and every family prints its `# HELP` and `# TYPE` even with no
  series. No series is labelled by user.
- Per-door series carry `door` (the door's name) and `door_id`. A door
  has them while it is not deleted and has a controller.
- The gauges read from a door's latest telemetry report (signal, latency,
  free bytes, uptime, mesh layer, mesh children, Wi-Fi channel) are shown
  only while its controller is online, so an offline door has no
  telemetry gauges and nothing draws stale values. It keeps
  `doorcode_door_online`, `doorcode_door_last_seen_timestamp_seconds`, its
  other state gauges, and the totals of its last report.
  A door whose controller has never reported has no telemetry gauges or
  report totals at all.
- `doorcode_events_total` and the HTTP request counters are kept in
  memory and start from zero when the server starts; Prometheus treats
  the drop as a counter reset. Every door that is not deleted and has a
  controller always shows a `doorcode_events_total` series, at 0 until
  its first event, for each alarm reason and for a controller start with
  each `start_reason` (empty and `other` included), so `increase()` sees
  the first alarm or crash and the example rules use it alone. Every
  other kind of event appears with its first event, which `increase()`
  misses.
- The per-door `_total` counters are the totals since the controller
  started that its latest report gave, so they reset when the controller
  restarts.
- When the database fails one of the scrape's reads, the scrape still
  answers 200, leaves out the series that read gives (every per-door
  series, `doorcode_events_total`, and `doorcode_firmware_updates`; or
  `doorcode_open_requests`), logs the error, and sets `doorcode_store_up`
  to 0.

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `doorcode_door_online` | gauge | `door`, `door_id` | 1 while the door's controller is connected to the broker, else 0. |
| `doorcode_door_unresponsive` | gauge | `door`, `door_id` | 1 while the controller is connected but has not been heard from for `unresponsive_minutes`, else 0. |
| `doorcode_door_last_seen_timestamp_seconds` | gauge | `door`, `door_id` | When the server last heard from the controller, in Unix seconds. |
| `doorcode_door_open` | gauge | `door`, `door_id` | 1 while the door is open, as its controller last said, else 0. |
| `doorcode_door_held_open` | gauge | `door`, `door_id` | 1 while the door has been open longer than its `door_held_open_s` while it should be locked, else 0. |
| `doorcode_door_config_mismatch` | gauge | `door`, `door_id` | 1 while the controller runs a configuration other than the one it was last sent (the door list's Mismatched configuration, Restarting with configuration, or Reverted configuration badge), else 0. |
| `doorcode_door_update_pending` | gauge | `door`, `door_id` | 1 while the door takes part in the firmware rollout and does not run its target yet, else 0. |
| `doorcode_door_firmware_info` | gauge | `door`, `door_id`, `version` | Always 1; `version` is the firmware version the controller runs. |
| `doorcode_door_signal_dbm` | gauge | `door`, `door_id` | Wi-Fi signal strength last reported, in dBm. Online doors only. |
| `doorcode_door_latency_seconds` | gauge | `door`, `door_id` | Round trip to the server last reported, in seconds. Online doors only. |
| `doorcode_door_free_bytes` | gauge | `door`, `door_id` | Free memory last reported, in bytes. Online doors only. |
| `doorcode_door_uptime_seconds` | gauge | `door`, `door_id` | How long the controller had run when it last reported, in seconds. Online doors only. |
| `doorcode_door_mesh_layer` | gauge | `door`, `door_id` | Mesh layer last reported, 0 when it reaches the access point itself. Online doors only. |
| `doorcode_door_mesh_children` | gauge | `door`, `door_id` | How many controllers hopped through this one when it last reported. Online doors only. |
| `doorcode_door_wifi_channel` | gauge | `door`, `door_id` | Wi-Fi channel of the access point last reported, 0 with no link. Online doors only. |
| `doorcode_door_opens_total` | counter | `door`, `door_id` | Times the door was opened since its controller started. |
| `doorcode_door_reconnects_total` | counter | `door`, `door_id` | Times the controller connected to the broker again since it started. |
| `doorcode_door_checks_sent_total` | counter | `door`, `door_id` | Code checks the controller sent since it started. |
| `doorcode_door_checks_lost_total` | counter | `door`, `door_id` | Code checks whose reply never reached the controller since it started. |
| `doorcode_door_tcp_segments_total` | counter | `door`, `door_id` | TCP segments the controller sent for the first time since it started. |
| `doorcode_door_tcp_retransmits_total` | counter | `door`, `door_id` | TCP segments the controller sent again since it started. |
| `doorcode_door_tcp_drops_total` | counter | `door`, `door_id` | TCP segments the controller dropped since it started. |
| `doorcode_door_wifi_drops_total` | counter | `door`, `door_id` | Times the controller lost its Wi-Fi access point since it started. |
| `doorcode_door_roams_total` | counter | `door`, `door_id` | Times the controller moved to another access point by its own choice since it started. |
| `doorcode_events_total` | counter | `door`, `door_id`, `type`, `outcome`, `reason`, `start_reason` | Events recorded since the server started, as the events page lists them. A deleted door's series stay until it is deleted for good. `start_reason` is the reason a controller gave for starting (`power_on`, `software`, `panic`, `watchdog`, `brownout`, `external`, or `unknown`), on `type="door"`, `reason="started"`, and empty on every other event; any other value a controller gives is counted as `other`, so firmware cannot grow the series without limit, while the event log keeps the value as given. |
| `doorcode_pending_enrollments` | gauge | none | Controllers waiting to be approved as doors. |
| `doorcode_open_requests` | gauge | none | Access requests waiting for an answer. |
| `doorcode_firmware_updates` | gauge | `state` | Doors taking part in the firmware rollout by their state in it: `pending`, `updating`, `failed` (the last update to its target failed), or `done`. Every state is listed, 0 when no door is in it. |
| `doorcode_broker_connected` | gauge | none | 1 while the server is connected to the broker, else 0. |
| `doorcode_store_up` | gauge | none | 1 when the store answered the scrape's reads, else 0. |
| `doorcode_http_requests_total` | counter | `method`, `route`, `status` | HTTP requests answered since the server started. `route` is the route pattern that matched, such as `/api/v1/doors/{id}`, or `unmatched`; `method` is the request's method, or `other` for a method HTTP does not define. |
| `doorcode_http_request_duration_seconds` | histogram | `method`, `route` (and `le` on `_bucket`) | How long the server took to answer, in seconds, with Prometheus's default buckets (5 ms to 10 s). |

The values of `doorcode_events_total`'s labels are those of the events
page: `type` is `check`, `command`, `open`, `alarm`, `door`, or `update`;
`outcome` is `allowed`, `denied`, `alert`, or `info`; `reason` is, for
example, `no rule` or `rate limited` on a denied check, `held open`
or `configuration reverted` on an alarm, `started` on a
controller's start, and `update failed` on an update (see
[docs/spec.md](docs/spec.md), section 15, for the full list).

## Environment variables

### Wiring

| Variable | Meaning | Default |
| --- | --- | --- |
| `DOORCODE_DATABASE_URL` | PostgreSQL connection URL, such as `postgres://doorcode:secret@db.example.com:5432/doorcode`. The server keeps a pool of up to the larger of 4 and the host's CPUs connections, which `pool_max_conns` in the URL changes; it handles the doors' messages four at a time, so a smaller pool makes them wait. | none, required |
| `DOORCODE_BROKER_URL` | The broker's address with its dynamic-security administrator's credentials, such as `mqtt://admin:secret@mqtt.example.com:1883`; `mqtts://` for TLS. Characters special in URLs must be percent-encoded. | none, required |
| `DOORCODE_LISTEN_ADDR` | The address the web server listens on, such as `:8080`. | none, required (`:8080` in the image) |
| `DOORCODE_SECRET_KEY` | 32 random bytes in standard base64 (`openssl rand -base64 32`) that encrypt secret settings and the firmware signing key. | none, required |
| `DOORCODE_ADMIN_PASSWORD` | For development: the password the first administrator is created with, in place of a generated one, which the log then does not show. It is used only when the server creates the first administrator, on a start with no users, and never changes an existing user's password. While it is set, it must follow the rules a password an administrator sets follows (`password_min_length`, at most 1024 characters), or the server refuses to start. It is not a setting and is never stored or shown in Settings. A production deployment leaves it unset. | unset (a generated password); `doorcode-admin` in `docker-compose.dev.yml` |

### docker compose only

| Variable | Meaning | Default |
| --- | --- | --- |
| `DOORCODE_POSTGRES_PASSWORD` | `docker-compose.example.yml` only: the database password; takes effect when the database's volume is new. | none, required |
| `DOORCODE_BROKER_ADMIN_USERNAME` | The broker administrator the server connects as; takes effect when the broker's volume is new. | `doorcode` |
| `DOORCODE_BROKER_ADMIN_PASSWORD` | Its password, likewise. | `doorcode` in `docker-compose.dev.yml`; required in `docker-compose.example.yml` |
| `DOORCODE_HTTP_PORT` | `docker-compose.dev.yml` only: host port the server is published on. | `8080` |
| `DOORCODE_POSTGRES_PORT` | `docker-compose.dev.yml` only: host port Postgres is published on. | `5432` |
| `DOORCODE_MQTT_PORT` | `docker-compose.dev.yml` only: host port the broker is published on. | `1883` |
| `DOORCODE_SMTP_PORT` | `docker-compose.dev.yml` only: host port the `mailpit` SMTP catcher, which the server's mail goes to, is published on. | `1025` |
| `DOORCODE_MAILPIT_PORT` | `docker-compose.dev.yml` only: host port `mailpit`'s web page, listing every message it caught, is published on. | `8025` |
| `DOORCODE_SMTP_STARTTLS_PORT`, `DOORCODE_MAILPIT_STARTTLS_PORT` | `docker-compose.dev.yml` only: host ports of `mailpit-starttls`'s SMTP and web page. | `1587`, `8026` |
| `DOORCODE_SMTP_TLS_PORT`, `DOORCODE_MAILPIT_TLS_PORT` | `docker-compose.dev.yml` only: host ports of `mailpit-tls`'s SMTP and web page. | `1465`, `8027` |
| `DOORCODE_MAIL_HOST` | The `mail_host` setting (see Settings). `docker-compose.dev.yml` sets it to its `mailpit`, the demo to `localhost` (`host.docker.internal` in its image); `docker-compose.example.yml` leaves it unset. | `mailpit` in `docker-compose.dev.yml` |
| `DOORCODE_MAIL_PORT` | The `mail_port` setting, likewise. | `1025` in `docker-compose.dev.yml` and the demo |
| `DOORCODE_MAIL_SECURITY` | The `mail_security` setting, likewise. | `none` in `docker-compose.dev.yml` and the demo |
| `DOORCODE_MAIL_FROM` | The `mail_from` setting, likewise. | `doorcode dev <doorcode@example.com>` in `docker-compose.dev.yml`, `doorcode demo <doorcode@example.com>` in the demo |

### Settings

Every setting can also be given as `DOORCODE_` followed by its key in upper
case, such as `DOORCODE_SITE_TIMEZONE=Europe/Berlin`. A variable that is set,
even to the empty value, wins over the value stored in the database and makes
the setting read-only in the UI. Without one, the value saved on the settings
page applies, and without that the default. The settings page splits its
parts into six tabs (Site setup, Sign-in, Look and feel, Mail, Alerts, and
Doors, at `/admin/settings?tab=site`, `sign-in`, `look`, `mail`, `alerts`,
and `doors`) and
asks for each setting with an input of its kind: a checkbox for yes or no, a
number bounded to its range with the range and unit beside it, a select of the
choices, a box per weekday for `update_window_days`, a color picker, a time of
day, a searchable list of zones for the timezone, address and email inputs
with an example as placeholder, a password field for a secret, and a text area
for PEM certificates that lists each certificate stored with its subject, its
validity in UTC, and whether it has expired or is not yet valid. A setting
the environment gives is shown read-only with the variable that sets it. A
refused value is shown, as typed, with what is wrong beside its input. The
page folds away the single sign-on and identity claims parts while
`oidc_enabled` is off.

| Setting | Meaning | Default |
| --- | --- | --- |
| `site_name` | Shown in the header and every page title. | `doorcode` |
| `site_timezone` | A tz database name; schedules, dates typed on pages, and every time shown are read in it. | `UTC` |
| `admin_email` | The email of the administrator created on first start when there are no users. | `admin@example.com` |
| `public_url` | The `https://` address, without a query, browsers reach the server at; `http://` only for development. Single sign-on needs it. | empty |
| `trusted_proxies` | IP addresses, CIDR ranges, and host names, separated by commas, of the reverse proxies whose `X-Forwarded-For` and `X-Forwarded-Proto` headers are believed. A host name is looked up by the periodic work, about once a minute, and trusts nobody until it has been. Empty believes no one. | empty |
| `theme_accent` | The accent color, `#rrggbb`. | `#4f39f6` |
| `theme_accent_dark` | The accent color in the dark theme; empty uses `theme_accent`. | empty |
| `password_hash_memory_kib` | KiB of memory hashing one password takes, 8 to 1048576. | `19456` |
| `password_min_length` | The shortest password chosen in the portal or set by an administrator, 8 to 128 characters. | `12` |
| `new_user_expiry_days` | Days until an account created by signing in at the provider expires. | `1826` |
| `oidc_enabled` | Single sign-on through an OpenID Connect provider, `true` or `false`. | `false` |
| `oidc_auto_sign_in` | `true` sends the sign-in page straight to the provider and hides the password forms (`?auto_sign_in=false` reaches the sign-in form); holders of the `admin` role, and anyone whose session began with a password, keep the portal form for changing their own password. | `false` |
| `oidc_issuer` | The provider's `https://` issuer, exactly as it names itself. | empty |
| `oidc_client_id` | The id the site is registered with at the provider. | empty |
| `oidc_client_secret` | The client secret, if the provider gave one. Secret: stored encrypted. | empty |
| `oidc_scopes` | Scopes asked for, separated by spaces, `openid` among them. | `openid email profile` |
| `oidc_claim_user_id` | The claim holding the user id people are matched by first; empty reads none. | empty |
| `oidc_claim_email` | The claim holding the email. Required. | `email` |
| `oidc_claim_first_name` | The claim holding the first name. | `given_name` |
| `oidc_claim_last_name` | The claim holding the last name. | `family_name` |
| `oidc_claim_display_name` | The claim holding the display name. | `name` |
| `oidc_claim_entitlements` | The claim listing the slugs of a person's groups. A sign-in that carries it makes them a member of exactly the existing groups it names (values naming no group are ignored; groups are never created); one without it leaves their groups unchanged. While set, new users join no default or pre-assigned groups. Empty reads none. | empty |
| `oidc_default_groups` | Group slugs, separated by commas, that users created by signing in join, together with the groups whose pre-assigned list holds their user id; only while `oidc_claim_entitlements` is empty. | empty |
| `portal_door_pictures` | `true` shows each public door's primary picture in the user portal and lets anyone signed in load that door's pictures; `false` shows none there and serves pictures only to holders of manage-doors, door-commander, or manage-firmware. The administration always shows them. | `false` |
| `mail_host` | The host name or IP address of the SMTP server the site sends mail through. Nothing is sent while it or `mail_from` is empty. | empty |
| `mail_port` | The SMTP server's port, 1 to 65535: usually 465 for `tls`, 587 for `starttls`, 25 for `none`. | `465` |
| `mail_security` | `tls` speaks TLS from the start (implicit TLS); `starttls` connects in the clear and refuses to send unless the server takes STARTTLS; `none` stays in the clear, so the password is sent unencrypted. The server's certificate is verified against the system's trusted roots, TLS 1.2 or later. | `tls` |
| `mail_username` | The user to sign in to the SMTP server as (PLAIN authentication); empty sends without signing in. | empty |
| `mail_password` | Its password. Secret: stored encrypted. | empty |
| `mail_from` | The address mail comes from, alone or after a name, such as `Doorcode <doorcode@example.com>`. | empty |
| `mail_reply_to` | The address replies go to; empty for `mail_from`. | empty |
| `alert_recipients` | The addresses every alert set to mail mails, separated by commas or new lines, each alone or after a name. On the Alerts tab. Nothing is mailed while it is empty. | empty |
| `alert_mail_cleared` | `true` mails the recipients once more when a condition an alert mailed about clears. On the Alerts tab. | `true` |
| `alert_hold_down_minutes` | Minutes, 0 to 10080, a cleared condition must stay clear before it mails again when it comes back; one that comes back sooner is followed without mail. On the Alerts tab. | `15` |
| `certificate_check_enabled` | `true` has the server check, in the background, the certificates of the broker and of `device_url` (see [Certificate check](#certificate-check)); Check now works either way. On the Alerts tab. | `false` |
| `certificate_check_interval_hours` | Hours, 1 to 720, between two checks of an endpoint while `certificate_check_enabled` is on. On the Alerts tab. | `24` |
| `certificate_runway_days` | Days, 1 to 3650: the `certificate` alert is raised for an endpoint whose runway, the days until the earliest expiry along its chain after the leaf with the trust anchor included, is fewer. On the Alerts tab. | `548` |
| `certificate_leaf_days` | Days, 1 to 365: the `certificate` alert is raised for an endpoint whose leaf expires in fewer days. On the Alerts tab. | `14` |
| `certificate_unreachable_checks` | Checks, 1 to 100: the `certificate` alert is raised for an endpoint once this many checks in a row reached no server. On the Alerts tab. | `3` |
| `enrollment_username` | The broker username every new controller enrolls with. A change takes effect at once and reaches every door in its configuration (see below). | `doorcode-enroll` |
| `enrollment_password` | Its password. Generated, stored, and logged on first start when empty. Secret: stored encrypted. A change takes effect at once and reaches every door in its configuration (see below). | empty |
| `telemetry_retention_days` | Days each telemetry report is kept. | `7` |
| `telemetry_rollup_retention_days` | Days the hourly summaries of the reports are kept. | `730` |
| `unresponsive_minutes` | Minutes a connected door may stay silent before it counts as unresponsive, and a door may stay disconnected before it counts as offline for the `offline` alert. | `5` |
| `device_url` | The `https://` address, without a query, of the server's device-facing entrypoint, which presents a certificate chain leading to one of the certificates doors trust (`door_broker_ca_pem`); `http://` only for development, since doors verify the server's certificate only over https. Doors download firmware from it: sent in the `update` command and in the provisioning file's `server.device_url`. No update is sent while it is empty. | empty |
| `update_window_days` | `daily`, or weekdays separated by commas such as `sat,sun`. | `daily` |
| `update_window_start` | When the update window opens, `HH:MM` in the site timezone. | `02:00` |
| `update_window_minutes` | How long the update window lasts, in minutes. | `120` |
| `update_concurrency` | How many doors may be updating at once, 1 to 1000. | `5` |
| `update_timeout_minutes` | Minutes a door may take to come back on its new version before its update fails. | `30` |

Door configuration defaults are settings named `door_` followed by the field
name (so `DOORCODE_DOOR_STRIKE_PULSE_MS` sets the strike pulse of every door
that does not override it). A door's page overrides any of them for that
door, and doors receive a change at once; it marks each setting the door
overrides, counts them, and offers Use site defaults to remove them all.
Both pages say under each field
what it does and what values it takes, the fields in six groups under a
heading each, in the order of the table below: Keypad and strike, Server,
Broker, Wi-Fi, Mesh, and Firmware. `docs/controller.md` section 9 has the
full table.

| Setting | Meaning | Default |
| --- | --- | --- |
| `door_strike_pulse_ms` | How long the strike opens, 100 to 60000 ms. | `3000` |
| `door_held_open_s` | How long a door may stay open, when it should be locked, before it is logged as held open, 5 to 86400 s. | `60` |
| `door_keypad_idle_clear_ms` | Idle time after which the keypad clears what was typed, 1000 to 600000 ms. | `10000` |
| `door_min_code_length` | The shortest code the keypad sends, 1 to 16 digits. | `4` |
| `door_max_code_length` | The longest code the keypad takes, 16 to 64 digits. | `32` |
| `door_reply_timeout_ms` | How long the door waits for the server's answer, 100 to 60000 ms. | `3000` |
| `door_telemetry_interval_s` | How often the door reports telemetry, 5 to 86400 s. | `60` |
| `door_timeline_refresh_hours` | How often the server sends a door its timeline when nothing changed it, 1 to 24 hours. | `6` |
| `door_credential_rotation_days` | How old a door's credentials get before the server rotates them, 1 to 3650 days. | `30` |
| `door_auto_credential_rotation` | Whether the server rotates a door's credentials on its own once they are that old; `false` leaves rotating them to an administrator. | `true` |
| `door_rate_limit_per_minute` | Code checks a minute the door's token bucket refills, 1 to 6000. | `30` |
| `door_rate_limit_burst` | Code checks the bucket holds, 1 to 1000. | `10` |
| `door_log_level` | `error`, `warn`, `info`, or `debug`. | `info` |
| `door_ntp_server` | The time server the door uses. | `pool.ntp.org` |
| `door_broker_host` | The host name or IP address of the broker the door connects to, pushed with the rest of its configuration so doors can be moved to another broker; empty keeps the broker each door already uses. | empty |
| `door_broker_port` | The broker port doors connect to, 1 to 65535. | `8883` |
| `door_broker_tls` | `true` connects doors to the broker over TLS; `false` is for development brokers only. | `true` |
| `door_broker_ca_pem` | One or more PEM certificates doors trust over TLS, for the broker and for `device_url`: the root CA certificate of each, or a host's own certificate (see [Choosing the certificates doors trust](#choosing-the-certificates-doors-trust)); empty keeps the ones each door has. | empty |
| `door_keepalive_s` | The doors' MQTT keep-alive, 10 to 300 s; a door shows offline after one and a half of these without a packet. | `30` |
| `door_reconnect_max_s` | The longest pause between a door's attempts to connect again, 5 to 600 s. | `60` |
| `door_wifi_ssid` | The Wi-Fi network doors join, up to 32 bytes; empty keeps the network each door uses. | empty |
| `door_wifi_password` | Its password, up to 63 bytes. Secret: stored encrypted and shown masked. | empty |
| `door_wifi_roam_rssi` | Below this signal, -100 to 0 dBm, a door moves to an access point of its network at least 8 dB stronger. | `-70` |
| `door_wifi_11b_disabled` | `true` keeps doors off the slow 802.11b rates, 1 to 11 Mbps. It helps a door with a strong signal that still loses many packets on a crowded 2.4 GHz network and hurts a door with a weak signal at the edge of coverage, which needs those rates to stay connected; leave it off unless a door's telemetry shows a good signal with many TCP retransmits. A change makes the door restart its Wi-Fi, and a door in the mesh restart. | `false` |
| `door_mesh_enabled` | `true` lets doors join or form a Wi-Fi mesh when their own signal is poor. Refused, as a default and as a door's override, while the mesh would have no network name or password, which would leave its soft-AP open; a door whose configuration still has that (its override on, the site's password cleared later) is sent the mesh off, and its page and the settings page warn of it. | `false` |
| `door_mesh_ssid` | The mesh network's name, up to 32 bytes; needed while the mesh is on. | empty |
| `door_mesh_password` | The mesh network's password, empty or 8 to 63 bytes; needed while the mesh is on. Secret: stored encrypted and shown masked. | empty |
| `door_mesh_min_router_rssi` | Below this signal, -100 to 0 dBm, a door with the mesh enabled joins it through a neighbor. | `-75` |
| `door_mesh_max_layer` | The most hops a door may be from its access point in the mesh, 1 to 10. | `4` |
| `door_update_connect_timeout_s` | How long a door on new firmware tries to connect before it goes back to its old image, 60 to 1800 s. The settings page and a door's page warn when it is longer than `update_timeout_minutes`. | `300` |
| `door_accept_unsigned_firmware` | `true` lets doors install firmware without a valid signature; for bench and development boards only. | `false` |

## Mail

The site sends mail through one SMTP server, set on the Mail tab of the
settings (`mail_host`, `mail_port`, `mail_security`, `mail_username`,
`mail_password`, `mail_from`, `mail_reply_to`). Nothing is sent while
`mail_host` or `mail_from` is empty. Once the settings are saved, the tab's
Send test email button (or `POST /api/v1/settings/mail/test`) mails your own
address through them while you wait and shows what the server answered: that
it took the message, or the error it gave, such as a refused password or a
certificate that does not verify. It is refused while mail is not set up or
your account has no email address mail can be sent to.

## Alerts

The Alerts page (`/admin/alerts`, administrators, under Site in the
navigation) shows what is happening and changes no setting: every
condition raised now with what became of its mail (Raised now), the
certificate checks with Check now, and the newest alert mail with its
status, attempts, and last error. Its Alert settings link opens the Alerts
tab of the settings page (`/admin/settings?tab=alerts`), which sets each
kind of alert to `off`, `on`, or `mail` and sets whom alerts mail (`alert_recipients`),
when (`alert_mail_cleared`, `alert_hold_down_minutes`), and how the
certificates are checked, all saved with the settings form.
`GET /api/v1/alerts` reads the kinds, the recipients, and the outbox, and
`PUT /api/v1/alerts/kinds/{kind}` with `{"state": "off"}`, `{"state": "on"}`,
or `{"state": "mail"}` sets a kind (400 for any other body); the settings
API changes the settings.

| Kind | Raised | Cleared |
| --- | --- | --- |
| `held_open` | A door stays open past its held-open time while it should be locked, when the server logs a `held open` alarm. | The door closes, or an open span of its timeline begins. |
| `offline` | A door's controller has not been connected to the broker for `unresponsive_minutes` (a door whose credentials were revoked included). | The controller connects again. |
| `unresponsive` | A door is marked unresponsive: connected, but not heard from for `unresponsive_minutes`. | The server hears from it, or it is shown offline. |
| `config_reverted` | A door shows the Reverted configuration badge: it gave up the configuration it was last sent and runs the one before. | The badge goes: it reports the configuration it was last sent, or is sent another. |
| `config_restarting` | A door shows the Restarting with configuration badge: it started at least twice since its configuration was sent, with no report naming it. | The badge goes. |
| `config_mismatch` | A door shows the Mismatched configuration badge: two reports in a row over one connection named another configuration. | The badge goes. |
| `update_failed` | A door's firmware update fails, when the server logs an `update failed` event: the door said it could not install it, or it did not connect again with it before `update_timeout_minutes`. | The door's firmware no longer shows Update failed: an update of it is under way again, it runs its target, or its target changes. |
| `certificate` | A [certificate check](#certificate-check) of an endpoint finds, each its own condition: what it served does not verify; its runway is fewer days than `certificate_runway_days`; its leaf expires in fewer days than `certificate_leaf_days`; or `certificate_unreachable_checks` checks in a row reached no server. Once, and never cleared: a verified chain whose shape differs from the endpoint's chain shape. | A later check passes that reason: it verifies, the runway or the leaf has enough days, or it reaches the server. A check that reaches no server leaves the other reasons as they were. Without a cleared mail, the next check once the settings no longer give the endpoint. |

The periodic work, at most once a minute, raises and clears `held_open`,
`offline`, `unresponsive`, the three configuration kinds, and
`update_failed` from each door's state and firmware status as they stand
then, so they may be raised or cleared up to a minute after they changed;
`held_open` also clears as soon as the door closes or an open span
begins. A door already in a condition when its kind is set on or to mail
is raised at the next run. A door that is deleted or loses its controller
keeps its conditions as they were.

Each kind is in one of three states, `on` until it is set otherwise:

| State | Raised and listed under Raised now | Mailed |
| --- | --- | --- |
| `off` | No. Setting a kind off clears every condition of it, without mail. | No. |
| `on` | Yes. | No. |
| `mail` | Yes. | A condition raised while its kind is `mail`. |

The state decides only alerts. A door's badges, its alarm and other
events, its page, and the Prometheus metrics show its state whatever its
kinds are set to, `off` included.

A condition's mail is decided when it is raised: one raised while its
kind is `on` is never mailed, neither its raise nor its clearing, even
once the kind is set to `mail`, so setting a kind to `mail` never sends a
backlog; only what is raised from then on is mailed. A condition raised
while its kind is `mail` mails the recipients once and, while
`alert_mail_cleared` is on, once more when it clears, if its raise was
mailed and its kind is still set to `mail`. A condition that is raised
again less than `alert_hold_down_minutes` after a mailed raise cleared is
followed without mail, and so is its clearing; a raise that was never to
be mailed holds nothing down.

Nothing is mailed while `alert_recipients` is empty or mail is not set up
(`mail_host` or `mail_from` empty). A raise of a kind set to `mail` that
cannot be mailed then is listed under Raised now as waiting, and once
both allow it (looked at by the mail delivery loop the next time it runs,
and at once after a setting is saved) its raise mail is sent, once, if the
condition is still raised and its kind still set to `mail`, giving the
time it was raised. Setting the kind `on` or `off` meanwhile drops that
mail. A condition that clears before its mail could be sent sends
nothing, neither its raise nor its clearing, and holds nothing down: it
mails at once if it is raised again. A chain change of the `certificate`
kind that happens while the kind is `mail` but no recipient is given or
mail is not set up is kept the same way and mailed once, saying when it
happened; one that happens while the kind is `on` is never mailed, and
one that happens while it is `off` is not kept.

Alert mail goes through the Mail settings as they are when it is sent. It is
put in an outbox in the database and sent by a background loop beside the
door handling, so a slow, failing, or missing mail server never delays a
door. While mail is not set up, nothing in the outbox is tried: a message
keeps its attempts, shows as waiting for mail to be set up, and goes out
once it is. A message a configured mail server does not take is tried
again 1, 2, 4, 8, and 16 minutes after its first, second, third, fourth,
and fifth failed attempts, and is marked failed, with the last error, when
its sixth attempt fails.

### Certificate check

The Alerts page's Certificates section checks the certificates doors are
served the way a door checks them, so a chain that would stop doors
connecting shows before it does. Its endpoints come from the general
settings only: the broker (`door_broker_host`, `door_broker_port`) while
`door_broker_tls` is on, and the host of `device_url` (its port, else 443)
while it is an `https://` address; a door's own overrides are not checked.
Each endpoint is reached over TLS 1.2 or 1.3 naming its host as the server
(SNI), and what it serves is verified against `door_broker_ca_pem` alone,
never the system's roots: starting from the leaf, through the certificates
served in any order, to the first one a certificate of `door_broker_ca_pem`
issued, the path with the fewest certificates winning. The leaf must be
valid for the host, and every certificate of the path and the trust anchor
valid at the time. With the chain leaf, YE1, Root YE cross-signed by X2,
and X2 cross-signed by X1 served, and X1 and X2 trusted, the path is leaf,
YE1, Root YE (cross-signed by X2), with X2 as the anchor.

The page lists, for each endpoint, when it was last checked, whether it
verified (and up to which anchor), what the connection reported when it
could not be reached, or why it did not verify, the runway, the whole days
until the earliest expiry along the path after the leaf (the
intermediates, the cross-signs, and the anchor), the days left on the
leaf, and the chain with each certificate's subject, issuer, and expiry.
Check now checks every endpoint at once; with `certificate_check_enabled`
on, the server checks an endpoint once `certificate_check_interval_hours`
have passed since its last check, looking every minute. These checks run
apart from door handling, so an endpoint slow to answer (each has 10
seconds) never delays a door, and one check runs at a time: Check now
waits for a check in progress to end.

Each check also follows the `certificate` alert. The page shows each
endpoint's count of checks in a row that reached no server and its chain
shape: the subject and issuer of each certificate of the path, leaf first,
and of the trust anchor, recorded silently by the first check that
verified. A later verified chain of another shape, such as a path that now
ends at X1 through the X2 cross-sign or one through another intermediate,
mails once, naming the shape before and the chain now, and becomes the
chain shape; that change is not mailed again, even when the check could
not be stored. A chain that does not verify leaves the shape as it was. The
mail names the endpoint, the reason, and the chain: each certificate's
subject, issuer, expiry, and days left, the anchor, the runway, and the
leaf's days.

An endpoint the general settings no longer give, because
`door_broker_tls` was turned off, the broker's host or port changed, or
`device_url` changed or is no longer `https://`, is forgotten at the next
check (Check now, or the server's own while `certificate_check_enabled` is
on): its raised `certificate` conditions resolve without a cleared mail,
since nothing checks it any more, and the page no longer lists it. Should
the settings give it again, it starts as never checked, with no failures
counted and no chain shape, so its first verified chain records a shape
silently.

`GET /api/v1/alerts/certificates` reads the same, and
`POST /api/v1/alerts/certificates/check` checks now. The check only reads:
it never changes `door_broker_ca_pem` or any door's configuration.

## Single sign-on

With `oidc_enabled` true, an issuer, a client id, and `public_url` set,
register these addresses with the provider (shown with
`public_url` = `https://doorcode.example.com`):

- Redirect URI: `public_url` + `/sign-in/oidc/callback`, such as
  `https://doorcode.example.com/sign-in/oidc/callback`.
- Post-logout redirect URI: `public_url` + `/sign-in`, such as
  `https://doorcode.example.com/sign-in`. Signing out of a session that began
  at the provider ends it there too when the provider names an
  `end_session_endpoint`.

The client uses the authorization code flow with PKCE (S256) and, when a
secret is set, client authentication.

## Door enrollment, from a controller's side

Every message is a CBOR map with `v`, the protocol version (1). Topics start
with `doorcode/`. [docs/controller.md](docs/controller.md) is the contract a
firmware author builds against: every topic and message field with its CBOR
type and unit, the configuration, the timeline, what to do while
disconnected, credential rotation, and firmware updates. The demo's
simulated controller, `internal/demo/doors.go`, is a reference
implementation. In short:

0. **Provision.** Flash the controller with a provisioning file: the
   broker, its CA certificate, the server's address for firmware
   (`device_url`), the Wi-Fi network, the enrollment
   credentials, and a starting configuration. A holder of manage-firmware
   downloads it from the firmware page, or a door's page for a controller
   meant for that door, or from `GET /api/v1/firmware/provisioning` and
   `GET /api/v1/doors/<id>/provisioning` with an API token. It carries
   secrets in the clear, and is refused while `door_broker_host` is empty
   or `door_broker_tls` is on with no `door_broker_ca_pem`. A mesh left
   on with no network name or password is not refused: the file turns it
   off, as the configuration a door is sent does.
1. **Enroll.** Connect to the broker with the shared enrollment credentials
   (`enrollment_username`, `enrollment_password`) and the controller's
   hardware id as the MQTT client id, with a last will of `online: false`
   on `doorcode/enroll/<hwid>`. The broker lets this connection publish only
   `doorcode/enroll/<hwid>` and subscribe only to
   `doorcode/enroll/<hwid>/credentials`. Subscribe there, then publish,
   retained, on `doorcode/enroll/<hwid>` an announcement with `online: true`,
   `firmware_version`, and `signal`.
2. **Wait for approval.** The controller shows as pending under Enrollments
   for as long as it stays connected. When an administrator approves it as a
   door, the server publishes on the credentials topic the `door_id`, a
   `username`, and a `token`, and clears the retained announcement. There is
   no rejection: the list is what is connected and waiting right now. Power
   off a controller that should not be there, or, if it is not yours, change
   `enrollment_password` so nothing holding the old one can enroll.

   **Changing the enrollment credentials.** A change of
   `enrollment_username` or `enrollment_password` takes effect at once:
   the broker accepts the new credentials and stops accepting the old
   ones, and every door is sent them in its configuration
   (`enrollment_username`, `enrollment_password`), an offline door when
   it next connects with its own credentials, so a door that is revoked
   later enrolls again with them. A door's own credentials are not
   touched. Anything that is not a door and still holds the old
   credentials cannot enroll until it is provisioned again: a controller
   provisioned earlier and not yet enrolled, a firmware image that has
   them built in (build it again and replace it wherever it is served),
   and a door revoked before it received the new ones. Download a new
   provisioning file after a change; it carries the current ones.
3. **Connect as the door.** Reconnect with that username and token as the
   password. The door may use only `doorcode/doors/<id>/#`. Publish,
   retained, on `doorcode/doors/<id>/online` `online: true` with the
   username, firmware version, and signal, with a last will saying
   `online: false` with the same username.
4. **Follow the server.** Subscribe to the door's `config` and `timeline`
   (both retained, so they arrive at once and again on every change),
   `reply`, and `command` topics. Follow the timeline's open and locked
   spans while disconnected.
5. **Check codes.** Publish each entry on `doorcode/doors/<id>/check` with a
   `request_id` and the `digits` (a code, optionally followed by `*` and a
   keypad command). The answer arrives on `reply` with the same `request_id`,
   `allow`, and, when allowed, `strike_ms`. Without an answer within the
   reply timeout, deny.
6. **Report.** Publish telemetry on `doorcode/doors/<id>/telemetry` at the
   configured interval, and `opened` and `closed` on
   `doorcode/doors/<id>/event` as the door opens and closes, in open mode
   too.
7. **Obey commands.** A command carries an `id` and a `command`: `open` with
   `strike_ms`; `credentials` with a new `username` and `token` (connect
   with them, publish the online status, then close the old connection;
   credentials rotate automatically, unless automatic rotation is off
   for the door, and on demand; the old ones keep working until the
   door connects with the new ones and stop working then, and a door that
   has not yet connected with the newest it was sent is sent no more);
   `update` with `version`, `sha256`, and `url`; `reboot` with nothing
   more (ack it, restart, and connect again). Answer on
   `doorcode/doors/<id>/ack` with the command's `id`, a `result` (`failed`
   when it could not be carried out), and a `detail`.

Deleting a door deletes its broker accounts, which disconnects its
controller, and takes the controller off the door; a code check that still
arrives for it is not answered. The controller's next connection as the
door is refused as not authorized, so it enrolls again from step 1 and
shows as pending, to be approved as another door or, once the door is
restored, as the same one. Restoring a door does not bring its controller
back.

## Firmware rollout

Holders of manage-firmware upload images with a version (letters, digits,
`.`, `-`, `_`, `+`) on the Firmware page or with a multipart
`POST /api/v1/firmware`, as a release pipeline would. One version is the
site default; a door may name another. A door whose reported version
differs from its target is pending.

While the update window is open (`update_window_days`, `update_window_start`,
`update_window_minutes`, in the site timezone), the server sends pending
online doors, in id order, an `update` command until `update_concurrency`
doors are updating; a door's page can also update it at once. The command's
`url` is `device_url` + `/api/v1/doors/<id>/firmware/<version>?token=...`,
valid only for that update until it times out, so nothing is sent while
`device_url` is empty, even with `public_url` set. The door downloads and
checks the image against `sha256`, installs it, and reconnects reporting the new version, which ends
the update as succeeded. A door that cannot install it answers `failed` on
its ack topic; one that does not come back within `update_timeout_minutes`
fails too. A door that connects again on another version before it has
downloaded the image never received the command, which the broker does
not keep for a door that is away, and is sent it again under the same
timeout; the API shows when the image was first downloaded
(`last_update.fetched_at`). A door that failed is sent its target again in
the next window.
A door that cannot reconnect on the new version reverts on its own and
reappears on the old.

## Development and verification

Requirements are Go, Make, and a POSIX shell; every tool is pinned in
`go.mod` and run with `go tool`.

```sh
make verify-fast        # build, vet, lint, entrypoint and fast connector tests with coverage, dead code, OpenAPI drift
make verify-mutation    # mutation testing of the application code
make verify-mutation-diff  # the same, only on the lines changed since main
```

The slow connector tests need a local Postgres, Mosquitto, and three
mailpit SMTP catchers (in the clear, STARTTLS, and TLS from the start),
which `docker-compose.dev.yml` provides; the mailpits deliver nothing. It
also runs the server built from the checkout (`docker compose -f
docker-compose.dev.yml up -d --wait`, with `DOORCODE_SECRET_KEY` set), with the passwords `doorcode` and the broker
without TLS on 1883, for development only. Its first administrator signs
in as `admin@example.com` with `doorcode-admin`
(`DOORCODE_ADMIN_PASSWORD`, which the caller's environment overrides). That server's mail goes to the
plain mailpit, whose web page <http://localhost:8025> lists every message,
so Send test email works with nothing typed in; the slow tests send to
recipients of their own and delete nothing there.

```sh
docker compose -f docker-compose.dev.yml up -d --wait postgres mosquitto mailpit mailpit-starttls mailpit-tls
export DOORCODE_TEST_DATABASE_URL='postgres://doorcode:doorcode@localhost:5432/doorcode?sslmode=disable'
export DOORCODE_TEST_BROKER_URL='mqtt://doorcode:doorcode@localhost:1883'
export DOORCODE_TEST_MAILPIT_URL='smtp://localhost:1025?web=http://localhost:8025'
export DOORCODE_TEST_MAILPIT_STARTTLS_URL='smtp://localhost:1587?web=http://localhost:8026'
export DOORCODE_TEST_MAILPIT_TLS_URL='smtp://localhost:1465?web=http://localhost:8027'
make verify-integration
docker compose -f docker-compose.dev.yml down -v
```

Set `DOORCODE_POSTGRES_PORT`, `DOORCODE_MQTT_PORT`, and the mailpits'
`DOORCODE_SMTP_PORT`, `DOORCODE_SMTP_STARTTLS_PORT`, `DOORCODE_SMTP_TLS_PORT`,
`DOORCODE_MAILPIT_PORT`, `DOORCODE_MAILPIT_STARTTLS_PORT`, and
`DOORCODE_MAILPIT_TLS_PORT` before `docker compose up` when the default ports
are taken, and use those ports in the URLs.
`make verify-all` runs all three. AGENTS.md describes what each gate
enforces and how the code is laid out.

## License

MIT; see [LICENSE](LICENSE).
