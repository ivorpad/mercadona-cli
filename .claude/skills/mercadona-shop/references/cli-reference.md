# `mercadona` CLI reference

Exact command surface and output shapes. Data goes to **stdout**, logs/errors to **stderr**,
exit code `1` on error — safe to pipe and parse. Add `--json` to any command for machine
output. Common flags may appear anywhere after the (sub)command: `--wh mad1`, `--lang es`, `--json`.

The client **auto-retries throttling** (HTTP 429/503) with backoff, honouring `Retry-After` — an
occasional rate limit surfaces as a short pause, not an error. `set-many`'s price-fetch burst is
capped at 4 parallel GETs (`MERCADONA_CONCURRENCY=1..16` to tune).

## Read commands (anonymous — no login)

| Command | Purpose |
| --- | --- |
| `mercadona search <term...> [--limit N] [--category <id\|name>] [--fresh]` | full-text product search (Algolia) |
| `mercadona batch [-f file] [--hits N] [--category <id\|name>] [--fresh]` | search many terms in ONE request (~100/call) |
| `mercadona total [-f file]` | deterministic basket total from `<id> [qty]` lines (CLI sums it) |
| `mercadona product <id>` | product detail + price + nutrition table (when the product has one) |
| `mercadona categories [--id N]` | category tree, or one category's products |

`batch` reads one term per line from `-f <file>` (`-f -` = stdin) or positional args. Lines
starting with `#` are skipped. Default returns the top hit per term (`--hits` for more).

**`--category` / `--fresh` (both `search` and `batch`)** add Algolia category facet filters:

- `--category <id|name>` restricts results to a category. A numeric id is used directly; a name is
  resolved (case-insensitively) against the `categories` tree — an ambiguous name errors and lists
  the candidates, so pass a numeric id to disambiguate.
- `--fresh` excludes the *Congelados* (frozen, id 17) **and** *Conservas, caldos y cremas* (canned,
  id 14) top-level categories, so a bare "gambas"/"mejillón"/"guisantes" surfaces the fresh item
  instead of the frozen/canned one. It's a heuristic: if no fresh variant exists, excluding those
  categories can let Algolia typo-match unrelated items — eyeball the result. Combine the two for
  fresh-within-a-category. Implemented as `facetFilters` (`categories.id:N`, `categories.id:-N`).

`total` reads `<id> [qty]` per line (`-f <file>`/`-f -` = stdin; inline or whole-line `#` comments —
so `5044 1  # Arroz redondo` is valid and self-documenting) — or bare
ids as positional args (qty 1 each). It fetches each product's price **in the configured/`--wh`
warehouse** and sums `unit_price × qty` in integer cents, so the pre-cart estimate is exact and
reproducible instead of hand-added; qty may be fractional for weight/bulk items. A line whose id
can't be priced is reported and excluded, and the command exits non-zero. `--json` →
`{lines:[{id,name,qty,unit_price,subtotal}], total, count, complete}`. The authoritative total is
still the cart/checkout API (`ExtractTotal`); `total` is the no-login estimate before Gate 1.

### Search hit shape (`--json`)

```json
{
  "query": "leche entera",
  "nbHits": 15,
  "hits": [
    {
      "id": "10379",
      "display_name": "Leche entera Hacendado",
      "packaging": "Pack-6",
      "share_url": "https://tienda.mercadona.es/product/10379/...",
      "categories": [{ "name": "Huevos, leche y mantequilla" }],
      "price_instructions": {
        "unit_price": "5.76",
        "reference_price": "0.960",
        "reference_format": "L",
        "is_pack": true
      }
    }
  ]
}
```

`id` is the product id you pass to `cart add`/`set`. `unit_price` is the price you pay for the
item as sold; `reference_price`/`reference_format` is the per-unit comparison (e.g. 0.960 €/L).
Ids are **per-warehouse** — an id from a different `--wh` may 404.

## Location (warehouse selection)

Product ids **and prices** are per-warehouse, and online checkout needs the cart's warehouse to
match the delivery address — so the warehouse must be the one that serves the user's postal code.

| Command | Purpose |
| --- | --- |
| `mercadona set-postal <cp>` | resolve `cp` → warehouse (via `POST /api/postal-codes/actions/change-pc/`, reading the `x-customer-wh` header) and save both `postal_code` + `warehouse` to `[defaults]`; **no login needed** |

