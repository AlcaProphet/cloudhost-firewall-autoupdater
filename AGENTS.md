# AGENTS.md — FWAlizer AI 编码指令

> 本文档是给 AI 编码助手的指令集，也是项目**唯一的强要求文档**（详见「十二、文档体系与优先级」）。
> 项目设计方向见 [Design5.md](./Design5.md)（设计记录，当前），已完成构建记录见 [Build7.md](./Build7.md)（告警与运行健康，Step 0～7），当前工作见 [Issue7.md](./Issue7.md)（P1-01 TAG 所有权与目标级同步，Step 0～5 已本地实施完成，真实云/浏览器/远端 CI 待人工执行）；问题历史见 [Issue5.md](./Issue5.md) 与 [Issue6.md](./Issue6.md)，人工验收清单见 [ProdTestList.md](./ProdTestList.md)。

---

## 一、项目基本信息

- **模块路径**：`github.com/alcaprophet/cloudhost-firewall-autoupdater`
- **仓库名称**：`cloudhost-firewall-autoupdater`
- **产品与兼容标识**：产品显示名、二进制名、`FWALIZER_DATA_DIR` 部署变量、数据目录及 GHCR 镜像继续使用 `FWAlizer` / `fwalizer`，避免破坏保留的部署边界
- **Go 版本**：`go 1.25`
- **平台约束**：仅支持 **Linux 与 macOS**（平台文件 build tag 精确为 `linux || darwin`）；**不支持 Windows**（Windows pidfile 实现与 `%APPDATA%` 数据目录分支已移除，`GOOS=windows` 构建按预期失败）；构建与发布面向 `linux/amd64`
- **文档定位与优先级**：编码前先阅读本文件（强要求）。设计记录见 [Design5.md](./Design5.md)（当前，非强制，供参考）；Build6/Build7 为已完成的历史构建记录；当前实施合同与串行步骤见 [Issue7.md](./Issue7.md)（Step 0～5 已完成）；问题历史见 [Issue5.md](./Issue5.md) 与 [Issue6.md](./Issue6.md)；人工验收清单见 [ProdTestList.md](./ProdTestList.md)；历史文档（Design1-4、Build1-5、Issue1-4）见 [HistoryDocs/](./HistoryDocs/)
- **Build6 已完成构建（历史记录）**：目标形态固定为 WebUI 单二进制 + SQLite。截至 2026-09-27，Step 0～7 已验收通过。Step 5 的工程实现、本地自动门禁、浏览器人工回归及真实云/DNS/同步链路已经完成并由用户确认真机通过；跨实例不同自增历史的人工交叉导入因当前无该使用场景而免除（底层 ID 映射仍由自动化覆盖）。Step 7 的自动补测、统一验收门禁、真实二进制/Docker 容器验收与文档闭环已完成，并按用户确认的最小边界修复 Issue6 A10/A5/A7/A6/A8；**在此之后按用户一次性授权完成 Issue6 批次 1（A1、A12）、批次 3（A20、A16）、批次 2（A2）、批次 4（A3、A18、A13、A15 与 A11 接口部分）、批次 5（A4、A9、A14、A17）、批次 6（A11 收尾）与批次 7（A19、A8 文档收口）**，批次 8（低风险清理）亦已完成，Issue6 §2.1 状态表为权威记录。远端 GitHub Actions 已取得真实结果（tag `v2.0.0` → 运行 `36300428681` 成功，含远端 `go test -race -v ./...`，并真实推送 `ghcr.io/alcaprophet/fwalizer:2.0.0`），Issue5 O5-02 已关闭。真实 Email/SMTP/收件箱与 Webhook 的人工验收（原 `ProdTestList.md` PT-B6-08/09）经用户 2026-09-27 明确决定跳过、由用户自行处理，属**人工验收免除**（沿用 PT-B6-04 先例），不阻塞 Step 7，**但这两个外部链路仍无真实通过结论，不得写成已经通过**。
- **Build7 实施状态（2026-09-28）**：Build7（[Build7.md](./Build7.md)，告警与运行健康）**Step 0～6 已全部实施完成，Step 7（核验缺陷修复）亦已完成**：告警三个触发开关（默认全部关闭，渠道与触发同时开启才订阅）、可编辑纯文本邮件主题与正文（固定事件后缀与稳定详情顺序）、`POST /api/alerts/test-email`、唯一 `OperationalHealth` 计算源（`internal/health`）+ 30 秒内部监督器 + `GET /api/health/operational`、Uptime Kuma Push（默认关闭、默认 60s、最小 20s）；配置包为 version 3，version 1/2 与其他版本直接拒绝。Step 7 修复了两项核验缺陷：告警页测试邮件请求体多带 `enabled` 导致必然 HTTP 400（前端改为显式 8 字段载荷，后端严格契约不变）；启动窗口把「同步引擎尚未启动」误判为「未运行」导致重启误报 Push DOWN 与运行健康异常（启动顺序确定性化 + 固定 10 秒启动宽限）。**本机已取得的证据**：`go test ./... -race -count=1`（12 包）、`go vet ./...`、`go build ./...`、前端 `npm ci`/`npm run build`/两条 `npm audit`（0 漏洞）、`docker compose config`、`docker build`、容器非 root（uid 1000）+ `healthy` + `docker stop` 有界且退出码 0、真实二进制进程级用例（静态 `/api/health` 与 `/api/health/operational` 200、Push 心跳发往本地 mock、SIGTERM 干净退出、UI 载荷测试邮件走完假 SMTP、重启后首条心跳为 up）。**仍无真实通过结论（不得写成通过）**：真实 SMTP 接受、真实收件箱投递、真实 Webhook、真实 Uptime Kuma HTTP Monitor 与 Push 的 DOWN/恢复通知、真实云 API、远端 CI/GHCR 均未执行，清单见 [ProdTestList.md](./ProdTestList.md)。
- **Issue7 实施状态（2026-09-29）**：P1-01「TAG 所有权与目标级同步」**Step 0～5 已全部本地实施完成**（本地提交 `28559ed`）：严格 TAG 命名空间（`[TAG]foo` 不再归属，`Parse`/`OwnedRules` 共用同一判定）；唯一 canonical `FunctionalKey`（family + canonical CIDR + 协议 + 端口 + action，期望侧与回读侧双向归一化）与统一能力矩阵；目标级纯 planner（`provider.PlanTarget`，无网络/时钟/日志依赖，Dry Run 与正式同步共用）；Provider 快照携带 revision（Lighthouse `FirewallVersion`、CVM `Version`）并在分页不完整/版本缺失时失败（ECS `NextToken` 不推进即 `snapshot_incomplete`）；正式同步改为目标级 `S0 → Add(S0 版本) → S1 → 同一 planner 复算覆盖 → 清理安全门 → 条件 Delete(S1 定位) → 必要时 S2 验证`，Add 或覆盖验证失败时删除调用恒为 0；四平台条件清理（Lighthouse 版本 + key 唯一性、CVM 单请求 `PolicyIndex + Version`、SWAS 仅 S1 `RuleId`、ECS 每批 ≤100 且部分成功如实计数）；`EventTargetSyncComplete` 目标级完成事件取代逐域名完成事件；目标级 sync_logs（一目标一条、写库错误交 EventBus 统一 WARN）；Dry Run 每目标一项且数组恒为 `[]`；Dashboard 只按后端 `outcome` 展示（idle 不再误报、`cleanup_deferred` 黄色提示且保持 healthy）。**独立核验补强（2026-09-29，工作树改动，未提交）**：修复 Lighthouse 期望侧多端口展开粒度回归（`provider/plan.go`，`80,443` 现在落成 80 与 443 两个单端口功能与两条单端口新建规则；已存在合并规则零重建零清理）与 SWAS 分页页数上限用尽未按 `snapshot_incomplete` 失败（`provider/ali_swas.go`），两项均带红灯→绿灯判别性用例；并如实登记：`go test ./... -race -count=1` 受**既有** flaky 用例 `TestIsRetryable_RealWorldShapes`（`syncer/retry_test.go`，本次未改动）影响，完整套件实测 3 次中 2 次失败（修复后一次 12/12 全绿）、单包隔离 8 个 20 次重复批次中 2 个失败，**不得把单次绿色外推为稳定绿色**（按用户裁决不放宽断言、不改该测试）。**本机已取得证据**：`go test ./... -race -count=1`（12 包，补强后一次全绿）、`go test ./internal/tag ./provider ./syncer -race -count=1`、`go vet ./...`、`go build ./...`、`gofmt` 无输出、前端 `npm ci`/`npm run build`/两条 `npm audit`（0 漏洞，lockfile 未变）、`docker compose config --quiet`、`fwalizer:issue7` 容器非 root（uid 1000）+ `healthy` + `docker stop` ExitCode 0、独立重建的真实二进制 `/api/health` 与 `/api/health/operational` 200、SPA 资源 200、目标级 Dry Run DTO 端到端、`SIGTERM` 退出码 0。**仍无真实通过结论（不得写成通过）**：真实腾讯云/阿里云（含 Lighthouse 多端口收敛与四平台删除安全）、真实浏览器回归与远端 CI/GHCR 均未执行；清单见 [ProdTestList.md](./ProdTestList.md) PT-I7-01～07。逐 Step 证据与核验补强记录见 [Issue7.md](./Issue7.md) §12.3～§12.4。

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
- WebUI 默认绑定 `127.0.0.1`，可通过 `WEBUI_HOST` 配置监听地址（Docker 内使用 `0.0.0.0`）；端口通过 `WEBUI_PORT` 配置（默认 `60200`）；只有监听返回 `EADDRINUSE` 时才由 OS 随机选择可用端口并记录 WARN，权限、非法地址等其他监听错误原样返回并以非零状态退出（参见 `webui/server.go` 的 `Server.Start`）
- Docker 用户通过 `-p` 自行决定暴露范围
- 凭据作为独立业务设置存入 SQLite，不与资源声明混合，不接受环境变量 override

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
- TAG 是唯一的自动操作授权命名空间：仅当描述等于 `[TAG]` 或以 `[TAG] `（右方有一个空格）开头时才属于当前 TAG；`[TAG]foo` 不属于该命名空间
- comment 只用于人类可读，可为空、重复、修改或被平台截断，不参与所有权、功能身份、Diff、删除定位或唯一性校验；修改 comment 不触发云规则重建
- 非当前 TAG 规则可以被只读用于判定所需功能是否已存在，但永远不得被修改、删除或接管；只有功能 key 精确等价才算满足，不推断更宽 CIDR/端口的包含关系
- 功能 key 固定由 `address-family + canonical CIDR + canonical protocol + canonical port expression + action` 组成；TAG、comment、description、本地规则 ID、域名、云端 RuleID/PolicyIndex 均不进入 key
- 同一云目标上的所有适用规则必须先汇总为目标级完整期望功能集，同一功能 key 只创建一条；新建规则的 comment 按本地规则 ID 升序取第一个非空值，全空时只写 `[TAG]`
- 同步顺序固定为“获取快照 → 目标级规划 → 增量添加 → 重读并验证所需功能已存在 → 经安全门条件删除陈旧 TAG 规则”；任何 Add 都必须在 Delete 之前，Add 失败或结果不确定时保留旧规则
- 自动清理只在完整期望集非空、所有适用域名解析成功且非空、无平台能力跳过、新快照已证明覆盖完整、候选仍属于当前 TAG、平台删除定位无歧义且不会删除任何期望 key 时执行；其他情况只记录 `cleanup_deferred` 并保留残留
- 只要所需功能已经重读确认存在，清理延后或删除失败仍记为 `success` 且 operational health 保持 healthy；平台能力导致的期望规则无法实施记为 `partial`；DNS/Describe/Add/新增后覆盖验证失败记为 `failed`
- 删除时“规则已不存在”视为成功（幂等），不报错
- 添加时“规则已存在”视为成功，WARN 日志并跳过
- 支持协议：TCP / UDP / TCP+UDP / **ICMP**（ICMP 时端口由各 Provider 按 API 要求处理：Lighthouse 传 ALL，阿里云传 -1/-1，CVM 省略 Port 字段）
- **TCP+UDP 协议拆分：** 仅阿里云 SWAS 原生支持 TCP+UDP，Lighthouse/CVM/ECS 均不支持，由 `buildDesired()` 自动拆分为 TCP + UDP 两条规则
- **IPv6+ICMP 处理：** Lighthouse 使用 ICMPv6 协议，CVM 使用 ICMPV6 协议，ECS 不支持（AuthorizeSecurityGroup 无 ICMPv6，直接跳过并 WARN）
- 端口格式：单端口、逗号分隔、范围（`8000-8010`）、`ALL`
- 腾讯云 CVM 安全组规则上限 **100 条**，接近上限时停止新增并告警

