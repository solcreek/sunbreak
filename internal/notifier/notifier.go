package notifier

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"radar/internal/model"
)

type Dispatcher struct {
	stdout bool
	writer io.Writer
	logger *slog.Logger
}

func NewDispatcher(stdout bool, writer io.Writer, logger *slog.Logger) *Dispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Dispatcher{stdout: stdout, writer: writer, logger: logger}
}

func (d *Dispatcher) Dispatch(ctx context.Context, msg model.OutboxMessage) error {
	_ = ctx
	switch msg.Channel {
	case "stdout", "":
		if !d.stdout {
			d.logger.Info("stdout notification skipped", "outbox_id", msg.ID, "subject", msg.Subject)
			return nil
		}
		if d.writer == nil {
			return nil
		}
		_, err := fmt.Fprintf(d.writer, "\n[radar notification] %s\n%s\n", msg.Subject, msg.Body)
		return err
	default:
		return fmt.Errorf("unsupported notification channel: %s", msg.Channel)
	}
}
