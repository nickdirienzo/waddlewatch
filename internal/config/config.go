// Package config loads Waddlewatch's YAML config with env var overrides.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level Waddlewatch configuration.
type Config struct {
	Listen       string        `yaml:"listen"`
	StoragePath  string        `yaml:"storage_path"`
	CatalogPath  string        `yaml:"catalog_path"`
	DataPath     string        `yaml:"data_path"`
	PollInterval time.Duration `yaml:"poll_interval"`
	S3           S3Config      `yaml:"s3"`
}

// S3Config holds optional S3 settings. When Bucket is empty, local storage is used.
type S3Config struct {
	Bucket   string `yaml:"bucket"`
	Region   string `yaml:"region"`
	Endpoint string `yaml:"endpoint"`
	Prefix   string `yaml:"prefix"`
}

// UsingS3 reports whether S3 storage is configured.
func (c Config) UsingS3() bool {
	return c.S3.Bucket != ""
}

// Default returns a config with sensible defaults.
func Default() Config {
	return Config{
		Listen:       "127.0.0.1:8080",
		StoragePath:  "/var/lib/waddlewatch/parquet",
		CatalogPath:  "/var/lib/waddlewatch/catalog.sqlite",
		DataPath:     "/var/lib/waddlewatch/data",
		PollInterval: 60 * time.Second,
	}
}

// Load reads YAML from path and applies env overrides. An empty path returns defaults
// with env overrides applied.
func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("reading config %s: %w", path, err)
		}
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("parsing config %s: %w", path, err)
		}
	}
	applyEnv(&cfg)
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyEnv(c *Config) {
	if v := os.Getenv("WADDLEWATCH_LISTEN"); v != "" {
		c.Listen = v
	}
	if v := os.Getenv("WADDLEWATCH_STORAGE_PATH"); v != "" {
		c.StoragePath = v
	}
	if v := os.Getenv("WADDLEWATCH_CATALOG_PATH"); v != "" {
		c.CatalogPath = v
	}
	if v := os.Getenv("WADDLEWATCH_DATA_PATH"); v != "" {
		c.DataPath = v
	}
	if v := os.Getenv("WADDLEWATCH_POLL_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.PollInterval = d
		} else if secs, err := strconv.Atoi(v); err == nil {
			c.PollInterval = time.Duration(secs) * time.Second
		}
	}
	if v := os.Getenv("WADDLEWATCH_S3_BUCKET"); v != "" {
		c.S3.Bucket = v
	}
	if v := os.Getenv("WADDLEWATCH_S3_REGION"); v != "" {
		c.S3.Region = v
	}
	if v := os.Getenv("WADDLEWATCH_S3_ENDPOINT"); v != "" {
		c.S3.Endpoint = v
	}
	if v := os.Getenv("WADDLEWATCH_S3_PREFIX"); v != "" {
		c.S3.Prefix = v
	}
}

// Validate checks required fields and normalizes paths.
func (c *Config) Validate() error {
	if c.Listen == "" {
		return fmt.Errorf("listen address is required")
	}
	if c.CatalogPath == "" {
		return fmt.Errorf("catalog_path is required")
	}
	if c.PollInterval <= 0 {
		return fmt.Errorf("poll_interval must be positive")
	}
	if !c.UsingS3() && c.StoragePath == "" {
		return fmt.Errorf("storage_path is required when s3.bucket is not set")
	}
	if c.DataPath == "" {
		c.DataPath = filepath.Join(filepath.Dir(c.CatalogPath), "data")
	}
	return nil
}
