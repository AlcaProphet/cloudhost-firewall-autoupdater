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
- **增量同步（先增后验）**：以一个云目标为单位汇总所需功能，**先新增并重读验证权限已生效，再经安全门条件清理陈旧规则**；Add 或覆盖验证失败时保留全部旧规则，绝不覆盖手动配置的防火墙规则
- **严格 TAG 命名空间**：只有描述等于 `[TAG]` 或以 `[TAG] `（右方一个空格）开头才由本工具管理；`[TAG]foo` 这类紧贴后缀的规则**不属于**本工具，永不被修改或删除
- **DNS 熔断保护**：连续解析失败达阈值后自动熔断，半开探测自动恢复，避免误删规则
- **乐观锁重试**：每次写入前重新拉取最新状态，最多 3 次指数退避重试
- **跨云并行**：不同云厂商并行同步，同厂商内串行（避免触发频率限制）
- **单二进制分发**：前端 WebUI 编译进二进制，无运行时依赖
- **Docker 就绪**：Alpine 基础镜像，非 root 运行，容器健康检查只认 HTTP `/api/health`（静态存活）；应用运行健康由 `/api/health/operational` 表达

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

**模拟测试**：按云目标汇总所需功能并给出**目标级**变更预览（所需功能、已由 TAG 满足、已由外部规则满足、待新增、清理候选与延后原因、冲突与 DNS 错误），不实际写入云防火墙

![模拟测试](./ReadmeAsset/dry-run.png)

**同步日志**：历史记录 + 实时运行日志（与终端格式一致）

![同步日志](./ReadmeAsset/logs.png)

**告警配置**：邮件（SMTP）+ Webhook（钉钉/飞书/Slack）双通道

![告警配置](./ReadmeAsset/alerts.png)

---

## 快速上手

无需任何配置文件，一个二进制即可开始：

### 第 1 步：获取程序

**方式 A：从源码编译**（需要 Go 1.27.1+，前端构建需要 Node.js；macOS 运行要求 13 或更新版本）

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

- **仪表盘**：查看同步引擎状态（运行中/已暂停/已停止）、上次同步时间和整轮统计概览（目标数/规则数/最近新增/删除/清理候选·已清理·残留）；同步健康提示只依据后端整轮结论——失败红色、部分实施（平台无法表达）黄色、**仅当权限已确认但陈旧规则清理延后时黄色提示**，无目标/无适用规则的空调度（idle）视为正常不再误报；可「立即同步」或「暂停/开启」同步（更多页面截图见上文「界面预览」）
- **模拟测试**（推荐先做）：进入「模拟测试」页 →「执行模拟测试」，**所有已配置目标各显示一张卡片**；有适用规则的目标读取一次云端快照并展示完整规划，无适用规则的目标明确标记为跳过且不访问云 API，全程不写入云防火墙规则；清理候选只是预览，正式同步还会在新快照上重新校验全部安全门后才可能删除，确认无误后再开启同步
- **同步日志**：查看每次同步的历史记录（新增/删除计数、失败原因详情）与实时运行日志
- **删除配置**：目标与规则的行内「删除」先展示对象摘要与影响说明，确认后才提交；取消、关闭、Esc 或点击遮罩不会删除，提交中不能取消。目标删除后不再由后续同步管理，已有云规则不会立即清理；规则删除可能让后续同步安全清理不再需要的 TAG 规则，但目标无适用规则时会跳过并保留已有云规则。被规则显式引用的目标须先修改引用。若提示删除结果不确定，请核对刷新后的列表；若提示配置已删除但刷新失败，请刷新页面核对。

---

## 运行方式与部署参数

### 唯一运行形态：WebUI + SQLite

程序启动后直接进入 WebUI + SQLite 模式：

- 默认地址：`http://127.0.0.1:60200`（仅绑定本机，端口被占用时由操作系统随机分配可用端口）
- 业务配置全部存储在 `<数据目录>/config.db` 中，通过浏览器管理，修改后自动热重载
- 启动时通过 pidfile（`<数据目录>/fwalizer.pid`）的 OS 非阻塞独占文件锁，防止同一数据目录多实例运行；文件内 PID 仅供诊断

数据目录（`FWALIZER_DATA_DIR` 未设置时）按平台自动选择：

