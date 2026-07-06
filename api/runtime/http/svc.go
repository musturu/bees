package http

import (
	"bees/api/runtime/http/middleware"
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
