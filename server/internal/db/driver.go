package db

// DriverConfig contains a normalized driver name and DSN for opening a DB connection.
type DriverConfig struct {
	Name string // driver name to pass to database/sql (e.g. "sqlite3", "postgres")
	DSN  string // data source name / connection string
}

// ChooseDriverFromConfig maps a simple config map into a DriverConfig.
// This is intentionally lightweight and has no external imports so it won't
// break compilation. Real wiring to database/sql and drivers will be done
// by the calling code.
func ChooseDriverFromConfig(cfg map[string]string) DriverConfig {
	driver := cfg["db.driver"]
	if driver == "" {
		driver = cfg["database.driver"]
	}
	if driver == "" {
		// default to sqlite3 for single-node installs
		driver = "sqlite3"
	}

	switch driver {
	case "sqlite", "sqlite3", "modernc", "modernc-sqlite":
		dsn := cfg["db.path"]
		if dsn == "" {
			dsn = cfg["database.path"]
		}
		if dsn == "" {
			dsn = "printmaster.db"
		}
		return DriverConfig{Name: "sqlite3", DSN: dsn}
	default:
		// For other drivers, prefer an explicit dsn key
		dsn := cfg["db.dsn"]
		if dsn == "" {
			dsn = cfg["database.dsn"]
		}
		return DriverConfig{Name: driver, DSN: dsn}
	}
}
