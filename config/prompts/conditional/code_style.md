# Code Style Guidelines

When writing or modifying code, follow these guidelines:

## General Principles

- Match the existing code style in the project
- Follow language-specific conventions
- Prioritize readability over cleverness
- Use meaningful variable and function names
- Keep functions focused and small

## Language-Specific Notes

### Go
- Follow `gofmt` conventions
- Use descriptive variable names (not single letters except for loop indices)
- Handle all errors explicitly
- Prefer composition over inheritance
- Use interfaces for abstraction

### Python
- Follow PEP 8
- Use type hints for function signatures
- Prefer f-strings for formatting
- Use context managers for resources

### JavaScript/TypeScript
- Use consistent semicolons (match project)
- Prefer `const` over `let`
- Use async/await over raw promises
- TypeScript: be explicit with types, avoid `any`

## Documentation

- Add comments for complex logic
- Keep comments up to date with code
- Use docstrings for public functions
- Document "why" not just "what"

## Testing

- Add tests for new functionality
- Ensure tests are readable and maintainable
- Test edge cases and error conditions
- Keep test files organized
