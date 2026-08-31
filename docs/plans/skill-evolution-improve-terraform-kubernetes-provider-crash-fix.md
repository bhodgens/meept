# Plan: Skill evolution: improve terraform-kubernetes-provider-crash-fix

## Meta

- plan_id: plan-20260831232057-0011
- created: 2026-08-31
- status: planning

## Summary

Effectiveness is 0.20 (9 positive, 25 negative). The skill likely fails because it provides overly generic fix patterns without emphasizing diagnosis-first approach, root cause identification, or version-specific provider constraints that commonly cause crashes.

Candidate content:
---
name: terraform-kubernetes-provider-crash-fix
description: |
  Diagnose and fix Terraform Kubernetes provider crashes (panic, segfault, or provider process termination).
  These are often caused by provider version incompatibilities, malformed resource configs, or known bugs in specific provider versions.
metadata:
  author: Sapiens AI
  created: 2026-01-01
  version: 2.0
  tags:
    - terraform
    - kubernetes
    - crash
    - panic
    - provider
---

# Terraform Kubernetes Provider Crash Fix

## When to Use

Use this skill when Terraform operations on Kubernetes resources crash with:
- Provider panics (runtime errors from the provider plugin)
- Segmentation faults in the provider process
- Provider process termination during plan/apply
- "plugin did not respond" or similar provider disconnect errors
- Stack traces referencing `hashicorp/terraform-provider-kubernetes`

## Prerequisites

Before applying any fix, confirm you are working with Terraform and the Kubernetes provider.

## Diagnostic Steps

### Step 1: Capture the Crash Details

Collect the full error output including stack trace. Run with verbose logging:

```
TF_LOG=DEBUG terraform plan -var-file="${VARIABLES_FILE:-vars.tfvars}" -no-color 2>&1 | tee terraform-crash.log
```

Identify the specific resource and operation that triggers the crash.

### Step 2: Check Provider Version

```
terraform version | grep -A2 "kubernetes"
```

Known problematic versions that cause crashes include:
- `<2.24.0`: Multiple panic fixes not yet included
- `2.26.0` - `2.27.x`: Several stability regressions fixed in patch releases
- `>=2.30.0` with old Kubernetes clusters: Compatibility issues with clusters < v1.28

### Step 3: Check Cluster Compatibility

```
kubectl version --short
```

The Kubernetes provider has strict API version compatibility with the target cluster. Mismatches between provider version and cluster version commonly cause crashes.

### Step 4: Identify the Crashing Resource

Isolate which resource type and operation causes the panic. Try:

```
terraform state list | grep "kubernetes_"
```

Then run targeted `terraform plan -target=<resource>` to isolate the failure.

## Fix Patterns

### Pattern A: Provider Version Upgrade/Downgrade

Upgrade to a known-stable version or downgrade from a buggy one:

```hcl
terraform {
  required_providers {
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = ">= 2.31.0"  # Pin to stable version
    }
  }
}
```

Then run:

```
terraform init -upgrade
```

### Pattern B: Fix Malformed Resource Configuration

Common crash triggers in resource config:
- `api_version` set to an deprecated GVK (e.g., `extensions/v1beta1` in cluster >= 1.16)
- Nested blocks using incorrect structure for the resource type
- Missing required fields that cause nil pointer dereferences

Review the crashing resource against the provider documentation for the correct schema.

### Pattern C: State Drift from Manual Cluster Changes

If resources were modified outside Terraform, the provider may crash on subsequent reads:

```
terraform refresh -target=<crashing_resource>
terraform plan -target=<crashing_resource>
```

If refresh crashes, import may also fail. Consider removing the resource from state and re-creating:

```
terraform state rm <resource>
terraform apply -target=<resource>
```

### Pattern D: Disable Field-Level Caching for Problematic Resources

Some provider versions crash when caching certain computed fields. Add:

```hcl
provider "kubernetes"
{
  disable_fqdn_lookup = true
  load_config_file    = true
}
```

### Pattern E: Known Bug Workarounds

| Crash Symptom | Known Fix |
|---|---|
| Panic on `kubernetes_pod` with init containers | Upgrade to provider >= 2.28.0 |
| Panic on `kubernetes_service` with external IPs | Set `external_ip` to null or upgrade |
| Panic during `terraform destroy` on configmap | Upgrade to provider >= 2.25.1 |
| Nil pointer on `kubernetes_manifest` with CRDs | Upgrade to provider >= 2.30.0 |
| Crash on ingress with TLS secret reference | Ensure secret exists before apply |

## Execution

1. Run diagnostics (Steps 1–4 above)
2. Identify the applicable fix pattern
3. Apply the fix configuration changes
4. Run `terraform init` to fetch updated provider
5. Validate with `terraform plan` before applying
6. Run `terraform apply` if plan succeeds

## Post-Fix Verification

```
terraform plan -no-color
terraform apply -auto-approve
kubectl get pods -A | head -20
```

Confirm no provider crashes and all Kubernetes resources are in expected state.

## Failure Modes to Report

If none of the above patterns resolve the crash:
- Open issue at https://github.com/hashicorp/terraform-provider-kubernetes/issues
- Include: full debug log, provider version, Kubernetes cluster version, and minimal reproducible config
- Do not attempt to modify provider source code or use `TERRAFORM_PLUGIN_SKIP_VERSION_CHECK` in production


## Notes

