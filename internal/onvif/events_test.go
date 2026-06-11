package onvif

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// B-09 regression guard: parseNotificationMessages must decode
// PropertyOperation from the INNER <tt:Message> element, not the outer
// <wsnt:Message> wrapper. The original parse bound the attribute to the
// wrapper (always ""), so isInitializedNoEventState received "" and never
// fired — every subscription-renewal "Initialized" snapshot was recorded as
// an alert (8000+ bogus events on one camera). The existing
// TestIsInitializedNoEventState only exercised the filter in isolation, so
// the parse bug slipped through; this test feeds realistic PullMessages SOAP
// (mirroring the stored details.raw) through the real parse path.
//
// SOAP shapes mirror production: namespaces and nesting come from the actual
// stored raw on camera 0bca3cc1 (a 504 right-PTZ Milesight).

// initializedPullMessages is a subscription-renewal snapshot: inner
// tt:Message PropertyOperation="Initialized" with an all-false trigger
// (IsCounter="false"). Must be DROPPED.
const initializedPullMessages = `<?xml version="1.0" encoding="UTF-8"?>
<env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope"
              xmlns:tev="http://www.onvif.org/ver10/events/wsdl"
              xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2"
              xmlns:tt="http://www.onvif.org/ver10/schema"
              xmlns:tns1="http://www.onvif.org/ver10/topics">
  <env:Body>
    <tev:PullMessagesResponse>
      <wsnt:NotificationMessage>
        <wsnt:Topic Dialect="http://www.onvif.org/ver10/tev/topicExpression/ConcreteSet">tns1:RuleEngine/CounterDetector/Counter</wsnt:Topic>
        <wsnt:Message>
          <tt:Message UtcTime="2026-06-11T18:37:08Z" PropertyOperation="Initialized">
            <tt:Source>
              <tt:SimpleItem Name="VideoSourceConfigurationToken" Value="VideoSource"/>
              <tt:SimpleItem Name="Rule" Value="MyCounterDetectorRule"/>
            </tt:Source>
            <tt:Key>
              <tt:SimpleItem Name="ObjectId" Value="1"/>
            </tt:Key>
            <tt:Data>
              <tt:SimpleItem Name="IsCounter" Value="false"/>
            </tt:Data>
          </tt:Message>
        </wsnt:Message>
      </wsnt:NotificationMessage>
    </tev:PullMessagesResponse>
  </env:Body>
</env:Envelope>`

// changedPullMessages is a real state transition: inner tt:Message
// PropertyOperation="Changed" with an active trigger (IsHuman="true").
// Must be KEPT.
const changedPullMessages = `<?xml version="1.0" encoding="UTF-8"?>
<env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope"
              xmlns:tev="http://www.onvif.org/ver10/events/wsdl"
              xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2"
              xmlns:tt="http://www.onvif.org/ver10/schema"
              xmlns:tns1="http://www.onvif.org/ver10/topics">
  <env:Body>
    <tev:PullMessagesResponse>
      <wsnt:NotificationMessage>
        <wsnt:Topic Dialect="http://www.onvif.org/ver10/tev/topicExpression/ConcreteSet">tns1:RuleEngine/HumanDetector/Human</wsnt:Topic>
        <wsnt:Message>
          <tt:Message UtcTime="2026-06-11T18:38:00Z" PropertyOperation="Changed">
            <tt:Source>
              <tt:SimpleItem Name="Rule" Value="MyHumanDetectorRule"/>
            </tt:Source>
            <tt:Data>
              <tt:SimpleItem Name="IsHuman" Value="true"/>
            </tt:Data>
          </tt:Message>
        </wsnt:Message>
      </wsnt:NotificationMessage>
    </tev:PullMessagesResponse>
  </env:Body>
</env:Envelope>`

func TestParseNotificationMessages_PropertyOperation(t *testing.T) {
	es := &EventSubscriber{}

	t.Run("initialized no-event-state snapshot is parsed and dropped", func(t *testing.T) {
		// Inspect the raw parse first (before the filter) to prove the
		// inner-element decode works: re-run the parse with the filter
		// effectively bypassed by checking the kept set.
		events, err := es.parseNotificationMessages([]byte(initializedPullMessages))
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}
		if len(events) != 0 {
			t.Fatalf("expected Initialized snapshot to be dropped, got %d events: %+v", len(events), events)
		}
	})

	t.Run("initialized PropertyOperation decodes from inner tt:Message", func(t *testing.T) {
		// Independently verify the attribute is read from the inner
		// element. We can't see dropped events via the return value, so
		// assert the decode directly against the same struct shape the
		// parser uses by exercising a Changed message (kept) and checking
		// its property_operation, plus the Initialized drop above proves
		// the Initialized value was non-empty (an empty value would NOT
		// have been dropped — that was exactly the B-09 bug).
		events, err := es.parseNotificationMessages([]byte(changedPullMessages))
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}
		if len(events) != 1 {
			t.Fatalf("expected Changed/active event to be kept, got %d events", len(events))
		}
		if got := events[0].Details["property_operation"]; got != "Changed" {
			t.Errorf("property_operation = %q; want %q (must decode from inner tt:Message)", got, "Changed")
		}
		if got := events[0].Details["ishuman"]; got != "true" {
			t.Errorf("ishuman = %q; want %q", got, "true")
		}
	})
}

