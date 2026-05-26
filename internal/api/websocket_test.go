package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"ironsight/internal/auth"
	"ironsight/internal/config"
	"ironsight/internal/database"
	"github.com/gorilla/websocket"
)

// TestRouteKeyFromMessage exercises every broadcast envelope shape the
// existing hub.Broadcast call sites emit, plus the malformed / missing-
// key fallbacks that must collapse to uuid.Nil so canSee restricts them
// to global-view roles.
func TestRouteKeyFromMessage(t *testing.T) {
	cam := uuid.New()
	cases := []struct {
		name string
		msg  any
		want uuid.UUID
	}{
		{
			name: "top-level camera_id (event message from cameras.go)",
			msg: map[string]any{
				"type":      "event",
				"camera_id": cam.String(),
				"event":     "motion",
			},
			want: cam,
		},
		{
			name: "nested data.camera_id (alert envelope)",
			msg: map[string]any{
				"type": "alert",
				"data": map[string]any{
					"id":        "alarm-abc",
					"camera_id": cam.String(),
				},
			},
			want: cam,
		},
		{
			name: "top-level wins when both present (uniform shape)",
			msg: map[string]any{
				"camera_id": cam.String(),
				"data":      map[string]any{"camera_id": uuid.New().String()},
			},
			want: cam,
		},
		{
			name: "missing key → uuid.Nil",
			msg:  map[string]any{"type": "system_announcement", "text": "deploy"},
			want: uuid.Nil,
		},
		{
			name: "malformed uuid → uuid.Nil (no half-parse)",
			msg:  map[string]any{"camera_id": "not-a-uuid"},
			want: uuid.Nil,
		},
		{
			name: "empty payload → uuid.Nil",
			msg:  map[string]any{},
			want: uuid.Nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.msg)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			got := routeKeyFromMessage(raw)
			if got != tc.want {
				t.Errorf("routeKeyFromMessage(%s) = %v, want %v", string(raw), got, tc.want)
			}
		})
	}
}

// TestRouteKeyFromMessage_NonJSON guarantees that malformed bytes don't
// panic — the broadcaster has to tolerate any payload because the bridge
// subscriber will replay whatever comes off Redis.
func TestRouteKeyFromMessage_NonJSON(t *testing.T) {
	got := routeKeyFromMessage([]byte("\x00\x01not-json"))
	if got != uuid.Nil {
		t.Errorf("routeKeyFromMessage(garbage) = %v, want uuid.Nil", got)
	}
}

// TestWSClient_CanSee_GlobalView verifies that admin / soc_* roles
// receive every message including the no-routing-key system case.
// Without this, a benign system-wide announcement would silently fail
// to deliver to the on-call operator.
func TestWSClient_CanSee_GlobalView(t *testing.T) {
	c := &wsClient{
		claims:     &auth.Claims{UserID: "admin-1", Role: "admin"},
		globalView: true,
	}
	if !c.canSee(uuid.New()) {
		t.Error("globalView client should see arbitrary camera id")
	}
	if !c.canSee(uuid.Nil) {
		t.Error("globalView client should see uuid.Nil (system-wide) messages")
	}
}

// TestWSClient_CanSee_Restricted verifies a customer user whose RBAC
// allows two specific cameras receives exactly those, never anything
// else, and never the no-key system case.
//
// THIS is the regression test for the cross-tenant leak P1-A-04 fixes.
// Pre-fix, every connected client received every Broadcast.
func TestWSClient_CanSee_Restricted(t *testing.T) {
	camA := uuid.New()
	camB := uuid.New()
	camForeign := uuid.New()
	c := &wsClient{
		claims: &auth.Claims{UserID: "customer-7", Role: "customer_admin"},
		allowedCameras: map[uuid.UUID]struct{}{
			camA: {},
			camB: {},
		},
		globalView: false,
	}
	if !c.canSee(camA) {
		t.Error("restricted client should see camA in its allow-set")
	}
	if !c.canSee(camB) {
		t.Error("restricted client should see camB in its allow-set")
	}
	if c.canSee(camForeign) {
		t.Error("restricted client must NOT see another tenant's camera")
	}
	if c.canSee(uuid.Nil) {
		t.Error("restricted client must NOT receive no-key system messages — default deny")
	}
}

