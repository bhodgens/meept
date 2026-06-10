; Python definitions query
; Extracts function, class, and variable definitions

; Function definitions
(function_definition name: (identifier) @name.definition.function)

; Async function definitions
(async_function_definition name: (identifier) @name.definition.function)

; Class definitions
(class_definition name: (identifier) @name.definition.class)

; Method definitions inside classes
(method_definition name: (identifier) @name.definition.method)

; Assignment statements (variable definitions)
(assignment left: (identifier) @name.definition.variable)

; Annotated assignment (type hints)
(annotated_assignment left: (identifier) @name.definition.variable)

; For loop variable
(for_statement left: (identifier) @name.definition.variable)

; With statement variable
(with_statement items: (with_item variable: (identifier) @name.definition.variable)

; Exception variable
(exception_group_statement name: (identifier) @name.definition.variable)

; Decorator names
(decorator (identifier) @name.definition.decorator)