# Platform Configuration

The platform configuration controls the core behavior of the Meept platform process.

## Configuration File

Platform settings are configured in `~/.meept/meept.toml` under the `[daemon]` section:

```toml
[daemon]
socket_path = "~/.meept/meept.sock"
pid_file = "~/.meept/meept.pid"
log_level = "INFO"
data_dir = "~/.meept"
```

## Configuration Options

### socket_path
- **Type**: string
- **Default**: `~/.meept/meept.sock`
- **Description**: Path to the Unix domain socket used for CLI-platform communication

### pid_file
- **Type**: string
- **Default**: `~/.meept/meept.pid`
- **Description**: Path where the platform process ID file is stored

### log_level
- **Type**: string
- **Default**: `INFO`
- **Valid values**: `DEBUG`, `INFO`, `WARN`, `ERROR`
- **Description**: Controls the verbosity of platform logging

### data_dir
- **Type**: string
- **Default**: `~/.meept`
- **Description**: Base directory for all platform data files

## Log Levels

Meept uses structured logging with the following levels:

- **DEBUG**: Detailed debugging information including internal state and operations
- **INFO**: General operational information about what the platform is doing
- **WARN**: Warning messages about potential issues or unexpected conditions
- **ERROR**: Error messages indicating failures that may affect functionality

## Example Configuration

```toml
[daemon]
socket_path = "/tmp/meept.sock"
pid_file = "/var/run/meept.pid"
log_level = "WARN"
data_dir = "~/.meept"
```

## Related Files

- `~/.meept/meept.log` - Platform log file
- `~/.meept/meept.sock` - Communication socket
- `~/.meept/meept.pid` - Process ID file

## Notes

- The platform must be restarted for configuration changes to take effect
- Socket files are automatically created and managed by the platform
- Log files rotate automatically based on size