# Issue7.md — P1-01 TAG 所有权与目标级同步实施合同

> **文档定位：** 本文是 `fwalizer-audit-final1.md` 中 P1-01 定案后的唯一当前实施合同，参考 Build 文档的写法，把产品语义、当前源码替换边界、建议代码形态、串行 Step、判别性测试与验收口径固定下来。`AGENTS.md` 是唯一强要求；若本文、Design、Build、Audit 之间出现新冲突，必须暂停并交由用户裁决。
>
> **Step 0 历史基线：** 2026-09-29，开始 Step 0 时 `main` HEAD 为 `108e5285fd88cd5a0bbc2dbecee81042091f19fa`，相对 `origin/main` ahead 2，工作树干净。Step 0 只修改文档，没有修改源码、测试、依赖或外部系统状态。
>
> **本次合同补强基线：** 2026-09-29，核对当前工作树时 `main` HEAD 为 `d70796ba1c229e9e93732cc27a758a88b0ff57fd`，相对 `origin/main` ahead 1；本次仍只补充本文，不实施 Step 1～5，不修改源码、测试、依赖、数据库或外部系统。
>
> **Step 1 开工前基线（2026-09-29）：** 用户一次性授权 Step 1～5 后重新核对：`main` HEAD 为 `3ce40fe47bcb9e75abd7d72dcfea4265ea8e4da3`，相对 `origin/main` ahead 2，工作树干净（仅 `fwalizer`、`webui/frontend/dist/`、`webui/frontend/node_modules/` 三项既有 ignored 产物）。基线门禁全部通过：`go test ./... -race -count=1`（12 包 ok）、`go vet ./...`、`go build ./...`、`git diff --check`、前端 `npm ci` + `npm run build` + 两条 `npm audit`（0 漏洞，lockfile 未变化）、`docker compose -f docker-compose.yml.example config --quiet`。
>
> **本次一次性授权的准确边界：** 只含本地源码/测试/前端/文档实施、本地只读门禁与 `docker build`（含隔离 `FWALIZER_DATA_DIR` 的本地二进制/容器验收）。**不含**提交、推送、tag、远端 CI/GHCR、发布、真实云写入、真实 SMTP/Webhook/Uptime Kuma 与浏览器验证；这些层次在本次实施中一律记为「未执行」。
>
> **实施状态（2026-09-29）：** Step 0～5 **已全部本地实施完成**：Step 1 纯规划器/快照模型、Step 2 目标级先增后验、Step 3 四平台条件清理、Step 4 Dry Run/事件/日志/仪表盘口径、Step 5 完整门禁 + 真实二进制/Docker 验收 + 文档闭环。逐 Step 证据见 §12.3。**未提交、未推送、未打 tag、未触发远端 CI/GHCR、未调用真实云、未执行浏览器验证**；这些层次在 §12.3「未执行边界」中如实登记。

---

## 一、目标、非目标与不可破坏不变量

### 1.1 目标

本项不是给旧的逐域名 `Diff` 打补丁，而是把一个云目标上的全部适用规则合并为一个规划和写入单元，完整修复以下问题：

1. P1-01：相同 comment（尤其空 comment）导致不同规则互删、逐轮振荡；
2. P2-01：IPv6 + ICMP 在期望侧与云端回读侧 key 不对称；
3. P2-02：ECS 删除超过 100 个 RuleID 未分批；
4. P2-03：平台能力不支持项被静默丢弃，进而可能错误放开清理门；
5. P2-08/P2-09、P3-11/P3-22 与 Dry Run 相关 P3-16：状态、日志与页面仍建立在逐规则模型上；
6. P3-25：ECS `NextToken` 不推进时可能把不完整快照当成完整快照。

固定方案为：

```text
目标级完整期望集
+ TAG 所有权命名空间
+ comment 纯可读
+ 先增后验
+ 平台化条件删除
+ 无法证明删除安全时保留残留
```

### 1.2 非目标

本项不实施以下内容：

- 不强制 comment 必填或唯一；
- 不把 host、协议、端口、本地规则 ID 或哈希写进 description；
- 不新增“本地规则—云规则”持久化映射表或 Schema 迁移；
- 不因 comment 修改而回写、删除或重建云规则；
- 不在完整期望集为空时自动清空整个 TAG 命名空间；
- 不跨旧 TAG、已删除目标、reset/import 前的其他命名空间追踪孤儿规则；
- 不使用 Lighthouse `ModifyFirewallRules`、CVM 重置安全组规则等全量覆盖 API；
- 不为四云表面同形而丢弃 Lighthouse/CVM 的版本保护或 SWAS/ECS 的稳定 RuleID；
- 不顺带重构熔断器、ticker、pause/resume、pidfile、Webhook 或其他独立审计问题；
- 不在首轮增加人工清理 API、CLI 或页面入口。

### 1.3 不可破坏不变量

实施过程中必须始终满足：

1. **新增先于删除：** 任一目标在 S1 证明所需功能存在前，自动删除调用次数必须为 0。
2. **权限优先：** 尽快补齐当前 IP 所需权限是首要职责；陈旧 TAG 清理只是尽力而为的后台收敛。
3. **授权最小化：** 非当前 TAG 规则只读；即使精确满足期望，也永不修改、删除、改备注或接管。
4. **失败保留：** DNS、Describe、Add、覆盖验证、分页完整性或版本竞争无法确认时，保留旧规则。
5. **整目标重试：** 每次重试重新获取该目标完整快照并重新规划，不沿用旧 `PolicyIndex`、RuleID、Version、候选集或覆盖结论。
6. **状态真实：** 平台能力不支持为 `partial`；DNS/Describe/Add/覆盖验证失败为 `failed`；仅清理延后或删除失败仍为 `success`/healthy。
7. **证据分层：** mock、单测、真实二进制、Docker、浏览器、真实云和远端 CI/GHCR 互不替代。

---

## 二、术语、所有权与功能身份

### 2.1 术语

| 术语 | 固定含义 |
|---|---|
| 目标 | 一个 Provider 实例，即一条本地 target 对应的一个云实例或安全组 |
| 适用规则 | `targets` 为空，或显式包含当前目标数据库 ID 的本地 `DomainRule` |
| S0 | 某次 attempt 开始时取得的完整云端快照；用于规划和带版本新增 |
| S1 | Add 完成后重新取得的完整快照；用于覆盖验证和生成删除定位 |
| S2 | 发生实际删除或部分删除后取得的完整快照；用于确认清理未破坏所需功能 |
| Owned | description 严格属于当前 `[TAG]` 命名空间的云规则 |
| External | 不属于当前 TAG 的云规则；只能用于精确功能满足判断 |
| Desired | 所有适用本地规则在 DNS、平台展开和归一化后的目标级完整期望功能集 |
| Implementable | 当前平台能够表达并创建的 Desired 子集 |
| Coverage | S1/S2 中 Owned 或 External 规则对 Implementable 的精确 key 覆盖 |
| Cleanup candidate | S1 中属于当前 TAG、但功能 key 不在完整 Desired 中的陈旧规则 |
| Cleanup deferred | 候选存在但安全门不满足，或清理未全部完成；保留候选并给出稳定原因 |

### 2.2 TAG 所有权语法

唯一合法的归属判断是：

```go
func IsOwned(description, tag string) bool {
	prefix := "[" + tag + "]"
	return description == prefix || strings.HasPrefix(description, prefix+" ")
}
```

因此：

| description | 是否属于 TAG=`auto-dns` |
|---|---:|
| `[auto-dns]` | 是 |
| `[auto-dns] 生产 API` | 是 |
| `[auto-dns] ` | 是；尾随空格不改变授权边界 |
| `[auto-dns]foo` | 否 |
| `[auto-dns-old] foo` | 否 |
| `x[auto-dns] foo` | 否 |

当前 `internal/tag/tag.go:HasPrefix` 使用裸 `strings.HasPrefix(description, "["+tag+"]")`，会误接纳 `[TAG]foo`；Step 1 必须以失败先行用例替换该语义，并让 `Parse` 与 `OwnedRules` 共用同一判定，禁止各写一套。

