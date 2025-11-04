# MIB profiles (mib_profiles/) and automation

This document describes the lightweight JSON schema used to store manufacturer MIB profiles, recommended workflow for generating profiles from collected MIB walks, and how profiles can be distributed to agents.

Schema (example keys)

- `vendor` (string): short vendor id, e.g. "hp"
- `enterprise_oid` (string): vendor enterprise prefix, e.g. "1.3.6.1.4.1.11"
- `version` (string): profile version
- `canonical` (map): semantic name -> OID (page_count, serial, supplies roots, etc.)
- `probes` (array): recommended quick probe OIDs that are safe/cheap to GET during discovery
- `sample_values` (map): example values to help human review and tests
- `notes` (string): freeform notes

Storage and runtime plan

- Profiles live in `agent/mib_profiles/` in JSON form. Curated profiles should be committed to the repo for review.
- Agents should optionally load `mib_profiles/*.json` at startup (opt-in via config). Loaded `probes` merge into the runtime quick-probe lists so you can add vendors without code changes.
- Operators can add a file via the `mib_profiles_local/` directory to keep environment-specific or server-pushed profiles.

Profile generation workflow

1. Collect many bounded MIB walks for a vendor (the agent already writes `logs/mib_walk_<ip>_<ts>.json`).
2. Aggregate walks and extract candidate OIDs (Counter32 for counters, OctetString for names, Integer for levels). Produce a short summary (see `logs/hp_oid_summary.json`).
3. Create a draft `mib_profiles/<vendor>.json` with canonical OIDs and a small `probes` list (3–8 OIDs). Add `sample_values` and notes.
4. Validate profile against a small test set of devices by running targeted GETs using the candidate OIDs.
5. Publish profile to a central repo or server UI. Agents can fetch published profiles or receive them via server push.

Design notes and versioning

- Keep `probes` small. The goal is to be lean: probe only a few OIDs to prove printer-ness. Only run larger enterprise walks for confirmed printers.
- Version profiles when you change canonical mappings to allow rollbacks.
- Include `sample_values` and `device_examples` in the profile to make review easier.

Next steps to implement in the agent (low-risk):

1. Add a small loader that reads `mib_profiles/*.json` and merges `probes` into the `vendorProbes` map at startup.
2. Add a CLI command `agent import-profile <file>` to validate and add a local profile.
3. Add a server-side endpoint to host curated profiles and an agent-side opt-in to fetch them.

See `mib_profiles/hp.json` for a first HP draft derived from local walks.
