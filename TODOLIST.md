# FWAlizer TODO List

> 本文件记录 2026-10-03 对“FWAlizer 修复真实性只读审阅（28 编号逐项收口）”的子代理核验结果及后续跟进事项。
>
> 本文件只记录问题、证据、边界和建议动作。本次创建文件没有修改生产代码、测试、既有文档、配置或依赖；没有运行测试、构建、vet、npm、Docker，也没有 fetch、提交或推送。

## 一、核验基线

- 分支：`main`
- HEAD：`396885f362faa37c84a5e5ca633bbb74d9c6fbdd`
- 本地跟踪引用：`origin/main = 5e1d79c2b3378f31079e3240c75777b174c30781`
- 相对本地跟踪引用：ahead 4、behind 0；未执行 fetch，不能据此推断远端服务器当前状态。
- 工作树：创建本文件前干净；此前无暂存、无未跟踪文件。
- 忽略产物：`fwalizer`、`webui/frontend/dist/`、`webui/frontend/node_modules/` 共 3 项。
- 报告列出的 27 个历史修复提交均存在且为当前 HEAD 祖先；当前 HEAD `396885f` 是 P3-16 后续提交，不在报告列出的 27 个提交短 SHA 台账中。
- 本轮使用 5 个只读子代理分片核验：基线台账、规则/启动、产品语义/通知、事务/错误/权限/工具链、Provider/前端交互。

> **P3-18 后续补记（2026-10-03）：** 上述 `396885f` 是本文件创建时的核验快照，原核验结论保留。本轮实施前为 `5860cef`、本地 `origin/main / 5e1d79c`、ahead 5，工作树干净；P3-17 已由 `5860cef` 提交。用户随后授权 P3-18 推荐 B 十文件文档/注释修复；本轮没有重跑原核验涉及的业务测试或构建，也不修改 TODO-001～003/008 的业务与测试边界。

## 二、状态定义

- **已确认（源码层面）**：当前代码存在报告描述的生产路径和关键分支；不等于本轮重新测试通过。
- **需补充**：主线基本成立，但存在遗漏、输出语义问题、契约措辞差异或测试判别力不足。
- **部分修复**：同一编号的部分子项已实现，另一些边界仍未实现。
- **未修复/跳过**：报告明确未声明修复，或当前代码仍保留问题。
- **外部未验收**：真实云、真实通知、真实浏览器、远端 CI/GHCR、特定平台真机等均不能由本文件升级状态。

## 三、最高优先级待办

### TODO-001：修复 unsupported-only 计划的 `coverage_ready` 误报

- **关联编号**：P1-01、P2-03。
- **优先级**：中低（2026-10-08 文档订正；当前独立 unsupported 清理安全门冻结删除，无误删后果）。
- **当前状态**：核心安全门已存在，但输出字段存在可达语义错误，不能保持“无保留成立”。
- **问题**：`provider/plan.go:655` 当前主要依据 `len(plan.Desired) > 0 && !in.AddStateUnknown && len(plan.ToAdd) == 0` 判定 `CoverageReady`，没有确认存在可实施的 Desired。
- **触发场景**：目标只配置 SWAS IPv6、SWAS DROP 或其他不支持能力的规则时：
  - `Desired` 非空；
  - 该项 `Implementable=false`；
  - `ToAdd` 为空；
  - `CoverageReady` 可能被错误设置为 `true`。
- **契约依据**：`Issue7.md:321`、`:325` 要求没有任何可实施期望时 `CoverageReady=false`。
- **安全影响**：当前 unsupported 仍会生成结构化原因码，且清理冻结仍由 `provider/plan.go:699-727` 保护，正式目标仍可判为 `partial`；主要影响是 Dry Run/API 对覆盖状态的误导。
- **建议动作**：修正 `CoverageReady` 判定，候选为 `implementable > 0 && covered == implementable && !AddStateUnknown`，使其依赖非空可实施期望和覆盖结果；不要放宽 unsupported 清理安全门。
- **验收要求**：覆盖空 Desired，补充全量 unsupported → `coverage_ready=false` 的正向/负向判别测试，并覆盖“unsupported + 一个可实施项”的混合场景。
- **外部边界**：修改后仍需将真实四云和浏览器验收独立登记，不能仅凭本地 planner 测试宣称外部通过。

