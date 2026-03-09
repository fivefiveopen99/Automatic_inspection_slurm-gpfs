package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type CheckResult struct {
	Check  string `json:"check"`
	Status string `json:"status"` // PASS/FAIL/WARN/SKIP
	Detail string `json:"detail"`
}

type NodeConfig struct {
	Host      string `json:"host"`
	User      string `json:"user"`
	SlurmNode string `json:"slurm_node"`
	SyncUser  string `json:"sync_user"`
	IBPort    int    `json:"ib_port"`
}

type Config struct {
	SSHUser           string       `json:"ssh_user"`
	SyncUser          string       `json:"sync_user"`
	TimeoutSec        int          `json:"timeout_sec"`
	TimeSyncThreshold int          `json:"time_sync_threshold_sec"`
	MaxWorkers        int          `json:"max_workers"`
	IBPort            int          `json:"ib_port"`
	Nodes             []NodeConfig `json:"nodes"`
}

type NodeReport struct {
	Host    string        `json:"host"`
	User    string        `json:"user"`
	Results []CheckResult `json:"results"`
}

type Report struct {
	GeneratedAt string                 `json:"generated_at"`
	Config      map[string]interface{} `json:"config"`
	Nodes       []NodeReport           `json:"nodes"`
	Summary     map[string]int         `json:"summary"`
}

func main() {
	configPath := flag.String("config", "inspection_config.example.json", "配置文件路径")
	outputPath := flag.String("output", "inspection_report.json", "输出报告 JSON 文件")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	report := runInspection(cfg)
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "报告序列化失败: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*outputPath, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "写入报告失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("巡检完成，报告输出: %s\n", *outputPath)
	fmt.Printf("汇总: %+v\n", report.Summary)
}

func loadConfig(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("读取配置失败: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("解析配置失败: %w", err)
	}
	if len(cfg.Nodes) == 0 {
		return Config{}, errors.New("配置中的 nodes 为空")
	}
	if cfg.TimeoutSec <= 0 {
		cfg.TimeoutSec = 5
	}
	if cfg.TimeSyncThreshold <= 0 {
		cfg.TimeSyncThreshold = 30
	}
	if cfg.MaxWorkers <= 0 {
		cfg.MaxWorkers = 5
	}
	if cfg.IBPort <= 0 {
		cfg.IBPort = 1
	}
	if cfg.SSHUser == "" {
		cfg.SSHUser = "root"
	}
	if cfg.SyncUser == "" {
		cfg.SyncUser = "root"
	}
	return cfg, nil
}

func runInspection(cfg Config) Report {
	report := Report{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Config: map[string]interface{}{
			"ssh_user":                cfg.SSHUser,
			"sync_user":               cfg.SyncUser,
			"timeout_sec":             cfg.TimeoutSec,
			"time_sync_threshold_sec": cfg.TimeSyncThreshold,
			"max_workers":             cfg.MaxWorkers,
			"ib_port":                 cfg.IBPort,
		},
		Nodes: make([]NodeReport, 0, len(cfg.Nodes)),
	}

	nodeCh := make(chan NodeConfig)
	resCh := make(chan NodeReport, len(cfg.Nodes))

	var wg sync.WaitGroup
	workers := cfg.MaxWorkers
	if workers > len(cfg.Nodes) {
		workers = len(cfg.Nodes)
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for node := range nodeCh {
				resCh <- inspectNode(cfg, node)
			}
		}()
	}
	for _, n := range cfg.Nodes {
		nodeCh <- n
	}
	close(nodeCh)
	wg.Wait()
	close(resCh)

	for r := range resCh {
		report.Nodes = append(report.Nodes, r)
	}
	report.Summary = summarize(report.Nodes)
	return report
}