### 2.3 comment 的固定职责

- comment 可空、可重复、可修改、可被平台截断，只用于人类阅读；
- comment 不参与功能 key、所有权、覆盖验证、清理候选、删除定位或唯一性校验；
- 修改 comment 后，已有云规则保持不动；未来因功能变化新建的规则才使用当时选出的 comment；
- 同一功能 key 由多条本地规则产生时，按本地规则 ID 升序选择第一条非空 comment；全部为空时只写 `[TAG]`；
- comment 截断发生在最终 description 渲染层，不能反向改变功能 key；
- Lighthouse 的 64 字符限制有仓内 API 文档依据；SWAS “50 字符”仍是 PT-B7-09 待真机项，不得写成已核实事实。

### 2.4 功能 key

功能 key 固定由以下五项组成：

```text
address-family
+ canonical CIDR
+ canonical protocol
+ canonical port expression
+ action
```

不得进入 key 的字段：TAG、comment、description、本地规则 ID、域名、云端 RuleID、PolicyIndex、快照版本、Provider 名称。

建议在 `provider/common.go` 把当前未导出的 `ruleKey` 收口为唯一 canonical key 实现；`RuleInfo` 与 `RuleAction` 必须调用同一入口，不能分别归一化：

```go
type AddressFamily string

const (
	AddressIPv4 AddressFamily = "ipv4"
	AddressIPv6 AddressFamily = "ipv6"
)

type FunctionalKey struct {
	Family   AddressFamily
	CIDR     string
	Protocol string
	Port     string
	Action   string
}
```

建议形态只固定职责与字段，不要求机械照抄命名；若实施时需要改名，必须保持一个唯一 canonical key 层和下列归一化表。

### 2.5 归一化表

| 输入差异 | canonical 结果 |
|---|---|
| IPv4 `CidrBlock` / IPv6 `Ipv6CidrBlock` | 明确拆成 `family + CIDR`；两个字段同时有值或都为空视为快照冲突，不进入可删除集合 |
| 协议大小写 | 大写 |
| IPv6 + `ICMP` / Lighthouse `ICMPv6` / CVM `ICMPV6` | key 中统一为 `ICMP`，由 family 区分 v4/v6 |
| ICMP 端口空串 / `ALL` / `-1/-1` | `ALL` |
| Aliyun 单端口 `443/443` / 内部 `443` | `443` |
| Aliyun 范围 `8000/8010` / 内部 `8000-8010` | `8000-8010` |
| `ALL` / `-1/-1` | `ALL` |
| Lighthouse 逗号端口 | 先按 `portconv.Parse` 的业务语义展开为独立 canonical 端口项，不把顺序不同的逗号字符串当作不同功能；**展开项同时是 Desired 与 ToAdd 的最小单元**，因此新建时 `80,443` 会下发为两条单端口规则（2026-09-29 用户裁决）。云侧一条逗号规则会同时覆盖其展开后的全部 key，故已存在的合并规则既不会被重建也不会被删除 |
| `TCP+UDP` | SWAS 保持一个原生功能；Lighthouse/CVM/ECS 展开为 TCP 与 UDP 两个功能 |
| action 大小写 | 大写 `ACCEPT` / `DROP` |

CIDR 必须使用 `net/netip` 或等价标准库能力做掩码后的规范字符串；不得只对原字符串比较，也不得推断“更宽 CIDR 包含更窄 CIDR”。

---

## 三、当前源码基线与替换边界

本节不是实现证据，而是 Step 1 开始前必须替换或保留的当前源码地图。行号会漂移，实施时应以符号名和测试名定位。

| 当前符号/文件 | 当前行为 | Issue7 目标 |
|---|---|---|
| `internal/tag/tag.go:HasPrefix/Parse` | 裸前缀匹配，会接纳 `[TAG]foo` | 共用严格所有权语法 |
| `provider/common.go:ruleKey/keyOf/keyOfAction` | 未显式表达 family；ICMPv6 对称性不完整 | 唯一 `FunctionalKey` 与双向 canonical 化 |
| `provider/common.go:Diff` | 按单条本地规则和 description 精确匹配，直接同时产出 Add/Delete | 替换为目标级纯规划器；规划器只产出候选，不执行写入 |
| `provider/common.go:buildDesired` | 对单条规则展开；SWAS/ECS IPv6 与 ECS ICMPv6 静默过滤 | 目标级汇总；过滤改为显式 `unsupported` |
| `syncer/retry.go:retrySyncDetailed` | 每条规则独立 `Describe → Diff → Delete → Add` | 整目标 `S0 → Plan → Add → S1 → Verify → Cleanup → S2` |
| `provider.Provider` | `GetRules` 只返回规则；Create/Delete 无快照版本参数 | 快照携带 revision/completeness；腾讯写入必须传对应版本 |
| `provider/tc_lighthouse.go` | Describe 丢弃 `FirewallVersion`；Create/Delete 不传版本 | S0/S1 版本分别用于 Add/Delete； mismatch 整目标重试 |
| `provider/tc_cvm.go` | Describe 丢弃 `Version`；逐条 Delete 导致版本/索引漂移风险 | S0/S1 版本保护；同一 S1 的候选单请求批量删除 |
| `provider/ali_swas.go` | RuleID 已回读；DROP 在 Diff/Create 两层有跳过语义 | S1 RuleID 精确删除；能力矩阵只保留一个判定源 |
| `provider/ali_ecs.go` | NextToken 未检查不推进；Delete 未按 100 分批 | 重复 token 硬失败；S1 RuleID 每批最多 100 |
| `syncer/syncer.go:DryRun` | Provider×规则循环，每规则解析、Describe、sleep | 每目标一次解析去重、一次 Describe、一次纯规划 |
| `syncer/syncer.go:RoundSummary` | 单元为 Provider×规则 | 单元改为目标；空目标/空适用规则仍为 idle |
| `notifier.EventDomainSyncComplete` | 承载逐域名增删计数 | 新增 `EventTargetSyncComplete` 承载目标级结果；旧事件不再承载云写入计数 |
| `webui/api/logwriter.go` | 按逐域名事件写日志且吞掉 `AddSyncLog` 错误 | 消费目标级事件；写库错误返回给 EventBus 统一 WARN |
| `DryRunResult` / `DryRunResults.vue` | 以 domain 为结果与 `v-for` key，只有 add/delete/skipped | 改为目标级完整计划字段和稳定目标 key |
| `Dashboard.vue` | 页面自行解释 round outcome，idle 被误报 | 只按后端 outcome/cleanup 字段展示，不生成第二套健康结论 |

保留的正向控制包括：`TestDiff_AliyunPortRoundTripConverges`、运行时快照一次捕获、四云串行/跨云并行、最大 3 次指数退避、幂等“已存在/已不存在”、CVM 100 条新增保护、敏感信息日志边界和 Build7 的 OperationalHealth 唯一计算源。

---

## 四、目标代码结构与纯规划器合同

### 4.1 Provider 快照与写入边界

当前 `GetRules() ([]RuleInfo, error)` 无法承载腾讯版本。建议把“规则 + revision + 完整性”作为不可拆分快照传递：

```go
type RuleSnapshot struct {
	Rules    []config.RuleInfo
	Revision string // Lighthouse FirewallVersion / CVM Version；Aliyun 为空
}

type Provider interface {
	GetSnapshot() (RuleSnapshot, error)
	CreateRules(snapshot RuleSnapshot, rules []config.RuleAction) (CreateResult, error)
	DeleteRules(snapshot RuleSnapshot, rules []config.RuleInfo) error
	ConvertPorts(port string) []string
	// 既有 Name/CloudType/TargetIndex 保留
}
```

固定要求：

