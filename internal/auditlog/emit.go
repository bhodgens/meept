package auditlog

import "context"

// Emit implements employee.ChainEmitter. It maps the record through the
// store's Append and returns the error. Kept in the auditlog package so
// employee never depends on concrete Store construction details.
func (s *Store) Emit(rec Record) error {
	_, err := s.Append(context.Background(), rec)
	return err
}
