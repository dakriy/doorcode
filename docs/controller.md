# doorcode: the door controller contract

This document is for whoever writes the firmware of a door controller: the
device at a door with a keypad, a strike, and a door sensor. It says
everything such a controller has to do to work with a doorcode server: how
it connects, how it enrolls, every topic and every message field, how it
checks codes, what it does while it cannot reach the server, and how it is
updated. Nothing here needs knowledge of how the server is built.

The product contract is [spec.md](spec.md); section 9 there summarizes the
protocol this document spells out. The server's protocol package,
`internal/protocol`, is the single definition of the topics and messages,
and every name it exports is indexed at the end of this document. The demo's
simulated controller, `internal/demo/doors.go`, is a small reference
implementation of the controller side in Go: it enrolls, connects as its
door, follows its configuration and timeline, checks codes, reports
telemetry and door events, and obeys commands.

The words *must* and *must not* are requirements the server relies on.
*Should* is advice the server does not check but that the rest of the
system assumes.

## 1. The life of a controller

A controller is always in one of three conditions:

1. **Enrolling.** It has no door credentials. It connects with the shared
   enrollment credentials, announces itself, and waits until an
   administrator approves it as a door, which sends it its credentials.
2. **Serving as its door.** It connects with its door's own credentials,
   receives its configuration and timeline, asks the server about every
   code entered, reports telemetry and door events, and obeys commands.
3. **Serving offline.** It holds door credentials but is not connected. It
   follows its cached timeline, denies every code, and keeps trying to
   connect.

A controller whose door credentials the broker refuses has been revoked:
it forgets them and goes back to enrolling.

## 2. Connecting to the broker

| Item | Requirement |
| --- | --- |
| Broker | Mosquitto 2 with its dynamic-security plugin. Every account and its topic permissions are managed by the server. |
| Transport | TLS. The controller must verify the broker's certificate against a CA or certificate it ships with. (A development broker may listen without TLS, such as the compose file's port 1883.) |
| MQTT version | 3.1.1, which the server itself speaks. |
| QoS | 1 for every publish and subscription. The server publishes at QoS 1. |
| Client id | The controller's **hardware id**, on every connection, enrolling or as its door. The enrollment account's permissions depend on it (section 6). |
| Clean session | Should be true. Configuration and timeline arrive again as retained messages on every subscribe; replies and commands are meant for now, and a queued old one must not act later. |
| Keep alive | `keepalive_s` from the configuration (section 9), 30 seconds until the first one. The broker publishes the last will, and the server shows the door offline, only once one and a half keep-alive intervals pass without a packet, so a shorter interval shows a lost door sooner. |
| Last will | Retained, QoS 1, on the topic and with the payload given in sections 6 and 7. |
| Reconnecting | After a lost connection, connect again with growing pauses: 1, 2, 4 seconds and so on up to `reconnect_max_s` (section 9, a minute until the first configuration). A refusal of door credentials as not authorized means they are revoked. |

The **hardware id** is fixed for the life of the device, such as its MAC
address in hex. It is a topic level, a client id, and part of a URL, so it
must be non-empty and should use only letters, digits, `-`, and `_`; it must
not contain `/`, `+`, or `#`.

The broker address, the CA certificate, the Wi-Fi network, and the
enrollment credentials are the controller's provisioning (section 3); a door's
configuration can later move it to another broker host, port, or CA, or
another Wi-Fi network (`broker_host`, `broker_port`, `broker_tls`,
`broker_ca_pem`, `wifi_ssid`, `wifi_password`, section 9). The enrollment username is the server's
`enrollment_username` setting (default `doorcode-enroll`); its password is
`enrollment_password`, which the server generates and logs on first start
when it is empty.

The controller verifies the broker, and the server when it downloads
firmware (section 15), against the same certificate: `broker_ca_pem`, or
the one it was provisioned with. That is usually the certificate of an
internal CA the site runs, while people reach the server's web pages with a
certificate from a public CA. So the server needs a second entrypoint
wherever TLS is terminated, serving a certificate chain from the internal
CA the controllers trust, such as a second port or host name on the reverse
proxy forwarding to the same server; the main entrypoint may use any public
CA. The server's `device_url` setting is that second entrypoint's
address, which controllers download firmware from; while it is empty no
update is sent. The server's `public_url` is for browsers and single
sign-on and is never given to controllers.

## 3. Provisioning

Before it first enrolls, a controller is given its provisioning: the
broker, the certificate to verify it against, the Wi-Fi network, the
enrollment credentials, and a configuration to start with. The server makes
it as a JSON file, for a flashing tool or the firmware's setup mode to write
into the controller:

| Request | File |
| --- | --- |
| `GET /api/v1/firmware/provisioning` | `provisioning.json`: the site's defaults. |
| `GET /api/v1/doors/<id>/provisioning` | `provisioning-<id>.json`: the configuration of door `<id>`, its overrides applied, for a controller meant for that door. |

Both need the `manage-firmware` role, with a personal API token as a bearer
token or as the HTTP Basic password, or a signed-in browser: the firmware
page and each door's page offer a "Download provisioning file" button. The
answer is `application/json` with `Content-Disposition: attachment`.

```
{
  "format": 1,
  "generated_at": 1767603600,
  "broker": {"host": "mqtt.example.com", "port": 8883, "tls": true,
             "ca_pem": "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----\n"},
  "server": {"device_url": "https://devices.example.com:8443"},
  "enrollment": {"username": "doorcode-enroll", "password": "<enrollment_password>"},
  "wifi": {"ssid": "<wifi_ssid>", "password": "<wifi_password>"},
  "door_config": {"strike_pulse_ms": 3000, "keypad_idle_clear_ms": 10000, "...": "...",
                  "firmware_signing_key": "<128 lowercase hex characters>"}
}
```

| Field | JSON type | Meaning |
| --- | --- | --- |
| `format` | number | The version of the file's layout, 1. It changes only when a field changes meaning or goes away; new fields, in `door_config` too, come without a change. |
| `generated_at` | number | When the server made the file, in Unix seconds. |
| `broker.host`, `broker.port`, `broker.tls` | string, number, boolean | The broker to connect to: `broker_host`, `broker_port`, and `broker_tls` of the configuration. |
| `broker.ca_pem` | string | The certificate to verify the broker against (`broker_ca_pem`), PEM with LF line ends; empty when `tls` is false. |
| `server.device_url` | string | The address the server is reached at for firmware downloads: its `device_url` setting; empty while that is not set. Its certificate chains to the same CA as the broker's (section 2). The `update` command carries the full download address (section 15), so the controller need not keep this; it is there for setup tools and for checking the address. |
| `enrollment.username`, `enrollment.password` | string, string | The enrollment credentials (section 6): the server's `enrollment_username` and `enrollment_password` settings. |
| `wifi.ssid`, `wifi.password` | string, string | The Wi-Fi network to join: `wifi_ssid` and `wifi_password` of the configuration. |
| `door_config` | object | Every field of the configuration (section 9), with the same names and types as the configuration message: integers are JSON numbers, text JSON strings, booleans JSON booleans. |
| `door_config.firmware_signing_key` | string | The site's firmware signing key, as the configuration message carries it (section 9), so a controller checks the signature of the first update it is sent. |

Secrets are in the clear: the enrollment password and the Wi-Fi and mesh
passwords. Besides the configuration message, this file is the only place
they leave the server, so keep it as safe as they are.

