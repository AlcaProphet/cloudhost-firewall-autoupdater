# Build8 — EdgeOne 回源同步构建追踪

> 2026-11-12 · 工作分支 `beta` · 起始基线 `102d838f3bcc3d1624c32e44ed65f2eb6feaa0b4`。
> 设计与已确认行为见 [Design6](Design6.md)。当前编码约束见 [AGENTS.md](AGENTS.md)。各 Step 的实施状态见[进度索引](#进度索引)。

## 使用方式

按 Step 列出的依赖推进。一个 Step 只收口一个可单独验证的改动范围。完成当步 Checklist 并填写实际证据后，再勾选进度索引。证据记录日期、提交或工作树状态、执行命令与结果、实际覆盖及待补项。模拟、真实云和发布分别标记。实现若需要变更现有合同，在对应 Step 同步 `AGENTS.md` 中涉及的条款。

<a name="execution-rules"></a>

### 执行规则与方案变更

1. **开工**　确认本轮任务指向的 Step，读取 [Design6 冻结合同](Design6.md#frozen-contract)、该 Step、直接依赖和相关 [AGENTS.md](AGENTS.md) 条款。记录当前分支、HEAD、工作区和依赖证据。依赖仍未完成时，报告实际缺口，已可进行的只读核对和夹具准备可继续。现有文档本身不代表已经开工、真实写入或发布。
2. **控制范围**　按当步列出的主改动面推进。必要的同层接线、回归夹具和当步证据随 Step 提交。把后续 Step 的代码留到对应范围。文件拆分和私有类型可随现有代码调整，影响公开 API、Schema、默认值、定时、失效处理或云写入授权的变更按下一条处理。
3. **处理偏差**　设计示例与真实代码有小差异时，以冻结行为为验收目标，记录实际接线。遇到真实 SDK/Provider 契约缺口或需要改变已冻结行为，写明原规则、证据、建议修订及影响的 Step，取得用户对该差异的决定后再实施相关部分。保留已确认方案和已接受的运行取舍，避免另设部署方式、自动清理策略或必选人工操作。
4. **收口**　先跑当步最小验证，再按影响面跑相关基线。正向与反向用例使用可区分的夹具，涉及云操作用实际调用计数和快照核对。检查工作区差异和 `git diff --check`，记录实际状态。测试没有执行、被跳过或无有效匹配时保留待验项。统一的全仓门禁、真实云和发布证据分别由 Step 16 与 17 收口。

### 阶段接线边界

| 阶段 | 当段可验收 | 后续由哪步接入 |
|---|---|---|
| Step 1～4 | 迁移、设置、站点 CRUD、完整备份与事务发布 | Step 8 接入正式调度，Step 13 接入配置界面。 |
| Step 5～7 | SDK 只读、纯规划、冲突决策与 SQLite 记录 | Step 8 接入正式查询和预检，Step 9 接入目标执行。此段以模拟请求和夹具验收。 |
| Step 8～10 | 正式准入、目标增量写入和站点确认链路 | 用本地替身验证写入、回读、确认。实际账号端到端验收留 Step 17。 |
| Step 11～15 | 过渡保护与清理、只读模拟、WebUI、运行健康 | Step 11 完整接入保护前缀；Step 12 验证正式和模拟的一致性；Step 16 收口完整链路。 |
| Step 16～17 | 本地完整验收、真实站点证据与远端发布 | 本地结果、真实云和发布分别标记。无待更新窗口时保留相应的外部验收项。 |

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
- [x] [Step 1 DNS 默认 TAG 和旧库迁移](#step-1)
- [x] [Step 2 双功能开关与间隔设置](#step-2)
- [x] [Step 3 站点与目标绑定 API](#step-3)
- [x] [Step 4 配置包往返与兼容](#step-4)
- [x] [Step 5 TEO 只读查询](#step-5)
- [x] [Step 6 CIDR 规划与 TAG 隔离](#step-6)
- [x] [Step 7 全局重叠判定和自动恢复状态](#step-7)
- [x] [Step 8 独立调度与正式入口](#step-8)
- [x] [Step 9 目标写入与回读](#step-9)
- [x] [Step 10 站点自动确认](#step-10)
- [ ] [Step 11 版本切换与旧网段清理](#step-11)
- [ ] [Step 12 模拟测试 API](#step-12)
- [ ] [Step 13 全局设置和 EdgeOne 页面](#step-13)
- [ ] [Step 14 模拟测试与仪表盘界面](#step-14)
- [ ] [Step 15 运行健康与日志](#step-15)
- [ ] [Step 16 本地端到端验收](#step-16)
- [ ] [Step 17 真实云验收与发布证据](#step-17)

### 当前进度（2026-11-13）

Step 0～7 的实施及核验提交见 `cbec01b`、`1bcfbaa`、`14642a6`、`9df4e19`、`8efbda8`。Step 8～10 在 `beta` 的实施基线为 `76241cc`，首次独立核验修复为 `2c4653d`。本次复核发现并修复查询期间缓存失效和旧代次确认回包两处问题，证据见下方。Step 0～10 已达到本地阶段判据，共 11/18 步；Step 11～17 待执行。真实云与发布验收留 Step 17。

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

**方案冻结复核**　从文档提交 `ab31d04` 细化执行规则、逐步边界和收口判据。验证 139 个本地链接、18 个 Step、17 组改动边界和完成判据、11 行预检决策与 12 项跨步验收用例。提取预检 Go 示例并按冻结的入口门控复核 12 个用例。以上是文档与参考代码复核，实施 Checklist 保持原状态。

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
| 老库已有 `" my-dns "` | 首次打开和重启的有效值均为 `my-dns`，数据库原值保留空白，此前 `[my-dns]` 规则仍可识别。 |
| 已迁移库 reset、重启；迁移中注入写入失败 | reset 后稳定使用 `auto-ddns`；迁移失败时原设置和迁移标记都未被部分提交。 |

使用 `config/store_v3_test.go` 的旧库构造方式和 `TestConfigExportEmptyDatabaseSchema` 的导出断言方式。建议运行 `go test ./config ./webui/api -race -count=1`，证据包含数据库实际值及 API 回显。

**本步改动边界**　主改动在 `config/config.go`、`config/store.go`、`webui/api/settings.go`、受影响的默认值测试与 README 默认值示例。备份字段、总开关和 DNS 同步流程留在原行为。

**收口判据**　从同一旧库分别走启动加载、设置 GET、配置导出（Step 4 起为 v4），三者看到的 TAG 必须相同。新库首次启动与 reset 后重启均返回 `auto-ddns`。迁移失败注入点放在写旧 TAG 后、提交前，以原值与迁移标记同时回滚为准。

**Checklist**

- [x] 新库及 reset 后使用 `auto-ddns`。共享默认值用于运行快照和设置回显，更新当前用户文档中的默认示例。
- [x] 在建表迁移事务内区分新库和旧库，保留旧库显式 TAG，并为原本靠隐式 `auto-dns` 运行的旧库写入原有效值。验证重复启动与 reset 不会重复迁移。
- [x] 用新库、缺键旧库、空白键旧库、显式自定义 TAG、迁移失败回滚验证行为。对照云规则描述，旧 TAG 的所有权保持可判定。

**证据**　2026-11-12，`beta`，实施前 HEAD `0cd3a98`，工作树改动（本步未提交）。实际改动：`config/config.go` 新增共享 `DefaultDNSTag=auto-ddns` 与仅迁移用的 `LegacyDefaultDNSTag=auto-dns`；`config/store.go` 在 `initTables` 事务内、建表前读取 `settings` 表是否存在与 `PRAGMA user_version`，`migrateDataTx` 对已初始化旧库的缺键/空白 `tag` 显式写入 `auto-dns`，并把 `user_version=1` 与建表、数据写入同事务提交；`normalizeSettings` 与 `webui/api/settings.go` 的默认回显统一使用共享默认值；`README.md` 两处当前默认示例改为 `auto-ddns`。新增 `config/store_dns_tag_test.go`（新库、缺键/空白旧库、显式 `my-dns`、注入 trigger 使写 TAG 失败、reset 后重启）与 `webui/api/dns_tag_test.go`（同一旧库的运行快照、GET 设置、导出三处一致，新库与 reset/重启一致）。

执行命令与结果：`go test ./config -run 'TestNewStoreUsesAutoDDNSAndMarksMigration|TestLegacyMissingOrBlankDNSTagMigratesOnce|TestLegacyExplicitDNSTagPreserved|TestLegacyDNSTagMigrationFailureRollsBack|TestResetKeepsMigrationMarkerAndUsesAutoDDNS' -race -count=1 -v` → 5 个顶层用例及缺键/空白两个子用例全部 PASS（config 1.558s）；`go test ./webui/api -run 'TestDNSTagSources' -race -count=1 -v` → 3 个用例全部 PASS（api 1.461s）；`go test ./config ./webui/api -race -count=1 -timeout=10m` → 两包均 ok（config 34.851s，api 43.482s）。数据库实际值断言包括：新库无显式 `tag` 行且 `user_version=1`；缺键/空白旧库首次打开后 `tag=auto-dns`、`user_version=1`；第二次打开保持 `auto-dns`，删除该行后再打开得到 `auto-ddns`（证明迁移不重放）；显式值 `" my-dns "` 原样保留、运行快照为 `my-dns`，`[my-dns] 生产API` 可由 `internal/tag` 判定所有权；注入 trigger 失败后 `tag` 行、`user_version`、`alert_email`/`targets` 新表均未部分提交，移除故障后可重新完成迁移；reset 后 `user_version` 保留且运行快照、GET、导出均为 `auto-ddns`。既有 v3 导出仍为 version 3，备份字段与总开关未改（该句描述 Step 1 当时状态；Step 4 已按后续合同改为导出 v4；本步实施时迁移版本为 1，Step 2 提升为 2，见下一步证据）。

待补项：本步未执行真实云规则核对、数据库跨版本文件互操作、Linux/Docker 与远端 CI；这些不属于 Step 1 的本地收口范围。`git diff --check` 在文档回填后随 Step 2 一并复查。

<a name="step-2"></a>

### Step 2 · 双功能开关与间隔设置

依赖 Step 1。范围为后端设置、严格校验及配置发布。

**代码入口**　`settingsKeysV3` 所在的配置键集合、`BusinessSnapshot.ToRuntimeConfig`、`RuntimeConfig.DeepCopy`、`settingsPatch.values`、`handlePutSettings` 和 `ConfigCoordinator.Mutate`。 [运行配置](config/runtime.go) · [设置 API](webui/api/settings.go) · [接口和示例](Design6.md#api-reference)

**实施顺序**

1. 保留 `sync_enabled` 保留键，增加 `dns_enabled`、`edgeone_enabled`、`edgeone_tag`、`edgeone_interval` 的已知键、默认值与快照字段。不要将 v3 备份字段集合误扩展为要求 v3 带新字段。
2. GET 保持字符串映射。PUT 区分四个新增字段的省略、字符串和显式 `null`，任一 `null` 返回 400。复用现有布尔文本、TAG 及正时长校验。运行时使用解析后的时长，回显和备份保留文本。
3. 把两个 TAG 的比较放在事务内完整合并之后。请求只提交一项 TAG 时仍能判定与当前另一个 TAG 是否相同。验证并发设置与失败回滚。
4. 把迁移、v3 转换和明确用户设置分开。历史 DNS TAG 已为 `edgeone` 时新模块初始化 `edgeone-origin`，普通保存与 v4 遇到重名直接返回校验错误。

**最小测试矩阵**　开关字符串 `"false"` 生效；未知键和 `sync_enabled` 的普通 PUT 仍被拒绝；`36h`、`1h30m`、`1.5h` 正确解析，空白、`0s`、负值及错误类型失败。覆盖「同时换两个 TAG，最终值不相同」成功，以及「只改其中一个与另一项冲突」事务零写入、零 Apply。基于 `TestPutSettings_PartialUpdate` 和配置协调器夹具执行 `go test ./config ./webui/api -race -count=1`。

**本步改动边界**　主改动在 `config/runtime.go`、`config/store.go`、`config/validate.go` 和 `webui/api/settings.go`。只发布配置字段，正式调度接线由 Step 8 处理，配置包版本由 Step 4 处理。

**收口判据**　先保存 DNS TAG 为 `alpha`、EdgeOne TAG 为 `beta`，单独把前者改为 `beta` 应返回 400，原两键及运行快照保持；同一请求把两个键换成不同值应一次提交并发布。以 `time.Duration` 等价值比较调度间隔，保存同值和重新计时的行为留给 Step 8 证明。

**Checklist**

- [x] 保留 `sync_enabled` 总暂停，新增独立 DNS 与 EdgeOne 开关，并把它们放入同一不可变运行时快照。默认 DNS 开启、EdgeOne 关闭。
- [x] 增加可自定义 EdgeOne TAG 和同步间隔，默认分别为 `edgeone` 和 `72h`。EdgeOne 间隔复用现有正时长解析，与 DNS 间隔分别保存和回显。
- [x] 校验去除首尾空白后 TAG 互斥。兼容 DNS TAG 已为 `edgeone` 的旧配置，给新模块初始化独立值。
- [x] 覆盖部分更新、同时改两个 TAG、非法时长、重名、回滚零发布和正确提交只发布一次。

**证据**　2026-11-12，`beta`，实施前 HEAD `0cd3a98`，Step 1 工作树基础上继续（本步未提交）。实际改动：`config/config.go` 新增 `DefaultEdgeOneTag=edgeone`、`LegacyEdgeOneTag=edgeone-origin`、`DefaultEdgeOneIntervalText=72h`/`DefaultEdgeOneInterval=72h`；`config/validate.go` 新增功能开关字符串校验、`edgeone_tag` 字段化 TAG 校验与 `DefaultEdgeOneTagForDNS` 兼容选择；`config/runtime.go` 保持 `settingsKeysV3` 12 键不变，新增当前 16 键 `settingsKeysCurrent`，`RuntimeConfig` 增加 `DNSEnabled`、`EdgeOneEnabled`、`EdgeOneTag`、`EdgeOneInterval`；`config/store.go` 的迁移版本提升为 2，旧库缺键/空白 `edgeone_tag` 仅在 DNS TAG 恰为 `edgeone` 时显式写入 `edgeone-origin`（显式值保留、只执行一次），`normalizeSettings` 补四项默认值与校验，并在全部键合并后按 Trim 后大小写敏感比较两个 TAG；`webui/api/settings.go` 的 GET/PUT 扩展到 15 个字符串键（仍不接受 `sync_enabled`），PUT 每次保存后由协调器在同一事务内读取完整候选快照，TAG 重名时回滚并返回 400；`webui/api/bundle_v3.go` 的 v3 兼容转换补 `dns_enabled=true`、`edgeone_enabled=false`、按 DNS TAG 选择 EdgeOne TAG、`edgeone_interval=72h`，v3 DTO 字段集合本身未扩展。

执行命令与结果：`go test ./config -run 'TestNewStoreEdgeOneDefaultsAndRuntime|TestLegacyDNSConflictBootstrapsEdgeOneTagOnce|TestLegacyEdgeOneTagExplicitPreserved|TestEdgeOneTagConflictRejectedBySnapshot|TestNormalizeSettingsRejectsInvalidFeatureValues|TestDefaultEdgeOneTagForDNS' -race -count=1 -v` → 6 个顶层用例全部 PASS；`go test ./webui/api -run 'TestPutSettingsFeatureFieldsAndRuntime|TestPutSettingsTagConflictAfterMerge|TestPutSettingsConcurrentTagChangesSerialized|TestV3ImportBootstrapsEdgeOneTag|TestPutSettingsRejectsUnknownAfterFeatureExpansion' -race -count=1 -v` → 5 个顶层用例及 9 个非法值/4 个冲突子用例全部 PASS；`go test ./config ./webui/api -race -count=1 -timeout=10m` → 两包均 ok（config 35.074s，api 42.114s；另一次组合运行 config 34.967s、api 43.696s）。关键断言：新库/迁移库运行快照为 DNS 开启、EdgeOne 关闭、`edgeone`/`72h`；旧库 DNS TAG 为 `edgeone` 时首次升级只写 `edgeone-origin`，重复启动不重写，显式改值后重启保留；`dns_enabled=false`、`edgeone_enabled=true`、`36h`/`1h30m`/`1.5h` 正确进入运行时；空白、`0s`、负值、错误类型均 400 且零发布；先存 `alpha`/`beta` 后单独改 `tag=beta` 或 `edgeone_tag=alpha` 均 400，数据库与运行快照保持，同一请求交换两项成功且只发布一次；两个并发请求分别写两个 TAG 为 `gamma` 时恰好一成一败、只多一次发布；v3 导入保留 `tag=edgeone` 并补 `edgeone_tag=edgeone-origin`，普通 TAG 补 `edgeone`。

后续接线：[Step 4](#step-4) 已接入 v4 配置包字段与文件往返，正式调度留 [Step 8](#step-8)。Step 1 证据中记录的 `user_version=1` 是本步实施前的版本。Step 2 将迁移版本提升为 2，Step 1 的迁移、回滚和 reset 断言在两版代码上均通过，旧 TAG 迁移只执行一次。

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

**本步改动边界**　主改动在 `config` 的站点领域与 Store、`webui/api/deps.go` 注册和站点/目标 handler。站点 API 不同步查询云，建立绑定也不发防火墙请求。前端由 Step 13 接入。

**收口判据**　创建站点返回 201；重复站点 409；空绑定、悬空 ID、同实际资源别名、非法端口项或混入 `ALL` 的列表返回 400；更新/删除缺失站点返回 404；删被绑定目标返回 409。对每个拒绝例检查事务前后完整快照和 Apply 次数。用同一站点两目标的成功路径核对外键语义与排序。

**Checklist**

- [x] 保存唯一 `ZoneId`、目标列表及入站协议、端口、地址族，复用现有目标 ID 与规则字段校验。
- [x] 实现站点读取、创建、编辑和删除，随事务发布完整运行快照。配置保存阶段只做本地校验，首轮查询在发布后调度。
- [x] 检查目标存在性、重复绑定、实际资源别名和引用闭包。被 EdgeOne 站点引用的目标删除返回 409。
- [x] 覆盖一站多目标、多个站点共用目标、reset、引用保护及无效写入的事务回滚。

**证据**　2026-11-12，`beta`，Step 2 工作树基础上继续（本步未提交）。实际改动：新增 `config/edgeone.go`（`EdgeOneSite`、`NormalizeZoneID`/`NormalizeEdgeOneSite`、端口表达式严格校验、`TargetIdentity`、站点深拷贝）与 `config/store_edgeone.go`（站点/绑定读取、增删改、目标引用查询、站内存在性/别名校验、完整快照闭包校验）；`config/store.go` 增加 `edgeone_sites`、`edgeone_site_targets` 与目标索引 DDL，站点读取接入 `LoadBusinessSnapshotTx`，reset 与导入清理按「绑定 → 站点 → 目标/规则」顺序执行；`config/runtime.go` 的 `BusinessSnapshot`/`RuntimeConfig` 增加 `EdgeOneSites` 并深拷贝；新增 `webui/api/edgeone.go` 与 `deps.go` 路由（`GET/POST /api/edgeone/sites`、`PUT/DELETE /api/edgeone/sites/{zone_id}`），`handleDeleteTarget` 增加站点引用 409 保护。站点动作不进入请求或数据库，按合同由后端固定 `ACCEPT`；本步没有任何腾讯云查询或防火墙调用。

执行命令与结果：`go test ./config -run 'TestNormalizeEdgeOneSite|TestEdgeOneSite' -race -count=1 -v` → 6 个顶层用例及 17 个站点参数/端口反向子用例全部 PASS；`go test ./webui/api -run 'TestEdgeOne|TestTargetUpdateAlias' -race -count=1 -v` → 8 个顶层用例（含核验后补入的更新回滚与持久化空绑定错误分类）、15 个非法创建子用例与 4 个非法更新子用例全部 PASS；`go test ./config ./webui/api -race -count=1 -timeout=10m` → 两包均 ok（config 35.896s，api 47.007s）。关键断言：空列表为 `{"sites":[]}`；创建 201、重复 409、缺失更新/删除 404、路径与请求体 `zone_id` 不一致 400；空绑定、缺失 `target_ids`、重复本地 ID、悬空 ID、同实际资源别名、非法端口/协议/地址族、ZoneId 路径分隔符、请求体出现 `action` 均 400 且零发布零写库；合法创建/更新/删除各只发布一次；一站多目标按 `target_id`、站点按 `zone_id` 稳定排序且运行时快照独立深拷贝；两个站点可共享同一目标；非法更新保留旧绑定；绑定写入注入 trigger 失败时站点主行与绑定一起回滚且零发布；把目标改成同站点另一目标的实际资源返回 400 并回滚，改为新资源后成功；删除仍被站点绑定的目标返回 409，删除站点后目标可删除；reset 清空站点、绑定与运行时快照。

后续接线：[Step 5](#step-5) 已提供 TEO 只读客户端。正式调度、站点运行状态和过渡记录、前端站点配置页分别留 [Step 8](#step-8)、[Step 10～11](#step-10) 和 [Step 13](#step-13)。真实云、Linux/Docker 或远端 CI 无本步验收证据。

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

**本步改动边界**　主改动在 `webui/api` 的 bundle/导入/导出、Store 完整替换路径、前端下载名回退和相应兼容条款。站点运行记录的实际加载和写入分别在 Step 7、10、11 继续接线。v3 原 DTO 严格语义继续可测。

**收口判据**　保持同一份原始 v3 夹具可以导入并返回指定旧 TAG；v4 新字段缺失或错误时返回 400 且 Apply 为零。导出端和导入端目标 ID 分别用不同的自增区间，比较实际云资源和绑定等价性。导出 JSON 中 `edgeone_sites` 空值为 `[]`，运行态表字段无输出。

**Checklist**

- [x] 实现严格 v4 导出及导入，纳入两项设置和站点绑定，并随版本更新下载名和相关说明。
- [x] 维持严格 v3 导入，保留原 DNS TAG、间隔和总暂停，填入 EdgeOne 关闭且无站点的默认状态。v1、v2 和未知版本继续拒绝。
- [x] 使用新旧 ID 不同的 SQLite 夹具验证一站多目标的 `export_id` 重映射，以及重名、悬空引用、坏载荷零写入和事务失败零发布。
- [x] 覆盖同步日志、告警和运行状态的备份边界，同步 [AGENTS.md 配置合同](AGENTS.md#九配置约束)。

**证据**　2026-11-12，`beta`，Step 3 工作树基础上继续（本步未提交）。实际改动：`webui/api/bundle_v3.go` 保留 v3 wire DTO，导出 DTO 升级为 `bundleV4`/`bundleV4Settings`/`bundleV4Site`，`toBundleV4` 输出 version 4、`edgeone_sites`、两个功能开关和两个 TAG/间隔；新增 `webui/api/bundle_v4.go` 的 v4 严格 DTO、presence 顺序（`metadata → targets → rules → edgeone_sites → settings → alerts → monitoring`）、导出 ID 唯一性、站点目标闭包/本地重复/实际资源别名/ZoneId 重复与 TAG 互斥校验；`webui/api/decode.go` 改为先有界读取并识别 version，再按 v3/v4 选择 DTO 严格解码（v1/v2/未知版本直接 400，保留大小写字段的未知字段诊断）；`webui/api/export.go` 的导入事务新增站点重映射与插入，清理顺序保持「绑定 → 站点 → 规则 → 目标 → 设置 → 告警」，导出固定 v4 与 `fwalizer-config-v4-` 附件名；`webui/frontend/src/views/Settings.vue` 下载名回退改为 `fwalizer-config-v4.json`；`README.md` 与 `AGENTS.md` §9 当前版本条款更新为「导出 v4、严格接受 v3/v4」，历史 Build7/其他历史段落保留；新增前端回退行为断言。v3 导入仍走原 DTO，保留 DNS TAG、间隔和 `sync_enabled`，补 `dns_enabled=true`、`edgeone_enabled=false`、无站点，并在 DNS TAG 恰为 `edgeone` 时初始化 `edgeone-origin`。

执行命令与结果：`go test ./webui/api -run 'TestExportV4ShapeAndNoRuntimeState|TestImportV4RoundTripWithSitesAndSettings|TestImportV4ValidationZeroWriteNoApply|TestImportV4TransactionFailureRollsBack|TestImportV3PreservesLegacySettingsAndAddsNewDefaults' -race -count=1 -v` → 5 个顶层用例及 17 个 v4 负向子用例全部 PASS（api 1.823s；补入重复 export_id、缺目标数组、zone 重复后单项复跑 1.615s）；v4 负向矩阵覆盖缺 `edgeone_sites`、`edgeone_sites:null`、缺 `dns_enabled`、开关错误类型、重复 `export_id`、站点空目标/缺目标数组/重复本地目标/悬空引用/实际资源别名/非法端口/非法地址族/非法 ZoneId/zone 重复、TAG 重名、未知版本 5、未知运行字段；每个拒绝例均断言 400、旧目标/设置/站点不变、Apply 为零。v4 往返夹具在来源库与目标库预置不同自增区间，导出后目标库已有 3 条目标历史，导入后一站两目标通过 `export_id → target_id` 映射解析到 `sg-src-a`/`sg-src-b`，站点绑定的目标 ID 与来源/目标旧 ID 均不同；v4 导出原始 JSON 断言 `edgeone_sites` 非 null、空时为 `[]`、开关为 JSON boolean、运行表字段不出现；v3 原始 fixture 导入保留 `legacy-tag`/`7m`/`sync_enabled=false`，并断言 v3 混入 `edgeone_sites` 仍 400 且不发布。`go test ./... -race -count=1 -timeout=20m` → 12 包全部 ok（最终复查：app 1.160s、config 35.947s、webui/api 47.288s、syncer 21.293s、provider 8.821s 等）；`go vet ./...` → 0（最终复查同）；`npm --prefix webui/frontend test` → 154 项全部通过（含新增 B8S4 下载名回退断言）；`npm --prefix webui/frontend run build` → `vue-tsc` + Vite 构建成功（446ms）；`git diff --check` → 0（最终复查同）。既有 v3 严格解码、缺失字段、10 MiB 限制、告警/凭据/同步日志边界与事务失败回滚回归继续通过；导出仍为唯一允许敏感值的响应且保持 no-store。

后续接线：[Step 7](#step-7) 已加入 `sync_address_conflict`，并验证其排除在 v4 导出之外。`edgeone_origin_transitions` 的确认和保护记录由 [Step 10～11](#step-10) 接入，继续排除在备份之外。真实云、浏览器人工验收、Linux/Docker、依赖审计和远端 CI/GHCR 证据留 Step 16/17。`bundle_v3.go` 仍承载 v3 wire 与两版本共用的 value 类型，导出构建函数使用 v4。

**核验后修复（2026-11-12）**　独立只读核验发现并修复：① `webui/api/bundle_v4.go` 未通过 `gofmt`，已格式化；② 持久化“站点存在但目标为空”被 `NormalizeEdgeOneSite` 的 `ValidationError` 包装成 HTTP 400，已改为在归一化前返回内部完整性错误，站内实际资源别名仍保留请求侧 400；③ `TargetIdentity` 增加云类型/地域/资源 ID 去首尾空白，避免历史/外部 SQL 空白值逃过别名检测。补测试：`TestLegacyV1DBBootstrapsEdgeOneTag`（Step 1 `user_version=1` 升级路径）、`TestEdgeOneSiteUpdateRollbackOnBindingFailure`（PUT 更新绑定中途失败整体回滚）、`TestEdgeOneSitePersistedEmptyBindingReturnsInternalError`（HTTP 500 与零 Apply）、`TestImportV3ClearsExistingSites`、v4 往返预置旧站点验证替换、`TestImportV4PreservesSyncLogsAndReplacesAlerts`（同步日志/扫描缓存/告警边界）、`TestImportV4PresenceMatrix`（顶层对象、四个新设置字段、站点目标 null/元素 null）、进程级往返加入真实站点绑定与目标重映射；`TestEdgeOneSiteEmptyBindingRejected` 增加 `!errors.As(..., *ValidationError)` 判别。

核验后门禁：`gofmt -l config webui/api main_test.go` 无输出；`go test ./config ./webui/api -race -count=1 -timeout=10m` 两包 ok；`go test ./... -race -count=1 -timeout=20m` 12 包全部 ok（root 21.878s、config 36.015s、webui/api 45.690s 等）；`go vet ./...`、`go build ./...`、`npm --prefix webui/frontend test`（154 项）、`npm --prefix webui/frontend run build`、`git diff --check` 通过。本批未执行真实云、浏览器人工验收、Linux/Docker、依赖审计与远端 CI/GHCR。该批核验修复已提交为 `1bcfbaa`。`cbec01b` 的提交说明“共 30 文件”与实际 `git show --stat cbec01b` 的 38 文件不一致，属提交元数据记录偏差。

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

**本步改动边界**　主改动在 TEO 客户端、`go.mod`、`go.sum` 和本地 SDK 夹具。保留既有客户端池职责和四平台防火墙接口。请求模型只接入 Describe，确认由 Step 10 处理。

**收口判据**　构造客户端调用计数为零。调用适配器时捕获 HTTPS endpoint 配置、实际 POST 和 `ZoneId`；模拟两目标共享一次站点结果。当前版缺失、字段族错误或超时返回错误，并证明它们不会被转换成可清理的空集合。

**Checklist**

- [x] 核对 SDK 模块与现有腾讯云 `common` 依赖，配置 endpoint、请求超时、频率限制和安全日志。
- [x] 解析源站防护状态、当前及待更新版本、计划生效时间和两类 CIDR，规范地址族、掩码及去重。
- [x] 验证一个站点的结果可供多个目标复用，区分明确无下一版、空或畸形数据、权限错误、限流和超时。
- [x] 用模拟服务核对 POST、Action、版本、`ZoneId`、请求签名接线和 `RequestId`，记录真实响应仍待 Step 17 核验。

**证据**　2026-11-12，`beta`，实施前 HEAD `1bcfbaa`，工作树改动（本步未提交）。实际改动：`go.mod` 增加直接依赖 `github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/teo v1.3.186`，`go.sum` 增加该模块与 `common v1.3.186/go.mod` 三行校验和；`provider/teo.go` 新增 TEO 只读适配（`TEOClient`、`OriginACLVersion`、`OriginACLSnapshot`、安全错误分类），`provider/teo_test.go` 新增本地 SDK 回归；本文件回填证据。

依赖核对：`teo v1.3.186` 的 `go.mod` 要求 `common v1.3.186`，项目现有 `common` 仍为 `v1.3.192`，MVS 取较高版本，`lighthouse v1.3.182`、`vpc v1.3.188` 与 `common v1.3.192` 未变化；`go list -m all | grep tencentcloud` 实际输出四种模块版本。`go mod tidy`（`GOPROXY=https://goproxy.cn`，随环境网络可达镜像）把 `teo` 从临时 indirect 归位到直接 require，未改动任何既有依赖版本。适配层复用 `ClientPool` 的不可变腾讯云凭据与缓存：生产 endpoint `https://teo.tencentcloudapi.com`（`profile.HttpProfile.Scheme=HTTPS`），单次 `DescribeOriginACLWithContext` 使用调用方 context 叠加 30s 本地预算，查询/确认共用 50ms 最小间隔（20 次/秒），显式关闭 SDK 内部网络与限流重试；构造只调用 `teo.NewClient`，零网络请求。当前只接 `DescribeOriginACL`，`ConfirmOriginACLUpdate` 留给 Step 10。客户端包装对象按 `pool + endpoint + scheme + account` 缓存，使同一实例的限速状态跨调用保留。

转换与安全边界：只接受 `Status=online/updating`；`offline`/空状态返回 `disabled` 或 `incomplete`。当前版必须存在且带版本号；待更新版必须带版本号、可解析的计划生效时间与 `EntireAddresses`。IPv4/IPv6 字段分别校验实际地址族，任何非法 CIDR、空地址项或字段错配整份返回 `malformed`，不保留部分结果；单个地址族显式为空且另一族有效视为合法；两个地址族都没有网段返回 `empty`，字段整体缺失返回 `incomplete`。前缀统一 `Masked`、去重并按 IPv4 → IPv6 字符串稳定排序，`AllPrefixes` 合并当前版与待更新版，`PrefixesForFamily` 支持 `ipv4/ipv6/dual`。`TEOError` 只保留分类、云错误码、`RequestId` 与固定中文文案，`Error()` 不回显 SDK 原始 Message、请求体、签名或凭据；底层错误仅经 `Unwrap` 保留。

执行命令与结果：`go test ./provider -run 'TestTEO|TestOriginACL|TestParseTEO' -race -count=1 -timeout 60s` → ok（1.829s，`TestTEO*`、`TestOriginACL*`、`TestParseTEO*` 全部通过）；`go test ./provider -race -count=1 -timeout 10m` → ok（9.227s）；`go vet ./provider` → 无输出；`gofmt -l provider/teo.go provider/teo_test.go` 与 `git diff --check` → 无输出。

httptest 真实 SDK 证据：`TestTEODescribeOriginACLPostsSignedRequest` 经真实 `teo` SDK 发往本地模拟服务，断言 POST `/`、`X-TC-Action=DescribeOriginACL`、`X-TC-Version=2022-09-01`、`Content-Type=application/json`、JSON 仅含 `ZoneId=zone-test`、无 `X-TC-Region`，并用虚构密钥重算 `TC3-HMAC-SHA256` 的 canonical request/string-to-sign/signature 与 `Credential` scope 完全匹配，同时捕获 `RequestId=req-success`、当前版 `2026-01`、待更新版 `2026-02`、计划时间 `2026-11-30T16:00:00Z` 与规范化前缀。`TestTEOClientConstructionMakesNoRequest` 证明构造（含同 pool 第二次构造）请求数为 0，实际查询后为 1。`TestTEOOneSnapshotSharedByMultipleTargets` 用一次查询结果服务两个目标，请求数保持 1。`TestTEORateLimitAndContextCancel` 验证 3 次查询至少跨两个 25ms 槽位、预留远期槽位时 30ms context 超时且零新增请求。

反向与分类证据：`TestTEOResponseConversionMatrix` 覆盖 21 个子场景：无下一版、IPv4/IPv6 单族合法为空、两族整体为空（`empty`）、地址族字段缺失/`null`（`incomplete`）、非法 CIDR/空项/两向地址族错配（`malformed`）、`OriginACLInfo` 缺失、空响应、`offline`、状态/版本号/计划时间缺失、待更新版整体为空、主机位掩码化与去重、`updating` 并集；所有错误路径均断言绝不返回部分快照。`TestTEOErrorClassificationAndNoSDKRetry` 用模拟权限错误与限流响应断言 `permission`/`throttled`、`RequestId`/Code 保留、原始 Message 与虚构密钥不回显、请求数各为 1（无 SDK 内部重试）；请求预算超时用延迟 200ms 的本地服务与 50ms 预算断言 `timeout` 且请求已到达。`TestTEOInvalidZoneNeverBecomesEmptySnapshot` 对空/空白 `ZoneId` 断言 `malformed`、零请求、空快照，证明无效输入不会变成可用于清理的空集合。`TestOriginACLSnapshotPrefixesForFamily` 与 `TestParseTEOPlanTime` 覆盖并集/地址族筛选与四种时间文本。

外部验收状态：本步使用 `httptest` 与虚构凭据。真实账号权限、真实待更新窗口、响应形状、计划时间时区与实际限流配额留 Step 17，包含 `CurrentOriginACL`/`NextOriginACL` 字段组合、`Status=updating` 时的地址完整性及 20 次/秒限流实测。定期查询及调度状态留 Step 8，`ConfirmOriginACLUpdate` 与站点状态留 Step 10，运行健康与日志留 Step 15。

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

**本步改动边界**　主改动在 `provider/plan.go` 的输入、来源和覆盖分类及其测试。复用端口展开和平台能力，保留 `Provider` 接口和云删除定位，不在纯规划器里执行 RPC 或配置写入。

**收口判据**　对同一目标给两个站点相同 `/24 TCP 443`，断言一条新增、两条来源；再以只有另一功能 TAG 的完全等价云规则作反例，断言该 key 无覆盖、报告目标级冲突且无改标动作。手工等价正向对照及现有 DNS 的 canonical key 保持。

**Checklist**

- [x] 规划输入保留 EdgeOne CIDR 掩码，同时保持 DNS 单地址转换与现有端口展开、功能 key、能力矩阵。
- [x] 在实际目标上合并所有适用站点，按功能去重，按 TAG 渲染新增规则与确定删除归属。
- [x] 另一功能 TAG 的等价规则只作为目标级冲突，手工规则继续按现有精确等价判定。覆盖声明需区分冲突、平台不支持和可实施子集。
- [x] 定向测试跨站点同目标、双栈、另一 TAG 残留、相反动作、模板导致保守清理、规则容量与 DNS 既有结果。

**证据**　2026-11-12，`beta`，实施前 HEAD `1bcfbaa`，工作树改动（本步未提交）。实际改动：`provider/plan.go` 扩展纯规划输入/来源/覆盖分类；新增 `provider/plan_edgeone_test.go`；本文件回填证据。规划器仍不访问网络、数据库、时钟、日志或事件总线，不执行 RPC 或配置写入。

输入与来源：`TargetPlanInput` 新增 `TargetID`、`OtherTag`、`EdgeOneSites`（`EdgeOneSitePlanInput`：`Site`、`Snapshot`、`UnavailableReason`）；`PlannedRule` 新增 `SiteIDs`，`TargetPlan` 新增 `EdgeOneErrors`。`TargetID` 非零时只纳入 `Site.TargetIDs` 包含它的站点；新增 `SelectEdgeOneSitesForIdentity` 复用既有 `config.TargetIdentity`，把 `targets` 的本地 ID 映射到 `cloud_type + region + resource_id`，归并指向同一物理目标的别名记录后一次性传入；`EdgeOneSites` 为空时完全保持既有 DNS 规划。EdgeOne 期望由站点 `Current` 与 `Next` 的并集生成，经 `PrefixesForFamily` 按 `ipv4/ipv6/dual` 选择，前缀保留网段掩码；DNS 仍经 `CanonicalCIDR` 生成 `/32` 或 `/128`。端口展开复用 `expandPlannedPorts`/`ExpandPorts`，协议复用 `canonicalProtocol`，能力复用 `Capabilities`，TAG 渲染复用 `RenderDescription`/`TruncateDescription`；EdgeOne 动作固定 `ACCEPT`。同一 `FunctionalKey` 只保留一条 `Desired`，多站点来源合并进 `SiteIDs`，DNS 来源保留 `RuleIDs`；同 key 的 DNS 与 EdgeOne 来源合并为一条新增。`SiteIDs`/`RuleIDs` 始终为非 null 数组，按稳定顺序输出。

覆盖与冲突分类：云端规则索引拆为当前 TAG（`ownedIndex`）、另一功能 TAG（`otherTagIndex`）和普通手工规则（`externalIndex`）。当前 TAG 的精确 key 计入 `SatisfiedByOwned`；普通手工规则继续按精确等价计入 `SatisfiedByExternal`；另一功能 TAG 的精确等价规则只生成 `IssueCrossTagEquivalent` 目标级冲突，不计覆盖、不进入 `ToAdd`、不进入清理候选、不产生改标动作。同一 desired key 即使已有当前 TAG 覆盖，只要另一功能 TAG 存在等价规则也会同时报告冲突并冻结清理。`CoverageReady` 继续要求非空可实施子集全部被精确覆盖，并在 `EdgeOneErrors` 非空时强制为 false；`CleanupDeferred` 增加 `edgeone_unavailable` 门，平台不支持、相反动作、模板/畸形规则、快照版本与删除定位合同保持原样。

执行命令与结果：`go test ./provider -run 'TestEdgeOnePlan|TestDNSPlanArrays|TestSelectEdgeOneSitesForIdentity|TestPlan_ExternalExactMatchOnly|TestPlan_CoverageReadyImplementableSubset' -race -count=1 -v -timeout 120s` → 新增 12 个顶层用例与 10 个既有覆盖子场景全部 PASS；`go test ./provider -race -count=1 -timeout 15m` → ok（核验修复后 9.362s）。`gofmt -l provider/plan.go provider/plan_edgeone_test.go` 与 `git diff --check` → 无输出。

判别性证据：`TestSelectEdgeOneSitesForIdentityMergesAliases` 用两个本地 ID 指向同一 `lh-1` 实际目标（含首尾空白历史值）证明别名站点被归并、另一个 `lh-2` 站点被排除；`TestEdgeOnePlanTwoSitesSharePrefixProduceOneAdd` 让同目标两个站点共享 `203.0.113.0/24 TCP 443`，断言只产生一条 `Desired`/一条 `ToAdd` 且 `SiteIDs=[zone-a zone-b]`，同时排除只绑定其他目标的 `zone-other`；`TestEdgeOnePlanCurrentAndNextSamePrefixOneAdd` 验证同站点两版本共同 CIDR 去重、并集新增；`TestEdgeOnePlanPreservesMaskWhileDNSHostPrefix` 验证 DNS `203.0.113.7/32` 与 EdgeOne `203.0.113.0/24` 各自粒度；`TestEdgeOnePlanSharedKeyMergesDNSAndSite` 验证同一 key 只新增一条并保留 `RuleIDs=[11]` 与 `SiteIDs=[zone-a]`。`TestEdgeOnePlanCrossTagEquivalentIsConflictOnly` 用只有 `[auto-dns]` 等价规则的反例断言无覆盖、无新增、无清理、无改标且存在 `cross_tag_equivalent` 冲突；`TestEdgeOnePlanManualExactMatchStillCovers` 对照手工精确规则可覆盖、不同端口不覆盖；`TestEdgeOnePlanIPv6OnSWASUnsupported` 覆盖 IPv6/SWAS 能力矩阵与清理冻结；`TestEdgeOnePlanOppositeActionAndTemplateFreezeCleanup` 覆盖相反动作、模板/畸形规则与可删除集合为空；`TestEdgeOnePlanUnavailableSiteNeverBecomesEmpty` 覆盖查询失败、零值/半截快照、非法 `netip.Prefix` 与非 nil 空 `Next` 不被当作合法空集或新增来源；`TestEdgeOnePlanTCPUDPSplitAndPortExpansion` 覆盖 Lighthouse TCP+UDP 拆分、SWAS 原生化与 CVM 多端口展开；`TestDNSPlanArraysRemainNonNull` 保护 DNS 既有数组语义。既有 `TestPlan_ExternalExactMatchOnly`、`TestPlan_CoverageReadyImplementableSubset` 作为基线对照通过；既有 `TestTargetRound_CVMCapacityReject` 的 CVM 100 条容量、配额计数缺失与 SDK LimitExceeded 三个子场景定向 race 通过，Provider 容量路径未修改。

后续接线：本步运行纯函数与本地夹具。Step 8 提供站点查询及预检结果，Step 9 按物理目标归并后调用本规划器，并将站点查询失败映射为 `UnavailableReason`。Step 10 用 `SiteIDs` 从目标回读提取每站覆盖证明，真实云快照及写入留 Step 17。

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

**本步改动边界**　主改动在 `syncer` 的预检纯决策、`config` 的冲突行读写及测试。配置快照保持不可变，状态写入与查询分离。当前阶段可直接注入过渡前缀，正式读取过渡表由 Step 11 接线。

**收口判据**　构造跨两个目标的重叠，并把另一域名查询设为失败，预检仍阻断两个来源。使用独立 SQLite 写锁复现首次保存失败，下一轮 DNS 数据不足时以进程内证据保持零云写入和零确认。成功提交冲突后重启，再用完整无重叠输入及成功提交解除作为恢复的唯一正向对照。

**Checklist**

- [x] 同一份配置中汇总有效 DNS 地址与所有启用站点当前、待更新 CIDR，跨目标和端口执行包含判断。
- [x] 验证已证明重叠优先于数据不足。无已知冲突时只停止失败侧；已知冲突在数据不足或仍重叠时继续停止两侧。
- [x] 记录已查实的冲突，验证首次写库失败后进程内保持阻断，后续复查重试持久化。完整有效输入证明无重叠并提交后自动清除。覆盖已提交冲突的重启恢复、读写失败、关闭一项、重新同时启用和清空配置。
- [x] 区分无任务、冲突、数据不足、无重叠和恢复。确认 DNS 熔断只记真实解析结果，模拟判定本身无运行状态副作用。

**证据**　2026-11-12，`beta`，实施前 HEAD `1bcfbaa`，工作树改动（本步未提交）。实际改动：`config/store.go` 增加 `sync_address_conflict` DDL 与 reset 清理；新增 `config/store_address_conflict.go`（唯一冲突行读写与坏记录拒绝）；新增 `syncer/preflight.go`（纯 CIDR 包含与准入决策）；新增 `syncer/conflict_state.go`（进程内 + SQLite 运行态、启动加载、保存重试与解除顺序）；新增 `config/store_address_conflict_test.go`、`syncer/preflight_test.go`、`syncer/conflict_state_test.go`、`webui/api/address_conflict_export_test.go`；本文件回填证据。`syncer.runRound` 与正式调度未接线，留 Step 8；过渡表读取留 Step 11；模拟/状态 API 留 Step 12/15。

纯预检：`DNSAddress`/`EdgeOnePrefix` 只保留地址与网段，没有目标、端口、动作维度，因此不同目标/端口不能削弱全局包含判断。`FirstOverlap` 先按域名/IP、站点/CIDR 稳定排序，DNS 地址执行 `Unmap`（IPv4-mapped 返回 IPv4 身份），EdgeOne 前缀执行 `Masked`；IPv4 与 IPv6 不互相匹配，网段边界与网段外分别覆盖。`DecideAdmission` 按 Design6 §8.3 固定优先级：单开/双关只放行已启用且完整的来源；双开时 `Overlap` 立即返回 `KeepConflict` 且两侧零准入；已知冲突在数据不足时继续两侧阻断；无已知冲突时只停止输入不完整的一侧；只有两侧完整且无重叠才放行。`PreflightOutcome` 使用 `single_source`、`overlap`、`data_unavailable`、`clear` 区分无任务/冲突/数据不足/无重叠/恢复，并保留 `DNSComplete`、`EdgeOneComplete`、`KnownConflict`、`Overlap` 两个独立结果；正向命中优先于任何查询失败。

运行态记录：`sync_address_conflict` 固定 `id=1`，字段为 `dns_name`、`dns_address`、`edgeone_prefix`、`zone_id`、`first_seen_at`、`last_seen_at`。`GetAddressConflict` 缺行返回 `(nil,nil)`，读取失败或 DNS 地址/网段/时间/ZoneId 损坏返回错误而不是无冲突；`SaveAddressConflict` 在短事务内 upsert，规范化 `Unmap`/`Masked` 并保留已有 `first_seen_at`、刷新证据与 `last_seen_at`，时间统一 UTC RFC3339；`ClearAddressConflict` 删除唯一行。`ResetAllTx` 清理冲突记录；`DeleteImportOwnedTablesTx` 不触碰该表（完整导入继续由有效预检解除），v4 导出只读业务配置，不含运行态记录。上述边界均有回归。

自动恢复状态：`AddressConflictState` 在发现重叠时先 `observe` 进程内证据，再 `persist`；首次写库失败仍保留内存证据并返回存储错误，后续双开轮次即使 DNS 数据不足也会重试保存并继续阻断。双开读取状态失败直接返回错误并让两侧零准入；单侧启用不读取/不清理冲突表，只保留进程内证据并允许正常侧。已持久化冲突在启动时由 `Load` 恢复；两侧完整且无重叠时先提交 `ClearAddressConflict`，成功后才清除内存并放行；解除提交失败保留内存与 SQLite 证据。冲突判定不接触 `dns.CircuitBreaker`，真实解析失败仍由既有 DNS 轮次单独计数。

执行命令与结果：`go test ./provider ./syncer ./config -race -count=1 -timeout 20m` → provider 9.362s、syncer 32.770s、config 36.510s；`go test ./webui/api -race -count=1 -timeout 20m` → ok（核验修复后 46.521s）。定向：`go test ./syncer -run 'TestAddressConflict|TestFirstOverlap|TestDecideAdmission|TestPreflightConflict' -race -count=1 -v -timeout 240s` → 全部 PASS；`go test ./config -run 'TestAddressConflict' -race -count=1 -v` → 4 个顶层用例及 7 个坏记录子场景全部 PASS；`go test ./webui/api -run 'TestExportV4ExcludesAddressConflictRuntimeState|TestExportV4ShapeAndNoRuntimeState' -race -count=1 -v` → 两项 PASS。`gofmt -l` 覆盖本步 Go 文件无输出，`git diff --check` 无输出。

连续故障实证（真实临时 SQLite 写锁 + 计数替身）：`TestAddressConflictStateSQLiteAutoRecoverySequence` 用 `BEGIN IMMEDIATE` 在独立连接持有真实写锁，并断言首次 Save/解除失败文本含 `locked`/`busy` 且等待至少 2 秒，证明失败确由真实 SQLite 写锁造成。第 1 轮 `203.0.113.7` 对 `203.0.113.0/24` 查实重叠，`SaveAddressConflict` 等待 busy_timeout 后失败，进程内证据保留，`Admission.DNS/EdgeOne=false` 且 `KeepConflict=true`，DNS 写入、EdgeOne 写入、站点确认计数均为 0，数据库无行。第 2 轮 DNS 数据不足、EdgeOne 完整，已知冲突继续阻断并在本轮重试保存成功，三项计数仍为 0，`first_seen_at` 保持第 1 轮时间。第 3 轮新建状态并 `Load` 模拟重启，输入两侧不足仍阻断且零写入，`first_seen_at` 不变。第 4 轮完整无重叠但写锁使解除提交失败，内存与数据库记录保留、两侧零准入、计数仍为 0。第 5 轮完整无重叠且解除成功，先删行再清内存，状态变为 `clear`，DNS/EdgeOne/站点确认各计 1。第 6 轮再次完整复查保持 `clear`。`TestAddressConflictStateOverlapPriorityAndSaveRetry` 用受控替身单独验证「部分成功仍命中 → 首次保存失败 → 数据不足重试成功 → 完整解除」；`TestAddressConflictStateReadFailureBlocksBoth` 验证双开读失败两侧阻断、单开不读表；`TestAddressConflictStateClearFailureKeepsMemory` 验证解除失败保留内存并可重试；`TestAddressConflictStateSingleSideAndReopen` 验证关闭一项继续 DNS、重新双开立即恢复阻断；`TestAddressConflictStateNoKnownDataStates` 覆盖双关、完整无重叠、单侧不足与两侧不足；`TestPreflightConflictBlocksEveryTargetAndConfirmation` 对两个目标同时断言零写入；`TestFirstOverlapIncludesNextVersionPrefix` 证明只存在于待更新版的被跟踪网段经 `AllPrefixes` 注入后仍参与判定；`TestAddressConflictDoesNotTouchDNSCircuitBreaker` 证明冲突判定不增加熔断计数，真实 `RecordFailure` 仍单独打开熔断器；`TestAddressConflictStateInvalidSamplesForceIncomplete` 验证非法 DNS 地址/EdgeOne 网段即使调用方传 `Complete=true` 也强制对应侧不完整且停止准入；`TestAddressConflictStateLoadWithoutRowRetriesPersist` 验证外部清空 SQLite 行后进程内证据仍会重试保存；`TestAddressConflictSaveRejectsInvalidFilledTimeOrder` 验证零值首次时间补齐后 last 早于 first 被拒绝且不留坏行；`TestAddressConflictStateForgetAfterReset` 验证 reset 清空 SQLite 后 `Forget` 清除进程内证据、不残留阻断，完整无重叠输入可正常恢复；`TestAddressConflictStateForgetReReadsPersistedRow` 验证 reset 未真正清行时下一次评估重新读取并恢复阻断。`TestAddressConflictGetRejectsCorruptRows` 覆盖 DNS 名称/地址/网段/ZoneId/时间损坏；`TestAddressConflictStoreRoundTripAndNormalization` 覆盖规范化、更新保留首次时间与非法保存不覆盖旧记录；`TestAddressConflictResetAndImportBoundary` 覆盖 reset 清理与完整导入保留。

外部验收状态：本步使用本地临时 SQLite、受控输入及 `Admission` 落点计数；正式轮次、真实 DNS 和腾讯云请求仍待验收。后续接线：Step 8 启动时调用 `Load`，在目标派发前将当前和待更新网段传入 `Evaluate`，并输出 `address_preflight`。reset 成功后调用 `Forget`，reset/导入与在途轮次按既定串行或配置代际边界处理，避免旧结果重新写入。Step 11 从 `edgeone_origin_transitions` 补入受保护前缀，Step 12 接入只读模拟，Step 14 展示预检状态，Step 15 记录冲突和恢复事件。

**本批收口门禁（Step 5～7）**　2026-11-12，构建提交为 `14642a6`（15 文件）。提交内容上复跑：`go test ./provider ./syncer ./config -race -count=1 -timeout=20m` → provider 9.845s、syncer 31.743s、config 37.221s；`go test ./webui/api -race -count=1 -timeout=20m` → 47.733s；提交前同一工作树的全量 `./...` 12 包通过。随后独立核验发现五项安全/证据缺口（见下），已在当前工作树修复并重跑：三包 9.362s/32.770s/36.510s、`webui/api` 46.521s、`go test ./... -race -count=1 -timeout=20m` 12 包全部 ok（root 21.622s、app 1.299s、config 36.510s、dns 1.530s、internal/health 43.257s、internal/portconv 1.283s、internal/tag 1.292s、notifier 15.921s、provider 9.234s、syncer 31.618s、webui 2.904s、webui/api 46.365s）。`go vet ./...`、`go build ./...` 无输出；本批 Go 文件 `gofmt -l` 无输出；`git show --check 14642a6`、`git diff --check` 与新增文件 whitespace 检查均无错误。当前工作树相对 `14642a6` 有 8 个修改文件、无新增文件、未提交；`beta` ahead 6 包含该构建提交。未执行真实腾讯云/真实 DNS、产品浏览器、Linux/Docker、依赖审计、远端 CI/GHCR 或发布；Step 8～17 保持待执行，真实证据边界见各步。

**核验后修复（2026-11-12）**　对 `14642a6` 的 15 文件构建做独立核验后修复以下五项，已提交为 `9df4e19`。

1. **Step 6 非法/空网段快照**：`appendEdgeOnePlannedRules` 原以 `PrefixesForFamily` 结果为准，零值/非法 `netip.Prefix` 会被族筛选丢弃并按“合法空地址族”处理；`Next` 指针存在但前缀为空也会被当作无下一版。新增 `originACLSnapshotPrefixesValid`：当前版至少一个前缀、当前/待更新版不得含非法前缀、非 nil `Next` 不得为空；不满足只记 `IssueEdgeOneUnavailable`，不产生 `Desired`/`ToAdd`/覆盖。`TestEdgeOnePlanUnavailableSiteNeverBecomesEmpty` 增加非法前缀与空 `Next` 子场景。
2. **Step 7 非法预检样本**：`Evaluate` 原信任调用方 `DNSComplete/EdgeOneComplete`，而 `FirstOverlap` 会跳过非法样本，导致“非法样本 + Complete=true”可能被视为完整输入。现在先校验样本，任一非法 DNS 地址/EdgeOne 网段强制对应完整性为 false；非法侧停止准入，另一侧仍可按单侧继续。新增 `TestAddressConflictStateInvalidSamplesForceIncomplete`。
3. **Step 7 外部清行后的持久化状态**：`loadLocked` 原在内存已有证据而 SQLite 行不存在时保留 `persisted=true`，外部 reset/清行后不会重试保存。现在缺行一律 `persisted=false`，后续数据不足轮次重新保存。新增 `TestAddressConflictStateLoadWithoutRowRetriesPersist`。
4. **Step 7 时间顺序与写锁证据**：`SaveAddressConflict` 原在 `FirstSeenAt` 为零、`LastSeenAt` 为过去时间时先补当前时间，可能写入 `last_seen_at < first_seen_at` 的坏行；现在补齐后校验顺序并拒绝。新增 `TestAddressConflictSaveRejectsInvalidFilledTimeOrder`。写锁回归从“只要失败”加强为断言失败文本含 `locked`/`busy` 且等待至少 2 秒，证明首次 Save/解除失败确由真实 SQLite 写锁造成。
5. **Step 7 reset 后进程内证据**：`ResetAllTx` 只清 SQLite 行，原进程内已知冲突会在 reset 后继续阻断后续数据不足轮次。新增 `AddressConflictState.Forget`：清除内存证据并把状态标为未加载，下一次双开评估重新读取 SQLite；reset 已成功时正常放行，reset 实际未提交时记录会重新进入阻断。测试 `TestAddressConflictStateForgetAfterReset` 覆盖 reset→Forget→数据不足轮次不再报 overlap→完整输入恢复，`TestAddressConflictStateForgetReReadsPersistedRow` 覆盖 SQLite 仍有记录时重新阻断。正式 reset API 的调用与在途轮次串行/配置代际处理留 Step 8。

修复后定向用例、三包 race、`webui/api` race 与全量 12 包均通过。五项修复分别做仓外 overlay 负向控制（仅替换生产文件、移除对应修复）：`TestEdgeOnePlanUnavailableSiteNeverBecomesEmpty` 报“非法网段必须按不可用处理: []”；`TestAddressConflictStateInvalidSamplesForceIncomplete` 报 `Admission={DNS:true EdgeOne:true}`；`TestAddressConflictStateLoadWithoutRowRetriesPersist` 报外部清行后 `saves=1` 未重试；`TestAddressConflictSaveRejectsInvalidFilledTimeOrder` 报非法时间顺序未被拒绝；`TestAddressConflictStateForgetAfterReset` 在 `Forget` 置空实现时报“Forget 后进程内证据必须清空”。五个负向控制均按断言失败，恢复修复后全部通过。另对 `Forget` 的重读行为做 overlay 控制：将 `loaded` 置回 true 后，`TestAddressConflictStateForgetReReadsPersistedRow` 报 `state=data_unavailable`，未重新发现持久化冲突。未改变 Step 5 客户端契约、Step 6 公开规划字段或 Step 7 表结构/状态机顺序，真实外部验收状态保持未执行。

<a name="step-1-7-review-fixes"></a>

### Step 1～7 独立核验修复（2026-11-12）

实施前为 `beta @ 9df4e19`，工作树干净。本轮修复独立核验复现的五项问题。

| Step | 修复与回归 |
|---|---|
| 1 | 设置 GET 对旧库 DNS TAG 去首尾空白，再应用缺省值。`TestDNSTagSourcesLegacyExplicitTagWithWhitespace` 核对首次打开和重启的运行时、GET、v4 导出均为 `my-dns`，SQLite 原值仍带空白。 |
| 2 | 四个新增设置字段中的显式 `null` 在进入协调器前返回 400。同一字段后续的合法值也无法覆盖该错误。`TestPutSettingsRejectsExplicitNullFeatureFields` 核对字段诊断、零写库和零发布。 |
| 6 | Lighthouse 对陈旧候选的每个功能 key 同时检查当前 TAG、手工规则和另一功能 TAG。任一 key 定位歧义时，候选仍可预览，清理原因为 `owned_key_ambiguous`。`TestEdgeOnePlanLighthouseStaleRuleLocator` 覆盖唯一规则、手工和另一 TAG 重复及合并端口中仅一个 key 重复。 |
| 7 | SQLite 读取失败时将本轮已查实的重叠保留在内存，两侧继续阻断，后续轮次重试读表。读恢复后保留待写入证据，沿用库内首次时间。`TestAddressConflictStateRetainsOverlapAcrossReadFailures` 覆盖连续读失败、缺行及已有行恢复、重启阻断和解除提交。 |
| 7 | 冲突记录更新在同一 SQLite 写事务中读取并解析原 `first_seen_at`，拒绝早于该值的 `last_seen_at`。`TestAddressConflictSaveValidatesStoredFirstSeen` 证明非法更新保留原行，验证相等和相差一纳秒的边界。 |

原核验的五个反例在修复后全部通过。五个新增回归在 `9df4e19` 生产代码的临时 overlay 上均按预期失败，使用修复后代码时全部通过。

本地验证通过 `make all`（前端构建、154 项前端测试、`go vet`、12 个 Go 包的 race 回归及产品二进制构建）。五个新增回归和设置 API 字段回归使用 `-race -count=20` 重复通过。变更位于本地工作树，真实腾讯云、真实 DNS 和远端 CI 留待后续阶段验收。

<a name="step-8"></a>

### Step 8 · 独立调度与正式入口

依赖 Step 2、Step 5 和 Step 7。使用可控时钟、假客户端和写入计数验证调度。生产 EdgeOne 防火墙写入接线留到 Step 9。

**代码入口**　`Run`、`consumeControlState`、`beginRound`、`TriggerSync` 和 `syncAll`。 [主循环](syncer/syncer.go) · [调度参考](Design6.md#preflight-reference)

**实施顺序**

1. 保留现有主循环作为唯一正式入口，计算 DNS 和 EdgeOne 各自的到期、首次启用、手动触发和恢复标记。读取运行快照后以同一准入点确定本轮来源。
2. 只对实际改变的间隔重排对应来源。测试运行跨过到期时间、重复 tick、相同间隔设置、很短的自定义间隔以及等待限速时的合并。
3. 两项启用时在任何目标任务前先形成可写来源集合。为本步使用计数执行器，验证门控后才会进入写入阶段。DNS 正式路径保留既有启停合同。
4. 经 `GET /api/sync/status` 提供 DNS、EdgeOne 各自的 `enabled`、`effective`、`next_due_at` 与 `address_preflight` 摘要，保留原有总开关字段。Step 14 读取状态用于展示。

**最小测试矩阵**　四种开关组合、总暂停、Stop 前后、配置快速合并、EdgeOne 首次启用、只改 EdgeOne 间隔、手动触发撞定时与实际重叠。用可控时钟和通道控制交错；断言两个来源的查询/执行次数、到期值和跨目标零调用。参考 `TestP324CoalescedResumePreservesScheduling`、`TestStaleTriggerAfterPauseDoesNotAddRound`、`TestI8R18StopGateEntrances`。建议 `go test ./syncer -race -count=1`。

**本步改动边界**　主改动在 `syncer/syncer.go` 的 Run 与触发门控、独立到期状态及测试时钟。该步接入正式调度和状态，EdgeOne 防火墙执行可用替身观察，实际执行接线在 Step 9。

**收口判据**　记录 DNS 与 EdgeOne 的 next due 和调用计数。只改 EdgeOne 间隔、EdgeOne 单独到期或新增站点时，DNS 到期值保持。双开且仅 EdgeOne 到期时，可为预检查询 DNS，该查询不触发 DNS 写入。测试总暂停后手动请求返回现有 409，恢复只调度已启用来源。

**Checklist**

- [x] 在现有 Syncer 统一接入两项的到期、首次启用、热更新、手动触发及暂停恢复。保持一个正式写入协调入口。
- [x] 两项分别计时，发布可供仪表盘读取的实际启用和调度状态。EdgeOne 间隔热更新不重置 DNS 定时，常规 DNS 轮复用有效 EdgeOne 快照，过期或变更触发相应重查。
- [x] 总暂停、Stop 和恢复复用现有准入及在途收尾合同，模拟测试保持独立。验证计时到期、短间隔限速、排队触发和并发配置变更。
- [x] 在全部目标写入前完成全局判定。以双开关矩阵和跨目标冲突零写入计数证明任何正式入口均未绕过。

**当前基线与正式路径**　运行快照已有两个功能开关和独立间隔；正式 `Run` 已改为 `Run → beginRound → runScheduledRound`，同时驱动 DNS 与 EdgeOne 的启动、定时、手动与恢复准入。启动接 `Load`，正式轮接 `Evaluate`，成功 reset 后接 `Forget`；reset/导入的在途结果沿 [Step 7](#step-7) 的配置代际边界处理。2026-11-13 修复批次补齐了被阻断轮次的 `round_started_at` 清理、启动/恢复 EdgeOne 退避、停用 TAG 只读覆盖、能力跳过证明、失败 attempt 证明丢弃、强制查询/缓存刷新与稳定区代次校验。 [主循环](syncer/syncer.go) · [冲突状态](syncer/conflict_state.go)

**证据**　2026-11-12，`beta`，实施前 HEAD `8efbda8`，工作树改动（Step 8～10 未提交；该历史记录随后已提交为 `76241cc`，2026-11-13 修复见文末修复批次）。实际改动：`syncer/syncer.go` 增加 DNS/EdgeOne 独立到期、ticker/触发/恢复门控、`ConfigEpoch`、`sourceStatus`、`address_preflight` 摘要与旧轮次稳定区；新增 `syncer/edgeone_round.go`（每轮 TEO 快照缓存、站点配置/凭据/计划切换失效、单站点一次查询、双开只读预检、来源准入与状态）；`config/runtime.go` 增加生产快照的 DNS 开关标记以兼容零值测试夹具；`webui/api/coordinator.go` 增加 `WithStableConfig` 与 reset 后 `Forget` 钩子，`webui/api/settings.go` 在 reset 成功后清除冲突内存证据，`run.go` 注入冲突状态、稳定区与 TEO 过渡存储；`webui/api/deps.go`、`webui/server.go` 暴露协调器供启动接线，`webui/api/sync.go` 保留 nil 依赖下的 200 状态骨架。新增 `syncer/step8_schedule_test.go`、`webui/api/reset_forget_test.go`。

执行命令与结果：`go test ./syncer -run 'TestStep8' -race -count=1 -timeout=5m` → ok（1.276s）；`go test ./... -race -count=1 -timeout=20m` → 12 包全部 ok（root 16.277s、syncer 31.156s、webui/api 42.935s 等）；`go vet ./...`、`go build ./...`、`gofmt -l syncer provider config webui/api webui run.go` 与 `git diff --check` 均无输出。

来源隔离证据：`TestStep8OnlyEdgeOneDueDoesNotWriteDNS` 启动轮写入 DNS 规则并查询一次 TEO；10s 后 EdgeOne 到期时 DNS TAG 规则数保持、EdgeOne TAG 新增，证明 DNS 写入为零；`TestStep8OnlyDNSDueReusesEdgeOneCache` 证明仅 DNS 到期时 TEO 查询次数不增加、EdgeOne 规则数保持；`TestStep8EdgeOneOnlySkipsDNS` 证明仅 EdgeOne 启用时 DNS 解析零调用；`TestStep8BothDisabledNoSchedule` 证明两开关关闭时零 TEO 查询、无 next_due；`TestStep8NewSiteSchedulesEdgeOne` 证明新增站点立即安排 EdgeOne 查询；`TestStep8OverlapBlocksBothSourcesBeforeWrite` 用 `203.0.113.7` 对 `203.0.113.0/24` 证明重叠时 Provider 快照/写入为零、冲突记录落库并暴露 `overlap`；`TestStep8StatusJSONShape` 校验原字段与 `dns/edgeone/address_preflight`、next_due_at 和 clear 摘要；`TestStep8EvaluateRefusesStaleState` 证明旧 RuntimeState 的预检写入被稳定区拒绝；`TestConfigResetForgetsAddressConflictAfterCommit` 证明 reset 提交后调用 `Forget`；`TestStep8ConflictStoreFailureBlocksAndRecovers` 证明首次冲突保存失败时两侧零写入、后续输入完整且保存恢复后自动放行。

外部验收状态：本步全部使用 `synctest`、本地 TEO/Provider 替身和内存/SQLite 状态；真实腾讯云、真实 DNS、短间隔限流实测、产品浏览器、Linux/Docker 与远端 CI 均未执行。Step 9 接入实际 EdgeOne 目标写入后，本步测试仍保留来源隔离断言。

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

**本步改动边界**　主改动在 `syncer/target.go`、同目标执行分组和回读证明。允许通过测试接缝使用四平台实现；现有 API 版本、删除定位与并发策略保持既有边界。旧 EdgeOne 网段清理按 Step 11 接入。

**收口判据**　给同一物理目标两个本地别名，记录 S0→Create→S1 调用顺序，确保该目标只有一个写入执行单元。对腾讯版本冲突重读 S0，核对新版本和原先通过预检的输入；DNS-only 回归仍观察到逐 attempt 解析。能力跳过或 S1 失配时站点覆盖不可用。

**Checklist**

- [x] 同一物理目标串行执行增量添加及 S1 验证，计算过渡期规则量和平台容量。延续 Provider 的版本与稳定删除定位。
- [x] 两项同时启用时，目标重试使用本轮已检查的地址集，每次重读云快照；仅启用 DNS 时保留原 DNS 重试。
- [x] 删除只允许该功能 TAG，沿用先增、回读、安全门和必要的 S2。EdgeOne 的过期网段清理等 Step 11 提供生效证明后准入。
- [x] 用本地模拟验证同组多站点、部分写入、配额不足、版本竞争、跨 TAG 残留和跨目标预检。失败时核对真实写入计数与保留旧规则。

**当前基线与待接线**　DNS 正式链已有 S0、增量添加、S1 和条件清理；Step 6 已提供站点 CIDR 规划和物理目标归并辅助。本步已用 Step 8 的预检结果生成正式目标输入，按实际资源合并执行，并在双开重试中复用已检查的数据、输出站点覆盖证明。 [正式目标链](syncer/target.go) · [规划器](provider/plan.go)

**证据**　2026-11-12，`beta`，HEAD `8efbda8`，工作树改动（未提交；随后已提交为 `76241cc`，2026-11-13 修复见文末修复批次）。实际改动：`provider/plan.go` 的 `TargetPlanInput` 增加 `EdgeOneTag`，双源联合规划把 DNS TAG 与 EdgeOne TAG 都视为 owned 命名空间；共享 key 由任一 TAG 覆盖且只新增一条（DNS TAG 优先），单源 key 的另一 TAG 等价仍为 `cross_tag_equivalent`，清理按 TAG 归属。新增 `provider/plan_edgeone_test.go` 双 TAG 用例；`syncer/target.go` 增加 `targetExecution`、活动来源过滤、双开冻结 DNS 回读、站点证明生成与 EdgeOne 清理延后；新增 `syncer/target_group.go`（按 `cloud_type + region + resource_id` 归并本地别名、规则并集、站点并集、单元计数）与 `syncer/site_coverage.go`（当前/待更新版本、规范前缀摘要、配置代次、完整性/冲突/原因）；`syncer/syncer.go` 的 `runRound` 改为按物理目标分组执行。新增 `syncer/step9_target_test.go`。

执行命令与结果：`go test ./provider -run 'TestEdgeOnePlanDualTag' -race -count=1` → ok（1.311s，双 TAG 共享/单源冲突）；`go test ./syncer -run 'TestStep9' -race -count=1 -timeout=5m` → ok（1.278s）；`go test ./... -race -count=1 -timeout=20m` → 12 包全部 ok；`go vet`、`go build`、`gofmt`、`git diff --check` 无输出。

判别性证据：`TestStep9AliasesShareOneExecutionUnit` 用两个本地 ID 指向同一 CVM 安全组，断言 p1 零快照、p0 只有一次 `CreateRules` 且规则终态为 DNS+EdgeOne 两条；`TestStep9DoubleSourceRetryKeepsCheckedDNS` 让首次 S0 返回 `versionMismatch`、解析器第二次若被调用返回重叠地址，断言双开重试只解析一次、只写 `192.0.2.10/32`，未使用新地址；`TestStep9S1MissingBlocksConfirmation` 让 S1 看不到新增规则，断言目标失败、保留不完整站点证明、确认次数为零；`TestEdgeOnePlanDualTagSharedKeyCoveredByEitherTag` 证明共享 key 在 DNS/EdgeOne TAG 任一归属下均覆盖且不新增重复规则；`TestEdgeOnePlanDualTagSingleSourceStillConflicts` 证明单源 key 的跨 TAG 等价仍冲突；`TestStep9EdgeOneCleanupDeferred` 证明 EdgeOne TAG 旧规则在 Step 9 零删除并保留延后。既有 `TestTargetRetry_IdempotentCreateRequiresCoverage`、`TestTargetRetry_DeleteProgressAcrossAttempts`、容量/版本/清理回归继续通过。

外部验收状态：本步使用假 Provider、`synctest` 与内存状态，不证明真实四平台写入、真实配额或真实 S1 回读；EdgeOne 旧网段清理按合同保持延后，留给 Step 11。真实云、浏览器、Linux/Docker 与远端 CI 未执行。

<a name="step-10"></a>

### Step 10 · 站点自动确认

依赖 Step 9。范围为 `ConfirmOriginACLUpdate` 与站点级覆盖证明。

**代码入口**　Step 5 的 TEO 客户端、Step 9 的目标回读结果、站点运行状态与当前运行配置。 [确认调用与状态参考](Design6.md#target-reference) · [确认 API](https://cloud.tencent.com/document/product/1552/120409)

**实施顺序**

1. 把每个目标的回读结果转换为站点证据，核对同一 `ZoneId`、待更新版本、目标身份和配置。以配置要求的目标集作分母。
2. 使用一个确认函数统一判断必要地址族、完整覆盖和相关冲突。重查腾讯云当前及待更新两版的版本和规范网段，并在发请求前再次检查当前配置和站点启用。总暂停后拒绝新发站点确认。
3. 加入过渡运行表的确认状态读写，按 `ZoneId`、待更新版本和规范前缀摘要去重。请求前提交 `unknown`。同一指纹的 `accepted` 跨轮和重启均跳过确认；`unknown` 先只读复查，只有明确证据证明未执行或尚未确认时，才重新核验并允许重试。Step 11 接入写防火墙前的网段保护和清理准入。
4. 经 `GET /api/edgeone/status` 提供站点状态摘要，包含 `RequestId` 及诊断原因，确认失败作为站点结果体现。Step 11 补入过渡保护与清理信息，Step 14 读取该接口。

**证据目标**　同一站点两个目标均通过且版本不变时确认恰好一次；分别让任一目标失败、只覆盖平台可实施子集、相反动作、绑定变更、总暂停、版本或同版本网段变化，确认计数均为零。加「无已知冲突、DNS 失败、EdgeOne 完整」正向用例。成功后让同一指纹在下一轮及重启后仍出现在 `Next`，确认计数仍为一次；超时后依次测试「重查仍未知」「明确未执行后允许重试」「`OperationDenied.LatestVersionNow` 后重查无待确认工作」。同时核对 `unknown` 提交失败不发送请求、`accepted` 写入失败后不重发。建议运行 TEO 相关包和 `./syncer` 的 race 门禁。

**本步改动边界**　主改动在 TEO Confirm 适配、站点证明汇总、确认状态的 SQLite 短事务和只读状态响应。旧网段保护与释放规则由 Step 11 接入，目标写入继续走 Step 9。

**收口判据**　两个目标读回同一指纹后，确认序列为重查→提交 `unknown`→一次发送→保存 `accepted`。撤掉一个证明、变更配置或重查版本时确认计数为零。同指纹跨轮及重启合计一次；超时而重查仍未知时仅新增 Describe 调用。

**Checklist**

- [x] 聚合站点全部绑定目标对同一配置和版本的完整回读证据，纳入容量、地址族和相关冲突。任一目标不通过时确认请求计数为零。
- [x] 确认前重查版本和网段，复核站点绑定和启用状态。全部通过时自动确认；无已知重叠而 DNS 查询失败时，EdgeOne 仍可完成确认。
- [x] 验证同一待更新指纹跨轮、重启去重，以及超时后只读复查与有明确证据时的重试准入。记录站点确认结果、`RequestId` 和实际生效状态，供站点页读取。

**当前基线与待接线**　TEO 客户端已通过本地服务验证签名 Describe 请求。站点配置 CRUD 已注册。本步已实现确认调用、目标覆盖汇总、指纹去重和只读站点状态接口。 [TEO 客户端](provider/teo.go) · [已注册路由](webui/api/deps.go)

**证据**　2026-11-12，`beta`，HEAD `8efbda8`，工作树改动（未提交；随后已提交为 `76241cc`，2026-11-13 修复见文末修复批次）。实际改动：`config/store.go` 增加 `edgeone_origin_transitions` 冻结表与 reset 清理；新增 `config/store_edgeone_transition.go`（规范 CIDR JSON、UTC 时间、同指纹保留 accepted/unknown、`SetEdgeOneConfirmation` 指纹 CAS、导入剪枝、删除站点清理）与 `config/store_edgeone_transition_test.go`；`provider/teo.go` 增加 `ConfirmOriginACLUpdate`、`TEOErrorAlreadyLatest`/`TEOErrorDenied`、发送前 `NotSent` 证据、`CurrentOriginACL` 的 `ActiveTime`/`IsPlaned`；`syncer/edgeone_confirm.go` 实现站点证明聚合、发送前重查、unknown 前置提交、一次确认、accepted+RequestId、超时/网络只读、LatestVersionNow、明确未执行重试；`syncer/edgeone_round.go` 在 EdgeOne 轮末执行确认；`webui/api/edgeone_status.go` 增加只读 `GET /api/edgeone/status`，`webui/api/deps.go` 注册路由；`run.go` 注入过渡存储。新增 `syncer/step10_confirm_test.go`、`webui/api/edgeone_status_test.go`。

执行命令与结果：`go test ./syncer -run 'TestStep10' -race -count=1 -timeout=5m` → ok（1.281s）；`go test ./provider -run 'TestTEOConfirm|TestTEOOriginACLCurrent' -race -count=1` → ok（1.311s）；`go test ./config -run 'TestEdgeOneTransition' -race -count=1` → ok（1.359s）；`go test ./webui/api -run 'TestEdgeOneStatus|TestConfigResetForgets' -race -count=1` → ok（1.404s）；全量 12 包 race、vet/build/gofmt/diff-check 通过。

确认证据：`TestStep10HappyPathAndCrossRoundDedupe` 断言首次确认恰好一次并保存 `accepted`/`req-confirm`，同指纹下一轮不重发；`TestStep10AcceptedFingerprintSurvivesRestart` 证明重启后同指纹仍不重发；`TestStep10UnknownSaveFailureSendsZero` 证明 unknown 提交失败零发送；`TestStep10AcceptedSaveFailureDoesNotResend` 证明 accepted 保存失败只留在进程内去重、同指纹不重发；`TestStep10TimeoutOnlyRechecks` 证明超时后状态为 unknown、再次调用不重发；`TestStep10LatestVersionNowDoesNotMarkAccepted` 证明 `OperationDenied.LatestVersionNow` 只更新实际状态、不伪造 accepted；`TestStep10RecheckPrefixChangeSendsZero` 证明同版本网段变化在发送前被拦截；`TestStep10IncompleteCoverageSendsZero` 证明任一目标缺证明时确认次数为零；`TestStep10DNSFailureStillConfirmsCompleteEdgeOne` 证明 DNS 失败但 EdgeOne 全部目标完整时仍恰好确认一次；`TestEdgeOneStatusReadsLocalStateOnly` 证明状态接口只读本机站点、确认结果、覆盖与最近查询，不访问 TEO。

外部验收状态：全部确认请求经本地假 TEO/SQLite，不证明真实腾讯云确认执行、真实待更新窗口、`IsPlaned` 真实取值或 `LatestVersionNow` 真实返回；Step 11 的过渡保护与旧网段清理未在本步实现；真实云、产品浏览器、Linux/Docker 与远端 CI 未执行。

**Step 8～10 本批门禁（2026-11-12）**　`make all` 退出码 0：真实前端 `npm ci && npm run build`、前端行为测试 154/154 pass、Go 全量 `go test ./... -race -v` 12 包通过、`go vet ./...`、`go build -o fwalizer .` 全部成功。因本机默认 npm cache 目录存在权限/EEXIST 问题，首次 `make all` 在 `npm ci` 缓存阶段失败；使用临时 `NPM_CONFIG_CACHE` 重跑后上述门禁成功，该失败不是产品代码或依赖解析失败，已保留记录。另独立执行 `go test ./... -race -count=1 -timeout=20m` 12 包 ok（root 17.571s、config 34.377s、syncer 32.874s、webui/api 45.044s 等），`gofmt -l` 与 `git diff --check` 无输出。未执行真实腾讯云/真实 DNS/真实确认、Linux/Docker、依赖审计或远端 CI/GHCR。该段为 2026-11-12 实施时历史记录；改动随后已提交为 `76241cc`。2026-11-13 修复批次测试与边界见下方。

### Step 8～10 修复批次（2026-11-13）

修复分支从 `beta / 76241cc612435cc389258ef4b732d6c2158046ac` 建立，分支名为 `fix/build8-step8-10`；修复前工作树/暂存区干净。本轮只修改生产代码、测试与文档，不改 SQLite schema、配置包版本、Provider 接口或前端协议。

**关键决策**

1. **停用来源 TAG 只读覆盖**：DNS/EdgeOne 只有一侧启用时，规划器把停用来源 TAG 下的精确等价规则归入只读覆盖（类似手工外部规则），不计 owned、不报 `cross_tag_equivalent`、不新增、不删除、不改标；双开时保持原双 TAG 语义。对应 `provider/plan.go` 的 `CoverageOnlyTag`。
2. **失败 attempt 不保留证明**：只有最终成功 attempt 的 `siteProofs` 可写入本轮覆盖；中间失败 attempt 清空证明；最后一次清理重试耗尽的既有 `success + cleanup_deferred` 分支保留该 attempt 证明。
3. **EdgeOne 到期/手动/首次启用/新站点/恢复必须实际查询**；仅 DNS 到期可复用有效缓存；强制查询失败时，仍在有效期内的最后成功快照可继续使用但记录查询错误；成功查询（含确认前重查、unknown 复查）刷新缓存；reset/完整导入/站点删除清除进程内 EdgeOne 运行态。
4. **共享 key 的全局预检边界**：双开全局预检先于目标执行；DNS 地址落入任何 EdgeOne 当前/待更新网段时仍按重叠阻断。因此“共享 key 只新增一条”的写入路径在正常双开轮会被预检拦截，只在直接目标执行/已有规则覆盖等放行场景达到；本轮仍修正执行层 TAG 选择（仅 EdgeOne 活动时写 EdgeOne TAG），但**不改变重叠阻断**。
5. **失败目标/证明**：目标最终 `failed` 时本轮证明为空；`TargetPartial` 是否可确认仍取决于 EdgeOne 证明完整性（能力跳过现在会使证明不完整）。按来源统计留 Step 14/15，不在本轮阻塞。

**生产改动**

| 问题 | 改动 |
|---|---|
| P1-3 假在途轮次 | `runScheduledRound` 所有返回路径 `defer clearRoundStarted()`；`syncAllStateWithInput` defer 保留为幂等双保险。 |
| P1-4 启动/恢复丢失退避 | 新增 `settleEdgeAfterStartResume`；启动/恢复轮后应用 `EdgeRetry`，成功只重置 `edgeRetryDelay`、保持轮前计时；手动触发、首次启用、新站点重置退避。 |
| P1-1 共享 key TAG | `activeAddActions` 在共享 key 仅 EdgeOne 活动时用 EdgeOne TAG；DNS 同时活动时仍 DNS TAG 优先。 |
| D1 停用 TAG | `TargetPlanInput.CoverageOnlyTag`；停用 TAG 规则归入 external 只读覆盖，不冲突/不清除。 |
| P1-2 能力跳过 | `siteCoverageForExec` 对 `!Implementable` 的站点期望追加 `unsupported`，不再生成完整证明。 |
| P1-5 失败证明 | `syncTargetExec` 失败 attempt 清空 `siteProofs`；清理重试耗尽特殊分支保留当前 attempt 证明。 |
| P2-1 缓存 | `prepareEdgeOneData(force)` 以 `edgeDue` 强制查询；成功查询刷新快照/观测时间；强制失败回退有效缓存；`applyEdgeOneQueryResult` 统一稳定区/代次写入；reset/import/site delete 清理 `edgeCache/edgeStatus/roundCoverage/confirmedMem/preflight/edgeRetryDelay`。 |
| P2-2 状态 | `LastQueryAt` 使用实际查询时间，缓存命中保留 `PlannedActiveAt`；超时/重查变化/已最新/明确未执行写入诊断；非 TEO 错误不再误标为“TEO 查询失败”；单源 `address_preflight` 回到 `single_source`。 |
| P2-3 代次 | 轮次捕获 `epoch` 传入目标执行/证明/预检；`storeSiteProofsForState`/`storePreflight` 校验状态指针与代次；旧状态查询结果不写缓存/状态。 |

**新增/调整测试**

- `syncer/step8_fix_test.go`：阻断轮 `round_started_at` 清理、启动 1m→2m 退避、恢复成功清零、手动触发强制查询、缓存命中保留查询时间/计划时间、站点删除清理、旧证明/旧缓存拒绝、单源状态。
- `syncer/step9_fix_test.go`：共享 key EdgeOne TAG 直接执行、能力跳过零确认/不完整证明、失败 attempt 证明不保留。
- `syncer/step9_target_test.go`：把“S1 缺项保留证明”旧断言改为“失败 attempt 不保留证明、最终 failed”。
- `provider/plan_coverage_only_test.go`：DNS-only 与 EdgeOne-only 下停用 TAG 只读覆盖。
- `webui/api/reset_forget_test.go`：reset 后同时调用 `ForgetEdgeOneRuntime`。
- `syncer/step10_confirm_test.go`：超时后必须留下 `LastConfirmationErr`。

**本地验证（2026-11-13）**

- `go test ./syncer -race -count=1 -timeout=20m` → ok（31.614s，syncer 包完整通过）。
- `go test ./provider ./config ./webui/api -race -count=1 -timeout=20m` → provider 9.096s、config 37.333s、webui/api 47.610s，全部 ok。
- `go test ./... -race -count=1 -timeout=20m` → 12 包全部 ok（root 16.932s、app 1.627s、config 37.333s、dns 1.892s、internal/health 43.364s、internal/portconv 1.623s、internal/tag 1.542s、notifier 16.248s、provider 9.096s、syncer 31.614s、webui 2.761s、webui/api 47.610s）。
- 新增/变更判别性回归 `go test ./syncer -run 'TestStep8Blocked|TestStep8Resume|TestStep8Manual|TestStep8SiteRemoval|TestStep8Stale|TestStep8SingleSource|TestStep9SharedKey|TestStep9Capability|TestStep9FailedAttempt|TestEdgeOneCacheHit|TestStep10Timeout|TestStep10Latest' -race -count=20 -timeout=10m` → ok（2.470s，20 轮）。
- `go vet ./...`、`go build ./...`、受影响 Go 文件 `gofmt -l`、`git diff --check` → 均无输出。
- `syncer/step8_schedule_test.go` 现在同时断言：仅 EdgeOne 到期轮的新增动作不含 `[auto-dns]`、删除调用数不增加；`syncer/step9_target_test.go` 的别名目标同时断言 p1 的 snapshot/create/delete 均为 0；`step10_confirm_test.go` 的 DNS 失败正向用例断言没有 `[auto-dns]` 创建动作。
- 未执行真实腾讯云/真实 DNS/真实确认、Linux/Docker、前端构建、依赖审计或远端 CI/GHCR；本轮全部为本地替身/临时 SQLite/`httptest`。Step 11～17 保持未完成。

**旧证据口径订正**

- 原 Step 8 证据以“DNS TAG 规则数保持”推断零 DNS 写入；2026-11-13 起 `TestStep8OnlyEdgeOneDueDoesNotWriteDNS` 同时断言新增动作描述与删除调用数，不再只依赖终态。
- 原 Step 9 “p1 零快照”只证明第二执行单元未读快照；2026-11-13 起同时断言 p1 的 `CreateRules/DeleteRules` 均为 0，并保留 p0 一次新增断言。
- 原 Step 10 `TestStep10IncompleteCoverageSendsZero` 实际是“单目标、完全没有证明”，不是“一站两目标缺任一证明”；原证据段“任一目标”表述过强，双目标用例仍待补。
- 原 Step 10 `TestStep10AcceptedSaveFailureDoesNotResend` 的第二次调用由 SQLite 中的 `unknown` 分支阻止，不是 `confirmedMem` 去重；本轮保留该行为，并补了失败 attempt 证明和 `LatestVersionNow` 实际状态/诊断断言。
- 原状态接口测试的 Syncer 没有 TEO 客户端，因此“零 TEO 请求”是结构性结果，不是调用计数证据；本轮未新增 TEO spy 计数测试，留作后续证据补强。
- 原提交说明写“共 27 文件”，`git diff 8efbda8..HEAD` 实际为 32 文件；该元数据偏差无法通过修改提交说明订正，记录在此。

**剩余已知边界**

- 共享 key 的正常双开写入被全局重叠预检阻断（见决策 4）；若产品要求共享 key 必须可在双开轮写入，需要单独裁决全局预检例外，不在本轮擅自放宽安全门。
- `TargetPartial` 的确认准入按 EdgeOne 证明完整性判断；DNS 侧能力跳过不单独阻断 EdgeOne，EdgeOne 侧能力跳过会阻断。
- 按来源统计（DNS/EdgeOne 各自的 added/deleted）仍留 Step 14/15。
- 真实外部证据仍缺 Step 17。

### Step 8～10 合并前复核修复（2026-11-13）

- **Step 8 缓存有效期。** 逐站在查询失败返回后检查缓存，并在其他站点和 DNS 输入准备完成、预检开始前再次检查复用快照。超过间隔或达到已观察到的计划切换时间即按不完整处理。有效缓存回退保留最后成功查询时间和失败诊断。EdgeOne 到期轮仍按查询失败退避；仅 DNS 到期时保持 EdgeOne 调度。
- **Step 10 确认回包代次。** SQLite 和内存确认结果写入共用配置协调器的稳定区，先校验 RuntimeState 与 ConfigEpoch。请求和只读复查保持在锁外。重置、删除或导入已经清除的状态不会被旧回包恢复。若同一站点的过渡记录仍保留，则已提交的 `unknown` 保持原值，后续同指纹只读复查。当前代次的正常成功仍持久化 `accepted` 和 RequestId。
- **验证范围。** `syncer/build8_followup_test.go` 覆盖查询返回和预检前的失效、有效缓存回退、仅 DNS 到期、在途确认回包与 SQLite 配置变更，并以正式 Run 补齐一站两物理目标的确认计数（全部覆盖时一次，缺任一 S1 证明时零次）。`webui/api/edgeone_confirm_import_test.go` 走正式 `POST /api/config/import`、临时 SQLite 和配置发布，验证同站点保留 `unknown`、移除站点剪枝、旧回包零回写。

**本地结果**　Go 1.27.3（darwin/arm64）。Step 8～10 专项和导入回归 race 20 轮、全仓 race（`-count=1`）12 包、`go vet ./...`、`go build ./...`、前端 154 项、前端构建、格式与差异检查均通过。用 `2c4653d` 的两份生产源文件做负向对照，新增缓存、旧代次与正式导入用例按行为断言失败。真实云、Linux/Docker 和发布验收未在本轮执行。

<a name="step-11"></a>

### Step 11 · 版本切换与旧网段清理

依赖 Step 9 和 Step 10。范围为过渡状态、清理准入和重启恢复。

**代码入口**　过渡记录写入与读取、确认结果、`runTargetCleanup`。 [运行表 SQL](Design6.md#schema-reference) · [版本与清理参考](Design6.md#target-reference)

**实施顺序**

1. 观察到待更新版本时记录基准版、待更新版和保护前缀，先提交保护，再进行本轮 EdgeOne 写入。站点处于过渡期间时，即使只返回当前版，也保留仍无法证明可移除的前缀。
2. 重启用 SQLite 恢复保护及未确定的确认状态。将仍可能用于回源的保护前缀纳入全局预检。查询失败时保留前缀用于证明正向重叠，同时维持输入不完整状态。
3. 有效查询显示追踪的待更新版已成为当前且网段一致时，先重新计算预检的保护集合，再按同目标所有适用站点计算清理候选。复用当前 S1 的 TAG、版本、定位与 S2。无法证明删除条件时记录延后，下一次从新快照收敛。

**证据目标**　按「A 当前 → A 当前且 B 待更新 → B 已确认但仍是 A 当前 → 重启后 A 当前且无 Next → B 成为当前」依次推进夹具。重启后让 DNS 地址只命中 B 的受保护前缀，断言两侧仍零写入。B 成为当前时再验证 A 特有前缀可先从预检保护集合移出，实际删除继续走目标安全门。加入强制切换、待更新替换、另一站点仍需要 A、删除失败和 S2 失去覆盖的对照。检查 SQLite 过渡内容、预检输入、删除调用计数及规则终态。

**本步改动边界**　主改动在过渡表读写、全局预检的数据准备及目标删门。保留 Step 10 的确认去重和现有 Provider 删除契约。读取状态、持久化保护与云请求之间有清楚的事务边界。

**收口判据**　用 A、B 两版中各一段特有网段及一段共用网段，证明重启且 `Next` 消失时 B 的特有网段仍可触发全局阻断。随后查询确认 B 成为当前，A 特有网段可从预检保护集合移出。实际删除须通过本轮 S1、归属和定位检查，删除后按合同读 S2。共用网段及另一站点仍需的 A 网段不进入可删集合。

**Checklist**

- [ ] 持久化需保护的网段过渡状态，并将仍可能用于回源的前缀纳入预检。验证重启后 `Next` 消失且 DNS 命中受保护前缀时，两侧仍停止写入。
- [ ] 覆盖已确认但尚未生效、下一版消失但当前版未变化、腾讯云强制切换等返回形态。有效查询明确新版成为当前版后，才允许清理过时 EdgeOne TAG 规则。
- [ ] 复用安全删除门和 S2 验证，覆盖同目标另一站点仍需要旧网段、清理失败保留和再次执行收敛。

**当前基线与待接线**　Step 5～7 已支持当前版与待更新版 CIDR、纯规划和冲突持久化。待 Step 10 接入 `edgeone_origin_transitions` 的确认状态后，本步补入保护前缀持久化与重启恢复，并接入全局预检和旧网段清理准入。 [现有 SQLite 表](config/store.go) · [过渡表设计](Design6.md#schema-reference)

<a name="step-12"></a>

### Step 12 · 模拟测试 API

依赖 Step 6、Step 7、Step 9 和 Step 11。范围为现有模拟测试端点及响应。

**代码入口**　`Syncer.DryRun`、`handleSyncDryRun`、`DryRunResult`、现有正式规划器。 [Dry Run API](webui/api/sync.go) · [同步器](syncer/syncer.go) · [响应参考](Design6.md#view-reference)

**实施顺序**

1. 保留既有 TryLock、只读取 S0、每目标结果、空数组语义。按配置准备两种来源，调用共用预检与目标规划。
2. 附加站点版本、数据完整性、容量依据和目标覆盖问题。当前计数、预计新增、过渡期预计占用和已知限额各有字段；未知限额显式表示。
3. 以只读方式取得已知冲突和同一过渡记录，使用正式的保护集合计算预览。只有需要预览时读取云快照，正式缓存、熔断器、运行状态和下次到期时间均保持只读预览前的值。

**断言与命令**　同一组 fixture 对比正式 plan 和 dryrun，覆盖仅 DNS、仅 EdgeOne、两项冲突和总暂停仍可模拟。共享同一份保存的过渡记录，覆盖 `Next` 消失、仍受保护和保护已解除的场景，逐项比较预检保护网段、冲突及清理候选。计数器证明 Create/Delete/Confirm、冲突和过渡状态写入均为零，结果序列化检查空数组不是 null。保留 `TestDryRun_MatchesProductionPlanner`、`TestDryRun_ArraysNeverNull`、`TestDryRun_PublishesNoEvents`。建议 `go test ./syncer ./webui/api -race -count=1`。

**本步改动边界**　主改动在 `Syncer.DryRun`、`webui/api/sync.go` 的响应及模拟夹具。只读准备路径可复用正式的数据判定，正式执行、写运行表和调度计时不从模拟入口调用。

**收口判据**　冻结同一运行配置、缓存有效性与过渡记录，分别构造正式计划和模拟结果。校验可用前缀、冲突、计划新增和清理候选逐项相符。用 spy 计数 Create/Delete/Confirm 均为零，同时比较 SQLite、熔断计数和下次到期值前后不变。

**Checklist**

- [ ] 使用正式重叠判定、过渡保护集合和同一规划器，返回站点版本、绑定目标、预计新增、过渡期容量和目标限制。与正式计划对照保护网段、冲突和清理候选。
- [ ] 无适用规则的目标保持既有空结果语义。覆盖仅 DNS、仅 EdgeOne、两项同时启用、已暂停和查询不完整。
- [ ] 验证模拟无云增删、无站点确认、无 DNS 熔断改写、无正式冲突状态变更，且前后端响应数组和错误字段一致。

**当前基线与待接线**　现有 DNS Dry Run 在总暂停时可运行，重叠的 Dry Run 请求返回 409；正式触发在总暂停时返回 409。已有按目标只读规划与空结果语义。待扩展 EdgeOne 站点、共用预检、保护网段、容量与覆盖输出，并验证正式与模拟的一致性。 [Dry Run 实现](syncer/syncer.go) · [暂停回归](syncer/state_test.go) · [HTTP 映射](webui/api/sync.go)

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

**本步改动边界**　主改动在 `App.vue`、`main.ts`、设置页、EdgeOne 站点页及配置表单测试。页面消费 Step 2～4 合同。运行状态、模拟结果和健康呈现由 Step 14、15 接入。

**收口判据**　渲染页面后模拟多选两个目标并提交，核对 JSON 中整数 ID、端口和地址族。保存设置时核对两个 TAG 和间隔字段，禁止错误 payload 顺带修改总暂停。深链、回退、失败保留编辑内容及晚到请求的断言落在实际组件和请求计数上。

**Checklist**

- [ ] 添加左侧栏「EdgeOne 回源」和 `#/edgeone`，验证深链、侧栏高亮与浏览器历史。
- [ ] 全局设置显示 DNS 开关、DNS TAG、EdgeOne TAG 和 EdgeOne 间隔。表单可回显自定义值和明确的字段错误。
- [ ] EdgeOne 页面支持启用开关、站点配置和目标多选，展示安全组影响范围及全部实际源站绑定要求。
- [ ] 验证保存失败保留表单、重复提交锁和晚到响应处理，覆盖一站多目标和备份导入后回显。

**当前基线与待接线**　Step 2～4 的字符串设置、站点 CRUD、目标绑定和 v3/v4 备份合同已具备，可直接用于配置表单。现有前端已有 DNS TAG 输入和配置包下载逻辑。新增功能字段、`#/edgeone` 路由、站点表单及行为验收待本步完成。 [设置 API](webui/api/settings.go) · [站点 API](webui/api/edgeone.go) · [现有设置页](webui/frontend/src/views/Settings.vue)

<a name="step-14"></a>

### Step 14 · 模拟测试与仪表盘界面

依赖 Step 11、Step 12 和 Step 13。范围为状态消费和结果呈现。

**代码入口**　`Dashboard.vue`、`RunTest.vue`、`DryRunResults.vue`、`useDryRun.ts`、`types.ts` 与 EdgeOne 站点页。 [仪表盘](webui/frontend/src/views/Dashboard.vue) · [模拟组件](webui/frontend/src/components/DryRunResults.vue) · [响应参考](Design6.md#view-reference)

**实施顺序**

1. 仪表盘区分总暂停、DNS 与 EdgeOne 开关、下次到期和实际失败状态。站点页读取只读状态端点，展示版本、覆盖、确认和更新时间。
2. 在模拟页扩展现有结果组件，显示站点版本、作用目标、容量依据与全局冲突明细。未知限额、未知确认状态和已确认使用不同提示。
3. 添加无记录、全局冲突、数据不足单侧继续、持续冲突后恢复和仅单项运行的 UI 夹具。保留离页晚到请求抑制、真实零和未观测的区分。

**验收方式**　优先在 `dashboard-observation.test.mjs`、`dryrun-state.test.mjs` 和站点页 fixture 中断言真实模板输出。测试无数据不被展示成成功，查询成功也不被误报为已确认。运行前端定向入口及完整前端门禁。

**本步改动边界**　主改动在站点状态页、仪表盘、模拟结果组件及其响应类型。通过后端状态接口读取事实，前端无第二套冲突判定、倒计时调度或确认控制。

**收口判据**　模拟返回相同 200 状态的三种结果：完整无冲突、重叠阻断、数据不足单侧继续。三种须显示不同含义。站点结果未知、明确待确认、已确认和已生效分别可辨识。总暂停时不允许正式触发，模拟仍可运行。

**Checklist**

- [ ] 展示总暂停及两项实际启用状态，EdgeOne 显示当前/待更新版本、最近查询、下次同步和确认状态。
- [ ] 模拟结果显示按目标的容量与覆盖，以及全局地址重叠和数据不足单侧继续的区别。
- [ ] 验证查询失败、待确认、自动恢复、没有适用目标和页面重新挂载时的状态反馈。

**当前基线与待接线**　仪表盘与模拟页已有原同步状态和 DNS 模拟结果的展示。本步待消费 Step 8 的调度与预检摘要、Step 10～11 的站点状态和 Step 12 的模拟响应，并接到 Step 13 的配置界面。 [仪表盘](webui/frontend/src/views/Dashboard.vue) · [模拟组件](webui/frontend/src/components/DryRunResults.vue)

<a name="step-15"></a>

### Step 15 · 运行健康与日志

依赖 Step 8、Step 10 和 Step 14。范围为后端状态和既有观测链路。

**代码入口**　`Checker.Evaluate`、同步状态发布、事件总线及 `StoreLogWriter`。 [运行健康](internal/health/health.go) · [日志写入](webui/api/logwriter.go) · [状态模型](Design6.md#view-reference)

**实施顺序**

1. 总暂停时沿用现有健康边界。根据启用来源及其到期时间评估调度，避免 EdgeOne 的 72h 正常等待被 DNS 5m 判定为卡住。
2. 记录来源与站点的真实结果。已查实重叠不能在无目标计数时走 `idle`；数据不足单侧继续必须能追溯未完成的来源。
3. 复用现有同步失败、运行健康和日志行为。长期冲突的通知按状态边沿处理，成功恢复记一条恢复事件。日志只写安全字段。

**验收方式**　用固定时钟覆盖仅 EdgeOne 正常等待、EdgeOne 过期、仅 DNS、两项全局阻断、单侧失败、恢复、总暂停。断言 `GET /api/health/operational` 与内部评估一致、静态 `/api/health` 继续可用；事件被实际日志链消费，无凭据或 SDK 原始错误载荷。参考 `TestEvaluateScheduleStallAndStartupGrace`、`TestOperationalEndpointUnhealthy503`。建议 `go test ./internal/health ./webui/api ./syncer -race -count=1`。

**本步改动边界**　主改动在 `internal/health`、同步结果事件、`webui/api/logwriter.go` 及其测试。使用现有事件总线与日志链，保持静态 health、告警开关和 shutdown 顺序。

**收口判据**　固定时钟下，EdgeOne-only 在 72h 周期内正常等待健康；超过自己到期边界的异常可见。双开全局阻断不能被空目标计数误判为 idle；一侧继续时保留失败来源。运行健康、日志与站点状态对同一轮能互相核对。

**Checklist**

- [ ] 运行健康按启用功能及其各自的到期状态判断。保留静态 health 语义，防止 EdgeOne 正常低频等待被误判为 DNS 调度停滞。
- [ ] 记录冲突、单侧继续、恢复和站点确认结果。区分真实错误与无任务，按现有事件合同写日志和告警，验证无凭据泄露。
- [ ] 验证长期冲突、自动恢复、总暂停、单侧失败和仅 EdgeOne 运行的状态与健康回归。

**当前基线与待接线**　已有运行健康、同步事件与日志链。调度停滞目前仍按单一间隔和最近轮次判断。待使用分来源的到期状态、冲突阻断与恢复，以及站点确认结果扩展健康和日志。 [运行健康](internal/health/health.go) · [同步日志](webui/api/logwriter.go)

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

**本步改动边界**　主改动在验收夹具、回归测试及证据记录。产品代码缺陷回写到对应 Step 的范围后再重跑相关门禁。无需真实账号就能执行本步。

**收口判据**　按下方跨步验收矩阵记录云动作计数、SQLite 状态、同步结果和 API 输出，补上有界退出与只读模拟的真实二进制测试。然后执行全量门禁。完成判据包含目标行为和回归证据，进程存活或页面加载本身不足以勾选。

**Checklist**

- [ ] 验证新库和旧库启动、站点绑定、间隔热更新、v3 导入/v4 往返、只读模拟和真实二进制进程行为。
- [ ] 跨目标重叠时两侧云写入与确认均为零。已知冲突重启后保留，查实解除后自动恢复；无已知冲突时验证失败侧停止、正常侧继续。
- [ ] 验证一站多目标的全部回读、版本重查、自动确认、实际生效后清理，以及 DNS 单独运行的现有回归。
- [ ] 运行受影响包定向 race、`make all`、适用的依赖审计及 `git diff --check`，记录 Go/Node 版本、真实二进制和浏览器结果。

**已具备的基线证据**　Step 1～7 工作树已通过 `make all`、12 个 Go 包的非缓存 race 回归及 154 项前端行为测试。现有真实进程回归覆盖空库启动、SIGTERM 退出和 v4 跨 SQLite 实例往返（含站点绑定重映射）。这些结果可作为接线前的对照。待完整功能接入后，以同一进程验证双功能调度、冲突与重启恢复、模拟、确认和清理，完成浏览器验证、依赖审计及最终全量门禁。 [阶段修复证据](#step-1-7-review-fixes) · [进程级测试](main_test.go)

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

**本步改动边界**　主产物是真实站点/目标及发布记录、README 与已实施合同的证据更新。实际云写入和发布依已有授权执行，候选验收不自动部署或打 tag。

**收口判据**　真实只读查询、当前版目标写入、待更新自动确认、新版生效与清理、远端 CI/镜像分别给出结果。找不到实际待更新窗口就记录相应项目待补，保留已取得的其他证据；不要用模拟响应填写真实结果。

**Checklist**

- [ ] 用付费站点验证只读权限、源站防护状态、实际 CIDR、待更新版字段及目标配额，记录可用的 `RequestId` 和脱敏响应证据。
- [ ] 以明确纳入的真实测试源站和安全组核验一站多目标、实际写入和回读、确认与生效时序。无待更新版本时记录该场景未覆盖。
- [ ] 更新 README、生产验收清单与阶段实施记录；逐项区分本地模拟、真实云和远端发布证据。
- [ ] 如执行发布，记录 tag/提交、远端 Actions、目标架构和实物镜像；待补项目按实际状态登记。

**当前证据**　仓库记录的 `v3.0.2` 为 Build8 前的发布基线。Build8 的 TEO 查询证据来自本地模拟服务；真实站点与目标、实际待更新窗口、Build8 对应的远端 CI 与镜像均待本步取证。 [发布基线](AGENTS.md) · [TEO 本地证据](#step-5)

<a name="acceptance-matrix"></a>

## 跨 Step 验收矩阵

以下用例使用正式入口配本地替身。可共用夹具，每行应留下独立断言。云写入指新增和删除的实际调用，确认单独计数。需要证明某个安全门有效时，应让该门前面的条件可通过，避免测试因为更早的无关条件退出而误判。

| 场景与夹具 | 断言 | 收口 Step |
|---|---|---|
| 旧库无 TAG，已有 `[auto-dns]` 云规则；新库首次启动及 reset | 旧库保留 `auto-dns` 归属，新库和 reset 得到 `auto-ddns`；重启后不反复迁移。 | [Step 1](#step-1) |
| 两项都关闭、仅 DNS、仅 EdgeOne、双开；各自重复测试总暂停 | 只查询和调度有资格的来源；总暂停无新正式轮；只读模拟仍可用。 | [Step 8](#step-8)、[Step 12](#step-12) |
| EdgeOne 间隔从 `72h` 改为 `36h`，再保存等价时长；缓存已到期或失效 | 只重排 EdgeOne 的计时。等价值不重置。过期缓存不被当作完整输入，其他来源按冻结决策处理。 | [Step 2](#step-2)、[Step 8](#step-8) |
| DNS 与 EdgeOne 绑定不同目标、端口不同，DNS 命中 EdgeOne 网段 | 无任一目标新增或删除，无站点确认。即使另一项查询失败，已有命中仍阻断。 | [Step 7](#step-7)～[Step 9](#step-9) |
| 首次查实冲突但 SQLite 写锁阻断；下一轮 DNS 数据不足；重试保存成功后重启 | 初次、下一轮及已提交状态重启后的两侧写入和确认均为零。完整输入无冲突且解除提交成功后才恢复。 | [Step 7](#step-7)、[Step 16](#step-16) |
| 首轮查实重叠且 SQLite 读取失败；后续 DNS 数据不足，SQLite 稍后恢复 | 保留本轮冲突证据。读取恢复前继续阻断两侧且只重试读取；恢复后保存证据，完整无重叠且解除提交成功后才放行。Step 7 已有状态层回归，正式入口调用计数待验。 | [Step 7](#step-7)、[Step 8](#step-8)、[Step 16](#step-16) |
| 双开且无已知冲突，DNS 查询失败，EdgeOne 所有目标覆盖有效 | DNS 零写入；EdgeOne 按本轮可用输入写入、回读，重查与状态提交通过后恰好一次确认。 | [Step 8](#step-8)～[Step 10](#step-10) |
| 双开且无已知冲突，某一必需 EdgeOne 站点无可用数据 | EdgeOne 零写入及零确认；可用 DNS 继续，状态明确显示单侧继续。 | [Step 7](#step-7)～[Step 9](#step-9) |
| 一个站点绑两个物理目标；分别注入 S1 不全、平台能力限制或配置变化 | 缺任一证明时零确认。所有目标达到同轮覆盖并重查相符的正向对照恰好一次。 | [Step 9](#step-9)、[Step 10](#step-10) |
| 首次确认成功后同一指纹仍出现，或者确认超时且重查不明 | 已确认的指纹跨轮和重启不重发；未知时只读，明确未执行且再次满足覆盖时才允许重试。 | [Step 10](#step-10) |
| 版本 A 当前、B 待更新，重启后只看到 A 且 DNS 命中 B；随后证实 B 生效 | B 仍受保护时两侧零写入。B 生效后 A 是否实际可删由 S1、TAG、定位及其他站点的期望决定。 | [Step 11](#step-11) |
| 正式计划与 Dry Run 共用同一过渡记录 | 冲突、保护集合与清理候选一致。模拟新增、删除、确认和正式状态更新均为零。 | [Step 12](#step-12) |
| 只有 EdgeOne 启用且正常等待，或存在全局冲突且无目标结果 | 正常等待健康；已查实冲突保持可见。运行健康不把两者误报成 DNS 卡住或无任务。 | [Step 15](#step-15) |

执行 Step 16 时，从实际计数器、当前快照或 SQLite 查询取证。应用层结果、SDK 请求与运行健康至少能关联到同一轮或站点，记清配置代次、版本或 RequestId 中适用的字段。