### TODO-002：补齐 P2-02 ECS 删除分批测试判别力

- **关联编号**：P2-02。
- **优先级**：P1 / 高。
- **当前状态**：生产代码按 100 条分批、部分成功计数和 S2 残留核验成立；测试证据仍有明确缺口。
- **证据**：`provider/ali_ecs.go:193-225`；`provider/request_mock_test.go:1058-1122`；`syncer/cleanup_test.go:501`。
- **缺口**：
  1. mock 没有强制请求超过 100 条即失败的负向约束；
  2. 150 条只有两批，无法证明第三批之前失败后不会继续；
  3. 没有断言两批 RuleID 拼接后与原输入的完整集合/顺序一致；
  4. 没有真实 ECS Provider 分批请求贯通到 syncer S2 的删除计数测试。
- **建议动作**：增加三批以上输入、请求上限断言、完整 RuleID 集合断言，并保留第二批失败后的部分进度语义。
- **验收要求**：证明批次不超过 100、失败停止条件正确、已成功批次计数保留、S2 残留统计不虚增。

### TODO-003：统一 P3-06 的 CVM Describe 错误分类

- **关联编号**：P3-06。
- **优先级**：P1 / 中高。
- **当前状态**：数量保护行为成立，但错误契约存在低严重度不一致。
- **问题**：`provider/tc_cvm.go:248-251` 中 `DescribeSecurityGroupPolicies` 调用本身失败时返回 `查询规则数量失败: %w`，没有包装 `ErrSnapshotIncomplete`。
- **对照**：响应、`SecurityGroupPolicySet` 或集合缺失路径会返回 `ErrSnapshotIncomplete`；现有 `provider/request_mock_test.go:816-830` 还要求保留云错误原文。
- **待决策**：选择统一包装为 `ErrSnapshotIncomplete`，或明确把“Describe 调用失败保留云错误、结构缺失返回 ErrSnapshotIncomplete”写入当前契约。
- **验收要求**：补充 `errors.Is` 语义测试，同时保留云错误码/原文诊断价值；不能让新增规则在配额不可信时继续发送。

## 四、编号逐项 TODO 与核验结论

### P0-01：Aliyun 端口表达式收敛

- **状态**：已确认，需修正文档证据路径。
- **当前生产路径**：`syncer.Run → syncAll → runRound → syncTarget → runTargetAttempt/runTargetCleanup → provider.PlanTarget`。
- **当前收敛点**：`provider/plan.go:239-254` 的端口展开与 `:300-316` 的 `canonicalPortExpression`；回读侧为 `provider/ali_swas.go:234-243`、`provider/ali_ecs.go:250-261`。
- **问题**：报告把修复点指向 `normalizePortForCompare`/旧 `Diff`，但 `provider.Diff` 已不在生产调用链。旧回归 `provider/common_test.go:418-455` 只能证明废弃路径行为；当前生产 planner 回归在 `provider/plan_test.go:172-204`。
- **文档动作**：将旧 `Diff` 测试标为废弃路径正向控制，明确当前修复点是 planner canonical 化；不要删除旧测试前误认为当前生产链无覆盖。
- **边界**：真实 Aliyun SWAS/ECS 回读形态仍未验收。

### P1-01：TAG 所有权与目标级同步

- **状态**：核心安全主线已确认；因 `coverage_ready` 误报需补充，不能写成无保留成立。
- **已确认**：严格 TAG 命名空间 `internal/tag/tag.go:22-25`；canonical `FunctionalKey`；S0 → Add → S1 → 覆盖验证 → 安全门 → Delete → 必要时 S2 的目标级状态机。
- **待办**：完成 TODO-001；补充 unsupported-only 和混合可实施/不可实施目标的 `CoverageReady` 判别测试。
- **边界**：真实四云、真实浏览器、当前 revision 远端 CI/GHCR 未验收。

### P1-02：PID 文件互斥

