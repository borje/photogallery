#!/usr/bin/env python3
"""Load-test client for the Smugbox publish API.

Speaks the same protocol as the Lightroom Classic plug-in: it creates albums
(optionally inside a folder tree), uploads JPEGs from a directory as
multipart form data with the plug-in's metadata fields, sets the photo order
and cover, optionally re-publishes a fraction of them, then waits for the
background variant worker to catch up and reads the photos back as a visitor
would.

Point it at a throwaway server started by ./testserver.sh:

    ./testserver.sh up
    eval "$(./testserver.sh env)"
    ./smugbox_loadtest.py --photos ~/Pictures/export --albums 3 --workers 4

or let it manage the stack itself:

    ./smugbox_loadtest.py --docker --photos ~/Pictures/export

Standard library only; no packages to install.
"""

from __future__ import annotations

import argparse
import json
import os
import random
import ssl
import statistics
import subprocess
import sys
import threading
import time
import uuid
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass, field
from datetime import datetime
from http.client import HTTPConnection, HTTPSConnection
from pathlib import Path
from urllib.parse import urlsplit

HERE = Path(__file__).resolve().parent
STATE_FILE = HERE / ".testserver.env"
TESTSERVER = HERE / "testserver.sh"

CHUNK = 256 * 1024
PHOTO_SUFFIXES = {".jpg", ".jpeg", ".JPG", ".JPEG"}
USER_AGENT = "smugbox-loadtest/1.0"

# Mirrors PublishTask.metadataFields: the plug-in sends these exif keys.
EXIF_SAMPLE = {
    "make": "Canon",
    "model": "Canon EOS R6",
    "lens": "RF24-105mm F4 L IS USM",
    "exposure": "1/250 sec at f/5.6",
    "focal_length": "50 mm",
    "iso": "400",
    "aperture": "f/5.6",
    "shutter_speed": "1/250 sec",
}
KEYWORD_POOL = [
    "landscape", "portrait", "travel", "family", "autumn", "winter",
    "sweden", "norway", "golden hour", "black and white",
]


# --------------------------------------------------------------------------
# Measurements


@dataclass
class Sample:
    phase: str
    ok: bool
    status: int
    elapsed: float
    nbytes: int = 0
    error: str = ""


class Recorder:
    """Thread-safe sample collection plus live counters for the progress line."""

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self.samples: list[Sample] = []
        self.done = 0
        self.failed = 0
        self.bytes = 0

    def add(self, s: Sample) -> None:
        with self._lock:
            self.samples.append(s)
            self.done += 1
            self.bytes += s.nbytes
            if not s.ok:
                self.failed += 1

    def snapshot(self) -> tuple[int, int, int]:
        with self._lock:
            return self.done, self.failed, self.bytes

    def reset_counters(self) -> None:
        with self._lock:
            self.done = self.failed = self.bytes = 0

    def of_phase(self, phase: str) -> list[Sample]:
        with self._lock:
            return [s for s in self.samples if s.phase == phase]

    def phases(self) -> list[str]:
        seen: list[str] = []
        with self._lock:
            for s in self.samples:
                if s.phase not in seen:
                    seen.append(s.phase)
        return seen


