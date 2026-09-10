# Manual test checklist for the Lightroom plug-in

Lightroom's Lua environment has no test runner, so the plug-in is verified by
hand against a running backend. The backend contract test
(`backend/internal/api`) defines what the plug-in must send.

## Setup

1. Start the backend (locally or in Docker) and create an API key:
   `gallery admin create-api-key --label "Lightroom"`.
2. Lightroom Classic: File > Plug-in Manager > Add, select
   `lightroom-plugin/gallery.lrplugin`. Leave "Write a log file" on.
3. Library module > Publish Services > Photo Gallery > Set Up. Enter the
   server URL and API key, click **Test connection** (expect "Connected"),
   check that Image Sizing shows "Resize to fit" unchecked, save.

Log file: see the path shown in Plug-in Manager (`GalleryPublish.log`).
Check it after every step below.

## Publish

- [ ] Create a published collection "Test album", add 3 photos, click Publish.
      Expect: album appears at `<server>/api/albums`, three photos with
      dimensions, titles, captions, keywords and capture time.
- [ ] Right-click the collection > "Open album in browser" opens the album page.
- [ ] Download an original from the web and compare with the exported file
      (byte-identical, `sha256sum`).
