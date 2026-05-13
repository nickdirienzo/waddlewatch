package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("WADDLEWATCH_LISTEN", "")
	cfg, err := Load("")
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:8080", cfg.Listen)
	require.Equal(t, 60*time.Second, cfg.PollInterval)
}

func TestLoadYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := `
listen: 0.0.0.0:9000
storage_path: /tmp/parquet
catalog_path: /tmp/catalog.duckdb
data_path: /tmp/data
poll_interval: 5s
`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))

	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "0.0.0.0:9000", cfg.Listen)
	require.Equal(t, "/tmp/parquet", cfg.StoragePath)
	require.Equal(t, 5*time.Second, cfg.PollInterval)
	require.False(t, cfg.UsingS3())
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv("WADDLEWATCH_LISTEN", "0.0.0.0:7777")
	t.Setenv("WADDLEWATCH_POLL_INTERVAL", "10s")
	t.Setenv("WADDLEWATCH_S3_BUCKET", "my-bucket")
	cfg, err := Load("")
	require.NoError(t, err)
	require.Equal(t, "0.0.0.0:7777", cfg.Listen)
	require.Equal(t, 10*time.Second, cfg.PollInterval)
	require.True(t, cfg.UsingS3())
}

func TestValidateMissingCatalog(t *testing.T) {
	c := Default()
	c.CatalogPath = ""
	require.Error(t, c.Validate())
}
