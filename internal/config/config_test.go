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
	levelVar   = "FIZZBUZZ_LOG_LEVEL"
	formatVar  = "FIZZBUZZ_LOG_FORMAT"
	addressVar = "FIZZBUZZ_HTTP_ADDRESS"
)

// defaults lets each case below state only what it changes.
var defaults = config.Config{
	Log: config.LogConfig{
		Level:  slog.LevelInfo,
		Format: slogx.FormatJSON,
	},
	HTTP: config.HTTPConfig{
		Address: ":8080",
	},
}

// setEnv makes the environment match env exactly, clearing anything inherited
// from the ambient environment so the defaults case really tests defaults.
func setEnv(t *testing.T, env map[string]string) {
	t.Helper()

	for _, key := range []string{levelVar, formatVar, addressVar} {
		value, ok := env[key]
		t.Setenv(key, value) // registers restoration of the original value
		if !ok {
			require.NoError(t, os.Unsetenv(key))
		}
	}
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		// nil means the case should load the defaults unchanged.
		override func(cfg *config.Config)
		wantErr  bool
	}{
		{
			name: "defaults when unset",
		},
		{
			name: "explicit values",
			env: map[string]string{
				levelVar:   "debug",
				formatVar:  "text",
				addressVar: "127.0.0.1:9090",
			},
			override: func(cfg *config.Config) {
				cfg.Log.Level = slog.LevelDebug
				cfg.Log.Format = slogx.FormatText
				cfg.HTTP.Address = "127.0.0.1:9090"
			},
		},
		{
			name:     "level is case insensitive",
			env:      map[string]string{levelVar: "WARN"},
			override: func(cfg *config.Config) { cfg.Log.Level = slog.LevelWarn },
		},
		{
			name: "empty values fall back to the defaults",
			env: map[string]string{
				formatVar:  "",
				addressVar: "",
			},
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

			want := defaults
			if tt.override != nil {
				tt.override(&want)
			}

			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}
