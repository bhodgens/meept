package bot

import (
	"github.com/caimlas/meept/internal/comm/telegram"
)

// Compile-time interface checks: verify TelegramChannel implements our interfaces.
var _ Bot = (*telegram.TelegramChannel)(nil)
var _ MessagingBot = (*telegram.TelegramChannel)(nil)
