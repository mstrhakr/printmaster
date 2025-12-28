# Live Upload Plan

Status: Draft
Date: 2025-11-13

This document records the PrintMaster live-upload design and the decisions made during planning. It intentionally complements `docs/AGENT_UPLOAD_ARCHITECTURE.md` and `docs/WEBSOCKET_PROXY.md` and fills the gaps required to implement a durable, streaming-first agent upload system.

Goals
 - Durable-by-default uploads: events are reliably stored and retried until acknowledged by the server.
 - Primary low-latency transport: WebSocket streaming for near-real-time events.
 - Robust fallback: HTTP batch uploads if WS is unavailable or for bulk catch-up.
 - Clear ACK semantics: server indicates both receipt and later confirmation of data integrity.
 - Configurable but safe defaults: memory-first optimizations possible, but durability enabled by default.

Key decisions (user-approved)
 - Durability: default is durable-by-default. Agents persist events to the on-disk queue (SQLite) and will only delete items after server confirmation.
 - Connection-awareness: the UploadWorker (or the upload subsystem) will consult the local `serverClient` / WS connection manager to decide whether to send an event over WS, queue it (persist), or fall back to HTTP batch.
 - Migration approach: replace the existing UploadWorker upload paths with the streaming-first system. Run in parallel during rollout (feature-flag) until tests and trialing confirm stability, then remove old worker.
 - ACK semantics: two-stage ACK model:
   1. "ack.received" — server acknowledges the full message payload was received and validated syntactically. Agent may mark an event as "received" but should still wait for confirmation before deleting permanently.
   2. "ack.confirmed" — server confirms the data has been persisted and integrity-checked (optional SHA/hash verification or content validation). Only on confirmed should the agent permanently delete the queued item.
 - Flow-control & throttling: server may instruct agent to slow down or pause (control message). Agents should honor maximum in-flight count and configurable rate limits.

Message shapes (JSON over WS)

Event (Agent -> Server):
```
{
  "type": "event",
  "event_id": "uuid-v4",
  "serial": "DEVICE-SERIAL",
  "kind": "metric_snapshot|device_update|scan_event",
  "timestamp": "2025-11-13T14:12:01.123456789Z",
  "payload": { ... },
  "sha256": "optional-sha256-of-payload"
}
```

ACK (Server -> Agent) examples:

Received ack:
```
{
  "type": "ack",
  "event_id": "uuid-v4",
  "status": "received",
  "received_at": "2025-11-13T14:12:01.150000000Z"
}
```

Confirmed ack:
```
{
  "type": "ack",
  "event_id": "uuid-v4",
  "status": "confirmed",
  "persisted_at": "2025-11-13T14:12:01.500000000Z",
  "server_seq": 12345
}
```

Negative ack example:
```
{
  "type": "ack",
  "event_id": "uuid-v4",
  "status": "error",
  "code": "validation_failed",
  "message": "missing serial",
  "received_at": "..."
}
```

On-disk queue schema (SQLite)
```
CREATE TABLE IF NOT EXISTS upload_queue (
  id TEXT PRIMARY KEY,             -- uuid event id
  serial TEXT NOT NULL,            -- device serial
  sequence_no INTEGER DEFAULT 0,   -- optional per-serial ordering
  kind TEXT NOT NULL,              -- metric_snapshot, device_update, etc.
  payload TEXT NOT NULL,           -- JSON payload
  sha256 TEXT,                     -- optional content hash
  attempts INTEGER DEFAULT 0,
  last_error TEXT,
  created_at TEXT NOT NULL,        -- RFC3339Nano UTC
  received_at TEXT,                -- timestamp of server "received" ack
  confirmed_at TEXT,               -- timestamp of server "confirmed" ack
  acked INTEGER DEFAULT 0          -- 0 = pending, 1 = removed/confirmed
);
CREATE INDEX IF NOT EXISTS idx_upload_queue_serial ON upload_queue(serial);
CREATE INDEX IF NOT EXISTS idx_upload_queue_created_at ON upload_queue(created_at);
```

Queue semantics
 - Enqueue on event generation before attempting network send (durable-by-default).
 - Attempt sending from queue when WS is connected (prefer), otherwise use HTTP batch for catch-up.
 - On WS send: mark attempts++, wait for ack.received; on ack.received set received_at; on ack.confirmed set confirmed_at and delete the row.
 - Retry with exponential backoff on transient errors; surface permanent failures for admin inspection.

Interaction with existing UploadWorker and serverClient
 - The upload subsystem will consult the local `serverClient`/WS manager for connection status and heartbeats. Example checks: `IsConnected()`, `LastSeen()`, and `InFlightCount()`.
 - During rollout both systems can run concurrently (streaming & existing batch worker), but the plan is to fully migrate to streaming and remove the legacy batch uploader once stable.

ACK timeouts, limits and resource planning
 - ACK timeouts: default 30s for ack.received, 120s for ack.confirmed; configurable.
 - Max in-flight events per WS: default 256; configurable per agent based on server scaling.
 - Queue retention policy: keep pending items for up to N days (configurable, default 14 days). Admin can inspect/trim or requeue.
 - Server scaling: the server must be designed to handle many concurrent WS connections. Typical approaches:
   - Use a horizontally-scalable WebSocket gateway (stateless front-tier) with sticky routing (or centralized connection manager) and a distributed backend for persistence.
   - Use connection pooling and backpressure (server instructs clients to slow down).
   - Shard agent connections across backend nodes. Each backend persists incoming events to a durable queue (Kafka, DB) for processing.

