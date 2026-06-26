// Command mercadona is an unofficial, agent-friendly CLI for tienda.mercadona.es.
//
// v0 ships the reliable, anonymous read core: product search (single + batch)
// and catalog/detail reads. Authenticated commands (login, cart, checkout) land
// in the next increment. Every command supports --json (data to stdout, logs to
// stderr) for programmatic use by agents.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/ivorjpc/mercadona/internal/client"
	"github.com/ivorjpc/mercadona/internal/config"
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

// common flags shared by every subcommand.
type common struct {
	wh, lang string
	jsonOut  bool
}

func addCommon(fs *flag.FlagSet) *common {
	c := &common{}
	fs.StringVar(&c.wh, "wh", "mad1", "warehouse code (e.g. mad1, bcn1)")
	fs.StringVar(&c.lang, "lang", "es", "language (es, en, ca, eu, vai)")
	fs.BoolVar(&c.jsonOut, "json", false, "emit raw JSON to stdout")
	return c
}

func newClient(c *common) *client.Client {
	cl := client.New()
	cl.Warehouse = c.wh
	cl.Lang = c.lang
	return cl
}

func cmdSearch(args []string) error {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	cf := addCommon(fs)
	limit := fs.Int("limit", 24, "max results")
	_ = fs.Parse(args)
	if fs.NArg() == 0 {
		return fmt.Errorf("usage: mercadona search [flags] <term...>")
	}
	cl := newClient(cf)
	res, err := cl.Search(strings.Join(fs.Args(), " "), *limit)
	if err != nil {
		return err
	}
	if cf.jsonOut {
		return emitJSON(res)
	}
	fmt.Printf("query=%q  nbHits=%d  (index=%s)\n", res.Query, res.NbHits, cl.IndexName())
	for _, h := range res.Hits {
		printHit("  ", h)
	}
	return nil
}

func cmdBatch(args []string) error {
	fs := flag.NewFlagSet("batch", flag.ExitOnError)
	cf := addCommon(fs)
	file := fs.String("f", "", "file with one term per line ('-' for stdin); else terms are positional args")
	hits := fs.Int("hits", 1, "results per term")
	_ = fs.Parse(args)
	terms, err := collectTerms(*file, fs.Args())
	if err != nil {
		return err
	}
	if len(terms) == 0 {
		return fmt.Errorf("no terms given (use -f file, stdin, or positional args)")
	}
	cl := newClient(cf)
	results, err := cl.Batch(terms, *hits)
	if err != nil {
		return err
	}
	if cf.jsonOut {
		return emitJSON(results)
	}
	for i, r := range results {
		term := r.Query
		if term == "" && i < len(terms) {
			term = terms[i]
		}
		if len(r.Hits) == 0 {
			fmt.Printf("• %-24s → (sin resultados)\n", term)
			continue
		}
		h := r.Hits[0]
		fmt.Printf("• %-24s → [%s] %s — %s€ %s\n", term, h.ID, h.DisplayName, h.Price.UnitPrice, refFormat(h.Price))
	}
	return nil
}

func cmdProduct(args []string) error {
	fs := flag.NewFlagSet("product", flag.ExitOnError)
	cf := addCommon(fs)
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: mercadona product [flags] <id>")
	}
	cl := newClient(cf)
	pv, raw, err := cl.Product(fs.Arg(0))
	if err != nil {
		return err
	}
	if cf.jsonOut {
		return emitRaw(raw)
	}
	fmt.Printf("[%s] %s\n", pv.ID, pv.DisplayName)
	fmt.Printf("  precio: %s€  (%s %s)\n", pv.Price.UnitPrice, pv.Price.ReferencePrice, pv.Price.ReferenceFormat)
	if pv.Packaging != "" {
		fmt.Printf("  formato: %s\n", pv.Packaging)
	}
	if pv.ShareURL != "" {
		fmt.Printf("  url: %s\n", pv.ShareURL)
	}
	return nil
}

