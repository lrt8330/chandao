# 禅道托盘助手实施计划

> **执行说明：** 用户已授权初始化 Git 仓库并推送到 `https://github.com/lrt8330/chandao`。远端当前为空；每个任务完成后提交，全部验收通过后推送。不要强制推送，不要提交密码、Cookie、`state.json` 或本机配置。

**目标：** 构建一个 NSGo 原生 Windows 托盘程序，读取当前用户指派给本人的禅道任务和 Bug，提供主工作台、新增/状态变化通知以及禅道详情跳转。

**架构：** `internal/zentao` 负责只读 HTTP 会话和页面解析；`internal/app` 负责基线、轮询、变化检测和本地状态；NSGo `Root` 组件负责主工作台、设置、详情和托盘。密码通过 NSGo `cx.Secrets()` 保存到 Windows 凭据管理器，业务状态保存为不含敏感信息的 JSON。

**技术栈：** Go 1.27.3+、NSGo、`net/http`、`golang.org/x/net/html`、NSGo signals/components、Windows Credential Manager、NSGo 系统通知与托盘 API。

---

## Task 0: 工具链与 NSGo 能力核验

**文件：**
- 创建：`go.mod`
- 创建：`go.sum`
- 创建：`nsgo.yaml`

**步骤：**
1. 运行 `go version`，确认版本至少为 `go1.27.3`；不足时安装该版本或允许 Go 自动工具链下载。
2. 运行 `go install github.com/nsgo-dev/nsgo/cmd/nsgo@latest`。
3. 运行 `nsgo version` 和 `nsgo doctor`，确认 `windows_x64` 可构建、可运行。
4. 阅读 NSGo 仓库自带技能：`https://github.com/nsgo-dev/nsgo/tree/main/skills/nsgo`，重点确认 `desktop.NewTray`、`OpenPanel`、`ShowNotification`、`OnNotificationPress`、`Secrets` 和窗口隐藏 API。
5. 在空目录运行 `nsgo init -module chandao/zentaotray .`，保留生成的标准文件。
6. 修改 `nsgo.yaml`：应用名 `禅道助手`、ID `dev.chandao.zentaotray`、版本 `0.1.0`、目标 `windows_x64`、图标 `assets/IconWindows.png`。
7. 运行 `nsgo check`，必须通过。

**检查点：** `nsgo doctor` 显示 Windows 目标可用，`nsgo check` 无错误。

## Task 1: 领域模型与变化检测（TDD）

**文件：**
- 创建：`internal/app/model.go`
- 创建：`internal/app/changes.go`
- 创建：`internal/app/changes_test.go`

**步骤：**
1. 写失败测试，定义 `ItemType`、`Item`、`Change`、`ChangeKind`。
2. 运行 `go test ./internal/app`，确认因类型未定义失败。
3. 实现最小模型：

```go
type ItemType string

const (
	ItemTask ItemType = "task"
	ItemBug  ItemType = "bug"
)

type Item struct {
	ID        string
	Type      ItemType
	Title     string
	Project   string
	Status    string
	UpdatedAt string
	URL       string
}

func (i Item) Key() string { return string(i.Type) + ":" + i.ID }

type ChangeKind string

const (
	ChangeNew     ChangeKind = "new"
	ChangeStatus  ChangeKind = "status"
)

type Change struct {
	Kind ChangeKind
	Item Item
	Old  string
	New  string
}
```

4. 写失败测试，覆盖首次基线、新增、状态变化、重复数据、事项消失和空列表。
5. 实现 `DetectChanges(previous, current map[string]Item, first bool) []Change`：
   - `first == true` 返回空变化。
   - 当前项不存在于 previous：`ChangeNew`。
   - 已存在但 `Status` 不同：`ChangeStatus`。
   - 其他情况不产生变化。
6. 运行 `go test ./internal/app`，全部通过。

**检查点：** 基线、新增、状态变化和失败重试不会误报。

## Task 2: 禅道登录与真实页面取证

**文件：**
- 创建：`internal/zentao/client.go`
- 创建：`internal/zentao/login_test.go`
- 创建：`testdata/login.html`
- 创建：`testdata/my.html`