func inspectNode(cfg Config, node NodeConfig) NodeReport {
	user := node.User
	if user == "" {
		user = cfg.SSHUser
	}
	nr := NodeReport{Host: node.Host, User: user, Results: []CheckResult{}}

	pingRes := checkPing(node.Host, cfg.TimeoutSec)
	sshRes := checkSSH(node.Host, user, cfg.TimeoutSec)
	nr.Results = append(nr.Results, pingRes, sshRes)

	if sshRes.Status != "PASS" {
		skipChecks := []string{"运行时长/天", "时间同步状态", "防火墙状态", "SELinux状态", "节点根目录占比", "节点调度状态", "用户同步状态", "IB网络状态", "文件系统挂载状态", "RDMA"}
		for _, chk := range skipChecks {
			nr.Results = append(nr.Results, CheckResult{Check: chk, Status: "SKIP", Detail: `\`})
		}
		return nr
	}

	// 3.1 通用巡检项
	nr.Results = append(nr.Results, checkUptime(node.Host, user, cfg.TimeoutSec))
	nr.Results = append(nr.Results, checkTimeSync(node.Host, user, cfg.TimeoutSec, cfg.TimeSyncThreshold))
	nr.Results = append(nr.Results, checkCollect(node.Host, user, "防火墙状态", "systemctl status firewalld.service --no-pager", cfg.TimeoutSec))
	nr.Results = append(nr.Results, checkCollect(node.Host, user, "SELinux状态", "getenforce", cfg.TimeoutSec))
	nr.Results = append(nr.Results, checkCollect(node.Host, user, "节点根目录占比", "df -h /", cfg.TimeoutSec))

	// 3.2 slurm+gpfs
	slurmNode := node.SlurmNode
	if slurmNode == "" {
		slurmNode = node.Host
	}
	syncUser := node.SyncUser
	if syncUser == "" {
		syncUser = cfg.SyncUser
	}
	ibPort := node.IBPort
	if ibPort <= 0 {
		ibPort = cfg.IBPort
	}

	nr.Results = append(nr.Results, checkCollect(node.Host, user, "节点调度状态", fmt.Sprintf("scontrol show node %s", shellQuote(slurmNode)), cfg.TimeoutSec))
	nr.Results = append(nr.Results, checkUserSync(node.Host, user, syncUser, cfg.TimeoutSec))
	nr.Results = append(nr.Results, checkIBState(node.Host, user, ibPort, cfg.TimeoutSec))
	nr.Results = append(nr.Results, checkCollect(node.Host, user, "文件系统挂载状态", "df -h", cfg.TimeoutSec))
	nr.Results = append(nr.Results, checkCollect(node.Host, user, "RDMA", "mmfsadm test verbs status; mmfsadm test verbs conns", cfg.TimeoutSec))

	return nr
}

func checkPing(host string, timeoutSec int) CheckResult {
	cmd := fmt.Sprintf("ping -c 1 -W %d %s", timeoutSec, shellQuote(host))
	rc, out, err := runLocal(cmd, timeoutSec+5)
	if rc == 0 {
		return CheckResult{Check: "ssh连通(ping)", Status: "PASS", Detail: "ping 正常"}
	}
	return CheckResult{Check: "ssh连通(ping)", Status: "FAIL", Detail: firstNonEmpty(err, out, "ping 失败")}
}

func checkSSH(host, user string, timeoutSec int) CheckResult {
	rc, out, err := runSSH(host, user, "echo ok", timeoutSec)
	if rc == 0 && strings.TrimSpace(out) == "ok" {
		return CheckResult{Check: "ssh连通(ssh)", Status: "PASS", Detail: "ssh 执行正常"}
	}
	return CheckResult{Check: "ssh连通(ssh)", Status: "FAIL", Detail: firstNonEmpty(err, out, "ssh 失败")}
}

func checkUptime(host, user string, timeoutSec int) CheckResult {
	rc, out, err := runSSH(host, user, "uptime", timeoutSec)
	if rc != 0 {
		return CheckResult{Check: "运行时长/天", Status: "WARN", Detail: firstNonEmpty(err, out, "无法获取 uptime")}
	}
	days, ok := parseUptimeDays(out)
	if !ok {
		return CheckResult{Check: "运行时长/天", Status: "WARN", Detail: "解析失败: " + out}
	}
	return CheckResult{Check: "运行时长/天", Status: "PASS", Detail: fmt.Sprintf("约 %d 天", days)}
}

func checkTimeSync(host, user string, timeoutSec, thresholdSec int) CheckResult {
	rcL, outL, _ := runLocal("date +%s", timeoutSec)
	rcR, outR, errR := runSSH(host, user, "date +%s", timeoutSec)
	if rcL != 0 || rcR != 0 {
		return CheckResult{Check: "时间同步状态", Status: "WARN", Detail: firstNonEmpty(errR, "时间获取失败")}
	}
	l, err1 := strconv.ParseInt(strings.TrimSpace(outL), 10, 64)
	r, err2 := strconv.ParseInt(strings.TrimSpace(outR), 10, 64)
	if err1 != nil || err2 != nil {
		return CheckResult{Check: "时间同步状态", Status: "WARN", Detail: fmt.Sprintf("时间解析失败: local=%s, remote=%s", outL, outR)}
	}
	delta := l - r
	if delta < 0 {
		delta = -delta
	}
	if delta < int64(thresholdSec) {
		return CheckResult{Check: "时间同步状态", Status: "PASS", Detail: fmt.Sprintf("时间差 %ds (<%ds)", delta, thresholdSec)}
	}
	return CheckResult{Check: "时间同步状态", Status: "FAIL", Detail: fmt.Sprintf("时间差 %ds (>=%ds)", delta, thresholdSec)}
}

func checkCollect(host, user, check, cmd string, timeoutSec int) CheckResult {
	rc, out, err := runSSH(host, user, cmd, timeoutSec)
	if rc == 0 {
		return CheckResult{Check: check, Status: "PASS", Detail: firstNonEmpty(out, "执行成功")}
	}
	return CheckResult{Check: check, Status: "WARN", Detail: firstNonEmpty(err, out, "执行失败")}
}

func checkUserSync(host, user, syncUser string, timeoutSec int) CheckResult {
	rc, out, err := runSSH(host, user, fmt.Sprintf("id %s", shellQuote(syncUser)), timeoutSec)
	if rc == 0 {
		return CheckResult{Check: "用户同步状态", Status: "PASS", Detail: firstNonEmpty(out, "存在")}
	}
	return CheckResult{Check: "用户同步状态", Status: "FAIL", Detail: firstNonEmpty(err, out, "不存在")}
}

func checkIBState(host, user string, ibPort int, timeoutSec int) CheckResult {
	rc, out, err := runSSH(host, user, fmt.Sprintf("ibstat %d", ibPort), timeoutSec)
	if rc != 0 {
		return CheckResult{Check: "IB网络状态", Status: "WARN", Detail: firstNonEmpty(err, out, "ibstat 执行失败")}
	}
	status, detail := inferIBStatus(out)
	return CheckResult{Check: "IB网络状态", Status: status, Detail: detail}
}

func inferIBStatus(output string) (string, string) {
	lower := strings.ToLower(output)
	if strings.Contains(lower, "state: active") || strings.Contains(lower, " active") {
		return "PASS", "Active"
	}
	if strings.Contains(lower, "state: down") || strings.Contains(lower, " down") {
		return "FAIL", "Down"
	}
	return "WARN", "未识别状态: " + firstNonEmpty(output, "unknown")
}

func parseUptimeDays(text string) (int, bool) {
	re := regexp.MustCompile(`up\s+(.+?),\s+\d+\s+user`)
	m := re.FindStringSubmatch(text)
	if len(m) < 2 {
		return 0, false
	}
	up := m[1]
	days := 0.0
	if dm := regexp.MustCompile(`(\d+)\s+day`).FindStringSubmatch(up); len(dm) > 1 {
		v, _ := strconv.Atoi(dm[1])
		days += float64(v)
	}
	if hm := regexp.MustCompile(`(\d+):(\d+)`).FindStringSubmatch(up); len(hm) > 2 {
		h, _ := strconv.Atoi(hm[1])
		mi, _ := strconv.Atoi(hm[2])
		days += float64(h)/24.0 + float64(mi)/1440.0
	}
	if mm := regexp.MustCompile(`(\d+)\s+min`).FindStringSubmatch(up); len(mm) > 1 {
		v, _ := strconv.Atoi(mm[1])
		days += float64(v) / 1440.0
	}
	return maxInt(1, int(math.Ceil(days))), true
}

func summarize(nodes []NodeReport) map[string]int {
	s := map[string]int{"PASS": 0, "FAIL": 0, "WARN": 0, "SKIP": 0}
	for _, n := range nodes {
		for _, r := range n.Results {
			s[r.Status]++
		}
	}
	return s
}

func runLocal(command string, timeoutSec int) (int, string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-lc", command)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return 124, "", "命令超时"
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			text := strings.TrimSpace(string(out))
			return ee.ExitCode(), text, text
		}
		return 1, "", err.Error()
	}
	return 0, strings.TrimSpace(string(out)), ""
}

func runSSH(host, user, remoteCmd string, timeoutSec int) (int, string, string) {
	sshCmd := fmt.Sprintf("ssh -o BatchMode=yes -o ConnectTimeout=%d %s@%s %s",
		timeoutSec,
		shellQuote(user),
		shellQuote(host),
		shellQuote(remoteCmd),
	)
	return runLocal(sshCmd, timeoutSec+5)
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
