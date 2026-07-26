package statistics_test

import (
	"context"
	"testing"

	"github.com/gbchd/fizzbuzz/internal/service/statistics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubService struct {
	wantSeq []string
	wantErr error
}

func (s *stubService) FizzBuzz(_ context.Context, _ int, _ string, _ int, _ string, _ int) ([]string, error) {
	return s.wantSeq, s.wantErr
}

// spyRepo lets a test assert whether Record ran, control what Top returns, and
// make Record fail.
type spyRepo struct {
	recorded  []statistics.Query
	recordErr error

	topQuery statistics.Query
	topHits  int
	topErr   error
}

func (r *spyRepo) Record(_ context.Context, q statistics.Query) error {
	r.recorded = append(r.recorded, q)

	return r.recordErr
}

func (r *spyRepo) Top(_ context.Context) (statistics.Query, int, error) {
	return r.topQuery, r.topHits, r.topErr
}

func TestRecorder_FizzBuzz(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		inner      *stubService
		wantResult []string
		wantErr    error
		wantRecord *statistics.Query
	}{
		{
			name:       "success records a Hit and returns the sequence unchanged",
			inner:      &stubService{wantSeq: []string{"1", "2", "fizz"}},
			wantResult: []string{"1", "2", "fizz"},
			wantRecord: &statistics.Query{Int1: 3, Str1: "fizz", Int2: 5, Str2: "buzz", Limit: 3},
		},
		{
			name:    "error is passed through without recording a Hit",
			inner:   &stubService{wantErr: assert.AnError},
			wantErr: assert.AnError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &spyRepo{}
			rec := statistics.NewRecorder(tt.inner, repo)

			got, err := rec.FizzBuzz(t.Context(), 3, "fizz", 5, "buzz", 3)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Empty(t, repo.recorded)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantResult, got)
			require.Len(t, repo.recorded, 1)
			assert.Equal(t, *tt.wantRecord, repo.recorded[0])
		})
	}
}

func TestRecorder_Top(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		repo      *spyRepo
		wantQuery statistics.Query
		wantHits  int
		wantErr   error
	}{
		{
			name: "delegates to the wrapped Repository",
			repo: &spyRepo{
				topQuery: statistics.Query{Int1: 3, Str1: "fizz", Int2: 5, Str2: "buzz", Limit: 15},
				topHits:  4,
			},
			wantQuery: statistics.Query{Int1: 3, Str1: "fizz", Int2: 5, Str2: "buzz", Limit: 15},
			wantHits:  4,
		},
		{
			name:    "passes through the Repository's error",
			repo:    &spyRepo{topErr: statistics.ErrNoHits},
			wantErr: statistics.ErrNoHits,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := statistics.NewRecorder(&stubService{}, tt.repo)

			gotQuery, gotHits, err := rec.Top(t.Context())

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantQuery, gotQuery)
			assert.Equal(t, tt.wantHits, gotHits)
		})
	}
}

func TestRecorder_FizzBuzzSwallowsARecordFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		recordErr error
	}{
		{name: "a failing Record still returns the Sequence", recordErr: assert.AnError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &spyRepo{recordErr: tt.recordErr}
			rec := statistics.NewRecorder(&stubService{wantSeq: []string{"1", "2", "fizz"}}, repo)

			got, err := rec.FizzBuzz(t.Context(), 3, "fizz", 5, "buzz", 3)

			require.NoError(t, err, "the failure must not reach the caller")
			assert.Equal(t, []string{"1", "2", "fizz"}, got)
		})
	}
}
