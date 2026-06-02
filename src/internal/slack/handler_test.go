package slack

import (
	"testing"
)

func TestRequestSummaryTitle(t *testing.T) {
	tests := []struct {
		name    string
		prTitle string
		want    string
	}{
		{
			name:    "strips request prefix for summary display",
			prTitle: "Request: Update GitHub repository settings",
			want:    "Update GitHub repository settings",
		},
		{
			name:    "keeps titles without prefix unchanged",
			prTitle: "Update GitHub repository settings",
			want:    "Update GitHub repository settings",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := requestSummaryTitle(tt.prTitle)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
