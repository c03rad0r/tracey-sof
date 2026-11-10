package main

import (
	"context"
	"time"

	"tracey-sof/internal/core"
	"tracey-sof/internal/fixtures"
	"tracey-sof/internal/model"
	"tracey-sof/internal/provenance"
)

type exampleDependencies struct {
	newSource   func([]fixtures.Fixture) (fixtures.Source, error)
	newEvidence func(model.PurchaseFile, model.AnnotationFile) (*provenance.Evidence, error)
	evaluate    func(model.Trace, []model.Purchase, model.AnnotationFile, time.Time) (model.Report, error)
}

func defaultExampleDependencies() exampleDependencies {
	return exampleDependencies{
		newSource:   fixtures.NewSource,
		newEvidence: provenance.NewEvidence,
		evaluate:    provenance.Evaluate,
	}
}

type nodeFixtureSource interface {
	Prefetch(context.Context, string, string, int64) error
	provenance.TransactionSource
}

type testNodeDependencies struct {
	newSource   func(*core.Client) nodeFixtureSource
	newEvidence func(model.PurchaseFile, model.AnnotationFile) (*provenance.Evidence, error)
	trace       func(context.Context, provenance.TransactionSource, []model.TargetRef, *provenance.Evidence, model.Chain, provenance.Limits) (model.Trace, error)
	evaluate    func(model.Trace, []model.Purchase, model.AnnotationFile, time.Time) (model.Report, error)
}

func defaultTestNodeDependencies() testNodeDependencies {
	return testNodeDependencies{
		newSource:   func(client *core.Client) nodeFixtureSource { return core.NewSource(client, "") },
		newEvidence: provenance.NewEvidence,
		trace:       provenance.Trace,
		evaluate:    provenance.Evaluate,
	}
}
