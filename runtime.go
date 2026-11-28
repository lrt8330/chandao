package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"chandao/zentaotray/internal/application"
	"chandao/zentaotray/internal/domain"
	"chandao/zentaotray/internal/infrastructure/notifyhistory"
	"chandao/zentaotray/internal/infrastructure/security"
	"chandao/zentaotray/internal/infrastructure/storage"
	"chandao/zentaotray/internal/infrastructure/zentao"
	"chandao/zentaotray/internal/ui"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	defaultZentaoBaseURL = "http://192.168.2.225/biz/"
	accountSecretKey     = "account"
	passwordSecretKey    = "password"
)

var zentaoBaseURL = configuredZentaoBaseURL()

func configuredZentaoBaseURL() string {
	if value := strings.TrimSpace(os.Getenv("ZENTAO_BASE_URL")); value != "" {
		return value
	}
	return defaultZentaoBaseURL
}

// workbenchRuntime coordinates blocking session work with the UI goroutine.
// Controller callbacks and all signal writes are marshaled through cx.Post.
type workbenchRuntime struct {
	cx            *core.Scope
	data          *core.Signal[ui.WorkbenchData]
	selectedKey   *core.Signal[string]
	showWindow    func()
	popups        *ui.MessagePopupManager
	report        *ui.ReportWindow
	credentials   security.SecretsCredentialStore
	statePath     string
	notifyHistory *notifyhistory.Store

	ctx    context.Context
	cancel context.CancelFunc

	// opMu serializes login, manual refresh, and settings writes. controllerMu
	// separately protects the pointer used by quit and callback generations.
	opMu         sync.Mutex
	controllerMu sync.Mutex
	controller   *application.Controller
	account      string
	generation   uint64
}

func newWorkbenchRuntime(cx *core.Scope, data *core.Signal[ui.WorkbenchData], selectedKey *core.Signal[string], showWindow func(), history *notifyhistory.Store) *workbenchRuntime {
	ctx, cancel := context.WithCancel(cx.Context())
	return &workbenchRuntime{
		cx:            cx,
		data:          data,
		selectedKey:   selectedKey,
		showWindow:    showWindow,
		popups:        ui.NewMessagePopupManager(cx, func(key string) { selectedKey.Set(key); showWindow() }, showWindow),
		report:        ui.NewReportWindow(cx, history),
		credentials:   security.SecretsCredentialStore{Secrets: cx.Secrets()},
		statePath:     filepath.Join(cx.DataDir(), "state.json"),
		notifyHistory: history,
		ctx:           ctx,
		cancel:        cancel,
	}
}

// openReport 打开独立报表窗体；重复打开时只显示并聚焦已有窗体。
func (r *workbenchRuntime) openReport() { r.report.Open() }

// runItemAction 在后台对一条事项执行生命周期操作（开始/完成/解决/关闭
// 等），成功后立即刷新；done 在 UI 线程收到人类可读的错误（成功为空串）。
func (r *workbenchRuntime) runItemAction(item domain.Item, action string, done func(errorMessage string)) {
	go func() {
		r.opMu.Lock()
		err := func() error {
			controller, _, _, ok := r.currentController()
			if !ok {
				return errors.New("请先登录禅道账号。")
			}
			ctx, cancel := context.WithTimeout(r.ctx, 30*time.Second)
			defer cancel()
			return controller.RunItemAction(ctx, item, action)
		}()
		r.opMu.Unlock()
		r.cx.Post(func() {
			if err != nil {
				done(userMessage(err))
				return
			}
			r.refresh()
			done("")
		})
	}()
}

func (r *workbenchRuntime) start() {
	r.cx.OnQuit(func(context.Context) { r.stop() })
	go r.loadSavedCredentials()
}

func (r *workbenchRuntime) stop() {
	r.cancel()
	r.opMu.Lock()
	defer r.opMu.Unlock()

	r.controllerMu.Lock()
	controller := r.controller
	r.controller = nil
	r.account = ""
	r.generation++
	r.controllerMu.Unlock()
	if controller != nil {
		controller.Stop()
	}
}

