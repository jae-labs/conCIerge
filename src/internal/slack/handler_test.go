package slack

import (
	"testing"

	goslack "github.com/slack-go/slack"
)

func TestIsRootApprovalMessage(t *testing.T) {
	tests := []struct {
		name      string
		msg       goslack.Message
		reactedTS string
		want      bool
	}{
		{
			name: "top level message without thread metadata",
			msg: goslack.Message{
				Msg: goslack.Msg{
					Timestamp: "1710000000.000100",
				},
			},
			reactedTS: "1710000000.000100",
			want:      true,
		},
		{
			name: "root message with matching thread timestamp",
			msg: goslack.Message{
				Msg: goslack.Msg{
					Timestamp:       "1710000000.000100",
					ThreadTimestamp: "1710000000.000100",
				},
			},
			reactedTS: "1710000000.000100",
			want:      true,
		},
		{
			name: "history fallback to root message for reacted thread reply",
			msg: goslack.Message{
				Msg: goslack.Msg{
					Timestamp: "1710000000.000100",
				},
			},
			reactedTS: "1710000000.000200",
			want:      false,
		},
		{
			name: "thread reply with parent user id",
			msg: goslack.Message{
				Msg: goslack.Msg{
					Timestamp:       "1710000000.000200",
					ThreadTimestamp: "1710000000.000100",
					ParentUserId:    "U123",
				},
			},
			reactedTS: "1710000000.000200",
			want:      false,
		},
		{
			name: "thread reply with different thread timestamp",
			msg: goslack.Message{
				Msg: goslack.Msg{
					Timestamp:       "1710000000.000200",
					ThreadTimestamp: "1710000000.000100",
				},
			},
			reactedTS: "1710000000.000200",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRootApprovalMessage(tt.msg, tt.reactedTS)
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

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