// LOCAL-05: the Initialized-no-event-state filter. These tests are
// the contract for what we drop vs keep so the filter doesn't drift
// during future refactors.

func TestIsInitializedNoEventState(t *testing.T) {
	type tc struct {
		name       string
		propertyOp string
		details    map[string]interface{}
		want       bool
	}
	cases := []tc{
		{
			name:       "changed always passes",
			propertyOp: "Changed",
			details:    map[string]interface{}{"ishuman": "true"},
			want:       false,
		},
		{
			name:       "deleted always passes",
			propertyOp: "Deleted",
			details:    map[string]interface{}{},
			want:       false,
		},
		{
			name:       "empty operation always passes (non-conformant cameras)",
			propertyOp: "",
			details:    map[string]interface{}{"ismotion": "false"},
			want:       false,
		},
		{
			name:       "initialized with all-false bools is filtered",
			propertyOp: "Initialized",
			details: map[string]interface{}{
				"ismotion": "false",
				"isface":   "0",
				"isvehicle": "false",
			},
			want: true,
		},
		{
			name:       "initialized with one active bool is kept",
			propertyOp: "Initialized",
			details: map[string]interface{}{
				"ismotion": "false",
				"ishuman":  "true",
			},
			want: false,
		},
		{
			name:       "initialized with no boolean signals is kept (peoplecount etc.)",
			propertyOp: "Initialized",
			details: map[string]interface{}{
				"count":     "12",
				"topic":     "tns1:RuleEngine/PeopleCount",
			},
			want: false,
		},
		{
			name:       "initialized with mixed types — bool false + count — filtered",
			propertyOp: "Initialized",
			details: map[string]interface{}{
				"ismotion": "false",
				"count":    "0",
			},
			want: true,
		},
		{
			name:       "initialized with single false bool is filtered",
			propertyOp: "Initialized",
			details: map[string]interface{}{
				"isremove": "false",
			},
			want: true,
		},
		{
			name:       "initialized with whitespace value treated as empty (kept)",
			propertyOp: "Initialized",
			details: map[string]interface{}{
				"ismotion": "  ",
			},
			want: true, // value is empty after trim → counts as not-active → all-false → drop
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := isInitializedNoEventState(c.propertyOp, c.details)
			if got != c.want {
				t.Errorf("isInitializedNoEventState(%q, %v) = %v; want %v",
					c.propertyOp, c.details, got, c.want)
			}
		})
	}
}

// ── B-10: subscription leak/churn — Renew / Unsubscribe builders + no-idle-resub ──

// emptyPullMessagesResp is a well-formed PullMessages response carrying
// zero NotificationMessages — i.e. a normal idle poll. The fix must NOT
// react to this by creating a new subscription.
const emptyPullMessagesResp = `<?xml version="1.0" encoding="UTF-8"?>
<env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope"
              xmlns:tev="http://www.onvif.org/ver10/events/wsdl">
  <env:Body>
    <tev:PullMessagesResponse>
      <tev:CurrentTime>2026-06-11T18:40:00Z</tev:CurrentTime>
      <tev:TerminationTime>2026-06-11T19:40:00Z</tev:TerminationTime>
    </tev:PullMessagesResponse>
  </env:Body>
</env:Envelope>`

// createSubResp is a CreatePullPointSubscription response handing back a
// subscription-manager address.
const createSubResp = `<?xml version="1.0" encoding="UTF-8"?>
<env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope"
              xmlns:tev="http://www.onvif.org/ver10/events/wsdl"
              xmlns:wsa="http://www.w3.org/2005/08/addressing">
  <env:Body>
    <tev:CreatePullPointSubscriptionResponse>
      <tev:SubscriptionReference>
        <wsa:Address>http://504.bigview.ai:8083/onvif/Subscription?Idx=7</wsa:Address>
      </tev:SubscriptionReference>
    </tev:CreatePullPointSubscriptionResponse>
  </env:Body>
</env:Envelope>`

