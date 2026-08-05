package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/Atrabilis/api-poller/internal/config"
	"github.com/Atrabilis/api-poller/internal/cursor"
	"github.com/Atrabilis/api-poller/internal/poller"
	"github.com/Atrabilis/api-poller/internal/storage"
)

func main() {
	var (
		configPath  = flag.String("config", "", "Path to YAML config")
		inputName   = flag.String("input", "", "Optional single input to run")
		prettyPrint = flag.Bool("pretty", false, "Pretty-print output JSON")
		writeOnly   = flag.Bool("write-only", false, "Write to storage without printing samples")
	)
	flag.Parse()

	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "-config is required")
		os.Exit(2)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	samples, cursorUpdates, err := poller.Run(context.Background(), cfg, *inputName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run poller: %v\n", err)
		os.Exit(1)
	}

	writers, err := storage.BuildWriters(storage.Config{Outputs: toStorageOutputs(cfg.Storage.Outputs)})
	if err != nil {
		fmt.Fprintf(os.Stderr, "init storage: %v\n", err)
		os.Exit(1)
	}
	defer storage.CloseAll(writers)

	if err := storage.WriteAll(context.Background(), writers, samples); err != nil {
		fmt.Fprintf(os.Stderr, "write storage: %v\n", err)
		os.Exit(1)
	}

	// Cursors only advance once the write above is confirmed, so a failed
	// write is retried on the next poll instead of silently skipped.
	for _, update := range cursorUpdates {
		if err := cursor.Write(update.File, update.Value); err != nil {
			fmt.Fprintf(os.Stderr, "write cursor for input %q: %v\n", update.InputName, err)
			os.Exit(1)
		}
	}

	if !*writeOnly {
		encoder := json.NewEncoder(os.Stdout)
		if *prettyPrint {
			encoder.SetIndent("", "  ")
		}
		for _, sample := range samples {
			if err := encoder.Encode(sample); err != nil {
				fmt.Fprintf(os.Stderr, "encode sample: %v\n", err)
				os.Exit(1)
			}
		}
	}
}

func toStorageOutputs(outputs []config.StorageOutput) []storage.OutputConfig {
	result := make([]storage.OutputConfig, 0, len(outputs))
	for _, output := range outputs {
		result = append(result, storage.OutputConfig{
			Name:    output.Name,
			Type:    output.Type,
			Enabled: output.Enabled,
			Timescaledb: storage.TimescaledbConfig{
				HostEnv:     output.Timescaledb.HostEnv,
				PortEnv:     output.Timescaledb.PortEnv,
				UserEnv:     output.Timescaledb.UserEnv,
				PasswordEnv: output.Timescaledb.PasswordEnv,
				DatabaseEnv: output.Timescaledb.DatabaseEnv,
				Schema:      output.Timescaledb.Schema,
				Table:       output.Timescaledb.Table,
				SSLMode:     output.Timescaledb.SSLMode,
			},
		})
	}
	return result
}
