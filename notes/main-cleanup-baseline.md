# Main Cleanup Baseline

Date: 2026-04-06

## Current Behavior Checklist

- [ ] Bot starts
- [ ] /start responds
- [ ] Duration buttons trigger scheduler
- [ ] Shutdown cancels tasks

## Baseline Validation

- go build ./...
- go test ./...

I can make it retrievable in the conversation, but I cannot persist it to the repo or memory from this turn because I’m in read-only Ask mode.

Use this as the recovery block. Search the chat for `PAYSTACK-HARDENING-EXEC-V1` if the session drops.

```text
PAYSTACK-HARDENING-EXEC-V1

Goal:
Harden the Paystack payment pipeline in ordered iterations, one commit per iteration.
Leave the duration-label mismatch untouched.

Iteration 1
Title: Cache Paystack recipient codes
Changes:
- Add recipient cache interface in internal/cache/types.go
- Add Redis recipient cache keyed by country + bankCode + accountNumber
- Update AutoTransferToOrder in internal/service/payment.go:
  - check cached recipient_code before CreateTransferRecipient
  - create and cache on miss
  - if cached recipient is stale/invalid, recreate once and refresh cache
Tests:
- cache hit skips recipient creation
- cache miss stores recipient
- stale cached recipient recreates once
Commit:
- feat(payment): cache paystack transfer recipient codes

Iteration 2
Title: Remove manual balance pre-check
Changes:
- Remove EnsureSufficientBalance call from transfer flow in internal/service/payment.go
- Keep ErrInsufficientBalance
- Add Paystack error classifier that maps transfer rejection messages into wrapped ErrInsufficientBalance
- Ensure AutoTransferToOrder returns ErrInsufficientBalance only from Paystack-originated failure
Tests:
- no GetBalance call in transfer flow
- insufficient-balance message maps to ErrInsufficientBalance
- unrelated Paystack errors remain generic
Commit:
- fix(payment): remove preflight balance check and classify paystack insufficient-funds errors

Iteration 3
Title: Add bounded retry/backoff for payment failures
Changes:
- Use PaymentIntentRecord.NextRetryAt as retry gate
- Update retry logic in internal/botruntime/runtime.go:
  - retry INSUFFICIENT_FUNDS after backoff
  - retry recoverable TRANSFER_FAILED after backoff
  - do not hot-loop every 15s
  - do not retry terminal failures forever
Tests:
- insufficient funds respects NextRetryAt
- recoverable transfer failure retries
- terminal failure stops retrying
- backoff advances on repeated failure
Commit:
- feat(runtime): back off and retry recoverable payment failures

Iteration 4
Title: Prevent local/provider paid-state divergence
Changes:
- In internal/webhook/paystack.go, do not advance workflow to PAID before provider MarkOrderPaid succeeds
- On transfer.success:
  - mark transfer success on intent
  - call MarkOrderPaid
  - only then advance workflow to PAID
- On provider mark failure:
  - set PROVIDER_MARK_FAILED
  - persist LastError and NextRetryAt
  - keep workflow non-terminal
- Add runtime retry path for provider mark failures
Tests:
- provider success advances workflow
- provider failure leaves workflow pending
- retry path resumes provider mark later
- duplicate webhook remains idempotent
Commit:
- fix(workflow): only mark orders paid after provider confirmation succeeds

Iteration 5
Title: Verify Paystack transfer before final completion
Changes:
- In internal/webhook/paystack.go, call VerifyTransfer(reference) before finalizing success
- Validate:
  - reference matches
  - status is success
  - amount matches intent
  - currency matches intent
- If verification fails:
  - do not finalize
  - record failure on intent
Tests:
- verified success finalizes
- verification mismatch blocks finalization
- amount/currency mismatch blocks finalization
Commit:
- feat(webhook): verify paystack transfers before completing payment workflow

Iteration 6
Title: Add direct payment-intent lookup by order id
Changes:
- Extend internal/cache/types.go with GetByOrderID
- Implement it in Redis payment-intent store
- Replace ListByChat scans in runtime retry path with direct lookup
Tests:
- GetByOrderID returns expected intent
- runtime retry uses direct lookup
- ListByChat still supports bot history command
Commit:
- refactor(cache): add direct payment-intent lookup by order id

Iteration 7
Title: Require webhook secret outside dev
Changes:
- In cmd/mtg-bot/main.go, require PAYSTACK_WEBHOOK_SECRET unless explicitly in dev mode
- Fail fast with clear startup error in non-dev mode
Tests:
- prod without secret fails
- dev without secret allowed
- prod with secret starts
Commit:
- fix(webhook): require paystack webhook secret outside dev mode

Iteration 8
Title: Graceful webhook shutdown
Changes:
- Replace bare ListenAndServe in cmd/mtg-bot/main.go with http.Server
- On signal, call Shutdown with timeout context
- Keep stop order deterministic: webhook server, bot, task manager
Tests:
- manual verification acceptable if no narrow test harness exists
Commit:
- feat(server): gracefully shut down webhook listener

Agent execution rules:
- Read current files before each iteration
- Keep each iteration isolated
- Run targeted tests, then go test ./...
- Ask for permission before each commit
- Create exactly one commit per iteration
- Do not touch the duration mismatch
- Treat recipient cache as optimization, not source of truth
```