- **状态**：已确认。
- **证据**：`config/pidfile.go:17-46` 先取得锁再收紧权限、截断和写 PID；`config/pidfile_unix.go:16-35` 使用同一 FD 的非阻塞 `flock`；`run.go:77-84` 以锁结果决定是否启动。
- **已移除**：生产路径不再使用 PID 内容、`processExists` 或 `Signal(0)` 判活。
- **边界**：NFS/网络文件系统、旧版本混跑、历史 Linux/amd64/SIGKILL/PID 复用验收需单独执行。

### P2-01：协议与地址族归一化

- **状态**：已确认，测试证据应限定为 planner/源码级。
- **证据**：`provider/plan.go:288-295` 的 `canonicalProtocol`；`SnapshotRuleKeys`/`PlannedActionKeys` 共用 canonical key；`provider/plan_test.go:206` 覆盖 Lighthouse/CVM 的 ICMPv6 形态。
- **遗留风险**：旧 `provider/common.go:keyOf` 只大写，不折叠 `ICMPv6 → ICMP`；旧 `Diff` 无生产调用者，但未来复用会重新引入不对称。
- **建议动作**：为旧路径增加明确注释或负向控制；不要把旧路径测试说成当前同步链测试。

### P2-02：Aliyun ECS 删除分批

- **状态**：生产逻辑已确认；测试判别力待按 TODO-002 补强。
- **已确认**：每批最多 100 条、部分成功结果保留、目标级 S2 进行残留核验。
- **未完成**：请求上限负向约束、三批停止条件、完整 RuleID 断言、Provider→syncer 贯通测试。
- **外部边界**：真实 ECS API 未验收。

### P2-03：unsupported 能力、partial 与清理冻结

- **状态**：unsupported 原因码、目标 `partial`、清理冻结已确认；`coverage_ready` 输出需按 TODO-001 修正。
- **证据**：`provider/plan.go:169-182` 能力矩阵、`:405-429` 原因码、`:699-728` 安全门；`syncer/target.go:213-217` partial。
- **验收重点**：unsupported-only 不得报告为 coverage ready；unsupported 与可实施期望混合时仍需保留 partial/安全门语义。

### P2-04：RuntimeState 发布顺序与健康唤醒

- **状态**：已确认。
- **证据**：`webui/api/deps.go:147-169` 先 `ApplyState`，后唤醒 Health/Push；`syncer/syncer.go:137-138` 先替换 Runtime 快照，再投递控制通知。
- **边界**：真实 Uptime Kuma 接收、DOWN/恢复通知未验收。

### P2-05：Webhook 三渠道业务响应与有界读取

- **状态**：已确认；P3-21① 的 drain 子项仍是部分修复。
- **证据**：`notifier/webhook.go:136-193`；读取上限 `16 KiB + 1`；钉钉、飞书、Slack 的业务成功码分别有专属校验。
- **已确认**：非 2xx、未知/非法正文、业务码失败、读取失败和超限正文不会被当作成功；EventBus 日志不泄露完整响应。
- **边界**：真实钉钉、飞书、Slack 接收未验收。

### P2-06：告警页面加载状态与保存守卫

- **状态**：已确认，模板级测试仍可补强。
- **证据**：`Alerts.vue:50-70` 三态/响应校验；`:135-141`、`:293` 双层加载和重复保存守卫；后端严格契约 `webui/api/alerts.go:112-221`、`decode.go:54-67`。
- **测试缺口**：现有组件测试覆盖脚本状态机，但没有逐项遍历模板 `disabled` 绑定的独立 AST 断言。
- **边界**：PT-B7-07、真实 SMTP/收件箱未验收。

### P2-07：Targets/Rules 删除确认与并发保护

- **状态**：已确认；文档状态存在历史残留。
- **证据**：`Targets.vue:198-224`、`Rules.vue:163` 等只在确认后发 DELETE；提交期间锁定、失败刷新和旧请求序号隔离均存在。
- **文档动作**：清理审计报告中 P2-07 已提交 `ec82d10` 与“尚未提交”并存的历史表述。
- **边界**：浏览器/API/SQLite 证据为历史记录，本轮未重放。

### P2-08：Dashboard 只消费后端 outcome

