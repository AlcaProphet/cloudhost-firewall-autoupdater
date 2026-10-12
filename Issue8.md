# Issue8.md — 当前剩余事项与后续决策台账

> **2026-10-10 用户授权整合。** 本文件是唯一活跃问题台账，汇总 Issue5～7、审计报告和旧 TODO/Issue8 的剩余事项。整理仅修改文档，不授权实施下列生产、测试、依赖或发布改动。
> **当前验收：** 用户确认已完成测试并真机核验，除 SMTP 外暂无问题。Webhook 与 Uptime Kuma HTTP/Push 继续豁免且不阻断项目，豁免不等于真实链路通过；SMTP 详见 [ProdTestList](./ProdTestList.md)。已收口的普通云/浏览器人工项目不再登记待执行。
> **编号：** 当前采用连续 `I8-R01`～`I8-R25`，R 表示本次整合的剩余事项。旧 `I8-xx`、Q、TODO、A/P/R7 编号保留在来源字段与归档中，不能把新 R 编号与旧编号混用。未来追加从 I8-R26 开始，关闭后保留编号及结果，不重复使用。
> **整合时证据：** 2026-10-10 整合仅做文档/关键源码静态对账，未重跑测试、npm audit、外部探针或 CI；后续实施的实际证据以各条目补记为准。历史结果仍为历史证据；“候选/待研究”不等于确认产品缺陷，真机暂无问题也不关闭独立代码或测试候选。

## 1. 基线与顺序

整合前 `main / 8445df937a32f7293e18f4674a64d892d5112b63`，本地 `origin/main / de42602affcc70f45c76018999fc1275601aac2e`，ahead 1（未 fetch）；工作树已有上一轮九份人工验收文档修改，本轮保留。最近 I8-16 实现已提交为 `8445df9`，历史补记的“尚未提交”不代表当前实现状态。

顺序为 SMTP 与依赖记录、测试可信度、扫描与预算、接口/产品研究、长期回归及发布证据。该顺序不代表所有候选均为阻断项；研究定型与实施授权仍分开处理。

| 当前编号 | 剩余内容 | 状态 | 旧来源 |
|---|---|---|---|
| I8-R01 | SMTP 安全模式修复与真实投递 | 本地已修复；人工验收未完成 | PT-B7-01/02/06；Issue5 O5-03；Build7 |
| I8-R02 | 前端依赖 high 告警与项目依赖升级 | 本地已修复；依赖审计通过 | Issue8 I8-08 实施补记及后续 npm ci 记录 |
| I8-R03 | ECS 删除分批判别力 | 测试补强本地完成 | TODO-002；P2-02 |
| I8-R04 | hook 测试观察竞争 | 测试补强本地完成 | 旧 I8-17g |
| I8-R05 | SWAS 资源扫描分页完整性 | 本地已修复 | 旧 I8-08 后续候选；Q-04 剩余 |
| I8-R06 | ECS 与资源扫描操作总预算 | 本地已修复 | 旧 I8-38；Q-12 ECS 剩余 |
| I8-R07 | Pause/Resume 接口的原子更新 | 本地已修复 | 旧 I8-17d/I8-41；Q-10 |
| I8-R08 | CVM Describe 错误分类合同 | 合理分类待明确 | 旧 I8-31；Q-11；TODO-003；P3-06 |
| I8-R09 | dangling 目标引用 | 历史候选待复核 | Issue6 §6.5 第4项 |
| I8-R10 | 日志持久化回调并发与背压 | 容量候选待定型 | Issue6 §6.5 第5项 |
| I8-R11 | 同步事件 SSE 续传与心跳 | 契约候选待裁决 | Issue6 §6.5 第6项 |
| I8-R12 | 阿里云错误码重试范围 | 行为候选待裁决 | Issue6 §6.5 第8项 |
| I8-R13 | 运行时间戳重启展示 | 产品边界待确认 | Issue6 §6.5 第11项 |
| I8-R14 | SQLite 容量与辅助事务边界 | 研究候选待复核 | Issue6 §6.5 第12项 |
| I8-R15 | Server.Addr 关闭后语义 | 低收益接口候选 | Issue6 §6.5 第14项剩余 |
| I8-R16 | 其他清理符号清单 | 维护候选未裁决 | 旧 I8-42；Q-13剩余 |
| I8-R17 | 日志渲染失败仓内长期回归 | 证据缺口 | 旧 I8-22；P3-10出口④ |
| I8-R18 | A20 修复前负向控制 | 历史证据未重演 | 旧 I8-23 |
| I8-R19 | 组件退出与资源泄漏证明 | 测试覆盖候选 | 旧 Issue8 §3 goroutine/fd/ticker |
| I8-R20 | 启动宽限10秒数值锚点 | 低成本证据缺口 | 旧 Issue8 §3 DefaultStartupGrace |
| I8-R21 | 前端长期行为覆盖与异步门禁 | 补强候选待复核 | TODOLIST P2-06/08/09/P3-16；旧 Issue8 §3/ I8-102 |
| I8-R22 | 当前 revision 远端发布证据 | 独立证据待完成 | PT-B7-08；Issue5 O5-02历史边界；旧 I8-25部分 |
| I8-R23 | 特定平台与长期运行证据 | 证据范围待核对 | 历史audit/Issue8外部边界 |
| I8-R24 | 合理前置条件与防御接口 | 非缺陷维护候选 | 旧 I8-17e/I8-47/I8-44-1/2/3 |
| I8-R25 | 历史统计与测试命名精度 | 文档/测试注释候选 | TODO-004/007；旧 Issue8附录B |

## 2. 剩余事项详情

<a id="i8-r01"></a>

### I8-R01 SMTP 安全模式修复与真实发送投递

