# Reference Documentation

This directory contains comprehensive reference documentation for the Meept platform.

## Documentation Structure

### [CLI Reference](cli.md)
Complete command-line interface reference covering all commands, flags, and usage patterns.

- **Commands**: `chat`, `status`, `sessions`, `jobs`, `memory`, `tasks`, `clawskills`, `selfimprove`, `agents`, `tools`, `daemon`, `queue`, `workers`, `version`, `help`
- **Global Flags**: `--socket`, `--state-dir`, `--debug`
- **Examples**: Interactive sessions, scheduled tasks, memory operations
- **Configuration**: File locations and key settings

### [RPC API Reference](rpc.md)
JSON-RPC 2.0 API documentation for programmatic access to the daemon.

- **Protocol Details**: JSON-RPC 2.0 over Unix sockets
- **Methods**: Built-in (`ping`, `status`), Chat (`chat.send`, `chat.stream`), Session management, Job management, Memory operations, Task management
- **Request/Response Examples**: Complete JSON examples for each method
- **Error Handling**: Standard error codes and formats
- **Client Libraries**: Go client example

### [Tool Reference](tools.md)
Comprehensive reference for all built-in tools available to agents.

- **Tool Interface**: Standard tool contract and parameters
- **Baseline Tools**: Memory operations, task management, platform tools
- **Additional Tools**: Filesystem, shell, web, scheduling, knowledge graph, code intelligence
- **Agent Access Matrix**: Which tools are available to each agent type
- **Risk Levels**: Security classification of tools
- **Parameter Schemas**: Complete JSON Schema for each tool

### [Logging Reference](logging.md)
Structured logging system documentation for observability and debugging.

- **Log Levels**: DEBUG, INFO, WARN, ERROR with usage guidelines
- **Configuration**: File-based, environment variables, CLI flags
- **Structured Fields**: Common and component-specific log fields
- **Output Destinations**: Foreground vs background mode behavior
- **Debugging Techniques**: Log analysis, performance monitoring, error tracking
- **Integration**: Monitoring system integration examples

### [Metrics & Observability](metrics.md)
Comprehensive metrics collection and observability features.

- **Metrics Store**: SQLite-backed persistent metric storage
- **Adaptive Timeouts**: Performance-based timeout adjustment
- **Agent Loop Metrics**: Iteration statistics, tool usage, token tracking
- **Memory Statistics**: Effectiveness measurements and patterns
- **Budget Tracking**: LLM cost monitoring and alerts
- **Debugging Commands**: CLI commands for metric analysis
- **Performance Optimization**: Bottleneck identification and solutions

## Key Features Documented

### Multi-Agent Architecture
- Agent roles and capabilities
- Tool access control
- Coworker awareness and delegation
- Model resolution based on capabilities

### Memory System
- Multi-tiered memory architecture
- Episodic, task, knowledge graph, distributed, and semantic memory
- Hybrid search combining keyword and vector similarity
- Memory consolidation and context injection

### Security Layers
- Input sanitization with pattern detection
- Security engine with permission checks
- Tirith shell command scanning
- Taint tracking for data provenance

### LLM Integration
- Multi-provider support (OpenAI, Anthropic, Google, Ollama)
- Capability-based model selection
- Token budgeting and rate limiting
- Native Anthropic driver with extended thinking

### Code Intelligence
- Tree-sitter AST parsing for multiple languages
- LSP integration for advanced code analysis
- Symbol extraction and query capabilities
- Multi-language support

## Usage Patterns

### Development Workflow
1. Start daemon: `meept daemon start --daemon`
2. Check status: `meept status`
3. Interactive session: `meept chat`
4. Monitor metrics: `meept metrics status`

### Production Deployment
1. Configure logging and metrics
2. Set up alerting rules
3. Monitor agent performance
4. Track budget utilization

### Integration Scenarios
1. RPC API for custom clients
2. Tool development for extended capabilities
3. Monitoring integration with Prometheus/Grafana
4. Custom agent configurations

## Related Documentation

- [Features Overview](../features.md) - High-level feature descriptions
- [Architecture Diagrams](../diagram.md) - System architecture visualizations
- [CLAUDE.md](../../CLAUDE.md) - Development guidelines and architecture
- [README.md](../../README.md) - Installation and quick start guide

## Contributing

When updating reference documentation:

1. Ensure accuracy by checking source code
2. Maintain consistent formatting and structure
3. Include practical examples and use cases
4. Update all related documentation when making changes
5. Follow the established documentation patterns