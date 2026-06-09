// internal/configui/sections_web.go
package configui


func buildWebFields() []Field {
	cfg := loadMainConfigOrFallback()
	s := &cfg.Web
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewTextField("host", "host", s.Host),
		NewNumberField("port", "port", s.Port),
		NewMaskedField("secret_key", "secret key", s.SecretKey),
	}
}