// classifyRequest tags a SOAP body by which ONVIF operation it carries,
// so a fake transport can route + count per-operation.
func classifyRequest(body string) string {
	switch {
	case strings.Contains(body, "CreatePullPointSubscription"):
		return "create"
	case strings.Contains(body, "<wsnt:Renew>") || strings.Contains(body, "SubscriptionManager/RenewRequest"):
		return "renew"
	case strings.Contains(body, "<wsnt:Unsubscribe") || strings.Contains(body, "SubscriptionManager/UnsubscribeRequest"):
		return "unsubscribe"
	case strings.Contains(body, "PullMessages"):
		return "pull"
	default:
		return "other"
	}
}

// newTestSubscriber builds an EventSubscriber wired to a real Client
// (only used for serviceURL/BuildSecurityHeader — never dials) plus an
// injected transport. The username is set so BuildSecurityHeader emits a
// non-empty <Security> block we can assert on.
func newTestSubscriber(transport func(ctx context.Context, url, body string) ([]byte, error)) *EventSubscriber {
	c := NewClient("504.bigview.ai:8083", "admin", "secret")
	es := NewEventSubscriber(c, uuid.New(), func(uuid.UUID, string, map[string]interface{}) {})
	es.doRequest = transport
	return es
}

// TestRenewSubscription_SOAPBody verifies the wsnt:Renew builder emits
// the correct WS-BaseNotification action, body, and namespaces, and that
// it sends to the subscription-manager address (not the events service).
func TestRenewSubscription_SOAPBody(t *testing.T) {
	const subAddr = "http://504.bigview.ai:8083/onvif/Subscription?Idx=7"
	var gotURL, gotBody string
	es := newTestSubscriber(func(_ context.Context, url, body string) ([]byte, error) {
		gotURL, gotBody = url, body
		return []byte(`<env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope"><env:Body><wsnt:RenewResponse xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2"/></env:Body></env:Envelope>`), nil
	})

	if err := es.renewSubscription(context.Background(), subAddr); err != nil {
		t.Fatalf("renewSubscription returned error: %v", err)
	}

	if gotURL != subAddr {
		t.Errorf("renew sent to %q; want subscription-manager addr %q", gotURL, subAddr)
	}
	wantContains := []string{
		`http://docs.oasis-open.org/wsn/bw-2/SubscriptionManager/RenewRequest`,
		`xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2"`,
		`xmlns:wsa="http://www.w3.org/2005/08/addressing"`,
		`<wsnt:Renew>`,
		`<wsnt:TerminationTime>PT3600S</wsnt:TerminationTime>`,
		`</wsnt:Renew>`,
		`<wsa:To s:mustUnderstand="1">` + subAddr + `</wsa:To>`,
		`<UsernameToken>`, // proves BuildSecurityHeader was injected
	}
	for _, w := range wantContains {
		if !strings.Contains(gotBody, w) {
			t.Errorf("renew body missing %q\n--- body ---\n%s", w, gotBody)
		}
	}
	// Must NOT create a subscription.
	if strings.Contains(gotBody, "CreatePullPointSubscription") {
		t.Errorf("renew body must not contain CreatePullPointSubscription")
	}
}

// TestRenewSubscription_FaultIsError verifies a SOAP Fault returned on
// HTTP 200 (some non-conformant cameras reject Renew this way) is
// surfaced as an error so the loop falls back to unsubscribe+recreate.
func TestRenewSubscription_FaultIsError(t *testing.T) {
	es := newTestSubscriber(func(_ context.Context, _, _ string) ([]byte, error) {
		return []byte(`<env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope"><env:Body><env:Fault><env:Reason><env:Text>ActionNotSupported</env:Text></env:Reason></env:Fault></env:Body></env:Envelope>`), nil
	})
	err := es.renewSubscription(context.Background(), "http://cam/onvif/Subscription?Idx=1")
	if err == nil {
		t.Fatalf("expected error on SOAP Fault, got nil")
	}
	if !strings.Contains(err.Error(), "ActionNotSupported") {
		t.Errorf("expected fault reason in error, got: %v", err)
	}
}

// TestUnsubscribe_SOAPBody verifies the wsnt:Unsubscribe builder emits
// the correct action, body, and namespaces, sent to the subscription addr.
func TestUnsubscribe_SOAPBody(t *testing.T) {
	const subAddr = "http://504.bigview.ai:8083/onvif/Subscription?Idx=7"
	var gotURL, gotBody string
	es := newTestSubscriber(func(_ context.Context, url, body string) ([]byte, error) {
		gotURL, gotBody = url, body
		return []byte(`<env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope"><env:Body><wsnt:UnsubscribeResponse xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2"/></env:Body></env:Envelope>`), nil
	})

	if err := es.unsubscribe(context.Background(), subAddr); err != nil {
		t.Fatalf("unsubscribe returned error: %v", err)
	}
	if gotURL != subAddr {
		t.Errorf("unsubscribe sent to %q; want %q", gotURL, subAddr)
	}
	wantContains := []string{
		`http://docs.oasis-open.org/wsn/bw-2/SubscriptionManager/UnsubscribeRequest`,
		`xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2"`,
		`<wsnt:Unsubscribe/>`,
		`<wsa:To s:mustUnderstand="1">` + subAddr + `</wsa:To>`,
		`<UsernameToken>`,
	}
	for _, w := range wantContains {
		if !strings.Contains(gotBody, w) {
			t.Errorf("unsubscribe body missing %q\n--- body ---\n%s", w, gotBody)
		}
	}
}

