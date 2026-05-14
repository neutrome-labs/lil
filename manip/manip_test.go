package manip_test

import (
	"encoding/json"
	"testing"

	"github.com/neutrome-labs/ail"
	"github.com/neutrome-labs/ail/manip"
	"github.com/neutrome-labs/ail/manip/slwin"
)

func TestConvertRequestAppliesManipBetweenParseAndEmit(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "system", "content": "sys"},
			{"role": "user", "content": "one"},
			{"role": "assistant", "content": "two"},
			{"role": "user", "content": "three"}
		]
	}`)

	out, err := manip.ConvertRequest(
		body,
		ail.StyleChatCompletions,
		ail.StyleChatCompletions,
		slwin.New(slwin.WithKeepEnd(1), slwin.WithKeepStart(1)),
	)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	var got struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("messages = %d, want 2: %s", len(got.Messages), out)
	}
	if got.Messages[0].Content != "sys" || got.Messages[1].Content != "three" {
		t.Fatalf("unexpected messages: %#v", got.Messages)
	}
}

func TestRequestConverterAppliesManipBetweenParseAndEmit(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "user", "content": "one"},
			{"role": "assistant", "content": "two"},
			{"role": "user", "content": "three"}
		]
	}`)

	converter, err := manip.NewRequestConverter(
		ail.StyleChatCompletions,
		ail.StyleChatCompletions,
		slwin.New(slwin.WithKeepEnd(1), slwin.WithKeepStart(0)),
	)
	if err != nil {
		t.Fatalf("new converter: %v", err)
	}
	out, err := converter.Convert(body)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	var got struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "three" {
		t.Fatalf("unexpected messages: %#v", got.Messages)
	}
}

func TestAttachEmitterAppliesManip(t *testing.T) {
	p := ail.NewProgram()
	p.EmitString(ail.SET_MODEL, "gpt-4o")
	for _, text := range []string{"a", "b", "c"} {
		p.Emit(ail.MSG_START)
		p.Emit(ail.ROLE_USR)
		p.EmitString(ail.TXT_CHUNK, text)
		p.Emit(ail.MSG_END)
	}

	emitter := manip.AttachEmitter(
		&ail.ChatCompletionsEmitter{},
		slwin.New(slwin.WithKeepEnd(1), slwin.WithKeepStart(0)),
	)
	out, err := emitter.EmitRequest(p)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	var got struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "c" {
		t.Fatalf("unexpected messages: %#v", got.Messages)
	}
}
