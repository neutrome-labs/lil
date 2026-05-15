package kvtools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/neutrome-labs/ail"
	"github.com/neutrome-labs/ail/manip"
)

func toolConversation() *ail.Program {
	p := ail.NewProgram()
	p.EmitString(ail.SET_MODEL, "gpt-4o")
	p.Emit(ail.MSG_START)
	p.Emit(ail.ROLE_USR)
	p.EmitString(ail.TXT_CHUNK, "first")
	p.Emit(ail.MSG_END)

	p.Emit(ail.MSG_START)
	p.Emit(ail.ROLE_AST)
	p.EmitString(ail.CALL_START, "call_old")
	p.EmitString(ail.CALL_NAME, "lookup")
	p.EmitJSON(ail.CALL_ARGS, json.RawMessage(`{"q":"old"}`))
	p.Emit(ail.CALL_END)
	p.Emit(ail.MSG_END)

	p.Emit(ail.MSG_START)
	p.Emit(ail.ROLE_TOOL)
	p.EmitString(ail.RESULT_START, "call_old")
	p.EmitString(ail.RESULT_DATA, "old-result")
	p.Emit(ail.RESULT_END)
	p.Emit(ail.MSG_END)

	p.Emit(ail.MSG_START)
	p.Emit(ail.ROLE_USR)
	p.EmitString(ail.TXT_CHUNK, "second")
	p.Emit(ail.MSG_END)

	p.Emit(ail.MSG_START)
	p.Emit(ail.ROLE_AST)
	p.EmitString(ail.CALL_START, "call_new")
	p.EmitString(ail.CALL_NAME, "lookup")
	p.EmitJSON(ail.CALL_ARGS, json.RawMessage(`{"q":"new"}`))
	p.Emit(ail.CALL_END)
	p.Emit(ail.MSG_END)

	p.Emit(ail.MSG_START)
	p.Emit(ail.ROLE_TOOL)
	p.EmitString(ail.RESULT_START, "call_new")
	p.EmitString(ail.RESULT_DATA, "new-result")
	p.Emit(ail.RESULT_END)
	p.Emit(ail.MSG_END)
	return p
}

func TestApplyCachesAndStripsOldResultsAndInjectsTool(t *testing.T) {
	store := manip.NewMemoryStore(100, time.Hour)
	kv := New(WithStore(store), WithScope("trace-1"))

	out, err := kv.Apply(toolConversation())
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	old, err := store.Get(context.Background(), DefaultKey(DefaultPrefix, "trace-1", "call_old"))
	if err != nil {
		t.Fatalf("old result not cached: %v", err)
	}
	if old != "old-result" {
		t.Fatalf("old cache = %q", old)
	}
	if _, err := store.Get(context.Background(), DefaultKey(DefaultPrefix, "trace-1", "call_new")); !errors.Is(err, manip.ErrNotFound) {
		t.Fatalf("new result should not be cached, err=%v", err)
	}

	var oldHasData, newHasData bool
	for _, result := range out.ToolResults() {
		_, data := resultData(out, result)
		switch result.CallID {
		case "call_old":
			oldHasData = data != ""
		case "call_new":
			newHasData = data != ""
		}
	}
	if oldHasData {
		t.Fatalf("old result data was not stripped")
	}
	if !newHasData {
		t.Fatalf("new result data should be kept")
	}
	if !hasToolDef(out, DefaultToolName) {
		t.Fatalf("retrieval tool was not injected")
	}
}

func TestApplyContextScopeOverridesStaticScope(t *testing.T) {
	store := manip.NewMemoryStore(100, time.Hour)
	kv := New(WithStore(store), WithScope("static"))
	ctx := ContextWithScope(context.Background(), "ctx-scope")

	if _, err := kv.ApplyContext(ctx, toolConversation()); err != nil {
		t.Fatalf("apply context: %v", err)
	}
	if _, err := store.Get(context.Background(), DefaultKey(DefaultPrefix, "static", "call_old")); !errors.Is(err, manip.ErrNotFound) {
		t.Fatalf("static scope should not have value, err=%v", err)
	}
	got, err := store.Get(context.Background(), DefaultKey(DefaultPrefix, "ctx-scope", "call_old"))
	if err != nil {
		t.Fatalf("context scoped value missing: %v", err)
	}
	if got != "old-result" {
		t.Fatalf("context scoped value = %q", got)
	}
}

func TestHandleToolCallAndDispatchCalls(t *testing.T) {
	kv := New(WithScope("trace"))
	ctx := context.Background()
	if err := kv.Store.Set(ctx, kv.KeyFunc(kv.Prefix, "trace", "call_old"), "cached", time.Hour); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	result, handled, err := kv.HandleToolCall(ctx, DefaultToolName, json.RawMessage(`{"tool_call_id":"call_old"}`))
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if !handled || result != "cached" {
		t.Fatalf("handled=%v result=%q", handled, result)
	}

	response := ail.NewProgram()
	response.Emit(ail.MSG_START)
	response.Emit(ail.ROLE_AST)
	response.EmitString(ail.CALL_START, "retrieve_1")
	response.EmitString(ail.CALL_NAME, DefaultToolName)
	response.EmitJSON(ail.CALL_ARGS, json.RawMessage(`{"tool_call_id":"call_old"}`))
	response.Emit(ail.CALL_END)
	response.Emit(ail.MSG_END)

	insts, n, err := kv.DispatchCalls(ctx, response)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if n != 1 {
		t.Fatalf("handled calls = %d", n)
	}
	resultProg := &ail.Program{Code: insts}
	results := resultProg.ToolResults()
	if len(results) != 1 || results[0].CallID != "retrieve_1" {
		t.Fatalf("unexpected tool result spans: %#v", results)
	}
	_, data := resultData(resultProg, results[0])
	if data != "cached" {
		t.Fatalf("tool result data = %q", data)
	}
}

func TestConvertRequestCanAttachKVTools(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role":"user","content":"first"},
			{"role":"assistant","content":null,"tool_calls":[{"id":"call_old","type":"function","function":{"name":"lookup","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"call_old","content":"old-result"},
			{"role":"user","content":"second"},
			{"role":"assistant","content":null,"tool_calls":[{"id":"call_new","type":"function","function":{"name":"lookup","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"call_new","content":"new-result"}
		]
	}`)
	kv := New(WithScope("trace"))

	out, err := manip.ConvertRequest(body, ail.StyleChatCompletions, ail.StyleChatCompletions, kv)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	var got struct {
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
		Messages []struct {
			Role       string `json:"role"`
			ToolCallID string `json:"tool_call_id"`
			Content    string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	foundTool := false
	for _, tool := range got.Tools {
		if tool.Function.Name == DefaultToolName {
			foundTool = true
		}
	}
	if !foundTool {
		t.Fatalf("emitted request did not include retrieval tool: %s", out)
	}
	for _, msg := range got.Messages {
		if msg.ToolCallID == "call_old" && msg.Content != "" {
			t.Fatalf("old tool content was not stripped: %#v", msg)
		}
		if msg.ToolCallID == "call_new" && msg.Content != "new-result" {
			t.Fatalf("new tool content was not kept: %#v", msg)
		}
	}
}
