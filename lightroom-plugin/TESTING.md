# Manual test checklist for the Lightroom plug-in

Lightroom's Lua environment has no test runner, so the plug-in is verified by
hand against a running backend. The backend tests in `backend/internal/api`
define what the plug-in must send; `SmugboxAPI.lua` is the only file that
talks HTTP and can be reviewed on its own.

## Setup

1. Start the backend (locally or in Docker) and create an API key:
   `smugbox admin create-api-key --label "Lightroom"`.
2. Lightroom Classic: File > Plug-in Manager > Add, select
   `lightroom-plugin/smugbox.lrplugin`. Leave "Write a log file" on. The panel
   shows where `SmugboxPublish.log` is written.
3. Library module > Publish Services > Smugbox > Set Up. Enter the
   server URL and API key, click **Test connection** (expect "Connected").
   Check that Image Sizing has "Resize to fit" unchecked and File Settings
   is JPEG/sRGB. Save.

Check the log after every step below. Server state can be inspected with
`smugbox admin list-albums` and `curl <server>/api/albums`.

## Create and publish

- [ ] Create a published collection "Test album". In the dialog, leave the
      password empty, keep "Show in the public album list" on, add a
      description. Add 3 photos with titles, captions and keywords. Publish.
      Expect: album listed at `/api/albums` with 3 photos; `GET
      /api/albums/test-album` shows width/height, title, caption, keywords,
      taken_at and exif for each photo.
- [ ] Right-click the collection > "Open album in browser" opens the album.
      Right-click a published photo > "Open photo in browser" opens the album
      at that photo.
- [ ] Download an original from the web page and compare it with the file
      Lightroom exported (`sha256sum`): byte-identical.

## Republish