---

## 四、DNS 解析约束

- 使用 SQLite `dns` 业务设置指定的自定义 DNS 服务器
- 通过 Go `net.Resolver` 的 `Dial` 函数指定上游 DNS
- 同时解析 **A 记录（IPv4）** 和 **AAAA 记录（IPv6）**
- IPv4 → `CidrBlock` 字段（格式 `1.2.3.4/32`）
- IPv6 → `Ipv6CidrBlock` 字段（格式 `2001:db8::1/128`）
- 域名解析失败：记录 WARN 日志，保留现有规则不变（不删除）
- 超时默认为 **10s**（连接 + 整体，由 SQLite `dns_timeout` 业务设置配置）
- 渐进式熔断：连续失败达阈值后熔断，半开状态每轮探测一次，成功后解除
- ⚠️ 仅支持单台服务器场景（少量 IP），不支持 CDN 等返回大量 IP 的域名

---

## 五、同步调度约束

- 使用 `time.Ticker`，不依赖外部 cron
- 间隔由 SQLite `interval` 业务设置控制（如 `5m`、`30m`、`1h`）
- 优雅退出：收到 `SIGTERM`/`SIGINT` 后，完成当前轮次再退出
- 支持配置热重载（WebUI 修改后通过 channel 通知 Syncer）
- 同步全局开关（SQLite `sync_enabled`，默认 true）：暂停时 ticker 与手动 trigger 均不触发同步；模拟测试与连接测试不受影响（独立于 Run() 主循环）

