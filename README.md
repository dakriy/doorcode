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
- **Codes.** Stored only as salted hashes, never on doors. Administrators set
  codes of 6 to 20 digits; people choose their own as their user id followed
  by 4 to 8 digits. A per-door rate limit protects against guessing.
- **Doors.** Enrollment over MQTT with administrator approval, per-door
  broker credentials that rotate automatically, pictures, zones, per-door
  configuration overrides over site defaults, opening from the UI, and live
  state: online, open or closed, forced open, held open, unresponsive.
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
  alarm, and update is logged with who and why, and searchable by door,
  user, type, outcome, reason, and date. The user is picked by searching
  the server as you type, so the page stays fast with thousands of users.
- **Dashboard.** Entries per day, a weekday by hour heatmap, denial reasons,
  the busiest and quietest doors, door uptime, signal and latency trends,
  firmware versions, and live counts.
- **People.** Local passwords and OpenID Connect single sign-on (matching by
  user id then email, an entitlements claim setting group membership exactly), a user portal
  (door code, password, access calendars, recent entries, access requests,
  API tokens), bulk import of users from CSV with a preview, and access
  requests answered by administrators.
- **API.** Everything the UI does is under `/api/v1`, described by an
  OpenAPI 3.1 document at `/api/v1/openapi.json` and browsable at
  `/api-docs`. Personal API tokens are accepted as a bearer token or as the
  HTTP Basic password.
- **Settings.** Edited in the UI, each overridable by an environment
  variable (and then read-only in the UI). Secret settings are encrypted at
  rest. Light and dark themes follow the browser; the accent color and the
  sign-in page picture are settings.

## Try the demo

The demo runs the real application over seeded in-memory data with simulated
doors. It needs neither Postgres nor a broker, and nothing it does outlives
it.

```sh
docker build --target doorcode-demo -t doorcode-demo .
docker run --rm -p 8080:8080 doorcode-demo
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

## Deploy with docker compose

`docker-compose.yml` runs the server (`app`) with PostgreSQL 18 and
Mosquitto 2 with its dynamic-security plugin. Migrations apply on start.

1. Generate the key that encrypts stored secrets, and keep it: secrets
   stored under one key cannot be read under another.

   ```sh
   export DOORCODE_SECRET_KEY="$(openssl rand -base64 32)"
   ```

   Put it in a `.env` file next to `docker-compose.yml` (as
   `DOORCODE_SECRET_KEY=...`) to have compose read it every time. The server
   refuses to start without a valid key.

2. Start everything and wait for it to be healthy:

   ```sh
   docker compose up -d --wait
   ```

3. Read the first administrator's password, and the generated enrollment
   password controllers enroll with, from the log:

   ```sh
   docker compose logs app
   ```

   Sign in at <http://localhost:8080> as `admin@example.com` (the
   `admin_email` setting) and change the password from the portal.

4. In Settings, set `public_url` to the `https://` address people's
   browsers reach the server at (such as `https://doorcode.example.com`).
   Single sign-on needs it. An `http://` address is accepted for
   development only.

   Doors verify the server, when they download firmware, against the same
   CA certificate as the broker (`door_broker_ca_pem`), usually an internal
   CA, while people's browsers want a certificate from a public CA. Give
   the server a second entrypoint wherever TLS is terminated that serves a
   certificate chain from the internal CA the doors trust, such as a second
   port or host name on the reverse proxy forwarding to the same server,
   and set `device_url` to it (such as
   `https://devices.doorcode.example.com`). The main entrypoint may use any
   public CA. No firmware update is sent while `device_url` is empty;
   `public_url` is never used for doors.

The compose file is a starting point: the broker listens without TLS on
1883, and the database and broker passwords are `doorcode`. For production,
put TLS in front of the web server and the broker, change the passwords,
and keep the volumes backed up. The `app` service answers its healthcheck on
`/healthz`.

Behind a reverse proxy that terminates TLS, list the proxy in the
`trusted_proxies` setting (such as `DOORCODE_TRUSTED_PROXIES=10.0.0.0/8` or
the proxy's host name). The server believes `X-Forwarded-For` (the client
address it logs for sign-ins, refused sign-ins, and failed requests) and
`X-Forwarded-Proto` (whether cookies are marked `Secure`) only from a
connection whose address is on that list, and ignores both from anyone else.

## Environment variables

### Wiring

