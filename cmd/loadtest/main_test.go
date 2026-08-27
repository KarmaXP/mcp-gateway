package main

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func newTally() *errorTally { return &errorTally{counts: map[string]uint64{}} }

func TestErrorTallyNamesTheCauseBehindTheCount(t *testing.T) {
	tally := newTally()
	for i := 0; i < 9; i++ {
		tally.record(errors.New("dial tcp 127.0.0.1:31400: connect: can't assign requested address"))
	}
	tally.record(errors.New("context deadline exceeded"))

	require.EqualValues(t, 10, tally.count())
	summary := tally.summary()
	require.Contains(t, summary, "can't assign requested address",
		"a bare error count cannot tell a broken gateway from a host out of sockets")
	require.Contains(t, summary, "9 of 10")
	require.Contains(t, summary, "2 distinct")
}

func TestErrorTallyStaysSilentWhenNothingFailed(t *testing.T) {
	tally := newTally()
	tally.record(nil)

	require.EqualValues(t, 0, tally.count())
	require.Empty(t, tally.summary())
}

func TestErrorTallyBreaksATieByNameNotByMapOrder(t *testing.T) {
	tally := newTally()
	for i := 0; i < 3; i++ {
		tally.record(errors.New("beta"))
		tally.record(errors.New("alpha"))
	}

	for attempt := 0; attempt < 64; attempt++ {
		require.Contains(t, tally.summary(), "alpha",
			"a tie resolved by map iteration order makes the reported cause change between runs")
	}
}

func TestErrorTallyTruncatesAnEndlessMessage(t *testing.T) {
	tally := newTally()
	tally.record(errors.New(strings.Repeat("x", errorSampleMaxRunes*3)))

	summary := tally.summary()
	require.Contains(t, summary, "...")
	require.Less(t, len([]rune(summary)), errorSampleMaxRunes*2)
}

func TestErrorTallyIsSafeUnderConcurrentWorkers(t *testing.T) {
	tally := newTally()
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				tally.record(errors.New("upstream refused"))
			}
		}()
	}
	wg.Wait()

	require.EqualValues(t, 800, tally.count())
	require.Contains(t, tally.summary(), "800 of 800")
}