---

## 六、乐观锁与重试

- 每次写入前重新拉取最新规则状态；每次重试都从整个云目标的 Describe/规划重新开始，不沿用上一 attempt 的删除定位
- Lighthouse 和 CVM 的增删必须传入与当次快照一致的 `FirewallVersion` / `Version`；版本不匹配时重新 Describe/规划，不得降级为无版本保护删除
- 写入失败自动重试（最多 3 次，指数退避）
- SWAS 与 ECS 使用当次重读快照中的稳定 RuleID 删除；Lighthouse 只在功能 key 唯一且当前 TAG 归属唯一时删除；CVM 使用同一快照的 `PolicyIndex + Version` 定位并避免逐条删除导致索引漂移

---

## 七、API 频率限制

- 不同云厂商频率限制不同，取对应间隔（详见 HistoryDocs/Build1.md）
- 同一云厂商内串行处理（共享配额），目标之间加入间隔；同一目标内域名只做解析去重，不再以“单域名规则”为云端写入/重试单元
- 不同云厂商可并行同步（API 配额独立）

---

## 八、Docker 约束

- 基础镜像：`alpine:3.20`
- 编译镜像：`golang:1.25-alpine`
- `CGO_ENABLED=0` 静态编译（Docker 构建）
- 非 root 用户运行（`adduser -D appuser`）
- 日志输出到 stdout（Text 格式，`docker logs` 查看）
- `HEALTHCHECK` 统一使用 WebUI HTTP `/api/health` 端点，不使用进程存活检查掩盖 HTTP 服务失效；Build7 起该端点保持静态存活语义，运行健康由 `/api/health/operational` 表达，**不得**把 HEALTHCHECK 改为 operational 端点

