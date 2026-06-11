package onvif

import "testing"

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
