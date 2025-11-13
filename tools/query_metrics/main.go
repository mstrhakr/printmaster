package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"

	_ "modernc.org/sqlite"
)

func main() {
	dbPath := flag.String("db", "", "Path to SQLite DB file")
	serial := flag.String("serial", "", "Device serial to query")
	limit := flag.Int("limit", 1000, "Max rows to return")
	listTables := flag.Bool("list", false, "List tables in the DB")
	schemaTable := flag.String("schema", "", "Show PRAGMA table_info for given table")
	rawQuery := flag.String("query", "", "Run arbitrary SQL query (returns rows)")
	flag.Parse()

	if *dbPath == "" {
		log.Fatalf("Usage: query_metrics -db <path> [-serial <serial>] [-list] [-schema <table>] [-query <sql>] [-limit N]")
	}

	db, err := sql.Open("sqlite", *dbPath+"?_foreign_keys=ON")
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	// If list flag provided, list tables and exit
	if *listTables {
		rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name;")
		if err != nil {
			log.Fatalf("failed to list tables: %v", err)
		}
		defer rows.Close()
		fmt.Println("Tables:")
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				log.Fatalf("scan failed: %v", err)
			}
			fmt.Println(" -", name)
		}
		return
	}

	// If schema flag provided, show PRAGMA table_info
	if *schemaTable != "" {
		q := fmt.Sprintf("PRAGMA table_info(%s);", *schemaTable)
		rows, err := db.Query(q)
		if err != nil {
			log.Fatalf("failed to run pragma: %v", err)
		}
		defer rows.Close()
		fmt.Printf("Schema for %s:\n", *schemaTable)
		for rows.Next() {
			var cid int
			var name string
			var ctype string
			var notnull int
			var dflt sql.NullString
			var pk int
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
				log.Fatalf("scan failed: %v", err)
			}
			fmt.Printf("%d | %s | %s | notnull=%d | pk=%d | default=%v\n", cid, name, ctype, notnull, pk, dflt.String)
		}
		return
	}

	// If rawQuery provided, run arbitrary SQL and print rows generically
	if *rawQuery != "" {
		rows, err := db.Query(*rawQuery)
		if err != nil {
			log.Fatalf("query failed: %v", err)
		}
		defer rows.Close()
		cols, _ := rows.Columns()
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		for rows.Next() {
			if err := rows.Scan(ptrs...); err != nil {
				log.Fatalf("scan failed: %v", err)
			}
			for i, c := range cols {
				fmt.Printf("%s=%v ", c, vals[i])
			}
			fmt.Println()
		}
		return
	}

	// Default: if serial provided, attempt to query common metrics tables
	if *serial == "" {
		log.Fatalf("No serial provided and no other action requested")
	}

	// Try common table names in order
	candidates := []string{"metrics_history", "metric_raw", "metrics_raw", "metric_raw_v1", "metrics_history_raw"}
	found := false
	for _, t := range candidates {
		q := fmt.Sprintf("SELECT id, serial, timestamp FROM %s WHERE serial = ? ORDER BY timestamp ASC LIMIT ?;", t)
		rows, err := db.Query(q, *serial, *limit)
		if err != nil {
			// skip table not exists errors
			continue
		}
		defer rows.Close()
		fmt.Printf("Results from table %s:\n", t)
		var count int
		for rows.Next() {
			var id int64
			var ser string
			var ts string
			if err := rows.Scan(&id, &ser, &ts); err != nil {
				log.Fatalf("row scan failed: %v", err)
			}
			count++
			fmt.Printf("%3d | id=%d | ts=%s\n", count, id, ts)
		}
		if count > 0 {
			found = true
			fmt.Printf("Total rows in %s: %d\n", t, count)
			break
		}
	}
	if !found {
		fmt.Println("No metrics rows found in known tables for serial", *serial)
	}
}

func nullInt(n sql.NullInt64) interface{} {
	if n.Valid {
		return n.Int64
	}
	return nil
}
