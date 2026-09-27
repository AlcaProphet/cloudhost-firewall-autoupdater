# ProdTestList.md — 长耗时 / 外部验收测试清单

> **用途：** 集中记录单次耗时较长、或需要远端与外部环境才能执行完成的验收项，便于安排时段批量处理。
> **状态时间：** 2026-09-24。Build6 Step 1、Step 2、Step 3、Step 4、Step 5 与 Step 6 的本地门禁均已执行完毕（见第一、二、二之二、二之三、二之四、二之五节）；远端 GitHub Actions、Docker 构建、人工浏览器和真实外部链路待办见第三节及各 Step 记录。
> **原则：** 本清单只记录"尚未执行/需重跑"的项；已通过的项如实记录命令与结果，绝不以更窄的命令替代原门禁。

---

## 一、Build6 Step 1 门禁：已全部完成（2026-09-23）

Build6 Step 1 规定的四道门禁均已在本机真实执行并通过：

| # | 命令 | 结果 | 备注 |
|---|------|------|------|
| 1 | `go test -race -count=100 ./notifier` | **通过**（`ok 45.358s`） | 100 轮 |
| 2 | `go test -race -count=100 ./webui/api` | **通过**（`ok 41.405s`） | 100 轮 |
| 3 | `go test -race -count=100 -timeout 40m ./syncer` | **通过**（`ok 665.887s`，0 次 `DATA RACE`） | **必须显式放宽 `-timeout`**，见下方说明 |
| 4 | `go test ./... -race`、`go vet ./...`、`git diff --check` | **通过** | 单轮全仓 + 静态检查 |

**关于第 3 项的超时（重要，后续复跑必读）：**

- Go 测试框架的默认包级超时是 **10 分钟**，而 `./syncer` 跑满 100 轮约需 **11 分钟**（单轮约 6.7 秒：既有 sleep/限速用例约 5 秒 + Step 1 新增的重试退避与轮次用例约 2 秒）。
- 首次按原命令（不带 `-timeout`）执行时，运行到 `600.642s` 被框架中断：`panic: test timed out after 10m0s`。完整日志计数显示每轮一次的标记出现 90 次，即 **100 轮中约 90 轮已完成，其中断言失败 0、`WARNING: DATA RACE` 0**，仅最后约 10 轮未执行——那次失败是**框架超时**，不是测试失败。
- 随后以 `-timeout 40m` 重跑同一测试集、同一轮次数，**完整通过**（`ok 665.887s`，exit=0）。未删减任何测试或轮次。
- 复跑命令（仓库根目录）：
  ```bash
  go test -race -count=100 -timeout 40m ./syncer
  ```
- 成功时 `go test` 不回显测试日志（未加 `-v`），日志文件里不会出现每轮标记，这是正常的；判定依据是最终的 `ok` 行与退出码。

---

## 二、Build6 Step 2 门禁：已全部完成（2026-09-23）

Build6 Step 2 规定的自动门禁已在本机真实执行并通过：

| # | 命令 | 结果 | 备注 |
|---|------|------|------|
| 1 | `cd webui/frontend && npm ci && npm run build` | **通过** | vue-tsc + vite 5.4.21，2818 模块 |
| 2 | `go test ./... -race` | **通过** | 根包（进程级用例）4.464s、config 1.551s、syncer 8.143s、webui/api 2.028s |
| 3 | `go vet ./...` / `go build ./...` / `git diff --check` | **通过** | 修改文件 `gofmt -l` 无输出 |
| 4 | `docker compose -f docker-compose.yml.example config --quiet` | **通过** | 渲染确认仅三个部署变量，healthcheck 为 `http://127.0.0.1:60200/api/health` |
| 5 | `docker build -f build/Dockerfile -t fwalizer:build6-step2 .` | **通过** | 三阶段构建，`CGO_ENABLED=0`，无 VERSION 注入 |
| 6 | 真实容器健康检查 | **通过** | 正常容器 `healthy`；进程存活但 HTTP 不可达时 `unhealthy`（无 pgrep fallback）；`WEBUI_PORT=61234` 覆盖时 healthcheck 跟随；`docker stop` 后 `ExitCode=0` |

**本 Step 仍待用户执行：**

