# 禅道助手（chandao）

NSGo 原生 Go 桌面应用：跟踪指派给我的禅道任务、需求与 Bug，本地增量
同步、变化弹窗提醒、报表查询，并可直接在应用内完成禅道生命周期操作，
无需打开浏览器。

## 功能

- **待办列表**：任务 / 需求 / Bug 按项目分组，类型筛选、仅看本轮变化、
  选中查看详情
- **本地搜索与项目概览**：在待办列表输入标题、项目名或编号，回车或点「搜索」后生效。`#108` 精确匹配各类事项的编号，`bug:108` 只匹配该 Bug。「项目概览」汇总当前账号本机快照中的任务、需求和 Bug，点项目即可聚焦，也可与类型、关键字和本轮变化筛选组合。断网仍可浏览缓存；这些操作不请求禅道，不改变后台轮询频率。
- **禅道操作**：按状态提供 开始/完成/暂停/继续/取消/关闭（任务）、
  解决/激活/关闭（Bug），确认后写入禅道并自动刷新
- **增量同步**：默认 60 秒轮询（30~300 可调），只抓变化条目；每 20 轮
  或大账号每小时做一次全量对账
- **消息弹窗**：检出的变化逐条或摘要弹窗，点击定位到列表行；托盘可开关
  （关闭只静音弹窗，变化仍写入本地历史与报表）
- **通知历史**：本地 SQLite 持久保留变化记录（启动时自动裁剪到最近
  1 万条），可回查
- **报表（独立窗体）**：待办总量演变折线、按天变化统计、事件明细查询
  （范围/类型/关键字）、任务/Bug 构成占比
- **命令面板**：`Ctrl+K` 搜索并执行全部动作；`Ctrl+R` 立即刷新

## 构建与安装

```sh
go test ./...                            # 回归
go tool nsgo build -target windows_x64   # 产出 dist\
```

日常使用的安装在 `C:\Users\Administrator\AppData\Local\Programs\禅道助手\`，
**改代码后两个位置都要更新**（详见 docs/zentao-integration.md 排障一节）：

```sh
cp dist/禅道助手-0.1.0-windows_x64/禅道助手.exe \
   "C:/Users/Administrator/AppData/Local/Programs/禅道助手/禅道助手.exe"
```

开发调试：`go tool nsgo dev`（nsgo.yaml 默认 native 目标 + 热重载）。

## 禅道集成

默认服务器地址 `http://192.168.2.225/biz/`，可用环境变量
`ZENTAO_BASE_URL` 覆盖。登录凭据按「服务器 + 账号」隔离保存到系统凭据
管理器，不落盘；快照与通知历史同样按账号分目录，换账号不会看到上一
账号的数据。升级后首次启动需要重新登录一次。

同步端点、预算、操作实现与**实机校验流程**见
[docs/zentao-integration.md](docs/zentao-integration.md)。

网络恢复后建议先跑一次实机校验（确认需求列表与操作表单的页面结构假设）：

```sh
go run ./cmd/zentaoctl -base http://192.168.2.225/biz/ -account 账号 -password 密码
```

## 代码结构

```
app.go / runtime.go            窗口装配与会话运行时
internal/domain/               事项与变化模型
internal/application/          控制器：轮询、增量/对账、操作
internal/infrastructure/       禅道客户端与解析、SQLite 历史、凭据
internal/ui/                   工作台、详情、报表窗体、弹窗、命令面板
cmd/zentaoctl/                 禅道实机校验工具
testdata/                      页面夹具（live/ 为实机抓取，不入库）
```
