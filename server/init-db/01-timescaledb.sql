-- Enable TimescaleDB extension
-- This script runs automatically on first database startup
-- when mounted to /docker-entrypoint-initdb.d/

-- Create the TimescaleDB extension (idempotent)
CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE;

-- Verify installation
DO $$
BEGIN
    RAISE NOTICE 'TimescaleDB extension enabled successfully';
END $$;
