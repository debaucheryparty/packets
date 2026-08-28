package storage

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go"
)

type NATSQueue struct {
	logger *slog.Logger
	conn   *nats.Conn
}

func NewNATSQueue(ctx context.Context, logger *slog.Logger, url string) (*NATSQueue, error) {
	conn, err := nats.Connect(url,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(10),
	)
	if err != nil {
		return nil, fmt.Errorf("NewNATSQueue connect to %s: %w", url, err)
	}

	logger.InfoContext(ctx, "connected to NATS", slog.String("url", url))

	return &NATSQueue{logger: logger, conn: conn}, nil
}

func (q *NATSQueue) Publish(ctx context.Context, subject string, data []byte) error {
	if err := q.conn.Publish(subject, data); err != nil {
		return fmt.Errorf("NATSQueue.Publish %s: %w", subject, err)
	}
	return nil
}

func (q *NATSQueue) Subscribe(ctx context.Context, subject string, handler func([]byte)) error {
	_, err := q.conn.Subscribe(subject, func(msg *nats.Msg) {
		handler(msg.Data)
	})
	if err != nil {
		return fmt.Errorf("NATSQueue.Subscribe %s: %w", subject, err)
	}
	return nil
}

func (q *NATSQueue) Close() error {
	q.conn.Close()
	return nil
}
