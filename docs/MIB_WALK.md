# MIB Walk (on-demand) — Deprecated

This document described the former on-demand MIB walk HTTP endpoint (`/mib_walk`). That endpoint has been removed.

Purpose
-------

A MIB walk helps identify which OIDs a device implements. The agent now performs targeted walks internally during discovery for confirmed printers, instead of exposing a general-purpose `/mib_walk` endpoint.

Endpoint
--------

The `/mib_walk` endpoint is removed. Use the discovery flow and device actions (e.g., Walk All) in the UI, which trigger bounded, targeted walks.

Developer notes
---------------

- Discovery uses the `SNMPClient` abstraction with bounded, targeted walks. Tests can mock `SNMPClient` to avoid network access.

Next steps
----------

- Keep targeted walks lean and vendor-agnostic. Avoid reintroducing large walk surfaces or external MIB dependencies.