func cmdCategories(args []string) error {
	fs := flag.NewFlagSet("categories", flag.ExitOnError)
	cf := addCommon(fs)
	id := fs.String("id", "", "fetch a single category (with products) by id")
	_ = fs.Parse(args)
	cl := newClient(cf)
	var raw json.RawMessage
	var err error
	if *id != "" {
		raw, err = cl.Category(*id)
	} else {
		raw, err = cl.Categories()
	}
	if err != nil {
		return err
	}
	if cf.jsonOut || *id != "" {
		return emitRaw(raw)
	}
	// compact human view of the top-level tree
	var tree struct {
		Results []struct {
			ID         int    `json:"id"`
			Name       string `json:"name"`
			Categories []struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"categories"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &tree); err != nil {
		return emitRaw(raw)
	}
	for _, top := range tree.Results {
		fmt.Printf("%d  %s\n", top.ID, top.Name)
		for _, sub := range top.Categories {
			fmt.Printf("    %d  %s\n", sub.ID, sub.Name)
		}
	}
	return nil
}

// ---- authenticated commands ----

// authedClient builds a client whose auth comes from, in precedence order:
// env vars (MERCADONA_TOKEN/COOKIE/CUSTOMER/USER/PASS), then ~/.mercadona/config.toml,
// then the cached session written by `login`/`import-curl`. If there's no access
// token but there are credentials (or a refresh token), it logs in / refreshes.
func authedClient(cf *common) (*client.Client, error) {
	cl := newClient(cf)
	cl.LoadToken() // cached session: access + refresh + cookie (best-effort)

	cfg, _ := config.LoadConfig() // ~/.mercadona/config.toml (missing = empty)
	if cfg.Auth.Username != "" {
		cl.Username = cfg.Auth.Username
	}
	if cfg.Auth.Password != "" {
		cl.Password = cfg.Auth.Password
	}
	if cfg.Auth.RefreshToken != "" && cl.RefreshToken == "" {
		cl.RefreshToken = cfg.Auth.RefreshToken
	}
	if u := os.Getenv("MERCADONA_USER"); u != "" {
		cl.Username = u
	}
	if p := os.Getenv("MERCADONA_PASS"); p != "" {
		cl.Password = p
	}
	if t := os.Getenv("MERCADONA_TOKEN"); t != "" {
		cl.Token = t
	}
	if ck := os.Getenv("MERCADONA_COOKIE"); ck != "" {
		cl.Cookie = ck
	}
	if cid := os.Getenv("MERCADONA_CUSTOMER"); cid != "" {
		cl.CustomerID = cid
	}

	if cl.Token == "" {
		if !cl.CanReauth() {
			if cl.Cookie != "" {
				return nil, fmt.Errorf("a cookie alone does not authenticate the API — you also need a Bearer token or credentials")
			}
			return nil, fmt.Errorf("not authenticated: `mercadona login` (and --save), set MERCADONA_USER/PASS or [auth] in ~/.mercadona/config.toml, or `mercadona import-curl`")
		}
		if err := cl.EnsureToken(); err != nil { // refresh, else login from creds
			return nil, fmt.Errorf("login/refresh failed: %w", err)
		}
	}
	if err := cl.EnsureCustomer(); err != nil {
		return nil, err
	}
	return cl, nil
}

func cmdWhoami(args []string) error {
	fs := flag.NewFlagSet("whoami", flag.ExitOnError)
	cf := addCommon(fs)
	_ = fs.Parse(args)
	cl, err := authedClient(cf)
	if err != nil {
		return err
	}
	cu, raw, err := cl.Me()
	if err != nil {
		return err
	}
	if cf.jsonOut {
		return emitRaw(raw)
	}
	fmt.Printf("ok — authenticated. customer id=%s\n", cu.Resolve())
	return nil
}

// import-curl extraction patterns (DevTools → Copy as cURL).
var (
	reBearer   = regexp.MustCompile(`(?i)authorization:\s*bearer\s+([A-Za-z0-9._~+/=\-]+)`)
	reCookieB  = regexp.MustCompile(`(?:^|\s)(?:-b|--cookie)\s+'([^']*)'`)
	reCookieH  = regexp.MustCompile(`(?i)(?:-H|--header)\s+'cookie:\s*([^']*)'`)
	reCustomer = regexp.MustCompile(`/api/customers/([^/'"\s]+)/`)
)

func cmdImportCurl(args []string) error {
	fs := flag.NewFlagSet("import-curl", flag.ExitOnError)
	file := fs.String("file", "-", "file with a DevTools 'Copy as cURL' command ('-' = stdin)")
	_ = fs.Parse(args)
	var (
		data []byte
		err  error
	)
	if *file == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(*file)
	}
	if err != nil {
		return err
	}
	s := string(data)
	token := firstSubmatch(reBearer, s)
	cookie := firstSubmatch(reCookieB, s)
	if cookie == "" {
		cookie = strings.TrimSpace(firstSubmatch(reCookieH, s))
	}
	customer := firstSubmatch(reCustomer, s)
	if customer == "me" {
		customer = "" // let the "me" default handle it
	}
	if token == "" && cookie == "" {
		return fmt.Errorf("no 'authorization: Bearer ...' or cookie found in the input")
	}
	if err := client.SaveSession(token, cookie, customer); err != nil {
		return err
	}
	// Never echo secrets — report only lengths + the (non-secret) customer id.
	fmt.Fprintf(os.Stderr, "imported session: token=%s, cookie=%s, customer=%s\n",
		present(token), present(cookie), orDefault(customer, "me"))
	fmt.Fprintln(os.Stderr, "→ verify with: mercadona whoami")
	return nil
}

