package contract

import "testing"

func TestNormalizeSnapshot(t *testing.T) {
	s := Snapshot{
		Status: 200,
		Headers: map[string][]string{
			"Date":          {"Sat, 18 Jul 2026 00:00:00 GMT"},
			"X-Request-ID":  {"abc123"},
			"Content-Type":  {"application/json"},
			"Authorization": {"secret"},
		},
		JSONBody: map[string]any{
			"updated_at": "2026-07-18 21:00:00",
			"created_at": "2026-07-18 20:00:00",
			"pid":        float64(1234),
			"uptime":     float64(99),
			"port":       float64(51234),
			"items": []any{
				map[string]any{"port": float64(51235), "name": "kept"},
			},
		},
		DatabaseRows: []map[string]any{{"id": float64(1), "updated_at": "2026-07-18 21:00:00"}},
		Paths:        []string{`C:\Users\fadlee\AppData\Local\Temp\gowa\instances\one`, `/tmp/gowa/instances/two`},
	}
	n := Normalize(s, Options{Ports: map[int]string{51234: "<manager-port>", 51235: "<instance-port>"}})
	if n.Headers["Date"] != nil || n.Headers["X-Request-ID"] != nil || n.Headers["Authorization"] != nil {
		t.Fatalf("volatile/sensitive headers not removed: %#v", n.Headers)
	}
	body := n.JSONBody.(map[string]any)
	if body["created_at"] != "<timestamp>" || body["updated_at"] != "<timestamp>" || body["pid"] != "<pid>" || body["uptime"] != "<duration>" || body["port"] != "<manager-port>" {
		t.Fatalf("unexpected normalized body: %#v", body)
	}
	items := body["items"].([]any)
	if items[0].(map[string]any)["name"] != "kept" || items[0].(map[string]any)["port"] != "<instance-port>" {
		t.Fatalf("array/object normalization failed: %#v", items)
	}
	if n.DatabaseRows[0]["updated_at"] != "<timestamp>" {
		t.Fatalf("database rows not normalized: %#v", n.DatabaseRows)
	}
	if n.Paths[0] != "<path>" || n.Paths[1] != "<path>" {
		t.Fatalf("paths not normalized: %#v", n.Paths)
	}
}
