package endpoint

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTaggedBinder_Body(t *testing.T) {
	type In struct {
		Name     string `json:"name"`
		Category string `json:"category"`
	}
	in := &In{}
	body := `{"name":"widget","category":"tools"}`
	req, err := http.NewRequest("POST", "http://example.test/", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	if err := TaggedBinder(req, in); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	if in.Name != "widget" || in.Category != "tools" {
		t.Fatalf("unexpected: %+v", in)
	}
}

func TestTaggedBinder_QueryHeaderPathOverride(t *testing.T) {
	type In struct {
		ID        string `path:"id"`
		Name      string `json:"name" query:"name"`
		Category  string `json:"category" query:"category"`
		RequestID string `header:"X-Request-ID"`
	}
	in := &In{Name: "body", Category: "bodycat"}
	body := `{"name":"body","category":"bodycat"}`

	// Create a mux with a pattern to set PathValue
	mux := http.NewServeMux()
	mux.HandleFunc("POST /items/{id}", func(w http.ResponseWriter, r *http.Request) {
		// Dummy handler, just to set PathValue
		w.WriteHeader(http.StatusOK)
	})

	req, err := http.NewRequest("POST", "http://example.test/items/123?name=query&category=tools", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("X-Request-ID", "abc123")

	// Call ServeHTTP to populate PathValue
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// Now req has PathValue set
	if err := TaggedBinder(req, in); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	if in.ID != "123" {
		t.Fatalf("expected id=123, got %s", in.ID)
	}
	if in.Name != "query" {
		t.Fatalf("expected name=query, got %s", in.Name)
	}
	if in.Category != "tools" {
		t.Fatalf("expected category=tools, got %s", in.Category)
	}
	if in.RequestID != "abc123" {
		t.Fatalf("expected request_id=abc123, got %s", in.RequestID)
	}
}
