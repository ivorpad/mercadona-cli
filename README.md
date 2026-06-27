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
go build -o mercadona ./cmd/mercadona
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

Common flags: `--wh mad1` (warehouse), `--lang es`, `--json` — and they can go anywhere after the (sub)command, not just up front.
Data goes to **stdout**, logs/errors to **stderr**, exit code `1` on error — friendly to scripts and agents.

### Location (warehouse) — set it from your postal code

```bash
mercadona set-postal 28022   # → resolves to warehouse mad1, saves it as the default
```

Product ids **and prices** are per-warehouse, and online checkout needs the cart's warehouse to
match your delivery address — so pin it to the warehouse that serves your postal code (no login
needed). Precedence: `--wh` flag > `config.toml [defaults]` > built-in `mad1`. `import-har` also
auto-detects and saves the warehouse from your session. (Within a city it varies: `28022 → mad1`,
`28013 → mad3`.)

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
warehouse = "mad1"        # or: `mercadona set-postal 28022` resolves + writes this for you
postal_code = "28022"
```

`MERCADONA_TOKEN`/`MERCADONA_COOKIE`/`MERCADONA_CUSTOMER` (and `MERCADONA_USER`/`MERCADONA_PASS`) env vars also work for one-off runs.

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
mercadona import-har --file tienda.mercadona.es.har   # auth (preferred; or import-curl)
mercadona whoami                           # → "ok — customer id=…"  (confirms auth)
mercadona cart get --json                  # inspect current cart (names, qty × unit_price, total)
mercadona cart add 51110 2 --max 80        # add 2× a product (capped at 80 €)
printf '51110 2\n13406 1\n' | mercadona cart set-many -f - --max 80   # many '<id> <qty>' in ONE write (0 removes)
mercadona cart clear                       # empty the cart in one write
mercadona checkout create --json           # open a checkout → id + default address
mercadona checkout addresses               # list saved delivery addresses
mercadona checkout slots --address <id>    # delivery slots (they hang off the address, not the checkout)
mercadona checkout get --checkout <id>     # show a checkout: total, address, slot
mercadona checkout set-delivery --checkout <id> --address <id> --slot <id>
mercadona checkout submit --checkout <id> --max 80 --yes   # IRREVERSIBLE — places the order
```

`cart add` adds to the existing quantity; `cart set` sets the absolute quantity (`0` removes). For a
whole basket, **`cart set-many -f -`** applies many `<id> <qty>` lines in a *single* write — and prices
it first, so `--max` refuses *before* writing — while **`cart clear`** empties it. All accept `--max`.

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

Precedence is **flag > env > config**; `0`/unset = no limit. Enforced on `cart add/set/set-many`,
`checkout create`, `checkout set-delivery`, and — critically — `checkout submit`, which **fails
closed**: with a cap set, if it can't read the order total it refuses rather than spend blind.
(With no cap, `submit` prints a warning.)

## Recipes — real examples

> The interesting part isn't "an AI does your shopping." It's that one person can now do things that
> used to need a developer or an analyst: track your *own* inflation, rank a category by €/kg, catch
> genuine price drops, build an allergen-safe basket. Every output below is **live CLI** — and since
> reads need no login, most are copy-paste.

### Price a whole list (and get cost-per-serving) — no login

`total` fetches each id's price and sums `unit_price × qty` **in integer cents** — exact,
reproducible, and fractional quantities work for weight items. Divide by servings for €/plate.

