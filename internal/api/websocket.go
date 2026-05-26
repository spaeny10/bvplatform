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

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"

	"ironsight/internal/auth"
	"ironsight/internal/config"
	"ironsight/internal/database"
	appmetrics "ironsight/internal/metrics"
)

// websocket.Upgrader is shared by every HandleWebSocket call.
// CheckOrigin is permissive because we authenticate via JWT ticket in the URL
// query string, not via same-origin. If that ever changes, tighten CheckOrigin here.
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// wsClient is a per-connection record that carries the authenticated identity
// and the pre-computed set of camera UUIDs the caller is allowed to receive
// events for. The allow-set is refreshed every 60 s by runRBACRefresher so
// role/assignment changes take effect without requiring a reconnect.
type wsClient struct {
	conn   *websocket.Conn
	claims *auth.Claims

	// allowedCameras is non-nil for restricted (customer-side) roles.
	// For global-view roles (admin/soc_*) we skip the set and use globalView=true.
	allowedCameras map[uuid.UUID]struct{}
	globalView     bool

	// rbacRefreshedAt tracks when the allow-set was last fetched from the DB.
	rbacRefreshedAt time.Time
}

// canSee reports whether this client should receive a message whose routing
// key is cameraID. The rules are:
//
//  1. nil wsClient -> default deny (defense in depth; half-registered clients).
//  2. globalView -> always true (admin / soc_* see everything including uuid.Nil
//     system-wide messages).
//  3. restricted client -> camera must be in the allow-set. uuid.Nil (no routing
//     key) is default-deny for restricted users.
func (c *wsClient) canSee(cameraID uuid.UUID) bool {
	if c == nil {
		return false
	}
	if c.globalView {
		return true
	}
	_, ok := c.allowedCameras[cameraID]
	return ok
}

// rbacResolverFn is the hook the RBAC refresher uses to fetch the allow-set
// for a client. In production it is defaultRBACResolver; in tests it is
// replaced by the test-local closure.
var rbacResolverFn = defaultRBACResolver

// defaultRBACResolver delegates to the package-level AuthorizedCameraIDs in
// authz.go. The returned restricted flag maps to wsClient.globalView (inverted:
// restricted=false <=> globalView=true).
func defaultRBACResolver(ctx context.Context, db *database.DB, claims *auth.Claims) ([]uuid.UUID, bool, error) {
	return AuthorizedCameraIDs(ctx, db, claims)
}

// Hub manages WebSocket clients on a single API replica and, when a
// Redis bridge is attached, fans out every broadcast to other replicas
// so clients connected to replica A see events emitted on replica B.
//
// The in-memory path is always active -- Redis is a decoration, not a
// requirement. If REDIS_URL is unset the hub behaves exactly as it did
// before Phase 2 work: one replica, one set of clients, no bridge.
type Hub struct {
	clients    map[*websocket.Conn]*wsClient
	broadcast  chan []byte
	register   chan *wsClient
	unregister chan *websocket.Conn
	mu         sync.RWMutex

	// rbacDB is the DB handle the RBAC refresher uses. Nil = refresher
	// is a no-op (safe default for deployments that never call SetRBACSource).
	rbacDB *database.DB

	// cfg is stored on the hub so HandleWebSocket (a method) can access it
	// without a closure factory. Set via Configure after NewHub.
	cfg *config.Config

	// Pub/sub bridge -- non-nil once a Redis client is attached. Nil in
	// single-replica / dev deployments; all methods check for nil.
	bridge *hubBridge
}

// hubBridge holds the Redis state for cross-replica fanout. Kept as a
// separate struct so the Hub's core responsibilities (local broadcast,
// client registry) stay readable; bridging is a pure layer on top.
type hubBridge struct {
	rdb      *redis.Client
	channel  string // pub/sub channel name -- one per deployment
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
// Use Configure to supply cfg+db before starting the hub.
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*websocket.Conn]*wsClient),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *wsClient),
		unregister: make(chan *websocket.Conn),
	}
}

// Configure supplies the application config and database handle to the hub.
// Must be called after NewHub and before Run. cfg is needed by HandleWebSocket
// to resolve WS auth credentials; db is used by the RBAC refresher.
// Replaces the former SetRBACSource-only wiring.
func (h *Hub) Configure(cfg *config.Config, db *database.DB) {
	if h == nil {
		return
	}
	h.cfg = cfg
	h.rbacDB = db
}

// SetRBACSource wires a database handle into the hub so the background RBAC
// refresher can query the live role/assignment state. Kept for backward
// compatibility; prefer Configure for new call sites.
func (h *Hub) SetRBACSource(db *database.DB) {
	if h == nil {
		return
	}
	h.rbacDB = db
}

