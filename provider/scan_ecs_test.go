package provider

import (
	"errors"
	"net/http"
	"strconv"
	"testing"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	ecs "github.com/alibabacloud-go/ecs-20140526/v7/client"
)

func TestScanECSIncompletePage(t *testing.T) {
	for _, payload := range []string{`{"NextToken":"T2"}`, `{"SecurityGroups":{"SecurityGroup":null}}`, `{"SecurityGroups":{"SecurityGroup":[null]}}`, `{"SecurityGroups":{"SecurityGroup":[{}]}}`} {
		t.Run(payload, func(t *testing.T) {
			m, host := newMockCloudAPI(t)
			m.reply = func(i int, _ recordedRequest) (int, string) {
				if i == 0 {
					return http.StatusOK, `{"SecurityGroups":{"SecurityGroup":[{"SecurityGroupId":"sg-a"}]},"NextToken":"T1"}`
				}
				return http.StatusOK, payload
			}
			cli := mockECS(t, host).client
			pool := NewClientPool(Credentials{})
			if _, err := pool.GetOrCreate(pool.CacheKey(config.CloudAliECS, "cn-hangzhou"), func() (any, error) { return cli, nil }); err != nil {
				t.Fatal(err)
			}
			resources, err := ScanResources(config.CloudAliECS, "cn-hangzhou", pool)
			if !errors.Is(err, ErrSnapshotIncomplete) || resources != nil {
				t.Fatalf("resources=%+v err=%v", resources, err)
			}
		})
	}
}

// TestDecodeECSScanPageMissingResponse 补齐 HTTP mock 无法构造的原生 nil 指针。
func TestDecodeECSScanPageMissingResponse(t *testing.T) {
	for _, resp := range []*ecs.DescribeSecurityGroupsResponse{nil, {}} {
		resources, token, err := decodeECSScanPage(resp, "cn-hangzhou")
		if !errors.Is(err, ErrSnapshotIncomplete) || resources != nil || token != "" {
			t.Fatalf("resources=%+v token=%q err=%v", resources, token, err)
		}
	}
}

// TestScanECSTokenProgress P3-25 独立证据：重复 token 有界失败，不返回半截资源。
func TestScanECSTokenProgress(t *testing.T) {
	cases := []struct {
		name   string
		tokens []string
		failed bool
	}{
		{"正常两页", []string{"T1", ""}, false},
		{"未推进", []string{"T1", "T1"}, true},
		{"历史环路", []string{"T1", "T2", "T1"}, true},
		{"不透明空格", []string{" T+/= ", "T+/=", ""}, false},
		{"不透明大小写", []string{"Token", "token", ""}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, host := newMockCloudAPI(t)
			m.reply = func(i int, req recordedRequest) (int, string) {
				// 修复缺失时也必然停止，用请求次数证明判别力，不靠超时。
				if i >= len(tc.tokens) {
					return http.StatusBadRequest, `{"Code":"InvalidParameter","Message":"fixture stop"}`
				}
				want := ""
				if i > 0 {
					want = tc.tokens[i-1]
				}
				if req.query("NextToken") != want {
					t.Errorf("请求 token=%q，预期 %q", req.query("NextToken"), want)
				}
				return http.StatusOK, `{"SecurityGroups":{"SecurityGroup":[{"SecurityGroupId":"sg-` + strconv.Itoa(i) + `"}]},"NextToken":"` + tc.tokens[i] + `"}`
			}
			pool := NewClientPool(Credentials{})
			cli := mockECS(t, host).client
			if _, err := pool.GetOrCreate(pool.CacheKey(config.CloudAliECS, "cn-hangzhou"), func() (any, error) { return cli, nil }); err != nil {
				t.Fatal(err)
			}
			resources, err := ScanResources(config.CloudAliECS, "cn-hangzhou", pool)
			if tc.failed {
				if !errors.Is(err, ErrSnapshotIncomplete) || resources != nil {
					t.Fatalf("resources=%+v err=%v", resources, err)
				}
			} else {
				if err != nil || len(resources) != len(tc.tokens) {
					t.Fatalf("resources=%+v err=%v，预期 %d 项", resources, err, len(tc.tokens))
				}
				for i, resource := range resources {
					if want := "sg-" + strconv.Itoa(i); resource.ResourceID != want {
						t.Fatalf("第 %d 项=%+v，预期资源 %q", i, resource, want)
					}
				}
			}
			if len(m.recorded()) != len(tc.tokens) {
				t.Fatalf("请求次数=%d，预期 %d", len(m.recorded()), len(tc.tokens))
			}
		})
	}
}