func (r *workbenchRuntime) login(account, password string) {
	account = strings.TrimSpace(account)
	if account == "" || strings.TrimSpace(password) == "" {
		r.postError("请输入禅道账号和密码。", true)
		return
	}
	r.setLoading(true)
	go func() {
		r.opMu.Lock()
		defer r.opMu.Unlock()

		r.stopController()
		controller, err := r.newController(account, password)
		if err != nil {
			r.postError(userMessage(err), true)
			return
		}

		// Verify both login and assigned-item access before keeping credentials.
		result, err := controller.Refresh(r.ctx)
		if err != nil {
			// Any failed verification leaves the user on the login panel.
			r.postError(userMessage(err), true)
			return
		}
		if err := r.credentials.Set(accountSecretKey, account); err != nil {
			r.postError(secureSaveMessage, true)
			return
		}
		if err := r.credentials.Set(passwordSecretKey, password); err != nil {
			// Never fall back to plaintext. Remove the incomplete account entry
			// when possible so the next launch asks for a complete login.
			_ = r.credentials.Delete(accountSecretKey)
			r.postError(secureSaveMessage, true)
			return
		}

		r.installController(controller, account)
		r.cx.Post(func() {
			r.data.Set(applyRefresh(r.data.Get(), account, result))
			r.data.Update(func(data ui.WorkbenchData) ui.WorkbenchData {
				data.Loading = true // Controller.Start performs an immediate poll.
				return data
			})
			// Ask only after an explicit, successful login. A denial is
			// asynchronous and does not affect the established session.
			if result.Snapshot.NotificationsEnabled {
				r.cx.RequestNotificationPermission(nil)
			}
		})
		r.startPollingController(controller, account)
	}()
}

func (r *workbenchRuntime) loadSavedCredentials() {
	r.opMu.Lock()
	defer r.opMu.Unlock()

	account, found, err := r.credentials.Get(accountSecretKey)
	if err != nil {
		r.postError(secureReadMessage, true)
		return
	}
	if !found {
		return
	}
	password, found, err := r.credentials.Get(passwordSecretKey)
	if err != nil {
		r.postError(secureReadMessage, true)
		return
	}
	if !found || strings.TrimSpace(account) == "" || strings.TrimSpace(password) == "" {
		r.postError("已保存的登录凭据不完整，请重新登录。", true)
		return
	}

	controller, err := r.newController(account, password)
	if err != nil {
		r.postError(userMessage(err), true)
		return
	}
	r.installController(controller, account)
	// 启动即渲染持久化快照：账号巨大时首轮全量刷新要数分钟，
	// 界面不应停留在空列表等待。
	snapshot := controller.CurrentSnapshot()
	r.cx.Post(func() {
		r.data.Update(func(data ui.WorkbenchData) ui.WorkbenchData {
			data.Account = account
			data.NeedsLogin = false
			data.Connected = false
			data.Loading = true
			data.ErrorMessage = ""
			data.Items = snapshotItems(snapshot.Items)
			data.Changes = nil
			data.LastSuccess = snapshot.LastSuccess
			data.PollSeconds = snapshot.PollSeconds
			data.NotificationsEnabled = snapshot.NotificationsEnabled
			data.PopupSeconds = snapshot.PopupSeconds
			return data
		})
	})
	// Saved credentials are reused directly and are never written back here.
	r.startController(controller, account)
}

func (r *workbenchRuntime) newController(account, password string) (*application.Controller, error) {
	client, err := zentao.New(zentaoBaseURL, account, password)
	if err != nil {
		return nil, err
	}
	return application.NewController(
		r.ctx,
		zentao.NewService(client),
		storage.FileSnapshotStore{Path: r.statePath},
		messagePopupNotifier{manager: r.popups, history: r.notifyHistory, duration: func() int { return r.data.Get().PopupSeconds }},
	)
}

// recordSnapshot 把一次成功刷新的总量快照写入本地报表库。
func (r *workbenchRuntime) recordSnapshot(snapshot application.Snapshot) {
	if r.notifyHistory == nil {
		return
	}
	point := notifyhistory.SnapshotPoint{}
	for _, item := range snapshot.Items {
		switch {
		case item.Type == domain.ItemTask:
			point.TasksTotal++
			switch strings.ToLower(item.Status) {
			case "doing":
				point.TasksDoing++
			case "wait":
				point.TasksWait++
			case "pause":
				point.TasksPause++
			}
		case item.Type == domain.ItemBug && strings.EqualFold(item.Status, "active"):
			point.BugsActive++
		}
	}
	point.Total = len(snapshot.Items)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.notifyHistory.AppendSnapshot(ctx, snapshot.LastSuccess, point); err != nil {
		_ = err
	}
}

func (r *workbenchRuntime) refresh() {
	if r.data.Get().Loading || r.data.Get().NeedsLogin {
		return
	}
	r.setLoading(true)
	go func() {
		r.opMu.Lock()
		defer r.opMu.Unlock()

		controller, generation, account, ok := r.currentController()
		if !ok {
			r.postError("请先登录禅道账号。", true)
			return
		}
		result, err := controller.Refresh(r.ctx)
		if err != nil {
			r.postControllerError(generation, account, err)
			return
		}
		r.cx.Post(func() {
			if !r.isCurrent(generation) {
				return
			}
			r.data.Set(applyRefresh(r.data.Get(), account, result))
		})
	}()
}

