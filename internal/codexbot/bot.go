package codexbot

import (
	"fmt"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zevro-ai/remote-control-on-demand/internal/codex"
	"github.com/zevro-ai/remote-control-on-demand/internal/session"
	tele "gopkg.in/telebot.v4"
)

const folderPageSize = 8

type Bot struct {
	tb            *tele.Bot
	sessionMgr    *session.Manager
	codexMgr      *codex.Manager
	allowedUserID int64
}

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
	go b.forwardNotifications()
	b.sendWelcome()
	b.tb.Start()
}

func (b *Bot) Stop() {
	b.tb.Stop()
}

func (b *Bot) SendMessage(msg string) {
	recipient := &user{id: b.allowedUserID}
	b.tb.Send(recipient, msg, tele.ModeHTML)
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

func (b *Bot) auth(next tele.HandlerFunc) tele.HandlerFunc {
	return func(c tele.Context) error {
		if c.Sender().ID != b.allowedUserID {
			if c.Callback() != nil {
				return c.Respond(&tele.CallbackResponse{Text: "Access denied."})
			}
			return c.Send("Access denied.")
		}
		return next(c)
	}
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

	resolved, matches := matchFolderQuery(b.listGitFolders(), folder)
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
				formatCodeList(matches, 8),
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
		return b.sendFolderPicker(c, 0, "<b>Select a repository for a new Codex session</b>")
	}

	resolved, matches := matchFolderQuery(b.listGitFolders(), folder)
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
				formatCodeList(matches, 8),
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

	return c.Send(sb.String(), b.sessionPickerMarkup(sessions), tele.ModeHTML)
}

func (b *Bot) handleUse(c tele.Context) error {
	id := strings.TrimSpace(c.Message().Payload)
	if id == "" {
		return b.sendSessionPicker(c, "cuse", "<b>Select the active session</b>")
	}

	sess, err := b.codexMgr.SetActive(id)
	if err != nil {
		return c.Send(fmt.Sprintf("Error: <code>%s</code>", html.EscapeString(err.Error())), tele.ModeHTML)
	}

	return c.Send(
		fmt.Sprintf("Active session: <code>%s</code> in <code>%s</code>", html.EscapeString(sess.ID), html.EscapeString(sess.RelName)),
		b.sessionActions(sess),
		tele.ModeHTML,
	)
}

func (b *Bot) handleClose(c tele.Context) error {
	id := strings.TrimSpace(c.Message().Payload)
	if id == "" {
		return b.sendSessionPicker(c, "cclose", "<b>Select a session to close</b>")
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
	return c.Send(msg, b.sessionActions(sess), tele.ModeHTML)
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
	replySession, response, err := b.codexMgr.Send(sess.ID, msg.Text)
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
		index, err := strconv.Atoi(parts[2])
		if err != nil {
			return nil
		}
		return b.handleClaudeFolderPick(c, parts[1], index)
	case "nav":
		if len(parts) != 3 {
			return nil
		}
		page, err := strconv.Atoi(parts[2])
		if err != nil {
			return nil
		}
		return b.sendClaudeFolderPicker(c, parts[1], page, "<b>Available projects</b>\nTap to start a Claude session.")
	case "cpick":
		if len(parts) != 2 {
			return nil
		}
		index, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil
		}
		return b.handleFolderPick(c, index)
	case "cnav":
		if len(parts) != 2 {
			return nil
		}
		page, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil
		}
		return b.sendFolderPicker(c, page, "<b>Available repositories</b>\nTap to create a Codex session.")
	case "cuse":
		if len(parts) != 2 {
			return nil
		}
		sess, err := b.codexMgr.SetActive(parts[1])
		if err != nil {
			return c.Send(fmt.Sprintf("Error: <code>%s</code>", html.EscapeString(err.Error())), tele.ModeHTML)
		}
		return c.Send(fmt.Sprintf("Active session: <code>%s</code> in <code>%s</code>", html.EscapeString(sess.ID), html.EscapeString(sess.RelName)), b.sessionActions(sess), tele.ModeHTML)
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
	return c.Send(msg, b.sessionActions(sess), tele.ModeHTML)
}

func (b *Bot) sendFolderPicker(c tele.Context, page int, text string) error {
	folders := b.listGitFolders()
	if len(folders) == 0 {
		return c.Send("No git repositories found in the projects folder.")
	}

	if page < 0 {
		page = 0
	}
	lastPage := (len(folders) - 1) / folderPageSize
	if page > lastPage {
		page = lastPage
	}

	start := page * folderPageSize
	end := min(start+folderPageSize, len(folders))

	markup := &tele.ReplyMarkup{}
	var rows [][]tele.InlineButton
	for i, folder := range folders[start:end] {
		rows = append(rows, []tele.InlineButton{{
			Text: "Repo " + folder,
			Data: fmt.Sprintf("cpick:%d", start+i),
		}})
	}

	if lastPage > 0 {
		var navRow []tele.InlineButton
		if page > 0 {
			navRow = append(navRow, tele.InlineButton{Text: "Prev", Data: fmt.Sprintf("cnav:%d", page-1)})
		}
		navRow = append(navRow, tele.InlineButton{Text: fmt.Sprintf("%d/%d", page+1, lastPage+1), Data: "noop:0"})
		if page < lastPage {
			navRow = append(navRow, tele.InlineButton{Text: "Next", Data: fmt.Sprintf("cnav:%d", page+1)})
		}
		rows = append(rows, navRow)
	}

	markup.InlineKeyboard = rows
	return c.Send(text, markup, tele.ModeHTML)
}