class Progress:
    """One rewriting status line per phase, on stderr."""

    def __init__(self, rec: Recorder, quiet: bool) -> None:
        self.rec = rec
        self.quiet = quiet
        self.tty = sys.stderr.isatty()
        self._stop = threading.Event()
        self._thread: threading.Thread | None = None
        self.label = ""
        self.total = 0
        self.started = 0.0

    def start(self, label: str, total: int) -> None:
        self.label, self.total = label, total
        self.rec.reset_counters()
        self.started = time.perf_counter()
        if self.quiet or not self.tty:
            return
        self._stop.clear()
        self._thread = threading.Thread(target=self._loop, daemon=True)
        self._thread.start()

    def _loop(self) -> None:
        while not self._stop.wait(0.4):
            self._print(end="\r")

    def _print(self, end: str = "\n") -> None:
        done, failed, nbytes = self.rec.snapshot()
        dt = max(time.perf_counter() - self.started, 1e-9)
        line = f"  {self.label}: {done}/{self.total}"
        if failed:
            line += f" failed={failed}"
        line += f"  {done / dt:6.1f} req/s"
        if nbytes:
            line += f"  {nbytes / dt / 1e6:6.1f} MB/s"
        line += f"  {dt:5.1f}s"
        sys.stderr.write(line.ljust(78) + end)
        sys.stderr.flush()

    def stop(self) -> float:
        elapsed = time.perf_counter() - self.started
        if self._thread is not None:
            self._stop.set()
            self._thread.join()
            self._thread = None
        if not self.quiet:
            self._print()
        return elapsed


# --------------------------------------------------------------------------
# HTTP


@dataclass
class Response:
    status: int
    data: bytes
    elapsed: float
    error: str = ""

    @property
    def ok(self) -> bool:
        return not self.error and 200 <= self.status < 300

    def json(self) -> dict:
        try:
            out = json.loads(self.data.decode("utf-8"))
            return out if isinstance(out, dict) else {}
        except Exception:
            return {}

    def failure(self) -> str:
        """Short label for the error table."""
        if self.error:
            return self.error
        code = self.json().get("error")
        return f"HTTP {self.status} {code}" if code else f"HTTP {self.status}"


class Client:
    """One keep-alive connection per thread."""

    def __init__(self, base_url: str, api_key: str, timeout: float, insecure: bool) -> None:
        parts = urlsplit(base_url)
        if parts.scheme not in ("http", "https"):
            raise SystemExit(f"unsupported URL scheme in {base_url!r}")
        self.https = parts.scheme == "https"
        self.host = parts.hostname or "127.0.0.1"
        self.port = parts.port or (443 if self.https else 80)
        self.prefix = parts.path.rstrip("/")
        self.api_key = api_key
        self.timeout = timeout
        self.ctx: ssl.SSLContext | None = None
        if self.https:
            self.ctx = ssl._create_unverified_context() if insecure else ssl.create_default_context()
        self._local = threading.local()

    def _connection(self) -> tuple[HTTPConnection, bool]:
        conn = getattr(self._local, "conn", None)
        if conn is not None:
            return conn, False
        if self.https:
            conn = HTTPSConnection(self.host, self.port, timeout=self.timeout, context=self.ctx)
        else:
            conn = HTTPConnection(self.host, self.port, timeout=self.timeout)
        self._local.conn = conn
        return conn, True

    def _drop(self) -> None:
        conn = getattr(self._local, "conn", None)
        if conn is not None:
            try:
                conn.close()
            except Exception:
                pass
            self._local.conn = None

    def request(
        self,
        method: str,
        path: str,
        *,
        body=None,
        body_len: int | None = None,
        content_type: str | None = None,
        auth: bool = True,
    ) -> Response:
        """`body` is bytes or a zero-argument factory returning an iterator."""
        headers = {
            "Accept": "application/json",
            "User-Agent": USER_AGENT,
            "Connection": "keep-alive",
        }
        if auth and self.api_key:
            headers["Authorization"] = "Bearer " + self.api_key
        if content_type:
            headers["Content-Type"] = content_type
        if body_len is not None:
            headers["Content-Length"] = str(body_len)
        elif body is None:
            headers["Content-Length"] = "0"

        url = self.prefix + path
        start = time.perf_counter()
        last = ""
        for attempt in (0, 1):
            conn, fresh = self._connection()
            try:
                payload = body() if callable(body) else body
                conn.request(method, url, body=payload, headers=headers)
                resp = conn.getresponse()
                data = resp.read()
                return Response(resp.status, data, time.perf_counter() - start)
            except Exception as exc:  # noqa: BLE001 - every failure is a data point
                self._drop()
                last = f"{type(exc).__name__}: {exc}"
                # A reused connection can be closed by the server between
                # requests; that is not a load-test result, so try once more.
                if attempt == 0 and not fresh:
                    continue
                return Response(0, b"", time.perf_counter() - start, error=last)
        return Response(0, b"", time.perf_counter() - start, error=last)

    def json_request(self, method: str, path: str, payload: dict | None = None) -> Response:
        body = json.dumps(payload).encode("utf-8") if payload is not None else None
        return self.request(
            method, path, body=body,
            body_len=len(body) if body is not None else None,
            content_type="application/json" if body is not None else None,
        )


