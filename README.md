# outbox

Outbox runs shell commands for files dropped into configured directories. It is
intended for small, self-hosted upload and ingestion workflows on macOS and
Linux.

## How it works

Outbox scans each configured directory and runs a shell command once for every
matching regular file. `run` repeats the scan after the configured interval;
`run-once` performs one scan for use with an external scheduler.

Commands receive these environment variables:

- `FILE`: absolute path of the matched file.
- `DIRECTORY`: absolute path of the configured directory.
- `RULE`: rule name.

Commands run sequentially through `/bin/sh`. After a command exits successfully,
Outbox can keep, delete, or archive its file. A failed command leaves the file
queued so a later scan can retry it. For `delete` and `archive`, Outbox first
moves each file atomically into a hidden `.outbox-processing` directory. This
prevents a newly arrived file with the same name from being modified by the
previous file's success action and preserves pending work across restarts. When
archiving, existing files are not overwritten: Outbox adds a numeric suffix to
the new archive name.

Outbox takes a non-blocking lock for each configuration file. A second process
using the same configuration exits with an error instead of processing the
same files concurrently. The operating system releases the lock if the process
stops unexpectedly.

Rule `name` values are stable queue identifiers. Do not rename a rule while it
has pending files under `.outbox-processing`, or restore those files to the
watched directory before renaming it.

## Configuration

Copy `config.example.yaml` to `outbox.yaml` and adapt the rules. Relative
directories are resolved from the configuration file location, and paths
starting with `~/` use the current user's home directory.

```yaml
rules:
  - name: example
    directory: ~/Outbox/example
    patterns:
      - "*.csv"
    minimum_age: 10s
    command: |
      curl --fail-with-body \
        -H "Authorization: Bearer $API_TOKEN" \
        -F "file=@$FILE" \
        "https://example.com/api/import"
    on_success: archive
    archive_directory: ~/Outbox/archive/example
```

Keep credentials in environment variables rather than in the configuration.
`interval` defaults to `30s`, `minimum_age` defaults to `0s`, and `on_success`
defaults to `keep`. Durations use Go syntax such as `500ms`, `30s`, or `2m`.

Available success actions are:

- `keep`: leave lifecycle management to the command; an unchanged file is
  processed again on the next scan.
- `delete`: remove the file after a successful command.
- `archive`: move it to the required `archive_directory`.

The archive directory must be on the same filesystem as the watched directory.

## Adding files safely

Outbox considers a matching file ready once its optional `minimum_age` has
elapsed. File age is only a delay, not proof that a writer has finished. Write
new files outside the configured directory, close them, then rename them into
the directory. The rename must stay on the same filesystem to be atomic. Do not
copy or write large files directly into a directory scanned by Outbox, as a scan
could observe them before the write completes.

## Usage

```sh
go build -o outbox ./cmd/outbox
./outbox run --config outbox.yaml
./outbox run-once --config outbox.yaml
```

The continuous command scans immediately, waits for the configured interval
after each completed scan, and stops cleanly on `SIGINT` or `SIGTERM`. Scan and
command errors are reported and retried. `run-once` exits with a non-zero status
when the configuration is invalid, a directory cannot be read, or at least one
command fails. Other matching files and rules are still processed after an
individual failure.
