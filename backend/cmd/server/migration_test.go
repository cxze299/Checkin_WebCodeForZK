package main

import (
	"errors"
	"testing"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

func TestIsIgnorableMigrationError(t *testing.T) {
	for _, number := range []uint16{1060, 1061} {
		if !isIgnorableMigrationError(&mysqlDriver.MySQLError{Number: number}) {
			t.Fatalf("expected MySQL error %d to be retry-safe", number)
		}
	}
	if isIgnorableMigrationError(&mysqlDriver.MySQLError{Number: 1045}) {
		t.Fatal("access denied must not be ignored")
	}
	if isIgnorableMigrationError(errors.New("ordinary error")) {
		t.Fatal("ordinary errors must not be ignored")
	}
}
