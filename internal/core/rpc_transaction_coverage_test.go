package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
)

type coverageRoundTripFunc func(*http.Request) (*http.Response, error)

func (f coverageRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type coverageErrorBody struct {
	closed bool
}

func (b *coverageErrorBody) Read([]byte) (int, error) { return 0, errCoveragePersistence }
func (b *coverageErrorBody) Close() error {
	b.closed = true
	return nil
}

type coverageZeroReader struct{}

func (coverageZeroReader) Read(buffer []byte) (int, error) {
	clear(buffer)
	return len(buffer), nil
}

type coverageTrackingBody struct {
	io.Reader
	closed bool
}

func (b *coverageTrackingBody) Close() error {
	b.closed = true
	return nil
}

func TestCoverageRPCClientConstructionAndAuthentication(t *testing.T) {
	client, err := NewClient(Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if client.http == nil || client.http.Transport == nil || client.timeout != 2*time.Minute || client.scanTimeout != time.Hour {
		t.Fatalf("default client was not fully initialized: %+v", client)
	}

	request, err := http.NewRequest(http.MethodPost, "http://localhost", nil)
	if err != nil {
		t.Fatal(err)
	}
	authenticated := &Client{username: "unit", password: "secret"}
	if err := authenticated.authenticate(request); err != nil {
		t.Fatal(err)
	}
	user, password, ok := request.BasicAuth()
	if !ok || user != "unit" || password != "secret" {
		t.Errorf("basic authentication = %q/%q, %t", user, password, ok)
	}
}

func TestCoverageRPCRequestAndReadFailures(t *testing.T) {
	t.Run("request construction", func(t *testing.T) {
		client := &Client{baseURL: "://", http: &http.Client{}}
		err := client.Call(context.Background(), "getbestblockhash", []any{}, nil)
		if err == nil || !strings.Contains(err.Error(), "create getbestblockhash request") {
			t.Errorf("Call() error = %v", err)
		}
	})

	t.Run("response read", func(t *testing.T) {
		body := &coverageErrorBody{}
		httpClient := &http.Client{Transport: coverageRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: body}, nil
		})}
		client, err := NewClient(Config{URL: "http://node", Timeout: time.Second}, httpClient)
		if err != nil {
			t.Fatal(err)
		}
		err = client.Call(context.Background(), "getbestblockhash", []any{}, nil)
		if !errors.Is(err, errCoveragePersistence) || !strings.Contains(err.Error(), "read getbestblockhash response") {
			t.Errorf("Call() error = %v", err)
		}
		if !body.closed {
			t.Error("response body was not closed after a read failure")
		}
	})
}

func TestCoverageRPCResponseSizeGuard(t *testing.T) {
	body := &coverageTrackingBody{Reader: io.LimitReader(coverageZeroReader{}, maxRPCResponse+1)}
	httpClient := &http.Client{Transport: coverageRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: body}, nil
	})}
	client, err := NewClient(Config{URL: "http://node", Timeout: time.Minute}, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	err = client.Call(context.Background(), "getbestblockhash", []any{}, nil)
	if err == nil || !strings.Contains(err.Error(), "response exceeds") {
		t.Fatalf("Call() error = %v, want size guard", err)
	}
	if !body.closed {
		t.Error("oversized response body was not closed")
	}
}

func TestCoverageTransactionDecoderAndScriptFallbackErrors(t *testing.T) {
	fixture := readFixtureFile(t, firstTransfer)
	_, err := decodeTransaction(fixture.Hex, func([]byte) (string, string, error) {
		return "", "", errCoveragePersistence
	})
	if !errors.Is(err, errCoveragePersistence) || !strings.Contains(err.Error(), "decode output 0 script") {
		t.Errorf("decodeTransaction() error = %v", err)
	}

	_, _, err = scriptAddressWith([]byte{txscript.OP_TRUE}, func([]byte, *chaincfg.Params) (txscript.ScriptClass, []btcutil.Address, int, error) {
		return txscript.NonStandardTy, nil, 0, errCoveragePersistence
	})
	if !errors.Is(err, errCoveragePersistence) {
		t.Errorf("scriptAddressWith() error = %v", err)
	}

	_, err = addressScriptWith("1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa", func(btcutil.Address) ([]byte, error) {
		return nil, errCoveragePersistence
	})
	if !errors.Is(err, errCoveragePersistence) {
		t.Errorf("addressScriptWith() error = %v", err)
	}
}
