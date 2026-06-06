# config

Root configuration structure containing all subsystem configurations.


## Example

```toml
[config] daemon.socket_path = "~/.meept/meept.sock"
```


## Fields

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| Daemon | DaemonConfig |  |  |
| LLM | LLMConfig |  |  |
| Memory | MemoryConfig |  |  |
| Memvid | MemvidConfig |  |  |
| MultiAgent | MultiAgentConfig |  |  |
| Agents | AgentsConfig |  |  |
| Agent | AgentConfig |  |  |
| Security | SecurityConfig |  |  |
| Scheduler | SchedulerConfig |  |  |
| Queue | QueueConfig |  |  |
| Workers | WorkersConfig |  |  |
| Isolation | IsolationConfig |  |  |
| Telegram | TelegramConfig |  |  |
| Web | WebConfig |  |  |
| MCP | MCPConfig |  |  |
| Plugins | PluginsConfig |  |  |
| Workspace | WorkspaceConfig |  |  |
| Skills | SkillsConfig |  |  |
| SelfImprove | SelfImproveConfig |  |  |
| Orchestrator | OrchestratorConfig |  |  |
| Shadow | ShadowConfig |  |  |
| DistributedMemory | DistributedMemoryConfig |  |  |
| CodeIntel | CodeIntelConfig |  |  |