| # | 项目 | 动作 | 原因 |
|---|------|------|------|
| 1 | 浏览器人工复核配置导入/导出 | 打开「全局设置」，执行一次导出与导入，确认文案与结果 | Step 2 改动了 `/api/settings` 返回集、导出内容与导出确认文案；未执行真实浏览器交互 |
| 2 | 远端 GitHub Actions 运行 | 推送分支/PR，观察工作流 | 工作流已去掉 build arg；远端结果无法由本地替代 |

---

## 二之二、Build6 Step 3 门禁：已全部完成（2026-09-23）

| # | 命令 / 动作 | 结果 | 备注 |
|---|------------|------|------|
| 1 | `go test -race -count=1 ./webui ./webui/api ./...` | **通过** | 0 次 `DATA RACE`；root 9.494s、webui 2.235s、webui/api 1.777s、syncer 7.921s |
| 2 | `go vet ./...` / `go build ./...` / `git diff --check` | **通过** | 改动文件 `gofmt -l` 无输出 |
| 3 | 真实二进制 SIGTERM / SIGINT | **通过** | `exit=0`；日志顺序 `收到停止信号 → 开始 HTTP 关闭 → 同步引擎停止 → HTTP 关闭完成`；退出后端口不再监听、pidfile 已清理 |
| 4 | 同步轮次在途时发 SIGTERM | **通过** | 1 目标 1 规则（不可达 DNS，重试 5.1s）：`开始同步` → `收到停止信号` → `同步失败(ERROR)` → `同步完成 耗时=5.117s` → `同步引擎停止`，`exit=0` |
| 5 | 真实 `/api/sync/events` 长连接 + SIGTERM | **通过** | 日志出现 `服务器关闭，同步事件 SSE 退出`，信号到退出 0.04s，未触发强制关闭 |
| 6 | HTTP 绑定失败（不可解析主机） | **通过** | `exit=1`，输出 `WebUI 监听失败: … can't assign requested address`，日志无 `开始同步` |
| 7 | `docker build -t fwalizer:build6-step3 .` + 容器 health/stop | **通过** | `healthy`（5s）；`docker stop` 0.19s、`ExitCode=0`；容器/volume 已清理 |

**Step 3 的证据边界（不得扩大解释）：**

- 容器为 0 targets 空库轮次，`docker stop` 只证明 HTTP/进程收尾顺序，**不**证明有真实同步负载时“完成当前轮次”；该语义由第 4 项真实二进制在途轮次证据支撑。
- `EACCES` 权限错误需低端口 + 非 root 才能构造，本轮以 `EADDRNOTAVAIL` 等非占用类错误代表“非 EADDRINUSE 不随机降级”。
- 进程外未制造 listener 被关闭的 Serve 运行错误并断言退出码；该路径由自动测试 `TestServeRuntimeErrorSurfacedToCaller` + `TestShutdownNormalizesServeResult` 覆盖，属证据层差异。

---

## 二之三、Build6 Step 4 门禁：已全部完成（2026-09-24）

| # | 命令 / 动作 | 结果 | 备注 |
|---|------------|------|------|
| 1 | `go test -race -count=1 ./config ./webui/api` | **通过** | config 1.455s、webui/api 3.023s，无 `WARNING: DATA RACE` |
| 2 | `go test ./... -race` | **通过** | 11 个包全部 ok（root 9.473s、syncer 9.140s、webui/api 4.844s 等），无 `WARNING: DATA RACE` |
| 3 | `go vet ./...` / `go build ./...` / `git diff --check` | **通过** | 本次改动与新增文件 `gofmt -l` 无输出 |
| 4 | `cd webui/frontend && npm run build` | **通过** | vue-tsc + Vite 5.4.21；`webui/frontend/dist` 已刷新（该目录被 `.gitignore` 忽略） |
| 5 | 独立复核：`go clean -testcache` 后重跑门禁 + 真实二进制 52 项 HTTP 契约探测 | **通过** | 首轮 49/52；3 项指向“导入未共用严格解码”，补齐后针对该 3 项 + 边界复跑 **6/6 通过**（尾随 JSON/多顶层值/未知字段→400，>10 MiB→413，被拒后旧配置保留，限内合法导入→200 且生效） |
| 6 | 完整 stdout 日志 sentinel 扫描（启动路径 + reload 路径） | **通过** | 4 个云凭据、SMTP 密码、Webhook URL 六个 sentinel 命中数 **全为 0**；仅出现 `channel=dingtalk` |
| 7 | 日志级别即时生效（进程级） | **通过** | `log_level=error` 期间同步触发与热重载均无 INFO 输出，恢复 info 后立即恢复 |
| 8 | `TestProcessSecretsNotLogged` 红/绿验证 | **通过** | 临时恢复历史 `url=` 日志 → 用例失败并打印 URL；还原（sha256 一致）后通过 |

