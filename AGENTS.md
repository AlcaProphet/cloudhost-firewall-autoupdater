# AGENTS.md — FWAlizer AI 编码指令

> 本文档是给 AI 编码助手的指令集，也是项目**唯一的强要求文档**（详见「十二、文档体系与优先级」）。
> 项目设计方向见 [Design5.md](./Design5.md)（设计记录，当前），已完成构建记录见 [Build7.md](./Build7.md)（告警与运行健康，Step 0～7），当前工作见 [Issue7.md](./Issue7.md)（P1-01 TAG 所有权与目标级同步，Step 0～5 主体已本地实施；§12.5 独立追踪完整核验未完成项，真实云/浏览器/远端 CI 待人工执行）；问题历史见 [Issue5.md](./Issue5.md) 与 [Issue6.md](./Issue6.md)，人工验收清单见 [ProdTestList.md](./ProdTestList.md)。

---

## 一、项目基本信息

- **模块路径**：`github.com/alcaprophet/cloudhost-firewall-autoupdater`
- **仓库名称**：`cloudhost-firewall-autoupdater`
- **产品与兼容标识**：产品显示名、二进制名、`FWALIZER_DATA_DIR` 部署变量、数据目录及 GHCR 镜像继续使用 `FWAlizer` / `fwalizer`，避免破坏保留的部署边界
- **Go 版本**：`go 1.25`
- **平台约束**：仅支持 **Linux 与 macOS**（平台文件 build tag 精确为 `linux || darwin`）；**不支持 Windows**（Windows pidfile 实现与 `%APPDATA%` 数据目录分支已移除，`GOOS=windows` 构建按预期失败）；构建与发布面向 `linux/amd64`
- **文档定位与优先级**：编码前先阅读本文件（强要求）。设计记录见 [Design5.md](./Design5.md)（当前，非强制，供参考）；Build6/Build7 为已完成的历史构建记录；当前实施合同与串行步骤见 [Issue7.md](./Issue7.md)（Step 0～5 主体已实施，§12.5 追踪完整核验未完成项）；问题历史见 [Issue5.md](./Issue5.md) 与 [Issue6.md](./Issue6.md)；人工验收清单见 [ProdTestList.md](./ProdTestList.md)；历史文档（Design1-4、Build1-5、Issue1-4）见 [HistoryDocs/](./HistoryDocs/)
- **Build6 已完成构建（历史记录）**：目标形态固定为 WebUI 单二进制 + SQLite。截至 2026-09-27，Step 0～7 已验收通过。Step 5 的工程实现、本地自动门禁、浏览器人工回归及真实云/DNS/同步链路已经完成并由用户确认真机通过；跨实例不同自增历史的人工交叉导入因当前无该使用场景而免除（底层 ID 映射仍由自动化覆盖）。Step 7 的自动补测、统一验收门禁、真实二进制/Docker 容器验收与文档闭环已完成，并按用户确认的最小边界修复 Issue6 A10/A5/A7/A6/A8；**在此之后按用户一次性授权完成 Issue6 批次 1（A1、A12）、批次 3（A20、A16）、批次 2（A2）、批次 4（A3、A18、A13、A15 与 A11 接口部分）、批次 5（A4、A9、A14、A17）、批次 6（A11 收尾）与批次 7（A19、A8 文档收口）**，批次 8（低风险清理）亦已完成，Issue6 §2.1 状态表为权威记录。远端 GitHub Actions 已取得真实结果（tag `v2.0.0` → 运行 `36300428681` 成功，含远端 `go test -race -v ./...`，并真实推送 `ghcr.io/alcaprophet/fwalizer:2.0.0`），Issue5 O5-02 已关闭。真实 Email/SMTP/收件箱与 Webhook 的人工验收（原 `ProdTestList.md` PT-B6-08/09）经用户 2026-09-27 明确决定跳过、由用户自行处理，属**人工验收免除**（沿用 PT-B6-04 先例），不阻塞 Step 7，**但这两个外部链路仍无真实通过结论，不得写成已经通过**。
- **Build7 实施状态（2026-09-28）**：Build7（[Build7.md](./Build7.md)，告警与运行健康）**Step 0～6 已全部实施完成，Step 7（核验缺陷修复）亦已完成**：告警三个触发开关（默认全部关闭，渠道与触发同时开启才订阅）、可编辑纯文本邮件主题与正文（固定事件后缀与稳定详情顺序）、`POST /api/alerts/test-email`、唯一 `OperationalHealth` 计算源（`internal/health`）+ 30 秒内部监督器 + `GET /api/health/operational`、Uptime Kuma Push（默认关闭、默认 60s、最小 20s）；配置包为 version 3，version 1/2 与其他版本直接拒绝。Step 7 修复了两项核验缺陷：告警页测试邮件请求体多带 `enabled` 导致必然 HTTP 400（前端改为显式 8 字段载荷，后端严格契约不变）；启动窗口把「同步引擎尚未启动」误判为「未运行」导致重启误报 Push DOWN 与运行健康异常（启动顺序确定性化 + 固定 10 秒启动宽限）。**本机已取得的证据**：`go test ./... -race -count=1`（12 包）、`go vet ./...`、`go build ./...`、前端 `npm ci`/`npm run build`/两条 `npm audit`（0 漏洞）、`docker compose config`、`docker build`、容器非 root（uid 1000）+ `healthy` + `docker stop` 有界且退出码 0、真实二进制进程级用例（静态 `/api/health` 与 `/api/health/operational` 200、Push 心跳发往本地 mock、SIGTERM 干净退出、UI 载荷测试邮件走完假 SMTP、重启后首条心跳为 up）。**仍无真实通过结论（不得写成通过）**：真实 SMTP 接受、真实收件箱投递、真实 Webhook、真实 Uptime Kuma HTTP Monitor 与 Push 的 DOWN/恢复通知、真实云 API、远端 CI/GHCR 均未执行，清单见 [ProdTestList.md](./ProdTestList.md)。
- **Issue7 实施状态（2026-09-29）**：P1-01「TAG 所有权与目标级同步」**Step 0～5 主体已本地实施**（提交 `28559ed`；F1/F5 核验补强已提交为 `38bdc19`）：严格 TAG 命名空间、canonical `FunctionalKey`、目标级纯 planner、四平台 revision/完整性、`S0 → Add → S1 → 覆盖验证 → 安全门 → Delete → 必要时 S2`、平台化条件清理、目标级事件/日志/Dry Run/Dashboard 主线均已落地；F1 Lighthouse 多端口展开粒度与 F5 SWAS 分页上限完整性已带判别性用例修复。**R7-01 已修复并提交为 `b80b1b0`**：只对“本 attempt 已由 S1 确认覆盖、失败点仅为可重试 Delete”的路径做类型化标记，前两次保持整目标重试，第三次耗尽后收敛为 `success + cleanup_deferred`/healthy；DNS/Describe/Add/S1/S2 失败仍为 `failed`，未绕过腾讯版本保护。**R7-02 已修复并提交为 `eab4bea`**：目标完成/失败事件统一补齐 `cleanup_deleted`、目标全生命周期 `duration_ms` 与 canonical `unsupported`，并由真实 publisher/EventBus/SQLite 整链证明清理计数与落库一致。**R7-03 已按用户裁决 A 修复并提交为 `297ccfe`**：Dry Run 对所有已配置目标各返回一项；无适用规则目标只返回非 null 空数组骨架与 `coverage_ready=false`，不解析 DNS、不读取云快照、不进入 planner、不产生限速等待；前端明确标记“无适用规则”并说明正式同步会跳过，正式同步 `RoundSummary.Total` 仍只统计有适用规则目标。**完整核验仍有 R7-04～R7-07 未完成项，不得把主体完成写成无保留闭环**：R7-04 为 S2 未参与最终残留计数的可观测性偏差，幂等 NotFound 必须消除 deferred 但不得虚增 `deleted/cleanup_deleted`；R7-05 为数组非 null 测试无判别力，必须改用结构化 JSON 检查并保留 null 负向控制；R7-06 与新增 R7-07 分别是 `TestIsRetryable_RealWorldShapes`、`TestAliClientRequestIsBounded` 丢弃 accepted `net.Conn` 导致 GC/finalizer 提前关连接的同根因 flaky，修复只能稳定测试夹具，不得扩大生产 `isRetryable`、降低超时下限或修改阿里云生产超时。后续固定按 Issue7 §12.5.4 串行处理：先 R7-06+R7-07 恢复可信门禁，再 R7-04，再 R7-05，最后多轮全量 race/vet/build/前端/diff-check 与文档闭环；**不得把单次全量绿色外推为稳定绿色**。当时 `main` 相对 `origin/main` ahead 1、工作树在本轮文档修改前干净，尚未推送（**该 ahead 1 为本段的历史快照；后续已推进至 `b84531b`，相对 `origin/main` ahead 7，见本文件「Issue7 后续本地修复状态」段与审计报告「最近核验基线」小节**）。**仍无真实通过结论（不得写成通过）**：真实腾讯云/阿里云（含 Lighthouse 多端口收敛与四平台删除安全）、真实浏览器回归与当前 revision 的远端 CI/GHCR 均未执行；清单见 [ProdTestList.md](./ProdTestList.md) PT-I7-01～07。逐 Step 证据、已提交补强与追踪项分别见 [Issue7.md](./Issue7.md) §12.3、§12.4、§12.5。
- **Issue7 后续本地修复状态（2026-09-30，优先于上一行的未完成项快照）**：R7-06/R7-07 与 R7-04 已提交为 `b19d271`：两个阻塞 TCP 测试夹具持有 accepted `net.Conn` 并有界回收；成功可信的 S2 planner 成为最终 `cleanup_deferred` 唯一来源，幂等 NotFound 保持 `deleted/cleanup_deleted=0`，S2 失败继续 `failed` 并使用保守 fallback。R7-05 已在当前工作树按测试-only 边界修复：`TestDryRun_ArraysNeverNull` 改用 `map[string]json.RawMessage` 检查顶层与目标级数组的存在、非 `null` 与 JSON array 类型，覆盖有适用规则、无适用规则骨架、零目标/零结果三种真实输出，并对全部数组字段加入 `null`、缺失、对象类型负向控制；未修改生产 DTO、planner、API 或前端。两个 GOGC 压力门禁、`syncer`/`provider` 包 `-race -count=20`（Syncer 显式 `-timeout=20m`）、全量 12 包 race 连续 3 次、vet、build、前端 build、diff-check 均本地通过。**R7-05 与本轮文档改动已提交为 `d6d208e`（不再处于未提交状态）**；P1-01 的 R7-01～R7-07 本地核验项已收口，但真实四云、浏览器与当前 revision 远端 CI/GHCR 仍未执行，故仍不得外推为外部验收通过或无保留发布闭环。

