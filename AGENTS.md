# AGENTS.md — FWAlizer AI 编码指令

> 本文档是给 AI 编码助手的指令集，也是项目**唯一的强要求文档**（详见「十二、文档体系与优先级」）。
> 项目设计方向见 [Design5.md](./Design5.md)（设计记录，当前），已完成构建记录见 [Build7.md](./Build7.md)（告警与运行健康，Step 0～7），当前工作见 [Issue7.md](./Issue7.md)（P1-01 TAG 所有权与目标级同步，Step 0～5 主体已本地实施；§12.5 记录 R7-01～R7-07 本地复核项已修复提交；真实云/浏览器/远端 CI 待人工执行）；问题历史见 [Issue5.md](./Issue5.md) 与 [Issue6.md](./Issue6.md)，人工验收清单见 [ProdTestList.md](./ProdTestList.md)。

---

## 一、项目基本信息

- **模块路径**：`github.com/alcaprophet/cloudhost-firewall-autoupdater`
- **仓库名称**：`cloudhost-firewall-autoupdater`
- **产品与兼容标识**：产品显示名、二进制名、`FWALIZER_DATA_DIR` 部署变量、数据目录及 GHCR 镜像继续使用 `FWAlizer` / `fwalizer`，避免破坏保留的部署边界
- **Go 版本**：`go 1.27.1`（源码构建最低补丁要求；CI/Docker 固定该版本）
- **平台约束**：仅支持 **Linux 与 macOS 13+**（平台文件 build tag 精确为 `linux || darwin`）；**不支持 Windows**（Windows pidfile 实现与 `%APPDATA%` 数据目录分支已移除，`GOOS=windows` 构建按预期失败）；构建与发布面向 `linux/amd64`
- **文档定位与优先级**：编码前先阅读本文件（强要求）。设计记录见 [Design5.md](./Design5.md)（当前，非强制，供参考）；Build6/Build7 为已完成的历史构建记录；当前实施合同与串行步骤见 [Issue7.md](./Issue7.md)（Step 0～5 主体已实施，§12.5 记录 R7-01～R7-07 本地复核收口）；问题历史见 [Issue5.md](./Issue5.md) 与 [Issue6.md](./Issue6.md)；人工验收清单见 [ProdTestList.md](./ProdTestList.md)；历史文档（Design1-4、Build1-5、Issue1-4）见 [HistoryDocs/](./HistoryDocs/)
- **Build6 已完成构建（历史记录）**：目标形态固定为 WebUI 单二进制 + SQLite。截至 2026-09-27，Step 0～7 已验收通过。Step 5 的工程实现、本地自动门禁、浏览器人工回归及真实云/DNS/同步链路已经完成并由用户确认真机通过；跨实例不同自增历史的人工交叉导入因当前无该使用场景而免除（底层 ID 映射仍由自动化覆盖）。Step 7 的自动补测、统一验收门禁、真实二进制/Docker 容器验收与文档闭环已完成，并按用户确认的最小边界修复 Issue6 A10/A5/A7/A6/A8；**在此之后按用户一次性授权完成 Issue6 批次 1（A1、A12）、批次 3（A20、A16）、批次 2（A2）、批次 4（A3、A18、A13、A15 与 A11 接口部分）、批次 5（A4、A9、A14、A17）、批次 6（A11 收尾）与批次 7（A19、A8 文档收口）**，批次 8（低风险清理）亦已完成，Issue6 §2.1 状态表为权威记录。远端 GitHub Actions 已取得真实结果（tag `v2.0.0` → 运行 `36300428681` 成功，含远端 `go test -race -v ./...`，并真实推送 `ghcr.io/alcaprophet/fwalizer:2.0.0`），Issue5 O5-02 已关闭。真实 Email/SMTP/收件箱与 Webhook 的人工验收（原 `ProdTestList.md` PT-B6-08/09）经用户 2026-09-27 明确决定跳过、由用户自行处理，属**人工验收免除**（沿用 PT-B6-04 先例），不阻塞 Step 7，**但这两个外部链路仍无真实通过结论，不得写成已经通过**。
- **Build7 实施状态（2026-09-28）**：Build7（[Build7.md](./Build7.md)，告警与运行健康）**Step 0～6 已全部实施完成，Step 7（核验缺陷修复）亦已完成**：告警三个触发开关（默认全部关闭，渠道与触发同时开启才订阅）、可编辑纯文本邮件主题与正文（固定事件后缀与稳定详情顺序）、`POST /api/alerts/test-email`、唯一 `OperationalHealth` 计算源（`internal/health`）+ 30 秒内部监督器 + `GET /api/health/operational`、Uptime Kuma Push（默认关闭、默认 60s、最小 20s）；配置包为 version 3，version 1/2 与其他版本直接拒绝。Step 7 修复了两项核验缺陷：告警页测试邮件请求体多带 `enabled` 导致必然 HTTP 400（前端改为显式 8 字段载荷，后端严格契约不变）；启动窗口把「同步引擎尚未启动」误判为「未运行」导致重启误报 Push DOWN 与运行健康异常（启动顺序确定性化 + 固定 10 秒启动宽限）。**本机已取得的证据**：`go test ./... -race -count=1`（12 包）、`go vet ./...`、`go build ./...`、前端 `npm ci`/`npm run build`/两条 `npm audit`（0 漏洞）、`docker compose config`、`docker build`、容器非 root（uid 1000）+ `healthy` + `docker stop` 有界且退出码 0、真实二进制进程级用例（静态 `/api/health` 与 `/api/health/operational` 200、Push 心跳发往本地 mock、SIGTERM 干净退出、UI 载荷测试邮件走完假 SMTP、重启后首条心跳为 up）。**仍无真实通过结论（不得写成通过）**：真实 SMTP 接受、真实收件箱投递、真实 Webhook、真实 Uptime Kuma HTTP Monitor 与 Push 的 DOWN/恢复通知、真实云 API、远端 CI/GHCR 均未执行，清单见 [ProdTestList.md](./ProdTestList.md)。
- **Issue7 实施状态（2026-09-29）**：P1-01「TAG 所有权与目标级同步」**Step 0～5 主体已本地实施**（提交 `28559ed`；F1/F5 核验补强已提交为 `38bdc19`）：严格 TAG 命名空间、canonical `FunctionalKey`、目标级纯 planner、四平台 revision/完整性、`S0 → Add → S1 → 覆盖验证 → 安全门 → Delete → 必要时 S2`、平台化条件清理、目标级事件/日志/Dry Run/Dashboard 主线均已落地；F1 Lighthouse 多端口展开粒度与 F5 SWAS 分页上限完整性已带判别性用例修复。**R7-01 已修复并提交为 `b80b1b0`**：只对“本 attempt 已由 S1 确认覆盖、失败点仅为可重试 Delete”的路径做类型化标记，前两次保持整目标重试，第三次耗尽后收敛为 `success + cleanup_deferred`/healthy；DNS/Describe/Add/S1/S2 失败仍为 `failed`，未绕过腾讯版本保护。**R7-02 已修复并提交为 `eab4bea`**：目标完成/失败事件统一补齐 `cleanup_deleted`、目标全生命周期 `duration_ms` 与 canonical `unsupported`，并由真实 publisher/EventBus/SQLite 整链证明清理计数与落库一致。**R7-03 已按用户裁决 A 修复并提交为 `297ccfe`**：Dry Run 对所有已配置目标各返回一项；无适用规则目标只返回非 null 空数组骨架与 `coverage_ready=false`，不解析 DNS、不读取云快照、不进入 planner、不产生限速等待；前端明确标记“无适用规则”并说明正式同步会跳过，正式同步 `RoundSummary.Total` 仍只统计有适用规则目标。**完整核验仍有 R7-04～R7-07 未完成项，不得把主体完成写成无保留闭环**：R7-04 为 S2 未参与最终残留计数的可观测性偏差，幂等 NotFound 必须消除 deferred 但不得虚增 `deleted/cleanup_deleted`；R7-05 为数组非 null 测试无判别力，必须改用结构化 JSON 检查并保留 null 负向控制；R7-06 与新增 R7-07 分别是 `TestIsRetryable_RealWorldShapes`、`TestAliClientRequestIsBounded` 丢弃 accepted `net.Conn` 导致 GC/finalizer 提前关连接的同根因 flaky，修复只能稳定测试夹具，不得扩大生产 `isRetryable`、降低超时下限或修改阿里云生产超时。后续固定按 Issue7 §12.5.4 串行处理：先 R7-06+R7-07 恢复可信门禁，再 R7-04，再 R7-05，最后多轮全量 race/vet/build/前端/diff-check 与文档闭环；**不得把单次全量绿色外推为稳定绿色**。当时 `main` 相对 `origin/main` ahead 1、工作树在本轮文档修改前干净，尚未推送（**该 ahead 1 为本段的历史快照；后续已推进至 `b84531b`，相对 `origin/main` ahead 7，见本文件「Issue7 后续本地修复状态」段与审计报告「最近核验基线」小节**）。**仍无真实通过结论（不得写成通过）**：真实腾讯云/阿里云（含 Lighthouse 多端口收敛与四平台删除安全）、真实浏览器回归与当前 revision 的远端 CI/GHCR 均未执行；清单见 [ProdTestList.md](./ProdTestList.md) PT-I7-01～07。逐 Step 证据、已提交补强与追踪项分别见 [Issue7.md](./Issue7.md) §12.3、§12.4、§12.5。
- **Issue7 后续本地修复状态（2026-09-30，优先于上一行的未完成项快照）**：R7-06/R7-07 与 R7-04 已提交为 `b19d271`：两个阻塞 TCP 测试夹具持有 accepted `net.Conn` 并有界回收；成功可信的 S2 planner 成为最终 `cleanup_deferred` 唯一来源，幂等 NotFound 保持 `deleted/cleanup_deleted=0`，S2 失败继续 `failed` 并使用保守 fallback。R7-05 已在当前工作树按测试-only 边界修复：`TestDryRun_ArraysNeverNull` 改用 `map[string]json.RawMessage` 检查顶层与目标级数组的存在、非 `null` 与 JSON array 类型，覆盖有适用规则、无适用规则骨架、零目标/零结果三种真实输出，并对全部数组字段加入 `null`、缺失、对象类型负向控制；未修改生产 DTO、planner、API 或前端。两个 GOGC 压力门禁、`syncer`/`provider` 包 `-race -count=20`（Syncer 显式 `-timeout=20m`）、全量 12 包 race 连续 3 次、vet、build、前端 build、diff-check 均本地通过。**R7-05 与本轮文档改动已提交为 `d6d208e`（不再处于未提交状态）**；P1-01 的 R7-01～R7-07 本地核验项已收口，但真实四云、浏览器与当前 revision 远端 CI/GHCR 仍未执行，故仍不得外推为外部验收通过或无保留发布闭环。

---

**第二轮只读核验与更正（2026-09-30）**：对审计报告做完整只读真实性核验后，按用户批准方案回写文档与测试（**生产代码零改动**）。关键更正：① P3-06 的「每方向 100」无官方依据——`AGENTS.md` §三「CVM 安全组规则上限 100 条」与官方 `SecurityGroupPolicyLimit` 定义均为**安全组级**上限，故 `provider/tc_cvm.go` 的四方向合计判定**不是过度保守**，原「只统计入站」修法已撤销，口径待 PT-I7-03 真实账号确认；② 新增 **P3-26**：`provider/scan.go` 分页中途空响应会静默截断并覆盖 `scanned_resources` 缓存（与 P3-25 资源扫描路径属不同缺陷类别，I-19 范围不变）；③ 新增「最近核验基线」小节固定当前基线（`main` / `b84531b`、`origin/main` / `d6d208e`、ahead 7）；④ P1-02/P3-08（`7aaa3f2`）与 P3-03（`c5cc79d`）的「尚未提交」已订正；P3-16 的 `RunTest.vue` 44px 已修复；P3-17 的 `export_test.go:461` 当时因只检索完整串 `version 2` 被误记为已修复，实际仍有 `v2` 注释，已由下方 2026-10-03 P3-17 实施补记更正并清理；P3-21 的 Webhook drain 子项部分修复、Push 字节上限子项仍未修复。本轮门禁仅覆盖 Go 侧：`gofmt`/`go vet`/`go build`/`git diff --check` 通过、定向两包 `-race` 通过、全量 12 包 `-race -count=1` 全部 ok、两个新增判别性用例各 `-race -count=20` 通过（含各自的红→绿判别力验证）；**未执行**前端构建、Docker/compose、真实云、SMTP/Webhook/Uptime Kuma、浏览器与远端 CI/GHCR，不外推为长期稳定绿色或外部验收通过。详见 `fwalizer-audit-final1.md`「最近核验基线」小节。 **P3-06 后续订正（2026-10-02）**：上述“合计判定不是过度保守、每方向 100 无官方依据”为当时仅取得泛称 API 字段定义时的历史判断；本次已取得 CVM 官方配额正文，明确默认入站、出站各 100 条，旧判断不再作为当前合同。当前以 §三与下方 P3-06 实施补记为准。

**P2-05 后续修复状态（2026-09-30）**：Webhook 三渠道业务响应校验与 16 KiB 有界读取已实施并提交为 `93e0e4b`，详见 [审计报告 P2-05](./fwalizer-audit-final1.md) 与 [Build7.md](./Build7.md) 后续补记。定向响应 race 20 轮、notifier 整包 race 3 轮、全量 12 包 race 1 轮及 vet/build/diff-check 已本地通过；重复证据不外推为全仓长期稳定绿色。真实钉钉/飞书/Slack 投递与该改动的产品浏览器、远端 CI/GHCR 未执行，PT-B7-03 仍为人工验收免除、非已通过。

