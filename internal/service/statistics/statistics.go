package statistics

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/gbchd/fizzbuzz/internal/pkg/slogx"
)

var ErrNoHits = errors.New("no hits recorded yet")

// Query identifies a class of /fizzbuzz calls, distinct from any one HTTP
// request that happens to carry it.
type Query struct {
	Int1  int
	Str1  string
	Int2  int
	Str2  string
	Limit int
}

// Repository records Hits against a Query and reports the most frequent one.
type Repository interface {
	Record(ctx context.Context, q Query) error
	Top(ctx context.Context) (q Query, hits int, err error)
}

type FizzBuzzService interface {
	FizzBuzz(ctx context.Context, int1 int, str1 string, int2 int, str2 string, limit int) ([]string, error)
}

type Service struct {
	inner FizzBuzzService
	repo  Repository
}

func NewRecorder(inner FizzBuzzService, repo Repository) *Service {
	return &Service{inner: inner, repo: repo}
}

// FizzBuzz records a Hit only on success to avoid polluting the statistics with invalid queries.
func (r *Service) FizzBuzz(ctx context.Context, int1 int, str1 string, int2 int, str2 string, limit int) ([]string, error) {
	slogx.FromCtx(ctx).DebugContext(ctx, "statistics.Service.FizzBuzz",
		slog.Int("int1", int1),
		slog.String("str1", str1),
		slog.Int("int2", int2),
		slog.String("str2", str2),
		slog.Int("limit", limit),
	)

	sequence, err := r.inner.FizzBuzz(ctx, int1, str1, int2, str2, limit)
	if err != nil {
		return nil, fmt.Errorf("statistics: %w", err)
	}

	if err := r.repo.Record(ctx, Query{Int1: int1, Str1: str1, Int2: int2, Str2: str2, Limit: limit}); err != nil {
		slogx.FromCtx(ctx).ErrorContext(ctx, "record hit failed", slog.Any("error", err))
	}

	return sequence, nil
}

func (r *Service) Top(ctx context.Context) (Query, int, error) {
	slogx.FromCtx(ctx).DebugContext(ctx, "statistics.Service.Top")

	query, hits, err := r.repo.Top(ctx)
	if err != nil {
		return Query{}, 0, fmt.Errorf("statistics: %w", err)
	}

	return query, hits, nil
}
