# Audit and evidence

## Chain

Every trajectory is one hash chain. An event hash is HMAC-SHA256 over the canonical typed record including `prev_hash`. The chain is per trajectory, not per call, so a long session is one causal history. Sequence numbers are contiguous; a gap is a capture or storage failure.

The signing key is derived from the instance key with a domain separator. The gateway owns the instance key; the harness SDK can submit events but cannot sign a checkpoint.

## Verification statuses

`verify-trajectory` reports:

- `verified`: contiguous sequence, valid previous hash, valid HMAC and a matching checkpoint;
- `in_progress`: a live trajectory whose chain and checkpoint are valid but which has no terminal event yet;
- `incomplete`: the range or checkpoint is incomplete;
- `broken`: a hash, sequence or checkpoint failure, with the sequence and reason;
- `unavailable`: the database or events could not be read.

A broken chain names the property that failed: `sequence_gap`, `previous_hash_mismatch`, `hash_mismatch`, `invalid_payload`, `merkle_root`, `checkpoint_signature`, `checkpoint_range` or `trajectory_mismatch`. Bundle verification adds `manifest_version`, `segment_missing`, `segment_content_missing`, `segment_hash` and `manifest_hash`.

## Checkpoints

Each append creates a cumulative checkpoint row over every event of the trajectory up to that batch:

- `from_seq`, `to_seq`, `merkle_root` over the event hashes;
- `signature` over the checkpoint fields with a separate HMAC key;
- `prev_hash` chaining to the previous checkpoint signature;
- optional `witness_ref` for an external witness.

A checkpoint is not an event; it anchors a range of events. The verifier recomputes the Merkle root and the signature offline. The current writer recomputes the full range on each append; incremental checkpointing for very long trajectories is a deliberate limit in [Roadmap](roadmap.md).

## Evidence bundle

A bundle contains:

- the trajectory id and its full event range;
- events with hashes and previous hashes;
- segments with content, labels and media type;
- edges connecting them;
- the policy version and hash in force for the trajectory;
- approvals and execution counts;
- the latest checkpoint;
- a manifest with a bundle hash and verifier version.

`export-bundle` writes it; `verify-bundle` recomputes the chain, Merkle root, checkpoint signature, every referenced segment's content hash and the manifest. A bundle that omits a referenced segment or its content fails verification. A failed bundle reports the property that failed, never a generic error. The evidence package does not import the repository or the writer.

## What the chain does and does not prove

It proves events were not edited after signing and that a checkpoint covers a range. It does not prove that every boundary was captured; that is the coverage model. A gap lowers coverage and produces a finding, so an incomplete recorder cannot produce a `safe` verdict.

## Operator commands

```text
safeagent verify-trajectory [-project ID] [-trajectory ID] [-limit N] [-json]
safeagent export-bundle -project ID -trajectory ID [-output FILE]
safeagent verify-bundle -file FILE
```

`verify-trajectory` requires the instance key because every HMAC depends on it. `verify-bundle` also requires the key to verify the checkpoint; it needs no database.

## Related

- [Model](model.md)
- [Storage](storage.md)
- [Threat](threat.md)
- [Development](development.md)
