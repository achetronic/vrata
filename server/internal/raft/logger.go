package raft

import (
	"io"
	"log/slog"
	"strings"

	hclog "github.com/hashicorp/go-hclog"
)

// newRaftLogger creates a hashicorp/go-hclog logger that delegates to slog.
func newRaftLogger(logger *slog.Logger) hclog.Logger {
	return hclog.New(&hclog.LoggerOptions{
		Name:   "raft",
		Level:  hclog.Info,
		Output: newSlogWriter(logger),
	})
}

// slogWriter adapts slog.Logger to io.Writer for hclog output.
type slogWriter struct {
	logger *slog.Logger
}

func newSlogWriter(logger *slog.Logger) io.Writer {
	return &slogWriter{logger: logger}
}

func (w *slogWriter) Write(p []byte) (int, error) {
	msg := strings.TrimRight(string(p), "\n")
	if strings.Contains(msg, "[ERROR]") || strings.Contains(msg, "[WARN]") {
		w.logger.Warn(msg)
	} else {
		w.logger.Debug(msg)
	}
	return len(p), nil
}

var _ io.Writer = (*slogWriter)(nil)