---

## 九、配置约束

- SQLite 是唯一业务配置源，目标、规则、云凭据、同步/DNS/TAG/日志/主题设置和告警均只通过 WebUI 管理
- 仅保留 `FWALIZER_DATA_DIR`、`WEBUI_HOST`、`WEBUI_PORT` 三个部署环境变量；它们不写入 SQLite、不进入配置包且不被配置导入覆盖
- `FWALIZER_DATA_DIR` 空白值按未设置处理；`WEBUI_HOST` 默认 `127.0.0.1`；`WEBUI_PORT` 默认 `60200` 且必须是 `1～65535` 的十进制整数
- 不保留云凭据或其他业务设置的 ENV override，不提供 `.env` 业务模式、`.env.example` 或隐藏兼容开关
- 密钥使用云厂商 **CAM 子账号 + 最小权限**

### 9.1 已固定实施契约（Build7 已实施合同 + Build6 既有边界）

> 本节同时承载 Build6 已落地边界与 Build7 已实施合同。Build7 条款自 2026-09-28 起已随 Step 1～7 实施完成（逐步证据见 [Build7.md](./Build7.md) 第八节与第十一节）；Build6 条款中未被 Build7 明确替代的部分继续有效。

**Build7 已实施合同（告警与运行健康，Step 1～7 于 2026-09-28 完成；Step 7 为核验缺陷修复）**

