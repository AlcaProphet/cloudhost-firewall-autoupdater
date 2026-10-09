package provider

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/alibabacloud-go/tea/tea"
)

// 真实 SWAS SDK → 本地端点；计数矛盾必须丢弃整个快照，非真实云验收。
func TestI811SWASTotalCount(t *testing.T) {
	r1 := swasRuleJSON("r1", "1.1.1.1/32")
	r2 := swasRuleJSON("r2", "2.2.2.2/32")
	r3 := swasRuleJSON("r3", "3.3.3.3/32")
	page := func(total string, rules ...string) string {
		return fmt.Sprintf(`{%s"FirewallRules":[%s]}`, total, strings.Join(rules, ","))
	}
	cases := []struct {
		name  string
		pages []string
		fail  bool
		count int
	}{
		{"decrease_3_to_1", []string{page(`"TotalCount":3,`, r1), page(`"TotalCount":1,`, r2)}, true, 0},
		{"decrease_3_to_0", []string{page(`"TotalCount":3,`, r1), page(`"TotalCount":0,`, r2)}, true, 0},
		{"zero_nonempty", []string{page(`"TotalCount":0,`, r1)}, true, 0},
		{"negative", []string{page(`"TotalCount":-1,`, r1)}, true, 0},
		{"overshoot", []string{page(`"TotalCount":1,`, r1, r2)}, true, 0},
		{"increase_3_to_4", []string{page(`"TotalCount":3,`, r1), page(`"TotalCount":4,`, r2), page(`"TotalCount":4,`, r3), page(`"TotalCount":4,`, swasRuleJSON("r4", "4.4.4.4/32"))}, true, 0},
		{"stable_short_pages", []string{page(`"TotalCount":3,`, r1), page(`"TotalCount":3,`, r2), page(`"TotalCount":3,`, r3)}, false, 3},
		{"zero_empty", []string{page(`"TotalCount":0,`)}, false, 0},
		{"all_null_total", []string{page(`"TotalCount":null,`, r1)}, false, 1},
		{"all_missing_total", []string{page("", r1)}, false, 1},
		{"known_then_missing", []string{page(`"TotalCount":3,`, r1), page("", r2), page("", r3)}, false, 3},

		{"known_null_known", []string{page(`"TotalCount":3,`, r1), page(`"TotalCount":null,`, r2), page(`"TotalCount":3,`, r3)}, false, 3},
		{"known_null_changed", []string{page(`"TotalCount":3,`, r1), page(`"TotalCount":null,`, r2), page(`"TotalCount":2,`, r3)}, true, 0},
		{"samepage_overshoot_later", []string{page(`"TotalCount":2,`, r1), page(`"TotalCount":2,`, r2, r3)}, true, 0},
		{"negative_empty", []string{page(`"TotalCount":-1,`)}, true, 0},
		{"early_empty", []string{page(`"TotalCount":3,`, r1), page(`"TotalCount":3,`)}, true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, h := newMockCloudAPI(t)
			m.reply = func(i int, req recordedRequest) (int, string) {
				if req.action() != "ListFirewallRules" || req.query("PageNumber") != fmt.Sprint(i+1) || req.query("PageSize") != "100" {
					t.Errorf("unexpected request: %+v", req)
				}
				if i >= len(tc.pages) {
					return 500, `{"Code":"InvalidParam","Message":"fixture stop"}`
				}
				return http.StatusOK, tc.pages[i]
			}
			snap, err := mockSWAS(t, h).GetSnapshot()
			calls := len(m.recorded())
			wantCalls := len(tc.pages)
			if tc.name == "increase_3_to_4" {
				wantCalls = 2
			}
			if calls != wantCalls {
				t.Fatalf("calls=%d want=%d", calls, wantCalls)
			}
			if tc.fail {
				if !errors.Is(err, ErrSnapshotIncomplete) || len(snap.Rules) != 0 {
					t.Fatalf("want empty snapshot + ErrSnapshotIncomplete")
				}
			} else if err != nil || len(snap.Rules) != tc.count {
				t.Fatalf("want success with %d rules", tc.count)
			}
		})
	}
}

