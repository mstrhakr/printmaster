package storage

import (
	"testing"

	"printmaster/common/config"
)

func TestNewStore_SQLite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     *config.DatabaseConfig
		wantErr bool
	}{
		{
			name: "nil config defaults to SQLite with memory db",
			cfg: &config.DatabaseConfig{
				Path: ":memory:",
			},
			wantErr: false,
		},
		{
			name: "explicit sqlite driver",
			cfg: &config.DatabaseConfig{
				Driver: "sqlite",
				Path:   ":memory:",
			},
			wantErr: false,
		},
		{
			name: "sqlite3 driver",
			cfg: &config.DatabaseConfig{
				Driver: "sqlite3",
				Path:   ":memory:",
			},
			wantErr: false,
		},
		{
			name: "modernc driver",
			cfg: &config.DatabaseConfig{
				Driver: "modernc",
				Path:   ":memory:",
			},
			wantErr: false,
		},
		{
			name: "modernc-sqlite driver",
			cfg: &config.DatabaseConfig{
				Driver: "modernc-sqlite",
				Path:   ":memory:",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store, err := NewStore(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewStore() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if store != nil {
				store.Close()
			}
		})
	}
}

func TestNewStore_UnsupportedDrivers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		driver string
	}{
		{"postgres", "postgres"},
		{"postgresql", "postgresql"},
		{"mysql", "mysql"},
		{"mariadb", "mariadb"},
		{"unknown", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &config.DatabaseConfig{
				Driver: tt.driver,
			}
			store, err := NewStore(cfg)
			if err == nil {
				if store != nil {
					store.Close()
				}
				t.Errorf("NewStore(%s) expected error, got nil", tt.driver)
			}
		})
	}
}