```console
$ printf '5044 1\n60393 1\n85499 1\n16044 1\n4740 0.5\n' | mercadona total -f - --json
{
  "complete": true,
  "count": 5,
  "lines": [
    { "id": "5044",  "name": "Arroz redondo Hacendado",               "qty": 1,   "unit_price": "1.20", "subtotal": "1.20" },
    { "id": "60393", "name": "Gambón grande congelado",               "qty": 1,   "unit_price": "6.00", "subtotal": "6.00" },
    { "id": "85499", "name": "Mejillón mediterráneo",                 "qty": 1,   "unit_price": "5.80", "subtotal": "5.80" },
    { "id": "16044", "name": "Tomate triturado Hacendado",            "qty": 1,   "unit_price": "0.55", "subtotal": "0.55" },
    { "id": "4740",  "name": "Aceite de oliva virgen extra Hacendado","qty": 0.5, "unit_price": "4.95", "subtotal": "2.48" }
  ],
  "total": "16.03"
}
```

→ basket **16.03 €** for 5 lines; a paella base for 3 ≈ **5.34 €/serving**.

### Get the *fresh* item, not the frozen/canned one

A bare term often top-ranks the frozen or canned version. `--fresh` drops the Congelados + Conservas
aisles, so the fresh product surfaces:

```console
$ mercadona search mejillon --limit 1
  [18615] Mejillones de Chile en escabeche Hacendado pequeños — 2.65€   (Conservas, caldos y cremas)
$ mercadona search mejillon --fresh --limit 1
  [85499] Mejillón mediterráneo — 5.80€   (Marisco y pescado)
```

### Sort a whole category by price-per-kilo

`reference_price` is the unit-normalised price (€/kg, €/L) on every product. Pull a whole category and
rank by it to surface the genuine value buys:

```console
$ mercadona categories --id 118 --json   # 118 = Arroz
```
| id | product | price | per kilo |
|---|---|---|---|
| `5044` | Arroz redondo Hacendado | 1.20€ | 1.200 €/kg |
| `5063` | Arroz largo Hacendado | 1.20€ | 1.200 €/kg |
| `5020` | Arroz vaporizado Hacendado | 1.55€ | 1.550 €/kg |
| `5042` | Arroz redondo J Sendra Hacendado | 1.60€ | 1.600 €/kg |
| `5184` | Arroz integral largo Hacendado | 1.65€ | 1.650 €/kg |

### Find products actually on offer (the API flags it)

Each product carries `price_decreased` + `previous_unit_price`, so you can catch genuine drops — not
marketing. A scan of ~440 staples turned up dozens:

```console
$ mercadona categories --id 112 --json | jq '.. | objects | select(.price_decreased==true)'
```
| id | product | was | now | drop |
|---|---|---|---|---|
| `4717` | Aceite de oliva virgen extra Hacendado | 14.55€ | 14.40€ | -1% |
| `4706` | Aceite de oliva virgen extra Gran Selección | 5.95€ | 5.75€ | -3% |
| `4718` | Aceite de oliva virgen extra Hacendado | 2.70€ | 2.60€ | -4% |
| `5063` | Arroz largo Hacendado | 1.25€ | 1.20€ | -4% |
| `26029` | Garbanzo cocido Hacendado | 0.85€ | 0.80€ | -6% |
| `6305` | Pajaritas vegetales Hacendado | 1.00€ | 0.90€ | -10% |

### Read allergens & ingredients per product (diet-safe baskets)

```console
$ mercadona product 10379 --json | jq '{display_name, brand, ean, nutrition_information}'
{
  "display_name": "Leche entera Hacendado",
  "brand": "Hacendado",
  "ean": "8402001002076",
  "nutrition_information": {
    "allergens": "Contiene leche y sus derivados (incluida la lactosa).",
    "ingredients": "Leche entera de vaca"
  }
}
```
> The product detail also exposes `brand`, `ean`, `origin`, and a `details` block. Nutrition gives
> **allergens + ingredients** (great for coeliac/allergy filters) but **no numeric macros**.

### Discover regional specialties with `--wh`

Prices are uniform nationwide (see below), but the **catalog isn't** — each warehouse stocks local
products. Mallorca's sobrasada shelf vs Madrid's:

