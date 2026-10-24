# ibkr-cli — Specification (DRAFT, 2026-10-24)

A Go command-line client for the **Interactive Brokers Client Portal
Web API 1.0 (CPAPI 1.0)** — modeled 1:1 on
[schwab-cli](https://github.com/rayli09/schwab-cli) and
[etrade-cli](https://github.com/rayli09/etrade-cli): same layout,
conventions, and safety rails, with the endpoint surface taken from
IBKR's official Web API documentation and cross-checked against
[IBind](https://github.com/Voyz/ibind) (the community Python client by
the IBeam author).

Status: **spec draft for Ray's approval. No code yet. No IBKR account
or credentials exist on file** — like etrade-cli, the build proceeds
against documentation + a stub server, and live wiring happens only
when Ray supplies an account.

---

## 1. Route decision (made 2026-10-24)

**Chosen: Client Portal Web API 1.0 (REST + WebSocket), not the TWS API.**

Why, in order of weight:

1. **Agent fit.** CPAPI is stateless REST + JSON. One CLI invocation =
   one request/response. The TWS API is a stateful, asynchronous TCP
   socket protocol behind a logged-in TWS/IB Gateway process — every
   command would need connect → handshake → account sync → request →
   drain, and a Go implementation means writing the wire protocol
   (no official Go SDK exists; community Go libs are thin).
2. **Sibling consistency.** schwab-cli / etrade-cli are REST ports.
   CPAPI keeps the same command grammar, config model, and test
   strategy. etrade-cli's hand-written OAuth 1.0a signer (RFC 5849
   vector-verified) is directly reusable for CPAPI's OAuth mode.
3. **Headless is a solved problem here.** Two community-proven paths
   exist (§3): IBeam for gateway mode, IBind's OAuth 1.0a
   implementation as the reference for direct mode.

**What we give up vs TWS API** (accepted, recorded): no
`reqSecDefOptParams`-style full option-chain enumeration, no native
Greeks surface — chains are assembled from strikes + per-conid
snapshots (§6). If chain-surface work ever becomes the core use case,
the TWS route can be revisited as a separate tool; it does not block
or invalidate this one.

**Non-goals for v1:** WebSocket streaming commands (the CLI is
one-shot; streaming belongs to a daemon, not this tool), combo/bracket
order construction beyond what CPAPI's order object passes through,
futures/forex-specific helpers (they work via the same contract/order
model but get no dedicated commands), Flex Web Service reporting.

> **Superseded 2026-10-24 (Ray's parity directive — "need to match
> all" of IBind):** the WebSocket non-goal above no longer holds.
> Streaming is implemented as *bounded* stream commands (`stream
> market|history|ladder|orders|trades|account|notifications`,
> --duration/--max-events, NDJSON out) over both auth modes, and the
> v1 surface was extended to full IBind parity: 94 leaf commands
> covering all 85 IBind REST methods and all 8 WS subscription
> classes (see README.md's parity table). The one-shot character is
> preserved — streams are bounded, not a daemon. The OAuth-mode WS
> handshake (api=<session> cookie + ?oauth_token=) follows IBind's
> implementation but, like P5, has never been exercised against a
> live IBKR account; treat it as implemented-but-unverified until a
> paper account exists.

## 2. Architecture

- **Language/stack:** Go (latest stable, ≥1.27 per sibling repos) +
  cobra. Standard library for HTTP and OAuth signing; no third-party
  OAuth library (sibling rule).
- **Layout** (mirrors etrade-cli):

  ```
  main.go
  cmd/
    root.go        # global flags, config load, auth-mode dispatch
    auth.go        # auth status / tickle / reauthenticate / oauth token mgmt
    accounts.go    # accounts list / summary / positions / ledger / pnl
    market.go      # search / quote / snapshot / history / strikes / chain
    orders.go      # orders list / get / preview / place / reply / modify / cancel / trades
  internal/
    cpapi/         # typed REST client: endpoints, rate limiter, error model
    auth/          # AuthProvider interface + gateway impl + oauth impl
    config/        # 0600 config file, env overrides
    contracts/     # conid resolution + cache, contract model
  ```

- **One client, two transports.** All commands talk to `cpapi.Client`.
  The client is constructed with a base URL + an `auth.Transport`
  (RoundTripper) supplied by the active AuthProvider:
  - gateway mode → base `https://localhost:5000/v1/api`, plain
    transport with TLS verification disabled **only** for the
    localhost gateway (self-signed cert; documented, scoped exception).
  - oauth mode → base `https://api.ibkr.com/v1/api` (verify exact
    host/path at build time against IBKR's OAuth docs), signing
    transport.
- **Rate limiting is in the client, not the commands.** CPAPI limits
  are per-endpoint (official docs): snapshot 10 req/s; history
  5 concurrent; `/iserver/orders` and `/iserver/trades` 1 req/5 s;
  `/portfolio/*` 1 req/5 s; `/tickle` 1 req/s; `/sso/validate`
  1 req/min; `/trsrv/secdef` ≤200 conids/request. The client carries
  a per-route limiter table; commands never sleep ad hoc.
- **Output:** human tables by default, `--json` for raw/structured
  output on every read command (agent consumption). Errors are typed
  (`SessionError`, `RateLimitError`, `QuestionPending`, …) and exit
  codes are stable: 0 ok, 2 usage, 3 auth/session, 4 API error,
  5 question-pending (orders).

## 3. Authentication — two modes behind one interface

```go
type AuthProvider interface {
    Transport() http.RoundTripper
    BaseURL() string
    Status(ctx) (SessionState, error)   // authenticated? brokerage session live?
    EnsureSession(ctx) error            // tickle / reauthenticate as applicable
}
```

### Mode G — Gateway via IBeam (build first)

- IBeam (Docker, `voyz/ibeam`) runs the official Client Portal
  Gateway, automates SSO login + 2FA, and maintains the session
  (tickle / reauthenticate / restart-on-failure). The CLI assumes
  the gateway is up at `https://localhost:5000` and **never handles
  the IBKR password itself**.
- CLI responsibilities in this mode: `auth status` →
  `GET /iserver/auth/status`; `auth tickle` → `POST /tickle`;
  `auth reauth` → `POST /iserver/reauthenticate`. If the brokerage
  session is down, commands fail with exit 3 and a message naming
  IBeam as the session owner (the CLI does not try to log in).
- Works with a paper (DU) account for development; no IBKR-side
  developer registration needed. This is why it ships first.

### Mode O — OAuth 1.0a direct (build second, same commands)

- IBKR's OAuth 1.0a variant: consumer key/secret issued through
  IBKR's self-service API portal; a Diffie-Hellman exchange derives
  the live-session token used as the HMAC-SHA256 signing key
  (IBind's Python implementation is the reference port — same
  diligence pattern as etrade-cli's pyetrade cross-check).
- No gateway process at all; the CLI signs each request. Token/secret
  material lives in the 0600 config (§4); secrets are never logged,
  never printed, and `auth status` reports presence + expiry only.
- **Build-time verification items** (do not guess): exact OAuth base
  URL and token endpoints, DH parameter set IBKR publishes, access
  token lifetime and renewal flow, whether retail self-service
  issuance covers all CPAPI endpoints or a subset.

### Session facts both modes must respect

- **One brokerage session per username across all IB platforms.**
  A live CPAPI brokerage session and a TWS/website login under the
  same username kick each other. The CLI surfaces this in `auth
  status` output and in the README — it is the #1 operational
  surprise for new users.
- Sessions are two-tier (outer read-only session; brokerage session
  for `/iserver/*`). `EnsureSession` distinguishes "read-only OK"
  from "brokerage live" so read commands can succeed while order
  commands correctly refuse.
- Idle sessions decay in minutes; long command chains call tickle
  between steps (the client does this automatically before any
  `/iserver` call if the last tickle is >5 min old — in gateway mode
  IBeam also maintains it, belt and suspenders).

## 4. Configuration

- File: `~/.config/ibkr-cli/config.json`, mode **0600**, created by
  `ibkr-cli auth init` (interactive, values never echoed). Holds:
  auth mode, gateway base URL, OAuth consumer key/secret + token
  material (mode O), default account id, paper/live preference.
- Env overrides: `IBKR_CLI_CONFIG` (path), `IBKR_GATEWAY_URL`.
  Credentials are **not** accepted via flags (shell history) and
  never via chat — wiring uses the secure-entry flow at build time,
  as with the sibling CLIs.
- Paper vs live is an **account property**, not a mode: paper
  accounts carry `DU…` ids. Mutating commands print the resolved
  account id + a `PAPER`/`LIVE` tag in their preview output, always.

## 5. Endpoint map and command tree

Base path `/v1/api` in both modes. Endpoint spellings below are from
IBKR's Web API documentation (checked 2026-10-24); each is re-verified
against the live docs during its build phase.

### Session / auth

| Command | Method & endpoint | Notes |
|---|---|---|
| `auth status` | GET `/iserver/auth/status` | authenticated + brokerage-session flags |
| `auth tickle` | POST `/tickle` | keepalive; also auto-invoked by client |
| `auth reauth` | POST `/iserver/reauthenticate` | brokerage session re-init |
| `auth validate` | GET `/sso/validate` | outer session check (1 req/min limit) |
| `auth oauth-status` | local only | mode O: token presence/expiry, no network |

### Accounts

| Command | Method & endpoint | Notes |
|---|---|---|
| `accounts list` | GET `/iserver/accounts` + GET `/portfolio/accounts` | merged view: ids, types, paper tag |
| `accounts summary` | GET `/portfolio/{id}/summary` | net liquidation, buying power, margin |
| `accounts positions` | GET `/portfolio/{id}/positions/0` | paginated upstream; CLI aggregates |
| `accounts ledger` | GET `/portfolio/{id}/ledger` | per-currency cash view |
| `accounts pnl` | GET `/iserver/account/pnl/partitioned` | 1 req/5 s limiter applies |

### Market data & contracts

| Command | Method & endpoint | Notes |
|---|---|---|
| `market search <text>` | GET `/iserver/secdef/search` | name/symbol → conids |
| `market stocks <sym…>` | GET `/trsrv/stocks` | symbol → per-exchange conids |
| `market info <conid>` | GET `/iserver/secdef/info` | contract details |
| `market quote <conid…>` | GET `/iserver/marketdata/snapshot` | field set §6; priming rule §7 |
| `market history <conid>` | GET `/iserver/marketdata/history` | `--period`, `--bar`; ≤5 concurrent |
| `market strikes <underlier-conid>` | GET `/iserver/secdef/strikes` | `--month` (e.g. `NOV26`) |

### Orders

| Command | Method & endpoint | Notes |
|---|---|---|
| `orders list` | GET `/iserver/account/orders` | live orders, all accounts in session |
| `orders trades` | GET `/iserver/account/trades` | executions (1 req/5 s) |
| `orders preview` | POST `/iserver/account/{id}/order/whatif` | margin/cost impact; **no order placed** |
| `orders place` | POST `/iserver/account/{id}/orders` | gated, §5.1; may enter Q/A state |
| `orders reply <replyId>` | POST `/iserver/reply/{replyId}` | answer a pending question (`--confirm`) |
| `orders modify` | POST `/iserver/account/{id}/order/{orderId}` | same gating as place |
| `orders cancel` | DELETE `/iserver/account/{id}/order/{orderId}` | prints order summary first |

21 leaf commands in v1 (sibling scale: etrade-cli shipped 24).

### 5.1 Order safety model (non-negotiable, sibling rails)

1. **Dry-run by default.** `orders place` without `--confirm` builds
   the order body, runs `whatif`, prints the ticket (account, PAPER/
   LIVE tag, conid-resolved contract, margin + commission estimate)
   and **sends nothing**.
2. **Preview before place.** A real placement requires the whatif
   output from the same invocation; there is no flag to skip it.
   (CPAPI has no server-side previewId as E*TRADE does, so the rail
   is procedural: whatif → display → explicit `--confirm`.)
3. **The Q/A loop is surfaced, never silently auto-answered.**
   CPAPI order submission can return *questions* (price-cap,
   size, and other confirmations) that must be answered via
   `/iserver/reply/{id}` before the order is live. Policy:
   - The CLI prints every question verbatim and stops (exit 5).
   - `orders reply <id> --confirm` sends an affirmative answer to
     exactly that one pending question. Batch/auto answering is
     not implemented in v1 — IBind automates this; we deliberately
     do not, because an agent must see each question.
   - A placement is not "done" until the reply chain terminates in
     an order id; the CLI reports the final state it observed and
     never assumes.
4. **No order is ever placed by any other command**, and read
   commands have no side effects beyond session tickle.
5. Every mutating call logs (locally, 0600 audit log): timestamp,
   account, request body, response, reply chain. Secrets excluded.

## 6. Options chain assembly (the CPAPI way)

There is no chain endpoint. `market chain <symbol> --expiry <YYYYMMDD>`
assembles one:

1. Resolve the underlier conid (`/trsrv/stocks` or secdef search).
2. GET `/iserver/secdef/strikes?conid=<underlier>&sectype=OPT&month=<MONYY>`
   → strike list for the month containing the expiry.
3. Filter strikes (default: ±15% of last price; `--all` overrides),
   resolve each option contract's conid via `/iserver/secdef/info`
   (batched where the API allows; respect the 200-conid/request
   ceiling on secdef batch endpoints).
4. Snapshot all option conids + the underlier in pages
   (`/iserver/marketdata/snapshot`, 10 req/s limiter), requesting
   the standard field set: last (31), bid (84), ask (86), bid/ask
   sizes, volume (87), open interest (7632*), IV and Greeks
   (model fields — *field codes for IV/Delta/Gamma/Theta/Vega are
   taken from the docs' field table at build time, not from memory*).
5. Render: strike × (call | put) table with bid/ask/mid/IV/Δ;
   `--json` emits the full assembled chain. Mark stale legs (no
   update timestamp) rather than dropping them silently.

Expected performance: a ±15% band on a liquid name ≈ 30–60 contracts
→ a few seconds. Full-surface scans are out of scope by design (§1).

## 7. Known quirks the implementation must absorb

1. **Snapshot priming.** CPAPI market-data snapshots are
   subscription-primed: the first request for a conid set often
   returns empty/partial fields; the client must re-request after a
   short delay (IBind and every other client do this). `market quote`
   implements prime → wait → fetch internally and reports which
   conids stayed empty.
2. **Conid-centric everything.** Symbols are display sugar; all data
   and order endpoints key on conid. The contracts package caches
   symbol→conid resolutions on disk (with the secdef payload) so
   repeated commands don't re-resolve; cache entries carry their
   fetch date and are refreshed on a miss or `--refresh`.
3. **Session competition.** See §3 — one brokerage session per
   username. In gateway mode, IBeam owns re-login; the CLI's job is
   to fail loudly (exit 3), never to fight for the session.
4. **Gateway TLS.** The CP Gateway serves a self-signed cert on
   localhost. TLS verification is disabled **only** when the base
   URL host is loopback; any non-loopback base URL requires a real
   cert or an explicit `--insecure-remote` that v1 does not ship.
5. **History limits.** `/iserver/marketdata/history` caps concurrent
   requests (5) and available period/bar combinations vary by
   instrument; the client surfaces the API's own error text.
6. **Order questions are normal flow**, not errors (§5.1.3) — but
   some question types indicate the order as built is wrong (e.g.
   unresolved contract ambiguity). The CLI never guesses an answer
   to a question it cannot quote verbatim to the user.
7. **IBeam credential posture.** In gateway mode the IBKR password
   lives with IBeam (env/secret store), by IBeam's own documented
   trade-off ("not so secure"). The CLI neither reads nor stores it.
   Mode O exists precisely to retire this posture later.

## 8. Testing

Sibling standard (etrade-cli: 54 tests, stub E2E smoke, CI green
before any live call):

- **Unit:** endpoint request construction, field-set assembly,
  rate-limiter pacing, error typing, config 0600 enforcement.
  OAuth mode: signing verified against published test vectors where
  available; the DH/LST derivation tested against vectors captured
  from IBind's test suite (with attribution).
- **Stub E2E:** a local fake CPAPI server (recorded fixtures from
  the official docs' example payloads) drives every leaf command,
  including: snapshot priming (empty → filled), an order placement
  that raises two Q/A rounds before returning an order id, session
  expiry mid-chain (exit 3), and rate-limit backoff.
- **No live calls in CI, ever.** Live verification is a manual,
  paper-account step at wiring time, separately approved.
- `gofmt` + `go vet` clean; GitHub Actions CI on push (sibling
  pattern).

## 9. Build phases

| Phase | Contents | Acceptance |
|---|---|---|
| P0 | Repo scaffold (public, `rayli09/ibkr-cli`), config, `auth` in gateway mode, `accounts list` | stub E2E green; CI green |
| P1 | Accounts reads (summary/positions/ledger/pnl) + market search/stocks/info | fixtures cover all five account reads |
| P2 | Quote (with priming), history, strikes, chain assembly | chain fixture test reproduces a recorded chain end-to-end |
| P3 | Orders: preview (whatif), place + Q/A loop, modify, cancel, trades — dry-run rails per §5.1 | stub E2E includes the two-question placement; no code path places without whatif + `--confirm` |
| P4 | OAuth 1.0a mode (after §3 build-time verifications) | signing vectors pass; gateway mode untouched |
| P5 | Live smoke on a paper account (only when Ray provides one) | read-only commands verified live; one paper order round-trip, reported with receipts |

WebSocket streaming, scanner commands (`/iserver/scanner/*`), and
FYI/notifications are **backlog**, not phases.

> **2026-10-24 update:** that backlog was cleared by the parity
> extension (Ray's directive, same day): scanner commands, bounded
> WebSocket streaming (including the unsolicited ntf/blt topics via
> `stream notifications`), and the rest of the IBind surface shipped
> as phases P6 (REST parity) and P7 (WS parity), verified against
> stub fixtures; live verification still waits on P5's account.

## 10. Open items (Ray / wiring time)

1. **IBKR account**: none on file. P0–P4 proceed without one
   (docs + stubs, the etrade-cli precedent). P5 needs a paper or
   live account Ray controls.
2. **OAuth consumer registration** (mode O prerequisite): issued via
   IBKR's API portal under Ray's account; the registration flow and
   endpoint coverage are build-time verifications in P4, and any
   credential handoff goes through secure entry, never chat.
3. **IBeam deployment choice** (mode G): Docker on this VM vs on
   Ray's Mac — affects where the CLI's gateway URL points; decide at
   P5 wiring, not now.
4. **Name/repo**: `ibkr-cli`, public, sibling of schwab-cli /
   etrade-cli — assumed; flag if Ray wants it private.

---

*Drafted 2026-10-24. Sources: IBKR Web API documentation
(ibkrcampus.com — sessions, per-endpoint rate limits, endpoint
reference), IBeam README/wiki (Voyz/ibeam), IBind docs/PyPI
(Voyz/ibind — CPAPI 1.0 client incl. OAuth 1.0a headless auth and
automated Q/A handling), sibling repos schwab-cli / etrade-cli for
all conventions. Items marked "verify at build time" are deliberate:
they are facts this spec refuses to assert from memory.*
