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

func formatArgsForConsole(msg string, args []any) string {
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
				payloadStr = fmt.Sprintf(" -> %s", string(b))
			} else {
				payloadStr = fmt.Sprintf(" -> %v", payload)
			}
		}
		
		return fmt.Sprintf("[%s] %v%s", comp, event, payloadStr)
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
	return out
}

func (l *Logger) Info(msg string, args ...any) {
	// Extremely clean, human-readable console output
	fmt.Printf("[%s] INFO: %s\n", time.Now().Format("15:04:05"), formatArgsForConsole(msg, args))
	
	// Structured JSON logging for the file
	if l.jsonLogger != nil {
		l.jsonLogger.Info(msg, args...)
	}
}

func (l *Logger) Error(msg string, args ...any) {
	fmt.Printf("[%s] ERROR: %s\n", time.Now().Format("15:04:05"), formatArgsForConsole(msg, args))
	if l.jsonLogger != nil {
		l.jsonLogger.Error(msg, args...)
	}
}
