package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)
import "testing"

func TestParseUptimeDays(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{" 10:00:00 up 5 min,  1 user,  load average: 0.01, 0.02, 0.03", 1, true},
		{" 10:00:00 up 2 days,  1:10,  2 users,  load average: 0.01, 0.02, 0.03", 3, true},
		{"bad format", 0, false},
	}
	for _, c := range cases {
		got, ok := parseUptimeDays(c.in)
		if ok != c.ok {
			t.Fatalf("parseUptimeDays(%q) ok=%v want=%v", c.in, ok, c.ok)
		}
		if ok && got != c.want {
			t.Fatalf("parseUptimeDays(%q)=%d want=%d", c.in, got, c.want)
		}
	}
}

func TestInferIBStatus(t *testing.T) {
	status, detail := inferIBStatus("State: Active")
	if status != "PASS" || detail != "Active" {
		t.Fatalf("unexpected active result: %s %s", status, detail)
	}
	status, detail = inferIBStatus("State: Down")
	if status != "FAIL" || detail != "Down" {
		t.Fatalf("unexpected down result: %s %s", status, detail)
	}
	status, _ = inferIBStatus("State: Init")
	if status != "WARN" {
		t.Fatalf("unexpected unknown status: %s", status)
	}
}

func TestSummarize(t *testing.T) {
	nodes := []NodeReport{
		{Results: []CheckResult{{Status: "PASS"}, {Status: "FAIL"}}},
		{Results: []CheckResult{{Status: "WARN"}, {Status: "SKIP"}, {Status: "PASS"}}},
	}
	s := summarize(nodes)
	if s["PASS"] != 2 || s["FAIL"] != 1 || s["WARN"] != 1 || s["SKIP"] != 1 {
		t.Fatalf("unexpected summary: %+v", s)
	}
}

func TestDiscoverNodesFromHosts(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hosts")
	content := "10.10.25.51 mgt\n10.10.25.52 io1\n10.10.25.53 io2\n10.10.25.54 node1\n10.10.25.55 node2\n"
	if err := os.WriteFile(hostsPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write hosts file failed: %v", err)
	}

	nodes, err := discoverNodesFromHosts(hostsPath, "mgt")
	if err != nil {
		t.Fatalf("discoverNodesFromHosts returned error: %v", err)
	}
	if len(nodes) != 4 {
		t.Fatalf("unexpected node count: %d", len(nodes))
	}
	for _, n := range nodes {
		if n.Host == "mgt" {
			t.Fatalf("management node should be excluded")
		}
		if n.SlurmNode != n.Host {
			t.Fatalf("slurm node should default to host, got %q for host %q", n.SlurmNode, n.Host)
		}
	}
}

func TestLoadNodesFromFile(t *testing.T) {
	dir := t.TempDir()
	nodePath := filepath.Join(dir, "nodes.txt")
	content := `# nodes
mgt
io1
io2
localhost
io1
`
	if err := os.WriteFile(nodePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write node file failed: %v", err)
	}

	nodes, err := loadNodesFromFile(nodePath)
	if err != nil {
		t.Fatalf("loadNodesFromFile returned error: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("unexpected node count: %d", len(nodes))
	}
	if nodes[0].Host != "mgt" || nodes[1].Host != "io1" || nodes[2].Host != "io2" {
		t.Fatalf("unexpected nodes: %+v", nodes)
	}
}

func TestRenderTableReport(t *testing.T) {
	report := Report{Nodes: []NodeReport{{
		Host: "c01n01",
		Results: []CheckResult{
			{Check: "ssh连通(ssh)", Status: "PASS", Detail: "ssh ok"},
			{Check: "ssh连通(ping)", Status: "PASS", Detail: "ping ok"},
			{Check: "运行时长/天", Status: "PASS", Detail: "约 3 天"},
			{Check: "时间同步状态", Status: "PASS", Detail: "时间差 1s (<30s)"},
			{Check: "防火墙状态", Status: "PASS", Detail: "Active: inactive"},
			{Check: "SELinux状态", Status: "PASS", Detail: "Disabled"},
			{Check: "节点根目录占比", Status: "PASS", Detail: `Filesystem Size Used Avail Use% Mounted on
/dev/sda1 500G 10G 490G 2% /`},
			{Check: "节点调度状态", Status: "PASS", Detail: "NodeName=c01n01 State=IDLE ThreadsPerCore=1"},
			{Check: "用户同步状态", Status: "PASS", Detail: "uid=1000(test)"},
			{Check: "IB网络状态", Status: "PASS", Detail: "Active"},
			{Check: "文件系统挂载状态", Status: "PASS", Detail: "ok"},
			{Check: "RDMA", Status: "PASS", Detail: "started"},
		},
	}}}
	out := renderTableReport(report, "my_report.txt")
	if !strings.Contains(out, "my_report.txt") {
		t.Fatalf("title not found in table: %s", out)
	}
	if !strings.Contains(out, "c01n01") || !strings.Contains(out, "IDLE") || !strings.Contains(out, "U 2%, F 490G") {
		t.Fatalf("expected fields not found in table: %s", out)
	}
}