| 平台 | 默认路径 |
|------|---------|
| macOS | `~/Library/Application Support/fwalizer/config.db` |
| Linux | `~/.config/fwalizer/config.db` |

启动会把最终数据目录收紧为 `0700`，数据库 `config.db`、已有 WAL/SHM 辅助文件和 `fwalizer.pid` 收紧为 `0600`；新建文件也使用同样的目标权限。任一权限操作失败都会停止启动，不继续监听 WebUI。只处理最终数据目录和这些已知文件，不递归修改父目录或目录中其他文件。

`FWALIZER_DATA_DIR` 应指向 FWAlizer 的专用目录，不要填写用户主目录、仓库根目录或其他共享目录。未显式指定且无法确定用户主目录时，程序会报错退出，不再回退到当前工作目录；显式设置专用数据目录的部署不依赖 `HOME`。

> **平台支持声明：** 本项目支持 **Linux 与 macOS**。**自本版本起不再支持 Windows**（Windows 专用的 pidfile 实现、`%APPDATA%` 数据目录分支与相关依赖已按用户决策移除；`GOOS=windows` 构建将按预期失败）。发布与 CI 面向 `linux/amd64`。

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
| 仪表盘 | 同步引擎状态（大字+状态色）、上次同步、目标级整轮统计概览（含清理候选/已清理/残留）与同步健康提示（5 秒轮询）、操作中心（立即同步/暂停/开启） |
| 云资源管理 | 云资源目标增删改、弹窗内「测试连接」、资源 ID 按平台提示 + 扫描结果自动补全、资源-地域联动、未配置密钥时提示 |
| 域名规则 | 域名规则增删改、适用目标（中文云产品名）、每规则独立 IPv6 解析开关 |
| 全局设置 | 凭据卡片化（腾讯云/阿里云分卡）+ 一键扫描云资源（按厂商聚合展示）、TAG/间隔/DNS/日志级别、配置导入导出、清空所有数据 |
| 同步日志 | 历史记录（新增/删除计数、failed 点击查看错误详情、清空/刷新记录）+ 实时运行日志（常驻展开） |
| 模拟测试 | 所有已配置目标各一张卡片；有适用规则时展示目标级变更预览（所需功能/已由 TAG 满足/已由外部满足/待新增/满足安全门后可清理/清理延后/无法实施/冲突/DNS 错误），无适用规则时明确标记跳过且不访问云 API；不实际写入，连接测试保留在目标添加/编辑弹窗 |
| 告警配置 | 触发条件（DNS 解析失败 / 同步最终失败 / 运行健康异常）、邮件（SMTP + 可编辑纯文本主题正文 + 测试发送）、Webhook（钉钉/飞书/Slack）、外部运行监控（Uptime Kuma Push） |

---

## 配置说明

全部业务配置在浏览器中完成（见「快速上手」），无需手写配置文件。SQLite 是唯一业务配置源，包括：

- 腾讯云、阿里云访问密钥；
- 云资源目标与域名规则；
- TAG、同步间隔、DNS 服务器、DNS 超时、DNS 失败阈值；
- 日志级别、同步开关、明暗主题；
- 邮件告警（SMTP 凭据、主题与纯文本正文）、Webhook 告警、告警触发条件与 Uptime Kuma Push 设置。

`sync_enabled`（同步开关）只由仪表盘的暂停/开启操作或配置导入修改；监听端口不属于业务配置。

### 配置导入导出

「全局设置」页提供配置导入/导出，协议为 **version 3 完整敏感快照**：

