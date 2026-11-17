package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"summa/internal/backup"
)

func main() {
	source := flag.String("source", "", "path to the SQLite database")
	destination := flag.String("output", "", "path for a new backup file")
	flag.Parse()
	if *source == "" || *destination == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: summa-backup -source DATABASE -output NEW_BACKUP_FILE")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := backup.Create(ctx, *source, *destination); err != nil {
		fmt.Fprintln(os.Stderr, "backup failed:", err)
		os.Exit(1)
	}
	fmt.Println("Backup created:", *destination)
}
