# Implementationsplan: Egen fotogalleri-lösning med Lightroom-publicering

## 1. Målbild

Bygg en självhostad ersättning för SmugMug bestående av tre komponenter:

1. Ett **Lightroom Classic-plugin** (Lua) som fungerar som en riktig Publish Service —
   inte bara engångsexport.
2. En **Go-backend** som tar emot uppladdningar från pluginet, lagrar bilder + metadata,
   genererar visningsstorlekar och serverar album till webbläsare — inklusive
   lösenordsskydd per album och nedladdning (enskild bild och hela album som zip).
3. En **React-frontend** som visar alben snyggt (rutnät + lightbox) och hanterar
   lösenordsflödet för besökare. Go-servern serverar frontend-bygget som statiska
   filer, så hela lösningen är **en process/container**.

Utgångspunkter/beslut som redan är tagna och INTE ska ifrågasättas av agenten utan att
fråga användaren först:

- Backend: **Go**, inte Node/Python/PHP.
- Databas: **SQLite** (en fil på disk) via `modernc.org/sqlite`. Se avsnitt 4.2 för
  motivering, inklusive varför Turso Database (Rust-omskrivningen av SQLite, f.d. Limbo)
  inte väljs i v1. Ingen extern databastjänst — lösningen ska vara helt självhostad och
  lasten är låg (en publicerare, ett fåtal besökare).
- Lagring: **lokal disk** i en datakatalog. Ingen S3/MinIO-abstraktion behövs.
- Bildbehandling: **libvips via `vipsgen`** (`github.com/cshum/vipsgen`). Motivering i 4.1.
- Frontend: **React + TypeScript**, med `react-photo-album` + `yet-another-react-lightbox`
  för galleri/lightbox, och `shadcn/ui` (Tailwind-baserat, "kopiera och ägd kod") för
  övrig UI (knappar, dialoger, lösenordsformulär).
- **Go-servern serverar frontend-bygget** som statiska filer från en katalog
  (`FRONTEND_DIR`) med SPA-fallback. Inte `embed` (onödig kompileringskoppling), och
  ingen separat static-container (Traefik kan inte servera filer själv).
- Router: **`net/http` med `http.ServeMux`** (Go 1.22+). Ingen extern router.
- Reverse proxy/TLS är **utanför den här planens scope**. I produktion sitter Traefik
  framför containern; i utveckling behövs ingen proxy (Vite dev-server proxar `/api`
  till Go). Planen ska inte innehålla Caddy/Traefik-konfiguration utöver att servern
  lyssnar på en port och respekterar `X-Forwarded-For` från en betrodd proxy.
- Lösenord per album är ett **hårt krav**, inte en nice-to-have.
- **Lösenordsskyddade album visas i den publika albumlistan** som låsta kort med namn,
  suddig omslagsbild, antal bilder och datum.
- **Nedladdning är alltid på** för alla som kan se albumet: enskild bild (originalet)
  och hela albumet som zip. Ingen per-album-inställning.
- Pluginet laddar upp **den JPEG Lightroom exporterar, i full storlek** ("original").
  Backend genererar mindre varianter. Inte RAW. Video är **utanför scope** i v1
  (`canExportVideo = false`).
- Lightroom-pluginet ska byggas mot **Adobes Lightroom Classic SDK (Lua)**, med
  `lrc-immich-plugin` (github.com/bmachek/lrc-immich-plugin) som referensimplementation
  för hur en publish service-callback-kedja ska se ut. Kopiera inte kod rakt av
  (annan backend-API), men följ samma struktur/mönster.
- **Enhetstester** ska skrivas löpande i backend och frontend, inte som en egen fas
  i slutet. Se avsnitt 8.

## 2. Arkitekturöversikt

```
Lightroom Classic (Lua-plugin)  ─┐                          ┌─▶ SQLite (metadata, lösenordshash, API-nycklar)
                                  ├─▶ [Traefik, ej i scope] ─▶ Go-server ─┼─▶ Lokal disk (original + genererade varianter)
Webbläsare (React-app)          ─┘                          └─▶ React-build (statiska filer, FRONTEND_DIR)
```

- **Allt backend-API ligger under `/api/`.** Allt annat serveras av samma Go-server
  som statiska filer ur `FRONTEND_DIR`, med SPA-fallback till `index.html`. Frontend och API delar origin, så CORS behövs inte. SPA-rutter
  (t.ex. `/a/{slug}`) kolliderar aldrig med API-rutter.
- Lightroom-pluginet autentiserar med en **egen API-nyckel** (skapas via backendens
  CLI), helt separat från album-lösenorden. Plugin-endpoints ligger under
  `/api/publish/`, besökar-endpoints under `/api/albums/`.
- Webbläsaren autentiserar mot lösenordsskyddade album via en **signerad session-cookie**
  satt av backend efter lyckad unlock. Cookien innehåller listan över upplåsta album-ID:n
  och ett utgångsdatum, HMAC-signerad med en serverhemlighet. Ingen sessionstabell behövs.
- **Bildfiler och zip-nedladdningar serveras alltid via backend** med samma
  cookie-kontroll som albumets JSON. Enda undantaget är den suddiga omslagsbilden
  (`blur`-varianten, ~40 px), som är publik för listade album.

## 3. Repo-struktur

Monorepo:

