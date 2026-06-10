; Generic definitions query (fallback)
; Matches common definition patterns across languages

; Function-like definitions
(function_declaration name: (identifier) @name.definition.function)
(function_definition name: (identifier) @name.definition.function)
(function_item name: (identifier) @name.definition.function)

; Class-like definitions
(class_declaration name: (identifier) @name.definition.class)
(class_definition name: (identifier) @name.definition.class)
(struct_item name: (type_identifier) @name.definition.type)

; Method definitions
(method_declaration name: (identifier) @name.definition.method)
(method_definition name: (property_identifier) @name.definition.method)

; Type definitions
(type_declaration name: (type_identifier) @name.definition.type)
(interface_declaration name: (type_identifier) @name.definition.type)

; Variable/constant definitions
(variable_declarator name: (identifier) @name.definition.variable)
(const_declaration (const_spec name: (identifier) @name.definition.constant))