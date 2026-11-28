package main

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"chandao/zentaotray/internal/application"
	"chandao/zentaotray/internal/domain"
	"chandao/zentaotray/internal/infrastructure/security"
	"chandao/zentaotray/internal/infrastructure/zentao"
	"chandao/zentaotray/internal/ui"
	"github.com/nsgo-dev/nsgo/core"
)

const defaultZentaoBaseURL = "http://192.168.2.225/biz/"

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
	cx          *core.Scope
	data        *core.Signal[ui.WorkbenchData]
	selectedKey *core.Signal[string]
	showWindow  func()
	// viewSession selects which account's history and report window the
	// workbench mounts. It is written only from the UI goroutine.
	viewSession *core.Signal[*accountSession]
	popups      *ui.MessagePopupManager
	report      *ui.ReportWindow
	credentials application.CredentialStore
	dataDir     string
	baseURL     string

	ctx    context.Context
	cancel context.CancelFunc

	// opMu serializes login, manual refresh, and settings writes. controllerMu
	// separately protects the pointer used by quit and callback generations.
	opMu         sync.Mutex
	controllerMu sync.Mutex
	session      *accountSession
	generation   uint64
}

func newWorkbenchRuntime(cx *core.Scope, data *core.Signal[ui.WorkbenchData], selectedKey *core.Signal[string], showWindow func()) *workbenchRuntime {
	ctx, cancel := context.WithCancel(cx.Context())
	return &workbenchRuntime{
		cx:          cx,
		data:        data,
		selectedKey: selectedKey,
		showWindow:  showWindow,
		viewSession: core.NewSignal[*accountSession](cx, nil),
		credentials: security.SecretsCredentialStore{Secrets: cx.Secrets()},
		dataDir:     cx.DataDir(),
		baseURL:     zentaoBaseURL,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// openReport 打开独立报表窗体；重复打开时只显示并聚焦已有窗体。
func (r *workbenchRuntime) openReport() {
	if r.report != nil && !r.data.Get().NeedsLogin {
		r.report.Open()
	}
}

// runItemAction 在后台对一条事项执行生命周期操作（开始/完成/解决/关闭
// 等），成功后立即刷新；done 在 UI 线程收到人类可读的错误（成功为空串）。
func (r *workbenchRuntime) runItemAction(item domain.Item, action string, done func(errorMessage string)) {
	if r.data.Get().NeedsLogin {
		done("请先登录禅道账号。")
		return
	}
	controller, generation, _, ok := r.currentController()
	if !ok {
		done("请先登录禅道账号。")
		return
	}
	go func() {
		r.opMu.Lock()
		if !r.isCurrent(generation) {
			r.opMu.Unlock()
			return
		}
		ctx, cancel := context.WithTimeout(r.ctx, 30*time.Second)
		err := controller.RunItemAction(ctx, item, action)
		cancel()
		r.opMu.Unlock()
		r.cx.Post(func() {
			if !r.isCurrent(generation) {
				return
			}
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
	r.stopController()
}

// clearSessionView drops windows, timers, and the selected row that belonged
// to the previous account. Call on the UI goroutine only.
func (r *workbenchRuntime) clearSessionView() {
	if r.report != nil {
		r.report.Close()
		r.report = nil
	}
	if r.popups != nil {
		r.popups.Dispose()
		r.popups = nil
	}
	r.selectedKey.Set("")
	r.viewSession.Set(nil)
}

func (r *workbenchRuntime) login(account, password string) {
	account = strings.TrimSpace(account)
	if account == "" || strings.TrimSpace(password) == "" {
		r.postError("请输入禅道账号和密码。", true)
		return
	}
	if r.data.Get().Loading {
		return
	}
	// 切换账号：旧账号的列表、历史视图和待确认操作立即失效，避免新账号
	// 在旧数据上继续操作。
	r.clearSessionView()
	r.data.Set(ui.WorkbenchData{NeedsLogin: true, Loading: true, PollSeconds: 60, PopupSeconds: 8, NotificationsEnabled: true})
	go func() {
		r.opMu.Lock()
		defer r.opMu.Unlock()

		r.stopController()
		session, err := r.newSession(account, password)
		if err != nil {
			r.postError(userMessage(err), true)
			return
		}
		// Verify both login and assigned-item access before keeping credentials.
		result, err := session.controller.Refresh(r.ctx)
		if err != nil {
			session.close()
			// Any failed verification leaves the user on the login panel.
			r.postError(userMessage(err), true)
			return
		}
		if err := saveLogin(r.credentials, r.baseURL, loginSecret{Account: account, Password: password}); err != nil {
			session.close()
			r.postError(secureSaveMessage, true)
			return
		}
		r.activate(session, result, false)
	}()
}

func (r *workbenchRuntime) loadSavedCredentials() {
	r.opMu.Lock()
	defer r.opMu.Unlock()

	secret, found, err := readLogin(r.credentials, r.baseURL)
	if err != nil {
		r.postError(secureReadMessage, true)
		return
	}
	if !found {
		return
	}
	account := strings.TrimSpace(secret.Account)
	if account == "" || strings.TrimSpace(secret.Password) == "" {
		r.postError("已保存的登录凭据不完整，请重新登录。", true)
		return
	}

	session, err := r.newSession(account, secret.Password)
	if err != nil {
		r.postError(userMessage(err), true)
		return
	}
	// 启动即渲染该账号持久化快照，首轮全量刷新完成前界面不空等。
	r.activate(session, application.RefreshResult{Snapshot: session.controller.CurrentSnapshot()}, true)
}

func (r *workbenchRuntime) newSession(account, password string) (*accountSession, error) {
	client, err := zentao.New(r.baseURL, account, password)
	if err != nil {
		return nil, err
	}
	var session *accountSession
	var notify messagePopupNotifier = func(changes []domain.Change) {
		r.cx.Post(func() {
			// 迟到的变化只属于产生它的会话；账号切换或退出登录后丢弃。
			if r.viewSession.Get() != session || r.popups == nil || r.data.Get().NeedsLogin {
				return
			}
			r.popups.Show(changes, r.data.Get().PopupSeconds)
		})
	}
	session, err = openAccountSession(r.ctx, r.dataDir, r.baseURL, account, zentao.NewService(client), notify)
	return session, err
}

// activate is called with opMu held. It publishes the account's history and
// report window generation, then lets the controller poll. Results from a
// superseded generation are dropped by isCurrent.
func (r *workbenchRuntime) activate(s *accountSession, result application.RefreshResult, restoring bool) {
	r.controllerMu.Lock()
	r.generation++
	generation := r.generation
	r.session = s
	r.controllerMu.Unlock()
	r.cx.Post(func() {
		if !r.isCurrent(generation) {
			return
		}
		r.cx.Batch(func() {
			r.clearSessionView()
			r.popups = ui.NewMessagePopupManager(r.cx, func(key string) {
				r.selectedKey.Set(key)
				r.showWindow()
			}, r.showWindow)
			if s.history != nil {
				r.report = ui.NewReportWindow(r.cx, s.history)
			}
			next := applyRefresh(ui.WorkbenchData{PollSeconds: 60, PopupSeconds: 8, NotificationsEnabled: true}, s.account, result)
			if restoring {
				// 持久化快照先展示，连接状态等首轮刷新回来再点亮。
				next.Connected = false
				next.Loading = true
			}
			r.data.Set(next)
			r.viewSession.Set(s)
		})
		// Ask only after an explicit, successful login. A denial is
		// asynchronous and does not affect the established session.
		if !restoring && result.Snapshot.NotificationsEnabled {
			r.cx.RequestNotificationPermission(nil)
		}
	})
	r.startControllerWithMode(s.controller, s.account, restoring)
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
			r.applyResult(account, result)
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

func (r *workbenchRuntime) stopController() {
	r.controllerMu.Lock()
	session := r.session
	r.session = nil
	r.generation++
	r.controllerMu.Unlock()
	if session != nil {
		session.close()
	}
}

func (r *workbenchRuntime) currentController() (*application.Controller, uint64, string, bool) {
	r.controllerMu.Lock()
	defer r.controllerMu.Unlock()
	if r.session == nil {
		return nil, 0, "", false
	}
	return r.session.controller, r.generation, r.session.account, true
}

func (r *workbenchRuntime) isCurrent(generation uint64) bool {
	r.controllerMu.Lock()
	defer r.controllerMu.Unlock()
	return r.session != nil && r.generation == generation
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
			r.applyResult(account, result)
		})
	}
	onError := func(err error) { r.postControllerError(generation, account, err) }
	if refreshImmediately {
		controller.Start(r.ctx, onResult, onError)
		return
	}
	controller.StartPolling(r.ctx, onResult, onError)
}

// messagePopupNotifier adapts change batches to the popup manager. History is
// recorded by the account session separately, so muting popups never loses it.
type messagePopupNotifier func([]domain.Change)

func (n messagePopupNotifier) Notify(_ context.Context, changes []domain.Change) {
	if n != nil {
		n(changes)
	}
}

// applyResult publishes a refresh result unless a newer one already landed.
// Manual refresh and polling can finish out of order; the older result must
// not overwrite the newer list, changes, or last-success time.
func (r *workbenchRuntime) applyResult(account string, result application.RefreshResult) {
	r.data.Update(func(data ui.WorkbenchData) ui.WorkbenchData {
		if !data.LastSuccess.IsZero() && result.Snapshot.LastSuccess.Before(data.LastSuccess) {
			return data
		}
		return applyRefresh(data, account, result)
	})
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
	secureReadMessage     = "无法读取安全存储中的登录凭据，请重新登录。"
	secureSaveMessage     = "无法安全保存登录凭据，请检查系统凭据管理器后重试。"
	stateLoadMessage      = "本地状态文件读取失败，请检查数据目录后重试。"
	stateSaveMessage      = "本地状态保存失败，请检查数据目录的写入权限。"
	loginMessage          = "账号或密码不正确，请检查后重试。"
	sessionMessage        = "禅道登录已过期，请重新登录。"
	parseMessage          = "禅道页面数据解析失败，请稍后重试或联系管理员。"
	networkMessage        = "网络连接异常或禅道暂时不可用，请稍后重试。"
	settingsMessage       = "设置无效，轮询间隔必须为 30 到 300 秒。"
	cancelledMessage      = "操作已取消，请重试。"
	baseURLMessage        = "禅道服务地址配置无效，请联系管理员。"
	responseSizeMessage   = "禅道返回的数据过大，请稍后重试。"
	actionUnknownMessage  = "未能确认禅道是否已执行操作，请先刷新核实，不要直接重复提交。"
	actionRejectedMessage = "禅道未接受此操作，请检查事项状态和表单要求。"
	itemChangedMessage    = "事项状态已变化，请刷新后重新选择操作。"
)

// userMessage maps internal failures to fixed Chinese text. Raw errors can
// contain local paths, request URLs, or transport details and are never shown.
func userMessage(err error) string {
	if err == nil {
		return ""
	}
	// 写操作结果未知优先于超时/网络提示：不能让用户以为可以安全重试。
	if errors.Is(err, zentao.ErrActionOutcomeUnknown) {
		return actionUnknownMessage
	}
	if errors.Is(err, zentao.ErrActionRejected) {
		return actionRejectedMessage
	}
	if errors.Is(err, application.ErrItemChanged) {
		return itemChangedMessage
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
		errors.Is(err, zentao.ErrPageLimitExceeded) ||
		errors.Is(err, zentao.ErrActionFormNotFound) ||
		errors.Is(err, zentao.ErrActionFormTarget) {
		return true
	}
	message := err.Error()
	return strings.Contains(message, "parse assigned ") ||
		strings.Contains(message, "invalid zentao login response") ||
		strings.Contains(message, "invalid zentao login challenge") ||
		strings.Contains(message, "unknown raw status code")
}
