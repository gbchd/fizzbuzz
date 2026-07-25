package slogx_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gbchd/fizzbuzz/internal/pkg/slogx"
)

type levelCheck struct {
	level slog.Level
	want  bool
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		level   slog.Level
		format  string
		wantErr bool
		checks  []levelCheck
	}{
		{
			name:   "json format",
			level:  slog.LevelInfo,
			format: "json",
		},
		{
			name:   "text format",
			level:  slog.LevelInfo,
			format: "text",
		},
		{
			name:    "invalid format",
			level:   slog.LevelInfo,
			format:  "yaml",
			wantErr: true,
		},
		{
			name:   "respects configured level",
			level:  slog.LevelWarn,
			format: "json",
			checks: []levelCheck{
				{level: slog.LevelInfo, want: false},
				{level: slog.LevelWarn, want: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := slogx.New(tt.level, tt.format)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, logger)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, logger)

			for _, c := range tt.checks {
				assert.Equal(t, c.want, logger.Enabled(t.Context(), c.level))
			}
		})
	}
}

func TestFromCtx(t *testing.T) {
	customLogger, err := slogx.New(slog.LevelDebug, "text")
	require.NoError(t, err)

	tests := []struct {
		name        string
		ctx         context.Context
		wantDiscard bool
		want        *slog.Logger
	}{
		{
			name:        "no logger set",
			ctx:         context.Background(),
			wantDiscard: true,
		},
		{
			name: "logger set via WithCtx",
			ctx:  slogx.WithCtx(context.Background(), customLogger),
			want: customLogger,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slogx.FromCtx(tt.ctx)
			require.NotNil(t, got)

			if tt.wantDiscard {
				assert.False(t, got.Enabled(context.Background(), slog.LevelError))
				return
			}

			assert.Same(t, tt.want, got)
		})
	}
}

func TestWithCtx(t *testing.T) {
	type otherKey struct{}

	tests := []struct {
		name   string
		parent context.Context
	}{
		{
			name:   "background parent",
			parent: context.Background(),
		},
		{
			name:   "parent with an existing value",
			parent: context.WithValue(context.Background(), otherKey{}, "value"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := slogx.New(slog.LevelInfo, "json")
			require.NoError(t, err)

			child := slogx.WithCtx(tt.parent, logger)
			assert.NotEqual(t, tt.parent, child)

			assert.Same(t, logger, slogx.FromCtx(child))
			assert.NotSame(t, logger, slogx.FromCtx(tt.parent))
		})
	}
}