**P3-26 / P3-25 资源扫描后续修复状态（2026-09-30，历史批次，后已提交 `901d642`）**：用户确认研究与临时副本验证方案后授权实施，基线 `ec82d10`。ECS 资源扫描严格区分结构异常与有效空数组：响应/Body/集合缺失、null 元素、缺失/空资源 ID 返回 `ErrSnapshotIncomplete` 且不带回半截资源；有效空数组仍按返回 token 分页；历史 token 集合同时拒绝未推进与环路。实际 SDK/API/临时 SQLite 回归证明异常保留旧缓存、完整结果覆盖、合法零结果清空对应地域，原生 nil 响应单独覆盖。定向新回归 race 20 轮、全量 12 包 race 1 轮及 vet/build/diff-check 本地通过，源码/测试/文档当时尚未提交，后已提交为 `901d642`。生产改动仅 ECS 扫描路径，未改防火墙同步、API 契约、schema、前端、SDK 或超时。缺失/null 集合失败为用户确认的保守兼容策略，真实云零资源是否省略集合未确认；未执行前端构建、浏览器、Docker、Go 1.25/Linux 或真实云/远端 CI，不外推稳定绿色或外部验收。证据与边界见审计报告 P3-26 和 I-19。

**P3-25 测试与文档后续补强（2026-10-08）**：用户在本聊天确认只读核验结论并授权优化剩余内容；实施前 `main == 本地 origin/main == 938bb8bac0a41ea285597ba454b34e727003d7ba`，工作树/暂存区干净。同步保护已由 `28559ed`、扫描保护与 P3-26 已由 `901d642` 提交，均保留在当前历史。本轮生产代码零改动：正式补充同步历史环路、token 空格/大小写原值、合法空中间页，以及真实 ECS SDK → 当前正式目标链的 S0/S1 重复/环路失败与正常清理对照；扫描补充 token 原值回归。统一本文和审计的历史“尚未提交”状态。范围为三份 Go 测试与本文/审计报告，共五文件；定向回归 race 20 轮、全仓 12 包 race 每项测试重复三次（单条 `-count=3` 命令）、vet/build/格式/diff-check 本地通过；八类外部 overlay 均按行为变红，详见审计文末「P3-25 当前补强记录」。整个分页操作的总预算仍为独立研究候选，未修改生产超时、SDK/依赖、API/schema、前端或目标状态机，未增加页数上限。真实云与当前 revision 的外部验收状态保持，不能把本地回归写成真实云/发布通过。

**P3-01 熔断器淘汰后续修复状态（2026-09-30，历史批次，后已提交 `ac0ee62`）**：用户确认细化后的 A 并授权实施，基线 `901d642`。成功解析删除域名计数；普通发布从新配置 DomainRules 提取原值域名，CloneForDomains 仅复制正数计数到紧凑的新 map，排除历史零值与已删除域名，暂停/零目标下保留仍配置域名的进度。新旧快照独立，空规则清空、删除后重加从零开始，完整导入继续 Reset。生产仅 dns/circuitbreaker.go 与 syncer/state.go；测试为两个对应文件及既有 API 导入测试夹具（补齐 probe.test 配置，导入仍含同域名以独立证明 Reset），不改变半开探测、IsOpen、schema/API/Provider 或健康告警。两个新增回归在旧实现上变红，修复后定向 race 20 轮通过；API 导入策略定向 race 20 轮通过；补齐夹具后全量 12 包 race 1 轮、vet、build、受影响 Go 文件 gofmt 检查与 diff-check 本地通过。源码/测试/文档尚未提交。Go 1.26.4 darwin/arm64；未执行 Go 1.25/Linux、前端构建、浏览器、Docker、真实云/通知链路与远端 CI，不外推长期稳定绿色或外部验收。详见审计报告 P3-01 实施补记。

**P3-02 每轮半开探测后续修复状态（2026-10-02，方案 B，历史批次，后已提交 `88154cd`）**：用户确认 B、完成仓库外候选与旧实现对照后授权正式修复，实施前基线 `main == origin/main == ac0ee62`、工作树干净。正式同步每轮创建独立 `dnsRound`，按轮初状态协调已熔断域名：一个调用负责半开探测，失败结果仅本轮复用；成功立即清空计数、等待者与后续 attempt 重新解析，不共享成功 IP。正常域名仍每 attempt 新解析；轮末只有尝试过且全轮无成功非空解析的域名加一次计数，未解析不变，阈值表示连续无成功解析的轮数。同轮任一成功优先与目标失败/运行健康相互独立，失败目标仍为 failed。DomainKey 固定 `Lower + TrimSpace`，配置裁剪、计数与解析去重统一身份，保留 P3-01 的成功淘汰、新旧快照独立及导入 Reset；**本段替代 P3-01 历史记录中的原值身份与不改变半开探测边界**。生产只改 breaker、目标同步与轮次传递，新增轮内协调文件；不修改 Provider、isRetryable、DNS 生产超时、schema/API/前端或事件粒度。源码/测试/文档尚未提交。本轮门禁与负向控制见审计报告 P3-02 实施补记；真实云、通知链路、浏览器、Docker、Go 1.25/Linux 与远端 CI/GHCR 未执行，不外推长期稳定绿色或外部验收。

**P3-04 日志 SSE 续传后续修复状态（2026-10-02，方案 B，历史批次，后已提交 `ebf8f19`）**：用户确认定型方案并授权实施及文档回写，实施前基线 `main / 88154cd`、本地 `origin/main / ac0ee62`、ahead 1、工作树干净；前序 P3-02 已提交为 `88154cd`。广播器实例标识+递增日志序号，Last-Event-ID 按缓存边界增量续传；锁内先历史入队再注册订阅，保证历史先于实时。首次/非法游标/实例改变/缓存过期发带基准 ID 的 reset（空流为 0），前端 BigInt 去重、同文不同 ID 保留、最多 1000 行并展示连接/重置/不连续提示；连接不等待历史请求，卸载清理与晚到响应隔离。生产仅 logstream.go 与 Logs.vue，不改共享 SSE deadline、同步事件流、SQLite/Schema、Provider/DNS/通知。正式定向 race 20 轮、全量 12 包 race 1 轮、vet/build、前端 6+5+20 个回归与 build、gofmt/diff-check 本地通过，正式后端/前端负向控制均按预期变红。本轮源码/测试/文档尚未提交。真实浏览器、Go 1.25/Linux、Docker/compose、真实云/通知链路及远端 CI 未执行；浏览器登记 ProdTestList PT-AUDIT-01。缓存外/旧进程日志和慢订阅者满载丢弃不承诺无损，不外推长期稳定绿色或外部验收。详见审计 P3-04 实施补记。

**P3-05 普通 JSON 响应禁缓存后续修复状态（2026-10-02，本次方案 B，历史批次，后已提交 `82ad2dc`）**：用户确认研究方案与五文件范围后授权实施及文档回写；实施前 `main / ebf8f19`、本地 `origin/main / ac0ee62`、ahead 2，工作树干净。生产仅 `webui/api/deps.go` 的 `writeJSON` 在提交响应头前无条件设置 `Cache-Control: no-store`，覆盖普通 JSON 成功/错误及 SSE/导出早期 JSON 错误；SSE 成功继续 `no-cache`、导出成功独立 `no-store`、静态 health/SPA 与 mux 自动响应边界保持。两份新增回归覆盖已提交头、真实 HTTP settings 成功/失败 GET/HEAD、四个凭据回显及状态矩阵；旧实现/补头过晚/只补 200 的正式负向控制均精确变红。新回归 race 20 轮、受影响两包完整 race 1 轮、全量 12 包 race 1 轮、vet/build、受影响 Go 格式与 diff-check 本地通过。代码规范固定共同出口规则，审计报告记录本次 B 与历史“接受现状”的 B 不同；I-07 仅 P3-05 收口，其他子项仍未修复。源码/测试/文档尚未提交；Go 1.26.6 darwin/arm64，未执行前端构建、产品真实二进制/浏览器、Docker/compose、Go 1.25/Linux、真实云/通知链路或远端 CI/GHCR，不外推长期稳定绿色或外部验收。详见审计 P3-05 当前实施补记。

**P3-06 CVM 入站配额后续修复状态（2026-10-02，方案 B，历史批次，后已提交 `c08f2e1`）**：用户在引用研究聊天选择并定型 B 后，本聊天按明确授权检查候选并正式实施及文档回写；实施前 `main / 82ad2dc`、本地 `origin/main / ac0ee62`、ahead 3、工作树干净，前序 P3-05 已提交 `82ad2dc`。官方正文已确认默认入站/出站各 100，替代第二轮历史“合计不是过度保守”的判断。唯一生产逻辑文件为 `provider/tc_cvm.go`：只数入站，两个入站统计完整则与 Ingress 条目数取较大值，统计不完整则回退明确数组（`[]` 有效），两种依据均不可用或响应/集合缺失返回 `ErrSnapshotIncomplete`，删除失去用途的 `uint64Val`；预计新增后 90 无 WARN、91～100 WARN 且允许、超过 100 整批拒绝。配额重读仅保护数量，不替代 S0 Version 或 S1 删除定位，满额轮换保留旧权限、不先删腾位；不动态适配提额账号。`syncer/retry.go` 只改注释，生产 `isRetryable` 不变；三份测试与 AGENTS/审计/Issue7/ProdTestList 配套回写，共九文件。正式 26 个 SDK 计数场景、4 个 WARN 边界、3 个 SDK 错误、零新增与正式目标链失败/版本竞争回归定向 race 20 轮通过；六类正式 overlay 负向控制全部按断言变红；受影响两包完整 race、全量 12 包 race 1 轮、vet/build、受影响 Go 格式及 diff-check 本地通过。源码/测试/本轮文档尚未提交，未 fetch/push。Go `1.26.6 darwin/arm64`；未执行 Go 1.25/Linux、前端构建、产品真实二进制/浏览器、Docker/compose、真实云/通知链路与远端 CI/GHCR，不外推长期稳定绿色或外部验收。真实零规则响应同时省略数组/统计时保守拒绝新增，模板统计及提额情况仍待 PT-I7-03（继续未执行）；I-10 其他子项继续独立追踪。详见审计报告 P3-06 当前实施补记。

**P3-07 旧同步流程清理后续状态（2026-10-02，方案 B，历史批次，后已提交 `ba82292`）**：用户依据引用研究聊天的结论授权再次检查范围、执行修复与文档回写；实施前 `main == origin/main == c08f2e1`，工作树与暂存区干净，未 fetch/push。再次证明现生产入口不调用旧链后，删除 `retrySync`、`retrySyncDetailed`、`truncateDesc` 及专属 imports、15 项旧入口测试、5 种专属 Provider 夹具与不再使用的辅助函数；不将旧算法搬入测试。现生产链为 `Run → syncAll → runRound → syncTarget → runTargetAttempt / runTargetCleanup`。在 `syncer/target_retry_test.go` 增加 12 个实际目标链重试/确认计数场景；描述边界直接归位 `provider/plan_test.go` 的共享渲染/截断实现。已走 `syncAll` 的 TAG 重试快照测试保留并重命名为 `TestSyncRound_TagSnapshotAcrossRetry`。`maxRetries`、现用错误判定函数、真实超时 accepted connection 夹具、`GetRules`（连接测试仍使用）、旧 Diff 及 P0-01 回归全部保留；唯一可执行生产源码变更是删除不可达内容，Lighthouse 只改一处描述注释。范围固定 11 文件，文档为本文件、审计报告、Issue6、Issue7。正式门禁结果见审计报告 P3-07 当前实施补记；源码/测试/文档尚未提交。本机 Go `1.26.6 darwin/arm64`，未执行 Go 1.25/Linux、前端构建、产品真实二进制/浏览器、Docker/compose、真实云/通知链路或当前 revision 远端 CI/GHCR，不外推长期稳定或外部验收。部分删除后 S2 失败、随后 S0 重试耗尽时残留计数被空 attempt 覆盖的独立观察只记录，未修改生产状态机或裁决其最终语义。

**P3-09 SQLite 写事务后续修复状态（2026-10-02，方案 A，历史批次，后已提交 `f6b3757`）**：用户在本聊天确认研究推荐并授权正式修复及同步文档，实施前 `main / ba82292`、本地 `origin/main / c08f2e1`、ahead 1，工作树与暂存区干净，前序 P3-07 已提交。唯一生产文件 `config/store.go`：新增独立 DSN 参数 `_txlock=immediate`，非 ReadOnly 的 Begin/BeginTx 在读取前预留写锁，避免日志/扫描独立提交使配置 WAL 快照升级失败；固定驱动对 `ReadOnly=true` 跳过 beginMode，导出/启动加载仍 deferred。原路径转义、单一 busy_timeout(5000) PRAGMA、一次性 WAL、schema/API、协调器发布、连接池和依赖保持，不加事务重试。三份测试及本文件/审计/Issue6，共七文件；旧拒绝 `_txlock` 的理由按驱动源码与判别性回归订正，保留 A4 历史证据。新回归与 DSN 结构定向 race 20 轮、入口约 5 秒超时/恢复、受影响两包完整 race、vet/build 通过；三类正式 overlay 负向控制按行为断言变红；全量 12 包 race 一轮、受影响 Go 文件 gofmt 检查与 diff-check 均通过。写锁占用超过 5 秒仍可能 BUSY/API 500，驱动不承诺 context 毫秒级取消，取消零提交/零发布及恢复已验证；ReadOnly 不代表驱动强制拒写。源码/测试/文档尚未提交，未 fetch/push。本机 Go `1.26.6 darwin/arm64`；未执行 Go 1.25/Linux、前端构建、产品真实二进制/浏览器、Docker/compose、真实云/通知链路或远端 CI/GHCR，不外推长期稳定或外部通过。详见审计报告 P3-09 当前实施补记。


