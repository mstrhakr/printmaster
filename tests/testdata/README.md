# PrintMaster E2E Test Data Directory
#
# This directory contains seed data for E2E testing.
# Databases are pre-populated with test devices, agents, and configuration
# to enable predictable, reproducible tests.
#
# Structure:
#   testdata/
#   ├── server/          # Server data directory (mounted to /data in container)
#   │   └── server.db    # Pre-seeded SQLite database
#   ├── agent/           # Agent data directory (mounted to /data in container)  
#   │   ├── agent.db     # Pre-seeded SQLite database
#   │   └── agent_id     # Fixed agent UUID for test stability
#   └── seed/            # SQL scripts to generate seed databases
#
# The .gitkeep files ensure directories exist in git.
# Actual database files are gitignored and regenerated during test setup.

## Regenerating Test Databases

Run the seed script to create fresh test databases:

```bash
# From tests/ directory
./seed-testdata.sh
```

Or manually:

```bash
# Create server seed database
sqlite3 testdata/server/server.db < seed/server_seed.sql

# Create agent seed database  
sqlite3 testdata/agent/agent.db < seed/agent_seed.sql
```

## Test Data Contents

### Server Database
- 1 tenant: "E2E Test Tenant"
- 1 registered agent: "e2e-test-agent" (UUID: e2e00000-0000-0000-0000-000000000001)
- 5 test devices with various vendors (HP, Kyocera, Brother, Lexmark, Xerox)
- Sample metrics data for the devices
- Admin user with password "e2e-test-password"

### Agent Database
- Agent ID: e2e00000-0000-0000-0000-000000000001
- 5 test devices matching server data
- Sample discovery results
- Scanner configuration

## Adding New Test Data

1. Modify the appropriate seed SQL file in `seed/`
2. Run `./seed-testdata.sh` to regenerate databases
3. Update this README if the schema changes
