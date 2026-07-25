package statsmemory_test

import (
	"sync"
	"testing"

	"github.com/gbchd/fizzbuzz/internal/repository/stats/statsmemory"
	"github.com/gbchd/fizzbuzz/internal/service/statistics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepository_Top(t *testing.T) {
	t.Parallel()

	fizz := statistics.Query{Int1: 3, Str1: "fizz", Int2: 5, Str2: "buzz", Limit: 15}
	buzz := statistics.Query{Int1: 5, Str1: "buzz", Int2: 3, Str2: "fizz", Limit: 15}

	tests := []struct {
		name      string
		queries   []statistics.Query
		wantQuery statistics.Query
		wantHits  int
		wantErr   error
	}{
		{
			name:    "empty repository reports no Hits recorded",
			wantErr: statistics.ErrNoHits,
		},
		{
			name:      "repeated identical queries accumulate on one Hit count",
			queries:   []statistics.Query{fizz, fizz, fizz},
			wantQuery: fizz,
			wantHits:  3,
		},
		{
			name:      "multiple distinct queries are tracked separately",
			queries:   []statistics.Query{fizz, buzz, buzz},
			wantQuery: buzz,
			wantHits:  2,
		},
		{
			name:      "a tie is broken by whichever query reached that count first",
			queries:   []statistics.Query{fizz, buzz, fizz, buzz},
			wantQuery: fizz,
			wantHits:  2,
		},
		{
			name:      "a later query overtaking the leader replaces it",
			queries:   []statistics.Query{fizz, buzz, buzz},
			wantQuery: buzz,
			wantHits:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := statsmemory.New()
			for _, q := range tt.queries {
				repo.Record(q)
			}

			gotQuery, gotHits, err := repo.Top()

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

func TestRepository_ConcurrentRecord(t *testing.T) {
	t.Parallel()

	q := statistics.Query{Int1: 3, Str1: "fizz", Int2: 5, Str2: "buzz", Limit: 15}

	tests := []struct {
		name       string
		goroutines int
	}{
		{name: "concurrent Record calls for the same Query all land", goroutines: 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := statsmemory.New()

			var wg sync.WaitGroup
			for range tt.goroutines {
				wg.Go(func() {
					repo.Record(q)
				})
			}
			wg.Wait()

			gotQuery, gotHits, err := repo.Top()

			require.NoError(t, err)
			assert.Equal(t, q, gotQuery)
			assert.Equal(t, tt.goroutines, gotHits)
		})
	}
}
