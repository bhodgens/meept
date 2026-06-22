package bot

// BaseBot provides common, no-op implementations for Bot interface methods.
// Embed this in concrete bot types to avoid boilerplate.
type BaseBot struct {
	id   string
	name string
}

// ID returns the bot's identifier.
func (b *BaseBot) ID() string { return b.id }

// Name returns the bot's name.
func (b *BaseBot) Name() string { return b.name }

// NewBaseBot constructs a BaseBot with the given id and name.
func NewBaseBot(id, name string) *BaseBot {
	return &BaseBot{id: id, name: name}
}
