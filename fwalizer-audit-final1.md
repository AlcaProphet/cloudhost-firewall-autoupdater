# FWAlizer 全量代码审核报告（最终版）

| 项 | 内容 |
|---|---|
| 仓库 | `/Users/kylechen/Desktop/Repo/cloudhost-firewall-autoupdater` |
| 分支 / HEAD | `main` / `11918fb945fe9dfe2a86ead5bc833b14dd156a68` |
| 审计开始前工作树 | **干净**（0 tracked 改动、0 非忽略未跟踪文件）；这是历史审计快照，不表示当前工作树状态 |
| 审核性质 | 代码审核为**只读**：未修改、未创建、未删除任何**受版本控制**的仓库文件 |
| 审核方式 | 8 路并行子代理分模块审核 + 主代理亲自覆盖超时范围 + 判别性探针独立复现 + 交叉复核裁决 |
| 审核范围 | 233 个 tracked 文件；**53/53 生产 Go 文件**（10,890 行）；62 个测试文件（20,457 行）；18 个前端源文件（2,212 行）；6 个构建/部署/CI 文件；8 份合同文档 |
| 报告版本 | final1（已剔除全部被驳回/误报项，并纳入用户 7 项决策） |

> **阅读规则（2026-09-29）**：本报告同时保留“审计快照事实”“后续状态补记”和“定案前历史分析”。当前强约束以 [AGENTS.md](./AGENTS.md) 为准；P1-01 当前设计与串行实施入口以 [Issue7.md](./Issue7.md) 为准；[Design5.md](./Design5.md) 记录当前设计方向。本报告用于保存审计证据与整理剩余问题，不建立第二套实施合同。源码行号属于审计快照，后续定位应同时使用 finding ID、符号名和测试名。

---

## 0. 当前状态、执行索引与 TODOLIST

### 0.1 状态图例

| 标记 | 含义 |
|---|---|
| ✅ | 已修复且有回归证据；保留原 finding 作为正向控制，不再列入待修复项 |
| 🟠 | 产品语义或方案已定案，但代码尚未实施 |
| 🔵 | 已确认的独立问题或已有用户决策，尚无完成证据 |
| ⏳ | 必须由真实环境或人工验收确认，自动化证据不可替代 |
| 📚 | 历史审计材料或旧排序，仅用于追溯，不得作为当前执行入口 |
| 🚫 | 已驳回候选，不得据此实施 |

> finding ID 表示审计严重级别，不表示施工顺序；施工顺序只以本节 TODO 阶段和 Issue7 Step 编号表达。

### 0.2 当前问题状态索引

| 范围 | 当前状态 | 当前入口 |
|---|---|---|
| P0-01 | ✅ 已由 `108e528` 修复；原证据与回归必须保留 | Issue7 Step 1 重构 functional key 时保持绿色回归 |
| P1-01 | 🟠 Step 0 文档合同已完成，代码未修复 | Issue7 Step 1～5 串行主线 |
| P1-02 | 🔵 已决定采用 flock，尚未实施 | 独立高优先级队列；同时收口 P3-08 |
| P2-01、P2-03 | 🟠 未修复 | Issue7 Step 1 |
| P2-02 | 🟠 未修复 | Issue7 Step 3 |
| P2-08、P2-09 | 🟠 未修复 | Issue7 Step 4 |
| P2-04～P2-07 | 🔵 未修复，且不构成 P1-01 前置 | 独立问题队列 |
| P3-25 | 🟠 自动清理安全硬前置 | Issue7 Step 1；不完整快照必须失败且零删除 |
| P3-11、P3-22 与 Dry Run 相关 P3-16 | 🟠 未修复 | Issue7 Step 4 |
| 其余 P3 | 🔵/⏳ 未修复、文档清理或待真实验证 | 按 0.4 的独立队列处理 |

### 0.3 当前唯一串行主线：Issue7

- [x] **Step 0｜文档合同**：已完成；只代表定性与实施合同完成，不代表代码修复。
- [ ] **Step 1｜纯规划器与失败先行用例**：吸收 P1-01、P2-01、P2-03、P3-25，并保留 P0-01 回归。
- [ ] **Step 2｜目标级先增后验**：Add → 重读 → coverage verification；覆盖确认前零自动删除。
- [ ] **Step 3｜四平台条件清理**：吸收 P2-02；无法证明安全时保留残留并记录 `cleanup_deferred`。
- [ ] **Step 4｜Dry Run、事件、日志、Dashboard 与健康口径**：吸收 P2-08、P2-09、P3-11、P3-22 和相关 P3-16。
- [ ] **Step 5｜完整门禁与真实云验收**：自动门禁与 PT-I7 分层记录；任一真实平台未执行都不得写成通过。

**主线停止条件**：必须逐 Step 实施和验收；任一 Step 未满足 [Issue7.md](./Issue7.md) 的停止条件时，不进入下一 Step。不得把 mock、单测、本地进程或 Docker 结果外推为真实云验收。

### 0.4 独立问题队列（不与 Issue7 主线编号混用）

以下顺序是整理后的 backlog，不表示已授权实施；每次仍应只处理一个问题并保留判别性测试。

- [ ] **I-01｜P1-02 + P3-08**：以 flock 替换 PID 判活，覆盖残留 pidfile、PID 复用与并发启动。
- [ ] **I-02｜P2-04 + P3-03**：先发布 `RuntimeState`，再唤醒 Health/Push；同时收口首次 Push 失败后的重试/唤醒语义。AGENTS 目标顺序已在 Step 0 修订，当前剩余是代码与测试。
- [ ] **I-03｜P2-06**：告警页增加 loaded 守卫，防止加载失败后用默认值覆盖真实敏感配置。
- [ ] **I-04｜P2-07**：目标与规则删除增加卡片式二次确认，满足 AGENTS 强要求。
- [ ] **I-05｜P2-05**：Webhook 按渠道解析业务错误码；真实 Webhook 仍需单独验收。
- [ ] **I-06｜P3-23、P3-24**：串行修复 ticker Reset 与 pause/resume 通知合并，不和 Issue7 状态机重构混做。
- [ ] **I-07｜P3-12、P3-05、P3-14、P3-10、P3-13**：文件权限与 HTTP/error 一致性；P3-11 已纳入 Issue7 Step 4。
- [ ] **I-08｜P3-01、P3-21、P3-15**：资源与连接健壮性；P3-02 保留为已知弱语义，不在 Issue7 中扩张为隔离/降频重构。
- [ ] **I-09｜P3-07**：Issue7 Step 1～4 完成后重新证明生产零引用，再清理旧同步包装与关联测试。
- [ ] **I-10｜其余独立 P3**：P3-04、P3-06、P3-09、P3-16 非 Dry Run 子项、P3-19、P3-20，按各 finding 的前置与真实环境边界逐项处理。
- [ ] **I-11｜P3-17、P3-18**：仅做文档/注释闭环；不得与业务语义修改混在同一批次。

### 0.5 真实外部与人工验收

- [ ] **PT-B7-01～09**：仍未执行；真实 SMTP、收件箱、Webhook、Uptime Kuma、浏览器、当前 revision 的远端 CI/GHCR、SWAS Remark 上限均不得写成已通过。
- [ ] **PT-I7-01～06**：仅在 Issue7 Step 1～4 完成后执行；四云写入/删除安全、异常分页零删除与目标级 Dry Run 均需真实或清单指定证据。
- [ ] P3-06 的 CVM 配额方向、P3-19/P3-20 的真实 SMTP/MTA 表现继续保留为外部不确定性。

---

## 1. 审核结论摘要

| 项 | 结果 |
|---|---|
| **P0** | **1** |
| **P1** | **2** |
| **P2** | **9** |
| **P3** | **25** |
| **当前状态补记（2026-09-29）** | P0-01 已于 `108e528` 修复并加回归；上述 P0/P1/P2/P3 数量仍是审计当时的发现统计，不等于当前未修复数 |
| 会实际破坏云端防火墙规则的问题 | **有，已实测复现**（P0-01、P1-01、P2-01） |
| ✅ **P1-01 设计已定案** | 2026-09-29 定为“目标级完整期望集 + TAG 所有权 + comment 纯可读 + 先增后验 + 平台化条件删除 + 可接受残留”；Step 0 文档合同已完成，代码 Step 1～5 未实施（详见 [Issue7.md](./Issue7.md) 与第 8 节） |
| ⏳ **待真机验收** | **1 项：SWAS `Remark` 实际长度上限**（登记为 `ProdTestList.md` **PT-B7-09**） |
| Goroutine / 连接 / 订阅泄漏 | **未发现** |
| 无界内存 | 仅 DNS 熔断器域名 map（P3-01；增长受"曾用域名数"约束） |
| 核心同步静默停止 | **未发现** |
| 明确凭据泄漏 | **未发现**（唯一残余是 P3-12 同机文件权限与 P3-19 的 SMTP 诊断文本回显） |
| 整体质量判断 | 架构与并发设计**优秀**（事务、快照、凭据、生命周期、SSE 五条主线干净，正向控制密度很高）；缺陷集中在**规则身份与端口比较层**（会造成持续删改云端规则）与**清理收尾** |

### 一句话结论

审计当时确认两条会每轮重复删改生产防火墙规则的路径：P0-01 已于 `108e528` 修复；P1-01 仍会让同一目标上除最后一条规则外的放行被持续拆除，其设计已定案但代码未实施。当前最高优先级是 Issue7 Step 1～5。

> ⚠️ **可立即执行与待决的区分**：
> - **P0-01（阿里云端口 key 不对称）已修复**：提交 `108e528`，回归 `TestDiff_AliyunPortRoundTripConverges`；Issue7 Step 1 只需在新规划器中保持该绿色回归。
> - **P1-01 设计已定案但代码未修复**。实施必须按 [Issue7.md](./Issue7.md) Step 1～5 串行进行；在修复完成前，仍建议避免同一目标上出现 comment 相同的规则（尤其都留空）。
> - **P1-02（容器 pidfile 崩溃循环）可立即修复**，方案已定（flock）。

### 本次审核中被驳回的候选（**请勿据其动手**）

| 候选 | 裁决与依据 |
|---|---|
| "`slog.TextHandler.Handle` 按 level 二次过滤 → WebUI 日志丢行" | **驳回**。Go 源码 `log/slog/handler.go` 中 `TextHandler.Handle` **不含任何 level 判断**（过滤只在 `commonHandler.enabled`）；且 `MultiHandler.Handle`（`app/logutil.go:30`）已按 `h.Enabled(ctx, r.Level)` 逐子 handler 正确门控。不存在双重过滤 |
| "发布顺序与 AGENTS.md 相反" | **驳回（但引出真实缺陷）**。这是审计快照中的历史结论：当时 AGENTS.md:199 原文为"…监督器唤醒 → Uptime Kuma Push 唤醒 → `RuntimeState`"，与当时代码**顺序一致**；该分派误读了文档。真正的缺陷不是"不一致"，而是该顺序本身会引发 P2-04。2026-09-29 Step 0 已把当前强合同修订为先发布 `RuntimeState` 再唤醒，代码仍待独立实施 |
| `SaveAlertEmailTx` / `SaveAlertWebhookTx` / `NormalizeResourceID` / `syncer/ratelimit.go` 无引用（"死代码"） | **驳回**。分别在 `config/store.go:1297`、`:1300`、`config/validate.go:84`、`syncer/syncer.go:813` 有**生产调用**。纯标识符 grep 对"仅包内自用"与"经 HTTP 路由驱动"的符号会产生假阳性 |
| "SPA 深链 404（缺少 history fallback）" | **驳回**。`webui/frontend/src/main.ts:6` 使用 `createWebHashHistory()`，深链与刷新经 URL hash 正常工作 |

### 上一版报告的两处自我更正

1. 我曾给出"P0 = 0"的结论，**该结论错误**——第八路子代理（同步/DNS/Provider，超时后返回）发现了 P0-01，我已用自写探针独立复现。
2. 我曾把"容器陈旧 pidfile 崩溃循环"列为待验证的 **P1**，第一次用 `--volumes-from` 的测试**未能复现**（该测试方法有缺陷，卷未被真正复用）；改用持久命名卷受控重测后**确认成立**，故保留为 P1。

---

## 2. 确认发现

### P0-01｜阿里云 SWAS/ECS 端口比较 key 不对称 → 每轮"删除并重建全部 TCP/UDP 规则"（✅ 已修复）

**审计当时最严重的缺陷，已由主代理独立复现；2026-09-29 已于 `108e528` 修复。**

#### 机制（三段确定性推导，与云端回传形态无关）

1. **期望侧**：`buildDesired`（`provider/common.go:207`）用 `p.ConvertPorts` 生成端口。SWAS（`provider/ali_swas.go:185-192`）与 ECS（`provider/ali_ecs.go:193-200`）对每个端口调用 `portconv.ToSlash`，产出斜杠形态：`"443"→"443/443"`、`"8000-8010"→"8000/8010"`（`internal/portconv/portconv.go:26-35`）。
2. **现有侧**：`GetRules` 把云端端口经 `normalizeSWASPort`（`provider/ali_swas.go:87,196-207`）/`normalizeECSPort`（`provider/ali_ecs.go:87,204-216`）归一化，**凡含 `/` 必被剥离**：`"443/443"→"443"`、`"8000/8010"→"8000-8010"`。
3. **比较层不对称**：`normalizePortForCompare`（`provider/common.go:95-104`）**只**为 `ICMP/ICMPV6` 与 `-1/-1` 做等价归一，其余仅 `ToUpper`。因此期望 key 恒为 `"443/443"`，现有 key 恒为 `"443"`，**永不相等**。

代码注释本身承认了这个不对称（`provider/common.go:93`）：

> 「desired 侧为云厂商格式、existing 侧为归一化格式，**避免 ICMP 规则永不收敛**」

即：作者当年**只为 ICMP 打了补丁，漏掉了斜杠格式本身**。

#### 复现证据（主代理自写探针，经 `go test -overlay` 虚拟映射，仓库零写入）

```
portconv.Parse("443")                     = ["443"]
SWAS.ConvertPorts("443")                  = ["443/443"]      ← 期望侧 Port
normalizeSWASPort("443/443")              = "443"            ← 现有侧 Port
normalizeSWASPort("8000/8010")            = "8000-8010"
normalizePortForCompare("TCP","443/443")  = "443/443"        ← 未归一
normalizePortForCompare("TCP","443")      = "443"
>>> ToAdd=1 ToDelete=1   （期望 0/0 —— 云端规则本已正确）
FAIL
```

范围端口 `8000/8010` 同样为 **1/1**；ICMP 为 **0/0**（这正是"ICMP 被单独打补丁"的直接反证）。

#### 影响面（精确）

**仅 `ali_swas` 与 `ali_ecs`。** Lighthouse（`tc_lighthouse.go:196-206`）与 CVM（`tc_cvm.go:211-213`）的 `ConvertPorts` 不使用 `ToSlash`，key 可收敛。端口为 `ALL` 的 TCP/UDP 规则因两侧都映射为 `ALL` 而收敛。

