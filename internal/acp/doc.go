// Package acp implements Agent Client Protocol (ACP) client-side types
// and newline-delimited JSON-RPC 2.0 stdio transport for meept.
//
// Protocol version is JSON integer 1 (not a string). Framing is
// newline-delimited JSON-RPC 2.0 over stdio — messages must not contain
// embedded newlines and are not LSP Content-Length framed.
//
// Verified 2026-08-29 from
// https://agentclientprotocol.com/protocol/v1/initialization.md and
// https://agentclientprotocol.com/protocol/transports.
package acp
