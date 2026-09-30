# FWAlizer 告警与运行健康构建计划（Build7：Step 0～7 已实施完成）

> **文档定位：** 本文档记录 Build6 完成后的下一阶段已定案方案，聚焦告警触发开关、纯文本邮件内容、测试邮件、轻量运行健康检查，以及 Uptime Kuma HTTP/Push 外部监控。Build 文档仍为非强制执行建议，唯一强要求是 [AGENTS.md](./AGENTS.md)。
>
> **当前基线：** 2026-09-28 初始只读核验基线为 `main` / `be41fb4ae38ed12fa8f218195123a8cc2cc17726`，工作区在建立本文档前无未提交改动，分支相对 `origin/main` ahead 1；初稿随后提交为 `641c09c`，本次定案在该初稿上继续更新。当前邮件与 Webhook 已订阅 DNS 解析失败和 Provider × 域名同步最终失败事件，但没有测试邮件 API、触发条件开关、可编辑邮件主题/正文或运行健康反向心跳。
>
> **授权边界：** 用户已确认本文档的告警、运行健康、Uptime Kuma Push 与破坏性配置协议方向。2026-09-28 用户完成最终审核并**一次性授权**按本文档 Step 0 → Step 6 串行实施全部构建：Step 之间无需重复申请授权，但必须逐 Step 串行、不得跳步或并行。仍须暂停并重新询问的情形：触发本文第十节停止条件、出现与 `AGENTS.md` 其他未覆盖强要求的新冲突、需要扩大范围，或需要真实外部权限/凭据。本文档的更新（含 Step 0 合同收口）不授权访问真实 SMTP、真实 Webhook、Uptime Kuma 或云服务。
>
> **与现有强要求的关系：** `AGENTS.md` 已在 **Build7 Step 0**（2026-09-28，仅文档）同步为本档的 version 3 目标合同：配置包 version 3 且旧 version 1/2 直接拒绝、告警默认全部关闭、唯一 `OperationalHealth` 与运行健康异常、Uptime Kuma HTTP/Push；同时把 Build6 定位为已完成的历史构建记录。用户已明确本阶段没有兼容性包袱，允许破坏性改动。Step 0 只改文档，Schema/API/前端自 Step 1 起才实施。

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
6. 同时支持 Uptime Kuma 两种最小接法：外部拉取 operational health、应用主动推送 heartbeat。

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
| 13 | 第三触发条件 | 名称固定为“运行健康异常”；由统一 operational health 计算，不声称能由进程自身覆盖进程死亡 |
| 14 | 健康超时 | 只设置一个 `health_timeout`，默认 `10m`；同时作为单轮执行超时和计划完成后的宽限 |
| 15 | 异常轮次 | 最近一轮 `failed` 或 `partial` 都视为 operational unhealthy，并使 operational 端点返回 HTTP 503 |
| 16 | 健康端点 | 保留 `/api/health` 的静态 Docker 存活语义；新增 `/api/health/operational` 表达应用工作状态 |
| 17 | 内部监督 | 每 30 秒检查一次；只在健康→异常边沿发布一次告警，持续异常不重复刷屏，恢复只写 INFO |
| 18 | Uptime Kuma | 同时支持 HTTP Monitor 拉取 operational 端点和 Push Monitor 反向心跳；Push 默认关闭 |
| 19 | Push 周期 | 可配置，默认 `60s`，最小 `20s`；每次都发送当前 up/down 状态，不因状态未变化停止心跳 |
| 20 | 配置包 | 提升到 version 3；version 1/2 和其他版本全部直接拒绝，不迁移、不补全、不兼容 |

### 2.1 默认关闭的组合语义

新安装、重置或新 Schema 初始状态固定为：

```text
email.enabled              = false
webhook.enabled            = false
trigger.dns_failed         = false
trigger.sync_error         = false
trigger.operational_error  = false
uptime_kuma_push.enabled   = false
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

### 4.1 固定领域结构

触发策略独立于具体渠道：

```go
type AlertPolicyConfig struct {
    DNSFailedEnabled        bool
    SyncErrorEnabled        bool
    OperationalErrorEnabled bool
    HealthTimeout           time.Duration
}
```

邮件配置在现有 SMTP 字段之外增加：

```go
Subject string
Body    string
```

Uptime Kuma Push 使用独立配置，不与普通 Webhook 混用：

```go
type UptimeKumaPushConfig struct {
    Enabled  bool
    URL      string
    Interval time.Duration
}
```

`HealthTimeout` 默认 `10m`；Push `Interval` 默认 `60s`，允许配置但不得小于 `20s`。

### 4.2 持久化边界

- 渠道配置、触发开关、邮件主题和正文属于业务配置，必须进入 SQLite；
- 新建单行 `alert_policy` 表承载全局触发策略，不把共用策略错误地塞进 `alert_email`；
- `alert_email` 增加主题与正文字段；
- 新建单行 `uptime_kuma_push` 表承载启用状态、敏感 Push URL 和发送间隔；不复用普通 `alert_webhook`，避免把告警目标与 dead-man 心跳端点混为一个渠道；
- 测试发送结果不进入 SQLite；
- reset 必须清除新的告警策略并恢复第二节的全部关闭默认值；
- 配置导出必须包含完整的新告警结构，配置导入必须覆盖式原子替换；
- 既然用户明确不保留兼容性，新 Schema 和新配置包可以严格要求全部新字段存在，旧包可以直接拒绝，不做缺字段补默认或迁移猜测；
- 配置包固定提升为 version 3；version 1/2 和其他版本直接返回 400，不迁移、不猜测、不补字段；
- 现有数据库实施最小显式 Schema 变更：增加新表/列并把邮件、Webhook、三个触发条件和 Push 启用状态统一写为 false；允许破坏旧启用状态，但不需要为了“无兼容负担”无意义删除目标、规则、凭据或同步日志；
- Step 0 必须先把 `AGENTS.md` 中 version 2 唯一协议和 reset 表清单改为 version 3 新合同，再进入 Schema 实施。

目标 Schema 形态固定为：

```sql
CREATE TABLE alert_policy (
    id INTEGER PRIMARY KEY DEFAULT 1,
    dns_failed_enabled INTEGER NOT NULL DEFAULT 0,
    sync_error_enabled INTEGER NOT NULL DEFAULT 0,
    operational_error_enabled INTEGER NOT NULL DEFAULT 0,
    health_timeout TEXT NOT NULL DEFAULT '10m'
);

