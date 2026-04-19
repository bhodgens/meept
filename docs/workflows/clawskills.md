# ClawSkills Marketplace

## Overview
ClawSkills provides a registry-based marketplace for third-party skills with security scanning, risk assessment, and automatic updates. Users can discover, install, and manage external skills safely.

## Problem
Limited built-in skills restrict functionality. ClawSkills enables:
- Third-party skill discovery and installation
- Security vetting before execution
- Risk-based access control
- Community skill sharing

## Behavior

### Registry Integration
- **Central Registry**: Skill discovery and metadata
- **Security Scanning**: Pre-installation vulnerability checks
- **Risk Assessment**: Assigns risk levels to skills
- **Automatic Updates**: Keeps skills current

### Installation Process
1. **Discovery**: Browse registry for available skills
2. **Security Scan**: Automated vulnerability assessment
3. **Risk Evaluation**: Assign risk level (low, medium, high, critical)
4. **Installation**: Download and configure skill
5. **Verification**: Validate installation success

### Risk Levels
- **Low**: Well-vetted, minimal permissions
- **Medium**: Standard community skills
- **High**: Powerful capabilities, careful review
- **Critical**: Maximum permissions, explicit approval

### CLI Commands
- `clawskills search "query"`: Search registry
- `clawskills install <slug>`: Install skill
- `clawskills list`: List installed skills
- `clawskills update`: Update all skills
- `clawskills uninstall`: Remove skill

## Configuration

```toml
[clawskills]
enabled = false
registry_url = "https://clawhub.ai"
install_dir = "~/.meept/clawskills"
auto_update = false
max_installed = 50
default_risk_level = "high"

[clawskills.security]
scan_before_install = true
require_approval_critical = true
block_unsigned = false

[clawskills.updates]
check_interval_hours = 24
auto_install_minor = true
notify_major_updates = true
```

## Observability

### Logging
- Skill installation events
- Security scan results
- Risk assessment decisions
- Update operations

### Metrics
- Installation success rate
- Security scan pass rate
- Risk level distribution
- Update adoption rates

### Debug Info
- Installed skill versions
- Security scan status
- Registry connectivity
- Update availability

## Edge Cases

### Security Scan Failure
- Installation blocked
- Manual review required
- Clear failure explanation

### Risk Level Mismatch
- User confirmation required for high-risk skills
- Alternative lower-risk options suggested
- Risk justification documented

### Registry Unavailable
- Local cache used temporarily
- Installation deferred
- User notified of connectivity issue

### Skill Compatibility Issue
- Version conflict detection
- Dependency resolution
- Rollback capability