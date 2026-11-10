package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"tracey-sof/internal/input"
	"tracey-sof/internal/model"
)

const (
	serverTestTxID    = "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16"
	serverTestAddress = "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"
)

func serverTestReport(marker string) model.Report {
	return model.Report{
		SchemaVersion: model.SchemaVersion,
		Network:       "mainnet",
		GeneratedAt:   marker,
		Chain: model.Chain{
			BestBlock:  "00000000d1145790a8694403d4063f323d499e655c83426834d4ce2f8dd4a2ee",
			BestHeight: 170,
		},
		UTXOs: []model.Target{{
			Outpoint:  model.Outpoint{TxID: serverTestTxID, Vout: 1},
			TxID:      serverTestTxID,
			Vout:      1,
			Address:   serverTestAddress,
			ValueSats: 12345,
			Status:    "documented",
			Trace:     "complete",
		}},
		Summary:     model.Summary{TotalUTXOs: 1, TotalSats: 12345, DocumentedUTXOs: 1, DocumentedSats: 12345},
		Annotations: input.EmptyAnnotations(),
	}
}

func newHandlerServer(t *testing.T, annotationPath string, report model.Report, rebuild func() (model.Report, error)) *Server {
	t.Helper()
	s, err := New(Config{Address: "127.0.0.1:0", AnnotationPath: annotationPath, Report: report, Rebuild: rebuild})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func serveRequest(t *testing.T, s *Server, request *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(response, request)
	result := response.Result()
	wantHeaders := map[string]string{
		"Content-Security-Policy": "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
		"Cache-Control":           "no-store",
	}
	for name, want := range wantHeaders {
		if got := result.Header.Get(name); got != want {
			t.Errorf("%s %s: %s = %q, want %q", request.Method, request.URL.Path, name, got, want)
		}
	}
	return response
}

func getPath(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	return serveRequest(t, s, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765"+path, nil))
}

func postAnnotation(t *testing.T, s *Server, body, contentType, origin string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765/api/annotations", strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	return serveRequest(t, s, request)
}

func TestNewLoopbackOnlyConfiguration(t *testing.T) {
	annotationPath := filepath.Join(t.TempDir(), "annotations.json")
	t.Run("default address", func(t *testing.T) {
		s, err := New(Config{AnnotationPath: annotationPath, Report: serverTestReport("default")})
		if err != nil {
			t.Fatal(err)
		}
		if got, want := s.Address(), "127.0.0.1:8765"; got != want {
			t.Fatalf("Address() = %q, want %q", got, want)
		}
		if s.server.Handler == nil || s.server.ReadHeaderTimeout != 10*time.Second || s.server.IdleTimeout != 2*time.Minute {
			t.Fatalf("server HTTP configuration was not initialized: %#v", s.server)
		}
	})
	for _, address := range []string{"127.0.0.2:9000", "[::1]:0"} {
		t.Run("loopback "+address, func(t *testing.T) {
			s, err := New(Config{Address: address, AnnotationPath: annotationPath})
			if err != nil {
				t.Fatal(err)
			}
			if s.Address() != address {
				t.Fatalf("Address() = %q, want %q", s.Address(), address)
			}
		})
	}

	for _, test := range []struct {
		name    string
		address string
		path    string
		wantErr string
	}{
		{name: "missing port", address: "127.0.0.1", path: annotationPath, wantErr: "invalid UI listen address"},
		{name: "missing host", address: ":8765", path: annotationPath, wantErr: "numeric loopback"},
		{name: "hostname", address: "localhost:8765", path: annotationPath, wantErr: "numeric loopback"},
		{name: "unspecified IPv4", address: "0.0.0.0:8765", path: annotationPath, wantErr: "numeric loopback"},
		{name: "public IPv4", address: "192.0.2.10:8765", path: annotationPath, wantErr: "numeric loopback"},
		{name: "unspecified IPv6", address: "[::]:8765", path: annotationPath, wantErr: "numeric loopback"},
		{name: "annotation path required", address: "127.0.0.1:0", path: "", wantErr: "annotation path is required"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, err := New(Config{Address: test.address, AnnotationPath: test.path})
			if err == nil || !strings.Contains(err.Error(), test.wantErr) || s != nil {
				t.Fatalf("New() = %#v, %v; want nil server and error containing %q", s, err, test.wantErr)
			}
		})
	}
}

func TestReportAndIndexResponses(t *testing.T) {
	report := serverTestReport("2026-01-02T03:04:05Z")
	s := newHandlerServer(t, filepath.Join(t.TempDir(), "annotations.json"), report, nil)

	first := getPath(t, s, "/api/report")
	second := getPath(t, s, "/api/report")
	if first.Code != http.StatusOK || first.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("GET report status/content type = %d, %q", first.Code, first.Header().Get("Content-Type"))
	}
	if first.Body.String() != second.Body.String() {
		t.Fatalf("report responses are not deterministic:\n%s\n%s", first.Body.String(), second.Body.String())
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := first.Body.String(), string(encoded)+"\n"; got != want {
		t.Fatalf("report response differs from canonical model JSON:\ngot:  %s\nwant: %s", got, want)
	}
	var decoded model.Report
	if err := json.Unmarshal(first.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("report response contains invalid Target JSON: %v", err)
	}
	if len(decoded.UTXOs) != 1 || decoded.UTXOs[0].Outpoint != report.UTXOs[0].Outpoint || decoded.UTXOs[0].Key() != serverTestTxID+":1" {
		t.Fatalf("target outpoint did not survive response decoding: %#v", decoded.UTXOs)
	}

	firstHTML := getPath(t, s, "/")
	secondHTML := getPath(t, s, "/")
	if firstHTML.Code != http.StatusOK || firstHTML.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("GET index status/content type = %d, %q", firstHTML.Code, firstHTML.Header().Get("Content-Type"))
	}
	if firstHTML.Body.String() != secondHTML.Body.String() {
		t.Fatal("index responses are not deterministic")
	}
	for _, want := range []string{"<!doctype html", report.GeneratedAt, serverTestTxID + ":1", `"server":true`, `"save_url":"/api/annotations"`} {
		if !strings.Contains(firstHTML.Body.String(), want) {
			t.Errorf("index does not contain %q", want)
		}
	}
	for _, unwanted := range []string{"/*__CSS__*/", "/*__GRAPH_JS__*/", "/*__APP_JS__*/", "/*__REPORT_JSON__*/", "/*__SERVER_CONFIG__*/"} {
		if strings.Contains(firstHTML.Body.String(), unwanted) {
			t.Errorf("index contains unresolved placeholder %q", unwanted)
		}
	}
}

