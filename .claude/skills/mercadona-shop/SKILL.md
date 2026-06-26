---
name: mercadona-shop
description: >-
  Do the grocery shop at Mercadona (tienda.mercadona.es) by driving the local `mercadona`
  CLI: turn a shopping list into real priced products, fill the cart, prepare delivery
  checkout, and (only on explicit go-ahead) place the order. Use this whenever the user
  wants to shop, price a grocery list, build a cart, or check out at Mercadona — including
  Spanish phrasings like "hazme la compra", "haz la compra de Mercadona", "compra en
  Mercadona", "añade X al carrito", "pídeme estos productos", "¿cuánto cuesta esta lista?",
  and English ones like "do my Mercadona shop", "price this grocery list at Mercadona",
  "fill my Mercadona cart", "order these groceries". Trigger even when the user just pastes
  a list of groceries and mentions Mercadona without saying "skill" or "CLI". Also use when the
  user wants to cook a dish or recipe and buy the ingredients at Mercadona ("quiero hacer una
  paella", "ingredientes para una cena para 6"): it asks how many people and any allergies before
  it prices, so it never guesses the headcount. Always confirm the resolved products before
  touching the cart, and never place the order without explicit consent. Do NOT use for other
  supermarkets (Carrefour, Lidl, Amazon) — this is Mercadona-only.
---

# Mercadona shop

Turn a shopping list into a real Mercadona order by driving the `mercadona` CLI — search
prices, fill the cart, set delivery, and place the order. The CLI talks to the same HTTP
endpoints `tienda.mercadona.es` uses; this skill is the playbook for using it safely on a
real account.

## The two safety gates (read first)

This spends real money on a real account, so two points are non-negotiable:

1. **Confirm before touching the cart.** Reading and pricing are free and side-effect-free —
   do them freely. But before the *first* `cart add`/`cart set`, show the user the resolved
   plan (each list item → the exact product you picked, with id, name, size and price) and
   wait for an explicit OK. Product matching is fuzzy (see below), so this is where the user
   catches a wrong brand or size — cheap to fix now, annoying to fix after the cart is full.
2. **Never submit without explicit consent.** `checkout submit` is irreversible and places a
   paid order. Run it only when the user has clearly said "yes, place it" (or "dale") *for
   this specific order*, after you've shown them the full total. The CLI also requires `--yes`
   as a hard backstop — never add `--yes` on the user's behalf to "save a step".

Everything else (search, batch pricing, `cart get`, `checkout create`, `checkout slots`,
`set-delivery`) is reversible and fine to run as you work toward those two checkpoints.

### Spending cap (defense in depth)

On top of the two gates, put a hard euro ceiling on every cart/checkout command with `--max <eur>`,
so a wrong product match or a fat-fingered quantity can't quietly run up a huge order. Choose the cap:

- the user's stated budget if they gave one ("máximo 80€", "no más de 100"); otherwise
- the agreed plan total from Gate 1, rounded up with a small margin.

Any `cart add/set/set-many/clear` or `checkout create/set-delivery/submit` whose total would exceed
`--max` fails with `error: BUDGET EXCEEDED …` and a non-zero exit, so you stop instead of
overspending. `set-many`/`add`/`set` refuse *before* writing (they price the basket first), so a
breach leaves the cart untouched. `submit` fails **closed**: with a cap set, if it can't read the
order total it refuses rather than pay. Treat a
budget error as **stop-and-report** — only raise the cap if the user explicitly raises the budget.
(You can also set it once for the run: `MERCADONA_MAX_EUR`, or `[limits].max_eur` in
`~/.mercadona/config.toml`; precedence is flag > env > config.)

**Three different euro figures — don't conflate them:**

- **Minimum order ≈ 60€** — a *floor* the product subtotal must clear or checkout is refused. The
  CLI surfaces the gap ("faltan X€…") in `cart get` and `checkout create`; if you're under, add
  items, don't fight it.
- **`--max` on the cart** — a *ceiling* on the product subtotal. Set it at or just above the agreed
  basket total **with a little headroom**: real prices are discrete, so a "≈60€" target often lands
  at, say, 60.10€ — `--max 61` clears it, `--max 60` would wrongly refuse.
- **Delivery fee ≈ 8.20€** — added by the API at checkout (it's the slot price), *on top of* the
  products. So the **`checkout` `--max` must cover products + fee** (e.g. a ~60€ basket → `--max 70`
  on `checkout create/set-delivery/submit`), while the **cart `--max` covers products only**. Using
  the cart cap on `submit` will refuse a perfectly fine order.

## Locate the binary

Run `mercadona` from `PATH`. If it isn't installed, install it (npm `@ivorpad/mercadona`, the
`curl | sh` script, or a GitHub release — see the project README) and confirm with `mercadona
version`. Never hardcode a local path or a build-from-source step: this skill ships to every
CLI user, not one machine.

Pin the warehouse to the user's area — ids and prices are **per-warehouse**. First check whether
it's already pinned (`grep -E '^\s*(warehouse|postal_code)' ~/.mercadona/config.toml`): if a
warehouse is set there (from a prior `set-postal` or `import-har`), it's done — **don't ask for the
postal code again.** Only if none is set, ask their postal code and run `mercadona set-postal <cp>`
once: it resolves and saves the right warehouse as the default (no login needed). Without any of
this the default is `mad1` (Madrid); you can still override per-command with `--wh`/`--lang` (e.g.
`--wh bcn1`). Always price and shop in the warehouse that will actually deliver.

## Authenticate (bring-your-own credentials)

Reads/search need no login. Cart and checkout need a Bearer token. The catch: **password
login requires a Google reCAPTCHA Enterprise token**, so it can't be done headlessly — the
*first* login has to happen in a real browser. The good news: the **refresh token renews the
session headlessly, forever** (the refresh call needs no captcha and rotates the token). So the
durable setup is one browser login, then the CLI runs unattended.

**Durable path (recommended) — one browser login, then headless forever:**
1. The user logs in once at `tienda.mercadona.es` in their **own local browser** (not a
   datacenter/Cloud browser — reCAPTCHA Enterprise scores datacenter IPs low and returns 412).
   Either method works — email/password or "Sign in with Google".
2. Easiest: DevTools → Network → **Export HAR…**, then let the CLI extract the refresh token:
   ```bash
   mercadona import-har --file tienda.mercadona.es.har
   ```
   It pulls the `refresh_token` (works for both login methods) into `~/.mercadona/config.toml` and
   caches the session — never reading the password. (Manual alternative: grab the `refresh_token`
   from the `POST /api/auth/tokens/` response or local storage and seed it with
   `printf '%s' '<refresh_token>' | mercadona set-refresh --stdin`.)
3. From then on every authenticated call auto-refreshes on a `401 token_not_valid` and retries —
   no browser, no captcha.

**Quick one-off — import a browser session:** in a logged-in tab, DevTools → Network → any
`…/api/customers/…` request → Copy → Copy as cURL → save to a file →
`mercadona import-curl --file session.txt`. Extracts the access token + cookie + customer id.
It carries **no refresh token**, so it can't auto-renew — fine for a quick test (~6-week token),
but seed a refresh token for anything ongoing.

Don't reach for `mercadona login --user/--password` in automation — it posts without the
reCAPTCHA token and will fail; it exists only as a browser-assisted fallback.

Verify either way with `mercadona whoami` → `ok — authenticated. customer id=…`. On
`not authenticated` or `401 token_not_valid`, see `references/troubleshooting.md`.

**Never echo secrets.** Feed tokens/passwords via stdin or env, never as flags/argv; don't
print tokens or cookies back to the user.

## The shopping workflow

**Pre-flight — check the existing cart.** Before you build or price anything, run `mercadona cart
get`. A real account often already holds items (a half-finished shop, a prior session). If the
cart is non-empty and unrelated to this request, tell the user and offer to start clean with
`mercadona cart clear` — otherwise your `set-many` lands *on top of* whatever was there, throwing
off the total and the 60€-minimum check. It's a free read; do it first, every time.

### 0. Scope & clarify before pricing (detect the gaps, ask only what matters)

A grocery request usually arrives underspecified, and the gaps change the basket — sometimes a
little, sometimes completely. Before pricing, scan for those gaps and resolve each the cheapest
way that won't surprise the user:

- **They already told you — or it's already configured** → use it, don't ask.
- **There's an obviously-safe default** → take it, but **mark it** in the plan (step 2) so the
  user can veto it.
- **It's consequential *and* you can't safely guess** → stop and ask.

**"Already told you" includes saved config and session state** — so check what you already have
*before* you ask. A warehouse/postal code saved from a prior `set-postal`/`import-har`, a budget
the user just named, an already-authenticated account: re-asking any of these is friction, not
safety. (Confirm the saved warehouse with a targeted read of just those keys —
`grep -E '^\s*(warehouse|postal_code)' ~/.mercadona/config.toml` — never dump the file; it holds
`[auth]` secrets.)

The trap to avoid is silently inventing a consequential value — defaulting "una paella" to 4
people, or a seafood version for someone allergic to shellfish. The opposite trap is just as real:
ten questions before you lift a finger. Aim between — ask the *few* things that genuinely move the
basket, **batch them into one round**, and offer options with a default so a one-word answer is
enough.

**First, read what kind of request it is** — it sets how much you decompose:

- **A dish/recipe** ("hazme una paella", "ingredientes para lasaña") → turn it into ingredients
  and *scale them to a headcount*. Most ambiguity lives here.
- **A list of items** ("arroz, pollo, 6 huevos") → resolve each to a product; little decomposition.
- **Specific products** ("Arroz SOS 1 kg, leche Hacendado") → near-direct; match and confirm.
- **Vague / a photo** ("lo de siempre", "la compra semanal") → you can't conjure a list from
  nothing. A photo of a list or recipe? Read it and confirm what you parsed. Otherwise ask for the
  list (or the dish + headcount).

**Then scan these axes — for each, ask, or default-and-show?**

- **Serving size / how many meals** — for any recipe or open-ended shop this is the number
  everything scales from. Always ask; never default it silently. ("¿Para cuántas personas?")
- **Diet & allergens** — for any recipe shop, ask once. An allergen miss (marisco, frutos secos,
  gluten, lactosa) is a health risk, not a wrong brand, so it's worth the one question. Record
  "none" only after they've actually said so.
- **Recipe variant** — when a dish has genuinely different versions (paella valenciana con conejo
  vs. de marisco vs. mixta), ask; the basket differs. Minor variants: pick the common one, mark it.
- **Brand / price tier** — "el más barato" or a named brand → honor it. Otherwise default to
  Hacendado / own-brand and show it; don't ask.
- **Format** (fresh vs. frozen, size, eco/bio, entera vs. desnatada) → default the common,
  price-relevant option and show it; ask only if it clearly matters to them.
- **Quantity & units** — "leche" with no amount → scale to headcount/duration and show it.
  Reconcile pack-vs-unit (step 1). **Sanity-check absurd amounts** — "200 huevos" from a dictated
  message is almost certainly "20"; confirm before it reaches the cart.
- **Warehouse / postal code** — prices and ids are per-warehouse. **If one is already configured
  (the `[defaults]` check above), use it silently — don't ask.** Only when none is set: ask their
  postal code once and `set-postal` it *before* pricing, so you don't quote Madrid's prices
  (`mad1`, the built-in default) for another city.
- **Budget** — given → that's your `--max`; not given → the agreed plan total becomes it (see
  *Spending cap*).

