package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	rtregistry "bees/api/runtime/registry"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"gopkg.in/yaml.v3"
)

type Runtime interface {
	Run() error
	Stop(ctx context.Context) error
	// Register registers a service or other component with the runtime.
	// The runtime decides which concrete types it supports and performs
	// any necessary wiring (type assertions internally).
	Register(service interface{}) error
}

// Api manages lifecycle and performs registration of services
// by delegating to the concrete runtime's Register method.
type Api struct {
	runtimes map[Runtime][]interface{}
}

// FromCue configures the API from a pre-built CUE value.
// The developer is responsible for loading CUE schemas, parsing config, and compiling into a CUE value.
// The API framework then extracts runtimes and services from the value.
func FromCue(val *cue.Value) ApiOption {
	return func(a *Api) {
		// Iterate over all registered runtimes
		for _, name := range rtregistry.All() {
			rtEntry, _ := rtregistry.GetRuntime(name)

			// Look up the runtime config in the CUE value
			rtVal := val.LookupPath(cue.ParsePath(name))
			if !rtVal.Exists() {
				slog.Debug("runtime config not found in CUE", "runtime", name)
				continue
			}

			// Create a pointer to the runtime struct (ConfigType is a Runtime pointer)
			rtInstance := rtEntry.ConfigType.(interface{})

			// Unmarshal CUE value into the runtime struct using JSON tags
			if err := rtVal.Decode(rtInstance); err != nil {
				slog.Error("failed to decode runtime config", "runtime", name, "error", err)
				continue
			}

			// Call Init() to initialize internal state
			// Runtime implements Initer interface
			if initer, ok := rtInstance.(interface{ Init() error }); ok {
				if err := initer.Init(); err != nil {
					slog.Error("failed to initialize runtime", "runtime", name, "error", err)
					continue
				}
			}

			// Assert to Runtime interface
			runtime, ok := rtInstance.(Runtime)
			if !ok {
				slog.Error("runtime does not satisfy Runtime interface", "runtime", name)
				continue
			}

			a.AddRuntime(runtime)

			// Handle services
			svcsVal := rtVal.LookupPath(cue.ParsePath("services"))
			if !svcsVal.Exists() {
				continue
			}

			svcReg, ok := rtregistry.GetServiceRegistry(name)
			if !ok {
				slog.Warn("no service registry for runtime", "runtime", name)
				continue
			}

			// Iterate over services in the CUE value
			iter, _ := svcsVal.Fields()
			for iter.Next() {
				svcName := iter.Label()
				svcVal := iter.Value()

				// Marshal service config to JSON bytes
				svcJSON, err := svcVal.MarshalJSON()
				if err != nil {
					slog.Error("failed to marshal service config", "runtime", name, "service", svcName, "error", err)
					continue
				}

				// Resolve service through the service registry
				svc, err := svcReg.Resolve(svcName, svcJSON)
				if err != nil {
					slog.Error("failed to resolve service", "runtime", name, "service", svcName, "error", err)
					continue
				}

				// Call Init() if the service implements it
				if initer, ok := svc.(interface{ Init() error }); ok {
					if err := initer.Init(); err != nil {
						slog.Error("failed to initialize service", "runtime", name, "service", svcName, "error", err)
						continue
					}
				}

				// Register service with the runtime
				if err := runtime.Register(svc); err != nil {
					slog.Error("failed to register service", "runtime", name, "service", svcName, "error", err)
					continue
				}
			}
		}
	}
}

// FromConfig returns an ApiOption that configures the Api from a file.
// It's a convenience wrapper around FromCue for simple file-based configuration.
func FromConfig(path string) ApiOption {
	return func(a *Api) {
		b, err := os.ReadFile(path)
		if err != nil {
			slog.Error("failed to read config", "path", path, "error", err)
			return
		}

		var cfg map[string]any
		// Try JSON, then YAML
		if err := json.Unmarshal(b, &cfg); err != nil {
			if err := yaml.Unmarshal(b, &cfg); err != nil {
				slog.Error("failed to parse config as JSON or YAML", "path", path, "error", err)
				return
			}
		}

		// Build CUE value from parsed config
		configBytes, _ := json.Marshal(cfg)

		val := cuecontext.New().CompileBytes(configBytes)
		if val.Err() != nil {
			slog.Error("failed to compile config into CUE", "error", val.Err())
			return
		}

		// Use FromCue to configure from the built CUE value
		FromCue(&val)(a)
	}
}

// ApiOption configures an Api before creation.
type ApiOption func(*Api)

// New receives a map of runtimes to their associated services.
func New(rts map[Runtime][]interface{}) *Api {
	return &Api{
		runtimes: rts,
	}
}

// NewWithOptions creates a new Api applying the provided options.
func NewWithOptions(opts ...ApiOption) *Api {
	a := &Api{
		runtimes: map[Runtime][]interface{}{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(a)
		}
	}
	return a
}

// WithRuntime returns an ApiOption that adds the runtime and associated services.
func WithRuntime(rt Runtime, svcs ...interface{}) ApiOption {
	return func(a *Api) {
		if a.runtimes == nil {
			a.runtimes = map[Runtime][]interface{}{}
		}
		a.runtimes[rt] = append(a.runtimes[rt], svcs...)
	}
}

// AddRuntime adds a runtime and services to the Api.
func (a *Api) AddRuntime(rt Runtime, svcs ...interface{}) {
	if a.runtimes == nil {
		a.runtimes = map[Runtime][]interface{}{}
	}
	a.runtimes[rt] = append(a.runtimes[rt], svcs...)
}

// Start blocks until SIGINT/SIGTERM then gracefully stops.
func (a *Api) Start(ctx context.Context) error {
	for rt, svcs := range a.runtimes {
		for _, svc := range svcs {
			if err := rt.Register(svc); err != nil {
				return fmt.Errorf("register: %w", err)
			}
		}
	}

	done := make(chan error, len(a.runtimes))
	for rt := range a.runtimes {
		go func(rt Runtime) {
			done <- rt.Run()
		}(rt)
	}

	// Log runtime errors but don't stop others
	go func() {
		for i := 0; i < len(a.runtimes); i++ {
			if err := <-done; err != nil {
				slog.Error("runtime error", "error", err)
			}
		}
	}()

	// Wait for signal or context done.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-quit:
		slog.Info("shutting down on signal")
	case <-ctx.Done():
		slog.Info("shutting down on context done")
	}

	// Graceful stop.
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for rt := range a.runtimes {
		if err := rt.Stop(sctx); err != nil {
			slog.Error("error stopping runtime", "error", err)
		}
	}
	return nil
}