- 配置包只接受 version 3；version 1/2 及其他版本一律 HTTP 400，不迁移、不补全、不兼容；version 3 固定包含 `metadata`、`targets`、`rules`、`settings`、`alerts`（`policy`/`email`/`webhook`）与 `monitoring.uptime_kuma_push` 全字段，数组与对象不得为 `null`
- 告警配置持久化固定为 `alert_policy`（三个触发开关 + `health_timeout`）、`alert_email`（含 `subject`/`body`）、`alert_webhook`、`uptime_kuma_push` 四张单行表；现有数据库执行一次性最小显式迁移，保证各单行表至多一行且业务 ID 固定为 1，不得依赖 `CREATE TABLE IF NOT EXISTS` 自动补列，也不得在每次启动重复重置用户配置
- 告警默认值固定全部关闭：`email.enabled`、`webhook.enabled`、三个触发开关与 `uptime_kuma_push.enabled` 的初始值与迁移结果均为 false；只有“渠道开关 + 对应触发开关”同时开启才安装该事件订阅；只开启触发条件但不启用渠道、或只启用渠道但不开启触发条件，都不发送自动通知；测试邮件独立于这些开关
- 三个触发开关是邮件与 Webhook **共用**的全局策略，不为两个渠道复制两套开关；`health_timeout` 默认 `10m` 且只保留这一个健康超时；Push 间隔默认 `60s`、最小 `20s`
- 邮件固定一套可编辑纯文本主题与正文，自动邮件主题追加固定后缀（`DNS 解析失败` / `同步失败` / `运行健康异常`），正文为“用户文本 + 系统固定详情块”（`事件类型`/`时间`/`Provider`/`域名`/`错误`，缺失字段写 `-`，顺序稳定，不遍历 map）；该详情块由**邮件与 Webhook 共用同一渲染器**（Webhook 首行保留事件类型标识），两渠道顺序一致；运行健康异常事件在详情块末尾追加固定 `原因` 行（稳定原因用 `; ` 连接，无原因则不输出该行）；多收件人逗号分隔并逐项 Trim；邮件固定 `text/plain; charset=UTF-8`
- `GET/PUT /api/alerts` 顶层固定为 `policy`、`email`、`webhook`、`uptime_kuma_push` 四个完整对象；PUT 的四个对象及其全部子字段必须出现，拒绝 `null`、缺字段、未知字段、尾随 JSON 与多个顶层值；四部分在同一协调器写事务内覆盖保存，任一步失败全部回滚；commit 后只做无失败内存发布与循环唤醒，不在协调器锁内等待 SMTP、Webhook 或 Uptime Kuma；GET 必须 `Cache-Control: no-store`
- 新增 `POST /api/alerts/test-email`：使用请求中的未保存表单值，不写 SQLite、不进入配置协调器、不 Apply 告警集合、不改变订阅、不要求邮件渠道已启用；复用生产 SMTP 会话的 10 秒连接上限与 30 秒整会话 deadline；成功文案固定为“SMTP 服务器已接受测试邮件”，不得表述为已送达收件箱；测试结果只存在于页面内存与既有实时日志
- 运行健康固定由唯一 `OperationalHealth` 计算源提供（内部监督器、`/api/health/operational` 与 Uptime Kuma Push 共用，禁止各写一套）：SQLite 探活最多 2 秒且不持有 Syncer/协调器锁；Syncer 主循环未运行即 unhealthy（正常 shutdown 阶段不监督；进程启动后的固定启动宽限 `StartupGrace=10s` 内、主循环尚未进入运行态不视为异常，超出宽限仍按未运行处理；已进入运行后停止则立即 unhealthy，不受宽限影响）；内部监督器与 Push 循环必须在同步主循环进入运行态后才启动（启动顺序确定性化，不得让首检/首发读到尚未启动）；`sync_enabled=false` 时只保留 SQLite 与主循环检查；最近一轮 `failed` 或 `partial` 视为 unhealthy（直到后续 `success`/`idle` 覆盖）；当前轮次超过 `health_timeout` 未完成、以及 `sync_enabled=true` 时距最近完成时间超过 `interval + health_timeout` 均视为 unhealthy；`idle` 与空目标/空规则视为正常；多原因去重并使用固定排序
- `/api/health` 保持静态 Docker 存活语义不变（不反映同步或运行健康）；新增 `GET /api/health/operational`：健康 200、异常 503，固定 `Content-Type: application/json; charset=utf-8` 与 `Cache-Control: no-store`，每次请求现场计算，响应只含稳定原因，不泄露 SQL、路径、凭据或 URL；Dockerfile 与 Compose 的 `HEALTHCHECK` 继续使用 `/api/health`
- 新增 30 秒内部健康监督器：仅在健康→异常边沿发布一次 `notifier.EventOperationalUnhealthy`（事件数据只含检查时间与稳定原因数组），持续异常不重复发送、原因变化只更新日志，恢复只写 INFO 且恢复后再次异常可重新发布一次；配置保存后立即唤醒一次检查；第三触发开关只控制该事件是否进入邮件/Webhook，不关闭 operational 端点与 Push；监督器与进程 shutdown 同一生命周期，不产生孤立 goroutine
- Uptime Kuma 同时支持 HTTP Monitor 拉取 `/api/health/operational` 与 Push Monitor 反向心跳；Push 默认关闭，解析用户填写的完整 URL 后覆盖 `status`/`msg`/`ping` 并保留 token 与其他未知 query；HTTP 上限 10 秒、同一时刻最多一条在途、不排队不重试；仅 2xx 且响应 JSON 为 `{"ok":true}` 算成功；down 的 `msg` 由稳定原因用 `; ` 连接并截断到 250 字符以内；Push 失败不改写应用健康、不产生自激告警；日志不得包含完整 Push URL 或 token；shutdown 取消在途请求

