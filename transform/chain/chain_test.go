package chain

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/neutrome-labs/ail"
	"github.com/neutrome-labs/ail/transform"
)

type recordingExecutor struct {
	mu       sync.Mutex
	requests []*ail.RequestUnit
}

func (e *recordingExecutor) Execute(ctx context.Context, req *ail.RequestUnit) transform.Stream {
	e.mu.Lock()
	e.requests = append(e.requests, req)
	e.mu.Unlock()

	prog := ail.NewProgram()
	prog.EmitString(ail.STREAM_DELTA, "caption")
	return transform.FromPrograms(ctx, prog)
}

func (e *recordingExecutor) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.requests)
}

func (e *recordingExecutor) requestText(i int) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return lastMessageText(e.requests[i].Program)
}

type sequenceExecutor struct {
	mu       sync.Mutex
	requests []*ail.RequestUnit
	outputs  []string
}

func (e *sequenceExecutor) Execute(ctx context.Context, req *ail.RequestUnit) transform.Stream {
	e.mu.Lock()
	e.requests = append(e.requests, req)
	idx := len(e.requests) - 1
	out := e.outputs[idx]
	e.mu.Unlock()

	prog := ail.NewProgram()
	prog.Emit(ail.STREAM_START)
	prog.EmitString(ail.STREAM_DELTA, out)
	prog.EmitString(ail.RESP_DONE, "stop")
	prog.Emit(ail.STREAM_END)
	return transform.FromPrograms(ctx, prog)
}

func (e *sequenceExecutor) requestText(i int) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return lastMessageText(e.requests[i].Program)
}

func TestChainBuffersReasoningAndEmitsCaptions(t *testing.T) {
	ctx := context.Background()
	exec := &recordingExecutor{}
	ch := New(
		WithExecutor(exec),
		WithPrompt("Caption this reasoning."),
		WithModel("small-caption-model"),
		WithFlushEveryChars(6),
	)

	p1 := ail.NewProgram()
	p1.EmitString(ail.STREAM_THINK_DELTA, "abc")
	p2 := ail.NewProgram()
	p2.EmitString(ail.STREAM_DELTA, "visible")
	p3 := ail.NewProgram()
	p3.EmitString(ail.STREAM_THINK_DELTA, "def")
	p4 := ail.NewProgram()
	p4.Emit(ail.STREAM_END)

	out := ch.Apply(ctx, transform.FromPrograms(ctx, p1, p2, p3, p4))

	var content, reasoning string
	for ev := range out {
		if ev.Err != nil {
			t.Fatalf("event error: %v", ev.Err)
		}
		if ev.Program == nil {
			continue
		}
		for _, inst := range ev.Program.Code {
			switch inst.Op {
			case ail.STREAM_DELTA:
				content += inst.Str
			case ail.STREAM_THINK_DELTA:
				reasoning += inst.Str
			}
		}
	}

	if content != "visible" {
		t.Fatalf("content = %q, want visible", content)
	}
	if strings.Contains(reasoning, "abcdef") {
		t.Fatalf("raw reasoning leaked into output: %q", reasoning)
	}
	if !strings.Contains(reasoning, "caption") {
		t.Fatalf("caption missing: %q", reasoning)
	}
	if exec.count() != 1 {
		t.Fatalf("executor calls = %d, want 1", exec.count())
	}
	reqText := exec.requestText(0)
	if !strings.Contains(reqText, "Caption this reasoning.") || !strings.Contains(reqText, "abcdef") {
		t.Fatalf("request text = %q", reqText)
	}
	if exec.requests[0].Program.GetModel() != "small-caption-model" {
		t.Fatalf("request model = %q", exec.requests[0].Program.GetModel())
	}
}

