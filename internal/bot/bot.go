package bot

import (
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/zevro-ai/remote-control-on-demand/internal/botutil"
	"github.com/zevro-ai/remote-control-on-demand/internal/session"
	tele "gopkg.in/telebot.v4"
)

type Bot struct {
	tb            *tele.Bot
	mgr           *session.Manager
	allowedUserID int64
	unsubNotif    func()
}

func New(token string, allowedUserID int64, mgr *session.Manager) (*Bot, error) {
	pref := tele.Settings{
		Token:  token,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	}

	tb, err := tele.NewBot(pref)
	if err != nil {
		return nil, fmt.Errorf("creating telegram bot: %w", err)
	}

	b := &Bot{
		tb:            tb,
		mgr:           mgr,
		allowedUserID: allowedUserID,
	}

	b.registerHandlers()
	b.registerCommands()
	return b, nil
}

func (b *Bot) Start() {
	b.unsubNotif = b.mgr.Subscribe(func(n session.Notification) {
		go b.SendMessage(n.Message)
	})
	b.sendWelcome()
	b.tb.Start()
}

func (b *Bot) Stop() {
	if b.unsubNotif != nil {
		b.unsubNotif()
	}
	b.tb.Stop()
}

func (b *Bot) SendMessage(msg string) {
	recipient := &botutil.User{ID: b.allowedUserID}
	b.tb.Send(recipient, msg, tele.ModeHTML)
}

func (b *Bot) auth(next tele.HandlerFunc) tele.HandlerFunc {
	return botutil.Auth(b.allowedUserID, next)
}

func (b *Bot) registerHandlers() {
	b.tb.Handle("/start", b.auth(b.handleStart))
	b.tb.Handle("/list", b.auth(b.handleList))
	b.tb.Handle("/kill", b.auth(b.handleKill))
	b.tb.Handle("/status", b.auth(b.handleStatus))
	b.tb.Handle("/logs", b.auth(b.handleLogs))
	b.tb.Handle("/restart", b.auth(b.handleRestart))
	b.tb.Handle("/folders", b.auth(b.handleFolders))
	b.tb.Handle("/help", b.auth(b.handleHelp))
	b.tb.Handle(tele.OnCallback, b.auth(b.handleCallback))
}

func (b *Bot) registerCommands() {
	b.tb.SetCommands([]tele.Command{
		{Text: "start", Description: "Start a session"},
		{Text: "list", Description: "List active sessions"},
		{Text: "kill", Description: "Stop a session"},
		{Text: "status", Description: "Session details"},
		{Text: "logs", Description: "Recent session logs"},
		{Text: "restart", Description: "Restart a session"},
		{Text: "folders", Description: "List available git repos"},
		{Text: "help", Description: "Show available commands"},
	})
}

func (b *Bot) sendWelcome() {
	var sb strings.Builder
	sb.WriteString("<b>RCOD is online</b>\n\n")

	folders := b.listGitFolders()
	if len(folders) == 0 {
		sb.WriteString("No git repositories found yet. Use <code>/help</code> to see commands.")
		b.SendMessage(sb.String())
		return
	}

	sb.WriteString(fmt.Sprintf(
		"Found <b>%d</b> project(s) under <code>%s</code>.\nUse <code>/start</code> to launch a session or <code>/folders</code> to browse.",
		len(folders),
		html.EscapeString(b.mgr.BaseFolder()),
	))

	previewCount := min(len(folders), 8)
	if previewCount > 0 {
		sb.WriteString("\n\n<b>First projects</b>\n")
		for _, folder := range folders[:previewCount] {
			sb.WriteString(fmt.Sprintf("• <code>%s</code>\n", html.EscapeString(folder)))
		}
	}

	b.SendMessage(sb.String())
}

func (b *Bot) handleStart(c tele.Context) error {
	folder := strings.TrimSpace(c.Message().Payload)
	if folder == "" {
		return b.sendFolderPicker(c, "start", 0, "<b>Select a project to start</b>")
	}

	resolved, matches := botutil.MatchFolderQuery(b.listGitFolders(), folder)
	switch {
	case resolved != "":
		return b.startSession(c, resolved)
	case len(matches) == 0:
		return c.Send(fmt.Sprintf("No project matched <code>%s</code>.", html.EscapeString(folder)), tele.ModeHTML)
	default:
		return c.Send(
			fmt.Sprintf(
				"Multiple projects matched <code>%s</code>:\n%s\n\nUse <code>/start exact-name</code> or browse with <code>/folders</code>.",
				html.EscapeString(folder),
				botutil.FormatCodeList(matches, 8),
			),
			tele.ModeHTML,
		)
	}
}

