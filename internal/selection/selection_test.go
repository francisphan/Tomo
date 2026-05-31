package selection

import (
	"testing"

	"tomo/internal/model"
)

func chapters(nums ...string) []model.Chapter {
	out := make([]model.Chapter, len(nums))
	for i, n := range nums {
		out[i] = model.Chapter{Number: n}
	}
	return out
}

func nums(chs []model.Chapter) []string {
	out := make([]string, len(chs))
	for i, c := range chs {
		out[i] = c.Number
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestFilter(t *testing.T) {
	all := chapters("1", "2", "3", "10", "10.5", "11", "20")
	cases := []struct {
		spec string
		want []string
	}{
		{"", []string{"1", "2", "3", "10", "10.5", "11", "20"}},
		{"2", []string{"2"}},
		{"1-3", []string{"1", "2", "3"}},
		{"10-11", []string{"10", "10.5", "11"}},
		{"11-", []string{"11", "20"}},
		{"-3", []string{"1", "2", "3"}},
		{"1,20", []string{"1", "20"}},
		{"1-2,11-", []string{"1", "2", "11", "20"}},
	}
	for _, c := range cases {
		sel, err := Parse(c.spec)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.spec, err)
		}
		got := nums(sel.Filter(all))
		if !eq(got, c.want) {
			t.Errorf("spec %q: got %v want %v", c.spec, got, c.want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, bad := range []string{"abc", "1-x", "x-2"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q): expected error", bad)
		}
	}
}