```
/backend                Go-modul
  /cmd/gallery          main: `serve` + `admin`-subkommandon
  /internal/api         HTTP-handlers, middleware (auth, rate limit, cookies), zip-streaming
  /internal/db          sqlite-åtkomst, migrations (embed:ade SQL-filer)
  /internal/storage     filsystemslagring, sökvägsuppbyggnad, path traversal-skydd
  /internal/image       vipsgen: derivatgenerering, dimensioner, autorotate, blur
  /internal/auth        bcrypt, API-nyckelhash, signerad cookie
  /internal/web         statisk filserver för frontend-build + SPA-fallback
  /migrations           001_init.sql, ...
/frontend               React + Vite + TS; `npm run build` → dist/ som kopieras in i imagen
/lightroom-plugin       Lua-plugin (.lrplugin)
/deploy                 Dockerfile, docker-compose.yml, backup-skript
```

Tre separata delsystem, men ett repo underlättar samordnade API-kontraktsändringar.
Migrations ligger i backend och bäddas in i binären med `embed`, inte i `/deploy`.

**Utvecklingsläge:** `go run ./cmd/gallery serve` på :8080 och `npm run dev` på :5173
med Vite-proxy för `/api` → `http://localhost:8080`. Ingen reverse proxy. Utan
`FRONTEND_DIR` (eller med tom katalog) svarar Go-servern 404 på allt utom `/api`.

## 4. Backend (Go)

### 4.1 Paket/bibliotek
- Router: `net/http` med `http.ServeMux` (Go 1.22+: metod- och wildcard-routing,
  t.ex. `mux.HandleFunc("GET /api/albums/{slug}", ...)`). Middleware som vanliga
  `func(http.Handler) http.Handler`.
- HTTP-server: sätt `ReadHeaderTimeout`; **ingen `WriteTimeout`** (zip-strömmar kan
  ta minuter) — använd i stället `http.TimeoutHandler` på JSON-rutterna. Graceful
  shutdown på SIGTERM. `GET /api/healthz` för Docker/Traefik-healthcheck.
- Loggning: `log/slog` (JSON i produktion, text i dev), request-logg med metod, path,
  status, tid. Nivå via `LOG_LEVEL`.
- DB: `modernc.org/sqlite` (ren Go, ingen CGO-koppling för databasen) med `database/sql`.
  Sätt `PRAGMA journal_mode=WAL`, `PRAGMA foreign_keys=ON`, `PRAGMA busy_timeout=5000`
  vid uppstart. Begränsa till en skrivande connection (`db.SetMaxOpenConns(1)` eller
  separat läs-/skrivpool) — SQLite har en skrivare åt gången.
- Migrations: `pressly/goose` med SQL-filerna i `embed.FS` (embed av *SQL-filer* är
  oproblematiskt; det är frontend-embed som skippas).
- Lösenord: `golang.org/x/crypto/bcrypt`.
- Sessioner: egen signerad cookie (HMAC-SHA256 över `album_ids|expires`, hemlighet
  från miljövariabel). Cookien MÅSTE vara `HttpOnly`, `Secure`, `SameSite=Strict`,
  `Path=/`. TTL ca 24 h. (`Secure` fungerar på `http://localhost` i utveckling.)
  Behåll max ~20 senast upplåsta album i cookien så den håller sig under några kB.
- Klocka och slump injiceras (`func() time.Time`, `io.Reader`) i rate limiter, cookie
  och API-nyckelgenerering så tester inte behöver sova.
- Rate limiting: in-memory token bucket per (IP, slug). Räcker eftersom det är en process.
  Klient-IP tas från `X-Forwarded-For` **bara** om anropet kommer från
  `TRUSTED_PROXY_CIDR` (Traefik i produktion); annars från `RemoteAddr`.
- Zip: `archive/zip` ur standardbiblioteket, `zip.Store` (JPEG komprimerar inte),
  streamat direkt till `ResponseWriter`. Ingen tempfil, zip64 hanteras automatiskt.
- Bildbehandling: **`vipsgen`** (`github.com/cshum/vipsgen`). Valt framför `govips`
  eftersom vipsgen underhålls aktivt, genereras från libvips introspektion (hela API:t
  tillgängligt) och har samma prestandaprofil. Kräver CGO + libvips i byggmiljö och
  Docker-image. Ingen fallback till stdlib — en kodväg.
  - Använd `Thumbnail`-operationen (snabb, streamande, tar hänsyn till orientering).
  - Autorotate enligt EXIF-orientering, konvertera till sRGB, strippa all metadata utom
    ICC ur derivaten. Originalet lagras orört (det är det som laddas ner).
  - `blur`-variant: ~40 px lång sida + `Gaussblur`, JPEG-kvalitet låg. Några hundra byte.
- Metadata: **pluginet skickar all metadata som formulärfält** från Lightroom-katalogen
  (titel, bildtext, nyckelord, tagningstid, kamera, objektiv, exponering). Det är
  pålitligare än att parsa EXIF, som exportinställningen kan strippa. Backend läser
  bara **dimensioner och orientering** ur filen, via libvips. Inget separat
  EXIF-bibliotek.

### 4.2 Databas: varför SQLite

- En publicerare (Lightroom), ett fåtal samtidiga läsare. Ingen skalningsfråga.
  SQLite i WAL-läge låter läsare fortsätta medan den enda skrivaren committar.
