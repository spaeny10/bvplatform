package onvif

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// EventCallback is called when an ONVIF event is received
type EventCallback func(cameraID uuid.UUID, eventType string, details map[string]interface{})

// EventClassifierFunc is an optional hook for manufacturer-specific event
// classification. Return a non-empty string to override the generic classifier.
type EventClassifierFunc func(topic string) string

// EventEnricherFunc is an optional hook for manufacturer-specific event
// metadata extraction. Returns additional key-value pairs to merge.
type EventEnricherFunc func(topic string, rawXML string) map[string]interface{}

// EventSubscriber manages ONVIF event subscriptions for a camera
type EventSubscriber struct {
	client   *Client
	cameraID uuid.UUID
	callback EventCallback
	stopCh   chan struct{}
	running  bool
	mu       sync.Mutex
	Classify EventClassifierFunc // optional: vendor-specific topic classifier
	Enrich   EventEnricherFunc   // optional: vendor-specific metadata extractor

	// doRequest is the SOAP transport. It defaults to es.client.DoRequest
	// but can be overridden in tests with a fake that returns canned SOAP
	// (and counts CreatePullPointSubscription calls) so the pull loop and
	// the Renew/Unsubscribe builders are exercisable without a real camera.
	doRequest func(ctx context.Context, url, body string) ([]byte, error)
}

// do routes a SOAP request through the injected transport when set,
// otherwise through the real ONVIF client. Lets tests intercept every
// SOAP call (create / pull / renew / unsubscribe) deterministically.
func (es *EventSubscriber) do(ctx context.Context, url, body string) ([]byte, error) {
	if es.doRequest != nil {
		return es.doRequest(ctx, url, body)
	}
	return es.client.DoRequest(ctx, url, body)
}

// InjectEvent allows external event sources (e.g. Milesight WebSocket) to fire
// events through the same callback pipeline as ONVIF PullPoint events.
func (es *EventSubscriber) InjectEvent(cameraID uuid.UUID, eventType string, details map[string]interface{}) {
	es.callback(cameraID, eventType, details)
}

// NewEventSubscriber creates a new event subscriber for a camera
func NewEventSubscriber(client *Client, cameraID uuid.UUID, callback EventCallback) *EventSubscriber {
	return &EventSubscriber{
		client:   client,
		cameraID: cameraID,
		callback: callback,
		stopCh:   make(chan struct{}),
	}
}

// Start begins pulling events from the camera
func (es *EventSubscriber) Start(ctx context.Context) error {
	es.mu.Lock()
	if es.running {
		es.mu.Unlock()
		return nil
	}
	es.running = true
	es.mu.Unlock()

	log.Printf("[EVENTS] Starting event subscription for camera %s", es.cameraID)

	go es.pullLoop(ctx)
	return nil
}

// Stop terminates the event subscription
func (es *EventSubscriber) Stop() {
	es.mu.Lock()
	defer es.mu.Unlock()
	if es.running {
		close(es.stopCh)
		es.running = false
		log.Printf("[EVENTS] Stopped event subscription for camera %s", es.cameraID)
	}
}

