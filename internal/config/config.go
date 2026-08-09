// Package config loads the LCS console's YAML configuration.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server  ServerConfig  `yaml:"server"`
	GMLC    GMLCConfig    `yaml:"gmlc"`
	Polling PollingConfig `yaml:"polling"`
	Log     LogConfig     `yaml:"log"`
}

// ServerConfig is where the LCS console's own web UI/API listens.
type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// GMLCConfig points at the GMLC's Le REST/JSON adapter and holds the
// client credentials used to authenticate against it. These never reach
// the browser — the console's backend is the only thing that talks to
// the GMLC directly.
type GMLCConfig struct {
	BaseURL        string        `yaml:"base_url"`
	ClientID       string        `yaml:"client_id"`
	BearerToken    string        `yaml:"bearer_token"`
	RequestTimeout time.Duration `yaml:"request_timeout"`
}

// PollingConfig controls how the backend polls the GMLC for a submitted
// request to reach a terminal state (the GMLC API has no push/streaming).
type PollingConfig struct {
	Interval time.Duration `yaml:"interval"`
	Timeout  time.Duration `yaml:"timeout"`
}

type LogConfig struct {
	Level string `yaml:"level"`
	File  string `yaml:"file"`
}

func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	cfg := &Config{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 8090,
		},
		GMLC: GMLCConfig{
			RequestTimeout: 10 * time.Second,
		},
		Polling: PollingConfig{
			Interval: 2 * time.Second,
			Timeout:  60 * time.Second,
		},
		Log: LogConfig{
			Level: "info",
		},
	}

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	if cfg.GMLC.BaseURL == "" {
		return nil, fmt.Errorf("gmlc.base_url is required")
	}
	if cfg.GMLC.ClientID == "" {
		return nil, fmt.Errorf("gmlc.client_id is required")
	}
	if cfg.GMLC.BearerToken == "" {
		return nil, fmt.Errorf("gmlc.bearer_token is required")
	}
	if cfg.GMLC.RequestTimeout <= 0 {
		cfg.GMLC.RequestTimeout = 10 * time.Second
	}
	if cfg.Polling.Interval <= 0 {
		cfg.Polling.Interval = 2 * time.Second
	}
	if cfg.Polling.Timeout <= 0 {
		cfg.Polling.Timeout = 60 * time.Second
	}
	if cfg.Server.Port <= 0 {
		cfg.Server.Port = 8090
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}

	return cfg, nil
}

func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}
