package notifier

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"sunbreak/internal/model"
)

func TestDispatchWritesStdoutNotification(t *testing.T) {
	var buf bytes.Buffer
	dispatcher := NewDispatcher(true, &buf, slog.New(slog.NewTextHandler(io.Discard, nil)))
	err := dispatcher.Dispatch(context.Background(), model.OutboxMessage{
		Channel: "stdout",
		Subject: "Match",
		Body:    "Body",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "[sunbreak notification] Match") || !strings.Contains(buf.String(), "Body") {
		t.Fatalf("unexpected notification output: %q", buf.String())
	}
}

func TestDispatchSkipsDisabledStdout(t *testing.T) {
	var buf bytes.Buffer
	dispatcher := NewDispatcher(false, &buf, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := dispatcher.Dispatch(context.Background(), model.OutboxMessage{Channel: "stdout"}); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no output, got %q", buf.String())
	}
}

func TestDispatchRejectsUnsupportedChannel(t *testing.T) {
	dispatcher := NewDispatcher(true, io.Discard, nil)
	err := dispatcher.Dispatch(context.Background(), model.OutboxMessage{Channel: "email"})
	if err == nil {
		t.Fatal("expected unsupported channel error")
	}
	if !strings.Contains(err.Error(), "unsupported notification channel") {
		t.Fatalf("unexpected error: %v", err)
	}
}
