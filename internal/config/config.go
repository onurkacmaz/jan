package config

import (
	"os"

	"github.com/ztrue/tracerr"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Name          string            `yaml:"name"`
	Command       string            `yaml:"command"`
	Args          []string          `yaml:"args"`
	Workdir       string            `yaml:"workdir"`
	Env           map[string]string `yaml:"env"`
	RestartPolicy string            `yaml:"restart_policy"`
	MaxRetries    int               `yaml:"max_retries"`
	BackoffMS     int               `yaml:"backoff_ms"`
}

const (
	RestartAlways    = "always"
	RestartNever     = "never"
	RestartOnFailure = "on-failure"
)

func Load(path string) (*Config, error) {
	fi, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	config, err := Parse(fi)
	if err != nil {
		return nil, tracerr.Wrap(err)
	}

	return config, nil
}

func Parse(raw []byte) (*Config, error) {
	var config Config
	if err := yaml.Unmarshal(raw, &config); err != nil {
		return nil, tracerr.Wrap(err)
	}

	if err := Validate(&config); err != nil {
		return nil, tracerr.Wrap(err)
	}

	return &config, nil
}

func Validate(config *Config) error {
	if config.Name == "" {
		return tracerr.New("name is required")
	}
	if config.Command == "" {
		return tracerr.New("command is required")
	}
	if config.RestartPolicy == "" {
		return tracerr.New("restart_policy is required")
	}
	if config.MaxRetries < 0 {
		return tracerr.New("max_retries must be non-negative")
	}
	if config.BackoffMS < 0 {
		return tracerr.New("backoff_ms must be non-negative")
	}

	switch config.RestartPolicy {
	case RestartAlways:
	case RestartNever:
	case RestartOnFailure:
	default:
		return tracerr.New("invalid restart_policy")
	}

	return nil
}
