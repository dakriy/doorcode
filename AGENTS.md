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
- Web UI: `html/template` pages and static assets embedded from `internal/app/ui`; htmx v2.0.10 and the Tailwind CSS v4.3.3 standalone CLI, both pinned with SHA-256 checksums in `scripts/vendor-ui.sh` (`make ui`). The built `internal/app/ui/static/app.css` and the vendored `htmx.min.js` are committed; the CLI is cached under `build/tools` and never committed. No Node, no package.json. Dark mode is Tailwind's default `dark:` variant, which follows `prefers-color-scheme`.
- HTML assertions: test support reads pages with `golang.org/x/net/html` into `testsupport.Page`.
- Static analysis and formatting: `go vet`; golangci-lint with govet, staticcheck, errcheck, errorlint, ineffassign, unused, unconvert, misspell, bodyclose, noctx; formatters gofmt and goimports (local prefix `github.com/dakriy/doorcode`). `make fmt` rewrites formatting.

## Layout

| What | Location |
| --- | --- |
| Entrypoints | `internal/app`: `app.New(ctx, connector.Factory) (*app.App, error)` and `(*App).Handler() http.Handler` |
| Application code | everything under the module except `cmd/`, `internal/driver/`, `internal/testsupport/`, `test/`; today `internal/app` (HTTP routes, pages, JSON responses, sign-in and sign-out, the role gates `signedIn`, `admin`, `adminAPI`; templates and assets in `internal/app/ui`), `internal/auth` (password hashing, first administrator, sessions, `User.Has`, the one place that decides whether someone holds a role), `internal/settings` (setting registry, validation, and env/database/default resolution; `Current` is the parsed view the application acts on), `internal/connector` |
| Connector interfaces | `internal/connector`: `Clock`, `Entropy`, `Environment`, `Log`, `Store` (`Migrate` plus one embedded interface per aggregate; today `SettingsStore`, `UsersStore`, `SessionsStore`) |
| Connector implementations | `internal/driver/<name>`: `systemclock` (Clock), `cryptoentropy` (Entropy), `osenv` (Environment), `slogjson` (Log, JSON lines via `log/slog`), `postgres` (Store; migrations in `internal/driver/postgres/migrations/NNN_name.sql`, embedded) |
| Connector factory interface | `internal/connector` (`connector.Factory`, one method per connector) |
| Production wiring | `cmd/doorcode` (`main.go`: reads `DOORCODE_DATABASE_URL` and `DOORCODE_LISTEN_ADDR`, opens the Postgres store, builds the production factory, calls `app.New`, serves `http.Server`) |
| Entrypoint suite | `test/entrypoint` |
| Connector suite, and how a test is marked slow | `test/connector`. Fast files carry `//go:build !slow`, slow files `//go:build slow`; untagged `_test.go` files there hold the shared implementation tables (`clockImpls`, `entropyImpls`, `envImpls`, `logImpls` in `system_impls_test.go`; `storeImpls` in `store_impls_test.go`). Each connector test runs against every entry of its table (stub and real). The Store tests are slow; the others are fast. |
| Test support, stubs, and the stub factory | `internal/testsupport`: `NewFactory()` returns a `*Factory` whose fields `ClockStub`, `EntropyStub`, `EnvStub`, `LogStub`, `StoreStub` are the stubs; `Get`, `PutJSON`, `Browser` (`NewBrowser`, keeps cookies, does not follow redirects), `Response`, `ParsePage` and `Page` (what a person sees on an HTML page), `Handler`, `LogEntry`, `ErrorText`, `DefaultNow`, `FastHashMemory` |

