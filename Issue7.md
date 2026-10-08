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
> **实施状态（2026-09-29）：** Step 0～5 **主体已本地实施**（本地提交 `28559ed`）：Step 1 纯规划器/快照模型、Step 2 目标级先增后验、Step 3 四平台条件清理、Step 4 Dry Run/事件/日志/仪表盘口径、Step 5 本地门禁 + 真实二进制/Docker 验收 + 文档闭环。逐 Step 证据见 §12.3；完整核验确认的未完成项见 §12.5，故不得将“主体已实施”表述为无保留闭环。
>
> **独立核验补强（2026-09-29，已提交）：** 对 `28559ed` 做独立只读核验后，按用户裁决修复两项缺陷——F1 Lighthouse 期望侧多端口展开粒度回归（`provider/plan.go` + 新增判别性用例）、F5 SWAS 分页硬上限用尽未按 `snapshot_incomplete` 失败（`provider/ali_swas.go` + 新增判别性用例）；并如实登记 F2（`go test ./... -race -count=1` 受既有 flaky 用例 `TestIsRetryable_RealWorldShapes` 影响，按裁决不改测试代码）与 F4（可重试清理失败语义分歧，仅登记）。补强已提交为 `38bdc19`；提交后 `main` 相对 `origin/main` ahead 4、工作树干净。完整记录见 §12.4。**仍未推送、未打 tag、未触发远端 CI/GHCR、未调用真实云、未执行浏览器验证**。完整复核发现的未完成项独立追踪见 §12.5。
>
> **R7 修复进展历史快照（2026-09-29）：** R7-01 已提交为 `b80b1b0`，R7-02 已提交为 `eab4bea`，R7-03 已按用户裁决 A 修复并提交为 `297ccfe`：Dry Run 对所有已配置目标各返回一项；无适用规则目标只返回非 null 空数组骨架与 `coverage_ready=false`，不解析 DNS、不读取云快照、不进入 planner、不产生限速等待；前端以“无适用规则”卡片明确正式同步会跳过该目标；正式同步 `RoundSummary.Total` 仍只统计有适用规则目标。当时 `main` 相对 `origin/main` ahead 1、工作树干净，尚未推送；当时进一步研究确认 R7-04～R7-06 尚未完成，并新增 R7-07。该段只保留当时的发现过程，当前状态以下一段为准。
>
> **R7 当前核验状态（2026-09-30）：** R7-06/R7-07 与 R7-04 已按 §12.5.4 固定顺序修复并提交为 `b19d271`：两个测试夹具持有并有界回收 accepted `net.Conn`；私有 `cleanupResult{deleted,deferred}` 让成功可信的 S2 planner 直接提供最终残留数，NotFound 不虚增实际删除，S2 失败继续使用保守 fallback 并保持 `failed`。R7-05 已按测试-only 边界修复并提交为 `d6d208e`：以 `json.RawMessage` 两层检查顶层/目标级全部数组的存在、非 `null` 与 array 类型，覆盖三种真实成功形状，并逐字段加入 `null`、缺失、对象类型负向控制；未修改生产 DTO、planner、API 或前端。提交中记录的两个 GOGC 压力门禁、`syncer`/`provider` 包 `-race -count=20`（Syncer 使用 `-timeout=20m`）、全量 12 包 race 连续 3 次、vet、build、前端 build 与 diff-check 均通过。**本次文档核验没有重跑这些门禁**；只以 Git 祖先关系、当前源码/测试静态证据确认 R7-01～R7-07 均保留在当前 `HEAD`。**该批次基线为 `main == origin/main == d6d208e`，核验开始时工作树干净**（属 2026-09-30 历史快照，非当前 HEAD）。其后已推进：`7aaa3f2`（I-01/flock）、`c23305e`、`7acf303`（P2-04）、`c5cc79d`（P3-03）、`93e0e4b`（P2-05）、`b84531b`（文档回写），当前 `main` 相对 `origin/main` **ahead 7**（见 AGENTS.md 头部与审计报告「最近核验基线」小节）。真实云、浏览器与当前 revision 的远端 CI/GHCR 仍未执行。

> **第二轮只读核验与更正（2026-09-30）：** 对 P1-01/R7 相关结论做完整只读真实性核验后，按用户批准方案回写文档与测试（生产代码零改动）。R7-01～R7-07 的提交祖先关系、生产符号与判别性测试**均在当前 HEAD `b84531b` 静态复核保留**（结论未变）；同时订正了三处与本主题相邻的过期表述：P3-07 的生产链指向（实为 `syncer.go:875 → syncTarget`，`syncDomain` 生产已不存在，且 `retrySyncDetailed`/`truncateDesc` 亦为死代码）、P3-25 同步路径的当前行号（`:107-113`），以及新增 **P3-26**（资源扫描分页中途空响应，I-19 范围不变）。本轮门禁（Go 侧）见 `fwalizer-audit-final1.md`「最近核验基线」小节；**未重跑任何历史门禁，也不外推长期稳定绿色**。

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
4. 可实施 Desired 子集非空（完整 Desired 非空仍不足以排除全量 unsupported；I8-01 方案 A）；
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
- 每个 attempt 都重新解析正常域名、取 S0、Plan；轮初已熔断域名适用下述半开探测例外；不得只重试最后一个 HTTP 请求；
- **Resolve 粒度裁决（2026-09-29 用户裁决）：** 「每个 attempt 重新 Resolve」优先于「每轮只解析一次」——每次 attempt 开始时按标准化 host 去重重新解析，同一 attempt 内同一 host 最多调用 Resolver 一次。Step 2 红灯 5 的「每目标每 host 每轮只 Resolve 一次」断言只针对单 attempt（无重试）成功轮；
- **P3-02 后续独立裁决（2026-10-02，优先于上一条的无例外表述）：** 正常域名继续每 attempt 新解析；轮初已熔断域名跨目标协调一次半开探测，失败结论只在本轮复用，探测成功后等待者与后续 attempt 重新解析；成功 IP 不共享。详见 §12.6。目标 attempt 次数、S0/S1/S2、版本保护、DNS 失败结果与事件粒度不变。
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
- 新增 `cleanup_candidates/cleanup_deleted/cleanup_deferred`：分别为最近清理观察的 S1 候选数、跨尝试累计实际确认清理数、最近观察/估计的残留兼容投影；当前完整性由 §12.9 新观察汇总表达；`deleted` 与 `cleanup_deleted` 在本 Issue 中数值相同，但保留前者兼容总写入语义、后者用于清理可观测性；
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
- P3-06 原属初始 Issue7 实施范围之外；2026-10-02 已按独立授权方案 B 本地修复 `checkRuleLimit`：入站本地保护上限 100，出站不占额度；完整入站统计与 Ingress 条目数取较大值，统计不完整则回退明确数组，无计数依据返回 `ErrSnapshotIncomplete`。Create 的额外 Describe 仅用于配额保护，不得替代 S0 Version 或 S1 删除定位；满额轮换不得先删腾位。真实返回形态仍待 PT-I7-03；
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