// pullLoop continuously pulls events from the camera using a single
// PullPoint subscription that it keeps alive with real WS-Notification
// Renews.
//
// B-10 (subscription leak/churn) — what changed and why:
//   - NO MORE idle re-subscribe. "No events" is the normal steady state
//     (the camera only emits on a real rule transition). The old loop
//     created a brand-new subscription after ~60s of empty polls, which
//     made the camera re-dump its entire Initialized rule state and
//     consumed another slot in its small subscription pool (~4–5),
//     never releasing the old one → "Maximum number of Subscribe
//     reached". We now just keep polling the same subscriptionAddr; the
//     empty counter is logging-only.
//   - Proactive renewal uses a real wsnt:Renew (renewSubscription),
//     which extends the SAME subscription's TTL — no new slot, no
//     Initialized snapshot. Only if Renew fails do we unsubscribe the
//     old addr and recreate.
//   - Every replace (renew-failed fallback + maxErrors-died path)
//     unsubscribes the old addr first, and the loop unsubscribes the
//     live addr on shutdown — so we stop leaking slots across renewals
//     and app restarts.
func (es *EventSubscriber) pullLoop(ctx context.Context) {
	const subscriptionTTL = 3600 * time.Second // must match PT3600S in create/renew
	const renewBeforeSec = 300 * time.Second   // renew 5 minutes before expiry
	const logEmptyEvery = 100                   // log a heartbeat every ~100 empty polls (~5min)
	const maxErrors = 10

	// subscriptionAddr is owned by this goroutine; the deferred
	// unsubscribe below reads it on exit (no concurrent access).
	var subscriptionAddr string
	var expiresAt time.Time

	newSubscription := func() (string, time.Time, error) {
		addr, err := es.createPullPointSubscription(ctx)
		if err != nil {
			log.Printf("[EVENTS] Failed to create subscription for camera %s: %v", es.cameraID, err)
			return "", time.Time{}, err
		}
		log.Printf("[EVENTS] PullPoint subscription created for camera %s", es.cameraID)
		return addr, time.Now().Add(subscriptionTTL), nil
	}

	// recreate releases the current (dead/expiring) subscription before
	// creating a fresh one, so we never stack a new slot on top of an old
	// one. Best-effort unsubscribe — a dead sub can't be released anyway.
	recreate := func() (string, time.Time, error) {
		if subscriptionAddr != "" {
			if err := es.unsubscribe(ctx, subscriptionAddr); err != nil {
				log.Printf("[EVENTS] Unsubscribe (pre-recreate) failed for camera %s (continuing): %v", es.cameraID, err)
			}
		}
		return newSubscription()
	}

	// Release the camera-side slot promptly on shutdown / context-cancel
	// instead of leaking it until the PT3600S TTL reaps it. Uses a fresh
	// short-lived context because ctx may already be canceled here.
	defer func() {
		if subscriptionAddr == "" {
			return
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := es.unsubscribe(shutdownCtx, subscriptionAddr); err != nil {
			log.Printf("[EVENTS] Unsubscribe on shutdown failed for camera %s (continuing): %v", es.cameraID, err)
		} else {
			log.Printf("[EVENTS] Unsubscribed camera %s on shutdown", es.cameraID)
		}
	}()

	// Initial subscription — retry until success or context canceled.
	// When the camera reports its subscription cap is exhausted (typical
	// on Milesight/Hikvision after stale subs leak across restarts) we
	// back off much harder: retrying every 60s would just keep leaking
	// and hammering the device. The camera reaps stale subs on its
	// PT3600S TTL, so 5-minute waits give the pool a chance to drain.
	for subscriptionAddr == "" {
		addr, exp, err := newSubscription()
		if addr != "" {
			subscriptionAddr = addr
			expiresAt = exp
			break
		}
		wait := 60 * time.Second
		if isSubscriptionCapError(err) {
			wait = 5 * time.Minute
			log.Printf("[EVENTS] Camera %s subscription cap exhausted — waiting %s for stale subs to expire", es.cameraID, wait)
		} else {
			log.Printf("[EVENTS] Retrying PullPoint subscription for camera %s in %s", es.cameraID, wait)
		}
		select {
		case <-time.After(wait):
		case <-es.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}

	consecutiveErrors := 0
	consecutiveEmpty := 0

	for {
		select {
		case <-es.stopCh:
			return
		case <-ctx.Done():
			return
		default:
		}

		// Proactive renewal: extend the EXISTING subscription's TTL with a
		// real wsnt:Renew before it expires. No new subscription, no
		// Initialized snapshot, no extra slot consumed. Only if the camera
		// rejects/fails the Renew do we unsubscribe-old + recreate.
		if time.Until(expiresAt) < renewBeforeSec {
			log.Printf("[EVENTS] Renewing subscription for camera %s (expires in %s)", es.cameraID, time.Until(expiresAt).Round(time.Second))
			if err := es.renewSubscription(ctx, subscriptionAddr); err == nil {
				expiresAt = time.Now().Add(subscriptionTTL)
			} else {
				log.Printf("[EVENTS] Renew failed for camera %s (%v) — unsubscribing old + recreating", es.cameraID, err)
				if addr, exp, _ := recreate(); addr != "" {
					subscriptionAddr = addr
					expiresAt = exp
					consecutiveErrors = 0
					consecutiveEmpty = 0
				}
			}
		}

		events, err := es.pullMessages(ctx, subscriptionAddr)
		if err != nil {
			consecutiveErrors++
			if consecutiveErrors%5 == 1 {
				log.Printf("[EVENTS] Pull error for camera %s (%d/%d): %v", es.cameraID, consecutiveErrors, maxErrors, err)
			}
			if consecutiveErrors >= maxErrors {
				// Subscription is genuinely dead — release the old slot and
				// force a fresh one immediately.
				log.Printf("[EVENTS] Subscription lost for camera %s — re-subscribing", es.cameraID)
				addr, exp, subErr := recreate()
				if addr != "" {
					subscriptionAddr = addr
					expiresAt = exp
					consecutiveErrors = 0
					consecutiveEmpty = 0
				} else {
					// Camera unreachable — the old addr is gone (we already
					// tried to unsubscribe it). Back off and retry; stretch
					// the wait on the subscription-cap error so we don't keep
					// adding to the leak.
					subscriptionAddr = ""
					wait := 30 * time.Second
					if isSubscriptionCapError(subErr) {
						wait = 5 * time.Minute
					}
					select {
					case <-time.After(wait):
					case <-es.stopCh:
						return
					case <-ctx.Done():
						return
					}
					// Re-establish before resuming the poll loop.
					for subscriptionAddr == "" {
						a, e, err2 := newSubscription()
						if a != "" {
							subscriptionAddr = a
							expiresAt = e
							consecutiveErrors = 0
							consecutiveEmpty = 0
							break
						}
						w := 60 * time.Second
						if isSubscriptionCapError(err2) {
							w = 5 * time.Minute
						}
						select {
						case <-time.After(w):
						case <-es.stopCh:
							return
						case <-ctx.Done():
							return
						}
					}
				}
			} else {
				time.Sleep(2 * time.Second)
			}
			continue
		}
		consecutiveErrors = 0

		if len(events) == 0 {
			// No events is the NORMAL state — do NOT re-subscribe here.
			// Just keep polling the same subscription. Counter is for a
			// periodic heartbeat log only.
			consecutiveEmpty++
			if consecutiveEmpty%logEmptyEvery == 0 {
				log.Printf("[EVENTS] Camera %s idle (%d empty polls) — still subscribed, next renew in %s",
					es.cameraID, consecutiveEmpty, time.Until(expiresAt).Round(time.Second))
			}
			continue
		}

		consecutiveEmpty = 0
		for _, evt := range events {
			es.callback(es.cameraID, evt.Type, evt.Details)
		}
	}
}

type parsedEvent struct {
	Type    string
	Details map[string]interface{}
}

// createPullPointSubscription creates an ONVIF PullPoint subscription
func (es *EventSubscriber) createPullPointSubscription(ctx context.Context) (string, error) {
	// Use the events service URL discovered via GetCapabilities. Vendors
	// disagree on the path — e.g. Milesight serves at /onvif/Events
	// while Hikvision uses /onvif/Events too but older firmware is on
	// /onvif/event_service. The naive string-replace fallback here only
	// triggers if discovery never populated eventsURL.
	eventsAddr := es.client.serviceURL("events", "event_service")

	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:tev="http://www.onvif.org/ver10/events/wsdl"
            xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2">
  <s:Header>%s</s:Header>
  <s:Body>
    <tev:CreatePullPointSubscription>
      <tev:InitialTerminationTime>PT3600S</tev:InitialTerminationTime>
    </tev:CreatePullPointSubscription>
  </s:Body>
</s:Envelope>`, es.client.BuildSecurityHeader())

	resp, err := es.do(ctx, eventsAddr, body)
	if err != nil {
		return "", err
	}

	// Extract subscription reference address
	type subscriptionResponse struct {
		XMLName xml.Name `xml:"Envelope"`
		Body    struct {
			Response struct {
				SubscriptionReference struct {
					Address string `xml:"Address"`
				} `xml:"SubscriptionReference"`
			} `xml:"CreatePullPointSubscriptionResponse"`
		} `xml:"Body"`
	}

	var parsed subscriptionResponse
	if err := xml.Unmarshal(resp, &parsed); err != nil {
		return "", fmt.Errorf("parse subscription response: %w", err)
	}

	addr := parsed.Body.Response.SubscriptionReference.Address
	if addr == "" {
		return "", fmt.Errorf("empty subscription address")
	}

	// Rewrite the host in the subscription URL to match the host we used to
	// connect to the camera. Cameras behind NAT (e.g. cellular 5G routers)
	// self-report their local LAN IP in ONVIF responses, but the reachable
	// address is the external IP stored in es.client.XAddr.
	// Compare hostnames only (without port) to avoid false rewrites when the
	// camera includes an explicit :80 but our XAddr doesn't.
	if clientURL, err := url.Parse(es.client.XAddr); err == nil {
		if subURL, err := url.Parse(addr); err == nil {
			if subURL.Hostname() != clientURL.Hostname() {
				log.Printf("[EVENTS] Rewriting subscription URL host from %s to %s for camera %s",
					subURL.Host, clientURL.Host, es.cameraID)
				subURL.Host = clientURL.Host
				addr = subURL.String()
			}
		}
	}

	return addr, nil
}

// pullMessages pulls messages from a PullPoint subscription.
//
// The SOAP envelope carries WS-Addressing headers (wsa:Action, wsa:To,
// wsa:MessageID) in addition to WS-Security. Newer Milesight firmware
// (MS-Cx2xx, AI series) ignores these and works either way; older
// firmware (MS-C5xxx Pro Series) returns ResourceUnknownFault on the
// first Pull when they're missing because their event handler routes
// requests by the wsa:To header rather than the URL path.
func (es *EventSubscriber) pullMessages(ctx context.Context, subscriptionAddr string) ([]parsedEvent, error) {
	const action = "http://www.onvif.org/ver10/events/wsdl/PullPointSubscription/PullMessagesRequest"
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:tev="http://www.onvif.org/ver10/events/wsdl"
            xmlns:wsa="http://www.w3.org/2005/08/addressing">
  <s:Header>
    <wsa:Action s:mustUnderstand="1">%s</wsa:Action>
    <wsa:To s:mustUnderstand="1">%s</wsa:To>
    <wsa:MessageID>urn:uuid:%s</wsa:MessageID>
    %s
  </s:Header>
  <s:Body>
    <tev:PullMessages>
      <tev:Timeout>PT3S</tev:Timeout>
      <tev:MessageLimit>100</tev:MessageLimit>
    </tev:PullMessages>
  </s:Body>
</s:Envelope>`, action, subscriptionAddr, newMessageID(), es.client.BuildSecurityHeader())

	resp, err := es.do(ctx, subscriptionAddr, body)
	if err != nil {
		return nil, err
	}

	return es.parseNotificationMessages(resp)
}

// renewSubscription sends a WS-BaseNotification wsnt:Renew to the
// subscription manager endpoint, extending the camera-side TTL WITHOUT
// creating a new PullPoint subscription. This is the key to fixing the
// leak/churn (B-10): the old code "renewed" by calling
// CreatePullPointSubscription, which spun up a brand-new subscription
// (and made the camera re-dump its full Initialized rule state) every
// renewal cycle while never releasing the prior one — eventually
// exhausting the camera's subscription pool ("Maximum number of
// Subscribe reached"). A real Renew reuses the same SubscriptionManager
// so there's no extra slot consumed and no Initialized snapshot.
//
// SOAP action: http://docs.oasis-open.org/wsn/bw-2/SubscriptionManager/RenewRequest
// Body:        <wsnt:Renew><wsnt:TerminationTime>PT3600S</wsnt:TerminationTime></wsnt:Renew>
//
// Some cameras reject Renew (non-conformant SubscriptionManager); the
// caller treats any error here as "renew unsupported/failed" and falls
// back to unsubscribe-old + recreate.
func (es *EventSubscriber) renewSubscription(ctx context.Context, subscriptionAddr string) error {
	const action = "http://docs.oasis-open.org/wsn/bw-2/SubscriptionManager/RenewRequest"
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:wsa="http://www.w3.org/2005/08/addressing"
            xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2">
  <s:Header>
    <wsa:Action s:mustUnderstand="1">%s</wsa:Action>
    <wsa:To s:mustUnderstand="1">%s</wsa:To>
    <wsa:MessageID>urn:uuid:%s</wsa:MessageID>
    %s
  </s:Header>
  <s:Body>
    <wsnt:Renew>
      <wsnt:TerminationTime>PT3600S</wsnt:TerminationTime>
    </wsnt:Renew>
  </s:Body>
</s:Envelope>`, action, subscriptionAddr, newMessageID(), es.client.BuildSecurityHeader())

	resp, err := es.do(ctx, subscriptionAddr, body)
	if err != nil {
		return err
	}
	// A SOAP fault comes back HTTP 200 from some cameras (DoRequest only
	// errors on non-200), so inspect the body. RenewResponse means success;
	// a Fault means the camera rejected the renew and we must recreate.
	if isSOAPFault(resp) {
		return fmt.Errorf("renew rejected by camera: %s", soapFaultReason(resp))
	}
	return nil
}

// unsubscribe sends a WS-BaseNotification wsnt:Unsubscribe to the
// subscription manager endpoint, releasing the camera-side slot
// immediately instead of leaking it until the PT3600S TTL reaps it.
// Best-effort: callers log and continue on error (a dead/unreachable
// subscription can't be unsubscribed anyway, and the TTL will reap it).
//
// SOAP action: http://docs.oasis-open.org/wsn/bw-2/SubscriptionManager/UnsubscribeRequest
// Body:        <wsnt:Unsubscribe/>
func (es *EventSubscriber) unsubscribe(ctx context.Context, subscriptionAddr string) error {
	if subscriptionAddr == "" {
		return nil
	}
	const action = "http://docs.oasis-open.org/wsn/bw-2/SubscriptionManager/UnsubscribeRequest"
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:wsa="http://www.w3.org/2005/08/addressing"
            xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2">
  <s:Header>
    <wsa:Action s:mustUnderstand="1">%s</wsa:Action>
    <wsa:To s:mustUnderstand="1">%s</wsa:To>
    <wsa:MessageID>urn:uuid:%s</wsa:MessageID>
    %s
  </s:Header>
  <s:Body>
    <wsnt:Unsubscribe/>
  </s:Body>
</s:Envelope>`, action, subscriptionAddr, newMessageID(), es.client.BuildSecurityHeader())

	_, err := es.do(ctx, subscriptionAddr, body)
	return err
}

