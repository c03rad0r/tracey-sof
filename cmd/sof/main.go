package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: sof <validate|node-check|discover|trace|report|audit|ui|export|example|test-node|pdf> [flags]")
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
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return app.report(ctx, *p, false, true)
	case "audit":
		fs, p := common("audit")
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
		return app.report(ctx, *p, false, true)
	case "ui":
		fs, p := common("ui")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return app.ui(ctx, *p)
	case "export":
		fs, p := common("export")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := app.report(ctx, *p, false, true); err != nil {
			return err
		}
		purchases, _, _, err := loadInputs(p.dataDir, true)
		if err != nil {
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
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return app.pdf(ctx, *p)
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
