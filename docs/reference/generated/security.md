# security

Security configuration including input sanitization, path restrictions, output monitoring, shell command scanning, and audit logging.


## Example

```toml
[security] sanitize_inputs = true
```


## Fields

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| SanitizeInputs | bool |  |  |
| SanitizeStrictness | string |  |  |
| LLMFilterExternal | bool |  |  |
| RequireConfirmationHigh | bool |  |  |
| RequireConfirmationCritical | bool |  |  |
| BlockFinancial | bool |  |  |
| AllowedPaths | []string |  |  |
| BlockedPaths | []string |  |  |
| MonitorOutput | bool | Output monitoring |  |
| RedactOutput | bool |  |  |
| ScanShellCommands | bool | Shell command security |  |
| TirithBinary | string |  |  |
| EnableAuditLog | bool | Audit logging |  |
| AuditDBPath | string |  |  |

