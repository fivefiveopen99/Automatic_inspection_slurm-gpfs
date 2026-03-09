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
# 默认从 hosts_file 自动发现节点（排除 management_node，默认 mgt）
# 如需手动指定，也可在 nodes 中填写

go run . --config inspection_config.json --output inspection_report.json -o /tmp/inspection_report.txt

# 或使用节点文件（优先级最高）
# 每行一个节点名，支持 # 注释
go run . --config inspection_config.json -f nodes.txt --output inspection_report.json -o /tmp/inspection_report.txt
```

## 输出说明

- 默认输出两个文件：

  - JSON 报告（`--output`）
  - 文本表格报告（`-o`），样式类似运维巡检大屏表格
- `PASS`：通过
- `FAIL`：不通过
- `WARN`：命令执行失败或状态无法判定
- 文本表格第一行标题使用 `-o` 指定的输出文件名（例如 `inspection_report.txt`）。
- `SKIP`：因 ssh 异常跳过（detail 为 `\`）


## 节点自动发现（/etc/hosts）

- `mgt` 视为管理节点，自动跳过。
- 其余主机名视为普通节点并加入巡检目标。
- 可通过配置 `hosts_file`、`management_node` 覆盖默认值。
- 若 `nodes` 非空，则优先使用 `nodes`。


## 节点文件（-f）

- 参数：`-f <节点文件路径>`。
- 文件格式：每行一个节点名（如 `io1`），空行和 `#` 注释行会忽略。
- 优先级：`-f` > 配置 `nodes` > `/etc/hosts` 自动发现。