def multipart(fields: dict[str, str], file_path: Path | None, file_name: str = "", file_size: int = 0):
    """Build a streaming multipart body: (content_type, length, body_factory).

    The file is read in chunks while it is sent, so a run with many workers
    and 30 MB originals does not hold them all in memory.
    """
    boundary = "----SmugboxLoadTest" + uuid.uuid4().hex
    marker = b"--" + boundary.encode()
    pre = bytearray()
    for key, value in fields.items():
        pre += marker + b"\r\n"
        pre += f'Content-Disposition: form-data; name="{key}"\r\n\r\n'.encode()
        pre += str(value).encode("utf-8") + b"\r\n"
    head = b""
    tail = marker + b"--\r\n"
    if file_path is not None:
        safe = file_name.replace('"', "_").replace("\r", "").replace("\n", "")
        head = (
            marker + b"\r\n"
            + f'Content-Disposition: form-data; name="file"; filename="{safe}"\r\n'.encode()
            + b"Content-Type: image/jpeg\r\n\r\n"
        )
        tail = b"\r\n" + tail
    length = len(pre) + len(head) + file_size + len(tail)
    frozen = bytes(pre)

    def factory():
        def gen():
            yield frozen
            if file_path is not None:
                yield head
                with open(file_path, "rb") as fh:
                    while True:
                        block = fh.read(CHUNK)
                        if not block:
                            break
                        yield block
            yield tail
        return gen()

    return "multipart/form-data; boundary=" + boundary, length, factory


# --------------------------------------------------------------------------
# Plan


@dataclass
class PhotoTask:
    album_index: int
    seq: int
    path: Path
    size: int
    lr_uuid: str
    photo_id: str = ""


@dataclass
class AlbumPlan:
    index: int
    name: str
    parent_id: str = ""
    album_id: str = ""
    slug: str = ""
    tasks: list[PhotoTask] = field(default_factory=list)

    @property
    def uploaded(self) -> list[PhotoTask]:
        return [t for t in self.tasks if t.photo_id]


def find_photos(root: Path, limit: int) -> list[Path]:
    if not root.is_dir():
        raise SystemExit(f"--photos: {root} is not a directory")
    found = sorted(p for p in root.rglob("*") if p.suffix in PHOTO_SUFFIXES and p.is_file())
    if not found:
        raise SystemExit(f"--photos: no .jpg/.jpeg files under {root}")
    return found[:limit] if limit > 0 else found


def metadata_fields(task: PhotoTask, album: AlbumPlan, rng: random.Random) -> dict[str, str]:
    """The same form fields the plug-in sends (PublishTask.metadataFields)."""
    taken = datetime.fromtimestamp(task.path.stat().st_mtime).strftime("%Y-%m-%dT%H:%M:%S")
    keywords = rng.sample(KEYWORD_POOL, k=rng.randint(1, 4))
    return {
        "lr_photo_uuid": task.lr_uuid,
        "filename": f"{task.path.stem}-{task.seq:05d}{task.path.suffix}",
        "title": f"{album.name} #{task.seq}",
        "caption": f"Uploaded by smugbox-loadtest from {task.path.name}.",
        "keywords": json.dumps(keywords),
        "taken_at": taken,
        "exif": json.dumps(EXIF_SAMPLE),
    }


