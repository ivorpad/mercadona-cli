package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ivorjpc/mercadona/internal/client"
)

func cmdCheckout(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mercadona checkout <get|addresses|slots|create|set-delivery|submit> [flags]")
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet("checkout", flag.ExitOnError)
	cf := addCommon(fs)
	checkoutID := fs.String("checkout", "", "checkout id")
	addressID := fs.Int("address", 0, "delivery address id")
	slotID := fs.String("slot", "", "delivery slot id")
	yes := fs.Bool("yes", false, "REQUIRED to actually place the order (irreversible, spends money)")
	maxFlag := fs.Float64("max", 0, "refuse a checkout whose total exceeds this many € (0 = env/config)")
	_ = fs.Parse(reorderArgs(fs, rest))
	cl, err := authedClient(cf)
	if err != nil {
		return err
	}
	switch sub {
	case "get":
		if *checkoutID == "" {
			return fmt.Errorf("need --checkout <id>")
		}
		raw, err := cl.GetCheckout(*checkoutID)
		if err != nil {
			return err
		}
		return emitRaw(raw)
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
		if bErr := budgetCheckRaw(raw, *maxFlag, "checkout"); bErr != nil {
			return bErr
		}
		// Nudge if the basket is under the delivery minimum (advisory; to stderr so
		// --json stdout stays clean). The slot/delivery fee is added later by the API.
		if total, ok := client.ExtractTotal(raw); ok && total < MinOrderEUR {
			fmt.Fprintf(os.Stderr, "⚠ basket %.2f€ is under the %.0f€ delivery minimum — faltan %.2f€ (the delivery fee is added on top once a slot is chosen)\n", total, MinOrderEUR, MinOrderEUR-total)
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
		if bErr := budgetCheckRaw(raw, *maxFlag, "checkout (with delivery)"); bErr != nil {
			return bErr
		}
		return emitRaw(raw)
	case "submit":
		if *checkoutID == "" {
			return fmt.Errorf("need --checkout <id>")
		}
		if !*yes {
			return fmt.Errorf("refusing to place a REAL order without --yes (irreversible)")
		}
		// Spending guard: verify the order total against the cap before paying.
		maxEUR, mErr := resolveMax(*maxFlag)
		if mErr != nil {
			return mErr
		}
		if maxEUR > 0 {
			chk, gErr := cl.GetCheckout(*checkoutID)
			if gErr != nil {
				return fmt.Errorf("refusing to submit: couldn't fetch the checkout to verify the %.2f€ limit: %w", maxEUR, gErr)
			}
			total, ok := client.ExtractTotal(chk)
			if bErr := checkBudget(total, ok, maxEUR, "submit order", true); bErr != nil {
				return bErr
			}
			fmt.Fprintf(os.Stderr, "budget ok: order total %.2f€ ≤ %.2f€ limit\n", total, maxEUR)
		} else {
			fmt.Fprintln(os.Stderr, "⚠ no spending limit set — submitting without a budget guard (set [limits].max_eur, MERCADONA_MAX_EUR, or --max to cap spend).")
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
