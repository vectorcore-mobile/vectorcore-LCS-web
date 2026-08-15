// Package config loads the LCS console's YAML configuration.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server ServerConfig `yaml:"server"`
	// GMLCClients is a list of named GMLC client profiles, each pointing
	// at its own base_url/credentials. There must be exactly one profile
	// named "default" — the one internal/api's existing routes bind to.
	// Additional profiles (e.g. "emergency") are for future routes to
	// bind to on their own — a genuinely separate GMLC client identity,
	// not a UI toggle on the default one (LCS-Client-Type is resolved
	// server-side, per-credential, on the GMLC — see
	// docs/mlp-le-interface-plan.md's L3).
	GMLCClients []GMLCClient  `yaml:"gmlc_clients"`
	Polling     PollingConfig `yaml:"polling"`
	Log         LogConfig     `yaml:"log"`
}

// ServerConfig is where the LCS console's own web UI/API listens.
type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// GMLCClient is one named profile pointing at a GMLC Le adapter and the
// credentials used to authenticate against it. These never reach the
// browser — the console's backend is the only thing that talks to the
// GMLC directly.
type GMLCClient struct {
	Name           string        `yaml:"name"`
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

	if err := cfg.validateAndApplyClientDefaults(); err != nil {
		return nil, err
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

// DefaultGMLCClient returns the profile named "default" — validated by
// Load to always exist — the profile internal/api's existing routes bind
// to.
func (c *Config) DefaultGMLCClient() GMLCClient {
	for _, p := range c.GMLCClients {
		if p.Name == "default" {
			return p
		}
	}
	panic("config: no default gmlc_clients profile (Load should have rejected this)")
}

func (c *Config) validateAndApplyClientDefaults() error {
	if len(c.GMLCClients) == 0 {
		return fmt.Errorf("gmlc_clients: at least one profile is required (a \"default\" one)")
	}
	seen := map[string]bool{}
	hasDefault := false
	for i := range c.GMLCClients {
		p := &c.GMLCClients[i]
		if p.Name == "" {
			return fmt.Errorf("gmlc_clients[%d]: name is required", i)
		}
		if seen[p.Name] {
			return fmt.Errorf("gmlc_clients: duplicate profile name %q", p.Name)
		}
		seen[p.Name] = true
		if p.Name == "default" {
			hasDefault = true
		}
		if p.BaseURL == "" {
			return fmt.Errorf("gmlc_clients[%s]: base_url is required", p.Name)
		}
		if p.ClientID == "" {
			return fmt.Errorf("gmlc_clients[%s]: client_id is required", p.Name)
		}
		if p.BearerToken == "" {
			return fmt.Errorf("gmlc_clients[%s]: bearer_token is required", p.Name)
		}
		if p.RequestTimeout <= 0 {
			p.RequestTimeout = 10 * time.Second
		}
	}
	if !hasDefault {
		return fmt.Errorf("gmlc_clients: a profile named \"default\" is required")
	}
	return nil
}

func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}
