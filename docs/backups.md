# Local SQLite snapshots

Create a consistent snapshot of a specific Summa SQLite database with:

```sh
go run ./cmd/summa-backup -source data/northstar.db -output /secure/path/summa-backup.db
```

The destination must not already exist. The tool uses SQLite `VACUUM INTO`,
checks the snapshot's integrity and foreign keys, and only publishes it after
those checks pass. It does not modify the source or automatically choose the
database the application is using. Select the path from the app's startup log.

To verify recovery, copy the backup to a new path, start Summa against that
copy using `SUMMA_DB`, and compare the books with the original. Do not replace
an active database or discard its WAL files. Store snapshots securely: this
command does not encrypt them, schedule backups, copy them offsite, or monitor
backup and restore failures. Those controls are required before relying on it
for real client data.