// isSOAPFault reports whether a SOAP response body carries a Fault
// element (namespace-prefix-agnostic). Used to detect a camera that
// rejected a Renew with HTTP 200 + <s:Fault>.
func isSOAPFault(resp []byte) bool {
	s := string(resp)
	return strings.Contains(s, ":Fault>") || strings.Contains(s, "<Fault>")
}

// soapFaultReason pulls the human-readable reason text out of a SOAP
// fault for logging. Best-effort: returns a truncated raw body if no
// recognizable reason element is present.
func soapFaultReason(resp []byte) string {
	if r := extractTagText(string(resp), "Text"); r != "" {
		return r
	}
	if r := extractTagText(string(resp), "faultstring"); r != "" {
		return r
	}
	s := string(resp)
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// extractTagText returns the text content of the first <…:tag>…</…:tag>
// (prefix-agnostic) in raw. Local helper so the fault parsing doesn't
// pull in a full SOAP unmarshal.
func extractTagText(raw, tag string) string {
	for _, open := range []string{":" + tag + ">", "<" + tag + ">"} {
		if i := strings.Index(raw, open); i >= 0 {
			start := i + len(open)
			if end := strings.Index(raw[start:], "</"); end >= 0 {
				return strings.TrimSpace(raw[start : start+end])
			}
		}
	}
	return ""
}

// newMessageID returns a fresh UUID for the wsa:MessageID header. Some
// ONVIF cameras dedupe requests by MessageID; reusing one across Pulls
// can cause silent drops.
func newMessageID() string { return uuid.NewString() }

// parseNotificationMessages extracts events from PullMessages response
func (es *EventSubscriber) parseNotificationMessages(data []byte) ([]parsedEvent, error) {
	// Generic XML parsing for notification messages
	type notificationEnvelope struct {
		XMLName xml.Name `xml:"Envelope"`
		Body    struct {
			PullMessagesResponse struct {
				NotificationMessage []struct {
					Message struct {
						// LOCAL-05: PropertyOperation is an attribute of the
						// INNER <tt:Message> element, NOT the outer
						// <wsnt:Message> wrapper. The ONVIF nesting is:
						//   wsnt:NotificationMessage
						//     > wsnt:Message            <- this struct
						//         > tt:Message[@PropertyOperation]  <- Inner
						//             > tt:Source / tt:Data
						// encoding/xml matches by local name, so a nested
						// field named "Message" inside this struct binds the
						// inner tt:Message and coexists with ,innerxml (which
						// captures the same bytes raw). Decoding the attr from
						// the outer wrapper always yielded "" and bypassed the
						// Initialized filter — every subscription-renewal
						// snapshot was recorded as an alert (B-09).
						//
						// ONVIF spec values: "Initialized" (subscription
						// bootstrap snapshot — every renewal cycle redumps
						// current rule state), "Changed" (state transition —
						// the real events), "Deleted" (rule removed). We
						// filter Initialized+all-false-state messages
						// downstream so renewals don't pile no-event-state
						// rows into the events table.
						Inner struct {
							PropertyOperation string `xml:"PropertyOperation,attr"`
						} `xml:"Message"`
						InnerXML string `xml:",innerxml"`
					} `xml:"Message"`
					Topic struct {
						Value string `xml:",chardata"`
					} `xml:"Topic"`
				} `xml:"NotificationMessage"`
			} `xml:"PullMessagesResponse"`
		} `xml:"Body"`
	}

	var envelope notificationEnvelope
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}

	var events []parsedEvent
	for _, msg := range envelope.Body.PullMessagesResponse.NotificationMessage {
		evt := parsedEvent{
			Details: make(map[string]interface{}),
		}

		topic := msg.Topic.Value

		// Try driver-specific classifier first, then fall back to generic
		if es.Classify != nil {
			evt.Type = es.Classify(topic)
		}
		if evt.Type == "" {
			evt.Type = classifyEvent(topic)
		}

		evt.Details["topic"] = topic
		evt.Details["raw"] = msg.Message.InnerXML
		evt.Details["property_operation"] = msg.Message.Inner.PropertyOperation

		// Parse common data items from the message
		parseDataItems(msg.Message.InnerXML, evt.Details)

		// Apply driver-specific enrichment (plate numbers, counts, etc.)
		if es.Enrich != nil {
			for k, v := range es.Enrich(topic, msg.Message.InnerXML) {
				evt.Details[k] = v
			}
		}

		// LOCAL-05: drop subscription-renewal snapshots. Every
		// PullPoint subscription renewal triggers the camera to dump
		// its current rule state via PropertyOperation="Initialized"
		// messages. With 4 cameras × ~10 preconfigured Milesight VCA
		// rules each, that's ~40 rows in the events table every
		// renewal cycle (~267s on the Milesight panoramic), all
		// representing "this rule is currently in the no-event
		// state". They flood the events table without conveying any
		// actual incident.
		//
		// Real state transitions arrive as PropertyOperation="Changed"
		// and pass through unchanged. The narrow case we DO keep
		// from Initialized: a message whose data items show an
		// active state (e.g. IsHuman=true on a persistent alarm
		// that was active when the subscription cycle restarted) —
		// these are rare but real signals.
		if isInitializedNoEventState(msg.Message.Inner.PropertyOperation, evt.Details) {
			continue
		}

		if evt.Type != "" {
			events = append(events, evt)
		}
	}

	return events, nil
}