- 导出生成 `fwalizer-config-v3-<UTC时间>.json`（`POST /api/config/export`，`Cache-Control: no-store`）；
- 导出内容包含：目标、规则、全部设置、告警（触发策略 + 邮件主题/正文 + Webhook + Uptime Kuma Push），以及**腾讯云密钥、阿里云密钥、SMTP 密码、Webhook URL、Uptime Kuma Push URL**；
- 导入为**覆盖式原子替换**：目标、规则、设置、云凭据、告警与 Push 全部替换；规则通过配置包内的 `export_id → 新数据库 ID` 映射重建目标关联；
- 导入保留同步日志，清空扫描缓存，不重置 SQLite 自增序列；
- 导入是整库替换语义，因此会同时**重置 DNS 渐进式熔断的失败计数**（普通设置变更仍保留计数）；
- 只接受 version 3；version 1/2 及其他版本配置包会被拒绝（HTTP 400），不提供迁移或兼容；
- 导入要求顶层为单一 JSON 对象，全部必填字段明确提供；字段名大小写必须与导出文件一致，任意层级的重复字段（即使值相同）、未知字段、必填字段缺失或 `null`、非法 UTF-8/孤立代理项与尾随内容均拒绝（HTTP 400）。合法 JSON 转义按解码后的名称识别；正常空数组 `[]`、字段顺序和合法空白允许。导入上限为 10 MiB，超限返回 413；被拒绝的包不改变现有配置或运行时。
- 配置包是**配置迁移方式**（跨实例或重装后恢复业务配置），**不是运行中 SQLite 数据库的在线备份**：它不包含同步日志与扫描缓存，不等同于复制 `config.db`；
- 监听地址/端口、数据目录、pidfile 等部署状态不进入配置包。

> ⚠️ **安全警告（务必阅读）**
>
> version 3 配置包是**明文完整敏感快照**，安全等级等同于生产密钥或 SQLite 数据库备份：
>
> - **不要**将其提交到 Git（本仓库 `.gitignore` 已忽略 `fwalizer-config-v3-*.json`）；
> - **不要**上传到公共网盘、对象存储公开桶或聊天工具；
> - **不要**通过不可信渠道传输，建议使用加密通道或加密归档；
> - 导入会**覆盖当前全部业务配置**，包括现有云凭据与告警设置；
> - 本版本不提供配置包加密、签名或密钥托管。

> **注意**：仅支持单台服务器场景（DNS 返回少量 IP），不支持 CDN 等返回大量 IP 的域名。

---

## 告警通知与运行健康

「告警配置」页面按四张卡片组织，保存后即时生效（热重载），无需重启：

1. **触发条件**：DNS 解析失败、Provider × 域名最终失败、运行健康异常三个开关（**默认全部关闭**），以及健康超时 `health_timeout`（默认 `10m`）。三个开关是邮件与 Webhook **共用**的全局策略：只有「渠道开启 + 对应触发开启」才会发送通知。
2. **邮件告警**：标准 SMTP（如 QQ 邮箱、163 邮箱、企业邮箱），可编辑纯文本主题与正文；系统会在正文后追加固定事件详情（事件类型 / 时间 / Provider / 域名 / 错误，缺失字段写 `-`）；多收件人用逗号分隔。「测试发送邮件」按钮使用当前表单值直接测试（不保存、不写库、不改变订阅），结果只显示在页面上、刷新即消失；成功只表示 **SMTP 服务器已接受**，不代表已投递到收件箱。
3. **Webhook 告警**：钉钉、飞书、Slack 三种渠道，自动适配各平台消息格式并检查业务响应；HTTP 200 本身不足以判定成功。
4. **外部运行监控（Uptime Kuma Push）**：默认关闭；启用后按发送间隔主动上报当前运行状态。

### Webhook 响应与失败排查

| 渠道 | 成功响应条件 |
|------|--------------|
| 钉钉 | HTTP 2xx，JSON 中明确存在整数 `errcode=0` |
| 飞书 | HTTP 2xx，JSON 中存在整数 `code=0` 或旧字段 `StatusCode=0`；两者同时出现时必须都合法且为零 |
| Slack | HTTP 200，去除首尾空白后的响应正文恰好为 `ok` |

平台返回业务错误、未知或非法响应、读取超时或响应体超限时，会在实时日志中记录失败 WARN。日志仅保留渠道、错误类别和必要的状态码，不输出 Webhook URL、token 或平台返回的消息原文。可据业务码检查机器人关键词、签名、IP 白名单和启用状态。

成功判断表示渠道接口已明确接受请求；实际群聊接收仍需自行确认。失败不会自动重试或补发，Webhook 投递结果也不改变应用运行健康判定。

### 运行健康端点

| 端点 | 语义 | 用途 |
|------|------|------|
| `GET /api/health` | **静态存活**：只要 HTTP 服务可达就返回 200 `{"status":"ok"}` | Docker `HEALTHCHECK`；同步失败或运行健康异常都**不会**让它失败 |
| `GET /api/health/operational` | 应用工作状态：健康 200、异常 503（`Cache-Control: no-store`，响应只含稳定原因） | 外部监控（Uptime Kuma HTTP Monitor） |

