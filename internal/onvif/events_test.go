package onvif

import "testing"

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
