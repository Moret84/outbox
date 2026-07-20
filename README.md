# outbox

Outbox runs shell commands for files dropped into configured directories. It is
intended for small, self-hosted upload and ingestion workflows on macOS and
Linux.

## Current scope

The first version provides a single `run-once` command. It scans each configured
directory, runs a shell command once for every matching regular file, and
reports a failure if any command fails.

Commands receive these environment variables:

- `FILE`: absolute path of the matched file.
- `DIRECTORY`: absolute path of the configured directory.
- `RULE`: rule name.

Commands run sequentially through `/bin/sh`. Outbox does not modify files in
this version: commands are responsible for deleting or moving files after a
successful operation.

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
    command: |
      curl --fail-with-body \
        -H "Authorization: Bearer $API_TOKEN" \
        -F "file=@$FILE" \
        "https://example.com/api/import"
```

Keep credentials in environment variables rather than in the configuration.

## Usage

```sh
go build -o outbox ./cmd/outbox
./outbox run-once --config outbox.yaml
```

Outbox exits with a non-zero status when the configuration is invalid, a
directory cannot be read, or at least one command fails. Other matching files
and rules are still processed after an individual failure.