- “完整性”由 `GetSnapshot` 成功返回隐含保证；任一页失败、token 不推进、字段不足以安全判定时必须返回 error，不得返回半截 `Rules`；
- Lighthouse/CVM 的 `Revision` 不能为空；缺失即 Describe 失败，不能退化为无版本写入；
- Aliyun revision 为空是合法的，但删除必须使用同一 S1 回读的稳定 RuleID；
- 可使用单独 `SnapshotRevision` 类型或平台私有写入方法，命名不是合同；版本与规则不可被调用方错配才是合同；
- 禁止在 Provider 内部为了 Create/Delete 再偷偷 Describe 并绕开目标级 attempt；CVM 规则上限检查如需 Describe，结果只能用于配额保护，不能替换规划快照或删除定位。

### 4.2 纯规划器输入

规划器不得访问网络、数据库、时钟、日志或事件总线。建议输入：

```go
type TargetPlanInput struct {
	CloudType config.CloudType
	Tag       string
	Rules     []config.DomainRule       // 已按 ID 升序的全部适用规则
	Resolved  map[int][]dns.ResolvedIP  // key 为本地 rule ID；同 host 的结果复用
	DNSErrors map[int]string            // 安全、可展示的稳定错误
	Snapshot  provider.RuleSnapshot
}
```

DNS 调度层必须先按标准化 host 去重；同一目标、同一 host、同一轮最多调用 Resolver 一次，再把同一结果分发给引用它的全部规则。一个 host 失败时，与它关联的规则全部记录同一 DNS 错误；其他 host 仍可规划和新增，但该目标清理门关闭，最终目标结果为 `failed`。

### 4.3 纯规划器输出

Dry Run、正式同步、日志和测试应消费同一 `TargetPlan`。建议最小字段：

```go
type TargetPlan struct {
	Desired             []PlannedRule
	SatisfiedByOwned    []PlanMatch
	SatisfiedByExternal []PlanMatch
	ToAdd               []config.RuleAction
	CleanupCandidates   []config.RuleInfo
	CleanupDeferred     []PlanIssue
	DNSErrors           []PlanIssue
	Unsupported         []PlanIssue
	Conflicts           []PlanIssue
	CoverageReady       bool
}
```

`PlannedRule` 至少保留 `FunctionalKey`、选中的可读 comment/description、来源本地 rule IDs 与来源 domains；`PlanIssue` 至少有稳定 `code`、用户可读 `message`，以及可选 key/domain/rule ID。不得只返回一段不可分类的字符串。

建议稳定原因码：

| code | 含义 | 对 outcome/清理门影响 |
|---|---|---|
| `dns_failed` / `dns_empty` | 适用域名解析失败或结果为空 | `failed`，禁止清理 |
| `snapshot_incomplete` | 分页/字段/版本不能证明完整 | `failed`，本 attempt 零写入或零删除 |
| `unsupported_ipv6` | SWAS 不支持 IPv6 | `partial`，禁止清理 |
| `unsupported_icmpv6` | ECS 不支持 ICMPv6 | `partial`，禁止清理 |
| `unsupported_action` | SWAS 不支持 DROP | `partial`，禁止清理 |
| `owned_locator_missing` | 陈旧 Owned 规则缺稳定删除定位 | `cleanup_deferred`，仍可 success |
| `owned_key_ambiguous` | Lighthouse 同 key 不能唯一定位 | `cleanup_deferred`，仍可 success |
| `snapshot_rule_invalid` | 云端规则缺 family/CIDR 等必要字段 | `conflicts`；不得进入删除集合，并冻结清理 |
| `opposite_action` | 相同 family/CIDR/protocol/port 同时存在 ACCEPT/DROP | `conflicts`；只提示，不做包含或优先级推断，冻结清理 |

`conflicts` 只表达无法安全自动裁决的结构冲突，不把“存在 External 精确等价规则”当成冲突。出现 conflict 时仍可补齐确定缺失的 Implementable key，但目标清理门关闭。

### 4.4 目标级期望集构造算法

固定算法如下：

```text
按本地 rule ID 升序取得全部适用规则
→ 按 host 去重解析，并保留每条规则的来源关系
→ 对每条成功且非空的解析结果执行 IPv4/IPv6、协议、端口和平台展开
→ 对每个展开项构造唯一 FunctionalKey
→ 平台无法表达的展开项进入 unsupported，不进入 ToAdd
→ 相同 FunctionalKey 合并来源 rule IDs/domains
→ comment 取来源 rule ID 最小顺序中的第一个非空值
→ description = 截断后的 Format(TAG, comment)
→ 用 S0 全部云规则的 canonical key 建立 Owned/External 多值索引
→ Desired 被 Owned 精确覆盖 → satisfied_by_owned
→ 否则被 External 精确覆盖 → satisfied_by_external
→ 否则且平台可实施 → to_add
→ S0 Owned 中 key 不在完整 Desired → cleanup_candidates（只作预览，不授权立即删除）
```

重要边界：

- `Desired` 包含平台不支持项，用于表达用户完整意图；`ToAdd` 只含 Implementable 缺失项；
- cleanup candidate 的比较基准是完整 `Desired`，不是 Implementable 子集，防止把当前无法实施但仍属用户意图的规则误删；
- 同 key 多条云规则必须保留多值，不能使用 `map[FunctionalKey]RuleInfo` 静默覆盖重复项；
- External 只有精确 key 才可满足，`0.0.0.0/0` 不推断覆盖 `1.2.3.4/32`，端口范围也不做包含推理；
- comment 改变时 key 不变，计划必须为零 Add、零 cleanup candidate；
- 完整 Desired 为空时 `CoverageReady=false`，所有 Owned 只可作为“候选预览 + cleanup_deferred”，不得自动清空。

### 4.5 S1 覆盖验证

Add 后必须重新取得 S1 并用**同一纯规划器/同一 canonical key**复算覆盖。`coverage_ready=true` 仅当：

1. S1 是完整快照；
2. 每一个 Implementable Desired key 都至少被一条 Owned 或 External 规则精确覆盖；
3. 不存在 Add 提交状态未知；
4. 完整 Desired 非空；
5. 这不表示清理门已打开；DNS、unsupported、conflict、删除定位仍需独立检查。

S1 出现了期望规则但 description 被云平台截断，只要严格 TAG 所有权仍成立且 key 精确相等，即可作为 Owned 覆盖；若 TAG 本身被破坏则只能按 External 处理，且不得删除。

### 4.6 清理安全门

自动清理必须同时满足全部条件：

| # | 条件 | 不满足时 |
|---:|---|---|
| 1 | 完整 Desired 非空 | 所有候选 `cleanup_deferred` |
| 2 | 全部适用域名解析成功且结果非空 | 目标 `failed`，保留候选 |
| 3 | `unsupported` 为空 | 目标 `partial`，保留候选 |
| 4 | `conflicts` 为空 | 保留候选；若冲突妨碍覆盖则 failed，否则 success + deferred |
| 5 | S1 `coverage_ready=true` | 目标 `failed`，保留候选 |
| 6 | 候选在 S1 仍严格属于当前 TAG | 非 Owned 项从候选剔除，绝不操作 |
| 7 | 候选 key 不在完整 Desired | 属于 Desired 的任何重复规则均不作为自动清理候选 |
| 8 | 平台删除定位完整且无歧义 | 对应候选 deferred |
| 9 | 腾讯写入使用 S1 revision | revision 缺失/竞争则整目标重新 attempt |

`cleanup_deferred` 必须是稳定原因列表，而不是一个 bool。清理门未开不阻止对其他确定缺失 key 的 Add；但同一 attempt 中，Add 或 S1 verification 失败后不得调用 Delete。

---

## 五、正式同步状态机、重试与计数

### 5.1 单个目标的固定状态机

```text
Resolve（按 host 去重）
  ↓
Get S0（完整规则 + revision）
  ↓
Plan(S0)
  ↓
Create(ToAdd, S0 revision)
  ↓
Get S1
  ↓
Plan/Verify(S1)
  ↓
Evaluate cleanup gate
  ├─ gate closed → success/partial/failed + cleanup_deferred，结束
  └─ gate open   → Delete(S1 candidates, S1 revision)
                       ↓
                    若实际删除或部分删除 → Get S2 → Verify
```