- [ ] Change a caption in Lightroom. The photo moves to "Modified Photos to
      Re-Publish". Publish. Expect: same photo id on the server (`GET
      /api/publish/albums/{id}/photos` still lists one row for it), new
      caption, new `content_hash`.
- [ ] Make a develop edit (exposure). Same expectation.
- [ ] Delete the photo on the server with curl (`DELETE
      /api/publish/albums/{id}/photos/{photo}`), then republish it from
      Lightroom. Expect: plug-in falls back to a fresh upload and the photo
      reappears with a new id.
- [ ] Delete the whole album on the server with curl (`DELETE
      /api/publish/albums/{id}`), change nothing in Lightroom and click
      Publish. Expect: a new album is created, an info dialog says all
      previously published photos were marked to re-publish, and they sit in
      "Modified Photos to Re-publish". Publish again: every photo is back on
      the server in the new album.
- [ ] Same, but edit one photo before publishing. Expect: that photo is
      uploaded in the first run, the rest after the second.

## Remove photos

- [ ] Remove one photo from the collection and publish. Lightroom asks to
      confirm deletion from the service. Expect: photo gone from
      `/api/albums/test-album`, its directory gone under `DATA_DIR/photos`.
- [ ] Delete a published photo from the catalog. Expect: Lightroom asks
      ("ask" behaviour) and the photo is removed on the server on confirm.

## Album settings

- [ ] Rename the collection. Expect: new name in `/api/albums`, slug unchanged,
      "Open album in browser" still works.
- [ ] Edit collection settings, set a password. Expect: `locked: true` in
      `/api/albums`, `GET /api/albums/test-album` returns 401 with name and
      photo_count, `/cover` returns a tiny blurred image.
- [ ] Edit collection settings again without changing the password (for
      example change the description). Expect: `password_version` unchanged
      (`smugbox admin list-albums` or sqlite3), so unlocked visitors stay in.
- [ ] Change the password. Expect: `password_version` incremented.
- [ ] Clear the password. Expect: album public again.
- [ ] Turn off "Show in the public album list". Expect: missing from
      `/api/albums`, still reachable at `/api/albums/test-album`.

## Cover photo

- [ ] Edit the settings of a published collection. Expect: a "Cover photo"
      popup listing the collection's photos by file name (and title), with
      "First photo in the album" preselected and a thumbnail of the first
      photo next to it. Changing the selection updates the thumbnail.
- [ ] Pick another photo and save. Expect: `cover_photo_id` in
      `GET /api/albums/test-album` is that photo's id and `/cover` serves its
      thumb; the album page shows it as the header image.
- [ ] Add a new photo to the collection, pick it as cover before publishing,
      save. Expect: the server cover is unchanged (log says "cover photo not
      published yet"). Publish. Expect: the new photo is now the cover.
- [ ] Remove the cover photo from the collection and publish. Expect: the
      server falls back to the first photo; reopening the settings shows
      "First photo in the album".
- [ ] Edit the settings of a collection that has never been published.
      Expect: a hint instead of the popup, no error.

## Sort order

- [ ] In the collection, set sort to "Custom Order" and drag photos around.
      Publish. Expect: `GET /api/albums/test-album` lists photos in that order
      and the zip download preserves it.

## Delete collection

- [ ] Delete the published collection in Lightroom. Expect: album gone from
      the server and its directory removed under `DATA_DIR/photos`.
- [ ] `smugbox admin gc --dry-run` reports nothing to clean.
- [ ] Delete the album row with curl first (`DELETE /api/publish/albums/{id}`),
      then delete the collection in Lightroom. Expect: no dialog; the
      collection disappears.
- [ ] Stop the backend container (proxy still answering) and delete a
      published collection, or remove photos from one and publish. Expect:
      after the retries, "not deleted on server"; the collection or photos
      are still listed in Lightroom and can be deleted again once the backend
      is back.

## Album sets (nested folders)

- [ ] Create a published collection set "Travel", and inside it a nested set
      "2024". Publish a collection from inside "2024". Expect: `smugbox admin
      list-albums` shows a PATH of `/travel/2024` for the album; `GET
      /api/folders/travel` lists a `2024` child folder; `GET
      /api/folders/2024` lists the album with a breadcrumb back to `travel`.
- [ ] Drag the collection from "2024" into "Travel" directly and publish
      again (no rename/settings dialog touched). Expect: the album's path
      updates to `/travel` — Lightroom fires no move callback, so this only
      works because publishing re-resolves the parent chain every time.
- [ ] Rename the "Travel" set. Expect: the folder's name changes at
      `/api/folders/travel` (slug unchanged).
- [ ] Delete the "2024" set (now empty after the drag above) with an album
      still under "Travel". Expect: only the set is removed; "Travel" and its
      album are unaffected.
- [ ] Delete the "Travel" set while it still contains an album. Expect:
      Lightroom's usual per-collection delete confirmation, then the set,
      its album and the album's photo files are all gone from the server;
      `smugbox admin gc --dry-run` reports nothing left over.
- [ ] Delete an album's backend row directly with curl, then publish a new
      photo into a *different* collection inside the same still-existing
      set. Expect: only the album is recreated; the set's folder id on the
      server is reused, not recreated.
- [ ] Delete a set's folder row with curl (`DELETE /api/publish/folders/{id}`,
      which also removes its albums), then create and publish a *new*
      collection inside that set. Expect: the set is recreated on the server
      (new id, same name), the publish succeeds, and the set's other
      collections republish into the new folder on their next publish.
- [ ] With the folder row deleted as above, open an existing collection's
      settings in that set and save. Expect: no "could not resolve album set"
      dialog; the set is recreated and the settings are saved.

## Error handling

- [ ] Stop the backend and publish. Expect: a clear error per photo, the
      photos stay in the re-publish queue, nothing crashes.
- [ ] Revoke the API key (`smugbox admin revoke-api-key`) and publish.
      Expect: "API key rejected" message; Test connection fails.
- [ ] Create a new collection and publish it while the backend is stopped;
      start the backend during the retries. Expect: the publish completes and
      `smugbox admin list-albums` shows exactly one album for it, also when
      the first create reached the backend but its response did not reach
      Lightroom (for example killing the container right after the row
      appears).