func (b *Bot) sendSessionPicker(c tele.Context, action, text string) error {
	sessions := b.codexMgr.List()
	if len(sessions) == 0 {
		return c.Send("No Codex sessions yet. Use /new.")
	}

	markup := &tele.ReplyMarkup{}
	var rows [][]tele.InlineButton
	for _, sess := range sessions {
		rows = append(rows, []tele.InlineButton{{
			Text: fmt.Sprintf("%s - %s", sess.ID, sess.RelName),
			Data: action + ":" + sess.ID,
		}})
	}
	markup.InlineKeyboard = rows
	return c.Send(text, markup, tele.ModeHTML)
}

func (b *Bot) handleFolderPick(c tele.Context, index int) error {
	folders := b.listGitFolders()
	if index < 0 || index >= len(folders) {
		return c.Send("That repository is no longer available. Refresh with /new.")
	}
	return b.createSession(c, folders[index])
}

func (b *Bot) sessionActions(sess *codex.Session) *tele.ReplyMarkup {
	markup := &tele.ReplyMarkup{}
	markup.InlineKeyboard = [][]tele.InlineButton{
		{
			{Text: "Use", Data: "cuse:" + sess.ID},
			{Text: "Close", Data: "cclose:" + sess.ID},
		},
	}
	return markup
}

func (b *Bot) sessionPickerMarkup(sessions []*codex.Session) *tele.ReplyMarkup {
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

func splitChunks(text string, maxLen int) []string {
	runes := []rune(text)
	if len(runes) <= maxLen {
		return []string{text}
	}

	var chunks []string
	for len(runes) > 0 {
		if len(runes) <= maxLen {
			chunks = append(chunks, string(runes))
			break
		}

		cut := maxLen
		for i := maxLen - 1; i >= maxLen/2; i-- {
			if runes[i] == '\n' || runes[i] == ' ' {
				cut = i + 1
				break
			}
		}

		chunks = append(chunks, string(runes[:cut]))
		runes = runes[cut:]
	}

	return chunks
}

func (b *Bot) listGitFolders() []string {
	return listGitFolders(b.baseFolder())
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

func listGitFolders(baseFolder string) []string {
	info, err := os.Stat(baseFolder)
	if err != nil || !info.IsDir() {
		return nil
	}

	var folders []string
	err = filepath.WalkDir(baseFolder, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path == baseFolder {
			return nil
		}

		if d.IsDir() {
			if shouldSkipScanDir(d.Name()) {
				return filepath.SkipDir
			}
			if hasGitMetadata(path) {
				rel, err := filepath.Rel(baseFolder, path)
				if err == nil && rel != "." {
					folders = append(folders, rel)
				}
				return filepath.SkipDir
			}
		}

		return nil
	})
	if err != nil {
		return nil
	}

	sort.Strings(folders)
	return folders
}

func shouldSkipScanDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}

	switch name {
	case "node_modules", "vendor", "dist", "build", "tmp":
		return true
	default:
		return false
	}
}

func hasGitMetadata(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func matchFolderQuery(folders []string, query string) (string, []string) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", nil
	}

	normalizedQuery := normalizeFolderQuery(query)
	for _, folder := range folders {
		if normalizeFolderQuery(folder) == normalizedQuery {
			return folder, nil
		}
	}

	var matches []string
	for _, folder := range folders {
		if strings.Contains(strings.ToLower(filepath.ToSlash(folder)), normalizedQuery) {
			matches = append(matches, folder)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return "", matches
}

func normalizeFolderQuery(value string) string {
	return strings.ToLower(filepath.ToSlash(filepath.Clean(value)))
}

func formatCodeList(items []string, limit int) string {
	if len(items) == 0 {
		return ""
	}

	maxItems := min(len(items), limit)
	lines := make([]string, 0, maxItems+1)
	for _, item := range items[:maxItems] {
		lines = append(lines, "• <code>"+html.EscapeString(item)+"</code>")
	}
	if len(items) > maxItems {
		lines = append(lines, fmt.Sprintf("• and %d more", len(items)-maxItems))
	}
	return strings.Join(lines, "\n")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type user struct {
	id int64
}

func (u *user) Recipient() string {
	return fmt.Sprintf("%d", u.id)
}