- Noll drift: ingen extra container, inga credentials, ingen nätverkslatens.
- Backup är en filkopia (`VACUUM INTO` eller `sqlite3 .backup`), och kan kompletteras
  med Litestream för kontinuerlig replikering om man vill.
- Drivrutin `modernc.org/sqlite`: ren Go (transpilerad SQLite), mogen, brett använd,
  inga native-bibliotek att packa upp vid körning.

**Turso Database (f.d. Limbo) — övervägt, inte valt i v1.** Turso Database är en
Rust-omskrivning av SQLite som körs *inbäddad* i processen, precis som SQLite. Den är
alltså inte en hostad tjänst eller extra server (det är Turso Cloud respektive
libSQL-servern, separata produkter) och skulle passa kravet på en självhostad process.
Go-drivrutinen `turso.tech/database/tursogo` implementerar `database/sql` (drivernamn
`turso`) via purego utan CGO, med det native biblioteket inbäddat i binären och uppackat
till disk vid start. Filformatet är SQLite-kompatibelt (spårar SQLite 3.50.4), så en
databasfil kan flyttas mellan motorerna genom att kopiera den.

Skälen att ändå välja SQLite:

- **Mognad.** Turso Database är pre-1.0 (v0.7.x, hösten 2026) och projektet rekommenderar
  självt oberoende backuper. Databasen är den enda tillståndsbärande komponenten i
  lösningen; den ska inte köra på en betamotor.
- **Inget som lasten behöver.** Tursos fördelar är MVCC/`BEGIN CONCURRENT` (flera
  samtidiga skrivare), asynkron I/O, vektorsökning samt experimentell kryptering och
  fulltextsökning. Med en skrivare och ett fåtal läsare löser inget av det ett problem
  det här projektet har.
- **Ekosystem.** `goose` känner inte igen drivernamnet `turso` (dialekt måste sättas
  manuellt), `golang-migrate` saknar stöd, och Litestream/`sqlite3`-CLI förutsätter
  SQLites egen WAL-hantering — Tursos fleprocess-WAL-koordinering är fortfarande
  experimentell, vilket gör backup av en fil som servern samtidigt skriver till osäkrare.
- **Drift.** Uppackning av native-biblioteket kräver skrivbar temp-katalog och matchande
  libc i containern. Löst i praktiken (imagen är Debian-baserad p.g.a. libvips), men det
  är ytterligare en sak att få rätt utan motsvarande vinst. Argumentet "ingen CGO" väger
  inget här eftersom `vipsgen` redan kräver CGO.

**Håll dörren öppen.** Skriv schemat och SQL:en i standard-SQLite utan
drivrutinsspecifika tillägg och håll DB-åtkomsten bakom `database/sql`. Tack vare
filkompatibiliteten blir ett senare byte till Turso Database i praktiken ett byte av
drivrutinsimport plus en explicit `goose`-dialekt, om t.ex. vektorsökning eller
samtidiga skrivare blir relevant.

### 4.3 Datamodell (utkast, agenten får förfina)

UUID:n lagras som `TEXT`. Tidsstämplar som ISO 8601 `TEXT` i UTC. JSON som `TEXT`.

```sql
CREATE TABLE albums (
    id              TEXT PRIMARY KEY,                 -- UUID
    slug            TEXT UNIQUE NOT NULL,             -- slugify(name), vid kollision + "-" + 4 slumptecken; sätts EN gång, ändras inte vid namnbyte
    name            TEXT NOT NULL,
    description     TEXT,
    password_hash   BLOB,                             -- NULL = publikt album
    password_version INTEGER NOT NULL DEFAULT 0,      -- ökas vid lösenordsbyte; ingår i cookie-signaturen
    is_listed       INTEGER NOT NULL DEFAULT 1,       -- visas i albumlistan (styrs från Lightroom)
    cover_photo_id  TEXT,                             -- NULL = första bilden i sort_order
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE TABLE photos (
    id              TEXT PRIMARY KEY,                 -- UUID; detta är det "remoteId" pluginet sparar i Lightroom
    album_id        TEXT NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
    lr_photo_uuid   TEXT NOT NULL,                    -- photo:getRawMetadata("uuid") — gör uppladdning idempotent
    filename        TEXT NOT NULL,                    -- Lightrooms filnamn, används vid nedladdning/zip
    mime_type       TEXT NOT NULL,
    size_bytes      INTEGER NOT NULL,                 -- originalets storlek
    width           INTEGER NOT NULL,                 -- EFTER EXIF-orientering (visningsdimensioner)
    height          INTEGER NOT NULL,
    title           TEXT,
    caption         TEXT,
    keywords        TEXT,                             -- JSON-array
    taken_at        TEXT,                             -- som Lightroom levererar, utan tidszon; visas som väggklocka
    exif            TEXT,                             -- JSON-objekt (kamera, objektiv, exponering ...)
    content_hash    TEXT,                             -- sha256 av uppladdad fil; används för ETag
    sort_order      INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    UNIQUE (album_id, lr_photo_uuid)
);
CREATE INDEX photos_album_sort ON photos(album_id, sort_order);

CREATE TABLE api_keys (
    id              TEXT PRIMARY KEY,
    key_prefix      TEXT NOT NULL,                    -- första 8 tecknen i klartext, för uppslag
    key_hash        BLOB NOT NULL,                    -- sha256(nyckel). INTE bcrypt: nyckeln är 32 slumpbytes
    label           TEXT,                             -- t.ex. "Lightroom på laptopen"
    created_at      TEXT NOT NULL,
    last_used_at    TEXT,
    revoked_at      TEXT
);
```

