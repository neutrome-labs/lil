package lil

import "encoding/json"

func unwrapRawObjects(items []map[string]any) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		if len(item) == 1 {
			if raw, ok := item["_raw"]; ok {
				out = append(out, raw)
				continue
			}
		}
		out = append(out, item)
	}
	return out
}

func rawMap(j json.RawMessage) map[string]any {
	var m map[string]any
	if json.Unmarshal(j, &m) == nil {
		return m
	}
	return map[string]any{"_raw": j}
}
