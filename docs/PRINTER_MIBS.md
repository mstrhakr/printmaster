# Printer MIBs and detection notes

This document summarizes the Printer-MIB OIDs and vendor detection heuristics used by the agent.

## Quick detection goals
- Prove a device is a printer as quickly as possible using a small number of SNMP GETs.
- Fetch the most reliable page counters and consumable metrics (mono/color counters, toner/supply levels).
- If the quick probe is successful, run deeper vendor-specific queries or a limited walk.

## Core OIDs the agent probes (fast path)
- sysObjectID: 1.3.6.1.2.1.1.2.0 — used to identify vendor enterprise OID (1.3.6.1.4.1.X)
- sysDescr: 1.3.6.1.2.1.1.1.0 — used as a fallback heuristic to detect vendor/model
- Printer-MIB (common):
  - prtMarkerLifeCount (marker 1): 1.3.6.1.2.1.43.10.2.1.4.1.1 — commonly contains total life count for the black marker (mono)
  - prtMarkerSuppliesDescr (index 1): 1.3.6.1.2.1.43.11.1.1.6.1.1 — supply description (e.g., "Black Toner")
  - prtMarkerSuppliesLevel (index 1): 1.3.6.1.2.1.43.11.1.1.9.1.1 — supply level/remaining

## Vendor enterprise probes (quick list — not exhaustive)
- HP: 1.3.6.1.4.1.11.*
- Brother: 1.3.6.1.4.1.2435.*
- Canon: 1.3.6.1.4.1.1602.*
- Lexmark: 1.3.6.1.4.1.641.*
- Epson: 1.3.6.1.4.1.231.*
- Kyocera: detected via sysDescr ("kyocera") — enterprise OIDs are logged for later mapping.

## Unknown manufacturers
- If the agent can't confidently detect a manufacturer, it appends a line to `logs/unknown_mfg.log` with timestamp, IP, sysObjectID and sysDescr. Use this file to identify new vendor enterprise OIDs to add to `vendorProbes`.

## How to add vendor OIDs (optional)
We no longer ingest or parse external MIB files inside the agent. If needed, you can add small, vendor-agnostic probe OIDs to the code for better hints. Prefer Printer-MIB first and keep vendor-specific additions minimal.

## UI and runtime behavior
- The UI runs a quick probe per-device on discovery; if the device looks like a printer the agent records `PrinterInfo` with:
  - PageCount / TotalMonoImpressions / MonoImpressions / ColorImpressions
  - TonerLevels (map description->level)
  - Consumables (list of supply descriptions)
  - StatusMessages (snippets that look like "toner low", "paper jam", etc.)
- On confirmed printers, the agent may perform a targeted walk to capture a subset of enterprise data for diagnostics. The generic MIB Walk UI/endpoint has been removed.

## Next steps
- Curate more vendor-specific OIDs for Kyocera, Epson and other manufacturers and add unit tests that mock the SNMP client to avoid network in CI.
- Avoid reintroducing MIB ingestion. Keep probes lean and self-contained.

## Two-phase scan (what we implemented)

- Phase 1 (quick verify): the agent performs a small number of SNMP GETs (sysObjectID, sysDescr, a few Printer-MIB index-1 OIDs and short vendor probes) to quickly determine whether the device looks like a printer.
- Phase 2 (deep probe): if Phase 1 yields evidence (marker counters, supplies, serial, or vendor enterprise hint), the agent runs a limited Printer-MIB walk and an enterprise-subtree walk for the detected vendor. The walk is intentionally capped (e.g., 200 entries) to keep discovery fast and bounded.

This two-phase approach keeps discovery lean on large networks while still capturing rich vendor-specific data when devices are confirmed to be printers.
