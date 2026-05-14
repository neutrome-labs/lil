package transform

import (
	"context"
	"testing"

	"github.com/neutrome-labs/ail"
)

func TestFanOutClonesPrograms(t *testing.T) {
	ctx := context.Background()
	prog := ail.NewProgram()
	prog.EmitString(ail.STREAM_DELTA, "hello")

	streams := FanOut(ctx, FromPrograms(ctx, prog), 2)
	if len(streams) != 2 {
		t.Fatalf("streams = %d, want 2", len(streams))
	}

	a := <-streams[0]
	b := <-streams[1]
	if a.Program == nil || b.Program == nil {
		t.Fatalf("expected programs")
	}
	if a.Program == b.Program {
		t.Fatalf("fanout reused program pointer")
	}
	a.Program.Code[0].Str = "changed"
	if b.Program.Code[0].Str != "hello" {
		t.Fatalf("branch mutation leaked: %q", b.Program.Code[0].Str)
	}
}

func TestMerge(t *testing.T) {
	ctx := context.Background()
	a := ail.NewProgram()
	a.EmitString(ail.STREAM_DELTA, "a")
	b := ail.NewProgram()
	b.EmitString(ail.STREAM_DELTA, "b")

	merged := Merge(ctx, FromPrograms(ctx, a), FromPrograms(ctx, b))
	var got int
	for ev := range merged {
		if ev.Program != nil {
			got++
		}
	}
	if got != 2 {
		t.Fatalf("merged programs = %d, want 2", got)
	}
}
