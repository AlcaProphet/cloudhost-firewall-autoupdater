# Issue7.md — P1-01 TAG 所有权与目标级同步实施合同

> **文档定位：** 本文是 `fwalizer-audit-final1.md` 中 P1-01 已定案后的当前实施合同，记录产品语义、与其余审计问题的关系、串行 Step 和验收边界。`AGENTS.md` 是唯一强要求；本文不改写 Build6/Build7 的已完成历史证据。
>
> **当前代码基线：** 2026-09-29，开始 Step 0 时 `main` HEAD 为 `108e5285fd88cd5a0bbc2dbecee81042091f19fa`，相对 `origin/main` ahead 2，工作树干净。Step 0 只修改文档，没有修改源码、测试、依赖或外部系统状态。
>
> **实施状态：** Step 0 已完成；Step 1～5 均未实施。除非用户后续明确授权，不得从文档阶段自动进入代码修复。

---

## 一、已定案的产品语义

### 1.1 优先级

FWAlizer 的首要职责是尽快补齐当前 IP 所需的防火墙权限。陈旧 TAG 规则的清理是尽力而为的后台收敛：只要所需功能已经重读确认存在，清理延后、歧义或删除失败不属于同步失败，不得将 operational health 变为 503。

固定方案为：

```text
目标级完整期望集
+ TAG 所有权命名空间
+ comment 纯可读
+ 先增后验
+ 平台化条件删除
+ 无法证明删除安全时保留残留
```

### 1.2 TAG 与 comment

- 所有权语法只接受 `description == "[TAG]"` 或 `description` 以 `"[TAG] "` 开头；`[TAG]foo` 不属于当前命名空间。
- comment 可空、可重复、可修改、可截断；它只给人阅读，不代表唯一性、指向性或删除授权。
- 修改 comment 不触发云规则重建或备注回写；只有新建规则使用当时的 comment。
- 非 TAG 规则若与期望功能精确等价，可被视为权限已存在，但不会被接管、修改或删除。
- 用户或其他工具主动使用完全相同的 TAG 命名空间，即表示主动交给 FWAlizer 管理。
- 修改 TAG、删除最后一条规则、删除目标、修改目标关系、reset 或 import 导致的旧 TAG 残留，均不跨命名空间自动清理。

### 1.3 功能身份与目标级规划

功能 key 固定为：

```text
address-family
+ canonical CIDR
+ canonical protocol
+ canonical port expression
+ action
```

TAG、comment、description、本地规则 ID、域名、RuleID 和 PolicyIndex 不进入 key。CIDR、ICMP/ICMPv6/ICMPV6、单端口、范围端口、Lighthouse 离散端口以及平台化 TCP+UDP 展开都必须先归一化。只比较精确功能等价，不实施“更宽 CIDR/端口范围也覆盖”的包含推理。

同一目标的全部适用规则必须先汇总再规划；每个唯一域名在该目标内只解析一次，相同功能 key 只产生一条期望规则。新建时按本地规则 ID 升序选第一条非空 comment，全空时只写 `[TAG]`，再按平台上限截断可读部分。

### 1.4 固定同步流程与清理门

```text
获取目标完整快照 S0
→ 汇总本地规则、解析并归一化期望功能集
→ 对已成功解析且平台可实施的缺失规则执行 Add
→ 重新获取快照 S1
→ 验证所需功能已存在
→ 独立评估清理安全门
→ 条件允许时删除陈旧 TAG 规则
→ 有必要时获取 S2，确认所需权限未被清理破坏
```

任何 Add 都先于 Delete。Add 失败、返回不确定、Describe 失败或快照不能证明完整时，本轮不得删除。重试粒度为整个云目标重新 Describe/Plan，最多 3 次并保留指数退避。

自动清理需要同时满足：完整期望集非空；全部适用域名解析成功且非空；没有平台能力不支持项；S1 已证明所有可实施功能存在；候选仍严格属于当前 TAG；删除定位满足平台安全条件；腾讯删除使用 S1 同版本；候选不包含任何期望 key。任一条件不满足时只记录 `cleanup_deferred`，但不阻止其他已成功解析且可实施规则的新增。

### 1.5 四平台删除策略

| 平台 | 新增 | 清理 |
|---|---|---|
| Tencent Lighthouse | 携带 S0 `FirewallVersion`；版本竞争后重新规划 | 仅功能 key 在 S1 中唯一、唯一项属于当前 TAG，且携带 S1 `FirewallVersion` 时删除；歧义时保留 |
| Tencent CVM | 携带 S0 `Version`；继续执行入站规则 100 条上限保护 | 使用同一 S1 的 `PolicyIndex + Version`，候选尽量同次删除，避免逐条导致索引漂移 |
| Aliyun SWAS | 增量创建；已有非 TAG 精确等价规则时不重复创建 | 只使用 S1 稳定 `RuleId` 精确批量删除；缺 ID 时保留 |
| Aliyun ECS | 增量创建，保留每批最多 100 条 | 只使用 S1 `SecurityGroupRuleId` 精确删除，每批最多 100 个；缺 ID 时保留 |

