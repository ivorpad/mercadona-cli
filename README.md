# mercadona

Unofficial, agent-friendly CLI for `tienda.mercadona.es` — search the catalog, read
prices, build a cart, and check out. Single static Go binary, no runtime
deps, structured `--json` output for programmatic/agent use.

> Unofficial. Mercadona has no public API. Bring your own credentials; use at a
> sane request rate. This talks to the same HTTP endpoints the website does.

## Install

**npm** — downloads the prebuilt binary for your platform on install:

```bash
npm install -g @ivorpad/mercadona      # puts `mercadona` on your PATH
npx @ivorpad/mercadona search queso    # …or run without installing
```

**curl** (macOS / Linux):

```bash
curl -fsSL https://raw.githubusercontent.com/ivorpad/mercadona-cli/main/install.sh | sh
```

Override with `MERCADONA_VERSION=v0.1.0` (pin a tag) or `MERCADONA_INSTALL_DIR=/path`
(install location; defaults to `/usr/local/bin`, else `~/.local/bin`).

**Manual** — download a tarball for your OS/arch from the
[releases page](https://github.com/ivorpad/mercadona-cli/releases), extract, and put
`mercadona` on your PATH.

**From source** (Go 1.26+) — clone, then:

```bash
go build -o mercadona .
```

(`go install <module>@latest` isn't wired up yet: the module path is
`github.com/ivorjpc/mercadona`, which doesn't match the repo URL.)

## Commands

### Read core (anonymous — no login)

```bash
mercadona search queso                      # full-text product search
mercadona search --limit 5 --json mayonesa  # structured output for agents
mercadona batch -f lista.txt                 # many terms in ONE request (≈100 items / call)
printf 'queso\ncarne\nmayonesa\n' | mercadona batch -f -
mercadona product 13406                      # detail + price
mercadona categories                         # category tree
mercadona categories --id 112 --json         # one category's products (raw JSON)
```

Common flags: `--wh mad1` (warehouse), `--lang es`, `--json`.
Data goes to **stdout**, logs/errors to **stderr**, exit code `1` on error — friendly to scripts and agents.

Example:

```
$ mercadona batch -f lista.txt
• queso            → [51110] Queso rallado mozzarella pizza-Roma Hacendado — 1.60€ (8.000€/kg)
• carne            → [34157] Carne de pimiento choricero Hacendado — 1.55€ (11.072€/kg)
• mayonesa         → [13406] Mayonesa Hacendado — 1.20€ (2.400€/L)
```

### Authenticated: `import-har` (preferred) / `import-curl` / `login`, `whoami`, `cart`, `checkout`

The API authenticates with a **Bearer token** (a SimpleJWT). The first sign-in must happen in a
browser (password login needs a Google reCAPTCHA Enterprise token; Google-account users have no
password at all). After that, the **refresh token renews the session headlessly, forever** —
`POST /api/auth/tokens/ {refresh_token}` needs no captcha and rotates the token. Verified.

**Two login methods, one outcome.** However you sign in, the response carries the same durable
`refresh_token`, so the CLI automates identically:

| Method | Endpoint | Request body | Response |
|---|---|---|---|
| Email + password | `POST /api/auth/tokens/` | `{username, password, recaptcha_token}` | `{access_token, customer_id, refresh_token}` |
| Google sign-in | `POST /api/auth/social/google/` | `{id_token, postal_code}` | `{access_token, customer_uuid, refresh_token}` |

**✅ Preferred login method — `import-har`.** One browser login (email *or* Google), then
headless forever. Export a HAR after signing in and let the CLI pull the refresh token out for you:

```bash
# DevTools → Network → ⤓ "Export HAR…"  (after you've logged in, by either method), then:
mercadona import-har --file tienda.mercadona.es.har
mercadona whoami     # confirms it's authenticated
```

`import-har` seeds `refresh_token` into `~/.mercadona/config.toml` (0600) and caches the current
access token + cookie. From then on every `401 token_not_valid` triggers an automatic refresh +
retry — no browser, no captcha, unattended. (It reads only auth *responses* and Bearer/Cookie
*headers*; the password in the request body is never touched.)

Prefer to do it by hand? Write the token yourself — `mercadona set-refresh <token>` (or edit
`~/.mercadona/config.toml`):

```toml
[auth]
refresh_token = "<your refresh token>"   # the durable, headless-renewable credential
[defaults]
warehouse = "mad1"
```

`MERCADONA_TOKEN`/`MERCADONA_COOKIE`/`MERCADONA_CUSTOMER` env vars also work for one-off runs.

**Quick one-off (no refresh):** `mercadona import-curl --file s.txt` from a DevTools "Copy as
cURL" of any `…/api/customers/…` request extracts the Bearer token + cookie + customer id. It has
no refresh token, so it can't auto-renew — re-import when the access token expires.

> `mercadona login --user … --password …` exists but will fail without a `recaptcha_token`
> (browser-only), and does nothing for Google accounts; prefer the HAR/refresh-token flow above.

The customer id is read automatically from the token's `customer_uuid` claim, so
you never pass it (the literal `me` alias is rejected with 403). Token/cookie/
customer can also come from `MERCADONA_TOKEN` / `MERCADONA_COOKIE` /
`MERCADONA_CUSTOMER`. Secrets are read from env/files, never taken as flags.

#### Test the checkout flow with your session

```bash
mercadona import-curl --file session.txt   # from DevTools "Copy as cURL"
mercadona whoami                           # → "ok — customer id=…"  (confirms auth)
mercadona cart get --json                  # inspect current cart
mercadona cart add 51110 2                 # add 2× a product
mercadona checkout create --json           # open checkout → returns id + delivery slots
mercadona checkout set-delivery --checkout <id> --address <id> --slot <id>
mercadona checkout submit   --checkout <id> --yes   # IRREVERSIBLE — places the order
```

The access token (a SimpleJWT) lasts ~6 weeks; when `whoami` starts returning
`401 token_not_valid`, re-import a fresh `Copy as cURL` (or use `login`).

### Spending guard (agent safety)

When an agent drives the CLI, cap how much it can ever spend. Any cart/checkout over the cap is
refused with a non-zero exit and an `error:` line — so the agent stops instead of running up a huge
order. Pass it as a flag (which can go anywhere on the line):

```bash
mercadona cart add 10379 99 --max 50                       # → error: BUDGET EXCEEDED … refusing (exit 1)
mercadona checkout submit --checkout <id> --max 80 --yes   # submits only if total ≤ 80 €
```

Or set it once so every command is capped — `MERCADONA_MAX_EUR=100` (env), or in config:

```toml
# ~/.mercadona/config.toml
[limits]
max_eur = 100        # refuse any cart/checkout whose total exceeds 100 €
```

Precedence is **flag > env > config**; `0`/unset = no limit. Enforced on `cart add/set`,
`checkout create`, `checkout set-delivery`, and — critically — `checkout submit`, which **fails
closed**: with a cap set, if it can't read the order total it refuses rather than spend blind.
(With no cap, `submit` prints a warning.)

## Design / reliability

Three layers, by IP-sensitivity:

1. **Search → Algolia.** Not behind Mercadona's Akamai at all; works from any IP.
   The public app-id **rotates** (`7UZJKL1GNI` → `7UZJKL1DJ0` …), so the CLI never
   relies on a hardcoded value: it ships a last-known-good fallback and, on a stale-creds
   signal (DNS failure / 401 / 403 / 404), **re-discovers the app-id, key and index from
   the live SPA bundle**, caches them, and retries. Survives rotation automatically.
2. **Catalog reads (`/api/...`)** — Akamai-fronted but served to anonymous GETs at
   human-paced volume. Sends web-app-like headers to stay in monitor mode.
3. **Auth + cart + checkout** — the only IP-sensitive part. Run from a **residential IP**
   (local, or a box on your own network — not a flagged datacenter / serverless egress),
   log in once and cache the token. A real browser is only needed as a *fallback* to mint
   Akamai clearance if a challenge ever appears.

## Config

State lives under the OS config dir (`~/Library/Application Support/mercadona` on macOS,
`~/.config/mercadona` elsewhere). Override with `MERCADONA_CONFIG_DIR`.

- `algolia.json` — cached/refreshed search credentials.
- `token.json` — cached bearer token (added with the auth commands).

## Claude skill

This repo bundles a Claude Code skill, **`mercadona-shop`** (`.claude/skills/mercadona-shop/`),
that drives this CLI to do the grocery shop: turn a list into priced products, fill the cart,
prepare delivery checkout, and place the order only on explicit user consent. Install it where
your Claude reads skills (symlink or copy `.claude/skills/mercadona-shop` into `~/.claude/skills/`);
it points back at this binary, so build the CLI first.

## Status

Read core (`search`, `batch`, `product`, `categories`) and the full authenticated leg
(`import-har`/`import-curl`/`set-refresh`, `whoami`, `cart`, `checkout`) are implemented and
verified live: reads, Algolia self-refresh, the uTLS fingerprint, headless token refresh, and a
real-session `cart get` → `checkout create` → `set-delivery` → `checkout get` all work, and the
order total the spending guard reads is confirmed against a live checkout. `checkout submit` is
gated behind both `--yes` and the `--max` budget cap; it has not been run end-to-end (no real
order has been placed).

## Releasing

Push a semver tag — GitHub Actions
([`.github/workflows/release.yml`](.github/workflows/release.yml)) cross-compiles with
[GoReleaser](https://goreleaser.com), publishes a GitHub Release (per-OS/arch archives +
`checksums.txt`), then publishes the npm wrapper that downloads from it.

```bash
git tag v0.1.0 && git push origin v0.1.0
```

**One-time setup** — add an Actions secret (repo → Settings → Secrets and variables → Actions):

- `NPM_TOKEN` — an npm automation token with publish rights to the `@ivorpad` scope.

Dry-run the build locally (no publish; artifacts land in `./dist`):

```bash
goreleaser release --snapshot --clean --skip=publish
```

**Homebrew** is prewired but disabled — to turn on `brew install ivorpad/tap/mercadona`,
follow the commented `brews:` block in [`.goreleaser.yaml`](.goreleaser.yaml) (needs a
`homebrew-tap` repo + a `HOMEBREW_TAP_GITHUB_TOKEN` secret).
