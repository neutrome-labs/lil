//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/neutrome-labs/ail"
)

type emittedRequestJSON struct {
	ID   string          `json:"id"`
	Body json.RawMessage `json:"body"`
}

func emitRequestSequence(emitter ail.Emitter, prog *ail.Program) ([]byte, error) {
	outputs := prog.CaptureResponseOutputs()
	requests := prog.Requests()

	out := make([]emittedRequestJSON, 0, len(requests))
	for _, span := range requests {
		fillDemoSelectorOutputs(prog, span, outputs)

		unit, err := prog.MaterializeRequest(span, outputs)
		if err != nil {
			return nil, err
		}
		body, err := emitter.EmitRequest(unit.Program)
		if err != nil {
			return nil, err
		}
		out = append(out, emittedRequestJSON{
			ID:   unit.ID,
			Body: json.RawMessage(body),
		})

		if _, ok := outputs[unit.ID]; !ok {
			outputs[unit.ID] = demoPlaceholderOutput(unit.ID)
		}
	}

	if len(out) == 1 {
		return out[0].Body, nil
	}
	return json.Marshal(out)
}

func fillDemoSelectorOutputs(prog *ail.Program, span ail.RequestSpan, outputs map[string]ail.RequestOutput) {
	if prog == nil {
		return
	}
	start, end := span.Start, span.End
	if span.Explicit {
		start++
		end--
	}
	if start < 0 {
		start = 0
	}
	if end >= len(prog.Code) {
		end = len(prog.Code) - 1
	}
	for i := start; i <= end && i < len(prog.Code); i++ {
		inst := prog.Code[i]
		if inst.Op != ail.SUB_CONTENT && inst.Op != ail.SUB_REASON {
			continue
		}
		reqID, _, ok := strings.Cut(inst.Str, ".")
		if !ok || reqID == "" {
			continue
		}
		if _, exists := outputs[reqID]; !exists {
			outputs[reqID] = demoPlaceholderOutput(reqID)
		}
	}
}

func demoPlaceholderOutput(id string) ail.RequestOutput {
	return ail.RequestOutput{
		Content:   fmt.Sprintf("{{%s.content}}", id),
		Reasoning: fmt.Sprintf("{{%s.reasoning}}", id),
	}
}

func shouldEmitRequestSequence(prog *ail.Program) bool {
	requests := prog.Requests()
	return len(requests) > 1 || len(prog.Responses()) > 0 || (len(requests) == 1 && requests[0].Explicit)
}
