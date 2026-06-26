# Troubleshooting

Common failure modes when driving the `mercadona` CLI, and how to recover.

## Auth

**`not authenticated: …`** — no usable session. Seed a refresh token from one browser login:
`printf '%s' '<token>' | mercadona set-refresh --stdin` (token from DevTools → the `POST
/api/auth/tokens/` response, or local storage). For a quick one-off instead, `mercadona
import-curl --file session.txt`. Confirm with `whoami`. Note `mercadona login` won't work
headless — it needs a browser reCAPTCHA.

**`401 token_not_valid` / `whoami` fails after it used to work** — the access token expired
(~6-week life). With a seeded refresh token the CLI auto-refreshes and retries on its own; if
it still fails, the refresh token is also expired/rotated-away — do a fresh browser login and
re-seed it with `set-refresh`, or re-`import-curl`.

**`412` (or login just fails)** — reCAPTCHA Enterprise scored the request too low, typical from
datacenter/Cloud IPs. The first login must be a real *local* browser; don't try to headless it
or run it from Cloud Browser/serverless. Once a refresh token is seeded, this never matters again.

**"a cookie alone does not authenticate the API"** — the import had a cookie but no Bearer
token. The API authenticates with the token, not the cookie (the cookie is only Akamai
clearance). Re-copy an authenticated `…/api/customers/…` request that includes the
`authorization: Bearer …` header.

**`403`** on an authenticated call where the token looks valid — usually the `me` alias or a
customer-id mismatch; the CLI resolves the id from the token's `customer_uuid`, so re-import a
fresh session rather than passing `MERCADONA_CUSTOMER` by hand.

## Akamai / network

The auth + cart + checkout layer sits behind Akamai. At human-paced volume from a residential
IP it stays in monitor mode. If you see HTML challenge pages, `_abck=~-1~`, or sudden `403`s on
the authed layer:

- **Run from the user's residential IP**, not a datacenter/serverless egress (flagged IPs draw
  hard challenges). Reads/search are IP-independent; only auth/checkout are sensitive.
- Slow down — space out requests; don't hammer cart writes in a tight loop.
- As a last resort, mint fresh Akamai clearance by loading `tienda.mercadona.es` in a real
  browser on the *same IP*, then `import-curl` that session (token + fresh cookie).

**Rate limits (HTTP 429 / 503).** The client now **backs off and retries automatically** (honouring
`Retry-After`, a few attempts), so an occasional throttle just shows up as a short pause, not a
failure — let it ride. To push gentler in the first place:

- Prefer **one `batch`** call (≈100 terms in a single request) over many individual `search` calls.
- `set-many` prices a *fresh* basket by fetching each new product; it caps that burst at 4 parallel
  GETs. If you're getting throttled, lower it with `MERCADONA_CONCURRENCY=1` (or `2`), or `cart
  clear` + build in a couple of smaller `set-many` batches.
- If a command still fails after backoff, wait a bit and retry — don't loop on it tightly (that
  makes Akamai escalate). Persistent 403/challenge pages are an IP/clearance problem, not a rate
  limit — see above.

Search runs against Algolia (not Akamai) and works from anywhere. If search starts failing
with DNS/`NXDOMAIN` or `401/403/404`, the Algolia app-id rotated — the CLI auto-rediscovers it
from the live SPA bundle and retries, so just run the command again.

## Product matching

**"no results" / wrong product** — Algolia relevance on a bare word is loose, and `batch`
only returns the top hit. Re-query with `mercadona search "<more specific term>" --limit 5`,
add brand/format words ("Hacendado", "1L", "sin lactosa"), or show the user options to choose
from. Don't trust `hit[0]` for anything the user was specific about.

**Right product, wrong price/availability, or a 404 on a known id** — you're in the wrong
warehouse. Ids and prices are per-warehouse; pass the user's `--wh` (e.g. `--wh bcn1`)
consistently across search, cart and checkout.

**Quantity looks wrong / bought packs instead of units** — the user's amount is in *their* units,
the cart quantity is in the *product's*. "6 huevos" on a by-the-dozen product is 1 pack, not 6;
"1 kg de plátanos" on a per-unit product is ~6 unidades, not quantity `1`. Check
`packaging`/`reference_format` and translate before setting the quantity. And a wildly large count
("200 huevos") from a dictated/typed list is usually a typo — confirm before it reaches the cart.

**A cart change didn't stick / a removed item reappeared** — the cart backend is **eventually
consistent**: reads can briefly lag writes. Two effects follow. (1) Back-to-back *separate*
`cart add`/`set` writes can race — the second reads the pre-change cart and overwrites the first.
(2) Even a single write can look like it "didn't take" if you `cart get` a beat too soon. The fix
for both: **use `cart set-many` (or `cart clear`)** — one read, one write, no inter-write race — and
if a read looks stale, just re-run `cart get`; it converges in a second or two. This is **not** a CLI
or `--max` parser bug (that was specifically chased down and disproven — `cart set <id> 0 --max N`
parses and removes correctly; the "didn't remove" was read lag). Don't add a workaround for a
nonexistent flag bug; prefer `set-many` and re-read. If you must fire several separate `add`/`set`
writes, still space them one at a time with a `cart get` between each, not in a burst.

## Checkout

**Minimum purchase error** — orders below **60€** are rejected (varies slightly by warehouse;
the bundle's "50" is not authoritative). Tell the user and offer to add items.

**No slots / all unavailable** — slots come from `checkout slots --address <id>`, not from
`checkout create`. If a day is full, check `available`/`open` on each slot and try another
address or day (`next_page` paginates).

**A prepared checkout is sitting unsubmitted** — that's the safe state. A `checkout create` +
`set-delivery` without `submit` reserves nothing irreversibly and costs nothing; it can be
left, replaced by a new checkout, or completed later. Only `submit --yes` places the order.
