package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

// websocket.Upgrader is shared by every HandleWebSocket call.
// CheckOrigin is permissive because we authenticate via JWT in the URL/header,
// not via same-origin. If that ever changes, tighten CheckOrigin here.
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Hub manages WebSocket clients on a single API replica and, when a
// Redis bridge is attached, fans out every broadcast to other replicas
// so clients connected to replica A see events emitted on replica B.
//
// The in-memory path is always active — Redis is a decoration, not a
// requirement. If REDIS_URL is unset the hub behaves exactly as it did
// before Phase 2 work: one replica, one set of clients, no bridge.
type Hub struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan []byte
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	mu         sync.RWMutex

	// Pub/sub bridge — non-nil once a Redis client is attached. Nil in
	// single-replica / dev deployments; all methods check for nil.
	bridge *hubBridge
}

// hubBridge holds the Redis state for cross-replica fanout. Kept as a
// separate struct so the Hub's core responsibilities (local broadcast,
// client registry) stay readable; bridging is a pure layer on top.
type hubBridge struct {
	rdb      *redis.Client
	channel  string // pub/sub channel name — one per deployment
	senderID string // opaque random ID assigned at startup; drops our own
	                // echoes when subscribing to the channel we publish on
}

// pubsubEnvelope wraps every message sent through Redis with the
// publisher's sender ID so subscribers can ignore their own echoes.
// Redis Pub/Sub delivers to ALL subscribers including the publisher,
// so without this wrapper every broadcast would be delivered twice.
type pubsubEnvelope struct {
	SenderID string          `json:"s"`
	Payload  json.RawMessage `json:"p"`
}

// NewHub creates a new WebSocket hub with the in-memory path ready to use.
// Use AttachRedisBridge to add cross-replica fanout after construction.
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
	}
}

// AttachRedisBridge wires a pub/sub bridge into the hub. Call at most
// once, after NewHub, before Run. `redisURL` is a standard Redis DSN
// like `redis://host:6379/0` — empty string disables the bridge.
// `channel` is the pub/sub topic; use the same value on every replica
// in a deployment or they won't see each other's events.
//
// If Redis can't be reached, the bridge logs and the hub silently
// falls back to in-memory only. WS fanout isn't a hard dependency —
// we'd rather serve degraded than fail the whole API start.
func (h *Hub) AttachRedisBridge(ctx context.Context, redisURL, channel string) error {
	if redisURL == "" {
		return nil // no bridge requested, behave as before
	}
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return err
	}
	rdb := redis.NewClient(opts)

	// Quick connectivity probe — fails fast if the operator pointed us at
	// a wrong host. Not fatal: we log and continue with in-memory only.
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		log.Printf("[WS] Redis bridge disabled: ping failed (%v) — in-memory only", err)
		_ = rdb.Close()
		return nil
	}

	// Sender ID is a 64-bit random, hex-encoded. Regenerated each startup
	// so we don't persist across deploys (no reason to — the bridge is
	// stateless by design).
	var raw [8]byte
	_, _ = rand.Read(raw[:])
	senderID := hex.EncodeToString(raw[:])

	h.bridge = &hubBridge{
		rdb:      rdb,
		channel:  channel,
		senderID: senderID,
	}
	log.Printf("[WS] Redis bridge attached (channel=%s, sender=%s)", channel, senderID)
	return nil
}

// Run starts the hub's event loop. If a bridge is attached it also starts
// the subscriber goroutine so cross-replica messages flow inbound. The
// context controls shutdown — when it cancels, the bridge subscriber
// exits cleanly (releasing its Redis connection) and Run returns.
func (h *Hub) Run(ctx context.Context) {
	if h.bridge != nil {
		go h.runBridgeSubscriber(ctx)
	}
	for {
		select {
		case <-ctx.Done():
			log.Printf("[WS] Hub shutting down (%d clients)", len(h.clients))
			h.mu.Lock()
			for conn := range h.clients {
				conn.Close()
			}
			h.clients = make(map[*websocket.Conn]bool)
			h.mu.Unlock()
			return
		case conn := <-h.register:
			h.mu.Lock()
			h.clients[conn] = true
			h.mu.Unlock()
			log.Printf("[WS] Client connected (%d total)", len(h.clients))

		case conn := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[conn]; ok {
				delete(h.clients, conn)
				conn.Close()
			}
			h.mu.Unlock()
			log.Printf("[WS] Client disconnected (%d total)", len(h.clients))

		case msg := <-h.broadcast:
			h.writeToClients(msg)
		}
	}
}