**本 Step 仍待用户执行：**

| # | 项目 | 动作 | 原因 |
|---|------|------|------|
| 1 | 浏览器人工复核 | 打开「全局设置」保存一次设置、打开「告警配置」保存一次，并在「域名规则」引用目标后尝试删除该目标，确认 409 提示文案 | 前端设置保存改为 11 键白名单 payload；HTTP 状态码与错误文案契约变化只在自动测试中验证，未做真实浏览器交互 |
| 2 | 远端 GitHub Actions 运行 | 推送分支/PR，确认 race 命令真实运行 | 同第三节第 1 项（O5-02 收尾） |

**Step 4 的证据边界（不得扩大解释）：**

- 连接测试与资源扫描的 SDK 原始错误仍按既有产品行为以 `200 + {"success":false,"error":...}` 返回给操作者以便诊断；本 Step 只把 **500** 响应改为安全通用文案，未在真实云上验证 SDK 报错不含密钥。
- 敏感值 sentinel 用例覆盖新增的 400/409/413/500 响应与服务端日志；`GET /api/settings`（11 键，含云凭据）与 `GET /api/alerts`（含 SMTP 密码与 Webhook URL）按 §12.9 契约仍返回配置值，属已文档化行为。
- 配置变更协调器是 Step 4 骨架：apply 仍封装既有分次 reload；commit 后若 `LoadConfig` 失败只记录 ERROR 而不更新运行时（正常写入路径已被校验拦在前面），完整原子发布属 Step 5。

---

## 二之四、Build6 Step 5 门禁：本地自动门禁已完成（2026-09-24）

| # | 命令 / 动作 | 结果 | 备注 |
|---|------------|------|------|
| 1 | `go test -race ./config ./provider ./syncer ./webui/api` | **通过** | config 2.062s、provider 1.656s、syncer 11.163s、webui/api 5.034s；0 次 `WARNING: DATA RACE` |
| 2 | `go test ./... -race -count=1`（冷缓存复跑） | **通过** | 11 包全 ok（root 10.402s、app 2.339s、config 3.027s、dns 3.319s、portconv 1.330s、tag 1.594s、notifier 2.512s、provider 2.862s、syncer 13.089s、webui 4.495s、webui/api 6.773s），0 次 `WARNING: DATA RACE` |
| 3 | `go vet ./...` / `go build ./...` / `git diff --check` | **通过** | 改动与新增 Go 文件 `gofmt -l` 无输出 |
| 4 | `cd webui/frontend && npm ci && npm run build` | **通过** | vue-tsc + Vite 5.4.21，2818 模块；`package.json`/`package-lock.json` sha256 前后一致（**未升级前端依赖**，属 Step 6） |
| 5 | 真实二进制进程级 version 2 往返 | **通过** | `TestProcessConfigExportImportRoundTrip`：`POST` 导出附件头（`Content-Disposition: attachment; filename="fwalizer-config-v2-<UTC>.json"`、`Cache-Control: no-store`）、跨数据目录导入后 HTTP 校验目标/规则/设置/告警、version 1 → 400、两个进程日志均无四个敏感 sentinel |
| 6 | 失败注入矩阵 | **通过** | 清表、目标插入、规则插入、settings、email、webhook、`scanned_resources` 清空、commit 共 8 项 trigger 注入 + 候选构造失败 + commit 失败，逐项断言旧库完整、旧运行时状态指针保持、零发布、500 不回显底层错误 |
| 7 | 同实例/跨实例 ID 映射 | **通过** | 导出→清空→导入后规则仍关联同一业务目标（比较业务字段而非数字 ID）；跨实例不同自增空间导入正确重建 |
| 8 | 调度控制与原子发布语义 | **通过** | `false→true` 立即一轮、`true→true` 只按新 interval、`true→false` 完成当前轮后不再启动、暂停期排队 trigger 不执行、`Stop` 幂等、停止后不启动新一轮；单快照轮次与 Dry Run 单快照；`RuntimeManager` 并发 Snapshot/Apply 无竞态 |

