package mysql

import (
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
	"time"
)

type DBService struct {
	dbPool *sql.DB
}

func NewDBService(dsn string, maxOpenConns int, maxIdleConns int, connMaxLifetime time.Duration) *DBService {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		panic(err.Error())
	}
	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleConns)
	db.SetConnMaxLifetime(connMaxLifetime)
	return &DBService{db}
}

func (dbs *DBService) QueryWithFetch(action func(values [][]byte), query string, args ...interface{}) {
	// Execute the query
	rows, err := dbs.dbPool.Query(query, args...)
	if err != nil {
		panic(err.Error()) // proper error handling instead of panic in your app
	}

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		panic(err.Error()) // proper error handling instead of panic in your app
	}

	// Make a slice for the values
	values := make([][]byte, len(columns))

	// rows.Scan wants '[]interface{}' as an argument, so we must copy the
	// references into such a slice
	// See http://code.google.com/p/go-wiki/wiki/InterfaceSlice for details
	scanArgs := make([]interface{}, len(values))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	// Fetch rows
	for rows.Next() {
		// get RawBytes from data
		err = rows.Scan(scanArgs...)
		if err != nil {
			panic(err.Error()) // proper error handling instead of panic in your app
		}
		action(values)
	}
	if err = rows.Err(); err != nil {
		panic(err.Error()) // proper error handling instead of panic in your app
	}
}

func (dbs *DBService) Close() error {
	return dbs.dbPool.Close()
}
