//go:build step16

package process

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alcaprophet/cloudhost-firewall-autoupdater/config"
)

func TestBuild9S7Browser(t *testing.T) {
	f := newStep16Fixture(t)
	f.cloud.setZone("zone-browser7", step16Zone{CurrentVer: "v1", CurrentV4: []string{"203.0.113.0/32", "203.0.113.2/32"}})
	f.cloud.setInstance("lhins-step16-a", step16Instance{Rules: step7ManualRows(95)})
	f.setup(1, func(t *testing.T, s *config.Store, ids []int) {
		if err := s.SetSetting("dns_enabled", "false"); err != nil {
			t.Fatal(err)
		}
		step16AddSite(t, s, config.EdgeOneSite{ZoneID: "zone-browser7", TargetIDs: ids, Protocol: "TCP", Ports: "80,443", AddressFamily: "ipv4"})
	})
	f.start()
	defer func() {
		if t.Failed() {
			t.Logf("browser product logs:\n%s", f.out.String())
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join("testdata", "step7", "browser.mjs"), f.base)
	cmd.Env = os.Environ()
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("browser: %v context=%v\n%s", err, ctx.Err(), out.String())
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.Contains(line, "B9S7_BROWSER") {
			t.Log(line)
		}
	}
	inst, _ := f.cloud.instanceSnapshot("lhins-step16-a")
	if inst.CreateCalls != 1 || inst.DeleteCalls != 1 || len(inst.Rules) != 95 {
		t.Fatalf("browser duplicate writes or missing cleanup: %+v", inst)
	}
	if _, cf := f.cloud.teoCounts("zone-browser7"); cf != 0 {
		t.Fatal("browser no pending version but confirmed")
	}
	settings, _ := f.settings()
	if settings["edgeone_enabled"] != "false" {
		t.Fatal("browser disable not committed")
	}
	f.stop()
}
