// Package telnyx is the Telnyx messaging (SMS) client (contact-channels
// plan, contract C5, leaf 12): the send client (messaging.go), the
// webhook payload parser, the telnyx_sms contact.ChannelAttempt adapter
// with its bounded DLR pending map (sms_attempt.go), and the named
// sentinels (ErrInvalidE164, ErrMessagingDisabled, ErrUnknownMessage).
//
// The webhook receiver half lives in internal/comm/http
// (MessagingRoutes) and reuses the existing Ed25519 telnyx-signature
// verifier from internal/voice/telnyx (VerifyWebhookSignature) through
// its injected Verify seam - never a second one.
//
// The API key is never logged and never appears in error text.
package telnyx