`POST /api/sync/dryrun` 路由和严格空对象/现有请求合同不因本项扩张；响应固定为**每个已配置目标一项**，`results` 永不为 null。建议目标项：

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
- 无适用规则的已配置目标只返回空数组骨架、`coverage_ready=false` 与空 `error`；该项表示“未调度”，不解析 DNS、不 Describe、不进入 planner、不限速等待，页面必须明确正式同步会跳过该目标；
- `cleanup_candidates` 是若此刻进入正式流程、且之后 S1 仍满足全部安全门时才可能删除的预览，不得写成“将删除”；
- `cleanup_deferred` 明确列出当前已知阻断原因；
- 每个有适用规则的目标只取一次完整 `GetSnapshot()`，每目标每 host 只解析一次；快照内部可分页/版本重读，不能等同于一次 HTTP 请求。2026-10-03 P3-22 后续 B 改进：按 `CloudType` 跨连续 Dry Run 保留冷却，只在下一次同平台读取前补足剩余间隔；成功/失败均从快照操作结束后计时，首次读取及末尾返回不额外等待，DNS/规划等已耗时间可抵扣，热更新/零规则不重置，无适用规则目标不读也不更新冷却。仍保持配置目标顺序与同平台串行，不扩为全部云请求的统一限流；见 §12.8。
- `target_id` 作为 Vue 稳定 key；domain 仅为来源列表，禁止再用 domain 作唯一 key；
- 所有数组固定输出 `[]`，不得输出 null。

### 7.2 事件

新增：

```go
EventTargetSyncComplete EventType = "target:sync_complete"
```

事件 Data 至少包含：provider、target_id、domains、outcome、added、deleted、unsupported、cleanup_candidates、cleanup_deleted、cleanup_deferred、duration_ms；I8-02 另追加 attempts/cleanup_observation/unsupported_observation（§12.9）；失败时还包含稳定 error。`EventDomainSyncComplete` 不再承载云端增删数量，新代码不得同时发布两套完成事件造成重复日志。

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

- Dashboard 依据后端 `last_round.outcome` 与 `cleanup_observation_summary` 展示（I8-02，§12.9）；不得用 `outcome != success` 推导失败；
- idle：中性信息或不展示异常，不能出现“最近一轮未完整成功”；
- partial：黄色，明确是平台无法实施；OperationalHealth 仍按 Build7 既有合同 unhealthy；
- failed：红色；OperationalHealth unhealthy；
- success + cleanup_deferred：黄色提示“所需权限已确认，陈旧规则清理延后”，OperationalHealth healthy；
- success 且观察完整、已观察残留为零：正常绿色/中性成功；存在估计/历史/未知时提示清理状态尚未最终确认，仍 healthy；
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
| P3-22 Dry Run 重复 Describe/sleep | Step 4 每目标一份快照；后续 B 按平台跨调用保留冷却、读取前补足等待，取消末尾等待，见 §12.8 |
| P3-25 ECS token 不推进 | 重复 token 硬失败，目标 failed 且零删除 |

### 8.2 受影响但不构成前置

| 审计项 | 排序决定 |
|---|---|
| P3-02 熔断器 | Issue7 初始实施不扩张熔断；后按 2026-10-02 独立授权方案 B 修复，每 attempt 正常解析 + 已熔断域名每轮半开探测例外，详见 §12.6 |
| P3-06 CVM 入站 100 条本地保护 | 2026-10-02 独立按 B 本地修复；官方默认入站/出站各 100，计数完整性和数组下界见 §6.2，不改目标级所有权/版本/删除主线；真实响应、模板统计与提额情况待 PT-I7-03 |
| P3-07 旧逐规则流程与描述包装 | 2026-10-02 按独立授权 B 删除 `retrySync` / `retrySyncDetailed` / `truncateDesc`，有效回归迁入生产目标链；保留现用错误判定、GetRules 与旧 Diff 回归，详见 §12.7 |
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
- 旧 `Diff/buildDesired` 可暂留适配测试，但新主线不得继续扩展它们。2026-10-02 P3-07 清理已删除旧 Syncer 流程；这些 Provider 适配与 P0-01 回归仍保留，`GetRules` 仍由连接测试生产 API 使用。

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
5. 正常路径每目标每 host 每 attempt 最多 Resolve 一次（单 attempt 成功轮即每轮一次），轮初已熔断域名按 §12.6 半开探测例外；每目标每 attempt 只取规定的 S0/S1；
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

- Step 1 开工前的**基线门禁**证据（`go test ./... -race -count=1`、`go vet ./...`、`go build ./...`、前端 `npm ci`/`npm run build`/两条 `npm audit`、`docker compose config --quiet` 在 HEAD `3ce40fe` 全部通过）只证明「改动前仓库是绿的」，不是 P1-01 已修复的功能证据；
- 原审计 overlay 探针证明 P0-01/P1-01/P2-01 在审计基线存在；三者均已在 Issue7 Step 1～4 实施后由 canonical key/能力矩阵消除，并由新增判别性用例与 `TestDiff_AliyunPortRoundTripConverges` 回归覆盖；
- 本文 §三「当前源码基线」表格是 Step 1 开工前的待替换基线地图，已被 §12.3 的实际实施结果取代；
- 真实 SMTP/Webhook/Uptime Kuma、真实云、浏览器与远端 CI/GHCR 的既有未执行状态保持不变。

### 12.2 文档变更记录

