# Models CLI Reference

The `meept models` command provides interactive model and provider management.

## Commands

### `meept models` (or `meept models setup`)

Launches the interactive setup wizard for adding/configuring models.

```bash
$ meept models setup
```

This opens an interactive TUI with:
- Provider selection (arrow keys to navigate, type to filter)
- Model selection with context window, pricing, and capabilities
- Automatic configuration saving

### `meept models list`

Lists all configured models.

```bash
# Table format
$ meept models list

# JSON format
$ meept models list --json
```

### `meept models providers`

Lists all available providers from the catalog.

```bash
$ meept models providers
```

Shows for each provider:
- Provider ID and name
- Authentication type
- Transport protocol
- Environment variable for API key
- Supported capabilities

### `meept models add`

Add a new provider/model interactively.

```bash
$ meept models add
```

### `meept models remove <provider/model>`

Remove a configured model.

```bash
$ meept models remove anthropic/claude-sonnet-4-6
```

### `meept models set-default <provider/model>`

Set the default model.

```bash
$ meept models set-default anthropic/claude-sonnet-4-6
```

### `meept models config`

Show or edit the models configuration file.

```bash
# Show config
$ meept models config

# Edit in specific editor
$ meept models config --editor vim
```

### `meept models credentials`

Manage API credentials.

```bash
# List stored credentials
$ meept models credentials list

# Add credential interactively
$ meept models credentials add anthropic

# Remove credential
$ meept models credentials remove anthropic
```

## Configuration

Models are stored in `~/.meept/models.json5`.

Example configuration:

```json5
{
  "model": "anthropic/claude-sonnet-4-6",
  "small_model": "ollama/llama3.2",
  "providers": {
    "anthropic": {
      "api": "anthropic_messages",
      "options": {
        "baseURL": "https://api.anthropic.com",
        "apiKey": "${ANTHROPIC_API_KEY}",
        "timeout": 300
      },
      "models": {
        "claude-sonnet-4-6": {
          "name": "claude-sonnet-4-6",
          "capabilities": ["completion", "code", "reasoning", "tool_use"],
          "input_cost": 3.0,
          "output_cost": 15.0,
          "context_limit": 200000,
          "max_output": 8192,
          "temperature": 0.7
        }
      }
    }
  }
}
```

## Provider Support

Meept supports 11+ providers including:

| Provider | Transport | Auth | Models |
|----------|-----------|------|--------|
| Anthropic | anthropic_messages | API Key | Claude Opus/Sonnet/Haiku |
| OpenAI | openai_chat | API Key | GPT-5.4, GPT-4.1 Mini |
| OpenRouter | openai_chat | API Key | Multi-provider gateway |
| Ollama | openai_chat | None (local) | Llama, Qwen, local models |
| Z.ai | openai_chat | API Key | GLM-4.7, GLM-4.5 Air |
| Google AI | openai_chat | API Key | Gemini 2.5 Pro/Flash |
| DeepSeek | openai_chat | API Key | DeepSeek Chat/Coder |
| xAI | openai_chat | API Key | Grok 3 |
| Groq | openai_chat | API Key | Llama 3.3 70B (fast) |
| Together AI | openai_chat | API Key | Llama 3.3 70B Instruct |
| AWS Bedrock | bedrock_converse | IAM | Bedrock models |

## Model Resolution

When you specify a model reference like `anthropic/claude-sonnet-4-6`:

1. The provider ID (`anthropic`) is looked up in the provider registry
2. The model ID (`claude-sonnet-4-6`) is validated against the provider's models
3. Configuration is loaded from `~/.meept/models.json5`
4. API credentials are resolved from environment variables or credential store

## Credential Storage

Credentials are stored in `~/.meept/credentials.json` with:
- File permissions: 0600 (owner read/write only)
- JSON format with provider ID keys
- Secure storage (no plaintext in config files)

## Examples

### First-time setup

```bash
# Launch interactive setup
$ meept models setup

# Or add a model interactively
$ meept models add

# Set credentials
$ meept models credentials add anthropic
Enter API key for Anthropic (ANTHROPIC_API_KEY): sk-ant-...
Credential saved successfully
```

### Switch models

```bash
# List configured models
$ meept models list

# Set a new default
$ meept models set-default ollama/llama3.2
```

### Use with chat

```bash
# Start chat with default model
$ meept chat

# Use specific model via daemon RPC (when running)
$ meept dev model set anthropic/claude-opus-4-7
```
