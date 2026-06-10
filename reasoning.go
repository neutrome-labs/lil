package lil

import "encoding/json"

func emitThinkingConfig(prog *Program, raw json.RawMessage, prefix string) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		prog.EmitKeyJSON(EXT_DATA, prefix, raw)
		return
	}
	for _, key := range []string{"type", "mode"} {
		if modeRaw, ok := obj[key]; ok {
			var mode string
			if json.Unmarshal(modeRaw, &mode) == nil && mode != "" {
				prog.EmitString(SET_REASON_MODE, mode)
				delete(obj, key)
				break
			}
		}
	}
	for _, key := range []string{"budget_tokens", "thinking_budget", "thinkingBudget"} {
		if budgetRaw, ok := obj[key]; ok {
			var budget int32
			if json.Unmarshal(budgetRaw, &budget) == nil {
				prog.EmitInt(SET_REASON_BUDGET, budget)
				delete(obj, key)
				break
			}
		}
	}
	for key, val := range obj {
		prog.EmitKeyJSON(EXT_DATA, prefix+"."+key, val)
	}
}

func emitReasoningConfig(prog *Program, raw json.RawMessage) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		prog.EmitKeyJSON(EXT_DATA, "reasoning", raw)
		return
	}
	if effortRaw, ok := obj["effort"]; ok {
		var effort string
		if json.Unmarshal(effortRaw, &effort) == nil && effort != "" {
			prog.EmitString(SET_REASON_EFFORT, effort)
			delete(obj, "effort")
		}
	}
	for key, val := range obj {
		prog.EmitKeyJSON(EXT_DATA, "reasoning."+key, val)
	}
}

func mergeReasoningConfig(extras map[string]any, effort string) json.RawMessage {
	obj := make(map[string]any)
	if effort != "" {
		obj["effort"] = effort
	}
	for key, val := range extras {
		obj[key] = val
	}
	if len(obj) == 0 {
		return nil
	}
	out, _ := json.Marshal(obj)
	return out
}

func prefixedExtras(ec *ExtrasCollector, prefix string) map[string]any {
	extras := make(map[string]any)
	cur := ec.levels[len(ec.levels)-1]
	for key, val := range cur {
		if key == prefix {
			if raw, ok := val.(json.RawMessage); ok {
				var obj map[string]any
				if json.Unmarshal(raw, &obj) == nil {
					for k, v := range obj {
						extras[k] = v
					}
				}
			}
			delete(cur, key)
			continue
		}
		prefixDot := prefix + "."
		if len(key) > len(prefixDot) && key[:len(prefixDot)] == prefixDot {
			extras[key[len(prefixDot):]] = val
			delete(cur, key)
		}
	}
	return extras
}
