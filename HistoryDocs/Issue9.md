# Issue9.md — 问题与维护归档

> **2026-11-09 归档，用户授权。** 本阶段已确认的修复已实施，SMTP 测试经用户确认通过。SQLite 连接容量按既有裁决暂缓，特定平台和长期运行证据按需补充。当前项目状态见 [AGENTS.md](../AGENTS.md)，历史文档见 [归档索引](README.md)。下文保留各次实施和核验时的证据范围。

> **建立日期：2026-11-02，用户授权。** 本文件建立时作为唯一活跃问题台账，承接 [归档 Issue8](Issue8.md) 的全部未完事项，以及本聊天对第三方审阅报告的筛选结果。旧编号不重用，新事项从 `I9-R01` 连续编号；完成后保留结果与日期。
> **产品边界：** 个人使用、内部访问、简单稳定的静默工具；允许过期目标、旧 IP 或旧端口未及时清理。不要求高精度清理、完整审计或极端异常防护。候选、条件性限制和证据范围不等于产品缺陷，更不自动成为发布阻断项。
> **建账范围：** 建立台账时只更新和归档文档，未实施下列代码、测试、依赖、构建配置或部署改动，未创建提交、标签或发布。后续按具体事项安排实施并在对应条目记录；归档表示阶段完成或剩余事项已转交，不表示所有外部链路均已通过。
> **后续实施（2026-11-02）：** 用户选择 A 并授权修复 I9-R01，现已落地独立 SDK/HTTP 读取实例与正式回归；本地验证及未发布边界见该项。其他事项未随本次修复实施。
> **I9-R03 后续实施（2026-11-02）：** 同日研究后用户选择 A，已实施历史空串读取出口修复与对应回归；范围及验证记录见 I9-R03。此记录不改写上轮建账和 I9-R01 实施时的事实。
> **I9-R04 后续实施（2026-11-04）：** 用户确认保留显式空正文并授权推荐 A，已修复 GET/导出误补默认正文的问题；正式范围、验证及未发布边界见 I9-R04。
> **I9-R05 后续实施（2026-11-04）：** 用户选择 A，已修复独立专项入口的路径与执行证据判定，并新增入口回归；生产代码与 Make/CI 保持，范围及验证见该项。
> **I9-R06 后续实施（2026-11-04）：** 用户授权推荐 A，已将默认读取预算回归拆为阶段同步取消、独立真实 deadline 与有裕量的跨页预算验证，并同步改用信号协调操作隔离；仅修改测试和文档，范围及本地证据见该项。

## 1. 归档基线与证据