#### 实际影响

稳态下 `to_add` 与 `to_delete` 包含**同一条规则**（内容相同），因此**不是净规则丢失**，而是：

1. **每轮删除 → 重建**：删除与重建之间存在规则**不存在的窗口**。若重建失败（配额 `FirewallRuleLimitExceed`、限流、网络中断、进程在窗口内被 SIGKILL），该轮结束时规则处于**已删除**状态 → 端口不可达。
2. **写配额翻倍 + 每轮空转**：SWAS 限速 100 次/60 秒，ECS 无明确限制但仍有往返延迟。
3. **可观测性完全失效**：`added`/`deleted` 永久非零，`outcome` 仍为 `success`（`failed==0 && skipped==0`）→ 运行健康**永不告警**，Push 不上报 DOWN，Dry Run 永远显示"全量替换"，用户无法从中发现异常。
4. **真实丢失场景（与 IP 变更叠加）**：IP 集收缩时，旧 IP 规则在 `to_delete`、新 IP 在 `to_add`；执行顺序是**先删后建**（`syncer/retry.go:72-116`），若 `CreateRules` 失败则旧 IP 已删、新 IP 未加 → 该域名的放行被完全拆除（此时 `failed` 会计数并告警）。

#### 为什么现有测试未发现

Diff 用例的 `existing` 传 `nil` 或手写统一格式 fixture，**没有任何 `GetRules→Diff` 往返用例**；`provider/request_mock_test.go` 的 SWAS/ECS 用例只单测 `CreateRules`/`DeleteRules`/`ConvertPorts`，不入 Diff。

#### 最小判别性测试

mock 让 `ListFirewallRules` 回传 `Port="443"`（以及 `"8000/8010"`），断言第二轮 `ToAdd==0 && ToDelete==0`；当前为 1/1。

#### 推荐整改

在**唯一比较归一化点** `normalizePortForCompare` 中把斜杠形态与短横形态统一，`CreateRules` 的线格式保持不变：

```go
// provider/common.go —— normalizePortForCompare
// 追加：斜杠形态归一为与现有侧一致的形态
if strings.Contains(port, "/") {
    parts := strings.SplitN(port, "/", 2)
    if parts[0] == parts[1] {
        return strings.ToUpper(parts[0])                  // "443/443" → "443"
    }
    return strings.ToUpper(parts[0] + "-" + parts[1])     // "8000/8010" → "8000-8010"
}
```

- **是否允许破坏性改动**：否（纯内部比较逻辑，不影响外部契约）
- **是否与 AGENTS.md 冲突**：**是**。实际行为违反 §三「只用**增量**添加 + 精确删除」的意图（实为每轮全量删+建）；不违反"不得使用全量覆盖 API"的字面要求
- **需用户决策**：无

#### 实施补记（2026-09-29）

- 提交：`108e5285fd88cd5a0bbc2dbecee81042091f19fa`。
- 实现：`normalizePortForCompare` 补齐斜杠形态归一化，`443/443 → 443`、`8000/8010 → 8000-8010`，线上 Create 格式不变。
- 回归：`TestDiff_AliyunPortRoundTripConverges` 覆盖 SWAS/ECS 单端口和范围端口往返收敛。
- Issue7 Step 1 会替换更大范围的 functional key/规划路径，因此该回归是必保留的正向控制，不是待重做的红灯修复。

---

### P1-01｜规则身份仅由 `desc` 决定，comment 可为空 → 同目标两条规则**互相删除**并逐轮震荡

**已由主代理独立复现。**

> ✅ **状态：设计已定案（2026-09-29），代码未实施。**
>
> 缺陷本身已确认并复现。最终设计不在 description 中编码 host/协议/端口，不强制 comment 唯一或必填，不新增本地—云端映射表，TAG 上限仍为 48。
>
> **定案要点**：TAG 是唯一操作授权，comment 只用于可读；功能身份改为归一化的地址族/CIDR/协议/端口/action；同一目标先汇总完整期望集，再先 Add、重读验证、最后经安全门条件 Delete。详见第 8 节与 [Issue7.md](./Issue7.md)。

#### 机制

`Diff` 只按 `r.Description == desc` 判定归属（`provider/common.go:142-148`），不在 `desiredKeys` 内即 `toDelete`（`provider/common.go:175-182`）。而

```go
desc = truncateDesc(tag.Format(tagStr, rule.Comment), p.CloudType())   // syncer/retry.go:56
```

`tag.Format(tag, "")` 返回 **`"[TAG]"`**（`internal/tag/tag.go:12-14`）。`comment` **允许为空**（`config/validate.go:97`，`webui/api/rules.go` 原样透传），因此两条都留空 comment 的规则 **desc 完全相同**。

#### 复现证据

```
规则A（comment 空，a.example.com）首轮: ToAdd=1
规则B（comment 空，b.example.com）对 A 的既有规则:
    将被删除: cidr=1.1.1.1/32 desc="[auto-dns]"
>>> ToDelete=1   （B 的同步把 A 的规则列入待删除）
FAIL
```

#### 实际影响

同一目标上两条规则（comment 相同、解析 IP 不同）会**互相删除对方**：每轮后处理的规则删掉先处理规则的规则，只保留自己的 IP。稳定状态是"**只有最后一条规则的 IP 被放行，其余域名的放行被持续拆除**"，确定性不可达；每轮 `added`/`deleted` 非零但 `failed==0` → **运行健康不告警**。

**加重因素**：`truncateDesc` 把 Lighthouse 截断到 64 rune、SWAS 截断到 50 rune（`syncer/retry.go:195-209`）→ **长 comment 也会碰撞**（前 64/50 字符相同即可）。

**边界（正确的部分）**：只删除本工具自己的规则，**不触碰非本工具规则**。

#### 为什么现有测试未发现

所有 Diff/同步用例的描述**互不相同**——`TestDiff_DomainIsolation`（`provider/common_test.go:227`）恰恰是用"不同描述"来断言"不删其他域名"，**测试假设与生产默认值方向相反**；`TestRetrySync_EmptyCommentDesc` 只有单条规则。

#### 决策状态：已定案，此前候选均不再是待选实施方案

| 子项 | 状态 | 说明 |
|---|---|---|
| **身份形态** | ✅ **已定案** | description 只承担 TAG 所有权 + comment 可读；个体身份被目标级功能 key/完整期望集取代 |
| **唯一性校验** | ✅ **不引入** | comment 可空、重复和修改；不强制唯一 |
| **长度超限行为** | ✅ **只截断可读部分** | 长度不再影响功能身份；TAG 上限仍为 48 |
| **旧规则迁移** | ✅ **仍然有效** | 用户已确认**无历史兼容负担**，因此任何最终方案都**不需要** orphan 清理入口 / 迁移脚本 / 旧格式 WARN。此结论与身份形态无关，独立成立 |
| **TAG 上限收紧（48→32）** | 🔶 **已撤回** | 该收紧**仅为配合方案 A** 而提出。`AGENTS.md:162` 未修改，`maxTagRunes` 仍为 **48**（`config/validate.go:34`） |

**Issue7 Step 0 已完成；未获用户后续授权时不得进入 Step 1 代码实施。** TAG 上限 48 与 Lighthouse/SWAS 描述容量仍会挤压 comment 的可读长度，但不再影响功能身份或是否能实施 P1-01。

**跨方案共通的前提**（无论最终选哪个方案都成立）：

- `hasPrefix`/`Parse` 以 `[TAG]` 前缀识别归属的机制**不能取消**（AGENTS §三 要求），因此 `[TAG] ` 前缀的容量开销是固定成本。
- 云端描述字段的上限：Lighthouse **64**（`TencentLighthouseAPIGuide/添加防火墙规则.md:15`）、SWAS **50**（来源存疑，见下）、ECS **512**（`AliyunECSAPIGuide/AuthorizeSecurityGroup.md:128`）、CVM 未声明上限。
- ⚠️ **不确定性**：SWAS 的 `Remark ≤ 50` 仅见于代码注释（`syncer/retry.go:200`），**仓内 API 文档查无出处**（已在 6 个提及 `Remark` 的文件中核查），需真实云确认（含字符/字节口径）。已登记为 **ProdTestList.md PT-B7-09**。
- 🔶 **与 TAG 上限的关系**：现行 `maxTagRunes = 48` 与上述描述字段上限之间**已经**存在紧张关系（TAG 用满 48 时 `[TAG] ` 占 51 runes，仅剩约 13 runes）。这属于同一字段的容量约束，研究本项时应一并考虑；但**本项修复不以收紧 TAG 为前提**，此前提出的 48→32 收紧**已随方案 A 一并撤回**。

---

### P1-02｜容器陈旧 pidfile 导致无限崩溃循环（Docker 部署专有）

**已由主代理受控实测确认。**

#### 机制

容器内应用恒为 **PID 1**（`build/Dockerfile:34` `ENTRYPOINT ["fwalizer"]`，无 `init`）；pidfile 内容正是 `os.Getpid()` = `"1\n"`。被 SIGKILL / OOM-kill 时 `defer cleanup()`（`run.go:76`）**不执行** → 持久卷内残留 `"1\n"`。

新容器**同样是 PID 1**，`processExists(1)`（`config/pidfile_unix.go:10-15`，`proc.Signal(syscall.Signal(0))`）对自身**必然成功** → `run.go:73-74` 打印「FWAlizer 已在运行 (PID: 1)」并 **exit 1**。

`docker-compose.yml.example:57` 为 `restart: unless-stopped`（**exit 1 属于会重启的情形**），卷在 `:39`（`fwalizer_data:/app/data`）持久化 → **无限崩溃循环，服务永久不可用，且不自愈**。

#### 实测证据（受控：持久命名卷 + 预置陈旧 pidfile）

```
$ printf "1\n" > /app/data/fwalizer.pid      # 卷内陈旧 pidfile
$ docker run -d -v fwa-vol:/app/data fwalizer:audit
state=exited exit=1
logs: FWAlizer 已在运行 (PID: 1)，请先停止现有实例
```

> 方法学说明：我第一次用 `--volumes-from` 的测试**未能复现**（该方式下卷未被真正复用），据此曾误判为不成立；改用持久命名卷后**稳定复现**。此处如实记录该更正。

#### 为什么宿主测试测不出

宿主进程 PID 不重复，`main_test.go` 的顺序启动用例天然无法构造该场景。

#### 用户决策：改用 flock 文件锁替代 PID 判活

**推荐实现要点**：

- `config/pidfile.go`：用 `os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)` + `syscall.Flock(fd, LOCK_EX|LOCK_NB)`（`golang.org/x/sys/unix` 或 `syscall`）。
  - 获取成功 → 截断并写入当前 PID（**仅供诊断，不再用于判活**），返回持有 `*os.File` 的 cleanup。
  - 获取失败（`EWOULDBLOCK`）→ 读取文件内 PID 用于提示文案，返回错误。
- cleanup **只需 `Close()`**（关闭 fd 即释放锁）；是否 `os.Remove` 均可——**推荐不删除**，这样即使两个进程交错启动，第二个进程也永远无法拿到已被占用的锁（删除会重新打开 TOCTOU 窗口）。
- `config/pidfile_unix.go` 的 `processExists` 可随之删除（或保留给诊断文案）；build tag `linux || darwin` 不变（`flock` 在两者均可用）。
- **依赖调用方持有返回值**：`run.go:76` 现有的 `defer cleanup()` 已满足。
- **收益**：同时修掉 P3-08 的两类失效（TOCTOU 漏判与 PID 复用误判）——它们在 flock 方案下**自动消失**。
- **注意事项**：若数据目录位于 NFS，`flock` 语义可能不可靠（**当前部署为本地卷，不构成问题**，但建议在文档中提示）。
- **是否与 AGENTS.md 冲突**：否（§十一 只要求"通过 pidfile 防多实例"，未规定判活机制）

---

### P2-01｜IPv6+ICMP 规则 key 不对称 → 同款每轮删+建（Lighthouse / CVM）

**已由主代理独立复现。**

- **机制**：期望 key 用配置值 `ICMP`（协议改写**只发生在 `CreateRules`**：`tc_lighthouse.go:117-122` 改 `ICMPv6`、`tc_cvm.go:126-131` 改 `ICMPV6`），而 `GetRules` 原样回传云端协议（`tc_lighthouse.go:79-89`、`tc_cvm.go:90`）→ `keyOf`/`keyOfAction` 的 `ToUpper(Protocol)` 不相等。
- **复现证据**：

  | 平台 | 云端回传 Protocol | 结果 |
  |---|---|---|
  | Lighthouse | `ICMPv6` | `ToAdd=1 ToDelete=1` |
  | CVM | `ICMPV6` | `ToAdd=1 ToDelete=1` |
  | 对照：IPv4 ICMP / IPv6 TCP | — | `0/0` |

- **影响**：与 P0-01 同机制（每轮删+重建），但作用面窄（仅 ICMP + IPv6 + 域名有 AAAA 记录）。AGENTS §三 明确 IPv6+ICMP 走 ICMPv6，因此这是**文档要求的能力**而非边缘用法。
- **推荐整改**：在 `normalizePortForCompare`（或协议归一函数）中，当存在 IPv6 CIDR 时把 `ICMP`/`ICMPv6`/`ICMPV6` 归一到同一别名；线格式不变。
- **判别性测试**：`existing` 传 `Protocol="ICMPv6"`（Lighthouse）/`"ICMPV6"`（CVM）+ `Ipv6CidrBlock`，断言 `ToAdd==0 && ToDelete==0`。
- **是否与 AGENTS.md 冲突**：否

---

### P2-02｜ECS 删除不分批：>100 条 RuleID 塞进单个 `RevokeSecurityGroup`

- **证据**：`provider/ali_ecs.go:163-190` 一次性把全部 `RuleID` 传给 `RevokeSecurityGroup`；而 **create 是分批的**（`:111-126` 使用 `batchRules(rules, 100)`）。`PlatformAPIDocs/AliyunECSAPIGuide/RevokeSecurityGroup.md` 明确 `SecurityGroupRuleId` **数组长度 0~100**。
- **触发条件**：单个 (provider, 规则) 单元待删规则 >100——例如域名 IP 集大幅收缩（TCP+UDP 拆分后 50+ 个 IP 即达 100），或修改端口列表（60 个端口 × TCP+UDP = 120 条）。
- **实际影响**：删除请求被云端拒绝 → 该单元每轮 `failed` → 整轮 failed → **运行健康持续 unhealthy 并反复告警**；同时**旧规则（旧 IP 的放行）永久残留在安全组里，安全面持续扩大**，需人工清理。
- **推荐整改**：`DeleteRules` 复用 `batchRules(rules, 100)` 逐批提交，并把已确认批次累加进 `PartialDeleteError.Deleted`。
- **判别性测试**：mock 增加"`SecurityGroupRuleId` 数量 >100 则返回 400"的约束，用 150 条断言产生 2 个请求。
- **是否与 AGENTS.md 冲突**：否（§十一 要求遵守 `PlatformAPIDocs/` 的参数限制）

