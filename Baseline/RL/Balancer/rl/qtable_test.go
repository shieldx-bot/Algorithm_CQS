package rl

import "testing"

func TestStateIDFromTotalQueue(t *testing.T) {
	maxQ := 1000
	cases := []struct {
		q    int
		want int
	}{
		{0, 0},
		{50, 0},
		{120, 1},
		{400, 2},
		{999, 3},
		{1500, 3},
	}

	for _, tc := range cases {
		got := StateIDFromTotalQueue(tc.q, maxQ)
		if got != tc.want {
			t.Fatalf("q=%d got=%d want=%d", tc.q, got, tc.want)
		}
	}
}

func TestQTableUpdate(t *testing.T) {
	qt := NewQTable()
	state := 1
	action := "1.2.3.4"
	alpha := 0.5

	v1 := qt.Update(state, action, 1.0, alpha)
	if v1 <= 0 {
		t.Fatalf("expected positive v1, got %v", v1)
	}
	v2 := qt.Update(state, action, 1.0, alpha)
	if v2 <= v1 {
		t.Fatalf("expected v2 > v1, got v1=%v v2=%v", v1, v2)
	}
	if qt.Count(state, action) != 2 {
		t.Fatalf("expected count=2 got %d", qt.Count(state, action))
	}
}