只要发起过删除且云端确认至少删除一条，或返回 `PartialDeleteError`，S2 就是必需的；没有删除调用时不取 S2。S2 Describe 失败，或 S2 不再覆盖全部 Implementable Desired，目标为 `failed`，记录高优先级错误并转入“后续轮次只补不删”；当前 attempt 不再继续任何删除。

### 5.2 重试边界

- 最多 3 次，退避保持 1s、2s；
- 可重试的 DNS/Describe/Create/version mismatch/网络错误进入下一整个目标 attempt；
- 每个 attempt 都重新 Resolve、S0、Plan；不得只重试最后一个 HTTP 请求；
- **Resolve 粒度裁决（2026-09-29 用户裁决）：** 「每个 attempt 重新 Resolve」优先于「每轮只解析一次」——每次 attempt 开始时按标准化 host 去重重新解析，同一 attempt 内同一 host 最多调用 Resolver 一次。Step 2 红灯 5 的「每目标每 host 每轮只 Resolve 一次」断言只针对单 attempt（无重试）成功轮；
- 已经确认写入的 Added/Deleted 计数跨 attempt 保留，但再次规划后不得重复计数；
- 写入提交状态未知时不猜测成功；下一 attempt 通过新快照确认；
- Lighthouse/CVM version mismatch 必须显式识别为可重试，不允许去掉版本重发；
- 达到 3 次仍失败时发布一次目标级失败事件；中间 attempt 不发布最终完成事件。

### 5.3 结果优先级

目标结果优先级固定为：

```text
failed > partial > success
```

| 场景 | 目标结果 | Operational health |
|---|---|---|
| 所需功能全部确认，无清理问题 | success | healthy |
| 所需功能全部确认，但清理门关闭、删除失败或仍有残留 | success + cleanup_deferred | healthy |
| 平台能力限制导致任一期望功能无法实施 | partial | unhealthy |
| DNS、S0/S1 Describe、Add、覆盖验证失败 | failed | unhealthy |
| 删除后 S2 Describe/覆盖验证失败 | failed | unhealthy |
| 无目标，或所有目标都没有适用规则 | idle | healthy |

如果同一目标既有 unsupported 又发生 DNS/Add/verification 失败，结果为 `failed`，但 `unsupported` 明细仍保留展示。

### 5.4 RoundSummary 统计口径

统计单元从“Provider × 一条适用规则”改为“有至少一条适用规则的目标”：

```text
total == ok + changed + failed + skipped
```

- `ok`：目标 success，且 `added==0 && deleted==0`；允许存在 `cleanup_deferred`；
- `changed`：目标 success，且至少确认新增或删除一条；允许存在 `cleanup_deferred`；
- `failed`：目标最终 failed；
- `skipped`：目标没有 failed，但存在 unsupported，亦即目标 partial；保留字段名以避免无必要 Schema/API 扩张，其注释和 UI 必须改为“部分实施目标数”；
- `added/deleted`：云端明确确认的实际写入总数；幂等已存在/不存在不计数；
- 新增 `cleanup_candidates/cleanup_deleted/cleanup_deferred`：分别为 S1 候选数、实际确认清理数、最终残留候选数；`deleted` 与 `cleanup_deleted` 在本 Issue 中数值相同，但保留前者兼容总写入语义、后者用于清理可观测性；
- `outcome`：`total==0 → idle`；否则 failed>0 → failed；否则 skipped>0 → partial；否则 success；
- 只有 success 刷新 `last_success`；带 cleanup_deferred 的 success 仍刷新。

### 5.5 幂等与部分成功

- Create 返回“已存在”：WARN，不计 Added；必须经 S1 覆盖验证后才能视为满足；
- Delete 返回“已不存在”：视为清理成功但不计 Deleted；必要时 S2 仍确认覆盖；
- 清理请求失败且没有确认删除：所需权限已由 S1 证明，结果仍 success + deferred；
- 部分删除：累计已确认 Deleted，未删候选进入 deferred，并强制 S2；
- ECS 100+50 两批中第二批失败：前 100 条计入 Deleted，余 50 条 deferred；
- 不得把 cleanup_deferred 加入既有 `skipped`，不得触发 `EventOperationalUnhealthy`。

---

## 六、四平台适配合同

### 6.1 Tencent Lighthouse

- `DescribeFirewallRules` 的 `FirewallVersion` 必须进入 snapshot；分页所有页必须属于同一次可证明一致的读取。若 API 无法保证跨页同版本，至少比较首末版本；变化则重读整个快照；
- Create 必须传 S0 `FirewallVersion`；版本不匹配重新整个目标 attempt；
- Delete 必须传 S1 `FirewallVersion`；不得降级为不传版本；
- Lighthouse 无稳定 RuleID，删除按完整规则值匹配。因此只有当候选 FunctionalKey 在 S1 全部规则中恰好出现一次、且该唯一项严格 Owned 时才可删除；重复、Owned/External 同 key 并存或字段不足一律 deferred；
- 删除请求不得包含任何 Desired key；删除后强制 S2；
- description 最多 64 个 Unicode 字符，截断必须保留完整 `[TAG]` 授权前缀。若合法 TAG 本身已让前缀超限，应在既有 TAG 校验/平台规划处返回可见冲突，不得截成另一个命名空间。

### 6.2 Tencent CVM

- `DescribeSecurityGroupPolicies` 返回的 `SecurityGroupPolicySet.Version` 必须进入 snapshot；每条 Ingress 必须保留 API 返回的真实 `PolicyIndex`；
- Create 使用 S0 Version；Delete 使用 S1 Version；version mismatch 整目标重试；
- 删除候选按 `PolicyIndex` 去重并放在**同一个** `DeleteSecurityGroupPolicies` 请求的 Ingress 数组中，同时携带 S1 Version，避免逐条删除引发索引漂移；
- 任一候选缺 PolicyIndex、索引解析失败、重复索引映射到不同规则或 S1 Version 为空，候选 deferred，禁止无版本/按值降级删除；
- 现有 `checkRuleLimit` 的配额口径 P3-06 不是本项修复范围；真实账号确认前保持当前偏保守行为，但 Create 的额外 Describe 不得被误当成 S0/S1；
- `PolicyIndex + Version` 都来自同一 S1，禁止排序后逐请求复用已经变化的 Version。

### 6.3 Aliyun SWAS

- S0/S1 必须完整遍历 PageNumber，页失败即 snapshot 失败；
- IPv6 与 DROP 等平台无法表达项统一由能力矩阵产生 `unsupported`，不得在 `buildDesired` 和 `CreateRules` 两层各自静默过滤；Provider 可保留最后一道防御，但返回值必须与规划一致；
- External 精确等价规则可以满足期望，但永远不改 Remark、不启停、不删除；
- cleanup 只使用 S1 回读的非空 RuleId；缺 RuleId 的候选 deferred；
- 删除按稳定 RuleId 批量进行；若 API 文档未声明最大批量，不凭空设定业务上限，遇服务端限制再依据真实错误和文档补合同；
- Remark “50 字符”仍待 PT-B7-09，仅影响 comment 显示截断，不影响身份与功能 key。

### 6.4 Aliyun ECS

- `DescribeSecurityGroupAttribute` 必须完整推进 NextToken；若非空 token 与本次请求 token 相同，或任一已见 token 再次出现，立即返回 `snapshot_incomplete`，该目标本 attempt 零删除；
- IPv6 支持，但 ECS ICMPv6 不支持；该项进入 `unsupported_icmpv6`，不能静默过滤；
- Create 继续每批最多 100 条；cleanup 只用 S1 非空 `SecurityGroupRuleId`；
- Delete 也必须每批最多 100 个 RuleID，稳定顺序切批；150 个候选固定为 100+50；
- 任一批部分成功后如实累计，剩余 candidate deferred；发起过删除即强制 S2；
- `InvalidParam.SecurityGroupRuleId` / NotFound 按既有幂等语义处理，但不能掩盖其他 RuleID 的真实失败。

