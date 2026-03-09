# slurm+gpfs 自动化运维巡检工具（Go）

本项目使用 Go 编写，用于执行 HPC 集群巡检。

## 覆盖巡检项

### 3.1 基础通用巡检项（集群通用）

- ssh连通：ping / ssh 远程执行
- 运行时长/天：`uptime`
- 时间同步状态：管理节点与巡检节点时间差（默认阈值 `<30s`）
- 防火墙状态：`firewalld.service`
- SELinux状态：`getenforce`
- 节点根目录占比：`df -h /`

> 注意：
> 1) ssh 连通异常时，后续检查项标记为 `\`；
> 2) 运行时长不足一天按一天计算。

### 3.2 HPC 巡检项（slurm+gpfs）

- 节点调度状态：`scontrol show node <节点名>`
- 用户同步状态：`id <用户名>`（存在/不存在）
- IB网络状态：`ibstat <端口>`（Active / Down）
- 文件系统挂载状态：`df -h`
- RDMA：`mmfsadm test verbs status` / `mmfsadm test verbs conns`

## 快速开始

```bash
cp inspection_config.example.json inspection_config.json
# 修改 nodes、用户名、端口等

go run . --config inspection_config.json --output inspection_report.json
```

## 输出说明

- `PASS`：通过
- `FAIL`：不通过
- `WARN`：命令执行失败或状态无法判定
- `SKIP`：因 ssh 异常跳过（detail 为 `\`）