| 版本 | 日期 | 内容 |
|---|---|---|
| v1.13 | 2026-10-03 | P3-22 后续方案 B 正式改进：每平台跨调用保留冷却、读取前补足剩余等待、取消末尾等待；明确一次快照不等于一次 HTTP 请求；§7.1/§8.1/§12.8 与正式回归、负向控制、门禁及未执行边界同步 |
| v1.12 | 2026-09-30 | 在 `main == origin/main == d6d208e`、工作树干净的**当时**基线上，只读复核 R7-01～R7-07 的提交祖先关系、生产符号与判别性测试：七项本地修复均真实保留；R7-05 与 v1.11 文档回写已由 `d6d208e` 提交。订正 §12.5 标题、定位、R7-05 状态和文档闭环提交状态；本轮未修改代码、未运行构建或测试，真实云/浏览器/当前 revision 远端 CI/GHCR 仍未执行 |
| v1.11 | 2026-09-30 | **提交前历史快照：** R7-04/R7-06/R7-07 已提交为 `b19d271`；按方案 A 仅修改 `syncer/dryrun_test.go` 修复 R7-05 失效门禁，以 `json.RawMessage` 检查两层数组字段存在、非 null、类型为 array，覆盖有适用规则、无适用规则骨架、零结果三种真实输出，并对顶层与十个目标数组字段加入 null/缺失/对象负向控制；未发现生产 null，故未改 DTO/planner/API/前端。定向测试、两个 GOGC 压力门禁、两包 `-race -count=20`、全量 race 连续 3 次、vet/build/前端/diff-check 通过；Syncer 20 轮首次受 Go 默认 10m 总超时中止，保持次数不变并显式 `-timeout=20m` 后 892.213s 通过；该工作树随后提交为 `d6d208e`，外部验收未执行 |
| v1.10 | 2026-09-30 | 按固定串行顺序本地修复 R7-06/R7-07 测试夹具与 R7-04 最终残留计数：两个阻塞 TCP helper 持有 accepted connection 并有界回收；清理结果改为直接传递 deleted/deferred，可信 S2 planner 成为最终残留唯一来源，NotFound 保持实际删除为 0，S2 失败保守回退并保持 failed；新增目标级计数矩阵与 EventBus/整轮/SQLite `1/0/0` 整链；压力、定向 race、全量 race 连续 3 次、vet/build/前端/diff-check 通过；未提交、未执行外部验收，R7-05 仍待处理 |
| v1.9 | 2026-09-29 | 进一步研究 R7-03 后续未完成项：订正 R7-03 已提交为 `297ccfe`、当前 ahead 1/工作树干净的事实；细化 R7-04 的 S2 最终残留计数合同与 NotFound 不虚增删除数语义；细化 R7-05 的 `json.RawMessage` 判别方案与 null 负向控制；以 `GOGC=1` 判别性复现确认 R7-06 的阻塞 TCP 测试助手丢弃连接导致 `connection reset by peer`；新增 R7-07 追踪 `TestAliClientRequestIsBounded` 的同根因夹具缺陷；固定“先稳定门禁夹具，再修 R7-04/R7-05，最后多轮全量门禁与文档闭环”的串行顺序 |
| v1.8 | 2026-09-29 | R7-03 按用户裁决 A 本地修复：Dry Run 覆盖所有已配置目标；无适用规则目标只返回未调度空骨架且零 DNS/云 API/planner/限速，前端明确显示跳过态；新增“两目标仅一目标适用”和“零规则仍返回全部目标”判别性用例；正式同步统计口径不变；定向 Syncer race、前端构建、vet/build/diff-check 通过，全量 race 因未改动的 `TestAliClientRequestIsBounded/扫描路径_scanAliECS` 提前返回而失败，隔离 `-count=5` 仍复现一次，不写成全量通过；R7-04～R7-06 未处理 |
| v1.7 | 2026-09-29 | R7-02 已在当前工作树修复并随后提交为 `eab4bea`：目标完成/失败事件补齐 `cleanup_deleted`、目标全生命周期 `duration_ms` 与 canonical `[]provider.PlanIssue` `unsupported`，Add 失败保留 S0 已确认能力限制；日志 writer 移除 `skipped_details` 双源并消费 canonical 结构；新增 publisher/EventBus/SQLite 真实整链 `2/1/1` 判别用例；R7-03～R7-06 当时未处理 |
| v1.6 | 2026-09-29 | R7-01 已在当前工作树修复：以私有类型区分「S1 已确认覆盖后的可重试 Delete 错误」，前两次继续整目标重试，第三次耗尽收敛为 `success + cleanup_deferred`；新增限流/版本竞争耗尽与第三次成功的判别性用例，定向 race、健康回归、vet/build 通过；全量 race 仅再次复现既有 R7-06 flaky，本次未处理 |
| v1.0 | 2026-09-29 | Step 0：固定目标级完整期望集、TAG 所有权、comment 纯可读、先增后验、四平台条件清理、状态口径与 Step 1～5 |
| v1.1 | 2026-09-29 | 详细补强：增加历史/当前基线区分、不可破坏不变量、严格 TAG 语法、canonical key 表、当前源码替换地图、Provider snapshot/revision 参考、纯 planner DTO、冲突/原因码、S0/S1/S2 状态机、重试与计数、四平台 API 合同、Dry Run JSON/事件/日志/UI 样式、逐 Step 红绿测试/命令/完成与停止条件、统一验收矩阵；仍仅为文档，Step 1～5 未实施 |
| v1.5 | 2026-09-29 | 完整核验未完成项独立追踪：新增 §12.5 R7-01～R7-06，区分强要求违背、集成缺口、待裁决语义、计数偏差、测试证据失效与 flaky 门禁；同步补强已提交为 `38bdc19`、ahead 4、提交后工作树干净的当前事实 |
| v1.4 | 2026-09-29 | 独立核验补强（当时先留工作树，随后提交为 `38bdc19`）：① 修复 Lighthouse 期望侧多端口展开粒度回归（§12.4 F1）；② SWAS 分页上限用尽改为 `snapshot_incomplete`（§12.4 F5）；③ 如实登记 `go test ./... -race -count=1` 受既有 flaky 用例影响（§12.4 F2，按用户裁决不改测试代码）；④ 登记可重试清理失败的语义分歧待后续处理（§12.4 F4）；⑤ 订正 §12.3 的提交状态/文件计数与 §12.1 的过时基线表述 |
| v1.3 | 2026-09-29 | Step 1～5 本地实施完成：回写实施状态、逐 Step 完成标记、§12.3 实施证据（HEAD、实际修改文件、逐条命令与结果、判别性用例、与原方案偏差、未执行边界）与 ProdTestList 人工核验项（PT-I7-01～07）|
| v1.2 | 2026-09-29 | Step 1 开工前裁决回写：登记 Step 1 开工基线（HEAD `3ce40fe`、ahead 2、工作树干净、基线门禁全绿）与本次一次性授权边界（含 `docker build`，不含提交/推送/tag/远端 CI/GHCR/真实云/浏览器）；Lighthouse 逗号端口固定为「展开项即 Desired 与 ToAdd 最小单元」；§5.2 固定 Resolve 粒度（每 attempt 按 host 去重重新解析，红灯 5 只针对单 attempt 轮）；§7.3 `domain` 固定写稳定排序后的来源域名；§8.3 明确 P2-04（AGENTS §9.1 发布顺序与源码差异）等正交问题全部越界，仅 P3-16 Dry Run 子项并入 Step 4；Step 5 登记「本次不执行浏览器验证」与「人工核验项统一更新到 ProdTestList」两条边界 |

### 12.3 本次实施证据（Step 1～5，2026-09-29）

> 证据分层：以下「已证明」只覆盖对应层次；mock/单测不能替代真实云，Docker 不能替代浏览器，本地不能替代远端 CI。

**基线与范围**

- 实施前 HEAD：`3ce40fe47bcb9e75abd7d72dcfea4265ea8e4da3`（`main`，相对 `origin/main` ahead 2，工作树干净）。
- 实施后：Step 1～5 的改动先留在工作树（30 个代码/测试文件 + 7 个新增文件），随后连同 §12.3 的文档回写一并提交为 `28559ed1e12eb9078a8d6ac039b89b61cf3ec3ab`（`git show --name-status`：**34 个修改 + 7 个新增**，含本文件、`AGENTS.md`、`Design5.md`、`ProdTestList.md`、`README.md`、`fwalizer-audit-final1.md` 6 份文档）；提交后工作树干净，`main` 相对 `origin/main` ahead 3，**未推送**。
- 规模：测试函数 446 → **502**，测试文件 62 → **67**，Go 包仍为 12（`git ls-tree 3ce40fe` / `git grep '^func Test'` 与 HEAD 实测一致）。

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
- 本次实施**未推送、未创建 tag、未修改任何远端状态**（改动随后提交为本地提交 `28559ed`，见本节「基线与范围」）。

---

### 12.4 独立核验补强记录（2026-09-29，已提交为 `38bdc19`）

> **范围：** 对已提交的 `28559ed`（Step 1～5）做独立只读核验后，按用户 2026-09-29 裁决执行的补强：修 F1、修 F5、登记 F2/F4、订正文档。补强随后提交为 `38bdc19`。**仍然不含**推送、tag、远端 CI/GHCR、真实云、真实 SMTP/Webhook/Uptime Kuma 与浏览器验证。
>
> **后续状态说明：** 本节保留 `38bdc19` 时点的核验事实；其中 F4 后续由 R7-01（`b80b1b0`）修复，F2 的测试夹具根因后续拆为 R7-06/R7-07 并由 `b19d271` 修复。当前状态以 §12.5 为准。

**已核验通过（复核，不重复外推）**

