# Models CLI

## Overview

The `meept models` command provides interactive model and provider management, allowing users to configure LLM providers, select models, and manage API credentials without manually editing configuration files.

## User Stories

- As a user, I want to easily add new LLM providers without editing JSON config files
- As a user, I want to see what models are available for each provider
- As a user, I want to switch between models quickly
- As a user, I want to manage API credentials securely
- As a user, I want visual feedback when selecting providers and models

## Requirements

### Functional

1. **Provider Catalog**: Display list of supported providers with metadata
2. **Model Catalog**: Display available models per provider with specs
3. **Interactive Selection**: TUI with keyboard navigation for provider/model selection
4. **Configuration Persistence**: Save selections to `~/.meept/models.json5`
5. **Credential Management**: Store API keys securely in `~/.meept/credentials.json`
6. **CLI Commands**: Support list, add, remove, set-default, config operations

### Non-Functional

1. **Security**: Credential file must have 0600 permissions
2. **Usability**: Arrow key navigation, type-to-filter filtering
3. **Compatibility**: Work with existing models.json5 format
4. **Discoverability**: Help text for all commands

## Design

### Provider Registry

Providers are defined in `internal/llm/provider_registry.go`:

```go
type ProviderDef struct {
    ID           string            // Canonical ID (e.g., "anthropic")
    Name         string            // Display name (e.g., "Anthropic")
    Transport    ProviderTransport // API protocol type
    AuthType     AuthType          // Authentication method
    APIKeyEnvVar string            // Environment variable name
    BaseURL      string            // API base URL
    DocURL       string            // Documentation URL
    Supports     []string          // Capabilities list
}
```

### Model Catalog

Models are defined in `internal/llm/models_catalog.go`:

```go
type ModelCatalogEntry struct {
    ModelID       string   // Model identifier
    Name          string   // Display name
    ProviderID    string   // Parent provider
    ContextWindow int      // Context size in tokens
    MaxOutput     int      // Max output tokens
    InputCost     float64  // $ per million input tokens
    OutputCost    float64  // $ per million output tokens
    Capabilities  []string // Model capabilities
}
```

### TUI Model Picker

The picker uses Bubble Tea v2 with two modes:
1. **Provider Selection**: List of all providers with filtering
2. **Model Selection**: List of models for selected provider

### CLI Commands

| Command | Description |
|---------|-------------|
| `meept models setup` | Interactive setup wizard |
| `meept models list` | List configured models |
| `meept models providers` | List available providers |
| `meept models add` | Add new provider/model |
| `meept models remove <ref>` | Remove a model |
| `meept models set-default <ref>` | Set default model |
| `meept models config` | Show/edit configuration |
| `meept models credentials` | Manage API credentials |

## Implementation

### File Structure

```
internal/llm/
  provider_registry.go    # Provider definitions
  models_catalog.go       # Model definitions
  model_picker.go         # TUI picker component
  credentials.go          # Credential storage

cmd/meept/
  models.go               # CLI command implementation
```

### Configuration Flow

1. User runs `meept models setup`
2. TUI picker launches in provider selection mode
3. User selects provider (arrow keys + enter)
4. TUI switches to model selection mode
5. User selects model
6. Configuration saved to `~/.meept/models.json5`
7. If API key needed, prompted to store in credentials

### Credential Storage

Credentials stored in JSON format:
```json
{
  "anthropic": "sk-ant-...",
  "openai": "sk-..."
}
```

File permissions set to 0600 (owner read/write only).

## Testing

### Unit Tests

- `TestGetProviderByID`: Verify provider lookup
- `TestGetProviderByEnvVar`: Verify environment variable lookup
- `TestListProviders`: Verify filtering by transport type
- `TestGetModelsForProvider`: Verify model catalog lookup
- `TestGetModel`: Verify specific model retrieval
- `TestModelCatalogEntry`: Verify model metadata

### Manual Testing

```bash
# Build and test
go build -o bin/meept ./cmd/meept
./bin/meept models providers
./bin/meept models list
./bin/meept models setup  # Interactive
```

## Future Enhancements

1. **Dynamic Model Discovery**: Fetch model lists from provider APIs
2. **OAuth Support**: Add device code and external OAuth flows
3. **System Keychain**: Integrate with macOS Keychain / Windows Credential Manager
4. **Model Comparison**: Side-by-side model specification comparison
5. **Usage Tracking**: Track token usage per model/provider
6. **Auto-Configuration**: Detect available providers via environment scanning

## Related Documentation

- [Models CLI Reference](../reference/models-cli.md)
- [LLM Management](llm-management.md)
- [Configuration](../configuration/)
