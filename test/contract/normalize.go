package contract

import "strings"

type Snapshot struct {
	Status       int
	Headers      map[string][]string
	JSONBody     any
	DatabaseRows []map[string]any
	Paths        []string
}

type Options struct {
	Ports map[int]string
}

func Normalize(s Snapshot, opts Options) Snapshot {
	return Snapshot{
		Status:       s.Status,
		Headers:      normalizeHeaders(s.Headers),
		JSONBody:     normalizeValue("", s.JSONBody, opts),
		DatabaseRows: normalizeRows(s.DatabaseRows, opts),
		Paths:        normalizePaths(s.Paths),
	}
}

func normalizeHeaders(headers map[string][]string) map[string][]string {
	keep := map[string]bool{"content-type": true, "location": true, "set-cookie": true, "cache-control": true}
	out := map[string][]string{}
	for key, values := range headers {
		if keep[strings.ToLower(key)] {
			out[key] = append([]string(nil), values...)
		}
	}
	return out
}

func normalizeRows(rows []map[string]any, opts Options) []map[string]any {
	out := make([]map[string]any, len(rows))
	for i, row := range rows {
		out[i] = normalizeValue("", row, opts).(map[string]any)
	}
	return out
}

func normalizePaths(paths []string) []string {
	out := make([]string, len(paths))
	for i := range paths {
		out[i] = "<path>"
	}
	return out
}

func normalizeValue(field string, value any, opts Options) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, child := range v {
			out[key] = normalizeValue(key, child, opts)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = normalizeValue(field, child, opts)
		}
		return out
	case float64:
		if field == "pid" {
			return "<pid>"
		}
		if field == "uptime" {
			return "<duration>"
		}
		if field == "port" {
			if label, ok := opts.Ports[int(v)]; ok {
				return label
			}
		}
		return v
	case int:
		if field == "pid" {
			return "<pid>"
		}
		if field == "uptime" {
			return "<duration>"
		}
		if field == "port" {
			if label, ok := opts.Ports[v]; ok {
				return label
			}
		}
		return v
	case string:
		if field == "created_at" || field == "updated_at" {
			return "<timestamp>"
		}
		if field == "path" || strings.HasSuffix(field, "_path") || strings.HasSuffix(field, "_dir") {
			return "<path>"
		}
		return v
	default:
		return value
	}
}
