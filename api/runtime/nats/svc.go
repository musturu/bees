package nats

import "github.com/nats-io/nats.go"

// NATSService is the interface that domain services must implement
// to map NATS subjects to handler functions on registration.
type NATSService interface {
	Subjects() map[string]nats.MsgHandler
}
