// internal/configui/sections_daemon.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildDaemonFields() []Field {
	cfg, _ := config.LoadDefault()
	d := &cfg.Daemon
	return []Field{
		NewSelectField("log_level", "log level", d.LogLevel, []string{"DEBUG", "INFO", "WARN", "ERROR"}),
		NewTextField("data_dir", "data dir", d.DataDir),
		NewTextField("socket_path", "socket path", d.SocketPath),
		NewTextField("pid_file", "pid file", d.PIDFile),
	}
}
