; Java definitions query
; Extracts class, method, and variable definitions

; Class declarations
(class_declaration name: (identifier) @name.definition.class)

; Interface declarations
(interface_declaration name: (identifier) @name.definition.type)

; Method declarations
(method_declaration name: (identifier) @name.definition.method)

; Constructor declarations
(constructor_declaration name: (identifier) @name.definition.method)

; Field declarations
(field_declaration declarator: (variable_declarator name: (identifier) @name.definition.property))

; Local variable declarations
(local_variable_declaration declarator: (variable_declarator name: (identifier) @name.definition.variable))

; Enum declarations
(enum_declaration name: (identifier) @name.definition.type)

; Annotation declarations
(annotation_declaration name: (identifier) @name.definition.annotation)

; Record declarations (Java 14+)
(record_declaration name: (identifier) @name.definition.type)