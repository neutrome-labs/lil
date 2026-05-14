package slwin

import (
	"testing"

	"github.com/neutrome-labs/ail"
)

func numberedConversation(n int) *ail.Program {
	p := ail.NewProgram()
	p.EmitString(ail.SET_MODEL, "test-model")
	p.Emit(ail.DEF_START)
	p.EmitString(ail.DEF_NAME, "lookup")
	p.Emit(ail.DEF_END)
	for i := 0; i < n; i++ {
		p.Emit(ail.MSG_START)
		if i%2 == 0 {
			p.Emit(ail.ROLE_USR)
		} else {
			p.Emit(ail.ROLE_AST)
		}
		p.EmitString(ail.TXT_CHUNK, string(rune('a'+i)))
		p.Emit(ail.MSG_END)
	}
	return p
}

func messageTexts(t *testing.T, p *ail.Program) []string {
	t.Helper()
	msgs := p.Messages()
	out := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, p.MessageText(msg))
	}
	return out
}

func TestApplyKeepsStartAndEndMessages(t *testing.T) {
	p := numberedConversation(8)
	out := Apply(p, 3, 2)

	got := messageTexts(t, out)
	want := []string{"a", "b", "f", "g", "h"}
	if len(got) != len(want) {
		t.Fatalf("message count = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("message %d = %q, want %q", i, got[i], want[i])
		}
	}

	if out.GetModel() != "test-model" {
		t.Fatalf("model was not preserved")
	}
	if defs := out.ToolDefs(); len(defs) != 1 || defs[0].Name != "lookup" {
		t.Fatalf("tool defs were not preserved: %#v", defs)
	}
}

func TestApplyReturnsOriginalWhenWindowCoversConversation(t *testing.T) {
	p := numberedConversation(3)
	out := Apply(p, 2, 1)
	if out != p {
		t.Fatalf("expected unchanged program pointer")
	}
}

func TestFromParamsMatchesRouterSyntax(t *testing.T) {
	defaults := FromParams("")
	if defaults.KeepEnd != 10 || defaults.KeepStart != 1 {
		t.Fatalf("defaults = end:%d start:%d", defaults.KeepEnd, defaults.KeepStart)
	}

	customEnd := FromParams("15")
	if customEnd.KeepEnd != 15 || customEnd.KeepStart != 1 {
		t.Fatalf("custom end = end:%d start:%d", customEnd.KeepEnd, customEnd.KeepStart)
	}

	customBoth := FromParams("15:3")
	if customBoth.KeepEnd != 15 || customBoth.KeepStart != 3 {
		t.Fatalf("custom both = end:%d start:%d", customBoth.KeepEnd, customBoth.KeepStart)
	}
}
