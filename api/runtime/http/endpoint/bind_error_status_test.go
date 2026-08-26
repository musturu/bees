package endpoint

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBindErrorHonorsHttpErrorStatus guards a bug found via manual testing:
// a Bind error wrapped in HttpError must produce that status code, not
// always 400 -- matching how the Write-error path already behaves.
func TestBindErrorHonorsHttpErrorStatus(t *testing.T) {
	ep := NewEndpoint("GET /thing", func(ctx context.Context, in *struct{}) (string, error) {
		return "unreachable", nil
	})
	ep.Bind = func(r *http.Request, in *struct{}) error {
		return HttpError{StatusCode: http.StatusNotFound, Err: errors.New("thing not found")}
	}

	req := httptest.NewRequest(http.MethodGet, "/thing", nil)
	rec := httptest.NewRecorder()
	ep.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("got status %d, want %d", rec.Code, http.StatusNotFound)
	}
}
