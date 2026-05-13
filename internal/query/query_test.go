package query

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseRangeDefaults(t *testing.T) {
	r, err := ParseRange("", "")
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().UTC(), r.End, 2*time.Second)
	require.Equal(t, time.Hour, r.End.Sub(r.Start))
}

func TestParseRangeExplicit(t *testing.T) {
	from := "2026-05-12T00:00:00Z"
	to := "2026-05-13T00:00:00Z"
	r, err := ParseRange(from, to)
	require.NoError(t, err)
	require.Equal(t, 24*time.Hour, r.End.Sub(r.Start))
}

func TestParseRangeInverted(t *testing.T) {
	_, err := ParseRange("2026-05-13T00:00:00Z", "2026-05-12T00:00:00Z")
	require.Error(t, err)
}
