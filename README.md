# outbox

Outbox watches directories and runs a shell command for each file dropped into
them. One directory means one destination: move a file in, and it gets sent.

It is meant for small, self-hosted workflows on macOS and Linux — the kind of
automation you would otherwise wire up with a cron job and a pile of shell
scripts, or pay a hosted service like Zapier or Make to do for you. Outbox runs
locally: no account, no cloud, no scheduler to configure.

Typical uses:

- Drop a downloaded invoice into `~/Outbox/documents` and let it upload itself
  to your Paperless instance.
- Keep one directory per bank, and have each statement export pushed to your
  accounting tool as soon as you save it there.

## Example

```yaml
interval: 30s

rules:
  - name: paperless
    directory: ~/Outbox/documents
    patterns:
      - "*.pdf"
    command: |
      curl --fail-with-body --retry 3 \
        -H "Authorization: Token $PAPERLESS_TOKEN" \
        -F "document=@$FILE" \
        "$PAPERLESS_URL/api/documents/post_document/" \
        && rm "$FILE"
```

Adapt the endpoint and credentials to your own setup, then run
`outbox run --config outbox.yaml`. See [Configuration](#configuration) for the
full format.

## Installation

Prebuilt binaries are published for each release. On an Apple Silicon Mac:

```sh
mkdir -p "$HOME/.local/bin"
curl -fL \
  https://github.com/Moret84/outbox/releases/latest/download/outbox_darwin_arm64 \
  -o "$HOME/.local/bin/outbox"
chmod +x "$HOME/.local/bin/outbox"
```

Other release assets are available for macOS Intel (`outbox_darwin_amd64`),
Linux x86-64 (`outbox_linux_amd64`), and Linux ARM64
(`outbox_linux_arm64`). Each release also includes `checksums.txt` for SHA-256
verification.

To install from source instead:

```sh
go install github.com/Moret84/outbox/cmd/outbox@latest
```

## Usage

```sh
outbox run --config outbox.yaml
outbox run-once --config outbox.yaml
```

Continuous mode reports individual scan errors and retries on the next pass. It
stops on `SIGINT` or `SIGTERM` and releases the configuration lock.

`run-once` exits with a non-zero status when the configuration is invalid, a
directory cannot be read, or at least one command fails. Other matching files
and rules are still processed after an individual failure.

## Configuration

Copy `config.example.yaml` to `outbox.yaml` and adapt the rules. Relative
directories are resolved from the configuration file location, and paths
starting with `~/` use the current user's home directory.

```yaml
interval: 30s

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

## How it works

`run-once` scans the configured directories once. `run` scans immediately and
then repeats after the configured interval, which defaults to 30 seconds.

Commands receive these environment variables:

- `FILE`: absolute path of the matched file.
- `DIRECTORY`: absolute path of the configured directory.
- `RULE`: rule name.

Commands run sequentially through `/bin/sh`. Outbox does not modify files in
this version: commands are responsible for deleting or moving files after a
successful operation.

Outbox takes a non-blocking lock for each configuration file. A second process
using the same configuration exits with an error instead of processing the
same files concurrently. The operating system releases the lock if the process
stops unexpectedly.

## Adding files safely

Outbox considers every matching file immediately ready for processing. Write
new files outside the configured directory, close them, then rename them into
the directory. The rename must stay on the same filesystem to be atomic. Do not
copy or write large files directly into a directory scanned by Outbox, as a scan
could observe them before the write completes.