The server refuses to make a file whose broker has no host, since the
controller would have no address to reach it, or has `tls` true and no
certificate, since the controller could not verify the broker: it answers
409 with an error naming `door_broker_host` or `door_broker_ca_pem`, or
both. The settings page, the firmware page, and a door's page warn of each
state. A mesh left open (section 9, `mesh_enabled`) is no reason to
refuse: the file's `door_config` carries `mesh_enabled` false then, as the
configuration message does, and the settings page and a door's page warn
of it.

The controller stores the broker, its certificate, the Wi-Fi network, and
the enrollment credentials as its provisioning, and runs with `door_config`
until its first configuration message. It must ignore keys it does not
know, and use the defaults of section 9 for configuration fields the file
lacks, so files and firmware can change independently.

## 4. Message encoding

Every payload is a CBOR (RFC 8949) map with text-string keys.

- Every message carries `v`, an unsigned integer, the protocol version,
  which is **1** (`protocol.Version`). The server refuses a message without
  it or with another value (`protocol.ErrVersion`), and the controller
  should ignore a message from the server whose `v` it does not speak.
- The server encodes with CBOR core deterministic encoding: definite
  lengths, the shortest form of every integer and length, and map keys
  sorted (shorter keys first, then bytewise). It decodes any well-formed
  CBOR, so the controller need not sort its keys.
- Types: *uint* and *int* are CBOR integers (major types 0 and 1), never
  floats; *text* is a text string (major type 3), never a byte string;
  *bool* is `true` or `false`; *array* is a definite or indefinite array.
- A field marked *omitted when zero* is left out when it is zero, `false`,
  or empty. A missing field reads as zero, `false`, or empty. Both sides
  ignore keys they do not know, so a later version can add fields.
- Times are Unix seconds (int). Durations are whole numbers of the unit the
  field's name ends in: `_ms` milliseconds, `_s` seconds, `_hours`, `_days`.

A message that is not a CBOR map of version 1, or that comes on a topic of
a door the server does not know, is logged by the server and otherwise
ignored: in particular, such a check gets no reply.

Examples, in CBOR diagnostic notation, with the bytes the server produces
for them:

| Message | Hex |
| --- | --- |
| Check `{"v": 1, "digits": "4211234", "request_id": "9f86d081884c7d65"}` | `a36176016664696769747367343231313233346a726571756573745f69647039663836643038313838346337643635` |
| Reply allowed `{"v": 1, "allow": true, "strike_ms": 3000, "request_id": "9f86d081884c7d65"}` | `a461760165616c6c6f77f569737472696b655f6d73190bb86a726571756573745f69647039663836643038313838346337643635` |
| Reply denied `{"v": 1, "allow": false, "request_id": "9f86d081884c7d65"}` | `a361760165616c6c6f77f46a726571756573745f69647039663836643038313838346337643635` |
| Door event `{"v": 1, "event": "opened"}` | `a2617601656576656e74666f70656e6564` |
| Announcement `{"v": 1, "online": true, "signal": -61, "firmware_version": "1.4.2"}` | `a4617601666f6e6c696e65f5667369676e616c383c706669726d776172655f76657273696f6e65312e342e32` |
| Timeline `{"v": 1, "to": 1793090000, "from": 1793000000, "spans": [{"end": 1793020000, "start": 1793010000, "state": "open"}]}` | `a461760162746f1a6ae061d06466726f6d1a6adf0240657370616e7381a363656e641a6adf50606573746172741a6adf2950657374617465646f70656e` |

## 5. Topics

Every topic starts with `doorcode/`. `<hwid>` is the hardware id; `<id>` is
the door's id, a whole number from 1, which the controller learns from its
credentials. A door account may publish and subscribe only below
`doorcode/doors/<id>/` (`protocol.DoorFilter`); the enrollment account only
on its own two enroll topics.

| Topic | Direction | Retained | Message | Section |
| --- | --- | --- | --- | --- |
| `doorcode/enroll/<hwid>` | controller → server | yes | `Enrollment` (announcement and last will) | 6 |
| `doorcode/enroll/<hwid>/credentials` | server → controller | no | `Credentials` | 6 |
| `doorcode/doors/<id>/online` | controller → server | yes | `Online` (status and last will) | 7 |
| `doorcode/doors/<id>/config` | server → controller | yes | `Config` | 9 |
| `doorcode/doors/<id>/timeline` | server → controller | yes | `Timeline` | 10 |
| `doorcode/doors/<id>/check` | controller → server | no | `Check` | 11 |
| `doorcode/doors/<id>/reply` | server → controller | no | `Reply` | 11 |
| `doorcode/doors/<id>/telemetry` | controller → server | no | `Telemetry` | 12 |
| `doorcode/doors/<id>/event` | controller → server | no | `DoorEvent` | 12 |
| `doorcode/doors/<id>/command` | server → controller | no | `Command` | 13 |
| `doorcode/doors/<id>/ack` | controller → server | no | `Ack` | 13 |

The server subscribes to `doorcode/enroll/+` (`EnrollFilter`) and to
`doorcode/doors/+/online`, `/check`, `/telemetry`, `/event`, and `/ack`
(`OnlineFilter`, `CheckFilter`, `TelemetryFilter`, `EventFilter`,
`AckFilter`). The last level of a door topic is its leaf (`LeafCheck`,
`LeafReply`, `LeafCommand`, `LeafConfig`, `LeafTimeline`, `LeafTelemetry`,
`LeafEvent`, `LeafAck`; the online leaf is `online`).

## 6. Enrollment

```
controller                          broker                         server / admin
    | CONNECT client id <hwid>, user enrollment_username,              |
    |   password enrollment_password, will: retained                   |
    |   doorcode/enroll/<hwid> {v:1, online:false}                     |
    |-------------------------------->|                                |
    | SUBSCRIBE doorcode/enroll/<hwid>/credentials (QoS 1)             |
    | PUBLISH retained doorcode/enroll/<hwid>                          |
    |   {v:1, online:true, firmware_version, signal}                   |
    |-------------------------------->|------------------------------->| shown as pending
    |                                 |                                | admin approves as door <id>
    |                                 |<-------------------------------| credentials (not retained)
    |<--------------------------------|  {v:1, door_id, username, token}
    |                                 |<-------------------------------| empty retained payload on
    |                                 |                                | doorcode/enroll/<hwid>
    | store the credentials, DISCONNECT, connect as the door (section 7)
```

1. Connect as section 2 says with the enrollment credentials and the
   hardware id as client id, with a retained last will of
   `{v: 1, online: false}` on `doorcode/enroll/<hwid>`
   (`protocol.EnrollTopic`). The broker substitutes the client id into the
   account's permissions (`EnrollOwnFilter`, `CredentialsOwnFilter`), so this
   connection may publish only `doorcode/enroll/<hwid>` and subscribe only
   to `doorcode/enroll/<hwid>/credentials` (`protocol.CredentialsTopic`).
2. Subscribe to the credentials topic **before** announcing; the
   credentials are not retained, so a controller not subscribed when they
   are sent never gets them.
3. Publish the announcement, retained.
4. Wait. The controller is listed as pending for as long as its
   announcement says `online: true`; its last will, or a cleared (empty)
   announcement, takes it off the list. An administrator approves it by
   choosing a door.
5. On credentials: store `door_id`, `username`, and `token` in non-volatile
   memory, disconnect cleanly (so the last will is not sent), and connect as
   the door. The server has cleared the retained announcement.

While waiting the controller should show it on the door (such as a slow
blink) and keep the door locked: it has no timeline and checks no codes.

