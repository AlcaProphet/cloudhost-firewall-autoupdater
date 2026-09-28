# FWAlizer 告警与运行健康构建计划（Build7：研究与待授权方案）

> **文档定位：** 本文档记录 Build6 完成后的下一阶段方案，聚焦告警触发开关、纯文本邮件内容、测试邮件，以及轻量运行健康检查与外部心跳监控。Build 文档仍为非强制执行建议，唯一强要求是 [AGENTS.md](./AGENTS.md)。
>
> **当前基线：** 2026-09-28 只读核验基线为 `main` / `be41fb4ae38ed12fa8f218195123a8cc2cc17726`，工作区在建立本文档前无未提交改动，分支相对 `origin/main` ahead 1。当前邮件与 Webhook 已订阅 DNS 解析失败和 Provider × 域名同步最终失败事件，但没有测试邮件 API、触发条件开关、可编辑邮件主题/正文或运行健康反向心跳。
>
> **授权边界：** 用户当前只授权研究并把已确认内容写入 `Build7.md`。本文档的建立不授权修改生产代码、测试、Schema、前端、现有 Design/Issue/Build 文档或 `AGENTS.md`，也不授权访问真实 SMTP、真实 Webhook、Uptime Kuma 或云服务。后续代码必须在用户确认本文档中仍待裁决的运行健康方案后，再按单 Step 授权实施。
>
> **与现有强要求的关系：** `AGENTS.md` 当前仍把 Build6 和 version 2 配置包写为现行固定边界。用户已明确本阶段没有兼容性包袱，允许破坏性改动；但配置包最终版本号、`AGENTS.md`/Design/Issue 的切换时机仍需在 Build7 Step 0 明确后再同步，本文档不提前修改强要求。

---

## 一、目标与非目标

### 1.1 目标

1. 给现有两类告警事件增加独立开关：
   - DNS 解析失败；
   - 某个 Provider × 域名同步单元在重试后最终失败。
2. 增加第三类运行健康异常触发条件，但不把进程自身无法完成的检测能力包装为可靠保证。
3. 邮件支持一套可编辑的纯文本主题和正文，不引入 HTML、Markdown 或模板引擎。
4. 增加测试邮件 API，使用页面当前未保存的表单值，直接报告 SMTP 是否已接受邮件或返回完整阶段错误。
5. 测试邮件结果只保留在当前告警页面内存中，并写入现有实时日志，不新增发送历史表。
6. 研究并预留 Uptime Kuma 等外部监控的两种最小接法：外部拉取 operational health、应用主动推送 heartbeat。

### 1.2 明确非目标

- 不增加邮件发送队列、失败重试队列、持久发送历史或 worker 框架；
- 不增加 HTML 邮件、附件、图片、富文本或任意模板语言；
- 不为三类事件各自维护一套邮件模板；
- 不新增前端依赖或前端测试框架；
- 不修改云 Provider 增量算法、DNS 熔断算法或同步重试次数；
- 不把本地假 SMTP、HTTP mock 或自动测试记为真实 SMTP/收件箱、Webhook、Uptime Kuma 或公网可达性已通过；
- 不承诺在进程已经退出、运行时完全卡死、宿主机断电或网络完全中断后由 FWAlizer 自身发送邮件。

---

## 二、已由用户确认的固定决策

下表内容已经确认；后续实施不得重新隐式选择相反方案。

| # | 决策点 | 固定口径 |
|---|--------|---------|
| 1 | 兼容性 | 当前没有任何兼容性包袱，允许破坏 Schema、API 和配置包；不为旧数据库字段或旧配置包保留隐藏兼容入口 |
| 2 | 邮件渠道默认值 | 默认关闭，不自动启用邮件通知 |
| 3 | Webhook 渠道默认值 | 默认关闭，不自动启用 Webhook 通知 |
| 4 | DNS 失败触发默认值 | 默认关闭 |
| 5 | Provider × 域名最终失败触发默认值 | 默认关闭 |
| 6 | 运行健康异常触发默认值 | 默认关闭 |
| 7 | 触发策略归属 | 三个触发开关是邮件和 Webhook 共用的全局告警策略，不为两个渠道复制两套开关 |
| 8 | 邮件内容 | 一套可编辑主题 + 一套可编辑纯文本正文；系统在正文后追加固定事件详情 |
| 9 | 测试邮件 | 新增独立 API；使用当前未保存表单值；不要求邮件渠道已启用；不保存、不热重载、不改变订阅 |
| 10 | 测试结果 | 只保留在当前告警页面内存态；刷新页面或重启后消失；同时写现有 stdout/WebUI 实时日志 |
| 11 | 成功口径 | 只显示“SMTP 服务器已接受测试邮件”，不得写成“已送达收件箱” |
| 12 | 失败口径 | 直接向用户展示完整 SMTP 阶段错误；不得主动拼接密码或完整邮件正文 |

