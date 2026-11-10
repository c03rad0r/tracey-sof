package provenance

import (
	"context"
	"testing"
	"time"

	"tracey-sof/internal/fixtures"
	"tracey-sof/internal/model"
)

func TestPolicyAgainstRealMainnetFixture(t *testing.T) {
	loaded, err := fixtures.LoadDir("../../testdata/mainnet")
	if err != nil {
		t.Fatal(err)
	}
	source, err := fixtures.NewSource(loaded)
	if err != nil {
		t.Fatal(err)
	}
	const firstTransfer = "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16"
	targets := []model.TargetRef{{TxID: firstTransfer, Vout: 0, ValueSats: 1000000000}}
	trace, err := Trace(context.Background(), source, targets, nil, model.Chain{BestBlock: "public-fixture", BestHeight: 170}, Limits{StopBeforeBlockHeight: 171})
	if err != nil {
		t.Fatal(err)
	}
	boundary := findBoundary(trace.Boundaries, transactionNodeID(firstTransfer), reasonPolicyBlockHeight)
	if boundary == nil || boundary.PolicyRule != "stop_before_block_height" || !boundary.ReviewRequired || !trace.Incomplete {
		t.Fatalf("real fixture policy boundary = %+v", trace.Boundaries)
	}
	if _, loadedParent := trace.Transactions["0437cd7f8525ceed2324359c2d0ba26006d92d856a9c20fa0241106ee5a597c9"]; loadedParent {
		t.Fatal("real fixture parent was fetched after the policy cutoff")
	}
	report, err := Evaluate(trace, nil, model.AnnotationFile{}, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if !report.UTXOs[0].ReviewRequired || report.UTXOs[0].Trace != incompleteTraceStatus || report.Policy.StopBeforeBlockHeight != 171 {
		t.Fatalf("real fixture report = %+v / %+v", report.UTXOs[0], report.Policy)
	}
}
