package logger

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// Logger defines the structured logging interface for the Skeleton Runtime.
type Logger struct {
	jsonLogger *slog.Logger
	DebugMode  bool
}

func New() *Logger {
	// For the skeleton, attempt to create a local logs directory relative to the executable root
	logDir := filepath.Join("..", "..", "logs")
	_ = os.MkdirAll(logDir, 0755)

	logPath := filepath.Join(logDir, "runtime.log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	
	var jsonLogger *slog.Logger
	if err == nil {
		// Machine-readable JSON handler strictly for the log file
		jsonHandler := slog.NewJSONHandler(file, &slog.HandlerOptions{Level: slog.LevelInfo})
		jsonLogger = slog.New(jsonHandler)
	}

	return &Logger{
		jsonLogger: jsonLogger,
		DebugMode:  false, // Default to normal mode
	}
}

func truncateMap(m map[string]any) {
	for k, v := range m {
		switch val := v.(type) {
		case string:
			if len(val) > 50 {
				m[k] = val[:47] + "..."
			}
		case map[string]any:
			truncateMap(val)
		case []any:
			for _, item := range val {
				if itemMap, ok := item.(map[string]any); ok {
					truncateMap(itemMap)
				}
			}
		}
	}
}

func formatArgsForConsole(msg string, args []any) (string, bool, bool) {
	fields := make(map[string]any)
	for i := 0; i < len(args); i += 2 {
		if i+1 < len(args) {
			if key, ok := args[i].(string); ok {
				fields[key] = args[i+1]
			}
		}
	}

	// Custom readable format specifically for domain events
	if event, ok := fields["event"]; ok {
		comp := fields["component"]
		payload := fields["payload"]
		
		payloadStr := ""
		if payload != nil {
			if b, err := json.Marshal(payload); err == nil && string(b) != "null" {
				var rawMap map[string]any
				if err := json.Unmarshal(b, &rawMap); err == nil {
					truncateMap(rawMap)
					if b2, err2 := json.Marshal(rawMap); err2 == nil {
						payloadStr = fmt.Sprintf(" -> %s", string(b2))
					}
				} else {
					payloadStr = fmt.Sprintf(" -> %s", string(b))
				}
			} else {
				payloadStr = fmt.Sprintf(" -> %v", payload)
			}
		}
		
		skipConsole := false
		eventName := fmt.Sprintf("%v", event)
		if eventName == "WaitlistStateRequested" || eventName == "WorkflowStateRequested" || eventName == "WaitlistUpdated" || eventName == "WorkflowUpdated" {
			skipConsole = true
		}
		
		return fmt.Sprintf("[%s] %v%s", comp, event, payloadStr), true, skipConsole
	}

	// Fallback for infrastructure logs (e.g. startup/shutdown)
	out := msg
	if len(args) > 0 {
		out += " ("
		first := true
		for k, v := range fields {
			// Skip session ID in terminal for readability, it's still in the JSON file
			if k == "session" {
				continue
			}
			if !first {
				out += ", "
			}
			out += fmt.Sprintf("%v: %v", k, v)
			first = false
		}
		out += ")"
	}
	return out, false, false
}

func (l *Logger) Info(msg string, args ...any) {
	out, isEvent, skipConsole := formatArgsForConsole(msg, args)
	
	if !skipConsole {
		if isEvent {
			fmt.Printf("[%s] EVENT: %s\n", time.Now().Format("15:04:05"), out)
		} else {
			// Extremely clean, human-readable console output
			fmt.Printf("[%s] INFO: %s\n", time.Now().Format("15:04:05"), out)
		}
	}
	
	// Structured JSON logging for the file
	if l.jsonLogger != nil {
		l.jsonLogger.Info(msg, args...)
	}
}

func (l *Logger) Error(msg string, args ...any) {
	out, isEvent, skipConsole := formatArgsForConsole(msg, args)
	
	if !skipConsole {
		if isEvent {
			fmt.Printf("[%s] EVENT (ERROR): %s\n", time.Now().Format("15:04:05"), out)
		} else {
			fmt.Printf("[%s] ERROR: %s\n", time.Now().Format("15:04:05"), out)
		}
	}

	if l.jsonLogger != nil {
		l.jsonLogger.Error(msg, args...)
	}
}