- **状态**：源码层面已确认；缺少前端自动化测试。
- **证据**：`webui/frontend/src/views/Dashboard.vue:44-68` 直接消费 `last_round.outcome`，处理 failed/partial/cleanup_deferred，不再用时间差自行推导异常。
- **待办**：可补 Dashboard 组件测试，覆盖 idle、success、partial、failed、cleanup_deferred 和空值。
- **边界**：真实浏览器和真实云同步未验收。

### P2-09：Dry Run 使用 `target_id` 稳定 key

- **状态**：源码层面已确认；缺少前端组件测试。
- **证据**：`DryRunResults.vue:133-137` 以 `item.target_id` 作为 key；后端 `syncer/syncer.go:579-584/646-648` 按目标输出。
- **待办**：补组件级渲染/稳定 key 测试，避免只由后端测试替代前端行为验证。
- **边界**：PT-I7-06 和真实浏览器未验收。

### P3-01：DNS 熔断器历史域名淘汰

- **状态**：已确认；身份语义已被 P3-02 的 `DomainKey` 替代。
- **证据**：`dns/circuitbreaker.go:42-73` 只复制配置中仍存在且计数为正的域名，成功后直接删除计数；`syncer/state.go:76-87` 从发布配置提取域名，完整导入继续 Reset。
- **文档动作**：AGENTS 中旧的“原值身份”描述应标注为历史记录，不得与当前 `Lower + TrimSpace` 身份混用。
- **边界**：无绝对域名规模上限；不保证底层 map RSS 立即下降；真实 DNS 上游和长时间压力未验收。

### P3-02：每轮 DNS 半开探测与失败计数

- **状态**：已确认。
- **证据**：`syncer/syncer.go:856-858` 每轮创建并结束 `dnsRound`；`syncer/dns_round.go:26-95` 对半开探测去重、成功清零、失败按轮计数。
- **文档精度**：当前 `syncer/dns_round_test.go` 有 6 个顶层 Test 函数；报告所称“7 个新增用例”应明确第 7 项是子用例，或改为 6 个顶层测试。
- **边界**：半开失败需等下一轮；无冷却 TTL、持久化或后台探测；真实公网 DNS 未验收。

### P3-03：非法 Uptime Kuma Push URL 重试下限

- **状态**：已确认。
- **证据**：`internal/health/push.go:130-145` 非法 URL 使用 timer；`:165-174` 对间隔使用默认值/20 秒下限；Wake/Stop 可打断，timer 到期重读配置。
- **边界**：仅覆盖非法配置路径；正常 HTTP/网络失败仍走普通间隔；脏 SQLite 直写可绕过 API 最小间隔校验；真实 Uptime Kuma 未验收。

### P3-04：日志 SSE 续传、epoch/seq 与前端去重

- **状态**：已确认；浏览器网络验收仍未完成。
- **证据**：`webui/api/logstream.go:31-32/49/57-137` 锁内先历史入队再注册实时订阅；`Logs.vue:51-111` 使用 BigInt、去重和 reset/断线提示。
- **边界**：缓存过期、进程重启、慢订阅者满载不承诺无损；该项是日志 SSE，不等同同步事件 SSE；PT-AUDIT-01 仍未验收。

### P3-05：普通 JSON 响应 `Cache-Control: no-store`

- **状态**：已确认，范围必须限定为普通 JSON 共同出口。
- **证据**：`webui/api/deps.go:229-244` 在提交响应头前设置 `no-store`；错误响应同样走 `writeJSON`。
- **独立出口**：成功 SSE 仍为 `no-cache`；导出成功、静态 health、SPA 静态资源和 mux 自动错误保持独立行为。
- **文档动作**：不得写成“全站所有 HTTP 响应均 no-store”，也不得把它表述为凭据回显/认证语义修复。

### P3-06：CVM 入站规则配额保护

- **状态**：数量保护已确认；错误分类按 TODO-003 处理。
- **证据**：`provider/tc_cvm.go:130-136/243-275` 新增前检查；统计与入站数组取保守较大值；91～100 告警，超过 100 拒绝。
- **边界**：真实零规则响应、模板统计、提额账号和字段组合未确认；配额保护不替代 S0 Version/S1 删除定位。

### P3-07：删除不可达旧同步链