**How to ask:** prefer the **AskUserQuestion** tool — tappable options plus "Other" beats a wall of
text and lets the user clear the whole round in a couple of taps. One axis per question, 2-4
concrete options, the sensible default first. (Tool unavailable? Ask in plain text — still batched
into one message.) Ask in the user's language.

**For a dish, show the ingredient math — don't hide it.** With the headcount and variant settled,
lay out the ingredients scaled to them (rice ~100 g/person, protein, sofrito, stock, saffron…) and
let the user adjust before pricing. Mark what you assumed. For pantry staples (aceite, sal, agua)
either ask "¿algo que ya tengas en casa?" or include them clearly so they can drop what they have —
don't silently pad the basket.

Treat the whole resolved plan as **editable** — brand, size, quantity, swaps. Looping here is free
and side-effect-free, so stay until they're happy, *before* any cart action.

### 1. Resolve the list to real products (free, no login)

Price the whole list in one request with `batch` — one term per line, ~100 items per call:

```bash
printf 'leche entera\nhuevos\nplátano\naceite de oliva\n' | mercadona batch -f -
```

`batch` returns the **top hit per term**, which is fast but only a rough match — Algolia
relevance on a bare word is loose (a search for "carne" can top-rank a pepper paste). So:

- For anything the user was specific about (brand, size, "el barato", "sin lactosa") or where
  the top hit looks off, run `mercadona search "<term>" --limit 5` and pick deliberately.
