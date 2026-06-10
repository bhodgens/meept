; TypeScript/JavaScript references query
; Extracts identifier usages

; All identifiers
(identifier) @ref.variable

; Property identifiers
(property_identifier) @ref.property

; Type identifiers in type annotations
(type_identifier) @ref.type

; Template string interpolations
(interpolation (identifier) @ref.variable)