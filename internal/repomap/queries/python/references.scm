; Python references query
; Extracts identifier usages

; All identifiers
(identifier) @ref.variable

; Attribute lookups
(attribute attribute: (identifier) @ref.attribute)

; Subscript indexing
(subscript value: (identifier) @ref.variable)