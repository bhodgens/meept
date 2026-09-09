# Silver-Label Adjudication Sheet — 48 Hermes-transcript cases

For each case: **keep** the silver label, or write the correct intent.
Intents: code debug analyze search chat platform git schedule plan review report recall (or `abstain` = intentionally unmatchable).
`cascade` column = what the current 3-stage system would do (C = falls to LLM chain).
Disagreement rows are sorted first — they matter most.

## Disagreements (35) — cascade answer differs from silver

### 1. silver=`review` cascade=`C(chain)` (A=- B=- conf=0.105)
> review the json files to make sure they're complete
**your label:** ``

### 2. silver=`analyze` cascade=`C(chain)` (A=- B=- conf=0.107)
> you need to re-analyze the images; they all have the readings in them (for any with a value range) in a bubble above the chart - please perform this analysis and update the json files, and report the discrepencies you found.
**your label:** ``

### 3. silver=`analyze` cascade=`C(chain)` (A=- B=- conf=0.106)
> you need to re-analyze the images; they all have the readings in them (for any with a value range) in a bubble above the chart - please perform this analysis and update the json files, and report the discrepencies you found. Use 1 subagent per image.
**your label:** ``

### 5. silver=`review` cascade=`C(chain)` (A=- B=- conf=0.106)
> using subagents, review the meept client for bugs, and correct them as you find them.
**your label:** ``

### 7. silver=`review` cascade=`C(chain)` (A=- B=- conf=0.14)
> using subagents, review the meept_ui flutter client for bugs, and correct them as you find them.
**your label:** ``

### 8. silver=`review` cascade=`C(chain)` (A=- B=- conf=0.119)
> using subagents, review the meept_ui flutter client for bugs, and correct them as you find them. Do not make assumptions based on prior context, read the actual files.
**your label:** ``

### 9. silver=`review` cascade=`C(chain)` (A=- B=- conf=0.118)
> do another run - using subagents, review the meept_ui flutter client for bugs, and correct them as you find them. Do not make assumptions based on prior context, read the actual files.
**your label:** ``

### 10. silver=`review` cascade=`C(chain)` (A=- B=- conf=0.108)
> using subagents, review the meept-daemon  for bugs, and correct them as you find them. Do not make assumptions based on prior context, read the actual files. Be thorough and complete.
**your label:** ``

### 11. silver=`git` cascade=`C(chain)` (A=- B=- conf=0.108)
> commit, then using subagents, review the meept-daemon  for bugs, and correct them as you find them. Do not make assumptions based on prior context, read the actual files. Be thorough and complete.
**your label:** ``

### 12. silver=`review` cascade=`C(chain)` (A=- B=- conf=0.106)
> using subagents, review the meept cli client for bugs, and correct them as you find them. Do not make assumptions based on prior context, read the actual files.
**your label:** ``

### 13. silver=`analyze` cascade=`C(chain)` (A=- B=- conf=0.108)
> investigate how #1 and #2 can be solved, and give me more detail what the actual contention is.
**your label:** ``

### 14. silver=`plan` cascade=`code` (A=- B=code conf=0.148)
> create a plan file for this under docs/
**your label:** ``

### 15. silver=`code` cascade=`C(chain)` (A=- B=- conf=0.139)
> implement the plan using subagents
**your label:** ``

### 21. silver=`review` cascade=`C(chain)` (A=- B=- conf=0.106)
> Review docs/plans/2026-05-27-project-context.md for completion and correctness and give me a chart with the completion of each feature as percentages.
**your label:** ``

### 22. silver=`review` cascade=`C(chain)` (A=- B=- conf=0.102)
> Review docs/plans/2026-05-27-project-context.md for completion and correctness and give me a chart with the completion of each feature as percentages. Do not assume correctness of any stated progress, review each file.
**your label:** ``

### 23. silver=`review` cascade=`C(chain)` (A=- B=- conf=0.102)
> do another review, i want to make sure we've got everything in entirety.

[Your active task list was preserved across context compression]
- [>] 7. Fix missing bullets/positions in resume-master.json (in_progress)
**your label:** ``

### 24. silver=`analyze` cascade=`C(chain)` (A=- B=- conf=0.129)
> how does the Inte Arc Pro B70 32GB compare against the 3090 or 4060?
**your label:** ``

### 26. silver=`search` cascade=`C(chain)` (A=- B=- conf=0.127)
> search and make sure it works if you haven't yet
**your label:** ``

### 27. silver=`review` cascade=`C(chain)` (A=- B=- conf=0.108)
> Review that original-data/csv data and help me organize it to prepare for import into odoo. The project proposal is ./bran
**your label:** ``

### 29. silver=`code` cascade=`C(chain)` (A=- B=- conf=0.134)
> let's add another step for local testing in there between 1 + 2 - use the local odoo install to test and review the work.
**your label:** ``

### 31. silver=`git` cascade=`code` (A=- B=code conf=0.162)
> commit, omit the .env but include an env.sample with commented out instructions in the file
**your label:** ``

