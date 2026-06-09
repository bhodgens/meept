// internal/configui/sections_telegram.go
package configui

import (
	"strconv"

)

func buildTelegramFields() []Field {
	cfg := loadMainConfigOrFallback()
	s := &cfg.Telegram
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewMaskedField("token", "token", s.Token),
		NewTextField("creator_id", "creator id", strconv.FormatInt(s.CreatorID, 10)),
		NewNumberField("poll_timeout", "poll timeout", s.PollTimeout),
	}
}