### 2.1 默认关闭的组合语义

新安装、重置或新 Schema 初始状态固定为：

```text
email.enabled              = false
webhook.enabled            = false
trigger.dns_failed         = false
trigger.sync_error         = false
trigger.operational_error  = false
```

- 只有渠道开关和对应触发开关同时开启，才安装该事件的订阅；
- 开启触发条件但没有启用任何渠道时不发送通知；
- 启用邮件或 Webhook 但没有开启任何触发条件时不发送自动通知；
- 测试邮件独立于上述开关，始终只按当前表单值执行一次人工测试。

---

## 三、当前实现基线

### 3.1 当前已有触发链

- `syncer/syncer.go` 在 DNS 解析失败时发布 `notifier.EventDNSFailed`；
- Provider × 域名同步在可重试错误最多 3 次后仍失败，或遇到不可重试错误时，发布 `notifier.EventSyncError`；
- `webui/api/alertset.go` 当前把邮件和 Webhook 都订阅到上述两个事件；
- `notifier/email.go` 与 `notifier/webhook.go` 再次过滤事件类型，只处理上述错误事件；
- 同步成功、发生规则增删、整轮完成、idle 和只有 skipped 的 partial 当前不会自动发送告警。

### 3.2 当前发送与可见性

- 邮件连接上限 10 秒，连接建立后的 SMTP 全会话 deadline 为 30 秒；
- 邮件和 Webhook 各自最多 4 个在途发送，满载时丢弃最新通知并写安全 WARN；
- 自动邮件成功当前没有专用成功日志；失败由 EventBus 写“事件处理失败” WARN；
- 告警页面只有 GET/PUT 配置，没有测试发送路由或结果区域；
- `/api/health` 当前是静态 HTTP 存活响应，不检查 SQLite、Syncer、同步停滞或最近轮次结论；
- `SyncStatus` 已有纯内存的 `running`、`enabled`、`last_sync`、`last_success`、`last_round`，但没有当前轮次开始时间。

---

## 四、告警配置与纯文本邮件合同

### 4.1 建议领域结构

触发策略独立于具体渠道：

```go
type AlertPolicyConfig struct {
    DNSFailedEnabled        bool
    SyncErrorEnabled        bool
    OperationalErrorEnabled bool
}
```

邮件配置在现有 SMTP 字段之外增加：

```go
Subject string
Body    string
```

运行健康研究若最终采用可配置超时，可再向策略增加一个正时长字段；字段名和默认值在第七节裁决后固定。

### 4.2 持久化边界

- 渠道配置、触发开关、邮件主题和正文属于业务配置，必须进入 SQLite；
- 推荐新建单行 `alert_policy` 表承载全局触发策略，不把共用策略错误地塞进 `alert_email`；
- `alert_email` 增加主题与正文字段；
- 测试发送结果不进入 SQLite；
- reset 必须清除新的告警策略并恢复第二节的全部关闭默认值；
- 配置导出必须包含完整的新告警结构，配置导入必须覆盖式原子替换；
- 既然用户明确不保留兼容性，新 Schema 和新配置包可以严格要求全部新字段存在，旧包可以直接拒绝，不做缺字段补默认或迁移猜测；
- 配置包最终继续使用 version 2 还是提升版本号，在 Step 0 与 `AGENTS.md` 的固定 version 2 合同一起裁决，不能在代码 Step 中临时决定。

### 4.3 邮件主题与正文

建议默认值：

```text
主题：[FWAlizer] 告警通知
正文：FWAlizer 检测到运行异常，请检查同步日志。
```

实际自动邮件主题由固定后缀区分类型：

```text
[FWAlizer] 告警通知 - DNS 解析失败
[FWAlizer] 告警通知 - 同步失败
[FWAlizer] 告警通知 - 运行健康异常
```