func (r *workbenchRuntime) updateSettings(pollSeconds int, notificationsEnabled bool, popupSeconds int) {
	go func() {
		r.opMu.Lock()
		defer r.opMu.Unlock()

		controller, generation, account, ok := r.currentController()
		if !ok {
			r.postError("请先登录后再保存设置。", true)
			return
		}
		if err := controller.UpdateSettings(r.ctx, pollSeconds, notificationsEnabled, popupSeconds); err != nil {
			r.postControllerError(generation, account, err)
			return
		}
		r.cx.Post(func() {
			if !r.isCurrent(generation) {
				return
			}
			r.data.Update(func(data ui.WorkbenchData) ui.WorkbenchData {
				data.PollSeconds = pollSeconds
				data.NotificationsEnabled = notificationsEnabled
				data.PopupSeconds = popupSeconds
				data.ErrorMessage = ""
				return data
			})
		})
	}()
}

func (r *workbenchRuntime) openURL(rawURL string) {
	openError := "无法打开禅道页面，请检查链接或默认浏览器设置。"
	if core.CheckURL(rawURL) != nil {
		r.postError(openError, false)
		return
	}
	r.cx.OpenURL(rawURL, func(opened bool) {
		if !opened {
			r.postError(openError, false)
		}
	})
}

func (r *workbenchRuntime) setLoading(loading bool) {
	r.data.Update(func(data ui.WorkbenchData) ui.WorkbenchData {
		data.Loading = loading
		data.ErrorMessage = ""
		return data
	})
}

func (r *workbenchRuntime) postError(message string, needsLogin bool) {
	r.cx.Post(func() {
		r.data.Update(func(data ui.WorkbenchData) ui.WorkbenchData {
			data.Loading = false
			data.Connected = false
			data.NeedsLogin = needsLogin
			data.ErrorMessage = message
			return data
		})
	})
}

func (r *workbenchRuntime) postControllerError(generation uint64, account string, err error) {
	r.cx.Post(func() {
		if !r.isCurrent(generation) {
			return
		}
		message := userMessage(err)
		r.data.Update(func(data ui.WorkbenchData) ui.WorkbenchData {
			data.Account = account
			data.Loading = false
			data.Connected = false
			data.NeedsLogin = requiresRelogin(err)
			data.ErrorMessage = message
			return data
		})
	})
}

func (r *workbenchRuntime) installController(controller *application.Controller, account string) {
	r.controllerMu.Lock()
	defer r.controllerMu.Unlock()
	r.controller = controller
	r.account = account
	r.generation++
}

func (r *workbenchRuntime) stopController() {
	r.controllerMu.Lock()
	controller := r.controller
	r.controller = nil
	r.account = ""
	r.generation++
	r.controllerMu.Unlock()
	if controller != nil {
		controller.Stop()
	}
}

func (r *workbenchRuntime) currentController() (*application.Controller, uint64, string, bool) {
	r.controllerMu.Lock()
	defer r.controllerMu.Unlock()
	if r.controller == nil {
		return nil, 0, "", false
	}
	return r.controller, r.generation, r.account, true
}

func (r *workbenchRuntime) isCurrent(generation uint64) bool {
	r.controllerMu.Lock()
	defer r.controllerMu.Unlock()
	return r.controller != nil && r.generation == generation
}

func (r *workbenchRuntime) startController(controller *application.Controller, account string) {
	r.startControllerWithMode(controller, account, true)
}

func (r *workbenchRuntime) startPollingController(controller *application.Controller, account string) {
	r.startControllerWithMode(controller, account, false)
}

func (r *workbenchRuntime) startControllerWithMode(controller *application.Controller, account string, refreshImmediately bool) {
	r.controllerMu.Lock()
	generation := r.generation
	r.controllerMu.Unlock()
	onResult := func(result application.RefreshResult) {
		r.cx.Post(func() {
			if !r.isCurrent(generation) {
				return
			}
			r.data.Set(applyRefresh(r.data.Get(), account, result))
		})
	}
	onError := func(err error) { r.postControllerError(generation, account, err) }
	if refreshImmediately {
		controller.Start(r.ctx, onResult, onError)
		return
	}
	controller.StartPolling(r.ctx, onResult, onError)
}

type messagePopupNotifier struct {
	manager  *ui.MessagePopupManager
	history  *notifyhistory.Store
	duration func() int
}

