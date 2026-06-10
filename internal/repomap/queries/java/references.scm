; Java references query
; Extracts identifier usages

; All identifiers
(identifier) @ref.variable

; Type identifiers
(type_identifier) @ref.type

; Field access
(field_access field: (identifier) @ref.field)