Auth & registration
 - Use existing registration/token flow (agentConfigStore + serverClient). Streaming checks must reuse or extend that logic: if WS auth fails, attempt re-registration and re-establish connection.
 - Auth errors (401) should trigger immediate re-registration attempts (bounded retries) similar to existing UploadWorker logic.

Data integrity / diff verification
- Two-stage ACKs provide room for a later "confirmed" that can be tied to a server-side verification (SHA/size/DB write). Agents SHOULD include a `sha256` (hex) of the payload in the event. The server MUST validate the `sha256` before accepting the payload as valid.

How verification works (recommended, avoids extra DB read)
1. Agent serializes the payload into a canonical byte representation (see notes below) and computes sha256(payload_bytes) -> client_sha.
2. Agent sends the event with the `sha256` field populated and the payload bytes (optionally compressed).
3. Server receives the message and computes sha256(received_payload_bytes) -> server_sha.
   - If server_sha != client_sha: respond with `ack` status `error` (code `hash_mismatch`) and do not persist the payload.
   - If server_sha == client_sha: proceed to persist.
4. Persist the payload and metadata (including persisted `sha256`) in a single atomic DB transaction (or write to blob store + reference in DB). Because the server already computed the sha from the same bytes it will persist, there's no need to re-read the DB to recompute the hash — persist and then emit `ack.confirmed`.

Notes on canonicalization and serialization
- To guarantee the client and server compute the same sha, both must agree on the exact bytes hashed. Recommended options:
  - Send the payload as raw bytes (agent serializes to UTF-8 JSON, with deterministic key ordering and no extra whitespace) and compute the sha over those bytes.
  - Better: agent sends a binary-encoded payload (CBOR/MsgPack/Protobuf) and computes sha over that representation; binary encodings avoid JSON canonicalization pitfalls.
  - If compression is used (e.g., gzip), compute the sha over the compressed bytes and document that the server validates compressed-bytes sha (include a `compression` field).

Alternatives and trade-offs
- Persisting the sha alongside the data (content-addressed) is efficient and avoids double-reading the DB. This is the recommended approach.
- Content-addressed storage: for dedupe and scaling, the server can store payload blobs keyed by sha256 and store lightweight references in the main DB. This saves space and simplifies dedup logic for identical payloads across agents.
- Batch checksum: for very high event rates, agents can send batches with a batch-level sha256 and per-event shas; server validates batch integrity first and then processes items. This reduces per-message overhead at the cost of slightly larger atomicity domains.

Performance considerations
- CPU: computing sha256 is CPU-light relative to network and DB writes for typical JSON metric payloads. Use streaming hashing for very large payloads to avoid excessive memory use.
- Network: adding a 32-byte hex sha256 field per event is negligible compared to typical metric payloads. If agents batch events, include per-batch and per-item shas where appropriate.
- DB reads/writes: avoid an extra read to verify persisted bytes. Compute the sha from received bytes and persist the sha in the same transaction as the payload.

When a mismatch indicates corruption
- If `hash_mismatch` occurs, the server should return a clear `ack` error and log the event for inspection. The agent should mark the attempt as failed, increment attempts, and either retry (after reserializing) or surface the error for operator action if repeated.

Backwards compatibility
- For rollout, the `sha256` field can be optional initially (server treats absence as no-integrity-check). However, to get the full guarantees, flip to required mode after a migration window. During the transition, agents that include `sha256` get stronger guarantees.

Security note
- Use a cryptographically-strong hash (SHA-256). Do not rely on non-cryptographic checksums for integrity guarantees.


Phases & rollout
 Phase 0 — Design & docs (current): finalize message shapes, queue schema, and tests. Feature flag support added.
 Phase 1 — Prototype (streaming+acks): implement WS send/ack handling, local queue, server ack endpoints. Default enabled behind feature flag.
 Phase 2 — Durability & fallback: ensure disk-backed queue is stable, implement HTTP batch fallback, implement admin tools for queue inspection and trimming.
 Phase 3 — Scale & remove legacy uploader: after trials, flip default to streaming without legacy batch worker and remove old code paths.

Testing
 - Unit tests for queue CRUD, enqueue/dequeue/ack logic.
 - Mocked WS integration tests: simulate server acks, timeouts, and reconnects.
 - Integration outage test: generate events while server unreachable, then restore server and verify queue drains and items are confirmed/removed.
 - Load test: simulate 1k agents with N devices each to validate server front-tier throughput and backend persistence.

Monitoring & metrics
 - Agent metrics to expose: queue_depth, in_flight_count, last_ack_latency, failed_attempts, last_server_seen.
 - Server metrics: ack_latency, per-agent in_flight_count, total_connections, total_events_received, processing_lag.
 - Alerts: queue_depth > threshold, sustained ack latency, high failed_attempts.

Admin tools
 - CLI or HTTP endpoints to inspect/trim/requeue items in the `upload_queue` for a given agent.
 - Admin action to reprocess or purge older items if needed.

Open questions / future improvements
 - Whether ack.confirmed must correspond to persistence in the primary DB or persistence to an intermediate durable message system (Kafka) is left to implementation and scaling choices.
 - Server sharding and WS gateway architecture will require additional ops planning for large deployments.

Appendix: minimal WS control messages
 - `control.pause` — server requests agent pause sending new events (until resumed)
 - `control.resume` — server resumes
 - `control.rate_limit` — server suggests max events/sec or max in-flight

Contact: operations / server-team to iterate on scaling and gateway choices.