If the connection drops while waiting, connect again and announce again;
the controller is pending again from then on. Approval takes it off the
pending list even if the credentials never reach it, so a controller still
waiting after a while (such as ten minutes) should publish its announcement
again; it is then pending again and can be approved again.

There is no way to reject a waiting controller, and no message that would
tell it so: the pending list is simply every controller connected with the
enrollment credentials and waiting right now. A controller that should not
be there is powered off, or, if it is not the site's own, the enrollment
password is changed (`enrollment_password`, section 2), which closes the
enrollment account to everything still holding the old one.

Approval also revokes any credentials the door had before, and, if this
hardware id was enrolled as another door, takes it off that door and
revokes that door's credentials.

### Enrollment (announcement and last will)

On `doorcode/enroll/<hwid>`, retained.

| Field | CBOR type | Unit | Meaning | When sent |
| --- | --- | --- | --- | --- |
| `v` | uint | | Protocol version, 1. | Always. |
| `online` | bool | | `true` in the announcement, `false` in the last will. | Always. |
| `firmware_version` | text | | The version the controller runs, exactly as its image was uploaded (letters, digits, `.`, `-`, `_`, `+`, up to 64). | Announcement; omitted when empty. |
| `signal` | int | dBm | Signal strength, such as -61. | Announcement; omitted when 0. |

An empty payload on this topic is a cleared announcement. The server sends
one on approval; the controller never needs to.

### Credentials

On `doorcode/enroll/<hwid>/credentials`, not retained, from the server.

| Field | CBOR type | Meaning |
| --- | --- | --- |
| `v` | uint | Protocol version, 1. |
| `door_id` | uint | The door the controller now is, `<id>` in its topics. |
| `username` | text | The broker username to connect as, `door-<id>-<12 hex digits>`. |
| `token` | text | The broker password, 32 characters of base64url (24 random bytes). |

The server keeps no copy of the token. Losing it means enrolling again.

## 7. Connecting as the door

```
controller                          broker                         server
    | CONNECT client id <hwid>, user <username>, password <token>,     |
    |   will: retained doorcode/doors/<id>/online                      |
    |   {v:1, online:false, username}                                  |
    | SUBSCRIBE doorcode/doors/<id>/config, /timeline, /reply, /command|
    |<--------------------------------| retained config and timeline   |
    | PUBLISH retained doorcode/doors/<id>/online                      |
    |   {v:1, online:true, username, firmware_version, signal}         |
    |-------------------------------->|------------------------------->| marks online
    |<--------------------------------|<-------------------------------| config and timeline again
```

1. Connect with the door's `username` and its `token` as the password, the
   hardware id as client id, and a retained last will of
   `{v: 1, online: false, username: <username>}` on
   `doorcode/doors/<id>/online`.
2. Subscribe to the door's `config`, `timeline`, `reply`, and `command`
   topics (`protocol.DoorTopic`). The retained configuration and timeline
   arrive at once, so the controller is current before the server even
   notices it; the server publishes both to every enrolled door that is not
   deleted, connected or not.
3. Publish the online status, retained, with the same username.
4. The first time it comes online as the door since it started, and only
   then (never on a plain reconnect), publish a `started` door event
   (section 12) saying why it started.

When the server reads `online: true` it marks the door online, records that
this credential is in use, revokes the door's older credentials, ends a
firmware update that this version completes (section 14), and publishes the
door's configuration and timeline again. An `online: false` from a revoked
credential, such as the last will of the connection a rotation replaced,
changes nothing.

The broker refusing the connection as not authorized means the credentials
are revoked (an administrator revoked them or deleted the door, or the
hardware was approved as another door): delete them and enroll again.

### Online

On `doorcode/doors/<id>/online`, retained.

| Field | CBOR type | Unit | Meaning | When sent |
| --- | --- | --- | --- | --- |
| `v` | uint | | Protocol version, 1. | Always. |
| `online` | bool | | `true` in the status, `false` in the last will. | Always. |
| `username` | text | | The username this connection uses. Must be set in both, so the server can tell a stale last will apart. | Always. |
| `firmware_version` | text | | The version running now. | Status; omitted when empty. |
| `signal` | int | dBm | Signal strength. | Status; omitted when 0. |

## 8. What the server sends, at a glance

| When | What arrives |
| --- | --- |
| On every subscribe | The retained `config` and `timeline`. |
| After the online status | `config` and `timeline` again. |
| A default or the door's override changes | `config`. |
| The door's zones or their open-mode schedules, an override of the door, or the site timezone changes; a keypad command changes the door's overrides | `timeline`. |
| At least every `timeline_refresh_hours` (default 6) | `timeline`, rebuilt from now. |
| After each `check` | `reply`. |
| An administrator acts, or the server rotates credentials or rolls out firmware | `command`. |

## 9. Configuration