func TestChainFlushesPartialBufferOnEnd(t *testing.T) {
	ctx := context.Background()
	exec := &recordingExecutor{}
	ch := New(WithExecutor(exec), WithFlushEveryChars(100))

	p1 := ail.NewProgram()
	p1.EmitString(ail.STREAM_THINK_DELTA, "short")
	p2 := ail.NewProgram()
	p2.Emit(ail.STREAM_END)

	for range ch.Apply(ctx, transform.FromPrograms(ctx, p1, p2)) {
	}
	if exec.count() != 1 {
		t.Fatalf("executor calls = %d, want 1", exec.count())
	}
	if !strings.Contains(exec.requestText(0), "short") {
		t.Fatalf("request did not include partial buffer: %q", exec.requestText(0))
	}
}

func TestChainCanEmitCaptionsAsContent(t *testing.T) {
	ctx := context.Background()
	exec := &recordingExecutor{}
	ch := New(
		WithExecutor(exec),
		WithTargetChannel(TargetContent),
		WithFlushEveryChars(1),
	)

	p := ail.NewProgram()
	p.EmitString(ail.STREAM_THINK_DELTA, "x")

	out := ch.Apply(ctx, transform.FromPrograms(ctx, p))
	var gotContent bool
	for ev := range out {
		if ev.Program == nil {
			continue
		}
		for _, inst := range ev.Program.Code {
			if inst.Op == ail.STREAM_DELTA && strings.Contains(inst.Str, "caption") {
				gotContent = true
			}
		}
	}
	if !gotContent {
		t.Fatalf("expected caption content delta")
	}
}

func TestChainSupportsBothSourceAndBothTarget(t *testing.T) {
	ctx := context.Background()
	exec := &sequenceExecutor{outputs: []string{"localized"}}
	ch := New(
		WithExecutor(exec),
		WithPrompt("Localize."),
		WithSourceField(SourceBoth),
		WithTargetChannel(TargetBoth),
		WithFlushEveryChars(1),
	)

	p := ail.NewProgram()
	p.EmitString(ail.STREAM_THINK_DELTA, "private ")
	p.EmitString(ail.STREAM_DELTA, "visible")

	out := ch.Apply(ctx, transform.FromPrograms(ctx, p))
	var content string
	for ev := range out {
		if ev.Err != nil {
			t.Fatalf("event error: %v", ev.Err)
		}
		if ev.Program == nil {
			continue
		}
		for _, inst := range ev.Program.Code {
			if inst.Op == ail.STREAM_DELTA {
				content += inst.Str
			}
			if inst.Op == ail.STREAM_THINK_DELTA {
				t.Fatalf("unexpected reasoning output %q", inst.Str)
			}
		}
	}

	reqText := exec.requestText(0)
	if !strings.Contains(reqText, "Localize.") || !strings.Contains(reqText, "private visible") {
		t.Fatalf("request text = %q", reqText)
	}
	if content != "localized" {
		t.Fatalf("content = %q", content)
	}
}

func TestChainMapsExecutorReasoningAndContentToBoth(t *testing.T) {
	ctx := context.Background()
	exec := transform.ExecutorFunc(func(ctx context.Context, req *ail.RequestUnit) transform.Stream {
		prog := ail.NewProgram()
		prog.EmitString(ail.STREAM_THINK_DELTA, "why")
		prog.EmitString(ail.STREAM_DELTA, "what")
		return transform.FromPrograms(ctx, prog)
	})
	ch := New(
		WithExecutor(exec),
		WithSourceField(SourceContent),
		WithTargetChannel(TargetBoth),
		WithFlushEveryChars(1),
	)

	p := ail.NewProgram()
	p.EmitString(ail.STREAM_DELTA, "source")

	out := ch.Apply(ctx, transform.FromPrograms(ctx, p))
	var reasoning, content string
	for ev := range out {
		if ev.Err != nil {
			t.Fatalf("event error: %v", ev.Err)
		}
		if ev.Program == nil {
			continue
		}
		for _, inst := range ev.Program.Code {
			switch inst.Op {
			case ail.STREAM_THINK_DELTA:
				reasoning += inst.Str
			case ail.STREAM_DELTA:
				content += inst.Str
			}
		}
	}
	if reasoning != "why" || content != "what" {
		t.Fatalf("reasoning=%q content=%q", reasoning, content)
	}
}

