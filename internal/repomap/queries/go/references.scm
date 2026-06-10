; Go references query
; Extracts identifier usages (references to symbols)

; All identifiers that are not definitions
(identifier) @ref.variable

; Field references
(field_identifier) @ref.field

; Type references in type annotations
(type_identifier) @ref.type

; Package name references in import paths
(import_spec path: (import_path) @ref.package)