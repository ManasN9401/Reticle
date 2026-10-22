package toolbroker

import (
    "encoding/json"
    "testing"
)

func TestAuditSchemaConstraints(t *testing.T) {
    schema := json.RawMessage(`{"type":"object","properties":{"mode":{"type":"string","enum":["read"]},"count":{"type":"integer","maximum":2},"target":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}},"required":["mode","count","target"],"additionalProperties":false}`)
    invalid := map[string]any{"mode":"write", "count":float64(999), "target":map[string]any{"unexpected":true}}
    t.Logf("enum, maximum and nested required/additionalProperties violations: %v", validateArguments(schema, invalid))
}
