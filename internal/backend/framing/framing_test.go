package framing

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadFrame(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		max     int
		want    string
		wantErr error
	}{
		{name: "short line", input: "hello\nrest", max: 64, want: "hello\n"},
		{name: "line exactly at the cap", input: "abcd\n", max: 5, want: "abcd\n"},
		{name: "line one byte over the cap", input: "abcde\n", max: 5, wantErr: ErrFrameTooLarge},
		{name: "no newline before EOF", input: "tail", max: 64, want: "tail", wantErr: io.EOF},
		{name: "never a newline, past the cap", input: strings.Repeat("x", 4096), max: 512, wantErr: ErrFrameTooLarge},
		{name: "empty input", input: "", max: 64, wantErr: io.EOF},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ReadFrame(bufio.NewReaderSize(strings.NewReader(tc.input), 16), tc.max)
			if tc.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			if tc.wantErr == nil || errors.Is(tc.wantErr, io.EOF) {
				require.Equal(t, tc.want, string(got))
			}
		})
	}
}

func TestReadFrameDoesNotBufferBeyondTheCap(t *testing.T) {
	t.Parallel()
	// 64 MiB with no newline: the cap must be hit without ever holding it all.
	_, err := ReadFrame(bufio.NewReaderSize(io.LimitReader(zeros{}, 64<<20), 4096), 1<<20)
	require.ErrorIs(t, err, ErrFrameTooLarge)
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func TestReadLineCapped(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		max   int
		want  string
	}{
		{name: "under the cap keeps the newline", input: "abc\n", max: 64, want: "abc\n"},
		{name: "over the cap keeps the head", input: "abcdefgh\nnext\n", max: 4, want: "abcd"},
		{name: "very long line does not grow", input: strings.Repeat("y", 100000) + "\n", max: 8, want: "yyyyyyyy"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			br := bufio.NewReaderSize(strings.NewReader(tc.input), 16)
			got, err := ReadLineCapped(br, tc.max)
			require.NoError(t, err)
			require.Equal(t, tc.want, string(got))
		})
	}
}

func TestReadLineCappedResyncsOnTheNextLine(t *testing.T) {
	t.Parallel()
	br := bufio.NewReaderSize(strings.NewReader(strings.Repeat("z", 5000)+"\nsecond\n"), 16)
	first, err := ReadLineCapped(br, 8)
	require.NoError(t, err)
	require.Equal(t, "zzzzzzzz", string(first))

	second, err := ReadLineCapped(br, 64)
	require.NoError(t, err)
	require.Equal(t, "second\n", string(second), "the discarded tail must not eat the next line")
}
