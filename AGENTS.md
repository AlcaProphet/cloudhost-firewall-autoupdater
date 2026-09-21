# AGENTS.md — FWAlizer AI 编码指令

> 本文档是给 AI 编码助手的指令集，也是项目**唯一的强要求文档**（详见「十二、文档体系与优先级」）。
> 项目设计方向见 [Design4.md](./Design4.md)（设计记录，当前），当前构建方案见 [Build5.md](./Build5.md)，当前问题记录见 [Issue4.md](./Issue4.md)；历史文档（Design1-3、Build1-4、Issue1-3）已移入 [HistoryDocs/](./HistoryDocs/)（已存档，仅记录，不再用于构建，仅用于核查等情况）。

---

## 一、项目基本信息

- **模块路径**：`github.com/alcaprophet/cloudhost-firewall-autoupdater`
- **仓库名称**：`cloudhost-firewall-autoupdater`
- **产品与兼容标识**：产品显示名、二进制名、环境变量前缀、数据目录及 GHCR 镜像继续使用 `FWAlizer` / `fwalizer`，避免破坏现有部署
- **Go 版本**：`go 1.25`
- **文档定位与优先级**：编码前先阅读本文件（强要求）。设计记录见 [Design4.md](./Design4.md)（当前，非强制，供参考）；详细构建方案见 [Build5.md](./Build5.md)（当前）；当前问题记录见 [Issue4.md](./Issue4.md)；历史文档（Design1-3、Build1-4、Issue1-3）见 [HistoryDocs/](./HistoryDocs/)（已存档，仅记录，不再用于构建，仅用于核查等情况）

---

## 二、核心编码原则

### 简单轻量化

- 功能做减法，不引入不必要的抽象和重型框架
- 优先使用 Go 标准库（`net/http`、`encoding/json`、`time.Ticker`、`log/slog`）
- 不使用 gin/echo 等 HTTP 框架，不使用外部 cron 库
- 单二进制分发，无运行时依赖

### 不过度防御

- 聚焦核心场景，不对极端边界做过度防御性编程
- 合理假设输入有效性，不做无意义的 nil 检查链
- 错误处理到位即可，不堆叠冗余的 fallback 逻辑

### 安全设计（内部使用导向）

- 项目以**内部使用**为设计前提，WebUI 不针对公开访问设计
- 网络安全边界由用户自己控制（防火墙、VPN、反向代理等）
- WebUI 默认绑定 `127.0.0.1`，可通过 `WEBUI_HOST` 配置监听地址（Docker 内使用 `0.0.0.0`）；端口通过 `WEBUI_PORT` 配置（默认 `60200`，若被占用则由 OS 随机选择可用端口；参见 `webui/server.go` 的 `findAvailablePort`）
- Docker 用户通过 `-p` 自行决定暴露范围
- 凭据通过独立环境变量传入，不与资源声明混合

### 开箱即用

- 最小化前置依赖，首次运行即可工作
- 配置有合理默认值，无必填项阻塞启动
- WebUI 模式启动后日志提示访问地址，浏览器手动访问即可

### 不确定时主动提问

- 遇到模糊需求、多种可行方案或技术取舍时，使用提问工具询问用户
- 提问时**必须附上推荐选项**并简要说明理由
- 不要在假设下自行决定关键设计

---

## 三、防火墙规则操作约束

- **绝不**使用全量覆盖类 API（如 Lighthouse 的 `ModifyFirewallRules`、CVM 的“重置安全组规则”），会误删非本工具管理的规则
- **只用**增量添加 + 精确删除（各云对应 API 详见 HistoryDocs/Build1.md 第五节）
- 所有由本工具创建的规则，通过对应的描述字段标记，格式：
  ```
  [TAG] comment
  ```
  示例：`[auto-dns] 生产API`
- 不同云厂商的规则标识字段不同（详见 HistoryDocs/Build1.md），均以 `[TAG]` 前缀识别
- 删除时“规则已不存在”视为成功（幂等），不报错
- 添加时“规则已存在”视为成功，WARN 日志并跳过
- 支持协议：TCP / UDP / TCP+UDP / **ICMP**（ICMP 时端口由各 Provider 按 API 要求处理：Lighthouse 传 ALL，阿里云传 -1/-1，CVM 省略 Port 字段）
- **TCP+UDP 协议拆分：** 仅阿里云 SWAS 原生支持 TCP+UDP，Lighthouse/CVM/ECS 均不支持，由 `buildDesired()` 自动拆分为 TCP + UDP 两条规则
- **IPv6+ICMP 处理：** Lighthouse 使用 ICMPv6 协议，CVM 使用 ICMPV6 协议，ECS 不支持（AuthorizeSecurityGroup 无 ICMPv6，直接跳过并 WARN）
- 端口格式：单端口、逗号分隔、范围（`8000-8010`）、`ALL`
- 腾讯云 CVM 安全组规则上限 **100 条**，接近上限时停止新增并告警

---

## 四、DNS 解析约束

