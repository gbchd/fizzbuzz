package config_test

import (
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gbchd/fizzbuzz/internal/config"
	"github.com/gbchd/fizzbuzz/internal/pkg/slogx"
)

const (
	levelVar  = "FIZZBUZZ_LOG_LEVEL"
	formatVar = "FIZZBUZZ_LOG_FORMAT"
)

// setEnv makes the environment match env exactly, clearing anything inherited
// from the ambient environment so the defaults case really tests defaults.
func setEnv(t *testing.T, env map[string]string) {
	t.Helper()

	for _, key := range []string{levelVar, formatVar} {
		value, ok := env[key]
		t.Setenv(key, value) // registers restoration of the original value
		if !ok {
			require.NoError(t, os.Unsetenv(key))
		}
	}
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    config.Config
		wantErr bool
	}{
		{
			name: "defaults when unset",
			want: config.Config{Log: config.LogConfig{
				Level:  slog.LevelInfo,
				Format: slogx.FormatJSON,
			}},
		},
		{
			name: "explicit values",
			env:  map[string]string{levelVar: "debug", formatVar: "text"},
			want: config.Config{Log: config.LogConfig{
				Level:  slog.LevelDebug,
				Format: slogx.FormatText,
			}},
		},
		{
			name: "level is case insensitive",
			env:  map[string]string{levelVar: "WARN"},
			want: config.Config{Log: config.LogConfig{
				Level:  slog.LevelWarn,
				Format: slogx.FormatJSON,
			}},
		},
		{
			name: "empty value falls back to the default",
			env:  map[string]string{formatVar: ""},
			want: config.Config{Log: config.LogConfig{
				Level:  slog.LevelInfo,
				Format: slogx.FormatJSON,
			}},
		},
		{
			name:    "invalid level",
			env:     map[string]string{levelVar: "verbose"},
			wantErr: true,
		},
		{
			name:    "invalid format",
			env:     map[string]string{formatVar: "yaml"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, tt.env)

			got, err := config.Load()

			if tt.wantErr {
				require.Error(t, err)
				assert.Empty(t, got)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
