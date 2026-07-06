package endpoint

import (
	"errors"

	"bees/api/runtime/http"
)

// ServiceOption configures an HTTPService implementation.
// It is applied by calling Init() on the returned configured service.
type ServiceOption func(http.HTTPService) error

// EndpointOption returns a ServiceOption that registers the provided
// typed endpoint into the service's mux.
func EndpointOption[T any, R any](ep *Endpoint[T, R]) ServiceOption {
	return func(s http.HTTPService) error {
		if s == nil {
			return errors.New("http: service is nil")
		}
		if ep == nil {
			return errors.New("http: endpoint is nil")
		}
		s.Mux().Handle(ep.MethodPattern, ep.Handler())
		return nil
	}
}
