package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"tracey-sof/internal/core"
	"tracey-sof/internal/fixtures"
	"tracey-sof/internal/model"
	"tracey-sof/internal/provenance"
)

func TestExampleInjectedFailureStages(t *testing.T) {
	app := application{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	t.Run("source", func(t *testing.T) {
		deps := defaultExampleDependencies()
		deps.newSource = func([]fixtures.Fixture) (fixtures.Source, error) { return nil, errors.New("source failure") }
		if err := app.exampleWith(context.Background(), paths{fixtures: cliFixtureDirectory()}, deps); err == nil || !strings.Contains(err.Error(), "source failure") {
			t.Fatalf("source failure = %v", err)
		}
	})
	t.Run("evidence", func(t *testing.T) {
		deps := defaultExampleDependencies()
		deps.newEvidence = func(model.PurchaseFile, model.AnnotationFile) (*provenance.Evidence, error) {
			return nil, errors.New("evidence failure")
		}
		if err := app.exampleWith(context.Background(), paths{fixtures: cliFixtureDirectory()}, deps); err == nil || !strings.Contains(err.Error(), "evidence failure") {
			t.Fatalf("evidence failure = %v", err)
		}
	})
	t.Run("evaluate", func(t *testing.T) {
		deps := defaultExampleDependencies()
		deps.evaluate = func(model.Trace, []model.Purchase, model.AnnotationFile, time.Time) (model.Report, error) {
			return model.Report{}, errors.New("evaluate failure")
		}
		if err := app.exampleWith(context.Background(), paths{fixtures: cliFixtureDirectory()}, deps); err == nil || !strings.Contains(err.Error(), "evaluate failure") {
			t.Fatalf("evaluate failure = %v", err)
		}
	})
}

type injectedNodeSource struct {
	transactions   fixtures.Source
	prefetchErr    error
	transactionErr error
	mismatch       bool
}

func (s *injectedNodeSource) Prefetch(context.Context, string, string, int64) error {
	return s.prefetchErr
}
func (s *injectedNodeSource) Transaction(_ context.Context, txid string) (model.Transaction, error) {
	if s.transactionErr != nil {
		return model.Transaction{}, s.transactionErr
	}
	if s.mismatch {
		return model.Transaction{TxID: txid, Hex: "different"}, nil
	}
	transaction, err := s.transactions.Transaction(context.Background(), txid)
	return transaction, err
}

func newInjectedNodeSource(t *testing.T) *injectedNodeSource {
	t.Helper()
	source, err := fixtures.NewSource(loadCLIFixtures(t))
	if err != nil {
		t.Fatal(err)
	}
	return &injectedNodeSource{transactions: source}
}

func validNodeTrace(t *testing.T) model.Trace {
	t.Helper()
	loaded := loadCLIFixtures(t)
	transactions := map[string]model.Transaction{}
	for _, fixture := range loaded {
		if fixture.TxID == firstTransfer || fixture.TxID == firstTransferParent {
			transaction, err := core.DecodeTransaction(fixture.Hex)
			if err != nil {
				t.Fatal(err)
			}
			transactions[fixture.TxID] = transaction
		}
	}
	return model.Trace{Transactions: transactions, Boundaries: []model.Boundary{{NodeID: "tx:" + firstTransferParent, Reason: "coinbase"}}}
}

func serveTestNodeDeps(t *testing.T, source *injectedNodeSource) testNodeDependencies {
	t.Helper()
	deps := defaultTestNodeDependencies()
	deps.newSource = func(*core.Client) nodeFixtureSource { return source }
	return deps
}

func TestTestNodeInjectedFailureStages(t *testing.T) {
	app := application{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	p := newCLIPaths(t, false)
	t.Run("transaction error", func(t *testing.T) {
		newCLIFakeCore(t)
		source := newInjectedNodeSource(t)
		source.transactionErr = errors.New("transaction failure")
		if err := app.testNodeWith(context.Background(), p, serveTestNodeDeps(t, source)); err == nil || !strings.Contains(err.Error(), "transaction failure") {
			t.Fatalf("transaction failure = %v", err)
		}
	})
	t.Run("hex mismatch", func(t *testing.T) {
		newCLIFakeCore(t)
		source := newInjectedNodeSource(t)
		source.mismatch = true
		if err := app.testNodeWith(context.Background(), p, serveTestNodeDeps(t, source)); err == nil || !strings.Contains(err.Error(), "differs from committed fixture") {
			t.Fatalf("mismatch error = %v", err)
		}
	})
	t.Run("evidence error", func(t *testing.T) {
		newCLIFakeCore(t)
		source := newInjectedNodeSource(t)
		deps := serveTestNodeDeps(t, source)
		deps.newEvidence = func(model.PurchaseFile, model.AnnotationFile) (*provenance.Evidence, error) {
			return nil, errors.New("evidence failure")
		}
		if err := app.testNodeWith(context.Background(), p, deps); err == nil || !strings.Contains(err.Error(), "evidence failure") {
			t.Fatalf("evidence error = %v", err)
		}
	})
	t.Run("first trace error", func(t *testing.T) {
		newCLIFakeCore(t)
		source := newInjectedNodeSource(t)
		deps := serveTestNodeDeps(t, source)
		deps.trace = func(context.Context, provenance.TransactionSource, []model.TargetRef, *provenance.Evidence, model.Chain, provenance.Limits) (model.Trace, error) {
			return model.Trace{}, errors.New("trace failure")
		}
		if err := app.testNodeWith(context.Background(), p, deps); err == nil || !strings.Contains(err.Error(), "trace failure") {
			t.Fatalf("trace error = %v", err)
		}
	})
	t.Run("malformed trace", func(t *testing.T) {
		newCLIFakeCore(t)
		source := newInjectedNodeSource(t)
		deps := serveTestNodeDeps(t, source)
		deps.trace = func(context.Context, provenance.TransactionSource, []model.TargetRef, *provenance.Evidence, model.Chain, provenance.Limits) (model.Trace, error) {
			return model.Trace{}, nil
		}
		if err := app.testNodeWith(context.Background(), p, deps); err == nil || !strings.Contains(err.Error(), "did not terminate") {
			t.Fatalf("malformed trace error = %v", err)
		}
	})
	t.Run("missing parent", func(t *testing.T) {
		newCLIFakeCore(t)
		source := newInjectedNodeSource(t)
		deps := serveTestNodeDeps(t, source)
		trace := validNodeTrace(t)
		delete(trace.Transactions, firstTransferParent)
		trace.Transactions[strings.Repeat("a", 64)] = model.Transaction{TxID: strings.Repeat("a", 64)}
		deps.trace = func(context.Context, provenance.TransactionSource, []model.TargetRef, *provenance.Evidence, model.Chain, provenance.Limits) (model.Trace, error) {
			return trace, nil
		}
		if err := app.testNodeWith(context.Background(), p, deps); err == nil || !strings.Contains(err.Error(), "parent was not traced") {
			t.Fatalf("missing parent error = %v", err)
		}
	})
	t.Run("policy trace error", func(t *testing.T) {
		newCLIFakeCore(t)
		source := newInjectedNodeSource(t)
		deps := serveTestNodeDeps(t, source)
		calls := 0
		trace := validNodeTrace(t)
		deps.trace = func(context.Context, provenance.TransactionSource, []model.TargetRef, *provenance.Evidence, model.Chain, provenance.Limits) (model.Trace, error) {
			calls++
			if calls == 2 {
				return model.Trace{}, errors.New("policy trace failure")
			}
			return trace, nil
		}
		if err := app.testNodeWith(context.Background(), p, deps); err == nil || !strings.Contains(err.Error(), "policy trace failure") {
			t.Fatalf("policy trace error = %v", err)
		}
	})
	t.Run("policy evaluation error", func(t *testing.T) {
		newCLIFakeCore(t)
		source := newInjectedNodeSource(t)
		deps := serveTestNodeDeps(t, source)
		trace := validNodeTrace(t)
		deps.trace = func(context.Context, provenance.TransactionSource, []model.TargetRef, *provenance.Evidence, model.Chain, provenance.Limits) (model.Trace, error) {
			return trace, nil
		}
		deps.evaluate = func(model.Trace, []model.Purchase, model.AnnotationFile, time.Time) (model.Report, error) {
			return model.Report{}, errors.New("policy evaluate failure")
		}
		if err := app.testNodeWith(context.Background(), p, deps); err == nil || !strings.Contains(err.Error(), "policy evaluate failure") {
			t.Fatalf("policy evaluate error = %v", err)
		}
	})
	t.Run("policy assertion", func(t *testing.T) {
		newCLIFakeCore(t)
		source := newInjectedNodeSource(t)
		deps := serveTestNodeDeps(t, source)
		trace := validNodeTrace(t)
		deps.trace = func(context.Context, provenance.TransactionSource, []model.TargetRef, *provenance.Evidence, model.Chain, provenance.Limits) (model.Trace, error) {
			return trace, nil
		}
		deps.evaluate = func(model.Trace, []model.Purchase, model.AnnotationFile, time.Time) (model.Report, error) {
			return model.Report{}, nil
		}
		if err := app.testNodeWith(context.Background(), p, deps); err == nil || !strings.Contains(err.Error(), "review-required") {
			t.Fatalf("policy assertion error = %v", err)
		}
	})
}