- 使用自定义 DNS 服务器（`DNS` 环境变量指定）
- 通过 Go `net.Resolver` 的 `Dial` 函数指定上游 DNS
- 同时解析 **A 记录（IPv4）** 和 **AAAA 记录（IPv6）**
- IPv4 → `CidrBlock` 字段（格式 `1.2.3.4/32`）
- IPv6 → `Ipv6CidrBlock` 字段（格式 `2001:db8::1/128`）
- 域名解析失败：记录 WARN 日志，保留现有规则不变（不删除）
- 超时统一为 **10s**（连接 + 整体，可通过 `DNS_TIMEOUT` 配置）
- 渐进式熔断：连续失败达阈值后熔断，半开状态每轮探测一次，成功后解除
- ⚠️ 仅支持单台服务器场景（少量 IP），不支持 CDN 等返回大量 IP 的域名

---

## 五、同步调度约束

- 使用 `time.Ticker`，不依赖外部 cron
- 间隔由 `INTERVAL` 环境变量控制（如 `5m`、`30m`、`1h`）
- 优雅退出：收到 `SIGTERM`/`SIGINT` 后，完成当前轮次再退出
- 支持配置热重载（WebUI 修改后通过 channel 通知 Syncer）
- 同步全局开关（`SYNC_ENABLED`/`sync_enabled`，默认 true）：暂停时 ticker 与手动 trigger 均不触发同步；模拟测试与连接测试不受影响（独立于 Run() 主循环）

---

## 六、乐观锁与重试

- 每次写入前重新拉取最新规则状态（Describe → Diff → Create/Delete）
- 不传入版本号参数（Lighthouse 的 `FirewallVersion`、CVM 的 `Version`，由云 API 自行管理）
- 写入失败自动重试（最多 3 次，指数退避）
- 重试时重新走完整流程：Describe → Diff → Create/Delete

---

## 七、API 频率限制

- 不同云厂商频率限制不同，取对应间隔（详见 HistoryDocs/Build1.md）
- 同一云厂商内串行处理（共享配额），域名之间加入间隔
- 不同云厂商可并行同步（API 配额独立）

---

## 八、Docker 约束

- 基础镜像：`alpine:3.20`
- 编译镜像：`golang:1.25-alpine`
- `CGO_ENABLED=0` 静态编译（Docker 构建）
- 非 root 用户运行（`adduser -D appuser`）
- 日志输出到 stdout（Text 格式，`docker logs` 查看）
- 支持 `HEALTHCHECK`（WebUI 模式用 HTTP 端点，`.env` 模式用进程检测）

---

## 九、配置约束

- `.env` 文件**不提交 Git**
- 提供 `.env.example` 模板
- 密钥使用云厂商 **CAM 子账号 + 最小权限**
- 凭据按云厂商独立环境变量（不嵌入 TARGETS）

---

## 十、CI/CD 约束

- 推送 tag（如 `v1.0.0`）时自动构建 Docker 镜像推送到 **ghcr.io**
- 镜像命名：`ghcr.io/alcaprophet/fwalizer:<tag>`
- 构建平台：`linux/amd64`
- PR 时仅编译检查，不推送镜像
- **SDK 版本策略（有意设计）**：腾讯云/阿里云要求使用较新的 SDK，使用过期 SDK 无法调用新接口/功能；因此每次构建前执行 `go get -u` 将云厂商 SDK 升级到最新（见 `docker-publish.yml`「更新所有 SDK 到最新版」步骤），镜像始终携带最新 SDK，SDK 可复现性要求服从该策略

---

## 十一、代码规范