# --------------------------------------------------------------------------
# Phases


class LoadTest:
    def __init__(self, args, client: Client) -> None:
        self.args = args
        self.client = client
        self.rec = Recorder()
        self.progress = Progress(self.rec, args.quiet)
        self.rng = random.Random(args.seed)
        self.run_id = uuid.uuid4().hex[:8]
        self.pool = find_photos(Path(args.photos).expanduser(), args.max_source_photos)
        self.folders: list[str] = []
        self.albums: list[AlbumPlan] = []
        self.phase_times: dict[str, float] = {}
        self.variant_wait: dict = {}

    # -- helpers ---------------------------------------------------------

    def record(self, phase: str, resp: Response, nbytes: int = 0) -> bool:
        self.rec.add(Sample(
            phase=phase, ok=resp.ok, status=resp.status, elapsed=resp.elapsed,
            nbytes=nbytes if resp.ok else 0,
            error="" if resp.ok else resp.failure(),
        ))
        return resp.ok

    def run_phase(
        self,
        label: str,
        items: list,
        fn,
        workers: int | None = None,
        phases: tuple[str, ...] = (),
        requests_per_item: int = 1,
    ) -> None:
        if not items:
            return
        self.progress.start(label, len(items) * requests_per_item)
        n = workers if workers is not None else self.args.workers
        if n <= 1:
            for item in items:
                fn(item)
        else:
            with ThreadPoolExecutor(max_workers=n) as pool:
                list(pool.map(fn, items))
        elapsed = self.progress.stop()
        for phase in phases or (label,):
            self.phase_times[phase] = elapsed

    # -- plan ------------------------------------------------------------

    def build_plan(self) -> None:
        per_album = self.args.photos_per_album or len(self.pool)
        cursor = 0
        for i in range(self.args.albums):
            album = AlbumPlan(index=i, name=f"Load test {self.run_id} album {i + 1}")
            for seq in range(per_album):
                path = self.pool[cursor % len(self.pool)]
                cursor += 1
                album.tasks.append(PhotoTask(
                    album_index=i, seq=seq, path=path, size=path.stat().st_size,
                    lr_uuid=f"lt-{self.run_id}-{i}-{seq}",
                ))
            self.albums.append(album)

    def total_bytes(self) -> int:
        return sum(t.size for a in self.albums for t in a.tasks)

    # -- phases ----------------------------------------------------------

    def create_folders(self) -> None:
        depth = self.args.folder_depth
        if depth <= 0:
            return
        self.progress.start("folders", depth)
        parent = ""
        for level in range(depth):
            payload = {
                "name": f"Load test {self.run_id} level {level + 1}",
                "parent_id": parent,
                "idempotency_key": f"lt-{self.run_id}-folder-{level}",
            }
            resp = self.client.json_request("POST", "/api/publish/folders", payload)
            if not self.record("folders", resp):
                self.phase_times["folders"] = self.progress.stop()
                raise SystemExit(f"folder create failed: {resp.failure()} {resp.data[:200]!r}")
            parent = resp.json()["id"]
            self.folders.append(parent)
        self.phase_times["folders"] = self.progress.stop()
        for album in self.albums:
            album.parent_id = parent

    def create_albums(self) -> None:
        def create(album: AlbumPlan) -> None:
            payload = {
                "name": album.name,
                "description": f"smugbox-loadtest run {self.run_id}",
                "parent_id": album.parent_id,
                "idempotency_key": f"lt-{self.run_id}-album-{album.index}",
            }
            if self.args.password:
                payload["password"] = self.args.password
            resp = self.client.json_request("POST", "/api/publish/albums", payload)
            if self.record("albums", resp):
                body = resp.json()
                album.album_id, album.slug = body["id"], body["slug"]

        self.run_phase("albums", self.albums, create, workers=min(self.args.workers, 4))
        missing = [a for a in self.albums if not a.album_id]
        if missing:
            raise SystemExit(f"{len(missing)}/{len(self.albums)} albums could not be created")

    def upload(self) -> None:
        tasks = [t for a in self.albums for t in a.tasks]
        if self.args.shuffle:
            self.rng.shuffle(tasks)

        def upload_one(task: PhotoTask) -> None:
            album = self.albums[task.album_index]
            fields = metadata_fields(task, album, self.rng)
            ctype, length, body = multipart(fields, task.path, fields["filename"], task.size)
            resp = self.client.request(
                "POST", f"/api/publish/albums/{album.album_id}/photos",
                body=body, body_len=length, content_type=ctype,
            )
            if self.record("upload", resp, nbytes=length):
                task.photo_id = resp.json().get("id", "")

        self.run_phase("upload", tasks, upload_one)

    def set_order_and_cover(self) -> None:
        if self.args.skip_order:
            return

        def finish(album: AlbumPlan) -> None:
            ids = [t.photo_id for t in album.uploaded]
            if not ids:
                return
            order = list(ids)
            self.rng.shuffle(order)
            self.record("order", self.client.json_request(
                "PUT", f"/api/publish/albums/{album.album_id}/order", {"photo_ids": order}))
            self.record("cover", self.client.json_request(
                "PUT", f"/api/publish/albums/{album.album_id}", {"cover_photo_id": ids[0]}))

        self.run_phase("order+cover", self.albums, finish, workers=min(self.args.workers, 4),
                       phases=("order", "cover"), requests_per_item=2)

    def republish(self) -> None:
        fraction = self.args.republish
        if fraction <= 0:
            return
        uploaded = [t for a in self.albums for t in a.uploaded]
        count = max(1, round(len(uploaded) * fraction)) if uploaded else 0
        picked = self.rng.sample(uploaded, k=min(count, len(uploaded)))

        def replace(task: PhotoTask) -> None:
            album = self.albums[task.album_index]
            # A different source file, so the content hash changes and the
            # server has to regenerate every variant.
            path = self.pool[(self.pool.index(task.path) + 1) % len(self.pool)]
            size = path.stat().st_size
            fields = metadata_fields(task, album, self.rng)
            fields["title"] += " (republished)"
            ctype, length, body = multipart(fields, path, fields["filename"], size)
            resp = self.client.request(
                "PUT", f"/api/publish/albums/{album.album_id}/photos/{task.photo_id}",
                body=body, body_len=length, content_type=ctype,
            )
            self.record("republish", resp, nbytes=length)

        self.run_phase("republish", picked, replace)

    def wait_for_variants(self) -> None:
        """Poll the visitor album view; it only lists photos with variants_ready."""
        if self.args.no_wait_variants:
            return
        expected = {a.slug: len(a.uploaded) for a in self.albums if a.slug}
        total = sum(expected.values())
        if total == 0:
            return
        if self.args.password:
            print("  variants: skipped (album is password protected)", file=sys.stderr)
            return
        started = time.perf_counter()
        deadline = started + self.args.variant_timeout
        ready = 0
        first_seen = None
        last_line = 0.0
        while time.perf_counter() < deadline:
            ready = 0
            for slug in expected:
                resp = self.client.request("GET", f"/api/albums/{slug}", auth=False)
                if resp.ok:
                    ready += len(resp.json().get("photos", []))
            if first_seen is None and ready > 0:
                first_seen = time.perf_counter() - started
            now = time.perf_counter()
            if not self.args.quiet and (self.progress.tty or now - last_line >= 10.0):
                dt = now - started
                rate = ready / dt if dt > 0 else 0
                line = f"  variants: {ready}/{total} ready  {rate:5.2f} photos/s  {dt:5.1f}s"
                sys.stderr.write(line.ljust(78) + ("\r" if self.progress.tty else "\n"))
                sys.stderr.flush()
                last_line = now
            if ready >= total:
                break
            time.sleep(self.args.variant_poll)
        elapsed = time.perf_counter() - started
        if not self.args.quiet and self.progress.tty:
            sys.stderr.write("\n")
        # A rate over a near-zero wait is noise: a server that renders the
        # variants inside the upload request is already done on the first poll.
        rate = round(ready / elapsed, 2) if elapsed >= 0.5 else None
        self.variant_wait = {
            "expected": total,
            "ready": ready,
            "seconds": round(elapsed, 2),
            "photos_per_second": rate,
            "first_ready_after": round(first_seen, 2) if first_seen is not None else None,
            "timed_out": ready < total,
        }
        self.phase_times["variants"] = elapsed

    def read_back(self) -> None:
        variants = [v for v in self.args.read_variants.split(",") if v]
        if not variants or self.args.password:
            return
        targets: list[tuple[str, int]] = []
        for album in self.albums:
            if not album.slug:
                continue
            resp = self.client.request("GET", f"/api/albums/{album.slug}", auth=False)
            self.record("album view", resp, nbytes=len(resp.data))
            for photo in resp.json().get("photos", []):
                for variant in variants:
                    url = photo.get("urls", {}).get(variant)
                    if url:
                        targets.append((url, 0))
        if not targets:
            return

        def fetch(item: tuple[str, int]) -> None:
            resp = self.client.request("GET", item[0], auth=False)
            self.record("read", resp, nbytes=len(resp.data))

        self.run_phase("read", targets, fetch)

    def cleanup(self) -> None:
        if not self.args.cleanup:
            return

        def drop(album: AlbumPlan) -> None:
            if album.album_id:
                self.record("cleanup", self.client.json_request(
                    "DELETE", f"/api/publish/albums/{album.album_id}"))

        self.run_phase("cleanup", self.albums, drop, workers=min(self.args.workers, 4))
        for folder_id in reversed(self.folders):
            self.record("cleanup", self.client.json_request("DELETE", f"/api/publish/folders/{folder_id}"))

    # -- run -------------------------------------------------------------

    def run(self) -> None:
        self.build_plan()
        total = sum(len(a.tasks) for a in self.albums)
        print(
            f"run {self.run_id}: {total} uploads "
            f"({self.total_bytes() / 1e6:.0f} MB) from {len(self.pool)} source files "
            f"into {len(self.albums)} album(s), {self.args.workers} worker(s)",
            file=sys.stderr,
        )
        self.create_folders()
        self.create_albums()
        self.upload()
        self.set_order_and_cover()
        self.republish()
        self.wait_for_variants()
        self.read_back()
        self.cleanup()