Ingen `storage_key`-kolumn: sökvägen härleds deterministiskt ur ID:n (se 4.5).
Antal bilder och datumintervall för albumlistan räknas fram med en aggregerande fråga
(`COUNT(*)`, `MIN/MAX(taken_at)`), ingen denormaliserad kolumn.

**Standardordning** för bilder: `sort_order, taken_at, filename`. `sort_order` är 0 för
alla tills Lightroom skickar en explicit ordning, då faller ordningen tillbaka på
tagningstid.

### 4.4 API-endpoints

Allt under `/api/`. JSON in/ut utom bilduppladdning (multipart) och bild-/zip-hämtning
(bytes).

**Besökare (`/api/albums/...`), cookie-baserad auth för skyddade album:**
- `GET /api/albums` → listade album (`is_listed = 1`), sorterade på senaste `taken_at`.
  Per album: `slug`, `name`, `locked`, `photo_count`, `taken_from`, `taken_to`,
  `cover_url`. Ingen bildlista.
- `GET /api/albums/{slug}/cover` → omslagsbild **utan cookie-kontroll**: `thumb`
  för publika album, `blur` för låsta album. Aldrig något skarpare för låsta album.
- `GET /api/albums/{slug}` → 200 med albuminfo + bildlista (inkl. width/height, titel,
  bildtext, tagningstid och URL:er för alla varianter, så frontend kan bygga `srcSet`)
  om publikt eller upplåst, annars `401 {"error":"password_required"}` med
  `name`, `photo_count` så `<PasswordGate>` kan visa vad man låser upp.
- `POST /api/albums/{slug}/unlock` `{password}` → sätter/uppdaterar session-cookie.
  Rate-limitas (t.ex. 5 försök/minut per IP+slug). **Samma statuskod, body och
  svarstid** för fel lösenord och icke-existerande album: kör bcrypt mot en fast
  dummy-hash när albumet inte finns.
- `GET /api/albums/{slug}/photos/{photo_id}/{variant}` → bildbytes. `variant` är en av
  `thumb|small|medium|large|original`. Kontrollerar cookie om albumet är skyddat.
  `?download=1` lägger på `Content-Disposition: attachment; filename="<filename>"`.
  `ETag` (= `content_hash` + variant) och `Cache-Control: private, max-age=...` för
  skyddade album, `public, max-age=...` för publika.
- `GET /api/albums/{slug}/download` → streamad zip med alla original i `sort_order`,
  filnamn från `filename` (dubbletter får suffix `-2`, `-3`). Zip-namn = albumnamn.
  Cookie-kontroll som ovan. Begränsa samtidiga zip-strömmar (t.ex. 2) för att skona
  disk-IO.

**Lightroom-plugin (`/api/publish/...`), kräver `Authorization: Bearer <api-key>`:**
- `POST   /api/publish/albums` `{name, description?, password?, is_listed?}` → skapar
  album, returnerar `{id, slug, url}`.
- `PUT    /api/publish/albums/{id}` `{name?, description?, password?, is_listed?,
  cover_photo_id?}` — `password: ""` tar bort skyddet, utelämnat fält lämnas orört.
  **Pluginet skickar lösenordet vid varje sparning av samlingsinställningar**, så
  backend måste jämföra mot befintlig hash (bcrypt) och bara skriva ny hash + öka
  `password_version` när lösenordet faktiskt ändrats. Annars loggas alla besökare ut
  varje gång man sparar inställningarna.
- `DELETE /api/publish/albums/{id}` — när samlingen tas bort i Lightroom. Raderar
  även filerna på disk.
- `GET    /api/publish/albums/{id}/photos` → lista med `id`, `lr_photo_uuid`,
  `content_hash`; används av pluginet för att avstämma vad som redan finns.
- `POST   /api/publish/albums/{id}/photos` — multipart: `file` + metadatafält
  (`lr_photo_uuid`, `filename`, `title`, `caption`, `keywords`, `taken_at`, `exif`).
  Om `(album_id, lr_photo_uuid)` redan finns behandlas anropet som uppdatering
  (idempotent vid nätverksfel/retry). Returnerar `{id, url}`.
- `PUT    /api/publish/albums/{id}/photos/{photo_id}` — **ersätt** bild och/eller
  metadata vid republish; derivaten regenereras alltid (Lightroom bäddar in ändrad
  bildtext i JPEG:en, så filen är i praktiken aldrig identisk — och libvips gör det
  på under en sekund). `404` om fotot inte längre finns; pluginet faller då tillbaka
  på `POST`.
- `DELETE /api/publish/albums/{id}/photos/{photo_id}`.
- `PUT    /api/publish/albums/{id}/order` `{photo_ids: [...]}` — sätter `sort_order`
  från Lightrooms `imposeSortOrderOnPublishedCollection`.

**Övrigt:**
- `GET /api/healthz` → `200 {"status":"ok"}` efter en `SELECT 1` mot databasen.

**Admin: CLI, inget webb-UI i v1.**
```
gallery admin create-api-key --label "Lightroom laptop"   # skriver ut nyckeln en gång
gallery admin revoke-api-key <id>
gallery admin list-albums
gallery admin set-password <slug>                          # prompt, eller --clear
gallery admin gc                                           # städa orphan-kataloger på disk
```
Det tar bort en hel autentiseringsyta och ett UI ur v1. Ett admin-UI kan läggas till
senare med egen inloggning (lösenord → session-cookie), aldrig med API-nyckel i
webbläsaren.

