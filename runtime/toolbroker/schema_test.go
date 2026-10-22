package toolbroker

import (
	"encoding/json"
	"testing"
)

func TestSchemaConstraintsAreEnforced(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"mode":{"type":"string","enum":["read"]},"count":{"type":"integer","minimum":1,"maximum":3},"config":{"type":"object","required":["path"],"properties":{"path":{"type":"string","minLength":1}},"additionalProperties":false}},"required":["mode","count","config"],"additionalProperties":false}`)
	for _, fixture := range []struct {
		args  string
		valid bool
	}{
		{`{"mode":"read","count":2,"config":{"path":"a"}}`, true},
		{`{"mode":"write","count":2,"config":{"path":"a"}}`, false},
		{`{"mode":"read","count":4,"config":{"path":"a"}}`, false},
		{`{"mode":"read","count":2,"config":{}}`, false},
		{`{"mode":"read","count":2,"config":{"path":""}}`, false},
		{`{"mode":"read","count":2,"config":{"path":"a","extra":true}}`, false},
	} {
		var args map[string]any
		if err := json.Unmarshal([]byte(fixture.args), &args); err != nil {
			t.Fatal(err)
		}
		if err := validateArguments(schema, args); (err == nil) != fixture.valid {
			t.Errorf("args=%s err=%v", fixture.args, err)
		}
	}
}

func TestNumericConstraintsAvoidToleranceAndOverflowBypasses(t *testing.T) {
	for _, fixture := range []struct {
		step  string
		value string
		valid bool
	}{
		{"1", "1.0000000001", false},
		{"0.1", "0.3", true},
		{"1e-308", "1e308", true},
	} {
		raw := json.RawMessage(`{"type":"object","properties":{"value":{"type":"number","multipleOf":` + fixture.step + `}}}`)
		var args map[string]any
		if err := json.Unmarshal([]byte(`{"value":`+fixture.value+`}`), &args); err != nil {
			t.Fatal(err)
		}
		if err := validateArguments(raw, args); (err == nil) != fixture.valid {
			t.Errorf("step=%s value=%s err=%v", fixture.step, fixture.value, err)
		}
	}
	if !matchesJSONType(float64(1e30), "integer") {
		t.Fatal("valid integer overflowed an int64 conversion")
	}
}

func TestSchemaAdmissionFailsClosed(t *testing.T) {
	for _, raw := range []string{
		`{"type":"object","$ref":"https://example.com/schema"}`,
		`{"type":"object","properties":{"x":{"format":"email"}}}`,
		`{"type":"object","properties":{"x":{"type":"unknown"}}}`,
		`{"type":"object","properties":{"x":{"minimum":"3"}}}`,
		`{"type":"object","properties":{"x":{"pattern":"["}}}`,
	} {
		var schema map[string]any
		if err := json.Unmarshal([]byte(raw), &schema); err != nil {
			t.Fatal(err)
		}
		if err := admitSchema(schema, 0); err == nil {
			t.Errorf("unsupported schema admitted: %s", raw)
		}
	}
}