// 晚出现的总数约束此前累计规则；第 100 页仍可证明完成。
func TestI811SWASLateTotalAndCap(t *testing.T) {
	for _, kind := range []string{"late_equal", "late_overshoot", "late_grow", "late_missing", "late_null", "cap_complete", "cap_incomplete"} {
		t.Run(kind, func(t *testing.T) {
			m, h := newMockCloudAPI(t)
			m.reply = func(i int, r recordedRequest) (int, string) {
				if r.query("PageNumber") != fmt.Sprint(i+1) || r.query("PageSize") != "100" {
					t.Errorf("bad request page/size %s/%s", r.query("PageNumber"), r.query("PageSize"))
				}
				if i > 100 {
					return 400, `{"Code":"InvalidParam"}`
				}
				rule := swasRuleJSON(fmt.Sprint(i), "1.1.1.1/32")
				if strings.HasPrefix(kind, "cap_") {
					total := 100
					if kind == "cap_incomplete" {
						total = 101
					}
					return 200, swasPage([]string{rule}, total, i+1)
				}
				if i == 0 {
					rules := make([]string, 100)
					for j := range rules {
						rules[j] = swasRuleJSON(fmt.Sprint(j), "1.1.1.1/32")
					}
					return 200, `{"FirewallRules":[` + strings.Join(rules, ",") + `]}`
				}
				total, rs := 100, []string{}
				if kind == "late_overshoot" {
					total = 99
				}
				if kind == "late_grow" || kind == "late_missing" || kind == "late_null" {
					total = 101
					rs = []string{rule}
				}
				if kind == "late_missing" {
					return 200, `{"FirewallRules":[` + strings.Join(rs, ",") + `]}`
				}
				if kind == "late_null" {
					return 200, `{"TotalCount":null,"FirewallRules":[` + strings.Join(rs, ",") + `]}`
				}
				return 200, swasPage(rs, total, i+1)
			}
			snap, err := mockSWAS(t, h).GetSnapshot()
			failed := kind == "late_overshoot" || kind == "cap_incomplete"
			wantCalls := 2
			if strings.HasPrefix(kind, "cap_") {
				wantCalls = 100
			}
			if len(m.recorded()) != wantCalls {
				t.Fatalf("calls=%d want=%d", len(m.recorded()), wantCalls)
			}
			if failed {
				if !errors.Is(err, ErrSnapshotIncomplete) || len(snap.Rules) != 0 {
					t.Fatalf("snap=%+v err=%v", snap, err)
				}
			} else {
				want := 100
				if kind == "late_grow" || kind == "late_missing" || kind == "late_null" {
					want = 101
				}
				if err != nil || len(snap.Rules) != want {
					t.Fatalf("count=%d want=%d err=%v", len(snap.Rules), want, err)
				}
			}
		})
	}
}

// 普通 SDK 错误保留原分类，后页失败不返回此前累计规则。
func TestI811SWASSDKErrors(t *testing.T) {
	for _, kind := range []string{"business", "json"} {
		t.Run(kind, func(t *testing.T) {
			m, h := newMockCloudAPI(t)
			m.reply = func(i int, _ recordedRequest) (int, string) {
				if i == 0 {
					return 200, swasPage([]string{swasRuleJSON("r1", "1.1.1.1/32")}, 3, 1)
				}
				if kind == "business" {
					return 400, `{"Code":"InvalidParam","Message":"fixture stop"}`
				}
				return 200, `{invalid`
			}
			snap, err := mockSWAS(t, h).GetSnapshot()
			if kind == "business" {
				var sdkErr *tea.SDKError
				if !errors.As(err, &sdkErr) || tea.StringValue(sdkErr.Code) != "InvalidParam" {
					t.Fatalf("SDK error type lost: %v", err)
				}
			}
			if err == nil || errors.Is(err, ErrSnapshotIncomplete) || len(snap.Rules) != 0 || len(m.recorded()) != 2 {
				t.Fatalf("count=%d err=%v calls=%d", len(snap.Rules), err, len(m.recorded()))
			}
		})
	}
}