// AttachRedisBridge wires a pub/sub bridge into the hub. Call at most
// once, after NewHub, before Run. `redisURL` is a standard Redis DSN
// like `redis://host:6379/0` -- empty string disables the bridge.
// `channel` is the pub/sub topic; use the same value on every replica
// in a deployment or they won't see each other's events.
//
// If Redis can't be reached, the bridge logs and the hub silently
// falls back to in-memory only. WS fanout isn't a hard dependency --
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

	// Quick connectivity probe -- fails fast if the operator pointed us at
	// a wrong host. Not fatal: we log and continue with in-memory only.
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		log.Printf("[WS] Redis bridge disabled: ping failed (%v) -- in-memory only", err)
		_ = rdb.Close()
		return nil
	}

	// Sender ID is a 64-bit random, hex-encoded. Regenerated each startup
	// so we don't persist across deploys (no reason to -- the bridge is
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
// context controls shutdown -- when it cancels, the bridge subscriber
// exits cleanly (releasing its Redis connection) and Run returns.
//
// Also starts the RBAC refresher goroutine at a 60s interval.
func (h *Hub) Run(ctx context.Context) {
	if h.bridge != nil {
		go h.runBridgeSubscriber(ctx)
	}
	// P1-A-04: RBAC background refresher. Runs alongside the event loop so
	// connected customers pick up site/role changes within ~60 s of the DB
	// change without having to reconnect.
	go h.runRBACRefresher(ctx, 60*time.Second)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[WS] Hub shutting down (%d clients)", len(h.clients))
			h.mu.Lock()
			for conn := range h.clients {
				conn.Close()
			}
			h.clients = make(map[*websocket.Conn]*wsClient)
			h.mu.Unlock()
			return

		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.conn] = client
			n := len(h.clients)
			h.mu.Unlock()
			appmetrics.SetWSClients(n)
			log.Printf("[WS] Client connected uid=%s role=%s global=%v (%d total)",
				client.claims.UserID, client.claims.Role, client.globalView, n)

		case conn := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[conn]; ok {
				delete(h.clients, conn)
				conn.Close()
			}
			n := len(h.clients)
			h.mu.Unlock()
			appmetrics.SetWSClients(n)
			log.Printf("[WS] Client disconnected (%d total)", n)

		case msg := <-h.broadcast:
			h.writeToClients(msg)
		}
	}
}

// runRBACRefresher is a background goroutine that refreshes the allowed-camera
// set for every restricted client on each tick. Global-view clients are skipped
// (no DB query needed). On DB error the cached set is kept unchanged so a
// transient outage does not flip a customer to "see nothing."
//
// The refresher is launched by Run and exits when ctx is cancelled.
func (h *Hub) runRBACRefresher(ctx context.Context, interval time.Duration) {
	log.Printf("[WS] RBAC refresher started (interval=%s)", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.refreshAllRBAC(ctx)
		}
	}
}

// refreshAllRBAC runs one RBAC refresh cycle: iterate every registered client,
// skip global-view clients, call the resolver for restricted clients, and
// update their allowedCameras set. Called from runRBACRefresher on each tick
// and directly in tests.
func (h *Hub) refreshAllRBAC(ctx context.Context) {
	if h.rbacDB == nil {
		return
	}
	appmetrics.IncRBACRefresh()

	h.mu.RLock()
	// Snapshot the client list under RLock so we don't hold the lock
	// across DB calls.
	clients := make([]*wsClient, 0, len(h.clients))
	for _, c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	for _, c := range clients {
		if c.globalView {
			continue // already sees everything; no DB query needed
		}
		ids, restricted, err := rbacResolverFn(ctx, h.rbacDB, c.claims)
		if err != nil {
			log.Printf("[WS] RBAC refresh error for uid=%s: %v -- keeping cached set",
				c.claims.UserID, err)
			appmetrics.IncRBACRefreshError()
			continue // keep the cached set on error
		}
		if !restricted {
			// Role was promoted to a global-view role.
			c.globalView = true
			c.allowedCameras = nil
		} else {
			newSet := make(map[uuid.UUID]struct{}, len(ids))
			for _, id := range ids {
				newSet[id] = struct{}{}
			}
			c.allowedCameras = newSet
		}
		c.rbacRefreshedAt = time.Now()
	}
}

// writeToClients delivers one message to every locally-connected client whose
// canSee check passes for the message's routing key. Dead connections are
// collected under RLock and removed under Lock to avoid the
// map-mutation-under-read-lock data race.
func (h *Hub) writeToClients(msg []byte) {
	routeKey := routeKeyFromMessage(msg)

	h.mu.RLock()
	var dead []*websocket.Conn
	for conn, client := range h.clients {
		if !client.canSee(routeKey) {
			continue
		}
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

// routeKeyFromMessage extracts the routing camera_id from a broadcast
// envelope. It checks the top-level "camera_id" field first; if absent,
// it falls back to "data.camera_id". Returns uuid.Nil for any message
// that lacks a valid camera_id (garbage JSON, missing key, malformed UUID).
//
// Top-level wins when both fields are present -- uniform fanout behaviour
// across the different envelope shapes the broadcast call sites emit.
func routeKeyFromMessage(raw []byte) uuid.UUID {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return uuid.Nil
	}
	// Top-level camera_id takes precedence.
	if v, ok := m["camera_id"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			if id, err := uuid.Parse(s); err == nil {
				return id
			}
		}
		return uuid.Nil // camera_id present but malformed
	}
	// Fall back to data.camera_id.
	if dataRaw, ok := m["data"]; ok {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(dataRaw, &nested); err == nil {
			if v, ok := nested["camera_id"]; ok {
				var s string
				if err := json.Unmarshal(v, &s); err == nil {
					if id, err := uuid.Parse(s); err == nil {
						return id
					}
				}
			}
		}
	}
	return uuid.Nil
}