### 1.6 状态、健康与可观测性

| 结果 | Round outcome | Operational health |
|---|---|---|
| 所需功能已全部确认，无清理问题 | `success` | healthy |
| 所需功能已全部确认，但陈旧 TAG 清理延后或失败 | `success` + `cleanup_deferred` | healthy |
| 期望功能因平台能力限制无法实施 | `partial` | unhealthy |
| DNS、Describe、Add 或新增后覆盖验证失败 | `failed` | unhealthy |
| 没有目标或没有适用规则 | `idle` | healthy |

`cleanup_deferred` 不计入既有 `skipped`，不刷新为 `partial`，不触发运行健康异常邮件/Webhook。Dashboard、Dry Run 和实时日志应使用黄色提示。如果删除后验证发现所需功能缺失，立即转入“只补不删”修复流程并将该轮记为 `failed`。

---

## 二、与其余审计问题的关系

### 2.1 必须吸收到本项的问题

| 审计项 | 关系 | 固定处理 |
|---|---|---|
| P0-01 Aliyun 端口 key 不对称 | 目标级功能 key 的正确性前置 | Step 1 在唯一 canonical key 层覆盖 `443`↔`443/443`、`8000-8010`↔`8000/8010`；不再另做一套临时 Diff 补丁 |
| P1-01 comment 碰撞互删 | 本项根因 | 移除 description/comment 个体身份职责，改为目标级完整期望集 |
| P2-01 IPv6+ICMP key 不对称 | 功能 key 正确性前置 | Step 1 按 address family 统一 `ICMP`/`ICMPv6`/`ICMPV6` |
| P2-02 ECS 删除超过 100 不分批 | ECS 平台清理的硬限制 | Step 3 按 S1 RuleID 每批最多 100 个；部分成功必须如实计数，未删部分进 `cleanup_deferred` |
| P2-03 SWAS/ECS 平台能力静默丢弃 | 清理安全门前置 | Step 1 规划器显式产生 `unsupported`；只要存在 unsupported，就禁止该目标清理并使轮次为 `partial` |
| P2-08 Dashboard 误报 idle | 与新健康口径共用状态消费层 | Step 4 一并取消前端自行派生的第二套健康语义，`idle` 与 `cleanup_deferred` 均不报 unhealthy |
| P2-09 Dry Run 重复 key/规则不可区分 | 旧的逐规则数据形态将被取代 | Step 4 改为目标级结果，不再以 domain 作唯一 key |
| P3-22 Dry Run 逐规则 Describe + sleep | 目标级规划直接消除旧路径 | Step 4 共用正式纯规划器，每目标只取一份快照 |
| P3-25 ECS `NextToken` 不推进 | 安全清理的硬前置 | 不采用原建议的“`break` + WARN 后继续”；重复 token 必须返回 Describe 失败，该目标本轮 `failed` 且零删除，避免把不完整快照误当成完整快照 |

### 2.2 受影响但不构成前置的问题

| 审计项 | 影响/排序决定 |
|---|---|
| P3-02 熔断器语义 | 目标内域名去重会改变失败计数次数；Step 1 需要锁定“每目标每域名每轮最多一次”，但本项不扩张为真正隔离/降频熔断重构 |
| P3-06 CVM 100 条上限口径 | 影响新增是否被拒绝，不改变所有权与清理语义；真实账号确认前保留当前偏保守上限 |
| P3-07 死包装 `retrySync` | 目标级主流程落地后才重新证明零引用并删除，不在 Step 1 前单独清理 |
| P3-10 忽略 error | Step 2～4 重写路径中涉及的返回值必须当场处理；其余点仍是独立问题 |
| P3-11 历史日志吞写库错误 | Step 4 会改变目标级事件与日志内容，应在新事件定型后修复，不得再建一套逐域名状态 |
| P3-16 前端偏移 | Dry Run 页相关子项等 Step 4 一并收口；主题、扫描、按钮等子项仍独立 |
| P3-23/P3-24 ticker/reset 与 pause/resume 通知 | 与本项共享 `syncer.go`、但语义正交；核心 Step 1～4 完成后再单独修复，避免同时改写轮次状态机 |

### 2.3 与本项正交的剩余问题

- P1-02 与 P3-08：pidfile/flock 生命周期，可独立实施。
- P2-04 与 P3-03：配置发布顺序/Push 首发；Step 0 已把强合同改为先发布 `RuntimeState` 再唤醒，代码仍待独立实施。
- P2-05、P2-06、P2-07：Webhook 业务错误、告警页加载守卫、删除二次确认，不依赖 TAG 设计。
- P3-01、P3-04、P3-05、P3-09、P3-12～P3-15、P3-17～P3-21：各自独立；其中 P3-18 的 P1-01/Design5/ProdTestList 文档漂移由 Step 0 部分收口，其余漂移仍保留。

---

## 三、串行实施步骤

