package telegram

// baseBot provides default implementations for bot.Bot methods,
// keeping the telegram package free of direct imports into internal/bot
// (which would cause an import cycle: bot -> telegram -> bot).
type baseBot struct {
	id   string
	name string
}

func newBaseBot(id, name string) *baseBot {
	return &baseBot{id: id, name: name}
}

func (b *baseBot) ID() string   { return b.id }
func (b *baseBot) Name() string { return b.name }
