// Package constants holds shared defaults across the Meept project.
package constants

// DefaultDevAPIKey is the default development API key used when no API keys
// are configured. Both the daemon (server) and the CLI (client) use this value
// so that HTTP transport works out of the box for local development.
//
// In production, always replace this with a generated key via:
//
//	meept token generate --save
const DefaultDevAPIKey = "meept_dev_default_key_CHANGE_ME"