**本 Step 仍待用户执行（不得由本地自动测试替代）：**

| # | 项目 | 动作 | 原因 |
|---|------|------|------|
| 1 | 浏览器人工复核 | 打开「全局设置」执行一次导出（确认危险确认文案列出四类密钥、下载文件名形如 `fwalizer-config-v2-<UTC>.json`）、再导入同一文件（确认成功提示与整页刷新）、故意导入损坏 JSON 与 version 1 文件（确认错误提示且**不**刷新） | 前端改动为 `POST + fetch + Blob`、`Content-Disposition` 文件名解析、整页 reload 与危险确认文案；`vue-tsc` 与生产构建不能替代真实浏览器行为 |
| 2 | 两个不同 SQLite 数据库交叉导入（人工逐项核对） | 在各自有不同自增历史的两库间导出/导入，逐项核对目标、规则目标关联、主题、同步状态与告警 | 自动测试只能断言程序语义，人工确认才是产品验收 |
| 3 | 真实外部链路 | 连接测试、资源扫描、真实云 API 增量写入/精确删除、SMTP 收件箱、Webhook | 需用户凭据与环境；Step 5 的凭据模型与快照语义变更后需重跑 |
| 4 | 远端 GitHub Actions 运行 | 推送分支/PR，确认 race 命令真实运行 | 同第三节第 1 项（O5-02 收尾） |

**Step 5 的证据边界（不得扩大解释）：**

- 全部为**本地 SQLite + httptest + 真实二进制进程**证据；未在真实云账号、真实 SMTP 或真实 Webhook 上验证；
- 「普通变更保留 DNS 熔断失败计数 / 完整导入重置计数」由 `syncer` 单测覆盖，未在长跑真实环境复现熔断进度；
- `modernc.org/sqlite` 驱动不强制 `sql.TxOptions.ReadOnly` 的写入拒绝，导出契约由「独立只读事务 + handler 不含写入语句」保证，测试只锁定可读取一致快照；
- 真实浏览器的 Blob 下载文件名与整页 reload 行为未验证；
- Step 5 已提交为 `c35eb9d`，但当前分支尚未推送，远端 CI 无对应运行结果。

---

## 二之五、Build6 Step 6 门禁：本地自动门禁已完成（2026-09-24）

