package ail

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSequenceAsmBinaryRoundTrip(t *testing.T) {
	text := `REQ_START main
  REQ_YIELD none
  SET_MODEL model-a
  MSG_START
    ROLE_USR
    TXT_CHUNK Write a plan
  MSG_END
REQ_END
REQ_START impl
  REQ_YIELD content
  SET_MODEL model-b
  MSG_START
    ROLE_USR
    TXT_CHUNK Implement this:
    SUB_REASON main.content
  MSG_END
REQ_END
`
	prog, err := Asm(text)
	if err != nil {
		t.Fatalf("asm: %v", err)
	}

	disasm := prog.Disasm()
	round, err := Asm(disasm)
	if err != nil {
		t.Fatalf("asm round-trip: %v", err)
	}

	var buf bytes.Buffer
	if err := round.Encode(&buf); err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := Decode(&buf)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got := strings.TrimSpace(decoded.Disasm()); got != strings.TrimSpace(disasm) {
		t.Fatalf("round-trip mismatch:\n%s", got)
	}
}

func TestMaterializeRequestInjectsPriorOutput(t *testing.T) {
	prog, err := Asm(`REQ_START draft
  REQ_YIELD none
  SET_MODEL model-a
  MSG_START
    ROLE_USR
    TXT_CHUNK Draft
  MSG_END
REQ_END
REQ_START polish
  REQ_YIELD content
  SET_MODEL model-b
  MSG_START
    ROLE_USR
    TXT_CHUNK <<<
Polish:

>>>
    SUB_CONTENT draft.content
  MSG_END
REQ_END
`)
	if err != nil {
		t.Fatalf("asm: %v", err)
	}

	spans := prog.Requests()
	if len(spans) != 2 {
		t.Fatalf("requests = %d, want 2", len(spans))
	}
	first, err := prog.MaterializeRequest(spans[0], nil)
	if err != nil {
		t.Fatalf("materialize first: %v", err)
	}
	if first.ID != "draft" || first.Yield != YieldNone {
		t.Fatalf("first unit = %#v", first)
	}
	if first.Program.HasOpcode(REQ_START) || first.Program.HasOpcode(REQ_YIELD) {
		t.Fatalf("sequence opcodes leaked into first request:\n%s", first.Program.Disasm())
	}

	second, err := prog.MaterializeRequest(spans[1], map[string]RequestOutput{
		"draft": {Content: "raw output"},
	})
	if err != nil {
		t.Fatalf("materialize second: %v", err)
	}
	msgs := second.Program.Messages()
	if len(msgs) != 1 {
		t.Fatalf("messages = %d, want 1", len(msgs))
	}
	if got := second.Program.MessageText(msgs[0]); got != "Polish:\nraw output" {
		t.Fatalf("message text = %q", got)
	}
}

func TestMaterializeRequestInjectsReasoning(t *testing.T) {
	prog, err := Asm(`REQ_START impl
  MSG_START
    ROLE_USR
    SUB_REASON plan.content
  MSG_END
REQ_END
`)
	if err != nil {
		t.Fatalf("asm: %v", err)
	}
	unit, err := prog.MaterializeRequest(prog.Requests()[0], map[string]RequestOutput{
		"plan": {Content: "private plan"},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if got := unit.Program.ThinkingText(unit.Program.Thinkings()[0]); got != "private plan" {
		t.Fatalf("thinking = %q", got)
	}
}

func TestSequenceEmitter(t *testing.T) {
	prog := NewProgram()
	prog.EmitString(SET_MODEL, "model-a")
	prog.Emit(MSG_START)
	prog.Emit(ROLE_USR)
	prog.EmitString(TXT_CHUNK, "hello")
	prog.Emit(MSG_END)

	emitted, err := NewSequenceEmitter(&ChatCompletionsEmitter{}).EmitRequests(prog, nil)
	if err != nil {
		t.Fatalf("emit requests: %v", err)
	}
	if len(emitted) != 1 {
		t.Fatalf("emitted = %d, want 1", len(emitted))
	}
	if emitted[0].ID != "main" || emitted[0].Yield != YieldContent {
		t.Fatalf("emitted metadata = %#v", emitted[0])
	}
	if !strings.Contains(string(emitted[0].Body), `"content":"hello"`) {
		t.Fatalf("body = %s", emitted[0].Body)
	}

	marshaled, err := json.Marshal(emitted)
	if err != nil {
		t.Fatalf("marshal emitted requests: %v", err)
	}
	if strings.Contains(string(marshaled), `"yield"`) || strings.Contains(string(marshaled), `"id"`) {
		t.Fatalf("runner metadata leaked into JSON: %s", marshaled)
	}
	if !strings.Contains(string(marshaled), `"messages"`) || !strings.Contains(string(marshaled), `"model"`) {
		t.Fatalf("provider body missing from JSON: %s", marshaled)
	}
}

func TestCaptureRequestOutput(t *testing.T) {
	prog := NewProgram()
	prog.Emit(STREAM_START)
	prog.EmitString(STREAM_THINK_DELTA, "why")
	prog.EmitString(STREAM_DELTA, "hello")
	prog.Emit(STREAM_END)

	out := CaptureRequestOutput(prog)
	if out.Content != "hello" {
		t.Fatalf("content = %q", out.Content)
	}
	if out.Reasoning != "why" {
		t.Fatalf("reasoning = %q", out.Reasoning)
	}
}

func TestCapturedResponseOutputs(t *testing.T) {
	prog, err := Asm(`RESP_START draft
  MSG_START
    ROLE_AST
    THINK_START
      THINK_CHUNK because
    THINK_END
    TXT_CHUNK answer
  MSG_END
RESP_END
`)
	if err != nil {
		t.Fatalf("asm: %v", err)
	}

	spans := prog.Responses()
	if len(spans) != 1 || spans[0].ID != "draft" {
		t.Fatalf("responses = %#v", spans)
	}
	outputs := prog.CaptureResponseOutputs()
	if outputs["draft"].Content != "answer" {
		t.Fatalf("content = %q", outputs["draft"].Content)
	}
	if outputs["draft"].Reasoning != "because" {
		t.Fatalf("reasoning = %q", outputs["draft"].Reasoning)
	}
}
