//go:build test

package frontend

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
)

func TestAddRoutesNoOpInTestBuild(t *testing.T) {
	component := New()
	router := mux.NewRouter()

	if err := component.AddRoutes(router); err != nil {
		t.Fatalf("AddRoutes() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected no routes in test build, got status %d", recorder.Code)
	}
}