| # | 命令 / 动作 | 结果 | 备注 |
|---|------------|------|------|
| 1 | 阶段 A：`npm audit fix --dry-run --json` → `npm audit fix`（均无 `--force`） | **通过** | `changed=2, added=0, removed=0`；仅 `nanoid 3.3.16→3.3.19`、`brace-expansion 2.1.2→2.1.7`；`package-lock.json` 6 增 6 删；`package.json` sha256 不变（`793fe6da…e0b4`） |
| 2 | 阶段 A：`npm ci` / `npm run build` | **通过** | vue-tsc + Vite 5.4.21；`dist` 与修复前**逐字节一致**（19 文件 / 796 KB） |
| 3 | 阶段 B：`npm install --save-dev vite@^8.3.1 @vitejs/plugin-vue@^6.0.9` | **通过** | 解析为 `vite 8.3.1`、`@vitejs/plugin-vue 6.0.9`；lockfile 543 增 / 699 删；`esbuild`、`rollup` 及其平台包全部移除，改由 `rolldown 1.2.10` + `lightningcss 1.33.0` 接管 |
| 4 | `npm ci` | **通过** | 75 packages，audited 76，`found 0 vulnerabilities` |
| 5 | `npm run build`（`vue-tsc && vite build`） | **通过** | `vite v8.3.1 building client environment for production...`、2819 模块、**`built in 135ms`**；`npm ci` 后二次构建与首次**逐字节一致** |
| 6 | `npm audit --audit-level=high` | **通过** | `found 0 vulnerabilities`（升级前为 1 moderate + 3 high） |
| 7 | `npm audit --omit=dev --audit-level=high` | **通过** | `found 0 vulnerabilities`（阶段 A 后即已通过） |
| 8 | 产物对比（升级前 → 升级后） | **通过** | 文件 19→21、JS 18→20、CSS 0→0、总字节 815,104→752,499、sourcemap 0→0；7 个视图路由 chunk 与 naive-ui 组件 chunk 全部保留；新增 `api-Cgz6hAsk.js`（Vue 运行时）与 `light-Slo-5Vyt.js`（naive-ui 公共部分） |
| 9 | `index.html` 引用完整性 | **通过** | 1 个 script + 2 个 `modulepreload`，逐一存在性校验通过 |
| 10 | `go test ./... -race -count=1`（`go clean -testcache` 冷缓存） | **通过** | 11 包全 ok，0 次 `WARNING: DATA RACE`（root 10.866s、syncer 15.486s、webui/api 6.642s、webui 3.857s） |
| 11 | `go vet ./...` / `go build ./...` / `git diff --check` | **通过** | 无输出 / 退出码 0 |
| 12 | 单二进制运行时（真实进程 + 临时数据目录） | **通过** | `/api/health` → 200；`/` → 200（471 B 新 `index.html`）；`index-DMKGbtVO.js`、`api-Cgz6hAsk.js`、`light-Slo-5Vyt.js`、`Dashboard`、`DataTable`、`Settings` 与 5 个动态路由 chunk 全部 `HTTP 200`；停止后端口不再监听 |
| 13 | `docker compose -f docker-compose.yml.example config --quiet` | **通过** | 该命令不需要 daemon |
| 14 | CI 工作流核对 | **通过** | `.github/workflows/docker-publish.yml` 13 个步骤；新增 `npm audit --omit=dev --audit-level=high` 与 `npm audit --audit-level=high` 两个**阻断**步骤，位于「构建前端」之后、「编译检查」之前 |
| 15 | 越界核对 | **通过** | 无 Vue Router 5、无 TypeScript 7、无 npm 12 改动、无 Go 依赖改动、无 overrides/resolutions；`vue`/`vue-router`/`naive-ui`/`typescript`/`vue-tsc` 与 `vite.config.ts`、`tsconfig.json`、前端源码均未改动 |

**补充：Docker 构建与容器验收（2026-09-24 已完成，用户启动 Docker Desktop 后执行）**

| # | 命令 / 动作 | 结果 | 备注 |
|---|------------|------|------|
| 1 | `docker build -f build/Dockerfile -t fwalizer:build6-step6 .` | **通过** | exit 0；`frontend-builder` 阶段实测 **Node v24.21.0 / npm 11.19.0**；日志 `vite v8.3.1`、2819 模块、`built in 225ms`；`CGO_ENABLED=0` 静态编译；镜像 74.1 MB |
| 2 | Node 版本固定（用户决策） | **已落地** | `build/Dockerfile`：`node:24-alpine` → `node:24.21-alpine`；`.github/workflows/docker-publish.yml`：`node-version: '24'` → `'24.21.0'`。实测三者 digest 原本相同（`sha256:ebfe2f90…`），固定后不改变构建结果、仅消除 minor 漂移；未采用 v26（Current、非 LTS、不在 Vite CI 测试范围） |
| 3 | 容器 health（`-p 62100:60200`） | **通过** | **6s** 转为 `healthy`；`GET /api/health` → 200；`GET /` → 200（471 B 新 `index.html`）；`index-DMKGbtVO.js`、`api-Cgz6hAsk.js`、`light-Slo-5Vyt.js`、`Targets-*`、`Settings-*` 全部 200；容器内 `uid=1000(appuser)` |
| 4 | `docker stop` 优雅停止 | **通过** | **ExitCode=0**、`OOMKilled=false`、耗时 0.155s；日志顺序 `收到停止信号 → 开始 HTTP 关闭 → 同步引擎停止 → HTTP 关闭完成`；停止后端口释放 |
| 5 | `docker compose -f docker-compose.yml.example config --quiet` | **通过** | 不需要 daemon |

**本 Step 仍待用户执行（不得由本地自动测试替代）：**

