package commands

import (
	"reflect"
	"testing"
)

func TestTokenize(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"hello world", []string{"hello", "world"}},
		{"one", []string{"one"}},
		{"", nil},
		{"  spaced  out  ", []string{"spaced", "out"}},
		{"--data '{\"key\":\"value\"}'", []string{"--data", `{"key":"value"}`}},
		{"keep 'multi word' together", []string{"keep", "multi word", "together"}},
	}
	for _, tt := range tests {
		got := tokenize(tt.input)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("tokenize(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestParseFlags(t *testing.T) {
	tests := []struct {
		input     string
		wantPos   []string
		wantFlags map[string]string
	}{
		{
			"list --limit 10 --sort name",
			[]string{"list"},
			map[string]string{"limit": "10", "sort": "name"},
		},
		{
			"create --name MyObj --verbose",
			[]string{"create"},
			map[string]string{"name": "MyObj", "verbose": "true"},
		},
		{
			"pos1 pos2",
			[]string{"pos1", "pos2"},
			map[string]string{},
		},
		{
			"",
			nil,
			map[string]string{},
		},
		{
			"create --data '{\"name\":\"test\"}'",
			[]string{"create"},
			map[string]string{"data": `{"name":"test"}`},
		},
	}
	for _, tt := range tests {
		pos, flags := parseFlags(tt.input)
		if !reflect.DeepEqual(pos, tt.wantPos) {
			t.Errorf("parseFlags(%q) pos = %v, want %v", tt.input, pos, tt.wantPos)
		}
		if !reflect.DeepEqual(flags, tt.wantFlags) {
			t.Errorf("parseFlags(%q) flags = %v, want %v", tt.input, flags, tt.wantFlags)
		}
	}
}

func TestParseData(t *testing.T) {
	t.Run("valid JSON", func(t *testing.T) {
		flags := map[string]string{"data": `{"name":"test","count":42}`}
		data, err := parseData(flags)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if data["name"] != "test" {
			t.Errorf("expected name=test, got %v", data["name"])
		}
	})

	t.Run("missing data flag", func(t *testing.T) {
		flags := map[string]string{"other": "value"}
		_, err := parseData(flags)
		if err == nil {
			t.Fatal("expected error for missing --data")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		flags := map[string]string{"data": "not-json"}
		_, err := parseData(flags)
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}

func TestFormatTable(t *testing.T) {
	t.Run("with data", func(t *testing.T) {
		headers := []string{"Name", "Type"}
		rows := [][]string{
			{"contacts", "standard"},
			{"deals", "standard"},
		}
		result := formatTable(headers, rows)
		if result == "" {
			t.Error("expected non-empty table")
		}
		// Should contain headers
		if !contains(result, "Name") || !contains(result, "Type") {
			t.Errorf("expected headers in output: %s", result)
		}
		// Should contain data
		if !contains(result, "contacts") || !contains(result, "deals") {
			t.Errorf("expected data in output: %s", result)
		}
	})

	t.Run("empty rows", func(t *testing.T) {
		result := formatTable([]string{"Name"}, nil)
		if result != "(no results)" {
			t.Errorf("expected '(no results)', got %q", result)
		}
	})
}

func TestFormatJSON(t *testing.T) {
	result := formatJSON(map[string]string{"key": "value"})
	if result == "" {
		t.Error("expected non-empty JSON output")
	}
	if !contains(result, `"key"`) {
		t.Errorf("expected key in JSON output: %s", result)
	}
}

func TestGetFlag(t *testing.T) {
	flags := map[string]string{"limit": "10", "sort": "name"}
	if v := getFlag(flags, "limit"); v != "10" {
		t.Errorf("expected '10', got %q", v)
	}
	if v := getFlag(flags, "missing"); v != "" {
		t.Errorf("expected empty, got %q", v)
	}
}

func TestGetFlagOr(t *testing.T) {
	flags := map[string]string{"limit": "10"}
	if v := getFlagOr(flags, "limit", "20"); v != "10" {
		t.Errorf("expected '10', got %q", v)
	}
	if v := getFlagOr(flags, "missing", "default"); v != "default" {
		t.Errorf("expected 'default', got %q", v)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && containsSub(s, sub)
}

func containsSub(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
