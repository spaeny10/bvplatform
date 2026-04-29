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
	client    *Client
	cameraID  uuid.UUID
	callback  EventCallback
	stopCh    chan struct{}
	running   bool
	mu        sync.Mutex
	Classify  EventClassifierFunc // optional: vendor-specific topic classifier
	Enrich    EventEnricherFunc   // optional: vendor-specific metadata extractor
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

// pullLoop continuously pulls events from the camera using PullPoint subscription.
// Proactively renews the subscription before the TTL expires so active cameras
// (which never hit the "empty" renewal path) don't lose their subscription.
func (es *EventSubscriber) pullLoop(ctx context.Context) {
	const subscriptionTTL = 3600 * time.Second // must match PT3600S in createPullPointSubscription
	const renewBeforeSec = 300 * time.Second   // renew 5 minutes before expiry
	const renewAfterEmpty = 20                 // also renew after 20 consecutive empty polls (~60s)
	const maxErrors = 10

	newSubscription := func() (string, time.Time, error) {
		addr, err := es.createPullPointSubscription(ctx)
		if err != nil {
			log.Printf("[EVENTS] Failed to create subscription for camera %s: %v", es.cameraID, err)
			return "", time.Time{}, err
		}
		log.Printf("[EVENTS] PullPoint subscription created for camera %s", es.cameraID)
		return addr, time.Now().Add(subscriptionTTL), nil
	}

	// Initial subscription — retry until success or context cancelled.
	// When the camera reports its subscription cap is exhausted (typical
	// on Milesight/Hikvision after stale subs leak across restarts) we
	// back off much harder: retrying every 60s would just keep leaking
	// and hammering the device. The camera reaps stale subs on its
	// PT3600S TTL, so 5-minute waits give the pool a chance to drain.
	var subscriptionAddr string
	var expiresAt time.Time
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

		// Proactive renewal: refresh subscription before it expires
		if time.Until(expiresAt) < renewBeforeSec {
			log.Printf("[EVENTS] Proactively renewing subscription for camera %s (expires in %s)", es.cameraID, time.Until(expiresAt).Round(time.Second))
			if addr, exp, _ := newSubscription(); addr != "" {
				subscriptionAddr = addr
				expiresAt = exp
				consecutiveErrors = 0
			}
		}

		events, err := es.pullMessages(ctx, subscriptionAddr)
		if err != nil {
			consecutiveErrors++
			if consecutiveErrors%5 == 1 {
				log.Printf("[EVENTS] Pull error for camera %s (%d/%d): %v", es.cameraID, consecutiveErrors, maxErrors, err)
			}
			if consecutiveErrors >= maxErrors {
				// Subscription probably died — force a fresh one immediately
				log.Printf("[EVENTS] Subscription lost for camera %s — re-subscribing", es.cameraID)
				addr, exp, subErr := newSubscription()
				if addr != "" {
					subscriptionAddr = addr
					expiresAt = exp
					consecutiveErrors = 0
				} else {
					// Camera unreachable — back off and retry. Stretch
					// the wait if it's the subscription-cap error so we
					// don't keep adding to the leak.
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
				}
			} else {
				time.Sleep(2 * time.Second)
			}
			continue
		}
		consecutiveErrors = 0

		if len(events) == 0 {
			consecutiveEmpty++
			if consecutiveEmpty >= renewAfterEmpty {
				log.Printf("[EVENTS] No events for %ds — renewing subscription for camera %s", consecutiveEmpty*3, es.cameraID)
				if addr, exp, _ := newSubscription(); addr != "" {
					subscriptionAddr = addr
					expiresAt = exp
					consecutiveEmpty = 0
				}
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

	resp, err := es.client.DoRequest(ctx, eventsAddr, body)
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

	resp, err := es.client.DoRequest(ctx, subscriptionAddr, body)
	if err != nil {
		return nil, err
	}

	return es.parseNotificationMessages(resp)
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

		// Parse common data items from the message
		parseDataItems(msg.Message.InnerXML, evt.Details)

		// Apply driver-specific enrichment (plate numbers, counts, etc.)
		if es.Enrich != nil {
			for k, v := range es.Enrich(topic, msg.Message.InnerXML) {
				evt.Details[k] = v
			}
		}

		if evt.Type != "" {
			events = append(events, evt)
		}
	}

	return events, nil
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