- **所有 error 必须处理**，不可忽略返回值
- 日志使用 `log/slog`（Go 1.21+ 内置结构化日志）
- 注释使用**中文**（面向国内开发者）
- 遵守 `PlatformAPIDocs/` 中的 API 文档要求（参数格式、字段长度限制、频率限制）
- 多云抽象基于 Provider 接口 + 工厂注册模式（详见 HistoryDocs/Build1.md）
- 项目交付范围仅包含 WebUI 单二进制、`.env` headless 与 Docker；不包含桌面托盘、开机自启或原生桌面打包计划
- 日志多路复用器 `MultiHandler` 统一定义在 `app/logutil.go`（消除与 `webui/api/logstream.go` 的重复）
- WebUI 模式通过 pidfile（`config/pidfile.go` + 平台文件）防止多实例运行
- 事件类型：全局同步完成用 `EventSyncComplete`，逐域名同步完成用 `EventDomainSyncComplete`（定义于 `notifier/bus.go`）
- 同步全局开关：`POST /api/sync/pause|resume` 端点（先写 DB 后通知 Syncer）；`SyncStatus.enabled` 字段；前端「模拟测试」页（路由 `/dry-run`）承载变更预览（`DryRunResponse{results, warnings}` 包装、`to_add`/`to_delete` 为规则明细数组）；连接测试保留在目标添加/编辑弹窗（`POST /api/test-connection`）
- 地域自动补全：数据源为 `PlatformAPIDocs/PlatformZoneGuide/`（后端 `webui/api/zones.go` 提供 `GET /api/zones`，文档更新时需同步数据）；后端仅提供预填数据、不校验地域合法性（允许输入列表外值，由云 API 自行报错，符合「不过度防御」）
- 资源扫描：`provider/scan.go` 实现四平台只读列表查询（Lighthouse `DescribeInstances`、SWAS `ListInstances`、CVM/ECS `DescribeSecurityGroups`），`webui/api/scan.go` 提供 `POST /api/scan-resources`（凭据缺失快速失败）、`GET/DELETE /api/scanned-resources`；结果按 cloud_type+region 覆盖式入库（`scanned_resources` 表），仅供添加目标自动补全，同步流程不依赖
- 清空所有数据：`POST /api/config/reset` 调 `Store.ResetAll()` 清空全部业务表（targets/rules/settings/sync_logs/alert_email/alert_webhook/scanned_resources），等效重新初始化；前端入口需红色警告按钮 + 卡片式二次确认
- 前端 UI 规范：全局字号 16px、页面级操作按钮统一 `size="large"`（44px，`App.vue` themeOverrides 按分尺寸变量覆盖）；表格内操作按钮（编辑/删除）保持小号；所有二次确认使用 `NModal preset="card"` 卡片式弹窗（危险操作确认按钮 `type="error"`）
- 资源 ID 输入提示：按云类型区分文案（轻量云=实例 ID，CVM/ECS=安全组 ID），由 `constants.ts` 的 `resourceIdHint()` 统一承载（仅 placeholder，不引入额外说明块）

---

## 十二、文档体系与优先级

### 12.1 文档定位与优先级（本文件为唯一强要求）

| 文档类型 | 文件 | 定位 | 约束力 |
|---------|------|------|--------|
| **强要求** | **AGENTS.md（本文件）** | AI 编码指令与约束 | **唯一强要求，尽量不违背** |
| 设计构想 | [Design4.md](./Design4.md)（当前）；历史：[HistoryDocs/](./HistoryDocs/)（Design1-3 已存档） | 设计大方向、架构构想、决策记录 | 非强制，供参考 |
| 构建方案 | [Build5.md](./Build5.md)（当前）；历史：[HistoryDocs/](./HistoryDocs/)（Build1-4 已存档） | 详细的分步构建方案与验收命令 | 非强制，执行建议 |
| 问题记录 | [Issue4.md](./Issue4.md)（当前）；历史：[HistoryDocs/](./HistoryDocs/)（Issue1-3 已存档） | 记录的错误与修复方案 | 非强制，经验参考 |

**执行规则：**

- 只有 **AGENTS.md** 是强要求文档，其他类型文档均为**设计取向，不是强规则**，不需要严格遵守
- **Design 文档**描述设计大方向与构想；**Build 文档**描述详细的构建方案；**Issue 文档**记录错误与修复方案
- 若 Design / Build / Issue 文档之间存在冲突，或与 AGENTS.md 冲突：**提示用户并让用户做决策**，不擅自选择遵守哪一份
- 若构想本身存在冲突，同样**提示用户**，由用户决策
- Design 文档（如 [Design4.md](./Design4.md)）中的内容不是强制性规定，仅是设计类构想

### 12.2 文档清单

| 文档 | 目标读者 | 内容 | 状态 |
|------|---------|------|------|
| AGENTS.md（本文件） | AI 编码助手 | 编码指令与约束（**唯一强要求**） | 活跃 |
| [Design4.md](./Design4.md) | 人类（开发者/用户） | 当前设计记录：功能全景、设计决策记录、后续候选（设计构想） | 活跃 |
| [Build5.md](./Build5.md) | 开发者 | 当前构建方案与候选构建项 | 活跃 |
| [Issue4.md](./Issue4.md) | 开发者 | 当前问题追踪：进行中问题 + 已知遗留/候选事项 | 活跃 |
| [HistoryDocs/](./HistoryDocs/) | 开发者 | Build1-4、Issue1-3、Design1-3 共 10 份历史文档（已存档，仅记录，不再用于构建，仅用于核查等情况） | 已存档 |

### 12.3 API 使用要求文档（PlatformAPIDocs/）

仓库 `PlatformAPIDocs/` 目录存放各云平台 **API 使用要求** 文档（参数格式、字段长度限制、频率限制等），编码前查阅：

| 目录 | 内容 |
|------|------|
| `PlatformAPIDocs/TencentLighthouseAPIGuide/` | 腾讯云轻量云防火墙 API 指南 |
| `PlatformAPIDocs/TencentCVMAPIGuide/` | 腾讯云 CVM 安全组 API 指南 |
| `PlatformAPIDocs/AliyunSWASAPIGuide/` | 阿里云轻量云（SWAS）API 指南 |
| `PlatformAPIDocs/AliyunECSAPIGuide/` | 阿里云 ECS 安全组 API 指南 |
| `PlatformAPIDocs/PlatformZoneGuide/` | 各平台地域与可用区指南（地域自动补全数据来源） |