| # | 项目 | 动作 | 原因 |
|---|------|------|------|
| 1 | 浏览器人工回归 | 打开生产构建对应的 WebUI（非 `vite dev`）：七个页面、hash 路由与整页刷新、深浅主题、页面级大按钮与表格内小按钮、卡片式二次确认、目标/规则弹窗、暂停/恢复、配置 version 2 导出（危险确认/Blob/文件名）、导入成功整页 reload、损坏 JSON 与 version 1 导入失败不 reload、Console 无构建器错误、Network 无 404 | `vue-tsc`、生产构建、curl 探测与容器 health 均不能替代真实浏览器行为；Rolldown 产物与拆包策略变化需真实浏览器确认 |
| 2 | Node 24 容器化交叉复验 | 重启 Docker Desktop 后执行 `docker run --rm -v "$PWD/webui/frontend":/w -w /w node:24.21-alpine sh -c 'npm ci && npm run build'`，并与本机 `dist` 逐字节比对 | Docker 构建本身已在 `node:24.21-alpine` 阶段真实跑通 `npm ci && npm run build`（等于已验证 Node 24 构建链），但"独立复现同一份 dist"这一步因本机 Docker Desktop VM 容器创建能力失效而未能执行（见下方环境异常记录） |
| 3 | 远端 GitHub Actions 运行 | 推送分支/PR，确认两个 audit 阻断步骤与 race 命令真实运行 | 同第三节第 1 项（O5-02 收尾）；当前分支尚未推送 |

**本机 Docker Desktop 环境异常（非本仓库问题，2026-09-24）：**

- 完成上述构建与容器验收后，Docker Desktop VM 的**容器创建能力失效**：`docker run` 对**所有**镜像均挂起在 `State=created`，包括此前已成功运行过的 `fwalizer:build6-step6` 与 `alpine:3.24`；切换 `--runtime=runc` 与 `docker compose run` 同样挂起；镜像拉取已完成（`node:24.21-alpine` 238 MB 在本地）；
- 同时 `docker version`、`docker system df`、`docker images`、`docker build` 均正常响应，磁盘充足（Images 766 MB；Build Cache 8.9 GB / 可回收 8.2 GB）；
- 结论：**需用户重启 Docker Desktop** 才能继续执行容器相关操作（第 2 项待办）。这是本机 Docker 环境问题，**不改变**已取得的构建与容器 health/stop 证据，也不是仓库或依赖升级引入的问题。

**Step 6 的证据边界（不得扩大解释）：**

- 阶段 A 与阶段 B 的构建/审计/Go 门禁取自**本机 Node 26.7.0 / npm 11.19.0**；Docker 构建与容器验收取自 `node:24.21-alpine`（Node v24.21.0 / npm 11.19.0）；**远端 CI 未执行**；
- Docker 镜像为 **linux/arm64**（本机 Apple Silicon daemon 默认平台）；CI 发布平台为 `linux/amd64`，该差异属既有设计（`docker-publish.yml` 中 `platforms: linux/amd64`），本地构建不覆盖 amd64 交叉验证；
- 全部写操作使用任务专用隔离 cache `/tmp/fwalizer-step6-npm-cache`（用户 `~/.npm/_cacache` 存在 root 所有的 `content-v2/sha512/96/` 子树导致 `EACCES`/`EEXIST`）；**未使用 `sudo`、未 `chown`、未清理全局 cache、未使用 `--force`**；
- 开发服务器类漏洞（`server.fs.deny` Windows 绕过、dev server 跨站读取、`launch-editor` NTLMv2）**只影响 `vite dev`**；生产单二进制仅嵌入 `dist` 静态文件，不含 dev server，不得描述为已确认的生产 WebUI 漏洞；
- `nanoid` 在 lockfile 中未被标记为 `dev`（经 `postcss` 引入），因此 `npm audit --omit=dev` 会统计它；该依赖只参与构建，不进入单二进制产物；
- 本轮浏览器回归若执行，可顺带补充 Step 5 的前端导入导出证据，但**不能替代**两库人工交叉导入、真实云 API、SMTP 或 Webhook；**Step 5 与 R5-01 的关闭仍由用户决定**。

---

## 三、仍待执行的长耗时 / 外部验收（按后续 Step 归属）