**Build6 既有边界（继续有效，除被上述 Build7 条款明确替代者）**

- 配置导出固定为 `POST /api/config/export`（旧 GET 端点已删除），只生成明文 JSON 完整敏感快照，包含云凭据、SMTP 密码、Webhook URL 与 Uptime Kuma Push URL；版本策略以上方 Build7 条款为准（version 3 为唯一版本）
- 配置导入为强类型、严格解码（10 MiB）、覆盖式原子事务；使用 `export_id → 新数据库 ID` 映射重建规则引用，显式写入完整设置键集合，并在同一事务内覆盖 policy/email/webhook/uptime_kuma_push
- 导入保留 `sync_logs`，清空 `scanned_resources`，不重置 SQLite 自增序列，不处理部署参数或数据库内部状态
- 被任一规则引用的目标不得直接删除，API 返回 HTTP 409；不静默删除引用或把规则扩大为“适用于全部目标”
- TAG Trim 后必须非空，禁止 `[` / `]` 和控制字符，最多 48 个 Unicode 字符
- 配置导入 JSON 最大 10 MiB，其他 JSON 请求最大 1 MiB，超限返回 413；固定结构 DTO 拒绝未知字段、尾随 JSON 和多个顶层值
- HTTP Server 使用 `ReadHeaderTimeout=5s`、`IdleTimeout=120s`，不设置全局 `ReadTimeout` / `WriteTimeout`；HTTP shutdown 上限 10s
- 两类 SSE 必须监听服务器级 shutdown 信号显式退出，不依赖 `http.Server.Shutdown()` 自动取消长连接
- 运行时 `cfg + providers + resolver` 一次原子替换（Step 5 已落地）：`syncer.RuntimeState`（Config/ClientPool/Providers/Resolver/Breaker）发布后不可修改，`RuntimeManager` 单锁边界替换指针；同步轮次、Dry Run、连接测试与资源扫描各只取一次完整快照，当前轮次继续使用旧完整快照，下一轮完整使用新快照
- 普通 API 与配置导入共用同一组最小领域校验（`config/validate.go`）：非法输入不写库、不触发 reload，合法事务提交后只应用一次运行时更新，删除被规则引用的目标返回 409
- 每渠道（邮件 / Webhook）最多 4 个在途发送，满载丢弃最新并记录不含密码、URL 与正文的安全 WARN；限流器跨配置热重载保持连续
- Build6 的完整 Schema、字段校验与事务顺序继续作为既有实现基线；Build7 的 Schema 增量（`alert_policy` / `uptime_kuma_push` / `alert_email.subject|body`）、配置包 version 3、告警 API 与验收矩阵以 [Build7.md](./Build7.md) 为当前实施方案

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
- 项目交付范围仅包含 WebUI 单二进制 + SQLite 以及 Docker/服务器部署；不包含 `.env` Headless 业务模式、CLI 子命令、桌面托盘、开机自启或原生桌面打包计划
- 日志多路复用器 `MultiHandler` 统一定义在 `app/logutil.go`（消除与 `webui/api/logstream.go` 的重复）
- WebUI 模式通过 pidfile（`config/pidfile.go` + 平台文件（linux/darwin））防止多实例运行
- 事件类型：全局同步完成用 `EventSyncComplete`，目标级云端写入与覆盖验证用 `EventTargetSyncComplete`，DNS 失败继续用 `EventDNSFailed`，运行健康异常边沿用 `EventOperationalUnhealthy`；`EventDomainSyncComplete` 不再承载云端增删数量
- 同步全局开关：`POST /api/sync/pause|resume` 端点（先写 DB 后通知 Syncer）；`SyncStatus.enabled` 字段；前端「模拟测试」页（路由 `/dry-run`）承载目标级变更预览，正式同步与 Dry Run 共用同一纯规划器；至少表达 `desired`、`satisfied_by_owned`、`satisfied_by_external`、`to_add`、`cleanup_candidates`、`cleanup_deferred`、`dns_errors`、`unsupported`、`conflicts`、`coverage_ready`；连接测试保留在目标添加/编辑弹窗（`POST /api/test-connection`）
- 地域自动补全：数据源为 `PlatformAPIDocs/PlatformZoneGuide/`（后端 `webui/api/zones.go` 提供 `GET /api/zones`，文档更新时需同步数据）；后端仅提供预填数据、不校验地域合法性（允许输入列表外值，由云 API 自行报错，符合「不过度防御」）
- 资源扫描：`provider/scan.go` 实现四平台只读列表查询（Lighthouse `DescribeInstances`、SWAS `ListInstances`、CVM/ECS `DescribeSecurityGroups`），`webui/api/scan.go` 提供 `POST /api/scan-resources`（凭据缺失快速失败）、`GET/DELETE /api/scanned-resources`；结果按 cloud_type+region 覆盖式入库（`scanned_resources` 表），仅供添加目标自动补全，同步流程不依赖
- 清空所有数据：`POST /api/config/reset` 只接受单一空对象 `{}`，经配置变更协调器在单事务内调 `Store.ResetAllTx()` 清空全部业务表（targets/rules/settings/sync_logs/alert_policy/alert_email/alert_webhook/uptime_kuma_push/scanned_resources），并恢复告警默认全部关闭与 `health_timeout=10m`、Push `interval=60s` 的默认值，等效重新初始化；前端入口需红色警告按钮 + 卡片式二次确认
- 普通 API 最小校验边界：固定结构请求与配置导入统一走 `webui/api/decode.go` 的 `decodeJSONStrict`（拒绝未知字段/尾随 JSON/多个顶层值，超限 413；普通请求 1 MiB、配置导入 10 MiB），路径 ID 用 `strconv.Atoi` 严格解析且必须大于 0；请求 DTO 不含数据库 `id`；更新/删除按 `RowsAffected` 返回 404；规则请求的 `targets` 必须显式提供（省略返回 400），空数组仍表示适用于全部目标
- 配置变更协调器：目标、规则、settings、alerts、pause/resume、reset 与配置导入的写入口统一经 `webui/api/ConfigCoordinator` 串行化（锁 → 单事务 → 事务内完整业务快照 → 事务内构造候选 `RuntimeState` 与候选告警集合 → commit → 无失败发布：日志级别 → 告警集合 → `RuntimeState` → 运行健康监督器唤醒 → Uptime Kuma Push 唤醒）；唤醒时新运行时必须已可见；非法输入零写入零 apply，commit 之后不重新读库、不构造 Provider、不访问网络
- Provider 凭据为不可变值 `provider.Credentials`，由 `ClientPool` 在创建时持有且无 setter；已删除进程级全局凭据与 `provider.SetCredentials`，连接测试、资源扫描、正式同步与 Dry Run 共用同一显式凭据模型和一次快照
- 运行时设置动态生效：日志级别使用 `app.LogLevelVar`（`slog.LevelVar`）与 `LogBroadcaster.SetLevel`；DNS 熔断阈值随完整 `RuntimeState` 原子发布（普通变更经 `dns.CircuitBreaker.Clone` + `SetThreshold` 保留既有失败计数；完整导入**确定**新建 breaker 并清空计数）
- 前端 UI 规范：全局字号 16px、页面级操作按钮统一 `size="large"`（44px，`App.vue` themeOverrides 按分尺寸变量覆盖）；表格内操作按钮（编辑/删除）保持小号；所有二次确认使用 `NModal preset="card"` 卡片式弹窗（危险操作确认按钮 `type="error"`）
- 资源 ID 输入提示：按云类型区分文案（轻量云=实例 ID，CVM/ECS=安全组 ID），由 `constants.ts` 的 `resourceIdHint()` 统一承载（仅 placeholder，不引入额外说明块）