// isInitializedNoEventState returns true when the message is a
// subscription-renewal snapshot that the camera emits to describe its
// current rule state (PropertyOperation="Initialized") AND every
// boolean-style data item is false-y. These are the high-volume
// no-information messages that flood the events table on every
// renewal cycle (LOCAL-05). Real state-transition events arrive as
// PropertyOperation="Changed" and bypass this filter entirely.
//
// Pass-through cases (returns false → message is kept):
//   - PropertyOperation != "Initialized" — Changed / Deleted / empty
//     (some non-conformant cameras omit the attribute on Changed
//     messages; we keep those).
//   - PropertyOperation = "Initialized" with at least one "is*"
//     SimpleItem set to true/1 — a persistent alarm that was
//     already active when the subscription resumed. Rare but real.
//   - PropertyOperation = "Initialized" with no boolean SimpleItems
//     at all — peoplecount, vehiclecount, LPR plates, etc. We can't
//     judge "no-event state" without a boolean signal so we keep the
//     message to be safe.
func isInitializedNoEventState(propertyOp string, details map[string]interface{}) bool {
	if propertyOp != "Initialized" {
		return false
	}
	// Scan for any "is*" SimpleItem (the ONVIF convention for trigger
	// booleans: IsMotion, IsHuman, IsVehicle, IsFace, IsRemove,
	// IsAbandoned, IsTamper, IsLineCross, IsIntrusion, ...). If we
	// find one that's true/1, the message represents an actual
	// active state — keep it.
	sawBool := false
	for key, raw := range details {
		if !strings.HasPrefix(key, "is") {
			continue
		}
		s, ok := raw.(string)
		if !ok {
			continue
		}
		sawBool = true
		v := strings.ToLower(strings.TrimSpace(s))
		if v != "" && v != "false" && v != "0" {
			return false // at least one boolean is active
		}
	}
	// Skip only when we found at least one boolean signal AND all of
	// them were false. No boolean signals at all → can't judge → keep.
	return sawBool
}