# --------------------------------------------------------------------------
# Reporting


def percentile(values: list[float], pct: float) -> float:
    if not values:
        return 0.0
    ordered = sorted(values)
    k = min(len(ordered) - 1, max(0, int(round(pct / 100 * (len(ordered) - 1)))))
    return ordered[k]


def fmt_ms(seconds: float) -> str:
    return f"{seconds * 1000:.0f}ms" if seconds < 10 else f"{seconds:.1f}s"


def report(test: LoadTest) -> dict:
    rows = []
    out_phases = {}
    for phase in test.rec.phases():
        samples = test.rec.of_phase(phase)
        oks = [s for s in samples if s.ok]
        lat = [s.elapsed for s in oks]
        nbytes = sum(s.nbytes for s in oks)
        wall = test.phase_times.get(phase, sum(s.elapsed for s in samples))
        wall = max(wall, 1e-9)
        rows.append((
            phase, len(samples), len(samples) - len(oks),
            fmt_ms(percentile(lat, 50)), fmt_ms(percentile(lat, 90)),
            fmt_ms(percentile(lat, 99)), fmt_ms(max(lat) if lat else 0),
            f"{len(samples) / wall:.1f}",
            f"{nbytes / wall / 1e6:.1f}" if nbytes else "-",
        ))
        out_phases[phase] = {
            "requests": len(samples),
            "failed": len(samples) - len(oks),
            "wall_seconds": round(wall, 3),
            "p50_ms": round(percentile(lat, 50) * 1000, 1),
            "p90_ms": round(percentile(lat, 90) * 1000, 1),
            "p99_ms": round(percentile(lat, 99) * 1000, 1),
            "max_ms": round(max(lat) * 1000, 1) if lat else 0,
            "mean_ms": round(statistics.fmean(lat) * 1000, 1) if lat else 0,
            "requests_per_second": round(len(samples) / wall, 2),
            "megabytes_per_second": round(nbytes / wall / 1e6, 2),
            "bytes": nbytes,
        }

    header = ("phase", "n", "err", "p50", "p90", "p99", "max", "req/s", "MB/s")
    widths = [max(len(str(r[i])) for r in (*rows, header)) for i in range(len(header))]
    print()
    print("  ".join(h.ljust(w) for h, w in zip(header, widths)))
    print("  ".join("-" * w for w in widths))
    for row in rows:
        print("  ".join(str(c).ljust(w) for c, w in zip(row, widths)))

    failures: dict[str, int] = {}
    for s in test.rec.samples:
        if not s.ok:
            failures[f"{s.phase}: {s.error}"] = failures.get(f"{s.phase}: {s.error}", 0) + 1
    if failures:
        print("\nfailures")
        for label, count in sorted(failures.items(), key=lambda kv: -kv[1]):
            print(f"  {count:5d}  {label}")

    if test.variant_wait:
        v = test.variant_wait
        state = "TIMED OUT" if v["timed_out"] else "complete"
        rate = f" ({v['photos_per_second']} photos/s)" if v["photos_per_second"] else ""
        print(f"\nvariants ready for visitors: {v['ready']}/{v['expected']} in "
              f"{v['seconds']}s{rate} — {state}")

    return {
        "run_id": test.run_id,
        "started": datetime.now().isoformat(timespec="seconds"),
        "config": {
            "url": f"{'https' if test.client.https else 'http'}://{test.client.host}:{test.client.port}",
            "albums": len(test.albums),
            "uploads": sum(len(a.tasks) for a in test.albums),
            "source_files": len(test.pool),
            "bytes": test.total_bytes(),
            "workers": test.args.workers,
            "republish_fraction": test.args.republish,
            "folder_depth": test.args.folder_depth,
        },
        "phases": out_phases,
        "failures": failures,
        "variants": test.variant_wait,
        "albums": [{"id": a.album_id, "slug": a.slug, "photos": len(a.uploaded)} for a in test.albums],
    }


