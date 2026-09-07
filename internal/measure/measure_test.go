package measure

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/g4lb/autor3search-go/internal/bench"
)

// TestInterleaveSwapsOrderEachRound pins the ABBA ordering. Always running
// the baseline first would leave the candidate permanently in the later slot
// of every round, turning any within-round drift into a constant bias on the
// score rather than noise that averages out. Swapping means each side leads
// half the rounds.
func TestInterleaveSwapsOrderEachRound(t *testing.T) {
	var order []string
	base := func(ctx context.Context, r int) (*bench.Set, error) {
		order = append(order, "base")
		return mkSet(10), nil
	}
	cand := func(ctx context.Context, r int) (*bench.Set, error) {
		order = append(order, "cand")
		return mkSet(8), nil
	}

	b, c, err := Interleave(context.Background(), 4, false, base, cand)
	if err != nil {
		t.Fatalf("Interleave: %v", err)
	}
	want := []string{"base", "cand", "cand", "base", "base", "cand", "cand", "base"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}

	bv, _ := b.Values("BenchmarkX-8", bench.UnitTime)
	cv, _ := c.Values("BenchmarkX-8", bench.UnitTime)
	if len(bv) != 4 || len(cv) != 4 {
		t.Fatalf("collected %d base and %d cand observations, want 4 and 4", len(bv), len(cv))
	}
	// Each side leads exactly half the rounds — the property that cancels
	// the offset. Counting is what the assertion above is really for; state
	// it directly so a future reordering cannot satisfy the literal sequence
	// while breaking the balance.
	leads := map[string]int{}
	for i := 0; i < len(order); i += 2 {
		leads[order[i]]++
	}
	if leads["base"] != 2 || leads["cand"] != 2 {
		t.Errorf("leading rounds = %v, want each side leading 2 of 4", leads)
	}
}

// TestInterleaveAttributesResultsToTheRightSide is the assertion the order
// swap makes possible to get wrong: on a round the candidate leads, its
// result must still come back as the candidate's. Reversing the run order
// without reversing the results would silently swap the two sides on half
// the rounds and invert the score.
func TestInterleaveAttributesResultsToTheRightSide(t *testing.T) {
	const baseNs, candNs = 10, 8
	base := func(ctx context.Context, r int) (*bench.Set, error) { return mkSet(baseNs), nil }
	cand := func(ctx context.Context, r int) (*bench.Set, error) { return mkSet(candNs), nil }

	b, c, err := Interleave(context.Background(), 4, true, base, cand)
	if err != nil {
		t.Fatalf("Interleave: %v", err)
	}
	bv, _ := b.Values("BenchmarkX-8", bench.UnitTime)
	cv, _ := c.Values("BenchmarkX-8", bench.UnitTime)
	for i, v := range bv {
		if v != baseNs/1e9 {
			t.Errorf("baseline observation %d = %v, want the baseline's own value", i, v)
		}
	}
	for i, v := range cv {
		if v != candNs/1e9 {
			t.Errorf("candidate observation %d = %v, want the candidate's own value", i, v)
		}
	}
}

// TestInterleavePropagatesErrorFromEitherSlot checks the error message names
// the side that actually failed, on a round where the candidate runs first.
func TestInterleavePropagatesErrorFromEitherSlot(t *testing.T) {
	boom := errors.New("boom")
	base := func(ctx context.Context, r int) (*bench.Set, error) { return mkSet(1), nil }
	// Round 1 is candidate-first, so this fails in the leading slot.
	cand := func(ctx context.Context, r int) (*bench.Set, error) {
		if r == 1 {
			return nil, boom
		}
		return mkSet(1), nil
	}

	_, _, err := Interleave(context.Background(), 2, false, base, cand)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if !strings.Contains(err.Error(), "candidate round 1") {
		t.Errorf("err = %v, want it to name the candidate side", err)
	}
}

func TestInterleaveDiscardsWarmupRound(t *testing.T) {
	calls := 0
	base := func(ctx context.Context, r int) (*bench.Set, error) {
		calls++
		return mkSet(10), nil
	}
	cand := func(ctx context.Context, r int) (*bench.Set, error) {
		calls++
		return mkSet(9), nil
	}

	b, _, err := Interleave(context.Background(), 2, true, base, cand)
	if err != nil {
		t.Fatalf("Interleave: %v", err)
	}
	if calls != 6 {
		t.Errorf("calls = %d, want 6 (3 rounds x 2 sides, first discarded)", calls)
	}
	bv, _ := b.Values("BenchmarkX-8", bench.UnitTime)
	if len(bv) != 2 {
		t.Errorf("kept %d observations, want 2", len(bv))
	}
}

func TestInterleavePropagatesError(t *testing.T) {
	boom := errors.New("boom")
	base := func(ctx context.Context, r int) (*bench.Set, error) { return mkSet(1), nil }
	cand := func(ctx context.Context, r int) (*bench.Set, error) { return nil, boom }

	if _, _, err := Interleave(context.Background(), 2, false, base, cand); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}

func TestInterleaveRejectsTooFewRounds(t *testing.T) {
	f := func(ctx context.Context, r int) (*bench.Set, error) { return mkSet(1), nil }
	if _, _, err := Interleave(context.Background(), 1, false, f, f); err == nil {
		t.Fatal("Interleave(rounds=1) = nil error, want error")
	}
}

// mkSet builds a one-observation Set for BenchmarkX-8.
func mkSet(ns float64) *bench.Set {
	s, err := bench.Parse(strings.NewReader(
		fmt.Sprintf("BenchmarkX-8\t100\t%v ns/op\n", ns)))
	if err != nil {
		panic(err)
	}
	return s
}
