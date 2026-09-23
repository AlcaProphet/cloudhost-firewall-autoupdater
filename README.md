# FWAlizer — 防火墙 DNS 自动同步工具

**FWAlizer**（Firewall DNS Synchronizer）是一个轻量级自动化工具：定时解析指定域名的 IP 地址，自动同步到云防火墙/安全组白名单中。专为域名 IP 频繁变动的场景设计（如动态 DNS、API 网关、VPN 入口）。

> FWAlizer 只有一种运行形态：**WebUI 单二进制 + SQLite**。程序可运行在 Linux 服务器、容器或其他无图形桌面环境，通过浏览器访问 WebUI 完成全部业务配置；不存在 `.env` Headless 模式，也不提供 CLI 子命令。

![仪表盘](./ReadmeAsset/dashboard.png)

---

## 目录

- [核心特性](#核心特性)
- [界面预览](#界面预览)
- [快速上手](#快速上手)
- [运行方式与部署参数](#运行方式与部署参数)
- [配置说明](#配置说明)
- [告警通知](#告警通知)
- [多云 API 权限与 API 文档](#多云-api-权限与-api-文档)
- [Docker 部署](#docker-部署)
- [开发指南](#开发指南)
- [常见问题（FAQ）](#常见问题faq)

---

## 核心特性

- **多云支持**：腾讯云 Lighthouse / CVM，阿里云轻量云（SWAS）/ ECS，四款云产品统一管控
- **WebUI 可视化管理**：浏览器完成全部配置，明暗主题、仪表盘状态卡片、同步日志实时查看、模拟测试预览
- **SQLite 单一配置源**：目标、规则、云凭据、同步/DNS/TAG/日志/主题设置与告警全部只通过 WebUI 管理，不存在环境变量 override
- **资源扫描与自动补全**：设置页一键扫描云厂商资源（实例/安全组），添加目标时自动补全资源 ID 并联动填入地域
- **地域自动补全**：内置四平台地域数据，添加目标时可下拉过滤选择，也可自由输入任意地域（兼容新区域）
- **增量同步**：仅操作带 `[TAG]` 标记的规则，绝不覆盖手动配置的防火墙规则
- **DNS 熔断保护**：连续解析失败达阈值后自动熔断，半开探测自动恢复，避免误删规则
- **乐观锁重试**：每次写入前重新拉取最新状态，最多 3 次指数退避重试
- **跨云并行**：不同云厂商并行同步，同厂商内串行（避免触发频率限制）
- **单二进制分发**：前端 WebUI 编译进二进制，无运行时依赖
- **Docker 就绪**：Alpine 基础镜像，非 root 运行，健康检查只认 HTTP `/api/health`

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

## 快速上手

无需任何配置文件，一个二进制即可开始：

### 第 1 步：获取程序

**方式 A：从源码编译**（需要 Go 1.25+，前端构建需要 Node.js）

```bash
make build
```

**方式 B：Docker 运行**（见下文「Docker 部署」）

### 第 2 步：启动

```bash
./fwalizer
```

启动后通过浏览器访问 `http://127.0.0.1:60200`（若端口被占用会自动选择可用端口，日志中会提示实际地址）。

程序不接受任何命令行参数：包括 `--help`、`--version` 在内的任意参数都会打印错误并以非零状态退出，不会启动 WebUI。

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

---

## 运行方式与部署参数

### 唯一运行形态：WebUI + SQLite

程序启动后直接进入 WebUI + SQLite 模式：

- 默认地址：`http://127.0.0.1:60200`（仅绑定本机，端口被占用时由操作系统随机分配可用端口）
- 业务配置全部存储在 `<数据目录>/config.db` 中，通过浏览器管理，修改后自动热重载
- 启动时通过 pidfile（`<数据目录>/fwalizer.pid`）检测已有实例，防止多实例运行

数据目录（`FWALIZER_DATA_DIR` 未设置时）按平台自动选择：

| 平台 | 默认路径 |
|------|---------|
| macOS | `~/Library/Application Support/fwalizer/config.db` |
| Linux | `~/.config/fwalizer/config.db` |
| Windows | `%APPDATA%\fwalizer\config.db` |

### 仅有的三个部署参数

以下环境变量只在进程启动时读取一次，**不写入 SQLite、不进入配置包、不被配置导入覆盖，也不参与热重载**：

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `FWALIZER_DATA_DIR` | 平台标准路径 | SQLite、pidfile 与持久化数据目录；空白值按未设置处理 |
| `WEBUI_HOST` | `127.0.0.1` | WebUI 监听地址；容器、局域网或反向代理访问时设为 `0.0.0.0` |
| `WEBUI_PORT` | `60200` | WebUI 监听端口；必须是 `1～65535` 的十进制整数，非法值启动失败 |

`TARGETS`、`RULES`、`TC_ACCESS_ID`、`INTERVAL`、`DNS`、`LOG_LEVEL` 等业务环境变量已不存在；即使进程中意外存在同名变量，也会被忽略，不会改变 SQLite 配置或切换运行模式。

### 页面一览

| 页面 | 功能 |
|------|------|
| 仪表盘 | 同步引擎状态（大字+状态色）、上次同步、统计概览、操作中心（立即同步/暂停/开启） |
| 云资源管理 | 云资源目标增删改、弹窗内「测试连接」、资源 ID 按平台提示 + 扫描结果自动补全、资源-地域联动、未配置密钥时提示 |
| 域名规则 | 域名规则增删改、适用目标（中文云产品名）、每规则独立 IPv6 解析开关 |
| 全局设置 | 凭据卡片化（腾讯云/阿里云分卡）+ 一键扫描云资源（按厂商聚合展示）、TAG/间隔/DNS/日志级别、配置导入导出、清空所有数据 |
| 同步日志 | 历史记录（新增/删除计数、failed 点击查看错误详情、清空/刷新记录）+ 实时运行日志（常驻展开） |
| 模拟测试 | 变更预览（按当前目标与规则计算，不实际写入）；连接测试保留在目标添加/编辑弹窗 |
| 告警配置 | 邮件（SMTP）+ Webhook（钉钉/飞书/Slack）告警 |

---

## 配置说明

全部业务配置在浏览器中完成（见「快速上手」），无需手写配置文件。SQLite 是唯一业务配置源，包括：

- 腾讯云、阿里云访问密钥；
- 云资源目标与域名规则；
- TAG、同步间隔、DNS 服务器、DNS 超时、DNS 失败阈值；
- 日志级别、同步开关、明暗主题；
- 邮件告警、SMTP 凭据与 Webhook 告警。

`sync_enabled`（同步开关）只由仪表盘的暂停/开启操作或配置导入修改；监听端口不属于业务配置。

### 配置导入导出

「全局设置」页提供配置导入/导出：

- 导出生成 JSON 配置文件（含 `version`、目标、规则与设置）；
- 导入为覆盖式：替换当前全部目标、规则与设置；
- 配置文件中**不包含云厂商凭据**，导入后需要重新填写凭据；
- 配置包是**配置迁移方式**（跨实例或重装后恢复业务配置），**不是运行中 SQLite 数据库的在线备份**：它不包含同步日志与扫描缓存，不等同于复制 `config.db`；
- 监听地址/端口、数据目录、pidfile 等部署状态不进入配置包。

> **注意**：仅支持单台服务器场景（DNS 返回少量 IP），不支持 CDN 等返回大量 IP 的域名。

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

```bash
docker pull ghcr.io/alcaprophet/fwalizer:latest

docker run -d --name fwalizer --restart=always \
  -p 60200:60200 \
  -e WEBUI_HOST=0.0.0.0 \
  -e FWALIZER_DATA_DIR=/app/data \
  -v fwalizer-data:/app/data \
  ghcr.io/alcaprophet/fwalizer:latest
```

然后可在宿主机访问 `http://127.0.0.1:60200`，或在防火墙允许的前提下通过 `http://<宿主机IP>:60200` 访问。
如果只允许同机反向代理访问，将端口映射改为 `-p 127.0.0.1:60200:60200`。

容器只需要三个部署变量（镜像已默认 `WEBUI_HOST=0.0.0.0`、`WEBUI_PORT=60200`、`FWALIZER_DATA_DIR=/app/data`）。如需修改监听端口，除设置 `WEBUI_PORT` 外还需同步修改 Compose 的 `healthcheck` 端口与端口映射。

### docker-compose 示例

完整示例见 [docker-compose.yml.example](./docker-compose.yml.example)：

```yaml
services:
  fwalizer:
    image: ghcr.io/alcaprophet/fwalizer:latest
    container_name: fwalizer
    restart: unless-stopped
    environment:
      - FWALIZER_DATA_DIR=/app/data
      - WEBUI_HOST=0.0.0.0
      - WEBUI_PORT=60200
    ports:
      - "60200:60200"              # 仅同机反代可改为 127.0.0.1:60200:60200
    volumes:
      - fwalizer_data:/app/data    # 数据持久化（SQLite）
    healthcheck:
      test: ["CMD-SHELL", "wget -q -O /dev/null \"http://127.0.0.1:${WEBUI_PORT:-60200}/api/health\" || exit 1"]
```

镜像会预先创建 `/app/data` 并将其授权给非 root 用户 `appuser`。首次创建的命名卷会继承该目录权限，容器无需以 root 用户运行。

镜像自带 `HEALTHCHECK`，只请求 `http://127.0.0.1:<WEBUI_PORT>/api/health`；**不使用进程存活检查**，因此进程仍在但 WebUI 不可用时会如实判为 `unhealthy`。

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
```

---

## 开发指南

### 目录结构

```
cloudhost-firewall-autoupdater/
├── main.go                  # 入口：调用 run() 并返回退出码
├── run.go                   # 唯一运行形态的启动流程（部署参数 → SQLite → WebUI → Syncer）
├── app/                     # 日志初始化（stdout + WebUI 日志流多路复用）
├── config/                  # 部署参数、业务配置模型、SQLite 存储
├── dns/                     # DNS 解析器 + 熔断器
├── provider/                # 多云抽象层（接口 + 四家 Provider 实现）
├── syncer/                  # 同步引擎（主循环、重试、频率控制）
├── notifier/                # 事件总线 + 告警（邮件、Webhook）
├── webui/                   # WebUI 后端（HTTP API + 前端 embed）
│   └── frontend/            # Vue 3 + Vite + Naive UI 前端源码
├── internal/                # 内部工具（端口转换、标签解析）
├── ReadmeAsset/             # README 截图资源
├── PlatformAPIDocs/         # 各云平台 API 使用要求 + 地域可用区指南文档
├── HistoryDocs/             # 历史工程文档（Design1-4/Build1-5/Issue1-4，共 13 份）
├── Design5.md               # 当前设计记录
├── Build6.md                # 当前构建方案
├── Issue5.md                # 当前问题追踪
└── build/                   # Dockerfile
```

### 本地开发步骤

```bash
# 1. 克隆项目
git clone https://github.com/alcaprophet/cloudhost-firewall-autoupdater.git
cd cloudhost-firewall-autoupdater

# 2. 编译后端（含前端构建）
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

打开「同步日志」页查看历史记录（每次同步的新增/删除计数与失败详情），或查看实时运行日志（与终端 stdout 格式一致）。

### 3. 本工具会不会删除我手动添加的防火墙规则？

**不会。** 本工具仅操作描述字段以 `[TAG]`（默认 `[auto-dns]`）开头的规则。手动创建的规则不带此标记，完全不受影响。

### 4. DNS 解析失败时会删除已有规则吗？

**不会。** DNS 解析失败时保留现有规则不变（仅记录 WARN 日志）。连续失败达到阈值（默认 5 次）后触发熔断，暂停该域名同步。**熔断后每轮同步仍会尝试一次半开探测**，解析成功后自动解除熔断并恢复正常同步，无需重启。

### 5. 支持 IPv6 吗？

腾讯云 Lighthouse、CVM 和阿里云 ECS 支持 IPv6（AAAA 记录）。阿里云轻量云（SWAS）不支持 IPv6，解析到的 IPv6 地址会自动跳过。每条域名规则可独立开关 IPv6 解析。

### 6. 阿里云 SWAS 支持 DROP 规则吗？

**不支持。** 阿里云轻量云的 `CreateFirewallRules` API 无 Policy 字段，创建的规则均为 accept。配置 DROP 时会记录 WARN 日志并跳过。

### 7. 支持命令行参数吗？

**不支持。** 程序只接受零个参数：任意参数（包括 `--help`、`--version`）都会打印错误并以非零状态退出，不会启动 WebUI。所有操作都通过浏览器在 WebUI 中完成。

### 8. 如何备份和恢复配置？

使用「全局设置」页的「导出配置」与「导入配置」完成业务配置迁移。配置文件不包含云厂商凭据，导入后需重新填写凭据。

需要注意：配置包是配置迁移方式，**不是 SQLite 数据库的在线备份**，不含同步日志与扫描缓存。若需要完整数据库备份，请停止 FWAlizer 后复制 `<数据目录>/config.db`。

### 9. 如何配置告警通知？

在左侧菜单进入「告警配置」页面，填写 SMTP 或 Webhook 信息并启用即可。Webhook 支持在配置页选择「通知渠道」（钉钉/飞书/Slack），程序会自动适配各平台的消息格式。配置保存后即时生效。

### 10. 如何通过局域网或反向代理访问 WebUI？

直接运行时默认只监听 `127.0.0.1`。需要通过主机 IP 访问，或反向代理与 FWAlizer 不在同一网络命名空间时，设置 `WEBUI_HOST=0.0.0.0`。此时请同时通过主机防火墙、VPN 或带身份验证的反向代理限制访问。

```bash
WEBUI_HOST=0.0.0.0 WEBUI_PORT=60200 ./fwalizer
```

Docker 需在容器内设置 `WEBUI_HOST=0.0.0.0`。宿主机的暴露范围由端口映射决定：`-p 60200:60200` 允许通过宿主机网卡访问，`-p 127.0.0.1:60200:60200` 则只允许宿主机本地反向代理访问。

### 11. 后端服务端口被占用怎么办？

默认端口 `60200` 被占用（`EADDRINUSE`）时，程序会由操作系统随机分配一个可用端口，并在日志中输出 WARN 提示和实际端口号。您也可以显式设置 `WEBUI_PORT` 环境变量指定其他端口。Docker 端口映射不会跟随容器内的随机端口，建议为容器保留专用的固定端口。

只有“地址已被占用”才会触发随机端口；权限不足、监听地址非法或地址不可用等其他监听错误不会被掩盖，程序会输出 `WebUI 监听失败` 并以非零状态退出（此时不会启动同步引擎）。

### 12. 能否同时启动多个实例？

**不能。** 程序通过 pidfile（`<数据目录>/fwalizer.pid`）检测已有实例，若检测到另一个 FWAlizer 进程正在运行，会拒绝启动并提示 PID。这避免了多实例操作同一 SQLite 数据库可能引起的问题。

### 13. 如何切换明暗主题？

侧边栏顶部 FWAlizer 标题右侧的 ☀️/🌙 开关即可切换；主题偏好持久化保存，重启后保持。

### 14. 如何快速扫描并添加云资源？

在「全局设置」的云厂商凭据卡片中选择云产品与地域，点击「扫描资源」列出该地域下的实例/安全组（仅只读查询，不修改任何云端配置）。随后在「云资源管理」添加目标时，资源 ID 可直接从扫描结果下拉选择，所选资源的地域会自动联动填入；未扫描或无结果时也可手动输入任意资源 ID 与地域（地域支持下拉补全或自由输入，兼容云平台新区域）。

### 15. 清空所有数据会删除什么？

「全局设置」页底部的「清空所有数据」按钮（红色警告，需二次确认）会清空全部业务数据：目标、规则、凭据、同步日志与扫描结果，数据库回到全新初始化状态。此操作不可恢复，操作前请确认或先用「导出配置」保存业务配置。

### 16. Docker 健康检查为什么是 unhealthy？

镜像与 Compose 的健康检查只请求 HTTP `/api/health`，不做进程存活检查。容器显示 `unhealthy` 说明 WebUI 没有正确提供服务（例如端口被占用、启动失败或 `WEBUI_PORT` 与 `healthcheck` 端口不一致），请查看 `docker logs` 排查。

---

## 许可证

[MIT License](./LICENSE)
