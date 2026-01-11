package http

import (
	"bees/api/runtime/http/middleware"
	"fmt"
	"log/slog"
	"net/http"
)

// HTTPService describes a simple HTTP service that exposes a mux and middleware chain.
// Implement this interface to be registerable with the HTTP Runtime.
type HTTPService interface {
	// Mux returns the service's router (usually a *http.ServeMux).
	Mux() *http.ServeMux

	// Chain returns middleware that will wrap the service handler.
	Chain() middleware.Middleware

	// Root returns the mount path for the service (e.g. "/api/users/").
	Root() string
}

type InitHTTPService interface {
	HTTPService
	// Init is called by the runtime when registering the service.
	// Use this to perform any initialization that requires runtime context.
	Init() error
}

// registerServices mounts one or more HTTPService implementations into the runtime's mux.
// It mounts each service using the service's Root and applies the Chain middleware.
func (rt *Runtime) registerServices(svcs ...HTTPService) error {
	mux, ok := rt.Server.Handler.(*http.ServeMux)
	if !ok {
		return fmt.Errorf("http runtime: expected *http.ServeMux, got %T", rt.Server.Handler)
	}

	for _, svc := range svcs {
		// If the service implements InitHTTPService, call Init
		if initSvc, ok := svc.(InitHTTPService); ok {
			if err := initSvc.Init(); err != nil {
				return fmt.Errorf("http runtime: service init failed: %w", err)
			}
		}
		root := svc.Root()
		handler := svc.Chain()(svc.Mux())

		// StripPrefix needs the path without trailing slash to avoid redirect issues
		// e.g., /api/ -> strip /api so /api/test becomes /test (not redirect to /test)
		stripPath := root
		if len(stripPath) > 1 && stripPath[len(stripPath)-1] == '/' {
			stripPath = stripPath[:len(stripPath)-1]
		}
		mux.Handle(root, http.StripPrefix(stripPath, handler))
		slog.Info("registered service", "root", root)
	}
	return nil
}