- When genuinely ambiguous, show the user 2-3 options and let them choose rather than guessing.
- Use `--json` when you need to parse ids/prices reliably; the default output is for humans.

**The fresh-vs-frozen (and canned) trap.** A bare term often top-ranks the *frozen* or *canned*
version when the user meant fresh: "gambas" → gamba ultracongelada, "mejillón" → mejillones en
escabeche, "guisantes" → congelados. Use `--fresh` to drop the *Congelados* and *Conservas*
categories so the fresh item surfaces — it works on both `search` and `batch`:

```bash
printf 'gambas\nmejillón\nmerluza\n' | mercadona batch -f - --fresh   # fresh seafood, not frozen/canned
mercadona search gambas --fresh --limit 5
```

`--fresh` is a nudge, not a guarantee: when no fresh variant exists (e.g. "guisantes" — only
frozen/canned in catalog), excluding those categories can leave junk via Algolia typo-matching
("guisantes"→"guantes"), so still eyeball the result. To pin a term to a specific aisle, use
`--category <id|name>` (e.g. `--category "Marisco y pescado"`, or a numeric id from `mercadona
categories`); combine with `--fresh` for fresh-within-a-category.

Note quantities: scale them to the headcount from step 0. Many items are by unit, but weight/bulk
items (fruit, deli) price per kg and accept fractional quantity (e.g. `0.5`).

