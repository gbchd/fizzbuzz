package fizzbuzz_test

import (
	"testing"

	"github.com/gbchd/fizzbuzz/internal/service/fizzbuzz"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestService_FizzBuzz(t *testing.T) {
	t.Parallel()

	svc := fizzbuzz.NewService()

	tests := []struct {
		name    string
		int1    int
		str1    string
		int2    int
		str2    string
		limit   int
		want    []string
		wantErr error
	}{
		{
			name:  "matches the classic fizzbuzz sequence",
			int1:  3,
			str1:  "fizz",
			int2:  5,
			str2:  "buzz",
			limit: 15,
			want: []string{
				"1", "2", "fizz", "4", "buzz", "fizz", "7", "8", "fizz", "buzz",
				"11", "fizz", "13", "14", "fizzbuzz",
			},
		},
		{
			name:    "invalid divisor error",
			int1:    0,
			str1:    "fizz",
			int2:    5,
			str2:    "buzz",
			limit:   15,
			wantErr: fizzbuzz.ErrInvalidDivisor,
		},
		{
			name:    "invalid limit error",
			int1:    3,
			str1:    "fizz",
			int2:    5,
			str2:    "buzz",
			limit:   0,
			wantErr: fizzbuzz.ErrInvalidLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := svc.FizzBuzz(t.Context(), tt.int1, tt.str1, tt.int2, tt.str2, tt.limit)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
