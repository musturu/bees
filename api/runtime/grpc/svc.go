package grpc

import grpcapi "google.golang.org/grpc"

// RegisterFunc is a callback-style registration function.
// This should call the generated pb.RegisterXServer function.
type RegisterFunc func(*grpcapi.Server) error

// Service packages a gRPC service and its registrations.
// Name should be the full gRPC service name (e.g., "package.Service").
// Registers should contain one or more generated pb.RegisterXServer functions.
// Unary/Stream interceptors extend the runtime-level chain for this service.
type Service struct {
	Name      string
	Registers []RegisterFunc
	Unary     []grpcapi.UnaryServerInterceptor
	Stream    []grpcapi.StreamServerInterceptor
	Init      func() error
}

// ServiceOption configures a Service during creation.
type ServiceOption func(*Service)

// NewService creates a service with the given name and register functions.
func NewService(name string, registers ...RegisterFunc) Service {
	return Service{
		Name:      name,
		Registers: registers,
	}
}

// NewServiceWithOptions creates a service with the given name and options.
func NewServiceWithOptions(name string, opts ...ServiceOption) Service {
	svc := Service{Name: name}
	for _, opt := range opts {
		if opt != nil {
			opt(&svc)
		}
	}
	return svc
}

// AddRegister appends register functions to the service.
func (s *Service) AddRegister(registers ...RegisterFunc) {
	s.Registers = append(s.Registers, registers...)
}

// WithRegister appends register functions to the service.
func WithRegister(registers ...RegisterFunc) ServiceOption {
	return func(s *Service) {
		s.Registers = append(s.Registers, registers...)
	}
}

// WithServiceUnaryInterceptors appends unary interceptors to the service.
func WithServiceUnaryInterceptors(interceptors ...grpcapi.UnaryServerInterceptor) ServiceOption {
	return func(s *Service) {
		s.Unary = append(s.Unary, interceptors...)
	}
}

// WithServiceStreamInterceptors appends stream interceptors to the service.
func WithServiceStreamInterceptors(interceptors ...grpcapi.StreamServerInterceptor) ServiceOption {
	return func(s *Service) {
		s.Stream = append(s.Stream, interceptors...)
	}
}

// WithInit sets the init hook for the service.
func WithInit(init func() error) ServiceOption {
	return func(s *Service) {
		s.Init = init
	}
}
