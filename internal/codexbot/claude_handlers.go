package codexbot

import (
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/zevro-ai/remote-control-on-demand/internal/session"
	tele "gopkg.in/telebot.v4"
)

func (b *Bot) forwardNotifications() {
	if b.sessionMgr == nil {
		return
	}

	for n := range b.sessionMgr.Notifications() {
		b.SendMessage(n.Message)
	}
}

func (b *Bot) handleList(c tele.Context) error {
	sessions := b.sessionMgr.List()
	if len(sessions) == 0 {
		return c.Send("No active Claude sessions.")
	}

	var sb strings.Builder
	sb.WriteString("<b>Claude sessions</b>\n")
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
		return b.sendClaudeSessionPicker(c, "kill", "<b>Select a Claude session to stop</b>")
	}
	return b.killSession(c, id)
}

func (b *Bot) handleStatus(c tele.Context) error {
	id := strings.TrimSpace(c.Message().Payload)
	if id == "" {
		return b.sendClaudeSessionPicker(c, "status", "<b>Select a Claude session</b>")
	}
	return b.showStatus(c, id)
}

func (b *Bot) handleLogs(c tele.Context) error {
	id := strings.TrimSpace(c.Message().Payload)
	if id == "" {
		return b.sendClaudeSessionPicker(c, "logs", "<b>Select a Claude session</b>")
	}
	return b.showLogs(c, id)
}

func (b *Bot) handleRestart(c tele.Context) error {
	id := strings.TrimSpace(c.Message().Payload)
	if id == "" {
		return b.sendClaudeSessionPicker(c, "restart", "<b>Select a Claude session to restart</b>")
	}
	return b.restartSession(c, id)
}

func (b *Bot) startSession(c tele.Context, folder string) error {
	sess, err := b.sessionMgr.Start(folder)
	if err != nil {
		return c.Send(fmt.Sprintf("Error: <code>%s</code>", html.EscapeString(err.Error())), tele.ModeHTML)
	}

	msg := fmt.Sprintf(
		"<b>Claude session started</b>\nID: <code>%s</code>\nProject: <code>%s</code>",
		html.EscapeString(sess.ID),
		html.EscapeString(folder),
	)
	return c.Send(msg, b.claudeSessionActions(sess), tele.ModeHTML)
}

func (b *Bot) killSession(c tele.Context, id string) error {
	if err := b.sessionMgr.Kill(id); err != nil {
		return c.Send(fmt.Sprintf("Error: <code>%s</code>", html.EscapeString(err.Error())), tele.ModeHTML)
	}
	return c.Send(fmt.Sprintf("Claude session <code>%s</code> stopped.", html.EscapeString(id)), tele.ModeHTML)
}

func (b *Bot) showStatus(c tele.Context, id string) error {
	sess, ok := b.sessionMgr.Get(id)
	if !ok {
		return c.Send(fmt.Sprintf("Claude session <code>%s</code> not found.", html.EscapeString(id)), tele.ModeHTML)
	}

	uptime := time.Since(sess.StartedAt).Truncate(time.Second)
	pid := 0
	if sess.Cmd != nil && sess.Cmd.Process != nil {
		pid = sess.Cmd.Process.Pid
	}

	msg := fmt.Sprintf(
		"<b>Claude session %s</b>\nProject: <code>%s</code>\nFolder: <code>%s</code>\nStatus: %s\nPID: %d\nUptime: %s\nRestarts: %d",
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

	return c.Send(msg, b.claudeSessionActions(sess), tele.ModeHTML)
}

func (b *Bot) showLogs(c tele.Context, id string) error {
	sess, ok := b.sessionMgr.Get(id)
	if !ok {
		return c.Send(fmt.Sprintf("Claude session <code>%s</code> not found.", html.EscapeString(id)), tele.ModeHTML)
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
	if err := b.sessionMgr.Restart(id); err != nil {
		return c.Send(fmt.Sprintf("Error: <code>%s</code>", html.EscapeString(err.Error())), tele.ModeHTML)
	}

	sess, ok := b.sessionMgr.Get(id)
	if !ok {
		return c.Send(fmt.Sprintf("Claude session <code>%s</code> restarted.", html.EscapeString(id)), tele.ModeHTML)
	}
	return c.Send(fmt.Sprintf("Claude session <code>%s</code> restarted.", html.EscapeString(id)), b.claudeSessionActions(sess), tele.ModeHTML)
}

func (b *Bot) sendClaudeFolderPicker(c tele.Context, action string, page int, text string) error {
	folders := b.listGitFolders()
	if len(folders) == 0 {
		return c.Send("No git repositories found in the projects folder.", tele.ModeHTML)
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
			Text: "📂 " + folder,
			Data: fmt.Sprintf("pick:%s:%d", action, start+i),
		}})
	}

	if lastPage > 0 {
		var navRow []tele.InlineButton
		if page > 0 {
			navRow = append(navRow, tele.InlineButton{Text: "◀ Prev", Data: fmt.Sprintf("nav:%s:%d", action, page-1)})
		}
		navRow = append(navRow, tele.InlineButton{Text: fmt.Sprintf("%d/%d", page+1, lastPage+1), Data: "noop:0"})
		if page < lastPage {
			navRow = append(navRow, tele.InlineButton{Text: "Next ▶", Data: fmt.Sprintf("nav:%s:%d", action, page+1)})
		}
		rows = append(rows, navRow)
	}

	markup.InlineKeyboard = rows
	return c.Send(text, markup, tele.ModeHTML)
}

func (b *Bot) sendClaudeSessionPicker(c tele.Context, action, text string) error {
	sessions := b.sessionMgr.List()
	var running []*session.Session
	for _, s := range sessions {
		if s.Status == session.StatusRunning {
			running = append(running, s)
		}
	}
	if len(running) == 0 {
		return c.Send("No active Claude sessions.")
	}

	markup := &tele.ReplyMarkup{}
	var rows [][]tele.InlineButton
	for _, s := range running {
		label := fmt.Sprintf("%s - %s", s.ID, s.RelName)
		rows = append(rows, []tele.InlineButton{{
			Text: label,
			Data: action + ":" + s.ID,
		}})
	}
	markup.InlineKeyboard = rows
	return c.Send(text, markup, tele.ModeHTML)
}

func (b *Bot) handleClaudeFolderPick(c tele.Context, action string, index int) error {
	folders := b.listGitFolders()
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

func (b *Bot) claudeSessionActions(sess *session.Session) *tele.ReplyMarkup {
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
