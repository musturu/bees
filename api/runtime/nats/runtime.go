// Package nats provides a NATS runtime implementation for the bees microservice framework.
package nats

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

var (
	ErrNotConnected   = errors.New("nats: not connected")
	ErrAlreadyRunning = errors.New("nats: runtime already running")
	ErrInvalidService = errors.New("nats: invalid service type")
)

// Runtime manages NATS connections and service registration.
type Runtime struct {
	mu sync.RWMutex

	// Connection
	conn     *nats.Conn
	url      string
	natsOpts []nats.Option

	// Services
	coreServices []NatsService
	jsServices   []JetStreamService

	// JetStream
	js jetstream.JetStream

	// Subscriptions tracking for cleanup
	subs []*nats.Subscription

	// JetStream consumers for cleanup
	consumers []jetstream.ConsumeContext

	// Lifecycle
	running bool
	done    chan struct{}
}

// Option is a functional option for configuring the Runtime.
type Option func(*Runtime)

// NewRuntime creates a new NATS runtime with the given options.
func NewRuntime(opts ...Option) *Runtime {
	rt := &Runtime{
		url:  nats.DefaultURL,
		done: make(chan struct{}),
		natsOpts: []nats.Option{
			nats.Name("bees-service"),
			nats.ReconnectWait(2 * time.Second),
			nats.MaxReconnects(-1), // Infinite reconnects
		},
	}

	for _, opt := range opts {
		opt(rt)
	}

	return rt
}

// -----------------------------------------------------------------------------
// Functional Options
// -----------------------------------------------------------------------------

// WithURL sets the NATS server URL.
func WithURL(url string) Option {
	return func(rt *Runtime) {
		rt.url = url
	}
}

// WithNATSOptions appends additional nats.Option configurations.
// This allows direct access to all NATS library options.
func WithNATSOptions(opts ...nats.Option) Option {
	return func(rt *Runtime) {
		rt.natsOpts = append(rt.natsOpts, opts...)
	}
}

// WithCredentials configures NATS credentials file authentication.
func WithCredentials(credsFile string) Option {
	return func(rt *Runtime) {
		rt.natsOpts = append(rt.natsOpts, nats.UserCredentials(credsFile))
	}
}

// WithNKey configures NKey authentication.
func WithNKey(nkeyFile string) Option {
	return func(rt *Runtime) {
		opt, err := nats.NkeyOptionFromSeed(nkeyFile)
		if err != nil {
			slog.Error("failed to load nkey", "error", err)
			return
		}
		rt.natsOpts = append(rt.natsOpts, opt)
	}
}

// WithUserPassword configures basic user/password authentication.
func WithUserPassword(user, password string) Option {
	return func(rt *Runtime) {
		rt.natsOpts = append(rt.natsOpts, nats.UserInfo(user, password))
	}
}

// WithToken configures token authentication.
func WithToken(token string) Option {
	return func(rt *Runtime) {
		rt.natsOpts = append(rt.natsOpts, nats.Token(token))
	}
}

// WithName sets the NATS connection name.
func WithName(name string) Option {
	return func(rt *Runtime) {
		rt.natsOpts = append(rt.natsOpts, nats.Name(name))
	}
}

// WithReconnectWait sets the time to wait between reconnection attempts.
func WithReconnectWait(d time.Duration) Option {
	return func(rt *Runtime) {
		rt.natsOpts = append(rt.natsOpts, nats.ReconnectWait(d))
	}
}

// WithMaxReconnects sets the maximum number of reconnection attempts (-1 for infinite).
func WithMaxReconnects(n int) Option {
	return func(rt *Runtime) {
		rt.natsOpts = append(rt.natsOpts, nats.MaxReconnects(n))
	}
}

// Register registers a service with the runtime.
// Supports NatsService (core NATS) and JetStreamService.
func (rt *Runtime) Register(svc interface{}) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	switch s := svc.(type) {
	case NatsService:
		if s == nil {
			return fmt.Errorf("%w: service is nil", ErrInvalidService)
		}
		rt.coreServices = append(rt.coreServices, s)
		return nil
	case JetStreamService:
		if s == nil {
			return fmt.Errorf("%w: service is nil", ErrInvalidService)
		}
		rt.jsServices = append(rt.jsServices, s)
		return nil
	default:
		return fmt.Errorf("%w: got %T", ErrInvalidService, svc)
	}
}

// Run connects to NATS and starts serving registered services.
func (rt *Runtime) Run() error {
	rt.mu.Lock()
	if rt.running {
		rt.mu.Unlock()
		return ErrAlreadyRunning
	}
	if rt.done == nil {
		rt.done = make(chan struct{})
	}

	// Connect to NATS
	conn, err := nats.Connect(rt.url, rt.natsOpts...)
	if err != nil {
		rt.mu.Unlock()
		return fmt.Errorf("nats connect: %w", err)
	}
	rt.conn = conn

	// Initialize JetStream (may be used by services)
	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		rt.mu.Unlock()
		return fmt.Errorf("jetstream init: %w", err)
	}
	rt.js = js

	// Register all core NATS services
	for _, svc := range rt.coreServices {
		if err := rt.registerCoreService(svc); err != nil {
			conn.Close()
			rt.mu.Unlock()
			return err
		}
	}

	// Register all JetStream services
	for _, svc := range rt.jsServices {
		if err := rt.registerJetStreamService(svc); err != nil {
			conn.Close()
			rt.mu.Unlock()
			return err
		}
	}

	rt.running = true
	rt.mu.Unlock()

	slog.Info("nats runtime started", "url", rt.url)

	// Block until done
	<-rt.done
	return nil
}

