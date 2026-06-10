package slack

import (
	"strings"
	"testing"

	goslack "github.com/slack-go/slack"
)

func TestValidateModalViewRequestAcceptsValidModal(t *testing.T) {
	view := goslack.ModalViewRequest{
		Type:            goslack.VTModal,
		Title:           goslack.NewTextBlockObject("plain_text", "Valid title", false, false),
		Submit:          goslack.NewTextBlockObject("plain_text", "Next", false, false),
		Close:           goslack.NewTextBlockObject("plain_text", "Cancel", false, false),
		CallbackID:      dynamicCallback{Mode: flowCreate, Step: 2}.String(),
		PrivateMetadata: "thread:nonce",
		Blocks: goslack.Blocks{BlockSet: []goslack.Block{
			goslack.NewInputBlock(
				BlockResourceKey,
				goslack.NewTextBlockObject("plain_text", "Repository Name", false, false),
				nil,
				goslack.NewPlainTextInputBlockElement(
					goslack.NewTextBlockObject("plain_text", "e.g. repo", false, false),
					ElemResourceKey,
				),
			),
		}},
	}

	if err := validateModalViewRequest(view); err != nil {
		t.Fatalf("validateModalViewRequest: %v", err)
	}
}

func TestValidateModalViewRequestRejectsLongBlockID(t *testing.T) {
	longID := strings.Repeat("a", 256)
	view := goslack.ModalViewRequest{
		Type:            goslack.VTModal,
		Title:           goslack.NewTextBlockObject("plain_text", "Valid title", false, false),
		Submit:          goslack.NewTextBlockObject("plain_text", "Next", false, false),
		Close:           goslack.NewTextBlockObject("plain_text", "Cancel", false, false),
		CallbackID:      dynamicCallback{Mode: flowCreate, Step: 2}.String(),
		PrivateMetadata: "thread:nonce",
		Blocks: goslack.Blocks{BlockSet: []goslack.Block{
			goslack.NewInputBlock(
				longID,
				goslack.NewTextBlockObject("plain_text", "Repository Name", false, false),
				nil,
				goslack.NewPlainTextInputBlockElement(
					goslack.NewTextBlockObject("plain_text", "e.g. repo", false, false),
					ElemResourceKey,
				),
			),
		}},
	}

	err := validateModalViewRequest(view)
	if err == nil {
		t.Fatal("expected validation error")
		return
	}
	if !strings.Contains(err.Error(), "block_id") {
		t.Fatalf("err=%q, want block_id violation", err)
	}
}

func TestValidateModalViewRequestRejectsTooManyStaticSelectOptions(t *testing.T) {
	options := make([]*goslack.OptionBlockObject, 101)
	for i := range options {
		options[i] = goslack.NewOptionBlockObject(
			strings.Repeat("v", 10),
			goslack.NewTextBlockObject("plain_text", "repo", false, false),
			nil,
		)
	}
	view := goslack.ModalViewRequest{
		Type:            goslack.VTModal,
		Title:           goslack.NewTextBlockObject("plain_text", "Valid title", false, false),
		Submit:          goslack.NewTextBlockObject("plain_text", "Next", false, false),
		Close:           goslack.NewTextBlockObject("plain_text", "Cancel", false, false),
		CallbackID:      CallbackDynamicSelectTarget,
		PrivateMetadata: "thread:nonce",
		Blocks: goslack.Blocks{BlockSet: []goslack.Block{
			goslack.NewInputBlock(
				BlockResourceKey,
				goslack.NewTextBlockObject("plain_text", "Repository Name", false, false),
				nil,
				goslack.NewOptionsSelectBlockElement(
					"static_select",
					goslack.NewTextBlockObject("plain_text", "Select a target...", false, false),
					ElemResourceKey,
					options...,
				),
			),
		}},
	}

	err := validateModalViewRequest(view)
	if err == nil {
		t.Fatal("expected validation error")
		return
	}
	if !strings.Contains(err.Error(), "options exceeds Slack limit") {
		t.Fatalf("err=%q, want options limit violation", err)
	}
}
