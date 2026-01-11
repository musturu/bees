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
	runtime  Runtime
	services []interface{}
}

// New receives the concrete runtime and an optional list of services to register.
func New(rt Runtime, svcs ...interface{}) *Api {
	return &Api{
		runtime:  rt,
		services: svcs,
	}
}

// Register appends a service to the Api.
// Services will be registered with the runtime when Start() is called.
func (a *Api) Register(svc interface{}) {
	a.services = append(a.services, svc)
}

// Start blocks until SIGINT/SIGTERM then gracefully stops.
func (a *Api) Start(ctx context.Context) error {
	for _, svc := range a.services {
		if err := a.runtime.Register(svc); err != nil {
			return fmt.Errorf("register: %w", err)
		}
	}

	type result struct{ err error }
	done := make(chan result, 1)
	go func() { done <- result{a.runtime.Run()} }()

	// 3.  Wait for signal or runtime crash.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-quit:
		slog.Info("shutting down on signal")
	case <-ctx.Done():
		slog.Info("shutting down on context done")
	case res := <-done:
		if res.err != nil {
			return res.err
		}
	}

	// 4.  Graceful stop.
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return a.runtime.Stop(sctx)
}
