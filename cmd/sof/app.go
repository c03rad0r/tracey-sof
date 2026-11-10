package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"time"

	"tracey-sof/internal/core"
	"tracey-sof/internal/fixtures"
	"tracey-sof/internal/input"
	"tracey-sof/internal/model"
	"tracey-sof/internal/provenance"
	reportfiles "tracey-sof/internal/report"
	uiserver "tracey-sof/internal/server"
)

type paths struct{ dataDir, cacheDir, outDir, fixtures string }
type application struct{ stdout, stderr io.Writer }

const firstTransfer = "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16"
const firstTransferParent = "0437cd7f8525ceed2324359c2d0ba26006d92d856a9c20fa0241106ee5a597c9"

func loadInputs(dir string, artifacts bool) (model.PurchaseFile, model.Owned, model.AnnotationFile, error) {
	owned, err := input.LoadOwned(filepath.Join(dir, "owned.json"))
	if err != nil {
		return model.PurchaseFile{}, model.Owned{}, model.AnnotationFile{}, err
	}
	purchases, err := input.LoadPurchases(filepath.Join(dir, "purchases.json"), artifacts)
	if err != nil {
		return model.PurchaseFile{}, model.Owned{}, model.AnnotationFile{}, err
	}
	annotations, err := input.LoadAnnotations(filepath.Join(dir, "annotations.json"))
	return purchases, owned, annotations, err
}

func (a application) validate(p paths) error {
	_, _, _, err := loadInputs(p.dataDir, true)
	if err == nil {
		fmt.Fprintln(a.stdout, "owned, purchase, annotation, and artifact inputs are valid")
	}
	return err
}

func rpcClient() (*core.Client, error) {
	return core.NewClient(core.Config{URL: env("BITCOIND_RPC_URL"), CookieFile: env("BITCOIND_COOKIE_FILE"), Username: env("BITCOIND_RPC_USER"), Password: env("BITCOIND_RPC_PASSWORD")}, nil)
}

func (a application) reportNode(info core.NodeInfo) {
	fmt.Fprintln(a.stdout, core.StringSnapshot(info))
	for _, warning := range info.Warnings {
		fmt.Fprintln(a.stdout, "warning:", warning)
	}
}

func (a application) nodeCheck(ctx context.Context, _ paths) error {
	client, err := rpcClient()
	if err != nil {
		return err
	}
	info, err := client.NodeCheck(ctx)
	if err != nil {
		return err
	}
	a.reportNode(info)
	return printJSON(a.stdout, info)
}

func (a application) discover(ctx context.Context, p paths) error {
	_, owned, _, err := loadInputs(p.dataDir, false)
	if err != nil {
		return err
	}
	client, err := rpcClient()
	if err != nil {
		return err
	}
	info, err := client.NodeCheck(ctx)
	if err != nil {
		return err
	}
	a.reportNode(info)
	if info.InitialBlockDownload || info.Blocks < info.Headers {
		return fmt.Errorf("node is not synchronized")
	}
	source := core.NewSource(client, filepath.Join(p.cacheDir, "transactions.json"))
	if err := source.Load(); err != nil {
		return err
	}
	targets, chain, err := client.Discover(ctx, owned, source)
	if err != nil {
		return err
	}
	if err := core.SaveDiscovery(filepath.Join(p.cacheDir, "discovery.json"), chain, targets); err != nil {
		return err
	}
	if err := source.Save(); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "discovered %d current UTXOs at block %d %s\n", len(targets), chain.BestHeight, chain.BestBlock)
	return nil
}

func traceLimits() (provenance.Limits, error) {
	limits := provenance.Limits{MaxDepth: 25, MaxTransactions: 10000}
	for name, destination := range map[string]*int{"TRACE_MAX_DEPTH": &limits.MaxDepth, "TRACE_MAX_TRANSACTIONS": &limits.MaxTransactions} {
		if value := env(name); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return limits, fmt.Errorf("%s must be a positive integer", name)
			}
			*destination = parsed
		}
	}
	return limits, nil
}

