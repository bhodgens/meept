# Plans Configuration

Plans are implementation-tracking documents the system writes as it
decomposes and executes work. They are project tracking data: by default
they live inside the project directory and travel with it.

## Where plans are stored

`plans.storage` in `~/.meept/meept.json5` controls plan file locations:

```json5
{
  "plans": {
    "storage": {
      // Default plan directory, RELATIVE to each project's root.
      // Default: "docs/plans" — plans travel with the project.
      "default_path": "docs/plans",
      // ABSOLUTE override: store ALL plans in one place outside any
      // project (e.g. "~/.meept/plans"). Takes precedence over
      // default_path when non-empty.
      "external_path": "",
      // Filename template: {{slug}}, {{date}}, {{id}}
      "filename_template": "{{slug}}.md"
    }
  }
}
```

Resolution order in `internal/plan/manager.go` (`resolvePlanDir`):

1. `external_path` (env-expanded, absolute) — when non-empty, every plan
   lands there regardless of project.
2. `default_path` joined with the project's root — the default keeps
   plans versioned with the code they track.

The shipped template (`config/meept.json5`) defaults to `docs/plans`
inside the project. This is intentional: plan documents are the audit
trail of what was planned, executed, and approved for that codebase.

## Evolver plans are separate

Machine-originated evolver plans are deliberately NOT project-scoped.
They write to the user-scoped sink `~/.meept/plans/evolver`
(`agents.evolver.plan_dir` in the config), because they can originate
from an arbitrary CWD and must never pollute a repo's `docs/plans`.
See `internal/config/schema.go` (`DefaultEvolverPlanDir`).

## Plan documents change during execution

The daemon updates a plan's frontmatter (`status`, phase state) as the
plan moves through planning → execution → approval. If a project is a
git repository, these updates appear as working-tree modifications to
`docs/plans/*.md` — that is the tracking data doing its job, not noise.
Commit them alongside the work they describe.