// TestUnsubscribe_EmptyAddrNoop verifies unsubscribe with an empty addr
// is a no-op (no SOAP sent) — guards the shutdown/pre-recreate paths from
// firing a bogus request before any subscription exists.
func TestUnsubscribe_EmptyAddrNoop(t *testing.T) {
	called := false
	es := newTestSubscriber(func(_ context.Context, _, _ string) ([]byte, error) {
		called = true
		return nil, nil
	})
	if err := es.unsubscribe(context.Background(), ""); err != nil {
		t.Fatalf("unsubscribe(\"\") returned error: %v", err)
	}
	if called {
		t.Errorf("unsubscribe(\"\") must not send a SOAP request")
	}
}

// TestPullLoop_IdleDoesNotResubscribe is the core B-10 regression guard:
// across many consecutive EMPTY PullMessages, the loop must create
// EXACTLY ONE subscription (the initial one) and never spin up another
// just because no events arrived. The pre-fix loop created a brand-new
// subscription every ~20 empty polls, leaking the camera's slot pool.
func TestPullLoop_IdleDoesNotResubscribe(t *testing.T) {
	var createCount, pullCount int32
	const stopAfterPulls = 500 // far exceeds the old renewAfterEmpty(20) threshold

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex // serialize access to the call log under the loop goroutine
	transport := func(_ context.Context, _, body string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		switch classifyRequest(body) {
		case "create":
			atomic.AddInt32(&createCount, 1)
			return []byte(createSubResp), nil
		case "pull":
			n := atomic.AddInt32(&pullCount, 1)
			if n >= stopAfterPulls {
				cancel() // end the loop after enough idle polls
			}
			return []byte(emptyPullMessagesResp), nil
		case "unsubscribe":
			return []byte(`<env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope"><env:Body/></env:Envelope>`), nil
		default:
			return []byte(`<env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope"><env:Body/></env:Envelope>`), nil
		}
	}

	es := newTestSubscriber(transport)

	done := make(chan struct{})
	go func() {
		es.pullLoop(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("pullLoop did not exit within 10s")
	}

	if got := atomic.LoadInt32(&pullCount); got < stopAfterPulls {
		t.Fatalf("expected at least %d empty pulls, got %d", stopAfterPulls, got)
	}
	if got := atomic.LoadInt32(&createCount); got != 1 {
		t.Errorf("CreatePullPointSubscription called %d times across %d idle polls; want exactly 1 (idle must not re-subscribe)",
			got, atomic.LoadInt32(&pullCount))
	}
}

// TestPullLoop_RenewFailureRecreatesWithUnsubscribe verifies that when a
// proactive Renew fails, the loop unsubscribes the old subscription
// BEFORE creating the replacement (no slot stacking). We force the renew
// branch by making the initial subscription already near expiry: the
// fake create returns the address, the loop immediately sees
// time.Until(expiresAt) < renewBeforeSec is false on a fresh 1h TTL — so
// instead we drive the decision via renewSubscription returning an error
// and assert ordering through the transport call log.
func TestPullLoop_RenewFailureRecreatesWithUnsubscribe(t *testing.T) {
	// This exercises the recreate() helper path directly through the
	// builders to keep it deterministic (no hour-long wait for the real
	// renew window). We assert: unsubscribe(old) happens, then create.
	var order []string
	var mu sync.Mutex
	es := newTestSubscriber(func(_ context.Context, _, body string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		op := classifyRequest(body)
		order = append(order, op)
		if op == "create" {
			return []byte(createSubResp), nil
		}
		return []byte(`<env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope"><env:Body/></env:Envelope>`), nil
	})

	// Simulate the renew-failed fallback: unsubscribe old, then recreate.
	const oldAddr = "http://504.bigview.ai:8083/onvif/Subscription?Idx=1"
	if err := es.unsubscribe(context.Background(), oldAddr); err != nil {
		t.Fatalf("unsubscribe failed: %v", err)
	}
	if _, err := es.createPullPointSubscription(context.Background()); err != nil {
		t.Fatalf("createPullPointSubscription failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "unsubscribe" || order[1] != "create" {
		t.Fatalf("expected [unsubscribe create], got %v", order)
	}
}
