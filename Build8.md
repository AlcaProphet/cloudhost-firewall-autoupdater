# Build8 — EdgeOne 回源同步构建追踪

> 2026-11-12 · 工作分支 `beta` · 起始基线 `102d838f3bcc3d1624c32e44ed65f2eb6feaa0b4`。
> 设计与已确认行为见 [Design6](Design6.md)。当前编码约束见 [AGENTS.md](AGENTS.md)。Step 0 是本次文档准备，后续 Step 均为计划。

## 使用方式

按 Step 列出的依赖推进。一个 Step 只收口一个可单独验证的改动范围。完成当步 Checklist 并填写实际证据后，再勾选进度索引。证据记录日期、提交或工作树状态、执行命令与结果、实际覆盖及待补项。模拟、真实云和发布分别标记。实现若需要变更现有合同，在对应 Step 同步 `AGENTS.md` 中涉及的条款。

可直接复制到每步的证据栏。

```text
日期 / 分支 / 提交或工作树状态
实际改动文件
执行命令 / 返回值 / 覆盖场景
反向用例 / 预期与实际结果
外部验收状态 / 待补项
```

## 进度索引

- [x] [Step 0 文档与基线](#step-0)
- [ ] [Step 1 DNS 默认 TAG 和旧库迁移](#step-1)
- [ ] [Step 2 双功能开关与间隔设置](#step-2)
- [ ] [Step 3 站点与目标绑定 API](#step-3)
- [ ] [Step 4 配置包往返与兼容](#step-4)
- [ ] [Step 5 TEO 只读查询](#step-5)
- [ ] [Step 6 CIDR 规划与 TAG 隔离](#step-6)
- [ ] [Step 7 全局重叠判定和自动恢复状态](#step-7)
- [ ] [Step 8 独立调度与正式入口](#step-8)
- [ ] [Step 9 目标写入与回读](#step-9)
- [ ] [Step 10 站点自动确认](#step-10)
- [ ] [Step 11 版本切换与旧网段清理](#step-11)
- [ ] [Step 12 模拟测试 API](#step-12)
- [ ] [Step 13 全局设置和 EdgeOne 页面](#step-13)
- [ ] [Step 14 模拟测试与仪表盘界面](#step-14)
- [ ] [Step 15 运行健康与日志](#step-15)
- [ ] [Step 16 本地端到端验收](#step-16)
- [ ] [Step 17 真实云验收与发布证据](#step-17)

<a name="step-0"></a>

### Step 0 · 文档与基线

**输入与产物**　记录分支、完整提交、工作区差异和已确认行为。以 Design6 为技术参考，以本表为执行记录。

**核验方法**　用 `git status --short --branch`、`git rev-parse HEAD` 核对基线；检查文档之间的相对链接、Step 索引、依赖方向和 Checklist。对未跟踪文档使用 `git diff --no-index --check /dev/null <文件>`，以覆盖普通 `git diff --check` 看不到的新增文件。

**Checklist**

- [x] 建立 Design6、Build8 及双向导航，记录 `beta` 基线。
- [x] 对照已确认的双开关、总暂停、冲突自动恢复、数据不足时单侧继续及自动确认行为，核对两个新增默认设置。
- [x] 校验 Markdown 链接、Step 依赖和 Checklist 状态，确认工作区差异仅含本次文档。

**证据**　2026-11-12，已核对本地及远端 `beta` 基线。两份新增 Markdown 通过链接、编号、依赖和空白检查。工作区仅新增本文与 Design6，Step 1～17 保持待执行。

**参考样例复核**　从 Design6 提取 DDL，在临时 SQLite 实际建四张表，验证重复绑定、状态更新和迁移标记回滚。四份 JSON 样例通过解析及 v4 引用核对。从文档提取 Go 片段，在临时模块验证 IPv4/IPv6 重叠、准入与自动恢复、站点覆盖判定。核对 TEO SDK 的查询和确认方法签名。以上是文档样例证据，产品实施和真实云验收按各 Step 记录。

**核验后修订**　补齐首次冲突写库失败后的进程内阻断、过渡保护前缀参与全局预检、确认去重与未知结果处理，以及模拟测试对过渡保护的依赖。临时 SQLite 复现首次保存被写锁拒绝及后续保存成功。提取 Go 示例验证跨轮阻断和 `Next` 缺失时的重叠判定，用独立模型复查八个确认状态场景。Step 1～17 保持待执行。

<a name="step-1"></a>

### Step 1 · DNS 默认 TAG 和旧库迁移

依赖 Step 0。范围为默认值、设置回显和旧库迁移。

**代码入口**　`initTables`、`migrateSchemaTx`、`normalizeSettings`、`ResetAllTx`、`handleGetSettings`。 [Store](config/store.go) · [设置 API](webui/api/settings.go) · [DDL 和迁移参考](Design6.md#schema-reference)

**实施顺序**

1. 找出运行快照和设置接口的 `auto-dns` 默认值。建立一个供两处共用的 `DefaultDNSTag`，新值为 `auto-ddns`。区分现有 v3 文件里明确记录的 `tag`，以及本机读取新库的缺省值。
2. 在 `initTables` 的事务内，建表前识别已初始化旧库，再按一次性标记保留旧隐式 TAG。用实际 `settings` 值和 `strings.TrimSpace` 判断缺省，显式非空值继续走正常校验。
3. 测试迁移回滚和 reset 后重启。调整基线中断言「新库默认 `auto-dns`」的测试和当前 README 示例，保留验证历史显式值的夹具。

**断言与证据**

| 夹具 | 必须断言 |
|---|---|
| 空文件新库 | 运行快照、GET 设置和导出解析出的 DNS TAG 都为 `auto-ddns`。 |
| 老库无键或空白键 | 首次打开后得到 `auto-dns` 且写入旧有效值，第二次打开不重复迁移。 |
| 老库已有 `my-dns` | 新旧两次启动均保持 `my-dns`；此前 `[my-dns]` 规则仍可识别。 |
| 已迁移库 reset、重启；迁移中注入写入失败 | reset 后稳定使用 `auto-ddns`；迁移失败时原设置和迁移标记都未被部分提交。 |

使用 `config/store_v3_test.go` 的旧库构造方式和 `TestConfigExportEmptyDatabaseSchema` 的导出断言方式。建议运行 `go test ./config ./webui/api -race -count=1`，证据包含数据库实际值及 API 回显。

**Checklist**

- [ ] 新库及 reset 后使用 `auto-ddns`。共享默认值用于运行快照和设置回显，更新当前用户文档中的默认示例。
- [ ] 在建表迁移事务内区分新库和旧库，保留旧库显式 TAG，并为原本靠隐式 `auto-dns` 运行的旧库写入原有效值。验证重复启动与 reset 不会重复迁移。
- [ ] 用新库、缺键旧库、空白键旧库、显式自定义 TAG、迁移失败回滚验证行为。对照云规则描述，旧 TAG 的所有权保持可判定。

**证据**　待执行。

<a name="step-2"></a>

### Step 2 · 双功能开关与间隔设置

依赖 Step 1。范围为后端设置、严格校验及配置发布。

**代码入口**　`settingsKeysV3` 所在的配置键集合、`BusinessSnapshot.ToRuntimeConfig`、`RuntimeConfig.DeepCopy`、`settingsPatch.values`、`handlePutSettings` 和 `ConfigCoordinator.Mutate`。 [运行配置](config/runtime.go) · [设置 API](webui/api/settings.go) · [接口和示例](Design6.md#api-reference)

**实施顺序**

1. 保留 `sync_enabled` 保留键，增加 `dns_enabled`、`edgeone_enabled`、`edgeone_tag`、`edgeone_interval` 的已知键、默认值与快照字段。不要将 v3 备份字段集合误扩展为要求 v3 带新字段。
2. GET 保持字符串映射。PUT 增加独立指针字段并检查类型和空值，复用现有布尔文本、TAG 及正时长校验。将解析后的时长进入运行时，保留文本用于回显和备份。
3. 把两个 TAG 的比较放在事务内完整合并之后。请求只提交一项 TAG 时仍能判定与当前另一个 TAG 是否相同。验证并发设置与失败回滚。
4. 把迁移、v3 转换和明确用户设置分开。历史 DNS TAG 已为 `edgeone` 时新模块初始化 `edgeone-origin`，普通保存与 v4 遇到重名直接返回校验错误。

**最小测试矩阵**　开关字符串 `"false"` 生效；未知键和 `sync_enabled` 的普通 PUT 仍被拒绝；`36h`、`1h30m`、`1.5h` 正确解析，空白、`0s`、负值及错误类型失败。覆盖「同时换两个 TAG，最终值不相同」成功，以及「只改其中一个与另一项冲突」事务零写入、零 Apply。基于 `TestPutSettings_PartialUpdate` 和配置协调器夹具执行 `go test ./config ./webui/api -race -count=1`。

**Checklist**

- [ ] 保留 `sync_enabled` 总暂停，新增独立 DNS 与 EdgeOne 开关，并把它们放入同一不可变运行时快照。默认 DNS 开启、EdgeOne 关闭。
- [ ] 增加可自定义 EdgeOne TAG 和同步间隔，默认分别为 `edgeone` 和 `72h`。EdgeOne 间隔复用现有正时长解析，与 DNS 间隔分别保存和回显。
- [ ] 校验去除首尾空白后 TAG 互斥。兼容 DNS TAG 已为 `edgeone` 的旧配置，给新模块初始化独立值。
- [ ] 覆盖部分更新、同时改两个 TAG、非法时长、重名、回滚零发布和正确提交只发布一次。

**证据**　待执行。

<a name="step-3"></a>

### Step 3 · 站点与目标绑定 API

依赖 Step 2。范围为站点和绑定的 SQLite 模型及配置 API。

**代码入口**　`schemaSQL`、`LoadBusinessSnapshotTx`、`handleDeleteTarget`、`Deps.Register`、`ConfigCoordinator.Mutate`。 [Store](config/store.go) · [目标 API](webui/api/targets.go) · [建表 SQL](Design6.md#schema-reference) · [站点 API](Design6.md#api-reference)

**实施顺序**

1. 建立 `edgeone_sites`、`edgeone_site_targets` 及按目标查询引用的索引。站点、绑定、默认值和 reset 行为由正式 Store 负责。
2. 站点 CRUD 走相同的纯校验和事务入口。更新站点时替换原绑定，以一次发布反映目标列表。将完整快照的站点 slice 和各 `TargetIDs` 一起深拷贝。
3. 把请求校验与落库完整性错误分开。PUT/DELETE 的缺站点返回 404，重复创建或目标被绑定后删除返回 409，落库引用损坏进入现有完整快照错误路径。
4. 将物理目标身份集中在目标配置转换处，使用规范化的云类型、地域和资源 ID。目标更新后也校验已有绑定，确保两个别名不能在同一站点被当作两份证明。

**最小测试矩阵**　使用 API 测试环境和临时 SQLite 依次创建两个目标、绑定一个站点、修改成第二个站点共用其中一个目标。加入空列表、重复 ID、同物理资源别名、悬空 ID、目标改名后撞别名、站点更新回滚、目标删除阻止与 reset。失败断言包括旧绑定仍完整、无 Apply；成功断言包括列表稳定顺序与快照独立。参考 `TestI8R09MutationRejectsCorruptFinalSnapshot` 的最终快照判别方式。

**Checklist**

- [ ] 保存唯一 `ZoneId`、目标列表及入站协议、端口、地址族，复用现有目标 ID 与规则字段校验。
- [ ] 实现站点读取、创建、编辑和删除，随事务发布完整运行快照。配置保存阶段只做本地校验，首轮查询在发布后调度。
- [ ] 检查目标存在性、重复绑定、实际资源别名和引用闭包。被 EdgeOne 站点引用的目标删除返回 409。
- [ ] 覆盖一站多目标、多个站点共用目标、reset、引用保护及无效写入的事务回滚。

**证据**　待执行。

<a name="step-4"></a>

### Step 4 · 配置包往返与兼容

依赖 Step 3。范围为导入、导出和版本合同。

**代码入口**　`handleConfigImport`、`handleConfigExport`、`decodeBundleV3Strict`、`validateAndNormalizeBundle`、`DeleteImportOwnedTablesTx`。 [导入导出](webui/api/export.go) · [现有 v3 DTO](webui/api/bundle_v3.go) · [v4 完整示例](Design6.md#api-reference)

**实施顺序**

1. 在现有严格 JSON 校验路径识别版本，按版本选择对应 DTO 和必填结构。v3 使用原合同，v4 新字段完整。将两版本转换为同一已校验业务模型。
2. 扩展导入事务的清理与插入顺序。先插入 targets，沿用 `LastInsertId` 建立 `export_id → target_id`，再写 DNS 规则和 EdgeOne 绑定。
3. 导出复用只读业务快照，固定 v4、稳定顺序、空数组与附件命名。保留原有敏感备份传输策略。运行表、确认状态和历史冲突不进入文件。
4. 在对应阶段更新 AGENTS 的当前版本条款和前端下载名引用，归档资料保持原历史口径。

**证据目标**　先将 v3 原始 fixture 直接喂给新接口，校验保留 DNS 配置且 EdgeOne 关闭。再做 v4 往返，导入端预先消耗自增 ID，证明一个站点绑定的两个目标通过映射定位。负向用例用原始 JSON 覆盖字段缺失/null、重复 `export_id`、目标引用悬空、TAG 重名、非法版本、写到一半回滚。参考 `TestConfigImportCrossInstanceDifferentIDs`、`TestConfigImportStrictDecoding` 和 `TestConfigImportTransactionFailureRollsBack`，记录 HTTP 码、旧数据以及 Apply 次数。

**Checklist**

- [ ] 实现严格 v4 导出及导入，纳入两项设置和站点绑定，并随版本更新下载名和相关说明。
- [ ] 维持严格 v3 导入，保留原 DNS TAG、间隔和总暂停，填入 EdgeOne 关闭且无站点的默认状态。v1、v2 和未知版本继续拒绝。
- [ ] 使用新旧 ID 不同的 SQLite 夹具验证一站多目标的 `export_id` 重映射，以及重名、悬空引用、坏载荷零写入和事务失败零发布。
- [ ] 覆盖同步日志、告警和运行状态的备份边界，同步 [AGENTS.md 配置合同](AGENTS.md#九配置约束)。

**证据**　待执行。

<a name="step-5"></a>

### Step 5 · TEO 只读查询

依赖 Step 2。范围为 SDK 和 `DescribeOriginACL`，以本地模拟服务验收。

**代码入口**　现有腾讯云客户端构造及 `ClientPool`；建议新增独立 TEO 适配文件。 [客户端池](provider/common.go) · [CVM SDK 构造参考](provider/tc_cvm.go) · [TEO Go 示例](Design6.md#teo-reference)

**实施顺序**

1. 核对 `teo/v20220901` 真实模块版本与当前 `common` 的兼容性，记录 go.mod/go.sum 的实际变化。构造函数保持本地无请求，复用已有凭据。
2. 给 `DescribeOriginACLWithContext` 接入请求预算与限速。实现 SDK 响应到内部 `OriginACLSnapshot` 的转换，保留版本、计划时间、RequestId 和完整前缀集合。
3. 先用模拟响应跑转换矩阵，再用 `httptest` 经过真实 SDK 出请求。使用虚构凭据，校验请求头和 JSON 字段形状，避免在输出打印签名或凭据。
4. 将账号、版本数据形状、缺省字段的真实证据留给 Step 17。

**最小测试矩阵**　当前版单独有效；当前与待更新去重；IPv4 带主机位的前缀规范；IPv6 与字段地址族错配；nil/畸形下一版；非启用状态；网络 timeout；权限、限流与 SDK 请求 ID。有效空的单一地址族与整体无可用网段要得到不同结果。建议 `go test ./provider -race -count=1`，证据包括客户端请求数及脱敏错误类型。

**Checklist**

- [ ] 核对 SDK 模块与现有腾讯云 `common` 依赖，配置 endpoint、请求超时、频率限制和安全日志。
- [ ] 解析源站防护状态、当前及待更新版本、计划生效时间和两类 CIDR，规范地址族、掩码及去重。
- [ ] 验证一个站点的结果可供多个目标复用，区分明确无下一版、空或畸形数据、权限错误、限流和超时。
- [ ] 用模拟服务核对 POST、Action、版本、`ZoneId`、请求签名接线和 `RequestId`，记录真实响应仍待 Step 17 核验。

**证据**　待执行。

<a name="step-6"></a>

### Step 6 · CIDR 规划与 TAG 隔离

依赖 Step 3 和 Step 5。范围为纯规划器及其夹具。

**代码入口**　`TargetPlanInput`、`PlanTarget`、`FunctionalKey`、`SnapshotRuleKeys` 和 TAG 渲染。 [规划器](provider/plan.go) · [TAG 判定](internal/tag/tag.go) · [目标规划参考](Design6.md#target-reference)

**实施顺序**

1. 建立内部前缀输入，在 DNS 适配边界生成主机前缀，在 EdgeOne 适配边界保留网段长度。复用现有 key 计算和端口展开。
2. 给规划结果保留来源，DNS 记录规则 ID，EdgeOne 记录站点 ID。对同一物理目标的所有适用 EdgeOne 站点汇总后去重。所有来源仍能追溯每个所需 key。
3. 调整云端覆盖分类。相同 TAG 可证明所属规则，另一功能 TAG 等价残留进入目标级冲突，其他手工精确等价沿用现有约定。
4. 在纯规划阶段保留可实施、全量覆盖、冲突、可清理候选的差别。对不能安全确认的项提供稳定原因。

**判别性用例**　同站点两版本共同 CIDR 与两个站点同 CIDR 只产生一条新增；所有所属站点都出现在结果。DNS `/32` 与 EdgeOne `/24` 保留各自粒度。跨 TAG 等价旧规则产生冲突且无法作为覆盖，手工等价正向对照仍可覆盖；IPv6/SWAS、相反动作和模板定位限制仍生效。运行 `go test ./provider -race -count=1`，保留 `TestPlan_ExternalExactMatchOnly`、`TestPlan_CoverageReadyImplementableSubset` 作为基线对照。

**Checklist**

- [ ] 规划输入保留 EdgeOne CIDR 掩码，同时保持 DNS 单地址转换与现有端口展开、功能 key、能力矩阵。
- [ ] 在实际目标上合并所有适用站点，按功能去重，按 TAG 渲染新增规则与确定删除归属。
- [ ] 另一功能 TAG 的等价规则只作为目标级冲突，手工规则继续按现有精确等价判定。覆盖声明需区分冲突、平台不支持和可实施子集。
- [ ] 定向测试跨站点同目标、双栈、另一 TAG 残留、相反动作、模板导致保守清理、规则容量与 DNS 既有结果。

**证据**　待执行。

<a name="step-7"></a>

### Step 7 · 全局重叠判定和自动恢复状态

依赖 Step 5 和 Step 6。范围为正式写入前的决策与运行记录。

**代码入口**　`syncer.runRound` 上游、`dnsRound`、SQLite 事务。建议把纯判定和运行状态读写分开。 [预检 Go 参考](Design6.md#preflight-reference) · [运行表 SQL](Design6.md#schema-reference) · [DNS 熔断](syncer/dns_round.go)

**实施顺序**

1. 收集候选 DNS 地址、EdgeOne 当前/待更新 CIDR、各来源完整性。DNS IPv4 比较用 `Unmap`。先查成功样本中的正向重叠，再考虑查询是否完整。判定输入支持受保护的过渡前缀，正式读取过渡记录在 Step 11 接入。
2. 实现纯决策，并用进程内状态与 SQLite 唯一冲突行共同记录阻断。发现冲突时先保留内存状态，首次写库失败后继续阻断后续轮次并重试保存。完整无重叠时先提交解除，再清除内存状态；读取或清除失败时保持阻断。避免旧配置的迟到结果写回新一轮状态。
3. 同时启用才使用全局判定。退出到单侧不抹去已查实证据，重新双开先按有效配置复查。模拟测试只计算预览结果。
4. 将可解析但重叠与真实 DNS 失败分离。成功查询产生冲突不增加 DNS 熔断次数。

**判别性用例**　用 `203.0.113.7` 对 `203.0.113.0/24`、IPv6 主机对 `/64`、不同目标、不同端口、只在下一版命中的样本。覆盖部分成功仍证明重叠、首次数据不足时单侧准入、已知冲突重启且输入不足、完整有效输入解除、关一项再开和 reset。连续故障用例按「查实重叠 → 首次 SQLite 保存失败 → 下一轮 DNS 数据不足 → 后续保存成功」推进，断言全过程两侧零云写入、零站点确认。另测启动读冲突行失败和解除提交失败。建议 `go test ./syncer ./config -race -count=1`。

**Checklist**

- [ ] 同一份配置中汇总有效 DNS 地址与所有启用站点当前、待更新 CIDR，跨目标和端口执行包含判断。
- [ ] 验证已证明重叠优先于数据不足。无已知冲突时只停止失败侧；已知冲突在数据不足或仍重叠时继续停止两侧。
- [ ] 记录已查实的冲突，验证首次写库失败后进程内保持阻断，后续复查重试持久化。完整有效输入证明无重叠并提交后自动清除。覆盖已提交冲突的重启恢复、读写失败、关闭一项、重新同时启用和清空配置。
- [ ] 区分无任务、冲突、数据不足、无重叠和恢复。确认 DNS 熔断只记真实解析结果，模拟判定本身无运行状态副作用。

**证据**　待执行。

<a name="step-8"></a>

### Step 8 · 独立调度与正式入口

依赖 Step 2、Step 5 和 Step 7。使用可控时钟、假客户端和写入计数验证调度。生产 EdgeOne 防火墙写入接线留到 Step 9。

**代码入口**　`Run`、`consumeControlState`、`beginRound`、`TriggerSync` 和 `syncAll`。 [主循环](syncer/syncer.go) · [调度参考](Design6.md#preflight-reference)

**实施顺序**

1. 保留现有主循环作为唯一正式入口，计算 DNS 和 EdgeOne 各自的到期、首次启用、手动触发和恢复标记。读取运行快照后以同一准入点确定本轮来源。
2. 只对实际改变的间隔重排对应来源。测试运行跨过到期时间、重复 tick、相同间隔设置、很短的自定义间隔以及等待限速时的合并。
3. 两项启用时在任何目标任务前先形成可写来源集合。为本步使用计数执行器，验证门控后才会进入写入阶段。DNS 正式路径保留既有启停合同。
4. 统一输出 `enabled`、`effective`、`next_due_at` 与预检摘要。调度只读取结果，页面不负责推进时钟或解除冲突。

**最小测试矩阵**　四种开关组合、总暂停、Stop 前后、配置快速合并、EdgeOne 首次启用、只改 EdgeOne 间隔、手动触发撞定时与实际重叠。用可控时钟和通道控制交错；断言两个来源的查询/执行次数、到期值和跨目标零调用。参考 `TestP324CoalescedResumePreservesScheduling`、`TestStaleTriggerAfterPauseDoesNotAddRound`、`TestI8R18StopGateEntrances`。建议 `go test ./syncer -race -count=1`。

**Checklist**

- [ ] 在现有 Syncer 统一接入两项的到期、首次启用、热更新、手动触发及暂停恢复。保持一个正式写入协调入口。
- [ ] 两项分别计时，发布可供仪表盘读取的实际启用和调度状态。EdgeOne 间隔热更新不重置 DNS 定时，常规 DNS 轮复用有效 EdgeOne 快照，过期或变更触发相应重查。
- [ ] 总暂停、Stop 和恢复复用现有准入及在途收尾合同，模拟测试保持独立。验证计时到期、短间隔限速、排队触发和并发配置变更。
- [ ] 在全部目标写入前完成全局判定。以双开关矩阵和跨目标冲突零写入计数证明任何正式入口均未绕过。

**证据**　待执行。

<a name="step-9"></a>

### Step 9 · 目标写入与回读

依赖 Step 6 和 Step 8。范围为目标级正式云防火墙操作。

**代码入口**　`syncTarget`、`runTargetAttempt`、`runTargetCleanup`、`verifyCleanupResult` 和四平台 `Provider`。 [目标执行](syncer/target.go) · [Provider 接口](provider/provider.go) · [平台与定位参考](Design6.md#target-reference)

**实施顺序**

1. 用实际资源身份分组，确保同一个安全组有且只有一个写入执行单元。独立来源仍保留自己的计划、TAG、错误和统计。
2. 从本轮预检结果构造目标输入。S0 获取完整快照和版本，算新旧并存预计增量，再复用 Provider 增量创建。S1 回读验证，对应来源、目标及版本保留证明。
3. 重试以新 S0 和当前版本重新规划，两项启用时不改变已检查输入。DNS-only 继续执行原 DNS 解析与重试逻辑。
4. 延用精确删除和 S2。EdgeOne 旧版删门在 Step 11 接线，此步预留延后状态。实际确认的部分写入、幂等命中和失败要各自记账。

**证据目标**　用假 Provider 捕获 `GetSnapshot/CreateRules/DeleteRules` 的参数与顺序。构造一站跨两目标、同组多站点、CVM 配额不足、版本竞争、创建部分成功、S1 缺项和另一 TAG 残留。加入重试夹具，使第二次 DNS 若被调用会返回重叠地址，断言实际重试没有使用新地址。使用 `TestTargetRetry_IdempotentCreateRequiresCoverage`、`TestTargetRetry_DeleteProgressAcrossAttempts` 做基线对照。

**Checklist**

- [ ] 同一物理目标串行执行增量添加及 S1 验证，计算过渡期规则量和平台容量。延续 Provider 的版本与稳定删除定位。
- [ ] 两项同时启用时，目标重试使用本轮已检查的地址集，每次重读云快照；仅启用 DNS 时保留原 DNS 重试。
- [ ] 删除只允许该功能 TAG，沿用先增、回读、安全门和必要的 S2。EdgeOne 的过期网段清理等 Step 11 提供生效证明后准入。
- [ ] 用本地模拟验证同组多站点、部分写入、配额不足、版本竞争、跨 TAG 残留和跨目标预检。失败时核对真实写入计数与保留旧规则。

**证据**　待执行。

<a name="step-10"></a>

### Step 10 · 站点自动确认

依赖 Step 9。范围为 `ConfirmOriginACLUpdate` 与站点级覆盖证明。

**代码入口**　Step 5 的 TEO 客户端、Step 9 的目标回读结果、站点运行状态与当前运行配置。 [确认调用与状态参考](Design6.md#target-reference) · [确认 API](https://cloud.tencent.com/document/product/1552/120409)

**实施顺序**

1. 把每个目标的回读结果转换为站点证据，核对同一 `ZoneId`、待更新版本、目标身份和配置。以配置要求的目标集作分母。
2. 使用一个确认函数统一判断必要地址族、完整覆盖和相关冲突。重查腾讯云当前及待更新两版的版本和规范网段，并在发请求前再次检查当前配置和站点启用。总暂停后拒绝新发站点确认。
3. 加入过渡运行表的确认状态读写，按 `ZoneId`、待更新版本和规范前缀摘要去重。请求前提交 `unknown`。同一指纹的 `accepted` 跨轮和重启均跳过确认；`unknown` 先只读复查，只有明确证据证明未执行或尚未确认时，才重新核验并允许重试。Step 11 接入写防火墙前的网段保护和清理准入。
4. 发布供页面读取的状态摘要，包含 `RequestId` 及诊断原因，确认失败作为站点结果体现。

**证据目标**　同一站点两个目标均通过且版本不变时确认恰好一次；分别让任一目标失败、只覆盖平台可实施子集、相反动作、绑定变更、总暂停、版本或同版本网段变化，确认计数均为零。加「无已知冲突、DNS 失败、EdgeOne 完整」正向用例。成功后让同一指纹在下一轮及重启后仍出现在 `Next`，确认计数仍为一次；超时后依次测试「重查仍未知」「明确未执行后允许重试」「`OperationDenied.LatestVersionNow` 后重查无待确认工作」。同时核对 `unknown` 提交失败不发送请求、`accepted` 写入失败后不重发。建议运行 TEO 相关包和 `./syncer` 的 race 门禁。

**Checklist**

- [ ] 聚合站点全部绑定目标对同一配置和版本的完整回读证据，纳入容量、地址族和相关冲突。任一目标不通过时确认请求计数为零。
- [ ] 确认前重查版本和网段，复核站点绑定和启用状态。全部通过时自动确认；无已知重叠而 DNS 查询失败时，EdgeOne 仍可完成确认。
- [ ] 验证同一待更新指纹跨轮、重启去重，以及超时后只读复查与有明确证据时的重试准入。记录站点确认结果、`RequestId` 和实际生效状态，供站点页读取。

**证据**　待执行。

<a name="step-11"></a>

### Step 11 · 版本切换与旧网段清理

依赖 Step 9 和 Step 10。范围为过渡状态、清理准入和重启恢复。

**代码入口**　过渡记录写入与读取、确认结果、`runTargetCleanup`。 [运行表 SQL](Design6.md#schema-reference) · [版本与清理参考](Design6.md#target-reference)

**实施顺序**

1. 观察到待更新版本时记录基准版、待更新版和保护前缀，先提交保护，再进行本轮 EdgeOne 写入。站点处于过渡期间时，即使只返回当前版，也保留仍无法证明可移除的前缀。
2. 重启用 SQLite 恢复保护及未确定的确认状态。将仍可能用于回源的保护前缀纳入全局预检。查询失败时保留前缀用于证明正向重叠，同时维持输入不完整状态。
3. 有效查询显示追踪的待更新版已成为当前且网段一致时，先重新计算预检的保护集合，再按同目标所有适用站点计算清理候选。复用当前 S1 的 TAG、版本、定位与 S2。无法证明删除条件时记录延后，下一次从新快照收敛。

**证据目标**　按「A 当前 → A 当前且 B 待更新 → B 已确认但仍是 A 当前 → 重启后 A 当前且无 Next → B 成为当前」依次推进夹具。重启后让 DNS 地址只命中 B 的受保护前缀，断言两侧仍零写入。B 成为当前时再验证 A 特有前缀可先从预检保护集合移出，实际删除继续走目标安全门。加入强制切换、待更新替换、另一站点仍需要 A、删除失败和 S2 失去覆盖的对照。检查 SQLite 过渡内容、预检输入、删除调用计数及规则终态。

**Checklist**

- [ ] 持久化需保护的网段过渡状态，并将仍可能用于回源的前缀纳入预检。验证重启后 `Next` 消失且 DNS 命中受保护前缀时，两侧仍停止写入。
- [ ] 覆盖已确认但尚未生效、下一版消失但当前版未变化、腾讯云强制切换等返回形态。有效查询明确新版成为当前版后，才允许清理过时 EdgeOne TAG 规则。
- [ ] 复用安全删除门和 S2 验证，覆盖同目标另一站点仍需要旧网段、清理失败保留和再次执行收敛。

**证据**　待执行。

<a name="step-12"></a>

### Step 12 · 模拟测试 API

依赖 Step 6、Step 7、Step 9 和 Step 11。范围为现有模拟测试端点及响应。

**代码入口**　`Syncer.DryRun`、`handleSyncDryRun`、`DryRunResult`、现有正式规划器。 [Dry Run API](webui/api/sync.go) · [同步器](syncer/syncer.go) · [响应参考](Design6.md#view-reference)

**实施顺序**

1. 保留既有 TryLock、只读取 S0、每目标结果、空数组语义。按配置准备两种来源，调用共用预检与目标规划。
2. 附加站点版本、数据完整性、容量依据和目标覆盖问题。当前计数、预计新增、过渡期预计占用和已知限额各有字段；未知限额显式表示。
3. 以只读方式取得已知冲突和同一过渡记录，使用正式的保护集合计算预览。只有需要预览时读取云快照，正式缓存、熔断器、运行状态和下次到期时间均保持只读预览前的值。

**断言与命令**　同一组 fixture 对比正式 plan 和 dryrun，覆盖仅 DNS、仅 EdgeOne、两项冲突和总暂停仍可模拟。共享同一份保存的过渡记录，覆盖 `Next` 消失、仍受保护和保护已解除的场景，逐项比较预检保护网段、冲突及清理候选。计数器证明 Create/Delete/Confirm、冲突和过渡状态写入均为零，结果序列化检查空数组不是 null。保留 `TestDryRun_MatchesProductionPlanner`、`TestDryRun_ArraysNeverNull`、`TestDryRun_PublishesNoEvents`。建议 `go test ./syncer ./webui/api -race -count=1`。

**Checklist**

- [ ] 使用正式重叠判定、过渡保护集合和同一规划器，返回站点版本、绑定目标、预计新增、过渡期容量和目标限制。与正式计划对照保护网段、冲突和清理候选。
- [ ] 无适用规则的目标保持既有空结果语义。覆盖仅 DNS、仅 EdgeOne、两项同时启用、已暂停和查询不完整。
- [ ] 验证模拟无云增删、无站点确认、无 DNS 熔断改写、无正式冲突状态变更，且前后端响应数组和错误字段一致。

**证据**　待执行。

<a name="step-13"></a>

### Step 13 · 全局设置和 EdgeOne 页面

依赖 Step 2、Step 3 和 Step 4。范围为配置界面及其行为测试。

**代码入口**　`App.vue` 菜单、`main.ts` 路由、`Settings.vue` 可见字段白名单、`useSettings.ts` 和新增 EdgeOne 页面。 [侧栏](webui/frontend/src/App.vue) · [路由](webui/frontend/src/main.ts) · [设置页](webui/frontend/src/views/Settings.vue) · [接口示例](Design6.md#api-reference)

**实施顺序**

1. 在域名规则后添加独立路由，保留路由驱动高亮及已有主题。新页面复用现有表单和目标选项，保存显式 `target_ids`。
2. 设置页在白名单加入 `dns_enabled`、`edgeone_tag`、`edgeone_interval`。EdgeOne 开关从模块页经现有设置接口提交，布尔控件与字符串字段之间做明确转换。两种 TAG 校验提示以后台结果为准。
3. 站点表单创建、编辑、删除分别接入严格 API；显示一站多目标和安全组影响范围。按钮在保存或刷新阶段按现有行为限制重复提交。
4. 验证从深链打开、前后导航、目标选项为空、保存失败、切页期间迟到响应与导入后读回。

**验收方式**　沿用 `frontend-behavior.test.mjs` 和 `delete-confirm.test.mjs` 的真实组件 fixture，验证按钮、请求载荷、路由高亮和错误反馈，不能只测独立帮助函数。运行 `npm --prefix webui/frontend test` 与 `npm --prefix webui/frontend run build`，记录所用 Node 版本。

**Checklist**

- [ ] 添加左侧栏「EdgeOne 回源」和 `#/edgeone`，验证深链、侧栏高亮与浏览器历史。
- [ ] 全局设置显示 DNS 开关、DNS TAG、EdgeOne TAG 和 EdgeOne 间隔。表单可回显自定义值和明确的字段错误。
- [ ] EdgeOne 页面支持启用开关、站点配置和目标多选，展示安全组影响范围及全部实际源站绑定要求。
- [ ] 验证保存失败保留表单、重复提交锁和晚到响应处理，覆盖一站多目标和备份导入后回显。

**证据**　待执行。

<a name="step-14"></a>

### Step 14 · 模拟测试与仪表盘界面

依赖 Step 11、Step 12 和 Step 13。范围为状态消费和结果呈现。

**代码入口**　`Dashboard.vue`、`RunTest.vue`、`DryRunResults.vue`、`useDryRun.ts`、`types.ts` 与 EdgeOne 站点页。 [仪表盘](webui/frontend/src/views/Dashboard.vue) · [模拟组件](webui/frontend/src/components/DryRunResults.vue) · [响应参考](Design6.md#view-reference)

**实施顺序**

1. 仪表盘区分总暂停、DNS 与 EdgeOne 开关、下次到期和实际失败状态。站点页读取只读状态端点，展示版本、覆盖、确认和更新时间。
2. 在模拟页扩展现有结果组件，显示站点版本、作用目标、容量依据与全局冲突明细。未知限额、未知确认状态和已确认使用不同提示。
3. 添加无记录、全局冲突、数据不足单侧继续、持续冲突后恢复和仅单项运行的 UI 夹具。保留离页晚到请求抑制、真实零和未观测的区分。

**验收方式**　优先在 `dashboard-observation.test.mjs`、`dryrun-state.test.mjs` 和站点页 fixture 中断言真实模板输出。测试无数据不被展示成成功，查询成功也不被误报为已确认。运行前端定向入口及完整前端门禁。

**Checklist**

- [ ] 展示总暂停及两项实际启用状态，EdgeOne 显示当前/待更新版本、最近查询、下次同步和确认状态。
- [ ] 模拟结果显示按目标的容量与覆盖，以及全局地址重叠和数据不足单侧继续的区别。
- [ ] 验证查询失败、待确认、自动恢复、没有适用目标和页面重新挂载时的状态反馈。

**证据**　待执行。

<a name="step-15"></a>

### Step 15 · 运行健康与日志

依赖 Step 8、Step 10 和 Step 14。范围为后端状态和既有观测链路。

**代码入口**　`Checker.Evaluate`、同步状态发布、事件总线及 `StoreLogWriter`。 [运行健康](internal/health/health.go) · [日志写入](webui/api/logwriter.go) · [状态模型](Design6.md#view-reference)

**实施顺序**

1. 总暂停时沿用现有健康边界。根据启用来源及其到期时间评估调度，避免 EdgeOne 的 72h 正常等待被 DNS 5m 判定为卡住。
2. 记录来源与站点的真实结果。已查实重叠不能在无目标计数时走 `idle`；数据不足单侧继续必须能追溯未完成的来源。
3. 复用现有同步失败、运行健康和日志行为。长期冲突的通知按状态边沿处理，成功恢复记一条恢复事件。日志只写安全字段。

**验收方式**　用固定时钟覆盖仅 EdgeOne 正常等待、EdgeOne 过期、仅 DNS、两项全局阻断、单侧失败、恢复、总暂停。断言 `GET /api/health/operational` 与内部评估一致、静态 `/api/health` 继续可用；事件被实际日志链消费，无凭据或 SDK 原始错误载荷。参考 `TestEvaluateScheduleStallAndStartupGrace`、`TestOperationalEndpointUnhealthy503`。建议 `go test ./internal/health ./webui/api ./syncer -race -count=1`。

**Checklist**

- [ ] 运行健康按启用功能及其各自的到期状态判断。保留静态 health 语义，防止 EdgeOne 正常低频等待被误判为 DNS 调度停滞。
- [ ] 记录冲突、单侧继续、恢复和站点确认结果。区分真实错误与无任务，按现有事件合同写日志和告警，验证无凭据泄露。
- [ ] 验证长期冲突、自动恢复、总暂停、单侧失败和仅 EdgeOne 运行的状态与健康回归。

**证据**　待执行。

<a name="step-16"></a>

### Step 16 · 本地端到端验收

依赖 Step 15 及前置 Step。使用正式入口、本地模拟 TEO 与 Provider、临时 SQLite 和浏览器完成验收。

**执行材料**　临时 SQLite、模拟 DNS 和 TEO、假 Provider、真实 FWAlizer 进程、前端浏览器入口。复用进程级 `TestMain`、`TestNoArgsStartsWebUIWithEmptyDatabase` 和 `TestProcessConfigExportImportRoundTrip` 的隔离方式。 [进程夹具](main_test.go) · [Make 入口](Makefile)

**执行顺序**

1. 跑定向故障链，核对模拟服务的动作序列、SQLite 的运行记录、API 和前端展示相互吻合。
2. 运行真实二进制，按暂停、配置站点、模拟、启动、冲突、重启、自动恢复和 SIGTERM 顺序验证。浏览器覆盖设置回显、两组开关、状态与错误信息。
3. 执行以下门禁并记录精确命令、返回值与耗时。依赖审计使用当前仓库与 CI 约定。

```bash
go test ./config ./provider ./syncer ./webui/api ./internal/health -race -count=1
make all
npm --prefix webui/frontend audit
npm --prefix webui/frontend audit --omit=dev
git diff --check
```

**通过条件**　手动和定时都能复现跨目标冲突零写入、已知冲突重启保持以及完整数据自动恢复。另有单侧失败仍可正常确认的正向对照。一站多目标必须取得实际回读才进入确认，版本变化反向用例产生零确认。真实云和远端发布的证据留在下一步。

**Checklist**

- [ ] 验证新库和旧库启动、站点绑定、间隔热更新、v3 导入/v4 往返、只读模拟和真实二进制进程行为。
- [ ] 跨目标重叠时两侧云写入与确认均为零。已知冲突重启后保留，查实解除后自动恢复；无已知冲突时验证失败侧停止、正常侧继续。
- [ ] 验证一站多目标的全部回读、版本重查、自动确认、实际生效后清理，以及 DNS 单独运行的现有回归。
- [ ] 运行受影响包定向 race、`make all`、适用的依赖审计及 `git diff --check`，记录 Go/Node 版本、真实二进制和浏览器结果。

**证据**　待执行。

<a name="step-17"></a>

### Step 17 · 真实云验收与发布证据

依赖 Step 16。真实账号、真实写入与发布各按实际授权和可用环境执行。

**测试材料**　有付费源站防护功能的站点、按站点授权的腾讯云 CAM 凭据、所有实际源站对应的测试安全组或轻量云防火墙，以及可记录的真实待更新窗口。 [CAM 权限](https://cloud.tencent.com/document/product/598/99327) · [源站更新流程](https://cloud.tencent.com/document/product/1552/76086) · [历史生产验收记录](HistoryDocs/ProdTestList.md)

**执行顺序**

1. 先发只读 Describe，记录 `ZoneId`、RequestId、状态、当前/下一版、IPv4/IPv6 数量、端口展开后的预计规则数及配额依据。脱敏记录，凭据留在已有设置中。
2. 对照配置者提供的全部绑定清单。执行真实增量写入并读回所有目标，再观察实际确认与生效顺序。存在待更新版本时单独取证新旧并存与旧网段清理，并记录只读查询能否提供足以判明 `unknown` 的字段证据。
3. 校验发布时的目标架构、镜像摘要与 CI。真实站点无待更新版本时，当前版写入与确认/生效场景分别记录。

**证据格式**

| 类别 | 最少记录 |
|---|---|
| 真实查询 | 日期、站点、API 动作、RequestId、状态、CIDR 数量、版本。 |
| 云目标 | 平台、脱敏目标、变更前/后数量、回读覆盖、失败原因或容量。 |
| 真实确认 | 站点、待确认版本、确认 RequestId、实际生效观测、清理结果。 |
| 发布 | tag、commit、Actions 链接、目标架构、镜像摘要与实物检查；未执行项如实留待。 |

**Checklist**

- [ ] 用付费站点验证只读权限、源站防护状态、实际 CIDR、待更新版字段及目标配额，记录可用的 `RequestId` 和脱敏响应证据。
- [ ] 以明确纳入的真实测试源站和安全组核验一站多目标、实际写入和回读、确认与生效时序。无待更新版本时记录该场景未覆盖。
- [ ] 更新 README、生产验收清单与阶段实施记录；逐项区分本地模拟、真实云和远端发布证据。
- [ ] 如执行发布，记录 tag/提交、远端 Actions、目标架构和实物镜像；待补项目按实际状态登记。

**证据**　待执行。