On `doorcode/doors/<id>/config`, retained (`Config`). The message is one
flat map: `v`, every field below (`DoorConfig`), and
`firmware_signing_key`, never omitted. Each field below is the site's
default (setting `door_` + the field's name) unless the door overrides it. Replace the whole configuration on every message, store it in
non-volatile memory, and use it until the next one. Before the first one,
use the `door_config` of the provisioning file (section 3), or the defaults
below for a field it lacks.

| Field | CBOR type | Unit | Range | Default | Who uses it, for what |
| --- | --- | --- | --- | --- | --- |
| `strike_pulse_ms` | uint | ms | 100 to 60000 | 3000 | Controller: how long the strike stays released each time the door lets someone in, and for an open span's keypress. The server sends the same value as a reply's or open command's `strike_ms`, and lets the door open within this, plus `reply_timeout_ms`, of letting someone in without calling it forced open. |
| `keypad_idle_clear_ms` | uint | ms | 1000 to 600000 | 10000 | Controller: discard a partly typed entry after this long without a key. |
| `min_code_length` | uint | digits | 1 to 16 | 4 | Controller: the fewest digits a code may have; discard an entry whose code (the digits before any `*`) is shorter without sending it. |
| `max_code_length` | uint | digits | 16 to 64 | 32 | Controller: the longest entry the keypad takes, a keypad command included; take no more keys once an entry is this long, and do not send a longer one. |
| `reply_timeout_ms` | uint | ms | 100 to 60000 | 3000 | Controller: how long to wait for the server's reply to a check before denying the entry. The server allows a door to open this long, plus the strike pulse, after it let someone in before calling it forced. |
| `telemetry_interval_s` | uint | s | 5 to 86400 | 60 | Controller: how often to publish telemetry, its health readings such as its signal and uptime. |
| `held_open_s` | uint | s | 5 to 86400 | 60 | Server: how long a door may stand open, while it should be locked, before it counts as held open and an event is logged. The controller may use it for a local warning. |
| `timeline_refresh_hours` | uint | hours | 1 to 24 | 6 | Server: how often it sends the door its timeline of when to stay open and locked even when nothing changed it. The controller can expect a new timeline at least this often while connected. |
| `credential_rotation_days` | uint | days | 1 to 3650 | 30 | Server: how many days the door's broker credentials work before it issues new ones. |
| `rate_limit_per_minute` | uint | checks | 1 to 6000 | 30 | Server: the door's token bucket refills at this many checks a minute once its burst is spent; an entry over the limit is denied without its code being looked at and logged as rate limited. |
| `rate_limit_burst` | uint | checks | 1 to 1000 | 10 | Server: the bucket holds this many checks, the codes the door may send in quick succession before the limit a minute applies. |
| `ntp_server` | text | | a host name, up to 253 letters, digits, dots, and hyphens | `pool.ntp.org` | Controller: the time server to set the clock from. The timeline is in Unix seconds, so the door keeps to it only while the clock is right. |
| `broker_host` | text | | empty, a host name of up to 253 letters, digits, dots, and hyphens, or an IP address, IPv6 included | empty | Controller: the host name or IP address of the broker to connect to. Empty keeps the broker the controller uses now. When it names another broker, store it in place of the provisioned host and connect there from the next connection on, with the same credentials and the port, TLS, and CA below. |
| `broker_port` | uint | | 1 to 65535 | 8883 | Controller: the port of the broker to connect to, from the next connection on. |
| `broker_tls` | bool | | | `true` | Controller: whether to connect to the broker over TLS, checking the broker against `broker_ca_pem`. `false` is for development brokers only. |
| `broker_ca_pem` | text | | empty, or one or more PEM certificates (LF line ends) | empty | Controller: the certificate of the CA, or the broker's own certificate, to verify the broker against while `broker_tls` is true. Empty keeps the certificate the controller has. |
| `wifi_ssid` | text | bytes | up to 32 | empty | Controller: the Wi-Fi network to join. Empty keeps the network the controller uses now. Keep the last credentials that worked and fall back to them when the new ones fail. |
| `wifi_password` | text | bytes | up to 63 | empty | Controller: the Wi-Fi network's password. The server stores it encrypted and shows it to nobody; this message is the one place it travels in the clear, to the door alone. |
| `wifi_roam_rssi` | int | dBm | -100 to 0 | -70 | Controller: when the signal to the current access point is below this, scan and move to an access point of the same network that is at least 8 dB stronger, if there is one. |
| `wifi_11b_disabled` | bool | | | `false` | Controller: when true, the Wi-Fi station never uses the 802.11b rates, 1 to 11 Mbps, and keeps to the faster OFDM rates instead of falling back to the slow ones when frames are lost. It helps a door with a strong signal that still loses many frames on a crowded 2.4 GHz network and hurts a door with a weak signal at the edge of coverage, which needs those rates to stay connected. The station takes it only while Wi-Fi is stopped: a change makes the door leave its access point, restart Wi-Fi, and join again, or restart the door when it is in the mesh. |
| `keepalive_s` | uint | s | 10 to 300 | 30 | Controller: the MQTT keep-alive interval, how often the door lets the broker know it is still there, from the next connection on. The server shows the door offline once one and a half of these pass without a packet. |
| `reconnect_max_s` | uint | s | 5 to 600 | 60 | Controller: the longest pause between attempts to connect again; back off 1, 2, 4 seconds and so on up to this. |
| `mesh_enabled` | bool | | | `false` | Controller: whether it may join or form a Wi-Fi mesh with other controllers when its own access point's signal is poor. Never true with an empty `mesh_ssid` or a `mesh_password` shorter than 8 bytes, which would leave the mesh soft-AP open: the server refuses to store such a default or override, and sends `false` while a door's configuration still has it (say a door's override turns the mesh on and the site's password is cleared later). |
| `mesh_ssid` | text | bytes | up to 32 | empty | Controller: the mesh network's name, the same at every controller of the site. Not empty while `mesh_enabled` is true. |
| `mesh_password` | text | bytes | empty, or 8 to 63 | empty | Controller: the mesh network's password, the same at every controller of the site. Not empty while `mesh_enabled` is true. Stored encrypted and sent only to the doors, like `wifi_password`. |
| `mesh_min_router_rssi` | int | dBm | -100 to 0 | -75 | Controller: when the strongest access point it can see is below this and `mesh_enabled` is true, join the mesh through a neighboring controller instead. |
| `mesh_max_layer` | uint | hops | 1 to 10 | 4 | Controller: how many hops from the access point it may sit in the mesh. |
| `update_connect_timeout_s` | uint | s | 60 to 1800 | 300 | Controller: after installing firmware, go back to the previous image if it has not connected as its door within this long (section 15). Keep it within the server's `update_timeout_minutes`, after which the server counts the update failed; the server's pages warn when it is longer. |
| `accept_unsigned_firmware` | bool | | | `false` | Controller: when true, install firmware images without a valid signature. Leave it false except on bench and development boards. |
| `log_level` | text | | one of `error`, `warn`, `info`, `debug` | `info` | Controller: how much it writes to its own log, from errors only (`error`) to every detail (`debug`). |

A controller must ignore keys it does not know and use the defaults above
for keys a message lacks, so server and firmware can ship new fields
independently.

### Firmware signing key

| Field | CBOR type | Meaning |
| --- | --- | --- |
| `firmware_signing_key` | text | The public key every firmware image must be signed with (section 15): the ECDSA P-256 point `x \|\| y`, each coordinate 32 bytes big-endian, with no leading `04`, as 128 lowercase hex characters. |

It is not a setting: no door overrides it, no page edits it, and it is
the same in every door's message. The server makes the key pair the first
time it starts, keeps the private key encrypted with the other secret
settings, and never sends it; the firmware page names the key by its
fingerprint, the first 16 hex characters of the SHA-256 of the 64-byte
point. Store the key with the configuration and check every update with
the one in the configuration in use. Firmware has no key of its own, so a
controller that has none (it has not received a configuration or
provisioning file that carries one) cannot check an image (section 15).

Pin mappings and other hardware matters are the firmware's own and are not
here.

### Configuration digest and trying a configuration

A configuration is named by its digest: the first 8 bytes of the SHA-256
of the message's payload exactly as published on
`doorcode/doors/<id>/config` (the whole CBOR map with `v`), written as 16
lowercase hex digits (`protocol.Digest`). The server encodes the message
deterministically (section 4), so the same configuration always has the
same digest. Store the payload as received and report the digest of the
one in use as telemetry's `config_digest` (section 12), empty while
running the defaults.

A configuration that changes how the door reaches the server (the broker,
Wi-Fi, and mesh fields) may be tried: the controller keeps the one it ran,
restarts with the new one, and keeps it once it is online as its door
again. When it cannot come online with it within the
`update_connect_timeout_s` of the one it ran, it goes back to that one,
restarts, and once online says `config_reverted` (section 12) with the
digest of the configuration given up. It then ignores that configuration
while it stays retained; any other configuration is taken as usual and
makes it forget the one given up, so changing the door's configuration,
any change, makes it try again.

The server keeps, for each door, the digest of the configuration it last
sent and when it first sent it (sending the same configuration again, as
on every reconnect, keeps that time). It flags a door whose controller
does not run it:

- **mismatched configuration:** two telemetry reports in a row, both received
  after the configuration was sent, with the controller neither
  restarting nor connecting again between them (the second's `uptime_s`
  no lower and its `reconnects` the same), name another digest. One is
  not enough: a report made just as the configuration went out, or the
  first after the door came back online, may come before the controller
  took it, and a controller trying a configuration restarts and reports
  nothing until it is online with it. The flag dates from the first of
  the reports, and goes as soon as a report names the configuration last
  sent, names none, or a new configuration is sent.
- **reverted configuration:** the controller said `config_reverted`
  naming the configuration last sent, after it was sent. The flag holds,
  whatever the controller reports meanwhile, until a report names the
  configuration last sent or a new configuration is sent; sending the
  same one again, which the controller ignores, keeps it.

There is no flag for a door without a controller, a door sent no
configuration since the server began keeping the digest (it is sent one
on every reconnect), a controller that has not reported since the
configuration was sent, nor while its reports name no configuration
(empty `config_digest`: its defaults, or firmware that predates the
field), which the server shows as not reported. The door list shows the
flag as a badge, the door page as its Configuration and a note saying
what happened (for a configuration given up, also that changing the
configuration makes the door try again), and `GET /api/v1/doors/{id}/state` as `config_mismatch`,
`config_reverted`, and `config_mismatch_since`, beside
`config_sent_digest` and `config_sent_at`.

## 10. The timeline and the door's state

On `doorcode/doors/<id>/timeline`, retained (`Timeline`). It covers the next
25 hours from when it was built.

| Field | CBOR type | Unit | Meaning |
| --- | --- | --- | --- |
| `v` | uint | | Protocol version, 1. |
| `from` | int | Unix s | Start of the time the timeline covers: when the server built it. |
| `to` | int | Unix s | End of the covered time, 25 hours after `from`. |
| `spans` | array of `Span` | | The spans in which the door is not in its normal state, in order. Always present, possibly empty. |

Each `Span`:

| Field | CBOR type | Unit | Meaning |
| --- | --- | --- | --- |
| `start` | int | Unix s | First second of the span. |
| `end` | int | Unix s | End of the span, not included. |
| `state` | text | | `open` or `locked`. |

Spans never overlap, and two touching spans always differ in state; each
lies within `from` and `to`. Replace the whole timeline on every message and
store it in non-volatile memory.

At any instant the door is in one of three states:

| State | When | Online | Offline |
| --- | --- | --- | --- |
| **open** (open mode) | Inside an `open` span. | Any keypress releases the strike for `strike_pulse_ms`. No check is sent to the server for it; count the opening in telemetry's `opens`, and send door events as the sensor changes (section 12). | Any keypress releases the strike for `strike_pulse_ms`. |
| **locked** | Inside a `locked` span. | Send entries to the server as usual: a door commander can still enter, and keypad commands still work; the server denies everyone else. | Deny every entry. |
| **normal** | Anywhere else within `from` to `to`. | Send each entry and follow the reply (section 11). | Deny every entry. |

After `to`, without a newer timeline, the door is in its normal state. A
controller that does not know the time (before its first NTP answer, say)
should treat every instant as normal, which denies everything offline.

Because an open span asks the server nothing, a keypad command typed during
one does not reach the server.

## 11. Code entry and the check exchange

### The keypad entry

The controller collects the keys pressed: digits and `*`. The controller
decides which other key ends the entry, `#` by convention; that key is not
part of it. The entry is discarded when no key comes for
`keypad_idle_clear_ms`. An entry whose code part is
shorter than `min_code_length` is discarded without asking; the controller
takes no more keys once an entry reaches `max_code_length`.

The entry is `<code>` or `<code>*<command>`:

- **`<code>`** is the person's door code, digits only. Codes are never
  stored on the door; only the server can say whether one works.
- **`*<command>`** asks the server to carry out a keypad command. Anything
  after a `*` makes the entry a command: a valid command from a door
  commander is carried out and the door opens; anything else, an unknown
  command or one from someone who is not a door commander, is denied.

Keypad commands all start with the opcode `00`:

| Entry | Effect |
| --- | --- |
| `<code>*001<minutes>` | An open override from now for `<minutes>`, 1 to 1440. |
| `<code>*002<minutes>` | A locked override from now for `<minutes>`, 1 to 1440. |
| `<code>*000` | Ends every override of the door in force now. |

For example, `4211234*00130` keeps the door open for 30 minutes. A command
that changes the door's overrides is followed by a new timeline.

### Check

On `doorcode/doors/<id>/check`, published once per entry.

| Field | CBOR type | Meaning |
| --- | --- | --- |
| `v` | uint | Protocol version, 1. |
| `request_id` | text | An id the controller makes up for this check, unique among its recent checks, such as 16 random hex digits. The server echoes it in the reply and records it on the entry's event. |
| `digits` | text | The entry: the code, optionally followed by `*` and a command, without the key that ended it. |

### Reply

On `doorcode/doors/<id>/reply`, from the server.

| Field | CBOR type | Unit | Meaning | When sent |
| --- | --- | --- | --- | --- |
| `v` | uint | | Protocol version, 1. | Always. |
| `request_id` | text | | The `request_id` of the check it answers. | Always. |
| `allow` | bool | | Whether to let the person in. | Always. |
| `strike_ms` | uint | ms | How long to release the strike. | Only when `allow` is true. |

The controller waits for the reply with the check's `request_id` for
`reply_timeout_ms`. With `allow: true` it releases the strike for
`strike_ms`; with `allow: false`, or no reply in time, it denies (such as a
red light and a tone). A reply to a check it no longer waits for, or with
an unknown `request_id`, is ignored. Only one entry need be outstanding at a
time.

The reply says nothing of why. The server decides, in order: the door's
rate limit, an expired or deleted account, a keypad command, a door
commander, a locked then an open override, the open-mode schedules of the
door's zones, an access rule, and otherwise no; spec section 2 has the details. Every
decision is logged on the server with its reason. A code nobody has gets
the same reply as any other denied entry.

### Rate limiting

The server keeps a token bucket per door, refilled at
`rate_limit_per_minute` and holding `rate_limit_burst` checks. A check over
the limit is denied without its code being looked at, and logged as rate
limited; the controller just sees `allow: false`. The controller need not
limit itself, though it may hold back entries faster than the bucket allows.

## 12. Telemetry and door events

### Telemetry

On `doorcode/doors/<id>/telemetry`, every `telemetry_interval_s` while
connected, first soon after connecting. Every field is always sent.

Every count (`reconnects`, `opens`, `checks_sent`, `checks_lost`,
`tcp_sent`, `tcp_retransmits`, `tcp_drops`, `wifi_drops`, and `roams`) is
a total since the controller started: never reset after a report, only
when the controller starts, so a report lost on the way loses no counts.
A count kept in a 32-bit counter may wrap.

| Field | CBOR type | Unit | Meaning |
| --- | --- | --- | --- |
| `v` | uint | | Protocol version, 1. |
| `signal` | int | dBm | Signal strength now. |
| `latency_ms` | uint | ms | The controller's latest measure of the round trip to the server, such as the last MQTT ping or check and reply. |
| `uptime_s` | uint | s | Time since the controller started. |
| `free_bytes` | uint | bytes | Free memory. |
| `firmware_version` | text | | The version running now. |
| `reconnects` | uint | | How often it connected to the broker again since it started (0 for the first connection). |
| `door_open` | bool | | Whether the door sensor says the door is open now. |
| `opens` | uint | | How often the door opened, open-mode opens included. |
| `mesh_layer` | uint | | Where the controller sits in the site's Wi-Fi mesh: 0 when it is connected straight to the building's access point, 1 when it hops through one other controller, and so on. |
| `mesh_parent` | text | | The **hardware id** (the MQTT client id it enrolled with, not a door id) of the controller it hops through; empty at layer 0. |
| `mesh_children` | uint | | How many controllers hop through this one (its direct children in the mesh). |
| `checks_sent` | uint | | Code checks sent to the server. |
| `checks_lost` | uint | | Checks whose reply did not come in time. A check replaced by a newer one before then is not lost. |
| `tcp_sent` | uint | segments | TCP segments sent for the first time: those carrying data or opening or closing a connection, not bare acknowledgements, resets, or segments sent again. 0 on a controller whose network stack keeps no TCP counters. |
| `tcp_retransmits` | uint | segments | TCP segments sent again; 0 without TCP counters. |
| `tcp_drops` | uint | segments | TCP segments dropped; 0 without TCP counters. |
| `wifi_drops` | uint | | Times the Wi-Fi station lost its access point. |
| `wifi_drop_reason` | uint | | The system's reason code for the latest loss of the access point since the controller started (ESP-IDF `wifi_err_reason_t`); 0 when there was none. |
| `roams` | uint | | Times the Wi-Fi station moved to another access point by its own choice. |
| `bssid` | text | | The access point joined now, 12 lowercase hex digits; empty with no link. |
| `channel` | uint | | The Wi-Fi channel of the access point joined now; 0 with no link. |
| `config_digest` | text | | The digest of the configuration in use (section 9), 16 lowercase hex digits; empty while running the defaults. |

Firmware that predates the mesh fields leaves them out; the server reads a
missing `mesh_layer` and `mesh_children` as 0 and a missing `mesh_parent`
as empty, that is, as a controller reaching the access point itself.
Firmware that predates the link fields (`checks_sent` to `channel`) leaves
them out, which the server reads as 0 and an empty `bssid`. Firmware that
predates `config_digest` leaves it out, which the server reads as empty,
reporting no configuration (section 9).

The server works out what each report counted since the one before it:
each total less the same total in the report before. When there is no
report before, when `uptime_s` is lower than in the report before (the
controller restarted), or when a total is lower than before (its counter
wrapped or started again), all of that total counts as new. The server
keeps the reports, with both what they counted and the totals as sent, as
history and hourly summaries (each hour sums what its reports counted and
keeps the `mesh_layer`, `mesh_children`, `wifi_drop_reason`, `bssid`, and
`channel` of its last report), and shows the latest; a door whose
`mesh_layer` is above 0 is shown meshing, naming the door whose controller
has the `mesh_parent` hardware id. A connected door the server has heard
nothing from (online status, telemetry, or event) for
`unresponsive_minutes` (default 5) is shown unresponsive, so
`telemetry_interval_s` should stay below that.

### Door events

On `doorcode/doors/<id>/event`, when the door sensor changes, in open mode
too: `opened` and `closed`, so the server shows the door's state at once.
Inside an `open` span the strike is released, so `opened` says `unlocked`
true, and `closed` leaves out `open_ms` for an opening that began there,
as for any other. Firmware that predates this says nothing inside an
`open` span; the server learns the state from its next report. And once
after the controller starts, in open mode too: `started`, the first time
it comes online as the door since it started (section 7), never on a
plain reconnect, with neither `unlocked` nor `open_ms`. And once it is
online again after giving up a configuration it tried (section 9), right
after `started`: `config_reverted`, carrying the digest of the
configuration given up.

| Field | CBOR type | Meaning |
| --- | --- | --- |
| `v` | uint | Protocol version, 1. |
| `event` | text | `opened` (`protocol.EventOpened`), `closed` (`protocol.EventClosed`), `started` (`protocol.EventStarted`), or `config_reverted` (`protocol.EventConfigReverted`). |
| `unlocked` | bool | With `opened` only, optional: whether the door was unlocked when the sensor saw it open, that is, the strike was released, or had been locked again for no longer than a short grace period the controller chooses. |
| `open_ms` | uint | With `closed` only, optional: how many milliseconds the door was open, by the controller's clock. Sent only when the controller saw the opening from its start and it began outside an open span; otherwise left out. |
| `reason` | text | With `started` only, optional: why the controller started, one of the values below; left out when it cannot tell. |
| `config_digest` | text | With `config_reverted` only: the digest of the configuration given up, 16 lowercase hex digits. |

The `reason` values of `started`, and what the server's event says of each:

| `reason` | Constant | Meaning | Event detail |
| --- | --- | --- | --- |
| `power_on` | `StartPowerOn` | Powered on. | Power on. |
| `software` | `StartSoftware` | A restart the firmware asked for: a reboot command, an update, a revert. | A restart its firmware asked for. |
| `panic` | `StartPanic` | The firmware panicked. | Its firmware panicked. |
| `watchdog` | `StartWatchdog` | A watchdog reset it. | Its watchdog reset it. |
| `brownout` | `StartBrownout` | The supply voltage dropped too low. | A brownout: its supply voltage dropped too low. |
| `external` | `StartExternal` | The reset pin. | Its reset pin. |
| `unknown` | `StartUnknown` | The controller cannot tell. | It does not know why. |

A `started` event is recorded as one event at the door: type `door`,
outcome `info`, reason `started` (so `GET /api/v1/events?reason=started`
lists the starts), its detail the words above, a value the server does
not know as given, and nothing when `reason` is left out. The server
also keeps `reason` as given, as the event's `start_reason`, which
`GET /api/v1/events?start_reason=panic` and the events page's Why it
started filter narrow the starts by, a value the server does not know
included; a start without `reason`, and one recorded before the server
kept it, matches no such filter. The `doorcode_events_total` metric
counts a value the server does not know as `start_reason="other"`. It
leaves the
door's open or closed state as it was and, like any message from the
door, counts as contact. Firmware that predates it never sends it, and
the server derives nothing in its place.

A `config_reverted` event is recorded as an alarm at the door: type
`alarm`, outcome `alert`, reason `configuration reverted`, detail "The
door could not come online with configuration <digest> and went back to
the one it ran before.", and flags the door as section 9 says when the
digest is that of the configuration last sent.

From these the server derives:

- **forced open:** `opened` when the door was not open already and
  - with `unlocked`, it is false; the controller's word decides, whatever
    the server last let in;
  - without `unlocked` (firmware that predates it), nothing let anyone in
    within `strike_pulse_ms` + `reply_timeout_ms` before it (no allowed
    check or keypad command, no `open` command from the administration,
    no open span), so send `opened` as soon as the sensor sees it;
- **held open:** open for `held_open_s` or longer while it should be
  locked. Time in an open span of the door's timeline (open mode or an open
  override) does not count: the count starts when the door opened or when
  the latest open span ended, whichever is later, so a door propped open
  through open mode is not held open there, and one still open when the
  span ends is held open `held_open_s` after its end. A locked override is
  not an open span. One event per stretch.