### 32. silver=`review` cascade=`C(chain)` (A=- B=- conf=0.135)
> "Regularly provided architectural review and recommendations to customer executives and technical staff on ZFS, OpenZFS, NFS performance optimization, and cloud storage architecture at hyperscale." add Lustre to this
**your label:** ``

### 33. silver=`code` cascade=`platform` (A=platform B=- conf=0.104)
> create a skill-tailored resume for this posting (1-page) based on available data and save it under 2026/
**your label:** ``

### 34. silver=`review` cascade=`C(chain)` (A=- B=- conf=0.108)
> I want you to review ~/git/strategist-usa with the purpose of helping me add additional bulletpoints for my resume. Assume I am the primary if not exclusive developer of the
**your label:** ``

### 35. silver=`review` cascade=`C(chain)` (A=- B=- conf=0.113)
> Re-review the job posting and the resume as it sits right now. What are the gaps that I have between this resume variant and the job posting?
**your label:** ``

### 36. silver=`analyze` cascade=`C(chain)` (A=- B=- conf=0.117)
> do research. what is the best coding LLM I could fit on a 12G 3060 card and get acceptable (30-60+ tps decode) performance? Give me a chart with tradeoffs.
**your label:** ``

### 39. silver=`git` cascade=`C(chain)` (A=- B=- conf=0.11)
> don't push them, but did we have changes that we made to contact dedup?
**your label:** ``

### 40. silver=`debug` cascade=`C(chain)` (A=- B=- conf=0.121)
> domain is hodgens.net, they forward to gmail mostly. this used to work, but recent new aliases don't forward and the old ones have become inconsistent. I'm losing mail. (Other domains have the same problem, eg. meept@meept.dev -> aequitas@gmail.com isn't working at all)
**your label:** ``

### 41. silver=`code` cascade=`C(chain)` (A=- B=- conf=0.116)
> can we write a script to do this? I've got 12? or so domains
**your label:** ``

### 42. silver=`review` cascade=`C(chain)` (A=- B=- conf=0.104)
> I'd like you to review my github (github.com/bhodgens) and look at those repositories and see if there's anything which could be used to bolster my resume and/or cover leters for these positions. Give a summary/reocmmendation before modification
**your label:** ``

### 43. silver=`review` cascade=`C(chain)` (A=- B=- conf=0.105)
> review the go based skills specifically. I had another agent start the process but failed
**your label:** ``

### 44. silver=`review` cascade=`C(chain)` (A=- B=- conf=0.124)
> using subagents if possible review the go codebase for bugs systematically and completely. do not correct the bugs, simply have the subagents note them in grok-findings.md . When done, give me a summary of the findings.
**your label:** ``

### 45. silver=`code` cascade=`plan` (A=plan B=- conf=0.122)
> implement the plan
**your label:** ``

### 47. silver=`analyze` cascade=`C(chain)` (A=- B=- conf=0.144)
> Research and compare game engines suitable for building a team-based and solo FPS game, plus identify moddable commercial games whose engines could serve as a development base. Focus on licensing, cost, FPS capability, networking/multiplayer support, and mod tooling.
**your label:** ``

### 48. silver=`platform` cascade=`C(chain)` (A=- B=- conf=0.124)
> how do I use the local-only profile I'd just asked you to create?
**your label:** ``

## Agreements (13) — confirm or correct

### 4. silver=`code` cascade=`code`
> using subagents, implement the plan.md and output a report to report.md
**your label:** ``

### 6. silver=`review` cascade=`review`
> what other issues/failures did you identify in your review?
**your label:** ``

### 16. silver=`code` cascade=`code`
> Add ProjectsConfig struct and Projects field to the root Config in internal/config/schema.go. Also add default values in DefaultConfig().
**your label:** ``

### 17. silver=`code` cascade=`code`
> Add the projects section to the default meept.json5 config template.
**your label:** ``

### 18. silver=`code` cascade=`code`
> Implement Tasks 7 and 8: Add project fields to Session struct and wire ProjectManager into daemon components.
**your label:** ``

### 19. silver=`code` cascade=`code`
> Implement Tasks 9 and 10: Add project RPC methods and HTTP API endpoints.
**your label:** ``

### 20. silver=`code` cascade=`code`
> Implement Tasks 11 and 12: Add --project/--nofence CLI flags and meept projects subcommand.
**your label:** ``

### 25. silver=`debug` cascade=`debug`
> figure out why search isn't working for hermes and fix it
**your label:** ``

### 28. silver=`plan` cascade=`plan`
> are there any other questions which need clarification about the plan? give me a high level overview as you understand it, and confirm whether you used the initial proposal to scope the plan.
**your label:** ``

### 30. silver=`git` cascade=`git`
> commit and push
**your label:** ``

### 37. silver=`git` cascade=`git`
> how do i push these changes? are they disruptive?
**your label:** ``

### 38. silver=`git` cascade=`git`
> commit, push, then run prodenv/prod
**your label:** ``

### 46. silver=`git` cascade=`git`
> lets' set up a repo and push to 

git remote add origin git@github.com:bhodgens/rebellion-the-game.git
git branch -M main
git push -u origin main
**your label:** ``
