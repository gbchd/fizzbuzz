package statsredis

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/rueidis"

	"github.com/gbchd/fizzbuzz/internal/service/statistics"
)

var _ statistics.Repository = (*Repository)(nil)

// queriesKey is the single sorted set holding every Query's Hit count.
// There is deliberately no key-per-Query: one key means nothing to scan or expire.
const queriesKey = "fizzbuzz:statistics:queries"

// Repository is a statistics.Repository backed by a single Redis-protocol sorted set.
type Repository struct {
	client rueidis.Client
}

// New constructs a Repository around an already-connected client.
func New(client rueidis.Client) *Repository {
	return &Repository{client: client}
}

// Record atomically increments the Query's Hit count by 1.
func (r *Repository) Record(ctx context.Context, q statistics.Query) error {
	member, err := encodeQuery(q)
	if err != nil {
		return fmt.Errorf("statsredis: encode query: %w", err)
	}

	cmd := r.client.B().Zincrby().Key(queriesKey).Increment(1).Member(member).Build()
	if err := r.client.Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("statsredis: record: %w", err)
	}

	return nil
}

// Top reads the highest-scored member in one round trip.
// Ties are broken by the sorted set's own ordering rather than any bookkeeping of our own.
func (r *Repository) Top(ctx context.Context) (statistics.Query, int, error) {
	cmd := r.client.B().Zrevrange().Key(queriesKey).Start(0).Stop(0).Withscores().Build()

	scores, err := r.client.Do(ctx, cmd).AsZScores()
	if err != nil {
		return statistics.Query{}, 0, fmt.Errorf("statsredis: top: %w", err)
	}

	if len(scores) == 0 {
		return statistics.Query{}, 0, statistics.ErrNoHits
	}

	q, err := decodeQuery(scores[0].Member)
	if err != nil {
		return statistics.Query{}, 0, fmt.Errorf("statsredis: decode query: %w", err)
	}

	return q, int(scores[0].Score), nil
}

func encodeQuery(q statistics.Query) (string, error) {
	b, err := json.Marshal(q)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func decodeQuery(member string) (statistics.Query, error) {
	var q statistics.Query
	if err := json.Unmarshal([]byte(member), &q); err != nil {
		return statistics.Query{}, err
	}
	return q, nil
}