运行健康由唯一的内部判定源计算：SQLite 探活（最多 2 秒）、同步引擎是否在运行、最近一轮是否 failed/partial、单轮是否超过 `health_timeout`、同步开启时是否调度停滞；暂停时只检查 SQLite 与主循环；空目标/空规则视为正常。30 秒内部监督器只在**健康→异常边沿**发送一次运行健康异常告警（持续异常不刷屏，恢复只写 INFO，恢复后再次异常会再告警一次）。

### Uptime Kuma 最小配置

**方式一：HTTP Monitor（推荐，Uptime Kuma 能主动访问 FWAlizer）**

1. 在 Uptime Kuma 新建 HTTP(s) Monitor；
2. URL 填 `http(s)://<FWAlizer地址>/api/health/operational`；
3. 期望状态码 200（运行异常时 FWAlizer 返回 503）。

**方式二：Push Monitor（FWAlizer 位于不可入站网络时）**

1. 在 Uptime Kuma 新建 Push Monitor，复制它给出的 Push URL（含 token）；
2. 在 FWAlizer「告警配置 → 外部运行监控」粘贴该 URL，设置发送间隔（默认 `60s`，最小 `20s`）；
3. 把 Uptime Kuma 的 Heartbeat Interval 设置为**大于**该发送间隔并留出余量（默认发送 60 秒时建议 120 秒）；
4. 保存后 FWAlizer 立即发送第一条心跳，之后按间隔上报 `up`/`down`；进程死亡或完全卡死时不再有心跳，由 Uptime Kuma 依据缺失心跳判定 DOWN。

> ⚠️ 真实 Uptime Kuma 的 HTTP 拉取与 Push DOWN/恢复通知属于人工验收项（见 [ProdTestList.md](./ProdTestList.md)），本地 mock 测试不等于真实外部验收。

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

如果命名卷曾由旧镜像创建，卷内目录或数据库可能仍属于 `root`。非 root 的 `appuser` 无权收紧权限时，程序会报告对应的权限错误并停止启动；更新镜像不会自动修复旧卷属主。请先停止服务，再执行一次无损属主修复（不会删除数据库）；重启后程序会收紧目录和已知文件的权限：

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
├── internal/                # 内部工具（端口转换、标签解析、运行健康 internal/health）
├── ReadmeAsset/             # README 截图资源
├── PlatformAPIDocs/         # 各云平台 API 使用要求 + 地域可用区指南文档
├── HistoryDocs/             # 历史工程文档（Design1-4/Build1-5/Issue1-4，共 13 份）
├── Design5.md               # 当前设计记录
├── Build7.md                # 当前构建方案（告警与运行健康）
├── Build6.md                # 已完成的历史构建记录
├── Issue5.md                # 问题追踪
├── Issue6.md                # 问题追踪（A1～A20 批次）
├── ProdTestList.md          # 待用户执行的真实外部人工验收清单
└── build/                   # Dockerfile
```

### 本地开发步骤

```bash
# 1. 克隆项目
git clone https://github.com/alcaprophet/cloudhost-firewall-autoupdater.git
cd cloudhost-firewall-autoupdater

# 2. 完整核验与构建（共享一次真实前端构建）
make all
```

`make test`、`make vet`、`make build` 均会先执行 `npm ci` 和前端生产构建，清洁检出可直接使用；需要 Go 1.27.1+、Node.js 和 npm。同一次 Make 调用中的多个目标共享一次前端构建，分别执行三次 Make 则会构建三次。`make -j4 all` 同样先完成前端，再并行执行 Go 检查、测试与构建；任一阶段失败会使 Make 非零退出。

日常只检查 Go 时，可以先执行 `make frontend`，随后直接运行 `go test ./... -race` 或 `go vet ./...`。裸 Go 命令不会生成前端产物；前端源码、配置或锁文件变化后应重新执行 `make frontend`。默认 DNS 测试通过本地随机回环 UDP 服务运行真实解析器，不转发查询；普通完整 Go 测试与默认 CI 不访问真实 DNS 上游，无需为了避开 DNS 使用全仓 `-short`。npm/Go 依赖准备仍可能访问网络，这不等同整个构建离线。

真实 DNS 上游检查使用专用 `dnsintegration` 标签，固定查询 `223.5.5.5:53`，结果与默认本地回归分别记录：

```bash
# 仅编译真实上游测试，不发送 DNS 查询（默认 CI 执行此项）
go test -tags=dnsintegration -run '^$' ./dns