---

## 七、Dry Run、事件、日志与页面合同

### 7.1 Dry Run API

`POST /api/sync/dryrun` 路由和严格空对象/现有请求合同不因本项扩张；响应改为每目标一项，`results` 永不为 null。建议目标项：

```json
{
  "target_id": 12,
  "provider": "tc_lighthouse(lhins-xxx)",
  "domains": ["api.example.com", "www.example.com"],
  "desired": [],
  "satisfied_by_owned": [],
  "satisfied_by_external": [],
  "to_add": [],
  "cleanup_candidates": [],
  "cleanup_deferred": [],
  "dns_errors": [],
  "unsupported": [],
  "conflicts": [],
  "coverage_ready": false,
  "error": ""
}
```

固定语义：

- Dry Run 只基于 S0，绝不写入，所以 `coverage_ready` 表示“当前快照是否已覆盖 Implementable Desired”，不是对未来 Add 的预测；
- `cleanup_candidates` 是若此刻进入正式流程、且之后 S1 仍满足全部安全门时才可能删除的预览，不得写成“将删除”；
- `cleanup_deferred` 明确列出当前已知阻断原因；
- 每目标只 Describe 一次，每 host 只解析一次；限速发生在目标之间，不在同一目标的规则之间 sleep；
- `target_id` 作为 Vue 稳定 key；domain 仅为来源列表，禁止再用 domain 作唯一 key；
- 所有数组固定输出 `[]`，不得输出 null。

### 7.2 事件

新增：

```go
EventTargetSyncComplete EventType = "target:sync_complete"
```

事件 Data 至少包含：provider、target_id、domains、outcome、added、deleted、unsupported、cleanup_candidates、cleanup_deleted、cleanup_deferred、duration_ms；失败时还包含稳定 error。`EventDomainSyncComplete` 不再承载云端增删数量，新代码不得同时发布两套完成事件造成重复日志。

DNS 失败继续发布 `EventDNSFailed`，但相同目标/host 一轮最多一次。目标最终失败继续使用 `EventSyncError` 或等价目标失败事件；若保留 `EventSyncError`，Data 必须明确 target_id/domains，不能伪装成单域名结果。

### 7.3 sync_logs

本轮不迁移 Schema：

- 一条目标完成/失败事件写一条日志；`target` 仍写资源 ID；
- `domain` 写经稳定排序后用 `, ` 连接的来源域名（2026-09-29 用户裁决：保留 `logs` 页「域名」列与详情弹窗的可读性，该列不承担结构化职责）；无来源域名时为空；
- `result` 仍使用 success/partial/failed；cleanup_deferred 只追加黄色可读详情，不把 success 改成 partial；
- `added/deleted` 写云端确认数；
- `StoreLogWriter.OnEvent` 必须直接返回 `AddSyncLog` 错误，由 EventBus 现有统一 WARN 处理，不能内部 WARN 后返回 nil；
- 详情与日志不得包含云凭据、完整 Push/Webhook URL 或其他敏感字段。

### 7.4 Dashboard 与 OperationalHealth

- Dashboard 只依据后端 `last_round.outcome` 和 cleanup 计数展示；不得用 `outcome != success` 推导失败；
- idle：中性信息或不展示异常，不能出现“最近一轮未完整成功”；
- partial：黄色，明确是平台无法实施；OperationalHealth 仍按 Build7 既有合同 unhealthy；
- failed：红色；OperationalHealth unhealthy；
- success + cleanup_deferred：黄色提示“所需权限已确认，陈旧规则清理延后”，OperationalHealth healthy；
- success 且无 deferred：正常绿色/中性成功；
- `/api/health/operational` 继续只消费 Syncer 的单一 `RoundOutcome`，不得在前端或清理器中建立第二套健康算法。

### 7.5 Dry Run 页面样式

沿用现有 Naive UI 与 AGENTS 的 UI 规范：

- 顶部统计卡：目标数、所需功能、已由 TAG 满足、已由外部满足、待新增、清理候选、无法实施、错误；
- 每目标一个卡片，标题显示 Provider/资源 ID，副标题显示来源域名数；
- `to_add` 用主色/信息色，`satisfied_by_external` 用中性色，`cleanup_candidates` 与 `cleanup_deferred` 用 warning，DNS/error 用 error；
- 清理候选标题必须写“满足安全门后可清理”，不得写成确定动作；
- `cleanup_deferred`、`unsupported`、`conflicts` 显示稳定原因文案；
- 页面级“执行模拟测试”继续 `size="large"`；表格内部不新增大号按钮；本项不新增确认弹窗，因为 Dry Run 无写入。

---

## 八、与其余审计问题的关系

### 8.1 必须吸收

| 审计项 | 固定处理 |
|---|---|
| P0-01 Aliyun 端口 key 不对称 | 已由 `108e528` 修复；新 canonical key 必须保持 `TestDiff_AliyunPortRoundTripConverges` 绿色，并覆盖单端口/范围/ALL |
| P1-01 comment 碰撞互删 | description/comment 不再承担个体身份；以目标级完整 Desired 取代 |
| P2-01 IPv6+ICMP key 不对称 | family 显式入 key，ICMP/ICMPv6/ICMPV6 双向归一化 |
| P2-02 ECS 删除 >100 | S1 RuleID 稳定分批，每批最多 100，部分成功如实计数 |
| P2-03 平台能力静默丢弃 | 统一能力矩阵产出 unsupported，目标 partial 且冻结清理 |
| P2-08 Dashboard idle 误报 | Step 4 改为消费后端 outcome，idle/cleanup_deferred 不 unhealthy |
| P2-09 Dry Run key 重复 | 每目标一项，以 target_id 为稳定 key |
| P3-11 日志吞错 | 目标事件定型后让写库错误返回 EventBus |
| P3-22 Dry Run 重复 Describe/sleep | 每目标一份快照，目标间限速 |
| P3-25 ECS token 不推进 | 重复 token 硬失败，目标 failed 且零删除 |

### 8.2 受影响但不构成前置

| 审计项 | 排序决定 |
|---|---|
| P3-02 熔断器 | 本项只固定每目标每 host 每轮最多解析一次，不扩张为熔断重构 |
| P3-06 CVM 100 条口径 | 真实账号确认前保留偏保守实现，不阻塞目标级所有权 |
| P3-07 `retrySync` 死包装 | Step 1～4 后重新证明生产零引用再删除 |
| P3-10 忽略 error | Step 2～4 触及路径当场修；其余点仍独立 |
| P3-16 前端偏移 | 仅 Dry Run 相关子项并入 Step 4 |
| P3-23/P3-24 | ticker/reset 与 pause/resume 语义正交，不与目标状态机同时改 |

### 8.3 正交问题

- P1-02/P3-08 pidfile/flock；
- P2-04/P3-03 RuntimeState 发布顺序与 Push 首发；
- P2-05/P2-06/P2-07 Webhook、告警页加载守卫、删除确认；
- P3-01、P3-04、P3-05、P3-09、P3-12～P3-15、P3-17～P3-21。

**越界确认（2026-09-29 用户裁决）：** 上述正交问题在本次 Step 1～5 一次性授权中**全部保持越界、不予修复**，按 `fwalizer-audit-final1.md` §0.4 独立队列（I-01～I-11）另行处理。其中 P2-04 是 `AGENTS.md` §9.1 发布顺序（`RuntimeState` 先于唤醒）与当前 `webui/api/deps.go` 实际顺序（唤醒先于 `ApplyState`）之间**已登记**的差异，本次只如实记录，不修改协调器发布路径与相关测试。唯一例外是 P3-16 的 Dry Run 子项（`RunTest.vue` 页面级按钮补 44px），已并入 Step 4。

---

## 九、串行实施步骤

### Step 0｜文档合同（已完成，本次已补强）

**范围：** 只同步产品语义、代码参考、步骤、测试与验收边界；不改代码。

**已完成：**

