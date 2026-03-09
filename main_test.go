package main

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
