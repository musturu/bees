package endpoint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bees/api/runtime/http/middleware"
)

type envelope struct {
	Error struct {
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Details map[string]string `json:"details"`
	} `json:"error"`
}

func decodeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) envelope {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var env envelope
	if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
		t.Fatalf("decoding error envelope: %v", err)
	}
	return env
}

func TestCodedJSONWriterErrors(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantStatus  int
		wantCode    string
		wantMessage string
		wantDetails map[string]string
	}{
		{
			name:        "coded error with status",
			err:         HttpError{StatusCode: http.StatusConflict, Err: CodedError{Code: "SLOT_FULL", Message: "slot full"}},
			wantStatus:  http.StatusConflict,
			wantCode:    "SLOT_FULL",
			wantMessage: "slot full",
		},
		{
			name: "coded error with details, wrapped",
			err: fmt.Errorf("saving: %w", HttpError{StatusCode: http.StatusUnprocessableEntity, Err: CodedError{
				Code: "VALIDATION_FAILED", Message: "invalid", Details: map[string]string{"name": "required"},
			}}),
			wantStatus:  http.StatusUnprocessableEntity,
			wantCode:    "VALIDATION_FAILED",
			wantMessage: "invalid",
			wantDetails: map[string]string{"name": "required"},
		},
		{
			name:        "coded error without status is a 500",
			err:         CodedError{Code: "BROKEN", Message: "broken"},
			wantStatus:  http.StatusInternalServerError,
			wantCode:    "BROKEN",
			wantMessage: "broken",
		},
		{
			name:        "plain 4xx HttpError gets a status-derived code",
			err:         HttpError{StatusCode: http.StatusBadRequest, Err: errors.New("bad input")},
			wantStatus:  http.StatusBadRequest,
			wantCode:    "BAD_REQUEST",
			wantMessage: "bad input",
		},
		{
			name:        "plain error does not leak its text",
			err:         errors.New("pq: connection refused to 10.0.0.1"),
			wantStatus:  http.StatusInternalServerError,
			wantCode:    "INTERNAL_SERVER_ERROR",
			wantMessage: "Internal Server Error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if err := CodedJSONWriter(t.Context(), rec, req, nil, tt.err); err != nil {
				t.Fatalf("CodedJSONWriter: %v", err)
			}
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			env := decodeEnvelope(t, rec)
			if env.Error.Code != tt.wantCode || env.Error.Message != tt.wantMessage {
				t.Fatalf("error = {%q, %q}, want {%q, %q}", env.Error.Code, env.Error.Message, tt.wantCode, tt.wantMessage)
			}
			if len(env.Error.Details) != len(tt.wantDetails) {
				t.Fatalf("details = %v, want %v", env.Error.Details, tt.wantDetails)
			}
			for k, v := range tt.wantDetails {
				if env.Error.Details[k] != v {
					t.Fatalf("details = %v, want %v", env.Error.Details, tt.wantDetails)
				}
			}
		})
	}
}

func TestCodedJSONWriterSuccess(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if err := CodedJSONWriter(t.Context(), rec, req, map[string]int{"n": 1}, nil); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"n":1}` {
		t.Fatalf("got %d %q, want 200 {\"n\":1}", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	if err := CodedJSONWriter(t.Context(), rec, req, nil, nil); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("nil output: status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

// TestBindErrorGoesThroughWriter guards the bind-error path: a failed Bind
// must be rendered by the endpoint's writer (so it gets the same response
// format as handler errors), with 400 unless the error carries a status.
func TestBindErrorGoesThroughWriter(t *testing.T) {
	ep := NewEndpoint("POST /thing", func(ctx context.Context, in *struct{ N int }) (string, error) {
		t.Fatal("Handle should not run after a Bind error")
		return "", nil
	})
	ep.Bind = TaggedBinder[struct{ N int }]
	ep.Write = CodedJSONWriter

	req := httptest.NewRequest(http.MethodPost, "/thing", strings.NewReader("{not json"))
	rec := httptest.NewRecorder()
	ep.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if env := decodeEnvelope(t, rec); env.Error.Code != "BAD_REQUEST" {
		t.Fatalf("code = %q, want BAD_REQUEST", env.Error.Code)
	}
}

func TestEndpointMiddlewareWrapsOnlyThatEndpoint(t *testing.T) {
	deny := middleware.Middleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		})
	})
	open := NewEndpoint("GET /thing", func(ctx context.Context, in *struct{}) (string, error) { return "ok", nil })
	guarded := NewEndpoint("PUT /thing", func(ctx context.Context, in *struct{}) (string, error) {
		t.Fatal("guarded Handle should not run")
		return "", nil
	})
	guarded.Middleware = deny

	svc, err := NewService("/", WithEndpoint(open), WithEndpoint(guarded))
	if err != nil {
		t.Fatal(err)
	}

	for method, want := range map[string]int{http.MethodGet: http.StatusOK, http.MethodPut: http.StatusForbidden} {
		rec := httptest.NewRecorder()
		svc.Mux().ServeHTTP(rec, httptest.NewRequest(method, "/thing", nil))
		if rec.Code != want {
			t.Errorf("%s /thing: status = %d, want %d", method, rec.Code, want)
		}
	}
}
