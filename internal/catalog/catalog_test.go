package catalog

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSignalForFile(t *testing.T) {
	cases := []struct {
		path string
		want Signal
		ok   bool
	}{
		{"/var/wdw/logs/2026-05-13/file.parquet", SignalLogs, true},
		{"/var/wdw/metrics/file.parquet", SignalMetrics, true},
		{"/var/wdw/traces/file.parquet", SignalTraces, true},
		{"/tmp/random.parquet", "", false},
	}
	for _, c := range cases {
		got, ok := SignalForFile(c.path)
		require.Equal(t, c.ok, ok, c.path)
		require.Equal(t, c.want, got, c.path)
	}
}
