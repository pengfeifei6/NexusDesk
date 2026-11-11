package cache

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

type decodeItem struct {
	ID, OrgID, CreatorID int64
	AssigneeID           *int64
	Title, Body          string
	Priority             int16
	Status               string
	Version              int64
	CreatedAt, UpdatedAt time.Time
	DeletedAt            *time.Time
}

// A missing/null/foreign or malformed snapshot must remain a cache miss;
// a valid empty list must remain a hit with a non-nil slice.
func TestDecodeListContract(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		ok        bool
		want      []int
	}{
		{"valid", `{"org_id":7,"data":[1,2]}`, true, []int{1, 2}},
		{"empty", `{"org_id":7,"data":[]}`, true, []int{}},
		{"null", `{"org_id":7,"data":null}`, false, nil},
		{"missing", `{"org_id":7}`, false, nil},
		{"foreign", `{"org_id":8,"data":[1]}`, false, nil},
		{"truncated", `{"org_id":7,"data":[1]`, false, nil},
		{"trailing", `{"org_id":7,"data":[]} false`, false, nil},
		{"wrong type", `{"org_id":7,"data":{}}`, false, nil},
		{"bad item", `{"org_id":7,"data":["invalid"]}`, false, nil},
		{"missing org", `{"data":[]}`, false, nil},
		{"invalid org", `{"org_id":"7","data":[]}`, false, nil},
		{"null envelope", `null`, false, nil},
		{"corrupt", `garbage`, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := decodeList[int]([]byte(tc.raw), 7, 1024)
			if ok != tc.ok || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got=%v ok=%v", got, ok)
			}
		})
	}
	raw := []byte(`{"org_id":7,"data":[]}`)
	if _, ok := decodeList[int](raw, 7, len(raw)-1); ok {
		t.Fatal("oversize accepted")
	}
	if _, ok := decodeList[int](raw, 7, len(raw)); !ok {
		t.Fatal("exact size rejected")
	}
}

// Test-only comparator preserves the baseline double-unmarshal path.
func baselineDecode[T any](b []byte, org int64, max int) ([]T, bool) {
	var e envelope
	var items []T
	if len(b) > max || json.Unmarshal(b, &e) != nil || e.OrgID != org || len(e.Data) == 0 || string(e.Data) == "null" || json.Unmarshal(e.Data, &items) != nil || items == nil {
		return nil, false
	}
	return items, true
}
func benchmarkPayload() []byte {
	items := make([]decodeItem, 100)
	for n := range items {
		items[n] = decodeItem{ID: int64(n + 1), OrgID: 7, CreatorID: 1, Title: "Synthetic ticket", Body: strings.Repeat("Synthetic body. ", 20), Priority: 2, Status: "OPEN", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	}
	data, _ := json.Marshal(items)
	raw, _ := json.Marshal(envelope{OrgID: 7, Data: data})
	return raw
}
func TestDecodeMatchesBaselineOnRepresentativeList(t *testing.T) {
	raw := benchmarkPayload()
	before, ok := baselineDecode[decodeItem](raw, 7, 1<<20)
	if !ok {
		t.Fatal("invalid fixture")
	}
	after, ok := decodeList[decodeItem](raw, 7, 1<<20)
	if !ok || !reflect.DeepEqual(before, after) {
		t.Fatal("valid list changed")
	}
}
func BenchmarkCacheEnvelope(b *testing.B) {
	raw := benchmarkPayload()
	for _, name := range []string{"baseline", "typed"} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(raw)))
			for b.Loop() {
				var ok bool
				if name == "baseline" {
					_, ok = baselineDecode[decodeItem](raw, 7, 1<<20)
				} else {
					_, ok = decodeList[decodeItem](raw, 7, 1<<20)
				}
				if !ok {
					b.Fatal("decode failed")
				}
			}
		})
	}
}

// Reusing backing elements across repeated envelope fields can inherit an old
// item's ID/org/content and make an incomplete final item pass validation.
func TestRepeatedDataStartsWithFreshItems(t *testing.T) {
	prefix := `{"org_id":7,"data":[{"ID":1,"OrgID":7,"CreatorID":5,"Title":"old","Body":"stale","Priority":2,"Status":"OPEN","Version":3}],`
	for _, tc := range []struct {
		name, final string
		want        []decodeItem
		valid       bool
	}{
		{"empty object", `"data":[{}]}`, []decodeItem{{}}, false},
		{"null item", `"data":[null]}`, []decodeItem{{}}, false},
		{"partial item", `"data":[{"Title":"partial"}]}`, []decodeItem{{Title: "partial"}}, false},
		{"valid replacement", `"data":[{"ID":9,"OrgID":7,"Title":"new"}]}`, []decodeItem{{ID: 9, OrgID: 7, Title: "new"}}, true},
		{"escaped key", `"d\u0061ta":[{}]}`, []decodeItem{{}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(prefix + tc.final)
			got, ok := decodeList[decodeItem](raw, 7, 1<<20)
			if !ok || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got=%+v ok=%v want=%+v", got, ok, tc.want)
			}
			if valid := (got[0].ID > 0 && got[0].OrgID == 7); valid != tc.valid {
				t.Fatalf("corruption validation=%v", valid)
			}
			old, oldOK := baselineDecode[decodeItem](raw, 7, 1<<20)
			if !oldOK || !reflect.DeepEqual(old, got) {
				t.Fatal("last-field behavior differs from baseline")
			}
		})
	}
}
