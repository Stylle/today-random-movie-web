# Automatic notification delivery maintenance

2026-11-17 local update. No remote publication.

Previously the scheduler claimed a period before sending and skipped it forever after a transient network/channel/selection failure. Durable per-period delivery rows now retain the selected movie, successful channels, attempt count, next retry and error. At most four attempts use 1/5/15-minute backoff. A restart or calendar boundary does not abandon pending retries; disabled rules pause them and attempts overdue by 24 hours expire. Already confirmed channels are skipped. Response loss or a crash after external acceptance can still produce duplicates; no exactly-once guarantee. Old-version claimed periods are not replayed on upgrade. Manual runs remain independent new selections.

The schedule cards expose the result, attempt count, retry time and error. Whole-database backups include the auxiliary table automatically; old backups migrate on next startup. Original rules, channels, filters and authentication are preserved.

Verified with isolated protocol mocks: retry recovery, same-movie reuse, channel-level success deduplication, durable state reload, backoff, exhaustion, no-match reporting, old-claim preservation, calendar boundaries and disabled rules. Full Go tests/vet and Linux race tests passed. Production frontend and phone/tablet/desktop light/dark browser regression passed. No real chat was used for testing.