---

**第二轮只读核验与更正（2026-09-30）**：对审计报告做完整只读真实性核验后，按用户批准方案回写文档与测试（**生产代码零改动**）。关键更正：① P3-06 的「每方向 100」无官方依据——`AGENTS.md` §三「CVM 安全组规则上限 100 条」与官方 `SecurityGroupPolicyLimit` 定义均为**安全组级**上限，故 `provider/tc_cvm.go` 的四方向合计判定**不是过度保守**，原「只统计入站」修法已撤销，口径待 PT-I7-03 真实账号确认；② 新增 **P3-26**：`provider/scan.go` 分页中途空响应会静默截断并覆盖 `scanned_resources` 缓存（与 P3-25 资源扫描路径属不同缺陷类别，I-19 范围不变）；③ 新增「最近核验基线」小节固定当前基线（`main` / `b84531b`、`origin/main` / `d6d208e`、ahead 7）；④ P1-02/P3-08（`7aaa3f2`）与 P3-03（`c5cc79d`）的「尚未提交」已订正；P3-16 的 `RunTest.vue` 44px 与 P3-17 的 `export_test.go:461` 已修复；P3-21 的 Webhook drain 子项部分修复、Push 字节上限子项仍未修复。本轮门禁仅覆盖 Go 侧：`gofmt`/`go vet`/`go build`/`git diff --check` 通过、定向两包 `-race` 通过、全量 12 包 `-race -count=1` 全部 ok、两个新增判别性用例各 `-race -count=20` 通过（含各自的红→绿判别力验证）；**未执行**前端构建、Docker/compose、真实云、SMTP/Webhook/Uptime Kuma、浏览器与远端 CI/GHCR，不外推为长期稳定绿色或外部验收通过。详见 `fwalizer-audit-final1.md`「最近核验基线」小节。

