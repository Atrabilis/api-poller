package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Inputs  []Input       `yaml:"inputs"`
	Storage StorageConfig `yaml:"storage"`
}

type Input struct {
	Name     string   `yaml:"name"`
	Request  Request  `yaml:"request"`
	Response Response `yaml:"response"`
	Metrics  []Metric `yaml:"metrics"`
}

type Request struct {
	Method  string            `yaml:"method"`
	URL     string            `yaml:"url"`
	Timeout time.Duration     `yaml:"timeout"`
	Headers map[string]string `yaml:"headers"`
	Body    string            `yaml:"body"`
}

type Response struct {
	Format    string `yaml:"format"`
	ItemsPath string `yaml:"items_path"`
}

type Metric struct {
	Name      string            `yaml:"name"`
	ValuePath string            `yaml:"value_path"`
	Where     []Condition       `yaml:"where"`
	Transform Transform         `yaml:"transform"`
	Labels    map[string]string `yaml:"labels"`
}

type Condition struct {
	Path   string `yaml:"path"`
	Equals any    `yaml:"equals"`
}

type Transform struct {
	Scale  float64 `yaml:"scale"`
	Offset float64 `yaml:"offset"`
}

type StorageConfig struct {
	Outputs []StorageOutput `yaml:"outputs"`
}

type StorageOutput struct {
	Name        string                   `yaml:"name"`
	Type        string                   `yaml:"type"`
	Enabled     *bool                    `yaml:"enabled,omitempty"`
	Timescaledb StorageTimescaledbConfig `yaml:"timescaledb,omitempty"`
}

type StorageTimescaledbConfig struct {
	HostEnv     string `yaml:"host_env"`
	PortEnv     string `yaml:"port_env"`
	UserEnv     string `yaml:"user_env"`
	PasswordEnv string `yaml:"password_env"`
	DatabaseEnv string `yaml:"database_env"`
	Schema      string `yaml:"schema"`
	Table       string `yaml:"table"`
	SSLMode     string `yaml:"sslmode"`
}

func Load(path string) (Config, error) {
	var cfg Config

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}

	applyDefaults(&cfg)

	if err := validate(cfg); err != nil {
		return cfg, err
	}

	return cfg, nil
}

func applyDefaults(cfg *Config) {
	for i := range cfg.Inputs {
		input := &cfg.Inputs[i]
		if input.Request.Method == "" {
			input.Request.Method = "GET"
		}
		if input.Request.Timeout == 0 {
			input.Request.Timeout = 5 * time.Second
		}
		if strings.TrimSpace(input.Response.Format) == "" {
			input.Response.Format = "json"
		}
		for j := range input.Metrics {
			if input.Metrics[j].Transform.Scale == 0 {
				input.Metrics[j].Transform.Scale = 1
			}
		}
	}
}

func validate(cfg Config) error {
	if len(cfg.Inputs) == 0 {
		return fmt.Errorf("config must define at least one input")
	}

	for _, input := range cfg.Inputs {
		if input.Name == "" {
			return fmt.Errorf("input name is required")
		}
		if input.Request.URL == "" {
			return fmt.Errorf("input %q is missing request.url", input.Name)
		}
		switch input.Response.Format {
		case "", "json", "xml":
		default:
			return fmt.Errorf("input %q has unsupported response.format %q", input.Name, input.Response.Format)
		}
		if len(input.Metrics) == 0 {
			return fmt.Errorf("input %q must define at least one metric", input.Name)
		}
		for _, metric := range input.Metrics {
			if metric.Name == "" {
				return fmt.Errorf("input %q has a metric without name", input.Name)
			}
			if metric.ValuePath == "" {
				return fmt.Errorf("input %q metric %q is missing value_path", input.Name, metric.Name)
			}
		}
	}

	for _, output := range cfg.Storage.Outputs {
		if output.Name == "" {
			return fmt.Errorf("storage output name is required")
		}
		if output.Type == "" {
			return fmt.Errorf("storage output %q must define type", output.Name)
		}
		if output.Type != "timescaledb" {
			return fmt.Errorf("storage output %q has unsupported type %q", output.Name, output.Type)
		}
		if output.Timescaledb.HostEnv == "" {
			return fmt.Errorf("storage output %q timescaledb requires host_env", output.Name)
		}
		if output.Timescaledb.PortEnv == "" {
			return fmt.Errorf("storage output %q timescaledb requires port_env", output.Name)
		}
		if output.Timescaledb.UserEnv == "" {
			return fmt.Errorf("storage output %q timescaledb requires user_env", output.Name)
		}
		if output.Timescaledb.PasswordEnv == "" {
			return fmt.Errorf("storage output %q timescaledb requires password_env", output.Name)
		}
		if output.Timescaledb.DatabaseEnv == "" {
			return fmt.Errorf("storage output %q timescaledb requires database_env", output.Name)
		}
		if output.Timescaledb.Schema == "" {
			return fmt.Errorf("storage output %q timescaledb requires schema", output.Name)
		}
		if output.Timescaledb.Table == "" {
			return fmt.Errorf("storage output %q timescaledb requires table", output.Name)
		}
	}

	return nil
}
