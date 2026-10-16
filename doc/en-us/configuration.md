# Configuration

## Raw documents

Each network is a raw EasyTier TOML document. The raw text is the source of truth: nothing parses a
model and re-serializes it over the user's file, so comments, unknown fields and formatting
survive.

- **Revisions** are SHA-256 hashes of the text. A save names the revision it edited from; a
  mismatch fails with a conflict and leaves the file untouched.
- **Writes** are atomic, and the previous content is backed up (ten backups per document).
- **Invalid drafts may be saved.** Activation refuses them, and runs the selected core's
  `--check-config` first.

## Form projection

The form edits a projection of the document (`configdoc.Values`). `configdoc.Changes` computes
the fields the user changed and `configdoc.ApplyValues` rewrites only their spans; every write
re-parses its result. Structures the form cannot represent stay raw-text only.

ACL rules are edited in the TOML editor; the structured ACL editor returns in a later release.

## Save and apply

Saving writes the document. Applying is a separate, explicit action that restarts that network's
core with a frozen copy of the saved revision. If the new revision fails during its grace period,
the daemon returns to the previously applied revision and reports why.

## Secrets

`network_secret` and secure-mode private keys are masked in the form, never logged, never put into
API snapshots or diagnostics, and never persisted outside the user's documents.