---

## 十二、文档体系与优先级

### 12.1 文档定位与优先级（本文件为唯一强要求）

| 文档类型 | 文件 | 定位 | 约束力 |
|---------|------|------|--------|
| **强要求** | **AGENTS.md（本文件）** | AI 编码指令与约束 | **唯一强要求，尽量不违背** |
| 设计构想 | [Design5.md](./Design5.md)（当前）；历史：[HistoryDocs/](./HistoryDocs/)（Design1-4 已存档） | 设计大方向、架构构想、决策记录 | 非强制，供参考 |
| 构建方案 | [Build7.md](./Build7.md)（Step 0～7 已完成）；历史构建记录：[Build6.md](./Build6.md)（已完成）；更早：[HistoryDocs/](./HistoryDocs/)（Build1-5 已存档） | 详细的分步构建方案与验收命令 | 非强制，执行建议 |
| 问题记录 | [Issue7.md](./Issue7.md)（P1-01 实施合同，Step 0～5 已本地实施完成）；[Issue5.md](./Issue5.md) 与 [Issue6.md](./Issue6.md)（已实施问题记录与残余候选）；历史：[HistoryDocs/](./HistoryDocs/)（Issue1-4 已存档） | 记录的错误、固定产品语义、分步实施与验收合同 | 非强制，经验参考 |