// resolveWSAuth extracts and validates the caller's identity from a WebSocket
// upgrade request. It checks, in order:
//
//  1. ?ticket=<wsTicket> -- a short-lived WS ticket minted by GET /api/auth/ws-ticket.
//     Valid ticket -> return claims.
//  2. X-Forwarded-Email header -- the SSO trust path.
//  3. ?token=<sessionJWT> -- the LEGACY path. Intentionally IGNORED (returns
//     nil, nil) so the handler treats it as "no credential" and returns 401.
//     This closes the blast-radius window from P1-A-02 part 3.
//
// Returns (nil, nil) when no recognized credential is present -- the caller
// must respond 401. Returns (nil, error) only for a credential that was
// present but cryptographically invalid (expired ticket, bad signature).
func resolveWSAuth(req *http.Request, cfg *config.Config, db *database.DB) (*auth.Claims, error) {
	q := req.URL.Query()

	// 1. WS ticket (preferred path).
	if ticket := q.Get("ticket"); ticket != "" {
		claims, err := auth.ParseWSTicket(ticket, cfg.JWTSecret)
		if err != nil {
			return nil, err
		}
		return claims, nil
	}

	// 2. Legacy ?token= -- explicitly ignored. Return (nil, nil) so the
	//    handler 401s the same way as "no credential." This is the
	//    P1-A-02 part 3 closure: any code path that minted a session JWT
	//    and appended it as ?token= is now broken-by-design.
	if q.Get("token") != "" {
		return nil, nil
	}

	// 3. SSO trust header (X-Forwarded-Email). Only active when cfg has it
	//    configured, mirroring the RequireAuth middleware's behaviour.
	if cfg != nil && cfg.SSOTrustHeader == "email" {
		email := req.Header.Get("X-Forwarded-Email")
		if email != "" {
			return &auth.Claims{
				UserID:   email,
				Username: email,
				Role:     cfg.SSODefaultRole,
			}, nil
		}
	}

	// No credential recognized.
	return nil, nil
}

// Broadcast sends a message to all connected clients -- locally, and, if
// a Redis bridge is attached, to every other replica's clients too.
//
// The call site doesn't change from pre-P2: `hub.Broadcast(jsonBytes)`.
// The bridge is transparent.
func (h *Hub) Broadcast(msg []byte) {
	// Local delivery via the existing channel. We never block on this;
	// if the buffered channel is full the message is dropped -- that's
	// the same behavior as before, and preserves the "WS events are
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
			log.Printf("[WS] bridge subscribe failed: %v -- retrying in %s", err, backoff)
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
				continue // our own publish echoed back -- already delivered locally
			}
			h.writeToClients([]byte(envelope.Payload))
		}

		// Channel closed (disconnect). Loop and reconnect.
		_ = pubsub.Close()
		log.Printf("[WS] bridge subscriber disconnected -- reconnecting in %s", backoff)
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

// HandleWebSocket handles WebSocket upgrade requests. Auth is checked BEFORE
// the upgrade so unauthenticated callers get a plain HTTP 401 rather than a
// WebSocket close-frame.
//
// Credential precedence:
//  1. ?ticket=<wsTicket>  -- short-lived WS ticket from GET /api/auth/ws-ticket
//  2. X-Forwarded-Email   -- SSO header trust (when SSOTrustHeader == "email")
//  3. ?token=<sessionJWT> -- LEGACY, intentionally rejected (P1-A-02 part 3)
//
// cfg and db must be set via Configure before the first request arrives.
func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	claims, err := resolveWSAuth(r, h.cfg, h.rbacDB)
	if err != nil || claims == nil {
		if err != nil {
			log.Printf("[WS] auth error: %v", err)
		}
		http.Error(w, "authorization required", http.StatusUnauthorized)
		return
	}

	// Pre-compute the allow-set at upgrade time. This is the connect-time
	// snapshot; the refresher keeps it live thereafter.
	ids, restricted, err := AuthorizedCameraIDs(r.Context(), h.rbacDB, claims)
	if err != nil {
		log.Printf("[WS] RBAC lookup failed for uid=%s: %v", claims.UserID, err)
		http.Error(w, "authorization error", http.StatusInternalServerError)
		return
	}

	globalView := !restricted
	allowedCameras := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		allowedCameras[id] = struct{}{}
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WS] Upgrade error: %v", err)
		return
	}

	client := &wsClient{
		conn:            conn,
		claims:          claims,
		allowedCameras:  allowedCameras,
		globalView:      globalView,
		rbacRefreshedAt: time.Now(),
	}

	h.register <- client

	// Read loop -- keeps connection alive and surfaces client-side close.
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
