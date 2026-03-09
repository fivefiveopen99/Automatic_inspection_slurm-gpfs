# Automatic inspection for slurm+gpfs

使用 Go 重构的 HPC 自动化巡检工具。

## 快速开始

1. 准备配置文件（可复制示例）

```bash
cp inspection_config.example.json inspection_config.json
# 按实际节点修改 host、user、slurm_node、sync_user 等字段
```

2. 运行巡检

```bash
go run . --config inspection_config.json --output inspection_report.json
```

或先构建二进制后运行：

```bash
go build -o hpc-inspector .
./hpc-inspector --config inspection_config.json --output inspection_report.json
```

3. 查看报告

输出为 JSON，包含每台节点每个检查项状态：
- PASS：通过
- FAIL：不通过
- WARN：命令执行失败或结果不可判定
- SKIP：因 ssh 异常按规范跳过后续项

## 巡检项覆盖

- 基础通用巡检：ssh/ping、运行时长、时间同步、防火墙、SELinux、根目录占比
- HPC 巡检（slurm+gpfs）：节点调度状态、用户同步、IB 网络、文件系统挂载、RDMA

## 说明

- 当 ssh 检查失败时，脚本会自动将其余检查项标记为 `SKIP`。
- 运行时长按“不足一天按一天计算”处理。
- 时间同步阈值默认 30s，可通过配置 `time_sync_threshold_sec` 调整。