**P2-05 后续修复状态（2026-09-30）**：Webhook 三渠道业务响应校验与 16 KiB 有界读取已实施并提交为 `93e0e4b`，详见 [审计报告 P2-05](./fwalizer-audit-final1.md) 与 [Build7.md](./Build7.md) 后续补记。定向响应 race 20 轮、notifier 整包 race 3 轮、全量 12 包 race 1 轮及 vet/build/diff-check 已本地通过；重复证据不外推为全仓长期稳定绿色。真实钉钉/飞书/Slack 投递与该改动的产品浏览器、远端 CI/GHCR 未执行，PT-B7-03 仍为人工验收免除、非已通过。

**P3-26 / P3-25 资源扫描后续修复状态（2026-09-30）**：用户确认研究与临时副本验证方案后授权实施，基线 `ec82d10`。ECS 资源扫描严格区分结构异常与有效空数组：响应/Body/集合缺失、null 元素、缺失/空资源 ID 返回 `ErrSnapshotIncomplete` 且不带回半截资源；有效空数组仍按返回 token 分页；历史 token 集合同时拒绝未推进与环路。实际 SDK/API/临时 SQLite 回归证明异常保留旧缓存、完整结果覆盖、合法零结果清空对应地域，原生 nil 响应单独覆盖。定向新回归 race 20 轮、全量 12 包 race 1 轮及 vet/build/diff-check 本地通过，源码/测试/文档尚未提交。生产改动仅 ECS 扫描路径，未改防火墙同步、API 契约、schema、前端、SDK 或超时。缺失/null 集合失败为用户确认的保守兼容策略，真实云零资源是否省略集合未确认；未执行前端构建、浏览器、Docker、Go 1.25/Linux 或真实云/远端 CI，不外推稳定绿色或外部验收。证据与边界见审计报告 P3-26 和 I-19。