**执行规则：**

- 只有 **AGENTS.md** 是强要求文档，其他类型文档均为**设计取向，不是强规则**，不需要严格遵守
- **Design 文档**描述设计大方向与构想；**Build 文档**描述详细的构建方案；**Issue 文档**记录错误与修复方案
- 若 Design / Build / Issue 文档之间存在冲突，或与 AGENTS.md 冲突：**提示用户并让用户做决策**，不擅自选择遵守哪一份
- 若构想本身存在冲突，同样**提示用户**，由用户决策
- Design 文档（如 [Design5.md](./Design5.md)）中的内容不是强制性规定，仅是设计类构想

### 12.2 文档清单

| 文档 | 目标读者 | 内容 | 状态 |
|------|---------|------|------|
| AGENTS.md（本文件） | AI 编码助手 | 编码指令与约束（**唯一强要求**） | 活跃 |
| [Design5.md](./Design5.md) | 人类（开发者/用户） | 当前设计记录：WebUI 单二进制目标形态、配置边界与安全决策 | 活跃 |
| [Build7.md](./Build7.md) | 开发者 | 当前构建方案：告警与运行健康，Step 0～7（含核验缺陷修复）的分步实施与验收 | 活跃 |
| [Build6.md](./Build6.md) | 开发者 | 已完成的历史构建记录：Step 0-7（version 2 配置包边界已被 Build7 的 version 3 取代，仅用于核查） | 已完成 |
| [Issue5.md](./Issue5.md) | 开发者 | 问题追踪：R5、O5 与 A5 事项 | 活跃 |
| [Issue6.md](./Issue6.md) | 开发者 | 问题追踪：A1～A20 批次、后续核验与残余候选 | 活跃 |
| [Issue7.md](./Issue7.md) | 开发者 | P1-01 TAG 所有权、目标级规划、先增后验与平台化清理；Step 0～5 已本地实施完成（真实云/浏览器/远端 CI 未执行，见 §12.3） | 已实施（真实外部验收待人工） |
| [ProdTestList.md](./ProdTestList.md) | 人类（用户） | 待用户执行的真实外部人工验收清单 | 活跃 |
| [HistoryDocs/](./HistoryDocs/) | 开发者 | Build1-5、Issue1-4、Design1-4 共 13 份历史文档（已存档，仅记录，不再用于构建，仅用于核查等情况） | 已存档 |

### 12.3 API 使用要求文档（PlatformAPIDocs/）

仓库 `PlatformAPIDocs/` 目录存放各云平台 **API 使用要求** 文档（参数格式、字段长度限制、频率限制等），编码前查阅：

| 目录 | 内容 |
|------|------|
| `PlatformAPIDocs/TencentLighthouseAPIGuide/` | 腾讯云轻量云防火墙 API 指南 |
| `PlatformAPIDocs/TencentCVMAPIGuide/` | 腾讯云 CVM 安全组 API 指南 |
| `PlatformAPIDocs/AliyunSWASAPIGuide/` | 阿里云轻量云（SWAS）API 指南 |
| `PlatformAPIDocs/AliyunECSAPIGuide/` | 阿里云 ECS 安全组 API 指南 |
| `PlatformAPIDocs/PlatformZoneGuide/` | 各平台地域与可用区指南（地域自动补全数据来源） |