func TestMuxResponses(t *testing.T) {
	s := newHandlerServer(t, filepath.Join(t.TempDir(), "annotations.json"), serverTestReport("mux"), nil)

	for _, test := range []struct {
		method string
		path   string
		status int
	}{
		{method: http.MethodGet, path: "/missing", status: http.StatusNotFound},
		{method: http.MethodPost, path: "/api/report", status: http.StatusMethodNotAllowed},
		{method: http.MethodGet, path: "/api/annotations", status: http.StatusMethodNotAllowed},
	} {
		response := serveRequest(t, s, httptest.NewRequest(test.method, "http://127.0.0.1:8765"+test.path, nil))
		if response.Code != test.status {
			t.Errorf("%s %s returned %d, want %d", test.method, test.path, response.Code, test.status)
		}
	}
}

func TestListenAndServeAndShutdown(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := New(Config{Address: address, AnnotationPath: filepath.Join(t.TempDir(), "annotations.json"), Report: serverTestReport("network")})
	if err != nil {
		t.Fatal(err)
	}
	serveResult := make(chan error, 1)
	go func() { serveResult <- s.ListenAndServe() }()

	client := &http.Client{Timeout: 100 * time.Millisecond}
	deadline := time.Now().Add(3 * time.Second)
	var response *http.Response
	for response == nil && time.Now().Before(deadline) {
		select {
		case err := <-serveResult:
			t.Fatalf("ListenAndServe returned before shutdown: %v", err)
		default:
		}
		response, err = client.Get("http://" + address + "/api/report")
		if err != nil {
			time.Sleep(5 * time.Millisecond)
			continue
		}
	}
	if response == nil {
		_ = s.Shutdown(context.Background())
		t.Fatalf("server did not become ready: %v", err)
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("reading network response: read=%v close=%v", readErr, closeErr)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), `"generated_at":"network"`) {
		t.Fatalf("network report response = %d %s", response.StatusCode, body)
	}
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("network response lost security headers: %#v", response.Header)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-serveResult:
		if err != nil {
			t.Fatalf("ListenAndServe after graceful Shutdown = %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ListenAndServe did not return after Shutdown")
	}
}

