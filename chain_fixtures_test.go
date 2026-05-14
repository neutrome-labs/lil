package ail

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChainRequestFixtures(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("fixtures", "ail", "request", "*.ail"))
	if err != nil {
		t.Fatalf("glob fixtures: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected chain request fixtures")
	}

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			input, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			prog, err := Asm(string(input))
			if err != nil {
				t.Fatalf("asm fixture: %v", err)
			}
			spans := prog.Requests()
			if len(spans) < 2 {
				t.Fatalf("fixture should contain at least two requests, got %d", len(spans))
			}

			outputs := map[string]RequestOutput{}
			emitter := &ChatCompletionsEmitter{}
			for _, span := range spans {
				unit, err := prog.MaterializeRequest(span, outputs)
				if err != nil {
					t.Fatalf("materialize %s: %v", span.ID, err)
				}
				for _, inst := range unit.Program.Code {
					switch inst.Op {
					case REQ_START, REQ_YIELD, REQ_END, SUB_CONTENT, SUB_REASON, RESP_START, RESP_END:
						t.Fatalf("sequence opcode leaked into materialized request: %s", inst.Op)
					}
				}
				body, err := emitter.EmitRequest(unit.Program)
				if err != nil {
					t.Fatalf("emit %s: %v", span.ID, err)
				}
				var obj map[string]any
				if err := json.Unmarshal(body, &obj); err != nil {
					t.Fatalf("provider body is not JSON: %v", err)
				}
				if _, ok := obj["yield"]; ok {
					t.Fatalf("yield leaked into provider body: %s", body)
				}
				if strings.Contains(string(body), "REQ_YIELD") || strings.Contains(string(body), "SUB_CONTENT") || strings.Contains(string(body), "SUB_REASON") {
					t.Fatalf("sequence mnemonic leaked into provider body: %s", body)
				}

				outputs[unit.ID] = RequestOutput{
					Content:   "captured content for " + unit.ID,
					Reasoning: "captured reasoning for " + unit.ID,
				}
			}
		})
	}
}