func (a application) trace(ctx context.Context, p paths) error {
	purchases, _, annotations, err := loadInputs(p.dataDir, true)
	if err != nil {
		return err
	}
	targets, chain, err := core.LoadDiscovery(filepath.Join(p.cacheDir, "discovery.json"))
	if err != nil {
		return fmt.Errorf("run discover first: %w", err)
	}
	client, err := rpcClient()
	if err != nil {
		return err
	}
	tip, err := client.ChainTip(ctx)
	if err != nil {
		return err
	}
	if tip != chain {
		return fmt.Errorf("chain tip changed since discovery; rerun discover before tracing")
	}
	source := core.NewSource(client, filepath.Join(p.cacheDir, "transactions.json"))
	if err := source.Load(); err != nil {
		return err
	}
	evidence, err := provenance.NewEvidence(purchases, annotations)
	if err != nil {
		return err
	}
	limits, err := traceLimits()
	if err != nil {
		return err
	}
	trace, traceErr := provenance.Trace(ctx, source, targets, evidence, chain, limits)
	if traceErr != nil {
		_ = source.Save()
		return traceErr
	}
	end, err := client.ChainTip(ctx)
	if err != nil {
		_ = source.Save()
		return err
	}
	if end != chain {
		_ = source.Save()
		return fmt.Errorf("chain tip changed during tracing; rerun discovery and trace")
	}
	trace.Network = "mainnet"
	if err := core.SaveTrace(filepath.Join(p.cacheDir, "trace.json"), trace); err != nil {
		return err
	}
	if err := source.Save(); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "traced %d transactions with %d boundaries across %d targets\n", len(trace.Transactions), len(trace.Boundaries), len(targets))
	return nil
}

func (a application) buildReport(p paths) (model.Report, error) {
	trace, err := core.LoadTrace(filepath.Join(p.cacheDir, "trace.json"))
	if err != nil {
		return model.Report{}, fmt.Errorf("run trace first: %w", err)
	}
	purchases, _, annotations, err := loadInputs(p.dataDir, true)
	if err != nil {
		return model.Report{}, err
	}
	report, err := provenance.Evaluate(trace, purchases.Purchases, annotations, time.Now())
	if err == nil {
		report.SoftwareVersion = model.SoftwareVersion
	}
	return report, err
}

func (a application) pdf(ctx context.Context, p paths) error {
	pdfCtx, cancel := context.WithTimeout(ctx, reportfiles.PDFTimeout())
	defer cancel()
	if err := reportfiles.PDF(pdfCtx, p.outDir, env("CHROMIUM")); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "wrote %s\n", filepath.Join(p.outDir, "report.pdf"))
	return nil
}

func (a application) report(ctx context.Context, p paths, server, includePDF bool) error {
	report, err := a.buildReport(p)
	if err != nil {
		return err
	}
	if err := reportfiles.WriteStructured(report, p.outDir, server); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "wrote report for %d UTXOs to %s\n", len(report.UTXOs), p.outDir)
	if includePDF {
		return a.pdf(ctx, p)
	}
	return nil
}

func (a application) ui(_ context.Context, p paths) error {
	report, err := a.buildReport(p)
	if err != nil {
		return err
	}
	address := env("SOF_LISTEN")
	server, err := uiserver.New(uiserver.Config{Address: address, AnnotationPath: filepath.Join(p.dataDir, "annotations.json"), Report: report,
		Rebuild: func() (model.Report, error) { return a.buildReport(p) }})
	if err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "Serving local report at http://%s (loopback only; Ctrl-C to stop)\n", server.Address())
	return server.ListenAndServe()
}

func (a application) export(p paths, purchases model.PurchaseFile) error {
	report, err := reportfiles.Load(filepath.Join(p.outDir, "report.json"))
	if err != nil {
		return err
	}
	if err := reportfiles.Bundle(report, purchases, p.dataDir, p.outDir); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "wrote %s\n", filepath.Join(p.outDir, "accountant.zip"))
	return nil
}

