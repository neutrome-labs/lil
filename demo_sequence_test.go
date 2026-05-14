package ail

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

type demoEmittedRequestJSON struct {
	ID   string          `json:"id"`
	Body json.RawMessage `json:"body"`
}

func TestDemoSequencePreviewEmitsDependentRequests(t *testing.T) {
	prog, err := Asm(`REQ_START main
  REQ_YIELD none
  SET_MODEL smart-model
  MSG_START
    ROLE_USR
    TXT_CHUNK Draft answer
  MSG_END
REQ_END
REQ_START chain:1
  REQ_YIELD content
  SET_MODEL localizer-model
  SET_STREAM
  MSG_START
    ROLE_USR
    TXT_CHUNK Localize:
    SUB_CONTENT main.content
  MSG_END
REQ_END
`)
	if err != nil {
		t.Fatalf("asm: %v", err)
	}

	out, err := demoPreviewEmitRequestSequence(&ChatCompletionsEmitter{}, prog)
	if err != nil {
		t.Fatalf("preview emit: %v", err)
	}
	if strings.Contains(string(out), `"yield"`) {
		t.Fatalf("yield leaked into demo output: %s", out)
	}

	var decoded []demoEmittedRequestJSON
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("unmarshal preview output: %v\n%s", err, out)
	}
	if len(decoded) != 2 {
		t.Fatalf("preview emitted %d requests, want 2", len(decoded))
	}
	if decoded[0].ID != "main" || decoded[1].ID != "chain:1" {
		t.Fatalf("ids = %#v", decoded)
	}
	if !strings.Contains(string(decoded[1].Body), "{{main.content}}") {
		t.Fatalf("dependent body did not include placeholder substrate: %s", decoded[1].Body)
	}
}

func demoPreviewEmitRequestSequence(emitter Emitter, prog *Program) ([]byte, error) {
	outputs := prog.CaptureResponseOutputs()
	requests := prog.Requests()

	out := make([]demoEmittedRequestJSON, 0, len(requests))
	for _, span := range requests {
		demoPreviewFillSelectorOutputs(prog, span, outputs)
		unit, err := prog.MaterializeRequest(span, outputs)
		if err != nil {
			return nil, err
		}
		body, err := emitter.EmitRequest(unit.Program)
		if err != nil {
			return nil, err
		}
		out = append(out, demoEmittedRequestJSON{ID: unit.ID, Body: json.RawMessage(body)})
		if _, ok := outputs[unit.ID]; !ok {
			outputs[unit.ID] = demoPreviewPlaceholderOutput(unit.ID)
		}
	}
	if len(out) == 1 {
		return out[0].Body, nil
	}
	return json.Marshal(out)
}

func demoPreviewFillSelectorOutputs(prog *Program, span RequestSpan, outputs map[string]RequestOutput) {
	start, end := span.Start, span.End
	if span.Explicit {
		start++
		end--
	}
	for i := start; i <= end && i < len(prog.Code); i++ {
		inst := prog.Code[i]
		if inst.Op != SUB_CONTENT && inst.Op != SUB_REASON {
			continue
		}
		reqID, _, ok := strings.Cut(inst.Str, ".")
		if ok && reqID != "" {
			if _, exists := outputs[reqID]; !exists {
				outputs[reqID] = demoPreviewPlaceholderOutput(reqID)
			}
		}
	}
}

func demoPreviewPlaceholderOutput(id string) RequestOutput {
	return RequestOutput{
		Content:   fmt.Sprintf("{{%s.content}}", id),
		Reasoning: fmt.Sprintf("{{%s.reasoning}}", id),
	}
}