func TestListenErrorsAndShutdownBeforeServe(t *testing.T) {
	s, err := New(Config{Address: "127.0.0.1:70000", AnnotationPath: filepath.Join(t.TempDir(), "annotations.json")})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ListenAndServe(); err == nil || errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("ListenAndServe(invalid port) = %v, want non-ErrServerClosed listen error", err)
	}

	stopped, err := New(Config{Address: "127.0.0.1:0", AnnotationPath: filepath.Join(t.TempDir(), "annotations.json")})
	if err != nil {
		t.Fatal(err)
	}
	if err := stopped.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown before Serve = %v", err)
	}
	if err := stopped.ListenAndServe(); err != nil {
		t.Fatalf("ListenAndServe on already shut down server = %v, want nil", err)
	}
}

func TestAnnotationOriginAndContentTypeEnforcement(t *testing.T) {
	validBody := `{"transaction":"` + serverTestTxID + `","label":"purchase"}`
	for _, test := range []struct {
		name        string
		origin      string
		contentType string
		status      int
	}{
		{name: "no origin", contentType: "application/json", status: http.StatusOK},
		{name: "same origin", origin: "http://127.0.0.1:8765", contentType: "application/json", status: http.StatusOK},
		{name: "same origin trailing slash", origin: "http://127.0.0.1:8765/", contentType: "application/json", status: http.StatusOK},
		{name: "surrounding content type whitespace", contentType: "  application/json  ", status: http.StatusOK},
		{name: "cross origin", origin: "http://example.com", contentType: "application/json", status: http.StatusForbidden},
		{name: "wrong scheme", origin: "https://127.0.0.1:8765", contentType: "application/json", status: http.StatusForbidden},
		{name: "malformed origin", origin: "http://[::1", contentType: "application/json", status: http.StatusForbidden},
		{name: "wrong port", origin: "http://127.0.0.1:9999", contentType: "application/json", status: http.StatusForbidden},
		{name: "missing content type", status: http.StatusForbidden},
		{name: "text content type", contentType: "text/plain", status: http.StatusForbidden},
		{name: "content type parameters rejected", origin: "http://127.0.0.1:8765", contentType: "application/json; charset=utf-8", status: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "annotations.json")
			s := newHandlerServer(t, path, serverTestReport("origin"), nil)
			response := postAnnotation(t, s, validBody, test.contentType, test.origin)
			if response.Code != test.status {
				t.Fatalf("POST returned %d, want %d; body: %s", response.Code, test.status, response.Body.String())
			}
			_, err := os.Stat(path)
			if test.status == http.StatusForbidden {
				if response.Body.String() != "same-origin application/json request required\n" {
					t.Errorf("forbidden response body = %q", response.Body.String())
				}
				if !errors.Is(err, os.ErrNotExist) {
					t.Errorf("forbidden request persisted annotations: %v", err)
				}
			} else if err != nil {
				t.Errorf("accepted request did not persist annotations: %v", err)
			}
		})
	}
}