# 显式真实上游验收；每次 DNS 调用 5 秒，命令总保护上限 20 秒
go test -race -tags=dnsintegration -run '^TestResolve_Upstream223$' -count=1 -timeout=20s ./dns
```

显式真实验收不能与 `-short` 联用，冲突时会在查询前失败。超时、网络错误、公共域名无有效地址或 `.invalid` 被成功解析均 FAIL，不自动 Skip 或切换上游；不固定公共域名的动态 IP，也不要求双栈。代理/TUN/Fake-IP 或出口限制可能使真实检查失败，应独立核对实际网络路径；默认本地回归或带标签编译通过都不能证明真实上游通过。

构建入口的本地替身回归可单独运行，不安装前端依赖、不执行真实 Go 测试：

```bash
sh build/makefile_test.sh
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

### 告警页行为回归

告警页的本地行为回归使用独立入口（先准备前端依赖）：

```bash
cd webui/frontend
npm run test:alerts
# 直接使用 Node 时也需保留超时参数：
node --test --test-timeout=30000 tests/alerts-load.test.mjs
```

每项测试有 30 秒预算；加载中保存和重复保存会在等待前检查请求次数，失败时也清理挂起的模拟请求。以命令退出码 0 判绿，超时可能计入 cancelled，不能只检查 `fail=0`。该预算不对应真实 SMTP 请求时限，也不是整套测试的 30 秒总上限；同步死循环和遗留活动句柄仍不属于原生超时的硬终止保证。其他前端测试入口未统一增加预算，Make、CI 与 Docker 当前也不自动执行这组行为回归。

### 构建完整二进制（含前端）

```bash
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

**不会。** 本工具只操作描述字段等于 `[TAG]` 或以 `[TAG] `（右方一个空格）开头的规则（默认 `[auto-dns]`）；`[TAG]foo` 这类紧贴后缀的描述**不属于**本工具，同样不会被修改或删除。手动创建的规则不带该标记，完全不受影响；非本工具的精确等价规则还可以只读地满足所需功能（例如你已手工放行了同一 IP 与端口），此时工具不会重复创建，也永远不会改动它。

### 4. DNS 解析失败时会删除已有规则吗？

**不会。** DNS 解析失败时保留现有规则不变（仅记录 WARN 日志）。连续失败达到阈值（默认 5 次）后触发熔断，暂停该域名同步。**熔断后每轮同步仍会尝试一次半开探测**，解析成功后自动解除熔断并恢复正常同步，无需重启。

### 5. 支持 IPv6 吗？

腾讯云 Lighthouse、CVM 和阿里云 ECS 支持 IPv6（AAAA 记录）。阿里云轻量云（SWAS）不支持 IPv6，解析到的 IPv6 地址会自动跳过。每条域名规则可独立开关 IPv6 解析。

### 6. 阿里云 SWAS 支持 DROP 规则吗？

**不支持。** 阿里云轻量云的 `CreateFirewallRules` API 无 Policy 字段，创建的规则均为 accept。配置 DROP 时会记录 WARN 日志并跳过。

### 7. 支持命令行参数吗？

**不支持。** 程序只接受零个参数：任意参数（包括 `--help`、`--version`）都会打印错误并以非零状态退出，不会启动 WebUI。所有操作都通过浏览器在 WebUI 中完成。

### 8. 如何备份和恢复配置？

使用「全局设置」页的「导出配置」与「导入配置」完成业务配置迁移。配置包是 version 3 明文完整敏感快照，**包含云凭据、SMTP 密码、Webhook URL 与 Uptime Kuma Push URL**，请按上文「配置导入导出」的安全警告妥善保管；导入会覆盖当前全部业务配置。version 1/2 配置包不再被接受（需重新在新版本导出后使用）。

需要注意：配置包是配置迁移方式，**不是 SQLite 数据库的在线备份**，不含同步日志与扫描缓存。若需要完整数据库备份，请停止 FWAlizer 后复制 `<数据目录>/config.db`。

#### 从旧版本升级：`rules.targets` 数据损坏的修复提示

早期版本的规则 `targets` 列在「适用于全部目标」时会写成字面量 `null`。当前版本对损坏值采取 **fail-closed**：`null`、SQL NULL、对象、标量、非整数数组或解析失败都会**中止同步/导出/启动加载**并返回安全错误（错误日志会带规则 ID），**不再**静默当作「适用于全部目标」。

这是**预期行为**，请按以下任一方式修复后再启动：

```bash
# 方式一：直接把损坏值改写为合法的空数组（= 适用于全部目标）
sqlite3 <数据目录>/config.db "UPDATE rules SET targets='[]' WHERE targets IS NULL OR targets='null';"

