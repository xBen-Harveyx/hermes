package logx

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

type LineLogger struct {
	mu   sync.Mutex
	file *os.File
}

func New(path string) (*LineLogger, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &LineLogger{file: file}, nil
}

func (l *LineLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}

func (l *LineLogger) Write(text string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range splitLogLines(text) {
		_, _ = l.file.WriteString(fmt.Sprintf("%s %s\n", time.Now().Format("2006-01-02 15:04:05"), line))
	}
}

func (l *LineLogger) Writef(format string, args ...any) {
	l.Write(fmt.Sprintf(format, args...))
}

func splitLogLines(text string) []string {
	trimmed := strings.TrimRight(text, "\n")
	if trimmed == "" {
		return []string{""}
	}
	return strings.Split(trimmed, "\n")
}