func (b *Bot) handleList(c tele.Context) error {
	sessions := b.mgr.List()
	if len(sessions) == 0 {
		return c.Send("No active sessions.")
	}

	var sb strings.Builder
	sb.WriteString("<b>Sessions</b>\n")
	for _, s := range sessions {
		uptime := time.Since(s.StartedAt).Truncate(time.Second)
		sb.WriteString(fmt.Sprintf(
			"<code>%s</code> | <code>%s</code> | %s | %s\n",
			html.EscapeString(s.ID),
			html.EscapeString(s.RelName),
			html.EscapeString(string(s.Status)),
			html.EscapeString(uptime.String()),
		))
	}
	return c.Send(sb.String(), tele.ModeHTML)
}

func (b *Bot) handleKill(c tele.Context) error {
	id := strings.TrimSpace(c.Message().Payload)
	if id == "" {
		return b.sendSessionPicker(c, "kill", "<b>Select a session to stop</b>")
	}
	return b.killSession(c, id)
}

func (b *Bot) handleStatus(c tele.Context) error {
	id := strings.TrimSpace(c.Message().Payload)
	if id == "" {
		return b.sendSessionPicker(c, "status", "<b>Select a session</b>")
	}
	return b.showStatus(c, id)
}

func (b *Bot) handleLogs(c tele.Context) error {
	id := strings.TrimSpace(c.Message().Payload)
	if id == "" {
		return b.sendSessionPicker(c, "logs", "<b>Select a session</b>")
	}
	return b.showLogs(c, id)
}

func (b *Bot) handleRestart(c tele.Context) error {
	id := strings.TrimSpace(c.Message().Payload)
	if id == "" {
		return b.sendSessionPicker(c, "restart", "<b>Select a session to restart</b>")
	}
	return b.restartSession(c, id)
}

func (b *Bot) handleFolders(c tele.Context) error {
	return b.sendFolderPicker(c, "start", 0, "<b>Available projects</b>\nTap to start a session.")
}

func (b *Bot) handleHelp(c tele.Context) error {
	msg := `<b>Remote Control On Demand</b>

<code>/start</code> Start a session
<code>/list</code> List active sessions
<code>/kill</code> Stop a session
<code>/status</code> Session details
<code>/logs</code> Recent session logs
<code>/restart</code> Restart a session
<code>/folders</code> Browse available projects
<code>/help</code> Show this message`
	return c.Send(msg, tele.ModeHTML)
}

func (b *Bot) handleCallback(c tele.Context) error {
	data := c.Callback().Data
	c.Respond()

	parts := strings.Split(data, ":")
	if len(parts) < 2 {
		return nil
	}

	switch parts[0] {
	case "pick":
		if len(parts) != 3 {
			return nil
		}
		return b.handleFolderPick(c, parts[1], parts[2])
	case "nav":
		if len(parts) != 3 {
			return nil
		}
		return b.handleNavigation(c, parts[1], parts[2])
	case "start", "kill", "status", "logs", "restart":
		if len(parts) != 2 {
			return nil
		}
		switch parts[0] {
		case "start":
			return b.startSession(c, parts[1])
		case "kill":
			return b.killSession(c, parts[1])
		case "status":
			return b.showStatus(c, parts[1])
		case "logs":
			return b.showLogs(c, parts[1])
		case "restart":
			return b.restartSession(c, parts[1])
		}
	}

	return nil
}

func (b *Bot) startSession(c tele.Context, folder string) error {
	sess, err := b.mgr.Start(folder)
	if err != nil {
		return c.Send(fmt.Sprintf("Error: <code>%s</code>", html.EscapeString(err.Error())), tele.ModeHTML)
	}

	msg := fmt.Sprintf(
		"<b>Session started</b>\nID: <code>%s</code>\nProject: <code>%s</code>",
		html.EscapeString(sess.ID),
		html.EscapeString(folder),
	)
	return c.Send(msg, b.sessionActions(sess), tele.ModeHTML)
}

func (b *Bot) killSession(c tele.Context, id string) error {
	if err := b.mgr.Kill(id); err != nil {
		return c.Send(fmt.Sprintf("Error: <code>%s</code>", html.EscapeString(err.Error())), tele.ModeHTML)
	}
	return c.Send(fmt.Sprintf("Session <code>%s</code> stopped.", html.EscapeString(id)), tele.ModeHTML)
}

