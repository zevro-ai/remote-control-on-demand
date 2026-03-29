package rcodbot

import (
	"context"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"

	"github.com/zevro-ai/remote-control-on-demand/internal/botutil"
	"github.com/zevro-ai/remote-control-on-demand/internal/chat"
	"github.com/zevro-ai/remote-control-on-demand/internal/codex"
	"github.com/zevro-ai/remote-control-on-demand/internal/gemini"
	"github.com/zevro-ai/remote-control-on-demand/internal/provider"
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
	tb                *tele.Bot
	sessionMgr        *session.Manager
	codexMgr          *codex.Manager
	geminiMgr         *gemini.Manager
	allowedUserID     int64
	unsubSession      func()
	currentProviderID string
}

// NopBot returns a no-op Notifier for HTTP-only mode.
func NopBot() Notifier {
	return &nopBot{}
}

type nopBot struct{}

func (n *nopBot) Start()               {}
func (n *nopBot) Stop()                {}
func (n *nopBot) SendMessage(_ string) {}

func New(token string, allowedUserID int64, sessionMgr *session.Manager, codexMgr *codex.Manager, geminiMgr *gemini.Manager) (*Bot, error) {
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
		geminiMgr:     geminiMgr,
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
	sb.WriteString("<b>RCOD bot is online</b>\n\n")
	sb.WriteString("<b>Claude remote control</b>\n")
	sb.WriteString("Use <code>/start</code>, <code>/list</code>, <code>/status</code>, <code>/logs</code>, <code>/restart</code>, <code>/kill</code>, or <code>/folders</code>.\n\n")
	sb.WriteString("<b>Codex & Gemini chat</b>\n")
	sb.WriteString("Use <code>/new repo</code> to create a session, then send plain text to chat with the agent in that repo.\n")
	sb.WriteString("Use <code>/sessions</code>, <code>/use</code>, <code>/current</code>, and <code>/close</code> to manage chats.\n")

	folders := b.listGitFolders()
	if len(folders) > 0 {
		sb.WriteString(fmt.Sprintf("\n\nFound <b>%d</b> git repo(s) under <code>%s</code>.", len(folders), html.EscapeString(b.baseFolder())))
	}
	if active, ok := b.codexMgr.Active(); ok {
		sb.WriteString(fmt.Sprintf("\nCurrent Codex session: <code>%s</code> in <code>%s</code>", html.EscapeString(active.ID), html.EscapeString(active.RelName)))
	}
	if active, ok := b.geminiMgr.Active(); ok {
		sb.WriteString(fmt.Sprintf("\nCurrent Gemini session: <code>%s</code> in <code>%s</code>", html.EscapeString(active.ID), html.EscapeString(active.RelName)))
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
	msg := `<b>RCOD + Codex & Gemini</b>

<b>Claude remote control</b>
<code>/start repo</code> start a Claude session
<code>/list</code> list Claude sessions
<code>/kill id</code> stop a Claude session
<code>/status id</code> show Claude session details
<code>/logs id</code> show Claude logs
<code>/restart id</code> restart a Claude session
<code>/folders</code> browse repos for Claude

<b>Codex & Gemini chat</b>
<code>/new repo</code> create a chat session
<code>/sessions</code> list chat sessions
<code>/use id</code> switch active chat session
<code>/close id</code> close a chat session
<code>/current</code> show active chat session

After creating a chat session, send a normal text message and it will go to the active agent.`
	return c.Send(msg, tele.ModeHTML)
}

func (b *Bot) handleNew(c tele.Context) error {
	folder := strings.TrimSpace(c.Message().Payload)
	if folder == "" {
		return b.sendProviderPicker(c, 0, "<b>Select a provider for a new chat session</b>")
	}

	resolved, matches := botutil.MatchFolderQuery(b.listGitFolders(), folder)
	switch {
	case resolved != "":
		return b.sendProviderPickerForFolder(c, resolved, "<b>Select a provider for <code>"+html.EscapeString(resolved)+"</code></b>")
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
	var sessions []*chat.Session
	sessions = append(sessions, b.codexMgr.ListSessions()...)
	sessions = append(sessions, b.geminiMgr.ListSessions()...)

	if len(sessions) == 0 {
		return c.Send("No chat sessions yet. Use /new.")
	}

	activeCodex, _ := b.codexMgr.Active()
	activeGemini, _ := b.geminiMgr.Active()

	var sb strings.Builder
	sb.WriteString("<b>Chat sessions</b>\n")
	for _, sess := range sessions {
		providerID := "codex"
		isActive := activeCodex != nil && activeCodex.ID == sess.ID
		if _, ok := b.geminiMgr.GetSession(sess.ID); ok {
			providerID = "gemini"
			isActive = activeGemini != nil && activeGemini.ID == sess.ID
		}

		marker := " "
		if isActive {
			marker = "*"
		}
		sb.WriteString(fmt.Sprintf(
			"%s <code>%s</code> | <code>%s</code> | %s\n",
			marker,
			html.EscapeString(sess.ID),
			html.EscapeString(sess.RelName),
			providerID,
		))
	}

	return c.Send(sb.String(), b.sessionPickerMarkup(sessions), tele.ModeHTML)
}

func (b *Bot) handleUse(c tele.Context) error {
	id := strings.TrimSpace(c.Message().Payload)
	if id == "" {
		return b.sendSessionPicker(c, "use", "<b>Select the active session</b>")
	}

	var sess *chat.Session
	var err error
	var providerID string
	if _, ok := b.codexMgr.GetSession(id); ok {
		sess, err = b.codexMgr.SetActive(id)
		providerID = "codex"
	} else if _, ok = b.geminiMgr.GetSession(id); ok {
		sess, err = b.geminiMgr.SetActive(id)
		providerID = "gemini"
	} else {
		return c.Send(fmt.Sprintf("Session <code>%s</code> not found.", html.EscapeString(id)), tele.ModeHTML)
	}

	if err != nil {
		return c.Send(fmt.Sprintf("Error: <code>%s</code>", html.EscapeString(err.Error())), tele.ModeHTML)
	}

	b.currentProviderID = providerID


	return c.Send(
		fmt.Sprintf("Active session: <code>%s</code> in <code>%s</code>", html.EscapeString(sess.ID), html.EscapeString(sess.RelName)),
		b.sessionActions(sess),
		tele.ModeHTML,
	)
}

func (b *Bot) handleClose(c tele.Context) error {
	id := strings.TrimSpace(c.Message().Payload)
	if id == "" {
		return b.sendSessionPicker(c, "close", "<b>Select a session to close</b>")
	}

	var err error
	if _, ok := b.codexMgr.GetSession(id); ok {
		err = b.codexMgr.DeleteSession(id)
	} else if _, ok = b.geminiMgr.GetSession(id); ok {
		err = b.geminiMgr.DeleteSession(id)
	} else {
		return c.Send(fmt.Sprintf("Session <code>%s</code> not found.", html.EscapeString(id)), tele.ModeHTML)
	}

	if err != nil {
		return c.Send(fmt.Sprintf("Error: <code>%s</code>", html.EscapeString(err.Error())), tele.ModeHTML)
	}
	return c.Send(fmt.Sprintf("Closed session <code>%s</code>.", html.EscapeString(id)), tele.ModeHTML)
}

func (b *Bot) handleCurrent(c tele.Context) error {
	activeCodex, hasCodex := b.codexMgr.Active()
	activeGemini, hasGemini := b.geminiMgr.Active()

	if !hasCodex && !hasGemini {
		return c.Send("No active session. Use /new or /use.")
	}

	var sb strings.Builder
	if hasCodex {
		sb.WriteString(fmt.Sprintf("<b>Active Codex</b>: <code>%s</code> in <code>%s</code>\n", html.EscapeString(activeCodex.ID), html.EscapeString(activeCodex.RelName)))
	}
	if hasGemini {
		sb.WriteString(fmt.Sprintf("<b>Active Gemini</b>: <code>%s</code> in <code>%s</code>\n", html.EscapeString(activeGemini.ID), html.EscapeString(activeGemini.RelName)))
	}

	return c.Send(sb.String(), tele.ModeHTML)
}

func (b *Bot) handleChat(c tele.Context) error {
	msg := c.Message()
	if msg == nil {
		return nil
	}
	if strings.HasPrefix(strings.TrimSpace(msg.Text), "/") {
		return nil
	}

	var mgr chatProvider
	switch b.currentProviderID {
	case "gemini":
		if _, ok := b.geminiMgr.Active(); ok {
			mgr = b.geminiMgr
		}
	case "codex":
		if _, ok := b.codexMgr.Active(); ok {
			mgr = b.codexMgr
		}
	}

	// Fallback logic if currentProviderID is not set or session is gone
	if mgr == nil {
		if _, ok := b.geminiMgr.Active(); ok {
			mgr = b.geminiMgr
			b.currentProviderID = "gemini"
		} else if _, ok = b.codexMgr.Active(); ok {
			mgr = b.codexMgr
			b.currentProviderID = "codex"
		}
	}

	if mgr == nil {
		return c.Send("No active chat session. Use /new or /use.")
	}

	sess, err := mgr.ResolveActive()
	if err != nil {
		return c.Send(err.Error())
	}

	_ = c.Notify(tele.Typing)
	replySession, response, err := mgr.Send(context.Background(), sess.ID, msg.Text, nil)
	if err != nil {
		return c.Send(err.Error())
	}

	header := fmt.Sprintf("[%s | %s | %s]\n", mgr.Metadata().DisplayName, replySession.ID, replySession.RelName)
	return b.sendTextChunks(c, header+response)
}

type chatProvider interface {
	Metadata() provider.Metadata
	ResolveActive() (*chat.Session, error)
	Send(ctx context.Context, id, prompt string, attachments []chat.Attachment) (*chat.Session, string, error)
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
		return b.handleChatFolderPickLegacy(c, parts[1])
	case "cnav":
		if len(parts) != 2 {
			return nil
		}
		return b.handleChatNavigation(c, parts[1])
	case "p-pick":
		if len(parts) != 3 {
			return nil
		}
		return b.handleProviderPick(c, parts[1], parts[2])
	case "use":
		if len(parts) != 2 {
			return nil
		}
		id := parts[1]
		var sess *chat.Session
		var err error
		var providerID string
		if _, ok := b.codexMgr.GetSession(id); ok {
			sess, err = b.codexMgr.SetActive(id)
			providerID = "codex"
		} else {
			sess, err = b.geminiMgr.SetActive(id)
			providerID = "gemini"
		}
		if err != nil {
			return c.Send(fmt.Sprintf("Error: <code>%s</code>", html.EscapeString(err.Error())), tele.ModeHTML)
		}
		b.currentProviderID = providerID
		return c.Send(fmt.Sprintf("Active session: <code>%s</code> in <code>%s</code>", html.EscapeString(sess.ID), html.EscapeString(sess.RelName)), b.sessionActions(sess), tele.ModeHTML)
	case "close":
		if len(parts) != 2 {
			return nil
		}
		id := parts[1]
		var err error
		if _, ok := b.codexMgr.GetSession(id); ok {
			err = b.codexMgr.DeleteSession(id)
		} else {
			err = b.geminiMgr.DeleteSession(id)
		}
		if err != nil {
			return c.Send(fmt.Sprintf("Error: <code>%s</code>", html.EscapeString(err.Error())), tele.ModeHTML)
		}
		return c.Send(fmt.Sprintf("Closed session <code>%s</code>.", html.EscapeString(id)), tele.ModeHTML)
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

func (b *Bot) sendProviderPicker(c tele.Context, page int, text string) error {
	markup := &tele.ReplyMarkup{}
	markup.InlineKeyboard = [][]tele.InlineButton{
		{
			{Text: "Codex", Data: "p-pick:codex:browse"},
			{Text: "Gemini", Data: "p-pick:gemini:browse"},
		},
	}
	return c.Send(text, markup, tele.ModeHTML)
}

func (b *Bot) sendProviderPickerForFolder(c tele.Context, folder string, text string) error {
	markup := &tele.ReplyMarkup{}
	markup.InlineKeyboard = [][]tele.InlineButton{
		{
			{Text: "Codex", Data: "p-pick:codex:" + folder},
			{Text: "Gemini", Data: "p-pick:gemini:" + folder},
		},
	}
	return c.Send(text, markup, tele.ModeHTML)
}

func (b *Bot) handleProviderPick(c tele.Context, provider, folder string) error {
	if folder == "browse" {
		return b.sendChatFolderPicker(c, provider, 0, "<b>Select a repository for <code>"+provider+"</code></b>")
	}

	var sess *chat.Session
	var err error
	if provider == "codex" {
		sess, err = b.codexMgr.CreateSession(folder)
	} else {
		sess, err = b.geminiMgr.CreateSession(folder)
	}

	if err != nil {
		return c.Send(fmt.Sprintf("Error: <code>%s</code>", html.EscapeString(err.Error())), tele.ModeHTML)
	}

	b.currentProviderID = provider

	msg := fmt.Sprintf(
		"<b>%s session created</b>\nID: <code>%s</code>\nProject: <code>%s</code>\n\nSend text to chat.",
		strings.Title(provider),
		html.EscapeString(sess.ID),
		html.EscapeString(sess.RelName),
	)
	return c.Send(msg, b.sessionActions(sess), tele.ModeHTML)
}

func (b *Bot) sendChatFolderPicker(c tele.Context, provider string, page int, text string) error {
	folders := b.listGitFolders()
	if len(folders) == 0 {
		return c.Send("No git repositories found.")
	}

	markup := botutil.FolderPickerMarkup(botutil.FolderPickerConfig{
		Folders:  folders,
		Page:     page,
		PickData: func(index int) string { return fmt.Sprintf("p-pick:%s:%s", provider, folders[index]) },
		NavData:  func(p int) string { return fmt.Sprintf("cnav:%s:%d", provider, p) },
		Label:    func(folder string) string { return "Repo " + folder },
	})
	return c.Send(text, markup, tele.ModeHTML)
}

func (b *Bot) handleChatFolderPickLegacy(c tele.Context, indexStr string) error {
	// Fallback for old cpick format if needed, but new code uses p-pick
	return nil
}

func (b *Bot) handleChatNavigation(c tele.Context, data string) error {
	parts := strings.Split(data, ":")
	if len(parts) != 2 {
		return nil
	}
	providerID := parts[0]
	page, _ := strconv.Atoi(parts[1])
	return b.sendChatFolderPicker(c, providerID, page, "<b>Available repositories</b>\nTap to create a session.")
}

func (b *Bot) sendSessionPicker(c tele.Context, action, text string) error {
	var sessions []*chat.Session
	sessions = append(sessions, b.codexMgr.ListSessions()...)
	sessions = append(sessions, b.geminiMgr.ListSessions()...)

	if len(sessions) == 0 {
		return c.Send("No chat sessions yet.")
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

func (b *Bot) sessionActions(sess *chat.Session) *tele.ReplyMarkup {
	markup := &tele.ReplyMarkup{}
	markup.InlineKeyboard = [][]tele.InlineButton{
		{
			{Text: "Use", Data: "use:" + sess.ID},
			{Text: "Close", Data: "close:" + sess.ID},
		},
	}
	return markup
}

func (b *Bot) sessionPickerMarkup(sessions []*chat.Session) *tele.ReplyMarkup {
	markup := &tele.ReplyMarkup{}
	var rows [][]tele.InlineButton
	for _, sess := range sessions {
		rows = append(rows, []tele.InlineButton{
			{Text: "Use " + sess.ID, Data: "use:" + sess.ID},
			{Text: "Close", Data: "close:" + sess.ID},
		})
	}
	markup.InlineKeyboard = rows
	return markup
}

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
	if b.geminiMgr != nil {
		return b.geminiMgr.BaseFolder()
	}
	if b.sessionMgr != nil {
		return b.sessionMgr.BaseFolder()
	}
	return ""
}