- **状态**：已确认；另有独立状态语义观察未裁决。
- **证据**：旧 `retrySync`、`retrySyncDetailed`、`truncateDesc` 全仓生产路径已删除；当前链为 `Run → syncAll → runRound → syncTarget → runTargetAttempt/runTargetCleanup`。
- **未裁决观察**：部分删除成功后 S2 失败，随后 S0 重试耗尽时，前次可信残留计数是否会被空 attempt 覆盖为 0。不要把该观察并入旧链删除问题，也不要假设它已解决。

### P3-08：旧 PID 判活残留

- **状态**：已确认，与 P1-02 同源，不应重复计为独立实现。
- **证据**：全仓生产代码已无 `processExists`、`Signal(0)`、`syscall.Kill` 判活路径；互斥由 flock 承担。
- **边界**：与 P1-02 相同，NFS、旧版本混跑、Linux/amd64 真机和 SIGKILL/PID 复用未在本轮执行。

### P3-09：SQLite 写事务 `immediate`

- **状态**：已确认。
- **证据**：`config/store.go:82` DSN 使用 `_txlock=immediate`；`BeginTx` 适用于可写事务，ReadOnly 事务跳过该 begin mode；本地 `modernc.org/sqlite@v1.54.0` 的 `tx.go`、`conn.go`、`sqlite.go` 语义与实现一致。
- **边界**：写锁超过 `busy_timeout=5000` 仍可能 BUSY/API 500；没有事务重试；ReadOnly 不是文件系统级拒写。

### P3-10：数据库、日志渲染、SSE 序列化和 health 写错误处理

- **状态**：已确认。
- **证据**：`config/store.go:154-161` 统一关闭并必要时 `errors.Join`；`webui/api/logstream.go:121-137` 渲染失败不更新序号/缓存；`webui/api/sync.go:128-133` 坏事件固定 WARN 后跳过；`webui/server.go:296-301` 静态 health 写失败只记 Debug。
- **边界**：渲染失败主要是规范性补齐，`bytes.Buffer` 不易复现写失败；`slog` 不会自动告警。

### P3-11：`AddSyncLog` 错误上抛

- **状态**：已确认。
- **证据**：`webui/api/logwriter.go:83-87` 返回数据库错误；`notifier/bus.go:117-123` 的异步 EventBus 统一 WARN。
- **边界**：没有持久化重试队列，不保证最终落库，也不应表述为自动恢复。

### P3-12：数据目录、SQLite/WAL/SHM、pidfile 权限

- **状态**：已确认。
- **证据**：`run.go:44-52`、`config/deployment.go:65-70`、`config/pidfile.go:28-36`、`config/store.go:114-132/142-151/172-174`；目录 0700，主库/WAL/SHM/pidfile 0600，失败即退出。
- **边界**：不阻止 root/同 UID、ACL/NFS 等特殊主体或挂载；文档中的 Linux arm64 容器兼容执行不等于原生 linux/amd64 主机验收。

### P3-13：Go 1.27.1 与标准库 MultiHandler

- **状态**：已确认。
- **证据**：`go.mod:3`、`build/Dockerfile:11-12`、`.github/workflows/docker-publish.yml:11-12/31` 固定 Go 1.27.1 和 `GOTOOLCHAIN=local`；`app/logutil.go:33-41` 使用标准库 MultiHandler。
- **边界**：CI 仍有 SDK `go get -u`/`go mod tidy` 策略，工具链固定不等于依赖完全固定；远端 CI、原生 amd64、macOS 13 真机未验收。

### P3-14：缺失服务端依赖返回 503

- **状态**：已确认。
- **覆盖端点**：`/api/sync/trigger`、`dryrun`、`pause`、`resume`、`events`、`/api/logs/stream`。
- **证据**：`webui/api/sync.go:24-97`、`webui/api/logstream.go:164-167` 先检查依赖，再进行协调器/SSE 探测；`/api/sync/status` 的 nil 语义仍为 200；已接线但未 Run 的 trigger 可为 202；SSE 能力缺失仍为 500。
- **边界**：没有 `Retry-After`，HTTP 503 不保证原生 EventSource 自动重连；真实部署和浏览器未验收。

### P3-15：丢弃告警日志聚合

