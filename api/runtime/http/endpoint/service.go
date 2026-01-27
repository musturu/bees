package endpoint

import (
	"errors"
	"net/http"

	"bees/api/runtime/http/middleware"
)

// Service is a lightweight HTTPService bridge that registers typed endpoints.
type Service struct {
	mux   *http.ServeMux
	root  string
	chain middleware.Middleware
}

// Option configures a Service.
type Option func(*Service) error

// NewService creates a Service with the provided root and options.
func NewService(root string, opts ...Option) (*Service, error) {
	if root == "" {
		return nil, errors.New("handlers: root is required")
	}
	svc := &Service{mux: http.NewServeMux(), root: root}
	for _, opt := range opts {
		if err := opt(svc); err != nil {
			return nil, err
		}
	}
	return svc, nil
}

func (svc *Service) WithOpt(s ...Option) error {
	for _, opt := range s {
		err := opt(svc)
		if err != nil {
			return err
		}
	}
	return nil
}

// WithMux overrides the internal mux.
func WithMux(mux *http.ServeMux) Option {
	return func(s *Service) error {
		if mux == nil {
			return errors.New("handlers: mux is nil")
		}
		s.mux = mux
		return nil
	}
}

// WithChain sets the service middleware chain.
func WithChain(chain middleware.Middleware) Option {
	return func(s *Service) error {
		s.chain = chain
		return nil
	}
}

// EndpointOption registers a typed Endpoint into the service mux.
// The Endpoint.MethodPattern may be either a path ("/foo") or a method + path ("GET /foo").
func WithEndpoint[T any, R any](ep *Endpoint[T, R]) Option {
	return func(s *Service) error {
		if s.mux == nil {
			s.mux = http.NewServeMux()
		}
		if ep == nil {
			return errors.New("handlers: endpoint is nil")
		}
		h := ep.Handler()
		s.mux.Handle(ep.MethodPattern, h)
		return nil
	}
}

// Mux implements HTTPService.Mux
func (s *Service) Mux() *http.ServeMux { return s.mux }

// Chain implements HTTPService.Chain
func (s *Service) Chain() middleware.Middleware { return s.chain }

// Root implements HTTPService.Root
func (s *Service) Root() string { return s.root }
