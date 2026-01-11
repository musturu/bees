package http

import (
	"context"
	"crypto/tls"
	"fmt"
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
func (rt *Runtime) Register(s interface{}) error {
	switch v := s.(type) {
	case HTTPService:
		return rt.registerServices(v)
	case []HTTPService:
		return rt.registerServices(v...)
	case []interface{}:
		var collected []HTTPService
		for _, it := range v {
			if hs, ok := it.(HTTPService); ok {
				collected = append(collected, hs)
			} else {
				return fmt.Errorf("http runtime: unsupported element type %T in slice", it)
			}
		}
		return rt.registerServices(collected...)
	default:
		return fmt.Errorf("http runtime: unsupported register type %T", s)
	}
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