1. `AGENTS.md` 固定 TAG、功能 key、先增后验、版本保护、健康口径和事件强合同；
2. `Design5.md`、审计报告与 `ProdTestList.md` 同步设计和真实验收入口；
3. 创建本文；
4. 本次把纯规划器结构、快照版本、状态机、字段口径、四平台适配、失败矩阵、逐 Step 文件/测试/门禁补齐。

完成只代表合同可执行，不代表 P1-01 已修复。

### Step 1｜纯规划器、快照模型与失败先行用例（✅ 已实施）

**先写红灯：**

1. `[TAG]foo` 不属于 TAG；Format/Parse/OwnedRules 一致；
2. 两条空 comment、不同域名形成两个 Desired key，互不成为 cleanup candidate；
3. 同域名 TCP 443 + UDP 443 或多端口互不删除；
4. comment 修改后相同 key 零 Add、零 cleanup candidate；
5. External 精确等价满足，宽 CIDR/宽端口不推断满足；
6. Aliyun `443↔443/443`、`8000-8010↔8000/8010`、ALL 收敛；
7. IPv6 ICMP 与 ICMPv6/ICMPV6 收敛；
8. SWAS IPv6/DROP、ECS ICMPv6 进入 unsupported 且冻结清理；
9. ECS NextToken 重复返回 error，不返回部分 snapshot；
10. 相同功能 key 的 comment 按 rule ID 升序取第一个非空值；
11. 空 Desired 不授权清空 TAG；
12. Lighthouse 重复 key、缺 locator、opposite action 进入 conflict/deferred。

**实现范围：**

- `internal/tag/tag.go` 严格所有权；
- `provider/common.go` 唯一 FunctionalKey、能力矩阵、目标级纯 planner；
- `provider/provider.go` snapshot/revision 与 plan DTO；
- 四 Provider 的完整 snapshot，先只读不接正式写入；
- ECS token 进度保护；
- 旧 `Diff/buildDesired` 可暂留适配测试，但新主线不得继续扩展它们。

**定向门禁：**

```bash
go test ./internal/tag ./provider ./syncer -race -count=1
go vet ./provider ./syncer
git diff --check
```

**完成标准：** 红灯全部转绿；planner 无网络/数据库/时间依赖；S0 不完整时没有可用删除计划；P0-01 既有回归继续通过。

**停止条件：** planner 仍依赖 comment/description 区分个体；FunctionalKey 有第二套实现；快照缺 version 仍能进入腾讯写入；需要新增数据库表或更改强合同。

### Step 2｜目标级先增后验主流程（✅ 已实施）

**先写红灯：**

1. B 的 Create 调用必定早于 A 的 Delete；本 Step Delete 调用恒为 0；
2. Add 失败/状态未知、S1 Describe 失败、S1 缺覆盖时旧规则全保留且目标 failed；
3. External 精确满足时零 Create；
4. version mismatch 重新 Resolve/Get S0/Plan，不沿用旧 snapshot；
5. 每目标每 host 每轮只 Resolve 一次，每目标每 attempt 只取规定的 S0/S1；
6. 三次重试与 1s/2s 退避可用测试时钟/接缝验证，不让单测真实等待；
7. 幂等已存在必须经 S1 才确认，不虚增 Added。

**实现范围：**

- `syncer/syncer.go` 调度单元改为目标；
- `syncer/retry.go` 改为整个目标 attempt；
- Provider Create 接受 S0 revision；
- 实现 S0→Add→S1→coverage verification；
- 暂时所有平台零自动 Delete；候选全部标记 cleanup_deferred；
- RoundSummary 内部先切目标口径，UI/API 兼容收口留 Step 4。

**定向门禁：**

```bash
go test ./syncer ./provider -race -count=1
go test ./... -race -count=1
go vet ./...
go build ./...
git diff --check
```

**完成标准：** 正式同步已不再逐规则写云；所有 Add 后有 S1；无 Delete；目标级错误和计数可判别。

**停止条件：** 任一路径在 S1 覆盖确认前删除；重试复用旧 version/PolicyIndex/RuleID；同目标并行写导致配额或版本竞争；Add 失败仍 success。

### Step 3｜四平台条件清理（✅ 已实施，严格 Lighthouse→CVM→SWAS→ECS 串行）

严格按 Lighthouse → CVM → SWAS → ECS 串行，每完成一个平台立即跑该平台红绿用例，不先写一个丢失能力的通用删除器。

**Lighthouse：** S1 version、唯一 key/唯一 Owned、歧义冻结、mismatch 整目标重试、S2。

**CVM：** S1 `PolicyIndex + Version`、同请求批量删除、缺失/重复索引冻结、mismatch 重试、S2。

**SWAS：** 只用 S1 RuleId；缺 ID 冻结；External 永不操作；S2。

**ECS：** 只用 S1 SecurityGroupRuleId；150 条固定 100+50；第二批失败形成部分计数+deferred；S2。

**共通红灯：**

- 空 Desired、DNS error/empty、unsupported、conflict、coverage false 任一存在时 Delete=0；
- cleanup failure 不抹掉已确认 Add，不把目标改 partial/failed；
- 发生部分删除后 S2 缺覆盖则 failed；
- 候选在删除前再次验证严格 Owned 且 key 不属于 Desired；
- 非 TAG 与 `[TAG]foo` 永不进入 Delete 请求。

**门禁：**

```bash
go test ./provider ./syncer -race -count=1
go test ./... -race -count=1
go vet ./...
go build ./...
git diff --check
```

**完成标准：** 四平台分别证明删除定位与版本安全；清理失败/deferred 的状态和计数符合第五节。

**停止条件：** 为了复用代码去掉版本/RuleID；Lighthouse 歧义时仍删除；CVM 逐条复用旧版本删除；ECS 单批超过 100；清理失败使所需权限成功被改写为 partial。

### Step 4｜Dry Run、事件、日志、Dashboard 与健康口径（✅ 已实施）

**实现范围：**

- `DryRun` 共用 Step 1 planner，每目标一次 snapshot/host 解析去重；
- `DryRunResult` 与前端类型补齐第七节字段；
- 新增 `EventTargetSyncComplete`，移除生产链对逐域名增删完成事件的依赖；
- StoreLogWriter 改为目标级并正确返回写库 error；
- RoundSummary/API/frontend 全部切换目标统计口径；
- Dashboard 修复 idle，增加 cleanup_deferred 黄色提示；
- Dry Run 页面按目标卡片展示，target_id 为 key；
- 不修改 Build7 OperationalHealth 算法，只让它消费正确 outcome。

**红灯：**

1. Dry Run 与正式 S0 planner 对同一输入逐字段一致；
2. 同域名多规则只出现一个目标卡片且无重复 Vue key；
3. 每目标一次 Describe，目标内不 sleep；
4. arrays 非 null；候选文案不是确定删除；
5. cleanup_deferred → success/healthy + Dashboard warning；
6. idle → healthy 且不显示失败；
7. unsupported → partial/503；DNS/Add/verify → failed/503；
8. 一目标只写一条完成日志；日志写库失败由 EventBus WARN；
9. EventDomainSyncComplete 不再承载云增删数。

**门禁：**

```bash
go test ./syncer ./webui/... ./internal/health ./notifier -race -count=1
go test ./... -race -count=1
go vet ./...
go build ./...
cd webui/frontend && npm ci
cd webui/frontend && npm run build
cd webui/frontend && npm audit --audit-level=high
cd webui/frontend && npm audit --omit=dev --audit-level=high
git diff --check
```

执行命令时应在仓库根目录分别运行前四/最后一条；上方 `cd` 行是独立命令示意，不得在错误目录连续嵌套 `cd webui/frontend`。

**完成标准：** 正式同步、Dry Run、事件、日志、Dashboard 使用同一目标结果；不存在第二套健康判断。

**停止条件：** Dry Run 复制 planner；cleanup_deferred 制造 partial/unhealthy；页面仍以 domain 为唯一 key；API 把空数组输出为 null；为了 UI 方便改变强合同。

