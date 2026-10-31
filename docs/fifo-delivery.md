# Experimental FIFO delivery

CatTV can run a local delivery server whose endpoints are a POSIX FIFO and,
optionally, a Unix domain socket:

```console
cattv fifo serve
cat ~/.cache/cat.fifo
```

The server uses the existing metadata catalog, on-demand HTTP downloader,
image decoder, and terminal renderers. It does not cache image pixels. The
initial default renderer is Kitty Graphics, intended for Ghostty and other
Kitty-compatible terminals. Set `--renderer ansi` or `--renderer iterm2` when
needed.

## What is guaranteed

For a **single reader at a time**, the implementation is designed so that:

1. The server waits for a reader before starting a network request.
2. It renders one complete frame into memory.
3. It writes the frame and closes the writer.
4. That reader observes EOF.
5. The next reader starts a new selection and download.

The server never opens the FIFO read-write. It opens only a blocking write end
per frame, plus a read end when canceling a blocked open or cleaning up. After a
reader connects, CatTV rotates the public FIFO path to a fresh, `0600` FIFO
inode before rendering. The current reader remains attached to the old inode, so
closing the old writer produces EOF even if the next `cat` has already opened
the new path. This avoids a common FIFO race where immediately reopening the
same inode prevents a previous reader from seeing EOF.

## What is not guaranteed

POSIX FIFOs are byte streams. They provide no messages, reader identity, request
framing, or per-reader delivery.

- **Concurrent readers share one stream.** If two processes have the FIFO open
  at the same time, the kernel may deliver the frame to one reader, split it
  arbitrarily between readers, or give one reader nothing. Both `cat`
  invocations can exit successfully. CatTV cannot detect reader count
  portably, and `cat` cannot send a request or acknowledge a frame.
- **A download can be wasted if the reader disconnects.** A closed reader is
  observed as `EPIPE`/`ECONNRESET` when the server writes. The server logs this,
  closes the writer, and continues for the next reader, but cannot cancel a
  download earlier.
- **`cat` cannot report an image failure.** On a network or catalog failure the
  server writes a short human-readable error line to the FIFO. `cat` exits
  successfully because it merely copied bytes; the process exit status does not
  tell you whether an image was displayed.
- **Terminal geometry and renderer are fixed at server start.** The FIFO has no
  way for `cat` to report its terminal size or capabilities. Use `--width`,
  `--height`, `--renderer`, and the `CATTV_CELL_*` environment variables as
  needed. The default Kitty choice assumes a compatible terminal; it is not an
  automatic per-reader detection.
- **Graceful shutdown has a tiny reader race.** Cleanup removes the path and
  wakes a currently waiting reader. A reader that opens the path in the exact
  cleanup window can still be left blocked after the server exits.

Therefore the exact semantics "every successful concurrent `cat` invocation
receives exactly one complete image" are **not achievable reliably with a plain
FIFO**. They work for normal sequential use when the network and terminal are
healthy, but CatTV does not claim a guarantee.

## Strict Unix socket transport

Because message boundaries are impossible in the FIFO, `cattv fifo serve` also
listens on a user-only Unix domain socket by default:

```console
cattv fifo client
```

This uses one HTTP `GET /cat` request per connection. The response is
`application/x-cattv-terminal-frame` with an exact `Content-Length`. Each client
therefore receives either one complete rendered frame or an HTTP error. Multiple
concurrent clients receive independent responses. The body is still the raw
terminal escape sequence expected by the configured renderer; the socket adds
framing, not a new image format.

Disable the socket endpoint with `--no-socket`:

```console
cattv fifo serve --no-socket
```

Custom endpoints:

```console
cattv fifo serve --fifo /tmp/cat.fifo --socket /tmp/cat.sock
cat /tmp/cat.fifo
cattv fifo client --socket /tmp/cat.sock
```

## Endpoint lifecycle and security

- Default paths are `~/.cache/cat.fifo` and `~/.cache/cat.sock`, overridable
  with `--fifo`, `--socket`, `CATTV_FIFO`, or `CATTV_SOCKET`.
- The parent directory is created with `0700` when absent. The FIFO, socket, and
  lock files are secured to `0600` and checked for current-user ownership.
- CatTV refuses to replace an existing non-FIFO object at the FIFO path and a
  non-socket object at the socket path.
- Stale FIFOs are reused securely; stale sockets are removed and recreated under
  the server lock.
- A `flock`-based lock file (`<endpoint>.lock`) prevents two delivery servers
  from owning the same endpoint. Lock files may remain on disk and are harmless:
  exclusion is the kernel lock, not the file's existence.
- The FIFO path is atomically rotated to a new inode after each reader connects.
  Readers already attached to the previous inode see EOF when its writer closes.
- A SIGPIPE handler is disabled by the server so reader disconnects are handled
  as write errors rather than terminating the process.
- The server removes its FIFO and socket paths during graceful shutdown.
- Ctrl-C stops both endpoints, cancels an in-progress generation (the downloader
  and HTTP request contexts are canceled), and removes the paths.

## Testing strategy

The delivery package uses deterministic synchronization channels rather than
sleeps:

- Sequential FIFO reads assert two complete, distinct frames and mode `0600`.
- A counting generator asserts that no download starts before a reader opens the
  FIFO.
- A concurrent-reader test attaches two readers to one FIFO inode, releases one
  frame, and verifies that the bytes cannot add up to two independent images.
- A disconnect test closes the first reader before release and verifies the
  server survives and serves a second reader.
- Shutdown, stale-path, non-FIFO replacement, and second-server tests cover the
  lifecycle.
- Strict socket tests run eight concurrent clients and verify eight unique,
  complete responses, plus binary payload equality and error propagation.

## Commands

```text
cattv fifo serve [--fifo PATH] [--socket PATH] [--no-socket] [display flags]
cattv fifo client [--socket PATH] [--timeout 30s]
cattv fifo path [--fifo PATH] [--socket PATH]
```

The existing `cattv`, `cattv show`, and `cattv local` commands are unchanged.