func (a application) example(ctx context.Context, p paths) error {
	loaded, err := fixtures.LoadDir(p.fixtures)
	if err != nil {
		return err
	}
	source, err := fixtures.NewSource(loaded)
	if err != nil {
		return err
	}
	var selected *fixtures.Fixture
	for i := range loaded {
		if loaded[i].TxID == firstTransfer {
			selected = &loaded[i]
		}
	}
	if selected == nil {
		return fmt.Errorf("first-transfer fixture is missing")
	}
	amount := int64(1000000000)
	purchases := model.PurchaseFile{SchemaVersion: 1, Purchases: []model.Purchase{{ID: "example-first-transfer", Date: time.Unix(selected.BlockTime, 0).UTC().Format(time.RFC3339), Provider: "Historical mainnet demonstration", TxIDs: []string{firstTransfer}, AmountSats: &amount, Notes: "A user-supplied purchase-style boundary used only to demonstrate report mechanics, not a claim of ownership or an actual ATM invoice."}}}
	owned := []model.TargetRef{{TxID: firstTransfer, Vout: 0, ValueSats: 1000000000, OwnerIDs: []string{"example"}, Labels: []string{"historical demonstration"}}}
	chain := model.Chain{BestBlock: selected.BlockHash, BestHeight: selected.BlockHeight}
	evidence, err := provenance.NewEvidence(purchases, input.EmptyAnnotations())
	if err != nil {
		return err
	}
	trace, err := provenance.Trace(ctx, source, owned, evidence, chain, provenance.Limits{})
	if err != nil {
		return err
	}
	trace.Network = "mainnet"
	report, err := provenance.Evaluate(trace, purchases.Purchases, input.EmptyAnnotations(), time.Unix(selected.BlockTime, 0))
	if err != nil {
		return err
	}
	report.SoftwareVersion = model.SoftwareVersion
	report.Warnings = append(report.Warnings, "Offline historical mainnet demonstration; these outputs are not claimed as current owned UTXOs")
	if err := reportfiles.WriteStructured(report, p.outDir, false); err != nil {
		return err
	}
	return a.pdf(ctx, p)
}

func (a application) testNode(ctx context.Context, p paths) error {
	loaded, err := fixtures.LoadDir(p.fixtures)
	if err != nil {
		return err
	}
	client, err := rpcClient()
	if err != nil {
		return err
	}
	tip, err := client.ChainTip(ctx)
	if err != nil {
		return err
	}
	source := core.NewSource(client, "")
	for _, fixture := range loaded {
		if err := source.Prefetch(ctx, fixture.TxID, fixture.BlockHash, fixture.BlockHeight); err != nil {
			return fmt.Errorf("verify %s: %w", fixture.TxID, err)
		}
		tx, err := source.Transaction(ctx, fixture.TxID)
		if err != nil {
			return err
		}
		expected, err := core.DecodeTransaction(fixture.Hex)
		if err != nil {
			return err
		}
		if !bytes.Equal([]byte(tx.Hex), []byte(expected.Hex)) {
			return fmt.Errorf("transaction %s differs from committed fixture", fixture.TxID)
		}
		fmt.Fprintf(a.stdout, "verified %s at block %d\n", fixture.TxID, fixture.BlockHeight)
	}
	emptyEvidence, err := provenance.NewEvidence(model.PurchaseFile{SchemaVersion: 1, Purchases: []model.Purchase{{ID: "intentionally-no-match"}}}, input.EmptyAnnotations())
	if err != nil {
		return err
	}
	targets := []model.TargetRef{{TxID: firstTransfer, Vout: 0, ValueSats: 1000000000}}
	trace, err := provenance.Trace(ctx, source, targets, emptyEvidence, tip, provenance.Limits{MaxDepth: 3, MaxTransactions: 10})
	if err != nil {
		return err
	}
	if len(trace.Transactions) != 2 || trace.Incomplete || len(trace.Boundaries) != 1 || trace.Boundaries[0].Reason != "coinbase" {
		return fmt.Errorf("real first-transfer ancestry did not terminate correctly at its coinbase")
	}
	if _, ok := trace.Transactions[firstTransferParent]; !ok {
		return fmt.Errorf("real first-transfer parent was not traced")
	}
	fmt.Fprintln(a.stdout, "verified first-transfer backward ancestry through its block-9 coinbase parent")
	return nil
}