func TestAnnotationBodyAndSelectorErrors(t *testing.T) {
	oversized := `{"transaction":"` + serverTestTxID + `","label":"` + strings.Repeat("x", 65536) + `"}`
	for _, test := range []struct {
		name string
		body string
		want string
	}{
		{name: "empty body", body: "", want: "invalid annotation JSON\n"},
		{name: "malformed JSON", body: `{"transaction":`, want: "invalid annotation JSON\n"},
		{name: "JSON array", body: `[]`, want: "invalid annotation JSON\n"},
		{name: "unknown field", body: `{"transaction":"` + serverTestTxID + `","extra":true}`, want: "invalid annotation JSON\n"},
		{name: "wrong field type", body: `{"transaction":"` + serverTestTxID + `","label":42}`, want: "invalid annotation JSON\n"},
		{name: "oversized body", body: oversized, want: "invalid annotation JSON\n"},
		{name: "two objects", body: `{}` + ` {}`, want: "one JSON object required\n"},
		{name: "second JSON value", body: `{}` + ` null`, want: "one JSON object required\n"},
		{name: "trailing garbage", body: `{}` + ` garbage`, want: "one JSON object required\n"},
		{name: "neither selector", body: `{"label":"missing"}`, want: "exactly one transaction or address is required\n"},
		{name: "null request", body: `null`, want: "exactly one transaction or address is required\n"},
		{name: "empty selectors", body: `{"transaction":"","address":""}`, want: "exactly one transaction or address is required\n"},
		{name: "both selectors", body: `{"transaction":"` + serverTestTxID + `","address":"` + serverTestAddress + `"}`, want: "exactly one transaction or address is required\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "annotations.json")
			s := newHandlerServer(t, path, serverTestReport("body errors"), nil)
			response := postAnnotation(t, s, test.body, "application/json", "http://127.0.0.1:8765")
			if response.Code != http.StatusBadRequest || response.Body.String() != test.want {
				t.Fatalf("POST returned %d %q, want 400 %q", response.Code, response.Body.String(), test.want)
			}
			if response.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
				t.Errorf("error Content-Type = %q", response.Header().Get("Content-Type"))
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("invalid request persisted annotations: %v", err)
			}
		})
	}
}

func TestAnnotationsPersistTransactionAndAddress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "nested", "annotations.json")
	s := newHandlerServer(t, path, serverTestReport("persistence"), nil)

	transactionBody := `{"transaction":"` + serverTestTxID + `","label":"purchase","note":"exchange receipt"}` + " \n\t"
	transactionResponse := postAnnotation(t, s, transactionBody, "application/json", "http://127.0.0.1:8765/")
	if transactionResponse.Code != http.StatusOK {
		t.Fatalf("transaction save returned %d: %s", transactionResponse.Code, transactionResponse.Body.String())
	}
	if got := transactionResponse.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("transaction response Content-Type = %q", got)
	}
	if got, want := transactionResponse.Body.String(), `{"annotation":{"label":"purchase","note":"exchange receipt"},"ok":true}`+"\n"; got != want {
		t.Errorf("transaction response = %q, want %q", got, want)
	}
	wantFile := input.EmptyAnnotations()
	wantFile.Transactions[serverTestTxID] = model.Annotation{Label: "purchase", Note: "exchange receipt"}
	assertPersistedAnnotations(t, path, wantFile)
	firstBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("annotation file mode = %o, want 600", got)
	}

	addressBody := `{"address":"` + serverTestAddress + `","label":"cold wallet","note":"self custody"}`
	addressResponse := postAnnotation(t, s, addressBody, "application/json", "")
	if addressResponse.Code != http.StatusOK {
		t.Fatalf("address save returned %d: %s", addressResponse.Code, addressResponse.Body.String())
	}
	if got, want := addressResponse.Body.String(), `{"annotation":{"label":"cold wallet","note":"self custody"},"ok":true}`+"\n"; got != want {
		t.Errorf("address response = %q, want %q", got, want)
	}
	wantFile.Addresses[serverTestAddress] = model.Annotation{Label: "cold wallet", Note: "self custody"}
	assertPersistedAnnotations(t, path, wantFile)

	secondBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(firstBytes, secondBytes) {
		t.Fatal("transaction annotation was lost by address save")
	}

	repeated := postAnnotation(t, s, addressBody, "application/json", "")
	if repeated.Code != http.StatusOK || repeated.Body.String() != addressResponse.Body.String() {
		t.Fatalf("repeated save returned a different response: %d %q", repeated.Code, repeated.Body.String())
	}
	thirdBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(secondBytes, thirdBytes) {
		t.Fatal("identical annotation saves did not produce deterministic file output")
	}
}

