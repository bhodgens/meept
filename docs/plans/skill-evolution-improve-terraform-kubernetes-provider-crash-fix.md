# Plan: Skill evolution: improve terraform-kubernetes-provider-crash-fix

## Meta

- plan_id: plan-20260831224758-0043
- created: 2026-08-31
- status: planning

## Summary

Effectiveness is only 0.20 with 24 negative vs 9 positive feedback. The skill likely lacks specific version pinning guidance, common crash patterns, and step-by-step remediation paths, causing agents to give vague or incorrect advice.

Candidate content:
# terraform-kubernetes-provider-crash-fix

## Purpose
Diagnose and resolve crashes/hangs in Terraform operations involving the Kubernetes provider (hashicorp/kubernetes).

## Common Crash Patterns

### Pattern 1: `panic: interface conversion` during apply
- **Cause**: Provider version mismatch between Terraform CLI and Kubernetes API server version
- **Fix**: Pin provider to exact version compatible with your K8s cluster (see Version Matrix below)

### Pattern 2: `context deadline exceeded` / hangs on state refresh
- **Cause**: Large number of resources or slow API server responses
- **Fix**: Increase timeout, use `-target`, or enable parallelism limits

### Pattern 3: `resource not found` after successful create
- **Cause**: Provider retries with stale connection or RBAC misconfiguration
- **Fix**: Check service account permissions; retry with fresh provider config

### Pattern 4: EOF / connection reset during plan
- **Cause**: Network instability or max connections reached on kubeconfig
- **Fix**: Set `poll_interval` and connection timeouts explicitly

## Version Compatibility Matrix

| Terraform CLI | kubernetes Provider | Notes |
|--------------|-------------------|-------|
| 1.5+ | >= 2.25 | Required for K8s 1.28+ |
| 1.6+ | >= 2.27 | Required for K8s 1.29+ |
| 1.7+ | >= 2.30 | Required for K8s 1.30+ |
| < 1.5 | <= 2.20 | Legacy — upgrade recommended |

**Rule**: Always use the latest patch version in your minor line. Pin in `required_providers`.

## Step-by-Step Remediation

1. **Identify the crash** — Capture the full error/output. Note:
   - Terraform version (`terraform version`)
   - Provider version (`grep kubernetes .terraform.lock.hcl`)
   - Kubernetes server version (`kubectl version`)

2. **Check for known issues** — Search [GitHub issues](https://github.com/hashicorp/terraform-provider-kubernetes/issues) for the exact error string.

3. **Pin provider version** — Add explicit version constraint:
   ```hcl
   terraform {
     required_providers {
       kubernetes = {
         source  = "hashicorp/kubernetes"
         version = "~> 2.30"  # adjust for your Terraform/K8s version
       }
     }
   }
   ```

4. **Upgrade/downgrade provider** — Run:
   ```bash
   terraform providers lock -platform=windows_amd64 -platform=darwin_amd64 -platform=linux_amd64
   terraform init -upgrade
   ```

5. **Set timeouts for large manifests** — Add to provider block:
   ```hcl
   provider "kubernetes" {
     config_path = "~/.kube/config"
     apply_retry_max      = 5
     apply_retry_sleep_min = 10
     apply_retry_sleep_max = 30
   }
   ```

6. **Work around resource-specific crashes** — Use `-target` to isolate crashing resource, fix configuration, then re-apply.

7. **Migrate to `kubernetes_manifest` if needed** — For custom resources (CRDs) causing crashes, prefer:
   ```hcl
   resource "kubernetes_manifest" "my_resource" {
     manifest = jsondecode(file("my-resource.yaml"))
   }
   ```

## When to Skip
Do not use this skill when:
- The crash is clearly a user configuration error (invalid YAML, missing fields)
- The issue is a network/connectivity problem unrelated to the provider
- The Terraform version itself is unsupported (< 1.1)

## Output Format
Always return:
1. Root cause diagnosis
2. Specific remediation steps with code snippets
3. Version constraints to apply
4. Fallback option if the fix fails

## Notes

