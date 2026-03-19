package botutil

import (
	"fmt"

	tele "gopkg.in/telebot.v4"
)

func Auth(allowedUserID int64, next tele.HandlerFunc) tele.HandlerFunc {
	return func(c tele.Context) error {
		if c.Sender().ID != allowedUserID {
			if c.Callback() != nil {
				return c.Respond(&tele.CallbackResponse{Text: "Access denied."})
			}
			return c.Send("Access denied.")
		}
		return next(c)
	}
}

type User struct {
	ID int64
}

func (u *User) Recipient() string {
	return fmt.Sprintf("%d", u.ID)
}
