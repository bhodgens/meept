# Platform Configuration

The platform configuration controls the core behavior of the Meept platform process.

## Configuration File

Platform settings are configured in `~/.meept/meept.toml` under the `[daemon]` section:

```toml
[daemon]
socket_path = "~/.meept/meept.sock"
pid_file = "~/.meept/meept.pid"
log_level = "INFO"
log_max_bytes = 268435456
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

### log_max_bytes
- **Type**: integer (bytes)
- **Default**: `268435456` (256 MiB); any value of 0 or less selects this default
- **Description**: Caps `~/.meept/meept.log`. When the file reaches this size the
  writer rotates it: the current file becomes `meept.log.1`, the previous `.1`
  becomes `meept.log.2`, and the oldest generation is discarded. The worst-case
  on-disk cost is therefore 3x this value regardless of how much is logged.

  The cap is applied inside the daemon process, not by the process that spawns
  it. A spawner holds the log file open for the daemon's whole life, so
  rotating the file from outside would leave the daemon appending to an unlinked
  file that no longer consumes the cap.

  Without this setting a runaway loop can fill the disk. On 2026-10-08 a task
  creation loop drove the log to 145 GB and filled a 927 GB disk to 95%.

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