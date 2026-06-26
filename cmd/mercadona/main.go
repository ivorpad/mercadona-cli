// Command mercadona is an unofficial, agent-friendly CLI for tienda.mercadona.es.
//
// It covers the anonymous read core (product search single + batch, catalog/detail
// reads) and the authenticated leg (import-har/import-curl/login, cart, checkout)
// with a configurable spending cap. Every command supports --json (data to stdout,
// logs to stderr) for programmatic use by agents.
package main

import (
	"fmt"
	"os"
)

// Build metadata, injected at release time via -ldflags (GoReleaser).
// Defaults are for a local `go build`.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "search":
		err = cmdSearch(os.Args[2:])
	case "batch":
		err = cmdBatch(os.Args[2:])
	case "total":
		err = cmdTotal(os.Args[2:])
	case "product":
		err = cmdProduct(os.Args[2:])
	case "categories":
		err = cmdCategories(os.Args[2:])
	case "login":
		err = cmdLogin(os.Args[2:])
	case "import-curl":
		err = cmdImportCurl(os.Args[2:])
	case "import-har":
		err = cmdImportHar(os.Args[2:])
	case "set-refresh":
		err = cmdSetRefresh(os.Args[2:])
	case "set-postal":
		err = cmdSetPostal(os.Args[2:])
	case "whoami":
		err = cmdWhoami(os.Args[2:])
	case "cart":
		err = cmdCart(os.Args[2:])
	case "checkout":
		err = cmdCheckout(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println(versionString())
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// versionString renders the build version, appending the commit/date when they
// were injected at release time (a local `go build` prints just "dev").
func versionString() string {
	if commit == "" {
		return version
	}
	if date != "" {
		return fmt.Sprintf("%s (%s, %s)", version, commit, date)
	}
	return fmt.Sprintf("%s (%s)", version, commit)
}

func usage() {
	fmt.Fprint(os.Stderr, `mercadona — unofficial CLI for tienda.mercadona.es

USAGE:
  mercadona <command> [flags]

READ COMMANDS (anonymous, no login):
  search <term...>        full-text product search (Algolia)
                          --category <id|name>  filter to a category (see 'categories')
                          --fresh               drop frozen (Congelados) + canned (Conservas)
  batch [-f file]         search many terms in one request (100 items ≈ 1 call)
                          --category/--fresh    apply to every term's top hit
  total [-f file]         deterministic basket total from '<id> [qty]' lines (summed in code)
  product <id>            product detail + price
  categories [--id N]     category tree, or one category's products

LOCATION (pins prices/ids to the warehouse serving your address):
  set-postal <cp>         resolve a postal code → warehouse and save it as the
                          default in config.toml (no login needed). Ids & prices
                          are per-warehouse, so set this to your real CP (e.g. 28022).

AUTHENTICATED COMMANDS (bring your own credentials):
  login                   POST /api/auth/tokens, cache bearer token
                          creds: MERCADONA_USER/MERCADONA_PASS, --user/--pass, or --password-stdin
  import-curl [--file f]  import a browser session from a DevTools 'Copy as cURL'
                          (extracts Bearer token + cookie + customer id; '-' = stdin)
  import-har [--file f]   PREFERRED: import a browser session from a DevTools HAR
                          export; extracts the refresh token → config.toml for
                          headless auto-renew (works for email AND Google accounts)
  set-refresh <token>     seed a refresh token (from one browser login) into config.toml;
                          the CLI then auto-renews the session headlessly (--stdin supported)
  whoami                  verify the session (GET /api/customers/me/)
  cart get                show current cart (names, qty × unit_price, total; --json for raw)
  cart add <id> <qty>     add qty of a product to the cart (concise; --json for raw)
  cart set <id> <qty>     set a product's absolute qty (0 removes)
  cart set-many [-f file] apply many '<id> <qty>' lines in ONE write (0 removes);
                          prices the basket first so --max refuses before writing
  cart clear              empty the cart in one write
  checkout get            --checkout <id>   show a checkout (id, total, address, slot)
  checkout addresses      list delivery addresses
  checkout create         open a checkout from the cart (returns id + default address)
  checkout slots          --address <id>   list delivery slots for an address
  checkout set-delivery   --checkout <id> --address <id> --slot <id>
  checkout submit         --checkout <id> --yes   (IRREVERSIBLE: places the order)

SPENDING GUARD (agent safety — caps how much can be spent):
  --max <eur>             refuse any cart/checkout whose total exceeds <eur>.
                          Also MERCADONA_MAX_EUR env or [limits] max_eur in config.toml.
                          'checkout submit' fails CLOSED: with a cap set, if the total
                          can't be read it refuses rather than spending.

COMMON FLAGS (may go anywhere after the (sub)command):
  --wh mad1               warehouse code (default: config [defaults].warehouse, else mad1)
  --lang es               language (default: config [defaults].lang, else es)
  --json                  emit raw JSON (data→stdout, logs→stderr)

  version | help
`)
}
