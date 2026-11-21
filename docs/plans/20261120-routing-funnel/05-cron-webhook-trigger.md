# Cron and webhook triggers - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below. Make the 00T cron test pass. Do not weaken it.
> Do NOT commit. Do NOT git add.

## Meta

- **Parent:** master.md
- **Scope:** Enabled cron and webhook bot triggers call Manager.Trigger.
- **Dependencies:** 00T-test-gate.md, 01 REVIEWED, 03 REVIEWED, contract C6
- **Estimated Context:** 55K
- **Concurrency Group:** C

## Goal

A stored cron row starts a goal round. A stored webhook row starts a goal round. Neither path calls BotRunner.

## Tasks

1. On bot start, for each enabled trigger of type `cron`, register a scheduler job. The job calls `EmployeeManager.Trigger` with source `cron` and the trigger prompt. A duplicate registration for the same bot id and schedule is a no-op.

2. Add one HTTP route for webhook triggers. Look up the bot by the webhook id in the path. Call Trigger with source `webhook` and the request body as the prompt payload. Unknown id returns 404. Disabled trigger returns 404. Do not log the raw body if it may hold a secret. Log the bot id and the byte length only.

3. Both sources stamp ResultTopic `employee.result` through the leaf 01 stamp path. They must not set the loop flag and hope Execute reads it.

4. The round uses EmployeeChatSessionID from leaf 03.

5. Make `TestCronTriggerCallsManager` pass. Add a sibling test for the webhook route if the cron test does not cover HTTP.

Do not restore BotRunner. If `tests/bot_integration_test.go` still calls NewBotRunner, leave that file to leaf 06 unless this leaf cannot compile without a one-line compile fix. Prefer leaf 06 for that fix.

## Interface Contract

Exposes C6 source strings `cron` and `webhook`.

## Self-Verification Checklist

- [ ] A cron tick calls Trigger, not a runner Execute
- [ ] Webhook 404s on an unknown id
- [ ] Do NOT commit

## Review Checklist

- [ ] No token or body secret in logs
- [ ] Result topic is employee.result for these sources
- [ ] Do NOT commit
