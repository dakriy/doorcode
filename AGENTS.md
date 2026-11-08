# AGENTS.md

This file is the contract for anyone changing this repository. The first half
is the standing methodology and is identical across repositories. The second
half is project-specific. Where a project-specific section records a
deviation, the deviation wins for the scope it names.

> Effects behind connectors. Tests through the real entrypoint. Everything
> else is verified by the build.

## Judgment rules

These are the rules the build cannot check. Hold them in mind while coding.

1. **Effects go behind connectors.** Anything that touches the outside world
   or is nondeterministic is reached only through a connector interface:
   network, storage, processes, environment, clock, randomness, identifier
   generation. A connector interface is narrow and named for what the
   application needs, never a pass-through for the external protocol; a
   method that accepts raw SQL or a raw URL moves the query logic into
   application code where no test can see whether it is right. The
   implementation is as thin as the external API allows, reports what the
   system said in whatever types are natural, and makes no decisions: no
   branching on content, no defaults, no interpretation of codes or absences.
   A generator seeded from an explicit input is pure and is not a connector;
   the seed's source is.
2. **Production wiring is the only place real connectors are built.** A
   connector factory interface has one method per connector and nothing else,
   never a plain value. The production factory lives with `main` and does
   nothing but construct real connectors and call the entrypoint. The stub
   factory lives in test support. The entrypoint takes the factory and never
   constructs a connector itself, so everything from the entrypoint inward is
   tested.
3. **Tests enter at the real entrypoint and assert on outcomes.** Every test
   of application code calls the entrypoint with the stub factory. It asserts
   on results, state, what the stubs recorded, and errors. Never on which
   internal function ran, how often, or in what order. One behavior per test,
   named for the behavior. Stubs are hand-written connector implementations in
   test support; nothing else is ever replaced, and no mocking library exists
   in the repository. There are no unit tests of internals.
4. **No test asserts absence.** Not that a symbol, import, or feature does
   not exist, not that a call was not made, not that output is empty with
   nothing else said. Assert the outcome that does happen. Absence of code is
   the dead-code check's job.
5. **The test DSL grows from repetition.** Extract shared setup on the third
   occurrence, in the language of the product, with no hidden assertions and
   no tests of its own.
6. **Wire it or delete it.** Production code with no caller reachable from an
   entrypoint does not ship. Create abstractions after a pattern exists, not
   before.
7. **Boring, immutable code.** A reader follows any function top to bottom
   without decoding it: plain loops, named intermediates, early returns, the
   obvious language feature. Produce new values rather than mutating, unless
   the reason is visible at the site. Repeat text, not knowledge: extract
   shared code on the third occurrence and only when the copies encode the
   same decision.
8. **Done means verified, and no mutant survives.** `verify-fast` green after
   every change. `verify-mutation` green whenever application code changed,
   and green means an empty survivor list: every survivor the run produced
   was killed by a stronger test, removed with the over-defensive code it
   lived in, or suppressed as equivalent with a written reason.
   `verify-integration` green only when a connector changed. A verification
   not run is reported as not run. No gate is lowered and no allowlist
   widened to make a change pass.

## What the build enforces

Consult this when a gate fails.

Two kinds of production code exist. **Application code** is everything that
is not a connector implementation or production wiring, entrypoints included.
**Connector code** is connector implementations and the production factory,
verified by connector tests rather than by the coverage and mutation gates.

| Gate | Fails when | Configured under |
| --- | --- | --- |
| Application imports | application code imports a connector implementation, the production wiring, or an effect package | Effect packages |
| Effect calls | application code reads the clock, entropy, or the environment directly, where the linter can see it | Stack |
| Entrypoint-suite imports | an entrypoint test imports anything but entrypoint packages, test support, and listed test libraries | Fast-suite dependencies |
| Coverage | any application function is below 100% line coverage from the entrypoint suite; generated code is excluded by path | Generated code |
| Mutation | any mutant in application code survives, is not covered, or times out | Mutation exclusions |
| Complexity caps | an application function exceeds the application cap, or a connector function exceeds the lower cap | Complexity caps |
| Dead code | production code is unreachable from every entrypoint, where the ecosystem has a tool | Known gaps if none |
| Runtime budget | `verify-fast`'s tests run longer than the budget | Commands |
| Slow-test placement | a connector test marked slow is reached by `verify-fast` | Layout |
| Integration compile | the slow connector tests do not compile or type-check | Commands |
| Static analysis, formatting | the stack's own linter or formatter objects | Stack |
| Duplicate-code report | never; it prints repeated blocks in production code for the next audit | Stack |

## The suites

