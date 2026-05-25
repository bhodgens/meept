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
		NewToggleField("transport.http.require_auth", "require auth", h.RequireAuth),
		NewDrilldownField("transport.http.api_keys", "api keys", buildStringSliceItems("key", h.APIKeys)),
		NewToggleField("transport.http.use_tls", "use tls", h.UseTLS),
		NewToggleField("transport.http.auto_tls_cert", "auto tls cert", h.AutoTLSCert),
		NewToggleField("transport.http.rest", "rest", h.REST),
		NewToggleField("transport.http.websocket", "websocket", h.WebSocket),
		NewTextField("transport.http.ws_path", "ws path", h.WSPath),
		NewToggleField("transport.http.mcp", "mcp", h.MCP),
		NewTextField("transport.http.mcp_path", "mcp path", h.MCPPath),
	}
}