### Step 0｜文档合同（已完成）

范围只包含：

1. 更新 `AGENTS.md` 的 TAG、功能 key、先增后验、版本保护、健康口径和事件强合同。
2. 更新 `Design5.md` 的当前设计记录，并清除与 version 3 已实施事实直接冲突的 version 2 当前口径。
3. 更新 `fwalizer-audit-final1.md` 的 P1-01 状态，保留缺陷证据，用已定案方案取代“已冻结/待决策”。
4. 更新 `ProdTestList.md`，保留未验证边界并增加四云真实验收项。
5. 创建本文，将 Step 1～5 与其余审计问题的依赖/冲突固定下来。

完成只代表文档已定案，不代表 P1-01 已修复。

### Step 1｜纯规划器与失败先行用例（待实施）

先加入在当前代码上必须失败的判别性测试，再实现：

- 严格 TAG 所有权语法；
- 功能 key 及 P0-01/P2-01 所需全部归一化；
- 目标级期望集、域名解析去重、本地功能去重；
- 人工规则精确满足、宽泛/冲突规则警告；
- `unsupported`、`cleanup_candidates`、`cleanup_deferred` 和 `coverage_ready` 的纯数据模型；
- P3-25 的 token 不推进硬失败。

本 Step 不连接真实写入，不启用删除。停止条件：纯规划器仍依赖 comment/description 区分个体，或无法证明快照完整性。

### Step 2｜目标级先增后验主流程（待实施）

- 统计单元改为云目标，重试单元改为整个目标 Describe/Plan。
- 实现 S0 → Add → S1 → coverage verification；暂时保持全平台零自动删除。
- 同一功能已由非 TAG 规则满足时不重复创建。
- Add 失败或 S1 不能确认覆盖时该目标为 `failed`，旧规则全保留。
- 保留最多 3 次重试和指数退避，但不允许从旧快照继续写。

停止条件：任一路径仍可在新权限确认前删除旧规则，或失败重试沿用旧 PolicyIndex/RuleID/version。

### Step 3｜四平台条件清理（待实施）

按 Lighthouse → CVM → SWAS → ECS 逐平台实施，每完成一个平台就执行该平台判别性测试，不将四云抽象成丢失平台安全能力的统一删除器。

必须覆盖：Lighthouse 功能重复歧义冻结；CVM `PolicyIndex + Version` 同快照且避免索引漂移；SWAS/ECS 只用 S1 稳定 ID；ECS 150 条删除分为 100+50；删除部分失败只形成 `cleanup_deferred`，不抹掉已确认的新权限成功。

### Step 4｜Dry Run、事件、日志、Dashboard 与健康口径（待实施）

- 正式同步与 Dry Run 共用同一纯规划器，Dry Run 改为每目标一项。
- 新增 `EventTargetSyncComplete`；`EventDomainSyncComplete` 不再承载云端增删数。
- `RoundSummary` 追加 `cleanup_candidates`、`cleanup_deleted`、`cleanup_deferred`；`added/deleted` 仍只表示云端已确认写入数。
- `sync_logs` 本轮不迁移 Schema；目标级记录可使 domain 为空并在详情列出来源域名。
- 合并修复 P2-08/P2-09/P3-22 和 P3-16 的 Dry Run 相关子项；清理延后用黄色提示。

停止条件：`cleanup_deferred` 会制造 `partial`/unhealthy，Dashboard 仍自行派生与 `OperationalHealth` 冲突的结论，或 Dry Run 与正式规划不同源。

### Step 5｜完整门禁与真实云验收（待实施）

1. 执行有关包失败先行用例、race、vet、build、前端构建/audit、Docker 和 `git diff --check`。
2. 覆盖两个空 comment/不同域名不互删；同域名多协议/端口不互删；comment 修改零增删；非 TAG 精确等价满足期望且永不被修改；B 的创建确认早于任何 A 删除；B 添加失败时 A 保留；清理延后仍 success/healthy；`[TAG]foo` 不归属；空期望集不自动清空；四平台删除安全条件；Dry Run 与正式规划一致。
3. 按 `ProdTestList.md` 的 PT-I7 项分四平台真实验收。任一平台未执行都必须保留“未执行”，不得用 mock/单测替代。

---

## 四、明确排除

本轮不实施 comment 唯一性/必填校验，不把 host/协议/端口/本地规则 ID 编入 description，不新增“本地规则—云规则”持久状态表，不因 comment 修改重建规则，不在空期望集时清空整个 TAG 命名空间，不为追求四云同形而放弃 RuleID/Version 安全能力，不使用全量覆盖 API，不在 P1-01 首轮增加手工清理入口。

## 五、证据分层

- 当前 Step 0 只有文档一致性证据，不是代码通过证据。
- 原审计 overlay 探针证明 P0-01/P1-01/P2-01 存在，不证明新设计已修复。
- 本地 mock/自动测试、真实二进制、Docker、浏览器、真实腾讯/阿里云、远端 CI/GHCR 必须分层记录，不得互相代替。