// Stop gracefully shuts down the NATS connection.
func (rt *Runtime) Stop(ctx context.Context) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if !rt.running {
		return nil
	}

	slog.Info("nats runtime stopping")

	// Stop JetStream consumers
	for _, c := range rt.consumers {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c.Stop()
	}

	// Unsubscribe all subscriptions
	for _, sub := range rt.subs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := sub.Drain(); err != nil {
			slog.Warn("failed to drain subscription", "subject", sub.Subject, "error", err)
		}
	}

	// Drain the connection (waits for pending messages)
	if rt.conn != nil {
		done := make(chan error, 1)
		go func() {
			done <- rt.conn.Drain()
		}()
		select {
		case <-ctx.Done():
			rt.conn.Close()
			return ctx.Err()
		case err := <-done:
			if err != nil {
				slog.Warn("failed to drain connection", "error", err)
			}
		}
	}

	rt.running = false
	if rt.done != nil {
		close(rt.done)
		rt.done = nil
	}

	slog.Info("nats runtime stopped")
	return nil
}

// registerCoreService registers a NatsService by subscribing to Root() + "." + key for each mux entry.
func (rt *Runtime) registerCoreService(svc NatsService) error {
	root := svc.Root()
	mux := svc.Mux()
	queueGroup := svc.QueueGroup()

	if root == "" {
		return fmt.Errorf("service root cannot be empty")
	}

	if len(mux) == 0 {
		return fmt.Errorf("service mux is empty")
	}

	for key, handler := range mux {
		if handler == nil {
			slog.Warn("handler is nil, skipping", "endpoint", key)
			continue
		}

		subject := root + "." + key
		qg := ""
		if queueGroup != nil {
			qg = *queueGroup
		}

		var sub *nats.Subscription
		var err error

		if qg != "" {
			sub, err = rt.conn.QueueSubscribe(subject, qg, handler)
			slog.Debug("subscribed", "subject", subject, "queue", qg)
		} else {
			sub, err = rt.conn.Subscribe(subject, handler)
			slog.Debug("subscribed", "subject", subject)
		}

		if err != nil {
			return fmt.Errorf("subscribe %s: %w", subject, err)
		}

		rt.subs = append(rt.subs, sub)
	}

	return nil
}

// registerJetStreamService registers a JetStreamService.
func (rt *Runtime) registerJetStreamService(svc JetStreamService) error {
	ctx := context.Background()

	root := svc.Root()
	mux := svc.Mux()
	consumerCfg := svc.ConsumerConfig()

	if root == "" {
		return fmt.Errorf("service root cannot be empty")
	}

	if len(mux) == 0 {
		return fmt.Errorf("service mux is empty")
	}

	if consumerCfg == nil {
		return fmt.Errorf("jetstream service missing consumer config")
	}

	// Build subjects from root + mux keys
	subjects := make([]string, 0, len(mux))
	for key := range mux {
		subjects = append(subjects, root+"."+key)
	}

	// Create stream config with subjects
	streamCfg := jetstream.StreamConfig{
		Name:     root + "_stream",
		Subjects: subjects,
	}

	// Create or update stream
	stream, err := rt.js.CreateOrUpdateStream(ctx, streamCfg)
	if err != nil {
		return fmt.Errorf("create stream %s: %w", streamCfg.Name, err)
	}

	// Create or update consumer
	consumer, err := stream.CreateOrUpdateConsumer(ctx, *consumerCfg)
	if err != nil {
		return fmt.Errorf("create consumer %s: %w", consumerCfg.Name, err)
	}

	// Build combined handler that routes to appropriate mux entry
	jsHandler := func(msg jetstream.Msg) {
		subject := msg.Subject()

		// Extract the key from subject: root.key
		if len(subject) > len(root)+1 {
			key := subject[len(root)+1:]
			if handler, ok := mux[key]; ok {
				handler(msg)
			} else {
				slog.Warn("no handler found for subject", "subject", subject)
				msg.Nak()
			}
		} else {
			slog.Warn("invalid subject format", "subject", subject, "root", root)
			msg.Nak()
		}
	}

	// Start consumer (push-based delivery by default)
	consumeCtx, err := consumer.Consume(jsHandler)
	if err != nil {
		return fmt.Errorf("consume %s: %w", consumerCfg.Name, err)
	}
	rt.consumers = append(rt.consumers, consumeCtx)

	slog.Debug("jetstream service registered",
		"root", root,
		"subjects", subjects,
		"consumer", consumerCfg.Name)

	return nil
}

// Conn returns the underlying NATS connection.
// Returns nil if not connected.
func (rt *Runtime) Conn() *nats.Conn {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.conn
}

// JetStream returns the JetStream context.
// Returns nil if not initialized.
func (rt *Runtime) JetStream() jetstream.JetStream {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.js
}
