# FWAlizer 告警与运行健康构建计划（Build7：Step 0～7 已实施完成）

> **文档定位：** 本文档记录 Build6 完成后的下一阶段已定案方案，聚焦告警触发开关、纯文本邮件内容、测试邮件、轻量运行健康检查，以及 Uptime Kuma HTTP/Push 外部监控。Build 文档仍为非强制执行建议，唯一强要求是 [AGENTS.md](./AGENTS.md)。
>
> **P3-19 后续诊断边界（2026-10-03，用户确认方案 B，已本地修复）：** §1.1 目标 4、§二决策 12、§5.2/5.3/5.4 及§九中的“完整阶段错误/完整诊断/完整错误返回”为 2026-09-28 历史约定；当前合同替换为 SMTP 会话内生成的安全诊断：只含固定调用阶段、SMTP 数字响应码或固定类别，不含服务器/底层错误原文或原始错误链。下文保留旧约定并标注替代关系；正式工作树证据与外部边界见文末 P3-19 后续补记。§4.4 告警业务事件详情块的“错误”内容不属于本次 SMTP 发送错误治理。
>
> **历史实施前基线（2026-09-28）：** 2026-09-28 初始只读核验基线为 `main` / `be41fb4ae38ed12fa8f218195123a8cc2cc17726`，工作区在建立本文档前无未提交改动，分支相对 `origin/main` ahead 1；初稿随后提交为 `641c09c`，本次定案在该初稿上继续更新。当时邮件与 Webhook 已订阅 DNS 解析失败和 Provider × 域名同步最终失败事件，尚无测试邮件 API、触发条件开关、可编辑邮件主题/正文或运行健康反向心跳；这些缺口随后按 Step 1～7 实施，当前完成状态见第八节与第十一节。
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
4. 增加测试邮件 API，使用页面当前未保存的表单值，直接报告 SMTP 是否已接受邮件或返回完整阶段错误（历史约定；P3-19 后改为安全阶段诊断，见 §5.2）。
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
| 3 | Webhook 渠道默认值 | 默认关闭、空 URL、空 channel；三个渠道单选初始未选中，启用必选，关闭保留输入（2026-10-08 I8-113 细化 A） |
| 4 | DNS 失败触发默认值 | 默认关闭 |
| 5 | Provider × 域名最终失败触发默认值 | 默认关闭 |
| 6 | 运行健康异常触发默认值 | 默认关闭 |
| 7 | 触发策略归属 | 三个触发开关是邮件和 Webhook 共用的全局告警策略，不为两个渠道复制两套开关 |
| 8 | 邮件内容 | 一套可编辑主题 + 一套可编辑纯文本正文；系统在正文后追加固定事件详情 |
| 9 | 测试邮件 | 新增独立 API；使用当前未保存表单值；不要求邮件渠道已启用；不保存、不热重载、不改变订阅 |
| 10 | 测试结果 | 只保留在当前告警页面内存态；刷新页面或重启后消失；同时写现有 stdout/WebUI 实时日志 |
| 11 | 成功口径 | 只显示“SMTP 服务器已接受测试邮件”，不得写成“已送达收件箱” |
| 12 | 失败口径 | 历史约定：直接向用户展示完整 SMTP 阶段错误；不得主动拼接密码或完整邮件正文。**P3-19 已替代为固定阶段、数字响应码或固定类别，不含服务器/底层原文或原始错误链** |
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

## 三、历史实施前实现基线（2026-09-28）

> 本节“当前/已有/没有”等措辞均指上述实施前基线，保留用于对照构建起点；不代表 Step 0～7 完成后的现时功能状态。后续独立修复见各补记及 [审计报告](./fwalizer-audit-final1.md)，外部验收状态见 [ProdTestList.md](./ProdTestList.md)。

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

`alert_webhook` 保持现有字段形态，但 `enabled` 默认和既有 Build7 一次性迁移归零结果固定为 0。I8-113 后新 channel 列默认空字符串；初始化和 reset 显式写入空渠道，旧表不重建。已有渠道/URL 保留；仅本次新增 channel 列时，已有 id=1 主行补固定历史钉钉值，无主行则初始化空值，再次启动不重复转换。迁移失败整个事务回滚。实际迁移可使用安全的建表/复制/替换或等价显式 SQL；不得依赖 `CREATE TABLE IF NOT EXISTS` 自动补列。迁移完成必须保证各单行表至多一行且业务 ID 固定为 1。

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
      "channel": ""
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