- **状态**：源码层面已确认；文档仍有提交状态残留。
- **证据**：`notifier/inflight.go:66-148` 按渠道聚合满载丢弃日志；`notifier/email.go:174-181`、`notifier/webhook.go:92-100` 接入；`webui/api/alertset.go:94-132` 长期持有限流器。
- **文档动作**：清理审计报告和 AGENTS 中 P3-15 “尚未提交”与已提交 `ce3eeaa` 并存的表述。
- **边界**：进程在 30 秒汇总前退出可能丢失未输出计数；真实通知链路未验收。

### P3-16：前端状态、路由、保存守卫和扫描清空

- **状态**：六个子项源码层面已确认；44px 与部分模板行为缺少完整自动化/浏览器证据。
- **① RunTest 44px**：`RunTest.vue:32` 使用 `size="large"`，`App.vue:19-20` 主题覆盖为 44px；当前没有 RunTest 专属自动化尺寸断言。只能称静态源码成立。
- **② 路由高亮**：`App.vue:9-20/68-70/95` 从 `route.path` 派生 active key，菜单点击只导航，不提前写本地高亮状态；`main.ts` 路由表与菜单 key 一致。
- **③ Targets/Rules 保存守卫**：保存期间防重复提交、旧列表失效、表单/删除/连接测试锁定；`p316-state.test.mjs` 有结构化断言。
- **④ Rules 端口禁用**：ICMP 端口独立使用 `saving || protocol === 'ICMP'`，不受 NForm 整体 disabled 影响。
- **⑤ Settings 保存**：`Settings.vue` 只提交十个可见字段，theme 独立处理；后端 `settings.go` 的完整契约保持；保存入口有重复提交和卸载晚到守卫，但不能扩展为整个 NForm 保存时都禁用。
- **⑥ 扫描清空/缓存**：同厂商两个 DELETE 使用 `Promise.allSettled`；部分失败不虚构原子回滚；序号隔离旧 GET；后端两个产品清空仍是独立 DELETE。
- **边界**：单浏览器实例级保护，不提供跨客户端幂等；清空不具备厂商级原子性；`runScan` 返回后未完全复用 `pageActive`，不要把结论扩大成所有扫描请求卸载后都不更新状态；真实浏览器和产品联合验收未执行。
- **文档动作**：清理 `fwalizer-audit-final1.md`、`AGENTS.md` 中 P3-16 已提交 `396885f` 与“尚未提交”并存的历史文字。

### P3-21：响应体 drain 与 Push 读取上限

#### P3-21① Webhook drain

- **状态**：部分修复。
- **已实现**：`notifier/webhook.go:139-146` 对 2xx 响应最多读取 16 KiB+1；不超限时通常读到 EOF。
- **未实现**：非 2xx 直接返回，不 drain；超过 16 KiB 后停止读取，不继续 drain 到 EOF。
- **后续动作**：若合同要求连接复用/完整 drain，单独设计有界 drain 策略和测试；不能把当前 P2-05 的有界读取校验误写成完整 drain 已完成。

#### P3-21② Push 响应体字节上限

- **状态**：未修复/报告明确跳过。
- **证据**：`internal/health/push.go:252-266` 使用 `json.NewDecoder(resp.Body).Decode(&payload)`，没有 `io.LimitReader`、`MaxBytesReader` 或等价字节上限。
- **后续动作**：若继续处理，应先定义最大响应体、超限错误、连接关闭和 JSON 尾随数据语义；10 秒 HTTP timeout 不能替代字节上限。

### P3-25：分页完整性与资源扫描范围

#### P3-25（同步路径）

- **状态**：已确认，建议补直接贯通测试。
- **证据**：`provider/ali_ecs.go:58-111` 对 NextToken 未推进/历史重复返回 `ErrSnapshotIncomplete`；`syncer/target.go:153-154` 在 S0 不完整时提前返回，不进入删除。
- **测试缺口**：当前主要是 ECS Provider 分页测试 + 通用 fake Provider 同步测试，没有真实 ECS SDK 分页异常直接贯通 `syncTarget → DeleteRules` 并断言删除为 0 的单一测试。

#### P3-25（资源扫描路径）

