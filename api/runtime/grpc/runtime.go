// Package grpc provides a gRPC runtime implementation for the bees microservice framework.
package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"

	grpcapi "google.golang.org/grpc"
)

var (
	ErrAlreadyRunning = fmt.Errorf("grpc: runtime already running")
	ErrInvalidService = fmt.Errorf("grpc: invalid service type")
)

type serviceInterceptors struct {
	unary  []grpcapi.UnaryServerInterceptor
	stream []grpcapi.StreamServerInterceptor
}

// Config defines gRPC runtime configuration.
// All fields have JSON tags for unmarshaling from configuration.
type Config struct {
	Addr     string         `json:"addr"`
	Services map[string]any `json:"services"`
}

// Runtime manages a gRPC server and service registration.
// Config is embedded for unmarshaling from configuration.
// All other fields are unexported implementation details.
type Runtime struct {
	Config

	// Unexported implementation details
	mu sync.RWMutex

	server     *grpcapi.Server
	serverOpts []grpcapi.ServerOption

	unaryInterceptors  []grpcapi.UnaryServerInterceptor
	streamInterceptors []grpcapi.StreamServerInterceptor

	serviceInterceptors map[string]*serviceInterceptors

	listener net.Listener
	running  bool
}

// Init initializes the gRPC runtime after Config has been unmarshaled.
func (rt *Runtime) Init() error {
	rt.serviceInterceptors = map[string]*serviceInterceptors{}
	rt.server = grpcapi.NewServer(rt.serverOptions()...)
	return nil
}

// ------------------------
// Service registry helpers
// ------------------------

// NOTE: service factory registration and config-based service registration
// have been moved to registry subpackages.

// Register registers a service with the runtime.
// Supports Service and []Service.
func (rt *Runtime) Register(svc any) error {
	rt.mu.Lock()
	if rt.running {
		rt.mu.Unlock()
		return fmt.Errorf("grpc runtime: cannot register after start")
	}
	if rt.server == nil {
		rt.server = grpcapi.NewServer(rt.serverOptions()...)
	}
	rt.mu.Unlock()

	switch v := svc.(type) {
	case Service:
		return rt.registerService(v)
	case []Service:
		for _, s := range v {
			if err := rt.registerService(s); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("grpc runtime: unsupported register type %T", svc)
	}
}

// Run starts the gRPC server. It will return nil on graceful shutdown.
func (rt *Runtime) Run() error {
	rt.mu.Lock()
	if rt.running {
		rt.mu.Unlock()
		return ErrAlreadyRunning
	}

	if rt.server == nil {
		rt.server = grpcapi.NewServer(rt.serverOptions()...)
	}

	lis, err := net.Listen("tcp", rt.Config.Addr)
	if err != nil {
		rt.mu.Unlock()
		return fmt.Errorf("grpc listen: %w", err)
	}

	rt.listener = lis
	rt.running = true
	rt.mu.Unlock()

	slog.Info("grpc runtime started", "addr", rt.Config.Addr)

	if err := rt.server.Serve(lis); err != nil {
		if err == grpcapi.ErrServerStopped {
			return nil
		}
		return err
	}
	return nil
}

// Stop performs a graceful shutdown and falls back to immediate stop on context cancellation.
func (rt *Runtime) Stop(ctx context.Context) error {
	rt.mu.RLock()
	if !rt.running || rt.server == nil {
		rt.mu.RUnlock()
		return nil
	}
	server := rt.server
	rt.mu.RUnlock()

	done := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(done)
	}()

	select {
	case <-ctx.Done():
		server.Stop()
	case <-done:
	}

	rt.mu.Lock()
	rt.running = false
	rt.mu.Unlock()

	slog.Info("grpc runtime stopped")
	return nil
}

// -----------------------------------------------------------------------------
// Internal helpers
// -----------------------------------------------------------------------------

func (rt *Runtime) registerService(svc Service) error {
	if svc.Name == "" {
		return fmt.Errorf("grpc runtime: service name is required")
	}
	if svc.Init != nil {
		if err := svc.Init(); err != nil {
			return fmt.Errorf("grpc runtime: service init failed: %w", err)
		}
	}
	if len(svc.Registers) == 0 {
		return fmt.Errorf("grpc runtime: service %q has no registers", svc.Name)
	}

	rt.addServiceInterceptors(svc.Name, svc.Unary, svc.Stream)

	for _, register := range svc.Registers {
		if register == nil {
			return fmt.Errorf("grpc runtime: register func is nil for service %q", svc.Name)
		}
		if err := register(rt.server); err != nil {
			return fmt.Errorf("grpc runtime: register service %q: %w", svc.Name, err)
		}
	}

	slog.Info("registered grpc service", "service", svc.Name)
	return nil
}

