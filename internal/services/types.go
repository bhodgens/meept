package services

// ListOptions contains common pagination and filtering parameters for list operations.
type ListOptions struct {
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
	Filter string `json:"filter,omitempty"`
}


// PaginatedResponse wraps a paginated list response.
type PaginatedResponse[T any] struct {
	Items      []T  `json:"items"`
	Total      int  `json:"total"`
	HasMore    bool `json:"has_more"`
	NextOffset int  `json:"next_offset,omitempty"`
}