CREATE TABLE alert_email (
    id INTEGER PRIMARY KEY DEFAULT 1,
    enabled INTEGER NOT NULL DEFAULT 0,
    host TEXT NOT NULL DEFAULT '',
    port TEXT NOT NULL DEFAULT '587',
    username TEXT NOT NULL DEFAULT '',
    password TEXT NOT NULL DEFAULT '',
    from_addr TEXT NOT NULL DEFAULT '',
    to_addr TEXT NOT NULL DEFAULT '',
    subject TEXT NOT NULL DEFAULT '[FWAlizer] 告警通知',
    body TEXT NOT NULL DEFAULT 'FWAlizer 检测到运行异常，请检查同步日志。'
);

CREATE TABLE uptime_kuma_push (
    id INTEGER PRIMARY KEY DEFAULT 1,
    enabled INTEGER NOT NULL DEFAULT 0,
    url TEXT NOT NULL DEFAULT '',
    interval TEXT NOT NULL DEFAULT '60s'
);
```

`alert_webhook` 保持现有字段形态，但 `enabled` 默认和迁移结果固定为 0。实际迁移可使用安全的建表/复制/替换或等价显式 SQL；不得依赖 `CREATE TABLE IF NOT EXISTS` 自动补列。迁移完成必须保证各单行表至多一行且业务 ID 固定为 1。

### 4.3 version 3 配置包告警与监控结构

version 3 继续保留 Build6 已有的 metadata、targets、rules 与 settings 主体，但 `alerts` 固定扩展策略和纯文本字段，并新增 `monitoring`。相关字段全部必需，数组/对象不得为 `null`：

```json
{
  "version": 3,
  "metadata": {
    "exported_at": "2026-09-28T12:00:00Z"
  },
  "targets": [],
  "rules": [],
  "settings": {
    "credentials": {
      "tencent": { "secret_id": "", "secret_key": "" },
      "aliyun": { "access_key_id": "", "access_key_secret": "" }
    },
    "tag": "auto-dns",
    "interval": "5m",
    "dns": "223.5.5.5",
    "dns_timeout": "10s",
    "dns_fail_threshold": 5,
    "log_level": "info",
    "sync_enabled": true,
    "theme": "light"
  },
  "alerts": {
    "policy": {
      "dns_failed_enabled": false,
      "sync_error_enabled": false,
      "operational_error_enabled": false,
      "health_timeout": "10m"
    },
    "email": {
      "enabled": false,
      "host": "",
      "port": "587",
      "username": "",
      "password": "",
      "from_addr": "",
      "to_addr": "",
      "subject": "[FWAlizer] 告警通知",
      "body": "FWAlizer 检测到运行异常，请检查同步日志。"
    },
    "webhook": {
      "enabled": false,
      "url": "",
      "channel": "dingtalk"
    }
  },
  "monitoring": {
    "uptime_kuma_push": {
      "enabled": false,
      "url": "",
      "interval": "60s"
    }
  }
}
```

固定规则：

- version 3 是唯一可导入/导出的版本；
- Push URL 与 SMTP 密码、Webhook URL 一样属于敏感完整快照字段；导出响应体允许包含，其他日志和错误响应不得包含；
- 导入仍是覆盖式原子事务；targets/rules ID 映射、保留 `sync_logs`、清空 `scanned_resources`、不重置 `sqlite_sequence` 等 Build6 未被本方案改变的合同继续保留；
- 导入必须在事务内同时写入 policy、email、webhook、uptime_kuma_push，并在 commit 前构造完整候选；
- commit 后应用顺序扩展为：日志级别 → 告警集合/策略 → 运行健康监督配置 → Uptime Kuma Push 配置 → `RuntimeState`；全部必须是无失败的内存发布，外部网络请求不得发生在协调器锁内；
- 导入成功后下一次监督 tick/Push tick 使用新配置；已经在途的 SMTP/Webhook/Push 请求允许按旧快照完成。

### 4.4 邮件主题与正文

固定默认值：

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

该详情块由**邮件与 Webhook 共用同一渲染器**（Build7 Step 7）：Webhook 正文为
`[FWAlizer] <事件类型>` 首行 + 同一详情块，因此两个渠道顺序一致、缺失字段同样写 `-`。

运行健康异常事件（`EventOperationalUnhealthy`）在详情块末尾追加固定行：

```text
原因：最近一轮同步失败; SQLite 检查失败
```

`原因` 行由事件数据中的稳定原因数组用 `; ` 连接；无原因（或原因全为空）时该行不输出，
因此 DNS 解析失败与同步失败事件的正文保持既有形态不变。

### 普通 Webhook 响应判断（P2-05 后续补记，2026-09-30）

P2-05 是 Build7 完成后的独立修复，已提交为 `93e0e4b`；不改变 Step 0～7 历史验收结果，也不复用 Uptime Kuma Push 的响应协议。

| 渠道 | 明确成功条件 |
|------|--------------|
| 钉钉 | HTTP 2xx；JSON 对象中存在整数 `errcode=0` |
| 飞书 | HTTP 2xx；存在整数 `code=0` 或旧 `StatusCode=0`；两字段同时出现时必须都合法且为零 |
| Slack | HTTP 200；TrimSpace 后正文恰好为 `ok` |

非 2xx、空/未知/非法正文、状态字段缺失/null/类型错误、非零业务码、读取失败或超过 16 KiB 均返回安全错误。响应最多读取 16 KiB + 1 字节，原有 10 秒 HTTP 上限覆盖读取过程，在途名额直到读取和关闭结束才释放。业务失败沿既有 EventBus 记录 WARN，只包含安全渠道、固定类别和必要状态码；不输出或包装 URL、token、请求/响应正文或底层错误。关闭错误单独记录固定 `response_close` WARN，已确认业务成功时不改判失败。

本修复不增加重试、补发、投递状态持久化或健康联动，不修改告警配置、订阅、在途限流、前端与 Push。平台接受请求与真实群聊收到告警分别验证。

### 4.5 最小校验

- `subject` Trim 后不能为空，禁止换行和控制字符，最多 200 个 Unicode 字符；
- `body` 允许普通换行，最大 10 KiB；
- 邮件仍固定使用 `text/plain; charset=UTF-8`；
- SMTP host/port/from/to 与渠道启用时的既有最小校验保留；
- 测试邮件按“实际要发送”校验 SMTP 字段，即使邮件自动通知开关关闭也要求 host/from/to 完整；
- 多收件人继续使用逗号分隔，实施时应逐项 Trim，避免页面示例 `a@example.com, b@example.com` 把第二个地址连同前导空格传给 SMTP。
- `health_timeout` 必须是大于 0 的 Go duration，默认 `10m`；
- Push `interval` 必须是可解析且不少于 `20s` 的 Go duration，默认 `60s`；
- Push 启用时 URL 必须是 host 非空的绝对 `http/https` URL；禁用时允许 URL 为空；
- Push URL 允许直接粘贴 Uptime Kuma 页面给出的完整地址及既有 query；发送时解析 URL 并覆盖 `status`、`msg`、`ping` 三个参数，不做字符串拼接。

### 4.6 告警配置 API

继续使用现有端点，不增加第二套保存入口：

```text
GET /api/alerts
PUT /api/alerts
```

GET/PUT 顶层固定为四个完整对象：

```json
{
  "policy": {
    "dns_failed_enabled": false,
    "sync_error_enabled": false,
    "operational_error_enabled": false,
    "health_timeout": "10m"
  },
  "email": {
    "enabled": false,
    "host": "",
    "port": "587",
    "username": "",
    "password": "",
    "from_addr": "",
    "to_addr": "",
    "subject": "[FWAlizer] 告警通知",
    "body": "FWAlizer 检测到运行异常，请检查同步日志。"
  },
  "webhook": {
    "enabled": false,
    "url": "",
    "channel": "dingtalk"
  },
  "uptime_kuma_push": {
    "enabled": false,
    "url": "",
    "interval": "60s"
  }
}
```

- PUT 四个对象及其全部子字段都必须出现，拒绝 `null`、缺字段、未知字段、尾随 JSON 和多个顶层值；
- 四部分在同一个 `ConfigCoordinator` 写事务内覆盖保存，任一步失败全部回滚；
- transaction 内构造包含 RuntimeState、AlertSet、OperationalSupervisorConfig 与 UptimeKumaPushConfig 的完整候选；
- commit 后只替换内存配置和唤醒对应循环，不在 HTTP handler/协调器锁内等待 SMTP、Webhook 或 Uptime Kuma；
- GET 为现有告警表单返回完整敏感对象的既有边界，仍会返回 SMTP 密码、Webhook URL 和 Push URL；响应必须 `Cache-Control: no-store`，不得被日志中间件记录 body。

### 4.7 告警页面布局

现有 `/alerts` 页面保持单页，按以下顺序放置四张卡片：

1. **触发条件**：DNS 解析失败、Provider × 域名最终失败、运行健康异常三个开关；第三个开关下显示 `health_timeout`；
2. **邮件告警**：渠道开关、SMTP 字段、主题、纯文本正文、测试发送按钮和本次测试结果；
3. **Webhook 告警**：渠道开关、URL、钉钉/飞书/Slack 选择；
4. **外部运行监控（Uptime Kuma Push）**：启用开关、Push URL、发送间隔，以及“Uptime Kuma Heartbeat Interval 应大于本值；默认建议 120s”的提示。

页面底部保留一个“保存配置”按钮，一次提交第 4.6 节的完整对象。页面级保存和测试按钮均使用 `size="large"`；测试按钮不触发保存。SMTP 与 Push URL 输入使用密码型/可切换显示的敏感输入样式，不在页面额外复制显示完整值。

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
- 前端请求上限固定为 35 秒；
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

## 七、运行健康检查固定合同

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

### 7.2 最小内部健康判定

固定新增一个纯读取的统一 `OperationalHealth` 计算函数，供内部监督器、HTTP 端点与 Uptime Kuma Push 共用；不得在三处各写一套判断。判断如下：

```text
SQLite：Ping/SELECT 1 成功
Syncer：主循环 running=true
启动宽限：从未进入运行态且距进程启动未超过固定 10s 时，不因「主循环未运行」判异常（Step 7）
最近轮次：failed 或 partial 视为 unhealthy，直到下一轮 success/idle 覆盖
当前轮次：开始后超过 health_timeout 仍未完成，视为 unhealthy
调度停滞：sync_enabled=true，距最近完成时间超过 interval + health_timeout，视为 unhealthy
暂停状态：不检查同步停滞，但仍检查 SQLite 与 Syncer 主循环
空目标/空规则：idle 视为正常，避免新安装永久异常
```

只保留一个用户可配置正时长 `health_timeout`，默认 `10m`。它同时作为“单轮最长容忍时间”和“超过下一次计划时间后的宽限”，不增加多组阈值。

为使上述判断可复现，Syncer 内存状态固定追加：

```go
RoundStartedAt *time.Time // 当前无轮次时为 nil
ProcessStartedAt time.Time
```

精确判断顺序：

1. 使用最多 2 秒的 context 执行 SQLite `PingContext` 或等价 `SELECT 1`；失败即 unhealthy；
2. 非 shutdown 阶段 `SyncStatus.running=false` 按三分支判定（Build7 Step 7）：
   - `SyncStatus.started_at != null`（已进入运行态后停止）→ **立即** unhealthy，不受启动宽限影响；
   - 从未进入运行态且距 `process_started_at` **未超过**固定启动宽限 `StartupGrace=10s` → 跳过（进程刚启动，主循环可能尚未被调度）；
   - 从未进入运行态且**超过**宽限 → 按「同步引擎未运行」上报。
   应用已经进入正常 shutdown 时停止监督，不再制造“引擎停止”告警；
3. `sync_enabled=false` 时跳过轮次结论、轮次超时和调度停滞，只保留 SQLite/Syncer 检查；
4. `RoundStartedAt != nil` 且 `now-RoundStartedAt > health_timeout` 时记“同步轮次超时”；
5. `last_round.outcome` 为 `failed` 或 `partial` 时记“最近一轮失败/部分完成”，直到后续 `success` 或 `idle` 覆盖；
6. `sync_enabled=true` 且当前没有在途轮次时：
   - 有 `last_sync`：`now-last_sync > interval+health_timeout` 记“同步调度停滞”；
   - 无 `last_sync`：`now-ProcessStartedAt > health_timeout` 记“启动后尚无完成轮次”；
7. `idle` 本身是健康结果；它仍会刷新 `last_sync`，因此若调度随后停止，仍可由第 6 条发现；
8. 任一原因成立即 unhealthy；多项同时成立时全部返回，但相同原因去重并使用固定排序。

状态读取必须取一份一致的内存快照；SQLite 检查在快照之外有界执行，不持有 Syncer 或配置协调器锁。

这套判定有意偏敏感，允许较高误报，但每项都有清晰证据和可展示原因，不使用 CPU、内存、goroutine 数或主机负载等容易把宿主问题与应用问题混淆的指标。

### 7.3 内部监督器

固定使用一个轻量 `time.Ticker` 每 30 秒计算一次统一健康状态：

- `healthy → unhealthy`：发布一次新的运行健康异常事件，并写 WARN；
- 持续 unhealthy：不重复发送，避免每 30 秒刷屏；原因变化只更新日志；
- `unhealthy → healthy`：写 INFO 恢复日志，当前阶段不要求发送恢复邮件；
- 只有第三个触发开关开启时，运行健康异常事件才进入邮件/Webhook；
- 新事件类型固定为 `notifier.EventOperationalUnhealthy`，事件数据只包含检查时间与稳定原因数组；
- 监督器始终计算和记录健康状态；第三开关只控制邮件/Webhook 是否订阅，不关闭 operational endpoint 或 Uptime Kuma Push；
- 保存新配置后立即唤醒一次监督检查，不等待最长 30 秒；若从“第三开关关闭”变为开启且当前已经 unhealthy，下一次检查视为需要发送一次当前异常；
- 监督器停止和应用 shutdown 使用同一生命周期，不引入孤立 goroutine；
- 启动顺序确定性化（Step 7）：`run.go` 先启动同步主循环并有界等待其进入运行态（`Syncer.Started()`，此时 `running=true` 已可见），再启动监督器与 Push 循环；二者首检/首发因此不会读到「尚未启动」而误报；
- 外部心跳发送失败只写 WARN，不反向把“监控服务不可达”加入应用健康，避免自激循环。

该监督器仍无法覆盖进程已经死亡的场景，必须由第 7.5/7.6 节补足。

### 7.4 Operational HTTP 端点

保留现有 Docker 端点：

```text
GET /api/health
```

它继续只表示 HTTP 服务可达，避免一次同步失败直接让 Docker 把容器标为 unhealthy 或触发编排重启。

另新增供外部监控使用的固定端点：

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
- 响应固定使用 `Content-Type: application/json; charset=utf-8` 与 `Cache-Control: no-store`；
- 该端点不要求请求体、不接受 query 控制检查范围，也不提供“强制健康”旁路；
- 单次请求现场计算健康，不只返回监督器最多 30 秒前的缓存结果；
- SQLite 检查超过 2 秒按失败返回 503，避免外部监控请求自身长期挂住。

### 7.5 Uptime Kuma 外部拉取

如果 Uptime Kuma 能访问 FWAlizer，固定支持通过 HTTP(s) Monitor 指向：

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

在现有告警配置页增加第三张“外部运行监控（Uptime Kuma Push）”卡片，与邮件/Webhook 一次保存但使用独立配置对象：

```text
启用 Uptime Kuma Push：false
Push URL：空
心跳间隔：60s
```

固定行为：

- 默认关闭；
- URL 作为敏感业务配置写入 SQLite 和完整配置包，但绝不写日志或错误响应；
- 保存后通过协调器无失败发布配置，并立即唤醒 Push 循环；从关闭变为开启或 URL 变化时立即发送第一条，不等待完整 interval；
- 此后按配置 interval 计算一次与 operational endpoint 完全相同的健康状态；
- 健康时请求 `status=up&msg=OK`；
- unhealthy 时请求 `status=down&msg=<短原因>`，短原因由稳定 reasons 用 `; ` 连接并截断到 250 字符以内；
- 进程死亡或完全卡死时不再有 push，Uptime Kuma 依靠缺失 heartbeat 判 DOWN；
- 使用 HTTP GET；解析用户填写的完整 URL，覆盖 `status`、`msg`、`ping` query，保留 Uptime Kuma 自带 token 和其他未知 query；
- `ping` 填写本次 operational health 计算耗时的毫秒数；
- HTTP client 使用 10 秒上限，同一时刻最多一条在途，不排队、不重试；某次仍在途时到达的新 tick 直接跳过并写安全 WARN；
- HTTP 2xx 且 JSON 为 `{"ok":true}` 才算 Push 成功；非 2xx、无效 JSON、`ok!=true` 或网络错误均记失败；
- 成功只写 DEBUG（不含 URL），失败写 WARN（只含安全类别和状态码，不含 URL/token 或底层可能回显 URL 的文本）；
- 不把 Push 失败再发布为邮件/Webhook 告警，避免网络故障时循环放大；
- Push 配置关闭或 URL/interval 非法时不启动循环、不发任何网络请求；
- 应用 shutdown 取消在途请求并退出 Push 循环，不等待完整 10 秒；
- UI 明示：Uptime Kuma 侧的 Heartbeat Interval 必须大于 FWAlizer 的发送间隔并留余量；默认发送 60 秒时推荐 Kuma 设置 120 秒。

官方依据：

- [Uptime Kuma API Documentation - Push Endpoint](https://github.com/louislam/uptime-kuma/wiki/API-Documentation/692198f84f3675a53a8ece7eb91a6a84566ee98e)
- [Uptime Kuma 官方 Go Push 示例](https://github.com/louislam/uptime-kuma/blob/master/extra/push-examples/go/index.go)
- [Uptime Kuma README - HTTP/JSON Query/Push 等监控类型](https://github.com/louislam/uptime-kuma/blob/master/README.md)

### 7.7 最终组合

本 Build 固定同时提供：

1. 保持 `/api/health` 作为 Docker HTTP 存活检查；
2. 新增 `/api/health/operational` 作为应用工作状态检查；
3. 新增内部 30 秒健康监督器，负责在进程仍可运行时发布第三类告警；
4. 可选 Uptime Kuma Push URL，默认每 60 秒发送一次健康心跳；
5. 外部环境能主动访问时，优先再配置 Uptime Kuma HTTP Monitor 拉取 operational endpoint。

固定值为：第三开关“运行健康异常”、`health_timeout=10m`、failed/partial 均 unhealthy、operational endpoint 与 Push 同期实施、配置包 version 3 且拒绝旧版本。代码 Step 不再重新询问这些产品决策；只有发现与 `AGENTS.md` 其他未覆盖强要求的新冲突时才停止。

---

## 八、实施步骤（Step 0～7 已完成后附统一证据）

### Step 0：合同收口

- 把已确认的 version 3、默认全关闭、运行健康与 Uptime Kuma Push 合同同步到 `AGENTS.md`；
- 将 Build7 标为当前构建方案，并同步 Design/Issue/README 的文档定位和旧 version 2 边界；
- 检查所有并行状态文案，避免 Build6“当前方案”与 Build7“待实施”互相矛盾；
- 本 Step 只改文档，不改代码。

**实施状态：** ✅ 已完成（2026-09-28，仅文档）

**实际证据（2026-09-28）：**

- 实际改动：`AGENTS.md`（文档定位、§9.1「Build7 目标合同 + Build6 既有边界」、§八 HEALTHCHECK 语义、§十一 reset 表清单与事件类型、§十二 文档体系表）、`Build7.md`（本文件：授权边界、§八标题、Step 4/5 分界、变更记录、本节证据）、`Build6.md`（标题与定位横幅改为已完成历史构建记录，正文原有证据原文未改）、`Design5.md`、`Issue5.md`、`Issue6.md`、`README.md`（目录结构与文档定位）、`ProdTestList.md`。
- 自动检查：`git diff --check` → 通过；`git diff --name-only` 只含 8 个 `.md` 文件，无任何代码、测试、Schema、前端、配置或依赖改动；文档内互相引用的相对链接全部存在。
- 判别性核对（改动前红 → 改动后绿）：改动前 `grep -n "Build7" AGENTS.md` 无匹配，且 `AGENTS.md` 把 version 2 写成唯一导入导出版本、reset 表清单缺少 `alert_policy`/`uptime_kuma_push`、事件类型缺少 `EventOperationalUnhealthy`；改动后 Build7 合同已写入强要求，reset 清单与事件类型同步，各文档均显式声明「Step 1～6 尚未实施」。
- 人工检查：未执行浏览器、Docker、真实 SMTP/Webhook/Uptime Kuma、真实云或远端 CI；本 Step 不适用以上外部证据。
- 未完成项：无（本 Step 范围内）。
- 与计划偏差：无。按准备报告第 4 节的合并裁决，`.gitignore` 与前端 fallback 文件名属代码/配置，随 Step 1 更新；README 的 version 2 协议细节按用户裁决留到 Step 6。
- 状态：✅ 完成

### Step 1：Schema、配置模型与严格 API

**实施状态：** ✅ 已完成（2026-09-28）：`alert_policy`/`uptime_kuma_push` 单行表、`alert_email.subject|body`、一次性显式迁移（重启不重复归零）、version 3 唯一协议并拒绝 v1/2/其他版本、四对象 `GET/PUT /api/alerts`（GET `no-store`）、reset 默认全关、策略/Push 进入 `RuntimeState`、导出改 `fwalizer-config-v3-*`（含 `.gitignore` 与前端 fallback）；判别性证据：`config/store_v3_test.go`、`webui/api/alerts_v3_test.go`、`export_test.go`、`import_test.go`、`settings_alerts_test.go`、`main_test.go` 进程级往返。

- 新增告警策略持久化、邮件主题/正文和可选健康/Push 设置；
- 更新完整快照、RuntimeState、reset、导入导出和失败回滚；
- 先写 version 1/2/其他版本拒绝、默认全关闭、破坏性 Schema 状态归零和事务失败零发布的判别性测试；
- 不连接任何外部服务。

### Step 2：测试邮件

**实施状态：** ✅ 已完成（2026-09-28）：`notifier.BuildTestEmailContent`/`SendTestEmail`（复用 10s/30s 有界会话）、`POST /api/alerts/test-email`（不写库/不进协调器/不 Apply/不改订阅、无渠道开关要求）、页面测试按钮 + 内存态结果 + 35s 上限；判别性证据：`notifier/email_test.go`（本地假 SMTP：成功/认证失败/RCPT/DATA/静默 deadline）、`webui/api/test_email_test.go`。

- 先用本地假 SMTP 建立成功、认证失败、RCPT/DATA 失败和静默 deadline 用例；
- 实现 `POST /api/alerts/test-email`；
- 实现页面内存态结果与实时安全日志；
- 验证不写库、不 Apply、不改变订阅。

### Step 3：触发条件过滤与邮件内容

**实施状态：** ✅ 已完成（2026-09-28）：`policySubscriptions` 策略驱动订阅（渠道 + 触发同时开启）、固定主题后缀、固定详情顺序（缺失写 `-`）、多收件人逐项 Trim、自动邮件成功 INFO；判别性证据：`webui/api/alertset_policy_test.go`（矩阵 + 真实总线投递 + 热重载切换）、`notifier/email_test.go`。

- 按三个开关安装订阅；
- 统一纯文本格式与固定详情顺序；
- 覆盖默认全关闭、各开关独立、邮件/Webhook 共用策略和热重载边界。

### Step 4：运行健康计算、内部监督器与 operational 端点

**实施状态：** ✅ 已完成（2026-09-28）：`internal/health` 唯一计算源（八步判定、SQLite ≤2s、paused 只查 SQLite/Syncer）、Syncer `RoundStartedAt`/`ProcessStartedAt`、30 秒监督器（边沿一次、恢复 INFO、开关补发、Wake、shutdown 静默）、`GET /api/health/operational`（200/503、`no-store`、现场计算、稳定原因）、`/api/health` 静态语义不变；判别性证据：`internal/health/*_test.go`、`syncer/status_time_test.go`、`webui/api/operational_test.go`、`webui/server_test.go`、`main_test.go` 进程级用例。

- 实现唯一 `OperationalHealth` 计算源（`internal/health` 包，供内部监督器、HTTP 端点与 Push 共用）；
- 增加轮次开始时间/超时观测；
- 实现健康状态边沿事件，持续异常不重复通知；
- 保持 shutdown 有界且 race 通过。
- 实现 `GET /api/health/operational`：健康 200、异常 503、固定 `Content-Type: application/json; charset=utf-8` 与 `Cache-Control: no-store`、现场计算、只返回稳定原因；`/api/health` 静态存活语义保持不变。
- 判别性覆盖：SQLite 正常/失败/2 秒超时、Syncer 未运行、暂停、启动宽限、在途未超时/超时、success/failed/partial/idle、调度停滞、多原因固定排序、恢复后再次异常重新发送一次；
- 端点覆盖：健康 200、异常 503、静态 `/api/health` 始终不受同步状态影响、响应不泄露底层错误。

### Step 5：外部监控接入（Uptime Kuma Push）

**实施状态：** ✅ 已完成（2026-09-28）：`internal/health/push.go`（默认关闭、启用/URL 变化立即首发、覆盖 status/msg/ping 且保留 token 与未知 query、10 秒上限、单在途跳过、不排队不重试、2xx+`{"ok":true}` 才算成功、失败不改健康不自激、日志不含 URL/token、shutdown 取消在途）、协调器唤醒顺序「告警集合 → 监督器 → Push → RuntimeState」（当时实现顺序，已由下述 P2-04 后续修复纠正）；判别性证据：`internal/health/push_test.go`（本地 `httptest`）、`webui/api/operational_test.go`、`main_test.go`（真实二进制 + 本地 mock 首发心跳）。

**后续纠正（2026-09-30，P2-04）：** 独立授权修复配置热更新发布时序，当前代码按「日志级别 → 告警集合 → RuntimeState → 监督器唤醒 → Push 唤醒」执行；新增两条发布分支的 Wake 可见性、PUT/import 与真实 Pusher 本地首发回归。该修复不修改启动顺序或 Push 内部机制，P3-03 与真实 Uptime Kuma DOWN/恢复验收仍独立未完成；本轮门禁详见 `fwalizer-audit-final1.md` P2-04 实施补记。

**后续纠正（2026-09-30，P3-03）：** 上述 P2-04 补记中的「P3-03 未完成」为当时状态；本轮在 `7acf303` 基线按用户独立授权完成 P3-03 本地修复。非空非法 URL 校验失败后使用 timer/Wake/Stop 等待，timer 到期重读运行配置；非正间隔回退 60s、正值不足 20s 按 20s，保留未激活状态和正常发送周期。新增真实 20s timer、无 Wake 重读恢复、Wake/Stop、关闭/恢复、脏 SQLite、普通失败控制与日志脱敏用例；两个 overlay 负向控制精确失败，修复版定向 race、快速用例 20 轮 race、全量 12 包 race 连续 3 轮、vet/build/前端 build 与格式/diff-check 通过。本机工具链 Go 1.26.4，未单独运行 Go 1.25；**该批次改动已提交为 `c5cc79d`**（不再处于未提交状态）、未推送。空 URL/关闭分支、健康、告警、HTTP 超时和配置发布顺序不改；真实 Uptime Kuma DOWN/恢复与远端 CI/GHCR 仍未执行。详细证据见 `fwalizer-audit-final1.md` P3-03 当前实施补记。

**第二轮核验补记（2026-09-30，仅测试改动）**：为 P2-05 的「在途名额保持到读取与关闭结束」补一条此前缺失的判别性用例 `TestWebhookResponseSlotHeldUntilClose`（`notifier/webhook_response_test.go`）：闸门阻塞响应体读取期间断言 `InFlight()==1` 且响应体未关闭，放行并返回后断言 `InFlight()==0` 且响应体已关闭。判别力经「把 `defer release()` 改为读取前提前释放 → 精确失败 → 恢复 → 通过」验证；`-race -count=20` 通过。同时订正本文件 P3-03 段落的提交状态（已提交 `c5cc79d`）。门禁范围与边界见 `fwalizer-audit-final1.md`「最近核验基线」小节；**真实钉钉/飞书/Slack 接收仍未执行**。

- operational endpoint 已在 Step 4 实现，本 Step 只接入 Uptime Kuma Push 与配置热重载；
- URL 脱敏、超时、无重试和无自激循环；
- 使用本地 `httptest` 验证协议，不访问真实 Uptime Kuma。
- 判别性覆盖：默认/关闭时零请求、启用后立即首发、周期 up、异常 down、恢复 up、query 安全覆盖且 token 保留、`ping` 数值、250 字符 msg、非 2xx、`ok=false`、坏 JSON、10 秒有界超时、在途时丢弃新 tick、URL/interval 热重载、关闭后停止、shutdown 取消、全部日志不含完整 URL/token；
- 文档给出 Uptime Kuma HTTP Monitor 与 Push Monitor 的最小配置步骤，但真实 DOWN/恢复通知仍留给外部人工验收。

### Step 6：统一验收与文档闭环

**实施状态：** ✅ 已完成（2026-09-28）：统一门禁、真实二进制/Docker 容器验收与本文档闭环均已完成，证据见下节。


- targeted tests → 相关包 race → `go test ./... -race -count=1` → vet/build；
- 前端 `npm ci`、build、生产与完整 audit；
- 浏览器检查默认全关闭、编辑/保存、测试中/成功/失败和内存态消失；
- 真实 SMTP/收件箱、真实 Webhook、真实 Uptime Kuma HTTP/Push 分层记录，未执行不得写成通过；
- 最后更新 README、AGENTS、Design、Issue 与生产测试清单。

### Step 7：核验缺陷修复

**实施状态：** ✅ 已完成（2026-09-28）。本节记录 Step 0～6 完成后一次独立只读核验所发现问题的修复。
该核验同时确认：**P1 是全项目唯一一处「前端发送后端 DTO 没有的字段」缺陷**（19 处写请求与全部
GET 响应字段已逐字段核对，其余 18 处对齐）。

| 编号 | 问题 | 根因 | 修复 |
|---|---|---|---|
| P1 | 告警页「测试发送邮件」必然 HTTP 400，成功态不可达 | `Alerts.vue` 序列化整个 email 表单对象（多一个 `enabled`），而后端 `testEmailRequest` 只有 8 个字段且严格解码拒绝未知字段 | 前端新增 `TestEmailPayload` 类型并显式构造 8 个发送字段（`webui/frontend/src/types.ts`、`Alerts.vue`）；后端 DTO 与严格解码契约不变 |
| P2 | 重启时把「同步引擎尚未启动」误判为「引擎未运行」：WARN 运行健康异常、Push 首条心跳 `status=down`；开启第三开关 + 渠道时误发一次告警 | `run.go` 先启动监督器/Push、后启动 `Syncer.Run`，而 `running=true` 只在 `Run` 内置位 | ① 启动顺序确定性化：`Syncer` 暴露 `Started()` 信号（在 `running=true` 可见后关闭一次），`run.go` 有界等待后再启动监督器与 Push；② 健康判定增加固定启动宽限 `StartupGrace=10s`，并用 `SyncStatus.started_at` 区分「从未启动」与「启动后停止」（后者立即异常） |
| P3 | Webhook 正文遍历 map，同一事件顺序随机 | `notifier/webhook.go` 使用 `formatEventBody`（map 遍历） | 抽取共用固定渲染器：Webhook 复用 `formatEventDetails`，删除 `formatEventBody`；运行健康异常事件在详情块末尾追加固定「原因」行（无原因不输出） |
| P4 / P4b / P5 | 滞后注释：多处仍写「version 2」、发布顺序注释缺监督器/Push、快照注释与实现不符 | 文档滞后 | 注释与实现、合同口径对齐（零行为变更） |
| P6 / P7 | 导出/导入弹窗敏感清单未列 Push URL；Webhook URL 为明文输入 | 体验与一致性 | 弹窗补 `Uptime Kuma Push URL（含 token）`；Webhook URL 改密码型可切换显示 |

**判别性证据（本机 2026-09-28）：**

- P1：新增 `main_test.go:TestProcessTestEmailWithUIPayload`（真实二进制 + 本地假 SMTP）：与页面一致的 8 字段载荷 → `200 {"success":true,"message":"SMTP 服务器已接受测试邮件"}`，并断言主题固定后缀、正文固定说明与多收件人逐项 Trim；同一载荷多带 `enabled` → `400 请求体包含未知字段: enabled`（修复前页面载荷正是该形态）。
- P2：新增 `main_test.go:TestProcessRestartPushFirstHeartbeatIsUp`（持久化 Push 配置 + `GOMAXPROCS=1` 重启）：首条心跳必须 `status=up&msg=OK`、token 与未知 query 保留、启动日志不得出现「运行健康异常」；修复前实测 12/12 复现 `status=down&msg=同步引擎未运行`，并真实向本地 mock 发出一次误报 Webhook。新增 `internal/health:TestEvaluateStartupGrace`（宽限内健康 / 超宽限异常 / 已启动后停止立即异常）与 `syncer:TestStartedSignalClosesAfterRunBegins`、`syncer:TestStartedNeverClosesWhenStopPrecedesRun`。
- P3：新增 `notifier:TestWebhookContentIsDeterministicAndUnified`（同一事件连续 20 次渲染逐字节一致、三渠道、固定顺序、不含未固定键）、`notifier:TestWebhookSyncEventUsesFixedBlockWithoutExtraKeys`、`notifier:TestEmailOperationalEventIncludesReasons`。
- 统一门禁：`gofmt -l`（无输出）、`go build ./...`、`go vet ./...`、`go test ./... -race -count=1`（12 包全绿）、前端 `npm run build` 与两条 `npm audit`（0 漏洞）、`docker build` + 容器非 root（uid 1000）/`healthy`/`docker stop` 有界且退出码 0。
- **未执行 / 无真实结论（不得写成通过）**：真实 SMTP 与收件箱、真实 Webhook、真实 Uptime Kuma HTTP/Push 的 DOWN/恢复通知、浏览器人工交互回归（本环境无浏览器工具，PT-B7-07 仍未执行）、真实云 API 与远端 CI/GHCR。

---

### Step 0～7 统一证据（2026-09-28，本机）

- **自动门禁**：前端 `npm ci`、`npm run build`、`npm audit --audit-level=high`、`npm audit --omit=dev --audit-level=high`（均 0 漏洞）；`go test ./... -race -count=1`（12 包全绿）、`go vet ./...`、`go build ./...`、`gofmt -l`（本次修改文件无输出）、`docker compose -f docker-compose.yml.example config --quiet`、`docker build -f build/Dockerfile -t fwalizer:build7 .`、`git diff --check`。
- **真实二进制（进程级）**：静态 `/api/health` 恒为 `{"status":"ok"}`；`/api/health/operational` 返回 200/`reasons:[]`；Push 配置保存后立即向本地 HTTP mock 首发 `status=up&msg=OK&ping=<ms>` 且保留 token 与未知 query；SIGTERM 退出码 0。
- **Docker 容器运行**：非 root（`uid=1000(appuser)`）；`HEALTHCHECK` 仍指向 `/api/health` 并在 10 秒内 `healthy`；SPA 首页与其 JS 资源 200；`docker stop` 有界返回且退出码 0；关闭日志无伪运行健康异常。
- **健康/监督器判别性覆盖**：SQLite 失败与探活阻塞、主循环未运行、暂停跳过轮次类检查、轮次超时、failed/partial 与 success/idle 覆盖、启动宽限、调度停滞、多原因固定顺序去重、边沿一次发布/原因变化不重发/恢复 INFO/恢复后再异常再发一次/开关补发一次/Wake 立即检查/Stop 有界静默。
- **Push 判别性覆盖**：关闭零请求、启用立即首发、周期 up→down→up、query 覆盖与 token 保留、`ping` 数值、250 字符 msg、非 2xx/`ok=false`/坏 JSON、超时有界、在途跳过不排队、URL/interval 热重载、关闭停止、shutdown 取消、日志脱敏、失败不影响健康与不发布事件。
- **未执行/无真实结论（不得写成通过）**：真实 SMTP 接受与收件箱投递、真实 Webhook、真实 Uptime Kuma HTTP Monitor 与 Push 的 DOWN/恢复通知、真实云 API、真实 WAN/反向代理异常、浏览器人工交互回归（本环境无浏览器工具，仅验证了 HTTP 层与构建产物）、远端 GitHub Actions 与 GHCR 发布。上述项目登记在 [ProdTestList.md](./ProdTestList.md)。
- **Step 7 核验缺陷修复**：判别性证据见上表（P1 假 SMTP + UI 载荷、P2 `GOMAXPROCS=1` 重启首条心跳为 up 且无伪 WARN、P3 同一事件 20 次渲染逐字节一致），并在同一批次重跑全量门禁通过。


### P2-05 后续本地证据（2026-09-30）

- 修复与测试提交：`93e0e4b`；本次文档同步仅回写已有证据，不重新运行或新增代码门禁。
- `notifier/webhook_response_test.go` 覆盖三渠道成功/业务失败、缺字段/null/类型错误、飞书双字段冲突、空/非法/尾随 JSON、16 KiB 边界和实际读取量、读取/关闭错误安全、三渠道 EventBus WARN、真实本地 HTTP 连续交替响应及生产默认 10 秒正文 deadline；既有正文与限流夹具使用渠道合法成功响应。
- 定向响应测试 `-race -count=20`、notifier 整包 `-race -count=3`、全量 12 包 `go test ./... -race -count=1 -timeout=20m`、`go vet ./...`、`go build ./...`、`git diff --check` 均在修复轮本地通过。重复证据只覆盖对应测试范围，不把单次全量绿色外推为全仓长期稳定绿色。
- 真实钉钉/飞书/Slack 接收、此次改动的产品浏览器与远端 CI/GHCR 未执行；PT-B7-03 继续保持人工免除、非已通过。完整修复前后证据见 [审计报告 P2-05](./fwalizer-audit-final1.md)。

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
- 实现者试图把 version 3、`health_timeout=10m`、failed/partial unhealthy 或 Push 同期实施重新改为未裁决；
- 真实外部服务凭据不可用时，任何人试图用 mock 替代并把外部验收写成通过。

---

## 十一、文档变更记录

| 版本 | 日期 | 说明 |
|------|------|------|
| v1.4 | 2026-09-30 | 同步独立 P2-05 修复（`93e0e4b`）：三渠道明确业务成功响应、16 KiB 有界读取、原 10 秒超时与安全错误；保留原异步/限流/配置/Push 边界；回写本地 race/vet/build 证据，真实 Webhook/产品浏览器/远端 CI 未验收 |
| v0.1 | 2026-09-28 | 建立 Build7 研究初稿：记录告警开关、纯文本邮件、测试邮件、运行健康与 Uptime Kuma 两种接法；运行健康和配置协议尚待裁决 |
| v1.0 | 2026-09-28 | 用户确认完整方向：固定默认全部关闭、运行健康异常、`health_timeout=10m`、failed/partial→503、operational endpoint、Uptime Kuma Push、version 3 且拒绝旧版本；补齐 Schema、配置包、API、页面、健康算法、Push 生命周期、测试矩阵与 Step 0～6 合同 |
| v1.2 | 2026-09-28 | **Step 1～6 实施完成**：Schema/version 3 配置包与四对象告警 API、测试邮件、触发过滤与邮件内容、唯一运行健康计算源 + 内部监督器 + operational 端点、Uptime Kuma Push；统一门禁、真实二进制与 Docker 容器证据见第八节；真实 SMTP/收件箱、Webhook、Uptime Kuma 与远端 CI 仍未执行 |
| v1.3 | 2026-09-28 | **Step 7 核验缺陷修复**：①测试邮件 UI 载荷改为显式 8 字段（修复修复前必然 HTTP 400、成功态不可达）；②启动顺序确定性化（`Syncer.Started()` + `run.go` 有界等待）并新增固定 10s 启动宽限与 `SyncStatus.started_at`（修复重启误报 Push DOWN 与运行健康异常）；③Webhook 与邮件共用固定详情渲染器，运行健康异常追加固定「原因」行；④version 2 滞后注释、发布顺序注释、导出/导入敏感清单与 Webhook URL 输入样式收口；`AGENTS.md` §9.1 同步启动宽限与两渠道详情块条款 |
| v1.1 | 2026-09-28 | Step 0 合同收口（仅文档）：一次性授权取代逐 Step 授权；`AGENTS.md` 同步 Build7 的 version 3 目标合同、默认全部关闭、运行健康、Uptime Kuma Push、新表/新列与 `EventOperationalUnhealthy`，reset 表清单与 HEALTHCHECK 语义同步，Build6 定位改为已完成历史构建记录；Step 4 与 Step 5 的 operational endpoint 分界固定为「Step 4 实现计算源与端点，Step 5 只做 Push」。Step 1～6 尚未实施（该状态已由 v1.2 取代） |