// import-har extracts a session from a browser HAR export (DevTools → Network →
// "Export HAR…"). Unlike import-curl it also captures the durable refresh token
// and, by default, seeds it into ~/.mercadona/config.toml so the CLI re-auths
// headlessly forever — works for both email/password and Google-login accounts.
func cmdImportHar(args []string) error {
	fs := flag.NewFlagSet("import-har", flag.ExitOnError)
	file := fs.String("file", "", "path to a .har file ('-' or omitted = stdin; may also be passed positionally)")
	save := fs.Bool("save", true, "seed the refresh token into ~/.mercadona/config.toml for headless auto-renew")
	_ = fs.Parse(args)

	src := *file
	if src == "" && fs.NArg() == 1 {
		src = fs.Arg(0)
	}
	var (
		data []byte
		err  error
	)
	if src == "" || src == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(src)
	}
	if err != nil {
		return err
	}

	sess, err := client.ParseHAR(data)
	if err != nil {
		return err
	}
	if err := client.SaveHARSession(sess); err != nil {
		return err
	}

	seeded := false
	if *save && sess.RefreshToken != "" {
		cfg, _ := config.LoadConfig()
		cfg.Auth.RefreshToken = sess.RefreshToken
		if err := config.SaveConfig(cfg); err != nil {
			return fmt.Errorf("session cached but writing config.toml failed: %w", err)
		}
		seeded = true
	}

	// Never echo secrets — report only lengths + the (non-secret) customer id.
	fmt.Fprintf(os.Stderr, "imported HAR session (%s login): access=%s, refresh=%s, cookie=%s, customer=%s\n",
		orDefault(sess.LoginKind, "unknown"), present(sess.AccessToken), present(sess.RefreshToken),
		present(sess.Cookie), orDefault(sess.CustomerID, "(from JWT)"))
	switch {
	case seeded:
		fmt.Fprintln(os.Stderr, "→ refresh token seeded into ~/.mercadona/config.toml (0600): the CLI now auto-renews headlessly — always authenticated.")
	case sess.RefreshToken == "":
		fmt.Fprintln(os.Stderr, "⚠ no refresh token in this HAR — cached the access token only (expires ~6 wk, no auto-renew). Re-export a HAR that includes the login response, or use `set-refresh`.")
	}
	fmt.Fprintln(os.Stderr, "→ verify with: mercadona whoami")
	return nil
}