- `gofmt -l`（改动 Go 文件）、`go vet ./...`、`go build ./...`、`git diff --check`：无输出/退出码 0。
- 源码级复核：严格 TAG 命名空间（`IsOwned` 为唯一判定，`Parse`/`OwnedRules` 共用）；唯一 canonical `FunctionalKey`（`SnapshotRuleKeys` 与 `PlannedActionKeys` 共用 `buildKeys`）；`PlanTarget` 无网络/时钟/日志依赖；四平台快照 revision/完整性（Lighthouse 版本重读、CVM `Version`+`PolicyIndex`、SWAS `TotalCount`、ECS token 不推进）；`syncer/target.go` 的 `S0 → Add(S0) → S1 → 同一 planner 复验 → 安全门 → Delete(S1) → S2` 顺序与「Add/覆盖验证失败时删除恒为 0」；目标级 Dry Run/事件/日志/Dashboard；`internal/health` 零改动、无 Schema/迁移改动（仅 `config.RuleInfo` 补 JSON 标签）。
- PlatformAPIDocs 依据复核：Lighthouse `FirewallVersion`（创建/删除/`UnsupportedOperation.FirewallVersionMismatch`）与 `FirewallRuleDescription ≤ 64`；CVM `SecurityGroupPolicySet.Version`（添加/删除/`UnsupportedOperation.VersionMismatch`）与 `PolicyIndex`；ECS `RevokeSecurityGroup.SecurityGroupRuleId` 数组 0~100 与 `InvalidParam.SecurityGroupRuleId`；SWAS `RuleIds` 与 `TotalCount`。Issue7 的版本/删除策略有仓内文档支撑。
- 真实二进制（本轮重新构建，隔离 `FWALIZER_DATA_DIR`）：`/api/health` 200 静态存活、`/api/health/operational` 200 且 `Content-Type: application/json; charset=utf-8` + `Cache-Control: no-store`、SPA 首页与 `assets/index-*.js` 200、`POST /api/sync/dryrun {}` 返回 `results: []`（非 null）、`SIGTERM` 退出码 0；启动轮日志为目标级计数（`outcome=idle total=0 … cleanup_candidates/cleanup_deleted/cleanup_deferred`）。
- Docker（`fwalizer:issue7`，镜像创建时间 13:20:39Z 早于提交 13:25:33Z，内容对应成为 `28559ed` 的工作树）：`User=appuser`、`Entrypoint=[fwalizer]`、`HEALTHCHECK` 仍为 `wget … /api/health`（未改成 operational）；容器内 `uid=1000(appuser)`、`Health=healthy`（FailingStreak=0）、容器内外 `/api/health` 与 `/api/health/operational` 200、`docker stop` 141ms 且 `ExitCode=0`；`docker compose -f docker-compose.yml.example config --quiet` 退出码 0。
- 规模核对：测试函数 446 → 502、测试文件 62 → 67、Go 包 12，与 §12.3 记载一致。

**F1｜Lighthouse 期望侧多端口展开粒度回归（已修复）**

- 现象（修复前实测，`go test -overlay` 零写入探针）：本地规则 `Ports="80,443"` 时 `PlanTarget` 只产出合并 key `ipv4|1.1.1.1/32|TCP|80,443|ACCEPT` 且 `ToAdd` 下发一条 `Port="80,443"`；而云端回读把同一条规则展开为 `…|TCP|80|…` 与 `…|TCP|443|…` 两个 key → 第二轮仍 `ToAdd=1`、`SatisfiedByOwned=0`、该规则反成 `CleanupCandidates=1`、`CoverageReady=false`。对照探针证明旧逐规则路径（`Diff` + `ConvertPorts`）在同形态下 `toAdd=0/toDelete=0`，即这是 Step 1 引入的回归，且与 §2.5 的 2026-09-29 用户裁决（展开项即 Desired/ToAdd 最小单元）及 §12.3 偏差 1 相反。
- 修复：`provider/plan.go` 新增 `expandPlannedPorts`（先按 `portconv.Parse` 拆分端口项，再对每项取线格式），`PlanTarget` 改用它；`ExpandPorts` 的合并语义只保留给旧逐规则写入路径，并在注释中写明粒度差异。
- 判别性证据：新增 `provider/plan_test.go:TestPlan_LighthouseDesiredMultiPortExpandsPerPort`（修复前红灯：`Desired 数量 = 1, want 2`；修复后绿灯：`Desired=2`、`ToAdd` 为 80 与 443 两条单端口、已存在合并规则时零新增零候选且 `CoverageReady=true`、两个单端口稳定形态同样收敛）；同一探针复跑显示第二轮 `toAdd=0 satisfiedOwned=2 candidates=0 coverage=true`。

**F2｜全量 race 门禁受既有 flaky 用例影响（如实登记，按用户裁决不改测试代码）**

- 现象：本次核验共执行 **3 次完整套件运行**（`go test ./... -race -count=1`）：修复前 2 次均为 `syncer` 包 FAIL（其中 1 次捕获到断言文本），修复后 1 次 **12/12 包 ok**。捕获到的失败点为既有用例 `TestIsRetryable_RealWorldShapes`（`syncer/retry_test.go:72`，其助手 `realHTTPTimeoutError` **未被 `28559ed` 改动**）：期望 `Client.Timeout exceeded while awaiting headers`，实际拿到 `read: connection reset by peer`。此外 `go test ./provider ./syncer -race` 合并运行 2 次（1 次失败、1 次通过，失败未捕获用例名）；单包隔离下 8 个 `-count=20` 批次中 2 个失败（约 2/160 次迭代），`./syncer -race -count=1` 单独运行多次通过。
- 处理（用户裁决）：**不修改测试代码、不放宽断言**，只在本文件与 `AGENTS.md` 如实登记。因此 Step 5 的完成标准应理解为「门禁可复现为绿，但存在一个与本项改动无关、环境相关的既有 flaky 用例」；不得把任意单次绿色运行外推为稳定绿色。

**F4｜可重试的清理失败语义分歧（已登记，未改实现与契约）**

- §5.3/§5.5 规定「删除失败且未确认删除」记 `success + cleanup_deferred`（healthy）；§5.2 又要求可重试错误进入下一整个目标 attempt。当前实现（`syncer/target.go` 的 `runTargetCleanup`）对**可重试**清理失败（限流/网络超时等）走整目标重试，3 次后目标 `failed` → unhealthy；只有**不可重试**失败才按 §5.5 记 `success + cleanup_deferred`（已有用例覆盖）。
- 处理：按用户 2026-09-29 裁决**只登记不改动**，留待后续独立处理；本条不构成本次的实施前置。

**F5｜SWAS 分页硬上限用尽未证明完整（已修复）**

- 现象（修复前红灯：`页数上限用尽仍未证明完整时必须失败，实际返回 10000 条规则`）：`provider/ali_swas.go` 的 100 页上限用尽且云端未返回 `TotalCount` 时，函数直接返回截断快照，违反 §4.1/§6.3「不能证明完整即必须失败」。
- 修复：新增 `proven` 判据，只有主动 `break`（`len(allRules) >= TotalCount` 或本页不足一页）才算证明完整；因页数上限自然结束即返回 `ErrSnapshotIncomplete`。
- 判别性证据：新增 `provider/snapshot_test.go:TestSnapshot_SWASPageCapFailsIncomplete`（修复前红灯；修复后绿灯：100 次请求后返回 `ErrSnapshotIncomplete` 且不返回半截规则）；既有 `TestSnapshot_SWASPaginatesAllPages`、`TestSnapshot_SWASIncompletePaginationFails` 继续通过。

**补强后的门禁与未执行边界**

