//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
	"syscall/js"

	"github.com/neutrome-labs/lil"
	"github.com/neutrome-labs/lil/manip"
	"github.com/neutrome-labs/lil/manip/kvtools"
	"github.com/neutrome-labs/lil/manip/slwin"
)

const styleLIL = "lil"

func main() {
	js.Global().Set("convertLIL", js.FuncOf(convertLIL))
	js.Global().Set("lilWasmReady", true)
	select {}
}

func convertLIL(this js.Value, args []js.Value) any {
	resp := js.Global().Get("Object").New()
	if len(args) != 1 {
		resp.Set("error", "convertLIL expects one request object")
		return resp
	}

	req := args[0]
	prog, err := parse([]byte(req.Get("input").String()), req)
	if err != nil {
		resp.Set("error", err.Error())
		return resp
	}

	prog, err = applyManips(prog, req.Get("manips"))
	if err != nil {
		resp.Set("error", err.Error())
		return resp
	}

	to := req.Get("toStyle").String()
	out, err := emit(prog, to, req.Get("type").String())
	if err != nil {
		resp.Set("error", err.Error())
		return resp
	}
	if to != styleLIL {
		out = prettyJSON(out)
	}

	resp.Set("output", string(out))
	resp.Set("disasm", prog.Disasm())
	return resp
}

func parse(input []byte, req js.Value) (*lil.Program, error) {
	from := req.Get("fromStyle").String()
	if from == styleLIL {
		prog, err := lil.Asm(string(input))
		if err != nil {
			return nil, fmt.Errorf("LIL parse error: %w", err)
		}
		return prog, nil
	}

	parser, err := parserFor(from, req.Get("type").String())
	if err != nil {
		return nil, err
	}
	return parser(input)
}

func parserFor(style, typ string) (func([]byte) (*lil.Program, error), error) {
	switch typ {
	case "request":
		parser, err := lil.GetParser(lil.Style(style))
		if err != nil {
			return nil, err
		}
		return parser.ParseRequest, nil
	case "response":
		parser, err := lil.GetResponseParser(lil.Style(style))
		if err != nil {
			return nil, err
		}
		return parser.ParseResponse, nil
	case "stream_chunk":
		parser, err := lil.GetStreamChunkParser(lil.Style(style))
		if err != nil {
			return nil, err
		}
		return parser.ParseStreamChunk, nil
	default:
		return nil, fmt.Errorf("unknown type %q", typ)
	}
}

func emit(prog *lil.Program, style, typ string) ([]byte, error) {
	if style == styleLIL {
		return []byte(prog.Disasm()), nil
	}

	switch typ {
	case "request":
		emitter, err := lil.GetEmitter(lil.Style(style))
		if err != nil {
			return nil, err
		}
		return emitter.EmitRequest(prog)
	case "response":
		emitter, err := lil.GetResponseEmitter(lil.Style(style))
		if err != nil {
			return nil, err
		}
		return emitter.EmitResponse(prog)
	case "stream_chunk":
		emitter, err := lil.GetStreamChunkEmitter(lil.Style(style))
		if err != nil {
			return nil, err
		}
		return emitter.EmitStreamChunk(prog)
	default:
		return nil, fmt.Errorf("unknown type %q", typ)
	}
}

func applyManips(prog *lil.Program, req js.Value) (*lil.Program, error) {
	if req.Type() != js.TypeObject {
		return prog, nil
	}

	active := make([]manip.Manip, 0, 2)
	if cfg := req.Get("kvtools"); cfg.Type() == js.TypeObject && jsBool(cfg.Get("enabled")) {
		active = append(active, kvtools.New(
			kvtools.WithScope("demo"),
			kvtools.WithKeepRecentInteractions(jsIntOr(cfg.Get("keepRecent"), 1)),
		))
	}
	if cfg := req.Get("slwin"); cfg.Type() == js.TypeObject && jsBool(cfg.Get("enabled")) {
		active = append(active, slwin.New(
			slwin.WithKeepStart(jsIntOr(cfg.Get("keepStart"), slwin.DefaultKeepStart)),
			slwin.WithKeepEnd(jsIntOr(cfg.Get("keepEnd"), slwin.DefaultKeepEnd)),
		))
	}
	if len(active) == 0 {
		return prog, nil
	}
	return manip.Chain(prog, active...)
}

func jsBool(v js.Value) bool {
	return v.Type() == js.TypeBoolean && v.Bool()
}

func jsIntOr(v js.Value, fallback int) int {
	if v.Type() != js.TypeNumber {
		return fallback
	}
	return v.Int()
}

func prettyJSON(body []byte) []byte {
	var raw json.RawMessage
	if json.Unmarshal(body, &raw) != nil {
		return body
	}
	pretty, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return body
	}
	return pretty
}