| Variable | Meaning | Default |
| --- | --- | --- |
| `DOORCODE_DATABASE_URL` | PostgreSQL connection URL, such as `postgres://doorcode:secret@db.example.com:5432/doorcode`. | none, required |
| `DOORCODE_BROKER_URL` | The broker's address with its dynamic-security administrator's credentials, such as `mqtt://admin:secret@mqtt.example.com:1883`; `mqtts://` for TLS. Characters special in URLs must be percent-encoded. | none, required |
| `DOORCODE_LISTEN_ADDR` | The address the web server listens on, such as `:8080`. | none, required (`:8080` in the image) |
| `DOORCODE_SECRET_KEY` | 32 random bytes in standard base64 (`openssl rand -base64 32`) that encrypt secret settings. | none, required |

### docker compose only

| Variable | Meaning | Default |
| --- | --- | --- |
| `DOORCODE_HTTP_PORT` | Host port the server is published on. | `8080` |
| `DOORCODE_POSTGRES_PORT` | Host port Postgres is published on. | `5432` |
| `DOORCODE_MQTT_PORT` | Host port the broker is published on. | `1883` |
| `DOORCODE_BROKER_ADMIN_USERNAME` | The broker administrator the server connects as; takes effect when the broker's volume is new. | `doorcode` |
| `DOORCODE_BROKER_ADMIN_PASSWORD` | Its password, likewise. | `doorcode` |

### Settings

Every setting can also be given as `DOORCODE_` followed by its key in upper
case, such as `DOORCODE_SITE_TIMEZONE=Europe/Berlin`. A variable that is set,
even to the empty value, wins over the value stored in the database and makes
the setting read-only in the UI. Without one, the value saved on the settings
page applies, and without that the default. The settings page splits its
parts into four tabs (Site setup, Sign-in, Look and feel, and Doors, at
`/admin/settings?tab=site`, `sign-in`, `look`, and `doors`) and asks for each
setting with an input of its kind (a color picker, a checkbox, a number, a
select, and a searchable list of zones for the timezone) and folds away the
single sign-on and identity claims parts while `oidc_enabled` is off.

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
| `password_min_length` | The shortest password chosen in the portal, 8 to 128 characters. | `12` |
| `new_user_expiry_days` | Days until an account created by signing in at the provider expires. | `1826` |
| `oidc_enabled` | Single sign-on through an OpenID Connect provider, `true` or `false`. | `false` |
| `oidc_auto_sign_in` | `true` sends the sign-in page straight to the provider and hides the password forms (`?auto_sign_in=false` reaches them). | `false` |
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
| `enrollment_username` | The broker username every new controller enrolls with. Takes effect on the next start. | `doorcode-enroll` |
| `enrollment_password` | Its password. Generated, stored, and logged on first start when empty. Secret: stored encrypted. Takes effect on the next start. | empty |
| `telemetry_retention_days` | Days each telemetry report is kept. | `7` |
| `telemetry_rollup_retention_days` | Days the hourly summaries of the reports are kept. | `730` |
| `unresponsive_minutes` | Minutes a connected door may stay silent before it counts as unresponsive. | `5` |
| `device_url` | The `https://` address, without a query, of the server's device-facing entrypoint, which presents a certificate from the managed CA controllers trust, the same CA as the broker's (`door_broker_ca_pem`); `http://` only for development, since doors verify the server's certificate only over https. Doors download firmware from it: sent in the `update` command and in the provisioning file's `server.device_url`. No update is sent while it is empty. | empty |
| `update_window_days` | `daily`, or weekdays separated by commas such as `sat,sun`. | `daily` |
| `update_window_start` | When the update window opens, `HH:MM` in the site timezone. | `02:00` |
| `update_window_minutes` | How long the update window lasts, in minutes. | `120` |
| `update_concurrency` | How many doors may be updating at once, 1 to 1000. | `5` |
| `update_timeout_minutes` | Minutes a door may take to come back on its new version before its update fails. | `30` |

Door configuration defaults are settings named `door_` followed by the field
name (so `DOORCODE_DOOR_STRIKE_PULSE_MS` sets the strike pulse of every door
that does not override it). A door's page overrides any of them for that
door, and doors receive a change at once.

