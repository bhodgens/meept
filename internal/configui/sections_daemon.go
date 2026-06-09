// internal/configui/sections_daemon.go
package configui


func buildDaemonFields() []Field {
	cfg := loadMainConfigOrFallback()
	d := &cfg.Daemon
	return []Field{
		NewSelectField("log_level", "log level", d.LogLevel, []string{"DEBUG", "INFO", "WARN", "ERROR"}),
		NewTextField("data_dir", "data dir", d.DataDir),
		NewTextField("socket_path", "socket path", d.SocketPath),
		NewTextField("pid_file", "pid file", d.PIDFile),
	}
}