```console
$ mercadona search sobrasada --wh mad1 --json | jq .nbHits   # Madrid:   19
$ mercadona search sobrasada --wh 3842 --json | jq .nbHits   # Baleares: 28
```
**10 sobrasada products are in Baleares but not Madrid**, e.g. `[20869] Sobrasada de Mallorca Can Pere
Joan — 5.25€`, `[53114] Sobrasada cerdo negro de Mallorca — 14.84€`, con miel, picante…

### Verify a claim: are prices identical across regions?

Same product id, priced in five warehouses with `--wh`. To the cent, everywhere — islands included
(Mercadona's "Siempre Precios Bajos" is literal):

```console
$ for wh in mad1 bcn1 vlc1 svq1 3842; do mercadona product 5044 --wh $wh --json; done
```
| id | product | Madrid | Barcelona | Valencia | Sevilla | Baleares | |
|---|---|---|---|---|---|---|---|
| `5044` | Arroz redondo | 1.20€ | 1.20€ | 1.20€ | 1.20€ | 1.20€ | ✓ same |
| `4740` | AOVE Hacendado | 4.95€ | 4.95€ | 4.95€ | 4.95€ | 4.95€ | ✓ same |
| `10379` | Leche entera | 5.76€ | 5.76€ | 5.76€ | 5.76€ | 5.76€ | ✓ same |
| `60393` | Gambón | 6.00€ | 6.00€ | 6.00€ | 6.00€ | 6.00€ | ✓ same |
| `64000` | Helado bombón | 2.90€ | 2.90€ | 2.90€ | 2.90€ | 2.90€ | ✓ same |

### Compose the rest with an agent

The same primitives back richer, agent-driven flows — the [Claude skill](#claude-skill) drives them,
always capping spend with `--max` and never submitting without explicit consent:

- **Personal inflation tracker** — cron the `total --json` recipe on your real basket → CSV → chart your own CPI.
- **Reverse budgeter** — "feed 4 for a week on 50 €": batch-price candidates, optimise `reference_price` under `--max`.
- **Household cart by chat** — "añade leche" in WhatsApp/Slack → `cart set-many` updates a shared basket through the week.
- **Pantry-photo restock** — an agent maps a fridge photo to product *names* → `search` → `cart set-many`. (No barcode lookup — but `ean` is exposed, so you can build your own scan map.)
- **Smart-home / calendar triggers** — Home Assistant "milk low", or "dinner party Saturday for 8" → fills the cart and books a slot.
- **DIY Subscribe-&-Save** — a weekly cron rebuilds your staples with `cart set-many` and preps checkout; you just approve.
- **Voice-first shopping** — a complete weekly shop by conversation, no app UI to fight: the clearest case of augmenting, not replacing.

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

State lives in `~/.mercadona/` (override with `MERCADONA_CONFIG_DIR`):

- `config.toml` — user-authored (`0600`): `[auth] refresh_token` (+ optional `username`/`password`),
  `[defaults] warehouse`/`lang`/`postal_code` (honoured by every command; set via `set-postal`),
  `[limits] max_eur`.
- `token.json` — cached session: access + refresh token + cookie (machine-managed).
- `algolia.json` — cached/auto-refreshed search credentials.

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

The workflow is hardened: actions are pinned to commit SHAs, permissions are per-job
least-privilege, and **npm publishes via OIDC Trusted Publishing** (no long-lived token) with a
SLSA provenance attestation. One-time setup: configure a Trusted Publisher for `@ivorpad/mercadona`
on npmjs.com (Settings → Trusted Publisher → GitHub Actions: user `ivorpad`, repo `mercadona-cli`,
workflow `release.yml`).

Dry-run the build locally (no publish; artifacts land in `./dist`):

```bash
goreleaser release --snapshot --clean --skip=publish
```

**Homebrew** is prewired but disabled — to turn on `brew install ivorpad/tap/mercadona`,
follow the commented `brews:` block in [`.goreleaser.yaml`](.goreleaser.yaml) (needs a
`homebrew-tap` repo + a `HOMEBREW_TAP_GITHUB_TOKEN` secret).