正文由“用户文本 + 系统固定详情块”组成，不引入占位符语法：

```text
FWAlizer 检测到运行异常，请检查同步日志。

事件类型：DNS 解析失败
时间：2026-09-28 12:34:56
Provider：tc_lighthouse(lhins-example)
域名：example.com
错误：完整错误内容
```

不同事件缺少的字段用 `-` 表示。固定详情顺序必须稳定，不再遍历 map 产生随机顺序。

### 4.4 最小校验

- `subject` Trim 后不能为空，禁止换行和控制字符，最多 200 个 Unicode 字符；
- `body` 允许普通换行，最大 10 KiB；
- 邮件仍固定使用 `text/plain; charset=UTF-8`；
- SMTP host/port/from/to 与渠道启用时的既有最小校验保留；
- 测试邮件按“实际要发送”校验 SMTP 字段，即使邮件自动通知开关关闭也要求 host/from/to 完整；
- 多收件人继续使用逗号分隔，实施时应逐项 Trim，避免页面示例 `a@example.com, b@example.com` 把第二个地址连同前导空格传给 SMTP。

---

## 五、测试邮件 API 与页面反馈

### 5.1 API

固定新增：

```text
POST /api/alerts/test-email
```

请求体使用告警页当前邮件表单的全部发送字段，但不包含 Webhook 或触发开关：

```json
{
  "host": "smtp.example.com",
  "port": "587",
  "username": "user@example.com",
  "password": "secret",
  "from_addr": "user@example.com",
  "to_addr": "admin@example.com",
  "subject": "[FWAlizer] 告警通知",
  "body": "FWAlizer 检测到运行异常，请检查同步日志。"
}
```

固定行为：

- 使用普通请求 1 MiB 上限和严格 JSON 解码；
- 使用当前请求中的表单值，不从 Store 或 RuntimeState 替换字段；
- 不写 SQLite、不进入 `ConfigCoordinator`、不 Apply 告警集合；
- 不要求 `email.enabled=true`；
- 测试主题追加固定后缀 ` - 测试邮件`；
- 测试正文追加“这是一次手动测试邮件”与当前时间；
- 复用生产邮件的 SMTP 会话实现、10 秒连接上限和 30 秒整会话 deadline；
- 前端请求上限建议 35 秒；
- 测试不经过自动告警的事件开关和每渠道在途订阅路径。

### 5.2 响应

SMTP 完整会话成功：

```json
{
  "success": true,
  "message": "SMTP 服务器已接受测试邮件"
}
```

SMTP 或网络失败：

```json
{
  "success": false,
  "error": "SMTP 认证失败: 535 Authentication failed"
}
```

- JSON/字段校验错误仍使用 HTTP 400；
- SMTP、TLS、认证、MAIL、RCPT、DATA、QUIT 等预期外部错误使用结构化测试结果返回，页面直接展示 `error`；
- 错误允许包含 SMTP 返回的完整诊断，不主动脱敏 host/IP/状态文本，但不得主动加入密码、请求正文或配置包内容；
- `success=true` 只证明 SMTP 服务器接受，不证明最终投递、收件箱展示或垃圾邮件分类。

### 5.3 告警页面

- 邮件开关只控制自动通知，不再用它禁用 SMTP 表单输入；用户可以在自动通知关闭时编辑并测试；
- 邮件卡片增加主题单行输入和纯文本多行输入；
- 增加页面级大按钮“测试发送邮件”；
- 按钮请求期间 loading，防止同一页面重复点击；
- 按钮下使用简单结果区域：测试中、SMTP 已接受、失败完整错误；
- 结果只存在于当前 `Alerts.vue` 组件内存，刷新页面即消失；
- 不增加结果查询 API、SSE、轮询或发送历史页面。

### 5.4 日志

测试邮件：

```text
INFO 测试邮件已被 SMTP 服务器接受 to=admin@example.com
WARN 测试邮件发送失败 error="SMTP 认证失败: 535 Authentication failed"
```

自动邮件：

```text
INFO 邮件告警已被 SMTP 服务器接受 event=dns:failed
WARN 邮件告警发送失败 event=sync:error error="..."
```

日志进入既有 stdout 与 WebUI 实时日志流，不写 `sync_logs`，不增加新表。日志不得包含 SMTP 密码、完整正文或 Webhook URL。

---

## 六、触发过滤合同