- 定向门禁：`go test ./internal/tag ./provider ./syncer -race -count=1` → 3 包全 ok；`go test ./... -race -count=1` → 12/12 包 ok（F2 前提下不承诺稳定复现）。
- **未执行（不得写成通过）**：真实腾讯云/阿里云（含 Lighthouse 多端口收敛、四平台删除安全、版本竞争、ECS 分批）、浏览器回归、真实 SMTP/收件箱/Webhook/Uptime Kuma、远端 CI/GHCR；SWAS `Remark` 上限仍无仓内依据。
- 本次补强涉及 `provider/plan.go`、`provider/plan_test.go`、`provider/snapshot_test.go`、`provider/ali_swas.go` 与同期文档回写，已提交为 `38bdc19da5a791cc3c64ab01840fd8f3c4d1f9b8`；提交后 `main` 相对 `origin/main` ahead 4、工作树干净。**未推送、未打 tag、未修改任何远端状态**。

---

### 12.5 完整核验项独立追踪（2026-09-29～2026-09-30，R7-01～R7-07 本地已收口）

> **定位与授权边界：** 本节最初记录在 `HEAD 38bdc19` 上对 P1-01 实施改动进行三路独立只读复核后确认的问题，并作为逐项修复入口。R7-01～R7-03 已分别提交，R7-04/R7-06/R7-07 已提交为 `b19d271`，R7-05 已按测试-only 边界修复并提交为 `d6d208e`。2026-09-30 在 `main == origin/main == d6d208e`、工作树干净的基线上再次静态核验，七项实现与判别性测试均仍存在，相关提交均为当前 HEAD 的祖先。至此 R7-01～R7-07 的本地核验项已收口；§12.3 主体实现、§12.4 F1/F5 与本节后续修复证据继续分层记录。本次未重跑构建或测试，真实云、浏览器与当前 revision 的远端 CI/GHCR 仍未执行，不能外推为外部验收通过或无保留发布闭环。

| ID | 级别 | 状态 | 内容 | 当前证据与后续验收合同 |
|---|---|---|---|---|
| R7-01 | **P1** | ✅ 已修复并提交（`b80b1b0`） | **可重试清理失败耗尽后的 outcome 已符合 AGENTS 强要求。** `syncer/target.go` 新增私有 `retryableCleanupError`，只标记本 attempt 已由 S1 确认覆盖、失败点仅为 version mismatch/限流/网络超时等可重试 Delete 错误；前两次仍完整重试，第三次耗尽后保留残留并返回 `success + cleanup_deferred`。DNS/Describe/Add/S1/S2 失败仍走 `failed`，未修改 Provider、`isRetryable` 或 OperationalHealth。 | 红灯→绿灯：`TestCleanup_RetryableFailureExhaustedKeepsSuccessAndDefers` 覆盖限流与版本竞争，断言 3 次 attempt、每次重读快照、每次使用当次 S1 revision、最终 success/healthy 与 deferred=1；`TestCleanup_RetryableFailureThenSuccessKeepsWholeTargetRetry` 证明第三次成功仍删除并执行 S2。该提交的定向 `syncer` race、`internal/health` 回归、vet/build/diff-check 通过；当时全量 race 曾复现的 R7-06 后续已由 `b19d271` 修复。 |
| R7-02 | **P2** | ✅ 已修复并提交（`eab4bea`） | **目标事件与真实 sync_logs 清理详情已统一。** `publishTargetResult` 对完成/失败事件统一发布 `cleanup_deleted`、目标全生命周期 `duration_ms` 与非 null canonical `[]provider.PlanIssue` `unsupported`；`runTargetAttempt` 在 S0 规划后即保留 unsupported，故后续 Add 失败仍不会丢失能力限制；`StoreLogWriter` 删除 `skipped_details` 双源并直接消费 canonical 结构。 | 红灯→绿灯：`TestTargetSyncCompleteCarriesUnsupported`、`TestSyncErrorRetainsUnsupportedAfterAddFailure` 覆盖完成/失败公共字段与结构化能力限制；`TestStoreLogWriter_ProductionChainPersistsCleanupCounts` 通过真实 `Syncer.Run → syncTarget → publisher → EventBus → StoreLogWriter → SQLite` 证明候选/已清理/延后 `2/1/1` 与落库详情一致，并验证 `duration_ms`、非 null 空 `unsupported`。该提交的四包 race、全量 12 包 race、vet/build/diff-check 通过；当时尚未修复的 R7-06 后续已由 `b19d271` 修复。 |
| R7-03 | **P2** | ✅ 已修复并提交（`297ccfe`） | **Dry Run 已按“所有已配置目标各一项”统一。** 无适用规则目标返回非 null 空数组骨架、`coverage_ready=false`、空 `error`；不解析 DNS、不 Describe、不进入 planner、不限速等待，页面以“无适用规则”卡片明确正式同步会跳过。 | `TestDryRun_IncludesTargetsWithoutApplicableRules` 覆盖两个目标、规则只引用一个目标，断言另一目标仍返回且零 DNS/云 API；`TestDryRun_NoRulesReturnsAllConfiguredTargets` 覆盖零规则仍返回全部配置目标并保留全局 warning。正式同步 `RoundSummary.Total` 及 idle/health 口径未修改，仍只统计有适用规则目标。该提交的定向 Syncer race、前端构建、vet/build/diff-check 通过；当时暴露的 Provider 超时夹具问题后续作为 R7-07 由 `b19d271` 修复。 |
| R7-04 | **P3 / 可观测性** | ✅ 已修复并提交（`b19d271`） | **最终残留已由可信 S2 planner 直接决定。** `targetResult.cleanupResolved` 已删除；私有 `cleanupResult` 直接传递实际删除与最终 deferred。正常删除、部分删除或幂等 NotFound 后，只有成功取得 S2 且 Desired 覆盖成立时才采用 `len(plan2.CleanupCandidates)`；否则保留 S1 与 Provider 确认进度的 fallback。NotFound 始终不增加 `deleted/cleanup_deleted`，S2 Describe/覆盖失败仍为 `failed`。 | `TestCleanup_IdempotentNotFoundUsesS2FinalCandidates` 覆盖 `1/0/0`、`2/0/1`、`2/0/2` 及 S2 新增残留导致 deferred 大于 candidates；`TestCleanup_IdempotentNotFoundStillFailsWhenS2Untrusted` 覆盖 Describe/覆盖失败与错误事件；`TestStoreLogWriter_ProductionChainPersistsNotFoundS2Convergence` 证明目标事件、整轮事件和 SQLite 一致记录 `1/0/0`。既有正常删除、R7-01 重试、ECS 部分删除控制与全量门禁继续通过。 |
| R7-05 | **P3 / 测试缺口** | ✅ 已修复并提交（`d6d208e`） | **失效字符串断言已替换为结构化 JSON 合同检查。** `validateDryRunArrayJSON` 与 `requireJSONArray` 使用 `map[string]json.RawMessage` 区分字段缺失、`null`、`[]` 与错误类型；检查顶层 `results`/`warnings` 及每个目标的十个数组字段。三种真实输出均满足合同，未发现生产 `null`，因此未修改 DTO、planner、API 或前端。 | `TestDryRun_ArraysNeverNull` 覆盖有适用规则、R7-03 无适用规则骨架、零目标/零结果；`TestValidateDryRunArrayJSON_RejectsInvalidShapes` 对两个顶层字段与十个目标字段逐一注入 `null`、缺失、对象类型，并拒绝 `null` 目标项。提交中记录的定向测试、Syncer 单包 race、两包 20 轮 race、全量 race 连续 3 次及静态/前端门禁均通过；本次只读核验未重跑。 |
| R7-06 | **门禁可靠性** | ✅ 已修复并提交（`b19d271`） | `realHTTPTimeoutError` 现保存全部 accepted `net.Conn` 强引用，并在 cleanup 中按“关闭 listener → 等待 Accept goroutine → 关闭连接”回收；真实 `*url.Error`、awaiting-headers 文本与生产 `isRetryable` 均未放宽。 | `GOGC=1 go test ./syncer -run '^TestIsRetryable_RealWorldShapes$' -count=50` 与包含全部清理用例的 `-race -count=20` 通过；未修改生产重试集合。 |
| R7-07 | **门禁可靠性 / 新增** | ✅ 已修复并提交（`b19d271`） | `aliBlockingServer` 以包内 mutex/WaitGroup 保存并有界回收 accepted connections；四条真实 HTTP 构造路径、150ms 下限和生产 10s/30s 默认值均未修改。 | `GOGC=1 go test ./provider -run '^TestAliClientRequestIsBounded$' -count=20` 与同用例 `-race -count=20` 通过；未修改 Provider 或生产超时。 |