**Watch for, as you match:**

- **Pack vs. unit.** The user's amount is in *their* units; the cart quantity is in the *product's*.
  "6 huevos" on a by-the-dozen product is 1 pack, not 6; "1 kg de plátanos" on a per-unit product is
  ~6 unidades, not quantity `1`. Read `packaging`/`reference_format` and translate before you set a
  quantity — this is a common silent way to buy 6× too much.
- **"El más barato por kilo/litro."** Cheapest *as sold* (`unit_price`) isn't cheapest *per unit*.
  Compare `reference_price` (€/kg, €/L) when the user is optimizing for value, not pack price.
- **Unavailable / no decent match.** If nothing fits, or the only hit is clearly wrong, say so and
  offer the nearest alternative (or ask) — don't force a bad match into the cart to avoid a question.

### 2. Present the plan and get the OK (Gate 1)

Before touching the cart, show a compact table: each list item → chosen product (`[id] name —
size — unit price`) and a line quantity. **Compute the total with code, never by hand** — feed the
resolved `<id> <qty>` lines to `mercadona total` and quote its figure (it sums `unit_price × qty`
deterministically, in the right warehouse):

```bash
printf '5044 1\n2779 2\n24181 1\n' | mercadona total -f -   # → per-line subtotals + total
```

Surface every assumption you marked in step 0 (headcount, brand, size, fresh/frozen, a substituted
product) so the user can veto it here. Then wait for the go-ahead — and if they want changes, edit
the plan and re-run `total`. This is the confirm-before-cart gate. (The cart/checkout API total is
the authoritative one once items are in the cart; `total` is the pre-cart estimate.)