// TestWSClient_CanSee_RestrictedZeroAssignments covers the legitimate
// "customer with no site assignments" state. Pre-fix, AuthorizedCameraIDs
// returning an empty list would have meant "no filter applied" (broken
// for years per phase-1 notes); the wsClient must instead treat it as
// "deliver nothing."
func TestWSClient_CanSee_RestrictedZeroAssignments(t *testing.T) {
	c := &wsClient{
		claims:         &auth.Claims{UserID: "u-empty", Role: "customer_viewer"},
		allowedCameras: map[uuid.UUID]struct{}{},
		globalView:     false,
	}
	if c.canSee(uuid.New()) {
		t.Error("restricted client with zero assignments must see nothing")
	}
	if c.canSee(uuid.Nil) {
		t.Error("restricted client with zero assignments must not see system-wide either")
	}
}

// TestWSClient_CanSee_NilClient guards against a panic if the broadcast
// loop ever sees a half-registered client. Defense in depth — the hub
// only stores fully-built wsClients today, but the writeToClients loop
// dereferences canSee without a nil check.
func TestWSClient_CanSee_NilClient(t *testing.T) {
	var c *wsClient
	if c.canSee(uuid.New()) {
		t.Error("nil wsClient must default-deny")
	}
}

// TestRBACRefresher_AppliesNewAllowSet drives one refresher tick with
// a fake resolver and asserts the connected restricted client picks
// up the new allow-set. This is the regression guard for the
// connect-time-snapshot bug — pre-fix, a customer added to a new
// site had to disconnect/reconnect to see events from cameras at the
// new site.
func TestRBACRefresher_AppliesNewAllowSet(t *testing.T) {
	camOld := uuid.New()
	camNew := uuid.New()

	// Stand the Hub up with a fake resolver that returns just camNew.
	origResolver := rbacResolverFn
	rbacResolverFn = func(_ context.Context, _ *database.DB, _ *auth.Claims) ([]uuid.UUID, bool, error) {
		return []uuid.UUID{camNew}, true, nil
	}
	t.Cleanup(func() { rbacResolverFn = origResolver })

	hub := NewHub()
	// Non-nil DB so refreshAllRBAC doesn't early-exit. The fake
	// resolver ignores it.
	hub.rbacDB = &database.DB{}

	client := &wsClient{
		conn:            &websocket.Conn{}, // zero-value; we never write to it in this test
		claims:          &auth.Claims{UserID: "customer-1", Role: "customer_admin"},
		allowedCameras:  map[uuid.UUID]struct{}{camOld: {}},
		globalView:      false,
		rbacRefreshedAt: time.Now().Add(-2 * time.Hour),
	}
	hub.clients[client.conn] = client

	hub.refreshAllRBAC(context.Background())

	if _, ok := client.allowedCameras[camOld]; ok {
		t.Error("refresh did not drop camOld from allow-set")
	}
	if _, ok := client.allowedCameras[camNew]; !ok {
		t.Error("refresh did not add camNew to allow-set")
	}
	if time.Since(client.rbacRefreshedAt) > time.Second {
		t.Errorf("rbacRefreshedAt not bumped; was %s ago", time.Since(client.rbacRefreshedAt))
	}
}

// TestRBACRefresher_PromotesToGlobalView covers the role-elevation
// case: a customer-side user becomes a SOC operator. The resolver
// returns restricted=false, and the refresher must flip globalView=true
// so canSee starts delivering every message.
func TestRBACRefresher_PromotesToGlobalView(t *testing.T) {
	origResolver := rbacResolverFn
	rbacResolverFn = func(_ context.Context, _ *database.DB, _ *auth.Claims) ([]uuid.UUID, bool, error) {
		return nil, false, nil
	}
	t.Cleanup(func() { rbacResolverFn = origResolver })

	hub := NewHub()
	hub.rbacDB = &database.DB{}

	client := &wsClient{
		conn:           &websocket.Conn{},
		claims:         &auth.Claims{UserID: "promoted-1", Role: "soc_operator"},
		allowedCameras: map[uuid.UUID]struct{}{uuid.New(): {}},
		globalView:     false,
	}
	hub.clients[client.conn] = client

	hub.refreshAllRBAC(context.Background())

	if !client.globalView {
		t.Error("refresh did not flip globalView=true after restricted=false response")
	}
	if !client.canSee(uuid.New()) {
		t.Error("post-refresh global client should now see arbitrary cameras")
	}
}

