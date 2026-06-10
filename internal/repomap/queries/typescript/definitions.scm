; TypeScript/JavaScript definitions query
; Extracts function, class, method, and variable definitions

; Function declarations
(function_declaration name: (identifier) @name.definition.function)

; Function expressions assigned to variables
(variable_declarator name: (identifier) @name.definition.variable
  value: (function))

; Arrow functions assigned to variables
(variable_declarator name: (identifier) @name.definition.variable
  value: (arrow_function))

; Method definitions in classes
(method_definition name: (property_identifier) @name.definition.method)

; Property definitions in classes
(property_definition name: (property_identifier) @name.definition.property)

; Class declarations
(class_declaration name: (identifier) @name.definition.class)

; Interface declarations
(interface_declaration name: (type_identifier) @name.definition.type)

; Type alias declarations
(type_alias_declaration name: (type_identifier) @name.definition.type)

; Enum declarations
(enum_declaration name: (identifier) @name.definition.type)

; Generator functions
(generator_function_declaration name: (identifier) @name.definition.function)

; Async functions
(async_function_declaration name: (identifier) @name.definition.function)

; Getter methods
(getter_definition name: (property_identifier) @name.definition.method)

; Setter methods
(setter_definition name: (property_identifier) @name.definition.method)