- **状态与来源：** 用户依据“检查腾讯云 SMTP 兼容性”和“检查腾讯云 SMTP 修复方案”授权实施，显式安全模式已本地修复；PT-B7-01/02/06 真实验收继续未完成。
- **已确认问题：** 用户提供 `smtp.qcloudmail.com:587`、反馈“服务器未响应”；研究聊天历史无认证探测记录为本机 587 欢迎消息超时、465 TLS 验证与欢迎消息成功，不代表部署机器或真实认证/投递通过。旧共用出口未在欢迎消息前握手，缺少隐式 TLS 能力。
- **实施合同：** `security` 固定三模式 auto_starttls/starttls/implicit_tls，与端口独立；腾讯云目标465 + SSL/TLS。旧库显式补列默认auto_starttls，保留已有配置和开关，重复启动保留已选模式；新库/reset/无行GET及导出默认一致。保存/测试/v3导入严格必填，旧v3缺字段拒绝，新包可能被旧程序拒绝，版本仍为3。共用出口先校验模式、显式Hello、保留具体TLS连接类型及证书验证；10秒建连后单一30秒会话deadline，不重计时。前端三项单选，切换不改端口，测试明确9字段，自动通知器与完整快照传递模式。未修改认证策略、依赖、队列、同步/云状态机或发布配置。
- **正式本地证据（2026-10-10）：** Go 1.27.1 darwin/arm64；配置、notifier、API 的 `TestSMTPSecurity*` 定向 `-race -count=20` 通过，新增无行导出/原值保留回归单独 race 20 轮通过。协议覆盖受信任CA+非回环身份的三模式AUTH/DATA、隐式TLS首字节与不二次STARTTLS、强制升级拒绝/握手失败、EHLO→HELO回退、证书不可信/主机名不符、静默握手/欢迎消息与唯一deadline、非法模式拨号前拒绝；三个自动邮件事件共用出口，API测试/通知器构造实际传递模式。迁移走正式OpenStore/重启/reset；API保存/完整快照/热发布/三模式v3往返、旧包缺模式拒绝且零写入零发布；58条配置包字段路径及9字段产品进程载荷回归通过。前端告警11/11、导入9/9通过，含真实组件SSR单选检查。`make all` 成功（真实npm ci/build前置、vet、全仓race、产品构建），另行 `go test ./... -race -count=1` 12包均通过；格式与diff-check通过。单轮全仓不外推长期稳定。
- **判别力与边界：** 仓库外overlay分别破坏证书验证、强制升级、deadline、不隐藏TLS类型、隐式握手、迁移开关保留、测试入口与自动通知器模式传递，共8类均由预期行为断言变红，无编译失败/panic/race/整体测试超时作为最终判别证据。本轮npm ci仍报告3条high，依赖问题保持I8-R02独立处理；未执行真实SMTP认证/发信/收件箱、真实浏览器、Linux/Docker或当前revision远端CI/GHCR，未fetch/提交/推送。自动测试与假SMTP不关闭本项人工验收。
- **后续与完成条件：** 分别验证测试邮件、DNS/同步失败自动邮件、运行健康边沿邮件；区分 SMTP 接受与收件箱投递，检查 MIME/中文/长主题、安全拒绝反馈。具体清单统一维护于 ProdTestList.md。

<a id="i8-r02"></a>

### I8-R02 前端依赖 high 告警与项目依赖升级