### 6.1 DNS 解析失败

- 开关映射现有 `EventDNSFailed`；
- DNS 失败仍保留现有规则，不触发删除；
- 当前事件粒度不改：一个 Provider × 一条规则执行到 DNS 失败即可产生事件；
- 本阶段不增加按域名去重、静默期、聚合或告警冷却。

### 6.2 Provider × 域名同步最终失败

- 开关映射现有 `EventSyncError`；
- 可重试错误在最多 3 次完整 Describe → Diff → Create/Delete 后仍失败才触发；
- 不可重试错误立即触发；
- 已确认的 added/deleted 进度继续保留在事件详情；
- 本阶段不把成功、changed、idle 或只有 skipped 的 partial 自动归入这一开关。

### 6.3 订阅应用

- `AlertManager` 按策略只向选中的事件类型安装邮件/Webhook 订阅；
- 配置事务仍遵守“事务内完整快照与候选构造 → commit → 无失败 Apply”；
- 候选构造不得连接 SMTP、Webhook、Uptime Kuma、DNS 或云 API；
- 热重载前已在途的发送自然完成，热重载后的新事件只进入新策略；
- 每渠道在途上限 4 的既有边界保持不变。

---

## 七、运行健康检查研究

### 7.1 能力边界

项目内部可以在进程仍可调度时检测并通知：

- SQLite 已不可用；
- Syncer 主循环异常停止；
- 最近一轮为 failed 或 partial；
- 一轮同步开始后长时间没有结束；
- 同步开启，但超过合理时间没有出现新的完成轮次。

项目内部不能可靠检测后再自行发送：

- 进程已退出；
- Go runtime 或整个进程完全卡死；
- 宿主机断电、系统崩溃；
- 网络完全中断，SMTP/Webhook/外部监控均不可达；
- HTTP 服务从外部不可达但内部自检仍能运行的全部网络路径问题。

因此“运行健康异常”必须拆成内部 best-effort 检查和外部 dead-man 检查，不能只靠内部邮件。

### 7.2 推荐的最小内部健康判定

建议新增一个纯读取的统一 `OperationalHealth` 计算函数，供内部监督器和 HTTP 端点共用。初步推荐如下：

```text
SQLite：Ping/SELECT 1 成功
Syncer：主循环 running=true
最近轮次：failed 或 partial 视为 unhealthy，直到下一轮 success/idle 覆盖
当前轮次：开始后超过 health_timeout 仍未完成，视为 unhealthy
调度停滞：sync_enabled=true，距最近完成时间超过 interval + health_timeout，视为 unhealthy
暂停状态：不检查同步停滞，但仍检查 SQLite 与 Syncer 主循环
空目标/空规则：idle 视为正常，避免新安装永久异常
```

建议只保留一个用户可配置正时长 `health_timeout`，默认候选为 `10m`。它同时作为“单轮最长容忍时间”和“超过下一次计划时间后的宽限”，避免增加多组阈值。

这套判定有意偏敏感，允许较高误报，但每项都有清晰证据和可展示原因，不使用 CPU、内存、goroutine 数或主机负载等容易把宿主问题与应用问题混淆的指标。

### 7.3 内部监督器

若采用第 7.2 节，建议使用一个轻量 `time.Ticker` 每 30 秒计算一次统一健康状态：

- `healthy → unhealthy`：发布一次新的运行健康异常事件，并写 WARN；
- 持续 unhealthy：不重复发送，避免每 30 秒刷屏；原因变化只更新日志；
- `unhealthy → healthy`：写 INFO 恢复日志，当前阶段不要求发送恢复邮件；
- 只有第三个触发开关开启时，运行健康异常事件才进入邮件/Webhook；
- 监督器停止和应用 shutdown 使用同一生命周期，不引入孤立 goroutine；
- 外部心跳发送失败只写 WARN，不反向把“监控服务不可达”加入应用健康，避免自激循环。

该监督器仍无法覆盖进程已经死亡的场景，必须由第 7.5/7.6 节补足。

### 7.4 Operational HTTP 端点

推荐保留现有 Docker 端点：

```text
GET /api/health
```

它继续只表示 HTTP 服务可达，避免一次同步失败直接让 Docker 把容器标为 unhealthy 或触发编排重启。

另新增供外部监控使用的端点，候选名称：

```text
GET /api/health/operational
```

