# daemon

Configuration for the daemon process including socket, logging, and data directory.


## Example

```toml
[daemon] socket_path = "~/.meept/meept.sock"
```


## Fields

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| SocketPath | string |  |  |
| PIDFile | string |  |  |
| LogLevel | string |  |  |
| DataDir | string |  |  |

