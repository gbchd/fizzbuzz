package statistics

import (
	"errors"
	"fmt"
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
	Record(q Query)
	Top() (q Query, hits int, err error)
}

type FizzBuzzService interface {
	FizzBuzz(int1 int, str1 string, int2 int, str2 string, limit int) ([]string, error)
}

type Service struct {
	inner FizzBuzzService
	repo  Repository
}

func NewRecorder(inner FizzBuzzService, repo Repository) *Service {
	return &Service{inner: inner, repo: repo}
}

// FizzBuzz records a Hit only on success to avoid polluting the statistics with invalid queries.
func (r *Service) FizzBuzz(int1 int, str1 string, int2 int, str2 string, limit int) ([]string, error) {
	sequence, err := r.inner.FizzBuzz(int1, str1, int2, str2, limit)
	if err != nil {
		return nil, fmt.Errorf("statistics: %w", err)
	}

	r.repo.Record(Query{Int1: int1, Str1: str1, Int2: int2, Str2: str2, Limit: limit})

	return sequence, nil
}

func (r *Service) Top() (Query, int, error) {
	query, hits, err := r.repo.Top()
	if err != nil {
		return Query{}, 0, fmt.Errorf("statistics: %w", err)
	}

	return query, hits, nil
}