- **2026-11-09 归档前全表复核。** 本地 `main` 为 `a4375cb`，包含 I9-R09 的暂缓裁决。远端 `main` 为 `0a5c8d2`，包含 I9-R01～R06。I9-R07 的 Alpine 升级和 I9-R11 的测试更名在本地工作树，已通过构建和回归。最新发布标签仍为 `v3.0.1`，指向 `26e5c8f`。各实施记录中的「未提交」「未推送」描述对应批次结束时的状态，本次复核及总表记录归档前状态，发布结果另行补记。
- 建账前工作树干净，`main`、本地 `origin/main` 与当时查询的实际 GitHub `main` 均为 `26e5c8f7f824cd10c2bfa57e1c2177251db4e1cc`。该提交合并远端 13 个提交与本地 R26/R27；“本地缺少 R12～R22/R24”的审阅结论已经过时。
- `v3.0.1` 解引用为上述 SHA；[Actions run 41374458503](https://github.com/AlcaProphet/cloudhost-firewall-autoupdater/actions/runs/41374458503) 为 success，前端行为回归、依赖审计、Go 检查/测试及镜像构建推送步骤成功。因此 R26/R27 已提交、推送并进入本次发布。发布证据不证明用户当前运行容器已升级，也不证明新的真实云或 SMTP 验收。
- 同日上轮只读核对：前端统一 `npm test` 151 项通过，失败/取消/跳过均为 0；`govulncheck@v1.9.0` 在 Go 1.27.1 下报告 11 项符号级可达标准库告警；仓外临时 overlay 复现 SWAS 数据竞争、历史空串 targets 的 null JSON、空正文 GET/导出不一致，以及两类保守清理行为。
- R06 专项正常路径执行通过；经 macOS `/tmp` 符号链接执行时，两个包均显示 `[no tests to run]` 却退出 0。该问题不否定已经实际运行过的历史测试，转 I9-R05。
- 上述测试与联网结果来自建账前的只读审阅。建账时没有重新执行产品门禁、漏洞扫描、真实云、SMTP 或容器部署；发布基线固定 Go 1.27.1 与 Alpine 3.20。当时没有对 GHCR 镜像另做二进制漏洞扫描；后续源码与镜像维护见 I9-R02、I9-R07。
- 用户 2026-10-10 确认普通云和浏览器等真机验收收口。2026-11-09 用户确认 SMTP 测试通过、无问题。Webhook/Kuma 继续豁免。

## 2. 归档事项总表

本表保留各项实施、验证和发布状态。暂缓项保留触发条件，按需补证。本表没有 P0 项。

**完成情况。** I9-R01～R07 的修复或维护改动已实施，I9-R11 的测试命名已修订。I9-R08 已按用户确认收口。发布与 I9-R11 的示例同步由发布流程跟进。I9-R09 按用户裁决暂缓，I9-R10 按需补充证据。复核未发现需要继续修复的已确认代码缺陷。

| 编号 | 内容 | 状态与优先顺序 | 来源 |
|---|---|---|---|
| [I9-R01](#i9-r01) | SWAS 共享 SDK client 数据竞争 | 已修复并进入远端 `main`（`b09167e`）；待发布 | 第三方 P1-2；同日双请求 race 复现 |
| [I9-R02](#i9-r02) | Go 标准库安全补丁与构建工具链一致升级 | 已进入远端 `main`（`590a7d6`），本地扫描通过；待发布 | 第三方 P0-2，撤销 P0 定级 |
| [I9-R03](#i9-r03) | 历史空串 targets 返回 null，前端删除报错 | 已修复并进入远端 `main`（`196816a`）；待发布 | 第三方 P1-3 / C-01 |
| [I9-R04](#i9-r04) | 空邮件正文保存与 GET/导出不一致 | 已修复并进入远端 `main`（`ba07a90`）；待发布 | 第三方 P2-1 / C-02 |
| [I9-R05](#i9-r05) | R06 专项脚本在符号链接路径下空跑成功 | 已修复并进入远端 `main`（`5e1dbec`）；独立测试工具 | 第三方 P2-2；原 I8-R06 |
| [I9-R06](#i9-r06) | 读取预算测试的短墙钟时序敏感性 | 已修复并进入远端 `main`（`0a5c8d2`）；测试维护 | 第三方 P2-6；受控延迟验证 |
| [I9-R07](#i9-r07) | Alpine 运行基础镜像维护 | 2026-11-09 按 A 本地升级并验证；待发布 | 第三方 P0-2 附带项 |
| [I9-R08](#i9-r08) | 真实 SMTP 测试、自动告警及健康异常邮件 | 2026-11-09 用户确认 SMTP 测试通过、无问题；验收收口 | I8-R01；PT-B7-01/02/06 |
| [I9-R09](#i9-r09) | SQLite 连接容量 | 2026-11-09 已研究，用户选择 A 继续暂缓 | I8-R14 未实施子项 |
| [I9-R10](#i9-r10) | 特定平台与长期稳定性证据范围 | 按需核对，不是一般发布阻断 | I8-R23 |
| [I9-R11](#i9-r11) | 测试命名、历史统计引用与部署示例版本 | 测试命名已本地修订；示例待下一次发布验证通过后更新 | I8-R25；第三方文档/示例观察 |

## 3. 事项详情

<a id="i9-r01"></a>

### I9-R01 SWAS 共享 SDK client 数据竞争

- **原问题与研究结论：** [AliSWAS.GetSnapshot](../provider/ali_swas.go) 原来直接调用池中共享 client 的 `ListFirewallRules`；OpenAPI v2.2.4 的 `GetRpcHeaders` 读取并清空 client.Headers，两个并发读取即可由 `-race` 报告竞争。只独立 SDK client 仍不足够：tea v1.5.3 的 dara 默认 HTTP 池在发送前修改共享 `http.Client.Timeout`，与另一请求中的读取竞争。源码依据见 [OpenAPI](https://github.com/alibabacloud-go/darabonba-openapi/blob/v2.2.4/client/client.go#L1757-L1762) 与 [tea/dara](https://github.com/alibabacloud-go/tea/blob/v1.5.3/dara/core.go#L349-L357)。没有据此证明生产中已经发生认证错误或误删。
- **正常触发路径：** 正式同步同平台目标内部串行，但 Dry Run 与正式同步可使用同一 Provider；连接测试另建 Provider 仍复用池条目。各入口可重叠，单 Provider 锁或仅保护池查表不能覆盖全部共享调用。
- **用户裁决与实施（2026-11-02，A）：** 基线 `main / d50daa5b9e619e70a7818349dcc6039bd125199f`，实施前工作树/暂存区干净。唯一生产文件为 `provider/ali_swas.go`：每次快照以 `context.Background()` 创建操作私有 `newAliReadHTTP`，通过 `swas.NewClient(aliReadConfig(...))` 从基础 client 的不可变配置/凭据创建独立 SDK client；不整体浅拷贝、不访问 Headers。分页内复用同一连接，所有返回路径经 `defer operation.close()` 收尾。代价是快照之间不共享读取连接，可能增加建连开销，未做性能量化。
- **保持的合同：** 单请求 Connect 10s/Read 30s/HTTP 整体40s，PageSize=100 与最多100页保持；不增加120秒快照总预算，也不接入HTTP断开/Stop新取消源。既有总数/分页完整性、TAG、S1/S2、稳定RuleID删除、失败计数和重试分类保持。ClientPool、共享读取设施、ECS、扫描、API/schema、前端、SDK/依赖、工具链与镜像配置均未修改；现有写调用仍按正式同步串行，不扩为任意并发写安全合同。
- **正式回归：** [provider/swas_concurrency_test.go](../provider/swas_concurrency_test.go) 覆盖同实例双读、同池不同实例双读、读取与新增/删除/扫描重叠；各快照跨两页，要求两条操作实际同时到达HTTP端点，并核对完整结果与请求数。另覆盖正常成功、第二页SDK错误及总数变化的空快照/错误链，检查分页内连接复用和各出口的空闲连接关闭。[syncer/swas_concurrency_test.go](../syncer/swas_concurrency_test.go) 走真实SWAS SDK、正式syncAll与DryRun重叠，核对覆盖、success、S0/S1读取数与零额外写入。均使用本地HTTP与虚假凭据，DNS/等待使用现有本地替身。
- **本地验证（2026-11-02）：** 最终正式文件执行 `go test ./provider ./syncer -race -run '^TestI9R01' -count=20 -timeout=2m` 通过。`make all` 通过：真实 `npm ci`/前端构建、151项行为测试（fail/cancelled/skipped均0）、`go vet ./...`、全仓12包 `go test ./... -race -v` 与产品构建均成功；其中6包使用Go测试缓存，provider/syncer/webui/API及其余受影响包实际执行，不称12包禁缓存重跑。`gofmt`检查与`git diff --check`通过。环境为macOS darwin/arm64、Go1.27.1、Node24.21.0、npm11.19.0。首轮Make在前端构建后因默认Go启动器1.26.4配合`GOTOOLCHAIN=local`而于vet前拒绝运行；随后显式使用本机已有Go1.27.1、保留`GOTOOLCHAIN=local`重跑完整入口成功，未修改工具链配置。
- **反向判别：** 对正式新测试使用仓外overlay：恢复原实现（并发专项与同步/预览各20轮）报race；只独立SDK而保留默认HTTP池（并发专项20轮）仍报Timeout race；移除操作close（成功/SDK错误/总数变化各1轮）均被连接关闭断言判失败。反例均明确非零，未修改正式源码或加入生产测试hook。
- **范围与发布状态：** 共五文件（一份生产、两份新增测试、AGENTS/Issue9）。本轮尚未部署、提交或推送；未执行真实云、SMTP、浏览器、Linux/Docker或新revision的远端CI/GHCR，不外推外部验收或长期稳定通过，也不将本地修复写成已进入v3.0.1。既有用户真机收口与SMTP人工清单保持。

<a id="i9-r02"></a>

### I9-R02 Go 标准库补丁与构建工具链

- **原问题。** 建账时 Go 1.27.1 的源码扫描报告 11 项符号级可达标准库告警，按常规安全维护定级。相关补丁见 [Go 发布记录](https://go.dev/doc/devel/release#go1.27.3) 和 [x509 公告](https://pkg.go.dev/vuln/GO-2026-6809)。
- **已实施。** `590a7d6` 已进入远端 `main`。`go.mod`、[Docker 编译阶段](../build/Dockerfile)、[CI](../.github/workflows/docker-publish.yml) 同步 Go 1.27.3，保留 `GOTOOLCHAIN=local`，并增加 SDK 更新及前端构建后的 `govulncheck@v1.9.0` 符号级阻断门禁。
- **2026-11-09 复核。** 仓外用 Go 1.27.3 对 `linux/amd64` 源码分别以锁定依赖和复现 CI 七条 SDK 更新后的依赖扫描，均报告 0 项符号级可达、0 项已导入包漏洞。两组均有两项 `x/net/html` 模块级公告。SDK 更新后的 `provider` 与 `syncer` 整包 race 通过。本地新镜像确认用 Go 1.27.3 静态编译。[GO-2026-6860](https://pkg.go.dev/vuln/GO-2026-6860) · [GO-2026-6866](https://pkg.go.dev/vuln/GO-2026-6866)
- **发布边界。** GHCR 的 `3.0.1` 和 `latest` 在复核时指向同一镜像，其二进制仍为 Go 1.27.1。新 revision 的远端门禁和镜像推送待发布时核对。

<a id="i9-r03"></a>

### I9-R03 历史空串 targets 的 JSON 兼容

- **原问题与复核：** [parseRuleTargets](../config/store.go) 明确兼容历史 `rules.targets=''`，但原实现返回 nil；GET `/api/rules` 直接编码为 `targets:null`。[Rules.vue](../webui/frontend/src/views/Rules.vue) 删除确认使用 `[...row.targets]`，会抛 TypeError。同日仓外探针以临时 SQLite 走 `GetRules`、完整业务快照、只读事务和正式 API，均复现该表示差异；编译真实 Rules SFC 脚本复现确认前报错、零 DELETE。直接后端 DELETE 正常，列表展示与编辑已有容错，导出的 `target_export_ids` 已独立返回 `[]`；不能把本项扩大为同步或导出故障，也未检查用户实际数据库是否存在该历史数据。
- **方案研究与裁决：** 用户选择 A：在公共历史兼容出口返回非 nil 空数组，直接消除当前 API 根因。前端单独兜底只能修复按钮而遗留 API 契约问题；API handler/DTO 单独归一化可行但使相同读取值在不同出口重复补齐；后端加前端兜底可兼容旧后端的 null，但当前单二进制包含前端、没有该额外兼容需求，故未采用。启动迁移/自动修库会增加持久化变更而无必要；为此切换普通 API 的 JSON 编码器影响面过大。`DeepCopyRules` 仍可将空切片拷为 nil，当前既不参与规则 GET 的直接编码，导出又有独立数组构造，故不扩为运行时全局非 nil 合同。
- **正式实施（2026-11-02）：** 实施前 `main / 590a7d6eaf0f471f5eb8c2dc849efd0362619018`，工作树/暂存区干净。唯一生产变更是 `config/store.go` 的历史精确空串分支从 `return nil, nil` 改为 `return []int{}, nil`。新写入仍由既有 `ruleColumns` 归一化。前端生产代码、schema/迁移、API 请求 DTO、JSON 编码器、SDK/依赖和同步流程均未修改。
- **保持的合同：** 空数组仍表示适用于全部目标；有效非空数组保留引用，SQL NULL、JSON null、纯空白和原有损坏值仍失败。缺失/null 请求不新增兼容，悬空引用校验与删除安全门保持。不自动改写存量数据库，不把未知或损坏值归为全部目标，不增加人工验收阻断项。
- **正式回归：** [store_corrupt_test.go](../config/store_corrupt_test.go) 强化既有四态测试，历史空串和显式空数组必须非 nil、长度为零，读取后存量文本保持不变，增加纯空白拒绝对照；其余损坏值原回归保留。[rules_test.go](../webui/api/rules_test.go) 新增 `TestI9R03RuleTargetsJSON`，临时 SQLite 经正式 GET 用 RawMessage 区分 `[]`、null 与缺失，覆盖历史空串、显式空数组、限定目标，并核对 GET 零发布与正式 DELETE 仅发布一次。[delete-confirm.test.mjs](../webui/frontend/tests/delete-confirm.test.mjs) 编译真实页面脚本，通过真实行按钮验证空数组规则可打开确认、取消零删除、确认仅发送一次 DELETE。
- **反向判别：** 仓外 overlay 只恢复原生产 `store.go`，正式 `TestLoadRulesTargetsFourStateContract` 与 `TestI9R03RuleTargetsJSON` 均在历史空串场景明确失败（分别为 nil 和 API null）；未通过测试替身修改生产行为。研究候选还核对了 GET/导出对 SQL NULL、JSON null、纯空白、对象与混合类型数组继续返回 500，以及普通 POST 的缺失/null targets 继续返回 400。
- **本地验证（2026-11-02）：** 正式 `go test ./config ./webui/api -race -run '^Test(I9R03RuleTargetsJSON|LoadRulesTargetsFourStateContract|CorruptTargetsBlocksAllLivePaths|I8R09SnapshotReferenceIntegrity|RuleTargetsMustBeExplicit)$' -count=1 -timeout=90s` 通过。`make all` 通过：真实 `npm ci`/前端构建、152 项前端行为测试（fail/cancelled/skipped 均 0）、`go vet ./...`、全仓 12 包 `go test ./... -race -v` 和产品构建成功；其中 7 包使用 Go 测试缓存，根包/config/internal-health/webui/API 五包实际执行，不称全仓禁缓存重跑。受影响 Go 文件 gofmt 与 `git diff --check` 通过。环境为 macOS darwin/arm64、Go 1.27.3、Node 26.7.0、npm 11.19.0；单次全量绿色不外推长期稳定。
- **范围与发布状态：** 共六文件（一份生产、三份测试、AGENTS/Issue9）。本轮未提交、推送、部署或发布；未执行真实云、SMTP、产品浏览器、Linux/Docker 或新 revision 的远端 CI/GHCR，不外推外部验收或长期稳定通过，也不将本地修复写成已进入 v3.0.1。

<a id="i9-r04"></a>

### I9-R04 空邮件正文的保存和回读

- **原问题与复核：** `NormalizeAlertEmail` 允许并保留 `body=""`，保存和运行时一致；[GET](../webui/api/alerts.go) 与 [导出](../webui/api/bundle_v3.go) 原来仅凭 `Body == ""` 替换为默认正文。2026-11-04 临时 SQLite 经正式 PUT/GET/导出/导入复现：邮件渠道开启和关闭时均发生，导回后数据库和运行时也变成默认正文。GET/导出本身不写库；前端原样接收 GET，重新打开页面后再保存其他告警设置也可能写回默认文案。未发现防火墙同步或凭据安全影响。
- **研究与用户裁决：** 用户确认空正文表示不添加用户正文，自动邮件仍保留系统详情，并授权推荐 A。历史缺正文列已由启动迁移补默认值，新库/reset 也已有默认行；无需新增迁移。直接删两处正文兜底会破坏缺行默认值；读取层改为无行返回默认配置可行，但会改变 Store/快照的原值读取及 HTTP 补默认值合同；显式传递存在标记会扩大读取签名和快照结构。因此保留现有无行零值约定，只缩窄 HTTP 输出层的补齐条件。
- **正式实施（2026-11-04，A）：** 实施前 `main / 196816a67b8f522985b99685df31b2a6135b24b9`，工作树/暂存区干净。生产仅修改 `webui/api/alerts.go` 和 `bundle_v3.go`：在补安全模式、端口、主题等默认字段前判断邮件配置是否为整份 `AlertEmailConfig{}`，仅该情况补默认正文，移除原来按正文空串补齐的分支。合法 PUT/导入要求主题、端口、安全模式有效，所以空正文配置不会是整份零值；外部直接写入非法整份零值仍与缺行无法区分，保持当前接受的数据库边界。
- **保持的合同：** Store 无行返回零值、已有行按原值读取，GET 仍是同一窄只读事务；普通 PUT、运行时、GET、v3 导出/导入及重启保留显式空正文与正文空白。新库/缺行/reset/历史补列默认值、请求字段严格性、配置包 v3、事务发布、邮件开关和 SMTP 行为保持。自动邮件/测试邮件仍在两个换行后追加系统详情/测试说明，不删除空正文前的这两个换行。不迁移、不自动改写存量数据；已被旧版本导出为默认文案的备份无法自动还原原来是否为空。
- **正式回归：** 新增 [email_body_test.go](../webui/api/email_body_test.go)，临时 SQLite 经正式 PUT→GET→导出→导入覆盖渠道开启/关闭与空串、纯空白、多行和默认正文，检查数据库/运行时、读取零发布、每次合法写入仅发布一次，并在保存后和导入后分别重开数据库验证启动迁移不改正文；RawMessage 检查 body 存在且为字符串，避免缺失/null 被零值掩盖。另覆盖新库/删除邮件行/reset 的默认正文及缺行 Store 零值不变、独立构造空正文导入包、本地假 SMTP 下自动邮件与正式测试邮件 handler 的系统内容。前端 [alerts-load.test.mjs](../webui/frontend/tests/alerts-load.test.mjs) 编译真实 Alerts SFC，验证 GET 空串经保存 PUT 与测试发送 POST 保持为空；未修改前端生产代码。
- **反向判别：** 仓外 overlay 只恢复 HEAD 的两份生产文件，正式 `TestI9R04EmailBodyRoundTrip` 的渠道开启/关闭空正文场景及 `TestI9R04ImportEmptyBody` 均按 GET/导出/往返正文差异明确失败；另一 overlay 直接删除正文补齐，正式 `TestI9R04EmailBodyDefaults/absent` 按缺行 GET/导出失去默认正文明确失败。两组均以 `-race -count=1` 执行并返回非零，未替换测试或加入生产 hook。
- **本地验证（2026-11-04）：** 正式 `go test ./webui/api -race -run '^Test(I9R04|I818SnapshotGET|PutAlertsFourObjectsRoundTrip|ConfigExportEmptyDatabaseSchema|ResetRestoresAlertDefaults)' -count=1 -timeout=90s` 通过；前端 alerts-load 定向 12 项通过。完整 `make all` 通过：真实 `npm ci`/前端构建、153 项前端行为测试（fail/cancelled/skipped 均 0）、`go vet ./...`、全仓 12 包 `go test ./... -race -v` 与产品构建成功；12 包均本次实际执行，输出无测试缓存命中。受影响 Go 文件 gofmt 与 `git diff --check` 通过。环境为 macOS darwin/arm64、Go 1.27.3、Node 26.7.0、npm 11.19.0；Make 显式选用本机 Go 1.27.3 并保留 `GOTOOLCHAIN=local`。首轮 Make 因用户全局 npm 缓存 EACCES 在前端安装阶段退出，随后使用仓外本轮独立 `npm_config_cache` 重跑完整入口成功，未修改仓库配置或全局缓存权限。单次全量绿色不外推长期稳定。
- **范围与发布状态：** 共六文件（两份生产、一份新增 Go 测试、一份前端测试及 AGENTS/Issue9）。本轮未提交、推送、部署或发布；未执行真实云、SMTP/收件箱、产品浏览器、Linux/Docker 或新 revision 远端 CI/GHCR，不外推外部验收或长期稳定通过，也不写成已进入 v3.0.1。既有人工收口/豁免和 SMTP 待验收清单保持，不新增人工阻断项。

<a id="i9-r05"></a>

### I9-R05 R06 专项入口不能以空跑判绿

- **原问题与复现：** [run_integration.py](../testdata/i8-r06/run_integration.py) 已对脚本和 overlay 路径使用 resolve，但 subprocess 只设 cwd、仍继承逻辑 PWD。2026-11-04 在 `main / ba07a906dbc1b4182dfc3d440448ffaec710f32d`、干净工作树复核，原脚本真实路径 `--count 1` 通过；符号链接目录与同值 PWD 下两个包均为 `[no tests to run]`、退出 0。两种结果保留，不否定历史上实际执行过的专项。
- **根因与取舍：** Go 1.27.3 的 `os.Getwd` 对仍指向当前目录的绝对 PWD 优先返回该别名；本机 `go list` 证明包 Dir 随 PWD 改为别名路径，而 overlay 键仍为真实路径，导致夹具未注入。单独 `go -C <真实路径>` 仍保留同一别名，不能修复。第二层问题是入口只信任退出码：零匹配、Skip，甚至移除 `normal` 子场景后顶层测试继续 PASS，Go 都可退出 0。仅补 realpath、匹配 PASS 文本或只检查两个顶层测试不满足完成条件；预先 `-list` 也不能证明场景实际执行。依据见 [Go Getwd 文档](https://pkg.go.dev/os#Getwd) 与 [Go JSON 测试事件协议](https://pkg.go.dev/cmd/test2json#hdr-Output_Format)。
- **用户裁决与正式实施（2026-11-04，A）：** 保留现有独立入口和 testdata 夹具，不迁入默认 Go/Make/CI，不改生产预算或引入预算注入接口。脚本统一真实 repo、cwd、子进程 PWD 与临时 overlay 路径；启动 Go 前检查两份夹具及预算唯一锚点。使用 `go test -json`，实时转发 Output，按完整包名和测试名核对两包各一次 package pass、两个顶层测试与 15 个子场景各恰好 `--count` 次 run/pass。任意 fail/skip、零匹配、缺场景、次数不足、非 JSON 事件或子进程非零均失败，诊断指出具体缺项；必需场景清单独立于夹具，避免删场景后自动缩减验收范围。保留继承的其他环境与既有 `-race`、`-count`、每包 `-timeout=2m`。
- **正式入口回归：** 新增 [test_run_integration.py](../testdata/i8-r06/test_run_integration.py)，命令为 `python3 -B -m unittest discover -s testdata/i8-r06 -p 'test_run_integration.py' -v`。13 项测试使用隔离目录和 Go 命令替身，验证真实/符号链接/无关启动目录下 cwd/PWD/overlay 的一致、120 秒原源文件及 150ms 临时副本、命令参数、按包及重复次数验收，以及空跑、缺子场景、Skip、非零/fail、非法 JSON、缺夹具、预算锚点不唯一和非法 count 的失败传播。它们仅验证入口，不冒充真实 SDK 专项；继续独立运行。
- **本地实际验证：** 上述 13 项回归通过；正式 `python3 testdata/i8-r06/run_integration.py --count 3` 在真实路径和符号链接目录分别通过，两次均实际核验两个顶层测试及 15 个子场景各三次 run/pass，包含真实 SDK→正式目标链与 handler→临时 SQLite。另从 macOS `/tmp` 下的 repo 符号链接、保留该逻辑 PWD 运行同一正式入口 `--count 3` 通过。`git diff --check` 通过。环境为 macOS darwin/arm64、Go 1.27.3、Python 3.13.3；不从本机专项推定 Linux 或长期稳定性通过。
- **反向判别：** 仓外临时 overlay 配合正式 `run_tests` 与真实 Go，分别故意改为不匹配的测试名、移除 `normal` 场景、顶层显式 Skip，Go 均返回 0，但正式入口结果核验均返回 1，并准确报告缺项/Skip。仓外脚本变异分别恢复继承 PWD、从必需清单删掉 normal，新增路径回归与缺场景回归均明确变红；无产品代码 hook。研究原型还确认继承 `GOFLAGS=-skip=TestReadBudgetTargetSafety/normal` 的场景缺失会被核验拒绝。
- **完整专项复核（2026-11-04）：** 用户明确要求执行修复并同步文档后，确认 HEAD 仍为 `ba07a906dbc1b4182dfc3d440448ffaec710f32d`、工作树保留上述四文件修改，无需重复改写实现。13 项入口回归再次通过；正式 `python3 -B testdata/i8-r06/run_integration.py --count 20` 在真实仓库目录和 `/tmp` 下的 repo 符号链接目录各通过一次，后者显式保留符号链接逻辑 PWD。两次均带 race，两个顶层测试及 15 个子场景各实际 run/pass 20 次，未以 Go 缓存或零匹配判绿。另用正式 CLI 分别继承 `GOFLAGS=-skip=^TestReadBudget` 与 `GOFLAGS=-skip=TestReadBudgetTargetSafety/normal`，在 Go 包仍成功时均退出 1，分别报告全部测试缺失和仅 normal 场景缺失。Python 两文件语法检查与 `git diff --check` 通过；本次仅补充文档验证记录，未增加实现范围。
- **范围与发布边界：** 共四文件（专项脚本、新增 Python 回归及 AGENTS/Issue9）；生产 Go、两份 Go 夹具、SDK/依赖、120 秒产品预算和 150ms 临时预算、Make/CI/Docker 均未修改。未执行 `make all`、全量 Go/provider 测试、前端构建、产品浏览器、真实云/SMTP、Linux/Docker 或远端 CI/GHCR；既有 I9-R06 时序候选独立保留，不为本项扩大测试预算。本轮未提交、推送、部署或发布。

<a id="i9-r06"></a>

### I9-R06 读取预算测试的时序敏感性

- **原问题与证据：** [TestReadBudgetCancellation](../provider/read_budget_test.go) 原使用 40ms 预算，后页场景要求第二次请求在到期前发出，另以 160ms 墙钟上限判断取消，服务端 200ms 后自行补回响应；[TestReadBudgetSharedAcrossPages](../provider/read_budget_test.go) 原使用 65ms 总预算、每页等待 12ms 并要求至少两次请求。第三方报告过一次高负载失败及随后重复通过；本聊天研究时原取消/跨页/隔离测试 `-race -count=20` 通过，没有自然复现其具体失败。仓外只给首页响应增加 120ms 延迟，即令原 10 个后页取消场景及 2 个跨页场景因 `calls=1 want=2` 或 `calls<2` 失败，此时产品正确返回 deadline 错误和空结果。由此确认测试时序敏感性，不据此认定原报告失败的具体原因或产品竞态。
- **取舍与用户裁决：** 2026-11-04 用户授权推荐 A。取消矩阵用请求阶段信号，真实 deadline 身份与跨页总预算独立保留；仅加大所有时间参数仍混合阶段到达和取消正确性，全部手动取消会丢失自动到期证据。现有真实回环网络不能直接套用虚拟时间而保持原网络模型，本项不为此引入内存网络、生产测试 hook 或自定义伪 deadline context。维持低优先级测试维护，不增加人工验收或发布阻断项。
- **正式实施：** 唯一 Go 文件为 `provider/read_budget_test.go`。保留五条路径（ECS 规则快照及四平台扫描）×四阶段的 20 个取消场景，服务端在指定页收到请求、正文场景已写入并 Flush 部分响应后发信号，再由测试调用标准 `cancel()`；准确检查请求次数、空结果、`ErrSnapshotIncomplete + Canceled` 且不含 DeadlineExceeded，去掉服务端 200ms 自动恢复和 160ms 返回上限。新增 `TestReadBudgetDeadline` 的五路径真实 250ms 自动到期回归，检查不完整与 DeadlineExceeded、排除 Canceled；它不要求短预算内已进入指定请求阶段，在途与后页覆盖由前述矩阵承担。跨页测试仍用持续新 token 和真实共享 deadline，测试预算改为 500ms，保留至少两次请求和空结果断言。异步结果使用缓冲通道，阶段/完成等待使用 3s 测试保护，失败后释放/关闭服务端并通过完成信号等待读取协程退出；阿里云公共读取夹具单请求超时设为 20s，晚于 3s 保护，防止取消丢失后靠原单请求自然超时判绿。全部数值只属于测试，不是产品返回时限。
- **操作隔离补强：** 同文件 `TestReadBudgetOperationIsolation` 移除 35ms/85ms 顺序假设，等两个独立操作同时在途后取消并关闭其中一个，另一个再显式放行、完成并连续复用同一连接；保留原池不变检查，明确两条独立连接，并以 ConnState 完成信号检查关闭，代替毫秒轮询。该测试此前未复现失败，本次属于同类夹具维护，不新增已确认产品缺陷。
- **本地验证：** `go test ./provider -run '^TestReadBudget' -race -count=20 -timeout=3m` 通过，包含全部上述回归及既有成功/已过期零 HTTP/原单页超时/关闭/原池并发/TLS/代理用例。仓外 overlay 仅给正式测试的后页场景首页及跨页首页增加 120ms 延迟，`go test -overlay=<临时文件> ./provider -run '^TestReadBudget(Cancellation|SharedAcrossPages)$' -race -count=20 -timeout=2m` 通过。真实 `make all` 通过：npm ci、vue-tsc/Vite 构建、153 项前端行为测试（失败/取消/Skip 均 0）、Go vet、全仓 12 包 race 结果和产品二进制构建；其中根包/provider 实际执行，其他 10 包缓存命中，不称为全仓禁缓存重跑。npm ci 报 0 漏洞，另提示 fsevents 安装脚本未获 allowScripts 覆盖，未变更依赖或脚本权限；未独立运行 npm audit 或 govulncheck。本机 Go 1.27.3 darwin/arm64、Node 26.7.0。gofmt 与 `git diff --check` 通过。
- **正式回归反向判别：** 仓外生产 overlay 分别移除 `req.WithContext(a.ctx)` 绑定、允许 ECS 在后页失败时返回已累计规则、将预算错误身份改为 Canceled、令 ECS 整次读取脱离传入 context。正式取消矩阵在三条阿里路径的 later_body、ECS 两个后页阶段、五路径真实 deadline 回归及 ECS 跨页测试分别明确失败；脱离 context 的在途/持续分页由 3s 保护失败并正常回收夹具。未把 Go 全包超时或编译失败当作判别证据。
- **范围与边界：** 共三文件（上述一份 Go 测试及 AGENTS/Issue9），生产代码、120 秒预算、错误分类、SDK/依赖、目标状态机、API/schema、前端及 Make/CI/Docker 不变；独立跨包专项脚本、两份夹具及 150ms 临时预算未修改，本轮未重跑该专项，不能据默认门禁外推专项已执行。真实时间跨页测试及秒级测试保护仍可能受足够长的机器停顿影响，不宣称任意负载下永不失败或严格生产墙钟保证。本轮未提交、推送、部署或发布，未执行真实云/SMTP、产品浏览器、Linux/Docker 或远端 CI/GHCR，不外推外部验收或长期稳定，也不写成已进入 v3.0.1。既有人工收口/豁免和 SMTP 待验收清单保持。

<a id="i9-r07"></a>

### I9-R07 Alpine 运行基础镜像维护

- **用户裁决与实施（2026-11-09，A）。** [Dockerfile](../build/Dockerfile) 的运行层从 `alpine:3.20` 升到 `alpine:3.24`，同步 AGENTS。保留编译阶段、非 root 用户、数据目录和健康检查。3.20 于 2026-04-01 结束常规支持，3.24 支持至 2028-06-01。[Alpine 官方发布表](https://alpinelinux.org/releases/)
- **本地验证。** `docker build --pull --platform linux/amd64` 解析到 Alpine 3.24.4，基础镜像索引 digest 为 `sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf`。以 GHCR 的 3.0.1 在临时命名卷通过 API 写入设置后，切换候选可读回。候选的 UID 1000、CA 文件、目录 `0700`、SQLite 文件 `0600`、静态二进制、非默认端口 health、首页及其引用资源均已核对，两镜像 SIGTERM 退出码均为 0。验证环境为 ARM Docker VM 兼容运行 amd64。
- **工程门禁。** 临时源码副本包含本轮 Dockerfile 与测试改动。该副本的 `make all` 通过，含真实前端构建、153 项前端测试、Go vet、12 个 Go 包的实际 race 测试及产品构建。Compose 示例配置检查和 `git diff --check` 通过。
- **发布边界。** 本地维护改动未提交或发布。下一次镜像在远端 CI 与 GHCR 验证通过后，再按 I9-R11 更新部署示例。

<a id="i9-r08"></a>

### I9-R08 真实 SMTP 人工验收（原 I8-R01）

- **已完成部分：** 显式 `auto_starttls/starttls/implicit_tls`、测试邮件九字段载荷、共用发送出口与本地回归已实施，已随当前源码发布；归档原实现见 [I8-R01](Issue8.md#i8-r01)。不重复开发 SMTP 模式。
- **用户裁决与交接（2026-11-09，B）。** 在本地准备回环绑定、临时数据目录的独立 Docker 实例，由用户在 WebUI 填写配置并发送。同网络命名空间用系统 CA 验证腾讯云 465 的 TLS 1.3 握手。本地假 SMTP 演练捕获一封测试邮件、一封 DNS 告警和两段健康异常各一封，第一段跨完整监督器周期无重复，暂停后观察到监督器记录恢复。
- **真实测试邮件（PT-B7-01，2026-11-09）。** 隔离实例于北京时间 12:31:14 记录「测试邮件已被 SMTP 服务器接受」，用户确认收到邮件。已取得真实服务器接受和实际投递两层证据。该条为此前已取得的逐项证据。
- **用户最终验收（2026-11-09）。** 用户明确确认 SMTP 已测试通过、无问题，PT-B7-01/02/06 按该确认统一收口。手动邮件保留上述 SMTP 接受与收件证据，其余场景以用户确认作为验收结论。记录见 [ProdTestList.md](../ProdTestList.md)。

<a id="i9-r09"></a>

### I9-R09 SQLite 连接容量（原 I8-R14 剩余子项）

- **用户裁决（2026-11-09）。** 用户选择 A，继续暂缓，沿用现有连接池策略。本项仍是按需维护候选。实际部署出现可归因于连接峰值的内存或文件描述符压力时，再评估连接上限；若反复出现 `SQLITE_BUSY`，先区分 SQLite 写锁竞争和 Go 连接池取连接等待，再决定是否需要写入排队。当前发布无需增加压力验收门禁。
- **当前机制。** Store 共享一个 `sql.DB`，默认无打开连接数上限，最多保留 2 条空闲连接。配置变更和日志写入各自串行，扫描落库、清空缓存等入口仍可与它们争用 SQLite 写锁。已借到连接的写操作等待 SQLite 锁时会占用连接名额。DSN 的 5 秒 `busy_timeout` 作用于 SQLite 锁等待；运行健康的 2 秒探活预算也覆盖 Go 池取连接的等待。单连接共享池会让读请求与写事务排在同一条连接上，失去 WAL 提供的读写并行。 [Store](../config/store.go) · [Go 连接池](https://go.dev/doc/database/manage-connections) · [SQLite WAL 并发](https://www.sqlite.org/wal.html#concurrency)
- **仓外实验。** 基线 `0a5c8d224a98fde9c4b9e2b146d1a4eed283dbb9`，本机 macOS arm64、Go 1.27.3、SQLite 3.53.4。使用临时库和项目实际 Store、配置协调器、API handler，持有一笔配置写事务并加入扫描落库等待者。共享池上限为 1、2 或 4 时，占满均使运行健康约 2 秒后返回 503，释放后恢复 200；无上限、共享池有空位、单池在借连接前串行写入、独立单连接写池加读池的对照均返回 200。上限 4 为实验值。短时混合请求中，无上限组采样峰值为 11 条，结束后回到 2 条空闲连接。取消对照中，告警 GET 退出取连接队列，日志 GET 继续等到连接释放。
- **后续备选。** 若先做低频诊断，可观察打开、在用和空闲关闭计数及 SQLite `BUSY`。池的 `WaitCount` / `WaitDuration` 只统计 Go 池取连接等待，计数为零时仍可能存在 SQLite 写锁竞争。若需要硬上限，优先验证写入在借连接前排队，并核对所有写入口、普通请求取消、读侧占满和健康探活。拆分读写池可隔离两侧连接容量，也会增加 SQL 路由与池生命周期管理。 [Go DBStats](https://pkg.go.dev/database/sql#DBStats)
- **证据边界。** 仓外共享池与拆池对照及混合请求各通过 3 轮 race，写入排队及取消对照各通过 5 轮 race。当前仓库相关 Store 与 API 的 5 项既有 race 回归通过。实验未采集实际运行容器的数据，尚无生产容量结论。本轮仅更新台账。
- **不重开的已完成范围：** 扫描 SQLite 阶段 context/回滚、日志裁剪已实施；云扫描仍独立预算。当前 schema 没有外键声明、targets 是 JSON，单开 foreign_keys 无法保护引用；沿 I8-R09 应用层完整性合同。原分项与证据见 [I8-R14](Issue8.md#i8-r14)。

<a id="i9-r10"></a>

### I9-R10 特定平台与长期运行证据（原 I8-R23）

- **全部迁移的边界：** 已有 arm64 Docker VM 兼容运行 amd64 产物，不等于原生 linux/amd64 主机实测；macOS 13 最低版本和长期稳定性需要相应环境记录，不能从本机较新 macOS 或一次 CI 外推。压力、真实故障注入也只按实际执行范围表述。
- **当前状态：** 按需核对，不是确认产品缺陷；仅在准备做相应平台/稳定性声明时补证据。当前 v3.0.1 远端 Linux CI/镜像构建成功，不替代原生部署和长期运行。
- **不新增待办：** R26/R27 已有实现与发布证据，但本轮未重新执行真实云。回读格式、裸 IP 删除、双字段等特定云响应假设不写成实测通过，也不把它们泛化为新一轮普通云人工阻断。既有用户真机确认和 Webhook/Kuma 豁免保持。原范围见 [I8-R23](Issue8.md#i8-r23)。

<a id="i9-r11"></a>

### I9-R11 测试命名、历史统计引用与部署示例

- **原 I8-R25 全部迁移：** 基础 28 项/附加标签、顶层测试/子用例、tracked/ignored 数量与旧行号只描述采集基线；不作为当前规模。引用历史证据按固定 SHA/符号理解，不重算未执行探针或改写真实历史次数。
- **命名已修订（2026-11-09）。** [cleanup_test.go](../syncer/cleanup_test.go) 中 fake Provider 用例改名为 `TestCleanup_ECSCandidatesHandedOverForBatching`，与断言的 150 个候选整体交接一致。该用例和 Provider／正式同步链两个 SDK 分批用例均通过定向 race。历史归档保留原名和采集基线。[I8-R25](Issue8.md#i8-r25)
- **待发布后同步。** 按用户裁决 A，README 与 Compose 示例暂用已发布的 3.0.0。下一次含 I9-R07 的镜像在 GHCR 验证通过后，再同步更新示例的明确版本标签。

## 4. Issue8 全部编号迁移对照

| 原编号 | 当前去向与归档结论 |
|---|---|
| I8-R01 | SMTP 代码已完成；全部人工剩余转 I9-R08 / ProdTestList。 |
| I8-R02 | 依赖更新与当批审计完成；新发现的 Go 标准库补丁独立为 I9-R02，不把旧审计外推到现在。 |
| I8-R03 | ECS 实际 SDK 分批测试补强完成；候选交接测试命名已在 I9-R11 修订。 |
| I8-R04 | hook 测试观察竞争补强完成。 |
| I8-R05 | SWAS 资源扫描分页完整性完成；不等于 SWAS 规则快照竞态已修。 |
| I8-R06 | ECS/扫描总预算已实现；专项脚本缺陷转 I9-R05，测试时序候选转 I9-R06。 |
| I8-R07 | Pause/Resume 原子更新完成。 |
| I8-R08 | CVM Describe 错误分类合同与回归完成。 |
| I8-R09 | dangling 目标引用校验完成；历史空串 JSON 问题独立转 I9-R03。 |
| I8-R10 | 日志持久化队列、背压与收尾完成。 |
| I8-R11 | 即时通知用途已裁决、两类 SSE 心跳完成；不承诺同步事件无损续传。 |
| I8-R12 | 阿里云错误码重试范围完成。 |
| I8-R13 | 本进程时间戳与无记录展示完成。 |
| I8-R14 | 扫描 SQLite 取消/日志裁剪完成；容量转 I9-R09；外键当前 schema 不适用。 |
| I8-R15 | Server.Addr 关闭后语义与回归完成。 |
| I8-R16 | 维护符号清理与测试迁移完成。 |
| I8-R17 | 日志渲染失败长期回归完成。 |
| I8-R18 | Stop 四入口长期回归完成。 |
| I8-R19 | 组件生命周期修复与收尾回归完成；不要求全仓零泄漏证明。 |
| I8-R20 | 启动宽限 10 秒数值与边界回归完成。 |
| I8-R21 | 页面生命周期保护与 Make/npm/CI 前端门禁完成，同日 151 项通过。 |
| I8-R22 | v3.0.0 历史证据保留；发布基线 v3.0.1 的 CI/构建推送已成功，后续源码的发布状态见本文件 §1。 |
| I8-R23 | 特定平台/长期运行声明范围完整转 I9-R10，按需补证。 |
| I8-R24 | 合理前置条件已裁决、维护收口，非产品故障。 |
| I8-R25 | 历史统计与测试命名剩余完整转 I9-R11。 |
| I8-R26 | 全端口转换已实现、合并并进入 v3.0.1；保留原测试和真实云证据范围。 |
| I8-R27 | 裸 IP 规范化已实现、合并并进入 v3.0.1；保留保守清理合同与原测试范围。 |

## 5. 接受、排除和已失效的报告项目

本节保留审阅结论，避免未来把同一观察重新登记为高优先级待修；不把“接受/排除”写成“代码已修复”。

| 来源/观察 | 当前决定与边界 |
|---|---|
| P1-1 模板/组/前缀列表等外部规则冻结清理 | 接受当前保守策略；合法但当前模型不可解析的外部规则会阻止清理，覆盖仍可能满足。残留可能持续至手工整理，不承诺下一轮消失；不增加高级引用规则解析或清理告警。 |
| P1-5 Lighthouse 合并端口部分残留 | 接受：仍需 80 时可保留原 80,443 整条规则，无专门部分残留提示；不要求拆分重建或健康告警。 |
| P1-4 云规则数组 null 元素 panic | 源码缺口成立，但依赖畸形云响应、无正常路径证据；排除当前必修队列，不增加全局 recover 或普遍 nil 检查链。 |
| P2-5/C-03 Stop-before-Run 首检 | 不登记产品假告警缺陷；实际先停 Supervisor 再停 Syncer，隔离探针不足以证明正常关闭必然误报。 |
| P2-5/C-04 外部 SQL 写坏设置后 400/500 分类 | 排除；不为绕过产品入口的非法写入增加错误来源分类框架。 |
| ECS SourcePortRange 未建模 | 有条件的高级混用限制：手工规则限制源端口时可能被误判为满足期望，影响放通，不能仅称清理残留；当前简单规则用途不扩展五元组支持，实际采用该用法时再研究。 |
| 无内置鉴权/CSRF、可配置出站通知地址 | 继续内部使用和用户控制网络边界的设计，不新增登录、权限或 SSRF 防护系统。 |
| 扫描未接入 HTTP 断开/Stop 的云调用取消 | 沿既有独立云读取预算；SQLite 持久化阶段已有请求 context，不把两个阶段混为回归。 |
| P0-3 大量缺实现、P2-3 无前端门禁、R26/R27 未推送发布 | 当前合并与 v3.0.1 已使这些结论失效；不重新登记。 |
| P2-4 Compose 插值/所有接口映射/latest/无停止宽限 | 原问题已修：延迟容器变量展开、回环映射、明确标签和 5 分钟宽限。仅推荐版本维护见 I9-R11；5 分钟不是任意部署的进程硬退出保证。 |
| 真实 Webhook/Kuma 链路及普通云/浏览器验收 | 前者继续豁免、非通过，后者按用户确认收口；不新增待执行项或恢复历史阻断。 |

## 6. 文档与后续记录规则

- [AGENTS.md](../AGENTS.md) 仍是唯一强要求；本文件记录问题、建议和状态，不因归档撤销已实施合同或改变生产行为。
- [Design5](Design5.md)、[Build6](Build6.md)、[Build7](Build7.md) 已按阶段达标归档；[Issue8](Issue8.md) 按“完成部分归档、剩余完整转交”归档。历史原始次数、SHA、红灯、当时未执行和未提交状态保留，并由归档说明明确时间边界。
- 归档索引见 [HistoryDocs/README.md](README.md)。Issue8-history.md 是更早的历史快照，与本次归档 Issue8.md 不同，不能覆盖或混用。
- [TODOLIST.md](TODOLIST.md) 保留为历史导航；[ProdTestList.md](../ProdTestList.md) 保留人工验收结果。
- 实施后记录具体变更、实际验证与日期；真实外部链路和发布结果各自按真实证据更新。无需每项叠加长周期、极端故障或全平台验收，不能将延期清理变成新告警需求。