- **状态**：仅 ECS 资源扫描路径成立。
- **已确认**：ECS 扫描使用 NextToken，token 不推进/环路失败，结构异常不覆盖缓存。
- **未收口**：Lighthouse、CVM、SWAS 仍存在集合缺失静默结束、半截结果返回或 `resp.Response == nil` 风险；不能把 P3-25 写成四平台扫描器全部修复。

### P3-26：ECS 资源扫描结构异常与旧缓存保护

- **状态**：ECS 范围内已确认。
- **证据**：`provider/scan.go:184-228` 区分有效空数组与缺失/null 集合、null 元素、空资源 ID；只有扫描成功才由 `webui/api/scan.go:49-66` 覆盖保存。
- **边界**：真实 ECS 零资源响应是否一定返回显式空数组仍未确认；Lighthouse/CVM/SWAS 同类扫描语义未修复。
- **文档动作**：将标题和状态明确限定为“ECS 资源扫描分页与结构完整性”。

## 五、文档与台账整理待办

### TODO-004：修正编号总数和范围说明

- 将“28/28”改为“基础连续项 28 个；附加核验 P3-21、P3-25、P3-26，共 31 个标签”。
- 明确 P3-21 的两个子项，以及 P3-25 的同步路径/资源扫描路径拆分。
- 不要将附加编号重新塞回基础 28 项的统计分母。

### TODO-005：修正 P0-01 行号和生产路径说明

- 将旧 `provider/common.go` 的行号改为当前工作树实际位置。
- 同时记录旧 `Diff` 是废弃路径，当前生产收敛在 `provider/plan.go` canonical planner。

### TODO-006：清理“尚未提交”历史残留，但保留历史证据（P3-18 范围已本地完成）

- **2026-10-03 处理结果**：审计当前索引及相关批次标题已补提交事实，历史“尚未提交”继续作为各批次当时的事实保留；AGENTS P3-17 已标后续提交，最新 P3-18 段明确历史阅读规则。P3-18 该批"尚未提交"为当时记录（2026-10-08 复核订正：已提交 `f0997cd`），验证见审计独立补记；其他文档/测试数量和生产语义待办不因此关闭。
- **2026-10-08 复核订正**：审计 §0.2 / P3 清单 / §0.4 的 **P3-24 三处"尚未提交"** 已订正为已提交 `938bb8b`（= 当前 HEAD）；**P3-18 清单行** 已订正为 `f0997cd`；**P2-07 四处**（审计 `L848`/`L865`/`L1561`/`L1584`）已订正为 `ec82d10`；**§14 与附录 A 的"当前基线"** 已改指现时基线 `938bb8b`（ahead 0、工作树含该批 5 个未提交文件）；**P3-06 同格双结论** 已就地标注后半段被推翻。TODOLIST 自身的"tracked 267 / 忽略产物 3 项 / `fwalizer` 存在"已按实测订正为 **tracked 274 / ignored 2 项（`webui/frontend/dist/`、`webui/frontend/node_modules/`）/ 无 `fwalizer` 二进制**。仍未闭环的问题、待裁决事项、未派发专项与外部验收边界统一登记在 [Issue8.md](./Issue8.md)。
- 重点检查：P2-07、P3-06、P3-07、P3-14、P3-15、P3-16、P3-25/P3-26。
- 保留历史批次当时“尚未提交”的事实，但在段首加明确时间/基线，避免被当前状态索引误读。
- 当前提交事实：P2-07 `ec82d10`、P3-06 `c08f2e1`、P3-07 `ba82292`、P3-14 `5c16116`、P3-15 `ce3eeaa`、P3-16 `396885f`、P3-25/P3-26 `901d642`。

### TODO-007：修正测试数量与路径注释精度

- P3-02：`syncer/dns_round_test.go` 当前为 6 个顶层测试；如保留“7 个”，必须明确第 7 项为子用例。
- `syncer/cleanup_test.go:498` 注释名与相邻函数 `TestCleanup_ECSDeletesInBatchesOf100` 不一致，应在文档整理阶段统一。
- 审计报告 tracked 文件数 `244`、忽略产物 2 项等旧基线也应明确时间或更新为当前事实：**2026-10-08 实测为 tracked `274`、`git status --ignored` 共 2 项（`webui/frontend/dist/`、`webui/frontend/node_modules/`）、不存在 `fwalizer` 二进制**（原文"tracked 267 / 忽略产物 3 项 / `fwalizer` 存在"已过期）。

