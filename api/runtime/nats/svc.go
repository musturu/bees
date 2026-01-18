package nats

import (
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// NatsService defines a service that uses core NATS pub/sub messaging.
// Users implement this interface to define their service endpoints grouped by Root().
type NatsService interface {
	// Root returns the subject root for all endpoints (e.g., "users", "api.v1.orders").
	// Endpoints are subscribed to Root() + "." + key for each key in Mux().
	Root() string

	// Mux returns a map of endpoint names to handlers.
	// The full subject for each endpoint is Root() + "." + key.
	// Example: Root="users", Mux{"login": h} -> subscribes to "users.login"
	Mux() map[string]nats.MsgHandler

	// QueueGroup returns the queue group for load-balanced delivery.
	// Return nil/empty string for no queue group (all subscribers receive the message).
	QueueGroup() *string
}

// JetStreamService defines a service that uses JetStream for durable messaging.
type JetStreamService interface {
	// Root returns the subject root for all endpoints (e.g., "orders", "api.v1.events").
	// Endpoints are subscribed to Root() + "." + key for each key in Mux().
	Root() string

	// Mux returns a map of endpoint names to handlers.
	// The full subject for each endpoint is Root() + "." + key.
	// Example: Root="orders", Mux{"create": h} -> subscribes to "orders.create"
	Mux() map[string]jetstream.MessageHandler

	// QueueGroup returns the queue group for load-balanced delivery.
	// Return nil/empty string for no queue group.
	QueueGroup() *string

	// ConsumerConfig returns the JetStream consumer configuration.
	ConsumerConfig() *jetstream.ConsumerConfig
}
