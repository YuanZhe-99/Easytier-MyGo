# Configuration

## Raw documents

Each network is a raw EasyTier TOML document. The raw text is the source of truth: nothing parses a
model and re-serializes it over the user's file, so comments, unknown fields and formatting
survive.

- **Revisions** are SHA-256 hashes of the text. A save names the revision it edited from; a
  mismatch fails with a conflict and leaves the file untouched.
- **Writes** are atomic, and the previous content is backed up (ten backups per document).
- **Invalid drafts may be saved.** Activation refuses them, and runs the selected core's
  `--check-config` first. Values the core refuses without a reason are checked by the app first
  and named: an `instance_id` that is not a UUID (an empty one included).

## Form projection

The form edits a projection of the document (`configdoc.Values`). `configdoc.Changes` computes
the fields the user changed and `configdoc.ApplyValues` rewrites only their spans; every write
re-parses its result. Structures the form cannot represent stay raw-text only.

The form shows `instance_id`. When it is empty or missing, a fresh UUID is filled in and written
by the next save; a button replaces it with a new one. New networks get one when created.

ACL rules are edited in the TOML editor; the structured ACL editor returns in a later release.

## Core versions and EasyTier mini

The projection reads both EasyTier 2.6 and 2.7 documents: the VPN portal in either format (a
client range, or named clients), `tcp_stun_servers`, `socket_mark` and `prefer_peer_relay`.
The editor names what the network's core cannot run (`configdoc.VersionConflicts`): on 2.7 or
later the 2.6 portal format, before 2.7 the settings 2.7 added. Prefer existing relays is offered
on 2.7 cores, or when the document sets it.

**Mini copies.** EasyTier mini silently ignores much of a document. Instead of changing a network,
"Make a mini copy" (`network.mini-copy`) adds a new one named "<name> (mini)" ("(mini 2)", … when
taken) from its saved document with what mini ignores removed (`configdoc.StripForMini`) and a
fresh `instance_id`; the original stays as its backup. In a mini copy's editor a notice lists
what mini would still ignore, and rows mini does not run say so.

## Save and apply

Saving writes the document. Applying is a separate, explicit action that restarts that network's
core with a frozen copy of the saved revision. If the new revision fails during its grace period,
the daemon returns to the previously applied revision and reports why.

## Secrets

`network_secret` and secure-mode private keys are masked in the form, never logged, never put into
API snapshots or diagnostics, and never persisted outside the user's documents.