- **opened:** whenever the door goes from not open to open in the
  server's state, by an `opened` event or by a report with `door_open`
  true, in an open span or outside one, one event of type `door`,
  outcome `info`, reason `opened`, with no length. None is recorded when
  the server had the door open already. When the opening is also forced
  open, the alarm is a separate event beside it. Its outcome is `info`,
  so it never counts as an unlock for the forced open check.
- **closed:** whenever the door goes from open to closed in the server's
  state, by a `closed` event or by a report with `door_open` false, in an
  open span or outside one, one event of type `door`, outcome `info`,
  reason `closed`, saying how long it was open: `open_ms` when the
  message carries it, else the time since the server saw it open. None
  is recorded when the server did not have the door open.

`door_open` in each telemetry report also sets the state, so a missed event
is corrected by the next report. Events and telemetry made while offline are
not sent later; the server learns the state from the first report after
reconnecting.

## 13. Commands and acks

### Command

On `doorcode/doors/<id>/command`, from the server, only while the door is
connected. Commands are not retained and are never queued for an offline
door.

| Field | CBOR type | Unit | Meaning | When sent |
| --- | --- | --- | --- | --- |
| `v` | uint | | Protocol version, 1. | Always. |
| `id` | text | | The command's id, 16 hex digits, to quote in the ack. | Always. |
| `command` | text | | `open`, `credentials`, `update`, or `reboot`. | Always. |
| `strike_ms` | uint | ms | How long to release the strike. | `open`. |
| `username` | text | | The new broker username. | `credentials`. |
| `token` | text | | The new broker password. | `credentials`. |
| `version` | text | | The firmware version to install. | `update`. |
| `sha256` | text | | The SHA-256 of the signed image the server serves, signature block included, 64 lower-case hex digits. | `update`. |
| `url` | text | | The address to download the image from (section 15), `https://` except on development servers. | `update`. |