The **entrypoint suite** runs the real entrypoint against connector stubs. It
is the only test suite for application code, the only source of coverage,
and what mutation testing exercises.

The **connector suite** tests connector implementations against real
systems, local wherever possible: a temp directory, an in-process database, a
container. The same tests also run against the stubs. Each connector test is
marked fast or slow. Fast ones
run in `verify-fast` under the runtime budget; slow ones run only in
`verify-integration`. A connector test that cannot be made fast is slow, not
skipped. Nothing in either suite touches a production system. A system that
cannot run locally is either mimicked by a local service or represented only
by stub data, with no integration test.

The runtime budget covers everything `verify-fast` runs.

| Command | Runs | When |
| --- | --- | --- |
| `verify-fast` | every gate above except mutation; the entrypoint suite and the fast connector tests | after every change |
| `verify-mutation` | mutation testing of application code, survivor report | before finishing an application-code change |
| `verify-integration` | the slow connector tests | only when a connector interface or implementation changed, and on a schedule in CI |
| `verify-all` | all three, stopping at the first failure | before release or on request |

---

# Project-specific

## Stack

- Language and version: Go 1.27 (module `github.com/dakriy/doorcode`)
- Build system: the Go toolchain; GNU Make as the task runner. Every tool is pinned as a `tool` directive in `go.mod` and run with `go tool`, so the only host requirements are Go, Make, and a POSIX shell.
- Test framework: the standard `testing` package
- Assertion library with diff output: `github.com/google/go-cmp/cmp` (`cmp.Diff`)
- Coverage tool: `go test -coverpkg=<application packages> -coverprofile`, gated by `scripts/coverage-gate.sh` over `go tool cover -func`
- Mutation tool: gremlins v0.6.0 in `--integration` mode, gated by `scripts/mutation.sh` on the survivor list
- Import-rule tool: depguard via golangci-lint v2.15.0 (`.golangci.yml`)
- Effect-call linter: forbidigo via golangci-lint (`.golangci.yml`)
- Dead-code tool: `golang.org/x/tools/cmd/deadcode`, wrapped by `scripts/deadcode.sh`
- Duplicate-code tool: dupl via golangci-lint (`.golangci.dupl.yml`, threshold 50 tokens, reporting only)
- Database: PostgreSQL 18 through pgx v5 (`github.com/jackc/pgx/v5`); schema migrations are embedded SQL files run by tern v2 (`github.com/jackc/tern/v2/migrate`) from the store's `Migrate`
- Password hashing: argon2id (`golang.org/x/crypto/argon2`) in PHC string format, in `internal/auth`. The salt comes from the Entropy connector, so hashing is pure; the memory cost is the `password_hash_memory_kib` setting.
- Web UI: `html/template` pages and static assets embedded from `internal/app/ui`; htmx v2.0.10, Swagger UI 5.34.0 (`swagger-ui-dist` from npm), FullCalendar 6.1.22 (`fullcalendar` from npm, its standard global bundle, for calendar views), and the Tailwind CSS v4.3.3 standalone CLI, all pinned with SHA-256 checksums in `scripts/vendor-ui.sh` (`make ui`). The built `internal/app/ui/static/app.css`, the vendored `htmx.min.js`, `internal/app/ui/static/swagger-ui/` (bundle, stylesheet, license), and `internal/app/ui/static/fullcalendar/` (bundle, license) are committed; the CLI is cached under `build/tools` and never committed. No Node, no package.json. Dark mode is Tailwind's default `dark:` variant, which follows `prefers-color-scheme`.
- JSON API: every route, page or `/api/v1` operation, is one entry of the route table in `internal/app` (`(*App).routes`, assembled from per-feature functions such as `settingsRoutes` and `tokenRoutes`; `pageRoute` and `apiRoute` build entries). The mux and the OpenAPI 3.1 document are both built from it; there is no other registration path. An API entry carries an `apiDoc` (operationId, summary, tag, `openapi.Param`s with examples, an example request body or a `form` value whose type describes a multipart body, responses with a value of each body's wire type; `openapi.File` is a binary field or body and `Response.Type` names a body's media type when it is not JSON); the access check adds 401, 500, and, for a role, 403. `internal/openapi` derives schemas by reflection from json tags (`omitempty` makes a field optional; a `doc` tag is its description). The document is served at `/api/v1/openapi.json`, rendered by Swagger UI at `/api-docs`, and checked in at `docs/openapi.json`.
- API authentication: personal API tokens (`dct_` plus 32 entropy bytes, stored as a SHA-256 hash, optional expiry) in `Authorization: Bearer` or as the HTTP Basic password; an Authorization header that carries one wins over the session cookie, which is still accepted so the browser's own calls work.
- Time zones: the tz database reaches the application only through the `TimeZones` connector. Its driver, `internal/driver/tzdb`, calls `time.LoadLocation` and imports `time/tzdata`, so the binary carries the database and zones load on hosts without zoneinfo files. The `site_timezone` setting must name a zone it loads (not empty, not `Local`); `settings.Current().Zone` is the loaded zone. Schedule windows are read in it, and dates typed on pages are the start of that day in it.
- Pictures: `internal/picture` decodes JPEG, PNG, GIF (standard library) and WebP (`golang.org/x/image/webp`), scales with `golang.org/x/image/draw` (CatmullRom), flattens transparency onto white, and re-encodes as JPEG: an image at most `MaxSide` (1600) pixels on its longest side and a thumbnail at most `ThumbnailSide` (320). All pure Go, no cgo. An upload is at most `MaxBytes` (10 MB) and `MaxPixels` (40 million, read from the header before decoding). The app reads the `picture` field of a multipart body with `MultipartReader` in memory, never through `ParseMultipartForm`, which spools to disk. Both renditions are stored as `bytea` behind `PicturesStore` and served from `/api/v1/doors/{id}/pictures/{picture}` (and `/thumbnail`) with `Cache-Control: private, max-age=31536000, immutable`; pages use those URLs as image sources, since the API accepts the session cookie.
- Calendar views: a page lists its intervals in labelled tables, which the entrypoint suite reads, and hands the same intervals to FullCalendar as local wall-clock times without an offset (`data-events`, each with a `title` and a `kind`: `open`, `locked`, `access`, or `code`, which `ui/static/calendar.js` turns into a `calendar-<kind>` class colored in `ui/tailwind.css`); the calendar runs with `timeZone: "UTC"` so it draws them as given, whatever the browser's zone. Calendar pages share the `week` value (`(*App).weekOf`) and the `week-nav`, `calendar`, `calendar-key`, and `calendar-scripts` parts. A calendar is a page of its own (schedule, door timeline, a user's access at a door), never part of a page reached through htmx, and a link to one sets `hx-boost="false"` so its scripts load in order.
- Timelines: `internal/timeline` is pure. `Merge` turns open-mode intervals and overrides into ordered spans tagged `open` or `locked` (locked overrides win over open overrides, which win over schedules; gaps are the normal code-access state), `Of` expands a door's open-mode schedules first, and `CodeWorks` is when granted intervals open a door given its spans (never while locked). `door.Service.Timeline` loads what `Of` needs. `rule.Service.Affected` names the doors that are not deleted whose timeline or access depends on a schedule, override, zone, group, or rule as things stand; a change that moves doors in or out of that set affects the union of the sets before and after it. The API exposes it as `listAffectedDoors`, and the timeline pushes of the broker work call it directly.
- HTML assertions: test support reads pages with `golang.org/x/net/html` into `testsupport.Page`.
- Static analysis and formatting: `go vet`; golangci-lint with govet, staticcheck, errcheck, errorlint, ineffassign, unused, unconvert, misspell, bodyclose, noctx; formatters gofmt and goimports (local prefix `github.com/dakriy/doorcode`). `make fmt` rewrites formatting.

