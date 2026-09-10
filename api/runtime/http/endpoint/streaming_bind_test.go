package endpoint

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleCanReadBodyAfterBind guards against a regression of the eager
// r.Body.Close() that used to run between Bind and Handle: a custom Binder
// that hands the raw body through unread must still be readable inside Handle.
func TestHandleCanReadBodyAfterBind(t *testing.T) {
	type in struct {
		Body io.ReadCloser
	}

	ep := NewEndpoint("POST /echo", func(ctx context.Context, i *in) (string, error) {
		b, err := io.ReadAll(i.Body)
		if err != nil {
			return "", err
		}
		return string(b), nil
	})
	ep.Bind = func(r *http.Request, i *in) error {
		i.Body = r.Body // deliberately unread
		return nil
	}
	ep.Write = func(ctx context.Context, w http.ResponseWriter, r *http.Request, output any, handlerErr error) error {
		if handlerErr != nil {
			return handlerErr
		}
		_, err := w.Write([]byte(output.(string)))
		return err
	}

	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader("hello streaming"))
	rec := httptest.NewRecorder()
	ep.Handler().ServeHTTP(rec, req)

	if got := rec.Body.String(); got != "hello streaming" {
		t.Fatalf("got body %q, want %q (Handle could not read body after Bind?)", got, "hello streaming")
	}
}
