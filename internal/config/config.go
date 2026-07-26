// Package config reads the application's configuration from the environment.
package config

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/gbchd/fizzbuzz/internal/pkg/slogx"
)

// envPrefix namespaces our variables so a shared environment — a compose
// env_file, a dev shell — can't leak a generic LOG_LEVEL into this service.
const envPrefix = "FIZZBUZZ_"

// Config is the application's configuration.
type Config struct {
	Log  LogConfig  `envPrefix:"LOG_"`
	HTTP HTTPConfig `envPrefix:"HTTP_"`
}

// HTTPConfig holds only what an operator has to match to their platform; the
// connection timeouts are constants.
type HTTPConfig struct {
	Address string `env:"ADDRESS" envDefault:":8080"`

	// The per-client request allowance. Config rather than constants: what one
	// caller may fairly spend depends on how many are expected and on what the
	// deployment can serve, and neither is knowable here.
	RateLimitRequests int           `env:"RATE_LIMIT_REQUESTS" envDefault:"600"`
	RateLimitWindow   time.Duration `env:"RATE_LIMIT_WINDOW" envDefault:"1m"`
}

// LogConfig configures the logger. Both fields are typed rather than strings so
// an invalid value fails here, at startup, instead of at the first log call.
type LogConfig struct {
	Level  slog.Level   `env:"LEVEL" envDefault:"info"`
	Format slogx.Format `env:"FORMAT" envDefault:"json"`
}

// Load reads the configuration from the environment, defaulting any variable
// that is unset or empty.
func Load() (Config, error) {
	var cfg Config
	if err := env.ParseWithOptions(&cfg, env.Options{Prefix: envPrefix}); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}

	// Either value at zero rejects every request forever, which parses fine and
	// would only show up as a service that answers nothing.
	if cfg.HTTP.RateLimitRequests < 1 {
		return Config{}, fmt.Errorf("config: %sHTTP_RATE_LIMIT_REQUESTS must be at least 1, got %d", envPrefix, cfg.HTTP.RateLimitRequests)
	}
	if cfg.HTTP.RateLimitWindow <= 0 {
		return Config{}, fmt.Errorf("config: %sHTTP_RATE_LIMIT_WINDOW must be greater than 0, got %s", envPrefix, cfg.HTTP.RateLimitWindow)
	}

	return cfg, nil
}
