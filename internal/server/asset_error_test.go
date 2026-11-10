package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"tracey-sof/internal/model"
	webassets "tracey-sof/web"
)

func TestIndexReturnsHTMLErrorForBrokenTemplate(t *testing.T) {
	original := webassets.Index
	webassets.Index = "template without required markers"
	defer func() { webassets.Index = original }()
	s := &Server{report: model.Report{SchemaVersion: 1}}
	recorder := httptest.NewRecorder()
	s.index(recorder, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("got HTTP %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
}