# 方式二：先手工核对 rules 表的目标引用，再重新导入 version 3 配置包
# （导入会先清空 rules 再写入，可顺带修复损坏库）
```

若某条规则确实只适用于部分目标，请改为显式整数数组（如 `[1,3]`），或直接在「域名规则」页重新保存该规则。

### 9. 如何配置告警通知？

在左侧菜单进入「告警配置」页面：先按需打开「触发条件」中的开关（默认全部关闭），再启用邮件或 Webhook 渠道并填写信息；邮件可编辑主题与正文，并可用「测试发送邮件」验证 SMTP 是否接受。也可在同一页启用「外部运行监控（Uptime Kuma Push）」。配置保存后即时生效；运行健康状态可访问 `GET /api/health/operational` 查看。

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

**同一数据目录不能同时运行多个实例。** 程序通过 `<数据目录>/fwalizer.pid` 上的 OS 独占文件锁进行互斥；锁竞争时立即拒绝启动，文件中的 PID 仅供诊断。不同数据目录使用独立的锁。

正常退出及 SIGKILL/OOM-kill 后，OS 会释放实例持有的锁，后续实例可直接重启。锁文件会保留，残留 PID 不表示实例仍在运行，**无需清理该文件；运行期间不得删除或替换锁文件**，否则可能因文件 inode 改变而破坏互斥。

升级旧 PID 判活版本前，请先停止旧实例；旧版本与文件锁版本混跑不保证互斥。文件锁支持 Linux/macOS 本地文件系统及底层为本地文件系统的 Docker 卷；NFS 等网络文件系统需单独验证。新建锁文件权限为 `0600`；取得锁后也会把已有锁文件收紧为 `0600`，失败则释放锁并停止启动。目录、数据库及 WAL/SHM 的权限同样在启动时迁移，锁文件保留和互斥语义不变。

### 13. 如何切换明暗主题？

侧边栏顶部 FWAlizer 标题右侧的 ☀️/🌙 开关即可切换；主题偏好持久化保存，重启后保持。

### 14. 如何快速扫描并添加云资源？

在「全局设置」的云厂商凭据卡片中选择云产品与地域，点击「扫描资源」列出该地域下的实例/安全组（仅只读查询，不修改任何云端配置）。随后在「云资源管理」添加目标时，资源 ID 可直接从扫描结果下拉选择，所选资源的地域会自动联动填入；未扫描或无结果时也可手动输入任意资源 ID 与地域（地域支持下拉补全或自由输入，兼容云平台新区域）。

### 15. 清空所有数据会删除什么？

「全局设置」页底部的「清空所有数据」按钮（红色警告，需二次确认）会清空全部业务数据：目标、规则、凭据、同步日志与扫描结果，数据库回到全新初始化状态。此操作不可恢复，操作前请确认或先用「导出配置」保存业务配置。

### 16. Docker 健康检查为什么是 unhealthy？

镜像与 Compose 的健康检查只请求 HTTP `/api/health`，不做进程存活检查。容器显示 `unhealthy` 说明 WebUI 没有正确提供服务（例如端口被占用、启动失败或 `WEBUI_PORT` 与 `healthcheck` 端口不一致），请查看 `docker logs` 排查。

补充：`/api/health` 是**静态存活**端点，因此一次同步失败或运行健康异常**不会**让容器变成 `unhealthy`（避免编排误重启）。应用工作状态请改用 `GET /api/health/operational`（异常返回 503）配合 Uptime Kuma 等外部监控观察。

---

## 许可证

[MIT License](./LICENSE)
