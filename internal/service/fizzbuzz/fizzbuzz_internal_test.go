package fizzbuzz

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func TestNewRule(t *testing.T) {
	tests := []struct {
		name        string
		divisor     int
		replacement string
		wantErr     error
	}{
		{name: "valid divisor", divisor: 3, replacement: "fizz"},
		{name: "empty replacement allowed", divisor: 3, replacement: ""},
		{name: "zero divisor", divisor: 0, replacement: "fizz", wantErr: ErrInvalidDivisor},
		{name: "negative divisor", divisor: -3, replacement: "fizz", wantErr: ErrInvalidDivisor},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newRule(tt.divisor, tt.replacement)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestGenerate(t *testing.T) {
	fizz := must(newRule(3, "fizz"))
	buzz := must(newRule(5, "buzz"))
	blank := must(newRule(7, ""))

	tests := []struct {
		name    string
		limit   int
		rules   []rule
		want    []string
		wantErr error
	}{
		{
			name:  "classic fizzbuzz sequence",
			limit: 15,
			rules: []rule{fizz, buzz},
			want: []string{
				"1", "2", "fizz", "4", "buzz", "fizz", "7", "8", "fizz", "buzz",
				"11", "fizz", "13", "14", "fizzbuzz",
			},
		},
		{
			name:  "concatenates matches in rule order",
			limit: 15,
			rules: []rule{buzz, fizz},
			want: []string{
				"1", "2", "fizz", "4", "buzz", "fizz", "7", "8", "fizz", "buzz",
				"11", "fizz", "13", "14", "buzzfizz",
			},
		},
		{
			name:  "no rules returns plain numbers",
			limit: 3,
			rules: nil,
			want:  []string{"1", "2", "3"},
		},
		{
			name:  "empty replacement is distinguishable from no match",
			limit: 7,
			rules: []rule{blank},
			want:  []string{"1", "2", "3", "4", "5", "6", ""},
		},
		{
			name:    "non-positive limit is invalid",
			limit:   0,
			rules:   nil,
			wantErr: ErrInvalidLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := generate(tt.limit, tt.rules)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
