package services

// ListOptions contains common pagination and filtering parameters for list operations.
type ListOptions struct {
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
	Filter string `json:"filter,omitempty"`
}

// DefaultListOptions returns sensible defaults for list operations.
func DefaultListOptions() ListOptions {
	return ListOptions{
		Limit:  20,
		Offset: 0,
		Filter: "",
	}
}

// PaginatedResponse wraps a paginated list response.
type PaginatedResponse[T any] struct {
	Items      []T  `json:"items"`
	Total      int  `json:"total"`
	HasMore    bool `json:"has_more"`
	NextOffset int  `json:"next_offset,omitempty"`
}

// NewPaginatedResponse creates a paginated response from items and options.
func NewPaginatedResponse[T any](items []T, total int, opts ListOptions) PaginatedResponse[T] {
	hasMore := opts.Offset+len(items) < total
	nextOffset := opts.Offset + len(items)
	if !hasMore {
		nextOffset = 0
	}
	return PaginatedResponse[T]{
		Items:      items,
		Total:      total,
		HasMore:    hasMore,
		NextOffset: nextOffset,
	}
}