### 4.5 Lagring på disk

```
$DATA_DIR/
  gallery.db
  photos/{album_id}/{photo_id}/original.jpg   exakt det Lightroom exporterade (nedladdning, zip)
  photos/{album_id}/{photo_id}/large.jpg      2560 px lång sida (lightbox) — utelämnas om originalet är mindre; då serveras originalet som "large"
  photos/{album_id}/{photo_id}/medium.jpg     1600
  photos/{album_id}/{photo_id}/small.jpg      800
  photos/{album_id}/{photo_id}/thumb.jpg      400
  photos/{album_id}/{photo_id}/blur.jpg       ~40 px + gaussblur (publik omslagsbild för låsta album)
```

- Diskbudget: ett fullstort Lightroom-export är typiskt 5–15 MB; derivaten adderar
  ~2 MB. Räkna ~15 MB per bild.
- Alla sökvägar byggs av validerade UUID:n och en whitelist av variantnamn. Aldrig
  användarstyrda filnamn i sökvägen (`filename` lagras bara i databasen och saneras
  innan det används i `Content-Disposition`/zip).
- Skriv till tempfil i samma katalog och `rename` för atomicitet.
- Radering av album/foto tar bort katalogen. `gallery admin gc` hittar kataloger utan
  databasrad (orphans) efter t.ex. krasch mitt i en uppladdning.
- Filsystemsåtkomsten ligger i ett eget paket med ett litet interface så testerna kan
  använda en tempkatalog — men designa inte för S3.

### 4.6 Säkerhet — checklista
- [ ] Bcrypt (cost 12) för albumlösenord, aldrig klartext eller reversibel kryptering.
- [ ] API-nycklar: 32 slumpbytes, base64url, lagras som sha256. Jämförelse med
      `crypto/subtle.ConstantTimeCompare`. Uppslag via `key_prefix`.
- [ ] Rate limiting på `/unlock`; `X-Forwarded-For` litas på bara från `TRUSTED_PROXY_CIDR`.
- [ ] Identiskt svar (kod, body, tid) för fel lösenord vs. icke-existerande album.
- [ ] Signerad cookie: verifiera HMAC och utgångstid innan innehållet läses. Signaturen
      inkluderar `password_version` per album så lösenordsbyte loggar ut alla.
- [ ] Bildbytes och zip går alltid genom cookie-kontrollen. Enda publika bildvarianten
      för låsta album är `blur`, via `/cover`.
- [ ] Storleksgräns på uppladdning (t.ex. 100 MB per fil, `http.MaxBytesReader`).
      Chunkad/resumable upload behövs inte och går ändå inte att göra med
      `LrHttp.postMultipart`.
