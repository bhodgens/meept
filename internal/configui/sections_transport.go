// internal/configui/sections_transport.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildTransportFields() []Field {
	cfg, _ := config.LoadDefault()
	t := &cfg.Transport
	r := &t.RPC
	h := &t.HTTP
	return []Field{
		NewToggleField("transport.rpc.enabled", "rpc enabled", r.Enabled),
		NewTextField("transport.rpc.socket_path", "rpc socket path", r.SocketPath),
		NewToggleField("transport.http.enabled", "http enabled", h.Enabled),
		NewTextField("transport.http.addr", "http addr", h.Addr),
		NewTextField("transport.http.tls_cert_file", "tls cert file", h.TLSCertFile),
		NewTextField("transport.http.tls_key_file", "tls key file", h.TLSKeyFile),
	}
}