#### 12.5.1 R7-04 固定计数语义

R7-04 实施时必须同时满足以下口径，避免为了让三个数字表面相加而虚报云端写入：

- `cleanup_candidates` 继续表示 S1 看到的候选数；`deleted` 与 `cleanup_deleted` 继续只表示云端明确确认的实际删除，幂等“已不存在”不计入；
- 一旦发起过删除并成功取得 S2，`cleanup_deferred` 必须直接取 S2 planner 的实际剩余候选数；S2 已证明消失的候选属于“已解决但非本次确认删除”，不得继续 deferred，也不得虚增 deleted；
- 因此单候选 NotFound、S2 已无候选时允许形成 `candidates=1 / deleted=0 / deferred=0`；这表示候选已由 S2 证明不存在，不表示本进程实际删除了一条；
- S2 Describe 失败、快照不完整或 Desired 覆盖失败仍为 `failed`；不得因为 Delete 是幂等错误而吞掉 S2 失败；
- 未执行 S2 的清理错误仍以 S1 候选和 Provider 已确认进度计算 deferred，保持 R7-01 的重试与耗尽语义。

#### 12.5.2 R7-05 判别性测试合同

不得继续使用拼接字段名的字符串搜索。新检查器应解码为 `map[string]json.RawMessage`，并覆盖两层数组：顶层 `results`/`warnings`，以及每个结果内的 `domains`、`desired`、`satisfied_by_owned`、`satisfied_by_external`、`to_add`、`cleanup_candidates`、`cleanup_deferred`、`dns_errors`、`unsupported`、`conflicts`。每个字段必须存在、确为 JSON array 且不等于 `null`。

测试至少覆盖有适用规则目标、R7-03 无适用规则骨架、零目标/零结果三种成功形状。检查 helper 应返回 error，并用人工构造的 null JSON 负向样本证明旧断言会漏报而新断言会拒绝；不能只证明当前成功输出能通过。当前生产构造仍显式初始化非 nil slice，因此若新测试没有发现真实 null，不得借 R7-05 修改 DTO 或生产实现。

**实施结果（2026-09-30）：** `requireJSONArray`/`validateDryRunArrayJSON` 已按上述合同落地；除合同要求的代表性负向样本外，还对两个顶层字段与十个目标数组字段逐一覆盖 `null`、缺失、对象类型，并拒绝 `results` 中的 `null` 目标项。三种真实成功形状均通过且未发现生产 `null`，故生产 DTO、planner、API 与前端保持不变。

#### 12.5.3 R7-06/R7-07 共用根因与修复边界

两项都属于测试夹具生命周期错误，不是当前已确认的生产超时或重试回归。根因链固定为：`Accept` 成功 → 返回的 `net.Conn` 未保存 → 连接对象可达性丢失 → GC/finalizer 关闭底层 fd → 客户端提前收到 reset/EOF。修复只能稳定测试服务端生命周期，不得改生产重试集合、阿里云超时、Provider 调用链或断言阈值。

测试服务器必须保存所有 accepted connections；cleanup 顺序为关闭 listener、等待 Accept goroutine 退出、再关闭并清空保存的连接。R7-06 与 R7-07 可以在同一实施批次完成，但必须保留两个独立 ID 与各自的验收命令，避免将“重试分类”和“阿里云客户端有界性”混成一条证据。为两个包各保留一个小型本地 helper，不新增跨包 `internal/testutil` 抽象。

#### 12.5.4 后续推荐串行顺序与门禁

1. **R7-06 + R7-07 已完成：** 只修两个测试夹具并恢复可信门禁，未改变生产错误分类或超时。
2. **R7-04 已完成：** 以 S2 planner 校正最终残留，只改清理结果传递和判别性测试，未改 Provider 删除安全与外部 Schema。
3. **R7-05 已完成：** 失效字符串断言已替换为两层 `json.RawMessage` 检查并加入逐字段 null/缺失/错误类型负向控制；真实输出未发现 null，生产 DTO 未改。
4. **统一本地收口已完成：** 两个 GOGC 压力用例、两个包的 `-race -count=20`、`go test ./... -race -count=1` 连续 3 次、`go vet ./...`、`go build ./...`、前端 `npm run build` 与 `git diff --check` 均通过。Syncer 20 轮首次因 Go 默认 10m 总超时中止，保持次数不变并显式设置 `-timeout=20m` 后完整通过。
5. **文档闭环已完成并提交：** R7-05 与当轮 `AGENTS.md`、本文和审核报告回写已随 `d6d208e` 提交；真实云、浏览器、当前 revision 远端 CI/GHCR 未执行的边界继续保留，不得由本地稳定性门禁替代。

**实施停止条件：** 任一修复通过放宽业务断言、降低超时下限、扩大 `isRetryable`、虚增幂等删除计数、跳过 S2，或把一次全量绿色写成稳定绿色时，立即停止该批次；若 S2 最终候选口径与 §5.4/§5.5 出现新的不可调和冲突，也必须暂停并交由用户裁决。

**已复核成立、不得在修复上述条目时破坏的正向控制**

- 严格 TAG 命名空间、comment 不参与身份、canonical 五维 `FunctionalKey`、目标级聚合/去重与统一能力矩阵成立。
- F1 Lighthouse 多端口期望侧逐项展开和已存在合并规则零重建/零清理成立；F5 SWAS 100 页上限无法证明完整时返回 `ErrSnapshotIncomplete` 成立。
- 正式同步保持 `S0 → Add(S0 revision) → S1 → 同一 planner 覆盖验证 → 安全门 → Delete(S1 定位) → 必要时 S2`；Add、Describe 或覆盖验证失败时 Delete 调用恒为 0。
- Lighthouse/CVM 版本保护、SWAS/ECS 仅用 S1 稳定 ID、ECS 每批不超过 100 及部分成功计数成立；未发现全量覆盖 API 或新的误删路径。
- 正式同步与 Dry Run 共用 `provider.PlanTarget`；Dashboard/OperationalHealth 继续只消费后端 `RoundOutcome`，不得引入第二套健康推断。

**证据与外部边界**

- 本轮只读复核的定向命令包括 `go test ./internal/tag ./provider ./syncer -race -count=1`、Provider snapshot/request 用例、Syncer target/cleanup 用例、`go vet ./provider ./internal/tag` 与 `git diff --check 3ce40fe..HEAD`；核心定向门禁通过，但 F2 在包含 `syncer` 的另一轮 race 运行中再次复现。
- 2026-09-29 后续研究在 `HEAD 297ccfe`、本地 `go1.26.6 darwin/arm64`（模块合同仍为 Go 1.25）上取得两条判别证据：`GOGC=1 go test ./syncer -run '^TestIsRetryable_RealWorldShapes$' -count=50` 以 `connection reset by peer` 失败；`go test ./provider -run '^TestAliClientRequestIsBounded$' -count=50` 在 `scanAliSWAS` 约 2.37ms 提前返回。这些证据证明夹具不稳定，不等于 Go 1.25 远端 CI 已验证，也不证明生产超时回归。
- 真实四云、Lighthouse 多端口真机收敛、真实版本竞争/删除定位、真实浏览器、真实 SMTP/Webhook/Uptime Kuma、当前 revision 的远端 CI/GHCR 仍未执行；继续以 `ProdTestList.md` PT-I7-01～07、PT-B7-01～09 为准，不得用本节的源码/单测证据替代。