| # | 项目 | 命令或动作 | 归属 | 说明 |
|---|------|-----------|------|------|
| 1 | 远端 GitHub Actions 真实运行 | 推送分支/PR（或 tag），观察 `Docker Build & Publish` 工作流，确认 race 测试真实运行且失败会阻止镜像推送 | Step 1（O5-02 收尾）、各 Step | 本地只能证明工作流文件内容与本地命令；远端结果必须在 GitHub 上确认。当前 Issue5 O5-02 保持 ◧ |
| 2 | Docker 构建（Step 6 验收项） | `docker build -f build/Dockerfile -t fwalizer:build6-step6 .` | Step 6 | **已于 2026-09-24 完成**（exit 0；frontend-builder 阶段 Node v24.21.0 / npm 11.19.0）；容器 health/stop 亦已通过，见二之五节；不再作为待办 |
| 2b | 前端依赖审计 | `cd webui/frontend && npm audit --audit-level=high` 与 `npm audit --omit=dev --audit-level=high` | Step 6 | **已于 2026-09-24 完成**：两者均 `found 0 vulnerabilities`，并已加入 CI 双阻断门禁；不再作为待办 |
| 2c | Node 24 容器化交叉复验 | 重启 Docker Desktop 后 `docker run --rm -v "$PWD/webui/frontend":/w -w /w node:24.21-alpine sh -c 'npm ci && npm run build'` 并与本机 `dist` 逐字节比对 | Step 6 | **待办**：Docker 构建已在 Node 24.21.0 阶段真实跑通构建链，但独立复现比对因本机 Docker Desktop VM 容器创建能力失效而未执行（见二之五节环境异常记录） |
| 3 | 进程级信号验收（完整） | 真实二进制 SIGTERM/SIGINT、`docker stop`，验证"完成当前轮次再退出"与 SSE 退出 | Step 3 | **已于 2026-09-23 完成**（见二之二节第 3～7 项）；不再作为待办 |
| 4 | 真实外部链路 | 真实云 API 连接测试/扫描/增量写入/精确删除、SMTP 收件箱、各渠道 Webhook、浏览器人工验收 | Step 5、Step 7 | 需用户凭据与环境；不得用 mock 替代后标记通过。Step 5 的凭据模型与运行时快照语义已变更，本轮需重跑（见二之四节） |
| 5 | Step 4 浏览器复核 | 设置保存（11 键白名单 payload）、告警保存、删除被引用目标的 409 提示 | Step 4 | 见二之三节；未执行真实浏览器交互 |

---

## 四、执行建议

1. **远端 CI 验证**可最早做：本地 Step 1、Step 2 与 Step 3 门禁已全绿，推一个分支或 PR 即可确认 race 命令在 GitHub Actions 上真实运行；这也是 O5-02 转为验收通过的唯一剩余条件。
2. 第二节列出的两项待办（浏览器复核导入导出、远端 CI）建议由用户安排执行。
3. 若未来把 `-count=100` 固化为 CI 门禁，建议单独拆一个 race job 并让 Docker 发布依赖其成功（Build6 / Issue5 O5-02 第 5 条已给出方向），同时注意上文的 `-timeout` 要求。
4. Step 3 的证据边界（容器 0 targets、`EACCES` 不可构造、进程外 Serve 运行错误）已记录在二之二节；Step 4 的证据边界（SDK 错误返回、sentinel 作用域、协调器过渡结构）已记录在二之三节；Step 5 的证据边界（本地 SQLite/httptest/真实二进制、熔断计数仅单测、只读事务不强制拒写、浏览器与外部链路未验证）已记录在二之四节；若后续 Step 需要更强证据，应在有真实同步负载与真实外部环境时补做。
5. **Step 5 待办优先级建议**：先做二之四节第 1、2 项（浏览器导入导出 + 两库交叉导入人工核对），这两项无需云凭据即可完成；再做第 3 项（真实云/SMTP/Webhook，需用户凭据）；第 4 项（远端 CI）可与 Step 6 一并安排。
6. **Step 6 待办优先级建议**：Docker 构建与容器 health/stop 已于 2026-09-24 完成。剩余三项：① **重启 Docker Desktop** 后执行二之五节第 2 项 Node 24 容器化交叉复验（重新生成 `dist` 并与本机产物逐字节比对）；② 执行二之五节第 1 项浏览器人工回归（同时可补充 Step 5 的前端导入导出证据）；③ 远端 CI 与 O5-02 一并安排。Step 6 的浏览器回归**不替代** Step 5 的两库人工交叉导入、真实云 API、SMTP 与 Webhook。