**P3-01 熔断器淘汰后续修复状态（2026-09-30，历史批次，后已提交 `ac0ee62`）**：用户确认细化后的 A 并授权实施，基线 `901d642`。成功解析删除域名计数；普通发布从新配置 DomainRules 提取原值域名，CloneForDomains 仅复制正数计数到紧凑的新 map，排除历史零值与已删除域名，暂停/零目标下保留仍配置域名的进度。新旧快照独立，空规则清空、删除后重加从零开始，完整导入继续 Reset。生产仅 dns/circuitbreaker.go 与 syncer/state.go；测试为两个对应文件及既有 API 导入测试夹具（补齐 probe.test 配置，导入仍含同域名以独立证明 Reset），不改变半开探测、IsOpen、schema/API/Provider 或健康告警。两个新增回归在旧实现上变红，修复后定向 race 20 轮通过；API 导入策略定向 race 20 轮通过；补齐夹具后全量 12 包 race 1 轮、vet、build、受影响 Go 文件 gofmt 检查与 diff-check 本地通过。源码/测试/文档尚未提交。Go 1.26.4 darwin/arm64；未执行 Go 1.25/Linux、前端构建、浏览器、Docker、真实云/通知链路与远端 CI，不外推长期稳定绿色或外部验收。详见审计报告 P3-01 实施补记。

**P3-02 每轮半开探测后续修复状态（2026-10-02，方案 B，历史批次，后已提交 `88154cd`）**：用户确认 B、完成仓库外候选与旧实现对照后授权正式修复，实施前基线 `main == origin/main == ac0ee62`、工作树干净。正式同步每轮创建独立 `dnsRound`，按轮初状态协调已熔断域名：一个调用负责半开探测，失败结果仅本轮复用；成功立即清空计数、等待者与后续 attempt 重新解析，不共享成功 IP。正常域名仍每 attempt 新解析；轮末只有尝试过且全轮无成功非空解析的域名加一次计数，未解析不变，阈值表示连续无成功解析的轮数。同轮任一成功优先与目标失败/运行健康相互独立，失败目标仍为 failed。DomainKey 固定 `Lower + TrimSpace`，配置裁剪、计数与解析去重统一身份，保留 P3-01 的成功淘汰、新旧快照独立及导入 Reset；**本段替代 P3-01 历史记录中的原值身份与不改变半开探测边界**。生产只改 breaker、目标同步与轮次传递，新增轮内协调文件；不修改 Provider、isRetryable、DNS 生产超时、schema/API/前端或事件粒度。源码/测试/文档尚未提交。本轮门禁与负向控制见审计报告 P3-02 实施补记；真实云、通知链路、浏览器、Docker、Go 1.25/Linux 与远端 CI/GHCR 未执行，不外推长期稳定绿色或外部验收。

