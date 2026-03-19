package botutil

import (
	"fmt"

	tele "gopkg.in/telebot.v4"
)

const DefaultPageSize = 8

type FolderPickerConfig struct {
	Folders  []string
	Page     int
	PageSize int
	PickData func(index int) string
	NavData  func(page int) string
	Label    func(folder string) string
}

func FolderPickerMarkup(cfg FolderPickerConfig) *tele.ReplyMarkup {
	pageSize := cfg.PageSize
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}

	page := cfg.Page
	if page < 0 {
		page = 0
	}
	lastPage := (len(cfg.Folders) - 1) / pageSize
	if page > lastPage {
		page = lastPage
	}

	start := page * pageSize
	end := min(start+pageSize, len(cfg.Folders))

	markup := &tele.ReplyMarkup{}
	var rows [][]tele.InlineButton
	for i, folder := range cfg.Folders[start:end] {
		label := folder
		if cfg.Label != nil {
			label = cfg.Label(folder)
		}
		rows = append(rows, []tele.InlineButton{{
			Text: label,
			Data: cfg.PickData(start + i),
		}})
	}

	if lastPage > 0 {
		var navRow []tele.InlineButton
		if page > 0 {
			navRow = append(navRow, tele.InlineButton{Text: "◀ Prev", Data: cfg.NavData(page - 1)})
		}
		navRow = append(navRow, tele.InlineButton{Text: fmt.Sprintf("%d/%d", page+1, lastPage+1), Data: "noop:0"})
		if page < lastPage {
			navRow = append(navRow, tele.InlineButton{Text: "Next ▶", Data: cfg.NavData(page + 1)})
		}
		rows = append(rows, navRow)
	}

	markup.InlineKeyboard = rows
	return markup
}

type PickerItem struct {
	Label string
	Data  string
}

func PickerMarkup(items []PickerItem) *tele.ReplyMarkup {
	markup := &tele.ReplyMarkup{}
	var rows [][]tele.InlineButton
	for _, item := range items {
		rows = append(rows, []tele.InlineButton{{
			Text: item.Label,
			Data: item.Data,
		}})
	}
	markup.InlineKeyboard = rows
	return markup
}