### 3. Fill the cart (after the OK)

Fill the whole basket in **one write** with `set-many` — it reads the cart once, applies every
`<id> <qty>` line, prices the result, then issues a single PUT:

```bash
# one '<id> <qty>' per line; qty 0 removes that line
printf '5044 1\n37229 2\n82830.1 2\n' | mercadona cart set-many -f - --max 61
mercadona cart get                                # verify: names, qty × unit_price, total
```

`set-many` takes the **same `<id> <qty>` lines you already fed `total`** in Gate 1, so reuse that
file. It **prices the resulting basket before writing**, so `--max` refuses *before* it touches the
cart — a fuzzy match or fat-fingered quantity never lands. This replaces the old "one `cart add` at
a time, verify, repeat" dance: a single PUT instead of dozens, and no write-race to nurse.

For small incremental tweaks `cart add <id> <qty>` (additive) and `cart set <id> <qty>` (absolute;
`0` removes) still exist and are also `--max`-guarded; their output is concise by default (new
total + the changed line), with `--json` for the raw cart. But for building or swapping a basket,
reach for `set-many`/`clear`, not a loop of `add`s.

After writing, run `cart get` and reconcile the line set + total against your plan.

**The cart backend is eventually consistent — reads can lag writes.** `set-many` and `clear` each
do a single PUT, so *within* one command there's no race. But a `cart get` fired immediately after
a write may still show the pre-write state for a beat. If something looks like it "didn't take",
just re-run `cart get` — it converges within a second or two. Do **not** infer a CLI or `--max`
parser bug from a stale read; there isn't one (this was chased down and disproven). Re-read instead.
If you ever must fire several *separate* `add`/`set` writes, still space them one at a time with a
`cart get` between — but prefer `set-many` and sidestep it entirely.

### 4. Prepare delivery checkout (reversible)

```bash
mercadona checkout create --json --max 80   # → checkout id + default address (embedded)
mercadona checkout slots --address <id> --json   # delivery slots for that address
mercadona checkout set-delivery --checkout <chk> --address <addr> --slot <slot> --max 80
```

`checkout create` returns the checkout `id` and the default delivery address (slots are NOT
in this response — they hang off the address). Fetch slots with `checkout slots --address
<id>`, pick one the user wants (each slot has `start`/`end`/`price`/`available`), then attach
it with `set-delivery`. The response carries the final price breakdown.

The **minimum order is 60€** (enforced live; varies a little by warehouse) and delivery adds
a fee (~8.20€, charged as the slot price). The CLI now surfaces the shortfall for you — `cart
get` and `checkout create` print "faltan X€ para el mínimo" when the basket is under 60€. If you
see it, tell the user and offer to add items rather than letting checkout fail. Remember the fee
rides on top: size the **`checkout --max`** to cover products **+ ~8.20€** (see *Spending cap*).

### 5. Place the order (Gate 2 — explicit consent only)

Show the final order: line items, subtotal, delivery fee, total, and the chosen slot. Only if
the user explicitly confirms *this* order:

```bash
mercadona checkout submit --checkout <chk> --max 80 --yes   # IRREVERSIBLE — places the order (refuses if total > 80€)
```

If you have any doubt about consent, stop and ask. A prepared-but-unsubmitted checkout is
harmless and can sit until the user decides; a placed order cannot be taken back here.

## Reference material

- `references/cli-reference.md` — every command, its flags, and the exact JSON/field shapes
  (cart lines, checkout, slots). Read it when you need to parse `--json` output precisely.
- `references/troubleshooting.md` — auth expiry, Akamai 403/challenge, warehouse/postal
  mismatch, "no results", minimum-purchase and IP-sensitivity notes.

## Why no all-in-one script

This skill drives the CLI step by step instead of a single "shop it all" wrapper on purpose:
the two safety gates need a human in the loop, and product matching needs judgment. Keep the
control flow here, in the conversation, where the user can steer it.
