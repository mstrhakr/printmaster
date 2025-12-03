package db

import "testing"

func TestChooseDriverFromConfigDefaultSQLite(t *testing.T) {
	t.Parallel()

	// Empty config should default to sqlite3
	cfg := map[string]string{}
	result := ChooseDriverFromConfig(cfg)

	if result.Name != "sqlite3" {
		t.Errorf("Name = %q, want %q", result.Name, "sqlite3")
	}
	if result.DSN != "printmaster.db" {
		t.Errorf("DSN = %q, want %q", result.DSN, "printmaster.db")
	}
}

func TestChooseDriverFromConfigSQLiteVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cfg    map[string]string
		wantDSN string
	}{
		{
			name:    "sqlite driver",
			cfg:     map[string]string{"db.driver": "sqlite"},
			wantDSN: "printmaster.db",
		},
		{
			name:    "sqlite3 driver",
			cfg:     map[string]string{"db.driver": "sqlite3"},
			wantDSN: "printmaster.db",
		},
		{
			name:    "modernc driver",
			cfg:     map[string]string{"db.driver": "modernc"},
			wantDSN: "printmaster.db",
		},
		{
			name:    "modernc-sqlite driver",
			cfg:     map[string]string{"db.driver": "modernc-sqlite"},
			wantDSN: "printmaster.db",
		},
		{
			name: "with db.path",
			cfg: map[string]string{
				"db.driver": "sqlite3",
				"db.path":   "/data/custom.db",
			},
			wantDSN: "/data/custom.db",
		},
		{
			name: "with database.path fallback",
			cfg: map[string]string{
				"db.driver":     "sqlite3",
				"database.path": "/var/lib/app.db",
			},
			wantDSN: "/var/lib/app.db",
		},
		{
			name: "database.driver key",
			cfg: map[string]string{
				"database.driver": "sqlite3",
				"database.path":   "/opt/data.db",
			},
			wantDSN: "/opt/data.db",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := ChooseDriverFromConfig(tc.cfg)

			if result.Name != "sqlite3" {
				t.Errorf("Name = %q, want %q", result.Name, "sqlite3")
			}
			if result.DSN != tc.wantDSN {
				t.Errorf("DSN = %q, want %q", result.DSN, tc.wantDSN)
			}
		})
	}
}

func TestChooseDriverFromConfigOtherDrivers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		cfg        map[string]string
		wantDriver string
		wantDSN    string
	}{
		{
			name: "postgres with dsn",
			cfg: map[string]string{
				"db.driver": "postgres",
				"db.dsn":    "postgres://user:pass@localhost/db",
			},
			wantDriver: "postgres",
			wantDSN:    "postgres://user:pass@localhost/db",
		},
		{
			name: "mysql with dsn",
			cfg: map[string]string{
				"db.driver": "mysql",
				"db.dsn":    "user:pass@tcp(localhost:3306)/db",
			},
			wantDriver: "mysql",
			wantDSN:    "user:pass@tcp(localhost:3306)/db",
		},
		{
			name: "database.dsn fallback",
			cfg: map[string]string{
				"db.driver":    "postgres",
				"database.dsn": "postgres://localhost/app",
			},
			wantDriver: "postgres",
			wantDSN:    "postgres://localhost/app",
		},
		{
			name: "unknown driver with dsn",
			cfg: map[string]string{
				"db.driver": "cockroachdb",
				"db.dsn":    "postgresql://root@localhost:26257/db",
			},
			wantDriver: "cockroachdb",
			wantDSN:    "postgresql://root@localhost:26257/db",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := ChooseDriverFromConfig(tc.cfg)

			if result.Name != tc.wantDriver {
				t.Errorf("Name = %q, want %q", result.Name, tc.wantDriver)
			}
			if result.DSN != tc.wantDSN {
				t.Errorf("DSN = %q, want %q", result.DSN, tc.wantDSN)
			}
		})
	}
}

func TestChooseDriverFromConfigPriority(t *testing.T) {
	t.Parallel()

	// db.driver should take priority over database.driver
	cfg := map[string]string{
		"db.driver":       "sqlite3",
		"database.driver": "postgres",
		"db.path":         "/first.db",
		"database.path":   "/second.db",
	}

	result := ChooseDriverFromConfig(cfg)

	if result.Name != "sqlite3" {
		t.Errorf("Name = %q, want %q (db.driver should take priority)", result.Name, "sqlite3")
	}
	if result.DSN != "/first.db" {
		t.Errorf("DSN = %q, want %q (db.path should take priority)", result.DSN, "/first.db")
	}
}