Active warehouse/lang precedence: explicit `--wh`/`--lang` flag > `config.toml [defaults]` >
built-in `mad1`/`es`. `import-har` also auto-detects the warehouse (and lang) from the captured
session and saves it to `[defaults]`. An undeliverable postal code errors (no warehouse returned)
and leaves config untouched. Example: `28022 → mad1`, `28013 → mad3` (same city, different centre).

## Auth commands (bring-your-own credentials)

| Command | Purpose |
| --- | --- |
| `mercadona import-har [--file f]` | **preferred** — extract the refresh token from a browser HAR → headless forever |
| `mercadona set-refresh <token>` (or `--stdin`) | seed a refresh token → headless auto-refresh forever |
| `mercadona import-curl [--file f]` | import a browser session (access token + cookie); **no** refresh |
| `mercadona login [--user E] [--password-stdin] [--save]` | password login — **needs a browser reCAPTCHA**, fails headless |
| `mercadona whoami` | verify the session (prints customer id) |

Password login requires a Google reCAPTCHA Enterprise token, so it can't be done headlessly —
the first login is a browser step. The **refresh token** then renews the session with no
captcha (it rotates on each use), which is the durable, unattended credential.

**import-har** is the easiest durable setup: export a HAR (DevTools → Network → Export HAR…) after
logging in by **either** method, and it extracts the `refresh_token` (+ access token, cookie, customer
id) into `~/.mercadona/config.toml`, never reading the password. Email login posts to
`/api/auth/tokens/`; "Sign in with Google" posts to `/api/auth/social/google/` — both return a refresh
token, and import-har handles either.

**set-refresh** writes `[auth] refresh_token` to `~/.mercadona/config.toml` (0600). Get the
token from one browser login (DevTools → the `POST /api/auth/tokens/` response, or local
storage). Use `--stdin` to keep it out of argv. After seeding, any authed call auto-refreshes
on a `401 token_not_valid` and retries.

**import-curl** extracts the Bearer access token, cookie, and customer id from a DevTools
"Copy as cURL" (`--file`, or stdin); prints only lengths + the customer id, never the secrets.
It has **no** refresh token, so it can't auto-renew — the access token lasts ~6 weeks.

**login** posts `{username, password}` but the API also wants a `recaptcha_token` it can't
generate, so headless login fails (412/400). Present only as a browser-assisted fallback;
prefer set-refresh / import-curl.

Auth precedence: flags > env (`MERCADONA_TOKEN`/`COOKIE`/`CUSTOMER`/`USER`/`PASS`) >
`~/.mercadona/config.toml` > cached session (`token.json`). The customer id is decoded from
the token's `customer_uuid` claim — the literal `me` alias is rejected (403).

## Cart commands (auth required)

| Command | Purpose |
| --- | --- |
| `mercadona cart get` | show current cart (names, `qty × unit_price = subtotal`, total; `--json` = raw) |
| `mercadona cart set-many [-f file] [--max EUR]` | apply many `<id> <qty>` lines in ONE write (`0` removes) |
| `mercadona cart clear [--max EUR]` | empty the cart in one write |
| `mercadona cart add <product_id> <qty> [--max EUR]` | add qty to a product's existing quantity |
| `mercadona cart set <product_id> <qty> [--max EUR]` | set absolute qty (`0` removes the line) |

`qty` is a float — unit items use `1`, `2`…; weight/bulk items accept fractions (`0.5`).

**`set-many`** is the fast path for building/swapping a basket: it does **one** `GetCart`, folds
every `<id> <qty>` change onto the current lines (absolute set; `0` removes), and issues a **single**
`PutCart`. With a cap set it **prices the resulting basket first** (reusing prices already in the
cart, fetching only new ids concurrently) and **refuses before writing** if the estimate exceeds
`--max`; a final authoritative check reverts the write if the real total still breaches the cap.
Input is `<id> <qty>` per line via `-f <file>`/`-f -` (inline or whole-line `#` comments OK — annotate
each id with its product name), or positional
`<id> <qty> <id> <qty>…` pairs. This replaces firing many serial `add`/`set` writes.

**`clear`** does one `GetCart` + `PutCart` with empty lines; prints how many products were removed
(`✓ el carrito ya está vacío` if already empty).