### Step 5｜完整门禁、进程/Docker 验收、文档闭环与真实云（✅ 本地门禁与验收已实施；真实云与浏览器仍待人工执行）

**自动门禁：**

```bash
gofmt -l <本次修改的 Go 文件>
go test ./... -race -count=1
go vet ./...
go build ./...
cd webui/frontend && npm ci
cd webui/frontend && npm run build
cd webui/frontend && npm audit --audit-level=high
cd webui/frontend && npm audit --omit=dev --audit-level=high
docker compose -f docker-compose.yml.example config --quiet
docker build -f build/Dockerfile -t fwalizer:issue7 .
git diff --check
```

`gofmt -l` 必须无输出；npm 与根目录命令分开执行。Docker 继续验证非 root、`/api/health` HEALTHCHECK、SPA 资源、SIGTERM 有界退出；不得把 HEALTHCHECK 改成 operational。

**最小回归矩阵：**

- 两个空 comment/不同域名不互删；
- 同域名多协议/多端口不互删；
- comment 修改零增删；
- External 精确满足且永不操作；
- B 创建并经 S1 确认早于 A 删除；B 失败时 A 保留；
- `[TAG]foo` 不归属；空 Desired 不清空；
- Aliyun 端口与 IPv6 ICMP 归一化；
- unsupported、DNS、Describe、Add、S1/S2 verification、conflict、cleanup_deferred 的 outcome；
- Lighthouse/CVM version mismatch；Lighthouse 歧义；CVM 索引同快照；SWAS/ECS RuleID；ECS 100+50 与 token 循环；
- Dry Run 与正式 planner 同源；目标事件/日志/前端/OperationalHealth 一致。

**真实外部验收：** 只在 Step 1～4 全部完成后，按 `ProdTestList.md` 的 PT-I7-01～06 分层执行。四平台任一未执行都保持“未执行”；mock、SDK request test、本地二进制、浏览器或 Docker 均不能替代真实云结论。

**本次浏览器边界（2026-09-29 用户裁决）：** 本次实施**不执行浏览器验证**（本机无浏览器自动化工具，且不为此引入新依赖或改动前端依赖）。Step 5 的浏览器层证据记为「未执行」，由用户在 `ProdTestList.md` 中单独执行；Step 5 只提供自动测试、真实二进制 HTTP/静态资源与 Docker 证据，三者都不得外推为浏览器回归通过。

**人工核验登记（2026-09-29 用户要求）：** 本次实施中凡需要真实外部环境或人工操作才能确认的项（真实四云、真实 SMTP/收件箱、真实 Webhook、真实 Uptime Kuma、浏览器回归、SWAS `Remark` 上限等），一律更新到 `ProdTestList.md`，由用户单独执行；不得在本文或其他文档中写成已通过。

**文档闭环：** 只有真实完成的 Step 才可改为已完成，并记录 commit/HEAD、实际命令、结果、偏差和未执行边界；同步 `AGENTS.md` 的实施状态、`Design5.md`、审计状态索引、`ProdTestList.md` 与 README（如用户可见行为变化）。不得改写 Build6/Build7 的历史证据。

---

## 十、总验收矩阵

| 层次 | 必须证明 | 不能替代 |
|---|---|---|
| 纯 planner 单测 | 所有权、canonical key、目标聚合、能力矩阵、候选与安全门 | Provider SDK 请求正确 |
| Provider request mock | revision/PolicyIndex/RuleID、批量边界、分页完整性 | 真实云行为 |
| Syncer 单测/race | 先增后验、整目标重试、计数/outcome、并发与快照 | 浏览器与真实云 |
| API/前端测试 | 目标级 JSON、非 null、事件/日志、UI 状态 | 云端写入安全 |
| 真实二进制 | 运行时链路、SSE/API、OperationalHealth、退出 | Docker/真实云 |
| Docker | 静态二进制、非 root、静态 health、退出 | 云账号与平台限制 |
| 浏览器 | 目标卡片、颜色、文案、稳定 key、交互 | 真实云调用 |
| 真实四云 | API 版本竞争、真实定位、顺序、限制与残留 | 自动测试 |
| 远端 CI/GHCR | 远端构建与发布 | 本地/真实云功能验收 |

---

## 十一、统一停止条件与授权边界

遇到以下任一情况，停止当前 Step，保留已有证据并请求用户决定：

1. 本文与 `AGENTS.md`、Design/Build/Issue 发现未记录的新冲突；
2. 需要改变 TAG 48 字符上限、强制 comment、增加 Schema/持久映射或历史迁移；
3. 平台 API 文档无法支持既定版本/删除策略，且需要改产品语义；
4. 无法保证 Add 先于 Delete、快照完整或整目标重试；
5. 需要访问真实云、使用用户凭据、推送 tag、触发远端发布或改变外部系统状态，而用户尚未授权；
6. 工作树出现与本 Step 重叠的用户改动且无法安全绕开；
7. 自动测试只能靠放宽断言、删除正向控制或把 mock 冒充外部通过才能变绿；
8. 实现范围扩张到第 1.2 节明确排除项或第 8.3 节正交问题。

用户未来若一次性授权 Step 1～5，仍必须逐 Step 串行，完成当前 Step 的红绿测试和记录后才能进入下一 Step；不得并行构建多个 Step。

---

## 十二、证据分层与变更记录

### 12.1 当前证据边界

- 当前只有 Step 0 文档一致性证据与 Step 1 开工前的**基线门禁**证据（`go test ./... -race -count=1`、`go vet ./...`、`go build ./...`、前端 `npm ci`/`npm run build`/两条 `npm audit`、`docker compose config --quiet` 在 HEAD `3ce40fe` 全部通过），基线门禁只证明「改动前仓库是绿的」，不是 P1-01 已修复的功能证据；
- 原审计 overlay 探针证明 P0-01/P1-01/P2-01 在审计基线存在；P0-01 后续由 `108e528` + 回归证明已修，P1-01/P2-01 仍未修；
- 本文引用的当前源码形态只是待替换基线，不表示目标结构已存在；
- 真实 SMTP/Webhook/Uptime Kuma、真实云、浏览器与远端 CI/GHCR 的既有未执行状态保持不变。

### 12.2 文档变更记录

| 版本 | 日期 | 内容 |
|---|---|---|
| v1.0 | 2026-09-29 | Step 0：固定目标级完整期望集、TAG 所有权、comment 纯可读、先增后验、四平台条件清理、状态口径与 Step 1～5 |
| v1.1 | 2026-09-29 | 详细补强：增加历史/当前基线区分、不可破坏不变量、严格 TAG 语法、canonical key 表、当前源码替换地图、Provider snapshot/revision 参考、纯 planner DTO、冲突/原因码、S0/S1/S2 状态机、重试与计数、四平台 API 合同、Dry Run JSON/事件/日志/UI 样式、逐 Step 红绿测试/命令/完成与停止条件、统一验收矩阵；仍仅为文档，Step 1～5 未实施 |
| v1.3 | 2026-09-29 | Step 1～5 本地实施完成：回写实施状态、逐 Step 完成标记、§12.3 实施证据（HEAD、实际修改文件、逐条命令与结果、判别性用例、与原方案偏差、未执行边界）与 ProdTestList 人工核验项（PT-I7-01～07）|
| v1.2 | 2026-09-29 | Step 1 开工前裁决回写：登记 Step 1 开工基线（HEAD `3ce40fe`、ahead 2、工作树干净、基线门禁全绿）与本次一次性授权边界（含 `docker build`，不含提交/推送/tag/远端 CI/GHCR/真实云/浏览器）；Lighthouse 逗号端口固定为「展开项即 Desired 与 ToAdd 最小单元」；§5.2 固定 Resolve 粒度（每 attempt 按 host 去重重新解析，红灯 5 只针对单 attempt 轮）；§7.3 `domain` 固定写稳定排序后的来源域名；§8.3 明确 P2-04（AGENTS §9.1 发布顺序与源码差异）等正交问题全部越界，仅 P3-16 Dry Run 子项并入 Step 4；Step 5 登记「本次不执行浏览器验证」与「人工核验项统一更新到 ProdTestList」两条边界 |

