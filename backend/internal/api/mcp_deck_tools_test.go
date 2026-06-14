package api

import (
	"reflect"
	"testing"
)

func TestPickPreviewFrames(t *testing.T) {
	cases := []struct {
		want  []int
		count int
		exp   []int
	}{
		{nil, 0, nil},            // empty deck
		{nil, 3, []int{1, 2, 3}}, // default: all (under cap)
		{nil, 15, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}}, // capped at 10
		{[]int{2, 2, 9, 99, 0}, 5, []int{2}},            // dedup + clamp out-of-range
		{[]int{3, 1}, 5, []int{3, 1}},                   // explicit order preserved
	}
	for i, c := range cases {
		got := pickPreviewFrames(c.want, c.count)
		if !reflect.DeepEqual(got, c.exp) {
			t.Errorf("case %d: pickPreviewFrames(%v,%d)=%v want %v", i, c.want, c.count, got, c.exp)
		}
	}
}