# --------------------------------------------------------------------------
# Stack + entry point


def read_state() -> dict[str, str]:
    if not STATE_FILE.exists():
        return {}
    out = {}
    for line in STATE_FILE.read_text().splitlines():
        if "=" in line and not line.startswith("#"):
            key, _, value = line.partition("=")
            out[key.strip()] = value.strip()
    return out


def stack(command: str, *extra: str) -> None:
    subprocess.run([str(TESTSERVER), command, *extra], check=True)


def parse_args(argv: list[str]) -> argparse.Namespace:
    p = argparse.ArgumentParser(
        description=__doc__,
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    p.add_argument("--photos", required=True, metavar="DIR",
                   help="directory of JPEGs to upload (searched recursively)")
    p.add_argument("--url", default=os.environ.get("SMUGBOX_URL", ""),
                   help="server base URL (env SMUGBOX_URL, or .testserver.env)")
    p.add_argument("--api-key", default=os.environ.get("SMUGBOX_API_KEY", ""),
                   help="publish API key (env SMUGBOX_API_KEY, or .testserver.env)")
    p.add_argument("--docker", action="store_true",
                   help="start a throwaway server with ./testserver.sh first and mint a key")
    p.add_argument("--keep-stack", action="store_true",
                   help="with --docker, leave the container running afterwards")

    p.add_argument("--albums", type=int, default=1, help="albums to create (default 1)")
    p.add_argument("--photos-per-album", type=int, default=0,
                   help="uploads per album; 0 means one per source file")
    p.add_argument("--max-source-photos", type=int, default=0,
                   help="use at most this many files from --photos (0 = all)")
    p.add_argument("--workers", type=int, default=4, help="concurrent uploads (default 4)")
    p.add_argument("--folder-depth", type=int, default=0,
                   help="nest the albums this many folders deep (plug-in collection sets)")
    p.add_argument("--password", default="", help="protect the albums with this password")
    p.add_argument("--republish", type=float, default=0.0, metavar="FRACTION",
                   help="re-upload this fraction of the photos (0.0-1.0) to exercise replace")
    p.add_argument("--shuffle", action="store_true",
                   help="interleave uploads across albums instead of album by album")
    p.add_argument("--skip-order", action="store_true", help="skip the order and cover calls")
    p.add_argument("--read-variants", default="thumb,small",
                   help="variants to fetch back as a visitor, comma separated; empty to skip")
    p.add_argument("--no-wait-variants", action="store_true",
                   help="do not wait for the background variant worker")
    p.add_argument("--variant-timeout", type=float, default=900.0,
                   help="seconds to wait for variants (default 900)")
    p.add_argument("--variant-poll", type=float, default=1.0, help="variant poll interval")
    p.add_argument("--cleanup", action="store_true", help="delete the created albums and folders")
    p.add_argument("--timeout", type=float, default=300.0, help="per-request timeout in seconds")
    p.add_argument("--insecure", action="store_true", help="skip TLS verification")
    p.add_argument("--seed", type=int, default=0, help="RNG seed for metadata and ordering")
    p.add_argument("--json", metavar="FILE", help="write the full report as JSON")
    p.add_argument("--quiet", action="store_true", help="no progress lines")
    return p.parse_args(argv)


def main(argv: list[str]) -> int:
    args = parse_args(argv)
    if args.albums < 1:
        raise SystemExit("--albums must be at least 1")
    if not 0.0 <= args.republish <= 1.0:
        raise SystemExit("--republish must be between 0.0 and 1.0")

    started_stack = False
    if args.docker:
        stack("up")
        started_stack = True
    if not args.url or not args.api_key:
        state = read_state()
        args.url = args.url or state.get("SMUGBOX_URL", "")
        args.api_key = args.api_key or state.get("SMUGBOX_API_KEY", "")
    if not args.url or not args.api_key:
        raise SystemExit(
            "no server: pass --url and --api-key, set SMUGBOX_URL/SMUGBOX_API_KEY,\n"
            "run ./testserver.sh up first, or pass --docker"
        )

    client = Client(args.url, args.api_key, args.timeout, args.insecure)
    ping = client.request("GET", "/api/publish/ping")
    if not ping.ok:
        raise SystemExit(f"publish ping failed: {ping.failure()} {ping.data[:200]!r}")

    test = LoadTest(args, client)
    try:
        test.run()
    except KeyboardInterrupt:
        print("\ninterrupted; reporting what was measured", file=sys.stderr)
    finally:
        data = report(test)
        if args.json:
            Path(args.json).write_text(json.dumps(data, indent=2))
            print(f"\nwrote {args.json}")
        if not args.cleanup and test.albums and test.albums[0].slug:
            print(f"\nalbums left on the server, e.g. {args.url}/a/{test.albums[0].slug}")
        # Flush before the teardown so the report is not reordered behind
        # testserver.sh's stderr when stdout is a pipe.
        sys.stdout.flush()
        if started_stack and not args.keep_stack:
            stack("down")

    failed = sum(1 for s in test.rec.samples if not s.ok)
    timed_out = bool(test.variant_wait.get("timed_out"))
    return 1 if failed or timed_out else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
