# ADR-0012 · Read-only attester-duty POST

- **Status:** accepted
- **Date:** 2026-08-23
- **Deciders:** maintainers
- **Supersedes:** —

## Context

I-1 forbids POST requests to a beacon node unless the endpoint is explicitly
read-only by semantics and documented in an ADR. The standard attester-duty API is
POST because validator indices are supplied as a JSON array, not because it mutates
beacon-node state.

## Decision

Allow POST only to
`/eth/v1/validator/duties/attester/{epoch}`. It accepts validator indices and returns
computed duty assignments. It cannot publish duties, attestations, blocks, keys, or
configuration. The adapter exposes this as `FetchAttesterDuties`; no generic POST
surface is exported.

The endpoint contract is maintained by the Ethereum
[Beacon APIs specification](https://github.com/ethereum/beacon-APIs/blob/master/apis/validator/duties/attester.yaml).

## Consequences

Automatic attester tracking remains standards-compatible while I-1 stays auditable.
Any additional POST endpoint requires its own explicit read-only review and ADR
update.

## Alternatives considered

**Encode indices in a GET query.** Rejected because it is not the standardized API.

**Fetch every validator duty.** Rejected because it would increase node load and
expose unrelated operator data without removing the POST requirement.