**步骤：**
1. 使用浏览器从禅道登录入口真实登录，打开开发者工具 Network。
2. 记录登录请求 URL、方法、表单字段、密码是否前端哈希、重定向链和会话 Cookie 名称。
3. 打开 `/biz/my`，记录任务与 Bug 区块的真实 HTML 结构、详情 URL 格式和状态文本。
4. 将脱敏后的登录页与“我的”页保存为 `testdata`，不包含 Cookie、密码或真实敏感业务内容。
5. 写失败测试，验证登录页可提取 `account`、`password`、`referer` 及实际存在的隐藏字段。
6. 实现 `Client`：`BaseURL`、`Account`、`Password`、`http.Client`、`Jar`、`Login(ctx)`、`FetchMy(ctx)`。
7. 登录请求只使用 GET/POST 读取流程，不调用任何写接口。
8. 使用提供账号做一次最小真实验证：确认登录成功且能读取“我的”页；验证后不输出 Cookie 或密码。

**检查点：** 自动登录成功，`FetchMy` 返回登录后页面而非登录页。

## Task 3: 任务与 Bug 页面解析（TDD）

**文件：**
- 创建：`internal/zentao/parse.go`
- 创建：`internal/zentao/parse_test.go`

**步骤：**
1. 写失败测试，输入脱敏 `testdata/my.html`，断言能解析任务与 Bug。
2. 运行 `go test ./internal/zentao`，确认失败。
3. 使用 `golang.org/x/net/html` 实现语义解析，不依赖脆弱的全文正则。
4. 每个事项提取：类型、ID、标题、项目、状态、更新时间、详情 URL。
5. 仅保留指派给当前账号且未完成/未解决的事项。
6. 详情 URL 使用禅道页面中的真实链接，并限制为配置的禅道 Origin。
7. 增加登录失效页、空列表、重复 ID、缺失字段测试。
8. 运行 `go test ./internal/zentao`，全部通过。

**检查点：** 解析结果与浏览器“我的”页逐项一致。

## Task 4: 本地状态与安全账号存储

**文件：**
- 创建：`internal/app/store.go`
- 创建：`internal/app/store_test.go`
- 创建：`internal/app/settings.go`

**步骤：**
1. 写失败测试，覆盖状态文件保存/加载、损坏文件回退、空基线。
2. 实现状态结构：`Items map[string]Item`、`LastSuccess time.Time`、`NotificationsEnabled bool`、`PollSeconds int`。
3. 状态文件写入 `cx.DataDir()/state.json`，使用临时文件后改名，避免半写文件。
4. 实现 NSGo 安全存储封装：账号和密码分别使用 `cx.Secrets().Get/Set`，服务名使用应用 ID。
5. 写测试，验证状态文件不包含 `Password`、`Cookie`、`Account` 字段。
6. 默认值：轮询 60 秒、通知开启；允许范围 30–300 秒。

**检查点：** 重启后基线和设置保留，密码只存在于 Windows 凭据管理器。

## Task 5: 轮询、会话恢复与通知协调

**文件：**
- 创建：`internal/app/controller.go`
- 创建：`internal/app/controller_test.go`

**步骤：**
1. 写失败测试，使用假 `ZentaoClient` 接口模拟成功、登录失效、网络失败和解析失败。
2. 实现最小 `Controller`：`Refresh(ctx)`、`Start(ctx)`、`Stop()`。
3. `Refresh` 流程：登录/复用会话 → 拉取 → 解析 → 检测变化 → 更新状态 → 保存 → 发布 UI 事件。
4. 登录失效时自动重新登录一次；仍失败则保留旧数据和旧基线。
5. 请求失败或解析失败不调用 `DetectChanges`，不发送通知。
6. 使用 `time.Ticker` 轮询；手动刷新与自动刷新串行，避免并发覆盖。
7. 通知事件包含 `Change` 和事项 Key，供 UI 定位。
8. 运行 `go test ./internal/app`，全部通过。

**检查点：** 连续刷新不重复通知；失败轮询不产生假变化。

## Task 6: 主工作台 UI

**文件：**
- 创建：`ui/theme.go`
- 创建：`ui/workbench.go`
- 创建：`ui/item_detail.go`
- 创建：`ui/settings.go`
- 创建：`ui/workbench_test.go`

