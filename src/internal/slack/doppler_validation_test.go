package slack

import (
	"testing"

	goslack "github.com/slack-go/slack"
)

func dopplerAddValues(name, desc, justification string) map[string]map[string]goslack.BlockAction {
	return map[string]map[string]goslack.BlockAction{
		BlockDopplerName:   {ElemDopplerName: {Value: name}},
		BlockDopplerDesc:   {ElemDopplerDesc: {Value: desc}},
		BlockJustification: {ElemJustification: {Value: justification}},
	}
}

func dopplerUpdateValues(desc, justification string) map[string]map[string]goslack.BlockAction {
	return map[string]map[string]goslack.BlockAction{
		BlockDopplerDesc:   {ElemDopplerDesc: {Value: desc}},
		BlockJustification: {ElemJustification: {Value: justification}},
	}
}

func TestValidateDopplerAdd(t *testing.T) {
	tests := []struct {
		name    string
		values  map[string]map[string]goslack.BlockAction
		wantErr string // block ID expected in errors, or "" for valid
	}{
		{
			name:   "valid project",
			values: dopplerAddValues("my-project", "A cool project", "This project is needed for testing purposes"),
		},
		{
			name:    "empty name",
			values:  dopplerAddValues("", "A description", "This project is needed for testing purposes"),
			wantErr: BlockDopplerName,
		},
		{
			name:    "name too long",
			values:  dopplerAddValues("this-name-is-way-too-long-for-a-doppler-project", "A description", "This project is needed for testing purposes"),
			wantErr: BlockDopplerName,
		},
		{
			name:    "uppercase name",
			values:  dopplerAddValues("My-Project", "A description", "This project is needed for testing purposes"),
			wantErr: BlockDopplerName,
		},
		{
			name:    "name with spaces",
			values:  dopplerAddValues("my project", "A description", "This project is needed for testing purposes"),
			wantErr: BlockDopplerName,
		},
		{
			name:    "name with special characters",
			values:  dopplerAddValues("my$project", "A description", "This project is needed for testing purposes"),
			wantErr: BlockDopplerName,
		},
		{
			name:   "name with underscores",
			values: dopplerAddValues("my_project", "A cool project", "This project is needed for testing purposes"),
		},
		{
			name:    "empty description",
			values:  dopplerAddValues("my-project", "", "This project is needed for testing purposes"),
			wantErr: BlockDopplerDesc,
		},
		{
			name:    "justification too short",
			values:  dopplerAddValues("my-project", "A description", "too short"),
			wantErr: BlockJustification,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errs := validateDopplerAdd(tc.values)
			if tc.wantErr == "" {
				if len(errs) > 0 {
					t.Errorf("expected no errors, got %v", errs)
				}
			} else {
				if _, ok := errs[tc.wantErr]; !ok {
					t.Errorf("expected error on %s, got %v", tc.wantErr, errs)
				}
			}
		})
	}
}

func TestValidateDopplerUpdate(t *testing.T) {
	tests := []struct {
		name    string
		values  map[string]map[string]goslack.BlockAction
		wantErr string
	}{
		{
			name:   "valid update",
			values: dopplerUpdateValues("Updated description", "Updating because this is needed now"),
		},
		{
			name:    "empty description",
			values:  dopplerUpdateValues("", "Updating because this is needed now"),
			wantErr: BlockDopplerDesc,
		},
		{
			name:    "justification too short",
			values:  dopplerUpdateValues("A description", "too short"),
			wantErr: BlockJustification,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errs := validateDopplerUpdate(tc.values)
			if tc.wantErr == "" {
				if len(errs) > 0 {
					t.Errorf("expected no errors, got %v", errs)
				}
			} else {
				if _, ok := errs[tc.wantErr]; !ok {
					t.Errorf("expected error on %s, got %v", tc.wantErr, errs)
				}
			}
		})
	}
}
