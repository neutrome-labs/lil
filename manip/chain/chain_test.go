package chain

import (
	"testing"

	"github.com/neutrome-labs/ail"
)

func baseProgram() *ail.Program {
	p := ail.NewProgram()
	p.EmitString(ail.SET_MODEL, "model-a")
	p.Emit(ail.MSG_START)
	p.Emit(ail.ROLE_USR)
	p.EmitString(ail.TXT_CHUNK, "question")
	p.Emit(ail.MSG_END)
	return p
}

func TestApplyWrapsImplicitRequestAndAppendsChain(t *testing.T) {
	out, err := New("Suggest next steps", WithModel("model-b"), WithStream(true)).Apply(baseProgram())
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	requests := out.Requests()
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2\n%s", len(requests), out.Disasm())
	}
	if requests[0].ID != DefaultSourceID || requests[0].Yield != ail.YieldContent {
		t.Fatalf("source request = %#v", requests[0])
	}
	if requests[1].ID != "chain:1" || requests[1].Yield != ail.YieldContent {
		t.Fatalf("chain request = %#v", requests[1])
	}

	unit, err := out.MaterializeRequest(requests[1], map[string]ail.RequestOutput{
		DefaultSourceID: {Content: "answer"},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if unit.Program.GetModel() != "model-b" {
		t.Fatalf("model = %q", unit.Program.GetModel())
	}
	if !unit.Program.IsStreaming() {
		t.Fatalf("expected generated request to stream")
	}
	msgs := unit.Program.Messages()
	if len(msgs) != 1 {
		t.Fatalf("messages = %d, want 1", len(msgs))
	}
	if got := unit.Program.MessageText(msgs[0]); got != "Suggest next steps\n\nanswer" {
		t.Fatalf("message = %q", got)
	}
}

func TestApplyCanHideSourceAndInjectReasoning(t *testing.T) {
	out, err := New(
		"Implement",
		WithID("impl"),
		WithSourceYield(ail.YieldNone),
		WithTargetChannel("reasoning"),
	).Apply(baseProgram())
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	requests := out.Requests()
	if requests[0].Yield != ail.YieldNone {
		t.Fatalf("source yield = %q", requests[0].Yield)
	}
	unit, err := out.MaterializeRequest(requests[1], map[string]ail.RequestOutput{
		DefaultSourceID: {Content: "private plan"},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	thinkings := unit.Program.Thinkings()
	if len(thinkings) != 1 {
		t.Fatalf("thinkings = %d, want 1\n%s", len(thinkings), unit.Program.Disasm())
	}
	if got := unit.Program.ThinkingText(thinkings[0]); got != "private plan" {
		t.Fatalf("thinking = %q", got)
	}
}

func TestApplySupportsBothSourceAndBothTarget(t *testing.T) {
	out, err := New(
		"Localize",
		WithSourceField(FieldBoth),
		WithTargetChannel(FieldBoth),
	).Apply(baseProgram())
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	requests := out.Requests()
	unit, err := out.MaterializeRequest(requests[1], map[string]ail.RequestOutput{
		DefaultSourceID: {Reasoning: "private plan", Content: "visible answer"},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if got := unit.Program.ThinkingText(unit.Program.Thinkings()[0]); got != "private plan" {
		t.Fatalf("thinking = %q", got)
	}
	if got := unit.Program.MessageText(unit.Program.Messages()[0]); got != "Localize\n\nvisible answer" {
		t.Fatalf("message = %q", got)
	}
}

func TestApplySupportsBothSourceIntoContent(t *testing.T) {
	out, err := New(
		"Localize",
		WithSourceField(FieldBoth),
		WithTargetChannel(FieldContent),
	).Apply(baseProgram())
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	requests := out.Requests()
	unit, err := out.MaterializeRequest(requests[1], map[string]ail.RequestOutput{
		DefaultSourceID: {Reasoning: "private plan", Content: "visible answer"},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if len(unit.Program.Thinkings()) != 0 {
		t.Fatalf("unexpected thinking:\n%s", unit.Program.Disasm())
	}
	if got := unit.Program.MessageText(unit.Program.Messages()[0]); got != "Localize\n\nprivate planvisible answer" {
		t.Fatalf("message = %q", got)
	}
}
