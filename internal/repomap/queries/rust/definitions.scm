; Rust definitions query
; Extracts function, struct, enum, trait, and variable definitions

; Function definitions
(function_item name: (identifier) @name.definition.function)

; Method definitions in impl blocks
(method_declaration name: (identifier) @name.definition.method)

; Struct definitions
(struct_item name: (type_identifier) @name.definition.type)

; Enum definitions
(enum_item name: (type_identifier) @name.definition.type)

; Trait definitions
(trait_item name: (type_identifier) @name.definition.type)

; Impl blocks
(impl_item type: (type_identifier) @name.definition.type)

; Constant items
(const_item name: (identifier) @name.definition.constant)

; Static items
(static_item name: (identifier) @name.definition.variable)

; Let bindings
(let_declaration pattern: (identifier) @name.definition.variable)

; Function parameters
(parameters (identifier) @name.definition.parameter)

; Enum variant definitions
(enum_variant (identifier) @name.definition.enum_variant)

; Type aliases
(type_alias_declaration name: (type_identifier) @name.definition.type)