**P3-10 剩余错误处理后续修复状态（2026-10-03，细化后的方案 A，历史批次，后已提交 `99f3fe2`）**：用户确认本聊天的研究推荐并授权正式修复及文档同步；实施前 `main / f6b3757`、本地 `origin/main / c08f2e1`、ahead 2，工作树与暂存区干净，前序 P3-09 已提交。生产仅 `config/store.go`、`webui/api/logstream.go`、`webui/api/sync.go`、`webui/server.go`：数据库初始化失败统一关闭，关闭也失败才 `errors.Join` 保留主错误与关闭错误；日志渲染检查并返回 Handler 错误，失败不更新序号/缓存/投递，不递归记录；同步 SSE 序列化失败只记录固定 WARN、事件类型与错误类型，跳过坏事件但继续连接，不记录负载或错误原文；静态 health 的 Write 失败记 Debug，不补写状态码或重试。pidfile 子项此前已由 I-01 收口，本轮不改。`bytes.Buffer.Write` 的 error 保证为 nil，本项日志渲染属于规范性补齐，不宣称复现生产 buffer 故障；`slog.Logger` 丢弃 Handler 错误，不宣称自动告警。三份新增测试与本文件/审计报告，共九文件。正式新回归及日志格式/回放/缓存控制 race 20 轮、外部 writer 故障注入 race 20 轮、受影响三包完整 race 与十类正式负向控制已通过；全量 12 包 race 一轮（含 TestMain 构建真实产品二进制的既有进程回归）、vet/build、受影响 Go 文件 gofmt 与 diff-check 均本地通过。P3-10 本地收口，单次全量绿色不外推长期稳定。源码/测试/文档尚未提交，未 fetch/push。本机 Go `1.26.6 darwin/arm64`；未执行 Go 1.25/Linux、前端构建、P3-10 专门进程级故障验收/浏览器、Docker/compose、真实云/通知链路或远端 CI/GHCR。保持 `_txlock=immediate`、schema/事务策略、SSE 帧/deadline/订阅、静态健康语义与原有满队列丢弃边界，不外推长期稳定或外部通过。I-07 的 P3-12/P3-14/P3-13 继续独立追踪；详见审计报告 P3-10 当前实施补记。


**P3-12 文件权限后续修复状态（2026-10-03，细化后的方案 A，历史批次，后已提交 `5e1d79c`）**：用户确认本聊天研究推荐并授权正式实施与文档同步；实施前 `main / 99f3fe2`、本地 `origin/main / c08f2e1`、ahead 3，工作树与暂存区干净，前序 P3-10 已提交。生产仅 `run.go`、`config/deployment.go`、`config/store.go`、`config/pidfile.go`：最终数据目录显式收紧为 `0700`；取得同一 FD 上的 flock 后、截断 PID 前收紧锁文件为 `0600`；SQLite 首次访问前预创建/收紧主库 `0600` 并处理关闭错误，打开前与初始化后迁移已存在 WAL/SHM，缺失不预创建、不清空、不删除；初始化后的权限失败沿用 P3-10 统一关闭与错误聚合。默认目录无法确定时返回错误，不再回退 `.`；显式专用数据目录优先且不依赖 HOME。四份测试与本文件/审计/README，共 11 文件；无全进程 umask、递归 chmod、自动 chown、后台巡检或新权限框架。正式新回归及既有 pidfile/P3-10 收尾回归在 macOS Go `1.25.0` 下定向 race 20 轮、正式产品进程新回归 race 20 轮通过；macOS Go `1.25.0` 与 Linux Go `1.25.14`（arm64 容器）全量 12 包 race 各一轮、vet/build 通过。正式 Linux/amd64 静态产品二进制在现有运行镜像中以 UID1000 验证目录/锁/DB/WAL/SHM 五种权限失败、新目录、旧宽权限目录以及新/旧/root-owned 命名卷；旧卷恢复属主后重启保留配置。六类源码 overlay 负向控制与辅助文件打开前检查的容器负向控制有判别证据，详见审计 P3-12 当前实施补记。P3-12 本地收口，I-07 的 P3-13/P3-14 继续未完成；源码/测试/文档尚未提交，未 fetch/push。单轮全量不外推长期稳定；Docker VM 为 linux/aarch64，amd64 二进制通过兼容执行，未执行原生 amd64 主机、完整发布镜像重建/compose、前端构建、浏览器、真实云/SMTP/Webhook/Uptime Kuma 或远端 CI/GHCR；权限保护不阻止 root/同UID 主体，不承诺 ACL/NFS 等特殊边界。


**P3-13 / Go 1.27.1 后续实施状态（2026-10-03，历史批次，后已提交 `0818197`）**：用户确认引用聊天《核查 P3-13 并准备 Go 升级》的设计及 Go 1.27 最新稳定补丁方向，授权正式修改与文档同步。实施前 `main == origin/main == 5e1d79c`，工作树与暂存区干净，前序 P3-12 已提交。当前最低 Go 为 `1.27.1`、最低 macOS 为 13；CI/Docker 固定同一补丁并设置 `GOTOOLCHAIN=local`。唯一业务生产源码改动为 `app/logutil.go`：删除自写 MultiHandler，直接用标准库顺序分发全部启用路、逐路 Clone 并聚合错误；接受单错误包装，使用 `errors.Is/As` 识别。两份日志测试补分发、聚合、隔离、并发与实际初始化入口的实时/回放回归，五处 `%q` 参数改为 `line.Line`。范围固定九文件（模块、生产日志、两份测试、Docker、CI、AGENTS、README、审计）；依赖版本与 `go.sum` 不变，CI 七条 SDK 更新策略保留。正式 macOS 定向 race 20 轮、两包完整 race、全量 12 包 race 一轮、vet/build/tidy-diff 及旧接线/旧工具链负向控制已通过；Linux Go `1.27.1`（arm64 容器）全量 12 包 race 一轮、vet 与 CGO=0 linux/amd64 编译通过。正式三阶段 amd64 镜像构建通过（前端阶段使用有效缓存），新镜像 UID1000/healthy、两个健康端点与 SPA 200、目录0700及四文件0600、0.158秒停止/exit0 均经断言验证；受影响 Go 格式与 diff-check 通过。P3-13 本地收口，最终证据见审计补记。本轮未 fetch、提交或 push。默认 stdout/stderr 的 SIGPIPE、Handler 阻塞/panic 和 Logger 丢弃错误边界保持；真实云/通知链路、浏览器、远端 CI/GHCR、macOS 13 真机与原生 amd64 尚未执行，不外推长期稳定或外部通过。P3-14 继续独立未完成，I-07 整体不关闭。


**P3-14 缺失依赖 HTTP 状态后续修复状态（2026-10-03，细化后的方案 A，历史批次，后已提交 `5c16116`）**：用户确认本聊天研究结论后授权正式修复及文档同步；实施前 `main / 0818197dc98d993c91d21da96715d887ca1754ee`、本地 `origin/main / 5e1d79c2b3378f31079e3240c75777b174c30781`、ahead 1，工作树与暂存区干净，前序 P3-13 已提交。生产逻辑仅 `webui/api/sync.go`、`webui/api/logstream.go`：trigger/dryrun/pause/resume/events/logs 六处 nil 依赖分支返回 503，trigger/dryrun 去掉错误的“请先配置目标和规则”引导；`deps.go` 只将 Syncer 的可空注释改为未接线。新增 `dependency_status_test.go` 覆盖六项 JSON 与真实 HTTP 输出、缺失依赖早于协调器/SSE 探测、status 200、已接线但未进入 Run 的 trigger 202、暂停 trigger 409、Dry Run 成功/冲突/内部失败和 SSE 能力缺失 500；`cachepolicy_test.go` 同步两个 SSE 错误响应期望值，保留 no-store。共七文件含本文件与审计；不改接线、前端、健康、事务、Provider/DNS、依赖或 SSE 生命周期，不新增 Retry-After。正式定向 race 20 轮、API 整包 race 一轮、全量 12 包 race 一轮、vet/build、受影响 Go 格式与 diff-check 本地通过；正式旧实现/status 改 503/Running=false 判不可用/提前探测 SSE/恢复误导提示五类外部 overlay 均按行为变红。P3-14 本地收口；复核 P3-05/P3-11/P3-10/P3-12/P3-13 的既有证据与提交祖先后，I-07 标记为**本地修复闭环**，本段替代前序历史段落中 P3-14/I-07 未完成的现时口径。正常启动在监听前已注入引擎/事件总线/广播器，空配置不等于缺失接线；Logs.vue 已有 P3-04 的连接提示，但 HTTP 503 不保证原生 EventSource 自动重连。源码/测试/本轮文档尚未提交，未 fetch/push；Go `1.27.1 darwin/arm64`，使用既有 ignored 前端 dist。未执行 Linux/Docker、前端构建、浏览器、真实云/SMTP/Webhook/Uptime Kuma 或当前 revision 远端 CI/GHCR，既有人工清单状态保持，单次全量不外推长期稳定或外部通过。详见审计报告 P3-14 当前实施补记。

**P3-15 丢弃告警日志聚合后续修复状态（2026-10-03，推荐方案 B，历史批次，后已提交 `ce3eeaa`）**：用户依据引用研究聊天授权改动前检查、确认后实施及文档同步；实施前 `main / 5c1611601f42d9373d04818c3cccc6a60937efd6`，本地 `origin/main / 5e1d79c`，ahead 2，工作树与暂存区干净，前序 P3-14 已提交 `5c16116`。生产仅 `notifier/inflight.go`、`notifier/email.go`、`notifier/webhook.go`：渠道长期 limiter 持有固定类别计数，首条立即 WARN 且 `dropped=1`，后续约每 30 秒汇总新增丢弃，空窗口不输出并停止续约；锁内取走快照、锁外日志，汇总输出后再计时。计数/窗口跨真实 Apply、关闭再开启、平台切换和旧实例晚到回调连续，混合平台标记 `mixed` 并列出数量；不存敏感事件或配置引用，保持每渠道 4 在途、满载丢弃最新、返回 nil、不排队不重试。新增 notifier 10 项/API 2 项回归与 AGENTS/审计/Issue6/Build7，共九文件。正式新回归 race 20 轮、notifier/API 整包 race 一轮、五类正式负向控制、vet/build/格式/diff-check 通过；新日志捕获夹具的标准 log writer/flags 恢复问题已仅在新增测试中修正。全仓 race 首轮在既有 syncer hook 夹具时序断言失败，单项 20 轮通过，原生产基线加仓库外夹具窗口放大控制按同一断言失败，复核后全仓重跑 12 包全部 ok；两次结果保留，syncer 源码/正式夹具零改动，不外推长期稳定。P3-15 本地收口，P3-21/I-08 整体继续独立追踪；源码/测试/本轮文档尚未提交，未 fetch/push。Go `1.27.1 darwin/arm64`，使用既有 ignored 前端 dist；未执行 Linux/Docker/compose、前端构建、浏览器、P3-15 专门产品进程验收、真实云/SMTP/收件箱/Webhook/Uptime Kuma 或当前 revision 远端 CI/GHCR，既有人工状态保持。进程在汇总前退出可能丢失未输出计数，汇总不证明恢复；Logger/Handler 既有过滤/错误/阻塞/panic 边界保持。详见审计 P3-15 当前实施补记。

---

**P3-16 前端状态后续修复状态（2026-10-03，推荐方案 B，历史批次，后已提交 `396885f`）**：用户在本聊天确认研究方案后授权正式修复及同步文档；实施前 `main / ce3eeaa5e1d119fdac97c1f98dc6178d17e79405`、本地 `origin/main / 5e1d79c`、ahead 3，工作树与暂存区干净，前序 P3-15 已提交 `ce3eeaa`。生产仅 App.vue、Targets.vue、Rules.vue、Settings.vue、useScannedResources.ts：高亮由实际路由派生；目标/规则及设置保存入口增加在途守卫，目标/规则保存锁覆盖列表刷新、关闭/改换表单与删除入口，卸载后不发布晚到提示；设置页只提交十个可见字段，不回传隐藏 theme，时长语法交给后端；扫描清空分别汇总两个产品结果，仅全成功提示已清空，失败产品保留缓存，清空使旧 GET 失效，并限制当前页面扫描/清空重叠。真实浏览器发现端口输入原 disabled 覆盖表单禁用，已将 saving 并入该表达式并补模板回归。新增 p316-state.test.mjs 与 test:state，沿用 Node/Vue SFC 测试，无新增依赖；同步本文件/审计/ProdTestList，共十文件。初始正式 23 项在旧实现上 3 通过、20 失败；最终新增 27 项与既有 31 项全部通过，五类正式负向控制及端口禁用负向控制按断言变红；正式前端 build、API/config 两包 race 一轮、vet、真实产品二进制编译及 diff-check 本地通过。新构建、独立临时 SQLite、loopback 产品与故障代理在真实浏览器覆盖路由/主题/双击保存/关闭限制/失败恢复/列表刷新失败/卸载晚到/三种清空结果/旧 GET/扫描在途/合法与非法时长，详见审计 P3-16 当前实施补记。P3-16 本地收口；I-10 的 P3-19/P3-20 等继续独立未完成。源码/测试/文档尚未提交，未 fetch/push；Go 1.27.1 darwin/arm64，未执行全仓 Go race、Linux/Docker/compose、npm audit、真实云/SMTP/Webhook/Uptime Kuma 或远端 CI/GHCR，不外推长期稳定或其他人工清单整项通过。单页守卫不提供跨客户端幂等或厂商级原子清空，侧栏主题乐观持久化等独立边界保持。

---

**P3-17 测试版本表述后续修复状态（2026-10-03，推荐方案 B，历史批次，后已提交 `5860cef`）**：用户在本聊天确认研究结论后授权正式修复与同步文档；实施前 `main / 396885f362faa37c84a5e5ca633bbb74d9c6fbdd`、本地 `origin/main / 5e1d79c2b3378f31079e3240c75777b174c30781`、ahead 4，工作树与暂存区干净，前序 P3-16 已提交。陈旧 `version 2` 为 10 处/6 文件，加两处 `v2` 后为 12 处/7 文件，替代审计“10 处/10 文件”与 export_test 已修复的旧判断。仅清理七份测试中的注释、子测试名称与失败提示：完整配置包明确 version 3，业务设置和 export_id 映射不绑定版本；实际 version 3 且缺 monitoring 的用例改名为“monitoring 缺失”，原 JSON 不改；敏感快照注释明确 settings/alerts GET 凭据回显及导入响应/日志不泄露边界。四处合法 version 2 拒绝/历史说明及 SDK /v2 保留。正式工作树与实施前快照的 Go token 比对确认，除一个子测试名称和一个失败提示外，其余非注释 token 全部一致；不修改生产源码、载荷、断言、API/schema/依赖或前端。七测试加本文件/审计，**连同该提交新增的 `TODOLIST.md` 共十文件**（2026-10-08 复核订正，原文"共九文件"为当时记录）；正式工作树全量 12 包 race 一轮、vet/build、受影响 Go 格式检查及最终文档后 diff-check 均本地通过，详见审计 P3-17 当前实施补记。P3-17 本地修复闭环，I-11 的 P3-18 继续独立未完成；本轮尚未提交，未 fetch/push。Go `1.27.1 darwin/arm64`，使用既有 ignored 前端 dist；外部验收状态保持，不外推长期稳定或真实外部通过。