func firstSubmatch(re *regexp.Regexp, s string) string {
	if m := re.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

func present(s string) string {
	if s == "" {
		return "(none)"
	}
	return fmt.Sprintf("%d chars", len(s))
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// fmtQty renders a quantity without a trailing ".0" (1.0 → "1", 0.5 → "0.5").
func fmtQty(q float64) string {
	return strconv.FormatFloat(q, 'f', -1, 64)
}

func cmdLogin(args []string) error {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	cf := addCommon(fs)
	user := fs.String("user", "", "username/email (or env MERCADONA_USER, or config.toml)")
	pass := fs.String("password", "", "password (prefer --password-stdin / env / config)")
	passAlias := fs.String("pass", "", "alias of --password")
	passStdin := fs.Bool("password-stdin", false, "read the password from stdin")
	save := fs.Bool("save", false, "save credentials to ~/.mercadona/config.toml for auto-relogin")
	_ = fs.Parse(args)

	cfg, _ := config.LoadConfig()
	user2 := firstNonEmpty(*user, os.Getenv("MERCADONA_USER"), cfg.Auth.Username)
	if user2 == "" {
		return fmt.Errorf("missing username: pass --user, set MERCADONA_USER, or [auth].username in config.toml")
	}
	pass2 := firstNonEmpty(*pass, *passAlias, os.Getenv("MERCADONA_PASS"), cfg.Auth.Password)
	if *passStdin {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		pass2 = strings.TrimRight(string(b), "\r\n")
	}
	if pass2 == "" {
		return fmt.Errorf("missing password: --password, --password-stdin, MERCADONA_PASS, or config.toml")
	}

	cl := newClient(cf)
	tok, err := cl.Login(user2, pass2)
	if err != nil {
		return err
	}
	if *save {
		cfg.Auth.Username = user2
		cfg.Auth.Password = pass2
		if err := config.SaveConfig(cfg); err != nil {
			return fmt.Errorf("login ok but saving config.toml failed: %w", err)
		}
		fmt.Fprintln(os.Stderr, "credentials saved to ~/.mercadona/config.toml (0600); future runs auto-relogin")
	}
	if cf.jsonOut {
		return emitJSON(map[string]string{"status": "ok", "customer_id": tok.CustomerID.String()})
	}
	fmt.Printf("logged in — customer_id=%s (session cached)\n", tok.CustomerID.String())
	return nil
}

// set-refresh seeds the durable refresh token into ~/.mercadona/config.toml so
// the CLI can renew the session headlessly. Get the token from one browser login
// (DevTools → the POST /api/auth/tokens/ response, or local storage).
func cmdSetRefresh(args []string) error {
	fs := flag.NewFlagSet("set-refresh", flag.ExitOnError)
	stdin := fs.Bool("stdin", false, "read the refresh token from stdin (keeps it out of argv)")
	_ = fs.Parse(args)
	var rt string
	switch {
	case *stdin:
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		rt = strings.TrimSpace(string(b))
	case fs.NArg() == 1:
		rt = strings.TrimSpace(fs.Arg(0))
	default:
		return fmt.Errorf("usage: mercadona set-refresh <refresh_token>   (or: ... set-refresh --stdin)")
	}
	if rt == "" {
		return fmt.Errorf("empty refresh token")
	}
	cfg, _ := config.LoadConfig()
	cfg.Auth.RefreshToken = rt
	if err := config.SaveConfig(cfg); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "refresh token saved to ~/.mercadona/config.toml (%d chars, 0600).\n", len(rt))
	fmt.Fprintln(os.Stderr, "→ the CLI now auto-renews headlessly. test: mercadona whoami")
	return nil
}

func cmdCart(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mercadona cart <get|add|set> [flags] [args]")
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet("cart", flag.ExitOnError)
	cf := addCommon(fs)
	_ = fs.Parse(rest)
	cl, err := authedClient(cf)
	if err != nil {
		return err
	}
	switch sub {
	case "get":
		cart, raw, err := cl.GetCart()
		if err != nil {
			return err
		}
		if cf.jsonOut {
			return emitRaw(raw)
		}
		fmt.Printf("cart %s  (v%d, %d productos, total %s€)\n", cart.ID, cart.Version, cart.ProductsCount, cart.Summary.Total)
		for _, l := range cart.Lines {
			fmt.Printf("  %s× product %s\n", fmtQty(l.Quantity), l.ProductID)
		}
		return nil
	case "add", "set":
		a := fs.Args()
		if len(a) != 2 {
			return fmt.Errorf("usage: mercadona cart %s <product_id> <qty>", sub)
		}
		qty, err := strconv.ParseFloat(a[1], 64)
		if err != nil {
			return fmt.Errorf("invalid qty %q", a[1])
		}
		var raw json.RawMessage
		if sub == "add" {
			raw, err = cl.AddLine(a[0], qty)
		} else {
			raw, err = cl.SetLine(a[0], qty)
		}
		if err != nil {
			return err
		}
		return emitRaw(raw)
	default:
		return fmt.Errorf("unknown cart subcommand %q (get|add|set)", sub)
	}
}

func cmdCheckout(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mercadona checkout <addresses|slots|create|set-delivery|submit> [flags]")
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet("checkout", flag.ExitOnError)
	cf := addCommon(fs)
	checkoutID := fs.String("checkout", "", "checkout id")
	addressID := fs.Int("address", 0, "delivery address id")
	slotID := fs.String("slot", "", "delivery slot id")
	yes := fs.Bool("yes", false, "REQUIRED to actually place the order (irreversible, spends money)")
	_ = fs.Parse(rest)
	cl, err := authedClient(cf)
	if err != nil {
		return err
	}
	switch sub {
	case "addresses":
		raw, err := cl.Addresses()
		if err != nil {
			return err
		}
		return emitRaw(raw)
	case "slots":
		if *addressID == 0 {
			return fmt.Errorf("need --address <id> (from `checkout addresses` or the checkout's default address)")
		}
		raw, err := cl.Slots(*addressID)
		if err != nil {
			return err
		}
		return emitRaw(raw)
	case "create":
		cart, _, err := cl.GetCart()
		if err != nil {
			return err
		}
		raw, err := cl.CreateCheckout(cart)
		if err != nil {
			return err
		}
		return emitRaw(raw)
	case "set-delivery":
		if *checkoutID == "" || *addressID == 0 || *slotID == "" {
			return fmt.Errorf("need --checkout <id> --address <id> --slot <id>")
		}
		raw, err := cl.SetDelivery(*checkoutID, *addressID, *slotID)
		if err != nil {
			return err
		}
		return emitRaw(raw)
	case "submit":
		if *checkoutID == "" {
			return fmt.Errorf("need --checkout <id>")
		}
		if !*yes {
			return fmt.Errorf("refusing to place a REAL order without --yes (irreversible)")
		}
		raw, err := cl.SubmitOrder(*checkoutID)
		if err != nil {
			return err
		}
		return emitRaw(raw)
	default:
		return fmt.Errorf("unknown checkout subcommand %q", sub)
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// ---- output helpers ----

func printHit(indent string, h client.Hit) {
	cat := h.Category()
	if cat != "" {
		cat = "(" + cat + ")"
	}
	fmt.Printf("%s[%s] %s — %s€ %s %s\n", indent, h.ID, h.DisplayName, h.Price.UnitPrice, refFormat(h.Price), cat)
}

func refFormat(p client.PriceInstructions) string {
	if p.ReferencePrice == "" || p.ReferenceFormat == "" {
		return ""
	}
	return fmt.Sprintf("(%s€/%s)", p.ReferencePrice, p.ReferenceFormat)
}

func emitJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func emitRaw(raw json.RawMessage) error {
	var buf any
	if err := json.Unmarshal(raw, &buf); err != nil {
		_, err = os.Stdout.Write(raw)
		return err
	}
	return emitJSON(buf)
}

func collectTerms(file string, posArgs []string) ([]string, error) {
	if file == "" {
		return posArgs, nil
	}
	var r io.Reader
	if file == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(file)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}
	var terms []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if t := strings.TrimSpace(sc.Text()); t != "" && !strings.HasPrefix(t, "#") {
			terms = append(terms, t)
		}
	}
	return terms, sc.Err()
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
  batch [-f file]         search many terms in one request (100 items ≈ 1 call)
  product <id>            product detail + price
  categories [--id N]     category tree, or one category's products

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
  cart get                show current cart (raw JSON)
  cart add <id> <qty>     add qty of a product to the cart
  cart set <id> <qty>     set a product's absolute qty (0 removes)
  checkout addresses      list delivery addresses
  checkout create         open a checkout from the cart (returns id + default address)
  checkout slots          --address <id>   list delivery slots for an address
  checkout set-delivery   --checkout <id> --address <id> --slot <id>
  checkout submit         --checkout <id> --yes   (IRREVERSIBLE: places the order)

COMMON FLAGS (place right after the (sub)command):
  --wh mad1               warehouse code
  --lang es               language
  --json                  emit raw JSON (data→stdout, logs→stderr)

  version | help
`)
}
