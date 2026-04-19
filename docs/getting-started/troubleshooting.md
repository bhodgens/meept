# Troubleshooting

Common issues and their solutions.

## Daemon Won't Start

### "models.json5 not found or invalid"

Meept requires a valid `models.json5` to start. Copy the template and add your provider:

```bash
cp config/models.json5 ~/.meept/models.json5
# Edit to add your API key or Ollama endpoint
```

### "address already in use"

A daemon is already running. Kill it first:

```bash
# Find and kill the process
pkill meept-daemon

# Or remove the stale socket
rm ~/.meept/meept.sock
```

### "permission denied" on socket

```bash
# Remove stale socket and restart
rm -f ~/.meept/meept.sock
./bin/meept-daemon -f
```

## CLI Can't Connect

### "connection refused"

The daemon isn't running. Start it first:

```bash
./bin/meept-daemon -f
```

### "socket not found"

Check the socket path in your config:

```bash
# Default location
ls -la ~/.meept/meept.sock
```

## LLM Provider Issues

### "timeout" errors

- Check your network connection
- Verify the API endpoint URL in `models.json5`
- For Ollama, ensure it's running: `ollama list`
- Increase timeout: set `"default_timeout": 300` in `models.json5`

### "401 unauthorized"

- Verify your API key is set correctly
- Check that the environment variable is exported: `echo $OPENROUTER_API_KEY`
- For env var references in JSON5, use `"${VAR_NAME}"` syntax

### "model not found"

- Verify the model name matches exactly what the provider expects
- For Ollama, pull the model first: `ollama pull llama3.2`

## TUI Issues

### Blank screen or rendering issues

- Try a different terminal emulator
- Ensure your terminal supports 256 colors
- Run with `TERM=xterm-256color ./bin/meept chat`

### Slow response

- Check daemon logs for slow LLM calls
- Try a faster model in `models.json5`
- Reduce `max_context_items` in `[memory.episodic]`

## Memory Issues

### "database locked"

Only one process can access the SQLite database at a time. Ensure only one daemon is running.

### Memory not persisting

```bash
# Check memory database exists
ls -la ~/.meept/memory/

# Verify memory config
grep -A5 '\[memory\]' ~/.meept/meept.toml
```

## Build Issues

### "go: module ... not found"

```bash
go mod download
go mod tidy
```

### Build fails on macOS ARM

```bash
# Ensure Xcode command line tools are installed
xcode-select --install

# Then rebuild
make build
```

## Getting More Help

1. Run the daemon in debug mode: `./bin/meept-daemon -f --log-level debug`
2. Check existing issues on [GitHub](https://github.com/caimlas/meept/issues)
3. Enable audit logging in `[security]` to trace permission decisions
