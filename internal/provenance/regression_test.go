package provenance

import (
	"reflect"
	"testing"
	"time"

	"tracey-sof/internal/model"
)

func TestEvaluateSuccessfulPurchaseBoundariesDoNotWarn(t *testing.T) {
	transactionPurchaseTx := testTxID("a")
	outpointPurchaseTx := testTxID("b")
	transactionPurchaseTarget := testTarget(transactionPurchaseTx, 0)
	trace := model.Trace{
		Targets: []model.TargetRef{transactionPurchaseTarget, testTarget(outpointPurchaseTx, 2)},
		Transactions: map[string]model.Transaction{
			transactionPurchaseTx: testTransaction(transactionPurchaseTx, []model.Input{testInput(testTxID("c"), 0)}),
		},
		Boundaries: []model.Boundary{
			{NodeID: transactionNodeID(transactionPurchaseTx), Reason: reasonPurchase, Transaction: transactionPurchaseTx, Detail: "confirmed purchase transaction", PurchaseIDs: []string{"tx-purchase"}},
			{NodeID: outpointNodeID(testOutpoint(outpointPurchaseTx, 2)), Reason: reasonPurchase, Outpoint: testOutpoint(outpointPurchaseTx, 2).String(), Detail: "confirmed purchase outpoint", PurchaseIDs: []string{"outpoint-purchase"}},
		},
	}
	report, err := Evaluate(trace, nil, model.AnnotationFile{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Warnings) != 0 {
		t.Fatalf("successful purchases produced warnings: %v", report.Warnings)
	}
	for _, utxo := range report.UTXOs {
		if utxo.Status != documentedStatus || utxo.Trace != completeTraceStatus {
			t.Fatalf("successful purchase classification = %+v", utxo)
		}
	}
}

func TestEvaluateSortsSameTransactionByVoutAndDeduplicatesPurchaseIDs(t *testing.T) {
	txid := testTxID("a")
	opOne := testOutpoint(txid, 1)
	opTwo := testOutpoint(txid, 2)
	targetTwo := testTarget(txid, 2)
	targetOne := testTarget(txid, 1)
	trace := model.Trace{
		Targets: []model.TargetRef{targetTwo, targetOne},
		Boundaries: []model.Boundary{
			{NodeID: outpointNodeID(opTwo), Reason: reasonPurchase, PurchaseIDs: []string{"z", "a", "a"}},
			{NodeID: outpointNodeID(opOne), Reason: reasonPurchase, PurchaseIDs: []string{"a"}},
		},
	}
	report, err := Evaluate(trace, nil, model.AnnotationFile{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.UTXOs) != 2 || report.UTXOs[0].Vout != 1 || report.UTXOs[1].Vout != 2 {
		t.Fatalf("same-transaction target sorting = %+v", report.UTXOs)
	}
	if !reflect.DeepEqual(report.UTXOs[1].PurchaseIDs, []string{"a", "z"}) {
		t.Fatalf("purchase IDs not sorted and unique: %v", report.UTXOs[1].PurchaseIDs)
	}
}

func TestEvaluateRejectsEmptyPrevoutTransactionID(t *testing.T) {
	trace := validEvaluationTrace()
	txA := trace.Targets[0].TxID
	transaction := trace.Transactions[txA]
	transaction.Inputs[0].PrevOut = model.Outpoint{}
	trace.Transactions[txA] = transaction
	trace.Edges[0].PrevOut = model.Outpoint{}
	if _, err := Evaluate(trace, nil, model.AnnotationFile{}, time.Time{}); err == nil {
		t.Fatal("empty previous-output txid was accepted")
	}
}