- [ ] Validera att uppladdad fil faktiskt är JPEG (magic bytes), oavsett `Content-Type`.
- [ ] Path traversal omöjlig per konstruktion (4.5). Filnamn i `Content-Disposition`
      och zip saneras (inga `/`, `\`, kontrolltecken; RFC 5987 för icke-ASCII).
- [ ] Servern sätter `X-Content-Type-Options: nosniff` och en restriktiv
      `Content-Security-Policy` själv (inte beroende av proxyn). HSTS lämnas åt Traefik.

## 5. Lightroom Classic-plugin (Lua)

### 5.1 Grund
- Ladda ner **Lightroom Classic SDK** (aktuell version, Lua 5.1-kompatibel) från Adobe.
- Studera Adobes medföljande **Flickr-exempelplugin** (referensimplementation för
  publish services i SDK:t) samt `lrc-immich-plugin` på GitHub för hur en fullständig
  publish service-cykel struktureras.
- Dela upp koden: `Info.lua`, `PublishServiceProvider.lua` (callbacks),
  `GalleryAPI.lua` (all HTTP mot backend, inget Lightroom-beroende utöver `LrHttp`),
  `PluginInfoDialog.lua` (config). Logga med `LrLogger` till fil — Lua-plugins går
  annars inte att felsöka.

### 5.2 Krav på pluginet

Registrera sig som **Publish Service** i `Info.lua` (`LrExportServiceProvider` med
`supportsIncrementalPublish = true`). Sätt `canExportVideo = false`,
`allowFileFormats = { "JPEG" }`, `allowColorSpaces = { "sRGB" }`. **Dölj inte**
sektionen för bildstorlek — användaren styr exportstorleken själv, med förvalet
"Ändra inte storlek" (full storlek) eftersom backend gör derivaten. Dölj övriga
irrelevanta sektioner med `hideSections` (t.ex. vattenstämpel om det inte behövs,
utdatavässning kan lämnas).

Callbacks som måste implementeras:

- `sectionsForTopOfDialog` / `startDialog` — **config** för publish-tjänsten:
  backend-URL, API-nyckel, knapp "Testa anslutning".
- `viewForCollectionSettings` + `updateCollectionSettings` — **per-samlings-
  inställningar**: albumlösenord (`LrView.password_field`; tomt = publikt), "visa i
  albumlistan" (`is_listed`), beskrivning. Observera att Lightroom lagrar
  samlingsinställningar i klartext i katalogen — acceptabelt för albumlösenord. Detta är rätt ställe för lösenordet, inte ett globalt "förvalt lösenord"
  i plugin-configen. Skapar album via `POST /api/publish/albums` första gången (i
  `updateCollectionSettings` eller lazy vid första publicering) och sparar backendens
  `id` med `publishedCollection:setRemoteId()` och länken med `setRemoteUrl()`.
- `renamePublishedCollection` → `PUT /api/publish/albums/{id}` (slug ändras inte).
- `deletePublishedCollection` → `DELETE /api/publish/albums/{id}`.
- `processRenderedPhotos` — för varje rendition:
  - Om `rendition.publishedPhotoId` är satt → **republish**:
    `PUT /api/publish/albums/{id}/photos/{photo_id}`. Svarar backend `404` (fotot
    raderat server-side, t.ex. via CLI) → gör `POST` i stället och spara det nya ID:t
    med `recordPublishedPhotoId`.
  - Annars → `POST /api/publish/albums/{id}/photos`, och spara svaret med
    `rendition:recordPublishedPhotoId(id)` + `recordPublishedPhotoUrl(url)`.
  - Skicka metadata från katalogen som formulärfält: `photo:getRawMetadata("uuid")`,
    `getFormattedMetadata("fileName" | "title" | "caption" | "keywordTags"
    | "dateTimeOriginal" | "cameraModel" | "lens" | "exposure" | ...)`.
    `filename` = det exporterade filnamnet (basename av `pathOrMessage`), så
    filnamnsmallen i exportinställningarna respekteras.
  - Använd `LrHttp.postMultipart` med `filePath`. Hantera fel per bild
    (`rendition:uploadFailed(msg)`), inte hela batchen.
- `deletePhotosFromPublishedCollection` → `DELETE .../photos/{photo_id}` per remote-ID,
  anropa `deletedCallback(remoteId)` efter lyckad radering.
- `shouldDeletePhotosFromServiceOnDeleteFromCatalog` — returnera `"ask"` eller `"delete"`.
- `metadataThatTriggersRepublish` — titel, bildtext, nyckelord, utvecklingsinställningar
  ska flagga bilden för republicering.
- `supportsCustomSortOrder = true` + `imposeSortOrderOnPublishedCollection` →
  `PUT /api/publish/albums/{id}/order`.
- `getCollectionBehaviorInfo` — tillåt inte samlings-set i v1 (`canAddCollection = true`,
  `maxCollectionSetDepth = 0`).
- Kommentarer/betyg: `canAddCommentsToService = false`, implementera inte
  `getCommentsFromPublishedCollection`.

### 5.3 Ej i scope för v1
- Nedladdning/import tillbaka till Lightroom (som `lrc-immich-plugin` också har).
- Video.
- Samlings-set (hierarkiska album).
- Välja omslagsbild från Lightroom (backend tar första bilden i sorteringen; kan sättas
  via `PUT .../albums/{id}` senare).

## 6. Frontend (React)

### 6.1 Stack
- Vite + React + TypeScript. `vite.config.ts` proxar `/api` till `localhost:8080` i dev.
  `npm run build` skriver `dist/`, som i produktion kopieras in i imagen och pekas ut
  med `FRONTEND_DIR`.
- Tailwind CSS + `shadcn/ui` för generell UI (knappar, dialogrutor,
  lösenordsformulär, toggles).
- `react-photo-album` (rows/masonry-layout) + `yet-another-react-lightbox`
  (samma utvecklare, integrerar rent) för själva bildvisningen. Bygg `srcSet` av
  varianterna i API-svaret; `react-photo-album` behöver `width`/`height` per bild
  och får dem från backend.
- Routing: `/` albumlista, `/a/{slug}` albumvy. Aldrig `/api/*` i SPA-routern.

### 6.2 Vyer/komponenter
- **Albumlista** — `GET /api/albums`. Kort med omslagsbild (`/cover`), namn, antal
  bilder, datumintervall. Låsta album visar den suddiga omslagsbilden (servern
  levererar redan suddig; lägg dessutom CSS-`blur` + låsikon ovanpå så det är tydligt).
- **Albumvy** — hämtar `GET /api/albums/{slug}`. Vid `401` visas `<PasswordGate>`
  i stället för galleriet, med albumnamn och antal bilder från 401-svaret.
- **`<PasswordGate>`** — enkelt formulär, `POST /api/albums/{slug}/unlock` med
  `credentials: "include"`, refetchar albumdata vid lyckad upplåsning. Visar
  rate-limit-fel (`429`) begripligt. Cookien hanteras av webbläsaren automatiskt,
  ingen token i React-state.
- **Galleri** — `react-photo-album` med `thumb`/`small`/`medium` i `srcSet`,
  lightbox laddar `large`. Visa titel/bildtext i lightboxen.
- **Nedladdning** — knapp i lightboxen: vanlig `<a href=".../original?download=1">`
  (cookien följer med automatiskt). Knapp "Ladda ner album (zip)" i albumvyn:
  `<a href="/api/albums/{slug}/download">`. Inga fetch/blob-omvägar — webbläsaren
  streamar filen direkt.
- **Tomma tillstånd och fel** — tomt album, album som inte finns (404-sida), nätverksfel.

### 6.3 Senare (ej v1)
- **OpenGraph/Twitter-taggar** för delade länkar: eftersom Go serverar `index.html`
  kan servern injicera `og:title`/`og:image` (thumb för publika album, ingen bild för
  låsta) för `/a/{slug}`. Billigt och syns direkt i chattar/sociala medier.
- Admin-UI (ersätter CLI), se 4.4.

## 7. Bygg, driftsättning och backup

Reverse proxy/TLS ligger utanför planen (Traefik i produktion). Det som ingår:

- **Dockerfile** (multi-stage):
  1. `node` — `npm ci && npm run build` i `/frontend`.
  2. `golang` + `libvips-dev` — `go build` med CGO. Ingen frontend-koppling.
  3. `debian:bookworm-slim` + `libvips42` — binären + `dist/` → `/srv/frontend`,
     `EXPOSE 8080`, `VOLUME /data`, `HEALTHCHECK` mot `/api/healthz`.
- **docker-compose.yml** med en tjänst `gallery`: volym `/data`, port `8080`,
  miljövariabler `DATA_DIR`, `FRONTEND_DIR=/srv/frontend`, `LISTEN_ADDR`,
  `SESSION_SECRET`, `PUBLIC_BASE_URL`, `TRUSTED_PROXY_CIDR`, `MAX_UPLOAD_MB`,
  `LOG_LEVEL`. Traefik-labels lämnas åt användaren — men notera att Traefiks
  standard-timeouts behöver höjas för zip-nedladdningar på flera GB.
- **Backup**: skript i `/deploy` som kör `sqlite3 gallery.db "VACUUM INTO 'backup.db'"`
  och rsyncar `photos/` + `backup.db` till annan plats. Litestream är ett valfritt
  tillägg för kontinuerlig DB-replikering.

## 8. Tester

Tester skrivs i samma fas som koden de testar. Backend har `go test ./...` grönt i
varje fas; CI kör i Docker-imagen så libvips finns.

**Backend (Go, `testing` + `net/http/httptest`, SQLite i tempfil per test):**
- `auth`: API-nyckel skapas/hashas/verifieras; fel nyckel, revokerad nyckel, saknad
  header → 401. Constant-time-jämförelse används.
- `auth`: signerad cookie — roundtrip, utgången cookie avvisas, manipulerad payload
  avvisas, cookie utfärdad före lösenordsbyte (`password_version`) avvisas.
- `api/albums`: listan innehåller bara `is_listed`, `locked` korrekt, `photo_count`
  och datumintervall stämmer; `/cover` ger `blur` för låst album och `thumb` för
  publikt, utan cookie; 401-svaret för låst album innehåller namn och antal men
  ingen bildlista.
- `api/unlock`: rätt lösenord sätter cookie med korrekta flaggor; fel lösenord och
  icke-existerande slug ger byte-identisk body och samma statuskod; rate limit
  slår in efter N försök och släpper efter fönstret (injicerad klocka, inga sleeps);
  `X-Forwarded-For` ignoreras från icke-betrodd adress.
- `api/photos`: bild i skyddat album kräver cookie, i publikt album inte; okänd
  `variant` → 404; `ETag`/304; `?download=1` ger `Content-Disposition` med sanerat
  filnamn; `large` faller tillbaka på `original` när originalet är mindre än 2560.
- `api/download`: zip är giltig (`zip.NewReader`), innehåller alla bilder i
  `sort_order` med Lightroom-filnamn, dubbletter suffixas, poster är `Store`;
  kräver cookie för låst album; samtidighetsgränsen ger 429/503 över taket.
- `api/publish`: POST med samma `lr_photo_uuid` två gånger ger en rad (idempotens);
  PUT ersätter fil och regenererar derivat; PUT på raderat foto → 404; DELETE tar
  bort rad och katalog; `order` sätter `sort_order`; PUT med **ändrat** lösenord ökar
  `password_version`, PUT med **samma** lösenord ändrar den inte; slug-kollision ger
  suffix.
- `storage`: sökvägar byggs bara av UUID + whitelist; atomisk skrivning; radering;
  gc hittar orphan-kataloger.
- `image`: fixture-JPEG med EXIF-orientering 6 → derivat är roterade, lagrade
  `width`/`height` är de roterade dimensionerna, derivaten är sRGB och saknar
  GPS-metadata; `blur` är ≤ 40 px och < 2 kB; originalet är byte-identiskt med
  uppladdningen. Testet byggs med build-tag så det
  kan hoppas över där libvips saknas, men CI kör det alltid.
- `web`: med `FRONTEND_DIR` satt ger `/` och `/a/nagot` `index.html`, `/assets/...`
  rätt fil med rätt `Content-Type`; utan `FRONTEND_DIR` ger `/` 404; `/api/finns-inte`
  ger JSON-404 och aldrig `index.html`; `..`-sökvägar avvisas.
- `healthz`: 200 med fungerande databas, 503 utan.
- `db`: migrations körs från tom databas till aktuell version och är idempotenta.
- Ett **kontraktstest** som spelar in ett fullständigt publish-flöde (create album →
  upload → republish → reorder → set password → delete photo → delete album) mot
  `httptest.Server` och verifierar tillstånd i DB och på disk efter varje steg.

**Frontend (Vitest + React Testing Library, MSW för API-mock):**
- Albumlista renderar kort med omslag, antal, datum och låsikon för `locked`.
- `<PasswordGate>` visas vid 401 med albumnamn, skickar `POST /unlock` med
  `credentials: "include"`, refetchar vid 200, visar fel vid 401 och 429.
- Albumvy bygger `srcSet` korrekt av API-svaret; nedladdningslänkarna pekar på rätt
  URL:er.

**Lightroom-plugin:** inget körbart testramverk i Lightrooms Lua-miljö. Kompensera med:
- `GalleryAPI.lua` isolerat från SDK-objekt så att det kan läsas/granskas separat.
- Backendens kontraktstest ovan är sanningen för vad pluginet ska skicka.
- Manuell testchecklista i `/lightroom-plugin/TESTING.md`: skapa samling, publicera,
  ändra bildtext → republish, ta bort bild, byt namn, byt lösenord, sortera om, ta bort
  samling, verifiera att nedladdat original är byte-identiskt med exportfilen.
  Kontrollera `LrLogger`-loggen efter varje steg.

## 9. Beslut tagna vid avstämning (tidigare öppna frågor)

Inga öppna frågor kvarstår inför implementation. Följande avgjordes:

| Fråga | Beslut |
|---|---|
| Låsta album i albumlistan? | Ja, som låsta kort med namn, suddig omslagsbild, antal bilder, datum. |
| Reverse proxy / driftmiljö | Traefik i produktion, utanför planen. Ingen proxy i dev. Go serverar frontend. |
| Största visningsstorlek | Lightrooms fulla export lagras som `original`; `large` = 2560 px genereras. |
| Nedladdning | Alltid på. Enskild bild = original med Lightroom-filnamn. Hela album = streamad zip. |
| Per-album nedladdningsinställning | Nej. |
| Lagring | Lokal disk. |
| Databas | SQLite via `modernc.org/sqlite`. Turso Database (inbäddad Rust-motor) övervägd men pre-1.0 och utan fördel för lasten; se 4.2. |
| Chunkad upload | Nej — behövs inte, går inte i Lua-SDK:t. |
| Admin-UI i v1 | Nej, CLI. |
| Frontend-servering | Go serverar `dist/` från `FRONTEND_DIR`, inte `embed`. Traefik kan inte servera statiska filer, så alternativet vore en extra container. |
| Router | `http.ServeMux` (Go 1.22+), ingen extern router. |
| Migrations | `pressly/goose`. |
| EXIF-bibliotek | Inget; pluginet skickar metadata, libvips ger dimensioner/orientering. |

## 10. Föreslagen fasindelning

**Fas 1 — Backend-skelett**
SQLite + goose-migrations, `ServeMux`-routing, album/foto-CRUD under `/api/publish`,
API-nyckelauth, CLI för nycklar, lokal lagring med atomisk skrivning, `web`-paketet
(filserver + SPA-fallback), `/api/healthz`, slog. **vipsgen tas in redan här** för att
läsa dimensioner/orientering vid uppladdning, och Dockerfile med libvips finns från
dag ett så CI kan köra allt. Tester för auth, storage, db, web, publish-endpoints.

**Fas 2 — Bildbehandling och leverans**
Derivatgenerering inkl. `blur`, autorotate, sRGB. Bildendpoint med `ETag` och
`?download=1`, `/cover`, zip-streaming utan `WriteTimeout`, `GET /api/albums` med
aggregat. Tidigt eftersom frontend behöver varianter och dimensioner.

**Fas 3 — Lightroom-plugin, minimal publish service**
Config-dialog, skapa album, `processRenderedPhotos` med `recordPublishedPhotoId`,
metadata + filnamn som formulärfält. Verifierar hela kedjan end-to-end mot riktig
Lightroom. Bygg direkt som publish service — ingen "ren export"-mellanversion som slängs.

**Fas 4 — Lightroom-plugin, komplett**
Republish (`PUT`), radering, namnbyte, borttagning av samling, per-samlings-lösenord och
`is_listed` i `viewForCollectionSettings`, sorteringsordning,
`metadataThatTriggersRepublish`. `TESTING.md`-checklistan körs igenom.

**Fas 5 — Lösenordsskydd**
Bcrypt, unlock-endpoint med dummy-hash, signerad cookie med `password_version`, rate
limiting med trusted proxy, cookie-kontroll på bild-, zip- och albumendpoints,
`/cover`-undantaget. Tester enligt avsnitt 8.

**Fas 6 — Frontend**
Albumlista med kort, albumvy, `<PasswordGate>`, galleri + lightbox med `srcSet`,
nedladdningsknappar, tomma tillstånd/404. Vitest-tester.

**Fas 7 — Paketering**
Dockerfile kompletteras med frontend-steget och `HEALTHCHECK`, docker-compose med
`/data`-volym, backup-skript, `gallery admin gc`. Snabbtest bakom en lokal Traefik är
valfritt.

## 11. Referenser att ge agenten

- Lightroom Classic SDK (Adobe, officiell nedladdning) — särskilt Flickr-exemplet och
  kapitlet om publish services i programmeringsguiden
- https://github.com/bmachek/lrc-immich-plugin (referens för publish service-mönster)
- https://github.com/cshum/vipsgen (libvips-bindning)
- https://pkg.go.dev/modernc.org/sqlite (SQLite-driver, ren Go)
- https://pkg.go.dev/archive/zip (streamad zip)
- https://github.com/igordanchenko/react-photo-album
- https://github.com/igordanchenko/yet-another-react-lightbox
- https://ui.shadcn.com (shadcn/ui-dokumentation)
- https://litestream.io (valfri kontinuerlig SQLite-backup)
