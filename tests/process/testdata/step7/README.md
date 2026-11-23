# Build9 Step 7 本地过程验收

沿用 `step16` 独立标签、`tests/process` 回环云服务和链接期端点注入。
测试从当前源码构建正式入口二进制；SQLite、DNS、TEO、Lighthouse 和 Chrome
profile 均为本次验收隔离资源，不使用用户配置或真实云凭据。

在仓库根目录运行：

```sh
make all
go test -tags=step16 ./tests/process -run '^TestBuild9S7' -race -count=2 -v -timeout=8m
go test -tags=step16 ./tests/process -run '^TestStep16' -race -count=1 -v -timeout=15m
go test ./provider ./syncer ./webui/api -run '^TestBuild9S[345]' -race -count=3 -timeout=20m
```

`make all` 先构建真实前端。浏览器用本机 Chrome（默认 macOS 安装路径，可用
`CHROME_BIN` 指定）和 Node 内置 WebSocket，缺失时失败，不跳过。
`-race` 检查 Go 测试进程和回环服务；由夹具构建的产品子进程沿用正常 `go build`。
以上独立过程测试不进入默认 Go 门禁、Make、CI、Docker 或发布构建。

`build9_step7_process_test.go` 验证：

- HTTP 绑定不启用压缩；设置与临时 SQLite 跨重启一致。
- 95 条手工规则下两个离散 IPv4/两个端口压成一条 `/30`，峰值 96、额外 IPv4 为 2；模拟零写入。
- 关闭时满 100 条保留旧行与待复查；释放七条后先新增四条精确规则，再删除一条旧压缩规则，回读为 96 条并解除待复查；同一二进制继续手动清理四条至 92，再恢复正式同步重建四条至 96。
- S1 缺 `443` 时零删除/零确认；恢复完整回读后确认一次，同指纹不重复确认。
- 待更新版借用 96→100，实际生效并重启后只删除旧版四条，恢复为 96；同峰值但预计清理后 97 条的对照零增删/零确认。
- 暂停下预览/单次受理/幂等确认/逐目标回读；保留相似 TAG、DNS TAG、手工规则；开启时可重建，清理前关闭 EdgeOne 后不重建。
- TEO 确认在途时维护排队；两个目标一个删除成功、一个权限失败，分别保留确认计数和实际残留，整体不报清空，accepted 过渡记录保留。

`browser.mjs` 验证真实 DOM 的绑定提示、实例设置取消/双击保存、模拟风险与规则展示、
清理预览取消、清理确认双击守卫。CDP 在服务端受理响应阶段制造连接中断：
断开后不自动重发，刷新保留原预览标识，用户显式核对同一操作，再次刷新只 GET；
最终云端仅删除一次。日志使用 `B9S7_BROWSER` 前缀，截图写 `/tmp/fwalizer-step16-b9s7-*.png`。

四平台独立真 SDK 删除线格式、全快照唯一定位/版本保护及超时/回读失败等场景，
由既有 `TestBuild9S4*` / `TestBuild9S5*` 夹具回归，和 Lighthouse 产品过程证据分别记录。
本地替身结果不代表真实云写删、实际换版窗口、远端 CI 或镜像发布通过；这些留 Build9 Step 8。
