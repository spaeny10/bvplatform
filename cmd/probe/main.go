package main

import (
	"context"
	"fmt"
	"os"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	pool, err := pgxpool.New(context.Background(), os.Args[1])
	if err != nil { fmt.Fprintf(os.Stderr, "connect: %v\n", err); os.Exit(1) }
	defer pool.Close()
	t1, _ := pool.Exec(context.Background(), "DELETE FROM active_alarms")
	t2, _ := pool.Exec(context.Background(), "DELETE FROM incidents")
	t3, _ := pool.Exec(context.Background(), "DELETE FROM security_events")
	t4, _ := pool.Exec(context.Background(), "DELETE FROM events")
	fmt.Printf("Cleared: %d alarms, %d incidents, %d security_events, %d events\n",
		t1.RowsAffected(), t2.RowsAffected(), t3.RowsAffected(), t4.RowsAffected())
}