- **状态与来源：** 2026-10-11 本地修复与依赖审计收口；来源为 Issue8 I8-08 实施补记及后续 npm ci 记录。实施前 `main == 本地 origin/main == 5814725670e770841e1372b1e166d20dfbd84eba`，工作树/暂存区干净，未 fetch。
- **问题复核：** 本轮联网完整审计和 `--omit=dev` 审计均退出 1，确认三条 high、两个底层公告：@vue/server-renderer（[GHSA-g2v6-rqmx-r4w6](https://github.com/advisories/GHSA-g2v6-rqmx-r4w6)）、其上游 vue 条目、source-map-js（[GHSA-68fv-2mgg-jv7q](https://github.com/advisories/GHSA-68fv-2mgg-jv7q)）。Vue 3.5.42 与 source-map-js 1.2.2 为修复起点。产品为浏览器端 Vue + Go 静态文件服务；原依赖生产构建的模块清单没有 server-renderer、source-map-js、Vue 编译器或 PostCSS，SSR 使用位于测试，source-map 消费位于构建工具。本轮未发现线上用户输入进入这两个触发点的路径，但生产依赖分类不等于产物可达性，现有两条 CI 审计门禁确会被旧依赖阻断。
- **用户裁决与授权：** 用户允许项目依赖升级到最新版，并明确包括前端与 Go 后端。原生 TypeScript 7.0.2 在隔离副本使 vue-tsc 的 `typescript/lib/tsc` 解析失败，且现有测试使用的 `ModuleKind` / `transpileModule` API 不再可用；用户选择 Microsoft 官方 TypeScript 6 兼容包最新版，保留现有构建和测试入口。采用官方 npm alias `typescript: npm:@typescript/typescript6@^6.0.2`（当前内部 API 包为 @typescript/old 6.0.3），不以原生 TypeScript 7 通过作结论。方案依据见 [Microsoft 兼容包说明](https://devblogs.microsoft.com/typescript/announcing-typescript-7-0/#running-side-by-side-with-typescript-60) 与 [vue-tsc 3.3.13 的兼容包路径支持](https://github.com/vuejs/language-tools/blob/v3.3.13/packages/tsc/index.ts)。
- **正式范围：** 仅修改 go.mod/go.sum、前端 package.json/package-lock.json 与 AGENTS/README/本文件，共七文件。前端直接依赖当前稳定版为 Vue 3.5.43、Vue Router 5.4.0、Naive UI 2.45.3、Vite 8.3.4、plugin-vue 6.0.10、vue-tsc 3.3.13，TypeScript 按上述兼容包裁决；source-map-js 锁定 1.2.2，其余传递依赖在上游范围内刷新。Go 当前 module path 的显式依赖复核无剩余 Update；15 个模块更新，包括 ECS v7.11.5、Tea v1.5.3、腾讯 common v1.3.192/Lighthouse v1.3.182/VPC v1.3.188、x/net v0.61.0、SQLite v1.60.1 及相关间接依赖。OpenAPI v2.2.4、SWAS v3.0.0 已为对应 module path 最新版；另查 OpenAPI/v3、ECS/v8、SWAS/v4 均无可用版本。生产源码、测试、API/schema、Go 1.27.1、CI/Docker 固定 Node、基础镜像和七条 SDK 自动更新策略未改；无 overrides、--force 或审计门禁豁免。
- **正式本地门禁：** macOS Go 1.27.1 / Node 26.7.0 下原样 `make all` 通过（真实 npm ci/vue-tsc/Vite、vet、全仓 12 包 race 和产品构建）；随后独立 `go test ./... -race -count=1 -timeout=10m` 全仓 12 包通过，明确禁用结果缓存。正式前端全部 98 项回归通过；完整和生产依赖审计均退出 0、0 漏洞，npm outdated 返回空对象。go mod tidy -diff 与构建入口替身回归通过。固定 Node 24.21.0 的 Linux arm64 Docker 前端构建、98 项测试与两条审计通过。与正式四份依赖文件逐字节一致的隔离副本在 Linux arm64 Go 1.27.1 下全仓 race 一轮、vet/build 通过；linux/amd64 完整发布 Dockerfile 构建通过。浏览器使用正式二进制和独立空库，七个页面导航、目标弹窗打开/关闭及告警配置渲染正常，观察期间控制台无 warning/error；该冒烟不代替全部业务场景验收。
- **容器收尾与证据边界：** 本地 `fwalizer-i8r02:local` 为 linux/amd64 镜像，在 arm64 Docker VM 中兼容执行；UID=1000、healthy，静态 health/operational health/首页及首页引用的三个 JS（含 `_common`）均 HTTP 200，`docker stop -t 15` 后退出码 0，临时容器已删除。浏览器空库实例亦已 SIGTERM 干净退出。研究阶段的 Vue 3.5.42/3.5.43 最小候选、原生 TypeScript 7 失败记录与正式门禁分开保存；正式证据目录为 `/Users/kyle/.codex/outputs/i8-r02-upgrade-84zekkbi/`。本轮未执行真实云 API、SMTP/收件箱/Webhook/Kuma、远端 CI/GHCR、原生 linux/amd64 主机或长期压力验收；Go 模块版本刷新不是 Go 漏洞审计通过证明。用户既有真机收口与豁免状态保持，SMTP 仍由 I8-R01/ProdTestList 跟踪，不把本地回归外推为外部发布或长期稳定通过。尚未提交/推送。

<a id="i8-r03"></a>

### I8-R03 ECS 删除分批判别力

- **状态与来源：** 2026-10-11 按用户裁决方案 B 及正式修复授权完成仓内测试补强，本地收口；TODO-002；P2-02。实施前 `main / 5412d45172c36c5b8530f56954429c3fb5242142`，本地 `origin/main / 5814725670e770841e1372b1e166d20dfbd84eba`，ahead 1，工作树/暂存区干净，未 fetch。
- **问题复核：** 生产分批已由 Issue7 修复，无需重复改造；原 Provider 测试已有 150=100+50 与第二批失败保留100的证据，缺口是完整 ID 序列、失败后停止尚未发送的后续批次，以及实际 SDK 到目标链的联合证明。原 `TestCleanup_ECSDeletesInBatchesOf100` 使用 fake Provider，证明150候选整体交接与S2统计，继续保留；其命名修订仍由 I8-R25 独立处理，不能当作实际 SDK 分批证据。
- **正式范围：** 仅 `provider/request_mock_test.go`、新增 `syncer/ecs_delete_batches_test.go` 与本文件，共三文件；生产源码、重试/同步状态机、SDK/依赖、API/schema、前端与发布配置零改动。测试辅助限定本项，不修改四平台共用 mock/helper 的既有语义，无新增生产测试 hook。
- **Provider 判别回归：** `TestRequest_ECSDeleteBatches100` 用独立预期表覆盖0/1/99/100/101/150/200/201/250/350条，逐批检查最大100、准确大小、倒序输入的完整ID列表/顺序与云目标参数，拒绝按内容删除降级；第二批失败用250条证明第三批不发，另补首批失败、350条第三批失败后第四批不发、250条短尾批失败，检查 `Deleted/Resolved`、`PartialDeleteError.Deleted` 和 `errors.As` 至原 `tea.SDKError`/InternalError。250条末项为空或空白定位时整体零请求/零确认。两包局部 RPC 解析辅助均以结构化负向控制拒绝跳号、重复参数、非法索引/空值及无索引形式，避免整理参数隐藏错误。
- **实际 SDK 目标链：** `TestTargetRound_ECSDeleteBatchesThroughSDK` 使用 ECS v7.11.5 → `provider.NewProvider` → `syncAll` → 正式目标链 → S2 → 真实目标/整轮事件与 RoundSummary，全部请求只访问本地 HTTP 夹具。S0/S1 的同功能规则刻意使用不同RuleID，删除必须取S1定位；成功请求按实际ID更新集合，S2由当前集合产生。250条全成功为100+100+50、确认250/残留0；第二批失败只两次请求、确认100/残留150；外部再移除7条为100/143；第二批已生效但返回失败为100/50，不把未获成功响应的批次计入确认删除。S2查询失败或期望覆盖丢失仍failed，保留确认100与估计残留150，检查 `basis=delete_progress`、`EstimatedTargets=1`、`Complete=false`；可信S2则检查观察完整、实际残留及目标分类。逐场景检查请求顺序、3次Describe、1次确认新增、期望与非当前TAG规则未提交删除、attempt=1，以及目标事件/整轮事件/状态汇总一致。失败已生效是本地故障模型，不是特定阿里云错误码的真实生效行为结论。
- **正式本地门禁：** macOS Go 1.27.1 / Node 26.7.0；以下正式符号定向 race 20轮与两包完整 race 一轮通过。原样 `make all` 通过（真实npm ci/vue-tsc/Vite前置、vet、全仓race入口及产品构建；部分未受影响包复用缓存），随后独立 `go test ./... -race -count=1 -timeout=10m` 禁用结果缓存，全仓12包均通过；受影响Go文件gofmt检查与 `git diff --check` 通过。npm ci报告0漏洞，不将其写成另行执行两条独立依赖审计。单轮全仓绿色不外推长期稳定。

  ```bash
  go test ./provider ./syncer -race -run '^(TestRequest_ECSDelete.*|TestTargetRound_ECSDelete.*)$' -count=20 -timeout=3m
  go test ./provider ./syncer -race -count=1 -timeout=10m
  ```

- **正式负向控制与证据边界：** 仓库外8个生产源码overlay，仅运行正式仓内测试符号，分别破坏后批ID、失败停止、失败批次计数、末项ID、S1定位、S2残留来源、估计观察属性及部分成功进度；全部由预期行为断言变红，无编译失败/panic/race/整体测试超时作为最终判别证据。正式证据目录 `/Users/kyle/.codex/outputs/i8-r03-implementation-20261011/`，与先前研究/准备overlay证据分开。本轮未执行Linux/Docker、真实云/SMTP/Webhook/Kuma、浏览器或远端CI/GHCR；保持用户既有真机收口与豁免状态，不新增真实云/发布/长期稳定通过结论，SMTP仍由I8-R01跟踪。尚未提交/推送。

<a id="i8-r04"></a>

### I8-R04 hook 测试观察竞争

- **状态与来源：** 测试补强本地完成（2026-10-11，用户定型方案 C）；旧 I8-17g。用户依据《研究 I8-R04 解决方案》的最终准备结论授权正式修复与文档同步；实施前 `main == 本地 origin/main == 829ef302b5f208961cf56197c951f46bcddcc5c1`，工作树/暂存区干净，未 fetch。正式改动仅 `syncer/state_applied_hook_test.go` 与本文件，生产代码、共享测试夹具、依赖、前端及 API/schema 均未修改。
- **原问题与修复：** 原 hook 先 `hooks.Add(1)`、后 `enabledAtHook.Store`，主测试只等计数再读状态，允许观察到尚未写完的状态；原子操作不把两次写入合并成一次观察。保留两项既有测试符号，改用 `testing/synctest` 驱动真实 Run/目标链，固定内存 DNS 与能反映新增结果的既有 Provider。单值计数用原子操作，多字段观察用互斥锁统一追加/复制，完成后再断言，消除该观察窗口。本轮未宣称自然复现原样测试的偶发失败；研究阶段扩大写入间隔的故障模型与正式证据分开保存。
- **正式回归合同：** 普通轮测试逐轮执行启动轮、6 个定时轮、3 个手动轮，hook 始终准确为 0；新状态测试从暂停启动，串行消费启用、启用且间隔不变的配置变化、暂停、恢复四份新状态，hook 准确为 4。同一已消费指针的重复通知及普通轮不增加回调。每轮检查实际 S0/S1 调用数、单目标成功汇总和无在途标记；hook 内核对本次指针、开关及已完成轮数，首次和再次恢复均须先完成本次恢复轮，不能以上一轮成功掩盖提前回调。通知仍允许合并，不扩成快速连续 ApplyState 的逐次回调保证。
- **完成与退出观察：** 保留正式目标冷却；有新轮时先有界等待 `EventSyncComplete`，再 `synctest.Wait`，不能把冷却中的稳定阻塞当作轮次完成。无新轮的状态变化在 Wait 后断言。复用现有有界 Stop/Wait，订阅始终取消，成功退出后检查固定次数及 `Running=false`；仓外故意 Fatal 的清理哨兵也确认 Run 退出。5 秒完成信号预算为虚拟时间局部断言，Go 命令另有整体 timeout，不称墙钟硬上限。
- **正式本地门禁：** macOS Go 1.27.1 darwin/arm64 / Node 26.7.0；以下两种 GOMAXPROCS 环境中，每项正式测试各重复 200 次 race 均通过，syncer 整包 race 一轮通过。原样 `make all` 通过（真实 npm ci/vue-tsc/Vite 前置、vet、全仓 race 入口与产品构建；部分未受影响包复用缓存），随后独立禁用结果缓存的全仓 12 包 race 一轮全部通过。受影响 Go 文件 gofmt 检查与 `git diff --check` 通过。npm ci 报告 0 漏洞，不记为另行执行两条独立依赖审计；单轮全仓绿色不外推长期稳定。

  ```bash
  GOMAXPROCS=1 go test ./syncer -race -run '^TestStateAppliedHook' -count=200 -timeout=10m
  GOMAXPROCS=4 go test ./syncer -race -run '^TestStateAppliedHook' -count=200 -timeout=10m
  go test ./syncer -race -count=1 -timeout=10m
  make all
  go test ./... -race -count=1 -timeout=10m
  ```

- **正式负向控制与证据边界：** 仓外 7 个生产源码 overlay 仅替换 `syncer.go`，运行正式仓内测试；漏回调、重复回调、普通轮重复回调、提前回调、错误状态指针、跳过手动轮、跳过定时轮各重复 20 次，全部由对应行为断言变红，无编译失败/panic/race/整体测试超时混入判别证据。跳过轮次由局部完成信号断言检出；故意 Fatal 清理探针预期退出 1，另有退出哨兵确认，不记为绿色测试。正式命令、返回码与日志位于 `/Users/kyle/.codex/outputs/i8-r04-implementation-20261011/`，不沿用研究/准备结果充当正式门禁。本轮未执行 Linux/Docker、浏览器、真实云/SMTP/Webhook/Kuma 或远端 CI/GHCR；保持用户既有真机收口与豁免状态，SMTP 仍由 I8-R01 独立跟踪。本轮尚未提交或推送。

<a id="i8-r05"></a>

### I8-R05 SWAS 资源扫描分页完整性

- **状态、授权与基线：** 2026-10-11 用户选择 B（严格总数、不自动重扫），深入研究后进一步确认保持 typed SDK 边界，随后授权正式修复并同步文档。正式修复及文档同步已完成，专项门禁、判别性对照与完整本地门禁均通过。实施前 `main == 本地 origin/main == 173660f9d7d9f3bd5a706a4ebcbfba0d8f985c28`，工作树与暂存区干净，未 fetch。旧来源为 I8-08 后续候选、Q-04 剩余；此前结构保护已实施，本轮只补扫描分页完整性，不修改规则 ListFirewallRules 的 I8-11 合同。
- **实际范围：** 唯一生产文件 `provider/scan.go` 的 `scanAliSWAS`；调整并补充 `provider/scan_swas_test.go`、`webui/api/scan_swas_test.go`，同步本文及 `AGENTS.md`，共五文件。复用原 `ListInstances`、客户端池、`decodeSWASScanPage`、handler/Store/前端失败处理；SDK/依赖/锁文件、超时、API/schema、规则快照、同步状态机及其他平台扫描保持。
- **正式合同：** 每页 SDK 解码后的 TotalCount 必须存在、非负；首页复制数值为本次基准，后续页必须一致。PageNumber 从 1 开始、PageSize 保持 100；返回页码/页大小若非 nil 必须匹配请求，缺失/null 允许，单页超过 100 条失败。原值 InstanceId 在本次调用内同页/跨页不得重复，名称不参与身份；累计唯一资源数超过总数失败，精确相等才成功，尚不足时空页或非空短页均失败。总数为零只接受显式空数组，结构缺失不豁免；所有完整性失败返回 `nil + ErrSnapshotIncomplete`，不带回部分资源、不覆盖旧缓存，不自动重扫。普通 SDK 业务/解码错误链及分类保持；HTTP 仍为 200 + success=false/error + no-store，失败无 resources/count；成功只覆盖对应 cloud_type+region。总数、页码和去重状态不跨调用，不按声明总数预分配资源数组/map，计数比较用 int64。
- **SDK 与完整性边界：** 用户接受现有 SDK 的类型转换；当前 SWAS v3.0.0 将 TotalCount 的原始 JSON `"1"`、`1.9`、`true` 转为整数 1，因此本合同针对 typed 值，不承诺原始 JSON 类型严格检查或重复字段拒绝。总数一致且 ID 唯一不能识别等量资源替换，不提供跨页原子快照；显式边界对照保留。资源扫描不继承单实例规则快照的 100 页限制，合法 101 页列表已由正式对照覆盖；I8-R05 当时未增加操作总预算，现由 I8-R06 独立合同固定120秒且不新增页数上限，不宣称硬墙钟上限。API 字段定义见[官方 ListInstances 文档](https://help.aliyun.com/zh/simple-application-server/developer-reference/api-swas-open-2020-06-01-listinstances)。
- **正式回归与夹具迁移：** 保留原生 nil/Body/集合/元素/ID 结构回归及名称可空/原值合同。后续页 SDK/结构异常用例首页改为 100 条、总数 101，确保真实进入第二页；原总数 100 的满页对照改为只请求一次，正常两页总数固定 101。新 SDK 回归覆盖首页/后续页总数缺失/null/负数、增长/下降、空/短页、超额、重复页/ID、分页回显、超长页、终止后不多发请求与扫描状态独立；正式 handler→临时 SQLite 覆盖首页/后续页失败保留旧缓存、成功与零结果替换、其他地域/其他产品缓存保持及失败无部分结果。typed 数值转换与等量替换属于边界回归，不称原始类型或原子快照保护。
- **正式专项与判别力：** `go test ./provider ./webui/api -run '^(TestDecodeSWASScanPage|TestScanSWAS)' -race -count=20 -timeout=3m` 通过。当前正式测试挂入 HEAD 旧扫描源码后，提前短页、重复 ID、错误页码及 API 缓存场景按行为断言变红；另外八类仓外生产源码 overlay（放行缺失总数、忽略变化、放行重复 ID、提前短页继续、忽略页码、忽略页大小、放行超长页、错误返回部分资源）各重复 20 次，均由预期行为断言检出，不以编译失败/panic/race/测试超时计负向通过。overlay 只替换生产文件，使用正式仓内测试；完整命令、返回码与日志位于 `/Users/kyle/.codex/outputs/i8-r05-implementation-20261011/`，研究/准备结果不充当本轮正式门禁。
- **完整门禁与外部边界：** `make all`（前端 npm ci、vue-tsc/Vite 构建、Go vet、全仓 race 与产品构建）通过；其中部分 Go 包命中缓存，不计为第二轮无缓存全仓证据。另行执行 `go test ./... -race -count=1 -timeout=5m`，12 包全部通过；`go build ./...`、受影响 Go 文件 gofmt 检查与 `git diff --check` 通过。npm ci 报告 0 漏洞，未另行执行独立 npm audit。单次无缓存全仓绿色不外推为长期稳定性结论。本机为 Go 1.27.1 darwin/arm64 / Node 26.7.0。本轮未执行 Linux/Docker、真实云、产品浏览器、SMTP/Webhook/Kuma 或远端 CI/GHCR；保持既有用户真机收口与人工豁免，不恢复旧 PT-AUDIT-05，SMTP 继续由 I8-R01 跟踪。源码、测试及本轮文档尚未提交或推送。

<a id="i8-r06"></a>

### I8-R06 ECS 与资源扫描操作总预算

- **状态、授权与基线：** 2026-10-12 用户选择并定型 B（操作 deadline、中断在途网络请求；120 秒；同步沿用现有超时重试；扫描不自动重扫；不新增页数上限），进一步研究与仓外准备后授权正式修复及同步文档。正式实施和专项门禁已完成，完整门禁结果见下文。实施前 `main == 本地 origin/main == fe5b6f1317b68bf4e7019bc7c7a1821e295c1671`，工作树与暂存区干净，未 fetch。旧来源为 I8-38、Q-12 ECS 剩余；Lighthouse 规则快照的既有 120 秒/100 次合同保持。
- **实际范围：** 生产仅 `provider/read_budget.go`（新增）、`provider/ali_ecs.go` 与 `provider/scan.go`；迁移 `provider/ali_timeout_test.go` 两个扫描夹具到公共入口，新增默认 `provider/read_budget_test.go` 及 `testdata/i8-r06/` 的两份集成夹具和专项入口；同步本文与 `AGENTS.md`，共十文件。SDK/依赖/锁文件、API/schema、前端源码、规则分页结构/终止条件、正式同步状态机、`isRetryable/maxRetries` 与扫描 handler/Store 保持；未修改 Make/CI/Docker 接线。
- **预算与错误合同：** ECS 每次 `GetSnapshot`、每次单产品单地域 `ScanResources` 固定独立 120 秒，全部分页共享、不逐页重置；S0/S1/S2 与整目标重试后的读取分别计时。不新增页数/资源条数限制，合法超过 100 页扫描继续支持。腾讯扫描使用原生 WithContext，阿里云 HTTP 请求绑定同一操作 context；页前、SDK 返回后及成功返回前检查。耗尽返回空快照或 nil 资源，同时保留 `ErrSnapshotIncomplete`、`context.DeadlineExceeded` 与当时 SDK 错误链（若有）；操作 context 仍有效时，原单页超时不新加不完整标记，普通业务/网络/解码错误分类保持。阿里云单页 Connect 10s / Read 30s、HTTP 整体 40s 与腾讯默认 60s 保持，测试仍可沿已有接缝缩短。120 秒约束网络等待与正文读取，不强制终止签名、解码、计算、池锁等待或清理，不含数据库持久化、整目标或整轮，不承诺严格墙钟返回上限。
- **客户端隔离与回收：** 池提供捕获快照的基础 SDK 客户端，操作通过官方构造器读取其不可变配置/凭据创建独立实例，不复制或修改整个 Client，不读取可变 Headers。准备阶段的浅拷贝与 `GetRpcHeaders` 并发竞态已撤销，未进入正式源码；正式并发回归保护这一边界。读取不进入 dara 默认全局 HTTP 池、不动态改变每页 Timeout；保留签名、TLS/CA 与代理配置，操作内复用第一份 SDK Transport、操作间独立。SDK 正常关闭与操作收尾共用一次关闭器，覆盖正文读取失败或空正文遗漏关闭；下一页前清理前页残留，结束先 cancel，再关闭当前响应体与空闲连接。关闭错误统一处理，仅固定 `response_close` 阶段及错误类型 WARN，不记录原始错误/URL/正文/凭据，不覆盖主错误或完整读取结果，不增加清理重试。操作间可能新增连接/TLS握手开销；当前 SDK 未开启内部重试，未来更改 SDK、可变性或退避配置须重验取消/隔离，当前证据不外推未知版本。
- **安全门与重试后果：** 预算耗尽沿现有最多三次整目标 attempt；S0 持续耗尽时三次读取预算约六分钟加退避，不是整目标上限。S0 失败本 attempt 零增删；S1 失败保留已确认新增且零删除；S2 失败保持既有 failed 与保守观察合同，已确认增删不丢。后续可信 attempt 可恢复、重新规划并清理，不沿用失败快照授权删除。扫描不自动重扫，沿既有 HTTP 200 + success=false/error + no-store，失败无 resources/count、不覆盖旧缓存；成功/合法零结果仍只覆盖对应产品地域。不接入 HTTP 断开或 Stop 的新取消源，既有完成当前轮规则、限流/冷却及快照捕获边界保持。
- **正式回归与专项入口：** 默认 provider 回归覆盖五条路径首页/后页响应头与正文挂起、持续新 token 共用预算、已耗尽零 HTTP、正常成功、原单页超时分类、响应体遗漏/重复关闭与安全日志、真实 SDK 空正文/坏 JSON 回收、原池请求并发、操作隔离/连接复用回收、TLS自定义CA与HTTP代理。既有完整性、token原值、计数与101页扫描对照保持。跨包夹具存放于 `testdata/i8-r06/`，临时 Go overlay 注入实际测试包并仅将测试构建预算改为150ms，不修改源文件/产品设置；默认 Go 测试不加载该两份夹具，不据默认绿色声称已执行专项。真实 SDK→当前正式目标链覆盖 S0/S1/S2 连续耗尽、首次失败后恢复和正常清理；S2 连续耗尽夹具模拟每次成功删除后另一个写入方产生新的旧 TAG 规则，确认三次删除计数保留且最终 failed。四平台真实 SDK→handler→临时 SQLite 分别覆盖首页/后页耗尽、旧缓存及其他地域保持、响应无半截结果。以下两条专项每项 race 重复20次均通过：

  ```bash
  go test ./provider -run '^TestReadBudget' -race -count=20 -timeout=3m
  python3 testdata/i8-r06/run_integration.py --count 20
  ```

- **正式判别力：** 四个仓外生产帮助函数 overlay（取消 HTTP context 绑定、漏不完整标记、漏响应体收尾、移除一次关闭保护）使用当前正式仓内测试各重复20次，全部按取消时限、错误分类或关闭次数局部断言变红，无编译失败/panic/race/整体测试超时混入负向通过。正式命令、返回码与日志位于 `/Users/kyle/.codex/outputs/i8-r06-implementation-20261012/`，不将研究/准备或中间版本结果当作最终正式门禁。
- **完整门禁与外部边界：** 最终原样 `make all`（真实 npm ci/vue-tsc/Vite、Go vet、全仓 race、产品构建）通过，其中部分 Go 包复用缓存；另行执行 `go test ./... -race -count=1 -timeout=5m`，12 包禁缓存全仓 race 一轮全部通过；`go build ./...`、受影响 Go 文件 gofmt 检查与 `git diff --check` 通过。本机 Go1.27.1 darwin/arm64 / Node26.7.0，npm ci报告0漏洞，不记为另行执行独立 npm audit；单次全仓绿色不外推为长期稳定性结论。本轮未执行 Linux/Docker、真实云、产品浏览器、SMTP/Webhook/Kuma 或远端 CI/GHCR；保持既有用户真机收口与人工豁免，不重开旧人工清单，SMTP继续由 I8-R01 跟踪。正式源码、测试及本轮文档尚未提交或推送。

<a id="i8-r07"></a>

### I8-R07 Pause/Resume 接口的原子更新

- **状态与来源：** 本地已修复（2026-10-12，用户定型方案 A 并授权实施与文档同步）；旧 I8-17d/I8-41；Q-10。实施前 `main / 278a70383e430103b04985a9e4f20a12635e453b`，本地 `origin/main / fe5b6f1317b68bf4e7019bc7c7a1821e295c1671`，ahead 1；工作树/暂存区干净，未 fetch。
- **问题与适用边界：** 原 `setSyncEnabled` 在读取旧快照后复制并调用 `ApplyState`，完整读改写不在同一锁内，能够把其间发布的新 TAG、间隔、规则和运行时依赖退回旧状态；本轮由正式屏障回归重演，不再仅引用历史探针。具体 `Syncer.Pause()/Resume()` 仍无生产调用者，产品端点已有 SQLite 配置协调器串行提交/发布，不将此问题记为已发生的线上配置丢失。
- **正式合同：** 保留具体方法，`setSyncEnabled` 的读取、nil/同值判断、Config 深复制、开关修改和发布全程持有 `s.mu`；与 `ApplyState` 共用不自行加锁的 `applyStateLocked`，统一维护状态指针、enabled 镜像与真实 false→true 的 pendingResume，解锁后投递可合并通知。空状态/同值控制不发布、不通知、不增加恢复边沿；普通完整发布与暂停不提前清除待消费恢复，仍由 `consumeControlState` 同锁消费。沿用 `s.mu → RuntimeManager.mu`，不持锁调用自锁的 `ApplyState`；保留 Pool/Providers/Resolver/Breaker 引用，不重建依赖或修改旧快照。生产锁内无日志、状态消费回调、网络或同步轮次。
- **控制与调度语义：** 完整发布先完成时后续控制仅改变其开关；控制先完成时后续完整状态及其中的开关生效，不增加“运行时暂停永久优先”的覆盖层。具体方法仍不持久化，产品端点继续经协调器；不等待或取消在途轮次，已捕获旧完整快照的轮次继续完成。恢复合并、最终暂停、最新 interval、排队 trigger 与 Stop 门控保持。
- **正式回归：** 新增 `syncer/sync_enabled_test.go`，四种 Pause/Resume × 完整状态开关的读窗口组合检查整个配置、依赖身份、旧状态不可变和两个操作的有界完成；四种串行顺序组合保护开关优先级。另覆盖空状态、同值、深复制、镜像、通知、恢复边沿与最终暂停消费，以及真实 Run/目标链中恢复后普通保存、多恢复合并、最终暂停和 Stop。实例私有 `afterSyncEnabledReadHook` 默认 nil，无导出 setter，仅测试在并发开始前设置；调用时持锁、禁止重入。读窗口使用有界真实 channel，TryLock 只安排交错，最终按配置行为判定；不以 `synctest.Wait` 等待互斥锁阻塞。新增 `webui/api/sync_enabled_test.go` 使用真实临时 SQLite→真实协调器→真实 Syncer，证明 pause/resume 成功响应后配置与数据库一致、恰好一次发布，以及事务失败不改状态/不发布；该夹具不启动 Run，也不把它与读窗口测试合称为“真实 SQLite 提交的交错验证”。
- **正式专项与判别力：** `go test ./syncer ./webui/api -race -run '^TestI8R07' -count=20 -timeout=3m` 通过。仅替换生产源码的仓外 overlay 使用最终正式仓内测试：恢复 HEAD 旧算法并保留相同测试接缝后，四种读窗口各重复20次，共80次按配置回滚断言变红；另外六类错误变体（遗漏恢复标记、遗漏镜像、浅复制、同值仍发布、遗漏通知、普通保存清除待消费恢复）各重复20次，全部按预期行为断言变红，没有编译失败/panic/race/整体或局部屏障超时混入负向通过。命令、返回码、最终源码/测试哈希和日志位于 `/Users/kyle/.codex/outputs/i8-r07-implementation-20261012/`，不复用研究/准备结果作为正式证据。
- **完整门禁与范围：** 受影响两包完整 `-race -count=1 -timeout=5m` 通过；原样 `make all`（真实 npm ci/vue-tsc/Vite、全仓 vet/race、产品构建）通过，其中部分 Go 包复用缓存；追加 `go test ./... -race -count=1 -timeout=5m`，12包禁缓存全仓 race 一轮全部通过。`go build ./...`、三份受影响 Go 文件 gofmt 检查及 `git diff --check` 通过。唯一可执行生产文件为 `syncer/syncer.go`，加两份正式测试与 AGENTS/Issue8 共五文件；不改 RuntimeManager、协调器、API 接口、schema、Provider、DNS、前端或依赖，不改归档历史文档。
- **外部与提交边界：** 本机 Go1.27.1 darwin/arm64 / Node26.7.0；npm ci报告0漏洞，不记为另行执行独立 npm audit。本轮未执行 Linux/Docker、产品浏览器、真实云、SMTP/Webhook/Kuma 或远端 CI/GHCR，单次全仓绿色不外推长期稳定或外部通过。保持既有用户真机收口与人工豁免，不重开旧人工清单，SMTP继续由 I8-R01 跟踪。正式源码、测试及本轮文档尚未提交或推送。

<a id="i8-r08"></a>

### I8-R08 CVM Describe 错误分类合同

- **状态与来源：** 合理分类待明确；旧 I8-31；Q-11；TODO-003；P3-06。
- **证据与边界：** SDK调用失败保留错误链，成功响应结构缺失返回ErrSnapshotIncomplete，均停止新增。现状区分有合理性，旧“必须统一包装”的缺陷定性不沿用。
- **后续与完成条件：** 决定保留现状并明确合同，或经 errors.Is/云错误码/整目标重试判别后调整；不得扩大生产isRetryable或破坏配额失败零新增。

<a id="i8-r09"></a>

### I8-R09 dangling 目标引用

- **状态与来源：** 历史候选待复核；Issue6 §6.5 第4项。
- **证据与边界：** 历史观察为外部直接删除targets后，规则引用可能只作用于零目标；正常API与导入已有引用校验。本次不把绕过API的数据库修改等同正常用户路径缺陷。
- **后续与完成条件：** 追踪启动加载、普通发布、目标删除和导入的当前引用校验；决定损坏库fail-closed或明确支持边界，补零写入/零发布及恢复证据。

<a id="i8-r10"></a>

### I8-R10 日志持久化回调并发与背压

- **状态与来源：** 容量候选待定型；Issue6 §6.5 第5项。
- **证据与边界：** EventBus.Publish 对接口订阅者派生goroutine；StoreLogWriter同步写SQLite，慢库可能积压回调。现有发送在途限制不限制日志持久化，本次确认调用机制，不宣称已复现生产堆积。
- **后续与完成条件：** 先定义队列容量、丢弃/背压、退出等待、日志顺序和失败恢复要求；独立于邮件/Webhook在途策略，不能顺手改EventBus全局语义。

<a id="i8-r11"></a>

### I8-R11 同步事件 SSE 续传与心跳

- **状态与来源：** 契约候选待裁决；Issue6 §6.5 第6项。
- **证据与边界：** 同步事件channel满载可丢，未承诺id/Last-Event-ID回放；已完成的P3-04是日志SSE续传。历史还记录两类SSE无心跳。
- **后续与完成条件：** 先决定同步流是否要求回放、断线窗口与心跳，再考虑两种流的独立deadline和慢订阅者边界；日志已有续传不能作为同步流无损证明。

<a id="i8-r12"></a>

### I8-R12 阿里云错误码重试范围

- **状态与来源：** 行为候选待裁决；Issue6 §6.5 第8项。
- **证据与边界：** isRetryable保留网络/超时与既有字符串分类；历史Throttling/ServiceUnavailable不在显式列表。未重新复现真实供应商错误形态，不能仅凭名称泛化重试。
- **后续与完成条件：** 验证当前SDK错误链和已存在/不存在优先级，逐错误码决定是否重试；保留权限/配额/不完整快照失败闭合，测试实际目标重规划次数。

<a id="i8-r13"></a>

### I8-R13 运行时间戳重启展示

- **状态与来源：** 产品边界待确认；Issue6 §6.5 第11项。
- **证据与边界：** last_sync/last_success为内存状态，重启可为空；当前Dashboard用“暂无”，原“从未成功”担忧不能直接登记为现存文案缺陷。
- **后续与完成条件：** 复核全部消费页面并决定保持本进程语义或持久化；若无需持久化，可明确接受边界后关闭，不因时间戳为空改变运行健康。

<a id="i8-r14"></a>

### I8-R14 SQLite 容量与辅助事务边界

- **状态与来源：** 研究候选待复核；Issue6 §6.5 第12项。
- **证据与边界：** 历史队列包括无连接池上限、AddSyncLog COUNT全表计数/与写入非同事务、WithTransaction使用无context Begin、未启用foreign_keys。已完成_txlock=immediate、COUNT错误处理和导出只读合同不关闭这些不同方向。
- **后续与完成条件：** 分别评估连接容量、裁剪一致性、辅助入口取消和外键是否符合schema；先证明当前调用者/性能或损坏后果，再定每个子项，不擅自加PRAGMA、重试或事务锁策略。

<a id="i8-r15"></a>

### I8-R15 Server.Addr 关闭后语义

- **状态与来源：** 低收益接口候选；Issue6 §6.5 第14项剩余。
- **证据与边界：** Addr文案只规定Start成功前为空；Shutdown后保留listener地址的历史观察未裁决。Syncer.Wait未启动永久阻塞已经修复，不重开。
- **后续与完成条件：** 复核当前调用者是否依赖关闭后地址，选择明确语义或清理；若改动，用启动/关闭生命周期判别，不扩为HTTP重构。

<a id="i8-r16"></a>

### I8-R16 其他清理符号清单

- **状态与来源：** 维护候选未裁决；旧 I8-42；Q-13剩余。
- **证据与边界：** 旧I8-14身份链与I8-15注释/死赋值已完成；其他候选缺当前完整符号清单，历史8/5/2/14等数量不能证明安全。
- **后续与完成条件：** 先列具体符号、生产/测试消费者及保留合同，再决定是否清理。合理前置条件不是缺陷；不得删除安全字段、辅助验收夹具或为整洁改变行为。

<a id="i8-r17"></a>

### I8-R17 日志渲染失败仓内长期回归

- **状态与来源：** 证据缺口；旧 I8-22；P3-10出口④。
- **证据与边界：** 生产使用bytes.Buffer，正常写入不产生该错误；历史TestRenderFailureNoPublish来自仓外writer overlay。仓内未找到同名不能否认历史证据，本次未重演。
- **后续与完成条件：** 若补长期守护，验证原错误返回且序号、ring、投递不变；不扩大生产故障面，不宣称slog会自动报告Handler错误。

<a id="i8-r18"></a>

### I8-R18 A20 修复前负向控制

- **状态与来源：** 历史证据未重演；旧 I8-23。
- **证据与边界：** 历史修复和当前Stop回归保留；旧复核尚未用b38678a之前实现重演修复前红灯。真机确认不补齐该自动化判别记录。
- **后续与完成条件：** 若要补证明，用固定旧源码overlay准确触发行为断言；编译失败、超时不能记为判别成功，不改生产Stop合同。

<a id="i8-r19"></a>

### I8-R19 组件退出与资源泄漏证明

- **状态与来源：** 测试覆盖候选；旧 Issue8 §3 goroutine/fd/ticker。
- **证据与边界：** 已有TestShutdownNoGoroutineLeak和重复Shutdown回归；撤销“全仓零泄漏断言”。Syncer/Push/Supervisor/FD/ticker尚无全面确定性退出证明。
- **后续与完成条件：** 按组件评估需要补的真实退出观察、任务完成信号和句柄回收；单次goroutine数量、固定sleep或有限时间内没出现现象不等于全面无泄漏。

<a id="i8-r20"></a>

### I8-R20 启动宽限10秒数值锚点

- **状态与来源：** 低成本证据缺口；旧 Issue8 §3 DefaultStartupGrace。
- **证据与边界：** DefaultStartupGrace为10秒合同；现有测试引用常量作相对时间计算，历史10s改12s仍绿记录未在本次重演。其他告警/Push默认常量已有直接或间接锚点。
- **后续与完成条件：** 补独立字面数值与真实边界行为断言；不能仅把同一常量同时用于输入和预期。

<a id="i8-r21"></a>

### I8-R21 前端长期行为覆盖与异步门禁

- **状态与来源：** 补强候选待复核；TODOLIST P2-06/08/09/P3-16；旧 Issue8 §3/ I8-102。
- **证据与边界：** 后续已有Alerts、Dashboard观察、Dry Run请求阶段等正式回归，旧“完全无组件测试”不再沿用；仍需逐场景检查模板disabled、outcome完整矩阵、target_id稳定key、44px尺寸和卸载晚到覆盖。test:alerts逐项30秒已修，总命令预算/其他入口/CI接线未统一。
- **后续与完成条件：** 先建立当前用例与行为映射，再决定补缺；优先有判别力的请求/模板/虚拟时间观察，不机械替换全部sleep或新增镜像实现的测试。

<a id="i8-r22"></a>

### I8-R22 当前 revision 远端发布证据

- **状态与来源：** 独立证据待完成；PT-B7-08；Issue5 O5-02历史边界；旧 I8-25部分。
- **证据与边界：** v2.0.0旧Actions/GHCR成功不证明当前HEAD。最近本地全量race/make门禁已有补记，旧“本轮仅11包、dns未跑”不得用作现状；本次没有fetch或触发CI。
- **后续与完成条件：** 发布时记录确切SHA、真实Actions结论、镜像tag/digest与对应部署结果；自动SDK更新策略与固定工具链分别记录。不因用户真机确认推定远端CI通过。

<a id="i8-r23"></a>

### I8-R23 特定平台与长期运行证据

- **状态与来源：** 证据范围待核对；历史audit/Issue8外部边界。
- **证据与边界：** 用户已确认当前真机验收完成；既有容器arm64兼容执行不等于原生linux/amd64，macOS13与长期稳定性须有对应环境记录。本次未取得逐平台测试记录。
- **后续与完成条件：** 仅在需要宣称特定平台/长期稳定时核对或补其证据，不重新列为用户已收口的普通云/浏览器人工阻断项；保留压力与真实故障环境的结论范围。

<a id="i8-r24"></a>

### I8-R24 合理前置条件与防御接口

- **状态与来源：** 非缺陷维护候选；旧 I8-17e/I8-47/I8-44-1/2/3。
- **证据与边界：** dnsRound只接收本轮适用域名；Store启动接线保证非nil；mustDuration/mustThreshold前有normalize；Health生产传Policy；固定embed路径fs.Sub不是缺index的根因。旧产品缺陷判断撤销。
- **后续与完成条件：** 只有扩大可空/任意输入合同或允许独立调用时再定防御处理。不能因理论输入加改动就宣称修复现存产品故障；选择保持边界后可关闭。

<a id="i8-r25"></a>

### I8-R25 历史统计与测试命名精度

- **状态与来源：** 文档/测试注释候选；TODO-004/007；旧 Issue8附录B。
- **证据与边界：** 基础28项与附加标签口径、顶层测试/子用例、tracked/ignored数量、旧行号需按采集基线理解；cleanup_test.go的CandidatesHandedOver注释与DeletesInBatchesOf100函数名不一致仍保留。本次仅文档整理，未改Go注释。
- **后续与完成条件：** 活跃台账不沿用旧数量作当前规模；未来引用归档证据按符号/固定SHA复核。测试命名/注释修订独立处理，不能修改过去真实次数或重算未运行探针。

## 3. Issue6 历史14项去向对账

本表关闭的是重复登记或完成事项；转交的候选保持未处理。旧具体证据见 [Issue6 §6.5](./HistoryDocs/Issue6.md#65-历史复核发现但未纳入当时合同的后续候选2026-09-27)。

| 原项 | 当前去向 |
|---|---|
| 1 删除侧跳过仍计数 | 旧retry链已删除，当前Provider定位校验/实际确认计数与R7保护保留；不重开旧实现缺陷。ECS额外判别力已由I8-R03本地补强。 |
| 2 写了却报0 | 已由043ac36及后续目标级链收口，归档。 |
| 3 buildDesired静默丢弃 | 旧链已由I8-14删除；正式planner有unsupported/partial/清理冻结，归档。 |
| 4 dangling引用 | I8-R09，历史候选先复核。 |
| 5 日志回调积压 | I8-R10，独立容量候选。 |
| 6 SSE续传/心跳 | I8-R11；日志续传P3-04已完成，同步流另定合同。 |
| 7 Dashboard数字双口径 | 已由043ac36及后续Dashboard观察修复收口，归档。 |
| 8 阿里云重试码 | I8-R12，待验证形态并裁决。 |
| 9 Webhook Close/drain | 已按P3-21接受及时关闭/有界清理边界，Push正文上限也已实施；不作为待修项。真实Webhook/Kuma豁免不阻断。 |
| 10 SMTP收件人空格 | 后续共用邮件出口已逐项Trim；旧缺陷归档。真实投递仍由I8-R01跟踪，不能因归档宣称SMTP通过。 |
| 11 内存时间戳 | I8-R13，当前“暂无”不等于“从未成功”。 |
| 12 SQLite其余方向 | I8-R14；已修事务锁/错误处理不等于这些容量方向完成。 |
| 13 skipped日志语义 | 当前目标级success/partial/failed与unsupported详情已固定，旧逐域skipped观察归档。 |
| 14 生命周期边角 | Syncer未启动Wait已修；Server.Addr剩余转I8-R15。 |

## 4. 已完成与旧编号对照

已完成实现、原始故障证据、逐批门禁和裁决保存在 [Issue8历史快照](./HistoryDocs/Issue8-history.md)、[TODO历史快照](./HistoryDocs/TODOLIST-history.md) 与 [审计报告](./HistoryDocs/fwalizer-audit-final1.md)。不删除历史红灯/负向控制或将其改写为本次运行结果。

| 旧编号/范围 | 去向 |
|---|---|
| Issue5 R5/O5/A5 | 阶段已完成，见 [归档Issue5](./HistoryDocs/Issue5.md)。SMTP真实验收I8-R01、当前revision发布证据I8-R22独立保留。 |
| Issue6 A1～A20、批次1～8及已实施后续修复 | 阶段完成并归档；14项候选去向见§3。 |
| Issue7 Step0～5、F1/F5、R7-01～07、后续已实施修复 | 阶段完成，合同保留于 [归档Issue7](./HistoryDocs/Issue7.md)；人工真机按用户确认收口。 |
| 旧I8-01/02/18/111/113/12/03/04/102/09/08/10/11/17b/05/06/17f/43/14/15/16 | 已实施收口，原实现与证据在历史快照；I8-08扫描完整性与I8-102其他门禁方向分别转I8-R05/R21。 |
| 旧Q-01～09、Q-14～21 | 已裁决并有对应实施，不再作为活跃决策表。 |
| 旧Q-10、Q-11、Q-12 ECS、Q-13剩余 | 分别I8-R07、R08、R06、R16；Lighthouse预算和两批清理已完成，不重开。 |
| TODO-001/008 | 已由旧I8-01/02修复，归档。 |
| TODO-002/003 | I8-R03已本地补强；I8-R08继续独立研究。 |
| TODO-004/007剩余 | I8-R25；旧规模/行号按历史解释，不算生产缺陷。 |
| TODO-005/006已完成范围 | 当前planner路径与历史提交说明已整理，归档；剩余引用精度归I8-R25。 |
| 旧I8-17g、I8-22/23/25 | I8-R04、R17、R18；I8-25的全量本地门禁历史缺口已被后续门禁替代，发布/长期范围分别R22/R23。 |
| 旧I8-17e/47/44-1/2/3 | I8-R24，合理前置或撤销缺陷判断；不作为强制修复。 |
| 旧专项I8-50～60与附录撤销项 | 历史专项已执行/误判已撤销，不再登记待派发。 |

## 5. 文档归档与跟进规则

- [Issue5](./HistoryDocs/Issue5.md)、[Issue6](./HistoryDocs/Issue6.md)、[Issue7](./HistoryDocs/Issue7.md)按阶段完成归档；[audit](./HistoryDocs/fwalizer-audit-final1.md)按“报告完成、剩余事项转交”归档，不表述为全部发现均已修复。
- [AGENTS](./AGENTS.md)仍是唯一强要求；归档Issue7继续供查询已实施合同。旧Q编号在AGENTS中是已固定裁决来源，当前待办使用本文件新编号。
- [TODOLIST](./TODOLIST.md)只保留入口，不复制第二份状态；[ProdTestList](./ProdTestList.md)只记录SMTP人工操作。
- 不重新登记用户已完成的普通云/浏览器验收；特殊平台、压力/故障注入、远端发布证据须按对应声明范围记录。Webhook/Kuma人工豁免不因研究候选而撤销。
- 本轮没有实施候选、修改源码/测试/依赖、运行门禁或触发发布。后续逐项记录决定、授权、差异、判别性验证与完成日期。