**P3-18 文档漂移后续修复状态（2026-10-03，推荐方案 B，历史批次，后已提交 `f0997cd`）**：用户授权执行已研究的十文件范围，实施前 `main / 5860cefb4dc36aaeea6a2ff8290367ad1ef9da6b`、本地 `origin/main / 5e1d79c`、ahead 5，工作树与暂存区干净。九份 Markdown（Build6/Issue5/Issue6/Design5/Build7/ProdTestList/本文件/审计/TODOLIST）及 constants.ts 两处注释：更新当前入口，历史命令固定提交终点，区分文档基线/实现提交并标注旧授权与未提交状态；Design5 的 R7-01～07 状态对账，保留独立业务待办与外部未执行/免除边界。历史段落中的“当前/本轮/尚未提交”只指所标批次当时，不能替代最新状态索引；前序 P3-17 已提交 `5860cef`，其历史段的 P3-18 未完成由本段后续状态替代。P3-18 与 I-11 的本地文档/注释项收口，正式核验见审计文末 P3-18 补记。本次不改可执行代码、测试、API/schema/依赖或部署；尚未提交，未 fetch/push；不新增真实外部验收结论。

**P3-19 SMTP 安全错误后续修复状态（2026-10-03，定型方案 B，历史批次，后已提交 `974cb20`）**：用户依据引用聊天《研究 P3-19 修复方案》的定案授权执行修复及同步文档；实施前 `main / f0997cd0ada6c0c71cd386d85f8767f41d1411f2`、本地 `origin/main / 5e1d79c2b3378f31079e3240c75777b174c30781`、ahead 6，工作树与暂存区干净，前序 P3-18 已提交。唯一业务生产逻辑为 `notifier/email.go` 的 11 个失败出口，统一构造固定调用阶段、200～599 SMTP 数字码或固定中文类别，不复制服务器/底层错误原文、不保留 cause/Unwrap；`webui/api/test_email.go` 仅订正注释。两份新增安全回归与本文件/审计/Build7/ProdTestList，共八文件。正式定向 race 20 轮、notifier/API 两包完整 race 一轮、全量 12 包 race 一轮、vet/build、受影响 Go 格式与 diff-check 本地通过；五类正式负向控制均由行为断言检出，仓库外 deadline/正文 Write 故障探针在正式源码 overlay 上 race 20 轮通过、旧逻辑两出口对照按断言变红，详见审计 P3-19 当前实施补记。Build7 完整 SMTP 诊断旧约定由用户确认的 B 明确替代，既有发送顺序、10 秒拨号/30 秒会话时限、STARTTLS/PlainAuth 策略、API/no-store/零写入零发布、成功口径与在途限制保持。P3-19 本地收口，P3-20/P3-21 与 I-10 其他子项继续独立；源码/测试/本轮文档尚未提交，未 fetch/push。Go `1.27.1 darwin/arm64`，使用既有 ignored 前端 dist；未执行本项产品进程/浏览器、前端构建、Linux/Docker/compose、真实 SMTP/收件箱/云/Webhook/Uptime Kuma 或远端 CI/GHCR，不外推长期稳定或外部通过。供应商自由诊断与详细证书信息不再返回，固定输出长度不代表 SMTP 响应读取新增字节上限。

**P3-20 邮件 MIME 编码后续修复状态（2026-10-03，推荐方案：B 编码主题 + Base64 正文，历史批次，后已提交 `ee39f08`）**：用户依据引用聊天《研究并修复 P3-20 问题》的结论授权改动前复核、正式修复及同步文档；实施前 `main / 974cb2011d5bb03c4abde6578a8b082d47779ea3`、本地 `origin/main / 5e1d79c2b3378f31079e3240c75777b174c30781`、ahead 7，工作树与暂存区干净，P3-19 已提交。唯一生产文件 `notifier/email.go` 新增私有 MIME 序列化函数，标准库 B 编码主题并处理词间及首词折行；正文 CRLF 规范化后 Base64，每 76 字符折行。固定十文件：一份生产、五份新增/调整测试及本文件/审计/Build7/ProdTestList。研究预计八文件漏计 API 两份真实 SMTP 原始中文断言，本轮仅将它们改为 MIME 解码后的语义断言，未扩大生产范围。新增 18 场景在旧实现上全部变红，最终定向 race 20 轮、API 四项真实发送/订阅回归 race 20 轮、notifier/API 两包完整 race、真实产品进程 UI 八字段测试邮件、vet/build 已通过；全量 12 包 `go test ./... -race -count=1`、受影响 Go 格式与最终 `git diff --check` 均本地通过。五类正式源码 overlay 负向控制全部按行为失败，详见审计 P3-20 当前实施补记。保留 P3-19 安全错误、10 秒连接/30 秒会话、STARTTLS/PlainAuth、收件人 Trim、在途限制、业务渲染、API/SQLite/依赖/前端及成功仅 SMTP 接受口径。源码/测试/文档尚未提交，未 fetch/push；Go `1.27.1 darwin/arm64`，使用既有 ignored 前端 dist。未执行 Linux/Docker/compose、前端构建/真实浏览器、真实 SMTP/收件箱/云/Webhook/Uptime Kuma 或远端 CI/GHCR；人工免除不是通过，单次全量不外推长期稳定。P3-20 本地收口，P3-21 与 I-10 其他子项继续独立。

**P3-21 Push 响应读取后续修复状态（2026-10-03，定型方案 B，历史批次，后已提交 `9f46529`）**：用户依据引用聊天《研究并优化 P3-21 修复方案》的确认稿授权正式改进与文档同步；实施前 `main / ee39f08cba51ee505b678a7eba691c513f144d7d`、本地 `origin/main / 5e1d79c2b3378f31079e3240c75777b174c30781`、ahead 8，工作树/暂存区干净；前序 P3-20 已提交 `ee39f08`。已实际读取引用聊天、准备补丁/测试/文档清单，通过 HEAD/文件 SHA256/干净工作树/补丁可应用性复核。唯一生产文件 `internal/health/push.go` 增 16 KiB 有界完整读取、单 JSON 文档校验、`*bool` 确认 ok 与固定 Close 警告；新增 `push_response_test.go`，同步本文件/审计/Build7/Issue6/ProdTestList，共七文件。Webhook 保留及时失败并接受标准库有界清理，不保证异常连接复用。正式新增 12 个顶层响应测试定向 `-race -count=20 -timeout=5m`、health/notifier 两包完整 race 一轮、全量 12 包 `go test ./... -race -count=1 -timeout=20m`、`go vet ./...`、`go build ./...` 均通过；旧源码 overlay 与八类负向控制均按行为断言变红，无编译失败。受影响 Go 格式与最终文档后的 diff-check 见审计补记。复核 P3-01 `ac0ee62`、P3-02 `88154cd`、P3-15 `ce3eeaa` 的祖先关系与既有正式证据后，P3-21 与 I-08 标为**本地修复闭环**；本段替代前序历史段的未完成口径，I-10 整体仍独立。Go `1.27.1 darwin/arm64`，使用既有 ignored 前端 dist；未执行 Linux/Docker/compose、前端构建/真实浏览器、真实云/SMTP/收件箱/Webhook/Uptime Kuma 或当前 revision 远端 CI/GHCR。PT-B7-03 人工免除仍非通过，PT-B7-05 仍未执行；单轮全仓绿色不外推长期稳定或外部验收。源码/测试/文档尚未提交，未 fetch/push。

**P3-22 Dry Run 冷却后续改进状态（2026-10-03，推荐方案 B，历史批次，后已提交 `333451d`）**：用户在本聊天确认研究推荐并授权进一步改进及同步文档；实施前 `main / 9f46529662ee48c2e5b9ee83ea1c0ff3f3ace962`、本地 `origin/main / 5e1d79c2b3378f31079e3240c75777b174c30781`、ahead 9，工作树与暂存区干净，前序 P3-21 已提交。原逐规则重复快照问题已由 Issue7 Step 4 修复，本轮只优化后续等待：唯一生产文件 `syncer/syncer.go` 在既有 `dryRunMu` 下按 CloudType 保存下一次允许读取时间，读取前只补足剩余冷却；完整快照操作成功或失败后均记冷却，首次读取及末尾返回不额外等待，不同平台独立，跨连续调用/热更新/零规则保留。现有 5 秒/200 毫秒间隔、每目标一份快照、每目标每host解析去重、零写入/零事件/不改熔断器、结果顺序与数组/API/前端语义保持。仅改对应 dryrun_test.go 与本文件/审计/Issue7，共五文件。正式新增 12 场景与强化单目标断言定向 race 20 轮、既有 Dry Run/数组回归 race 20 轮、七类正式负向控制、vet/build 已通过；全量 12 包 race 一轮、受影响 Go 格式与最终文档后的 diff-check 均通过，最终证据见审计 P3-22 当前实施补记。只保存进程内时间，不加快照/DNS缓存、持久化、队列或协程，不把它扩为正式同步/扫描/连接测试/Provider分页的统一请求限流。Go `1.27.1 darwin/arm64`，使用既有 ignored 前端 dist；未执行 Linux/Docker/compose、前端构建/浏览器、真实云/通知链路或远端 CI/GHCR，既有人工未执行/免除边界保持。源码/测试/文档尚未提交，未 fetch/push；不外推长期稳定或外部验收。

**P3-23 同步计时后续修复状态（2026-10-05，定型方案 B，历史批次，后已提交 `9dd5d34`）**：用户在本聊天确认进一步研究后授权开始改动；实施前 `main == 本地 origin/main == 333451db6b8de8b8c55cc72e2a120328b20e2075`，工作树/暂存区干净，前序 P3-22 已提交 `333451d`。生产仅 `syncer/syncer.go` 的 Run：保存实际 tickerInterval，纯配置通知只在 interval 变化时 Reset；定时/手动轮返回后按最新 interval Reset（失败/idle 也算完成），保持轮后等待；false→false 不操作 ticker，恢复继续 Reset/立即轮/清空过期 trigger。启动与恢复保持轮前计时，旧 tick 与通知同时就绪仍无优先级，间隔变更从 Run 处理状态时生效。新增 `syncer/scheduling_test.go` 的 24 个虚拟时间真实调用链回归，加本文件/审计共四文件。正式新增回归在旧实现上 8 项按行为变红；修复后 24 场景 race 20 轮、syncer 整包 race 一轮、全仓 12 包 race 一轮、vet/build、格式及 diff-check 本地通过；七类正式工作树源码 overlay 均按行为断言失败，无编译失败/数据竞争。P3-23 本地修复闭环，详细证据见审计文末补记。未改 ApplyState/controlCh、运行时协调器、Provider/DNS、健康/重试/目标级状态机、API/schema/前端/依赖；P3-24 恢复边沿合并继续未修复，I-06 整体未关闭。Go 1.27 已移除 asynctimerchan，旧兼容模式不再是本项目待决策前提，不加 drain/Timer 重写。Go `1.27.1 darwin/arm64`，使用既有 ignored 前端 dist；未执行 Linux/Docker/compose、前端构建/产品进程/浏览器、真实云/通知链路或当前 revision 远端 CI/GHCR，既有人工未执行/免除状态保持；尚未提交，未 fetch/push，不外推长期稳定或外部验收。

---

**P3-24 恢复边沿后续修复状态（2026-10-05，细化方案 C，历史批次，后已提交 `938bb8b`）**：用户依据引用聊天《研究并核验 P3-24 修复》的定型结论授权正式修复及同步文档；实施前 `main == 本地 origin/main == 9dd5d34df2ec36dde4156eb349d4e65d521e8d78`，工作树/暂存区干净，五文件 SHA-256 与最终研究稿一致，前序 P3-23 已提交 `9dd5d34`。唯一生产文件 `syncer/syncer.go`：真实 false→true 在 `s.mu` 下与状态发布、镜像更新一起设置 `pendingResume`；Run 同锁取得最终状态并在起轮前消费标记，多次恢复合并一轮，最终暂停抑制补轮，新恢复留到下一批。启动消费既有标记，普通 ticker/trigger 准入前遇到标记转入恢复处理；最新 interval、过期 trigger 清理、Stop 门控、四处轮次入口及状态 hook 去重保持。新增独立控制回归 24 场景，原 P3-23 缺陷范围控制改为第 25 个 P3-24 回归，其余 23 个 P3-23 断言保持。旧实现与五类正式源码错误候选均在 race 20 轮按行为变红，无编译失败/超时/数据竞争；正式 P3-23/P3-24 race 20 轮、API 导入/协调器恢复 race 20 轮、syncer 整包 race、全仓 12 包 race 连续 3 轮、vet/build、格式/diff-check 本地通过。范围固定生产/两测试/本文件/审计，共五文件；P3-24 与 I-06 本地修复闭环，详细证据见审计文末补记。Go `1.27.1 darwin/arm64`，使用既有 ignored 前端 dist；全仓门禁含 TestMain 构建正式产品二进制的既有本地进程回归，不是 P3-24 专项进程验收。未执行 Linux/Docker/compose、前端构建/浏览器、真实云/DNS 上游/SMTP/收件箱/Webhook/Uptime Kuma 或当前 revision 远端 CI/GHCR；人工未执行/免除状态保持，连续三轮不外推长期稳定或外部验收。源码/测试/文档**已提交为 `938bb8b`**（该批"尚未提交"为当时记录，2026-10-08 复核订正；该提交即当前 HEAD），未 fetch/push。**仍未闭环的问题与待裁决事项见 [Issue8.md](./Issue8.md)。**

---