By default `add`/`set`/`set-many` print a **concise** summary (the changed line(s) + new cart count
and total, plus a "faltan X€…" hint under the 60€ minimum); pass `--json` for the raw PUT response.

### Cart shape (`cart get --json`)

Read (GET) nests the product under each line; the CLI also lifts `display_name` + price into the
projected line for the human view (the PUT shape it writes back stays a flat `product_id`):

```json
{
  "id": "<cart-id>",
  "version": 7,
  "products_count": 3,
  "open_order_id": null,
  "lines": [
    {
      "quantity": 2.0,
      "sources": [],
      "product": {
        "id": "10379",
        "display_name": "Leche entera Hacendado",
        "price_instructions": { "unit_price": "5.76", "reference_price": "0.960", "reference_format": "L" }
      }
    }
  ],
  "summary": { "total": "12.34" }
}
```

`quantity` is a float (the API returns `1.0`). With `--json`, `cart get` echoes this raw GET and
`add`/`set`/`set-many` echo the raw PUT response (same cart shape, carrying `summary.total`).

## Checkout commands (auth required)

| Command | Purpose |
| --- | --- |
| `mercadona checkout addresses` | list saved delivery addresses |
| `mercadona checkout create [--max EUR]` | open a checkout from the cart → id + default address |
| `mercadona checkout slots --address <id>` | delivery slots for an address |
| `mercadona checkout set-delivery --checkout <id> --address <id> --slot <id> [--max EUR]` | attach address + slot |
| `mercadona checkout submit --checkout <id> [--max EUR] --yes` | **place the order (irreversible)** |

### Flow + shapes

1. `checkout create --json` → `{ "id": "<chk>", "address": { "id": <addr>, ... }, ... }`.
   The default address is embedded; **slots are not** — they hang off the address.
2. `checkout slots --address <addr> --json` →
   `{ "next_page": ..., "results": [ { "id": "<slot>", "start": "...", "end": "...",
   "price": "...", "available": true, "open": true } ] }`.
   Slot `id` is a **string**; address `id` is an **int**.
3. `checkout set-delivery --checkout <chk> --address <addr> --slot <slot>` → reserves the slot;
   the response carries the final price breakdown (subtotal + delivery fee → total).
4. `checkout submit --checkout <chk> --yes` → places the order. Without `--yes` the CLI refuses.
   The **minimum order is 60€** (enforced live; bundle's "50" is wrong); delivery ≈ 8.20€. When the
   basket is under 60€, `checkout create` prints a "faltan X€…" warning to **stderr** (stdout stays
   clean JSON).

## Spending guard

`--max <eur>` caps the spend: any `cart add/set/set-many/clear` or `checkout
create/set-delivery/submit` whose total exceeds it fails with `error: BUDGET EXCEEDED …` and exit 1.

- **`set-many` (and `add`/`set`) is preventive**: it prices the resulting basket *before* the PUT and
  refuses without writing when the estimate is over the cap; a post-write authoritative check reverts
  the cart if the real total still breaches it. So a cap breach leaves the cart unchanged.
- **`checkout submit` fails closed** — with a cap set, if the order total can't be read it refuses
  rather than pay.
- **The cart `--max` bounds the product subtotal; the `checkout` `--max` must also cover the delivery
  fee (≈8.20€).** Size them accordingly, and leave a little headroom over a round target (discrete
  prices: a "≈60€" basket can land at 60.10€).

Also settable as `MERCADONA_MAX_EUR` or `[limits] max_eur` in config; precedence flag > env > config;
`0`/unset = off. Minimum order ≈60€ is a separate *floor* (not a `--max`); the CLI prints the shortfall
("faltan X€…") in `cart get` and `checkout create`.

## Config & state

State lives in `~/.mercadona/` (override with `MERCADONA_CONFIG_DIR`):

- `config.toml` — `[auth]` username/password/refresh_token, `[defaults]` warehouse/lang/postal_code,
  `[limits] max_eur` (spending cap) (0600). `[defaults]` is honoured by every command — set it with
  `set-postal` (or `import-har`'s auto-detect); flag `--wh`/`--lang` overrides it per command.
  Only `refresh_token` actually yields a headless session; stored `username`/`password` can't
  auto-login (the login call omits the reCAPTCHA token the API requires → 412/400).
- `token.json` — cached bearer + refresh + cookie (machine-managed).
- `algolia.json` — cached/auto-refreshed search creds.