// classifyEvent maps ONVIF topic strings to simplified event types
func classifyEvent(topic string) string {
	topic = strings.ToLower(topic)

	switch {
	case strings.Contains(topic, "motiondetect") || strings.Contains(topic, "cellmotion") || strings.Contains(topic, "motion"):
		return "motion"
	case strings.Contains(topic, "humandetect") || strings.Contains(topic, "human"):
		return "human"
	case strings.Contains(topic, "vehicledetect") || strings.Contains(topic, "vehicle"):
		return "vehicle"
	case strings.Contains(topic, "facedetect") || strings.Contains(topic, "face"):
		return "face"
	case strings.Contains(topic, "intrusion") || strings.Contains(topic, "fielddetect") || strings.Contains(topic, "regionentrance") || strings.Contains(topic, "regionexit"):
		return "intrusion"
	case strings.Contains(topic, "linedetect") || strings.Contains(topic, "linecross"):
		return "linecross"
	case strings.Contains(topic, "loitering") || strings.Contains(topic, "loiter"):
		return "loitering"
	case strings.Contains(topic, "peoplecount") || strings.Contains(topic, "peoplecounting"):
		return "peoplecount"
	case strings.Contains(topic, "tamper"):
		return "tamper"
	case strings.Contains(topic, "objectdetect") || strings.Contains(topic, "object"):
		return "object"
	case strings.Contains(topic, "licenseplate") || strings.Contains(topic, "lpr") || strings.Contains(topic, "anpr") || strings.Contains(topic, "plate"):
		return "lpr"
	case strings.Contains(topic, "videoloss") || strings.Contains(topic, "signalloss"):
		return "videoloss"
	default:
		return "other"
	}
}