**步骤：**
1. 在 `ui/theme.go` 定义设计文档中的云白/钴蓝颜色、圆角、间距和字体层级。
2. 写失败 UI 测试：工作台显示“未完成任务”“未解决 Bug”“本轮新增”“本轮状态变化”。
3. 实现顶部栏：应用名称、账号、连接状态、最后刷新时间、立即刷新按钮。
4. 实现汇总卡片和筛选：全部、任务、Bug、仅看本轮变化。
5. 实现事项列表：类型标识、标题、项目、状态、更新时间。
6. 实现详情面板：完整事项信息和“在禅道打开”按钮。
7. 实现空状态、加载状态、登录失效、网络失败、解析失败状态。
8. 运行 `nsgo test`，断言文本和按钮存在，筛选与详情交互通过。

**检查点：** 主工作台可在无窗口测试中完成筛选、查看详情和打开 URL 请求。

## Task 7: 托盘、窗口生命周期与系统通知

**文件：**
- 修改：`app.go`
- 创建：`desktop/tray.go`
- 创建：`desktop/notifications.go`
- 创建：`desktop/tray_test.go`

**步骤：**
1. 写失败测试，验证托盘菜单包含“打开工作台”“立即刷新”“通知开关”“退出”。
2. 使用 `desktop.NewTray` 创建托盘图标和菜单。
3. 主窗口关闭时隐藏而不是退出；托盘“打开工作台”恢复窗口。
4. 使用 `cx.ShowNotification` 发送通知，ID 为 `type:id`。
5. 使用 `cx.OnNotificationPress` 解析 ID，打开工作台并选中事项。
6. 实现“在禅道打开”：先 `core.CheckURL`，再 `cx.OpenURL`。
7. 同一轮超过 5 条变化时，发送 5 条明细和 1 条汇总通知。
8. 托盘“退出”调用 `desktop.RequestQuit`；`cx.OnQuit` 停止轮询并保存状态。
9. 运行 `nsgo test`，全部通过。

**检查点：** 托盘、通知、点击定位、URL 打开和退出路径均有测试或可观察结果。

## Task 8: 真实账号联调与页面一致性

**文件：**
- 修改：发现解析偏差的 `internal/zentao/*.go`
- 创建：`docs/verification/2026-11-27-real-account.md`

**步骤：**
1. 启动应用，在设置中输入提供账号密码并登录。
2. 记录工作台任务数、Bug 数与禅道“我的”页人工计数，要求一致。
3. 验证首次启动不发送历史通知。
4. 等待或构造一条新增/状态变化，验证右下角系统通知。
5. 点击通知，验证工作台打开并定位正确事项。
6. 点击“在禅道打开”，验证默认浏览器打开正确详情页。
7. 关闭工作台，确认托盘仍存在；执行立即刷新和退出。
8. 检查源码、配置文件、状态文件和日志，确认不含明文密码或 Cookie。

**检查点：** 真机登录、数据一致性、通知、跳转、托盘常驻全部通过。

## Task 9: Windows 构建与交付校验

**文件：**
- 创建：`assets/IconWindows.png`
- 创建：`docs/verification/2026-11-27-build.md`

**步骤：**
1. 使用云白/钴蓝风格生成 256×256 应用图标。
2. 运行 `go test ./...`。
3. 运行 `nsgo test`。
4. 运行 `nsgo check`。
5. 运行 `nsgo build -target windows_x64`。
6. 核对 `dist/releases` 中安装包、`.sha256`、应用目录和图标。
7. 运行安装包或便携目录中的程序，重复 Task 8 的关键验收。
8. 记录可执行文件、安装包、SHA-256、测试结果和已知限制。

**检查点：** 交付目录存在且每个文件体积稳定、校验和匹配、程序可启动。

## 完成标准

- 原生 Go 成功登录并读取禅道“我的”任务和 Bug。
- 主工作台数据与禅道页面一致。
- 首次基线不补弹，后续新增和状态变化准确通知。
- 通知点击定位和浏览器跳转可用。
- 托盘常驻、手动刷新、通知开关和退出可用。
- 密码不进入源码、配置、状态或日志。
- `go test ./...`、`nsgo test`、`nsgo check`、Windows 构建均通过。