### 12.3 本次实施证据（Step 1～5，2026-09-29）

> 证据分层：以下「已证明」只覆盖对应层次；mock/单测不能替代真实云，Docker 不能替代浏览器，本地不能替代远端 CI。

**基线与范围**

- 实施前 HEAD：`3ce40fe47bcb9e75abd7d72dcfea4265ea8e4da3`（`main`，相对 `origin/main` ahead 2，工作树干净）。
- 实施后：**同一 HEAD，改动全部留在工作树，未提交**；`git status` 显示 30 个修改文件 + 7 个新增文件（`provider/plan.go`、`provider/plan_test.go`、`provider/snapshot_test.go`、`syncer/target.go`、`syncer/target_test.go`、`syncer/cleanup_test.go`、`syncer/dryrun_test.go`）。
- 规模：测试函数 446 → **502**，测试文件 62 → **67**，Go 包仍为 12。

**实际执行的命令与结果（全部在本机实测）**

| 命令 | 结果 |
|---|---|
| `gofmt -l <本次修改的 Go 文件>` | 无输出 |
| `go vet ./...` | 无输出（通过） |
| `go build ./...` | 通过 |
| `go test ./... -race -count=1` | **12/12 包 ok**（根 10.8s、app 2.0s、config 8.7s、dns 1.6s、internal/health 4.4s、internal/portconv 2.8s、internal/tag 3.2s、notifier 3.6s、provider 4.3s、syncer 48.1s、webui 10.0s、webui/api 16.0s） |
| 最小回归矩阵（`-run` 组合，含 tag/provider/syncer/webui/api/webui） | 全部 ok |
| `cd webui/frontend && npm ci` | 通过（lockfile 未变化） |
| `cd webui/frontend && npm run build` | 通过（`✓ built in 126ms`） |
| `cd webui/frontend && npm audit --audit-level=high` | **0 漏洞** |
| `cd webui/frontend && npm audit --omit=dev --audit-level=high` | **0 漏洞** |
| `docker compose -f docker-compose.yml.example config --quiet` | 通过 |
| `docker build -f build/Dockerfile -t fwalizer:issue7 .` | 通过（镜像 sha256:311cad7a…） |
| `git diff --check` | 干净 |

**真实二进制验收（隔离 `FWALIZER_DATA_DIR`，未污染用户数据）**

- `GET /api/health` → 200（静态存活语义未变）；`GET /api/health/operational` → 200 `{"status":"ok","reasons":[]}`。
- SPA：`/` → 200 `text/html`；`/assets/index-*.js` → 200 `text/javascript`。
- `POST /api/sync/dryrun`（`{}`）→ `{"results":[],"warnings":["暂无云资源目标…","暂无域名规则…"]}`：目标级 DTO、数组非 null 已端到端成立。
- 启动轮日志：`outcome=idle total=0 … cleanup_candidates=0 cleanup_deleted=0 cleanup_deferred=0`（目标级计数口径生效）。
- `SIGTERM` → `收到停止信号，等待当前轮次完成…` → `开始 HTTP 关闭` → `同步引擎停止` → `HTTP 关闭完成`，**退出码 0，用时 7ms**。

**Docker 容器验收**

- 镜像：`User=appuser`、`Entrypoint=[fwalizer]`、`HEALTHCHECK` 仍为 `wget … /api/health`（未改成 operational）。
- 容器内 `id` → `uid=1000(appuser)`；`docker inspect .State.Health.Status` → `healthy`（failing=0）。
- 容器内 `/api/health` 200、`/api/health/operational` 200、SPA index/assets 200。
- `docker stop` → **131ms，ExitCode=0**，日志出现完整优雅退出序列。

**关键判别性证据（mock/SDK 请求层，本地）**

- P0-01 回归保持绿色（`TestDiff_AliyunPortRoundTripConverges`）+ 新规划器同形态收敛用例（SWAS/ECS 单端口/范围/ALL/斜杠回读 9 组）。
- P1-01：两条空 comment、不同域名互不成为清理候选；comment 修改零增删；`[TAG]foo` 既不归属也不可能被删除。
- P2-01：IPv6 ICMP 与 `ICMPv6`/`ICMPV6` 收敛。
- P2-02：ECS 150 条删除固定拆 100+50；第二批失败保留前批确认计数、剩余计入 `cleanup_deferred`。
- P2-03：SWAS IPv6/DROP、ECS ICMPv6 进入稳定 `unsupported_*` 原因码，不进入 `to_add` 且冻结清理。
- P2-08/P2-09/P3-11/P3-22/P3-16(Dry Run)：见 Step 4 用例与前端改动。
- P3-25：ECS `NextToken` 不推进时 2 次请求内失败并返回 `snapshot_incomplete`（修复前为无限重发同一 token）。
- 顺序不变式：`snapshot → create → snapshot(S1) → delete(S1) → snapshot(S2)`；Add 失败 / S1 缺覆盖 / 快照失败 → 删除调用恒为 0。
- 腾讯版本保护：Create 用 S0、Delete 用 S1 的 `FirewallVersion`/`Version`；缺失即失败且不发请求；版本竞争可重试。
- 目标级口径：`total == ok + changed + failed + skipped`；`cleanup_deferred` 不改变 success，OperationalHealth 算法未改动（`internal/health` 零改动）。

**与原方案的偏差（如实记录）**

1. **Lighthouse 逗号端口改为「一 canonical 端口项一条规则」**（按用户裁决）：已存在的合并规则因展开后多个 key 均被覆盖而不会被重建或删除，仅新建时下发为多条单端口规则。
2. **SWAS 分页判据修正**：首版沿用「本页不足一页即结束」时新增用例真实失败（`仅读取 2 条，云端声明共 3 条`），据此改为以 `TotalCount` 为权威判据并增加页数硬上限与空页失败。
3. **CVM 缺失 `PolicyIndex` 的规则保留在快照中**（而非像修复前那样跳过），使其仍能参与覆盖判断，仅由规划器标记为 `owned_locator_missing` 的 `cleanup_deferred`。
4. **Step 2 的「零自动删除」为临时性质**，Step 3 起门开即删；相关两个用例已迁移为更强的「顺序不变式」断言。
5. **Dry Run 目标内不再 sleep**：旧实现逐规则 sleep，违反 §7.1。
6. **`EventDomainSyncComplete` 常量保留但生产链不再发布**（避免删除事件类型造成无谓的强耦合），由 `EventTargetSyncComplete` 取代。
7. **`config.RuleInfo` 增加 JSON 标签**（供 `PlanMatch.rules` 直接序列化），不改变任何持久化 Schema。
8. **既有用例 `TestIsRetryable_RealWorldShapes` 出现一次偶发不稳定**（高负载下拿到 `connection reset` 而非超时形状）：`-count=3 -race` 复跑通过、全量复跑 12 包全绿，未放宽任何断言，如实登记。

**未执行边界（不得写成通过）**

- **真实腾讯云 / 阿里云**：未执行任何读写调用。四平台删除安全性、版本竞争、ECS 分批在真实账号下的行为、CVM 入站 100 条配额口径均**没有真实云结论**（PT-I7-01～05、PT-I7-07）。
- **浏览器**：本轮按用户决定**不执行**；Dry Run 目标卡片、重复 key、颜色与文案只有源码级 + 构建产物证据（PT-I7-06）。
- **真实 SMTP / 收件箱 / Webhook / Uptime Kuma**：沿用既有「未执行」状态（PT-B7-01～06）。
- **远端 CI / GHCR**：未推送、未触发；既有 `v2.0.0` 结果属更早 revision，不能证明当前改动（PT-B7-08）。
- **SWAS `Remark` 长度上限**：仍无仓内文档依据（PT-B7-09）。
- 本次**未提交、未推送、未创建 tag、未修改任何远端状态**。
