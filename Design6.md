# Design6 — EdgeOne 回源同步与 DNS 协同

> 2026-11-12 · 已确认方案的设计记录 · 实施状态见 [Build8](Build8.md)。
> 基线为 `beta` 的 `102d838f3bcc3d1624c32e44ed65f2eb6feaa0b4`。当前约束见 [AGENTS.md](AGENTS.md)，历史依据见 [Design5](HistoryDocs/Design5.md)、[Build7](HistoryDocs/Build7.md) 和 [Issue7](HistoryDocs/Issue7.md)。本文描述目标行为，现有发布和验收状态仍以仓库记录为准。

[冻结合同](#frozen-contract) · [SQLite 与迁移](#schema-reference) · [API 与配置包](#api-reference) · [TEO 客户端](#teo-reference) · [预检与调度](#preflight-reference) · [目标写入和确认](#target-reference) · [界面与健康](#view-reference)

## 一、功能与默认值

在左侧栏「域名规则」后新增「EdgeOne 回源」模块，路由为 `#/edgeone`。一个 `ZoneId` 对应一条站点配置，可绑定多个「云资源管理」中的目标，并指定入站协议、端口和地址族。绑定清单代表该站点的全部实际源站。腾讯云的七层域名和四层代理绑定信息可供核对，实际源站范围由配置者确定。选择安全组时，页面说明规则影响整个安全组及其关联实例。

| 设置 | 默认值与行为 |
|---|---|
| 总暂停 | 保留现有 `sync_enabled` 和仪表盘入口，管理两项正式同步的准入。恢复时仅调度已启用的功能。 |
| DNS 同步 | 新实例启用，沿用现有 DNS 间隔 `5m`。可单独关闭。 |
| EdgeOne 同步 | 新实例关闭。启用后，已绑定目标的站点进入调度；首次启用和新增站点安排首轮。 |
| DNS TAG | 新实例和明确清空配置后的默认值为 `auto-ddns`。已存实例的有效 TAG 保留。 |
| EdgeOne TAG | 默认 `edgeone`，在「全局设置」中可自定义，使用现有 TAG 格式和长度校验。 |
| EdgeOne 同步间隔 | 在「全局设置」中可自定义，默认 `72h`，使用现有正时长语法。它控制正常站点查询周期，与 DNS 间隔分别计时。 |

两种 TAG 去除首尾空白后按现有大小写敏感的所有权语义比较。相同值在保存、导入和完整快照校验时拒绝。描述统一使用现有 `[TAG]` 渲染，删前继续严格检查所有权。EdgeOne 首版按无历史 EdgeOne 规则的新功能设计。

## 二、持久化与兼容

- SQLite 保存站点及其目标引用，站点的 `ZoneId` 唯一，绑定至少一个有效目标。重复本地目标和指向同一实际资源的别名按云类型、地域和资源标识识别。多个站点可以指向同一实际目标，规划时聚合所有适用站点。删除被引用的目标沿用 HTTP 409，导入后按 `export_id` 重映射引用。站点、设置及目标校验复用配置协调器，校验和构造候选状态成功后一次提交与发布。
- 旧数据库中 `tag` 缺失或为空白时，有效值原为 `auto-dns`。在首次升级事务中识别已初始化旧库，显式写入原有效值并标记迁移完成。新建库、重启和 reset 各自验证，避免 reset 后又被识别成待迁移旧库。显式保存的 DNS TAG 原样承接其归一化值，云规则由现有描述继续归属。
- 计划导出 version 4，继续严格读取现有 version 3。v4 保存两个功能开关、两个 TAG、两个同步间隔及站点绑定。v3 导入保留原 DNS TAG、DNS 间隔和总暂停，补入「DNS 启用、EdgeOne 关闭、无站点」。如果旧配置原本把 DNS TAG 设为 `edgeone`，升级或 v3 导入时保留 DNS TAG，给新模块初始化为 `edgeone-origin`。显式 v4 重名按校验失败处理。v1、v2 和未知版本继续拒绝。
- 已查实的全局冲突和仍需保护的网段过渡状态保存在本机 SQLite 运行记录中。它们与导出备份的用户配置分开。普通保存、导入和重启后通过正式预检重新判定，显式清空配置时清理对应运行记录。

## 三、EdgeOne 查询与计划

使用腾讯云 TEO Go SDK，按 `ZoneId` 调用 `DescribeOriginACL` 和 `ConfirmOriginACLUpdate`。中国站为 `POST https://teo.tencentcloudapi.com/`，JSON 业务参数为 `ZoneId`，API 版本 `2022-09-01`，地域可省略，签名使用腾讯云 API 3.0 的 `TC3-HMAC-SHA256`。配置查询和确认所需的站点级 CAM 权限，复用现有腾讯云凭据设置。客户端需有请求超时，去除日志中的凭据；查询和确认按腾讯云频率限制调度，站点查询结果供其所有目标复用。 [查询接口](https://cloud.tencent.com/document/product/1552/120408) · [确认接口](https://cloud.tencent.com/document/product/1552/120409) · [签名方法](https://cloud.tencent.com/document/product/1552/73159) · [CAM 权限](https://cloud.tencent.com/document/product/598/99327)

- 初次同步使用 `CurrentOriginACL.EntireAddresses`。有待更新版本时，采用当前版与 `NextOriginACL.EntireAddresses` 的并集，保留版本及生效时间。校验状态、字段完整性和地址族，规范 CIDR 掩码并去重。明确无下一版是有效状态；读取失败、缺少必要字段、无有效网段和非法前缀进入查询失败状态。
- 独立调度保留每项最近结果和下次到期时间。实际改变某项间隔时，从处理该变更开始重新计时；保存相同间隔保留剩余等待，EdgeOne 间隔变更仅调整 EdgeOne 调度。正常周期、首次启用、明确的立即同步、失败重试和确认前查询由同一执行入口协调。配置通知合并、过短间隔下的请求合并和 API 限速都需验证。默认 `72h` 对应腾讯云建议的查询周期，定时到期和请求实际发出时间受在途工作与限速影响。 [腾讯云更新流程](https://cloud.tencent.com/document/product/1552/76086)
- 既有 Provider 继续负责防火墙读写。规划输入支持保留前缀掩码的地址类型，DNS 地址仍规范为 `/32` 或 `/128`。按物理目标串行使用同一规则快照和容量，按功能 key 去重，按 TAG 确定创建与删除归属。一个目标上 EdgeOne 的清理依据包括所有适用站点，即使其中部分站点本次未到查询时间。四平台的能力、S0/S1/S2、腾讯云版本和阿里云稳定删除定位继续生效。 [目标规划器](provider/plan.go) · [目标同步](syncer/target.go)

## 四、写入前全局检查与自动恢复

当 DNS 和 EdgeOne 同时启用时，在第一个目标写入前，从同一运行时配置取所有适用 DNS 规则的解析地址，并为每个有效 EdgeOne 站点合并当前版、待更新版和过渡中仍可能用于回源的受保护网段。按 IPv4/IPv6 的 CIDR 包含关系判断。DNS 的 IPv4 地址在比较时使用 IPv4 表示，保留既有云端快照地址身份。协议、端口、动作和目标差异均不影响全局重叠判定。没有适用规则或站点时可得到有效空集；查询失败导致的未知集合单独标记。

| 观测 | 正式同步决策 |
|---|---|
| 单项启用 | 只运行已启用功能及其查询，使用该功能的现有安全门。 |
| 两项启用，任一结果证明重叠 | 记录域名、IP、网段和 `ZoneId`，两侧新增、删除及站点确认均为零。部分查询失败时，已取得的正向冲突证据仍生效。 |
| 已查实重叠，后续数据不足或继续重叠 | 保持两侧写入停止，继续只读复查。进程内保留已知冲突，重启时恢复已持久化的状态。 |
| 已查实重叠，后续两侧完整且无重叠 | 自动解除冲突。定时或手动触发均可完成复查，并按调度恢复已启用的功能。 |
| 无已查实的冲突，一侧数据不足 | 停止失败侧，正常侧继续按自己的安全门写入。DNS 失败时，正常的 EdgeOne 仍可在全部绑定目标验证和版本重查通过后立即确认。状态明确标记为数据不足、单侧继续。 |
| 总暂停 | 阻止新一轮正式写入与站点确认，已准入轮沿用现有收尾语义。模拟测试仍可只读运行。 |

已知冲突只在两项同时启用时约束写入。关闭一项可继续另一项；重新同时启用时先复查，状态不足时保留已知冲突。正常 DNS 轮可使用有效的 EdgeOne 查询快照；快照随站点、凭据及计划切换状态失效。冲突判定的覆盖范围是本轮可用数据。允许单侧继续意味着后续可能发现重叠，届时阻止后续写入，保留已经成功写入的规则和实际确认记录。

两项同时启用时，目标重试使用本轮已做全局判定的地址，重新读取云规则、版本和删除定位。下一轮重新解析 DNS。仅启用 DNS 时保持原逐 attempt 解析。EdgeOne 查询失败和地址冲突使用各自的错误状态，DNS 熔断计数继续依据真实解析结果。 [现有调度](syncer/syncer.go) · [DNS 轮次](syncer/dns_round.go)

## 五、覆盖验证、站点确认与旧网段清理

1. 先增量添加，再回读。另一功能 TAG 下的等价残留规则作为目标级冲突报告，禁止把它计入覆盖或改标。手工规则保持既有精确等价和安全删除边界。平台不支持所需地址族、目标容量不足、相关相反动作冲突或读回缺失时，该站点保留待处理状态。
2. 按 `ZoneId` 聚合全部绑定目标的回读证明。同一实际资源只验证一次。`CoverageReady` 只是输入之一，还需验证该站点所需的全部规则及相关冲突。确认前重查站点、当前及待更新版本和两者的规范化网段，校验绑定和相关配置仍有效；已暂停或已关闭站点的确认准入同时复核。通过后自动调用确认，按站点记录结果和 `RequestId`。接口只接受 `ZoneId`，重查与确认之间存在版本变化窗口。 [确认接口](https://cloud.tencent.com/document/product/1552/120409)
3. 确认超时、响应不确定或返回版本已最新时，先读取实际站点状态再决定是否需要继续。站点确认和新版生效分别记录。确认成功、计划时间到达、下一版字段消失和服务重启均不足以单独授权清理。待有效查询证明新版成为当前版本后，在现有安全门内清理过时的 EdgeOne TAG 规则。过渡期间继续保护仍可能使用的网段。腾讯云可能在期限后强制切换，实际状态由查询确认。 [源站防护与更新流程](https://cloud.tencent.com/document/product/1552/76086)

## 六、界面、状态与验收边界

- 「EdgeOne 回源」提供站点与多目标配置、模块开关、版本、最近查询及下次同步、目标覆盖、确认状态和错误。「全局设置」提供 DNS 开关、两种 TAG 及 EdgeOne 间隔。「模拟测试」显示两项功能的有效计划、全局冲突、预计新增及过渡期容量、目标限制。模拟使用正式检查和规划逻辑，保持只读且独立于总暂停，模拟结果本身不改变正式冲突状态。
- 区分无任务、已暂停、地址重叠、数据不足单侧继续、站点待确认与已确认。EdgeOne 的低频等待使用自己的到期时间，运行健康继续使用相应功能的状态，避免把低频等待误报为 DNS 停滞。已查实的冲突保持可见，恢复时记录一次状态变化。
- 本阶段的本地回归、真实站点与目标的权限和响应、跨平台容量以及发布各自记录证据。当前只有设计和基线核验，实施与验收清单见 [Build8](Build8.md)。

## 七、实现参考

下列 SQL、字段与代码用于指导 Step 实施。现有可复用入口以链接指向的仓库文件为准。跨层流程片段中的新函数名是建议接线，不要求原样复制。

<a name="schema-reference"></a>

### 7.1 SQLite 表与迁移

#### 业务配置

沿用 `settings(key TEXT PRIMARY KEY, value TEXT NOT NULL)`，与既有设置一样保存字符串。总暂停仍由专用端点写入。导入和完整快照保留既有告警、监控及凭据字段。

| SQLite key | 新库 / reset | 值及语义 |
|---|---|---|
| `sync_enabled` | `true` | 现有总开关。 |
| `dns_enabled` | `true` | DNS 功能开关。 |
| `edgeone_enabled` | `false` | EdgeOne 功能开关。 |
| `tag` | `auto-ddns` | DNS TAG，沿用现有键。 |
| `interval` | `5m` | DNS 间隔，沿用现有键。 |
| `edgeone_tag` | `edgeone` | EdgeOne TAG。 |
| `edgeone_interval` | `72h` | EdgeOne 同步间隔，沿用现有正时长解析。 |

建议将站点参数存在主表，目标绑定单独存行。首版一站配置一组协议、端口和地址族，所有绑定目标使用这一组参数。站点的防火墙动作固定为 `ACCEPT`。协议沿用既有规则枚举，ICMP 的端口沿用 `ALL` 语义。地址族用 `ipv4`、`ipv6`、`dual` 表示，表单初始选 IPv4，实际使用双栈的站点应选择双栈。保存时检查端口表达式有可用项，防止空输入经现有 `portconv.Parse` 回退成 `ALL`。 [领域校验](config/validate.go) · [端口解析](internal/portconv/portconv.go)

```sql
CREATE TABLE IF NOT EXISTS edgeone_sites (
    zone_id        TEXT NOT NULL PRIMARY KEY CHECK (length(trim(zone_id)) > 0),
    protocol       TEXT NOT NULL CHECK (protocol IN ('TCP', 'UDP', 'TCP+UDP', 'ICMP')),
    ports          TEXT NOT NULL CHECK (length(trim(ports)) > 0),
    address_family TEXT NOT NULL CHECK (address_family IN ('ipv4', 'ipv6', 'dual'))
);

CREATE TABLE IF NOT EXISTS edgeone_site_targets (
    zone_id   TEXT    NOT NULL,
    target_id INTEGER NOT NULL CHECK (target_id > 0),
    PRIMARY KEY (zone_id, target_id)
);

CREATE INDEX IF NOT EXISTS idx_edgeone_site_targets_target_id
    ON edgeone_site_targets (target_id);
```

`edgeone_site_targets.zone_id` 指向 `edgeone_sites.zone_id`，`target_id` 指向现有 `targets.id`。沿用 Store 的事务内引用校验，并在 `LoadBusinessSnapshotTx` 用同一事务的目标集合验证完整闭包。一个站点必须至少有一条有效绑定，空数组不表示全部目标。相同本地 ID、同一实际资源的别名均在同一站点的请求中拒绝；不同站点可以共享实际目标。目标更新如果会让已有站点的两个绑定落到同一资源，也须在最终候选快照中拒绝。历史配置中同一目标存在多个本地记录时，正式规划按实际资源合并，不据此改写存量 DNS 引用。 [Store](config/store.go) · [目标 API](webui/api/targets.go) · [配置协调器](webui/api/coordinator.go)

站点创建、修改绑定和删除使用同一写事务。修改站点配置时将 `zone_id` 当作标识，路径标识与请求体冲突返回 400；要改绑另一站点，按新站点配置。删除站点先删绑定、再删站点及其过渡记录。总 reset 与完整导入先删除站点绑定，再删除站点和目标；完整导入时本机已知全局冲突继续由有效预检解除。运行记录保留与清除规则见下节。数据库删配置不直接发云防火墙请求。

#### 本机运行记录

```sql
CREATE TABLE IF NOT EXISTS sync_address_conflict (
    id               INTEGER PRIMARY KEY CHECK (id = 1),
    dns_name         TEXT NOT NULL,
    dns_address      TEXT NOT NULL,
    edgeone_prefix   TEXT NOT NULL,
    zone_id          TEXT NOT NULL,
    first_seen_at    TEXT NOT NULL,
    last_seen_at     TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS edgeone_origin_transitions (
    zone_id                 TEXT NOT NULL PRIMARY KEY,
    base_version            TEXT NOT NULL,
    pending_version         TEXT NOT NULL,
    pending_cidrs_json      TEXT NOT NULL,
    protected_cidrs_json    TEXT NOT NULL,
    confirmation_state      TEXT NOT NULL CHECK (
        confirmation_state IN ('not_attempted', 'unknown', 'accepted')
    ),
    confirmation_request_id TEXT NOT NULL DEFAULT '',
    updated_at              TEXT NOT NULL
);
```

- 正式预检查实重叠时，先在进程内设为已知冲突，再保存一条稳定排序的证据到 SQLite。首次写库失败仍保留进程内状态，报告存储错误，并在后续正式复查重试保存。两项同时启用时，进程内状态与 SQLite 已有记录任一表明存在冲突，就继续执行已知冲突规则。清除操作失败时也保留内存状态，完整有效数据证明无重叠并成功提交解除后才恢复写入。
- 冲突表缺行只表示没有已持久化的冲突。启动时加载已提交记录；表读取失败时两项同时启用的正式轮保持停止，等状态重新可读。若进程在首次记录提交前退出，该次观察无法从 SQLite 恢复，启动仍按读到的记录和当轮查询重新判定。站点移除时可保留已有记录作为历史证据，直至正式复查证明解除。日志文本、UI 状态和配置包均不作为冲突记录的恢复来源。
- 过渡表的 `base_version` 是发现待更新时的当前版本，`pending_version` 和 `pending_cidrs_json` 表示最新待更新版本及其规范前缀。 `protected_cidrs_json` 保存本次过渡仍需保留的前缀集合。两个 JSON 字段均为稳定排序、去重、非 null 的 CIDR 字符串数组。只在过渡或确认状态变化时写入，查询历史继续使用现有日志体系。站点删除或总 reset 可清除对应记录；完整导入对仍存在的同一站点保留过渡保护，移除不存在站点的记录。配置变化后的旧结果不能写回新配置的状态。
- UTC RFC3339 文本用于运行记录的时间戳。读取坏记录应报告状态不可用并保守保护相关云规则。运行表不存凭据、签名或 SDK 原始载荷，也不进入 v4 配置包。
- 将短事务用于本机状态写入。云查询、S0/S1/S2 和确认调用都在事务外执行。写前、发布前核对操作捕获的配置及站点身份。采用现有锁顺序和 SQLite 事务入口。 [SQLite 事务入口](config/store.go) · [运行时发布](syncer/state.go)

#### TAG 迁移参考

现有 `initTables` 先建表再迁移。首次迁移前在同一个事务内读取 `sqlite_schema`，确认 `settings` 是否已存在，再判断一次性迁移标记。基线未使用 `PRAGMA user_version`，可用它记录这次 Schema 迁移，值由实施时统一分配。标记、实际写入和建表必须同事务提交。 `strings.TrimSpace` 用于识别旧空白值，保持和现有读取行为一致。

```text
begin immediate transaction
  inspect settings table existence and migration version
  create/upgrade schema
  if this migration is new:
      if previous settings table existed and old tag is missing or blank:
          persist tag = "auto-dns"
      persist migration version
  commit
```

新库与已迁移库的 reset 通过新默认值解析为 `auto-ddns`，reset 保留 Schema 迁移标记。旧库有非法非空 TAG 时沿用现有校验错误，保持显式值和隐式默认的区别。阶段升级或 v3 导入时需要生成兼容 EdgeOne TAG，只有当原 DNS TAG 与 `edgeone` 相同才用 `edgeone-origin`。用户明确提交的 v4 配置重名直接报错。 [当前建表迁移与设置加载](config/store.go) · [TAG 校验](config/validate.go)

<a name="api-reference"></a>

### 7.2 配置、状态与备份接口

| 方法和路径 | 预期接口行为 |
|---|---|
| `GET /api/settings` | 保留现有字符串映射，增加两个功能开关、EdgeOne TAG 和间隔。 |
| `PUT /api/settings` | 继续用严格部分更新 DTO。全局开关仍由暂停和恢复端点处理。 |
| `GET /api/edgeone/sites` | 返回 `{"sites":[]}` 形状；含配置的列表按 `zone_id` 稳定排序。 |
| `POST /api/edgeone/sites` | 创建站点，成功返回 201 与站点配置，已存在返回 409。 |
| `PUT /api/edgeone/sites/{zone_id}` | 全量替换该站点的规则参数与绑定，成功返回 200，缺站点返回 404。 |
| `DELETE /api/edgeone/sites/{zone_id}` | 按现有配置删除端点风格返回 200，缺站点返回 404。 |
| `GET /api/edgeone/status` | 返回站点运行状态，不向腾讯云发请求。包含当前/待更新版本、最近查询、目标验证、计划时间和确认结果。 |
| `GET /api/sync/status` | 原字段继续可读，新增 `dns`、`edgeone` 的开关和调度摘要，以及全局预检状态。 |
| `POST /api/sync/trigger` | 排队运行已启用功能，沿用 202、总暂停时 409 与去重。 |
| `POST /api/sync/dryrun` | 只读取得计划和预检结果，保留现有并发保护及 `results`、`warnings`。 |
| `POST /api/config/export` / `POST /api/config/import` | 导出 v4，导入严格 v3 或 v4。 |

复用 `decodeJSONStrict`、`writeJSON` 及现有错误映射。类型错误、字段缺失、空绑定和无效目标 ID 走请求校验；站点路径作为不可变标识，去除首尾空白并拒绝空值、控制字符和路径分隔符，保留腾讯云 ID 的大小写。写入失败返回既有错误形状，成功返回前完成事务提交与候选快照发布。站点缺权限、配额不足和查询失败是运行结果，在页面和模拟中显示。 [路由与 JSON 写入](webui/api/deps.go) · [严格解码](webui/api/decode.go) · [设置 DTO](webui/api/settings.go)

```json
{
  "zone_id": "zone-example",
  "target_ids": [12, 19],
  "protocol": "TCP",
  "ports": "80,443",
  "address_family": "ipv4"
}
```

配置设置 API 沿用字符串映射，示例是一次部分更新。

```json
{
  "dns_enabled": "true",
  "edgeone_enabled": "true",
  "edgeone_tag": "edgeone",
  "edgeone_interval": "36h"
}
```

配置包中的开关使用 JSON boolean，目标关系使用 `target_export_ids`。以下是 v4 完整形状的脱敏示例。 `export_id` 可与当前数据库 ID 不同。没有站点时 `edgeone_sites` 写 `[]`。

```json
{
  "version": 4,
  "metadata": {"exported_at": "2026-11-12T00:00:00Z"},
  "targets": [
    {"export_id": 3, "cloud_type": "tc_cvm", "region": "ap-guangzhou", "resource_id": "sg-example"},
    {"export_id": 5, "cloud_type": "ali_ecs", "region": "cn-hangzhou", "resource_id": "sg-example2"}
  ],
  "rules": [],
  "edgeone_sites": [
    {
      "zone_id": "zone-example",
      "target_export_ids": [3, 5],
      "protocol": "TCP",
      "ports": "80,443",
      "address_family": "ipv4"
    }
  ],
  "settings": {
    "credentials": {
      "tencent": {"secret_id": "", "secret_key": ""},
      "aliyun": {"access_key_id": "", "access_key_secret": ""}
    },
    "tag": "auto-ddns",
    "interval": "5m",
    "dns": "223.5.5.5",
    "dns_timeout": "10s",
    "dns_fail_threshold": 5,
    "log_level": "info",
    "sync_enabled": true,
    "dns_enabled": true,
    "edgeone_enabled": false,
    "edgeone_tag": "edgeone",
    "edgeone_interval": "72h",
    "theme": "light"
  },
  "alerts": {
    "policy": {
      "dns_failed_enabled": false,
      "sync_error_enabled": false,
      "operational_error_enabled": false,
      "health_timeout": "10m"
    },
    "email": {
      "enabled": false,
      "host": "",
      "port": "587",
      "security": "compat_starttls",
      "username": "",
      "password": "",
      "from_addr": "",
      "to_addr": "",
      "subject": "[FWAlizer] 告警通知",
      "body": "FWAlizer 检测到运行异常，请检查同步日志。"
    },
    "webhook": {"enabled": false, "url": "", "channel": ""}
  },
  "monitoring": {"uptime_kuma_push": {"enabled": false, "url": "", "interval": "60s"}}
}
```

严格解码先识别 `version` 再校验该版本完整结构，沿用现有请求上限、重复字段及非法 JSON 校验。v3 按自己的字段集合验证，随后转换到共用的已校验配置模型。v4 显式字段全部要求存在，保留有效的 `false`、空列表及允许的空凭据。配置导入沿用 `MutateImport`，顺序为预校验、清理业务配置、插入目标并建立映射、插规则与站点绑定、写完整设置及告警、读完整快照、提交发布。 [v3 参考实现](webui/api/bundle_v3.go) · [必填字段检查](webui/api/bundle_presence.go) · [导入导出入口](webui/api/export.go)

<a name="teo-reference"></a>

### 7.3 TEO 客户端与领域对象

TEO 是地址来源，云防火墙 Provider 继续使用现有实现。业务模型与 SDK 指针类型隔离。站点配置和运行查询结果分别保存，`RuntimeState` 只携带深拷贝的配置和本地构造的客户端引用。SDK 模块版本和现有 `common` 的依赖关系由 Step 5 核对。

```go
// 配置对象参考，建议放在 config。TargetIDs 深拷贝后才发布。
type EdgeOneSite struct {
    ZoneID        string `json:"zone_id"`
    TargetIDs     []int  `json:"target_ids"`
    Protocol      string `json:"protocol"`
    Ports         string `json:"ports"`
    AddressFamily string `json:"address_family"`
}

// 只读 TEO 结果参考，Prefix 已经 Masked，地址族已验证。
type OriginACLVersion struct {
    Version  string
    Prefixes []netip.Prefix
}

type OriginACLSnapshot struct {
    ZoneID          string
    Current         OriginACLVersion
    Next            *OriginACLVersion
    PlannedActiveAt *time.Time
    ObservedAt      time.Time
    RequestID       string
}
```

签名调用使用腾讯云 SDK 的 context 入口。下面省略公共的限速、日志脱敏与 SDK 错误转换，`ctx` 由调用方提供请求预算。

```go
// import common、profile，以及 teo "…/tencentcloud/teo/v20220901"。
clientProfile := profile.NewClientProfile()
clientProfile.HttpProfile.Endpoint = "teo.tencentcloudapi.com"
client, err := teo.NewClient(common.NewCredential(secretID, secretKey), "", clientProfile)
if err != nil {
    return err
}
req := teo.NewDescribeOriginACLRequest()
req.ZoneId = common.StringPtr(zoneID)
resp, err := client.DescribeOriginACLWithContext(ctx, req)
// err 为 nil 后继续检查 resp.Response.OriginACLInfo 和必要字段。
```

| 输入或事件 | 转换与调度动作 |
|---|---|
| 源站防护有效、当前版完整、无下一版 | 返回 `Next=nil`，当前版参与规划。 |
| 当前版和下一版完整 | 保存版本与规范网段。相同前缀去重；`AddedAddresses`、`RemovedAddresses` 可展示，规则依据使用完整列表。 |
| 源站防护未启用、当前版缺失、下一版存在但字段不全、CIDR 不合法或无有效网段 | 作为查询错误处理，禁止将它解释为新空集合或清理授权。 |
| 明确合法的空地址族 | 按有效空列表读入；若用户选中的必要地址族缺失，站点本次无法取得覆盖证明。 |
| 权限拒绝、超时、限流 | 分类记录。失败查询不刷新有效期；只有仍符合当前站点、凭据和时间边界的旧快照可继续参与检查。 |
| 配置换站点、换凭据或已到刷新时间 | 重新查询。到达已知计划切换时间时，按实际查询结果推进状态。 |

同一轮一个站点只准备一份正式地址集，供全部目标和全局判定共用。确认前的重查使用独立请求，获取的版本若变化，放入后续正式轮重新规划。只读模拟可按请求获取地址用于预览，正式缓存、下一次到期和恢复状态由正式执行管理。 [TEO 查询与响应示例](https://cloud.tencent.com/document/product/1552/120408)

<a name="preflight-reference"></a>

### 7.4 全局预检、状态恢复与调度参考

预检输入同时保存成功部分和完整性。存在可适用规则但解析失败是未知；没有适用规则是完整空集。全局检查读取适用域名实际取得的 A/AAAA，以及站点当前版、下一版和过渡记录中尚不能排除仍用于回源的保护前缀。目标地址族筛选单独作用于同一轮的规划输入，即使某域名规则关闭 IPv6 写入，全局检查仍使用该域名实际取得的有效 AAAA。检测输入以正规 IPv4/IPv6 表示，IPv4 DNS 结果执行 `Unmap`，保留已有云规则读取的 IPv4-mapped 身份合同。地址全局重叠判断采用 CIDR 包含关系，目标覆盖仍按规范功能 key 精确匹配。

例如 A 仍是当前版、已铺设的 B 从 `Next` 暂时消失，B 仍参加预检。查询失败时仍可用保留前缀证明正向重叠，整份站点输入继续标为不完整。有效查询证明受跟踪的新版已成为当前版且网段吻合后，先按该证据重新计算保护集合，再执行全局预检。放行预检无需等待旧云规则实际删除，后续删除仍须通过目标安全门。

```go
type DNSAddress struct {
    Host string
    Addr netip.Addr
}

type EdgeOnePrefix struct {
    ZoneID string
    Prefix netip.Prefix
}

func firstOverlap(dns []DNSAddress, edge []EdgeOnePrefix) (DNSAddress, EdgeOnePrefix, bool) {
    // edge 包含当前、待更新与仍可能用于回源的保护前缀。
    // 调用前按域名/IP、站点/CIDR 排序，保证证据稳定。
    for _, host := range dns {
        address := host.Addr.Unmap()
        for _, network := range edge {
            if network.Prefix.Contains(address) {
                host.Addr = address
                return host, network, true
            }
        }
    }
    return DNSAddress{}, EdgeOnePrefix{}, false
}

type Preflight struct {
    DNSEnabled, EdgeOneEnabled   bool
    DNSComplete, EdgeOneComplete bool
    KnownConflict, Overlap       bool
}

type Admission struct {
    DNS, EdgeOne bool
    KeepConflict bool
}

func decideAdmission(in Preflight) Admission {
    result := Admission{
        DNS:          in.DNSEnabled,
        EdgeOne:      in.EdgeOneEnabled && in.EdgeOneComplete,
        KeepConflict: in.KnownConflict,
    }
    if !in.DNSEnabled || !in.EdgeOneEnabled {
        // 仅 DNS 的错误/重试策略继续由现有目标链处理。
        return result
    }
    if in.Overlap || (in.KnownConflict && !(in.DNSComplete && in.EdgeOneComplete)) {
        return Admission{KeepConflict: true}
    }
    return Admission{DNS: in.DNSComplete, EdgeOne: in.EdgeOneComplete}
}
```

`Admission` 是数据层准入，最终写入还要满足总暂停、到期来源和目标覆盖门。`KnownConflict` 取进程内已知状态或已持久化记录。正式轮查实重叠后立即阻断，存储失败也保持该内存状态；完整无重叠时，先提交解除，再清除内存状态并放行。只读模拟使用相同判定，并读取已知冲突和已有过渡保护；状态写入由正式轮负责。站点确认使用当前轮的数据准入和站点独立证明，DNS 查询失败但没有已知重叠时，仍能走 EdgeOne 确认。

```text
Syncer.Run
  消费启动 / 定时到期 / 手动触发 / 恢复 / 配置通知
  按同一准入点读取不可变配置、总暂停、待执行来源
  按需准备各自地址集 → 如两项启用，执行全局预检并记录结果
  只有获准且到期的来源进入正式目标执行
  等目标任务完成 → 处理需要确认的 EdgeOne 站点
  各自结算下次到期，发布真实状态和结果
```

仅 EdgeOne 到期时可读取 DNS 以完成预检，但不因此执行 DNS 防火墙写入或重置 DNS 计时。仅 DNS 到期同理，可使用有效 EdgeOne 结果。已知冲突由下一次调度或手动触发复查，无独立写入 goroutine。总暂停、Stop、同间隔保存和短间隔调度均复用现有准入约束。源数据准备完成后按实际目标分组；同一 Provider 目标只由一个执行单元负责，现有跨平台并行在全局预检通过后保留。 [调度入口](syncer/syncer.go) · [熔断轮次](syncer/dns_round.go)

<a name="target-reference"></a>

### 7.5 目标规划与站点确认参考

- 将解析结果在进入规划器前转换为统一前缀。DNS 单地址用 `netip.PrefixFrom(addr, addr.BitLen())`，EdgeOne 用规范的网段前缀。规划条目保留 DNS 规则 ID 或 EdgeOne 站点 ID，后者用于从同一目标回读中提取每站覆盖结果。
- 按现有地址族、CIDR、协议、端口、动作组成 `FunctionalKey`。多个 EdgeOne 站点产生同一 key 时只新增一条规则，保留所有来源用于回读证明和清理。删除时检查目标上的所有适用站点，防止 A 的旧网段仍被 B 使用。跨来源完全等价的旧规则标记为目标冲突，精确等价手工规则的覆盖语义保持。各平台的合并端口、模板和相反动作继续按原安全门处理。
- S0 计划用于容量估算和新增。S1 采用同一份预检输入验证全部所需项。只有覆盖、TAG、目标定位和版本均满足时才删除；有删除则按现有合同读 S2。执行 S2 后直接校验实际覆盖并用 S2 更新站点证明和冲突判断。跨 TAG 等价可使新增列表为空，仍需检查实际覆盖。对同一目标发生版本冲突时，重新 S0 和规划。两项启用时保持输入前缀，DNS 单独运行仍按现有路径重试。 [功能 key、覆盖和清理](provider/plan.go) · [Provider 接口](provider/provider.go) · [目标 attempt](syncer/target.go)

| 平台 | 复用的写入与删除定位 | 本阶段重点 |
|---|---|---|
| 腾讯云 Lighthouse | S0 / S1 的 `FirewallVersion`，按当前快照原值条件删除 | 多端口展开、版本变化后重试。 |
| 腾讯云 CVM | 安全组 `Version` 与 S1 `PolicyIndex` | 现有入站数量保护和新旧并存预算。 |
| 阿里云 ECS | S1 回读 `SecurityGroupRuleId` | 完整分页、分批和部分成功计数。 |
| 阿里云 SWAS | S1 回读 `RuleId` | IPv6 不可用、完整分页和有界读取。 |

站点确认的输入是该站点目标集合上实际回读的证明。可参考以下对象，状态与配置分别保留。

```go
type SiteCoverage struct {
    ZoneID         string
    TargetKey      string
    Current        string
    Pending        string
    SnapshotDigest [32]byte // 两版规范排序后的完整网段摘要
    ConfigEpoch    uint64
    Complete       bool
    HasConflicts   bool
}

func siteCoverageReady(required []string, proofs map[string]SiteCoverage, want SiteCoverage) bool {
    if len(required) == 0 || want.Pending == "" {
        return false
    }
    for _, target := range required {
        got, ok := proofs[target]
        if !ok || !got.Complete || got.HasConflicts || got.TargetKey != target ||
            got.ZoneID != want.ZoneID || got.Current != want.Current || got.Pending != want.Pending ||
            got.SnapshotDigest != want.SnapshotDigest || got.ConfigEpoch != want.ConfigEpoch {
            return false
        }
    }
    return true
}
```

`required` 来自当前站点配置里已去重的实际目标，`proofs` 只包含该站点本轮的目标回读。`Complete` 表示该站点所有必需地址族和功能 key 在完整快照中得到证明。地址族按当前版和待更新版的规范前缀并集检查，`dual` 必须同时有 IPv4 和 IPv6，缺任一族时标记 `address_family_missing`。`ConfigEpoch` 标识相关配置快照，摘要按前缀规范化并排序后计算；同版本内容变化时重新规划。示例的比较函数只判断站点证明，发确认前仍经过全局准入、版本重查和总暂停检查。

确认去重按 `ZoneId`、待更新版本和待更新规范网段摘要判断。可用过渡表里的 `pending_version` 与 `pending_cidrs_json` 计算同一指纹。目标覆盖证明仍绑定本轮配置及当前/待更新两版。站点绑定变更时重算覆盖，同一待更新指纹的已确认状态继续用于去重。

| 已保存状态或新观测 | 正式轮动作 |
|---|---|
| `not_attempted`，最新查询仍是同一待更新指纹 | 全局准入、所有绑定目标覆盖和确认前重查通过后，先提交 `unknown`，等完限速并复核当前准入后发送一次确认。提交失败或准入撤销时保持零确认请求。 |
| `accepted`，最新查询仍是同一待更新指纹 | 跳过确认，继续观察实际生效。跨轮、重启、修改绑定后仍适用。 |
| `unknown`，重查后仍无法证明先前请求结果 | 保持结果未明，只读复查并保留同指纹的安全诊断。仅有持久化状态时提供固定文案。`Next` 仍显示同一待更新版本或暂时消失均不足以授权重发。 |
| `unknown`，有明确证据表明上次未发送、未被执行或该指纹仍未确认 | 先重查同一待更新指纹，重新取得目标覆盖证明，按 `not_attempted` 重新走确认准入。 |
| 查询明确显示已无待确认工作，或返回 `OperationDenied.LatestVersionNow` | 重新只读核对当前/待更新状态。若有效结果已无待确认工作则跳过；其他不确定结果保持待核对。保留已取得的确认记录与生效证据。 |
| 新查询得到另一待更新指纹 | 下一次正式轮按新指纹准备覆盖证明。保留仍可能用于回源的旧过渡网段，直到取得释放证据。 |

```text
for 每个本轮需要确认的站点:
    读取持久化状态，按待更新指纹判定是否有资格发请求
    要求配置的每一个实际目标均有同一版本、同一配置的完整回读
    复核地址族、功能 key、跨 TAG 残留及相关相反动作
    重新 DescribeOriginACL，核对版本、规范网段和当前配置
    若站点仍启用且当前可发起确认:
        持久化确认状态 unknown
        等待限速，在交给 SDK 前复核配置代次、暂停、Stop、功能启用与站点配置
        若未获准且明确未发送，按指纹与本次提交时间条件恢复 not_attempted
        获准后调用 ConfirmOriginACLUpdate
        成功则记录 accepted 和 RequestId
    超时 / 已是最新版 / 状态未知 → 先查询实际状态，再决定后续动作
```

`unknown` 在请求之前落库，使进程在请求与结果持久化之间退出时，重启可以先查询实际状态。收到明确成功但 `accepted` 写入失败时，进程内仍按已确认去重，重启可根据已提交的 `unknown` 保守复查。腾讯云公开确认接口以 `ZoneId` 为输入，返回 `RequestId`。真实账号能否提供足以解除 `unknown` 的证据由 Step 17 验证。只有查询证明待更新版本成为当前且网段吻合时，才把旧过渡网段交给普通安全清理。 [站点确认接口](https://cloud.tencent.com/document/product/1552/120409) · [站点查询接口](https://cloud.tencent.com/document/product/1552/120408)

```go
// 仅在上述全部检查通过且 unknown 已提交后调用。
req := teo.NewConfirmOriginACLUpdateRequest()
req.ZoneId = common.StringPtr(zoneID)
resp, err := client.ConfirmOriginACLUpdateWithContext(ctx, req)
// 成功读取 RequestId 后记录 accepted。结果不确定时由调用方转入只读核对。
```

<a name="view-reference"></a>

### 7.6 响应、界面和可观测性参考

运行状态建议沿用现有 JSON 风格。三个开关分层展示。总开关字段沿用 `enabled`；`dns.enabled`、`edgeone.enabled` 表示配置开关，`effective` 表示与总开关合取后的执行资格。定时和进度字段保持区分，尚无记录时返回 null。

```json
{
  "enabled": true,
  "dns": {"enabled": true, "effective": true, "next_due_at": null},
  "edgeone": {"enabled": true, "effective": true, "next_due_at": null},
  "address_preflight": {
    "state": "overlap",
    "conflict": {
      "dns_name": "home.example",
      "dns_address": "203.0.113.7",
      "edgeone_prefix": "203.0.113.0/24",
      "zone_id": "zone-example"
    }
  }
}
```

此示例仅列本阶段新增的状态及既有总开关，现有轮次、健康和时间字段继续输出。 `address_preflight.state` 可使用 `single_source`、`clear`、`overlap`、`data_unavailable`；已查实冲突且复查数据不足时保留 `overlap`，同时附失败来源。恢复时发布一次状态变化。Dry Run 使用本次预览的判定和实际受影响目标；云端未知配额标为未知，已读快照数和预计新增数分别显示。建议只增加适配既有前端结构的字段，保持 `results`、`warnings` 等数组为空时为 `[]`。 [同步响应](syncer/syncer.go) · [前端类型](webui/frontend/src/types.ts) · [模拟测试页面](webui/frontend/src/views/RunTest.vue)

日志和健康沿用现有事件与端点。健康使用来源自己的到期与结果，区分可继续的部分结果与两侧全局阻断。诊断只包含安全字段，例如 `ZoneId`、目标标识、版本、状态和 RequestId。目标明确写入数量、候选数、已清理数和残留分别统计。用本地模拟证明各入口边界，真实站点数据形状、规则配额、权限、确认后的实际时序由 Build8 Step 17 留存独立证据。 [运行健康](internal/health/health.go) · [日志入库](webui/api/logwriter.go) · [构建追踪](Build8.md#step-17)

<a name="frozen-contract"></a>

## 八、冻结合同与实施边界

本节是本阶段已确认行为的实施索引。第一至六节给出产品行为，第七节提供数据结构和参考代码，本节规定交界条件和通过标准。参考代码的函数名、拆文件和内部类型可适配现有实现。若发现前文或现有实现与本节发生实际冲突，先按 [Build8 的变更规则](Build8.md#execution-rules)处理，再改变合同。 [现行编码约束](AGENTS.md)

### 8.1 首版交付与兼容边界

| 冻结项 | 通过标准 |
|---|---|
| 功能入口 | 左侧栏在「域名规则」后新增「EdgeOne 回源」，路径 `#/edgeone`。一个 `ZoneId` 绑定一个或多个现有目标，站点配置的一组协议、端口和地址族用于所有绑定。 |
| 配置默认 | 总开关开启，DNS 开启，EdgeOne 关闭。DNS 间隔 `5m`；EdgeOne 间隔 `72h`，两者可独立设置。新库和 reset 使用 DNS TAG `auto-ddns`、EdgeOne TAG `edgeone`。旧库的有效 DNS TAG 保留，缺键或空白旧值按原有效 `auto-dns` 迁移。 |
| 数据契约 | 新增设置键固定为 `dns_enabled`、`edgeone_enabled`、`edgeone_tag`、`edgeone_interval`；已有 `tag`、`interval`、`sync_enabled` 各保留既有用途。设置 API 开关用字符串，配置包开关用 JSON boolean。相同归一化 TAG 拒绝。仅旧库迁移和 v3 导入在 DNS TAG 恰为 `edgeone` 时给新模块选择 `edgeone-origin`。 |
| 目标引用 | 站点目标列表必须非空，重复本地 ID、同实际目标别名和悬空引用拒绝。不同站点可共用目标。删除被引用目标返回 409。按 `cloud_type + region + resource_id` 归并实际目标；保留已有目标的本地 ID 和 DNS 引用语义。 |
| Web API 与状态 | 站点配置、站点状态和同步继续使用 [接口表](#api-reference) 中的路径。现有同步状态字段保持兼容，新增 `dns`、`edgeone`、`address_preflight`。预检状态使用 `single_source`、`clear`、`overlap`、`data_unavailable`，两项均关闭时可由各自开关和 `single_source` 识别无双开预检。 |
| 备份 | Step 4 完成时导出 v4，严格导入 v3 和 v4。保留 v3 的 DNS TAG、间隔和总暂停，导入后无 EdgeOne 站点且功能关闭。v4 通过 `target_export_ids` 重映射所有绑定。运行状态留在本机 SQLite；reset 清理这些运行记录。 |
| 系统边界 | 使用当前 WebUI、SQLite、同步器、腾讯云 SDK 和现有四类目标 Provider。首版涉及现有安全组或轻量云防火墙。站点的实际源站范围由配置者提供；已确认的自动确认流程通过目标回读自行运行。 |

站点端口校验适用新增的 EdgeOne 表单/API。对于 TCP、UDP 和 TCP+UDP，接受单独的 `ALL`，或逗号分隔的端口与端口范围。每项去除首尾空白，端口为 1～65535，范围起点不大于终点。空项、非法项和 `ALL` 混入列表整份拒绝。ICMP 按既有语义归一化为 `ALL`。存量 DNS 端口校验继续走现有入口。动作固定为 `ACCEPT`，由后端生成。 [站点 API 与字段](#api-reference) · [现有领域校验](config/validate.go)

### 8.2 开关、调度与数据有效性

1. 总开关只控制正式执行准入，单项关闭只停止对应来源。两项都关闭时正式轮没有 DNS 解析、TEO 查询或云写入。仅 DNS 开启时保留现有 DNS 路径；仅 EdgeOne 开启时无需读取 DNS 结果。
2. 每个来源拥有自己的到期时间。首次启用、总暂停恢复、手动立即同步及新站点进入现有执行入口。手动立即同步安排已启用来源；新增站点安排 EdgeOne 工作。EdgeOne 正常到期、手动立即同步及首次启用时，对相应站点安排实际查询。仅 EdgeOne 到期可以为了双开预检读取 DNS，但本轮不因此执行 DNS 写入或重置 DNS 间隔。仅 DNS 到期时优先复用有效的 EdgeOne 结果，需刷新时合并同站点查询，EdgeOne 云写入仍按自己的到期状态。配置保存未改变间隔时保留对应计时。间隔变化只更新对应来源。
3. 以每站最后一次**成功且校验完整**的查询时间作为缓存年龄起点。允许复用的快照必须属于当前站点和生效凭据，且年龄小于当前配置的 EdgeOne 间隔。已观察到的计划切换时间到达后，若缓存早于该时间，须取得新查询才可以继续把它作为完整有效输入。读取失败不会刷新有效期；未过期的最后成功快照可按同一规则使用。处于过渡保护中的前缀可以证明命中，不能把不完整的站点查询转为完整。EdgeOne 到期、首次启用、新站点、手动立即同步及总暂停恢复必须实际查询；仅 DNS 到期可复用有效快照。强制查询失败时，若最后成功快照仍在当前间隔内且未到计划切换时间，可继续使用并保留查询错误；过期或失效缓存不得当作完整输入。查询失败返回时及预检使用复用快照前，各按当时的时间检查有效期和计划切换时间。EdgeOne 本轮到期且实际查询失败时，即使缓存仍可用于预检也按退避结算；仅 DNS 到期的轮次保持 EdgeOne 的原有计时。reset、完整导入和删除站点必须清除对应的进程内缓存、站点状态、覆盖摘要与确认内存证据。
4. 一轮正式执行先读取不可变配置和各来源数据，再按获准且到期的来源派发所绑定的物理目标。共享目标继续携带两侧完整期望。目标重试在双开时使用同一轮已检查的前缀，重新读取云快照及删除定位。DNS-only 保留逐 attempt 解析。过期调度、重复触发和限速等待应合并为有限工作，避免短间隔产生并行请求洪峰。查询、确认前重查和确认请求都走 TEO 限速，两个接口官方默认限制各为 20 次/秒。总暂停和 Stop 沿用已准入任务收尾约束。确认属于新的站点请求，等完限速后、交给 SDK 前在协调器稳定区完成最后一次本地准入。已获准的请求在锁外执行。 [查询 API](https://cloud.tencent.com/document/product/1552/120408) · [确认 API](https://cloud.tencent.com/document/product/1552/120409)
5. 冲突阻断期间正常调度继续做只读复查。恢复仍受来源到期、开关及最新目标计划约束。用户关闭一项后另一项按单项工作；再次双开，保留已有冲突证据直至完整复查解除。定时器、退避和限速的具体实现可复用现有基础，结果须满足本节。 [正式调度](syncer/syncer.go) · [EdgeOne 查询 API](https://cloud.tencent.com/document/product/1552/120408)

### 8.3 正式预检的决策优先级

按下表由上至下判定。这里的「两侧」指两个功能来源；只有已启用且本轮到期的来源实际进入目标写入。

| 条件 | DNS 防火墙 | EdgeOne 防火墙 | EdgeOne 站点确认 | 状态变化 |
|---|---|---|---|---|
| 总暂停或停止准入 | 无新轮 | 无新轮 | 无新请求 | 保留开关和已知冲突。 |
| 两项均关闭 | 无新轮 | 无新轮 | 无新请求 | 无来源任务，保留已有冲突证据。 |
| 单项启用 | 仅 DNS 启用才进入原路径 | 仅 EdgeOne 启用且该来源输入可用才执行 | EdgeOne 启用并满足站点独立准入才允许 | 双开冲突规则暂不阻断，保留已有证据。 |
| 双开且任何有效证据证明重叠 | 零写入 | 零写入 | 零调用 | 立即记为进程内已知冲突，持久化失败继续阻断。 |
| 双开且冲突记录读取失败 | 零写入 | 零写入 | 零调用 | 记录 SQLite 读取失败，等待可读后复查。 |
| 双开且已知冲突，但输入不完整 | 零写入 | 零写入 | 零调用 | 保留已知冲突并继续复查。 |
| 双开且已知冲突，两侧完整、无重叠 | 成功提交解除后按到期执行 | 同左 | 符合站点独立准入时允许 | 提交失败继续阻断；成功后清内存状态，记录一次恢复。 |
| 双开且无已知冲突，只有 DNS 输入不完整 | 零写入 | 按自己的安全门执行 | 所有绑定目标回读及重查通过后立即确认 | 记录 DNS 不完整和 EdgeOne 单侧继续。 |
| 双开且无已知冲突，只有 EdgeOne 输入不完整 | 按自己的安全门执行 | 零写入 | 零调用 | 记录 EdgeOne 不完整和 DNS 单侧继续。 |
| 双开且无已知冲突，两侧均不完整 | 零写入 | 零写入 | 零调用 | 记录两侧输入不足。 |
| 双开、两侧完整且无重叠 | 按到期执行 | 按到期执行 | 符合站点独立准入时允许 | 记录正常预检。 |

双开时，任一适用域名缺少可用解析结果，则 DNS 来源输入不完整；任一需参与比较的站点缺少有效快照，则 EdgeOne 来源输入不完整。禁用项和确实没有适用配置的有效空集不制造查询失败。完整性和正向命中分别计算，正向命中优先。若本机冲突状态无法读取，应由正式入口直接应用上表的读失败分支，不能把未知状态作为 `KnownConflict=false` 传给纯判定。目标 Provider 在预检后返回的写入、容量或回读失败属于目标级结果，按实际目标和站点覆盖汇总，不能覆盖已经判明的全局冲突状态。正式预检判断的是本轮证据，单侧继续期间后来才发现的冲突阻止后续写入。

来源停用时，另一来源 TAG 下的精确等价规则只作为只读覆盖证据，不计 owned、不报 `cross_tag_equivalent`、不新增、不删除、不改标；该证据使剩余来源可继续工作，但不得据此放宽双开重叠判定。双开全局预检始终先于目标执行：DNS 地址落入任何 EdgeOne 当前/待更新网段时仍按重叠阻断，不因功能 key 相同而例外。 [预检参考](#preflight-reference)

### 8.4 云写入、确认和清理的顺序

| 操作 | 放行所需证据 | 失败或未知时 |
|---|---|---|
| 新增 | 来源通过预检且到期，目标 S0 完整，功能可表达，按现有配额和增量写入规则处理。每个物理目标仅一条串行写入链。 | 保留旧规则。仅统计云端已确认的新增；能力跳过或错误仍进入覆盖证明。 |
| 回读覆盖 | 使用 S1 与本轮同一输入校验功能 key。若执行 S2，最终证明和冲突判断使用 S2。另一功能 TAG 的等价残留报目标冲突，手工等价规则继续按既有合同只读计入。 | S2 失去覆盖时目标失败并丢弃证明，保留已确认增删计数。无法完整证明时，该站点确认保持零调用。 |
| 站点确认 | 待更新指纹存在且需确认；同一站点本轮全部绑定目标有最终成功 attempt 的覆盖证明；当前与待更新版在重查时相符；配置、暂停和启用准入仍有效；`unknown` 已提交。 | 已确认同一指纹跳过。超时和响应未知只读复查，凭明确证据及新覆盖才可重试。目标通过和确认成功分开记录。中间失败 attempt 的证明必须丢弃；只有最终成功 attempt 的证明可参与确认，最后一次清理重试耗尽的既有 `success + cleanup_deferred` 分支除外。确认失败必须留下安全诊断原因；缓存命中时 `last_query_at` 仍是实际查询时间，`planned_active_at` 保留。 |
| 过渡保护 | 观察到待更新版时持久化保护；重启读取保护，仍可能用于回源的前缀进入全局预检和目标保护。 | `Next` 消失、确认成功、到达计划时间或查询失败均保留尚未证明可释放的保护。 |
| 清理 | 有效查询证明追踪的新版为当前且网段一致；在预检中重算保护集合；目标 S1 覆盖完整、TAG 精确归属、无能力/冲突阻断且删除定位无歧义。 | 保留残留并按现有合同延后，允许后续从新快照收敛。其他站点仍需要的 key 保持。 |

明确未取得最终准入且未发送时，按站点、待更新指纹、`unknown` 状态和本次前置提交时间条件恢复 `not_attempted`。更新失败时保留已有记录并记录安全诊断。处理已准入请求的确认回包时，协调器在稳定区内用轮次捕获的 RuntimeState 与 ConfigEpoch 校验确认结果写入。代次已变更时，仍存在的 `unknown` 保留给后续只读复查，已由重置或站点删除清除的状态不会被旧回包恢复。网络调用仍在稳定区外。

只有腾讯云当前生效状态的证据能释放旧网段保护。释放和实际删除分步判断，避免因冲突等待删除、删除又被冲突阻止。确认接口没有版本条件参数；重查能发现已观察到的变化，请求发出后仍存在云端版本变化窗口。真实账号能提供的状态证据由 Step 17 记录。 [目标与确认参考](#target-reference) · [腾讯云确认 API](https://cloud.tencent.com/document/product/1552/120409)

### 8.5 验收与可调整项

- 模拟测试调用同一套预检和规划，读取当前运行记录和 S0。模拟前后云规则、冲突与过渡表、正式缓存、DNS 熔断和调度时间保持相同。正式计划候选与执行后的实际写入分别记录，未知容量和确认状态保持可辨识。
- 当前阶段可调整包内文件拆分、私有接口、测试夹具、数据遍历实现和界面内部组件。修改任何公开字段、配置兼容语义、调度触发条件、失败准入、TAG 所有权、确认或删除授权均属于方案变更。
- 本地模拟可证明本地流程和请求形状，真实云权限、响应、生效时序及发布状态各自保留证据。缺少真实待更新窗口时，当前版链路可独立验收，确认与生效场景保留待补。