| Setting | Meaning | Default |
| --- | --- | --- |
| `door_strike_pulse_ms` | How long the strike opens, 100 to 60000 ms. | `3000` |
| `door_keypad_idle_clear_ms` | Idle time after which the keypad clears what was typed, 1000 to 600000 ms. | `10000` |
| `door_min_code_length` | The shortest code the keypad sends, 1 to 16 digits. | `4` |
| `door_max_code_length` | The longest code the keypad takes, 16 to 64 digits. | `32` |
| `door_submit_key` | The key that ends an entry: `#`, `A`, `B`, `C`, or `D`. | `#` |
| `door_reply_timeout_ms` | How long the door waits for the server's answer, 100 to 60000 ms. | `3000` |
| `door_telemetry_interval_s` | How often the door reports telemetry, 5 to 86400 s. | `60` |
| `door_held_open_s` | How long a door may stay open before it is logged as held open, 5 to 86400 s. | `60` |
| `door_timeline_refresh_hours` | How often the server sends a door its timeline when nothing changed it, 1 to 24 hours. | `6` |
| `door_credential_rotation_days` | How old a door's credentials get before the server rotates them, 1 to 3650 days. | `30` |
| `door_rate_limit_per_minute` | Code checks a minute the door's token bucket refills, 1 to 6000. | `30` |
| `door_rate_limit_burst` | Code checks the bucket holds, 1 to 1000. | `10` |
| `door_ntp_server` | The time server the door uses. | `pool.ntp.org` |
| `door_broker_host` | The host name or IP address of the broker the door connects to, pushed with the rest of its configuration so doors can be moved to another broker; empty keeps the broker each door already uses. | empty |
| `door_broker_port` | The broker port doors connect to, 1 to 65535. | `8883` |
| `door_broker_tls` | `true` connects doors to the broker over TLS; `false` is for development brokers only. | `true` |
| `door_broker_ca_pem` | The PEM certificate of the broker's CA, or of the broker itself, doors verify it against; empty keeps the one each door has. | empty |
| `door_wifi_ssid` | The Wi-Fi network doors join, up to 32 bytes; empty keeps the network each door uses. | empty |
| `door_wifi_password` | Its password, up to 63 bytes. Secret: stored encrypted and shown masked. | empty |
| `door_wifi_roam_rssi` | Below this signal, -100 to 0 dBm, a door moves to an access point of its network at least 8 dB stronger. | `-70` |
| `door_keepalive_s` | The doors' MQTT keep-alive, 10 to 300 s; a door shows offline after one and a half of these without a packet. | `30` |
| `door_reconnect_max_s` | The longest pause between a door's attempts to connect again, 5 to 600 s. | `60` |
| `door_mesh_enabled` | `true` lets doors join or form a Wi-Fi mesh when their own signal is poor. | `false` |
| `door_mesh_ssid` | The mesh network's name, up to 32 bytes. | empty |
| `door_mesh_password` | The mesh network's password, empty or 8 to 63 bytes. Secret: stored encrypted and shown masked. | empty |
| `door_mesh_min_router_rssi` | Below this signal, -100 to 0 dBm, a door with the mesh enabled joins it through a neighbor. | `-75` |
| `door_mesh_max_layer` | The most hops a door may be from its access point in the mesh, 1 to 10. | `4` |
| `door_update_connect_timeout_s` | How long a door on new firmware tries to connect before it goes back to its old image, 60 to 1800 s. The settings page and a door's page warn when it is longer than `update_timeout_minutes`. | `300` |
| `door_accept_unsigned_firmware` | `true` lets doors install firmware without a valid signature; for bench and development boards only. | `false` |
| `door_log_level` | `error`, `warn`, `info`, or `debug`. | `info` |

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
   or `door_broker_tls` is on with no `door_broker_ca_pem`.
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
   configured interval, and, outside open mode, `opened` and `closed` on
   `doorcode/doors/<id>/event`.
7. **Obey commands.** A command carries an `id` and a `command`: `open` with
   `strike_ms`; `credentials` with a new `username` and `token` (connect
   with them, publish the online status, then close the old connection;
   credentials rotate automatically, the old ones keep working until the
   door connects with the new ones and stop working then, and a door that
   has not yet connected with the newest it was sent is sent no more);
   `update` with `version`, `sha256`, and `url`; `reboot` with nothing
   more (ack it, restart, and connect again). Answer on
   `doorcode/doors/<id>/ack` with the command's `id`, a `result` (`failed`
   when it could not be carried out), and a `detail`.

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
fails too. A door that failed is sent its target again in the next window.
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

The slow connector tests need a local Postgres and Mosquitto, which compose
provides:

```sh
docker compose up -d --wait postgres mosquitto
export DOORCODE_TEST_DATABASE_URL='postgres://doorcode:doorcode@localhost:5432/doorcode?sslmode=disable'
export DOORCODE_TEST_BROKER_URL='mqtt://doorcode:doorcode@localhost:1883'
make verify-integration
docker compose down -v
```

Set `DOORCODE_POSTGRES_PORT` and `DOORCODE_MQTT_PORT` before `docker compose
up` when the default ports are taken, and use those ports in the URLs.
`make verify-all` runs all three. AGENTS.md describes what each gate
enforces and how the code is laid out.

## License

MIT; see [LICENSE](LICENSE).