func (b *Bot) showStatus(c tele.Context, id string) error {
	sess, ok := b.mgr.Get(id)
	if !ok {
		return c.Send(fmt.Sprintf("Session <code>%s</code> not found.", html.EscapeString(id)), tele.ModeHTML)
	}

	uptime := time.Since(sess.StartedAt).Truncate(time.Second)
	pid := 0
	if sess.Cmd != nil && sess.Cmd.Process != nil {
		pid = sess.Cmd.Process.Pid
	}

	msg := fmt.Sprintf(
		"<b>Session %s</b>\nProject: <code>%s</code>\nFolder: <code>%s</code>\nStatus: %s\nPID: %d\nUptime: %s\nRestarts: %d",
		html.EscapeString(sess.ID),
		html.EscapeString(sess.RelName),
		html.EscapeString(sess.Folder),
		html.EscapeString(string(sess.Status)),
		pid,
		html.EscapeString(uptime.String()),
		sess.Restarts,
	)
	if sess.ClaudeURL != "" {
		msg += fmt.Sprintf("\nClaude: <a href=\"%s\">open session</a>", html.EscapeString(sess.ClaudeURL))
	}

	return c.Send(msg, b.sessionActions(sess), tele.ModeHTML)
}

func (b *Bot) showLogs(c tele.Context, id string) error {
	sess, ok := b.mgr.Get(id)
	if !ok {
		return c.Send(fmt.Sprintf("Session <code>%s</code> not found.", html.EscapeString(id)), tele.ModeHTML)
	}

	lines := sess.OutputBuf.Lines(50)
	if len(lines) == 0 {
		return c.Send("No logs available.")
	}

	output := strings.Join(lines, "\n")
	if len(output) > 4000 {
		output = output[len(output)-4000:]
	}
	return c.Send(fmt.Sprintf("<pre>%s</pre>", html.EscapeString(output)), tele.ModeHTML)
}

func (b *Bot) restartSession(c tele.Context, id string) error {
	if err := b.mgr.Restart(id); err != nil {
		return c.Send(fmt.Sprintf("Error: <code>%s</code>", html.EscapeString(err.Error())), tele.ModeHTML)
	}

	sess, ok := b.mgr.Get(id)
	if !ok {
		return c.Send(fmt.Sprintf("Session <code>%s</code> restarted.", html.EscapeString(id)), tele.ModeHTML)
	}
	return c.Send(fmt.Sprintf("Session <code>%s</code> restarted.", html.EscapeString(id)), b.sessionActions(sess), tele.ModeHTML)
}

func (b *Bot) sendFolderPicker(c tele.Context, action string, page int, text string) error {
	folders := b.listGitFolders()
	if len(folders) == 0 {
		return c.Send("No git repositories found in the projects folder.", tele.ModeHTML)
	}

	markup := botutil.FolderPickerMarkup(botutil.FolderPickerConfig{
		Folders:  folders,
		Page:     page,
		PickData: func(index int) string { return fmt.Sprintf("pick:%s:%d", action, index) },
		NavData:  func(p int) string { return fmt.Sprintf("nav:%s:%d", action, p) },
		Label:    func(folder string) string { return "📂 " + folder },
	})
	return c.Send(text, markup, tele.ModeHTML)
}

func (b *Bot) sendSessionPicker(c tele.Context, action, text string) error {
	sessions := b.mgr.List()
	var items []botutil.PickerItem
	for _, s := range sessions {
		if s.Status == session.StatusRunning {
			items = append(items, botutil.PickerItem{
				Label: fmt.Sprintf("%s — %s", s.ID, s.RelName),
				Data:  action + ":" + s.ID,
			})
		}
	}
	if len(items) == 0 {
		return c.Send("No active sessions.")
	}

	return c.Send(text, botutil.PickerMarkup(items), tele.ModeHTML)
}

func (b *Bot) handleFolderPick(c tele.Context, action string, indexStr string) error {
	folders := b.listGitFolders()
	index := 0
	fmt.Sscanf(indexStr, "%d", &index)
	if index < 0 || index >= len(folders) {
		return c.Send("That project is no longer available. Refresh with <code>/folders</code>.", tele.ModeHTML)
	}

	switch action {
	case "start":
		return b.startSession(c, folders[index])
	default:
		return nil
	}
}

func (b *Bot) handleNavigation(c tele.Context, action string, pageStr string) error {
	page := 0
	fmt.Sscanf(pageStr, "%d", &page)
	return b.sendFolderPicker(c, action, page, "<b>Available projects</b>\nTap to start a session.")
}

func (b *Bot) sessionActions(sess *session.Session) *tele.ReplyMarkup {
	markup := &tele.ReplyMarkup{}
	rows := [][]tele.InlineButton{
		{
			{Text: "Status", Data: "status:" + sess.ID},
			{Text: "Logs", Data: "logs:" + sess.ID},
		},
		{
			{Text: "Restart", Data: "restart:" + sess.ID},
			{Text: "Stop", Data: "kill:" + sess.ID},
		},
	}
	if sess.ClaudeURL != "" {
		rows = append(rows, []tele.InlineButton{{Text: "Open Claude", URL: sess.ClaudeURL}})
	}
	markup.InlineKeyboard = rows
	return markup
}

func (b *Bot) listGitFolders() []string {
	return botutil.ListGitFolders(b.mgr.BaseFolder())
}
