package nats

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go"
)

// Config configures the NATS Runtime
type Config struct {
	URL string `json:"url"`
}

// Runtime implements api.Runtime for NATS Pub/Sub
type Runtime struct {
	Config Config
	conn   *nats.Conn
	subs   []*nats.Subscription
}

// Init connects to the NATS server
func (rt *Runtime) Init() error {
	url := rt.Config.URL
	if url == "" {
		url = nats.DefaultURL
	}
	conn, err := nats.Connect(url)
	if err != nil {
		return err
	}
	rt.conn = conn
	return nil
}

// Register maps NATSService subscriptions to the current connection
func (rt *Runtime) Register(svc interface{}) error {
	natsSvc, ok := svc.(NATSService)
	if !ok {
		return fmt.Errorf("nats runtime: unsupported service type, expected nats.NATSService")
	}

	if rt.conn == nil {
		return fmt.Errorf("nats runtime: not initialized, call Init() first")
	}

	for subject, handler := range natsSvc.Subjects() {
		sub, err := rt.conn.Subscribe(subject, handler)
		if err != nil {
			return err
		}
		rt.subs = append(rt.subs, sub)
		slog.Info("nats runtime: subscribed", slog.String("subject", subject))
	}
	return nil
}

// Run blocks as subscriptions run in background threads
func (rt *Runtime) Run() error {
	slog.Info("nats runtime: listening for messages")
	// Block until context cancellation or Stop()
	select {}
}

// Stop gracefully closes NATS connections
func (rt *Runtime) Stop(ctx context.Context) error {
	slog.Info("nats runtime: stopping, draining connection")
	if rt.conn != nil {
		return rt.conn.Drain()
	}
	return nil
}

// Publish is a helper allowing application logic to publish events easily
func (rt *Runtime) Publish(subject string, data []byte) error {
	if rt.conn == nil {
		return fmt.Errorf("nats runtime: not initialized")
	}
	return rt.conn.Publish(subject, data)
}

// Conn provides access to the underlying connection for advanced use cases
func (rt *Runtime) Conn() *nats.Conn {
	return rt.conn
}
