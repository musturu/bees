package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"bees/api/runtime/http/middleware"
)

type fakeService struct {
	mux *http.ServeMux
}

func newFakeService() *fakeService {
	mux := http.NewServeMux()
	mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return &fakeService{mux: mux}
}

func (s *fakeService) Mux() *http.ServeMux          { return s.mux }
func (s *fakeService) Chain() middleware.Middleware { return nil }
func (s *fakeService) Root() string                 { return "/" }

// TestRootMountDoesNotRedirect guards against a regression of the
// StripPrefix("/") redirect loop: a service mounted at Root()=="/" must
// serve requests directly, not 301 back to the same URL.
func TestRootMountDoesNotRedirect(t *testing.T) {
	rt := &Runtime{Config: Config{Addr: "127.0.0.1:0"}}
	if err := rt.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := rt.Register(HTTPService(newFakeService())); err != nil {
		t.Fatalf("Register: %v", err)
	}

	srv := httptest.NewServer(rt.server.Handler)
	defer srv.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // don't follow, so a loop shows up as a redirect status
		},
	}
	resp, err := client.Get(srv.URL + "/hello")
	if err != nil {
		t.Fatalf("GET /hello: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /hello: got status %d, want %d (redirect loop regression?)", resp.StatusCode, http.StatusOK)
	}
}
