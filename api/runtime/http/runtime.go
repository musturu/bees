package http

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// Runtime wraps http.Server and exposes a builder-style configuration API.
// Everything is configurable through Options.
type Runtime struct {
	Server      *http.Server
	tlsCertFile string
	tlsKeyFile  string
	tlsConfig   *tls.Config
}

// Option configures the HTTP runtime before creation.
type Option func(*Runtime)

// WithAddr sets the bind address for the server.
func WithAddr(addr string) Option {
	return func(rt *Runtime) { rt.Server.Addr = addr }
}

// WithHandler sets the server's handler. If not provided a new *http.ServeMux is used.
func WithHandler(h http.Handler) Option {
	return func(rt *Runtime) { rt.Server.Handler = h }
}

// WithReadTimeout sets the server's ReadTimeout.
func WithReadTimeout(d time.Duration) Option {
	return func(rt *Runtime) { rt.Server.ReadTimeout = d }
}

// WithWriteTimeout sets the server's WriteTimeout.
func WithWriteTimeout(d time.Duration) Option {
	return func(rt *Runtime) { rt.Server.WriteTimeout = d }
}

// WithIdleTimeout sets the server's IdleTimeout.
func WithIdleTimeout(d time.Duration) Option {
	return func(rt *Runtime) { rt.Server.IdleTimeout = d }
}

// WithTLSFiles enables TLS using the provided cert and key files.
func WithTLSFiles(certFile, keyFile string) Option {
	return func(rt *Runtime) {
		rt.tlsCertFile = certFile
		rt.tlsKeyFile = keyFile
	}
}

// WithTLSConfig sets a custom tls.Config for the server.
func WithTLSConfig(cfg *tls.Config) Option {
	return func(rt *Runtime) { rt.tlsConfig = cfg }
}

// NewRuntime builds a configured Runtime. Use functional options to customize behavior.
func NewRuntime(opts ...Option) *Runtime {
	rt := &Runtime{
		Server: &http.Server{
			Addr:         ":http",
			Handler:      http.NewServeMux(),
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  120 * time.Second,
		},
	}

	for _, o := range opts {
		o(rt)
	}

	// Ensure there's always a ServeMux
	if rt.Server.Handler == nil {
		rt.Server.Handler = http.NewServeMux()
	}

	return rt
}

// Register implements the api.Runtime.Register interface. It accepts either a single
// HTTPService or a slice of HTTPService values and dispatches to the internal helper.
func (rt *Runtime) Register(s any) error {
	switch v := s.(type) {
	case HTTPService:
		return rt.registerServices(v)
	case []HTTPService:
		return rt.registerServices(v...)
	default:
		return fmt.Errorf("http runtime: unsupported register type %T", s)
	}
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
		if root == "" {
			return fmt.Errorf("http runtime: service root is required")
		}
		if root[0] != '/' {
			return fmt.Errorf("http runtime: service root must start with '/'")
		}
		if root[len(root)-1] != '/' {
			return fmt.Errorf("http runtime: service root must end with '/'")
		}

		serviceMux := svc.Mux()
		if serviceMux == nil {
			return fmt.Errorf("http runtime: service mux is nil")
		}

		chain := svc.Chain()
		if chain == nil {
			chain = func(h http.Handler) http.Handler { return h }
		}

		handler := chain(serviceMux)

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

// Run starts the HTTP server. It will return nil on graceful shutdown.
func (rt *Runtime) Run() error {
	if rt.Server == nil {
		return fmt.Errorf("http runtime: server not configured")
	}

	if rt.tlsCertFile != "" && rt.tlsKeyFile != "" {
		if rt.tlsConfig != nil {
			rt.Server.TLSConfig = rt.tlsConfig
		}
		if err := rt.Server.ListenAndServeTLS(rt.tlsCertFile, rt.tlsKeyFile); err != nil {
			if err == http.ErrServerClosed {
				return nil
			}
			return err
		}
		return nil
	}

	if err := rt.Server.ListenAndServe(); err != nil {
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
	return nil
}

// Stop performs a graceful shutdown.
func (rt *Runtime) Stop(ctx context.Context) error {
	return rt.Server.Shutdown(ctx)
}