---

### P2-03｜云端能力限制被静默丢弃（SWAS/ECS IPv6、ECS ICMPv6）

- **证据**：`provider/common.go:215-223` 两处裸 `continue`——`ip.IsIPv6 && !supportsIPv6(...)` 与 ECS+ICMP+IPv6——**无日志、无 skipped**；`unsupportedReason`（`:192-197`）**只**覆盖 SWAS DROP；仅 ECS+ICMP 有一条 WARN（`syncer/syncer.go:895-903`），但同样不计 skipped。
- **实际影响**：用户以为 IPv6 放行已生效，实际从未创建；界面、日志、统计、运行健康**全无痕迹**（SWAS 连 WARN 都没有）。与 Issue6 A11「云端能力限制必须如实列为 skipped」的口径不一致（SWAS DROP 已按此改造，这两类漏改）。
- **用户决策：先只补 WARN 日志 + Dry Run 展示，不计入 skipped**

  | 子项 | 内容 |
  |---|---|
  | WARN | SWAS/ECS 跳过 IPv6 地址、ECS 跳过 ICMPv6 时输出与既有 ECS-ICMPv6 WARN 同级的 `slog.Warn`（含域名、协议、原因） |
  | Dry Run | 在目标单元的展示层体现（不改变 `RoundSummary` 语义、不影响 `outcome`） |
  | 不计 skipped | 避免因永久性能力缺失把整轮判为 `partial` → 健康持续 unhealthy → 反复告警 |

- **实现选择（建议采用前者）**：
  - 优先：给 `DryRunResult` 追加**独立字段**（如 `CapabilitySkips []RuleChange`），语义清晰、不影响整轮判定；
  - 次选：仅补 WARN，Dry Run 不动（改动最小）。
- **是否与 AGENTS.md 冲突**：否

---

### P2-04｜Push 心跳"唤醒"早于 `RuntimeState` 发布 → 启用后首条心跳可被无限期抑制

- **证据**：`webui/api/deps.go` 中 `Health.Wake()`（`:150`）与 `Push.Wake()`（`:155`）均早于 `ApplyState`（`:161`）。`internal/health/push.go:112` 循环顶部第一件事就是读 `p.config()` → `runtimeManager.Snapshot()`；而 **`:114-123` 的"未启用/URL 为空"分支只有 `<-p.wake` 与 `<-p.stop`，没有任何定时器**。
- **触发条件**：`uptime_kuma_push` 由关闭 → 启用并保存；Pusher goroutine 在 `ApplyState` 落指针**之前**完成 `Snapshot()` 读取（窗口 = 两次加解锁 + 指针写入，会被任何 `Snapshot()`/`Status()` 读者放大）。
- **实际影响**：读到旧（禁用）配置 → 进入纯等待分支；而 `Wake()` **只在配置保存时**产生 → **首条心跳被推迟到下一次任意配置保存或进程重启**。期间 Uptime Kuma 判定 DOWN 并发出**误导性 Push DOWN 告警**（正是 Build7 Step 7 刚修复过的同类缺陷）。成功日志只在 DEBUG（`push.go:257`），**完全静默**。
- **用户决策：改代码 —— 把 `ApplyState` 提到两次 `Wake()` 之前，并同步修订 AGENTS.md:199**

  | 动作 | 内容 |
  |---|---|
  | 代码 | `webui/api/deps.go` 的 `applyCandidate`：顺序改为「日志级别 → 告警集合 → **`RuntimeState`** → 监督器唤醒 → Push 唤醒」 |
  | 文档 | AGENTS.md:199 括号内顺序同步改为「…→ 无失败发布：日志级别 → 告警集合 → `RuntimeState` → 运行健康监督器唤醒 → Uptime Kuma Push 唤醒」；`webui/api/coordinator.go:50-53` 的注释同步 |
  | 依据 | 先发布后唤醒可保证接收方读到的必然是**新**配置；原顺序会让被唤醒者读到**旧**配置，语义上不可能正确 |
  | 注意 | `Syncer.ApplyState` → `RuntimeManager.Apply` 是纯内存指针替换（无失败出口），提前它不破坏"commit 后不重新读库、不访问网络"的约束 |

- **是否与 AGENTS.md 冲突**：**是**（需按上表同时修订强要求文档，已获用户授权）
- **判别性测试**：用一个"同步探针 waker"——其 `Wake()` 立即执行 Pusher 会执行的那次 `runtimeManager.Snapshot()` 并记录 `uptime_kuma_push.Enabled/URL`，然后调用 `applyCandidate` 保存启用 Push 的新配置，断言探针看到的是**新**配置。当前必然失败（读锁顺序确定）

---

### P2-05｜Webhook "成功"仅看 HTTP 状态码，忽略响应体业务错误码 → 告警静默失效

