package immutable

type WithImmutable struct {
	cfg string // immutable
	Name string
}

// Correct: immutable field set in constructor (New* function)
func NewWithImmutable(cfg string) *WithImmutable {
	return &WithImmutable{
		cfg:  cfg,
		Name: "test",
	}
}

// WRONG: immutable field write in non-constructor
func (w *WithImmutable) SetCfgWrong(cfg string) {
	w.cfg = cfg // want "fieldguard: writing to immutable field"
}

// Correct: mutable field write
func (w *WithImmutable) SetName(name string) {
	w.Name = name
}
