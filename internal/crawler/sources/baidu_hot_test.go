package sources

import "testing"

func TestNormalizeBaiduWord(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"小米SU7", "小米su7"},
		{"小米 SU7", "小米su7"},
		{"小米  SU7", "小米su7"},
		{"AI大模型", "ai大模型"},
		{"AI 大模型", "ai大模型"},
		{"杭州楼市新政", "杭州楼市新政"},
		{" 东方甄选 ", "东方甄选"},
		{"OpenAI", "openai"},
	}
	for _, tc := range cases {
		got := NormalizeBaiduWord(tc.input)
		if got != tc.want {
			t.Errorf("NormalizeBaiduWord(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestIsSimilarWord(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		// 完全相同
		{"杭州楼市新政", "杭州楼市新政", true},
		// 多/少一个字(编辑距离=1)
		{"杭州楼市新政", "杭州楼市新政策", true},
		{"ai大模型", "ai大模型们", true},
		// 变了一个字(编辑距离=1)
		{"杭州楼市新政", "杭州楼市旧政", true},
		// 包含关系
		{"杭州楼市", "杭州楼市新政策来了", true},
		// 差异太大(编辑距离>1,无包含)
		{"杭州楼市", "北京楼市", false},
		{"豆包大模型", "免签政策", false},
		// 差两个字
		{"小米汽车", "小米手机", false},
		// 短词差一个字 — 仍算相似
		{"免签", "免检", true},
	}
	for _, tc := range cases {
		got := IsSimilarWord(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("IsSimilarWord(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
