# FWAlizer — 防火墙 DNS 自动同步工具

**FWAlizer**（Firewall DNS Synchronizer）是一个轻量级自动化工具：定时解析指定域名的 IP 地址，自动同步到云防火墙/安全组白名单中。专为域名 IP 频繁变动的场景设计（如动态 DNS、API 网关、VPN 入口）。

> 默认推荐使用 **WebUI 模式**（浏览器可视化管理，零配置文件）；`.env` 模式作为**备用/进阶/极简**模式，适合服务器无界面场景。

![仪表盘](./ReadmeAsset/dashboard.png)

---

## 目录

- [核心特性](#核心特性)
- [界面预览](#界面预览)
- [快速上手（WebUI 模式，推荐）](#快速上手webui-模式推荐)
- [运行模式](#运行模式)
- [配置说明](#配置说明)
- [CLI 数据备份与恢复](#cli-数据备份与恢复)
- [告警通知](#告警通知)
- [多云 API 权限与 API 文档](#多云-api-权限与-api-文档)
- [Docker 部署](#docker-部署)
- [开发指南](#开发指南)
- [常见问题（FAQ）](#常见问题faq)

---

## 核心特性

- **多云支持**：腾讯云 Lighthouse / CVM，阿里云轻量云（SWAS）/ ECS，四款云产品统一管控
- **WebUI 可视化管理**（推荐）：浏览器完成全部配置，明暗主题、仪表盘状态卡片、同步日志实时查看、模拟测试预览
- **资源扫描与自动补全**：设置页一键扫描云厂商资源（实例/安全组），添加目标时自动补全资源 ID 并联动填入地域
- **地域自动补全**：内置四平台地域数据，添加目标时可下拉过滤选择，也可自由输入任意地域（兼容新区域）
- **增量同步**：仅操作带 `[TAG]` 标记的规则，绝不覆盖手动配置的防火墙规则
- **DNS 熔断保护**：连续解析失败达阈值后自动熔断，半开探测自动恢复，避免误删规则
- **乐观锁重试**：每次写入前重新拉取最新状态，最多 3 次指数退避重试
- **跨云并行**：不同云厂商并行同步，同厂商内串行（避免触发频率限制）
- **单二进制分发**：前端 WebUI 编译进二进制，无运行时依赖
- **Docker 就绪**：Alpine 基础镜像，非 root 运行，健康检查

---

## 界面预览

> 以下截图使用演示数据（示例目标与规则），实际界面以您的配置为准。

仪表盘支持明暗双主题，状态一目了然：

| 亮色主题 | 暗色主题 |
|:---:|:---:|
| ![仪表盘-亮色](./ReadmeAsset/dashboard.png) | ![仪表盘-暗色](./ReadmeAsset/dashboard-dark.png) |

**云资源管理**：目标增删改、资源 ID 按平台提示、扫描结果自动补全、弹窗内测试连接

![云资源管理](./ReadmeAsset/targets.png)

![添加目标弹窗](./ReadmeAsset/target-add-modal.png)

**域名规则**：协议/端口/动作/适用目标配置，每条规则独立 IPv6 解析开关

![域名规则](./ReadmeAsset/rules.png)

**全局设置**：凭据卡片化（腾讯云/阿里云分卡）+ 一键扫描云资源 + TAG/间隔/DNS 配置

![全局设置](./ReadmeAsset/settings.png)

**模拟测试**：按当前目标与规则计算变更预览，不实际写入云防火墙

![模拟测试](./ReadmeAsset/dry-run.png)

**同步日志**：历史记录 + 实时运行日志（与终端格式一致）

![同步日志](./ReadmeAsset/logs.png)

**告警配置**：邮件（SMTP）+ Webhook（钉钉/飞书/Slack）双通道

![告警配置](./ReadmeAsset/alerts.png)

---

## 快速上手（WebUI 模式，推荐）

无需任何配置文件，一个二进制即可开始：

### 第 1 步：获取程序

**方式 A：从源码编译**（需要 Go 1.25+）

```bash
make build
```

**方式 B：Docker 运行**（见下文「Docker 部署」）

### 第 2 步：启动

```bash
./fwalizer
```

启动后通过浏览器访问 `http://127.0.0.1:60200`（若端口被占用会自动选择可用端口，日志中会提示实际地址）。

### 第 3 步：完成首次配置（按引导提示顺序）

1. **全局设置**（左侧菜单 →「全局设置」）：
   - 填写云厂商 API 密钥：
     - 腾讯云：SecretId / SecretKey（[获取地址](https://console.cloud.tencent.com/cam/capi)，建议 CAM 子账号 + 最小权限）
     - 阿里云：AccessKeyId / AccessKeySecret（[获取地址](https://ram.console.aliyun.com/manage/ak)，建议 RAM 子账号 + 最小权限）
   - 每张凭据卡片内可选择云产品与地域，点击「扫描资源」自动列出该地域下的实例/安全组
   - 填写 `TAG`（规则标记前缀，默认 `auto-dns`）与同步间隔等，点击「保存」
2. **云资源管理**（左侧菜单 →「云资源管理」）：点击「添加目标」，选择云产品（腾讯云轻量云 / 腾讯云 CVM / 阿里云轻量云 / 阿里云 ECS）；资源 ID 可直接从扫描结果下拉选择（选择后自动填入所在地域），也可手动输入；地域支持下拉补全或自由输入，可先点「测试连接」验证配置
3. **域名规则**（左侧菜单 →「域名规则」）：点击「添加规则」，填写要同步的域名（如 `api.example.com`）、协议（TCP/UDP/TCP+UDP/ICMP）、端口、动作（ACCEPT/DROP）与适用目标，点击「保存」

### 第 4 步：验证与运行

- **仪表盘**：查看同步引擎状态（运行中/已暂停/已停止）、上次同步时间、统计概览（目标数/规则数/最近增删），可「立即同步」或「暂停/开启」同步（更多页面截图见上文「界面预览」）
- **模拟测试**（推荐先做）：进入「模拟测试」页 →「执行模拟测试」，**按当前目标与规则计算变更预览，不实际写入云防火墙规则**，确认无误后再开启同步
- **同步日志**：查看每次同步的历史记录（新增/删除计数、失败原因详情）与实时运行日志

> 💡 **提示**：首次使用若未配置任何密钥，仪表盘顶部会显示引导条，点击「去配置」直达全局设置。

---

## 运行模式

### WebUI 模式（默认，推荐所有用户）

未检测到 `TARGETS` 环境变量时自动进入。配置存储在 SQLite 数据库中，通过浏览器管理，修改后自动热重载。

- 默认地址：`http://127.0.0.1:60200`（仅绑定本机，端口被占用时自动在 50000–65535 随机选择）
- 数据路径（自动选择）：
  - macOS：`~/Library/Application Support/fwalizer/config.db`
  - Linux：`~/.config/fwalizer/config.db`
  - Windows：`%APPDATA%\fwalizer\config.db`
- 可通过 `FWALIZER_DATA_DIR` 环境变量自定义数据目录
- 页面一览：

| 页面 | 功能 |
|------|------|
| 仪表盘 | 同步引擎状态（大字+状态色）、上次同步、统计概览、操作中心（立即同步/暂停/开启） |
| 云资源管理 | 云资源目标增删改、弹窗内「测试连接」、资源 ID 按平台提示 + 扫描结果自动补全、资源-地域联动、未配置密钥时提示 |
| 域名规则 | 域名规则增删改、适用目标（中文云产品名）、每规则独立 IPv6 解析开关 |
| 全局设置 | 凭据卡片化（腾讯云/阿里云分卡）+ 一键扫描云资源（按厂商聚合展示）、TAG/间隔/DNS/日志级别、地域自动补全、配置导入导出（含凭据提示）、清空所有数据 |
| 同步日志 | 历史记录（新增/删除计数、failed 点击查看错误详情、清空/刷新记录）+ 实时运行日志（常驻展开） |
| 模拟测试 | 变更预览（按当前目标与规则计算，不实际写入）；连接测试保留在目标添加/编辑弹窗 |
| 告警配置 | 邮件（SMTP）+ Webhook（钉钉/飞书/Slack）告警 |

### .env 模式（备用 / 进阶 / 极简）

适合服务器、容器等无界面场景。当检测到 `TARGETS` 环境变量时自动进入（也可通过 `FWALIZER_MODE=env` 强制指定）。纯 headless 运行，日志输出到 stdout。

```bash
./fwalizer                    # 从 .env 加载配置并启动同步
./fwalizer validate .env      # 仅校验配置，不启动
./fwalizer version            # 显示版本号
./fwalizer backup             # 备份 WebUI 数据库
./fwalizer restore <文件>     # 从备份恢复数据库
```

最简单的 `.env`（复制 [.env.example](./.env.example) 后填写）：

```env
TARGETS=tc_lighthouse|lhins-abc123|ap-guangzhou

TC_ACCESS_ID=AKIDxxxxxxxx
TC_ACCESS_KEY=xxxxxxxx

RULES=api.example.com|TCP|443|ACCEPT||生产API
```

---

## 配置说明

### WebUI 模式

全部配置在浏览器中完成（见「快速上手」），无需手写配置文件；配置导入/导出在「全局设置」页提供（凭据字段不导出）。

### .env 模式变量表

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `TARGETS` | （必填） | 云资源目标列表，格式：`provider\|resource_id\|region`，逗号分隔 |
| `RULES` | （必填） | 域名规则列表，格式见「RULES 语法」 |
| `TAG` | `auto-dns` | 规则标记前缀，用于识别本工具创建的规则 |
| `INTERVAL` | `5m` | DNS 检查间隔（如 `30s`、`5m`、`1h`） |
| `LOG_LEVEL` | `info` | 日志级别：`debug` / `info` / `warn` / `error` |
| `SYNC_ENABLED` | `true` | 同步全局开关：`false` 时启动不执行同步（模拟测试不受影响） |
| `FWALIZER_MODE` | 自动检测 | 强制运行模式：`env` / `webui` |
| `DNS` | `223.5.5.5` | 上游 DNS 服务器地址（端口 :53 自动补全） |
| `DNS_TIMEOUT` | `10s` | DNS 解析超时时间 |
| `DNS_FAIL_THRESHOLD` | `5` | 连续失败多少次后触发熔断 |
| `TC_ACCESS_ID` / `TC_ACCESS_KEY` | （凭据） | 腾讯云 API 密钥（[获取地址](https://console.cloud.tencent.com/cam/capi)） |
| `ALI_ACCESS_ID` / `ALI_ACCESS_KEY` | （凭据） | 阿里云 AccessKey（[获取地址](https://ram.console.aliyun.com/manage/ak)） |
| `WEBUI_PORT` | `60200` | WebUI 监听端口（绑定 127.0.0.1） |
| `FWALIZER_DATA_DIR` | 各平台标准路径 | WebUI 数据存储目录（SQLite 数据库位置） |

### RULES 语法（.env 模式）

格式：`host|protocol|ports|action|targets|comment`

| 字段 | 说明 | 示例 |
|------|------|------|
| host | 要解析的域名 | `api.example.com` |
| protocol | 协议：`TCP` / `UDP` / `TCP+UDP` / `ICMP` | `TCP` |
| ports | 端口：单端口、逗号分隔、范围、`ALL` | `443,80` |
| action | 动作：`ACCEPT`（允许）/ `DROP`（拒绝） | `ACCEPT` |
| targets | 目标编号（从 0 开始，留空或 `*` = 全部） | `0,2` |
| comment | 可选备注 | `生产API` |

```env
# 单域名 + 多端口
RULES=api.example.com|TCP|443,80|ACCEPT||生产API

# 指定目标（仅第 2 个 TARGETS 条目，编号从 0 开始）
RULES=vpn.example.com|UDP|1194|ACCEPT|1|VPN接入

# 端口范围 + 多目标
RULES=game.example.com|TCP|8000-8010|ACCEPT|0,2|游戏端口

# ICMP（Ping），端口自动设为 ALL
RULES=ping.example.com|ICMP|ALL|ACCEPT||允许Ping

# TCP+UDP（仅阿里云 SWAS 原生支持，其他云自动拆分为两条规则）
RULES=voice.example.com|TCP+UDP|5060|ACCEPT||SIP语音
```

> **注意**：仅支持单台服务器场景（DNS 返回少量 IP），不支持 CDN 等返回大量 IP 的域名。

---

## CLI 数据备份与恢复

WebUI 模式下所有配置存储在 SQLite 数据库中，可通过 CLI 命令备份和恢复。**WebUI 模式启动时自动检测 pidfile，防止重复运行多个实例**（若已有实例运行会报错并退出）。

```bash
# 备份（自动生成时间戳文件名，保留最新 5 个）
./fwalizer backup

# 恢复（需先停止运行中的 FWAlizer）
./fwalizer restore config.db.bak.20260727_120000
```

> 备份和恢复仅适用于 WebUI 模式；`.env` 模式直接复制 `.env` 文件即可。

---

## 告警通知

在 WebUI 的「告警配置」页面中，可配置邮件（SMTP）和 Webhook 两种通知方式。启用后在发生同步错误或 DNS 解析失败时自动推送告警：

- **邮件告警**：支持标准 SMTP（如 QQ 邮箱、163 邮箱、企业邮箱）
- **Webhook 告警**：支持钉钉、飞书、Slack 三种渠道（在告警配置页选择「通知渠道」），自动适配各平台消息格式
- 告警配置修改后即时生效（热重载），无需重启

---

## 多云 API 权限与 API 文档

### 权限建议

使用子账号 + 最小权限策略。

**腾讯云（CAM 子账号）**

| 云产品 | 所需权限 |
|--------|----------|
| Lighthouse | `QcloudLighthouseFullAccess`（或自定义：`DescribeFirewallRules` + `CreateFirewallRules` + `DeleteFirewallRules`） |
| CVM（VPC） | `QcloudVPCFullAccess`（或自定义：`DescribeSecurityGroupPolicies` + `CreateSecurityGroupPolicies` + `DeleteSecurityGroupPolicies`） |

**阿里云（RAM 子账号）**

| 云产品 | 所需权限 |
|--------|----------|
| 轻量云（SWAS） | `AliyunSWASFullAccess`（或自定义：`swas:ListFirewallRules` + `swas:CreateFirewallRules` + `swas:DeleteFirewallRules`） |
| ECS | `AliyunECSFullAccess`（或自定义：`ecs:DescribeSecurityGroupAttribute` + `ecs:AuthorizeSecurityGroup` + `ecs:RevokeSecurityGroup`） |

### API 使用要求文档（PlatformAPIDocs/）

仓库 `PlatformAPIDocs/` 目录存放各云平台 **API 使用要求** 文档（参数格式、字段长度限制、频率限制等）：

| 目录 | 内容 |
|------|------|
| `PlatformAPIDocs/TencentLighthouseAPIGuide/` | 腾讯云轻量云防火墙 API 指南 |
| `PlatformAPIDocs/TencentCVMAPIGuide/` | 腾讯云 CVM 安全组 API 指南 |
| `PlatformAPIDocs/AliyunSWASAPIGuide/` | 阿里云轻量云（SWAS）API 指南 |
| `PlatformAPIDocs/AliyunECSAPIGuide/` | 阿里云 ECS 安全组 API 指南 |
| `PlatformAPIDocs/PlatformZoneGuide/` | 各平台地域与可用区指南（地域自动补全数据来源） |

---

## Docker 部署

### WebUI 模式运行（推荐）

```bash
docker pull ghcr.io/alcaprophet/fwalizer:latest

docker run -d --name fwalizer --restart=always \
  -p 60200:60200 \
  -e FWALIZER_DATA_DIR=/app/data \
  -v fwalizer-data:/app/data \
  ghcr.io/alcaprophet/fwalizer:latest
```

然后浏览器访问 `http://127.0.0.1:60200`。

### .env 模式运行（备用/进阶）

```bash
docker run -d --name fwalizer --restart=always \
  -v $(pwd)/.env:/app/.env:ro \
  ghcr.io/alcaprophet/fwalizer:latest
```

### docker-compose 示例

完整示例见 [docker-compose.yml.example](./docker-compose.yml.example)（默认推荐 WebUI 模式）：

```yaml
services:
  fwalizer:
    image: ghcr.io/alcaprophet/fwalizer:latest
    container_name: fwalizer
    restart: unless-stopped
    ports:
      - "60200:60200"              # WebUI 模式（推荐）
    volumes:
      - fwalizer_data:/app/data    # 数据持久化
    # .env 模式（备用）：挂载 .env 并注释 ports
    # volumes:
    #   - ./config/.env:/app/.env:ro
```

镜像会预先创建 `/app/data` 并将其授权给非 root 用户 `appuser`。首次创建的命名卷会继承该目录权限，容器无需以 root 用户运行。

如果命名卷曾由旧镜像创建，卷内目录可能仍属于 `root`，更新镜像不会自动改变已有卷的权限。请先停止服务，再执行一次无损权限修复（不会删除数据库）：

```bash
sudo docker compose stop fwalizer
sudo docker compose run --rm --user root --entrypoint chown fwalizer \
  -R appuser:appuser /app/data
sudo docker compose up -d
```

直接使用 `docker run` 且卷名为 `fwalizer-data` 时，可执行：

```bash
sudo docker stop fwalizer
sudo docker run --rm --user root \
  -v fwalizer-data:/app/data \
  --entrypoint chown \
  ghcr.io/alcaprophet/fwalizer:latest \
  -R appuser:appuser /app/data
sudo docker start fwalizer
```

不要使用 `docker compose down -v` 修复权限；该命令会删除命名卷及其中的 SQLite 数据。

### 本地构建镜像

```bash
make docker-build
docker run --rm fwalizer version
```

---

## 开发指南

### 目录结构

```
cloudhost-firewall-autoupdater/
├── main.go                  # 入口：模式判定 + 启动
├── app/                     # 应用生命周期（CLI、模式检测、日志初始化）
├── config/                  # 配置模型、.env 解析器、SQLite 存储、校验
├── dns/                     # DNS 解析器 + 熔断器
├── provider/                # 多云抽象层（接口 + 四家 Provider 实现）
├── syncer/                  # 同步引擎（主循环、重试、频率控制）
├── notifier/                # 事件总线 + 告警（邮件、Webhook）
├── webui/                   # WebUI 后端（HTTP API + 前端 embed）
│   └── frontend/            # Vue 3 + Vite + Naive UI 前端源码
├── internal/                # 内部工具（端口转换、标签解析）
├── ReadmeAsset/             # README 截图资源
├── PlatformAPIDocs/          # 各云平台 API 使用要求 + 地域可用区指南文档
├── HistoryDocs/             # 历史工程文档（Design1-3/Build1-4/Issue1-3，已存档）
├── Design4.md               # 当前设计记录
├── Build5.md                # 当前构建方案
├── Issue4.md                # 当前问题追踪
├── version/                 # 版本信息（ldflags 注入）
└── build/                   # Dockerfile
```

### 本地开发步骤

```bash
# 1. 克隆项目
git clone https://github.com/alcaprophet/cloudhost-firewall-autoupdater.git
cd cloudhost-firewall-autoupdater

# 2. 编译后端
make build

# 3. 运行测试
make test

# 4. 代码检查
make vet
```

### 前端开发

```bash
cd webui/frontend

# 安装依赖
npm install

# 启动开发服务器（热重载，代理 API 到 127.0.0.1:60200）
npm run dev

# 构建生产版本（输出到 dist/，go:embed 会自动包含）
npm run build
```

### 构建完整二进制（含前端）

```bash
cd webui/frontend && npm run build && cd ../..
make build
./fwalizer    # 访问 http://127.0.0.1:60200
```

---

## 常见问题（FAQ）

### 1. 首次启动后需要做什么？

按仪表盘引导条顺序：先在「全局设置」填写云厂商 API 密钥，再在「云资源管理」添加目标，最后在「域名规则」添加规则。推荐先用「模拟测试」预览变更，确认无误后再开启同步。

### 2. 如何确认规则是否同步成功？

打开「同步日志」页查看历史记录（每次同步的新增/删除计数与失败详情），或查看实时运行日志。`.env` 模式下查看 stdout 日志（默认 `info` 级别；`LOG_LEVEL=debug` 可查看详细解析与 Diff 过程）。

### 3. 本工具会不会删除我手动添加的防火墙规则？

**不会。** 本工具仅操作描述字段以 `[TAG]`（默认 `[auto-dns]`）开头的规则。手动创建的规则不带此标记，完全不受影响。

### 4. DNS 解析失败时会删除已有规则吗？

**不会。** DNS 解析失败时保留现有规则不变（仅记录 WARN 日志）。连续失败达到阈值（默认 5 次）后触发熔断，暂停该域名同步。**熔断后每轮同步仍会尝试一次半开探测**，解析成功后自动解除熔断并恢复正常同步，无需重启。

### 5. 支持 IPv6 吗？

腾讯云 Lighthouse、CVM 和阿里云 ECS 支持 IPv6（AAAA 记录）。阿里云轻量云（SWAS）不支持 IPv6，解析到的 IPv6 地址会自动跳过。WebUI 模式下每条域名规则可独立开关 IPv6 解析。

### 6. 阿里云 SWAS 支持 DROP 规则吗？

**不支持。** 阿里云轻量云的 `CreateFirewallRules` API 无 Policy 字段，创建的规则均为 accept。配置 DROP 时会记录 WARN 日志并跳过。

### 7. WebUI 模式如何切换为 .env 模式？

设置环境变量 `FWALIZER_MODE=env`，或确保 `TARGETS` 环境变量存在（程序会自动检测并进入 .env 模式）。

### 8. 如何备份和恢复 WebUI 配置？

使用 `./fwalizer backup` 备份 SQLite 数据库，`./fwalizer restore <文件>` 恢复。恢复前需先停止 FWAlizer 进程。

### 9. 如何配置告警通知？

启动 WebUI 模式后，在左侧菜单进入「告警配置」页面，填写 SMTP 或 Webhook 信息并启用即可。Webhook 支持在配置页选择「通知渠道」（钉钉/飞书/Slack），程序会自动适配各平台的消息格式。配置保存后即时生效。

### 10. 后端服务端口被占用怎么办？

默认端口 `60200` 被占用时，程序会自动在 `50000–65535` 范围内随机选择一个可用端口，并在日志中输出 WARN 提示和实际端口号。您也可以显式设置 `WEBUI_PORT` 环境变量指定其他端口。

### 11. WebUI 模式能否同时启动多个实例？

**不能。** 程序通过 pidfile（`<数据目录>/fwalizer.pid`）检测已有实例，若检测到另一个 FWAlizer 进程正在运行，会拒绝启动并提示 PID。这避免了多实例操作同一 SQLite 数据库可能引起的问题。

### 12. 如何切换明暗主题？

侧边栏顶部 FWAlizer 标题右侧的 ☀️/🌙 开关即可切换；主题偏好持久化保存，重启后保持。

### 13. 如何快速扫描并添加云资源？

在「全局设置」的云厂商凭据卡片中选择云产品与地域，点击「扫描资源」列出该地域下的实例/安全组（仅只读查询，不修改任何云端配置）。随后在「云资源管理」添加目标时，资源 ID 可直接从扫描结果下拉选择，所选资源的地域会自动联动填入；未扫描或无结果时也可手动输入任意资源 ID 与地域（地域支持下拉补全或自由输入，兼容云平台新区域）。

### 14. 清空所有数据会删除什么？

「全局设置」页底部的「清空所有数据」按钮（红色警告，需二次确认）会清空全部业务数据：目标、规则、凭据、同步日志与扫描结果，数据库回到全新初始化状态。此操作不可恢复，操作前请确认或先使用 `./fwalizer backup` 备份。

---

## 许可证

[MIT License](./LICENSE)
