package observability

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// LogFields holds structured log context.
type LogFields struct {
	CorrelationID string
	Component     string
	OrderID       string
	Intent        string
	Error         error
	Extra         map[string]any
	ChatID        int64
}

// Log performs structured logging with fields.
func Log(level string, message string, fields LogFields) {
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00")
	parts := []string{
		fmt.Sprintf("[%s]", timestamp),
		fmt.Sprintf("[%s]", level),
	}

	if fields.CorrelationID != "" {
		parts = append(parts, fmt.Sprintf("corr=%s", fields.CorrelationID))
	}
	if fields.Component != "" {
		parts = append(parts, fmt.Sprintf("comp=%s", fields.Component))
	}
	if fields.OrderID != "" {
		parts = append(parts, fmt.Sprintf("order=%s", fields.OrderID))
	}
	if fields.ChatID != 0 {
		parts = append(parts, fmt.Sprintf("chat=%d", fields.ChatID))
	}
	if fields.Intent != "" {
		parts = append(parts, fmt.Sprintf("intent=%s", fields.Intent))
	}
	if fields.Error != nil {
		parts = append(parts, fmt.Sprintf("err=%v", fields.Error))
	}
	for k, v := range fields.Extra {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}

	parts = append(parts, message)
	var logLine strings.Builder
	for _, p := range parts {
		logLine.WriteString(" " + p)
	}
	log.Printf("%-6s %s", level, logLine.String())
}

// Info logs an informational message.
func Info(message string, fields LogFields) {
	Log("INFO", message, fields)
}

// Warn logs a warning message.
func Warn(message string, fields LogFields) {
	Log("WARN", message, fields)
}

// Error logs an error message.
func Error(message string, fields LogFields) {
	Log("ERROR", message, fields)
}

// Debug logs a debug message.
func Debug(message string, fields LogFields) {
	Log("DEBUG", message, fields)
}

// ContextKey type for storing values in context.
type ContextKey string

const (
	// CorrelationIDKey stores the correlation ID in context
	CorrelationIDKey ContextKey = "correlation_id"
)

// WithCorrelationID creates a new context with a correlation ID.
func WithCorrelationID(ctx context.Context, correlationID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, CorrelationIDKey, correlationID)
}

// CorrelationIDFromContext retrieves the correlation ID from context.
func CorrelationIDFromContext(ctx context.Context) string {
	if v := ctx.Value(CorrelationIDKey); v != nil {
		if id, ok := v.(string); ok {
			return id
		}
	}
	return ""
}

// GenerateCorrelationID creates a new correlation ID (simple timestamp-based).
func GenerateCorrelationID() string {
	return fmt.Sprintf("req-%d", time.Now().UnixNano())
}