// TestRBACRefresher_SkipsGlobalView confirms the refresher doesn't
// waste a DB query on admin / soc_* clients — their canSee already
// returns true unconditionally.
func TestRBACRefresher_SkipsGlobalView(t *testing.T) {
	var calls int
	origResolver := rbacResolverFn
	rbacResolverFn = func(_ context.Context, _ *database.DB, _ *auth.Claims) ([]uuid.UUID, bool, error) {
		calls++
		return nil, false, nil
	}
	t.Cleanup(func() { rbacResolverFn = origResolver })

	hub := NewHub()
	hub.rbacDB = &database.DB{}

	admin := &wsClient{
		conn:       &websocket.Conn{},
		claims:     &auth.Claims{UserID: "admin-1", Role: "admin"},
		globalView: true,
	}
	hub.clients[admin.conn] = admin

	hub.refreshAllRBAC(context.Background())

	if calls != 0 {
		t.Errorf("resolver called %d times for an admin-only hub; want 0 (globalView clients should be skipped)", calls)
	}
}

// TestRBACRefresher_KeepsCachedSetOnDBError: a transient DB outage
// must not flip a customer to "deny everything" — that would be more
// disruptive than the staleness we're trying to close. The previous
// allow-set keeps governing fanout until the next successful refresh.
func TestRBACRefresher_KeepsCachedSetOnDBError(t *testing.T) {
	cam := uuid.New()
	origResolver := rbacResolverFn
	rbacResolverFn = func(_ context.Context, _ *database.DB, _ *auth.Claims) ([]uuid.UUID, bool, error) {
		return nil, true, errors.New("connection refused")
	}
	t.Cleanup(func() { rbacResolverFn = origResolver })

	hub := NewHub()
	hub.rbacDB = &database.DB{}

	client := &wsClient{
		conn:           &websocket.Conn{},
		claims:         &auth.Claims{UserID: "customer-2", Role: "customer_viewer"},
		allowedCameras: map[uuid.UUID]struct{}{cam: {}},
		globalView:     false,
	}
	hub.clients[client.conn] = client

	hub.refreshAllRBAC(context.Background())

	if _, ok := client.allowedCameras[cam]; !ok {
		t.Error("refresh-with-DB-error clobbered the cached allow-set; should have kept it")
	}
}

// TestRBACRefresher_NilDB asserts the refresher tick is a safe no-op
// when no RBAC source has been installed — the hub should not panic
// or block. Production deployments without SetRBACSource still work
// (just with connect-time-snapshot RBAC).
func TestRBACRefresher_NilDB(t *testing.T) {
	hub := NewHub()
	hub.refreshAllRBAC(context.Background()) // should return immediately
}

// TestResolveWSAuth_LegacyTokenIgnored is the regression guard for
// P1-A-02 part 3: a valid 24-hour session JWT presented in the legacy
// ?token= query parameter MUST be rejected. The old fallback let any
// session JWT upgrade a WS; we want it to fail closed now so no one
// accidentally re-introduces that surface.
func TestResolveWSAuth_LegacyTokenIgnored(t *testing.T) {
	secret := "ws-test-secret"
	cfg := &config.Config{JWTSecret: secret}
	// A genuine session JWT — must NOT be accepted on the WS upgrade.
	sessionTok, _, err := auth.SignToken("u-1", "alice", "admin", "Alice", "org-1", secret)
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/ws?token="+sessionTok, nil)
	claims, err := resolveWSAuth(req, cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if claims != nil {
		t.Errorf("session JWT presented as ?token= was accepted — legacy path not removed")
	}
}

// TestResolveWSAuth_TicketAccepted: the happy path now — a freshly
// minted WS ticket should yield the original session identity. Pairs
// with the legacy-token regression above to define the entire
// supported credential surface.
func TestResolveWSAuth_TicketAccepted(t *testing.T) {
	secret := "ws-test-secret"
	cfg := &config.Config{JWTSecret: secret}
	src := &auth.Claims{UserID: "u-2", Username: "bob", Role: "soc_operator"}
	ticket, err := auth.SignWSTicket(src, secret, auth.DefaultWSTicketTTL)
	if err != nil {
		t.Fatalf("SignWSTicket: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/ws?ticket="+ticket, nil)
	claims, err := resolveWSAuth(req, cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if claims == nil {
		t.Fatal("valid ticket rejected")
	}
	if claims.UserID != src.UserID || claims.Role != src.Role {
		t.Errorf("identity not preserved through ticket: got %+v", claims)
	}
}

// TestResolveWSAuth_NoCredential: the empty-handed upgrade must fail
// closed. This is the defense the WS upgrade handler relies on to
// return 401 before invoking the websocket.Upgrader.
func TestResolveWSAuth_NoCredential(t *testing.T) {
	cfg := &config.Config{JWTSecret: "any"}
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	claims, err := resolveWSAuth(req, cfg, nil)
	if err != nil || claims != nil {
		t.Errorf("expected (nil, nil), got (%v, %v)", claims, err)
	}
}
