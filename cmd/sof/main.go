package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"tracey-sof/internal/intake"
	"tracey-sof/internal/model"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: sof <validate|node-check|discover|trace|report|audit|ui|export|example|test-node|pdf|intake|resolve-intake|review-intake|promote-intake> [flags]")
	}
	app := application{stdout: stdout, stderr: stderr}
	common := func(name string) (*flag.FlagSet, *paths) {
		fs := flag.NewFlagSet(name, flag.ContinueOnError)
		fs.SetOutput(stderr)
		p := &paths{}
		fs.StringVar(&p.dataDir, "data-dir", "./data", "directory containing owned.json, purchases.json and annotations.json")
		fs.StringVar(&p.cacheDir, "cache-dir", "./cache", "private chain cache directory")
		fs.StringVar(&p.outDir, "out-dir", "./out", "report output directory")
		fs.StringVar(&p.fixtures, "fixtures", "./testdata/mainnet", "mainnet fixture directory")
		fs.StringVar(&p.inbox, "inbox", "./data/inbox", "directory containing receipts, images, CSV, TXT, or JSON sources")
		return fs, p
	}
	switch args[0] {
	case "validate":
		fs, p := common("validate")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return app.validate(*p)
	case "node-check":
		fs, p := common("node-check")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return app.nodeCheck(ctx, *p)
	case "discover":
		fs, p := common("discover")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return app.discover(ctx, *p)
	case "trace":
		fs, p := common("trace")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return app.trace(ctx, *p)
	case "report":
		fs, p := common("report")
		unredacted := fs.Bool("unredacted", true, "print identifiers and amounts (default); set false to redact")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return app.report(ctx, *p, false, true, *unredacted)
	case "audit":
		fs, p := common("audit")
		unredacted := fs.Bool("unredacted", true, "print identifiers and amounts (default); set false to redact")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := app.validate(*p); err != nil {
			return err
		}
		if err := app.discover(ctx, *p); err != nil {
			return err
		}
		if err := app.trace(ctx, *p); err != nil {
			return err
		}
		return app.report(ctx, *p, false, true, *unredacted)
	case "ui":
		fs, p := common("ui")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return app.ui(ctx, *p)
	case "graphs":
		fs, p := common("graphs")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return app.graphs(ctx, *p)
	case "bank-import":
		fs, p := common("bank-import")
		bankFile := fs.String("bank-file", "", "bank CSV export")
		merchants := fs.String("merchant", "Example Merchant,Pocket", "comma-separated merchant keywords")
		provider := fs.String("provider", "Example Exchange", "provider name for created purchase records")
		window := fs.Int("window-days", 3, "date window around each on-chain event")
		apply := fs.Bool("apply", false, "attach matched evidence and create purchase records")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *bankFile == "" {
			return fmt.Errorf("bank-import requires -bank-file")
		}
		return app.bankImport(*p, *bankFile, *merchants, *provider, *window, *apply)
	case "statement-requests":
		fs, p := common("statement-requests")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return app.statementRequests(*p)
	case "gaps":
		fs, p := common("gaps")
		inflowOnly := fs.Bool("inflow", false, "print only inbound exchange payouts")
		outflowOnly := fs.Bool("outflow", false, "print only outbound exchange transfers")
		strict := fs.Bool("strict", false, "exit non-zero while unaccounted exchange payouts remain")
		includeOutflows := fs.Bool("include-outflows", false, "make -strict fail on outbound transfers too")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		inflow := *inflowOnly || !*outflowOnly
		outflow := *outflowOnly || !*inflowOnly
		return app.gaps(ctx, *p, inflow, outflow, *strict, *includeOutflows)
	case "export":
		fs, p := common("export")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		purchases, _, _, err := loadInputs(p.dataDir, true)
		if err != nil {
			return err
		}
		if err := app.report(ctx, *p, false, true, true); err != nil {
			return err
		}
		return app.export(*p, purchases)
	case "example":
		fs, p := common("example")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return app.example(ctx, *p)
	case "test-node":
		fs, p := common("test-node")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return app.testNode(ctx, *p)
	case "pdf":
		fs, p := common("pdf")
		unredacted := fs.Bool("unredacted", true, "print identifiers and amounts (default); set false to redact")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return app.pdf(ctx, *p, *unredacted)
	case "intake":
		fs, p := common("intake")
		maxPages := fs.Int("max-pages", 20, "maximum PDF pages to OCR")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return app.intake(ctx, *p, *maxPages)
	case "resolve-intake":
		fs, p := common("resolve-intake")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return app.resolveIntake(ctx, *p)
	case "review-intake":
		fs, p := common("review-intake")
		list := fs.Bool("list", false, "list candidates")
		accept := fs.String("accept", "", "candidate ID to accept")
		reject := fs.String("reject", "", "candidate ID to reject")
		pending := fs.String("pending", "", "candidate ID to return to pending")
		role := fs.String("role", "", "candidate role")
		note := fs.String("note", "", "review note")
		purchaseID := fs.String("purchase-id", "", "purchase ID for the source")
		date := fs.String("date", "", "RFC3339 purchase date")
		provider := fs.String("provider", "", "purchase provider")
		amount := fs.Int64("amount-sats", 0, "positive purchase amount in satoshis")
		fiatAmount := fs.String("fiat-amount", "", "fiat amount as a decimal string")
		fiatCurrency := fs.String("fiat-currency", "", "three-letter fiat currency")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		options := intake.ReviewOptions{Accept: *accept, Reject: *reject, Pending: *pending, Role: *role, Note: *note,
			PurchaseID: *purchaseID, Date: *date, Provider: *provider}
		if *amount != 0 {
			options.AmountSats = amount
		}
		if *fiatAmount != "" || *fiatCurrency != "" {
			options.Fiat = &model.Fiat{Amount: *fiatAmount, Currency: *fiatCurrency}
		}
		return app.reviewIntake(*p, options, *list)
	case "promote-intake":
		fs, p := common("promote-intake")
		replace := fs.Bool("replace", false, "replace an existing purchase with the same ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return app.promoteIntake(*p, *replace)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func env(name string) string { return os.Getenv(name) }