| `command` | What the controller does |
| --- | --- |
| `open` (`CommandOpen`) | Release the strike for `strike_ms`, as a reply allowing someone in would. An administrator sent it from the door page; the server has logged it. |
| `credentials` (`CommandCredentials`) | Rotate to the new credentials (section 14). |
| `update` (`CommandUpdate`) | Install the firmware (section 15). |
| `reboot` (`CommandReboot`) | Restart, as below. An administrator sent it from the door page or the API; the server has logged it. |

An unknown command should be answered with a `failed` ack.

**Reboot.** A `reboot` command carries only `v`, `id`, and `command`; a
controller must not expect any other field. The controller:

1. Publishes an ack quoting the command's `id`, with `result` `done`, and
   should wait for the broker's PUBACK before going on, so the ack is not
   lost in the restart.
2. Restarts, as a power cycle would: the connection ends without a clean
   disconnect, so the broker publishes the last will and the server shows
   the door offline.
3. Connects again with the credentials it holds and goes on as in section
   6: last will, subscribe, online status. Its uptime and reconnect count
   start again from zero, and it keeps its stored timeline and
   configuration.

The server does not wait for the ack or the reconnection; a door that does
not come back is shown offline, and unresponsive in time, like any other.

### Ack

On `doorcode/doors/<id>/ack`.

