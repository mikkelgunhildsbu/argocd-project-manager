package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// Entry represents a single audit log entry
type Entry struct {
	Timestamp   time.Time `json:"timestamp"`
	Action      string    `json:"action"` // "add" or "remove"
	Project     string    `json:"project"`
	Server      string    `json:"server"`
	Namespace   string    `json:"namespace"`
	Name        string    `json:"name,omitempty"`
	Description string    `json:"description"`
	UserAgent   string    `json:"user_agent,omitempty"`
	RemoteAddr  string    `json:"remote_addr,omitempty"`
}

// Logger handles audit logging to a file
type Logger struct {
	file *os.File
	mu   sync.Mutex
}

// NewLogger creates a new audit logger that writes to the specified file path
func NewLogger(filePath string) (*Logger, error) {
	// Open file in append mode, create if doesn't exist
	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open audit log file: %w", err)
	}

	return &Logger{file: file}, nil
}

// Log writes an audit entry to the log file
func (l *Logger) Log(entry Entry) error {
	entry.Timestamp = time.Now().UTC()

	l.mu.Lock()
	defer l.mu.Unlock()

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal audit entry: %w", err)
	}

	// Write as newline-delimited JSON
	if _, err := l.file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write audit entry: %w", err)
	}

	return nil
}

// Close closes the audit log file
func (l *Logger) Close() error {
	return l.file.Close()
}