## Layout

| What | Location |
| --- | --- |
| Entrypoints | `internal/app`: `app.New(ctx, connector.Factory) (*app.App, error)` and `(*App).Handler() http.Handler` |
| Application code | everything under the module except `cmd/`, `internal/driver/`, `internal/testsupport/`, `test/`; today `internal/app` (route table, pages, JSON API, sign-in and sign-out, API tokens page and API, users and groups pages and API, schedules pages (list, form with add and remove row buttons, detail, weekly calendar) and API (including occurrences over a date range), doors pages (list with search, zone and deleted filters, pagination, and primary-picture thumbnails; new door form; detail with details, open-mode schedules, zones, picture gallery and upload, delete and restore) and API (doors, their pictures as multipart uploads and JPEG images), zones pages and API, access rule pages (from a group's page) and API, who can enter a door, override pages (from a door's page) and API, door timeline and user access calendars and API, the affected doors API, the access guards `page` and `api` driven by each route's `access`; templates and assets in `internal/app/ui`, shared form fields, badges, and button classes in `ui/templates/parts.html`), `internal/auth` (password and door code hashing, first administrator, sessions, personal API tokens, the role names, `User.Has`, the one place that decides whether someone holds a role: a user holds a role when one of their groups carries it or carries `admin`), `internal/check` (field problems: `Problem`, `Problems`, the `List` collector, `Required`, `TooLong`, `Unknown` (an id naming nothing), and `Distinct` (ids without repeats); every form and API body reports what is wrong through it), `internal/schedule` (schedules: field checks, the built-in Always schedule's refusals, `Expand`, the pure function from a schedule, a zone, and a range to merged absolute intervals, `ExpandDays`, the same over whole dates in the zone, and `DateOf`, the date an instant falls on in a zone; dates are midnight UTC on the day they name, and `DateLayout` is how one is written everywhere; `Delete` refuses with `InUseError`, naming doors and groups, while a door that is not deleted is in open mode during the schedule or an access rule uses it; `Merged` joins overlapping or touching intervals and `StartOf` is the start of a date in a zone), `internal/door` (doors, zones, pictures, and overrides: field checks, soft delete, in-memory search, zone filter, and paging of `Doors`, the first picture becoming primary and the oldest left taking over when the primary is deleted; overrides open or locked, active and upcoming, cancel (end now, only while in force), `Active`; `Timeline`, a door's timeline over a range), `internal/rule` (access rules: field checks, a group's rules, `ReachOf` (directly or through zones), `Entrants` (who can enter a door), `Windows` (when rules are active), `Reachable` and `UserAccess` (the doors a user's groups reach, and when their code works there), `Affected`), `internal/timeline` (the pure timeline functions; see Stack), `internal/picture` (`Prepare`, the pure function from an upload to its JPEG renditions, and the upload limits), `internal/directory` (users and groups: profile and group validation, soft delete, door codes, memberships, the pre-assigned join in `CreateUser`, which every way of creating a user goes through, and the refusals: your own account, your own admin role, groups carrying roles you do not hold), `internal/openapi` (OpenAPI document and reflection-based schemas), `internal/settings` (setting registry, validation, and env/database/default resolution; `Current` is the parsed view the application acts on), `internal/connector` |
| Connector interfaces | `internal/connector`: `Clock`, `Entropy`, `Environment`, `TimeZones`, `Log`, `Store` (`Migrate` plus one embedded interface per aggregate; today `SettingsStore` (also the site's door code hashing parameters), `UsersStore`, `GroupsStore` (groups and memberships), `SessionsStore`, `TokensStore`, `SchedulesStore` (schedules and their windows), `DoorsStore` (doors with their open-mode schedule ids and zone ids), `PicturesStore` (door pictures, their image and thumbnail data, the primary picture), `ZonesStore` (zones and their door ids; the same memberships as a door's zone ids), `RulesStore` (access rules with their door and zone ids; a rule goes with its group, a deleted zone leaves rules, a schedule a rule uses cannot be deleted), `OverridesStore` (door overrides with their creator's display name; a door's overrides that end after a time)) |
| Connector implementations | `internal/driver/<name>`: `systemclock` (Clock), `cryptoentropy` (Entropy), `osenv` (Environment), `tzdb` (TimeZones), `slogjson` (Log, JSON lines via `log/slog`), `postgres` (Store; migrations in `internal/driver/postgres/migrations/NNN_name.sql`, embedded) |
| Connector factory interface | `internal/connector` (`connector.Factory`, one method per connector) |
| Production wiring | `cmd/doorcode` (`main.go`: reads `DOORCODE_DATABASE_URL` and `DOORCODE_LISTEN_ADDR`, opens the Postgres store, builds the production factory, calls `app.New`, serves `http.Server`); `cmd/doorcode-openapi` writes `app.OpenAPI()` to stdout for `make openapi` and the drift gate |
| Entrypoint suite | `test/entrypoint` |
| Connector suite, and how a test is marked slow | `test/connector`. Fast files carry `//go:build !slow`, slow files `//go:build slow`; untagged `_test.go` files there hold the shared implementation tables (`clockImpls`, `entropyImpls`, `envImpls`, `zonesImpls`, `logImpls` in `system_impls_test.go`; `storeImpls` in `store_impls_test.go`). Each connector test runs against every entry of its table (stub and real). The Store tests are slow; the others are fast. `freshSchema` gives a test its own schema URL, and `connect` a raw connection to it, for a test that must arrange an older schema (as the role migration test does with tern's `MigrateTo`). |
| Test support, stubs, and the stub factory | `internal/testsupport`: `NewFactory()` returns a `*Factory` whose fields `ClockStub`, `EntropyStub`, `EnvStub`, `ZonesStub`, `LogStub`, `StoreStub` are the stubs; `Get`, `PutJSON`, `Browser` (`NewBrowser`, keeps cookies, does not follow redirects), `BasicAuth`, `Response`, `ParsePage` and `Page` (what a person sees on an HTML page), `JSON`, `JSONAt`, `JSONString` (JSON bodies as plain values, by JSON pointer), `OpenAPIOperations` and `APIOperation` (each documented operation, with its example target and body, and its listed statuses), `Handler`, `LogEntry`, `ErrorText`, `DefaultNow`, `FastHashMemory`, `AdministratorsGroup` (the group the migration seeds), `AlwaysSchedule` (the schedule the migration seeds); `Browser.Upload` (a multipart file and fields), `Browser.SendForm` (a raw body with a content type), `Browser.Header` (one header of a GET); `PNG` (a shaded image of a size, its top-left pixel transparent), `PNGHeader` (a header claiming a size with no pixels), `Padded` (content padded with zeros to a size), `ImageInfo` (an image body's format and size, such as `jpeg 320x240`) |

The entry function and how tests pass it the stub factory: an entrypoint test builds `f := testsupport.NewFactory()`, arranges stubs through its fields (`f.ClockStub.Set(rfc3339)`, `f.EnvStub["DOORCODE_SITE_NAME"] = "..."`, `f.StoreStub.SeedSetting(key, value, rfc3339)`, `f.StoreStub.Fail()`, `f.StoreStub.FailMigration()`), calls `app.New(t.Context(), f)` (the suite's `start(t, f)` helper fails the test on error), and drives `a.Handler()` in-process with `testsupport.Get`, `testsupport.PutJSON`, or a `testsupport.Browser` (`Get`, `Post` with alternating field names and values, `PutJSON`, `PostJSON`, `Delete`, `Send`, `SetHeader`, `SetCookie`, `Cookie`), which return a `testsupport.Response{Status, ContentType, Location, SetCookie, Body}` to compare with `cmp.Diff`. HTML bodies are compared as `testsupport.ParsePage(body)` (title, first heading, navigation links and their targets, alert and status messages, notes, field errors, input and text area values, checked checkboxes, the first unlabelled table, every table with an `aria-label` by label, images with their sources and alternative text, each checkbox's label text, each select's options, assets); the suite's `pageOf` leaves assets, navigation targets, checkbox labels, and select options out. New pages label every table; a page explains an action it does not offer in a `role="note"`. The suite's `signedInAdmin(t, f)` returns a browser signed in as the first administrator, whose password it reads from the log; `signIn(t, f, h)` does the same on an existing handler, and `adminSession(t, f)` returns both the handler and such a browser. `createToken(t, b, body)` creates a token through the API and returns its secret, `tokenClient(h, secret)` is a cookie-less client sending it as a bearer token, and `adminWithToken(t, f, body)` does both for the first administrator. `send(t, c, method, target, body, status)` calls the API and fails unless it answers with status; `createUser`, `createGroup`, and `join` build on it (users and groups created after startup get ids 2, 3, ... in order; the administrator is user 1 and the seeded administrators group is group 1); `userToken(t, h, b, user)` is a client holding a token the administrator created for that user; `adminHolding(t, f, roles)` leaves the administrator a member of only a new group carrying roles; `fiveUsers` and `manyUsers` seed user lists. `createSchedule(t, c, body)` creates a schedule through the API (Always is schedule 1, so the first one created gets id 2), `inZone(t, zone)` is an administrator's browser on a site whose timezone the environment sets, and `occurrences(t, c, id, query)` reads a schedule's occurrences as JSON. `createDoor` and `createZone` create through the API (doors, zones, and pictures are each numbered from 1); `doorSite` is an administrator's browser on a site with the schedules Weekdays (2) and Nights (3) and the zones North (1) and Annex (2); `uploadPicture` uploads through the API; `doorWithPictures` gives the front door (1) a 30x20 picture (1, primary) and a 20x30 one (2); `fiveDoors`, `manyDoors`, and `zonedDoors` seed door lists; `doorPage` reads a page after the sign-in flash is spent. `ruleSite` is `doorSite` with the doors Lobby (1, in North), Dock (2, in Annex), and Attic (3) and the groups Staff (2) and Guests (3); `staffAnn` adds Ann (2) as a member of Staff; `createRule` and `createOverride` create through the API (each numbered from 1); `lockedToday` is a locked override from 10:00 to 12:00 UTC on the stub clock's day; `timeline` and `affected` read those API answers; `freshDoorTables`, `noRules`, and `noAccess` are the tables a door, group, and user page show before any override or rule. JSON bodies are compared as `testsupport.JSON(literal)` against `testsupport.JSONAt(body, pointer)`. Logged lines are compared as `[]testsupport.LogEntry` against `f.LogStub.Entries`. No socket is opened.

Stub behavior tests rely on: the clock reads `testsupport.DefaultNow` until set; the entropy stub hands out consecutive bytes from `0x00`. Startup draws 18 bytes for the first administrator's password (`AAECAwQFBgcICQoLDA0ODxAR`) and 16 for its salt, so after startup the first request id is `2223242526272829`; signing in draws 32 more for the session token, so after `signedInAdmin` it is `4243444546474849`; creating a token draws 32 more for its secret before storing it, so the first token after `signedInAdmin` is `dct_QkNERUZHSElKS0xNTk9QUVJTVFVWV1hZWltcXV5fYGE` and the next request id is `6263646566676869`. Setting a door code draws 16 bytes for a candidate code hashing salt, so after `signedInAdmin` and one code the next request id is `5253545556575859`. The stub environment sets `DOORCODE_PASSWORD_HASH_MEMORY_KIB` to `testsupport.FastHashMemory` (8 KiB) so the suite hashes quickly; delete it from `f.EnvStub` after startup, never before, when a test needs the setting writable. The store reports `testsupport.ErrNotMigrated` until `Migrate` runs (as a real database without its tables would), except for values placed with `SeedSetting`, `SeedUser`, and `SeedCodeHashing`. The first `Migrate` seeds `AdministratorsGroup` and `AlwaysSchedule`, as the real migrations do; the first administrator joins the group. The stub time zones load zones as the real connector does (zones are fixed data, not state), and the site timezone is `UTC` unless a test sets `DOORCODE_SITE_TIMEZONE`. The stub refuses what the database's constraints would: duplicate unique values (schedule names ignoring case), memberships of missing users or groups, and nil role, user id, weekday, or excluded date lists. `Fail` fails every aggregate; `FailSettings`, `FailUsers`, `FailGroups`, `FailSessions`, `FailTokens`, `FailSchedules`, `FailDoors` (doors and pictures), `FailZones` fail one; `FailSettingWrites`, `FailUserWrites`, `FailGroupWrites`, `FailScheduleWrites`, `FailDoorWrites`, `FailZoneWrites` fail only the methods that change an aggregate, for the failure branches behind a successful read; `FailPrimaryPictures` fails only `SetPrimaryPicture`. `FailRules` and `FailOverrides` fail those aggregates and `FailRuleWrites` and `FailOverrideWrites` their writes. Deleting a group removes its rules and deleting a zone takes it off every rule, as the cascades do; deleting a schedule a rule uses is refused (`ErrReferenced`); rules and overrides naming what the stub does not hold are refused (`ErrNoSuchReference`). The stub refuses references to schedules, zones, doors, or pictures it does not hold (`ErrNoSuchReference`), another door's picture as a primary one, nil picture data, and duplicate door and zone names ignoring case; deleting a schedule takes it off the doors in open mode during it, as the foreign key's cascade does, and deleting a picture clears the primary picture it was. `ExpireUser`, `DeleteUser`, `RevokeRoles` (ends every membership), `LeaveGroup`, `RemovePassword`, `SetPasswordHash` change a stored user by email.

A setting is defined once in `internal/settings` (`definitions`), with an optional validator that both updates and `Current` apply. Its environment override is `DOORCODE_` plus the upper-cased key; a set variable (even empty) wins and makes the setting read-only.

The list of non-application directories lives once, in `scripts/scope.sh`; the coverage and mutation scopes are derived from it. The depguard and forbidigo file globs in `.golangci.yml` and the path regexes in `.golangci.connector.yml` mirror it and must change with it.

## Commands

| Command | Invocation |
| --- | --- |
| `verify-fast` | `make verify-fast` |
| `verify-mutation` | `make verify-mutation` |
| `verify-integration` | `make verify-integration` |
| `verify-all` | `make verify-all` |

`verify-fast` runs, in order: `go build ./...`; `go vet ./...`; `go vet -tags=slow ./test/connector/...` (integration compile); `golangci-lint run` and `run --build-tags=slow` (static analysis, formatting, import gates, effect calls, application complexity cap); `golangci-lint run -c .golangci.connector.yml` (connector complexity cap); `scripts/fast-tests.sh` (entrypoint suite with coverage, fast connector tests with connector coverage printed for information, coverage gate, runtime budget); `scripts/deadcode.sh`; `scripts/openapi-drift.sh` (OpenAPI drift gate: regenerates the document into `build/openapi.json` and fails when it differs from the checked-in `docs/openapi.json`); the dupl report. Make stops at the first failing step.

`make openapi` rewrites `docs/openapi.json` from the route table. Run it after changing a route, a wire struct, or an `apiDoc`, read the diff, and commit it. The entrypoint suite asserts the document's operations, statuses, and schemas by hand-written expectations, never by comparing with this file.

`verify-mutation` runs `scripts/mutation.sh`: gremlins with every optional mutator enabled and `--timeout-coefficient=100`, then prints every `LIVED`, `NOT COVERED`, and `TIMED OUT` line and fails if there is one. The log is `build/mutation.log`, the JSON report `build/mutation.json`.

Runtime budget for `verify-fast` tests: 10 seconds (`FAST_BUDGET_SECONDS` in the Makefile), measured over both test invocations with `-count=1` so the cache does not hide the cost. Measured at bootstrap: about 0.7 seconds; after the foundation work about 0.6 seconds; after local sign-in and the UI shell about 1.1 seconds; after the route table, OpenAPI, and API tokens about 1.3 seconds; after users, groups, and roles about 2.1 seconds; after schedules about 2.2 seconds; after doors, zones, and pictures about 2.5 seconds; after building the OpenAPI document on first request and running entrypoint tests in parallel about 1.1 seconds (the prebuilt entrypoint test binary alone about 0.15 seconds wall, down from about 1.9); after access rules, overrides, and timelines about 1.5 seconds.

`make ui` (`scripts/vendor-ui.sh`) rebuilds `internal/app/ui/static/app.css` with the pinned Tailwind CLI and re-vendors htmx and Swagger UI; run it after changing a template's classes or `internal/app/ui/tailwind.css`, and commit the result. It needs network access the first time. To upgrade a tool, change its version and every checksum in the script together, taking the checksums from the release's published sums (for Swagger UI, the SHA-256 of the npm tarball after checking it against the registry's published sha512 `integrity`).

Slow connector test prerequisites (record each new one here when the first slow test that needs it lands):

- **Postgres.** A disposable local PostgreSQL 18 whose URL is in `DOORCODE_TEST_DATABASE_URL`. `docker compose up -d --wait` starts one from `docker-compose.yml` (user, password, and database `doorcode`); then `export DOORCODE_TEST_DATABASE_URL='postgres://doorcode:doorcode@localhost:5432/doorcode?sslmode=disable'`. Set `DOORCODE_POSTGRES_PORT` (and `DOORCODE_MQTT_PORT`) before `docker compose up` when the host ports are taken, and use that port in the URL. Each test creates and drops its own schema, so the database may be shared. Without the variable the slow Store tests fail; they are never skipped.

## Complexity caps

- Metrics: cyclomatic (gocyclo) and cognitive (gocognit), both reporting strictly above the cap
- Application code, per function, each metric: 15
- Connector code, per function, each metric: 5

## Effect packages

Standard-library and third-party packages application code may not import
because they perform I/O or read nondeterministic state.

Denied by import (depguard rule `application`; entries match as prefixes, `$` anchors):

- `os`, `io/ioutil`, `io/fs`, `path/filepath`: filesystem, process, environment
- `syscall`, `plugin`: raw system calls, loading code from disk
- `net$`, `net/rpc`, `net/smtp`: sockets and network protocols
- `net/http/httputil`, `net/http/cookiejar`: reverse proxying and HTTP client state
- `net/http/httptest`: opens listeners; tests reach it only through `internal/testsupport`
- `database/sql`, `github.com/jackc/pgx`, `github.com/jackc/tern`: database
- `math/rand` (and `math/rand/v2`), `crypto/rand`: randomness and entropy
- `log` (and `log/slog`, `log/syslog`): writes to process stderr or syslog
- `github.com/google/uuid`: identifier generation
- `golang.org/x/crypto/bcrypt`: draws its salt from `crypto/rand`; passwords are hashed by `internal/auth` with argon2id and an Entropy salt
- `github.com/dakriy/doorcode/internal/driver`, `github.com/dakriy/doorcode/cmd`, `github.com/dakriy/doorcode/internal/testsupport`: connector implementations, production wiring, test support

`net/http` is allowed because the entrypoint is an HTTP handler and needs its server-side types. Its effectful parts are denied by call instead (forbidigo, application paths only):

- `time.Now`, `Since`, `Until`, `Sleep`, `After`, `AfterFunc`, `Tick`, `NewTimer`, `NewTicker`: clock
- `time.LoadLocation`: reads the tz database from the filesystem and the `ZONEINFO` variable; zones come from the `TimeZones` connector
- `os.Getenv`, `LookupEnv`, `Environ`, `ExpandEnv`: environment
- `fmt.Print`, `Printf`, `Println`, `Scan`, `Scanf`, `Scanln`, builtin `print`/`println`: process stdio
- `http.Get`, `Head`, `Post`, `PostForm`, `DefaultClient`, `DefaultTransport`, `Client`, `Transport`, `NewRequest`, `NewRequestWithContext`, `ListenAndServe`, `ListenAndServeTLS`, `Serve`, `ServeTLS`, `Server`, `ServeFile`, `ServeFileFS`, `FileServer`, `FileServerFS`, `Dir`: HTTP client, listener, filesystem serving
- `uuid.*`: identifier generation

Mocking libraries are denied in every file of the repository (depguard rule `no-mocks`): `github.com/golang/mock`, `go.uber.org/mock`, `github.com/stretchr/testify/mock`, `github.com/vektra/mockery`, `github.com/gojuno/minimock`, `github.com/petergtz/pegomock`, `github.com/maxbrunsfeld/counterfeiter`, `github.com/DATA-DOG/go-sqlmock`, `github.com/jarcoal/httpmock`.

## Fast-suite dependencies

Test libraries the entrypoint suite may import: the test runner and the
assertion library, nothing that can replace a collaborator.

The depguard rule `entrypoint-suite` (strict allowlist, exact matches) permits only:

- `testing`: the runner
- `github.com/google/go-cmp/cmp`: the assertion library
- `github.com/dakriy/doorcode/internal/app`: the entrypoint package
- `github.com/dakriy/doorcode/internal/testsupport`: stubs, stub factory, request helpers

Nothing else, standard library included. A test that needs `errors`, `strings`, or the connector interfaces gets a helper in `internal/testsupport` instead.

## Generated code

Paths excluded from coverage because the code is generated. Nothing else is
excluded from coverage.

None. Add module-relative path prefixes to `scripts/generated-paths.txt`.

## Mutation exclusions

Suppressed mutants, each with why it is equivalent.

None.

## Deviations

Intentional departures from the judgment rules, each with scope and reason.

None.

## Known gaps

Gates the bootstrap could not establish for this stack, and what compensates.

- **Branch coverage.** Go reports statement coverage only. The coverage gate is 100% of statements; mutation testing (conditional negation and boundary mutants on every condition) is the compensating control for unexercised branches.
- **Effect calls through `net/http`.** The package cannot be denied wholesale, since the entrypoint needs it. forbidigo bans the listed client, listener, and file-serving identifiers by name; an effect reached some other way through `net/http` (for example a method on a value obtained elsewhere) is not seen. Review catches it.
- **forbidigo matches by text.** It runs without type analysis, so an aliased import (`t "time"; t.Now()`) evades the clock ban. Do not alias effect packages.
- **gremlins mutators.** gremlins does not mutate unary negation (`!x`), returned values, or removed statements. The entrypoint suite's outcome assertions are the compensation; review checks that weakening an assertion would make a test fail. gremlins also cannot attribute coverage to negative numeric constants or to `switch { case <expr>: }` conditions; mutants there stay `NOT COVERED`. Move the expression into a covered statement (negate a positive constant at use, name the boolean before the switch) rather than suppressing it.
- **Dead code in whole packages.** deadcode reports unreachable functions in packages it loads with `./...`; it does not see test-only reachability. `internal/testsupport` is filtered out because it is reached only from tests. An application package nothing imports is also caught by the coverage gate at 0%.
- **Connector-test `unused`.** The `unused` linter is off for `test/connector/` because the shared implementation tables are unused under whichever build tag has no tests of a given connector. Dead connector-test helpers are not detected.
- **Built stylesheet drift.** Nothing in `verify-fast` checks that the committed `app.css` matches the templates, since building it needs the downloaded Tailwind CLI. A class added to a template without `make ui` is simply unstyled. Review checks that a template change comes with a rebuilt stylesheet.
- **Response bodies against the document.** The spec test (`TestEveryDocumentedOperationAnswersWithADocumentedStatus`) checks that every documented operation answers with a listed status; it does not validate response bodies against their schemas. The schemas are derived from the same wire types the handlers encode, and the entrypoint suite asserts both by hand; review checks that a handler writes the type its entry documents.
- **Calendar script.** No test runs the browser script that draws a calendar (`ui/static/calendar.js`) or FullCalendar itself. The entrypoint suite asserts the occurrences the page lists in its table, from the same values the script reads; review checks the script by loading the page.
- **Stub time zones are the host's.** The `TimeZones` stub loads zones as the driver does, from the host's tz database or Go's embedded copy, rather than from fixtures; tests use zones whose rules for the dates they assert are long settled.
- **CI.** The repository has no CI definition. When one is added, run `make verify-fast` and `make verify-mutation` on every change and `make verify-integration` on a schedule.