| Field | CBOR type | Meaning | When sent |
| --- | --- | --- | --- |
| `v` | uint | Protocol version, 1. | Always. |
| `id` | text | The `id` of the command answered. | Always. |
| `result` | text | `failed` (`protocol.AckFailed`) when the command could not be carried out; otherwise `done`, as the reference implementation sends. | Always. |
| `detail` | text | Why, in a few words, such as `checksum mismatch`. | Omitted when empty. |

The server reads one ack today: `failed` for the update under way, which
ends the update as failed with the detail. Acks of `open` are optional and
recorded nowhere, as are those of `reboot`; an update is acknowledged as a success by reconnecting on
the new version, not by an ack.

## 14. Credentials: rotation and revocation

A door normally has one live credential, and never more than two: the old
and the new during a rotation. The server rotates automatically once the
only live one is `credential_rotation_days` old and the door is connected;
an administrator can rotate from the door page or the API while it is
connected. Until the controller connects with the new credentials, the
door is handing over and no further rotation is issued: the automatic one
waits, and an administrator's is refused.

1. The server creates the new account and sends a `credentials` command over
   the current connection.
2. The controller stores the new `username` and `token` in non-volatile
   memory, keeping the old pair until the new one has worked.
3. It connects with the new credentials (and the same client id), sets the
   last will with the new username, subscribes, and publishes its online
   status with the new username, as in section 7.
4. It closes the old connection. With the same client id on both, the
   broker ends the old one as the new one connects, which also serves; a
   controller whose MQTT stack holds one connection may instead close the
   old connection first and then connect, as the reference implementation
   does.
5. The server, reading the online status with the new username, revokes
   the old credentials. Nothing else ends them: the old credentials stay
   valid until the door connects with the new ones.

If the new credentials are refused, the controller should connect with the
old ones, which still work, and try the new ones again later. Once the new
ones have worked, forget the old.

**Revocation.** An administrator revoking the credentials, deleting the
door, or approving this hardware as another door deletes the accounts,
which disconnects the controller. Its next connection is refused as not
authorized; it then forgets its credentials, its stored timeline, and its
stored configuration, and enrolls again.

## 15. Firmware updates

The server sends `update` to doors whose reported version differs from
their target: during the update window, a few doors at a time, or at once
from the door page. The controller:

1. Remembers the command's `id` and `version` in non-volatile memory.
2. Downloads the image with a `GET` of `url`, verifying the server's
   certificate when `url` is `https://`. An `http://` address is for
   development only; the controller downloads over it without any server
   verification. The address is the server's `device_url`, followed by
   `/api/v1/doors/<id>/firmware/<version>?token=<token>`; the token is for
   this one update and must not be logged. Verify the server against the
   same certificate as the broker (`broker_ca_pem`, or the one provisioned):
   `device_url` names the entrypoint whose certificate chain comes from
   that CA (section 2). The server answers:
   - `200` with the signed image as `application/octet-stream`: the image
     as uploaded followed by the server's 68-byte signature block, at most
     0x1F0000 bytes in all, the size of the smaller update slot;
   - `403` when the token is wrong, or the update has ended or timed out;
   - `404` when the version no longer exists.
3. Checks the SHA-256 of the whole download, signature block included,
   against `sha256`.
4. Checks the signature block against `firmware_signing_key` (section 9;
   see below), unless `accept_unsigned_firmware` is true.
5. Installs it into the inactive image slot and restarts into it.
6. Connects as its door (section 7) and publishes its online status with
   the new `firmware_version`. That ends the update as succeeded.

If the download, the checksum, the signature, or the installation fails,
the controller stays on its current firmware and publishes an ack with the command's `id`,
`result: failed`, and a `detail`. The server records the update as failed
and sends it again in the next update window.

**Signature.** The server signs every image as it serves it, with the
site's firmware signing key; images are uploaded unsigned, and an
administrator never signs one. The signature block is the last 68 bytes
of the download:

    +---------------------------+---------------+-------------------------+
    | image (N bytes)           | version (4)   | signature (64)          |
    +---------------------------+---------------+-------------------------+

- **Image**: the image exactly as uploaded.
- **Version**: a 32-bit word, little-endian, that is `0`.
- **Signature**: the ECDSA P-256 signature of the SHA-256 of the image's
  N bytes, as `r` followed by `s`, each 32 bytes big-endian. It is
  deterministic (RFC 6979), so the same image under the same key is served
  as the same bytes, with the same `sha256`, every time.

The controller verifies the signature with `firmware_signing_key` before
it installs the image. A controller without a key refuses the update with the ack detail
`no signing key`, and one whose download is shorter than 68 bytes, has a
version word other than 0, or whose signature does not verify with the
key refuses it with `bad signature`. With `accept_unsigned_firmware` true
it installs the image without checking the signature, key or no key; that
is for bench and development boards only.

**Self-revert.** New firmware that cannot connect to the broker within
`update_connect_timeout_s` (section 9) must restart into the previous image (an A/B
scheme with a boot counter, for instance), which then connects and reports
the old version. It should then publish a `failed` ack with the remembered
command `id` and a detail such as `reverted: could not connect`, so the
server learns it at once. Without the ack, the update fails when the server
has not seen the new version within `update_timeout_minutes` (default 30)
of sending it.

A door that reconnects on the target version during an update succeeds;
reporting the version only in telemetry does not end the update.

## 16. Disconnected

While the controller cannot reach the broker, it:

- follows its stored timeline (section 10): open spans unlock on any
  keypress; everything else denies every entry, logging nothing on the
  server (there is no queue of offline entries);
- runs with its stored configuration;
- keeps its door credentials and connects again with growing pauses;
- sends nothing later: telemetry, door events, and entries made offline are
  not replayed.

On reconnecting it does everything section 7 says; the retained
configuration and timeline bring it up to date at once.

## 17. What the controller stores

