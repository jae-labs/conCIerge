package slack

import (
	"testing"

	"github.com/jae-labs/concierge/internal/schema"
	"github.com/slack-go/slack"
)

func TestBuildDynamicModalUsesDynamicSelectOptions(t *testing.T) {
	resource := &schema.Resource{
		Label: "User Management",
		Kind:  schema.KindMembership,
		Steps: []schema.Step{{
			ID:    "membership",
			Title: "Membership",
			Fields: []schema.Field{
				{
					Path:  "team",
					Type:  schema.TypeSelect,
					Label: "Team",
					KeySource: &schema.KeySource{
						File:     "github/locals.tf",
						RootPath: "teams",
					},
				},
			},
		}},
	}

	view := BuildDynamicModal(resource, 0, nil, map[string][]string{"team": {"Maintainers", "Collaborators"}}, dynamicCallback{Mode: flowCreate, Step: 1}.String(), "meta")
	if len(view.Blocks.BlockSet) == 0 {
		t.Fatal("expected modal blocks")
	}
}

func TestBuildDynamicSelectModalTitleFitsSlackLimit(t *testing.T) {
	resource := &schema.Resource{
		Label:    "GitHub Repositories",
		KeyLabel: "Repository Name",
	}

	view := BuildDynamicSelectModal(resource, schema.ActionDelete, []string{"repo-a"}, CallbackDynamicSelectTarget, "meta")
	if got := len(view.Title.Text); got > 24 {
		t.Fatalf("title length=%d text=%q", got, view.Title.Text)
	}
}

func TestBuildDynamicModalTitleFitsSlackLimit(t *testing.T) {
	resource := &schema.Resource{
		Label: "Organization Settings",
		Kind:  schema.KindSingleton,
		Steps: []schema.Step{{
			ID:    "settings",
			Title: "Settings",
			Fields: []schema.Field{
				{Path: "name", Type: schema.TypeString, Label: "Name"},
			},
		}},
	}

	view := BuildDynamicModal(resource, 0, nil, nil, dynamicCallback{Mode: flowUpdate, Step: 1}.String(), "meta")
	if got := len(view.Title.Text); got > 24 {
		t.Fatalf("title length=%d text=%q", got, view.Title.Text)
	}
}

func TestBuildDynamicModalWithMapString(t *testing.T) {
	resource := &schema.Resource{
		Label: "GitHub Repositories",
		Kind:  schema.KindMapEntry,
		Steps: []schema.Step{{
			ID:    "access",
			Title: "Team Access",
			Fields: []schema.Field{
				{
					Path:  "team_access",
					Type:  schema.TypeMapString,
					Label: "Team Access",
					KeySource: &schema.KeySource{
						File:     "github/locals.tf",
						RootPath: "teams",
					},
					ValueOptions: []string{"admin", "maintain"},
				},
			},
		}},
	}

	dynamicKeys := map[string][]string{
		"team_access": {"team-a", "team-b"},
	}

	view := BuildDynamicModal(resource, 0, nil, dynamicKeys, dynamicCallback{Mode: flowCreate, Step: 1}.String(), "meta")
	if err := validateModalViewRequest(view); err != nil {
		t.Fatalf("expected modal view request to be valid: %v", err)
	}
}

func TestBuildDynamicModalJustificationForCreateAndSingleton(t *testing.T) {
	resource := &schema.Resource{
		Label: "GitHub Repositories",
		Kind:  schema.KindMapEntry,
		Steps: []schema.Step{
			{
				ID:    "step1",
				Title: "Step 1",
				Fields: []schema.Field{
					{Path: "name", Type: schema.TypeString, Label: "Name"},
				},
			},
			{
				ID:    "step2",
				Title: "Step 2",
				Fields: []schema.Field{
					{Path: "desc", Type: schema.TypeString, Label: "Description"},
				},
			},
		},
	}

	// 1. Create flow, intermediate step: should NOT have justification block
	view1 := BuildDynamicModal(resource, 0, nil, nil, dynamicCallback{Mode: flowCreate, Step: 1}.String(), "meta")
	hasJustification := false
	for _, b := range view1.Blocks.BlockSet {
		if inputBlock, ok := b.(*slack.InputBlock); ok && inputBlock.BlockID == BlockJustification {
			hasJustification = true
		}
	}
	if hasJustification {
		t.Error("expected intermediate step of create flow NOT to have justification block")
	}

	// 2. Create flow, final step: SHOULD have justification block
	view2 := BuildDynamicModal(resource, 1, nil, nil, dynamicCallback{Mode: flowCreate, Step: 2}.String(), "meta")
	hasJustification = false
	for _, b := range view2.Blocks.BlockSet {
		if inputBlock, ok := b.(*slack.InputBlock); ok && inputBlock.BlockID == BlockJustification {
			hasJustification = true
		}
	}
	if !hasJustification {
		t.Error("expected final step of create flow to have justification block")
	}

	// 3. Update flow, final step: should NOT have justification block (since it was collected during target selection)
	view3 := BuildDynamicModal(resource, 1, nil, nil, dynamicCallback{Mode: flowUpdate, Step: 2}.String(), "meta")
	hasJustification = false
	for _, b := range view3.Blocks.BlockSet {
		if inputBlock, ok := b.(*slack.InputBlock); ok && inputBlock.BlockID == BlockJustification {
			hasJustification = true
		}
	}
	if hasJustification {
		t.Error("expected final step of update flow NOT to have justification block")
	}

	// 4. Singleton update flow, final step: SHOULD have justification block (since target selection is bypassed)
	singletonResource := &schema.Resource{
		Label: "Org Settings",
		Kind:  schema.KindSingleton,
		Steps: []schema.Step{
			{
				ID:    "step1",
				Title: "Step 1",
				Fields: []schema.Field{
					{Path: "name", Type: schema.TypeString, Label: "Name"},
				},
			},
		},
	}
	view4 := BuildDynamicModal(singletonResource, 0, nil, nil, dynamicCallback{Mode: flowUpdate, Step: 1}.String(), "meta")
	hasJustification = false
	for _, b := range view4.Blocks.BlockSet {
		if inputBlock, ok := b.(*slack.InputBlock); ok && inputBlock.BlockID == BlockJustification {
			hasJustification = true
		}
	}
	if !hasJustification {
		t.Error("expected final step of singleton update flow to have justification block")
	}
}
