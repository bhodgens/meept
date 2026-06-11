# Configuration

Meept uses a flexible configuration system with TOML and JSON5 files to control all aspects of the platform.

## Configuration Files

Meept uses two main configuration files:

| File | Format | Purpose | Location |
|------|--------|---------|----------|
| `meept.toml` | TOML | Daemon settings, features, security | `~/.meept/meept.toml` |
| `models.json5` | JSON5 | LLM providers, models, capabilities | `~/.meept/models.json5` |

## Quick Start

### 1. Create Configuration Directory

```bash
mkdir -p ~/.meept
```

### 2. Choose a Configuration Template

- **[Minimal](examples/minimal.md)** - Local Ollama only, basic features
- **[Production](examples/production.md)** - Multiple providers, enhanced security
- **[Advanced](examples/advanced.md)** - Full feature set, enterprise-ready

### 3. Copy Configuration Files

Copy your chosen template to:
- `~/.meept/meept.toml`
- `~/.meept/models.json5`

### 4. Set Environment Variables

```bash
export OPENROUTER_API_KEY="your-key"  # If using external providers
export MEEPT_WEB_SECRET="your-secret" # If enabling web interface
```

### 5. Start the Daemon

```bash
go build -o bin/meept-daemon ./cmd/meept-daemon
./bin/meept-daemon -f
```

## Configuration Sections

### Core Configuration

- **[Daemon](daemon.md)** - Basic daemon settings and logging
- **[LLM](llm.md)** - Model providers, capabilities, and budget management
- **[Agents](agents.md)** - Multi-agent system configuration

### Feature Configuration

- **Memory** - Episodic, task, and personality memory settings
- **Security** - Input sanitization, command scanning, and permissions
- **Skills** - Local skill discovery and management
- **Self-Improve** - Automated code improvement system
- **Plans** - Plan lifecycle, approval workflow, and task synthesis
- **Scheduler** - Background job scheduling
- **Workspace** - Git-backed task tracking
- **Speech-to-Text (STT)** - Client-side voice transcription for TUI and Flutter
  - `stt.enabled` - Enable/disable speech-to-text (default: `false`)
  - `stt.engine` - Transcription engine: `"whisper"`, `"parakeet"`, or `"native"` (default: `"whisper"`)
  - `stt.language` - Language code for transcription (default: `"en"`)
  - `stt.auto_send` - Send transcription immediately without review (default: `false`)
  - `stt.whisper.bin_path` - Path to whisper-cli binary (default: `"whisper-cli"`)
  - `stt.whisper.model_path` - Path to whisper model file (default: `~/.meept/models/ggml-base.en.bin`)
  - `stt.whisper.threads` - Number of threads for whisper (default: `4`)
  - `stt.parakeet.bin_path` - Path to parakeet CLI binary
  - `stt.parakeet.model_path` - Path to parakeet model file
  - `stt.recording.recorder_bin` - Audio recorder: `"ffmpeg"` or `"sox"` (default: `"ffmpeg"`)
  - `stt.recording.sample_rate` - Sample rate in Hz (default: `16000`)
  - `stt.recording.channels` - Audio channels (default: `1`)
  - `stt.recording.format` - Audio format: `"wav"`, `"flac"`, or `"ogg"` (default: `"wav"`)
  - See [Speech-to-Text](../workflows/speech-to-text.md) for full feature documentation

### Analytics

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `analytics.enabled` | bool | false | Enable analytics collection |
| `analytics.retention_days` | int | 90 | Days to retain analytics data |

### Notifications

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `notifications.enabled` | bool | false | Enable desktop notifications |
| `notifications.retention` | int | 30 | Days to retain notification history |

### Integration Configuration

- **Telegram** - Telegram bot interface
- **Web** - HTTP API server
- **MCP** - Model Context Protocol servers
- **Plugins** - External plugin system

## Configuration Hierarchy

Meept uses a priority-based configuration system:

1. **Command-line flags** (highest priority)
2. **Environment variables**
3. **User configuration** (`~/.meept/`)
4. **System defaults** (built into the binary)

## Configuration Validation

The daemon validates configuration on startup:

- **Syntax checking** for TOML and JSON5
- **Semantic validation** of field values
- **Dependency checking** for enabled features
- **Path verification** for directories and files

## Dynamic Configuration

Some settings can be reloaded without restarting the daemon:

- **LLM budget limits**
- **Skill configurations**
- **Memory settings**
- **Agent definitions**

## Best Practices

### Security

- Use environment variables for sensitive data
- Restrict file permissions on configuration directory
- Enable audit logging for production deployments
- Regularly review security settings

### Performance

- Set appropriate budget limits for your use case
- Enable caching for frequently accessed data
- Configure memory limits based on available resources
- Monitor token usage and adjust limits accordingly

### Maintenance

- Keep configuration files in version control
- Document configuration changes
- Regularly backup memory and configuration data
- Test configuration changes in a staging environment

## Troubleshooting

### Common Issues

- **Configuration syntax errors** - Check TOML/JSON5 syntax
- **Missing environment variables** - Verify all required variables are set
- **Permission errors** - Ensure daemon user can access configuration files
- **Feature dependencies** - Some features require others to be enabled

### Debug Mode

Enable debug logging to diagnose configuration issues:

```toml
[daemon]
log_level = "DEBUG"
```

### Configuration Testing

Test your configuration with:

```bash
# Check configuration syntax
./bin/meept-daemon --check-config

# Test basic functionality
./bin/meept status
```

## Reference

- [Configuration Template](../config/meept.toml) - Full configuration template
- [Models Template](../config/models.json5) - LLM models template
- [API Documentation](../api/) - Programmatic configuration options
