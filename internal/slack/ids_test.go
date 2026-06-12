package slack

import "testing"

func TestParseDynamicCallback(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    dynamicCallback
		wantErr bool
	}{
		{"create step 1", "dynamic_create_step_1", dynamicCallback{Mode: flowCreate, Step: 1}, false},
		{"create step 12", "dynamic_create_step_12", dynamicCallback{Mode: flowCreate, Step: 12}, false},
		{"update step 3", "dynamic_update_step_3", dynamicCallback{Mode: flowUpdate, Step: 3}, false},
		{"select target rejected", CallbackDynamicSelectTarget, dynamicCallback{}, true},
		{"step 0 rejected", "dynamic_create_step_0", dynamicCallback{}, true},
		{"non-number rejected", "dynamic_create_step_x", dynamicCallback{}, true},
		{"empty rejected", "", dynamicCallback{}, true},
		{"unrelated rejected", "block_actions", dynamicCallback{}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseDynamicCallback(tc.in)
			switch {
			case tc.wantErr && ok:
				t.Fatalf("parseDynamicCallback(%q) ok=true, want false", tc.in)
			case !tc.wantErr && !ok:
				t.Fatalf("parseDynamicCallback(%q) ok=false, want true", tc.in)
			case ok && got != tc.want:
				t.Fatalf("parseDynamicCallback(%q)=%+v want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestDynamicCallbackRoundTrip(t *testing.T) {
	for _, c := range []dynamicCallback{
		{Mode: flowCreate, Step: 1},
		{Mode: flowCreate, Step: 5},
		{Mode: flowUpdate, Step: 1},
		{Mode: flowUpdate, Step: 17},
	} {
		s := c.String()
		got, ok := parseDynamicCallback(s)
		if !ok || got != c {
			t.Fatalf("round trip %s -> %+v, %v want %+v", s, got, ok, c)
		}
	}
}

func TestFieldIDHelpers(t *testing.T) {
	if got := fieldBlockID("description"); got != "block_description" {
		t.Fatalf("fieldBlockID=%q", got)
	}
	if got := fieldElemID("description"); got != "elem_description" {
		t.Fatalf("fieldElemID=%q", got)
	}
	if got := mapEntryBlockID("team_access", "Maintainers"); got != "block_map_team_access_Maintainers" {
		t.Fatalf("mapEntryBlockID=%q", got)
	}
	if got := mapEntryElemID("team_access", "Maintainers"); got != "elem_map_team_access_Maintainers" {
		t.Fatalf("mapEntryElemID=%q", got)
	}
}
