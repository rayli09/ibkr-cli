package apierr

import (
	"errors"
	"fmt"
	"testing"
)

func TestExitCodeMapping(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{nil, 0},
		{&UsageError{Msg: "x"}, 2},
		{&SessionError{Msg: "x"}, 3},
		{&APIError{Msg: "x"}, 4},
		{&RateLimitError{Route: "validate"}, 4},
		{&QuestionPending{Questions: []Question{{ID: "1"}}}, 5},
		{errors.New("plain"), 1},
		{fmt.Errorf("wrap: %w", &SessionError{Msg: "x"}), 3},
		{fmt.Errorf("wrap: %w", &QuestionPending{}), 5},
	}
	for _, tc := range cases {
		if got := ExitCode(tc.err); got != tc.want {
			t.Errorf("ExitCode(%v) = %d, want %d", tc.err, got, tc.want)
		}
	}
}