健康时返回 HTTP 200：

```json
{
  "status": "ok",
  "checked_at": "2026-09-28T12:00:00Z",
  "reasons": []
}
```

异常时返回 HTTP 503：

```json
{
  "status": "unhealthy",
  "checked_at": "2026-09-28T12:00:00Z",
  "reasons": ["最近一轮同步失败", "SQLite 检查失败"]
}
```

- 响应不包含云凭据、SMTP 密码、Webhook/Push URL、数据库路径或底层 SQL；
- 内部日志可以记录已归类的详细错误，但 HTTP 只返回稳定原因；
- Uptime Kuma、其他 HTTP 监控或反向代理可以按 2xx/503 直接判断；
- 不改变 Dockerfile/Compose 对现有 `/api/health` 的使用。

### 7.5 Uptime Kuma 外部拉取

如果 Uptime Kuma 能访问 FWAlizer，最简单可靠的方式是建立 HTTP(s) Monitor，目标指向：

```text
http(s)://<FWAlizer>/api/health/operational
```

优点：

- FWAlizer 进程退出、端口不可达、HTTP 超时、返回 503 都能由外部检测；
- 不需要 FWAlizer 保存 Uptime Kuma token；
- Uptime Kuma 自己负责重试、连续失败判定和通知渠道；
- 比“应用发现自己已死亡后发邮件”可靠。

限制：Uptime Kuma 必须能主动访问 FWAlizer；如果 FWAlizer 只绑定内网或位于不可入站网络，需要第 7.6 节的 Push 方式。

### 7.6 Uptime Kuma Push 反向心跳

Uptime Kuma 官方 Push Monitor 提供：

```text
/api/push/<pushToken>?status=up|down&msg=<message>&ping=<milliseconds>
```

官方文档说明该端点接受 GET/POST/PUT/PATCH，成功返回 `{"ok":true}`；Push Monitor 可以用“规定窗口内未收到 heartbeat”判定异常。

建议在告警配置页之外新增一个简洁的“外部运行监控”卡片：

```text
启用外部心跳：false
Push URL：空
心跳间隔：60s
```

推荐行为：

- 默认关闭；
- URL 作为敏感业务配置写入 SQLite 和完整配置包，但绝不写日志或错误响应；
- 每 60 秒计算一次与 operational endpoint 完全相同的健康状态；
- 健康时请求 `status=up&msg=OK`；
- 已检测到内部异常时可立即请求一次 `status=down&msg=<短原因>`；
- 进程死亡或完全卡死时不再有 push，Uptime Kuma 依靠缺失 heartbeat 判 DOWN；
- HTTP client 使用 10 秒上限，不排队、不重试；失败只写不含 URL 的 WARN；
- 不把 Push 失败再发布为邮件/Webhook 告警，避免网络故障时循环放大；
- Uptime Kuma 的 heartbeat interval 应大于 FWAlizer 的 60 秒发送间隔并留余量，具体操作值在真实联调时记录。

官方依据：

