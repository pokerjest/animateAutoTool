package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/model"
)

func TestAIProposalApplyArgumentsUsesPersistedPayloadOnly(t *testing.T) {
	row := &model.AIProposal{
		ApplyTool: "apply_subscription_rule_proposal",
		Payload:   `{"subscription_id":7,"filter_rule":"ANi","exclude_rule":"CAM","resolution_filter":"1080p","subtitle_language":"chs","unexpected":"ignored"}`,
	}
	raw, err := aiProposalApplyArguments(row)
	if err != nil {
		t.Fatalf("build apply arguments: %v", err)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		t.Fatalf("decode apply arguments: %v", err)
	}
	if args["subscription_id"] != float64(7) || args["filter_rule"] != "ANi" {
		t.Fatalf("unexpected persisted arguments: %#v", args)
	}
	if _, ok := args["unexpected"]; ok {
		t.Fatalf("unapproved payload field leaked into apply arguments: %#v", args)
	}
}

func TestAIProposalApplyArgumentsRejectsUnknownApplyTool(t *testing.T) {
	_, err := aiProposalApplyArguments(&model.AIProposal{ApplyTool: "arbitrary_shell", Payload: `{}`})
	if err == nil || !strings.Contains(err.Error(), "允许执行") {
		t.Fatalf("expected fixed tool allowlist error, got %v", err)
	}
}
