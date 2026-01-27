package http

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// Config defines HTTP runtime configuration.
// All fields have JSON tags for unmarshaling from configuration.
type Config struct {
	Addr         string         `json:"addr"`
	ReadTimeout  time.Duration  `json:"read_timeout"`
	WriteTimeout time.Duration  `json:"write_timeout"`
	IdleTimeout  time.Duration  `json:"idle_timeout"`
	TLSCertFile  string         `json:"tls_cert_file"`
	TLSKeyFile   string         `json:"tls_key_file"`
	Services     map[string]any `json:"services"`
}

// Runtime wraps http.Server. Config is embedded for unmarshaling from configuration.
// All other fields are unexported implementation details.
type Runtime struct {
	Config

	// Unexported implementation details
	server      *http.Server
	tlsCertFile string
	tlsKeyFile  string
	tlsConfig   *tls.Config
}

// Init initializes the HTTP runtime after Config has been unmarshaled.
func (rt *Runtime) Init() error {
	rt.server = &http.Server{
		Addr:         rt.Config.Addr,
		Handler:      http.NewServeMux(),
		ReadTimeout:  rt.Config.ReadTimeout,
		WriteTimeout: rt.Config.WriteTimeout,
		IdleTimeout:  rt.Config.IdleTimeout,
	}

	if rt.Config.TLSCertFile != "" && rt.Config.TLSKeyFile != "" {
		rt.tlsCertFile = rt.Config.TLSCertFile
		rt.tlsKeyFile = rt.Config.TLSKeyFile
	}

	return nil
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
	mux, ok := rt.server.Handler.(*http.ServeMux)
	if !ok {
		return fmt.Errorf("http runtime: expected *http.ServeMux, got %T", rt.server.Handler)
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
	if rt.server == nil {
		return fmt.Errorf("http runtime: server not initialized")
	}

	if rt.tlsCertFile != "" && rt.tlsKeyFile != "" {
		if rt.tlsConfig != nil {
			rt.server.TLSConfig = rt.tlsConfig
		}
		if err := rt.server.ListenAndServeTLS(rt.tlsCertFile, rt.tlsKeyFile); err != nil {
			if err == http.ErrServerClosed {
				return nil
			}
			return err
		}
		return nil
	}

	if err := rt.server.ListenAndServe(); err != nil {
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
	return nil
}

// Stop performs a graceful shutdown.
func (rt *Runtime) Stop(ctx context.Context) error {
	return rt.server.Shutdown(ctx)
}
