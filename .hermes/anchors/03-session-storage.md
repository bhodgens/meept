# Verified Anchors - branch 03-session-storage (verified at 40f6fa5a)

Grep-verified. Use these exact shapes; do not invent APIs.

- `internal/bus/topic.go:20` — `func NewTopic[T any](name string) Topic[T]`
- `internal/bus/topic.go:37` — `func PublishT[T any](b *MessageBus, t Topic[T], source string, payload T) int`
- `internal/bus/topic.go:80` — `func SubscribeT[T any](b *MessageBus, id string, t Topic[T], fn func(T)) *Subscriber`
- `internal/bus/bus.go:289` — `func (b *MessageBus) Subscribe(id, topic string) *Subscriber` (raw string topic)
- `internal/session/store_sqlite.go:29` — `type SQLiteStore struct`; `:77` `NewSQLiteStore(dbPath string, logger *slog.Logger, opts ...Option) (*SQLiteStore, error)`
- `internal/session/store_sqlite.go:760` — `(s *SQLiteStore) Delete(id string) bool`
- `internal/session/store_sqlite.go:896` — `(s *SQLiteStore) SaveMessages(sessionID string, messages []Message) error`
- `internal/session/store_sqlite.go:1078` — `(s *SQLiteStore) ClearMessages(sessionID string) error`
- `internal/session/store_sqlite.go:42-45` — `turnGossip TurnGossipPublisher` field; `:977` the post-commit publish site (snapshot under lock, fire after unlock) — the pattern leaf 02/04 mirror
- `internal/session/store.go:12` — `type Message struct` (read its fields before writing fold tests)
- `internal/session/store.go:80-105` — `type Store interface` (S1 appends GetSchemaVersion/MigrateSchema here)
- `internal/llm/spill.go:51` — `type SpillService struct`; `:67` `NewSpillService(meeptHome string) *SpillService`

RED-gate convention (repo precedent commit 537c92fd): a gate test that
references not-yet-existing symbols may be committed as a `.staged` file
(`format_migration_test.go.staged`) so `go vet`/`go build` stay green for the
rest of the repo, and the leaf renames it live when its implementation lands.
Prefer writing the test live ONLY if the package still compiles without the
implementation (i.e. the test file alone does not break `go vet` for the
package). When in doubt: write live, run `go vet ./internal/session`, and if
the undefined-symbol failure is confined to the test binary (`go test` fails,
`go build`/`go vet` of the package stay green), keep it live — that is the
ideal shape. Only park as `.staged` if `go vet`/`go build` of the NON-test
package breaks.

e2e suite shape: `//go:build e2e` + package name + import
`github.com/caimlas/meept/e2e/harness`. Register the suite in
`e2e/manifest.json` (path_map entry + suites entry) in the same change.
`harness.Start(t)` boots a real daemon; for store/bus contracts the leaf doc
permits spinning SQLiteStore + MessageBus in-process instead — simpler.