### 12.6 P3-02 后续独立修复（2026-10-02，方案 B）

用户先确认 B 方向，再确认仓库外候选研究结果并授权正式修复与文档回写。实施前基线为 `main == origin/main == ac0ee62`，工作树干净。该批次替代 §5.2 的无例外重新 Resolve 表述，不把熔断改动混入 Issue7 Step 0～5 的历史实施记录。该批"源码/测试/文档尚未提交"为当时记录，**现由提交 `88154cd` 替代**（2026-10-08 复核订正）。

- `runRound` 创建只存活本轮的 `dnsRound`，轮初按配置域名捕获 breaker 状态，各目标显式共享。网络解析与探测等待均不持协调锁或 breaker 锁，不增加后台协程。
- 正常域名仍每 attempt 重新解析。轮初已熔断域名只有一个调用半开探测：失败原始错误只在本轮复用，下一轮重新探测；成功立即解除，探测者使用自己的结果，等待者和后续 attempt 各自新解析。恢复后不重新启用本轮失败缓存。
- 失败计数改为每规范化域名每轮最多一次：全轮尝试过且无成功非空解析才加一；任一成功立即删除计数并阻止轮末加一，未解析不变。阈值表示连续无成功解析的轮数，不按目标数、规则数或重试数累加。原始空解析视为失败；成功结果因单条规则禁用 IPv6 而过滤为空，仍交由 planner 判定目标的 `dns_empty`，不误记为 DNS 上游失败。
- DomainKey 为 `Lower + TrimSpace`，配置发布裁剪、计数与解析去重统一身份；配置/展示原值保留，不增加尾点或 IDNA 规范化。保留成功淘汰、配置生命周期裁剪、普通变更独立复制和导入 Reset。轮末只更新捕获的旧 breaker，不合并候选发布后旧轮次晚到的更新，沿用快照隔离边界。
- 不提前跳过整目标、不复用旧 IP。既有目标重试与 Provider 版本保护保持原样；DNS 失败仍为 failed，其他域名可新增，删除必须继续通过全目标安全门。同轮某处 DNS 成功不改写失败目标或运行健康。DNS 事件仍每目标每 host 每轮最多一次，Dry Run 独立解析，不参与正式探测/计数/事件。

回归入口为 `TestDNSRound_TargetFlow`、`TestDNSRound_ConcurrentProbe`、`TestDNSRound_RealUDP`、`TestDNSRound_FailureExpiresAndLatePublishIsIsolated`、`TestDNSRound_EmptyResultDoesNotRecover` 与 `TestDNSRound_FilteredIPv6DoesNotCountFailure`，并保留版本竞争、删除安全、breaker 裁剪与 API 导入 Reset 既有回归。真实本地 UDP 证明生产 Resolver 的 A/AAAA 请求被合并；负向控制分别移除探测协调、让失败覆盖本轮成功标记，必须使对应测试变红。完整实施门禁与证据边界见 [审计报告 P3-02 实施补记](./fwalizer-audit-final1.md)。

半开失败后如果域名在本轮中途恢复，须等下一轮或下一次手动同步重新探测，这是已确认的隔离取舍。成功计数与目标运行健康独立；不新增冷却时间、TTL、长期 IP 缓存、持久化或日志/通知限流。真实云、浏览器、Docker、通知链路、Go 1.25/Linux 与当前 revision 远端 CI/GHCR 未执行，本地证据不外推为外部验收。


### 12.7 P3-07 后续独立清理（2026-10-02，方案 B）

实施前 `main == origin/main == c08f2e1`，工作树干净。用户根据引用研究聊天的结论授权再次检查并修复、同步回写文档；采用 B：删除不可达流程，按现生产语义迁移回归，复用已有判别性覆盖。A 的机械替换会保留写后快照不推进的夹具与不符合当前合同的断言，C 的 Provider Diff/GetRules 清理超出本次范围。

- 删除 `syncer/retry.go` 的 `retrySync`、`retrySyncDetailed`、`truncateDesc` 与专属 imports；保留 `maxRetries`、`isRetryable`、`isPartialDelete`、`isVersionMismatch`、`isIdempotentCreate/Delete`。不在 `_test.go` 中保留第二套同步流程。
- 三份旧测试文件删除 15 项旧入口测试及其 5 种专属 Provider 夹具、localhost 解析辅助与无消费者的夹具字段/方法。原本已走 `syncAll` 的 TAG 重试快照用例保留为 `TestSyncRound_TagSnapshotAcrossRetry`，并更正当前目标链和清理候选注释。
- 新增 `syncer/target_retry_test.go` 的 12 个目标链场景：真实超时/腾讯错误整目标重试、不可重试/耗尽、Written 与请求数区分、部分创建后失败与重试进度、unsupported 最终 attempt、幂等新增无覆盖失败、部分删除后 S2 失败的恢复/耗尽确认计数。
- 描述边界归位共享实现所在的 `provider/plan_test.go`，直接测试 `RenderDescription` / `TruncateDescription`；Lighthouse 仅更正一处描述渲染注释。完整旧→新对照见 Issue6 §7.9，正式门禁和负向控制见审计报告 P3-07 当前实施补记。
- 生产目标链仍为 `Run → syncAll → runRound → syncTarget → runTargetAttempt / runTargetCleanup`；`GetRules` 仍用于连接测试，旧 Diff/buildDesired 与 P0-01、planner 端口收敛、TAG/Provider 快照与 R7-06/R7-07 夹具继续保留。未修改目标状态机、Provider 增删、DNS、健康、API/schema、前端或 SDK。

本项只关闭 P3-07/I-09 的本地不可达代码与测试迁移问题，不新增 ProdTestList 人工要求，不改变 PT-I7/PT-B7/PT-AUDIT 的未执行/免除边界。部分删除后 S2 失败且后续 S0 耗尽时，前次残留被空 attempt 覆盖为 0 的观察独立记录、尚未定语义，本次不修复也不把 0 固定为正确值。**该观察已于 2026-10-08 复核被两个独立小组确定性复现，语义裁决登记为 [Issue8.md](./Issue8.md) I8-02 / Q-02；后续按 C 独立修复见 §12.9，不改写本项历史结论。** Go 1.26.6 / macOS arm64；该批"源码/测试/文档尚未提交"为当时记录，**现由提交 `ba82292` 替代**（2026-10-08 复核订正），未 fetch/push。未执行 Go 1.25/Linux、前端构建、产品真实二进制/浏览器、Docker、真实云/通知链路或当前 revision 远端 CI/GHCR，不外推长期稳定或外部验收。


### 12.8 P3-22 后续冷却改进（2026-10-03，方案 B）

原始逐规则重复 Describe/sleep 已由 Step 4 修复。本次研究实际验证单目标 20 条规则只读一份快照、零写入，原修复结论保留；进一步发现成功与失败分支均在末尾无条件等待，同平台单目标多等 5 秒、不同平台承受前一个平台间隔，旧测试只检查等待次数不超过一次。用户确认本聊天推荐 B 并授权正式改进与文档同步。