**P3-04 日志 SSE 续传后续修复状态（2026-10-02，方案 B）**：用户确认定型方案并授权实施及文档回写，实施前基线 `main / 88154cd`、本地 `origin/main / ac0ee62`、ahead 1、工作树干净；前序 P3-02 已提交为 `88154cd`。广播器实例标识+递增日志序号，Last-Event-ID 按缓存边界增量续传；锁内先历史入队再注册订阅，保证历史先于实时。首次/非法游标/实例改变/缓存过期发带基准 ID 的 reset（空流为 0），前端 BigInt 去重、同文不同 ID 保留、最多 1000 行并展示连接/重置/不连续提示；连接不等待历史请求，卸载清理与晚到响应隔离。生产仅 logstream.go 与 Logs.vue，不改共享 SSE deadline、同步事件流、SQLite/Schema、Provider/DNS/通知。正式定向 race 20 轮、全量 12 包 race 1 轮、vet/build、前端 6+5+20 个回归与 build、gofmt/diff-check 本地通过，正式后端/前端负向控制均按预期变红。本轮源码/测试/文档尚未提交。真实浏览器、Go 1.25/Linux、Docker/compose、真实云/通知链路及远端 CI 未执行；浏览器登记 ProdTestList PT-AUDIT-01。缓存外/旧进程日志和慢订阅者满载丢弃不承诺无损，不外推长期稳定绿色或外部验收。详见审计 P3-04 实施补记。

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
- 渐进式熔断：阈值为同一规范化域名连续无成功解析的轮数；本轮尝试过且全轮无成功非空解析才在轮末加一，本轮任一成功立即清空计数，未解析不变
- 轮初已熔断域名在正式同步中协调一次半开探测：失败结论仅本轮复用，下一轮重新探测；成功后立即解除，等待者与后续 attempt 重新解析，不共享成功 IP；正常域名每 attempt 新解析，同 attempt 内去重
- 域名身份统一使用 `dns.DomainKey`（`Lower + TrimSpace`），不修改配置/展示原值，不新增尾点或 IDNA 规范化；Dry Run 独立解析、不参与正式轮次的探测名额/计数/事件
- DNS 熔断不提前跳过整目标，不改变目标重试、事件粒度或删除安全门；同轮其他目标解析成功不掩盖失败目标及运行健康异常
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

**P2-05 后续已实施边界（2026-09-30）**

- 普通 Webhook 成功必须符合渠道业务协议：钉钉为 HTTP 2xx 且存在整数 `errcode=0`；飞书为 HTTP 2xx 且存在整数 `code=0` 或旧 `StatusCode=0`，两字段同时出现时必须都合法且为零；Slack 为 HTTP 200 且 TrimSpace 后正文恰好为 `ok`。缺字段、`null`、错误类型、空体、非法或未知响应均失败，不以默认零值或消息文本判断成功。
- 响应体上限固定为 16 KiB，最多读取上限加一字节识别超限；原有 10 秒 HTTP 超时覆盖正文读取，在途名额保持到读取和关闭结束。错误/WARN 不输出或包装 URL、token、请求/响应正文、平台消息原文或底层读取/解析/关闭错误；业务错误仅保留安全渠道、固定类别和整数业务码。
- 业务失败沿既有 EventBus 返回错误并记录 WARN；响应体关闭错误只记录固定 `response_close` WARN，平台已明确接受时不因此改判业务失败。不增加重试、补发、投递状态持久化或健康联动，不改变配置契约、异步投递、每渠道在途限流及 Uptime Kuma Push 的独立成功口径。

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
- 运行时设置动态生效：日志级别使用 `app.LogLevelVar`（`slog.LevelVar`）与 `LogBroadcaster.SetLevel`；DNS 熔断阈值随完整 `RuntimeState` 原子发布（普通变更经 `dns.CircuitBreaker.CloneForDomains` + `SetThreshold` 仅保留新配置规范化域名身份的正数失败轮计数；成功解析立即删除条目，新旧 breaker 独立且不按历史 map 大小预分配；完整导入**确定**新建 breaker 并清空计数）
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
| 问题记录 | [Issue7.md](./Issue7.md)（P1-01 实施合同，Step 0～5 主体已实施，§12.5 追踪完整核验未完成项）；[Issue5.md](./Issue5.md) 与 [Issue6.md](./Issue6.md)（已实施问题记录与残余候选）；历史：[HistoryDocs/](./HistoryDocs/)（Issue1-4 已存档） | 记录的错误、固定产品语义、分步实施与验收合同 | 非强制，经验参考 |

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
| [Issue7.md](./Issue7.md) | 开发者 | P1-01 TAG 所有权、目标级规划、先增后验与平台化清理；Step 0～5 主体已本地实施，完整核验未完成项见 §12.5，真实云/浏览器/远端 CI 未执行 | 主体已实施（本地待修项与真实外部验收未完成） |
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
