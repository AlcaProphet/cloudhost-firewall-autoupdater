package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/provider"
	"github.com/alcaprophet/cloudhost-firewall-autoupdater/syncer"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	ecs "github.com/alibabacloud-go/ecs-20140526/v7/client"
	swas "github.com/alibabacloud-go/swas-open-20200601/v3/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	lighthouse "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/lighthouse/v20200324"
	vpc "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/vpc/v20170312"
)

func TestReadBudgetScanCache(t *testing.T) {
	for _, ct := range []config.CloudType{config.CloudAliECS, config.CloudAliSWAS, config.CloudTCLighthouse, config.CloudTCCVM} {
		for _, later := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/later=%v", ct, later), func(t *testing.T) {
				e := newTestEnv(t)
				if err := e.store.ReplaceScannedResources(string(ct), "region", []config.ScannedResource{{ResourceID: "old"}}); err != nil {
					t.Fatal(err)
				}
				if err := e.store.ReplaceScannedResources(string(ct), "other", []config.ScannedResource{{ResourceID: "other"}}); err != nil {
					t.Fatal(err)
				}
				before, err := e.store.GetScannedResources(string(ct))
				if err != nil {
					t.Fatal(err)
				}
				var calls atomic.Int32
				release := make(chan struct{})
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if !readBudgetScanDrainRequest(t, r) {
						return
					}
					n := calls.Add(1)
					if later && n == 1 {
						switch ct {
						case config.CloudAliECS:
							readBudgetScanWriteFixture(t, w, r, `{"SecurityGroups":{"SecurityGroup":[{"SecurityGroupId":"new"}]},"NextToken":"T1"}`)
						default:
							list, id := "Instances", "InstanceId"
							if ct == config.CloudTCLighthouse {
								list = "InstanceSet"
							}
							if ct == config.CloudTCCVM {
								list = "SecurityGroupSet"
								id = "SecurityGroupId"
							}
							items := make([]map[string]string, 100)
							for i := range items {
								items[i] = map[string]string{id: fmt.Sprint("new-", i)}
							}
							b, err := json.Marshal(items)
							if err != nil {
								t.Error(err)
								return
							}
							payload := fmt.Sprintf(`{"%s":%s,"TotalCount":101}`, list, b)
							if ct == config.CloudTCLighthouse || ct == config.CloudTCCVM {
								payload = `{"Response":` + payload + `}`
							}
							readBudgetScanWriteFixture(t, w, r, payload)
						}
						return
					}
					select {
					case <-r.Context().Done():
					case <-release:
					case <-time.After(500 * time.Millisecond):
					}
				}))
				defer func() { close(release); srv.CloseClientConnections(); srv.Close() }()
				host := strings.TrimPrefix(srv.URL, "http://")
				creds := provider.Credentials{AliyunAccessKeyID: "fake", AliyunAccessKeySecret: "fake", TencentSecretID: "fake", TencentSecretKey: "fake"}
				pool := provider.NewClientPool(creds)
				var cli any
				if ct == config.CloudAliECS || ct == config.CloudAliSWAS {
					cfg := &openapi.Config{AccessKeyId: tea.String("fake"), AccessKeySecret: tea.String("fake"), Endpoint: tea.String(host), Protocol: tea.String("http"), ConnectTimeout: tea.Int(1000), ReadTimeout: tea.Int(1000)}
					if ct == config.CloudAliECS {
						cli, err = ecs.NewClient(cfg)
					} else {
						cli, err = swas.NewClient(cfg)
					}
				} else {
					cpf := profile.NewClientProfile()
					cpf.HttpProfile.Endpoint = host
					cpf.HttpProfile.Scheme = "http"
					if ct == config.CloudTCLighthouse {
						cli, err = lighthouse.NewClient(common.NewCredential("fake", "fake"), "region", cpf)
					} else {
						cli, err = vpc.NewClient(common.NewCredential("fake", "fake"), "region", cpf)
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				_, err = pool.GetOrCreate(pool.CacheKey(ct, "region"), func() (any, error) { return cli, nil })
				if err != nil {
					t.Fatal(err)
				}
				e.runtime.Apply(&syncer.RuntimeState{Config: config.RuntimeConfig{Credentials: config.Credentials{AliyunAccessKeyID: "fake", AliyunAccessKeySecret: "fake", TencentSecretID: "fake", TencentSecretKey: "fake"}}, Pool: pool})
				w := e.do(t, "POST", "/api/scan-resources", fmt.Sprintf(`{"cloud_type":"%s","region":"region"}`, ct))
				var result map[string]json.RawMessage
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if w.Code != 200 || string(result["success"]) != "false" || len(result["error"]) == 0 || result["resources"] != nil || result["count"] != nil || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("scan failure contract: status=%d body=%s", w.Code, w.Body.String())
				}
				after, err := e.store.GetScannedResources(string(ct))
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatalf("cache changed: before=%+v after=%+v err=%v", before, after, err)
				}
				want := int32(1)
				if later {
					want = 2
				}
				if calls.Load() != want {
					t.Fatalf("calls=%d want=%d", calls.Load(), want)
				}
			})
		}
	}
}

func readBudgetScanDrainRequest(t *testing.T, r *http.Request) bool {
	t.Helper()
	_, readErr := io.Copy(io.Discard, r.Body)
	closeErr := r.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		t.Error(err)
		return false
	}
	return true
}
func readBudgetScanWriteFixture(t *testing.T, w http.ResponseWriter, r *http.Request, text string) {
	t.Helper()
	if _, err := io.WriteString(w, text); err != nil && r.Context().Err() == nil {
		t.Error(err)
	}
}
