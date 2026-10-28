# deploy/ibeam — IBeam gateway stack for ibkr-cli (Mode G)

Mode G means a local Client Portal Gateway owns the IBKR session and
ibkr-cli just talks to it. The maintained way to run that gateway is
[IBeam](https://github.com/Voyz/ibeam) (Docker): it injects the
credentials, answers 2FA, and keeps the session alive with tickles.
The CLI never sees the IBKR password.

Everything here is **paper-first**: `env.list` sets
`IBEAM_USE_PAPER_ACCOUNT=true`. Going live means deliberately
flipping that one value.

Most of this runbook is automated by the CLI — step order, fixes, and
the final verdict:

```sh
ibkr-cli setup --up --wait 180s --json
```

`setup` never prompts, never prints credential values, and exits 0
only when the gateway is authenticated with a live brokerage session
(see the main README's "Agent quickstart" for its contract). The
manual steps below are what it checks, for when you want to drive by
hand.

## Step 0 — install Docker

Docker is a hard prerequisite. If `ibkr-cli setup` reports stage
`no-docker`, install Docker Engine / Docker Desktop for your platform
first; the scaffold intentionally does not try to install it.

## Step 1 — credentials file

```sh
cd deploy/ibeam
cp env.list.example env.list
chmod 600 env.list
$EDITOR env.list   # set IBEAM_ACCOUNT and IBEAM_PASSWORD
```

`env.list` is git-ignored and must stay mode 0600 — it holds the
account password in the clear, which is IBeam's documented trade-off
(SPEC §7.7). Compose reads it via `env_file:`; ibkr-cli itself never
opens it for the password (setup only reports which keys are set).

## Step 2 — start the stack

```sh
docker compose up -d
docker compose ps
docker compose logs -f ibeam   # watch the login
```

The compose file uses `restart: "no"` on purpose: a crash-looping
login retries until it exceeds `IBEAM_MAX_FAILED_AUTH` (default 5)
and can lock the account. Start it deliberately, fix problems,
start it again.

## Step 3 — verify the gateway

The gateway serves a self-signed certificate on loopback; `-k` is
expected here and only here:

```sh
curl -k -X POST https://localhost:5000/v1/api/iserver/auth/status
```

(The endpoint is POST — the SPEC's original GET was corrected at
build time; see the errata in the main README.)

You want `"authenticated": true, "connected": true`. Or skip curl:

```sh
ibkr-cli auth status
```

## Two-factor authentication

IBKR login requires 2FA. Two unattended options, both configured in
`env.list` (commented templates are in `env.list.example`):

- **TOTP (recommended for agents):** `IBEAM_TWO_FA_HANDLER=PyotpHandler`
  with `IBEAM_PYOTP_SECRET=<authenticator seed>` — IBeam's pyotp
  handler computes the code itself.
- **External request:** `IBEAM_TWO_FA_HANDLER=ExternalRequestHandler`
  with `IBEAM_EXTERNAL_REQUEST_URL=<endpoint>` — IBeam calls your
  endpoint to fetch the current code (phone webhook, code relay…).

Custom handlers can be plugged in via
`IBEAM_CUSTOM_TWO_FA_HANDLER=module.Class`.

## Daily reality: re-login and the one-session rule

- IBKR expires gateway sessions roughly **daily**; expect a fresh
  IBeam login about once a day. IBeam re-logs-in on its own schedule,
  but a 2FA method that needs a human in the loop makes that a daily
  human task — another reason to prefer TOTP here.
- **One brokerage session per username, across all platforms.** While
  this stack holds the session, logging into TWS / the website /
  mobile under the same username competes with it — sessions kick
  each other. Prefer a dedicated username (or a paper DU username)
  for the agent stack.

## Stopping and inspecting

```sh
docker compose down        # stop the stack (or: ibkr-cli setup --down)
docker compose logs ibeam  # recent login/session logs
docker compose ps          # container state
```

## Troubleshooting by setup stage

| `setup --json` stage | Meaning | Fix |
|---|---|---|
| `no-docker` | docker CLI not on PATH | Step 0 above |
| `bad-dir` | scaffold dir / compose.yaml not found | run from the repo, or pass `--dir` |
| `no-env` | env.list missing, loose, or required keys unset | Step 1 above |
| `stack-down` | daemon unreachable or container not running | `docker compose up -d` / `setup --up` |
| `gateway-unreachable` | container up, nothing answering on the gateway port | check `docker compose logs ibeam` |
| `session-not-authenticated` | gateway answers, no live session | login failed/pending 2FA — check IBeam logs and the 2FA config |
| `paper-ready` | authenticated + brokerage session live, paper mode | smoke: `auth status`, `accounts list`, `market quote`, `orders list` |