// writeToClients delivers one message to every locally-connected client.
// Dead connections are collected under RLock and removed under Lock to
// avoid the map-mutation-under-read-lock data race that was here pre-P2.
func (h *Hub) writeToClients(msg []byte) {
	h.mu.RLock()
	var dead []*websocket.Conn
	for conn := range h.clients {
		if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			dead = append(dead, conn)
		}
	}
	h.mu.RUnlock()

	if len(dead) > 0 {
		h.mu.Lock()
		for _, conn := range dead {
			delete(h.clients, conn)
			conn.Close()
		}
		h.mu.Unlock()
	}
}

// Broadcast sends a message to all connected clients — locally, and, if
// a Redis bridge is attached, to every other replica's clients too.
//
// The call site doesn't change from pre-P2: `hub.Broadcast(jsonBytes)`.
// The bridge is transparent.
func (h *Hub) Broadcast(msg []byte) {
	// Local delivery via the existing channel. We never block on this;
	// if the buffered channel is full the message is dropped — that's
	// the same behaviour as before, and preserves the "WS events are
	// best-effort, not durable" contract.
	select {
	case h.broadcast <- msg:
	default:
		log.Println("[WS] Broadcast channel full, dropping message")
	}

	// Cross-replica delivery. We publish the message AFTER queueing it
	// locally so a slow Redis doesn't delay local clients. Subscribers
	// on OTHER replicas receive this and relay to their clients. Our own
	// subscriber ignores the echo by sender-ID match.
	if h.bridge != nil {
		envelope := pubsubEnvelope{
			SenderID: h.bridge.senderID,
			Payload:  json.RawMessage(msg),
		}
		payload, err := json.Marshal(envelope)
		if err != nil {
			log.Printf("[WS] bridge marshal failed: %v", err)
			return
		}
		// Fire-and-forget publish with a short timeout. If Redis is flaky,
		// we don't hold up the caller. Dropped cross-replica messages are
		// acceptable for a best-effort fanout.
		go func(data []byte) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := h.bridge.rdb.Publish(ctx, h.bridge.channel, data).Err(); err != nil {
				log.Printf("[WS] bridge publish failed: %v", err)
			}
		}(payload)
	}
}

// runBridgeSubscriber holds a SUBSCRIBE connection open and relays
// received messages to local clients. Auto-reconnects on disconnect so
// a Redis restart doesn't require an API restart to recover fanout.
func (h *Hub) runBridgeSubscriber(ctx context.Context) {
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		if ctx.Err() != nil {
			return
		}
		pubsub := h.bridge.rdb.Subscribe(ctx, h.bridge.channel)
		// Block until the subscription is actually active, otherwise the
		// first message from another replica could be missed before the
		// subscription completes.
		if _, err := pubsub.Receive(ctx); err != nil {
			log.Printf("[WS] bridge subscribe failed: %v — retrying in %s", err, backoff)
			_ = pubsub.Close()
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = nextBackoff(backoff, maxBackoff)
			continue
		}
		backoff = time.Second // reset on a successful attach
		log.Printf("[WS] bridge subscriber active on %q", h.bridge.channel)

		ch := pubsub.Channel()
		for msg := range ch {
			var envelope pubsubEnvelope
			if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
				log.Printf("[WS] bridge received malformed envelope: %v", err)
				continue
			}
			if envelope.SenderID == h.bridge.senderID {
				continue // our own publish echoed back — already delivered locally
			}
			h.writeToClients([]byte(envelope.Payload))
		}

		// Channel closed (disconnect). Loop and reconnect.
		_ = pubsub.Close()
		log.Printf("[WS] bridge subscriber disconnected — reconnecting in %s", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = nextBackoff(backoff, maxBackoff)
	}
}

// nextBackoff doubles the current delay up to a ceiling. Cheap; no jitter
// because we only reconnect on Redis-down which is rare enough that
// thundering-herd across replicas isn't a realistic concern for this hub.
func nextBackoff(current, max time.Duration) time.Duration {
	next := current * 2
	if next > max {
		return max
	}
	return next
}

// HandleWebSocket handles WebSocket upgrade requests.
func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WS] Upgrade error: %v", err)
		return
	}

	h.register <- conn

	// Read loop — keeps connection alive and surfaces client-side close.
	go func() {
		defer func() {
			h.unregister <- conn
		}()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	}()
}