**I8-02 观察表达后续修复状态（2026-10-08，用户裁决 C）**：根据引用研究聊天的三项裁决授权实施，基线 `main / 256a7b3`、本地 `origin/main / 7696482`、ahead 1，工作树/暂存区干净。目标追加实际 attempts 与 nullable 清理/平台限制观察，S0/Add/S1早退保留历史来源，可信零可替换；清理区分 S1、可信 S2、delete_progress 估计，最新与最近完整规划分存。旧增删/cleanup_deleted 跨尝试累计，候选/残留整数为最近观察兼容投影；整轮追加 observed/estimated/historical/unknown 四类互斥汇总，只有 observed 参与已观察数量。Dashboard/SQLite 分开累计操作与候选观察，成功但估计零也提示清理未确认；outcome/健康与删除安全不变。范围固定 7 个生产/前端、6 个测试、5 个文档共 18 文件，无 schema/配置包/依赖变动，旧客户端忽略新增字段仍不能识别未知。定向两包 TestI802_* race 20 轮、全仓 12 包 race 每项重复三次（单条 -count=3 命令）、Dashboard 8 项与前端全部 66 项回归、前端 build、vet/Go build、格式/diff-check 已通过，门禁证据与字段合同见 Issue8 实施补记、Issue7 §12.9；真实浏览器/四云/通知链路、Linux/Docker/compose、原生 amd64/macOS 13 与当前 revision 远端 CI/GHCR 未执行，不外推外部验收或长期稳定。源码/测试/文档尚未提交，未 fetch/push。Q-02 与 TODO-008 的本地语义/修复已收口，其余台账独立保持。


**I8-18 告警 GET 一致快照后续修复状态（2026-10-08，用户裁决 A）**：用户确认窄只读事务及 8 文件范围后授权实施，基线 `main / 3f50f3821c86d04ee19b67a1d280a28069880194`，本地 `origin/main / 7696482243461f582342aeb78fdc2f467012f674`，ahead 2；实施前工作树/暂存区干净，未 fetch、提交或 push。生产仅 store.go 的 AlertsSnapshot/LoadAlertsSnapshot 与 alerts.go 的 GET 调用：四对象同只读 tx，方法返回前结束事务；HTTP 原默认值/原值/敏感字段/no-store/安全 500 保持，不读取无关业务表。正式两包 TestI818Snapshot* race 20 轮、受影响两包完整 race、vet/build 与五类行为负向控制通过；`go test ./... -race -count=3 -timeout=20m` 全仓 12 包通过（单条命令将每项测试重复三次，不称三次独立执行）；四个受影响 Go 文件的 gofmt 检查与最终 `git diff --check` 通过。全仓包含既有 TestMain 构建当前正式产品二进制的进程回归，不是 I8-18 专项产品验收。完整证据见 Issue8 文末补记；前端/写协调器/schema/配置包/依赖不变，不引入编辑冲突检测。Go `1.27.1 darwin/arm64`，Go 门禁使用本轮开始时既有 ignored 前端 dist（未重建前端）。未执行 I8-18 专项产品进程/浏览器、Linux/Docker/compose、原生 amd64/macOS 13、真实云/SMTP/收件箱/Webhook/Uptime Kuma 或当前 revision 远端 CI/GHCR；原人工未执行/免除状态保持。既有全仓门禁含 I8-04 网络 DNS 测试，包级 ok/自行 skip 不作为本项外部通过证据。本地重复回归不外推长期稳定或外部验收。源码/测试/文档尚未提交或推送。

---

**I8-111 配置导入严格解析后续修复状态（2026-10-08，严格方案 B）**：用户明确当前无兼容性需求、允许破坏性改动后授权正式修复和文档同步；实施前 `main / 3cc4c3d`、本地 `origin/main / 7696482`、ahead 3，工作树/暂存区干净，前序 I8-18 已提交。生产仅 decode.go/bundle_v3.go/export.go：导入 JSON v2、精确字段名、全层级重复与非法 Unicode 拒绝、合法转义等价；先完整 10 MiB 有界读取，超限 413、安全错误转换，数组复用 decoder/选项，不保留旧自定义解码。57 路径四类字段变体、旧配置/缓存/运行时/告警保留、限额与安全诊断正式回归定向 race 20 轮通过，六类负向控制按行为变红。全仓 12 包 race 每项重复三次通过；补充整数溢出回归后，安全错误专项 race 20 轮及最终全仓 race 一轮通过，vet/build/格式/diff-check 通过。 三生产/两测试/五文档共 10 文件，schema/版本3/依赖/普通 API/前端/事务发布链保持。Go 1.27.1 darwin/arm64，使用既有 ignored dist；未执行前端/专项产品进程/浏览器、Linux/Docker、真实云/通知链路或当前 revision 远端 CI/GHCR，不外推长期稳定或外部验收。源码/测试/文档尚未提交，未 fetch/push；完整证据见 Issue8 当前补记。

**I8-113 / Q-09 后续实施状态（2026-10-08，方向 A 细化为空渠道＋单选按钮）**：用户已在引用研究聊天确认空渠道初始值、不要求旧版程序导入新增空渠道配置包并保留 version 3，随后授权正式修复及文档同步。实施前 `main / 8052015c766121b05cdaf8afe2eabd55a45aa8db`、本地 `origin/main / 7696482243461f582342aeb78fdc2f467012f674`、ahead 4，工作树与暂存区干净，未 fetch。新建与补列 DDL 引用 `DefaultWebhookChannel=""`；初始化及 reset 共用显式渠道插入，旧表不重建。仅本次新增 channel 列时把已有 id=1 配置行转换为固定历史钉钉值，保留 URL，既有 Build7 一次性启用归零规则保持；已有列/配置不覆盖，再次启动不重复转换。GET/导出保留空值，关闭允许空渠道，开启必须选择，未知/缺失/null 继续拒绝。通知器取消隐式钉钉，并在限流/HTTP 前返回安全 invalid_channel。前端渠道为必填字符串，三个单选项初始未选中、放在 URL 前，开启未选零 PUT，关闭保留输入。九个生产文件（含本聊天追加确认的 embed.go 一行修正）、十三份测试与本文件/Issue8/Build7/ProdTestList/审计五份文档，共 27 文件；新前端构建生成的下划线资源需由 `//go:embed all:frontend/dist` 完整嵌入，用户已追加授权；正式证据见 Issue8 文末与审计 I8-113 补记。本轮尚未提交或推送；真实浏览器/通知平台与当前 revision 外部验收不因本地回归升级。

**I8-12 请求阶段后续实施状态（2026-10-08，推荐 A）**：用户在本聊天授权推荐方案与文档同步，基线 `main == 本地 origin/main == c3bb468`、工作树干净。三个前端文件采用四请求阶段、重试清空旧预览、持久失败原因、中性空结果/完成通知，最近请求结束时间不再充当成功标志；completed 不等于全部目标正常，现有目标级限制/错误/清理分区保持。专用 17 项真实页面/模板/请求封装行为回归及六类源码负向控制通过，完整前端 86 项与 build、Go vet/build 通过；全仓 12 包 race 一轮与最终 diff-check 通过，完整证据见 Issue8 文末补记。范围固定十文件，无后端/API/schema/依赖/同步/健康变更；真实浏览器待 ProdTestList PT-AUDIT-03，其他外部未执行/免除状态保持，不外推长期稳定或发布通过。尚未提交或推送。

**I8-03 构建入口后续修复状态（2026-10-08，用户裁决 A）**：用户确认共享真实前端前置后授权实施与文档同步；基线 `main / bb8e301`、本地 `origin/main / c3bb468`、ahead 1，实施前工作树/暂存区干净。Makefile 只补 test/vet 的 frontend 依赖；新增 17 项本地依赖/故障回归，五类仓外负向控制被检出。正式工作树真实前端构建、Go vet/build 与全仓 12 包 race 短模式一轮通过（外部测试包装器追加 `-short -timeout=5m`，Makefile 原测试命令不改）；无 dist/node_modules 的当前输入副本真实 `make -j4 vet build` 通过，各自安装/构建一次。README/本文/Issue8/审计同步，共六文件；最终语法、范围与 diff-check 通过。I8-03 本地收口、Q-05 已裁决，I8-04 和外部验收保持；无 Go 业务/embed/前端源码/依赖/CI/Docker 改动，不引入缓存或占位页。Go 1.27.1、Node 26.7.0、GNU Make 3.81；未执行 Linux/Docker、CI 固定 Node、浏览器或真实云/通知链路及远端 CI，单轮不外推稳定或外部通过。尚未提交或推送，未 fetch；完整证据见 Issue8 文末补记。

**I8-04 默认 DNS 门禁后续修复状态（2026-10-08，定型方案 B）**：用户依据引用研究聊天定型授权实施与文档同步；基线 `main / 6df6e848e476e61e5ee2cd8b6abd3384c50eb882`，本地 `origin/main / c3bb468478f02b32361c85be1090c70743750579`，ahead 2，实施前工作树/暂存区干净。默认 DNS 回归通过随机回环 UDP 服务运行真实 NewResolver/Resolve，不转发；完整测试命令保留，未加全仓 -short。真实上游测试仅在 dnsintegration 标签下显式执行，固定 223.5.5.5:53，网络/超时/不存在域名错误解析均 FAIL；与 -short 联用先报错，CI 默认只编译不执行。三份 DNS 测试、go.mod 直接测试依赖标记、workflow 及四份文档共九文件；生产源码、Makefile、go.sum 与模块版本不变。正式 DNS 包 race 20 轮、禁止非回环外发的默认 DNS 二进制 race 20 轮、八类行为负向控制、入口哨兵和真实测试本地正反对照通过；原样 make all 完成真实前端构建、vet、全仓 12 包完整 race 一轮及产品构建，另行 Go build/tidy-diff、格式/YAML/diff-check 通过。Go 1.27.1 darwin/arm64、Node 26.7.0；npm ci 提示 3 个 high 漏洞，未独立 audit 或改依赖。I8-04/Q-06 本地收口，不外推长期稳定、完整离线构建或真实上游/外部验收；Linux/Docker/浏览器、真实云/通知及当前 revision 远端 CI/GHCR 未执行。尚未提交/推送，未 fetch；正式证据见 Issue8 文末。

**I8-102 告警测试门禁后续修复状态（2026-10-08，推荐 B）**：用户确认研究推荐后授权修复和文档同步；实施前 `main == 本地 origin/main == bf2730320bbb53df5f6345fbfb9b7933b135b101`、工作树/暂存区干净。仅 test:alerts 逐项 30 秒预算，两项在途测试提前请求次数断言、t.after 结算挂起夹具并等待任务；同步本文/README/Issue8/审计，共六文件，生产代码与依赖/锁文件零改动，其他入口/Make/CI/Docker 保持。正式 npm 告警入口在 Node24.21.0/26.7.0 各独立 10 轮 8/8 通过，六套前端 86 项及 build 通过；加载/保存守卫负向控制均按行为变红且收尾哨兵通过，移除清理使哨兵变红。正式 script 的 30 秒未解 Promise 探针约 30.87 秒退出 1、9 pass/0 fail/1 cancelled，后续哨兵通过；移除参数到外部 4 秒上限仍未完成。语法/JSON/范围/diff-check 通过，完整证据见 Issue8 文末。I8-102/Q-15 本地收口；逐项预算不是命令硬上限，必须按退出码判绿，不承诺同步死循环/遗留句柄终止。未执行 Go 全仓门禁、npm ci/audit、Linux/Docker、浏览器、真实云/通知或当前 revision 远端 CI/GHCR，不外推长期稳定或外部验收。尚未提交/推送，未 fetch。

**I8-09 腾讯资源扫描后续修复状态（2026-10-08，方案 A，历史批次，已提交 `d3b3c4d`）**：用户确认研究定型方案后授权正式修复与文档同步；实施前 `main / b5dab987f997daba10b9a83e7f7791a2221968a4`、本地 `origin/main / bf2730320bbb53df5f6345fbfb9b7933b135b101`、ahead 1，工作树/暂存区干净，未 fetch。唯一生产文件 `provider/scan.go` 增加两个私有 typed 页解码函数并接入 Lighthouse/CVM 扫描循环，整页校验后才累计；结构异常保守失败，显式空数组合法，SDK 错误分类保持。两份专项测试与本文件/Issue8/审计/ProdTestList，共七文件。定向 92 个场景 `-race -count=20`、五类正式 overlay 负向控制、真实 `make frontend`（`npm ci`/build）、全仓 12 包 `go test ./... -race -count=1`、`go vet ./...`、`go build ./...`、受影响 Go 文件 gofmt 检查及 `git diff --check` 均本地通过，正式证据见 Issue8 文末 I8-09 补记。真实腾讯云零资源响应格式、真实云扫描/浏览器、Linux/Docker/compose 与当前 revision 远端 CI/GHCR 未执行；单轮全量绿色不外推长期稳定或外部通过。I8-08 SWAS、跨页 TotalCount 一致性与分页总预算保持独立未处理。源码/测试/本轮文档尚未提交或推送。

---

**I8-08 SWAS 资源扫描后续修复状态（2026-10-08，方案 A）**：用户确认研究推荐并授权正式修复及文档同步；实施前 `main / d3b3c4d65dfc34c4a35d843052f9afef7374aa09`、本地 `origin/main / bf2730320bbb53df5f6345fbfb9b7933b135b101`、ahead 2，工作树/暂存区干净，未 fetch。唯一生产文件 provider/scan.go 新增私有 typed 页解码并接入 SWAS 扫描；响应/Body/集合/元素/ID 异常整次 nil/error，保留旧缓存，零计数不豁免缺集合，显式空数组合法，名称可空、原值保持，SDK 错误分类不变。两份新增测试与本文/Issue8/审计/ProdTestList，共七文件。定向 `go test ./provider ./webui/api -run '^(TestDecodeSWASScanPage|TestScanSWAS)' -race -count=20`、五类正式 overlay 负向控制、真实 `make frontend`（npm ci + vue-tsc/Vite build）、全仓 12 包 `go test ./... -race -count=1`、`go vet ./...`、`go build ./...`、受影响 Go 文件 gofmt 检查与 `git diff --check` 均本地通过。全仓一轮包含受影响两包完整回归，单轮全量不外推长期稳定。本机 Go 1.27.1 darwin/arm64。npm ci/build 成功，但额外 npm audit 报 3 条 high（两个底层公告），独立记录于 Issue8 补记，未改依赖。Q-04 两部分本地收口；SWAS 总数一致性/短页/重复页与分页预算保持独立研究，不外推分页完整性。真实云/浏览器、Linux/Docker/compose 与当前 revision 远端 CI/GHCR 未执行，PT-AUDIT-05 保持未执行；源码/测试/本轮文档尚未提交或推送。