func assertPersistedAnnotations(t *testing.T, path string, want model.AnnotationFile) {
	t.Helper()
	got, err := input.LoadAnnotations(path)
	if err != nil {
		t.Fatal(err)
	}
	encodedValue, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var persistedWant model.AnnotationFile
	if err := json.Unmarshal(encodedValue, &persistedWant); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, persistedWant) {
		t.Fatalf("persisted annotations:\ngot:  %#v\nwant: %#v", got, persistedWant)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.MarshalIndent(want, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	if !bytes.Equal(data, encoded) {
		t.Fatalf("annotation file bytes are not canonical and deterministic:\ngot:\n%s\nwant:\n%s", data, encoded)
	}
}

func TestAnnotationExistingNullMapsAndUnrelatedData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "annotations.json")
	outpointKey := serverTestTxID + ":0"
	contents := fmt.Sprintf(`{"schema_version":1,"transactions":null,"addresses":null,"outpoints":{"%s":{"label":"keep"}},"purchase_links":[{"target":"%s","purchase_id":"buy","decision":"confirmed"}]}`, outpointKey, serverTestTxID)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newHandlerServer(t, path, serverTestReport("null maps"), nil)
	body := `{"address":"` + serverTestAddress + `","label":"wallet"}`
	if response := postAnnotation(t, s, body, "application/json", ""); response.Code != http.StatusOK {
		t.Fatalf("save returned %d: %s", response.Code, response.Body.String())
	}
	persisted, err := input.LoadAnnotations(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted.Transactions) != 0 || persisted.Addresses[serverTestAddress].Label != "wallet" {
		t.Errorf("null maps were not handled correctly: %#v", persisted)
	}
	if persisted.Outpoints[outpointKey].Label != "keep" || len(persisted.PurchaseLinks) != 1 || persisted.PurchaseLinks[0].PurchaseID != "buy" {
		t.Errorf("unrelated annotations were not preserved: %#v", persisted)
	}
}

