package storage

import (
	"context"
	"fmt"

	"github.com/Atrabilis/api-poller/internal/poller"
	"github.com/Atrabilis/api-poller/internal/storage/timescale"
)

type Writer interface {
	Name() string
	Available() bool
	WriteSamples(ctx context.Context, samples []poller.Sample) error
	Close()
}

func BuildWriters(cfg Config) ([]Writer, error) {
	var writers []Writer
	for _, output := range cfg.Outputs {
		if !output.IsEnabled() {
			continue
		}

		switch output.Type {
		case "timescaledb":
			writer, err := timescale.NewWriter(output.Name, timescale.Config(output.Timescaledb))
			if err != nil {
				return nil, err
			}
			writers = append(writers, writer)
		default:
			return nil, fmt.Errorf("unsupported storage output type %q", output.Type)
		}
	}
	return writers, nil
}

func WriteAll(ctx context.Context, writers []Writer, samples []poller.Sample) error {
	for _, writer := range writers {
		if err := writer.WriteSamples(ctx, samples); err != nil {
			return fmt.Errorf("writer %q: %w", writer.Name(), err)
		}
	}
	return nil
}

func CloseAll(writers []Writer) {
	for _, writer := range writers {
		writer.Close()
	}
}