| What | Why |
| --- | --- |
| Hardware id, broker address and CA, Wi-Fi network and password, enrollment username and password | Provisioning (section 3), to enroll. |
| `door_id`, `username`, `token` | To connect as the door after a restart. During a rotation, both pairs. |
| The last configuration, its `firmware_signing_key` included | To run offline and after a restart, and to check firmware updates. |
| The last timeline | To follow open and locked spans offline. |
| The `id` and `version` of an update under way | To ack a failure after a revert. |
| The configuration it ran before one it is trying, and the digest of one it gave up | To go back when it cannot come online, and to ignore the one given up (section 9). |
| The previous firmware image | To revert. |

Codes are never stored.

## 18. Index of the protocol package

Every name `internal/protocol` exports, and where this document describes
it.

| Name | Kind | Meaning | Section |
| --- | --- | --- | --- |
| `Version` | constant, 1 | The protocol version in every message's `v`. | 4 |
| `EnrollFilter` | constant | `doorcode/enroll/+`, the announcements the server reads. | 5 |
| `OnlineFilter` | constant | `doorcode/doors/+/online`. | 5 |
| `CheckFilter` | constant | `doorcode/doors/+/check`. | 5 |
| `TelemetryFilter` | constant | `doorcode/doors/+/telemetry`. | 5 |
| `EventFilter` | constant | `doorcode/doors/+/event`. | 5 |
| `AckFilter` | constant | `doorcode/doors/+/ack`. | 5 |
| `EnrollOwnFilter` | constant | `doorcode/enroll/%c`, what the enrollment account may publish; the broker puts the client id for `%c`. | 6 |
| `CredentialsOwnFilter` | constant | `doorcode/enroll/%c/credentials`, what the enrollment account may subscribe to. | 6 |
| `LeafCheck`, `LeafReply`, `LeafCommand`, `LeafConfig`, `LeafTimeline`, `LeafTelemetry`, `LeafEvent`, `LeafAck` | constants | The last levels of a door's topics: `check`, `reply`, `command`, `config`, `timeline`, `telemetry`, `event`, `ack`. | 5 |
| `EnrollTopic(hwid)` | function | `doorcode/enroll/<hwid>`. | 6 |
| `CredentialsTopic(hwid)` | function | `doorcode/enroll/<hwid>/credentials`. | 6 |
| `DoorTopic(id, leaf)` | function | `doorcode/doors/<id>/<leaf>`. | 5 |
| `DoorFilter(id)` | function | `doorcode/doors/<id>/#`, everything a door account may use. | 5 |
| `Topic` (`HardwareID`, `DoorID`, `Leaf`) | type | What a topic names: an enrolling controller's hardware id, or a door's id and the topic's leaf. | 5 |
| `Parse(topic)` | function | Reads a topic into a `Topic`; a hardware id must be non-empty and a door id a whole number from 1. | 5 |
| `Header` (`V`) | type | The `v` field every message has. | 4 |
| `Message` | interface | Any message of the protocol. | 4 |
| `Enrollment` (`Online`, `FirmwareVersion`, `Signal`) | message | `online`, `firmware_version`, `signal`. | 6 |
| `Credentials` (`DoorID`, `Username`, `Token`) | message | `door_id`, `username`, `token`. | 6 |
| `Online` (`Online`, `Username`, `FirmwareVersion`, `Signal`) | message | `online`, `username`, `firmware_version`, `signal`. | 7 |
| `CommandCredentials`, `CommandOpen`, `CommandUpdate`, `CommandReboot` | constants | `credentials`, `open`, `update`, `reboot`. | 13 |
| `Command` (`ID`, `Command`, `Username`, `Token`, `StrikeMS`, `Version`, `SHA256`, `URL`) | message | `id`, `command`, `username`, `token`, `strike_ms`, `version`, `sha256`, `url`. | 13 |
| `AckFailed` | constant | `failed`. | 13 |
| `Ack` (`ID`, `Result`, `Detail`) | message | `id`, `result`, `detail`. | 13 |
| `Check` (`RequestID`, `Digits`) | message | `request_id`, `digits`. | 11 |
| `Reply` (`RequestID`, `Allow`, `StrikeMS`) | message | `request_id`, `allow`, `strike_ms`. | 11 |
| `DoorConfig` (`StrikePulseMS`, `KeypadIdleClearMS`, `MinCodeLength`, `MaxCodeLength`, `ReplyTimeoutMS`, `TelemetryIntervalS`, `HeldOpenS`, `TimelineRefreshHours`, `CredentialRotationDays`, `RateLimitPerMinute`, `RateLimitBurst`, `NTPServer`, `BrokerHost`, `BrokerPort`, `BrokerTLS`, `BrokerCAPEM`, `WifiSSID`, `WifiPassword`, `WifiRoamRSSI`, `Wifi11bDisabled`, `KeepaliveS`, `ReconnectMaxS`, `MeshEnabled`, `MeshSSID`, `MeshPassword`, `MeshMinRouterRSSI`, `MeshMaxLayer`, `UpdateConnectTimeoutS`, `AcceptUnsignedFirmware`, `LogLevel`) | type | The configuration fields, `strike_pulse_ms` through `log_level`. | 9 |
| `Config` (`Header`, `DoorConfig`, `FirmwareSigningKey`) | message | `v`, the configuration fields, and `firmware_signing_key` in one map. | 9 |
| `Span` (`Start`, `End`, `State`) | type | `start`, `end`, `state`. | 10 |
| `Timeline` (`From`, `To`, `Spans`) | message | `from`, `to`, `spans`. | 10 |
| `Telemetry` (`Signal`, `LatencyMS`, `UptimeS`, `FreeBytes`, `FirmwareVersion`, `Reconnects`, `DoorOpen`, `Opens`, `MeshLayer`, `MeshParent`, `MeshChildren`, `ChecksSent`, `ChecksLost`, `TCPSent`, `TCPRetransmits`, `TCPDrops`, `WifiDrops`, `WifiDropReason`, `Roams`, `BSSID`, `Channel`, `ConfigDigest`) | message | `signal`, `latency_ms`, `uptime_s`, `free_bytes`, `firmware_version`, `reconnects`, `door_open`, `opens`, `mesh_layer`, `mesh_parent`, `mesh_children`, `checks_sent`, `checks_lost`, `tcp_sent`, `tcp_retransmits`, `tcp_drops`, `wifi_drops`, `wifi_drop_reason`, `roams`, `bssid`, `channel`, `config_digest`. | 12 |
| `EventOpened`, `EventClosed`, `EventStarted`, `EventConfigReverted` | constants | `opened`, `closed`, `started`, `config_reverted`. | 12 |
| `StartPowerOn`, `StartSoftware`, `StartPanic`, `StartWatchdog`, `StartBrownout`, `StartExternal`, `StartUnknown` | constants | `power_on`, `software`, `panic`, `watchdog`, `brownout`, `external`, `unknown`. | 12 |
| `DoorEvent` (`Event`, `Unlocked`, `OpenMS`, `Reason`, `ConfigDigest`) | message | `event`, `unlocked` (a `*bool`, nil when left out), `open_ms` (a `*uint64`, nil when left out), `reason` (empty when left out), `config_digest` (empty when left out). | 12 |
| `ErrVersion` | error | A message of another protocol version. | 4 |
| `Encode(m)` | function | `m` as CBOR, core deterministic, with `v` set to `Version`. | 4 |
| `Decode(payload, m)` | function | Reads a payload, refusing anything but a CBOR map of this version. | 4 |
| `Digest(payload)` | function | The digest naming a configuration message: the first 8 bytes of the payload's SHA-256 as 16 lowercase hex digits. | 9 |