func TestChainIncludesPreviousSourceAndOutputInNextRequest(t *testing.T) {
	ctx := context.Background()
	exec := &sequenceExecutor{outputs: []string{"uno", "dos"}}
	ch := New(
		WithExecutor(exec),
		WithPrompt("Translate to Spanish."),
		WithSourceField(SourceContent),
		WithTargetChannel(TargetContent),
		WithFlushEveryChars(3),
	)

	p1 := ail.NewProgram()
	p1.EmitString(ail.STREAM_DELTA, "one")
	p2 := ail.NewProgram()
	p2.EmitString(ail.STREAM_DELTA, "two")

	for range ch.Apply(ctx, transform.FromPrograms(ctx, p1, p2)) {
	}

	if len(exec.requests) != 2 {
		t.Fatalf("executor requests = %d, want 2", len(exec.requests))
	}
	first := exec.requestText(0)
	if strings.Contains(first, "Previous source text") {
		t.Fatalf("first request unexpectedly included history: %q", first)
	}
	second := exec.requestText(1)
	for _, want := range []string{
		"Previous source text:\none",
		"Previous transformed text:\nuno",
		"Next source segment:\ntwo",
	} {
		if !strings.Contains(second, want) {
			t.Fatalf("second request missing %q:\n%s", want, second)
		}
	}
}

func TestChainEmitsSingleTerminalAfterFinalTranslatedChunk(t *testing.T) {
	ctx := context.Background()
	exec := &sequenceExecutor{outputs: []string{"uno", "dos"}}
	ch := New(
		WithExecutor(exec),
		WithSourceField(SourceContent),
		WithTargetChannel(TargetContent),
		WithFlushEveryChars(3),
	)

	p1 := ail.NewProgram()
	p1.Emit(ail.STREAM_START)
	p1.EmitString(ail.STREAM_DELTA, "one")
	p2 := ail.NewProgram()
	p2.EmitString(ail.STREAM_DELTA, "two")
	p3 := ail.NewProgram()
	p3.EmitString(ail.RESP_DONE, "stop")
	p3.Emit(ail.STREAM_END)

	out := ch.Apply(ctx, transform.FromPrograms(ctx, p1, p2, p3))
	var ops []ail.Opcode
	var content string
	for ev := range out {
		if ev.Err != nil {
			t.Fatalf("event error: %v", ev.Err)
		}
		if ev.Program == nil {
			continue
		}
		for _, inst := range ev.Program.Code {
			ops = append(ops, inst.Op)
			if inst.Op == ail.STREAM_DELTA {
				content += inst.Str
			}
		}
	}

	if content != "unodos" {
		t.Fatalf("content = %q, want unodos", content)
	}
	var doneCount, endCount int
	var lastDelta, doneAt, endAt int
	for i, op := range ops {
		switch op {
		case ail.STREAM_DELTA:
			lastDelta = i
		case ail.RESP_DONE:
			doneCount++
			doneAt = i
		case ail.STREAM_END:
			endCount++
			endAt = i
		}
	}
	if doneCount != 1 || endCount != 1 {
		t.Fatalf("terminal counts: RESP_DONE=%d STREAM_END=%d ops=%v", doneCount, endCount, ops)
	}
	if doneAt <= lastDelta || endAt <= doneAt {
		t.Fatalf("terminal order wrong: lastDelta=%d doneAt=%d endAt=%d ops=%v", lastDelta, doneAt, endAt, ops)
	}
}

func lastMessageText(prog *ail.Program) string {
	msgs := prog.Messages()
	if len(msgs) == 0 {
		return ""
	}
	return prog.MessageText(msgs[len(msgs)-1])
}
