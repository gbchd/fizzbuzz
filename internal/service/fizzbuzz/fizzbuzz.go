package fizzbuzz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/gbchd/fizzbuzz/internal/pkg/slogx"
)

var (
	// ErrInvalidDivisor rejects non-positive divisors because zero is undefined modulo,
	// negative has no meaning here.
	ErrInvalidDivisor = errors.New("divisor must be positive")
	// ErrInvalidLimit rejects non-positive limits.
	ErrInvalidLimit = errors.New("limit must be positive")
)

// Service exposes FizzBuzz using the README's parameter shape, kept separate
// from generate's rule-based internals.
type Service struct{}

// NewService constructs a Service.
func NewService() *Service {
	return &Service{}
}

// FizzBuzz adapts the README's two-rule parameters into rules for generate.
func (s *Service) FizzBuzz(ctx context.Context, int1 int, str1 string, int2 int, str2 string, limit int) ([]string, error) {
	slogx.FromCtx(ctx).DebugContext(ctx, "fizzbuzz.Service.FizzBuzz",
		slog.Int("int1", int1),
		slog.String("str1", str1),
		slog.Int("int2", int2),
		slog.String("str2", str2),
		slog.Int("limit", limit),
	)

	rule1, err := newRule(int1, str1)
	if err != nil {
		return nil, fmt.Errorf("fizzbuzz: rule 1: %w", err)
	}
	rule2, err := newRule(int2, str2)
	if err != nil {
		return nil, fmt.Errorf("fizzbuzz: rule 2: %w", err)
	}

	result, err := generate(limit, []rule{rule1, rule2})
	if err != nil {
		return nil, fmt.Errorf("fizzbuzz: %w", err)
	}

	return result, nil
}

// rule's fields are unexported and buildable only via newRule, so an invalid
// rule can never exist downstream and never needs re-checking.
type rule struct {
	divisor     int
	replacement string
}

// newRule validates the divisor once, at construction, instead of on every use.
func newRule(divisor int, replacement string) (rule, error) {
	if divisor <= 0 {
		return rule{}, ErrInvalidDivisor
	}
	return rule{divisor: divisor, replacement: replacement}, nil
}

// generate walks rules in caller order, so which replacement comes first on a
// multi-match is the caller's choice, not an implicit sort.
func generate(limit int, rules []rule) ([]string, error) {
	if limit <= 0 {
		return nil, ErrInvalidLimit
	}

	sequence := make([]string, 0, limit)
	for n := 1; n <= limit; n++ {
		var sb strings.Builder
		matched := false // separate from text: an empty replacement must still count as a match

		for _, r := range rules {
			if n%r.divisor == 0 {
				sb.WriteString(r.replacement)
				matched = true
			}
		}
		if !matched {
			sb.WriteString(strconv.Itoa(n))
		}

		sequence = append(sequence, sb.String())
	}

	return sequence, nil
}