- version 3 是唯一可导入/导出的版本；Webhook channel 仍必须存在且为字符串，关闭允许空、开启必须为合法渠道；GET/导出保留空值。新版接受原有合法 version 3 包；旧版可能拒绝新增关闭空渠道包，用户已接受该边界（I8-113）；
- 配置导入使用独立的标准库 JSON v2 严格解码：字段名大小写必须精确匹配，任意对象内拒绝重复字段（同值也拒绝），拒绝非法 UTF-8 与孤立代理项；合法 JSON 转义按解码后名称判断，`version` 与 `\u0076ersion` 同时出现仍为重复。先完成 10 MiB 有界读取，超限统一 413；其余解析/字段错误为安全 400，仅返回固定原因与必要字段路径，不回显原始错误或字段值。全部解码与领域校验完成后才进入配置写事务，非法输入零写入零发布；普通 API 不随本项切换解析器。（2026-10-08 I8-111 严格方案 B，替代此前共享普通 API 解码器的边界。）
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

### 邮件传输编码（P3-20 后续补记，2026-10-03）

邮件在共用发送出口序列化为 MIME：非 ASCII 主题使用标准库 RFC 2047 UTF-8 B 编码，只在 encoded-word 之间以 CRLF + SPACE 折叠，首行长度计入 `Subject: ` 前缀；encoded-word 不超过 75 字符，含 encoded-word 的物理行不超过 76 字符。声明 `MIME-Version: 1.0`、`Content-Type: text/plain; charset=UTF-8` 与 `Content-Transfer-Encoding: base64`；仅在序列化时将正文 CRLF/LF/裸 CR 统一为 CRLF，然后 Base64 每 76 字符折行，解码后保留规范化原文的末尾空白及换行有无。自动告警与测试邮件共用此出口，业务主题后缀/详情渲染与 Webhook 文本不变；不扩展邮箱地址或 SMTPUTF8 合同。

