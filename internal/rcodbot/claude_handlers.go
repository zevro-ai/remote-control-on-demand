package rcodbot

import (
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/zevro-ai/remote-control-on-demand/internal/session"
	tele "gopkg.in/telebot.v4"
)

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
