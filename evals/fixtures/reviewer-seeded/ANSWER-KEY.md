# Answer key — withheld from the reviewer copy

Exactly three seeded defects are present in `change.diff`, all in the post-change `base/transfer.mjs`:

1. `base/transfer.mjs:4` — expected severity `P1`. The ownership check is inverted. A caller whose `actor.id` matches `account.ownerId` is rejected, while a caller with a different identity passes this check and can debit their own ledger to credit another account. Scenario: invoke `transfer` with `actor.id = "attacker"`, `account.ownerId = "victim"`, and a valid positive amount; the authorization check does not throw and the transfer reaches both ledger calls.
2. `base/transfer.mjs:3` — expected severity `P2`. The validation accepts amount `0`, despite the contract/error text requiring a positive amount. Scenario: invoke with `amount = 0` and an actor the post-change ownership check lets through (any non-owner, because of defect 1); the zero-value debit and credit calls execute. (The amount check at line 3 runs first, and `0` passes it; only then does defect 1 reject the owner at line 4, before either ledger call.)
3. `base/transfer.mjs:6` — expected severity `P3`. Rounding the credited amount to an integer loses fractional value while the debit uses the original amount. Scenario: invoke with a non-owner actor (the owner is rejected at line 4 before crediting), and valid `amount = 1.6`; it debits `1.6` and credits `2`, creating an imbalance. (At `1.4`, the credited amount is `1`, also unequal.)

Severity applies to this fixture's intended impact and is fixed before evaluation. The answer key is not part of the worker manifest and must not be copied to the reviewer destination.
