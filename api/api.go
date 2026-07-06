package api

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
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
