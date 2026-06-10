; Go definitions query
; Using patterns from existing codebase that work with tree-sitter-go

; Function declarations
(function_declaration name: (identifier) @name.definition.function)

; Type declarations (structs)
(type_declaration (type_spec name: (type_identifier) @name.definition.type))

; Constant declarations
(const_declaration (const_spec name: (identifier) @name.definition.constant))

; Variable declarations
(var_declaration (var_spec name: (identifier) @name.definition.variable))

; Type alias
(type_alias name: (type_identifier) @name.definition.type)