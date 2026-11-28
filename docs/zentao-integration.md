# 禅道集成指南

本文记录 chandao 与禅道服务器的集成方式、验证状态与排障方法，供后续
维护与实机校验使用。适用禅道 22.7（其余版本未验证，老版走 legacy 解析
分支，夹具见 `testdata/*_legacy.html`）。

## 数据同步（只读）

### 端点

指派列表统一走 `my` 模块，每页 100 条：

```
GET index.php?m=my&f={task|bug|story}&browseType=assignedTo&param=0&orderBy={id_desc|lastEditedDate_desc}&recTotal=0&recPerPage=100&pageID=N
```

- 全量同步按 `id_desc` 翻页，只保留未完结状态
- 增量同步按 `lastEditedDate_desc` 翻页，遇整页旧数据即停（含 2 分钟
  安全余量，覆盖时间戳精度与时钟偏差）
- 需求页没有项目列，项目名退回 `c-plan`，再退 `c-product`

### 策略与预算（application/controller.go、zentao/service.go）

| 场景 | 行为 | 请求量 |
|---|---|---|
| 首次登录建基线 | 全量翻页 | `ceil(条数/100)` × 3 类对象 |
| 日常稳态 | 增量同步 | 通常每类 1~2 页 |
| 全量对账 | 每 20 轮一次 | 同首次 |
| 大账号（指派 > 5 万） | 对账硬性节流到 **1 次/小时** | 最坏数百页/次 |
| 增量超预算 | 单次 50 页上限，超了回退全量 | — |
| 防失控 | 全量 2000 页上限 | — |

轮询间隔默认 60 秒，设置里可调 30~300 秒。对服务端的压力特征：查询走
`assignedTo` 索引、只触碰当前用户子集，与禅道总量（几百万行）无关；主要
成本是 PHP 页面渲染次数。唯一需要留意的是超大账号的首轮全量突发，建议
错峰登录。

### 解析

`parse.go` 支持两种页面形态：现代 dtable JSON 与老版 `<table>`（列位
映射）。夹具在 `testdata/`，解析器以「表格 ID + 单元格 class」定位：
任务 `tasktable`、Bug `bugList`、需求 `storyList`；项目列 `c-project`
（需求退 `c-plan`/`c-product`）、状态 `c-status`、更新时间
`c-lastEditedDate`、标题 `c-title`/`c-name`。

**需求页的列名假设未经真实环境验证**（夹具为合成）。若实机解析失败，
按 `zentaoctl` 输出把真实页面拿回来对照修改即可。

## 事项操作（写）

### 实现方式（zentao/actions.go）

等效于在禅道页面上打开操作弹窗直接点「确定」：

1. `GET index.php?m={type}&f={action}&{type}ID={id}` 抓操作页
2. 解析表单：隐藏域（token 等）、输入框/下拉选中值/文本域的**页面预填值**
   全部回填（工时、解决方案等保持禅道默认）
3. `POST` 表单 action 地址，`overrides` 可覆盖同名字段（当前 UI 未暴露）
4. 响应是登录页即视为会话失效

动作与对象走白名单（task/bug/story × start/finish/pause/cancel/close/
activate/resolve/confirm/assign/transfer/recordEstimate），防注入。
Controller 在会话过期时自动重登录并重试一次。

### 按状态可用的操作（ui/item_actions.go）

| 对象 | 状态 | 按钮 |
|---|---|---|
| 任务 | wait | 开始 / 取消 |
| 任务 | doing | 完成 / 暂停 / 取消 |
| 任务 | pause | 继续 / 取消 |
| 任务 | done/cancel | 关闭 |
| Bug | active | 解决 / 关闭 |
| Bug | resolved | 激活 / 关闭 |
| Bug | closed | 激活 |
| 需求 | — | 暂不开放（只读） |

UI 上每一步都要过 AlertDialog 确认；成功后自动刷新，错误回显在操作行。

### ⚠ 验证状态

- 已验证：httptest 全链路（登录 → 抓表单 → 回填 → POST → 覆盖备注）、
  会话过期重试、UI 全流程（`internal/ui/item_actions_test.go`）
- **未验证：真实禅道操作页的表单结构**（服务器在开发期不可达）。
  恢复后先跑一次实机校验（下节），或在一条测试任务上点「开始」实测。

## 实机校验（zentaoctl）

网络可达时在仓库根目录执行：

```sh
go run ./cmd/zentaoctl -base http://192.168.2.225/biz/ \
    -account 账号 -password 密码
```

工具会：登录 → 抓取任务/Bug/需求三个指派列表首页 → 用仓库解析器即时
解析 → 存原始页面到 `testdata/live/` → 逐项报告 ✓/✗。

可选校验操作表单（默认只抓不提交）：

```sh
go run ./cmd/zentaoctl -base … -account … -password … -form task:start:108
# 确认字段无误后，加 -submit 真正提交（会写禅道）
```

任何 ✗ 项：把 `testdata/live/` 下对应页面发给维护者（或贴给 AI 会话），
对照调整解析器后重跑。

### 夹具更新流程

1. `zentaoctl` 抓到的页面在 `testdata/live/`
2. 对照 `testdata/assigned_*_modern.html` 的结构修正解析器或替换夹具
   （live 页面含真实数据，**不要直接提交**，脱敏后作为夹具）
3. `go test ./internal/infrastructure/zentao/` 回归

## 排障记录

- **「网络连接异常」横幅 + 列表一直骨架屏**：禅道服务器不可达。刷新
  请求有 30 秒超时，期间加载动画持续重绘。恢复后下一轮轮询（≤5 分钟）
  自动接上，无需重启。
- **改了代码但界面没变**：`nsgo build` 只写 `dist/`；日常使用的安装在
  `C:\Users\Administrator\AppData\Local\Programs\禅道助手\`，**两个位置
  都要更新**（历史坑：dist 新、安装目录旧，导致已修复的 bug「还在」）。
  校验方法：比对两个 exe 的 SHA256。
- **禅道返回 HTTP 500 / 页面结构变化**：解析器报
  `ErrUnexpectedStructure`/`ErrIncompletePage` 时先跑 `zentaoctl` 取
  真实页面对照；禅道升级后列名变化是最常见原因。
- **UI 自动化测试/截图时窗口「表面」频繁失效**：加载动画期间界面逐帧
  重绘所致，等加载结束再操作，属预期现象。

## 相关代码索引

| 层 | 文件 |
|---|---|
| HTTP 客户端（登录/翻页/操作表单） | `internal/infrastructure/zentao/client.go`、`actions.go` |
| 解析器 | `internal/infrastructure/zentao/parse.go` |
| 同步策略（全量/增量/对账/预算） | `internal/infrastructure/zentao/service.go`、`internal/application/controller.go` |
| 操作目录与确认弹窗 | `internal/ui/item_actions.go` |
| 夹具 | `testdata/` |
| 实机校验工具 | `cmd/zentaoctl/` |
