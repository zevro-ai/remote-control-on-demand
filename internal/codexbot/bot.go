package codexbot

import (
	"context"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/zevro-ai/remote-control-on-demand/internal/botutil"
	"github.com/zevro-ai/remote-control-on-demand/internal/codex"
	"github.com/zevro-ai/remote-control-on-demand/internal/session"
	tele "gopkg.in/telebot.v4"
)

// Notifier abstracts the Telegram bot for HTTP-only mode.
type Notifier interface {
	Start()
	Stop()
	SendMessage(msg string)
}

type Bot struct {
	tb            *tele.Bot
	sessionMgr    *session.Manager
	codexMgr      *codex.Manager
	allowedUserID int64
	unsubSession  func()
}

// NopBot returns a no-op Notifier for HTTP-only mode.
func NopBot() Notifier {
	return &nopBot{}
}

type nopBot struct{}

func (n *nopBot) Start()               {}
func (n *nopBot) Stop()                {}
func (n *nopBot) SendMessage(_ string) {}

func New(token string, allowedUserID int64, sessionMgr *session.Manager, codexMgr *codex.Manager) (*Bot, error) {
	tb, err := tele.NewBot(tele.Settings{
		Token:  token,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil {
		return nil, fmt.Errorf("creating telegram bot: %w", err)
	}

	b := &Bot{
		tb:            tb,
		sessionMgr:    sessionMgr,
		codexMgr:      codexMgr,
		allowedUserID: allowedUserID,
	}
	b.registerHandlers()
	b.registerCommands()
	return b, nil
}

func (b *Bot) Start() {
	if b.sessionMgr != nil {
		b.unsubSession = b.sessionMgr.Subscribe(func(n session.Notification) {
			go b.SendMessage(n.Message)
		})
	}
	b.sendWelcome()
	b.tb.Start()
}

func (b *Bot) Stop() {
	if b.unsubSession != nil {
		b.unsubSession()
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
	b.tb.Handle("/new", b.auth(b.handleNew))
	b.tb.Handle("/folders", b.auth(b.handleFolders))
	b.tb.Handle("/sessions", b.auth(b.handleSessions))
	b.tb.Handle("/use", b.auth(b.handleUse))
	b.tb.Handle("/close", b.auth(b.handleClose))
	b.tb.Handle("/current", b.auth(b.handleCurrent))
	b.tb.Handle("/help", b.auth(b.handleHelp))
	b.tb.Handle(tele.OnText, b.auth(b.handleChat))
	b.tb.Handle(tele.OnCallback, b.auth(b.handleCallback))
}

func (b *Bot) registerCommands() {
	b.tb.SetCommands([]tele.Command{
		{Text: "start", Description: "Start a Claude session"},
		{Text: "list", Description: "List Claude sessions"},
		{Text: "kill", Description: "Stop a Claude session"},
		{Text: "status", Description: "Show Claude session details"},
		{Text: "logs", Description: "Show Claude session logs"},
		{Text: "restart", Description: "Restart a Claude session"},
		{Text: "folders", Description: "Browse repos for Claude"},
		{Text: "new", Description: "Create a Codex session"},
		{Text: "sessions", Description: "List Codex sessions"},
		{Text: "use", Description: "Switch active Codex session"},
		{Text: "close", Description: "Close a Codex session"},
		{Text: "current", Description: "Show active Codex session"},
		{Text: "help", Description: "Show all commands"},
	})
}

func (b *Bot) sendWelcome() {
	var sb strings.Builder
	sb.WriteString("<b>RCOD + Codex bot is online</b>\n\n")
	sb.WriteString("<b>Claude remote control</b>\n")
	sb.WriteString("Use <code>/start</code>, <code>/list</code>, <code>/status</code>, <code>/logs</code>, <code>/restart</code>, <code>/kill</code>, or <code>/folders</code>.\n\n")
	sb.WriteString("<b>Codex chat</b>\n")
	sb.WriteString("Use <code>/new repo</code> to create a Codex session, then send plain text to chat with Codex in that repo.\n")
	sb.WriteString("Use <code>/sessions</code>, <code>/use</code>, <code>/current</code>, and <code>/close</code> to manage Codex chats.\n")

	folders := b.listGitFolders()
	if len(folders) > 0 {
		sb.WriteString(fmt.Sprintf("\n\nFound <b>%d</b> git repo(s) under <code>%s</code>.", len(folders), html.EscapeString(b.baseFolder())))
	}
	if active, ok := b.codexMgr.Active(); ok {
		sb.WriteString(fmt.Sprintf("\nCurrent Codex session: <code>%s</code> in <code>%s</code>", html.EscapeString(active.ID), html.EscapeString(active.RelName)))
	}
	b.SendMessage(sb.String())
}

func (b *Bot) handleStart(c tele.Context) error {
	folder := strings.TrimSpace(c.Message().Payload)
	if folder == "" {
		return b.sendClaudeFolderPicker(c, "start", 0, "<b>Select a project to start in Claude</b>")
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

func (b *Bot) handleHelp(c tele.Context) error {
	msg := `<b>RCOD + Codex</b>

<b>Claude remote control</b>
<code>/start repo</code> start a Claude session
<code>/list</code> list Claude sessions
<code>/kill id</code> stop a Claude session
<code>/status id</code> show Claude session details
<code>/logs id</code> show Claude logs
<code>/restart id</code> restart a Claude session
<code>/folders</code> browse repos for Claude

<b>Codex chat</b>
<code>/new repo</code> create a Codex session
<code>/sessions</code> list Codex sessions
<code>/use id</code> switch active Codex session
<code>/close id</code> close a Codex session
<code>/current</code> show active Codex session

After creating a Codex session, send a normal text message and it will go to Codex.`
	return c.Send(msg, tele.ModeHTML)
}

func (b *Bot) handleNew(c tele.Context) error {
	folder := strings.TrimSpace(c.Message().Payload)
	if folder == "" {
		return b.sendCodexFolderPicker(c, 0, "<b>Select a repository for a new Codex session</b>")
	}

	resolved, matches := botutil.MatchFolderQuery(b.listGitFolders(), folder)
	switch {
	case resolved != "":
		return b.createSession(c, resolved)
	case len(matches) == 0:
		return c.Send(fmt.Sprintf("No project matched <code>%s</code>.", html.EscapeString(folder)), tele.ModeHTML)
	default:
		return c.Send(
			fmt.Sprintf(
				"Multiple projects matched <code>%s</code>:\n%s\n\nUse <code>/new exact-name</code> or run <code>/new</code> to browse.",
				html.EscapeString(folder),
				botutil.FormatCodeList(matches, 8),
			),
			tele.ModeHTML,
		)
	}
}

func (b *Bot) handleFolders(c tele.Context) error {
	return b.sendClaudeFolderPicker(c, "start", 0, "<b>Available projects</b>\nTap to start a Claude session.")
}

func (b *Bot) handleSessions(c tele.Context) error {
	sessions := b.codexMgr.List()
	if len(sessions) == 0 {
		return c.Send("No Codex sessions yet. Use /new.")
	}

	active, _ := b.codexMgr.Active()
	var sb strings.Builder
	sb.WriteString("<b>Codex sessions</b>\n")
	for _, sess := range sessions {
		marker := " "
		if active != nil && active.ID == sess.ID {
			marker = "*"
		}
		thread := "new"
		if sess.ThreadID != "" {
			thread = "live"
		}
		sb.WriteString(fmt.Sprintf(
			"%s <code>%s</code> | <code>%s</code> | %s\n",
			marker,
			html.EscapeString(sess.ID),
			html.EscapeString(sess.RelName),
			thread,
		))
	}

	return c.Send(sb.String(), b.codexSessionPickerMarkup(sessions), tele.ModeHTML)
}

func (b *Bot) handleUse(c tele.Context) error {
	id := strings.TrimSpace(c.Message().Payload)
	if id == "" {
		return b.sendCodexSessionPicker(c, "cuse", "<b>Select the active session</b>")
	}

	sess, err := b.codexMgr.SetActive(id)
	if err != nil {
		return c.Send(fmt.Sprintf("Error: <code>%s</code>", html.EscapeString(err.Error())), tele.ModeHTML)
	}

	return c.Send(
		fmt.Sprintf("Active session: <code>%s</code> in <code>%s</code>", html.EscapeString(sess.ID), html.EscapeString(sess.RelName)),
		b.codexSessionActions(sess),
		tele.ModeHTML,
	)
}

func (b *Bot) handleClose(c tele.Context) error {
	id := strings.TrimSpace(c.Message().Payload)
	if id == "" {
		return b.sendCodexSessionPicker(c, "cclose", "<b>Select a session to close</b>")
	}

	if err := b.codexMgr.Close(id); err != nil {
		return c.Send(fmt.Sprintf("Error: <code>%s</code>", html.EscapeString(err.Error())), tele.ModeHTML)
	}
	return c.Send(fmt.Sprintf("Closed session <code>%s</code>.", html.EscapeString(id)), tele.ModeHTML)
}

func (b *Bot) handleCurrent(c tele.Context) error {
	sess, ok := b.codexMgr.Active()
	if !ok {
		return c.Send("No active session. Use /new or /use.")
	}

	thread := "not started"
	if sess.ThreadID != "" {
		thread = sess.ThreadID
	}
	msg := fmt.Sprintf(
		"<b>Active session</b>\nID: <code>%s</code>\nProject: <code>%s</code>\nThread: <code>%s</code>",
		html.EscapeString(sess.ID),
		html.EscapeString(sess.RelName),
		html.EscapeString(thread),
	)
	return c.Send(msg, b.codexSessionActions(sess), tele.ModeHTML)
}

func (b *Bot) handleChat(c tele.Context) error {
	msg := c.Message()
	if msg == nil {
		return nil
	}
	if strings.HasPrefix(strings.TrimSpace(msg.Text), "/") {
		return nil
	}

	sess, err := b.codexMgr.ResolveActive()
	if err != nil {
		return c.Send(err.Error())
	}

	_ = c.Notify(tele.Typing)
	replySession, response, err := b.codexMgr.Send(context.Background(), sess.ID, msg.Text, nil)
	if err != nil {
		return c.Send(err.Error())
	}

	header := fmt.Sprintf("[%s | %s]\n", replySession.ID, replySession.RelName)
	return b.sendTextChunks(c, header+response)
}

func (b *Bot) handleCallback(c tele.Context) error {
	data := c.Callback().Data
	_ = c.Respond()

	parts := strings.Split(data, ":")
	if len(parts) < 2 {
		return nil
	}

	switch parts[0] {
	case "pick":
		if len(parts) != 3 {
			return nil
		}
		return b.handleClaudeFolderPick(c, parts[1], parts[2])
	case "nav":
		if len(parts) != 3 {
			return nil
		}
		return b.handleClaudeNavigation(c, parts[1], parts[2])
	case "cpick":
		if len(parts) != 2 {
			return nil
		}
		return b.handleCodexFolderPick(c, parts[1])
	case "cnav":
		if len(parts) != 2 {
			return nil
		}
		return b.handleCodexNavigation(c, parts[1])
	case "cuse":
		if len(parts) != 2 {
			return nil
		}
		sess, err := b.codexMgr.SetActive(parts[1])
		if err != nil {
			return c.Send(fmt.Sprintf("Error: <code>%s</code>", html.EscapeString(err.Error())), tele.ModeHTML)
		}
		return c.Send(fmt.Sprintf("Active session: <code>%s</code> in <code>%s</code>", html.EscapeString(sess.ID), html.EscapeString(sess.RelName)), b.codexSessionActions(sess), tele.ModeHTML)
	case "cclose":
		if len(parts) != 2 {
			return nil
		}
		if err := b.codexMgr.Close(parts[1]); err != nil {
			return c.Send(fmt.Sprintf("Error: <code>%s</code>", html.EscapeString(err.Error())), tele.ModeHTML)
		}
		return c.Send(fmt.Sprintf("Closed session <code>%s</code>.", html.EscapeString(parts[1])), tele.ModeHTML)
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

func (b *Bot) createSession(c tele.Context, folder string) error {
	sess, err := b.codexMgr.Create(folder)
	if err != nil {
		return c.Send(fmt.Sprintf("Error: <code>%s</code>", html.EscapeString(err.Error())), tele.ModeHTML)
	}

	msg := fmt.Sprintf(
		"<b>Codex session created</b>\nID: <code>%s</code>\nProject: <code>%s</code>\n\nSend a normal text message now and I will forward it to Codex.",
		html.EscapeString(sess.ID),
		html.EscapeString(sess.RelName),
	)
	return c.Send(msg, b.codexSessionActions(sess), tele.ModeHTML)
}

// Claude folder picker — uses pick:{action}:{index} / nav:{action}:{page} callbacks.
func (b *Bot) sendClaudeFolderPicker(c tele.Context, action string, page int, text string) error {
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

// Codex folder picker — uses cpick:{index} / cnav:{page} callbacks.
func (b *Bot) sendCodexFolderPicker(c tele.Context, page int, text string) error {
	folders := b.listGitFolders()
	if len(folders) == 0 {
		return c.Send("No git repositories found in the projects folder.")
	}

	markup := botutil.FolderPickerMarkup(botutil.FolderPickerConfig{
		Folders:  folders,
		Page:     page,
		PickData: func(index int) string { return fmt.Sprintf("cpick:%d", index) },
		NavData:  func(p int) string { return fmt.Sprintf("cnav:%d", p) },
		Label:    func(folder string) string { return "Repo " + folder },
	})
	return c.Send(text, markup, tele.ModeHTML)
}

func (b *Bot) sendCodexSessionPicker(c tele.Context, action, text string) error {
	sessions := b.codexMgr.List()
	if len(sessions) == 0 {
		return c.Send("No Codex sessions yet. Use /new.")
	}

	var items []botutil.PickerItem
	for _, sess := range sessions {
		items = append(items, botutil.PickerItem{
			Label: fmt.Sprintf("%s - %s", sess.ID, sess.RelName),
			Data:  action + ":" + sess.ID,
		})
	}
	return c.Send(text, botutil.PickerMarkup(items), tele.ModeHTML)
}

func (b *Bot) sendClaudeSessionPicker(c tele.Context, action, text string) error {
	sessions := b.sessionMgr.List()
	var items []botutil.PickerItem
	for _, s := range sessions {
		if s.Status == session.StatusRunning {
			items = append(items, botutil.PickerItem{
				Label: fmt.Sprintf("%s - %s", s.ID, s.RelName),
				Data:  action + ":" + s.ID,
			})
		}
	}
	if len(items) == 0 {
		return c.Send("No active Claude sessions.")
	}

	return c.Send(text, botutil.PickerMarkup(items), tele.ModeHTML)
}

func (b *Bot) handleClaudeFolderPick(c tele.Context, action string, indexStr string) error {
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

func (b *Bot) handleClaudeNavigation(c tele.Context, action string, pageStr string) error {
	page := 0
	fmt.Sscanf(pageStr, "%d", &page)
	return b.sendClaudeFolderPicker(c, action, page, "<b>Available projects</b>\nTap to start a Claude session.")
}

func (b *Bot) handleCodexFolderPick(c tele.Context, indexStr string) error {
	folders := b.listGitFolders()
	index := 0
	fmt.Sscanf(indexStr, "%d", &index)
	if index < 0 || index >= len(folders) {
		return c.Send("That repository is no longer available. Refresh with /new.")
	}
	return b.createSession(c, folders[index])
}

func (b *Bot) handleCodexNavigation(c tele.Context, pageStr string) error {
	page := 0
	fmt.Sscanf(pageStr, "%d", &page)
	return b.sendCodexFolderPicker(c, page, "<b>Available repositories</b>\nTap to create a Codex session.")
}

func (b *Bot) codexSessionActions(sess *codex.Session) *tele.ReplyMarkup {
	markup := &tele.ReplyMarkup{}
	markup.InlineKeyboard = [][]tele.InlineButton{
		{
			{Text: "Use", Data: "cuse:" + sess.ID},
			{Text: "Close", Data: "cclose:" + sess.ID},
		},
	}
	return markup
}

func (b *Bot) codexSessionPickerMarkup(sessions []*codex.Session) *tele.ReplyMarkup {
	markup := &tele.ReplyMarkup{}
	var rows [][]tele.InlineButton
	for _, sess := range sessions {
		rows = append(rows, []tele.InlineButton{
			{Text: "Use " + sess.ID, Data: "cuse:" + sess.ID},
			{Text: "Close", Data: "cclose:" + sess.ID},
		})
	}
	markup.InlineKeyboard = rows
	return markup
}

func (b *Bot) sendTextChunks(c tele.Context, text string) error {
	const maxLen = 3500
	if strings.TrimSpace(text) == "" {
		return c.Send("(empty response)")
	}

	chunks := splitChunks(text, maxLen)
	for _, chunk := range chunks {
		if err := c.Send(chunk); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bot) listGitFolders() []string {
	return botutil.ListGitFolders(b.baseFolder())
}

func (b *Bot) baseFolder() string {
	if b.codexMgr != nil {
		return b.codexMgr.BaseFolder()
	}
	if b.sessionMgr != nil {
		return b.sessionMgr.BaseFolder()
	}
	return ""
}