**I8-10 Lighthouse 完整快照后续修复状态（2026-10-09，用户裁决 B / Q-16）**：实施前 main 与本地 origin/main 均为 `36f7745`、工作树/暂存区干净。唯一生产逻辑文件 tc_lighthouse.go 固定 120 秒 context＋100 次分页查询，包含版本重读；额度耗尽返回空快照，次数耗尽不整目标重试，时间耗尽保留 deadline 身份并沿用超时重试。两份正式回归与本文/Issue7/Issue8/审计共七文件；定向两包 race 20 轮、六类正式负向控制、仓库外短预算真实 SDK 目标重试 race 20 轮、真实 make all（前端构建、全仓 12 包 race 一轮、vet、产品构建）、go build ./...、格式/diff-check 均本地通过，正式证据见 Issue8 文末。补充短预算目标测试在仓库外，不冒称已入仓或专门等待默认 120 秒验收。npm ci 仍提示既有三项 high，未独立 audit/升级。Q-12 ECS 部分、TotalCount/重复页/集合完整性与全目标预算独立；真实云/浏览器/Linux/Docker/通知/远端 CI 未执行，不外推外部通过或全仓长期稳定。尚未提交/推送，未 fetch。

**I8-11 SWAS 快照计数后续修复状态（2026-10-09，定型 B / Q-17）**：用户确认研究合同后授权正式修复与文档同步；实施前 main / 5452db1、本地 origin/main / 36f7745、ahead 1，工作树/暂存区干净。唯一生产逻辑 ali_swas.go 固定单次快照总数基准、拒绝负数/变化/超额并要求累计精确相等；缺失/null 不清除已有依据，始终无总数保留短页回退与 100 页上限。两份正式回归与本文/Issue7/Issue8/审计共七文件；25 个 Provider 与 19 个真实 SDK 目标链场景，S0/S1 零删除与 S2 已确认计数/估计观察保留有判别证据。定向 race 20 轮、五类正式 source overlay 负向控制、真实 make all（七包测试缓存命中）及追加一次禁用缓存的全仓 12 包 race、go build/格式/diff-check 均通过，正式证据见 Issue8 文末。Go 1.27.1 darwin/arm64，npm 安装仍提示既有三项 high，未独立 audit/升级。真实云/浏览器/Linux/Docker/通知/远端 CI 未执行，不保证原子快照或外部通过；资源扫描/规则结构/重复 ID/总预算独立。本轮未 fetch、提交或推送。

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
- `coverage_ready` 只表示当前完整快照已精确覆盖**非空的全部可实施期望**，且不存在 Add 提交状态未知；空期望或全量 unsupported 为 false。混合目标可为 `coverage_ready=true` 且 `partial`；该字段不代表整体成功或删除授权，unsupported 仍独立冻结清理。
- 只要所需功能已经重读确认存在，清理延后或删除失败仍记为 `success` 且 operational health 保持 healthy；平台能力导致的期望规则无法实施记为 `partial`；DNS/Describe/Add/新增后覆盖验证失败记为 `failed`
- 删除时“规则已不存在”视为成功（幂等），不报错
- 添加时“规则已存在”视为成功，WARN 日志并跳过
- 支持协议：TCP / UDP / TCP+UDP / **ICMP**（ICMP 时端口由各 Provider 按 API 要求处理：Lighthouse 传 ALL，阿里云传 -1/-1，CVM 省略 Port 字段）
- **TCP+UDP 协议拆分：** 仅阿里云 SWAS 原生支持 TCP+UDP，Lighthouse/CVM/ECS 均不支持，由目标级 `provider.PlanTarget` 自动拆分为 TCP + UDP 两条期望功能（旧 `buildDesired` 仅保留作 Diff 回归适配）
- **IPv6+ICMP 处理：** Lighthouse 使用 ICMPv6 协议，CVM 使用 ICMPV6 协议，ECS 不支持（AuthorizeSecurityGroup 无 ICMPv6，直接跳过并 WARN）
- 端口格式：单端口、逗号分隔、范围（`8000-8010`）、`ALL`
- 腾讯云 CVM 安全组默认额度为**入站、出站各 100 条**（[官方配额说明](https://cloud.tencent.com/document/product/213/43699)、[使用限制总览](https://cloud.tencent.com/document/product/213/15379)）；本工具只新增入站，使用 **100 条入站本地保护上限**，出站不占用该额度。预计新增后 90 条不告警、91～100 条 WARN 且允许、超过 100 条停止整批新增。计数包含所有入站条目，不按 TAG 过滤，不扣除待清理条目；取完整两个入站统计合计与明确返回的 Ingress 条目数的较大值，统计不完整时回退明确数组（`[]` 有效），两种依据均不可用或响应/规则集合缺失则返回 `ErrSnapshotIncomplete`、停止新增。配额重读仅作数量保护，不替代 S0 Version 或 S1 删除定位；暂不动态适配提额账号。

---

## 四、DNS 解析约束

- 使用 SQLite `dns` 业务设置指定的自定义 DNS 服务器
- DNS 上游地址接受 hostname/IPv4，可选 `:port`；IPv6 必须使用 `[地址]` 或 `[地址]:端口`，省略端口为 53，显式端口为 1～65535。裸 IPv6 配置拒绝（I8-17b / Q-18，2026-10-09 方案 A）；不推测 IPv6 最后一段为端口，不联网校验上游。
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
- Run 记录 ticker 实际使用的 `time.Duration`；纯配置通知只有 interval 实际变化才 Reset，同间隔保存保留剩余等待，不额外立即同步。间隔变更从 Run 处理最新状态时重新计时，不取消在途轮；普通配置通知只承诺最终状态，真实 false→true 恢复另由 `pendingResume` 保留。
- 定时/手动轮返回后（含 failed/partial/idle）按最新 interval Reset，保留完整轮后等待；启动/恢复仍在轮前创建/重置 ticker，不新增任意两轮的统一最小间隔。排队的显式手动 trigger 仍可在当前轮完成后立即执行，各轮在单一 Run 中串行。
- true→false Stop；false→false 不操作 ticker；最终为 true 且 Run 观察到 false→true 或有尚未消费恢复标记时，始终用最新 interval Reset，立即同步并清空既有排队 trigger，保留 Stop 门控。一次消费前多个恢复合并为一轮，最终暂停时不补轮；标记在补轮前与最新状态同锁消费，补轮期间的新恢复留待下一次处理。启动消费此前标记，由最终状态决定启动轮，避免重复补轮。ticker/trigger 的普通轮准入前遇到既有标记则转入恢复处理，检查后新增边沿属于下一批；不承诺 select 绝对优先级，不取消已开始轮次。“最终暂停不补轮”以状态消费的决策点为准。
- 当前 Go 1.27.1 合同使用同步 timer channel；asynctimerchan 已在 Go 1.27 移除，不加入 stale-tick drain 或旧 timer channel 兼容分支。
- 优雅退出：收到 `SIGTERM`/`SIGINT` 后，完成当前轮次再退出
- 支持配置热重载（WebUI 修改后通过 channel 通知 Syncer）
- 同步全局开关（SQLite `sync_enabled`，默认 true）：暂停时 ticker 与手动 trigger 均不触发同步；模拟测试与连接测试不受影响（独立于 Run() 主循环）

---

## 六、乐观锁与重试

- 每次写入前重新拉取最新规则状态；每次重试都从整个云目标的 Describe/规划重新开始，不沿用上一 attempt 的删除定位
- Lighthouse 完整快照额度（I8-10 / Q-16，2026-10-09 用户裁决 B）：每次 `GetSnapshot` 固定共用 120 秒请求 context 与最多 100 次 `DescribeFirewallRules` 分页查询，版本重读不重置额度；第 100 次若完整结束仍可成功，需要第 101 次才失败。额度耗尽返回空快照；次数耗尽返回 `ErrSnapshotIncomplete` 且不整目标重试，时间耗尽同时保留 `context.DeadlineExceeded` 并沿用现有超时重试，其他 SDK 错误链保持。S0/S1 不可信的 attempt 不得授权删除；后续可信 attempt 可恢复，S2 失败保持既有观察与计数合同。此为单次快照网络预算，不是整目标/整轮上限或强制终止 Go 计算的硬墙钟保证；不修改共享客户端 60 秒超时、SDK 重试配置、分页终止/版本保护或增删安全门；当前默认内部重试为零，未来改变 SDK 退避配置须重新验证取消。
- Lighthouse 和 CVM 的增删必须传入与当次快照一致的 `FirewallVersion` / `Version`；版本不匹配时重新 Describe/规划，不得降级为无版本保护删除
- 写入失败自动重试（最多 3 次，指数退避）
- SWAS 与 ECS 使用当次重读快照中的稳定 RuleID 删除；Lighthouse 只在功能 key 唯一且当前 TAG 归属唯一时删除；CVM 使用同一快照的 `PolicyIndex + Version` 定位并避免逐条删除导致索引漂移
- SWAS 规则快照计数（I8-11 / Q-17，2026-10-09 用户裁决 B）：每次 `GetSnapshot` 的首个可用 `TotalCount` 必须非负，复制数值固定基准；后续可用值必须相同，增长/下降均失败。已有基准时后页缺失或 null 不清除基准；中途首次出现总数约束此前全部累计规则，始终不可用时沿用短页终止。累计超过总数失败，精确相等才完成，不足且空页失败，不足且非空短页继续。保留 PageSize=100 与 100 页上限，第 100 页证明完成仍可成功。计数矛盾返回空快照与 `ErrSnapshotIncomplete`，不自动重扫、不新增整目标重试；普通 SDK 错误链/分类保持。S0/S1/S2 基准独立，允许合法增删造成三次快照总数不同；S0 失败零新增/删除，S1 失败保留已确认新增且零删除，S2 失败保留已确认增删和估计观察。只拒绝已观察计数矛盾，不保证分页期间无等量替换/重复 ID；集合结构、资源扫描、总预算与重试状态机不在本项范围。

---

## 七、API 频率限制

- 不同云厂商频率限制不同，取对应间隔（详见 HistoryDocs/Build1.md）
- 同一云厂商内串行处理（共享配额），目标之间加入间隔；同一目标内域名只做解析去重，不再以“单域名规则”为云端写入/重试单元
- 不同云厂商可并行同步（API 配额独立）
- Dry Run 按现有 `CloudType` 独立冷却：同一 Syncer 跨调用保留下一次允许读取时间，只在下一次快照读取前等待剩余时间；完整快照调用成功/失败结束后均按既定间隔更新。首次读取和末尾返回不额外等待，热更新/零规则不清空冷却，无适用规则目标不读也不更新冷却。该机制仅约束 Dry Run 的快照操作，不是所有云请求的统一限流；一次快照内部可有分页/版本重读，不保证一次 HTTP 请求。

---

## 八、Docker 约束

- 基础镜像：`alpine:3.20`
- 编译镜像：`golang:1.27.1-alpine`，编译阶段设置 `GOTOOLCHAIN=local`
- `CGO_ENABLED=0` 静态编译（Docker 构建）
- 非 root 用户运行（`adduser -D appuser`）
- 日志输出到 stdout（Text 格式，`docker logs` 查看）
- `HEALTHCHECK` 统一使用 WebUI HTTP `/api/health` 端点，不使用进程存活检查掩盖 HTTP 服务失效；Build7 起该端点保持静态存活语义，运行健康由 `/api/health/operational` 表达，**不得**把 HEALTHCHECK 改为 operational 端点

---

## 九、配置约束

- SQLite 是唯一业务配置源，目标、规则、云凭据、同步/DNS/TAG/日志/主题设置和告警均只通过 WebUI 管理
- 仅保留 `FWALIZER_DATA_DIR`、`WEBUI_HOST`、`WEBUI_PORT` 三个部署环境变量；它们不写入 SQLite、不进入配置包且不被配置导入覆盖
- `FWALIZER_DATA_DIR` 空白值按未设置处理；`WEBUI_HOST` 默认 `127.0.0.1`；`WEBUI_PORT` 默认 `60200` 且必须是 `1～65535` 的十进制整数
- 数据目录为应用专用目录：启动对最终目录显式收紧为 `0700`，主库、WAL/SHM 与持锁的 pidfile 为 `0600`；任一权限收敛失败即停止启动，不递归 chmod、不自动 chown。默认目录无法确定时返回部署错误，禁止回退到当前目录；显式 `FWALIZER_DATA_DIR` 不依赖默认路径解析。
- 不保留云凭据或其他业务设置的 ENV override，不提供 `.env` 业务模式、`.env.example` 或隐藏兼容开关
- 密钥使用云厂商 **CAM 子账号 + 最小权限**

### 9.1 已固定实施契约（Build7 已实施合同 + Build6 既有边界）

> 本节同时承载 Build6 已落地边界与 Build7 已实施合同。Build7 条款自 2026-09-28 起已随 Step 1～7 实施完成（逐步证据见 [Build7.md](./Build7.md) 第八节与第十一节）；Build6 条款中未被 Build7 明确替代的部分继续有效。

**Build7 已实施合同（告警与运行健康，Step 1～7 于 2026-09-28 完成；Step 7 为核验缺陷修复）**

- 配置包只接受 version 3；version 1/2 及其他版本一律 HTTP 400，不迁移、不补全、不兼容；version 3 固定包含 `metadata`、`targets`、`rules`、`settings`、`alerts`（`policy`/`email`/`webhook`）与 `monitoring.uptime_kuma_push` 全字段，数组与对象不得为 `null`
- 配置导入使用独立的标准库 JSON v2 严格解码：字段名大小写必须精确匹配，任意对象内拒绝重复字段（同值也拒绝），拒绝非法 UTF-8 与孤立代理项；合法 JSON 转义按解码后名称判断，`version` 与 `\u0076ersion` 同时出现仍为重复。先完成 10 MiB 有界读取，超限统一 413；其余解析/字段错误为安全 400，仅返回固定原因与必要字段路径，不回显原始错误或字段值。全部解码与领域校验完成后才进入配置写事务，非法输入零写入零发布；普通 API 不随本项切换解析器。（I8-111，用户确认当前无兼容性需求，采用严格方案 B。）
- 告警配置持久化固定为 `alert_policy`（三个触发开关 + `health_timeout`）、`alert_email`（含 `subject`/`body`）、`alert_webhook`、`uptime_kuma_push` 四张单行表；现有数据库执行一次性最小显式迁移，保证各单行表至多一行且业务 ID 固定为 1，不得依赖 `CREATE TABLE IF NOT EXISTS` 自动补列，也不得在每次启动重复重置用户配置
- 告警默认值固定全部关闭：`email.enabled`、`webhook.enabled`、三个触发开关与 `uptime_kuma_push.enabled` 的初始值与迁移结果均为 false；只有“渠道开关 + 对应触发开关”同时开启才安装该事件订阅；只开启触发条件但不启用渠道、或只启用渠道但不开启触发条件，都不发送自动通知；测试邮件独立于这些开关
- Webhook 未配置状态统一为关闭、空 URL、`channel=""`。新建/无主配置行/reset 显式使用 `DefaultWebhookChannel`，不采用旧表保存的渠道默认值；已有配置保留，只有本次新增 channel 列时转换已有主行的历史钉钉行为。GET/导出原样保留空渠道。`channel` 必填字符串，关闭允许空或合法渠道，开启必须选择 dingtalk/feishu/slack 并填写 host 非空的绝对 HTTP/HTTPS URL；未知、缺失、null 均拒绝。配置包仍为 version 3，新程序接受原有合法包，旧程序可能拒绝新增关闭空渠道包，用户已接受该兼容边界。通知器不隐式选渠道，相关事件的非法渠道在限流/HTTP 前安全拒绝；前端单选组初始未选中，关闭不清空输入。（I8-113 / Q-09。）
- 三个触发开关是邮件与 Webhook **共用**的全局策略，不为两个渠道复制两套开关；`health_timeout` 默认 `10m` 且只保留这一个健康超时；Push 间隔默认 `60s`、最小 `20s`
- 邮件固定一套可编辑纯文本主题与正文，自动邮件主题追加固定后缀（`DNS 解析失败` / `同步失败` / `运行健康异常`），正文为“用户文本 + 系统固定详情块”（`事件类型`/`时间`/`Provider`/`域名`/`错误`，缺失字段写 `-`，顺序稳定，不遍历 map）；该详情块由**邮件与 Webhook 共用同一渲染器**（Webhook 首行保留事件类型标识），两渠道顺序一致；运行健康异常事件在详情块末尾追加固定 `原因` 行（稳定原因用 `; ` 连接，无原因则不输出该行）；多收件人逗号分隔并逐项 Trim；邮件固定 `text/plain; charset=UTF-8`
- `GET/PUT /api/alerts` 顶层固定为 `policy`、`email`、`webhook`、`uptime_kuma_push` 四个完整对象；PUT 的四个对象及其全部子字段必须出现，拒绝 `null`、缺字段、未知字段、尾随 JSON 与多个顶层值；四部分在同一协调器写事务内覆盖保存，任一步失败全部回滚；commit 后只做无失败内存发布与循环唤醒，不在协调器锁内等待 SMTP、Webhook 或 Uptime Kuma；GET 四对象必须由 `Store.LoadAlertsSnapshot` 在同一窄只读事务内读取，事务在返回前结束，使用请求 context；不读取 targets/rules/settings、不进入协调器、不新增邮件/Webhook 归一化，表单默认值仍由 HTTP 层补齐；失败不返回半截快照。GET 必须 `Cache-Control: no-store`；该合同保证单次响应内部一致，不提供编辑版本冲突检测
- 新增 `POST /api/alerts/test-email`：使用请求中的未保存表单值，不写 SQLite、不进入配置协调器、不 Apply 告警集合、不改变订阅、不要求邮件渠道已启用；复用生产 SMTP 会话的 10 秒连接上限与 30 秒整会话 deadline；成功文案固定为“SMTP 服务器已接受测试邮件”，不得表述为已送达收件箱；测试结果只存在于页面内存与既有实时日志
- 邮件在共用发送出口序列化为 MIME：非 ASCII 主题使用标准库 RFC 2047 UTF-8 B 编码，只在 encoded-word 之间以 CRLF + SPACE 折叠，首行长度计入 `Subject: ` 前缀；encoded-word 不超过 75 字符，含 encoded-word 的物理行不超过 76 字符。声明 `MIME-Version: 1.0`、`Content-Type: text/plain; charset=UTF-8` 与 `Content-Transfer-Encoding: base64`；仅在序列化时将正文 CRLF/LF/裸 CR 统一为 CRLF，然后 Base64 每 76 字符折行，解码后保留规范化原文的末尾空白及换行有无。自动告警与测试邮件共用此出口，业务主题后缀/详情渲染与 Webhook 文本不变；不扩展邮箱地址或 SMTPUTF8 合同。
- SMTP 发送失败统一在 notifier 内构造安全诊断：仅含固定调用阶段、标准库解析的 200～599 SMTP 数字响应码或固定错误类别；范围外的响应码归入响应格式异常。其他错误按超时 → 可识别 TLS 类型 → 协议/挑战格式 → 连接关闭 → 其他网络错误 → 会话或安全策略异常分类。不复制服务器 Msg 或底层 Error() 文本，不保留原始错误链；测试邮件 JSON/WARN 与自动邮件 EventBus WARN 共用此边界。该条替代 Build7 的完整 SMTP 诊断历史约定，保留会话时限、STARTTLS/PlainAuth 策略、成功口径与在途限制。
- 运行健康固定由唯一 `OperationalHealth` 计算源提供（内部监督器、`/api/health/operational` 与 Uptime Kuma Push 共用，禁止各写一套）：SQLite 探活最多 2 秒且不持有 Syncer/协调器锁；Syncer 主循环未运行即 unhealthy（正常 shutdown 阶段不监督；进程启动后的固定启动宽限 `StartupGrace=10s` 内、主循环尚未进入运行态不视为异常，超出宽限仍按未运行处理；已进入运行后停止则立即 unhealthy，不受宽限影响）；内部监督器与 Push 循环必须在同步主循环进入运行态后才启动（启动顺序确定性化，不得让首检/首发读到尚未启动）；`sync_enabled=false` 时只保留 SQLite 与主循环检查；最近一轮 `failed` 或 `partial` 视为 unhealthy（直到后续 `success`/`idle` 覆盖）；当前轮次超过 `health_timeout` 未完成、以及 `sync_enabled=true` 时距最近完成时间超过 `interval + health_timeout` 均视为 unhealthy；`idle` 与空目标/空规则视为正常；多原因去重并使用固定排序
- `/api/health` 保持静态 Docker 存活语义不变（不反映同步或运行健康）；新增 `GET /api/health/operational`：健康 200、异常 503，固定 `Content-Type: application/json; charset=utf-8` 与 `Cache-Control: no-store`，每次请求现场计算，响应只含稳定原因，不泄露 SQL、路径、凭据或 URL；Dockerfile 与 Compose 的 `HEALTHCHECK` 继续使用 `/api/health`
- 新增 30 秒内部健康监督器：仅在健康→异常边沿发布一次 `notifier.EventOperationalUnhealthy`（事件数据只含检查时间与稳定原因数组），持续异常不重复发送、原因变化只更新日志，恢复只写 INFO 且恢复后再次异常可重新发布一次；配置保存后立即唤醒一次检查；第三触发开关只控制该事件是否进入邮件/Webhook，不关闭 operational 端点与 Push；监督器与进程 shutdown 同一生命周期，不产生孤立 goroutine
- Uptime Kuma 同时支持 HTTP Monitor 拉取 `/api/health/operational` 与 Push Monitor 反向心跳；Push 默认关闭，解析用户填写的完整 URL 后覆盖 `status`/`msg`/`ping` 并保留 token 与其他未知 query；HTTP 上限 10 秒、同一时刻最多一条在途、不排队不重试；仅 2xx 且响应 JSON 为 `{"ok":true}` 算成功；down 的 `msg` 由稳定原因用 `; ` 连接并截断到 250 字符以内；Push 失败不改写应用健康、不产生自激告警；日志不得包含完整 Push URL 或 token；shutdown 取消在途请求
- Push 的 HTTP 2xx 业务正文上限为 **16 KiB**，最多读取 16,385 字节识别超限；按实际 `Response.Body` 字节计数，默认 gzip 解压后的正文仍受保护。完整读取成功、未超限、整个正文为一个合法 JSON 文档且 `ok` 为非 null 的 true 才确认成功；允许合法尾随空白与未知字段，保留 `encoding/json` 的字段大小写匹配及重复键覆盖规则，不承诺拒绝所有重复键。非 2xx 及时记 `http_status` 并关闭，不主动额外 drain；读取失败/超限/非法 JSON/未确认 ok 分别记固定 `response_read`/`response_too_large`/`invalid_json`/`not_ok`。Close 错误只记固定 `response_close` WARN，不输出错误原文或敏感正文，不推翻已确认的业务结果；在途名额保持到读取与关闭结束。10 秒时限、单在途、不排队不重试、失败不改健康与 shutdown 取消继续有效。业务读取上限不是整个网络栈总下载上限；标准 HTTP/1 Transport 关闭后可能有界清理，异常连接复用不作保证，不将标准库清理的版本实现数值写为项目强要求。

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
- 每渠道（邮件 / Webhook）最多 4 个在途发送，满载丢弃最新并返回 nil；安全丢弃 WARN 由渠道长期限流器聚合：新活动周期首条立即输出 `dropped=1`，后续约每 30 秒汇总新增数量，首条不重复计入；空窗口不输出并停止续约计时。仅保存三类告警事件与固定安全平台的计数，不持有密码、URL、正文、事件或配置引用；混合 Webhook 平台标记 `mixed` 并列出平台计数。计数与窗口跨热重载、关闭再开启及旧实例晚到回调连续；关闭后允许汇总此前丢弃，汇总不表示渠道恢复。锁内取走快照、锁外写日志，汇总回调输出后才重新计时；不增加队列、重试、持久化或 shutdown 强制汇总，进程退出前未输出的计数可能丢失。
- Build6 的完整 Schema、字段校验与事务顺序继续作为既有实现基线；Build7 的 Schema 增量（`alert_policy` / `uptime_kuma_push` / `alert_email.subject|body`）、配置包 version 3、告警 API 与验收矩阵以 [Build7.md](./Build7.md) 为当前实施方案

---

## 十、CI/CD 约束

- 推送 tag（如 `v1.0.0`）时自动构建 Docker 镜像推送到 **ghcr.io**
- 镜像命名：`ghcr.io/alcaprophet/fwalizer:<tag>`
- 构建平台：`linux/amd64`
- CI 的 setup-go 固定 `1.27.1`，构建环境设置 `GOTOOLCHAIN=local`；云 SDK 更新若要求更高版本应显式失败，再单独决定升级，不隐式切换工具链。
- PR 时仅编译检查，不推送镜像
- **SDK 版本策略（有意设计）**：腾讯云/阿里云要求使用较新的 SDK，使用过期 SDK 无法调用新接口/功能；因此每次构建前执行 `go get -u` 将云厂商 SDK 升级到最新（见 `docker-publish.yml`「更新所有 SDK 到最新版」步骤），镜像始终携带最新 SDK，SDK 可复现性要求服从该策略

---

### 本地开发与构建入口（I8-03 / Q-05，2026-10-08 裁决 A）

- `make test`、`make vet`、`make build` 必须共享 `.PHONY frontend` 前置，先成功执行真实 `npm ci && npm run build` 再运行 Go 命令；前端失败时不得执行消费者，不以占位 dist、目录存在或旧 index.html 绕过构建。
- 完整本地核验推荐一次 `make all`；同一次 Make 调用共享一次前端构建，分别调用会分别重建。`make -j4 all` 允许前端完成后的 Go 检查/测试/构建并行，任一失败返回非零，不承诺其他并行任务从未运行或多个独立 Make 进程互斥。
- 裸 Go 命令使用已准备好的真实 dist，不自动生成前端；前端源码/配置/锁文件变化后重新构建。默认无参数 `make` 仍只构建前端，不引入增量缓存或新的 Make 版本要求。
- 依赖顺序与失败传播回归使用 `sh build/makefile_test.sh`，仅用隔离目录与本地命令替身；真实前端、静态嵌入与 Go 门禁需另行验证。默认 DNS 测试已按 I8-04 定型 B 使用本地回环服务；完整构建仍可能下载 npm/Go 依赖，不将本地收口写成完全离线或远端 CI/Docker 已通过。

### 告警前端测试门禁（I8-102 / Q-15，2026-10-08 方案 B）

- `npm run test:alerts` 固定使用 `node --test --test-timeout=30000 tests/alerts-load.test.mjs`，为每项测试设置 30 秒预算；裸 Node 运行需显式携带同一参数。正常退出码必须为 0，超时可能计入 cancelled，不以 `fail=0` 单独判绿。
- 加载中保存和重复保存的回归先记录调用返回的 Promise、在 await 前检查请求次数，再保留正常等待与后续状态断言；使用 `t.after` 在失败路径也结算挂起夹具并等待已启动任务。
- 该预算针对本地模拟请求回归，不对应产品测试邮件的 35 秒请求上限；整套测试可累计等待多个预算，原生超时不承诺中断同步死循环或清除遗留活动句柄。未增加进程级监督、统一其他五个前端测试入口，或将前端行为回归接入 Make/CI/Docker；这些保持独立范围。

### DNS 测试门禁（I8-04 / Q-06，2026-10-08 定型 B）

- I8-17b 默认回归同时覆盖 IPv4/IPv6 回环上游；IPv6 监听 `[::1]:0`，核验实际 A/AAAA 报文与结果，默认/显式端口通过 Dial.RemoteAddr 检查。IPv6 回环不可用时测试失败，不静默 Skip、不回退外部或 IPv4 上游。
- 默认 DNS 回归使用本地随机回环 UDP 服务，不转发查询，执行真实 `NewResolver → Resolve`；检查实际 A/AAAA 报文、地址/地址族/CIDR、错误链、时限与取消。完整默认 Go 测试不依赖真实 DNS 上游，不以全仓 `-short` 代替完整回归。
- 真实上游测试放在 `//go:build dnsintegration` 文件中，仅显式启用时访问 `223.5.5.5:53`；超时、网络错误、不存在域名被成功解析均 FAIL，不自动 Skip 或切换上游。显式真实验收与 `-short` 冲突时，在任何查询前报错；公共域名不固定动态 IP、不要求双栈。
- 默认 CI 增加 `go test -tags=dnsintegration -run '^$' ./dns`，只编译带标签测试，不执行其函数体；真实验收命令见 README，结果单独登记为通过、失败或未执行。
- 本合同只消除默认 DNS 用例的外网依赖；npm/Go 依赖准备仍可能访问网络，不等同整个构建离线或真实 DNS/云/通知/发布验收通过。生产 DNS 设置、默认超时与其他 DNS 语义仍以 §四为准。

## 十一、代码规范

- **所有 error 必须处理**，不可忽略返回值
- 同步事件 SSE 的序列化失败只记录固定 WARN、事件类型与错误类型，跳过坏事件并保持连接；不记录事件负载或序列化错误原文。日志渲染失败向 Handler 调用方返回错误，不在同一 Handler 内递归记录；静态 `/api/health` 写失败记录 Debug，不补写第二个响应。
- 日志使用 `log/slog`（Go 1.21+ 内置结构化日志）
- 注释使用**中文**（面向国内开发者）
- 遵守 `PlatformAPIDocs/` 中的 API 文档要求（参数格式、字段长度限制、频率限制）
- 多云抽象基于 Provider 接口 + 工厂注册模式（详见 HistoryDocs/Build1.md）
- 项目交付范围仅包含 WebUI 单二进制 + SQLite 以及 Docker/服务器部署；不包含 `.env` Headless 业务模式、CLI 子命令、桌面托盘、开机自启或原生桌面打包计划
- 日志多路复用使用标准库 `slog.NewMultiHandler`，只在 `app/logutil.go` 统一装配 stdout 与 WebUI；全部启用路按顺序分发，每路 `Record.Clone()`，错误由标准库聚合，调用方通过 `errors.Is/As` 识别，不依赖单错误直接相等。`slog.Logger` 丢弃 Handler 返回错误，不承诺自动告警。
- WebUI 模式通过 pidfile（`config/pidfile.go` + 平台文件（linux/darwin））防止多实例运行
- 目标同步观察：清理与平台限制观察用 nullable 对象明确未知，记录 attempt/时间/历史来源；删除进度估计不称当前云状态。latest 与 last_complete 分存，完整新零替换旧非零。RoundSummary 四类观察汇总不影响 outcome/健康/删除授权；旧整数仅作兼容投影，日志和 Dashboard 不把跨尝试累计删除与单次快照候选拼成数量关系（Issue7 §12.9）。
- 事件类型：全局同步完成用 `EventSyncComplete`，目标级云端写入与覆盖验证用 `EventTargetSyncComplete`，DNS 失败继续用 `EventDNSFailed`，运行健康异常边沿用 `EventOperationalUnhealthy`；`EventDomainSyncComplete` 不再承载云端增删数量
- 同步全局开关：`POST /api/sync/pause|resume` 端点（先写 DB 后通知 Syncer）；`SyncStatus.enabled` 字段；前端「模拟测试」页（路由 `/dry-run`）承载目标级变更预览，正式同步与 Dry Run 共用同一纯规划器；至少表达 `desired`、`satisfied_by_owned`、`satisfied_by_external`、`to_add`、`cleanup_candidates`、`cleanup_deferred`、`dns_errors`、`unsupported`、`conflicts`、`coverage_ready`；连接测试保留在目标添加/编辑弹窗（`POST /api/test-connection`）
- 模拟测试页面请求阶段固定为 `idle/running/completed/failed`；`completed` 仅表示收到可展示响应，不表示全部目标正常。每次执行清空旧 results/warnings/error；等待与失败不渲染结果统计或目标卡片，失败原因持久显示且可重试；最近请求结束时间在完成/失败均更新，不能用于推导阶段。空结果使用中性“本次未返回目标预览”并保留后端 warnings，不推导无变更或零配置；无适用规则与目标级错误/DNS/unsupported/conflicts/清理延后分区保持；完成通知用中性提示，不增加全局绿色无变更结论。
- 地域自动补全：数据源为 `PlatformAPIDocs/PlatformZoneGuide/`（后端 `webui/api/zones.go` 提供 `GET /api/zones`，文档更新时需同步数据）；后端仅提供预填数据、不校验地域合法性（允许输入列表外值，由云 API 自行报错，符合「不过度防御」）
- 资源扫描：`provider/scan.go` 实现四平台只读列表查询（Lighthouse `DescribeInstances`、SWAS `ListInstances`、CVM/ECS `DescribeSecurityGroups`），`webui/api/scan.go` 提供 `POST /api/scan-resources`（凭据缺失快速失败）、`GET/DELETE /api/scanned-resources`；结果按 cloud_type+region 覆盖式入库（`scanned_resources` 表），仅供添加目标自动补全，同步流程不依赖
- 腾讯资源扫描（I8-09，2026-10-08 方案 A）：Lighthouse/CVM 成功响应必须存在 Response 与显式资源集合；响应/集合缺失或 null、null 元素、缺失/null/空资源 ID 返回 `ErrSnapshotIncomplete`，任一页失败整次返回 nil/error，不带回已累计资源、不覆盖旧扫描缓存。显式 `[]` 合法，HTTP 成功资源始终为数组；名称可空，ID/名称原值与地域保持。`TotalCount=0` 不豁免缺失集合，本项不新增 TotalCount 一致性或分页预算规则；SDK 调用/解码错误保留原有错误链及分类。扫描 HTTP 的 200 + success=false/error + no-store 及对应 cloud_type+region 覆盖边界保持；SWAS 当前合同见下一条。
- SWAS 资源扫描（I8-08，2026-10-08 方案 A）：成功响应必须存在 resp/Body 与显式 Instances 集合；缺失/null 响应或集合、null 元素、缺失/null/空 InstanceId 返回包装 `ErrSnapshotIncomplete` 的错误。整页校验后才累计，任一页异常整次返回 nil/error，不返回部分资源、不覆盖旧缓存；`TotalCount=0` 不豁免缺失集合，显式 `[]` 合法且可清空对应地域。名称可空，ID/名称保持原值；SDK 调用/解码错误链及分类、HTTP 200 + success=false/error + no-store、按 cloud_type+region 覆盖边界保持。PageNumber/PageSize 与短页终止策略不变；TotalCount 一致性、提前短页/重复页及分页预算属独立研究候选，不承诺本项证明分页完整性。真实零资源格式与浏览器仍待 PT-AUDIT-05。
- 清空所有数据：`POST /api/config/reset` 只接受单一空对象 `{}`，经配置变更协调器在单事务内调 `Store.ResetAllTx()` 清空全部业务表（targets/rules/settings/sync_logs/alert_policy/alert_email/alert_webhook/uptime_kuma_push/scanned_resources），并恢复告警默认全部关闭与 `health_timeout=10m`、Push `interval=60s` 的默认值，等效重新初始化；前端入口需红色警告按钮 + 卡片式二次确认
- 普通 JSON API 的成功与错误响应统一由 `webui/api/deps.go` 的 `writeJSON` 在提交响应头前设置 `Cache-Control: no-store`；`writeError` 及请求/事务/内部错误包装共用该出口。SSE 成功流继续 `no-cache`，配置导出成功继续独立 `no-store`；静态资源、静态 `/api/health` 与路由器自动响应不由该规则改写。
- 缺失服务端依赖时，`/api/sync/trigger|dryrun|pause|resume`、`/api/sync/events`、`/api/logs/stream` 返回普通 JSON 503（`error` 单键、`no-store`），依赖判断早于协调器与 SSE 能力探测；不以 `Running=false` 替代 nil 判断，不承诺自动恢复或发送无依据的 `Retry-After`。`/api/sync/status` 的 nil 分支保持 200；暂停 trigger/Dry Run 冲突保持 409、SSE 能力缺失保持 500。
- 普通 API 最小校验边界：普通固定结构请求走 `webui/api/decode.go` 的 `decodeJSONStrict`（拒绝未知字段/尾随 JSON/多个顶层值，1 MiB、超限 413）；配置导入另走 `decodeBundleV3Strict`（10 MiB、JSON v2 严格规则见 §9.1），路径 ID 用 `strconv.Atoi` 严格解析且必须大于 0；请求 DTO 不含数据库 `id`；更新/删除按 `RowsAffected` 返回 404；规则请求的 `targets` 必须显式提供（省略返回 400），空数组仍表示适用于全部目标
- 配置变更协调器：目标、规则、settings、alerts、pause/resume、reset 与配置导入的写入口统一经 `webui/api/ConfigCoordinator` 串行化（锁 → 单事务 → 事务内完整业务快照 → 事务内构造候选 `RuntimeState` 与候选告警集合 → commit → 无失败发布：日志级别 → 告警集合 → `RuntimeState` → 运行健康监督器唤醒 → Uptime Kuma Push 唤醒）；唤醒时新运行时必须已可见；非法输入零写入零 apply，commit 之后不重新读库、不构造 Provider、不访问网络
- Provider 凭据为不可变值 `provider.Credentials`，由 `ClientPool` 在创建时持有且无 setter；已删除进程级全局凭据与 `provider.SetCredentials`，连接测试、资源扫描、正式同步与 Dry Run 共用同一显式凭据模型和一次快照
- 运行时设置动态生效：日志级别使用 `app.LogLevelVar`（`slog.LevelVar`）与 `LogBroadcaster.SetLevel`；DNS 熔断阈值随完整 `RuntimeState` 原子发布（普通变更经 `dns.CircuitBreaker.CloneForDomains` + `SetThreshold` 仅保留新配置规范化域名身份的正数失败轮计数；成功解析立即删除条目，新旧 breaker 独立且不按历史 map 大小预分配；完整导入**确定**新建 breaker 并清空计数）
- 前端 UI 规范：全局字号 16px、页面级操作按钮统一 `size="large"`（44px，`App.vue` themeOverrides 按分尺寸变量覆盖）；表格内操作按钮（编辑/删除）保持小号；所有二次确认使用 `NModal preset="card"` 卡片式弹窗（危险操作确认按钮 `type="error"`）
- 前端状态与提交边界：侧边栏高亮由当前路由派生；设置页仅提交十个可见设置键，theme 由侧栏单独部分更新，完整配置包仍保留 theme。时长业务校验统一由后端处理；目标/规则保存函数入口与 UI 共用在途守卫，原有控件独立 disabled 不得覆盖保存锁。扫描清空仅在全部产品成功时提示已清空，部分失败保留未确认产品缓存并明确反馈，旧 GET 不得恢复已清空缓存。
- 资源 ID 输入提示：按云类型区分文案（轻量云=实例 ID，CVM/ECS=安全组 ID），由 `constants.ts` 的 `resourceIdHint()` 统一承载（仅 placeholder，不引入额外说明块）

---

## 十二、文档体系与优先级

### 12.1 文档定位与优先级（本文件为唯一强要求）

| 文档类型 | 文件 | 定位 | 约束力 |
|---------|------|------|--------|
| **强要求** | **AGENTS.md（本文件）** | AI 编码指令与约束 | **唯一强要求，尽量不违背** |
| 设计构想 | [Design5.md](./Design5.md)（当前）；历史：[HistoryDocs/](./HistoryDocs/)（Design1-4 已存档） | 设计大方向、架构构想、决策记录 | 非强制，供参考 |
| 构建方案 | [Build7.md](./Build7.md)（Step 0～7 已完成）；历史构建记录：[Build6.md](./Build6.md)（已完成）；更早：[HistoryDocs/](./HistoryDocs/)（Build1-5 已存档） | 详细的分步构建方案与验收命令 | 非强制，执行建议 |
| 问题记录 | [Issue7.md](./Issue7.md)（P1-01 实施合同，Step 0～5 主体已实施，§12.5 记录 R7-01～R7-07 本地复核收口）；[Issue5.md](./Issue5.md) 与 [Issue6.md](./Issue6.md)（已实施问题记录与残余候选）；历史：[HistoryDocs/](./HistoryDocs/)（Issue1-4 已存档） | 记录的错误、固定产品语义、分步实施与验收合同 | 非强制，经验参考 |

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
| [Issue5.md](./Issue5.md) | 开发者 | Build6 阶段问题历史：R5、O5 与 A5 事项 | 已完成历史记录 |
| [Issue6.md](./Issue6.md) | 开发者 | 问题追踪：A1～A20 批次、后续核验与残余候选 | 活跃 |
| [Issue8.md](./Issue8.md) | 开发者 | **未闭环问题与待裁决事项台账**（2026-10-08 全量只读复核产出）：仍存在的缺陷、未完成审阅、待裁决语义、未派发专项、文档订正记录与外部验收边界 | 活跃 |
| [TODOLIST.md](./TODOLIST.md) | 开发者 | 业务待办（TODO-001～008）与文档动作 | 活跃 |
| [Issue7.md](./Issue7.md) | 开发者 | P1-01 TAG 所有权、目标级规划、先增后验与平台化清理；Step 0～5 主体已本地实施，R7-01～R7-07 本地复核收口见 §12.5；后续独立候选见审计/TODOLIST，真实云/浏览器/远端 CI 未执行 | 主体与 R7 复核已本地收口（外部验收未完成） |
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