// Notify 先把变化写入本地 SQLite 历史（失败仅降级，不影响通知），再交给
// 弹窗管理器分流：小批次逐条弹、大批次只弹一张摘要卡。
func (n messagePopupNotifier) Notify(ctx context.Context, changes []domain.Change) {
	if n.history != nil {
		at := time.Now()
		entries := make([]notifyhistory.Entry, 0, len(changes))
		for _, change := range changes {
			entries = append(entries, notifyhistory.Entry{
				Time:      at,
				Kind:      string(change.Kind),
				ItemType:  string(change.Item.Type),
				ItemID:    change.Item.ID,
				Title:     change.Item.Title,
				Project:   change.Item.Project,
				OldStatus: change.Old,
				NewStatus: change.New,
			})
		}
		if err := n.history.Append(ctx, at, entries); err != nil {
			_ = err
		}
	}
	n.manager.Show(changes, n.duration())
}

func snapshotItems(items map[string]domain.Item) []domain.Item {
	result := make([]domain.Item, 0, len(items))
	for _, item := range items {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key() < result[j].Key() })
	return result
}

func applyRefresh(current ui.WorkbenchData, account string, result application.RefreshResult) ui.WorkbenchData {
	current.Account = account
	current.NeedsLogin = false
	current.Connected = true
	current.Loading = false
	current.ErrorMessage = ""
	current.Items = snapshotItems(result.Snapshot.Items)
	current.Changes = result.Changes
	current.LastSuccess = result.Snapshot.LastSuccess
	current.PollSeconds = result.Snapshot.PollSeconds
	current.NotificationsEnabled = result.Snapshot.NotificationsEnabled
	current.PopupSeconds = result.Snapshot.PopupSeconds
	return current
}

func requiresRelogin(err error) bool {
	return errors.Is(err, application.ErrSessionExpired) ||
		errors.Is(err, zentao.ErrSessionExpired) ||
		errors.Is(err, zentao.ErrLoginFailed) ||
		errors.Is(err, zentao.ErrMissingCredentials)
}

const (
	secureReadMessage   = "无法读取安全存储中的登录凭据，请重新登录。"
	secureSaveMessage   = "无法安全保存登录凭据，请检查系统凭据管理器后重试。"
	stateLoadMessage    = "本地状态文件读取失败，请检查数据目录后重试。"
	stateSaveMessage    = "本地状态保存失败，请检查数据目录的写入权限。"
	loginMessage        = "账号或密码不正确，请检查后重试。"
	sessionMessage      = "禅道登录已过期，请重新登录。"
	parseMessage        = "禅道页面数据解析失败，请稍后重试或联系管理员。"
	networkMessage      = "网络连接异常或禅道暂时不可用，请稍后重试。"
	settingsMessage     = "设置无效，轮询间隔必须为 30 到 300 秒。"
	cancelledMessage    = "操作已取消，请重试。"
	baseURLMessage      = "禅道服务地址配置无效，请联系管理员。"
	responseSizeMessage = "禅道返回的数据过大，请稍后重试。"
)

// userMessage maps internal failures to fixed Chinese text. Raw errors can
// contain local paths, request URLs, or transport details and are never shown.
func userMessage(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return cancelledMessage
	}
	if errors.Is(err, core.ErrSecretsUnavailable) {
		return secureReadMessage
	}
	if errors.Is(err, application.ErrStateLoad) {
		return stateLoadMessage
	}
	if errors.Is(err, application.ErrSessionExpired) || errors.Is(err, zentao.ErrSessionExpired) {
		return sessionMessage
	}
	if errors.Is(err, zentao.ErrLoginFailed) || errors.Is(err, zentao.ErrMissingCredentials) {
		return loginMessage
	}
	if errors.Is(err, application.ErrInvalidSettings) {
		return settingsMessage
	}
	if errors.Is(err, zentao.ErrInvalidBaseURL) {
		return baseURLMessage
	}
	if errors.Is(err, zentao.ErrResponseTooLarge) {
		return responseSizeMessage
	}
	if isStateSaveFailure(err) {
		return stateSaveMessage
	}
	if isParseFailure(err) {
		return parseMessage
	}
	return networkMessage
}

func isStateSaveFailure(err error) bool {
	message := err.Error()
	return strings.Contains(message, "save snapshot:") || strings.Contains(message, "save settings:")
}

func isParseFailure(err error) bool {
	if errors.Is(err, zentao.ErrUnexpectedStructure) ||
		errors.Is(err, zentao.ErrIncompletePage) ||
		errors.Is(err, zentao.ErrInvalidDetailURL) ||
		errors.Is(err, zentao.ErrDuplicateItemID) ||
		errors.Is(err, zentao.ErrInvalidPagination) ||
		errors.Is(err, zentao.ErrConflictingDuplicate) ||
		errors.Is(err, zentao.ErrPageLimitExceeded) {
		return true
	}
	message := err.Error()
	return strings.Contains(message, "parse assigned ") ||
		strings.Contains(message, "invalid zentao login response") ||
		strings.Contains(message, "invalid zentao login challenge") ||
		strings.Contains(message, "unknown raw status code")
}