- [Uptime Kuma API Documentation - Push Endpoint](https://github.com/louislam/uptime-kuma/wiki/API-Documentation/692198f84f3675a53a8ece7eb91a6a84566ee98e)
- [Uptime Kuma 官方 Go Push 示例](https://github.com/louislam/uptime-kuma/blob/master/extra/push-examples/go/index.go)
- [Uptime Kuma README - HTTP/JSON Query/Push 等监控类型](https://github.com/louislam/uptime-kuma/blob/master/README.md)

### 7.7 当前推荐组合与待裁决点

当前研究推荐同时提供：

1. 保持 `/api/health` 作为 Docker HTTP 存活检查；
2. 新增 `/api/health/operational` 作为应用工作状态检查；
3. 新增内部 30 秒健康监督器，负责在进程仍可运行时发布第三类告警；
4. 可选 Uptime Kuma Push URL，每 60 秒发送一次健康心跳；
5. 外部环境能主动访问时，优先再配置 Uptime Kuma HTTP Monitor 拉取 operational endpoint。

仍需用户在代码实施前确认：

- 第三开关的最终展示名称是否使用“运行健康异常”；
- `health_timeout` 是否采用一个字段、默认 `10m`；
- failed/partial 是否都让 operational health 返回 503；
- 是否同时实施 operational endpoint 和可选 Push，还是本期只实施其中一个；
- 配置包版本号如何处理（无兼容要求已固定，但 `AGENTS.md` 当前仍固定 version 2）。

---

## 八、推荐实施步骤（均未获代码授权）

### Step 0：合同收口

- 用户裁决第 7.7 节；
- 固定配置包版本号与完整 JSON；
- 确认 Build7 成为当前构建方案后，再同步 `AGENTS.md`、Design/Issue/README 的文档定位；
- 本 Step 只改文档，不改代码。

### Step 1：Schema、配置模型与严格 API

- 新增告警策略持久化、邮件主题/正文和可选健康/Push 设置；
- 更新完整快照、RuntimeState、reset、导入导出和失败回滚；
- 先写旧 Schema/旧包拒绝、默认全关闭、事务失败零发布的判别性测试；
- 不连接任何外部服务。

### Step 2：测试邮件

- 先用本地假 SMTP 建立成功、认证失败、RCPT/DATA 失败和静默 deadline 用例；
- 实现 `POST /api/alerts/test-email`；
- 实现页面内存态结果与实时安全日志；
- 验证不写库、不 Apply、不改变订阅。

### Step 3：触发条件过滤与邮件内容

- 按三个开关安装订阅；
- 统一纯文本格式与固定详情顺序；
- 覆盖默认全关闭、各开关独立、邮件/Webhook 共用策略和热重载边界。

### Step 4：运行健康计算与内部监督器

- 实现唯一 `OperationalHealth` 计算源；
- 增加轮次开始时间/超时观测；
- 实现健康状态边沿事件，持续异常不重复通知；
- 保持 shutdown 有界且 race 通过。

### Step 5：外部监控接入

- 实现 operational endpoint；
- 若获授权，实现 Uptime Kuma Push；
- URL 脱敏、超时、无重试和无自激循环；
- 使用本地 `httptest` 验证协议，不访问真实 Uptime Kuma。

### Step 6：统一验收与文档闭环

- targeted tests → 相关包 race → `go test ./... -race -count=1` → vet/build；
- 前端 `npm ci`、build、生产与完整 audit；
- 浏览器检查默认全关闭、编辑/保存、测试中/成功/失败和内存态消失；
- 真实 SMTP/收件箱、真实 Webhook、真实 Uptime Kuma HTTP/Push 分层记录，未执行不得写成通过；
- 最后更新 README、AGENTS、Design、Issue 与生产测试清单。

---

## 九、验收矩阵

| 层次 | 必须证明 | 不能替代 |
|------|---------|---------|
| 源码/静态 | 默认全关闭、配置链与订阅链唯一、敏感 URL/密码不入日志 | 运行时发送成功 |
| notifier 单测 | 邮件格式、SMTP 接受/阶段错误、触发过滤、在途上限 | 真实 SMTP/收件箱 |
| API 集成 | 严格 JSON、事务、测试 API 不写库、完整错误返回 | 浏览器交互 |
| 健康状态测试 | SQLite/Syncer/failed/partial/超时/暂停/idle 的确定性结果 | 进程死亡、宿主机断电 |
| HTTP/Push mock | 200/503、Push up/down、URL 脱敏、超时无重试 | 真实 Uptime Kuma |
| race/build | 并发与编译门禁 | 外部服务真实性 |
| 浏览器 | 开关、主题/正文、loading、结果框、刷新消失 | SMTP 最终投递 |
| 真实 SMTP | SMTP 接受、收件箱与真实错误 | Webhook/Uptime Kuma |
| 真实外部监控 | HTTP 拉取、缺失 heartbeat、DOWN/恢复通知 | 项目内部单元测试 |

---

## 十、停止条件

遇到以下任一情况必须停止当前代码 Step 并请求用户决定：

- 需要改变 `AGENTS.md` 中未被本 Build7 明确覆盖的强要求；
- 运行健康检查将导致 Docker 因普通同步失败自动重启；
- 必须引入外部库、持久队列、后台框架或新前端依赖；
- 需要记录 SMTP 密码、Webhook URL、Uptime Kuma Push URL 或完整正文才能诊断；
- “系统未响应”只能通过虚假成功保证或无法测试的内部推断实现；
- 配置包版本号、health timeout 或 failed/partial 健康口径尚未裁决；
- 真实外部服务凭据不可用时，任何人试图用 mock 替代并把外部验收写成通过。
