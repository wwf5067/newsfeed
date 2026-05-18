package crawler

import "testing"

func TestParseHeat(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"1234 万热度", 12_340_000},
		{"12 亿热度", 1_200_000_000},
		{"1.5 万热度", 15_000},
		{"500 热度", 500},
		{"", 0},
		{"乱码 abc", 0},
		{"  4479 万热度  ", 44_790_000}, // 前后空白容忍
	}
	for _, c := range cases {
		if got := ParseHeat(c.in); got != c.want {
			t.Errorf("ParseHeat(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