// parseDataItems extracts key-value pairs from ONVIF message XML.
// It also parses Profile M analytics objects (tt:Object with tt:BoundingBox).
func parseDataItems(innerXML string, details map[string]interface{}) []analyticsObject {
	// --- SimpleItem extraction ---
	reader := strings.NewReader("<root>" + innerXML + "</root>")
	decoder := xml.NewDecoder(reader)
	decoder.Strict = false

	var objects []analyticsObject
	var inObject bool
	var currentObj analyticsObject

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}

		switch se := token.(type) {
		case xml.StartElement:
			switch se.Name.Local {
			case "SimpleItem":
				var name, value string
				for _, attr := range se.Attr {
					switch attr.Name.Local {
					case "Name":
						name = attr.Value
					case "Value":
						value = attr.Value
					}
				}
				if name != "" {
					details[strings.ToLower(name)] = value
					nameLower := strings.ToLower(name)
					if nameLower == "plate" || nameLower == "platenumber" || nameLower == "licenseplatenumber" {
						details["plate_number"] = value
					}
				}

			// ONVIF Profile M analytics: <tt:Object ObjectId="1">
			case "Object":
				inObject = true
				currentObj = analyticsObject{}
				for _, attr := range se.Attr {
					if attr.Name.Local == "ObjectId" {
						currentObj.ID = attr.Value
					}
				}

			// <tt:BoundingBox left="0.1" top="0.2" right="0.4" bottom="0.8"/>
			case "BoundingBox":
				if inObject {
					for _, attr := range se.Attr {
						val := parseFloat(attr.Value)
						switch attr.Name.Local {
						case "left":
							currentObj.Left = val
						case "top":
							currentObj.Top = val
						case "right":
							currentObj.Right = val
						case "bottom":
							currentObj.Bottom = val
						}
					}
				}

			// <tt:Class> ... <tt:Type Likelihood="0.95">Human</tt:Type>
			case "Type":
				if inObject {
					for _, attr := range se.Attr {
						if attr.Name.Local == "Likelihood" {
							currentObj.Confidence = parseFloat(attr.Value)
						}
					}
					// Label text is char data; we'll capture via CharData token
					currentObj.awaitingLabel = true
				}
			}

		case xml.CharData:
			if inObject && currentObj.awaitingLabel {
				label := strings.TrimSpace(string(se))
				if label != "" {
					currentObj.Label = strings.ToLower(label)
				}
				currentObj.awaitingLabel = false
			}

		case xml.EndElement:
			if se.Name.Local == "Object" && inObject {
				inObject = false
				objects = append(objects, currentObj)
			}
		}
	}

	// Store bounding boxes in details for DB/event panel display
	if len(objects) > 0 {
		type boxJSON struct {
			Label      string  `json:"label"`
			Confidence float64 `json:"confidence"`
			X          float64 `json:"x"`
			Y          float64 `json:"y"`
			W          float64 `json:"w"`
			H          float64 `json:"h"`
		}
		var boxes []boxJSON
		for _, o := range objects {
			// Normalize ONVIF Profile M coordinates (-1..1) to 0..1
			// Some cameras (Milesight, etc.) may already use 0..1;
			// if any value is negative, we normalize from -1..1 range.
			left, top, right, bottom := o.Left, o.Top, o.Right, o.Bottom
			if left < 0 || top < 0 || right < 0 || bottom < 0 {
				// ONVIF -1..1 → 0..1: val01 = (val + 1) / 2
				left = (left + 1) / 2
				top = (top + 1) / 2
				right = (right + 1) / 2
				bottom = (bottom + 1) / 2
			}
			// Clamp to 0..1
			left = clamp01(left)
			top = clamp01(top)
			right = clamp01(right)
			bottom = clamp01(bottom)

			w := right - left
			h := bottom - top
			if w <= 0 || h <= 0 {
				continue // skip degenerate boxes
			}

			boxes = append(boxes, boxJSON{
				Label:      o.Label,
				Confidence: o.Confidence,
				X:          left,
				Y:          top,
				W:          w,
				H:          h,
			})
		}
		details["bounding_boxes"] = boxes
	}

	// Store as JSON for readable logging
	if detailsJSON, err := json.Marshal(details); err == nil {
		_ = detailsJSON
	}

	return objects
}

type analyticsObject struct {
	ID            string
	Label         string
	Confidence    float64
	Left          float64
	Top           float64
	Right         float64
	Bottom        float64
	awaitingLabel bool
}

func parseFloat(s string) float64 {
	s = strings.TrimSpace(s)
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// isSubscriptionCapError reports whether a CreatePullPointSubscription
// error is the camera saying "I'm full". Milesight and Hikvision both
// return SOAP fault Reason text along the lines of "Maximum number of
// Subscribe reached" when their subscription pool is exhausted (usually
// 4–5 concurrent subs). When this is the cause, retrying every minute
// just keeps the leak alive — the right answer is to wait for the
// camera's PT3600S TTL to reap stale subs.
func isSubscriptionCapError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "maximum number of subscribe") ||
		strings.Contains(s, "too many subscriptions") ||
		strings.Contains(s, "subscription limit")
}

// UnusedImportPreventer prevents unused import errors
var _ = http.StatusOK