### P3-18：当前文档入口与历史证据对账（2026-10-03，本地完成）

- **范围**：九份 Markdown 与 constants.ts 两处注释，共十文件；当前状态、固定历史比较终点、文档/实现提交角色与源码符号引用已更正。
- **清单更正**：Push 旧注释已随 `c5cc79d` 修复，邮件/Webhook 所引注释缺少具体错误证据，本轮不修改这些 Go 源码。
- **收口边界**：P3-17 已提交 `5860cef`，P3-18 与 I-11 仅本地文档/注释闭环；TODO-004/005/007 的其余统计、生产路径证据与测试注释仍待独立处理，TODO-001～003/008 保持未完成/待裁决。
- **核验**：提交/祖先关系、固定历史 diff、相对链接/新增章节引用、外部状态、文件范围及 TypeScript token 比对结果见 [审计报告](./fwalizer-audit-final1.md) P3-18 实施补记。没有新增真实外部通过结论；该批改动**已提交 `f0997cd`**（2026-10-08 复核订正，原文"尚未提交"为当时记录），未 fetch/push。仍未闭环项见 [Issue8.md](./Issue8.md)。

## 六、尚未裁决的观察

### TODO-008：P3-07 空 attempt 覆盖可信残留计数

- **现象**：部分清理已成功，S2 随后可重试失败，再次 S0 重试耗尽时，前次可信残留计数可能被空 attempt 覆盖为 0。
- **当前状态**：未决定这是预期的“最后一次新快照优先”语义，还是会削弱残留可观测性。
- **要求**：先构造最小可判别状态矩阵，明确 S0/S1/S2 各阶段的可信计数来源，再决定是否修改；不得因为 P3-07 删除旧链而默认该观察已解决。

## 七、外部/人工验收清单（均未因本轮源码核验而升级）

- 真实腾讯云、阿里云 API：包括四云回读、分页、删除安全、Lighthouse 多端口、CVM 配额真实响应。
- 真实 SMTP、真实收件箱投递。
- 真实钉钉、飞书、Slack Webhook 接收和业务失败行为。
- 真实 Uptime Kuma Push、HTTP Monitor、DOWN/恢复通知。
- 真实浏览器：日志 SSE 续传、Dry Run、Dashboard、Alerts、删除确认、Settings/扫描清空联合行为。
- 当前 revision 的远端 GitHub Actions、GHCR 推送和部署镜像验收。
- macOS 13+ 真机、原生 linux/amd64 主机；现有 arm64 容器兼容执行不能替代原生 amd64。
- 历史仓外浏览器/临时 SQLite 目录的可复核性。

## 八、建议跟进顺序

1. 先处理 TODO-001：修复并测试 unsupported-only `coverage_ready`。
2. 再处理 TODO-002：补齐 ECS 分批测试判别力。
3. 决定 TODO-003 的 `ErrSnapshotIncomplete` 契约口径并补测试。
4. 对 P3-07 独立观察做状态矩阵，决定是否列入后续修复。
5. TODO-006 的 P3-18 范围已本地完成；继续独立整理 TODO-004/005/007，其余编号、数量、生产路径与测试注释不得因 I-11 闭环提前关闭。历史证据保留，只补时间和当前状态。
6. 如继续推进 P3-21，先分别定义 Webhook drain 与 Push body 上限的独立合同和边界。
7. 最后按外部验收清单逐项执行真实云、通知、浏览器、Docker/平台和远端 CI，不把本地源码/测试结论外推为发布闭环。

## 九、当前结论

- 报告的绝大多数生产路径判断得到子代理支持。
- 原“28/28 全部源码层面无保留成立”需要改写为：基础 28 项中，主线大多成立；P1-01/P2-03 存在 `coverage_ready` 输出缺陷；P3-06 有错误分类契约差异；额外 P3-21/P3-25/P3-26 必须按拆分范围理解。
- 本文件的原核验与未完成待办继续保留；仅本次明确标记的 P3-18/对应 TODO-006 文档项已本地实施，不代表其他待办或外部验收完成。