主题/正文编辑与持久化仍保留原始业务文本，编码只作用于 SMTP DATA 报文。[RFC 2047](https://www.rfc-editor.org/rfc/rfc2047) 定义头部 encoded-word 与折叠边界；[RFC 2045](https://www.rfc-editor.org/rfc/rfc2045) 定义 MIME 与 Base64 编码行长；[RFC 2046 §4.1.1](https://www.rfc-editor.org/rfc/rfc2046) 定义纯文本 CRLF 换行。自动邮件、测试邮件均使用该编码，真实收件箱显示与投递仍待人工确认。

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

- Webhook channel 按 Lower＋TrimSpace 归一化：关闭允许空或 dingtalk/feishu/slack，开启必须选择合法渠道并填写 host 非空的绝对 HTTP/HTTPS URL；未知、缺失、null 继续拒绝。通知器取消空值钉钉兜底，对相关事件在限流与 HTTP 前安全拒绝非法渠道。

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
    "channel": ""
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
- GET 四对象由 `Store.LoadAlertsSnapshot` 在一个窄只读事务内读取，共用现有 loader；事务在返回前结束，不读取 targets/rules/settings 或进入协调器，默认值仍由 HTTP 层补齐；只保证单次响应内部一致，不新增编辑冲突检测（I8-18 于 2026-10-08 按用户方案 A 修复）。
- GET 为现有告警表单返回完整敏感对象的既有边界，仍会返回 SMTP 密码、Webhook URL 和 Push URL；响应必须 `Cache-Control: no-store`，不得被日志中间件记录 body。

### 4.7 告警页面布局

现有 `/alerts` 页面保持单页，按以下顺序放置四张卡片：

1. **触发条件**：DNS 解析失败、Provider × 域名最终失败、运行健康异常三个开关；第三个开关下显示 `health_timeout`；
2. **邮件告警**：渠道开关、SMTP 字段、主题、纯文本正文、测试发送按钮和本次测试结果；
3. **Webhook 告警**：保留渠道开关；钉钉/飞书/Slack 单选组放在 URL 前，初始全部未选中；URL 提示随是否选择渠道变化，开启未选时提示并零 PUT，关闭再开启保留渠道和 URL；
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

SMTP 或网络失败（2026-09-28 历史响应示例，已由下方 P3-19 安全示例替代）：

```json
{
  "success": false,
  "error": "SMTP 认证失败: 535 Authentication failed"
}
```

当前安全响应示例（P3-19）：

```json
{
  "success": false,
  "error": "SMTP 认证失败: SMTP 响应码 535"
}
```

- JSON/字段校验错误仍使用 HTTP 400；
- SMTP、TLS、认证、MAIL、RCPT、DATA、QUIT 等预期外部错误使用结构化测试结果返回，页面直接展示 `error`；
- **历史约定（已被 P3-19 替代）：** 错误允许包含 SMTP 返回的完整诊断，不主动脱敏 host/IP/状态文本，但不得主动加入密码、请求正文或配置包内容；
- **当前约定：** notifier 内部仅复制固定调用阶段、标准库解析的 200～599 数字响应码或固定类别；范围外响应码归入响应格式异常，其他错误按超时、TLS、协议格式、关闭、网络、会话/安全策略顺序判别。不复制 `Msg` 或底层 `Error()` 文本，不包装原始 cause，不通过 `Unwrap` 暴露原文；API 继续 HTTP 200、`success=false`/`error` 与 `no-store`；
- `success=true` 只证明 SMTP 服务器接受，不证明最终投递、收件箱展示或垃圾邮件分类。

### 5.3 告警页面

- 邮件开关只控制自动通知，不再用它禁用 SMTP 表单输入；用户可以在自动通知关闭时编辑并测试；
- 邮件卡片增加主题单行输入和纯文本多行输入；
- 增加页面级大按钮“测试发送邮件”；
- 按钮请求期间 loading，防止同一页面重复点击；
- 按钮下使用简单结果区域：测试中、SMTP 已接受、失败安全诊断；原“失败完整错误”为已被 P3-19 替代的历史约定；
- 结果只存在于当前 `Alerts.vue` 组件内存，刷新页面即消失；
- 不增加结果查询 API、SSE、轮询或发送历史页面。

### 5.4 日志

测试邮件（当前安全诊断示例；旧完整诊断示例由 P3-19 替代）：

```text
INFO 测试邮件已被 SMTP 服务器接受 to=admin@example.com
WARN 测试邮件发送失败 error="SMTP 认证失败: SMTP 响应码 535"
```

自动邮件：

```text
INFO 邮件告警已被 SMTP 服务器接受 event=dns:failed
WARN 事件处理失败 type=sync:error error="SMTP 认证失败: SMTP 响应码 535"
```

日志进入既有 stdout 与 WebUI 实时日志流，不写 `sync_logs`，不增加新表。日志不得包含 SMTP 密码、完整正文或 Webhook URL；SMTP 发送错误同时禁止服务器/底层诊断原文与原始错误链。

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

**P3-15 后续实施说明（2026-10-03）**：满载丢弃日志按长期渠道限流器聚合，新活动周期首条立即 WARN，后续约每 30 秒汇总新增丢弃；空窗口停止计时，关闭后可汇总此前尾批。固定计数区分 DNS 失败、同步失败、运行健康异常及安全平台；混合 Webhook 平台标记 `mixed`，跨热重载与旧实例晚到回调保持计数连续。此聚合仅作用于丢弃日志，告警事件粒度、订阅开关、实际发送及运行健康语义沿用既有合同；不增加持久化、shutdown 强制汇总或通知重试。正式本地证据与剩余边界见 [审计 P3-15 当前实施补记](./fwalizer-audit-final1.md#p3-15-当前实施补记2026-10-03推荐方案-b)，既有真实通知验收状态保持。

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
- 此后按有效 interval 计算一次与 operational endpoint 完全相同的健康状态；普通保存保留截止时间，间隔变化按最近尝试结束时间+新间隔重算，已到期只尝试一次；timer 到期先重读配置。有效间隔非正回退60s，正值不足20s按20s，不回写配置；失败与非法 URL 校验也从尝试结束后等待。
- 健康时请求 `status=up&msg=OK`；
- unhealthy 时请求 `status=down&msg=<短原因>`，短原因由稳定 reasons 用 `; ` 连接并截断到 250 字符以内；
- 进程死亡或完全卡死时不再有 push，Uptime Kuma 依靠缺失 heartbeat 判 DOWN；
- 使用 HTTP GET；解析用户填写的完整 URL，覆盖 `status`、`msg`、`ping` query，保留 Uptime Kuma 自带 token 和其他未知 query；
- `ping` 填写本次 operational health 计算耗时的毫秒数；
- HTTP client 使用 10 秒上限，同一时刻最多一条在途，不排队、不重试；某次仍在途时到达的新 tick 直接跳过并写安全 WARN；
- HTTP 2xx 且 JSON 为 `{"ok":true}` 才算 Push 成功；非 2xx、无效 JSON、`ok!=true` 或网络错误均记失败；
- 成功只写 DEBUG（不含 URL），失败写 WARN（只含安全类别和状态码，不含 URL/token 或底层可能回显 URL 的文本）；
- 不把 Push 失败再发布为邮件/Webhook 告警，避免网络故障时循环放大；
- Push 配置关闭或 URL 为空时等待 Wake；非空非法 URL 按有效间隔重校验、不发网络请求，同配置 Wake 不重复校验或推迟等待。API/导入仍拒绝不足20s间隔；运行端夹紧绕过校验的脏配置。
- 应用 shutdown 取消在途请求并退出 Push 循环，不等待完整 10 秒；
- UI 明示：Uptime Kuma 侧的 Heartbeat Interval 必须大于 FWAlizer 的发送间隔并留余量；默认发送 60 秒时推荐 Kuma 设置 120 秒。

官方依据：

- [Uptime Kuma API Documentation - Push Endpoint](https://github.com/louislam/uptime-kuma/wiki/API-Documentation/692198f84f3675a53a8ece7eb91a6a84566ee98e)
- [Uptime Kuma 官方 Go Push 示例](https://github.com/louislam/uptime-kuma/blob/master/extra/push-examples/go/index.go)
- [Uptime Kuma README - HTTP/JSON Query/Push 等监控类型](https://github.com/louislam/uptime-kuma/blob/master/README.md)

**P3-21 后续响应读取合同（2026-10-03，方案 B）：**

- Push 的 HTTP 2xx 业务正文上限为 **16 KiB**，最多读取 16,385 字节识别超限；按实际 `Response.Body` 字节计数，默认 gzip 解压后的正文仍受保护。完整读取成功、未超限、整个正文为一个合法 JSON 文档且 `ok` 为非 null 的 true 才确认成功；允许合法尾随空白与未知字段，保留 `encoding/json` 的字段大小写匹配及重复键覆盖规则，不承诺拒绝所有重复键。非 2xx 及时记 `http_status` 并关闭，不主动额外 drain；读取失败/超限/非法 JSON/未确认 ok 分别记固定 `response_read`/`response_too_large`/`invalid_json`/`not_ok`。Close 错误只记固定 `response_close` WARN，不输出错误原文或敏感正文，不推翻已确认的业务结果；在途名额保持到读取与关闭结束。10 秒时限、单在途、不排队不重试、失败不改健康与 shutdown 取消继续有效。业务读取上限不是整个网络栈总下载上限；标准 HTTP/1 Transport 关闭后可能有界清理，异常连接复用不作保证，不将标准库清理的版本实现数值写为项目强要求。
- 16 KiB 为本工具的本地保护策略，不是 Uptime Kuma 官方响应配额；官方正常成功响应为 `{"ok":true}`。JSON 兼容语义参见 [encoding/json Unmarshal](https://pkg.go.dev/encoding/json#Unmarshal)。
- Webhook 的 ≤16 KiB 2xx 已读至 EOF；非 2xx 和超限仍及时关闭，保留 P2-05 的渠道协议。Go 1.27 标准 HTTP/1 Transport 会在 Close 后尝试有界清理（[官方发布说明](https://go.dev/doc/go1.27#net/http)），接受异常连接可能无法复用，不增加应用层主动 drain 或通用响应框架。

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
| notifier 单测 | MIME 字段、B 编码主题/折叠、Base64 正文/行长、CRLF 解码等价性、SMTP 接受/阶段错误、触发过滤、在途上限 | 真实 SMTP/收件箱 |
| API 集成 | 严格 JSON、事务、测试 API 零写入零发布、安全阶段诊断（原完整错误返回约定已由 P3-19 替代） | 浏览器交互 |
| 健康状态测试 | SQLite/Syncer/failed/partial/超时/暂停/idle 的确定性结果 | 进程死亡、宿主机断电 |
| HTTP/Push mock | 200/503、Push up/down、URL 脱敏、超时无重试；P3-21 新增 16 KiB/加一字节、完整正文、Content-Length/chunked/gzip、截断/停滞/取消、Read/Close 名额与正常失败后周期回归 | 真实 Uptime Kuma，PT-B7-05 未执行 |
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
| v1.9 | 2026-10-08 | I8-113 / Q-09 细化 A：空初始值＋单选按钮、关闭可空/开启必选、历史缺列主行仅转换一次、初始化/reset 显式未选择；保留 version 3 并接受旧版导入限制；正式证据见后续补记 |
| v1.8 | 2026-10-08 | I8-111 严格方案 B：导入 JSON v2，字段名精确匹配、全层级重复字段及非法 Unicode 拒绝；先完整有界读取保证超限 413，安全错误转换；正式证据见后续补记，外部状态不升级 |
| v1.5 | 2026-10-03 | P3-19 定型 B 本地修复：SMTP 11 出口安全错误取代完整诊断历史约定；固定阶段/数字码/类别、无原文/原始链；正式门禁见后续补记，外部验收未执行 |
| v1.7 | 2026-10-03 | P3-21 方案 B：Push 16 KiB 有界完整正文与单 JSON 校验、非 null ok、安全 Close WARN；Webhook 保留及时失败，Go 1.27 清理与复用边界更新；正式门禁见补记，外部验收未升级 |
| v1.6 | 2026-10-03 | P3-20 共用邮件出口采用 B 编码主题与 Base64 正文；首词/词间折行、CRLF 规范化、76 字符正文行长与解码语义验证；外部投递状态保持 |
| v1.4 | 2026-09-30 | 同步独立 P2-05 修复（`93e0e4b`）：三渠道明确业务成功响应、16 KiB 有界读取、原 10 秒超时与安全错误；保留原异步/限流/配置/Push 边界；回写本地 race/vet/build 证据，真实 Webhook/产品浏览器/远端 CI 未验收 |
| v0.1 | 2026-09-28 | 建立 Build7 研究初稿：记录告警开关、纯文本邮件、测试邮件、运行健康与 Uptime Kuma 两种接法；运行健康和配置协议尚待裁决 |
| v1.0 | 2026-09-28 | 用户确认完整方向：固定默认全部关闭、运行健康异常、`health_timeout=10m`、failed/partial→503、operational endpoint、Uptime Kuma Push、version 3 且拒绝旧版本；补齐 Schema、配置包、API、页面、健康算法、Push 生命周期、测试矩阵与 Step 0～6 合同 |
| v1.2 | 2026-09-28 | **Step 1～6 实施完成**：Schema/version 3 配置包与四对象告警 API、测试邮件、触发过滤与邮件内容、唯一运行健康计算源 + 内部监督器 + operational 端点、Uptime Kuma Push；统一门禁、真实二进制与 Docker 容器证据见第八节；真实 SMTP/收件箱、Webhook、Uptime Kuma 与远端 CI 仍未执行 |
| v1.3 | 2026-09-28 | **Step 7 核验缺陷修复**：①测试邮件 UI 载荷改为显式 8 字段（修复修复前必然 HTTP 400、成功态不可达）；②启动顺序确定性化（`Syncer.Started()` + `run.go` 有界等待）并新增固定 10s 启动宽限与 `SyncStatus.started_at`（修复重启误报 Push DOWN 与运行健康异常）；③Webhook 与邮件共用固定详情渲染器，运行健康异常追加固定「原因」行；④version 2 滞后注释、发布顺序注释、导出/导入敏感清单与 Webhook URL 输入样式收口；`AGENTS.md` §9.1 同步启动宽限与两渠道详情块条款 |
| v1.1 | 2026-09-28 | Step 0 合同收口（仅文档）：一次性授权取代逐 Step 授权；`AGENTS.md` 同步 Build7 的 version 3 目标合同、默认全部关闭、运行健康、Uptime Kuma Push、新表/新列与 `EventOperationalUnhealthy`，reset 表清单与 HEALTHCHECK 语义同步，Build6 定位改为已完成历史构建记录；Step 4 与 Step 5 的 operational endpoint 分界固定为「Step 4 实现计算源与端点，Step 5 只做 Push」。Step 1～6 尚未实施（该状态已由 v1.2 取代） |

### P3-19 后续实施补记（2026-10-03，定型方案 B）

- **授权与恢复点：** 用户依据《研究 P3-19 修复方案》定案授权正式修复与文档同步；实施前 `main / f0997cd`，本地 `origin/main / 5e1d79c`、ahead 6，工作树干净。前序研究与候选已实际读取，正式门禁重新运行，未将候选结果充当正式修复证据。
- **当前行为与诊断取舍：** 生产逻辑只在 `notifier/email.go` 收紧 11 个错误出口，测试 API 仅订正注释；自动邮件与测试邮件共用安全错误。固定数字码先于类别；无法类型化的认证安全策略使用固定兜底，不匹配标准库私有错误字符串。供应商自由文本和详细证书诊断不再返回；阶段为原调用点标签，隐式 EHLO/HELO 失败仍可能在 AUTH/MAIL 阶段返回，不新增 SMTP 状态机。
- **正式本地验证：** 两份安全回归共 9 个顶层 TestP319，定向 race 20 轮、notifier/API 两包完整 race 一轮、全量 12 包 race 一轮、vet/build、受影响 Go 格式与 diff-check 通过；五类错误改法负向控制由行为断言检出，deadline/正文 Write 仓库外故障探针在正式源码 overlay 上 race 20 轮通过、旧逻辑两出口对照失败。完整证据见审计 P3-19 当前实施补记。
- **状态与边界：** 10 秒建连、30 秒整会话 deadline、STARTTLS/PlainAuth 策略、发送顺序、信封/正文/主题、在途限制、API/SQLite/订阅与成功口径保持。源码/测试/文档尚未提交，未 fetch/push；Go 1.27.1 darwin/arm64，使用既有 ignored 前端 dist。未执行本项产品进程/浏览器、Linux/Docker、前端构建、真实 SMTP/收件箱/云/Webhook/Uptime Kuma 或远端 CI/GHCR；PT-B7 未执行/免除状态保持，单次全量不外推长期稳定。P3-20 编码、P3-21 响应读取继续独立，固定错误输出不表示响应读取已新增字节上限。


### P3-20 后续实施补记（2026-10-03，B 编码主题 + Base64 正文）

> 该批次其后已提交为 `ee39f08`；以下保留当时实施记录，其中“尚未提交”只指该批次。

- **授权与恢复点：** 用户依据引用研究结论授权复核后修复及文档同步。正式基线 `main / 974cb20`，本地 `origin/main / 5e1d79c`、ahead 7，工作树干净；候选单生产文件 patch 通过可应用性检查后实施。P3-19 已提交 `974cb20`，本轮未 fetch、提交或 push。
- **范围与实际行为：** 唯一生产文件为 notifier/email.go；新增 notifier/email_encoding_test.go，调整 notifier/email_test.go、main_test.go、webui/api/test_email_test.go 与 alertset_policy_test.go，四份文档，共十文件。两份 API 测试是研究预计八文件中漏计的原始中文报文断言，已改为解码后语义检查；零写入/零发布、三触发过滤及热重载真实投递断言保留。正文编码只在传输出口进行，不改共用业务渲染器或 Webhook。
- **正式证据：** 新增 18 场景在旧实现上全部按行为失败，最终 race 20 轮通过；API 四项发送/订阅用例 race 20 轮、notifier/API 两包完整 race、真实产品进程 TestProcessTestEmailWithUIPayload、vet/build 通过；全量 12 包 `go test ./... -race -count=1`、受影响 Go 格式与最终 `git diff --check` 均本地通过。正式五类负向控制分别检出首行 81 字符、正文编码行超限、非 ASCII DATA 被 554 拒绝、CTE 缺失、解码换行不等价。完整证据与修复中测试调整见审计 P3-20 当前实施补记。
- **状态与外部边界：** 源码/测试/文档尚未提交；Go 1.27.1 darwin/arm64，使用既有 ignored 前端 dist。10 秒连接/30 秒会话、TLS/认证策略、发送顺序、收件人 Trim、在途限制、安全错误、API/SQLite/依赖/前端与仅 SMTP 接受的成功口径保留。未执行 Linux/Docker/compose、前端构建/浏览器、真实 SMTP/收件箱/云/Webhook/Uptime Kuma 或远端 CI/GHCR；PT-B7 未执行/免除状态不变。编码正确不保证真实收件箱投递或展示，I-10/P3-21 不随本项关闭。

### P3-21 后续实施补记（2026-10-03，定型方案 B）

- **恢复点与范围：** `main / ee39f08`，本地 `origin/main / 5e1d79c`，ahead 8，实施前干净；前序 P3-20 已提交。唯一生产 push.go、新增响应测试、五份文档，共七文件；生产时限、健康、Webhook、配置/API/schema/依赖/前端边界保持。
- **正式本地验证：** 正式新增 12 个顶层响应测试定向 `-race -count=20 -timeout=5m`、health/notifier 两包完整 race 一轮、全量 12 包 `go test ./... -race -count=1 -timeout=20m`、`go vet ./...`、`go build ./...` 均通过；旧源码 overlay 与八类负向控制均按行为断言变红，无编译失败。受影响 Go 格式与最终文档后的 diff-check 见审计补记。
- **测试含义：** 成功必须读到真实 EOF；超大未知字段及尾随空白受限，尾随垃圾/第二个 JSON 拒绝，true 后 null 不成功；真实 HTTP 截断、gzip 解压超限、正文停滞与 shutdown 取消，Read/Close 两阶段名额保持、成功 HTTP/1 即时复用与失败后周期继续均覆盖。Close 失败只安全警告，已确认接受不改判；不保证所有异常响应连接复用。
- **状态与外部边界：** P3-21 与 I-08 本地修复闭环；具体正式证据、祖先核对与研究/正式区分见审计 P3-21 当前实施补记。Go `1.27.1 darwin/arm64`，使用既有 ignored 前端 dist；未执行 Linux/Docker/compose、前端构建/真实浏览器、真实云/SMTP/收件箱/Webhook/Uptime Kuma 或当前 revision 远端 CI/GHCR。PT-B7-03 人工免除仍非通过，PT-B7-05 仍未执行；单轮全仓绿色不外推长期稳定或外部验收。源码/测试/文档尚未提交，未 fetch/push。

### I8-18 后续实施补记（2026-10-08，方案 A）

- **范围与行为**：生产仅 config/store.go、webui/api/alerts.go，新增 config/API 两份快照测试，同步 AGENTS/Issue8/本文/审计，共 8 文件；四项窄只读快照、事务结束后返回，四对象响应和既有默认值/错误出口保持，前端与写侧不变。
- **基线与正式证据**：`main / 3f50f3821c86d04ee19b67a1d280a28069880194`，本地 `origin/main / 7696482243461f582342aeb78fdc2f467012f674`，ahead 2；实施前工作树/暂存区干净，未 fetch、提交或 push。正式定向两包 race 20 轮、受影响两包完整 race、vet/build 与五类行为负向控制通过；`go test ./... -race -count=3 -timeout=20m` 全仓 12 包通过（单条命令将每项测试重复三次，不称三次独立执行）；四个受影响 Go 文件的 gofmt 检查与最终 `git diff --check` 通过。全仓包含既有 TestMain 构建当前正式产品二进制的进程回归，不是 I8-18 专项产品验收。完整回归、夹具边界与命令见 Issue8 当前实施补记。
- **外部边界**：Go `1.27.1 darwin/arm64`，Go 门禁使用本轮开始时既有 ignored 前端 dist（未重建前端）。未执行 I8-18 专项产品进程/浏览器、Linux/Docker/compose、原生 amd64/macOS 13、真实云/SMTP/收件箱/Webhook/Uptime Kuma 或当前 revision 远端 CI/GHCR；原人工未执行/免除状态保持。既有全仓门禁含 I8-04 网络 DNS 测试，包级 ok/自行 skip 不作为本项外部通过证据。本地重复回归不外推长期稳定或外部验收。源码/测试/文档尚未提交或推送。

### I8-111 后续实施补记（2026-10-08，严格方案 B）

- **裁决与实现**：用户明确无兼容性需求并授权正式修复；配置导入使用 JSON v2，字段名精确匹配、任意层重复字段及非法 Unicode 拒绝，合法转义保持解码后等价。先完整 10 MiB 有界读取保证超限 413；数组继承同一选项，错误只返回安全原因/字段路径。
- **范围**：生产仅 decode.go/bundle_v3.go/export.go，新增 import_json_test.go、订正 import_test.go 注释，同步五文档共 10 文件。配置包版本仍为 3，schema/普通 API/前端/依赖/事务发布链保持；完整正式证据与授权基线见 Issue8 文末补记。
- **验证**：TestI8111* 定向 race 20 轮通过，57 条字段路径四类变体与六类正式源码负向控制具有判别力。全仓 12 包 race 每项重复三次通过；补充整数溢出回归后，安全错误专项 race 20 轮及最终全仓 race 一轮通过，vet/build/格式/diff-check 通过。
- **边界**：既有 ignored dist 未重建；浏览器/专项产品进程、Linux/Docker、真实云/通知链路和当前 revision 远端 CI/GHCR 未执行，不外推外部通过或长期稳定。尚未提交或推送。

### I8-113 后续实施补记（2026-10-08，细化 A）

- 用户定型为空渠道初始值＋单选按钮，保留 version 3，并接受旧版可能拒绝新增关闭空渠道包。新建/无主行/reset 显式未选择，已有配置保留；缺 channel 的历史主行只在本次新增列时转换为固定钉钉值，失败整个初始化事务回滚，重启不重复转换。
- GET/导出保留空渠道，关闭允许空、启用必选，未知/缺失/null 拒绝；通知器非法渠道在限流与 HTTP 前安全拒绝。前端单选位于 URL 前、初始无选中、启用未选零 PUT，关闭不清空输入。§二/§4.2～4.7、两个默认 JSON 示例与本版记录已同步。
- 正式三包专项 race 每项 20 次、三包完整 race 一轮、告警 8 项、前端全部 69 项与完整构建、vet/build 通过；八类 Go 与两类前端反向控制按行为变红。首轮全仓 race -count=3 为 11 包通过、webui 静态资源失败：新构建 _common 下划线文件被既有 embed 规则排除；一行仓外候选通过静态专项 race 20 次，用户已确认范围扩展并正式修改 `webui/embed.go` 为 `all:frontend/dist`；正式静态专项 race 20 次通过，恢复旧嵌入规则的反向控制精确 404 变红；该批次全仓重复门禁结果因中断未收口；2026-10-09 已在当前版本重新执行，最终证据见后续收口补记。详见 Issue8 与审计 I8-113 正式补记。
- 本批次已提交为 `c3bb468`；真实浏览器登记 PT-AUDIT-02 未执行，PT-B7-03 真实通知人工免除仍非通过；Linux/Docker/compose、真实云/通知链路、当前 revision 远端 CI/GHCR 等外部证据不升级。

### I8-05/I8-06 Push 调度后续修复（2026-10-09，Q-07 定型 A）

按用户授权实现同配置Wake保留截止时间、间隔变化以最近尝试结束为基准、timer到期先重读。正常发送与非法URL重校验共用有效间隔（非正60s、正值最小20s），不回写配置；启用/URL变化立即首发、关闭停止，失败正常周期不增加重试。保持同步发送与在途配置自然完成、Stop取消；Wake可合并，20秒不是换URL/启用首发的全局限速。原毫秒周期测试改用虚拟时间守护真实20秒合同，单在途守卫直接并发验证，读取夹具不依赖固定次数。正式门禁与五类负向控制见Issue8/审计文末同名补记；真实Kuma仍待PT-B7-05，前序历史通过证据不替代本次外部验收。

### I8-113 中断后收口补记（2026-10-09）

I8-113 与 `all:frontend/dist` 修正已提交为 `c3bb468` 并保留在当前 `main / 5613939` 历史中。恢复前工作树干净，本地 `origin/main / 36f7745`、ahead 6，未 fetch。本次仅补 AGENTS/Build7/Issue8/审计四份文档，未新增生产或测试修改，未提交或推送。此前中断的测试结果无法取回，不推定通过。

当前正式版本的 `make all` 通过（包含前端重建、vet、全仓 race 与产品构建，部分 Go 包命中缓存）；随后独立全仓 `go test ./... -race -count=3 -timeout=20m` 全部 12 包通过，每项测试在单条命令中重复三次。I8-113 三包专项 race 20 次、静态资源 race 20 次、前端全部 95 项回归、另行 Go build 与最终 diff-check 通过。仓库外恢复旧嵌入规则，正式回归明确因 `_common-qs-f2iD0.js` 返回 404 而失败。完整命令、日志路径及历史失败说明见 Issue8/审计文末同名收口补记。

I8-113 本地修复、验证与文档收口完成。PT-AUDIT-02 真实浏览器仍未执行；真实通知平台、云、上游 DNS、Linux/Docker/compose 与当前 revision 远端 CI/GHCR 无本次通过结论。npm ci 仍提示既有 3 项 high，未独立 audit/升级，不登记漏洞检查通过；本地重复不外推长期稳定或外部验收通过。
