# PrintMaster Documentation Index

Use this guide to find the authoritative documents for each area of the project. When updating or creating docs, add them to the appropriate section below.

## Start Here
- `PROJECT_STRUCTURE.md` – up-to-date repo layout and module overview
- `BUILD_WORKFLOW.md` – build/test/release commands + VS Code tasks
- `ROADMAP.md` – milestone status (tracks current 0.9.x → 1.0 plan)

## Architecture & Design
- `AGENT_UPLOAD_ARCHITECTURE.md` – agent ↔ server comms and heartbeat/upload flows
- `SECURITY_ARCHITECTURE.md` – authentication/authorization plans
- `DATABASE_ROTATION.md` – schema v8, rotation logic, backup strategy
- `DEVICE_CAPABILITIES.md`, `CAPABILITY_INTEGRATION.md` – per-printer feature tracking
- `WEBSOCKET_PROXY.md` – server tunnel/proxy details
- `SHARED_WEB_ASSETS.md` – guidance for embedded UI assets shared between components

## Operations & Deployment
- `AGENT_DEPLOYMENT.md`, `SERVICE_DEPLOYMENT.md` – installing the agent as a service on Win/Linux/macOS
- `DOCKER_DEPLOYMENT.md`, `UNRAID_DEPLOYMENT.md` – container-focused instructions
- `SERVER_SETTINGS_PLAN.md`, `TENANCY_ROADMAP.md` – multi-tenant and server configuration planning
- `UI_THEME_CUSTOMIZATION.md` – branding and theme overrides for hosted deployments

## Development & Testing
- `TESTING.md` – unit/integration testing strategy and helper patterns
- `SNMP_REFERENCE.md`, `SNMP_RESEARCH_NOTES.md`, `Printer-MIB.mib` – protocol research data
- `USB_IMPLEMENTATION.md`, `EPSON_REMOTE_MODE_PLAN.md` – feature spikes currently in development
- `LIVE_UPLOAD_PLAN.md`, `AGENT_SERVER_PLAN.md`, `METRICS_IMPROVEMENTS.md` – design notes backing existing functionality

## Legacy / Needs Review
- `AGENT_DEPLOYMENT.md` (older installer references) – keep for context but verify before using
- `DEVICE_CAPABILITIES.md` vendor tables – update when adding/removing OID mappings
- `SNMP_RESEARCH_NOTES.md` – historical experiments; consolidate into `SNMP_REFERENCE.md` when validated

> See `.github/copilot-instructions.md` for condensed guidance aimed at AI/code assistants (not a user-facing document).
