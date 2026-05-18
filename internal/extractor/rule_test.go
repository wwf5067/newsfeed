package extractor

import (
	"context"
	"testing"
)

func TestRuleExtractor_Basic(t *testing.T) {
	ex := NewRuleExtractor()

	cases := []struct {
		name             string
		title            string
		wantEntityCount  int
		wantEntityTypes  []string // 期望出现的 type(子集判断)
		wantEventCount   int
	}{
		{
			name:            "book mark + actor + location",
			title:           "马斯克宣布在上海发布《Grok 4》",
			wantEntityCount: 3, // 马斯克(person) + 上海(location) + Grok 4(work)
			wantEntityTypes: []string{EntityPerson, EntityLocation, EntityWork},
			wantEventCount:  1,
		},
		{
			name:            "only location",
			title:           "北京下大雪",
			wantEntityCount: 1,
			wantEntityTypes: []string{EntityLocation},
			wantEventCount:  1,
		},
		{
			name:            "empty title",
			title:           "",
			wantEntityCount: 0,
			wantEventCount:  0,
		},
		{
			name:            "dedup same entity",
			title:           "《活着》《活着》",
			wantEntityCount: 1, // 同名书只算一次
			wantEntityTypes: []string{EntityWork},
			wantEventCount:  1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := ex.Extract(context.Background(), tc.title, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(res.Entities) != tc.wantEntityCount {
				t.Errorf("entity count: got %d, want %d, entities=%+v",
					len(res.Entities), tc.wantEntityCount, res.Entities)
			}
			if len(res.Events) != tc.wantEventCount {
				t.Errorf("event count: got %d, want %d", len(res.Events), tc.wantEventCount)
			}
			for _, wantType := range tc.wantEntityTypes {
				found := false
				for _, e := range res.Entities {
					if e.Type == wantType {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("want entity type %q, not found in %+v", wantType, res.Entities)
				}
			}
		})
	}
}

func TestFingerprint_StableAcrossPunctuation(t *testing.T) {
	// 同主题、不同标点空格的标题应得到相同指纹
	fp1 := fingerprint("马斯克 宣布!发布Grok")
	fp2 := fingerprint("马斯克宣布发布Grok")
	fp3 := fingerprint("马斯克,宣布,发布,Grok")
	if fp1 != fp2 || fp2 != fp3 {
		t.Errorf("fingerprints should match: %s / %s / %s", fp1, fp2, fp3)
	}
	if len(fp1) != 16 {
		t.Errorf("fingerprint length: got %d, want 16", len(fp1))
	}
}

func TestFingerprint_DifferentTitles(t *testing.T) {
	fp1 := fingerprint("北京下雪")
	fp2 := fingerprint("上海下雪")
	if fp1 == fp2 {
		t.Errorf("different titles should have different fingerprints")
	}
}
