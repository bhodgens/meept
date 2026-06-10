; Rust references query
; Extracts identifier usages

; All identifiers
(identifier) @ref.variable

; Type identifiers
(type_identifier) @ref.type

; Field expressions
(field_expression field: (field_identifier) @ref.field)