- **证据**：`notifier/webhook.go:126-128` 仅判断 `resp.StatusCode >= 300`；`:124` 只 `Close()`，**从不读响应体**。测试 `notifier/webhook_content_test.go:26-29` 的假 transport 固定返回 200 + `{}`，**无 errcode 用例**。
- **外部依据**：钉钉自定义机器人在关键词不匹配 / 被限流 / 机器人停用时返回 **HTTP 200 + `errcode != 0`**（[钉钉错误码文档](https://help.dingtalk.io/zh/open/development/server-api-error-codes-1)、[同类症状报告](https://developer.aliyun.com/ask/641555)）。本项目正文首行固定为 `[FWAlizer] …`，按官方建议配置"自定义关键词"而非命中正文即落入该分支。
- **实际影响**：`OnEvent` 返回 nil，**无任何日志**。用户以为 Webhook 告警正常，实际一条未送达。Uptime Kuma Push 有 `{"ok":true}` 严格口径，普通 Webhook 渠道**没有对应口径**，形成能力落差。
- **推荐整改**：按渠道解析 `errcode`（钉钉）/`code`（飞书）/`ok`（Slack），非成功时返回**不含 URL** 的安全错误（只保留渠道名 + 业务码）。
- **判别性测试**：httptest 返回 200 + `{"errcode":310000,"errmsg":"keywords not in content"}`，断言 `OnEvent` 返回 error（当前返回 nil）
- **是否与 AGENTS.md 冲突**：否（§9.1 只对 Push 规定口径）

---

### P2-06｜告警页加载失败后仍可保存，用初始默认值覆盖全部真实告警配置并提示"保存成功"

- **证据**：`webui/frontend/src/views/Alerts.vue:19-48` 四个 ref 初值即"全部关闭 + 空 URL + 合法默认 `subject`/`body`/`port`/`interval`"；`:97-107` `load()` 失败仅弹 `message.error`；`:111-130` `save()` **无 loaded 守卫**；`:257` 保存按钮恒可用。后端 `webui/api/alerts.go:224-284` 为**覆盖式**写四张单行表，且 `config/validate.go` 中 `enabled=false` 时 host/URL 允许为空 → 该 PUT **必然通过校验**。
- **触发条件**：`GET /api/alerts` 返回非 2xx（SQLite 读错误、反向代理瞬时故障）后用户点"保存配置"。
- **实际影响**：SMTP 主机/用户名/**密码**/收发件人、Webhook URL、Uptime Kuma Push URL（含 token）、三个触发开关、`health_timeout` **全部被默认值覆盖**，且 UI 明确报"保存成功"——用户不会察觉。
- **推荐整改**：加 `loaded` 标志，`load` 失败时禁用保存按钮（或 `save()` 首行 `if (!loaded) return`）。
- **是否与 AGENTS.md 冲突**：否

---

### P2-07｜目标与规则的行内删除**零二次确认**（违反 AGENTS §11）

- **证据**：`webui/frontend/src/views/Targets.vue:165` `onClick: () => deleteTarget(row)`、`Rules.vue:127` `onClick: () => deleteRule(row)`——**直接绑定 DELETE**；全前端 `grep -rn "Popconfirm|useDialog|dialog\."` **零命中**。对照 `Logs.vue`（5 个 `NModal`）与 `Settings.vue`（9 个）均已合规。
- **违反条款**：AGENTS.md:202「所有二次确认使用 `NModal preset="card"` 卡片式弹窗（危险操作确认按钮 `type="error"`）」
- **实际影响**：一次误触即不可恢复删除。**规则删除无任何 409 兜底**（`webui/api/rules.go:124` 永远成功）；目标删除仅在"被规则引用"时有 409 保护。按钮与"编辑"相邻且同为 `tiny`。这是**管理云防火墙规则的工具**，误删直接影响生产可达性。
- **用户决策：补卡片式二次确认，满足 AGENTS §11**（复用 `Settings.vue`/`Logs.vue` 既有模式，确认按钮 `type="error"`；**不改强要求文档**）
- **是否与 AGENTS.md 冲突**：是（补弹窗即可满足）

---

### P2-08｜仪表盘把 `idle` 轮次误报为"最近一轮未完整成功"

- **证据**：`webui/frontend/src/views/Dashboard.vue:48-70`——`failed`/`partial` 提前 return，其余落到 `:63-65` 的 `lastSync !== lastSuccess` 比较。后端 `syncer/syncer.go:751-755` **每轮**都更新 `lastSync`，**仅 `RoundSuccess`** 更新 `lastSuccess`；`outcomeOf`（`:776-788`）中 `Total==0 → idle`（≠success）。
- **决定性证据**：`webui/sync_status_test.go:161` 的 `assertLastSuccessPreserved` **断言 idle 后 `last_success` 被保留**——证明"`last_sync` 前进而 `last_success` 不动"是**有意的后端契约**，不是缺陷。因此 `:63-65` 分支**唯一可达输入就是 `idle`**，净效果即误报。
- **违反语义**：AGENTS.md:151「`idle` 与空目标/空规则视为正常」
- **实际影响**：仪表盘显示黄色警告"最近一轮同步未取得完整成功"，而 `/api/health/operational` 同时返回 **200** → 前端把健康显示为异常，引导用户排查不存在的问题。
- **推荐整改**：`healthHint` 对 `outcome === 'idle'` 显式返回 null。
- **判别性测试**：跑出一轮 success → 删除全部规则 → 等下一轮 → 出现该警告，同时 `curl /api/health/operational` 返回 200
- **是否与 AGENTS.md 冲突**：否（属补齐 §9.1 语义）

---

### P2-09｜模拟测试结果以 `domain` 作 `v-for` key，同域名多规则时 key 重复且两张表无法区分

- **证据**：`webui/frontend/src/components/DryRunResults.vue:106` `v-for="item in items" :key="item.domain"`；后端 `syncer/syncer.go:604-638` **每条规则**产出一个 `DryRunResult{Provider: p.Name(), Domain: rule.Host}`；`rules` 表无 host 唯一约束（`config/store.go:147-155`），`NormalizeRule` 不做 host 去重。
- **触发条件**：同一域名 ≥2 条规则且适用于同一目标（如 `api.example.com` 的 TCP 443 + UDP 443——这是 TCP+UDP 拆分的自然结果）。
- **实际影响**：同父节点下 key 重复（Vue 要求 key 唯一，dev 报警、生产可能 patch 到错误节点复用旧行）；且两张表用同一个 `<h4>{{item.domain}}</h4>`，**用户无法判断哪张对应哪条规则**——而该页是开启同步前**唯一的变更预览门禁**。
- **推荐整改**：key 改为 `${item.domain}-${index}`，标题加入 protocol/ports（如 `api.example.com · TCP/443`）。
- **是否与 AGENTS.md 冲突**：否

---

### P3 清单（25 项，确认但低风险）

| ID | 结论 | 关键证据 | 备注 |
|---|---|---|---|
| P3-01 | **DNS 熔断器 `failCount` 永不淘汰**（唯一无界结构）+ 每次配置变更全量 `Clone()` | `dns/circuitbreaker.go:11` map；**全文无 `delete(`**；`:41-50` Clone 全量复制；`syncer/state.go:78` 每次 `BreakerPreserve` 都 Clone | 按"曾用域名数"增长；域名变更后旧条目永久残留。**用户决策：加淘汰策略**。建议在配置发布时按新配置的域名集合裁剪，或 Clone 时丢弃计数为 0 的条目 |
| P3-02 | 熔断器 `IsOpen` **不改变任何控制流**，仅影响日志分支 | `syncer/syncer.go:861-881`：解析照做、轮次照跑；全仓 `IsOpen` 唯一读取点 `:865` | "熔断"无隔离/降频效果；多 provider 共享同一域名时阈值按单元累加、由任一成功清零，"连续失败轮数"语义被扭曲。AGENTS §四字面满足（每轮本就只探测一次）。建议接受现状并在注释/文档写明语义 |
| P3-03 | Push 首次发送失败且配置为"启用"时可能无限静默 | `internal/health/push.go:126-140` `sendOnce` 返回 false → `active=false` 并进入纯等待分支 | 与 P2-04 **同源**（同一无定时器分支）；修 P2-04 时一并处理 |
| P3-04 | **SSE 断线重连重复回放最多 1000 行**，挤掉真实新日志 | `webui/frontend/src/views/Logs.vue:45-50` 无去重、无 `onerror`；`webui/api/logstream.go:52-88` 每次订阅都回放环形缓冲；`webui/api/sse.go:22` 无 `id:` 字段 | 后端重启/网络切换/休眠后浏览器自动重连即触发。修法：加 `id:` 序号+前端丢弃已见，或回放用独立 event 名 |
| P3-05 | `GET /api/settings` 返回四个云凭据（AK/SK）但**无 `Cache-Control`** | `webui/api/settings.go:33-51` 无该头；全仓生产代码仅 `operational.go:42`/`alerts.go:99`/`export.go:72` 设置 `no-store` | 同一批密钥在 `/api/alerts`、`/api/config/export` 都被显式禁缓存，此处**遗漏而非设计**。修法：补一行 `no-store` |
| P3-06 | CVM 100 条上限按"入站+出站合计"判定，实际配额为**每方向** 100 | `provider/tc_cvm.go:234-241` 把 IngressIPv4+IngressIPv6+EgressIPv4+EgressIPv6 相加与 100 比较；`PlatformAPIDocs/TencentCVMAPIGuide/查询用户安全组配额.md:62` 为 `"SecurityGroupPolicyLimit": 100`；`安全组添加规则.md:18` 明确"一次请求中只能创建单个方向的规则" | 偏保守：出站规则多时会**拒绝合法的入站新增**（硬错误、不可重试）。修法：只统计入站计数。**建议用真实账号确认配额口径后再改**（属人工验收项） |
| P3-07 | **死代码：`retrySync` 零生产调用方** | `syncer/retry.go:26` 仅被 3 个测试文件约 15 处调用；生产走 `retrySyncDetailed`（`syncer/syncer.go:905`） | 被取代后未删除。注意：`Issue6.md` A1 段仍把它写在生产链上，删除时需同步更新该文档 |
| P3-08 | pidfile 判活机制的两类相反失效：**TOCTOU 漏判**（可双实例）与 **PID 复用误判**（永久拒绝启动） | `config/pidfile.go:19-31` ReadFile→processExists→WriteFile **非原子**（无 `O_EXCL`/flock）；`config/pidfile_unix.go:15` `Signal(0)` 无法区分进程身份 | **主代理实测**：顺序启动被正确拒绝；**并发**启动时守卫也生效（一个退出）——TOCTOU 窗口真实但实践中难以命中，故定为 P3。PID 复用（崩溃后残留 + 该 PID 被无关进程占用）会永久拒绝启动、需手工删文件。**flock 方案（P1-02）下两类失效自动消失** |
| P3-09 | SQLite 写事务为 deferred，先读后写存在 WAL read→write 升级（`SQLITE_BUSY_SNAPSHOT`，`busy_timeout` 不生效）；且注释理由与驱动实现矛盾 | `config/store.go:32-34` `BeginTx(ctx,nil)`；`:77-78` 注释以"会让只读事务申请写锁"为由拒绝 `_txlock`；**但驱动 `modernc.org/sqlite@v1.54.0/tx.go:23` 为 `if !opts.ReadOnly && c.beginMode != ""`——只读事务根本不加 beginMode，该理由不成立** | 影响仅"偶发 500、重试即成功、不损坏数据"。修法：DSN 加 `_txlock=immediate`（`ReadOnly` 事务不受影响），并同步修正注释与 `config/store_dsn_test.go:142` 的断言 |
| P3-10 | 忽略返回值的 error（与 AGENTS §十一 冲突） | `config/pidfile.go:33` `os.Remove` 丢弃；`config/store.go:118/126` 两处 `db.Close()` 未检查；`webui/api/logstream.go:97` `_ = h.Handle(...)`；`webui/api/sync.go:128-130` `json.Marshal` 失败静默 `continue`；`webui/server.go:298` `w.Write` 忽略 | 均为实际不可失败或影响极小，但 §十一 为硬条款且同仓库其它位置都做了处理，口径不一致 |
| P3-11 | `StoreLogWriter.OnEvent` 吞掉写库失败 | `webui/api/logwriter.go:83-86` 记 WARN 后 `return nil` → `notifier/bus.go:114` 的"事件处理失败"路径**永不可达** | 磁盘满/SQLite BUSY 时同步历史静默缺行，用户看到上一轮记录，误以为本轮未跑 |
| P3-12 | 数据目录/DB/pidfile 无权限收敛 | `run.go:41` 目录 `0755`、`config/pidfile.go:29` `0644`、`sql.Open` 默认（通常 0644）；全仓 `Chmod\|Umask\|0600\|0700` **零命中** | 同机多用户可读明文云凭据与 SMTP 密码。**用户决策：收敛为 0700/0600**（注意：对**已存在**的目录/文件需显式 `Chmod`，`MkdirAll` 不改变既有权限） |
| P3-13 | `MultiHandler.Handle` 首个 handler 出错即 return，后续 handler 整条丢弃 | `app/logutil.go:28-37` `if err := h.Handle(...); err != nil { return err }` | stdout 写失败（管道断裂）时 WebUI 日志流同时静默丢行。`app/logutil_test.go` **完全没有 MultiHandler 用例** |
| P3-14 | 未就绪依赖返回 400 而非 503 | `webui/api/sync.go:24/76/94`、`webui/api/logstream.go:131`；对照 `operational.go:44-51` 未接线时正确返回 503 | 状态码语义不一致；前端 `EventSource` 对非 2xx 不重连，用户只看到"日志一直空" |
| P3-15 | 被丢弃的告警事件**每条约一条 WARN**，故障期日志洪水 | `notifier/inflight.go:62-67` `logDropped` 对每个被丢弃事件都记 WARN；`notifier/bus.go:113` 每事件每订阅者一个 goroutine | 500 个 DNS 失败事件 → 约 496 条 WARN。量级不大且信息有用，属可聚合项 |
| P3-16 | 前端 UI/状态偏离（AGENTS §11） | `RunTest.vue:32` 缺 `size="large"`（唯一漏网，**属明确漏改**）；`App.vue:10/66-69` 侧边栏高亮不随路由（刷新/深链后停在"仪表盘"）；`Targets.vue:209-212`/`Rules.vue:165` 保存按钮无 in-flight 守卫（双击产生重复行）；`Settings.vue:127-141` 把不可见的 `theme` 当隐藏字段回传（与侧边栏主题开关**双写**，导致主题静默回退）；`useScannedResources.ts:36-41`+`Settings.vue:98-105` 清空扫描失败仍提示"已清空"；`Settings.vue:14` 前端正则比后端更严（合法 `1h30m` 被拦） | 均为可静态判定；`theme` 双写与"清空误报成功"影响用户可观察状态 |
| P3-17 | 陈旧 "version 2" 表述残留约 12 处 | `config/runtime_test.go:27,66`、`webui/api/testenv_test.go:216`、`import_runtime_test.go:82`、`import_test.go:11,20,111`、`export_test.go:461`、`redact_test.go:111,116`、`main_test.go:732,735` | Build7 Step 7 声称已收口 version 2 滞后注释，**测试文件未被覆盖**；`import_test.go:111` 甚至用 map key `"version 2"` 承载 `{"version":3,...}` |
| P3-18 | 文档漂移（非强制文档） | **Step 0 部分收口：** `Design5.md` 当前配置包已改为 version 3，Build7 已完成状态与 `ProdTestList.md` 范围矛盾已收口；`Build6.md`/`Issue5.md` 历史措辞、`Issue6.md` 基线对账与测试内 version 2 注释等仍是剩余文档清理 | 与 P1-01 直接冲突的部分已修；其余不是 Issue7 Step 1 前置 |
| P3-19 | 邮件 AUTH 失败保留 SMTP 诊断文本（含 535 回显） | `notifier/email.go:219-221` `%w` 包装；`webui/api/test_email.go:78-82` 回给浏览器；`notifier/email_test.go:293-295` **显式断言**保留诊断 | 理论风险：若服务器回显 AUTH 载荷可间接泄露 base64 凭据（**未验证**；标准 SMTP 不会这样做）。属可调试性取舍 |
| P3-20 | 邮件头/正文直发原始 UTF-8（无 RFC 2047 头部编码、无 CTE 声明） | `notifier/email.go:243-244`；`config/validate.go:307-316` 已排除头部注入 | 默认配置即中文主题/正文。多数现代 MTA 可正常投递；**真实表现必须由真实收件箱验证**（未执行） |
| P3-21 | Webhook 响应体未 drain（连接不可复用）；Push 响应体解析无字节上限 | `notifier/webhook.go:120-124` 只 Close 未 drain；`internal/health/push.go:247` `json.Decoder` 无 `io.LimitReader`（仅 10s `client.Timeout` 兜底） | 每次告警多一次 TCP/TLS 握手；Push 内存峰值不受字节数约束 |
| P3-22 | Dry Run 对每条规则各发一次 `GetRules` 并 sleep 一个厂商限速间隔 | `syncer/syncer.go:604-640` 内层 for rule 里 `p.GetRules()` + `time.Sleep(rateLimitInterval(...))`，注释写"与 syncAll 一致"（实际 `syncAll` 每 provider 只 sleep 一次，`:805-817`）；`syncer/ratelimit.go:10-19` SWAS/Lighthouse 5s | Lighthouse/SWAS 目标 20 条规则 = 100 秒纯 sleep + 20 次 Describe；预览页可能先超时。修法：每 provider 取一次规则集，在内存中逐规则 Diff |
| P3-23 | ticker `Reset` 语义误用 | `syncer/syncer.go:289-291`（true→true 也 Reset）、`:296-299`（false→false 分支 Reset 了已 Stop 的 ticker）、`:279`（Reset 后未清空 `ticker.C`）；`time.Ticker.Stop` 不排空通道（Go 源码已核实） | (a) 以高于 interval 的频率保存任意配置（含主题修改）会**无限推迟**周期同步，配合 `interval + health_timeout` 判据还会误告警；(b) 暂停超过一个 interval 后恢复可多跑一轮（代码只对 trigger 做了 `drainTrigger`）。修法：仅 interval 实际变化时 Reset；暂停分支不 Reset；恢复时一并清空 `ticker.C` |
| P3-24 | 轮内 pause→resume 通知被合并，Run 只见 `true→true` → 恢复不触发立即一轮 | `syncer/syncer.go:212-215` `wasEnabled` 取本循环相位、`:274-288` 仅 `false→true` 立即一轮、`:138-141` 通知可合并 | 与 Build6 §12.5 文档化契约「false → true 立即触发一轮」不符（AGENTS §五本身只要求"暂停时不触发"）。修法：过渡判定用"已发布真值 + 上次已生效真值"组合，或记录 generation |
| P3-25 | ECS `NextToken` 分页缺"token 未推进即退出"守卫 | `provider/ali_ecs.go:96-102`、`provider/scan.go:208-213` 仅判空不判重复；对照 `tc_lighthouse.go:92-95` 与 `ali_swas.go:97-100` 用"本页数量 < pageSize"收敛 | **unproven**（依赖云端异常）。若云端回传同一 token 会无限循环 + 无界追加，`roundStartedAt` 一直非 nil → health 报轮次超时但 goroutine 与内存只增不减，`Stop` 后 `Wait` 也无法返回。修法：记录上次 token，相同即 break 并 WARN；可为 `ScanResources` 加条数上限 |

> 说明：第 9 节的决策表使用 #1..#7 编号（你实际决策的 7 项）；本 P3 表使用 P3-nn 编号，两套编号相互独立。P3-08 已合并原先拆分的两类 pidfile 失效（TOCTOU 漏判 / PID 复用误判），实施时由 flock 一次解决。

---

## 3. 内存与资源专项结论

| 类别 | 结论 | 依据 |
|---|---|---|
| **goroutine** | **有界，无泄漏** | 常驻仅 5 个（`s.Run`、`supervisor.Run`、`pusher.Run`、`srv.Wait`、`serveErrCh`）；`syncer.go:803` 每云厂商一个（≤4）且有 `wg.Wait()` 收束；`bus.go:113` **每事件每订阅者一个短命 goroutine**（无背压，故障期瞬时数百个但迅速退出，非泄漏）；收尾顺序 `pusher.Stop → supervisor.Stop → s.Stop → s.Wait` 完整 |
| **ticker/timer** | **有界，均正确 Stop**，但 `Reset` 语义有误用 | `syncer.go:193` ticker `defer Stop`；`supervisor.go:86` `defer ticker.Stop`；`push.go:147` timer 三分支都 `Stop()`；`run.go:62/244` 的 `time.After` 为短命启动/收尾等待。误用见 P3-23 |
| **channel/订阅** | **有界，无泄漏** | `triggerCh`/`controlCh` cap=1 且 `ApplyState` 用 `select/default` 合并（合并导致 P3-24）；`EventBus.chanSubs` 取消时**同锁内 delete** 且**永不 close**（`bus.go:78-80` 有 send-on-closed 的 panic 论证）；`LogBroadcaster.subs` 取消时 close+delete；`AlertManager.Apply` 先按**旧集合**退订再按新集合订阅 → `alertset_test.go:159-172` 重复 Apply 5 次验证无增长 |
| **HTTP body/连接** | **关闭完整**，两处可优化 | 全部 outbound 均 `defer Body.Close()`（`webhook.go:124`、`push.go:236`）；`webui/api/decode.go:78` 的唯一 `io.ReadAll` 已被 `http.MaxBytesReader` 界定（1 MiB/10 MiB）；未 drain 见 P3-21 |
| **SQLite rows/事务** | **完全干净** | 6 处 `rows` 全部 `defer Close()`；`ensureColumnTx` 三条错误分支显式 Close；所有事务 `committed` 标志 + defer Rollback（仅忽略 `sql.ErrTxDone`）；`config/store_error_test.go:23` 证明 panic 也回滚；commit 失败**不 apply 不发布** |
| **SSE** | **有界且退出闭合** | 两条流均 `defer unsubscribe()`，均 select `ShutdownCh`；`probeSSE` 在写响应头**之前**；每次写出独立 5s deadline（`webui/api/sse.go:10`）；订阅 channel 容量固定 `logRingSize+256`；**不存在"重连新开而不关旧"的累积**（`EventSource` 单实例） |
| **日志与集合** | **全部有界，两处例外** | `sync_logs` 裁剪至 1000（`config/store.go:984`）；`GetSyncLogs(100)`；`LogBroadcaster.ring` 固定 `[1000]string`；前端 `logLines` 上限 1000；`scanned_resources` 按 cloud_type+region **覆盖式**。例外：熔断器域名 map（P3-01）、ECS `NextToken` 循环（P3-25） |
| **配置快照** | **有界且不可变** | 每次配置变更创建一个 `RuntimeState`，旧状态与旧 `ClientPool` 被丢弃；`rc.DeepCopy()` + `DeepCopyRules` 确保发布后不可变；旧 SDK client 的空闲连接由 transport `IdleConnTimeout` 回收（**未显式 Close**，属可接受） |
| **前端响应式状态** | **有界** | `logLines` 1000 封顶；dry-run 结果每次覆盖不追加；扫描缓存按 cloud_type 覆盖；无 `localStorage`/`sessionStorage`/`cookie`/`console.*` |
| **Docker/进程资源** | **已验证正常，但含 P1-02** | 镜像非 root（`uid=1000(appuser)`）、`/app/data` 属主正确、`wget` 存在（BusyBox `/usr/bin/wget`）、`HEALTHCHECK` 指向静态 `/api/health`（30s/3s/10s/3）、容器 `healthy`、`docker stop` 0.125s 且 `ExitCode=0`、无 OOM。**但存在 P1-02 的崩溃循环** |

**结论**：本项目的资源管理**明显优于**同规模项目。唯二无界结构是熔断器域名 map（P3-01，已决定修）与 ECS 分页循环（P3-25，unproven），其余均已证明有界。

---

## 4. 冗余、死代码和兼容残留

### ✅ 确认可删除（已双重核实：静态引用 + 测试引用 + 路由/embed/构建 + 文档）

| 目标 | 位置 | 引用检索结果 | 删除影响 |
|---|---|---|---|
| `Config` + `LoadConfig` + `ToConfig` | `config/config.go:151`、`config/store.go:1025`、`config/runtime.go:116` | 生产 **0**；仅 5 个测试文件。自述"过渡兼容，最终由 RuntimeConfig 取代" | 需同步改 5 个测试改用 `LoadBusinessSnapshot().ToRuntimeConfig()`。**最高价值清理项** |
| `EventRuleChanged` | `notifier/bus.go:17` | **全仓 1 处（仅定义）**，零生产者零订阅者；AGENTS.md:193 只枚举三种事件类型，未提它 | 无影响，不与 AGENTS 冲突 |
| `ResolvedIPs` | `provider/provider.go:78` | 全仓 2 处（注释 + 定义），零使用 | 无影响 |
| `SyncEvent` | `webui/frontend/src/types.ts:117` | 全仓 1 处（仅定义） | 无影响 |
| `clearAllCache` | `webui/frontend/src/composables/useScannedResources.ts:44/62` | 全仓 2 处（定义 + 导出），零调用 | 无影响（其注释声称"「清空所有数据」后调用"，实际 `resetAll` 只做 `location.reload()`） |

### 🟡 高可信删除候选（建议清理，需一并改测试）

| 目标 | 位置 | 引用结果 |
|---|---|---|
| `retrySync` | `syncer/retry.go:26` | 生产 **0**；约 15 处测试调用。建议机械改测试调用点后删除，并同步更新 `Issue6.md` A1 段 |
| `PusherDeps.bus` + 相应断言 | `internal/health/push.go:60/91`、`push_test.go:443-462` | 只赋值不读；该断言**永真**（该包任何路径都不 Publish）→ 删字段或改为真正可失败的守卫 |
| `useDryRun.error` / `useSettings` 多余导出 | `webui/frontend/src/composables/useDryRun.ts:32`、`useSettings.ts:70` | 写而不读 / 导出未解构 |
| 仅测试使用的导出 | `Store.SetSetting`（`store.go:363`）、`SaveAlertEmail`（`:777`）/`SaveAlertWebhook`（`:826`，非 Tx 版）、`GetSettingsTx`（`:962`）、`TargetExistsTx`/`RuleExistsTx`（`:619-627`）、`SetBeforeRoundHook`（`syncer.go:466`）、`AlertManager.Current`（`alertset.go:148`）、`Server.ServeStarted`（`server.go:135`）、`Pusher.InFlight`（`push.go:180`）、`InFlightLimiter.InFlight`（`inflight.go:48`）、`ErrNoSnapshotLoader`（`coordinator.go:57`，全仓无 `errors.Is` 比较） | 生产 0（或仅声明处） | 建议改非导出而非删除——其中 `stop_gate_test.go` 依赖 `SetBeforeRoundHook` 做 A20 stop 门控回归，**有判别价值** |

### ⛔ 暂不能删除

| 目标 | 原因 |
|---|---|
| `syncer/ratelimit.go`、`NormalizeResourceID`、`SaveAlertEmailTx`、`SaveAlertWebhookTx` | **有生产调用**（`syncer.go:813`、`config/validate.go:84`、`config/store.go:1297/1300`） |
| `Syncer.Pause()` / `Resume()` | `webui/api/deps.go:20-25` 明确记录：**API 侧接口成员已删除，`syncer` 侧实现必须保留**（Build6 要求 + 包内测试使用）。若确定不再需要，需先修改文档 |
| `MultiHandler` / `NewMultiHandler` 降级为非导出 | 会撞 AGENTS.md:191「`MultiHandler` 统一定义在 `app/logutil.go`」→ **需先修改强要求** |
| `webui/api/redact_test.go`（无对应 `redact.go`） | 这不是死代码：它是针对现有写入器的 sentinel 泄漏断言测试，**属正向控制**，保留 |
| `docker-compose.yml.example:42` 的 `pgrep` 字样、`webui/server.go:40` 的 `serveOnce` 字样 | 均仅出现在**注释**中（声明不使用/描述修复前行为），属预期残留 |

### 🔁 重复实现

| 项 | 位置 |
|---|---|
| 默认值多处真值源：`"587"` / `"dingtalk"` | `config.DefaultAlertPort`/`DefaultWebhookChannel`、DDL `config/store.go:175/187`、`webui/api/bundle_v3.go:353/363` **硬编码字面量**（同文件 `alerts.go:82/91` 却用了常量） |
| `map[string]bool` 做 presence 检查 | `webui/api/bundle_v3.go:617-626`、`:786-795` → **Go map 迭代随机**，同一次非法请求的错误文案跨进程不稳定；同文件 `normalizeBundleSettings` 已用显式顺序 if 链（正确写法） |
| 前端 `theme` 双写 | `Settings.vue:127-141` 与 `useSettings.setTheme` 各写一次 |
| 三处独立健康/状态派生 | `Dashboard.vue:48-70` 自行派生"停滞"判定，与后端权威 `internal/health/health.go` 语义冲突（即 P2-08） |
| 前端重复实现后端校验 | `Settings.vue:14` 的 `intervalPattern` 比 `config/validate.go:172-179` 更窄 |

### 🛡 过度防御（无收益复杂度）

| 项 | 判定 |
|---|---|
| `mustDuration`/`mustThreshold` 解析失败**静默返回 0**（`config/runtime.go:137-152`），而注释称"非法值不可能到达这里" | 若真为 0，`syncer.go:193` `time.NewTicker(0)` **会 panic**（Go 公开契约）。生产两条路径都经 `normalizeSettings`，**当前不可达**；属"把不该发生转成必然崩溃输入"的负收益兜底。建议改为 fail-fast 或使用文档化默认值 |
| `internal/health/health.go:153/185` `slices.Compact` | 只去**相邻**重复，当前追加序列不可能重复 → 不可达的冗余防御，且**并非通用去重**（未来新增原因来源会静默失效） |
| `internal/health/health.go:194-198` 有 `Policy == nil` 检查，而 `Evaluate`（`:130-132`）直接调用无保护 | 防御强度不一致 |
| `internal/health/push.go:190-196` 先 `buildPushURL` 校验后 `_ = target`，`:220` 再完整重算 | 无用计算（保留首次调用的**校验**语义，消除赋值/重算） |
| `webui/server.go:303-306` 吞掉 `fs.Sub` 错误并按需禁用静态处理器 | 实践中不可达（embed 保证成功）；若可失败则整个 WebUI 变空，**静默降级**掩盖真实故障 |
| `internal/health/push_test.go:16-18,497-499` 的 `var _ =` | 唯一作用是让三个无用 import 通过编译，属死测试脚手架 |

### 📝 只需文档清理

Step 0 已修正 `Design5.md` 当前 version 3 口径、Build7 状态与 `ProdTestList.md` 范围冲突。剩余仅文档清理仍包括：`Build6.md:3`/`Issue5.md:5` 历史措辞、`Issue6.md:5,7,597` 基线对账、约 12 处测试内 "version 2" 注释、`webui/frontend/src/constants.ts:1/21`、`notifier/email.go:57`/`webhook.go:41` 与 `internal/health/push.go:103-104`。

---

## 5. 设计与运行逻辑评估

| 子系统 | 稳定设计 | 脆弱点 | 不必要复杂度 | 推荐简化方向 |
|---|---|---|---|---|
| **startup/shutdown** | 启动顺序确定性化（`go s.Run()` → 有界等 `Started()` → 再启 `supervisor`/`pusher`）；信号在 HTTP 绑定**之前**注册；收尾顺序 `HTTP shutdown ‖ pusher→supervisor→syncer` 正确；`store.Close()` 在 `s.Wait()` 之后 | **P1-02 容器崩溃循环**；`Server.Start` 失败后 `started` 保持 true 且 `waitDone` 永不关闭（重试被拒、`Wait()` 永久阻塞，**当前接线不可达**）；`supervisor.Stop`/`pusher.Stop` 在 `Run` 从未启动时永久阻塞（Syncer 有 `runGuard`，这两个没有） | — | 按 P1-02 换 flock；与 Syncer 对齐给 supervisor/pusher 加 `started` 守卫 |
| **配置事务与运行时发布** | **本项目最强的一环**：协调器 `锁 → 单事务 → 事务内快照 → 事务内构造候选 → commit → 无失败发布`；commit 后不读库不访问网络；`commit` 失败不 apply；`RuntimeState` 深拷贝 + 单锁替换；已证明**事务内无任何网络 I/O**（四个 SDK 工厂只做本地构造，无 IMDS/元数据/token 获取） | **P2-04 发布顺序**（Wake 早于 ApplyState） | — | 按 P2-04 决策调整顺序 + 同步修订 AGENTS.md:199 |
| **同步调度** | 单一控制通道 + 4 处 `beginRound()` 硬门控（stop 门控与 enabled 门控**并列不合并**）；`Stop` 为吸收态且 `doneCh` 单所有者；`idle/failed/partial/success` 判定清晰 | P3-23 ticker Reset 语义；P3-24 通知合并 | — | 仅 interval 变化时 Reset；恢复路径一并清空 `ticker.C` |
| **DNS/Provider** | 只使用**增量** API（已逐调用点验证，零全量覆盖 API）；TAG 精确匹配 + `Description` 匹配；熔断阈值随状态原子发布（普通变更 `Clone` 保留计数、导入重置）；`retrySyncDetailed` 每次 attempt 重新 `Describe → Diff → Create/Delete`；部分成功用 `PartialDeleteError` 如实累计 | **P0-01（已于 `108e528` 修复）/ P1-01 / P2-01 / P2-02 / P2-03**；`isRetryable` 依赖**字符串关键字兜底**（腾讯 SDK 错误类型无 `Unwrap`，属有据可查的妥协） | `retrySync` 死包装；`_txlock` 注释理由与驱动实现矛盾 | P0-01 保留回归；其余按 Issue7 Step 1～4 与独立批次修复；重构后重新证明再删 `retrySync` |
| **告警** | 默认全关；`渠道开关 + 触发开关`同时开启才订阅；邮件与 Webhook **共用同一固定渲染器**（顺序稳定、不遍历 map）；4 在途 + 满载丢弃最新 + 安全 WARN；限流器跨热重载连续；`test-email` 8 字段契约两侧严格一致且不写库 | **P2-05 Webhook 只看状态码**；P3-11 吞错；P3-15 丢弃日志逐条 WARN；P3-19/P3-20/P3-21 邮件与响应体细节 | — | 解析业务错误码；聚合丢弃日志 |
| **OperationalHealth** | **唯一计算源被三个消费者真实共用**（`supervisor` / `operational` 端点 / `pusher` 都走同一个 `*health.Checker`）；2s 非阻塞探活（`Store` 结构体**无互斥量**，不持应用锁）；`StartupGrace=10s` 三分支正确；`failed/partial` 直到被 `success/idle` 覆盖；原因稳定去重排序；30s 边沿监督器 | 判定输入来自三次独立 `Snapshot()`（`run.go:127-142`），注释自述"一致快照"但可能混用新旧 policy/interval → 30s 内一次瞬时误判，自愈 | `slices.Compact` 冗余 | 一次取 `*RuntimeState` 后派生 policy/interval |
| **HTTP/SSE** | 严格解码齐全（未知字段/尾随/多顶层值/10 MiB/1 MiB/413）；路径 ID `strconv.Atoi` 且 >0；请求 DTO 不含 DB `id`；导出 GET 已删（实测 405）；两类 SSE 监听服务器级 `ShutdownCh` 且每次写出有 5s deadline | P3-05 缺 `no-store`；P3-14 400/503 语义；`GET /api/alerts` 4 次非事务读存在撕裂窗口（PUT 单事务写，读侧可能"新 policy + 旧 email"，前端整体回传即把旧值写回） | `fs.Sub` 静默降级 | 补 `no-store`；GET alerts 改只读事务取快照 |
| **前端** | 8 字段测试邮件载荷两侧严格一致（历史上真实 bug 点，现有注释+类型双重防护）；无 `console.*`/存储/cookie 泄漏；密码与 Webhook/Push URL 用 `type="password"`；导出用 `fetch`+Blob 且 `revokeObjectURL`；EventSource 单实例且卸载关闭；无 `addEventListener` 泄漏 | **P2-06 / P2-07 / P2-08 / P2-09**；P3-04 SSE 重放；P3-16 一批 UI/状态偏离 | 前端重复实现后端校验 | 逐项按 P2/P3 收敛；**不建议**引入 Pinia/Vitest 等重型栈 |
| **Docker/CI** | 非 root + `CGO_ENABLED=0` 静态编译 + `HEALTHCHECK` 用静态 `/api/health`（实测全部符合）；前端在 builder 阶段构建并 `COPY` 进 Go 阶段（顺序正确）；容器实测 `healthy`、`ExitCode=0` | **清洁检出裸 Go 命令 100% 失败**（P1 级构建阻断）；P1-02 容器崩溃循环；`go get -u` 使构建不可复现（AGENTS §十 **有意设计**） | Makefile 与 CI 命令集不统一 | 给 `test`/`vet` 加 `frontend` 前置；`go get -u` 策略受强要求约束，**不建议**擅自改为锁版本 |

### 附：构建阻断（原 P1，已并入批次 6 但严重度仍在）

`webui/embed.go:5` 的 `//go:embed frontend/dist` 依赖被 `.gitignore:5` 忽略的目录；`Makefile:6` 的 `build` 有 `frontend` 前置，但 `Makefile:9-13` 的 `test`/`vet` **没有**。在 `git archive HEAD` 抽取的清洁副本上实测：

| 命令 | 结果 |
|---|---|
| `go build ./...` / `go vet ./...` / `go test ./...` | **EXIT=1 / 1 / 1** `pattern frontend/dist: no matching files found`（root 与 `webui` 包 `[setup failed]`） |
| `make vet` | **EXIT=2** |
| 手动 `mkdir -p webui/frontend/dist` 后 `go build ./...` | EXIT=0 |

**修复**：给 `test`/`vet` 加 `frontend` 前置依赖（Make 在同一次调用内对同一前置去重，`make all` 仍只跑一次 `npm ci`）。**不要**提交占位 `webui/frontend/dist/index.html`——它会被 embed 并由 `webui/server.go:305` 的 FileServer 当真实页面提供。

---

## 6. 测试质量与缺口

**总体**：62 文件 / 20,457 行（约为生产代码 1.9 倍），密度高，且**确实有判别性**——Issue6 逐条记录了"红灯 → 修复 → 绿灯"。`config/store_error_test.go` 用同名 VIEW 注入迁移失败、`config/store_v3_test.go:371` 判别"迁移只归零一次"、`webui/sync_status_test.go:161` 判别 `last_success` 保留语义、`main_test.go` 的 8 字段邮件载荷用例含**反向断言**（多带 `enabled` 必须 400），这些都是**真正会失败的**测试。

**判别性良好的核心合同**（无需改动）：事务与发布（`coordinator_test.go`、`import_failure_test.go`）、迁移与 Schema（`store_v3_test.go`、`store_dsn_test.go`）、生命周期（`stop_gate_test.go`、`started_test.go`、`webui/server_test.go`）、契约严格性（`decode_test.go`、`alerts_v3_test.go`、`export_test.go`、`redact_test.go`）、告警与健康（`inflight_test.go`、`supervisor_test.go`、`push_test.go`、`health_test.go`）、四平台 TCP+UDP/ICMP/IPv6 矩阵、CVM 100 上限。

**弱断言 / 误导**：

- `policy_test_helpers_test.go:31-35` `waitForNoSMTPData` —— **名为"断言无报文"，实为纯 `time.Sleep`，无任何断言**（4 个调用点各自补了 `rec.Data() != ""`，侥幸未失效）。**必须修正**
- `push_test.go:443-462` —— 断言 `pub.count()==0`，但该包任何路径都不 Publish → **永真、无判别力**（成因是 `PusherDeps.bus` 只赋值不读）
- `webui/api` 对 `AlertManager` 注入限流器这一步**零断言**（`grep InFlightLimiter webui/api/*_test.go` 零命中）→ 生产"每渠道 ≤4"接线只靠 notifier 包内**手工模拟**覆盖

**7 个永远无法失败的测试**（逐个读正文确认）：`dns/resolver_test.go:24-34`（`if err == nil { t.Log }`）、`:36-58`（唯一 `t.Fatal` 被 skip 保护）、`notifier/bus_test.go:108-118`、`syncer/state_test.go:411-436`、`syncer/syncer_test.go:646-670`（注释自述只靠 race detector → **不带 `-race` 时是空测试**）、`webui/api/alertset_test.go:175-179,215-217`。另 `dns/resolver_test.go:60-71` 只断言非 nil，**从未验证注释声称的"自动补 :53"**。

**固定数值契约没有字面量锚点** → 改错强要求数值仍全绿：`InFlightLimit=4`、`DefaultStartupGrace`、`DefaultHealthTimeout=10m`、`DefaultPushInterval=60s`、`MinPushInterval=20s` 的测试都用常量自身计算期望。对照良好范例：`push_test.go:243` 用字面量 `250`、`webui/server_test.go` 的 `TestServerTimeoutContract` 用字面量 `5s/120s`。

**缺失的关键路径**：

1. **前端 → 后端的载荷契约**：8 字段测试邮件与被严格解码的后端 DTO 之间**没有测试固定"前端实际发出的键集合"**（后端只测"多余字段被拒"）。历史上正是此处出过真实缺陷，当前唯一防线是两处注释
2. **构建/部署契约零守护**：`grep -rln "Dockerfile|docker-publish|Makefile|alpine:3.20|CGO_ENABLED" --include='*_test.go' .` → **0 命中**，而 AGENTS §八 用「**不得**」措辞
3. `GET /api/zones` handler —— 路由字符串**未出现在任何 Go 测试**（`zones_test.go` 只测 `zoneData` map）
4. **零 goroutine/ticker/连接泄漏断言**（`grep NumGoroutine|goleak` 唯一命中是注释）
5. `provider/scan.go` 四条扫描路径几乎无测试（`scan_test.go` 仅 16 行）
6. **负面 sleep 断言会在慢 runner 假通过**：`syncer/state_test.go:174-177,204-205`、`syncer/syncer_test.go:130`；syncer 包 25 处 sleep（12 处 >200ms，最长 600ms）集中在最关键的门控用例上。`webui` 包用 `ServeStarted`/channel 做确定性同步，是正面范例
7. `app/logutil_test.go` **完全没有 `MultiHandler` 用例**
8. `-race` 非空跑（`t.Parallel()`=0，但 24 个测试文件在用例内起 goroutine），唯一边角是上述 race-only 用例

**推荐新增的最小测试**（**不引入任何测试框架**）：

1. P0-01：Aliyun `GetRules→Diff` 往返收敛断言（已由 `TestDiff_AliyunPortRoundTripConverges` 补齐，Issue7 重构时必保留）
2. P1-01：同目标两条空 comment 规则的双轮断言（第二轮不得出现 delete）
3. P2-01：IPv6+ICMP key 断言（Lighthouse/CVM）
4. P2-02：ECS 150 条删除产生 2 个请求
5. P2-04：探针 waker 断言"wake 时新配置已可见"
6. P3-05：`GET /api/settings` 的 `no-store` 断言
7. 前端载荷契约固化在既有 Go 测试中（8 键字面量断言 200 + 带 `enabled` 断言 400）
8. 一个约 40 行的 `build_contract_test.go`（断言 alpine/golang 版本、`CGO_ENABLED=0`、非 root、HEALTHCHECK 指向 `/api/health` 且不含 `/operational`、`linux/amd64`、`.dockerignore` 含 `fwalizer-config`）
9. `config/pidfile` 单测（目前**不存在**该文件）

---

## 7. 正向控制（整改时**不得**误删）

1. **配置协调器的单事务 + 无失败发布骨架** —— 防"接口报错但库已变"的唯一屏障
2. **`RuntimeState` 深拷贝 + 单锁指针替换 + 发布后不可变**（含 `DeepCopyRules`、`Credentials` 无 setter）
3. **只使用增量云 API**（零全量覆盖/重置）+ `[TAG]` 前缀 + `Description` 匹配 —— "绝不误删**非本工具**规则"的全部依据（该边界经审核确认正确）
4. **`Stop` 门控与 `enabled` 门控并列不合并**（4 处 `beginRound()`）+ `Stop` 吸收态 + `doneCh` 单所有者
5. **唯一 `OperationalHealth` 计算源**（三消费者真实共用同一 `*health.Checker`）+ 2s 非阻塞探活 + `StartupGrace=10s` 三分支
6. **每渠道 ≤4 在途限流器跨热重载保持连续**（限流器由 `AlertManager` 持有，不随 notifier 实例替换）
7. **EventBus 取消订阅不关闭 channel**（`bus.go:78-80` 有 send-on-closed 的 panic 论证）+ 快照复制
8. **两类 SSE 监听服务器级 `ShutdownCh` 显式退出** + 每次写出独立 5s deadline
9. **严格解码全套**（未知字段/尾随/多顶层值/413/10 MiB/1 MiB/路径 ID >0/请求 DTO 不含 `id`/`present targets`）
10. **`/api/health` 保持静态存活语义**，`HEALTHCHECK` 不改用 operational 端点
11. **全部有界化**：`sync_logs` 1000、环形日志 1000、前端日志 1000、`scanned_resources` 覆盖式
12. **启动顺序确定性化**（`Syncer.Started()` 有界等待后再启动 supervisor/pusher）
13. **迁移显式化**（`ensureColumnTx` 用 PRAGMA 探测、单事务、失败中止启动、只归零一次）
14. **测试用 `127.0.0.1:0` 全体零固定端口**
15. **`redact_test.go` 的 sentinel 泄漏断言**（覆盖现有写入器，非死代码）
16. **`Syncer.Pause()` / `Resume()` 实现保留**（API 接口成员已移除，实现受 Build6 约束）
17. **`DailyRisk` 无关项：`isRetryable` 的字符串兜底有据**（腾讯 SDK 错误类型无 `Unwrap`，`.md` 注释已说明依据）

---

## 8. 专题：规则身份与 TAG 所有权（✅ 设计已定案，待实施）

> **本章是 P1-01 及其全部关联内容的唯一集中位置。**
>
> **状态（2026-09-29）**：用户已定案“目标级完整期望集 + TAG 所有权 + comment 纯可读 + 先增后验 + 平台化条件删除 + 无法证明安全时保留残留”。Step 0 文档合同已完成，Step 1～5 代码与验收尚未实施，统一见 [Issue7.md](./Issue7.md)。`maxTagRunes` 仍为 48。

### 8.0 最终方案（取代本节后续的历史候选状态）

- 仅 `description == "[TAG]"` 或以 `"[TAG] "` 开头的规则属于当前所有权命名空间；`[TAG]foo` 不属于。
- comment 可空/重复/修改/截断，不参与身份或删除；不做唯一性校验。
- 功能 key 是 `address-family + canonical CIDR + canonical protocol + canonical port expression + action`，不含 TAG/comment/description/域名/本地 ID/云端 ID。
- 同一目标的全部本地规则先汇总为完整期望集，同功能去重；非 TAG 精确等价规则可满足期望但永不被操作。
- 同步固定为 `S0 → Plan → Add → S1 → coverage verification → 安全门 → 条件 Delete → 必要时 S2`；Add/Describe/验证失败时旧规则全保留。
- Lighthouse/CVM 使用同快照版本保护；SWAS/ECS 只用 S1 稳定 RuleID；ECS 删除每批最多 100。
- 只要所需功能已确认，`cleanup_deferred` 仍是 `success`/healthy；平台能力不支持是 `partial`；DNS/Describe/Add/覆盖验证失败是 `failed`。
- 不新增本地映射表、不强制 comment、不在 description 编码功能身份、不在空期望集时自动清空 TAG。

下方 8.1～8.7 保留为定案前的缺陷机理、候选方案与外部不确定性记录；其中的“待决策/已冻结”不再表示当前状态。

### 8.1 历史决策状态与冻结说明（已被 8.0 取代）

| 议题 | 状态 | 说明 |
|---|---|---|
| **身份形态** | 🔶 **待决策** | 曾定为方案 A（`[TAG] <host>/<PROTOCOL>/<PORTS>[ <comment>]`）；**已撤回**。候选方案见 8.4 |
| **唯一性校验方式** | 🔶 **待决策** | 曾定为按 `(target set, host, protocol, ports)` 校验；**已撤回** |
| **长度超限行为** | 🔶 **待决策** | 曾定为"拒绝保存"；**已撤回** |
| **TAG 上限收紧 48→32** | 🔶 **已撤回** | 该收紧**仅为配合方案 A** 而提出。`config/validate.go:34` 仍为 48；`AGENTS.md:162` 未改 |
| **旧规则迁移** | ✅ **仍然有效** | 用户已确认**无历史兼容负担** → 任何最终方案都**不需要** orphan 清理入口 / 迁移脚本 / 旧格式 WARN。此结论与身份模型无关，独立成立 |
| **SWAS `Remark` 上限** | ⏳ **待真机验收** | 已登记为 `ProdTestList.md` **PT-B7-09**（详见 8.6） |

**冻结理由**：讨论中发现本缺陷与一个更根本的设计问题耦合——云端描述字段**同时承担三重职责**（见 8.3），且存在多个各有权衡的候选修复方案（见 8.4）。需要一次性想清身份模型，而不是打补丁。

#### 🔧 代码修复上线前的操作规避建议（历史运行阶段建议，非最终设计要求）

本缺陷在修复前**仍在生效**。规避方法很简单：

> **避免在同一目标上出现两条 comment 相同的规则**——尤其是两条都留空 comment（这是默认值，最容易触发）。

- 同 host 多规则（如 TCP 443 + UDP 443）→ 给两条填**不同备注**
- 不同 host 但备注都留空 → 至少给其中一条填备注
- 已存在碰撞配置时，**不会**有告警提示（这也是缺陷的一部分：`failed==0`，运行健康不报）

### 8.2 缺陷机理（完整因果链）

#### 8.2.1 三个事实（均已核对源码）

**事实 1：进入云端的"身份"只有 `desc` 一个字符串。**

```go
// syncer/retry.go:56
desc := truncateDesc(tag.Format(tagStr, rule.Comment), p.CloudType())
```

云端不保存"这条规则属于哪个 host"。工具重启后，唯一能从云端读回来、用于判断"这条规则是不是我的、属于哪条规则"的依据就是规则的描述字段。

**事实 2：`desc` 的取值只由 `tag + comment` 决定，而 `comment` 可空。**

```go
// internal/tag/tag.go:12-14
if comment == "" {
    return fmt.Sprintf("[%s]", tag)      // → "[auto-dns]"
}
return fmt.Sprintf("[%s] %s", tag, comment)
```

`comment` **允许为空**（`config/validate.go:97`「comment 允许为空，按原值保存」），且**无任何唯一性约束**。因此两条都留空 comment 的规则，`desc` **完全相同**。

**事实 3：`Diff` 完全靠"描述字符串精确相等"划定归属，并据此删除。**

```go
// provider/common.go:144-148
for _, r := range existing {
    if r.Description == desc {            // ← 唯一的归属判定
        domainExisting = append(domainExisting, r)
    }
}

// provider/common.go:177-182
for _, r := range domainExisting {
    k := keyOf(r)
    if !desiredKeys[k] {                  // ← 不在本规则期望集内 → 删除
        toDelete = append(toDelete, r)
    }
}
```

`desiredKeys` = 本规则当前**解析出的 IP** × 协议 × 端口。所以"清理陈旧 IP"正是靠这一步实现的。

#### 8.2.2 因果链

```
desc 只由 tag + comment 决定，comment 可空且无唯一性约束
        ↓
同一目标上两条规则可产生相同的 desc
        ↓
规则 A 同步时，规则 B 的云端规则因 desc 相同而进入 domainExisting
        ↓
B 的规则不在 A 的 desiredKeys（IP 不同）→ 被列入 toDelete
        ↓
规则 B 同步时反过来删除 A 的规则 → 互删，逐轮振荡
        ↓
最终稳定态：只剩最后被处理的那条规则的 IP 被放行，其余持续被拆除
且 failed==0 → 运行健康不告警（静默失效）
```

#### 8.2.3 已复现的证据（overlay 探针，仓库零写入）

```
规则A（comment 空，a.example.com）首轮: ToAdd=1
规则B（comment 空，b.example.com）对 A 的既有规则:
    将被删除: cidr=1.1.1.1/32 desc="[auto-dns]"
>>> ToDelete=1   （B 的同步把 A 的规则列入待删除）
FAIL
```

**边界（正确的部分）**：只删除本工具自己的规则，**不触碰非本工具规则**——`OwnedRules` 的 `[TAG]` 前缀过滤是有效的。

#### 8.2.4 两条产生碰撞的路径（最终方案必须同时覆盖）

| 路径 | 条件 | 示例 |
|---|---|---|
| **路径 1：不同域名 + 相同 comment** | 两条规则 comment 相同（**最常见：都留空**），解析到不同 IP | `api.example.com` 与 `www.example.com`，comment 均为空 |
| **路径 2：同一域名 + 多协议/多端口** | 同一 host 的多条规则，comment 相同 | `api.example.com` 的 TCP 443 与 UDP 443（**AGENTS §三 的 TCP+UDP 拆分机制本身就会产生**） |

> ⚠️ **仅给 desc 加 host 只能覆盖路径 1**。路径 2 的规则 host 相同，desc 仍会碰撞。这是方案 A 必须同时包含"协议 + 端口"的原因，也是本项研究不可忽略的一半。

### 8.3 结构性洞察：描述字段的三重职责

| 职责 | 内容 | 消费者 | 长度需求 |
|---|---|---|---|
| **A. 归属识别** | "这条规则是我的" | 工具（`OwnedRules` 按 `[TAG]` 前缀） | 固定（`[TAG] ` 前缀） |
| **B. 个体识别** | "这条规则对应我本地哪条规则" | 工具（`Diff` 按 desc 精确相等） | **可变且无界**（需要唯一） |
| **C. 人类可读** | "这条规则是干什么的" | 用户（云端控制台） | 可变、可截断 |

**结构性矛盾在于职责 B**：它要求"唯一"，而被编码进去的内容（域名）长度**无界**，字段容量却**定长**（Lighthouse 64、SWAS 50）→ 定长容器装不下无界数据 → **与 `[TAG]` 争夺同一段空间**。

这正是"TAG 变长会挤压域名"这一副作用（本报告早期版本曾详述）的根源，**且与用户是否想缩短 TAG 无关**——只要职责 B 仍由描述字段承担，容量矛盾就存在。

**用户的关键判断（2026-09-28）**：

> *"云端不需要展示完整的域名。备注是给人看的，我只需要 `[TAG]` + 用户自定义备注（可空），此处不需要完整域名。"*

该判断直接指向出路：**把职责 B 从描述字段上摘下来**，描述只保留 A + C，长度便只与 TAG 和备注有关，与域名彻底解耦。

**但必须注意连带后果**：职责 B 一旦摘掉，`Diff` 现有的**删除边界**（`r.Description == desc` 筛选）也随之失去依据——要保留"清理陈旧 IP"的能力，就必须为归属判定提供**新依据**。这是所有后续方案的分水岭：

> 描述里**放域名**是为了让归属筛选能区分不同规则；描述里**不放域名**，归属筛选就必须换依据。二者必须选一个。

---

### 8.4 历史候选方案（当前方案已由 8.0 裁决）

#### 8.4.1 四云可用字段核对结果

已逐个核对 `PlatformAPIDocs/`，结论：**"给域名另找一个字段存"无法作为通用方案**。

| 云 | 描述字段上限 | 是否有可用的独立字段 | 是否可读回（Diff 必需） | 结论 |
|---|---|---|---|---|
| Lighthouse | **64**（`TencentLighthouseAPIGuide/添加防火墙规则.md:15`） | ❌ `FirewallRules.N` 结构内无 Tag/Name | — | 不可行 |
| SWAS | **50**（来源存疑，见 8.6） | ✅ 有 `Tag`（Key/Value 各 ≤64），且 `ListFirewallRules.md:91-97` **确实回传 `Tags`** | ✅ | **仅此一云可行** |
| CVM | 未声明上限 | ❌ 查询安全组规则只回传 `PolicyDescription` | — | 不可行 |
| ECS | **512**（`AliyunECSAPIGuide/AuthorizeSecurityGroup.md:128`） | ❌ 参数表中无 Tag | — | 不可行 |

即便 SWAS 可行，其余三云仍只有描述字段可用 → 会导致四个 Provider 各走一套身份逻辑，与 AGENTS §十一 的收敛方向相悖。

#### 8.4.2 候选方案对比

| # | 方案 | 容纳任意域名 | TAG 完全自由 | 保留陈旧 IP 清理 | 跨四云一致 | 主要代价 |
|---|---|---|---|---|---|---|
| **A** | 描述含 `host + 协议 + 端口`，超限拒绝保存 | ❌（SWAS 最紧） | ❌（与域名互斥） | ✅ | ✅ | 容量耦合；TAG 与域名互相挤压；需收紧 TAG 上限；旧规则 orphan |
| **①** | 描述保持 `[TAG] comment`，**新增本地"规则 → 已下发 CIDR"状态表**做归属判定 | ✅ | ✅ | ✅ | ✅ | 引入持久状态；需处理"首次运行无状态"与"状态与云端漂移"的自愈 |
| **②** | 描述保持 `[TAG] comment`；**同 desc 分组，碰撞时不做删除**（只增不删） | ✅ | ✅ | ⚠️ 碰撞时暂停 | ✅ | 碰撞期间陈旧 IP 不被清理（可恢复：改掉重复 comment 即自愈） |
| **③** | 描述保持 `[TAG] comment`；**保存时强制"同一目标内 comment 唯一"** | ✅ | ✅ | ✅ | ✅ | 由于 comment 默认为空，实际会**强制用户为每条规则写备注** |
| **④** | 描述用定长本地标识（如 `[TAG] #<ruleID>` 或短哈希） | ✅ | ✅ | ✅ | ✅ | 云端控制台不可读（只见编号）；哈希方案存在非零碰撞 |
| **⑤** | 折中：`[TAG] #<ruleID> host`，**域名可截断、编号保唯一** | ✅ | ✅ | ✅ | ✅ | 描述格式最复杂；仍需实现截断与编号解析 |

#### 8.4.3 各方案的初步评估（供后续研究，非定论）

- **方案 ②** 的突出优点：改动最小（仅 `syncer/` 包内约 30 行；`provider.Diff` 签名与逻辑不动、store/schema/API/前端全不动、14 处测试调用点不动），且**取舍方向无脑正确**——把"可能误删云端规则（不可恢复）"换成"可能少删陈旧 IP（可恢复）"。其技术核心是在调用方（`syncer` 能看到同目标的全部规则）判定碰撞并清空 `toDelete`，两个调用点（正式同步 `syncer/retry.go:57`、Dry Run `syncer/syncer.go:625`）共用同一 helper。
- **方案 ①** 最彻底，但引入持久状态后必须回答"状态与云端漂移如何自愈"，复杂度显著上升。
- **方案 ③** 与方案 ② 同源，但把负担转给用户，且因 comment 默认为空而带有强制性。
- **方案 ④/⑤** 彻底解耦长度，但牺牲云端可读性——而可读性恰是 TAG 存在的价值之一，需用户判断其重要性。
- **方案 A** 已被撤回，不应作为默认选项。

### 8.5 与旧版 AGENTS.md 状态的关系（历史，不得据此实施）

- **无冲突的部分**：`[TAG]` 前缀识别归属（§三）在任何方案下都必须保留，这是固定成本。
- **潜在冲突的部分**：
  - 若最终采用**方案 A** 并收紧 TAG 上限 → 需修订 **AGENTS.md:162**「TAG … 最多 48 个 Unicode 字符」（属**强要求变更**，必须用户明确授权）。
  - 若最终采用**方案 ③**（强制 comment 唯一）→ 需确认是否与 §十一「普通 API 最小校验边界」的宽松取向冲突。
- **当时状态（2026-09-28，历史）**：方案尚未定，`AGENTS.md` 未修改；2026-09-29 Step 0 已用 8.0 的最终方案取代该状态，`maxTagRunes` 仍为 48。

### 8.6 待消除的外部不确定性

**SWAS 的 `Remark` 上限"50"在仓内 API 文档中查无出处。**

- `syncer/retry.go:200` 的注释写 `Remark ≤ 50 字符（阿里云 SWAS API）`，但 `PlatformAPIDocs/AliyunSWASAPIGuide/` 下所有提及 `Remark` 的文件（`CreateFirewallRules.md:48`、`CreateFirewallRule.md:45`、`ListFirewallRules.md:61`、`EnableFirewallRule.md:40`、`DisableFirewallRule.md:40`、`ModifyFirewallRule.md:43`）**均只描述字段、未给长度限制**。
- 影响：若真实上限 > 50，则当前实现会**过度保守**；若 < 50，则会在云端失败而非在本地被拦住；若按**字节**而非字符计算，中文备注预算再缩 3 倍。
- 该值对**方案 A** 是决定性的（决定 cap 常量），对**方案 ②** 则仅影响 `[TAG] comment` 的总长。
- **已登记为 `ProdTestList.md` PT-B7-09**（真机确认，含字符/字节口径）。**在确认前不得把 50 当作已核实事实。**

### 8.7 历史待决策清单（已由 8.0 裁决）

| # | 待决策项 | 说明 |
|---|---|---|
| 1 | **身份模型**：选 A / ① / ② / ③ / ④ / ⑤ 中的哪一个（或组合） | 决定后续全部实现 |
| 2 | 若选 **A**：desc 需含 host + **协议 + 端口**（否则路径 2 未修复）；长度超限行为（拒绝保存 / 截断 / 哈希降级）；是否收紧 `maxTagRunes` 并修订 AGENTS.md:162 | |
| 3 | 若选 **①**：本地状态表的形态、首次运行无状态的处理、与云端漂移的自愈策略 | |
| 4 | 若选 **②**：碰撞时"只增不删"是否可接受；WARN 的文案与字段 | |
| 5 | 若选 **③**：是否接受"每条规则必须写不同备注"（因 comment 默认为空） | |
| 6 | 若选 **④/⑤**：云端不可读是否可接受；用 ruleID 还是哈希 | |
| 7 | **云端描述字段的实际容量**：Lighthouse 64 已由文档确认；SWAS 50 待 PT-B7-09 真机确认；CVM 上限未声明 | |

### 8.8 决策状态记录

| 议题 | 状态 |
|---|---|
| identity 形态 | ✅ 目标级功能 key/完整期望集；description 只保留 TAG + comment |
| 唯一性校验方式 | ✅ 不引入 comment 唯一性/必填校验 |
| 长度超限行为 | ✅ 只影响可读 comment 截断，不影响功能身份 |
| TAG 上限收紧 48→32 | 🔶 **已撤回**（`config/validate.go:34` 仍为 48；AGENTS.md:162 未改） |
| 旧规则迁移 | ✅ **仍然有效**：用户确认**无历史兼容负担** → 任何最终方案都不需要 orphan 清理入口 / 迁移脚本 / 旧格式 WARN。此结论与身份模型无关，独立成立 |
| SWAS `Remark` 上限 | ⏳ 已登记 **ProdTestList.md PT-B7-09**，待真机确认；只影响 comment 可读长度，不阻塞 Issue7 Step 1～4 |

---

## 9. 用户决策记录（决策不等于已实施）

> 本表保留作出决策时的原始记录。当前实施状态统一看第 0 节：P2-04 的 AGENTS 目标顺序已在 Issue7 Step 0 修订，剩余为代码与测试；P2-03 原“只补 WARN/Dry Run、不计 skipped”的记录已被当前 AGENTS/Issue7 的 `unsupported → partial` 与清理冻结合同取代。保留旧记录是为了追溯，不得据此覆盖当前强要求。

| # | 议题 | 你的决策 | 实施要点 |
|---|---|---|---|
| 1 | P1-01 规则身份 | ✅ **设计已定案，代码待实施** | 采用目标级完整期望集、TAG 唯一操作授权、comment 纯可读、先增后验、平台化条件删除与可接受残留；不收紧 TAG 48、不需要迁移。详见第 8 节与 Issue7 Step 1～5 |
| 2 | P1-02 容器 pidfile | **改用 flock 文件锁替代 PID 判活** | 见 P1-02 章节；顺带消除 P3-08（TOCTOU 漏判 + PID 复用误判） |
| 3 | P2-04 唤醒顺序 | **改代码：`ApplyState` 提到两次 `Wake()` 之前，并同步修订 AGENTS.md:199** | 见 P2-04 章节；`coordinator.go:50-53` 注释同步 |
| 4 | P2-07 删除确认 | **补卡片式二次确认，满足 AGENTS §11** | `Targets.vue` / `Rules.vue` 各加一个 `NModal preset="card"`（确认按钮 `type="error"`），复用 `Logs.vue` 模式；**不改强要求文档** |
| 5 | P2-03 静默跳过 | **先只补 WARN 日志 + Dry Run 展示，不计入 skipped** | 见 P2-03 章节；建议为 Dry Run 新增独立字段 |
| 6 | P3-01 熔断器淘汰 | **加淘汰策略** | 在配置发布时按新配置的域名集合裁剪 `failCount`，或 Clone 时丢弃计数为 0 的条目 |
| 7 | P3-12 文件权限 | **收敛为 0700/0600** | 数据目录 `os.MkdirAll(dir, 0700)`；建库后 `os.Chmod(dbPath, 0600)`；pidfile/lock 文件 `0600`。注意：**对已存在的目录/文件需显式 Chmod**（`MkdirAll` 不改变既有权限） |

---

## 10. 当前实施顺序与历史批次

### 10.1 当前有效主线：Issue7 Step 0～5

**当前最高优先串行主线 — Issue7 P1-01（Step 0 已完成，Step 1～5 待实施）**

1. Step 1 纯规划器保留已修复 P0-01 的绿色回归，并吸收尚未修复的 P2-01、P2-03 与 P3-25 安全前置。
2. Step 2 实现目标级 Add → Describe → coverage verification，仍零自动删除。
3. Step 3 按 Lighthouse/CVM/SWAS/ECS 逐平台开启条件清理，同时吸收 P2-02。
4. Step 4 收口 Dry Run/事件/日志/Dashboard/健康，同时吸收 P2-08、P2-09、P3-22 和相关 P3-16。
5. Step 5 执行完整自动门禁与 PT-I7 四云/浏览器真实验收。

> P0-01 已是必保留的绿色回归；P2-01 仍需在唯一 functional key 中失败先行修复。P1-02/flock 仍是可独立实施的高优先级项，但不是 P1-01 的前置。

### 10.2 当前独立问题入口

与 Issue7 正交的 P1-02、P2-04～P2-07 及其余 P3 统一按第 0.4 节的 I-01～I-11 排队。该编号只表示 backlog 顺序，不改变 finding 严重级别，也不构成代码实施授权。

### 10.3 历史批次计划（与 Issue7 重叠部分已被取代）

> 下列批次是上一版报告形成时的排序记录，完整保留用于追溯。凡与 Issue7 Step 1～5 重叠的 P2/P3，均以 10.1 和 Issue7 为准；不得按本节另建第二套并行施工顺序。

**历史批次 2 — 其余云端规则正确性**

P2-01（IPv6+ICMP key）、P2-02（ECS 删除分批）、P2-03（补 WARN + Dry Run）、P2-04（唤醒顺序 + 文档）、P2-05（Webhook errcode）、P2-06（告警页守卫）、P2-07（删除确认）

> 验收：每项一个判别性用例；P2-02 需 150 条删除分批断言

**历史批次 3 — 生命周期、并发与内存**

P3-01（熔断器淘汰）、P3-23（ticker Reset）、P3-24（通知合并）、P3-25（ECS 分页守卫）、P3-22（DryRun 限速）、P3-09（`_txlock`）、P3-12（权限）

> 验收：配置保存 N 次后熔断器 map 不增长；`-race` 全绿

**历史批次 4 — 状态一致性与错误处理**

P3-05（`no-store`）、P3-10（忽略的 error）、P3-11（`StoreLogWriter` 返回错误）、P3-13（MultiHandler 收集全部错误）、P3-14（400→503）、P3-21（drain + 字节上限）

**历史批次 5 — 死代码与重复实现**

§4 全部"确认可删除"与"高可信删除候选"；`bundle_v3.go` 默认值改用常量、presence 改有序切片

**历史批次 6 — 构建、测试与文档闭环**

Makefile `test`/`vet` 加 `frontend` 前置；修 `waitForNoSMTPData`；消灭 7 个永不失败的测试；补字面量锚点；补构建契约测试；补 `pidfile` 单测；清理约 12 处 "version 2" 与 §4 文档漂移

**依赖关系**：P1-01 的产品语义已确认，不再有配置形态决策前置。P3-25 必须在自动清理前改为不完整快照硬失败；P2-04 代码需按 Step 0 已修订的 AGENTS 发布顺序独立落地；其余死代码与同文件清理应在 Issue7 Step 1～4 完成后再重新证明。

---

## 11. 覆盖矩阵摘要

| 模块 | 主审 | 复核 | 文件数 | 入口/调用链 | 验证 | 未验证 |
|---|---|---|---|---|---|---|
| 启动/生命周期（`main.go`,`run.go`,`app/`） | A | 主代理 | 3 生产 | `main → run → runWebUI → 启动/收尾序列` | 容器 SIGTERM 实测 `ExitCode=0`；双实例 pidfile 实测；**容器陈旧 pidfile 实测复现 P1-02** | 真实部署机 systemd |
| 配置/DB/运行时（`config/`） | A | 主代理 | 7 生产 | `OpenStore → initTables → LoadBusinessSnapshot → BuildRuntimeState` | `config` 包 `-race` 绿；驱动源码核对 | 真实旧库迁移路径 |
| 同步/DNS/重试（`syncer/`,`dns/`,`internal/`） | **B** | **主代理 overlay 探针复现 P0-01/P1-01/P2-01** | 7 生产 | `Run → beginRound → syncAll → runRound → syncDomain → retrySyncDetailed` | `syncer`/`provider`/`dns` 包测试绿 + 3 个判别性探针 | 真实云写入 |
| Provider（4 云） | **B** | 主代理（逐 API 调用点验证） | 8 生产 | `Provider 接口 → 四实现 → SDK 调用` | **确认零全量覆盖 API**；CVM 配额对照官方文档 | 真实云 API |
| 通知/健康（`notifier/`,`internal/health/`） | C | 主代理（推翻 1 项误报） | 7 生产 | `Publish → OnEvent → SMTP/HTTP`；三消费者共用 Checker | `-race` 绿；stdlib `handler.go` 源码核对 | 真实 SMTP/Webhook/Uptime Kuma |
| HTTP/API/SSE（`webui/`,`webui/api/`） | D | 主代理（推翻 1 项误读） | 20 生产 | `Register(28 路由) → handler → Coordinator` | `webui`/`webui/api` `-race` 绿；路由表逐一核对 | 浏览器交互 |
| 前端（`webui/frontend/src/`） | E | 主代理（复核 5 项 P2） | 18 源 | `view → api.ts → Go handler` 逐字段 | `npm run build`/`vue-tsc` 经 lead 门禁通过 | 浏览器人工 |
| 全局/部署/测试 | F | 主代理 | 6 构建文件 + 62 测试 | Makefile/Dockerfile/CI 三套命令对比 | 全部门禁实跑 | 远端 CI/GHCR |

**文件覆盖**：53/53 生产 Go 文件（root 2、app 1、config 7、dns 2、internal/health 3、internal/portconv 1、internal/tag 1、notifier 4、provider 8、syncer 4、webui 2、webui/api 18）**全部读过并归属至少一个主审**；高风险文件均有独立交叉复核。

**流程偏差如实记录**：`e38de1fb`（同步/DNS/Provider）与 `00a69dd7`（全局死代码/部署/测试）两路分派**超出合理时限未按时返回**；我已发出限时收敛请求，并**亲自完成**这两个范围的审核与验证（P0-01/P1-01/P2-01 由我自写探针复现）。因此覆盖无缺口，但这两路的"第二双眼睛"来自主代理而非独立子代理——建议后续针对 `syncer/syncer.go`（971 行）与 `webui/api/bundle_v3.go`（887 行）另做一次独立复核。

---

## 12. 命令与证据

### 门禁（全部 PASS）

| 命令 | 结果 |
|---|---|
| `go build ./...` | PASS（EXIT=0） |
| `go vet ./...` | PASS（EXIT=0） |
| `go test ./... -race -count=1` | **PASS — 12/12 包 ok**（root 15.9s、app 2.3s、config 8.2s、dns 2.0s、health 4.8s、portconv 2.8s、tag 2.5s、notifier 4.4s、provider 5.0s、syncer 43.6s、webui 6.1s、webui/api 16.1s） |
| `gofmt -l .`（排除 node_modules） | PASS（无输出） |
| `git diff --check` | PASS（EXIT=0） |
| `go mod verify` | PASS `all modules verified` |
| `go mod tidy -diff` | PASS（无差异） |
| `npm audit --omit=dev --audit-level=high` | PASS `found 0 vulnerabilities` |
| `npm audit --audit-level=high` | PASS `found 0 vulnerabilities` |
| `docker compose -f docker-compose.yml.example config --quiet` | PASS（EXIT=0） |
| `docker build -f build/Dockerfile -t fwalizer:audit .` | PASS |
| 容器 `User` / `id` | PASS `appuser` / `uid=1000(appuser)` |
| 容器 `wget` | PASS `/usr/bin/wget`（BusyBox） |
| `HEALTHCHECK` 目标 | PASS `wget … /api/health`，30s/3s/10s/3 |
| 容器健康状态 | PASS `healthy` |
| `GET /api/health` | PASS `{"status":"ok"}` |
| `GET /api/health/operational` | PASS `{"status":"ok","reasons":[]}`（200） |
| 容器内嵌 SPA `/` | PASS 返回 index.html |
| `docker stop` + 退出码 | PASS 0.125s / `ExitCode=0` / `OOMKilled=false` |

### 判别性验证（为证实/证伪具体结论而运行）

| 实验 | 结果 |
|---|---|
| **overlay 探针：Aliyun 端口 key 往返** | **P0-01 复现**：`ToAdd=1 ToDelete=1`（期望 0/0） |
| **overlay 探针：空 comment desc 碰撞** | **P1-01 复现**：B 域名把 A 的 `1.1.1.1/32` 列入 `ToDelete` |
| **overlay 探针：IPv6+ICMP key** | **P2-01 复现**：Lighthouse `ICMPv6` 与 CVM `ICMPV6` 均 1/1 |
| **持久卷 + 预置陈旧 pidfile `1` 启动容器** | **P1-02 复现**：`exited exit=1`，日志「FWAlizer 已在运行 (PID: 1)」。**更正**：首次用 `--volumes-from` 未能复现（卷未复用），此为受控重测结果 |
| `git archive HEAD` 抽取清洁副本 → `go build/vet/test ./...`、`make vet` | **EXIT=1/1/1/2** `pattern frontend/dist: no matching files found`（构建阻断） |
| 同上 + 手动 `mkdir webui/frontend/dist` → `go build ./...` | EXIT=0（定位根因） |
| 顺序启动两个实例（同一 `FWALIZER_DATA_DIR`） | 第二个被拒并提示 PID → pidfile 顺序保护**有效** |
| **并发**启动两个实例 | 一个退出、一个运行 → **未复现** TOCTOU 双实例（据此定为 P3-08 而非更高） |
| 遍历 provider 全部 SDK 调用点 vs 禁用 API 名 | 仅 Create/Delete/Authorize/Revoke/Describe，**零** `ModifyFirewallRules`/`ModifySecurityGroupPolicy`/reset |
| Go stdlib `log/slog/handler.go`：`TextHandler.Handle` 是否按 level 过滤 | **不过滤**（过滤只在 `commonHandler.enabled`）→ 驳回相应误报 |
| `modernc.org/sqlite@v1.54.0/tx.go:23` | `if !opts.ReadOnly && c.beginMode != ""` → 证实 `_txlock` 注释理由不成立、mitigation 安全（P3-09） |
| `查询用户安全组配额.md:62` / `安全组添加规则.md:18` | `SecurityGroupPolicyLimit: 100` + "一次请求只能创建单个方向" → 支撑 P3-06 |
| `grep 'delete(' dns/circuitbreaker.go` | **零命中** → 证实 P3-01 永不淘汰 |
| `grep io.ReadAll/LimitReader/Body.Close/NewTicker/go func` | 各 1/2/2/4/6 处，逐处核对均闭合 |
| fd 探针（5000 次注册，`/dev/fd` 计数） | `fd_before=5 fd_after=5` → 无 fd 增长 |
| 前端 8 字段载荷 vs Go DTO 逐字段比对 | **一致**（`webui/api/test_email.go:24-33` ↔ `Alerts.vue:66-75`） |
| `grep NModal` 各 view + 删除按钮绑定 | Targets/Rules 删除**直接绑定 DELETE** → 证实 P2-07 |

---

## 13. 证据边界与未验证项（**不得外推为通过**）

下表同时包含“未执行”“历史证据不可外推”“已执行但范围有限”和“不适用”。每行状态必须按原文理解；只有明确写为当前 revision、当前环境已完成的证据，才能支持相应范围内的结论。

| 边界 | 状态 |
|---|---|
| **真实腾讯云 / 阿里云 API** | **未执行**。P0-01 与 P2-01 的 key 不对称在 **provider 层与"云端回传形态无关地"**被证明；但真实云端的实际回传、CVM 配额方向口径、ECS 是否真的拒绝 >100 个 RuleId、SWAS Remark 50 上限均**未实测** |
| **真实 SMTP 服务器接受** | **未执行**。仅使用假 SMTP |
| **真实收件箱投递**（含中文主题的 MTA 编码表现，P3-20） | **未执行**。用户已于 2026-09-27 决定跳过并自行处理 |
| **真实 Webhook**（P2-05 的依据来自官方文档与公开同类报告，**非本项目实测**） | **未执行**。用户已决定跳过 |
| **真实 Uptime Kuma HTTP Monitor** | **未执行** |
| **真实 Uptime Kuma Push DOWN/恢复通知**（含 P2-04 的实际触发概率） | **未执行**。P2-04/P3-03 为代码与时序分析结论 |
| **浏览器人工检查**（PT-B7-07；布局、按钮尺寸、重复 key 的实际 patch 行为、侧边栏高亮、SSE 重连表现） | **未执行**。本环境无浏览器工具；前端结论均为源码级 + 一条 naive-ui 源码取证 |
| **Docker 容器运行** | **已执行**（healthy / uid 1000 / ExitCode 0 / SIGKILL 后重启复现 P1-02）。但**真实负载下"完成当前轮次再退出"未验证**（本轮为空库轮次） |
| **远端 CI / GHCR** | **未执行**。既有 `v2.0.0`（run `36300428681`）结果属**更早 revision，不能证明当前改动** |
| 真实 DNS 上游（`dns/resolver_test.go` 两用例无网络时 `t.Skip`） | **未执行** |
| SQLite 真实 BUSY/慢盘争用下的 2s 探活上限端到端保证 | **未执行**（仅核对驱动有 `interruptOnDone`、`Store` 无互斥量） |
| 真实旧库迁移路径 / 生产 SQLite | **未执行** |
| `GOOS=windows` 运行验收 | **不适用**（用户已决定移除支持，仅以构建失败为证据） |
| 后端 `syncer/syncer.go` 与 `webui/api/bundle_v3.go` 的**第二个独立子代理**复核 | **未完成**（原分派超时，由主代理亲自覆盖替代） |

**结论**：`go test` 通过只证明对应测试覆盖的行为，**不得**外推为真实云、真实 SMTP、真实 Webhook、真实 Uptime Kuma、浏览器或远端 CI 已通过。

---

## 14. 审计快照的 Git 完整性（历史记录）

| 项 | 审核前 | 审核后 |
|---|---|---|
| `git rev-parse HEAD` | `11918fb945fe9dfe2a86ead5bc833b14dd156a68` | **同一值** |
| `git status --short --branch` | `## main...origin/main`（干净） | 干净；**本报告文件为新增的未跟踪文件**（见下） |
| tracked 文件改动 | 0 | **0** |
| `git diff --stat` / `--cached --stat` | 空 | **空** |
| ignored 产物 | `fwalizer`、`webui/frontend/dist/`、`webui/frontend/node_modules/` | **完全相同的 3 项**（均系审核前已存在，未新增） |
| 审核产生的临时产物 | — | **全部清理**：overlay 探针目录、清洁检出副本、pid/fd 探针目录、`fwalizer:audit` 镜像、测试容器与命名卷均已删除 |

**说明**：`fwalizer-audit-final1.md`（本文件）由用户明确要求写入项目根目录，因此它是**审核过程之外新增的未跟踪文件**；`.gitignore` 中没有任何规则匹配它，故它会出现在 `git status` 的未跟踪列表中。若你希望它不出现在 `git status`，可在 `.gitignore` 增加一行 `fwalizer-audit-*.md`（**该修改需你另行确认**，本次未执行）。

**明确确认：本次代码审核未修改、未创建、未删除任何受版本控制的仓库文件，未执行任何 state-changing git 命令，未触碰真实云/SMTP/Webhook/Uptime Kuma，未升级依赖，未安装任何全局工具。** 探针均经 `go test -overlay` 虚拟映射运行，仓库零写入。

---

## 附录 A：本报告相对上一版的差异

| 变更 | 内容 |
|---|---|
| **分级更正** | 由"P0=0 / P1=2 / P2=6 / P3=33"更正为 **P0=1 / P1=2 / P2=9 / P3=25** |
| 新增 P0-01 | 阿里云端口比较 key 不对称（已探针复现） |
| 新增 P1-01 | 空 comment 导致 desc 碰撞互删（已探针复现） |
| 新增 P2 项 | IPv6+ICMP key、ECS 删除不分批、静默丢弃云端能力限制、Dry Run key 重复、仪表盘 idle 误报（后两项由上一版的泛 P2 归位） |
| 新增 P3 项 | `IsOpen` 无控制流效果、DryRun 过度限速、ticker Reset 语义、通知合并、ECS 分页守卫、邮件报文细节 |
| 剔除误报 | `TextHandler` 二次过滤、发布顺序"与文档相反"、4 项误删候选、SPA fallback |
| 更正表述 | P0-01 影响由"每轮全量替换/净规则丢失"更正为"同一条规则被删+建，**非净丢失**；净丢失需与 IP 变更叠加" |
| 方法学更正 | P1-02 首次测试（`--volumes-from`）未能复现，改用持久命名卷后确认；已在正文如实记录 |
| 纳入决策 | 用户 7 项决策写入第 9 节与相应 finding |
| P1-01 状态继续更新 | 2026-09-28 的“待决策/已冻结”已于 2026-09-29 被第 8 节最终方案取代；当前以 Issue7 Step 0 已完成、Step 1～5 待实施为准，`maxTagRunes` 仍为 48，SWAS `Remark` 上限仍登记为 PT-B7-09 |

## 附录 B：规则身份专题（已移至正文第 8 节）

> 本附录的内容已**整体移入第 8 节「规则身份与 TAG 所有权」**，以避免同一议题在文档内出现两处。
>
> **当前摘要**：P1-01 已探针复现，并已定案将“个体识别”从 description/comment 上移除，改为目标级完整期望集与 canonical functional key；TAG 仍为唯一操作授权，`maxTagRunes` 仍为 48。实施状态见 Issue7。
>
> 完整内容请看 **第 8 节**：8.0 为最终方案；8.1～8.7 保留定案前的机理、候选与不确定性历史；8.8 记录当前决策状态。