- 实施前 `main / 9f46529662ee48c2e5b9ee83ea1c0ff3f3ace962`，本地 `origin/main / 5e1d79c`，ahead 9，工作树/暂存区干净；前序 P3-21 已提交。唯一生产 `syncer/syncer.go`，对应 `syncer/dryrun_test.go`，AGENTS/审计/本文，共五文件；未 fetch、提交或 push。
- `dryRunNextRead` 只由既有 `dryRunMu` 保护，按 CloudType 保存时间而非快照/DNS/配置引用；读取前用剩余时间等待，完整快照调用返回后无论成功/失败均更新冷却。平台间独立且跨连续调用保留，热更新/零规则不清空，进程重启自然清空。既有 5 秒/200 毫秒间隔、结果顺序、每目标每host去重、只读 S0/同一纯 planner、数组形状、防重入/暂停独立与零事件/不改 breaker 保持。
- 正式测试强化首次单目标零等待；新增调度九场景、热更新与零规则一场景、已过期/真实剩余等待两场景，共 12 场景。20 条同域名规则夹具证明每目标每次一份快照、一次解析、零写入；等待顺序与独立写出的既定间隔分别断言。实际时钟用例只等待约 20ms，不对整个测试运行时间作上限断言。
- 正式新增/强化回归 race 20 轮、全部 Dry Run/数组回归 race 20 轮、vet/build 通过；全量 12 包 race 一轮、受影响 Go 格式与最终文档后的 diff-check 通过。旧 DryRun、遗漏等待、失败不记冷却、每次清空、缩短间隔、平台共用冷却、从读取开始计时七类正式 overlay 均按行为断言变红，无编译失败或 panic；最终门禁/格式/diff-check 见审计 P3-22 当前实施补记。

本项只改 Dry Run 快照操作的等待位置，不修改正式同步、Provider分页/增删/超时、DNS解析、API/schema、前端、依赖或暂停/恢复状态机，不增加跨平台并发、全局请求限流、取消合同或持久化。Go `1.27.1 darwin/arm64`，使用既有 ignored 前端 dist；未执行 Linux/Docker/compose、前端构建/浏览器、真实云/通知链路或远端 CI/GHCR，不变更人工清单未执行/免除状态，也不外推长期稳定或外部通过。该批"源码/测试/文档尚未提交"为当时记录，**现由提交 `333451d` 替代**（2026-10-08 复核订正）。


### 12.9 I8-02 观察表达修复（2026-10-08，用户裁决 C）

用户在引用聊天《研究 I8-02 修复方案》选定 C、追加兼容字段，并补充选择分别保留最新规划与最近完整规划；本聊天授权正式实施与文档回写。实施前 `main / 256a7b365cfa1759ae6e51dcaa2686422c9dfd00`、本地 `origin/main / 7696482243461f582342aeb78fdc2f467012f674`、ahead 1，工作树/暂存区干净。未 fetch/push。该项修正目标结果与展示，不改变 Provider 调用顺序、整目标重试、版本保护、删除安全门、目标 outcome 或 OperationalHealth。

**新增字段与兼容合同**

- 目标完成/失败事件追加 `attempts`（实际尝试次数，从 1 开始）、`cleanup_observation` 与 `unsupported_observation`。没有建立相应观察时明确为 `null`，不能把占位零解读为无残留或无限制。
- `cleanup_observation`：`attempt`、S1 候选 `candidates/candidates_at`、残留 `deferred/deferred_at`、`basis`、`desired_complete`、`historical`。`deferred_at` 是残留依据时间，估计形成时间不称云端残留观察时间。`basis=s1` 表示 S1 规划后没有发起删除；`s2` 表示取得 S2 且通过现有覆盖验证；`delete_progress` 表示已发起删除但无可信 S2，只是沿用既有 Provider 进度计算的保守估计（含估计零）。
- S0/Add/S1 早退不建立新清理观察；已有记录保留。S1 成功规划总是建立新观察，包含零值，后续覆盖验证/DNS 失败也不抹去；S2 验证成功直接采用 S2 planner 残留。候选与残留可来自不同阶段，分别记录时间。`historical` 只比较观察 attempt 与最终 `attempts`，不引入时间阈值。
- `unsupported_observation` 分别记录 `latest` 与 `last_complete`；每份含 `attempt/stage/observed_at/complete/issues/historical`，`stage` 为 `s0/s1`，`issues` 总是非 null 数组。新规划更新 latest；只有所有适用规则解析成功且非空、完整进入规划才更新 last_complete。完整空明细清空旧完整记录，不完整空明细保留前次完整记录。两份列表不求并集、不累加、不用于重判 outcome。完整性仅指输入范围，不代表覆盖或删除授权。
- `added/deleted/cleanup_deleted` 继续跨 attempt 累计确认；旧 `cleanup_candidates/cleanup_deferred` 从最近清理观察派生，无观察时为兼容占位零；旧 `unsupported` 为 latest.issues 的兼容投影。原字段类型不变，失败早退的数值语义有意修正；旧客户端忽略新增字段时仍不能识别未知。历史 SQLite 日志不回填。

**整轮、日志与 Dashboard**

`RoundSummary`、整轮事件与 `/api/sync/status.last_round` 追加值类型 `cleanup_observation_summary`：`observed_targets/estimated_targets/historical_targets/unknown_targets` 互斥且合计等于 total；历史优先于估计分类。最终 attempt 完整输入且依据为 s1/s2 的目标属 observed；最终 attempt 只有删除进度估计或输入不完整属 estimated；只有前次观察属 historical；从未建立清理观察属 unknown。`observed_candidates/observed_deferred` 仅对 observed 求和；`complete` 当且仅当全部统计目标属 observed。零目标 complete=true、outcome=idle，页面显示“无适用目标”；该布尔值不参与成功/健康判定。

SQLite 继续使用现有详情列，分行写累计确认操作、S1 候选来源与时间、S2 残留或进度估计、历史与当前未知、最新规划及前次完整明细；移除“候选 X 条：已清理 Y 条”的数量关系。Dashboard 分开累计清理与候选观察；未完整时显示已观察部分和估计/历史/未知目标数，成功但估计为零仍提示清理未确认；failed/partial 提示仍优先，暂停不提示，暂无轮记录显示“暂无记录”。状态/SSE handler 的生产逻辑、数据库 schema、配置包版本、依赖与通知固定详情合同均不改。

**实施与验证**

范围为 7 个生产/前端文件、6 个测试文件与 5 个文档，共 18 文件。仓内 `TestI802_*` 覆盖部分删除→S2失败→S0/Add/S1早退、恢复、可信新零替换、unknown/S1零/S2零/估计零/NotFound失败、latest不完整与last_complete历史/完整空替换、混合四类目标、nullable JSON、Run→EventBus→SQLite，以及真实 HTTP 状态/SSE。Dashboard 新增 8 项真实组件 setup/模板渲染回归。六类仓外 Go overlay 与两类前端负向控制守护历史保留、零更新、估计分类、完整明细、汇总/事件、旧展示与零估计提示。

门禁结果见 [Issue8.md 的 I8-02 当前实施补记](./Issue8.md#i8-02-当前实施补记2026-10-08方案-c)。真实浏览器、真实四云/通知链路、Linux/Docker/compose、原生 amd64/macOS 13 与当前 revision 远端 CI/GHCR 未执行；PT-I7-07 只补观察合同，人工未执行/免除状态不变。全仓默认 Go 门禁含既有真实 DNS 测试（I8-04），不把其包级 ok 或自行 skip 称本项真实上游验收。源码/测试/文档尚未提交。