func (rt *Runtime) addServiceInterceptors(name string, unary []grpcapi.UnaryServerInterceptor, stream []grpcapi.StreamServerInterceptor) {
	if name == "" {
		return
	}
	if len(unary) == 0 && len(stream) == 0 {
		return
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	entry, ok := rt.serviceInterceptors[name]
	if !ok {
		entry = &serviceInterceptors{}
		rt.serviceInterceptors[name] = entry
	}
	entry.unary = append(entry.unary, unary...)
	entry.stream = append(entry.stream, stream...)
}

func (rt *Runtime) serverOptions() []grpcapi.ServerOption {
	opts := append([]grpcapi.ServerOption{}, rt.serverOpts...)
	opts = append(opts,
		grpcapi.UnaryInterceptor(rt.unaryDispatcher()),
		grpcapi.StreamInterceptor(rt.streamDispatcher()),
	)
	return opts
}

func (rt *Runtime) unaryDispatcher() grpcapi.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpcapi.UnaryServerInfo, handler grpcapi.UnaryHandler) (any, error) {
		serviceUnary := rt.serviceUnaryInterceptors(info.FullMethod)
		interceptors := append([]grpcapi.UnaryServerInterceptor{}, rt.unaryInterceptors...)
		interceptors = append(interceptors, serviceUnary...)

		if len(interceptors) == 0 {
			return handler(ctx, req)
		}
		return chainUnary(interceptors...)(ctx, req, info, handler)
	}
}

func (rt *Runtime) streamDispatcher() grpcapi.StreamServerInterceptor {
	return func(srv any, stream grpcapi.ServerStream, info *grpcapi.StreamServerInfo, handler grpcapi.StreamHandler) error {
		serviceStream := rt.serviceStreamInterceptors(info.FullMethod)
		interceptors := append([]grpcapi.StreamServerInterceptor{}, rt.streamInterceptors...)
		interceptors = append(interceptors, serviceStream...)

		if len(interceptors) == 0 {
			return handler(srv, stream)
		}
		return chainStream(interceptors...)(srv, stream, info, handler)
	}
}

func (rt *Runtime) serviceUnaryInterceptors(fullMethod string) []grpcapi.UnaryServerInterceptor {
	name := serviceNameFromFullMethod(fullMethod)
	if name == "" {
		return nil
	}

	rt.mu.RLock()
	entry := rt.serviceInterceptors[name]
	rt.mu.RUnlock()
	if entry == nil || len(entry.unary) == 0 {
		return nil
	}
	return append([]grpcapi.UnaryServerInterceptor{}, entry.unary...)
}

func (rt *Runtime) serviceStreamInterceptors(fullMethod string) []grpcapi.StreamServerInterceptor {
	name := serviceNameFromFullMethod(fullMethod)
	if name == "" {
		return nil
	}

	rt.mu.RLock()
	entry := rt.serviceInterceptors[name]
	rt.mu.RUnlock()
	if entry == nil || len(entry.stream) == 0 {
		return nil
	}
	return append([]grpcapi.StreamServerInterceptor{}, entry.stream...)
}

func serviceNameFromFullMethod(fullMethod string) string {
	if fullMethod == "" || fullMethod[0] != '/' {
		return ""
	}
	parts := strings.Split(fullMethod[1:], "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[0]
}

// chainUnary builds a single interceptor from a list, applying them in order.
func chainUnary(interceptors ...grpcapi.UnaryServerInterceptor) grpcapi.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpcapi.UnaryServerInfo, handler grpcapi.UnaryHandler) (any, error) {
		chained := handler
		for i := len(interceptors) - 1; i >= 0; i-- {
			current := interceptors[i]
			next := chained
			chained = func(currentCtx context.Context, currentReq any) (any, error) {
				return current(currentCtx, currentReq, info, next)
			}
		}
		return chained(ctx, req)
	}
}

// chainStream builds a single interceptor from a list, applying them in order.
func chainStream(interceptors ...grpcapi.StreamServerInterceptor) grpcapi.StreamServerInterceptor {
	return func(srv any, stream grpcapi.ServerStream, info *grpcapi.StreamServerInfo, handler grpcapi.StreamHandler) error {
		chained := handler
		for i := len(interceptors) - 1; i >= 0; i-- {
			current := interceptors[i]
			next := chained
			chained = func(currentSrv any, currentStream grpcapi.ServerStream) error {
				return current(currentSrv, currentStream, info, next)
			}
		}
		return chained(srv, stream)
	}
}