The entry function and how tests pass it the stub factory: an entrypoint test builds `f := testsupport.NewFactory()`, arranges stubs through its fields (`f.ClockStub.Set(rfc3339)`, `f.EnvStub["DOORCODE_SITE_NAME"] = "..."`, `f.StoreStub.SeedSetting(key, value, rfc3339)`, `f.StoreStub.Fail()`, `f.StoreStub.FailMigration()`), calls `app.New(t.Context(), f)` (the suite's `start(t, f)` helper fails the test on error), and drives `a.Handler()` in-process with `testsupport.Get`, `testsupport.PutJSON`, or a `testsupport.Browser` (`Get`, `Post` with alternating field names and values, `PutJSON`, `SetHeader`, `SetCookie`, `Cookie`), which return a `testsupport.Response{Status, ContentType, Location, SetCookie, Body}` to compare with `cmp.Diff`. HTML bodies are compared as `testsupport.ParsePage(body)` (title, first heading, navigation links, alert and status messages, field errors, input values, first table, assets); the suite's `pageOf` leaves assets out. The suite's `signedInAdmin(t, f)` returns a browser signed in as the first administrator, whose password it reads from the log; `signIn(t, f, h)` does the same on an existing handler. Logged lines are compared as `[]testsupport.LogEntry` against `f.LogStub.Entries`. No socket is opened.

Stub behavior tests rely on: the clock reads `testsupport.DefaultNow` until set; the entropy stub hands out consecutive bytes from `0x00`. Startup draws 18 bytes for the first administrator's password (`AAECAwQFBgcICQoLDA0ODxAR`) and 16 for its salt, so after startup the first request id is `2223242526272829`; signing in draws 32 more for the session token, so after `signedInAdmin` it is `4243444546474849`. The stub environment sets `DOORCODE_PASSWORD_HASH_MEMORY_KIB` to `testsupport.FastHashMemory` (8 KiB) so the suite hashes quickly; delete it from `f.EnvStub` after startup, never before, when a test needs the setting writable. The store reports `testsupport.ErrNotMigrated` until `Migrate` runs (as a real database without its tables would), except for values placed with `SeedSetting` and `SeedUser`. `Fail` fails every aggregate; `FailSettings`, `FailUsers`, `FailSessions` fail one. `ExpireUser`, `DeleteUser`, `RevokeRoles`, `RemovePassword`, `SetPasswordHash` change a stored user by email.

A setting is defined once in `internal/settings` (`definitions`), with an optional validator that both updates and `Current` apply. Its environment override is `DOORCODE_` plus the upper-cased key; a set variable (even empty) wins and makes the setting read-only.

The list of non-application directories lives once, in `scripts/scope.sh`; the coverage and mutation scopes are derived from it. The depguard and forbidigo file globs in `.golangci.yml` and the path regexes in `.golangci.connector.yml` mirror it and must change with it.

## Commands

| Command | Invocation |
| --- | --- |
| `verify-fast` | `make verify-fast` |
| `verify-mutation` | `make verify-mutation` |
| `verify-integration` | `make verify-integration` |
| `verify-all` | `make verify-all` |

`verify-fast` runs, in order: `go build ./...`; `go vet ./...`; `go vet -tags=slow ./test/connector/...` (integration compile); `golangci-lint run` and `run --build-tags=slow` (static analysis, formatting, import gates, effect calls, application complexity cap); `golangci-lint run -c .golangci.connector.yml` (connector complexity cap); `scripts/fast-tests.sh` (entrypoint suite with coverage, fast connector tests with connector coverage printed for information, coverage gate, runtime budget); `scripts/deadcode.sh`; the dupl report. Make stops at the first failing step.

`verify-mutation` runs `scripts/mutation.sh`: gremlins with every optional mutator enabled and `--timeout-coefficient=100`, then prints every `LIVED`, `NOT COVERED`, and `TIMED OUT` line and fails if there is one. The log is `build/mutation.log`, the JSON report `build/mutation.json`.

Runtime budget for `verify-fast` tests: 10 seconds (`FAST_BUDGET_SECONDS` in the Makefile), measured over both test invocations with `-count=1` so the cache does not hide the cost. Measured at bootstrap: about 0.7 seconds; after the foundation work about 0.6 seconds; after local sign-in and the UI shell about 1.1 seconds.

`make ui` (`scripts/vendor-ui.sh`) rebuilds `internal/app/ui/static/app.css` with the pinned Tailwind CLI and re-vendors htmx; run it after changing a template's classes or `internal/app/ui/tailwind.css`, and commit the result. It needs network access the first time. To upgrade either tool, change its version and every checksum in the script together, taking the checksums from the release's published sums.

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
- **CI.** The repository has no CI definition. When one is added, run `make verify-fast` and `make verify-mutation` on every change and `make verify-integration` on a schedule.
