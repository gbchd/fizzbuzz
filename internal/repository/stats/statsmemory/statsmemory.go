package statsmemory

import (
	"sync"

	"github.com/gbchd/fizzbuzz/internal/service/statistics"
)

var _ statistics.Repository = (*Repository)(nil)

// Repository is a concurrency-safe, in-memory statistics.Repository. The
// zero value is not usable; construct one with New.
type Repository struct {
	mu   sync.Mutex
	hits map[statistics.Query]int

	// Keep a memory of the top query to avoid map range randomness.
	// Can only be updated by a Query with a strictly higher count than the current top.
	top     statistics.Query
	topHits int
	hasHits bool
}

// New constructs an empty Repository.
func New() *Repository {
	return &Repository{hits: make(map[statistics.Query]int)}
}

func (r *Repository) Record(q statistics.Query) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.hits[q]++
	count := r.hits[q]

	if !r.hasHits || count > r.topHits {
		r.top = q
		r.topHits = count
		r.hasHits = true
	}
}

func (r *Repository) Top() (statistics.Query, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.hasHits {
		return statistics.Query{}, 0, statistics.ErrNoHits
	}

	return r.top, r.topHits, nil
}
