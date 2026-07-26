package statsredis_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/redis/rueidis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/gbchd/fizzbuzz/internal/pkg/metrics"
	"github.com/gbchd/fizzbuzz/internal/repository/stats/statsredis"
	"github.com/gbchd/fizzbuzz/internal/service/statistics"
)

// queriesKey mirrors statsredis's unexported key so tests can inspect TTL without widening its exported surface.
const queriesKey = "fizzbuzz:statistics:queries"

// newRepository starts a real Dragonfly instance via testcontainers-go and returns a Repository wired to it.
// Skipped under -short, for contributors without a local Docker daemon.
func newRepository(t *testing.T) (*statsredis.Repository, rueidis.Client) {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping Dragonfly-backed test in -short mode (requires Docker)")
	}

	ctx := t.Context()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "ghcr.io/dragonflydb/dragonfly:latest",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForLog("listening on 0.0.0.0:6379"),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, container.Terminate(context.Background()))
	})

	endpoint, err := container.PortEndpoint(ctx, "6379/tcp", "")
	require.NoError(t, err)

	client, err := rueidis.NewClient(rueidis.ClientOption{InitAddress: []string{endpoint}, DisableCache: true})
	require.NoError(t, err)
	t.Cleanup(client.Close)

	return statsredis.New(client), client
}

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
			// str1/str2 that could collide under a naive delimited encoding stay distinct once JSON-encoded.
			name:      "queries that would collide under a delimited encoding stay distinct",
			queries:   []statistics.Query{{Int1: 1, Str1: "a,b", Int2: 2, Str2: "c", Limit: 10}, {Int1: 1, Str1: "a,b", Int2: 2, Str2: "c", Limit: 10}, {Int1: 1, Str1: "a", Int2: 2, Str2: "b,c", Limit: 10}},
			wantQuery: statistics.Query{Int1: 1, Str1: "a,b", Int2: 2, Str2: "c", Limit: 10},
			wantHits:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo, _ := newRepository(t)
			for _, q := range tt.queries {
				require.NoError(t, repo.Record(t.Context(), q))
			}

			gotQuery, gotHits, err := repo.Top(t.Context())

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

// TestRepository_TieBreak confirms that repeated calls to Top return the same winner when multiple queries are tied on Hit count.
func TestRepository_TieBreak(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		queries []statistics.Query
	}{
		{
			name: "two queries tied on Hit count resolve to one stable winner",
			queries: []statistics.Query{
				{Int1: 3, Str1: "fizz", Int2: 5, Str2: "buzz", Limit: 15},
				{Int1: 5, Str1: "buzz", Int2: 3, Str2: "fizz", Limit: 15},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo, _ := newRepository(t)

			for _, q := range tt.queries {
				require.NoError(t, repo.Record(t.Context(), q))
			}

			var want statistics.Query
			for i := range 3 {
				gotQuery, gotHits, err := repo.Top(t.Context())
				require.NoError(t, err)
				assert.Equal(t, 1, gotHits)
				if i == 0 {
					want = gotQuery
					continue
				}
				assert.Equal(t, want, gotQuery, "repeated Top calls must agree on the winner")
			}

			// Confirm the winner is really the lexicographically-native one ZREVRANGE hands back.
			encoded := make([]string, len(tt.queries))
			for i, q := range tt.queries {
				b, err := json.Marshal(q)
				require.NoError(t, err)
				encoded[i] = string(b)
			}
			winner := tt.queries[0]
			for i, q := range tt.queries {
				if encoded[i] > encoded[0] {
					winner = q
				}
			}
			assert.Equal(t, winner, want)
		})
	}
}

// TestRepository_ErrorMetrics confirms that failed calls to Record and Top are counted in the Prometheus metrics.
func TestRepository_ErrorMetrics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		operation string
		call      func(repo *statsredis.Repository) error
	}{
		{
			name:      "a failed Record is counted",
			operation: "record",
			call: func(repo *statsredis.Repository) error {
				return repo.Record(t.Context(), statistics.Query{Int1: 1, Str1: "a", Int2: 2, Str2: "b", Limit: 3})
			},
		},
		{
			name:      "a failed Top is counted",
			operation: "top",
			call: func(repo *statsredis.Repository) error {
				_, _, err := repo.Top(t.Context())
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo, client := newRepository(t)
			client.Close() // the connection is gone, so the next call must fail

			require.Error(t, tt.call(repo))

			out, err := metrics.Prometheus()
			require.NoError(t, err)
			assert.Contains(t, out, `statsredis_errors_total{operation="`+tt.operation+`"} 1`)
		})
	}
}

// TestRepository_NoExpiry confirms Record sets no TTL/expiry: statistics persist indefinitely by design.
func TestRepository_NoExpiry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query statistics.Query
	}{
		{
			name:  "a recorded Hit leaves the statistics key without a TTL",
			query: statistics.Query{Int1: 3, Str1: "fizz", Int2: 5, Str2: "buzz", Limit: 15},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo, client := newRepository(t)

			require.NoError(t, repo.Record(t.Context(), tt.query))

			pttl, err := client.Do(t.Context(), client.B().Pttl().Key(queriesKey).Build()).ToInt64()
			require.NoError(t, err)
			assert.Equal(t, int64(-1), pttl, "the statistics key must never expire")
		})
	}
}
