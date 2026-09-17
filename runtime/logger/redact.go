package logger

import (
	"encoding/json"
	"os"
	"strings"
)

// Redact removes configured credential values before text crosses log/UI boundaries.
func Redact(text string) string {
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		key = strings.ToUpper(key)
		if ok && len(value) >= 8 && (strings.Contains(key, "KEY") || strings.Contains(key, "TOKEN") || strings.Contains(key, "SECRET") || strings.Contains(key, "PASSWORD")) {
			text = strings.ReplaceAll(text, value, "[REDACTED]")
		}
	}
	return text
}
func safeArgs(args []any) []any {
	out := append([]any(nil), args...)
	for i := 1; i < len(out); i += 2 {
		// encoding/json represents most error implementations as {}, which hid
		// the useful diagnostic as map[]. Preserve their text before redaction.
		if err, ok := out[i].(error); ok {
			out[i] = Redact(err.Error())
			continue
		}
		b, err := json.Marshal(out[i])
		if err != nil {
			continue
		}
		var value any
		if json.Unmarshal([]byte(Redact(string(b))), &value) == nil {
			out[i] = value
		}
	}
	return out
}
