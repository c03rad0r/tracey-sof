package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"tracey-sof/internal/input"
	"tracey-sof/internal/model"
	reportfiles "tracey-sof/internal/report"
)

type Config struct {
	Address        string
	AnnotationPath string
	Report         model.Report
	Rebuild        func() (model.Report, error)
}

type Server struct {
	address, annotationPath string
	rebuild                 func() (model.Report, error)
	mu                      sync.RWMutex
	saveMu                  sync.Mutex
	report                  model.Report
	server                  *http.Server
}

type annotationRequest struct {
	Transaction string `json:"transaction,omitempty"`
	Address     string `json:"address,omitempty"`
	Label       string `json:"label"`
	Note        string `json:"note"`
}

func New(cfg Config) (*Server, error) {
	if cfg.Address == "" {
		cfg.Address = "127.0.0.1:8765"
	}
	host, _, err := net.SplitHostPort(cfg.Address)
	if err != nil {
		return nil, fmt.Errorf("invalid UI listen address: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("UI must bind to a numeric loopback address")
	}
	if cfg.AnnotationPath == "" {
		return nil, fmt.Errorf("annotation path is required")
	}
	s := &Server{address: cfg.Address, annotationPath: cfg.AnnotationPath, report: cfg.Report, rebuild: cfg.Rebuild}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("GET /api/report", s.getReport)
	mux.HandleFunc("POST /api/annotations", s.saveAnnotation)
	s.server = &http.Server{Addr: cfg.Address, Handler: securityHeaders(mux), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	return s, nil
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	expected := (&url.URL{Scheme: "http", Host: r.Host}).String()
	parsed, err := url.Parse(origin)
	return err == nil && strings.TrimRight(parsed.String(), "/") == expected
}

func (s *Server) index(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	report := s.report
	s.mu.RUnlock()
	data, err := reportfiles.HTMLBytes(report, true)
	if err != nil {
		http.Error(w, "could not generate report", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (s *Server) getReport(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.report)
}

func (s *Server) saveAnnotation(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) || strings.TrimSpace(r.Header.Get("Content-Type")) != "application/json" {
		http.Error(w, "same-origin application/json request required", http.StatusForbidden)
		return
	}
	var request annotationRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&request); err != nil {
		http.Error(w, "invalid annotation JSON", http.StatusBadRequest)
		return
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		http.Error(w, "one JSON object required", http.StatusBadRequest)
		return
	}
	if (request.Transaction == "") == (request.Address == "") {
		http.Error(w, "exactly one transaction or address is required", http.StatusBadRequest)
		return
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	file := input.EmptyAnnotations()
	data, err := os.ReadFile(s.annotationPath)
	if err != nil {
		if !os.IsNotExist(err) {
			http.Error(w, "cannot read annotations", http.StatusInternalServerError)
			return
		}
	} else if err := json.Unmarshal(data, &file); err != nil {
		http.Error(w, "stored annotations are invalid", http.StatusConflict)
		return
	}
	if file.Transactions == nil {
		file.Transactions = map[string]model.Annotation{}
	}
	if file.Addresses == nil {
		file.Addresses = map[string]model.Annotation{}
	}
	annotation := model.Annotation{Label: request.Label, Note: request.Note}
	if request.Transaction != "" {
		file.Transactions[request.Transaction] = annotation
	} else {
		file.Addresses[request.Address] = annotation
	}
	if err := input.SaveAnnotations(s.annotationPath, file); err != nil {
		http.Error(w, "invalid or unwritable annotation: "+err.Error(), http.StatusBadRequest)
		return
	}
	if s.rebuild != nil {
		updated, err := s.rebuild()
		if err != nil {
			http.Error(w, "annotation saved but report rebuild failed: "+err.Error(), http.StatusConflict)
			return
		}
		s.mu.Lock()
		s.report = updated
		s.mu.Unlock()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "annotation": annotation})
}

func (s *Server) ListenAndServe() error {
	err := s.server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error { return s.server.Shutdown(ctx) }
func (s *Server) Address() string                    { return s.address }
