# Load testing the publish API

A Python client that stands in for the Lightroom plug-in and a script that
runs a throwaway server to point it at. Nothing to install: Python 3.11+
standard library and `docker`.

The client speaks the same protocol as `lightroom-plugin/`: it creates
folders and albums, uploads JPEGs as multipart form data with the plug-in's
metadata fields, sets the photo order and cover, optionally re-publishes
some photos, waits for the background variant worker, and reads the photos
back the way a visitor would.

## Run one

```
cd tools/loadtest
./smugbox_loadtest.py --docker --photos ~/Pictures/export --albums 3 --workers 4
```

`--docker` builds the image (the first time; several minutes), starts a
container on port 8099 with a fresh volume, mints an API key, runs the load,
prints the report and removes the container again. Add `--keep-stack` to
leave it running.

To keep one server across several runs:

```
./testserver.sh up
eval "$(./testserver.sh env)"
./smugbox_loadtest.py --photos ~/Pictures/export --albums 3 --workers 8
./smugbox_loadtest.py --photos ~/Pictures/export --albums 1 --republish 1.0
./testserver.sh down
```

`testserver.sh up` writes the URL and key to `.testserver.env`; the client
reads that file when `--url`/`--api-key` and `SMUGBOX_URL`/`SMUGBOX_API_KEY`
are all unset. To load-test a server you already run, pass `--url` and an API
key from `smugbox admin create-api-key`.

## What comes out

```
phase       n   err  p50    p90    p99    max    req/s   MB/s
----------  --  ---  -----  -----  -----  -----  ------  -----
upload      10  0    146ms  173ms  175ms  175ms  25.1    79.3
republish   2   0    96ms   101ms  101ms  101ms  19.7    62.0
read        20  0    1ms    2ms    2ms    2ms    3478.4  301.4

background variants: 10/10 ready in 5.04s (1.98 photos/s) — complete
```

Latency percentiles and throughput per phase, a table of failures grouped by
the server's error code (`HTTP 415 not_jpeg`, …), and how long the variant
worker needed to catch up after the uploads were acknowledged. That last
number is the interesting one. Depending on the server, the display
variants are rendered either inside the upload request — the cost lands in
the `upload` row and the wait is close to zero — or by a background worker
after the upload has been acknowledged, which gives a fast `upload` row and
a slower drain. Either way the drain is what limits how soon visitors see a
freshly published album. `--json report.json` writes all of it for diffing
between runs.

The exit code is non-zero if any request failed or the variant wait timed
out, so a run works as a smoke test in CI.

## Useful options

| Option | Purpose |
|---|---|
| `--albums N`, `--photos-per-album M` | Size of the run. `M` defaults to one upload per source file, so `--albums 3` with 200 photos is 600 uploads. |
| `--workers N` | Concurrent uploads. The plug-in uses one; more than that tells you how the server behaves under a load it will not normally see. |
| `--shuffle` | Interleave uploads across albums instead of finishing one album at a time. |
| `--folder-depth N` | Nest the albums N folders deep, like publishing inside collection sets. |
| `--republish 0.25` | Re-upload a quarter of the photos with different bytes, exercising the replace path and the re-render of every variant. |
| `--password pw` | Protect the albums. Skips the variant wait and read-back, which use the visitor API. |
| `--cleanup` | Delete the albums and folders afterwards. |
| `--max-source-photos N` | Use only the first N files from `--photos`. |
| `--no-wait-variants`, `--variant-timeout S` | Control the wait for the background worker (default 900s). |
| `--read-variants thumb,small` | Which variants to fetch back; empty string skips the read phase. |
| `--seed N` | Reproducible metadata and ordering. |

`./smugbox_loadtest.py --help` lists the rest.

## Tuning the server under test

`testserver.sh` reads a few environment variables:

| Variable | Default | Purpose |
|---|---|---|
| `SMUGBOX_TEST_PORT` | `8099` | Host port. |
| `SMUGBOX_TEST_IMAGE` | `smugbox:loadtest` | Image tag to build and run. |
| `SMUGBOX_TEST_CPUS`, `SMUGBOX_TEST_MEMORY` | unset | `docker run` limits, e.g. `SMUGBOX_TEST_CPUS=1` to see what a small VPS does with the variant queue. |
| `MAX_UPLOAD_MB`, `LOG_LEVEL` | `100`, `info` | Passed to the server. |

`./testserver.sh logs -f` follows the server log and `./testserver.sh stats`
shows the container's CPU and memory while a run is in flight.

## Notes

- Source photos are used read-only. Each upload gets a unique
  `lr_photo_uuid`, so re-using the same file across albums creates separate
  photos rather than updating one.
- `taken_at` comes from the file's modification time and the title, caption,
  keywords and EXIF fields are synthesised; the point is to fill the same
  columns the plug-in fills, not to preserve your metadata.
- The container and its volume are removed by `down`, so a run never leaves
  state behind. Nothing here touches a production server unless you point
  `--url` at one.