func TestAnnotationStorageSchemaAndValidationFailures(t *testing.T) {
	validBody := `{"transaction":"` + serverTestTxID + `","label":"purchase"}`
	t.Run("stored file read error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "annotations.json")
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		s := newHandlerServer(t, path, serverTestReport("read"), nil)
		response := postAnnotation(t, s, validBody, "application/json", "")
		if response.Code != http.StatusInternalServerError || response.Body.String() != "cannot read annotations\n" {
			t.Fatalf("read error response = %d %q", response.Code, response.Body.String())
		}
	})
	t.Run("malformed stored JSON", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "annotations.json")
		if err := os.WriteFile(path, []byte(`{"schema_version":`), 0o600); err != nil {
			t.Fatal(err)
		}
		s := newHandlerServer(t, path, serverTestReport("stored parse"), nil)
		response := postAnnotation(t, s, validBody, "application/json", "")
		if response.Code != http.StatusConflict || response.Body.String() != "stored annotations are invalid\n" {
			t.Fatalf("stored parse response = %d %q", response.Code, response.Body.String())
		}
	})
	t.Run("unsupported stored schema", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "annotations.json")
		if err := os.WriteFile(path, []byte(`{"schema_version":2}`), 0o600); err != nil {
			t.Fatal(err)
		}
		s := newHandlerServer(t, path, serverTestReport("schema"), nil)
		response := postAnnotation(t, s, validBody, "application/json", "")
		if response.Code != http.StatusBadRequest || !strings.HasPrefix(response.Body.String(), "invalid or unwritable annotation: ") || !strings.Contains(response.Body.String(), "unsupported schema_version 2") {
			t.Fatalf("schema response = %d %q", response.Code, response.Body.String())
		}
	})
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "invalid transaction ID", body: `{"transaction":"not-a-txid","label":"bad"}`},
		{name: "invalid address", body: `{"address":"not-an-address","label":"bad"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "annotations.json")
			s := newHandlerServer(t, path, serverTestReport("validation"), nil)
			response := postAnnotation(t, s, test.body, "application/json", "")
			if response.Code != http.StatusBadRequest || !strings.HasPrefix(response.Body.String(), "invalid or unwritable annotation: ") {
				t.Fatalf("validation response = %d %q", response.Code, response.Body.String())
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("invalid annotation was persisted: %v", err)
			}
		})
	}
}

func TestAnnotationRebuildSuccessUpdatesReportAndIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "annotations.json")
	initial := serverTestReport("before rebuild")
	updated := serverTestReport("after rebuild")
	calls := 0
	s := newHandlerServer(t, path, initial, func() (model.Report, error) {
		calls++
		persisted, err := input.LoadAnnotations(path)
		if err != nil {
			t.Fatalf("rebuild could not load saved annotations: %v", err)
		}
		if persisted.Transactions[serverTestTxID].Label != "new label" {
			t.Fatalf("rebuild ran before annotation persistence: %#v", persisted)
		}
		return updated, nil
	})
	body := `{"transaction":"` + serverTestTxID + `","label":"new label"}`
	if response := postAnnotation(t, s, body, "application/json", ""); response.Code != http.StatusOK {
		t.Fatalf("save/rebuild returned %d: %s", response.Code, response.Body.String())
	}
	if calls != 1 {
		t.Fatalf("rebuild called %d times, want 1", calls)
	}
	response := getPath(t, s, "/api/report")
	var got model.Report
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.GeneratedAt != updated.GeneratedAt {
		t.Errorf("GET report after rebuild GeneratedAt = %q, want %q", got.GeneratedAt, updated.GeneratedAt)
	}
	if html := getPath(t, s, "/").Body.String(); !strings.Contains(html, updated.GeneratedAt) || strings.Contains(html, initial.GeneratedAt) {
		t.Error("index did not use the rebuilt report")
	}
}

func TestAnnotationRebuildFailurePersistsButKeepsOldReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "annotations.json")
	initial := serverTestReport("unchanged report")
	calls := 0
	s := newHandlerServer(t, path, initial, func() (model.Report, error) {
		calls++
		return model.Report{}, errors.New("synthetic rebuild failure")
	})
	body := `{"address":"` + serverTestAddress + `","label":"persisted"}`
	response := postAnnotation(t, s, body, "application/json", "")
	if response.Code != http.StatusConflict || response.Body.String() != "annotation saved but report rebuild failed: synthetic rebuild failure\n" {
		t.Fatalf("rebuild failure response = %d %q", response.Code, response.Body.String())
	}
	if calls != 1 {
		t.Fatalf("rebuild called %d times, want 1", calls)
	}
	persisted, err := input.LoadAnnotations(path)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Addresses[serverTestAddress].Label != "persisted" {
		t.Errorf("annotation was not persisted despite rebuild failure: %#v", persisted)
	}
	var got model.Report
	if err := json.Unmarshal(getPath(t, s, "/api/report").Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.GeneratedAt != initial.GeneratedAt {
		t.Errorf("failed rebuild replaced report: GeneratedAt = %q", got.GeneratedAt)
	}
}
