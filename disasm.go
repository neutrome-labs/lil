package lil

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// ─── Disassembly (human-readable) ────────────────────────────────────────────

// Disasm returns a human-readable assembly listing of the program.
//
// If the program contains side-buffers referenced by *_REF opcodes, they are
// emitted as base64-encoded ".ref N"
// directives at the very top, before any opcodes. Asm() understands this
// format and round-trips them back into Program.Buffers.
func (p *Program) Disasm() string {
	var sb strings.Builder

	// ── Buffer declarations ──────────────────────────────────────────────────
	if len(p.Buffers) > 0 {
		for i, buf := range p.Buffers {
			sb.WriteString(fmt.Sprintf(".ref %d %s\n", i, base64.StdEncoding.EncodeToString(buf)))
		}
		sb.WriteByte('\n')
	}

	indent := 0
	for _, inst := range p.Code {
		// Decrease indent before END opcodes
		switch inst.Op {
		case MSG_END, DEF_END, CALL_END, RESULT_END, STREAM_END, THINK_END:
			indent--
			if indent < 0 {
				indent = 0
			}
		}

		for range indent {
			sb.WriteString("  ")
		}

		sb.WriteString(inst.Op.Name())

		// writeStr emits a string argument, using a heredoc block when the
		// value contains newlines so that the Asm round-trip is lossless.
		writeStr := func(s string) {
			if strings.Contains(s, "\n") {
				sb.WriteString(" <<<\n")
				sb.WriteString(s)
				sb.WriteString("\n>>>")
			} else {
				sb.WriteByte(' ')
				sb.WriteString(s)
			}
		}

		// writeJSON emits a JSON argument as a compacted single line.
		writeJSON := func(j json.RawMessage) {
			var buf bytes.Buffer
			if err := json.Compact(&buf, j); err != nil {
				// Fallback: write as-is (should not happen for valid programs).
				sb.WriteByte(' ')
				sb.Write(j)
			} else {
				sb.WriteByte(' ')
				sb.Write(buf.Bytes())
			}
		}

		writeFormat := func(j json.RawMessage) {
			var obj map[string]json.RawMessage
			if json.Unmarshal(j, &obj) == nil && len(obj) == 1 {
				if typeRaw, ok := obj["type"]; ok {
					var typ string
					if json.Unmarshal(typeRaw, &typ) == nil && typ != "" {
						sb.WriteByte(' ')
						sb.WriteString(typ)
						return
					}
				}
			}
			writeJSON(j)
		}

		switch inst.Op {
		case TXT_CHUNK, DEF_NAME, DEF_DESC, CALL_START, CALL_NAME,
			RESULT_START, RESULT_DATA, RESP_ID, RESP_MODEL, RESP_DONE,
			SET_MODEL, SET_STOP, STREAM_DELTA,
			THINK_CHUNK, STREAM_THINK_DELTA, SET_REASON_EFFORT, SET_REASON_MODE:
			writeStr(inst.Str)

		case SET_TEMP, SET_TOPP:
			sb.WriteString(fmt.Sprintf(" %.4f", inst.Num))

		case SET_MAX, SET_REASON_BUDGET:
			sb.WriteString(fmt.Sprintf(" %d", inst.Int))

		case IMG_REF, AUD_REF, TXT_REF, FILE_REF, VID_REF, THINK_REF:
			sb.WriteString(fmt.Sprintf(" ref:%d", inst.Ref))

		case SET_FMT:
			writeFormat(inst.JSON)

		case PART_JSON, DEF_SCHEMA, DEF_RAW, CALL_ARGS, USAGE, STREAM_TOOL_DELTA, SET_SAFETY, SET_TOOL:
			writeJSON(inst.JSON)

		case SET_META:
			sb.WriteByte(' ')
			sb.WriteString(inst.Key)
			sb.WriteByte(' ')
			sb.WriteString(inst.Str)

		case EXT_DATA:
			sb.WriteByte(' ')
			sb.WriteString(inst.Key)
			writeJSON(inst.JSON)
		}

		sb.WriteByte('\n')

		// Increase indent after START opcodes
		switch inst.Op {
		case MSG_START, DEF_START, CALL_START, RESULT_START, STREAM_START, THINK_START:
			indent++
		}
	}
	return sb.String()
}
