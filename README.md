# vibetype

Minimalistischer, schneller, offline-fähiger 10-Finger-Trainer mit Peer-to-Peer-Charakter:
jeder Nutzer kann Texte anlegen, per Link/Code teilen und sieht live, wer seinen Text gerade tippt.

* **Keine Rollen:** Jeder angemeldete Nutzer kann Texte erstellen und teilen. Der Ersteller eines Texts bekommt
  automatisch die Live-Ansicht aller Tipp-Sitzungen auf diesem Text.
* **Local-First:** Die Tipp-Logik läuft komplett im Browser. Ein Sync-Loop schickt den Fortschritt alle 3 s an den
  Server; offline wird gepuffert (`localStorage`), ein Service Worker hält die App-Shell vor.
* **Stop-on-Error:** Bei einem Tippfehler bleibt der Cursor stehen und das Zeichen „wackelt“ rot, bis die richtige
  Taste kommt.
* **Bildschirmtastatur mit Fingerhinweisen:** Unter dem Text zeigt eine Tastatur (QWERTZ oder QWERTY) die nächste
  Taste, die nötige Umschalt- bzw. AltGr-Taste und den Finger dafür („rechter Zeigefinger + linke Umschalttaste“). Die
  Tasten sind nach Finger eingefärbt, F und J haben die Tastmarke. Eine falsch gedrückte Taste blinkt rot, falls sie
  auf dem gewählten Layout liegt. Ein- und Ausblenden sowie das Layout merkt sich der Browser. Voreinstellung:
  QWERTZ bei deutscher Browsersprache (auch `de-CH`, ein Schweizer Layout gibt es noch nicht), sonst QWERTY.
  Großschreibung folgt der 10-Finger-Regel: Shift drückt der kleine Finger der jeweils anderen Hand. Bei QWERTZ werden
  Tottasten in zwei Schritten erklärt: `^` allein ist „^, dann Leertaste“, `é` ist „´, dann e“. Zeichen, die auf dem
  Layout fehlen (z. B. ñ), zeigen „Taste nicht auf diesem Layout“.
* **Passkeys:** Login per WebAuthn (discoverable credentials), ohne Passwort und ohne E-Mail. Sitzung per HTTP-only-Cookie.

## Stack

| Teil | Wahl |
|---|---|
| Backend | Go, ein einziges Binary, Frontend per `embed.FS` |
| DB | SQLite im WAL-Mode (`modernc.org/sqlite`, ohne CGO) |
| Auth | `github.com/go-webauthn/webauthn` |
| Frontend | Vanilla JS (ES Modules, kein Build-Step), eigenes CSS, Monospace im Tippbereich |

## Starten

```sh
go run . -addr :8080 -origin http://localhost:8080 -db vibetype.db
```

Produktiv:

```sh
vibetype -addr :8080 -origin https://vibetype.thomas-guettler.de -db /data/vibetype.db
```

Die WebAuthn-`rpId` wird aus dem Host von `-origin` abgeleitet. Passkeys sind an diese Domain gebunden, ein späterer
Domainwechsel macht alle Passkeys ungültig. Statt Flags gehen auch die Umgebungsvariablen `VIBETYPE_ADDR`,
`VIBETYPE_ORIGIN` und `VIBETYPE_DB`. TLS terminiert ein Reverse-Proxy (Ingress/Caddy). Passkeys funktionieren nur
über HTTPS oder `localhost`.

Docker: `docker build -t vibetype . && docker run -p 8080:8080 -v vibetype:/data -e VIBETYPE_ORIGIN=... vibetype`
(das Image läuft als uid 65532, `/data` gehört diesem Nutzer).

`GET /healthz` antwortet mit `ok <version>`.

## Release & Deployment

* Jeder grüne CI-Lauf auf `main` erzeugt per [release.yml](.github/workflows/release.yml) den nächsten Tag
  `v0.0.N` und das Image `ghcr.io/guettli/vibetype:v0.0.N`.
* Produktiv läuft vibetype auf dem vps-Cluster, die Manifeste liegen in
  [guettli/gitops](https://github.com/guettli/gitops) unter `k8s/plain/vps/vibetype/`. Renovate erhöht dort den
  Image-Tag, Flux rollt aus. Es läuft ein einzelnes Replica mit `strategy: Recreate`, weil die SQLite-Datei auf einem
  RWO-Volume liegt.

## Tests

```sh
go test ./...                       # Normalisierung, API, Sync-Semantik, Rechte, CSRF, Limits, Nebenläufigkeit
node --test web/test/*.test.mjs     # Tipp-Engine (Stop-on-Error, CPM, Genauigkeit), Tastatur-Layouts/Finger
# E2E (Chromium mit virtuellem Passkey-Authenticator):
go run . -addr :8099 -origin http://localhost:8099 -db /tmp/e2e.db &
(cd e2e && npm install && npx playwright install chromium && node e2e.mjs)
```

## Architektur

### Datenmodell

`users`, `passkeys`, `texts` und `typing_sessions` wie im Plan ([docs/PLAN.md](docs/PLAN.md)), dazu zwei Ergänzungen:

* `passkeys.credential` speichert zusätzlich das komplette `webauthn.Credential` als JSON (Flags, Transports,
  Attestation). go-webauthn prüft beim Login die Backup-Flags, `public_key` und `sign_count` allein reichen dafür nicht.
* `auth_sessions` enthält die Login-Sitzungen. Gespeichert wird nur der SHA-256 des Cookie-Tokens.

### Texte

Beim Speichern wird normalisiert ([internal/textnorm](internal/textnorm/textnorm.go)): Unicode NFC, typografische
Anführungszeichen, Gedankenstriche und Auslassungspunkte werden zu ASCII, Leerraum wird zusammengefasst, Leerzeilen
fallen weg. Ein Zeilenumbruch entspricht genau einem Enter. Erlaubt sind danach nur Zeichen, die man auf einer
deutschen oder US-Tastatur tippen kann (druckbares ASCII, Latin-1 wie ä ö ü ß é § °, dazu €). Texte mit anderen
Zeichen (Emoji, CJK, …) werden mit einer Fehlermeldung abgelehnt, die die betroffenen Zeichen nennt. Positionen zählen in Unicode-Codepoints (Go-Runes = `Array.from()` in JS). Share-Slugs haben 6 Zeichen ohne
verwechselbare Zeichen (kein 0/o, 1/l/i).

### Sync-Protokoll

`PUT /api/sessions/{client-uuid}` mit `{text_id, cursor_pos, error_count, cpm, started_at, finished_at}`.

* Die Queue (`pending` in `localStorage`) hält pro Sitzung nur den letzten Stand (latest wins). Sie bleibt auch
  nach langer Offline-Phase klein.
* Alle Offline-Daten (Texte, Sitzungen, Queue) liegen unter `vt:u:<user-id>:`. Auf einem geteilten PC im Kursraum
  sieht oder synchronisiert ein Account nie die Daten eines anderen. Beim Abmelden wird zuerst synchronisiert; ist
  dann noch etwas offen, fragt die App nach. Danach werden die Daten des Nutzers gelöscht.
* Der Server ist idempotent und monoton: `cursor_pos` und `error_count` können nicht sinken, `finished_at` wird nur
  einmal gesetzt. Doppelte oder vertauschte Requests verfälschen den Fortschritt deshalb nicht. `cpm` übernimmt der
  Server nur aus Requests, deren `cursor_pos` nicht kleiner als der gespeicherte Wert ist. `updated_at` ist immer
  Serverzeit.
* Der Server prüft: Sitzung gehört dem Nutzer und dem Text, `cursor_pos ≤ Textlänge`, `cpm ≤ 3000`, `started_at`
  liegt nicht in der Zukunft, `finished_at` nur am Textende und nach `started_at`, höchstens 50 Sitzungen pro Nutzer
  und Text (sonst 429).
* Fehlerverhalten: Netzwerkfehler sowie 502/503/504 vom Reverse-Proxy (Backend startet neu) → „offline“, erneuter
  Versuch. 401 → Hinweis „neu anmelden“, Stand bleibt erhalten. Andere 5xx und 429 → Fehlermeldung, erneuter Versuch.
  Andere 4xx → Fehlermeldung, der Stand wird verworfen (ein erneuter Versuch kann nicht helfen).
* Liefert der Proxy bei einem Seitenaufruf 5xx, antwortet der Service Worker mit der gecachten App-Shell.

### Sicherheit

* Passkeys mit `residentKey: required` und `userVerification: required` (PIN/Biometrie). Ein Login wird abgelehnt,
  wenn go-webauthn eine Clone-Warnung meldet (Signaturzähler läuft rückwärts).
* CSRF: Jeder nicht-GET-Request muss einen `Origin`-Header haben, der genau `-origin` entspricht. JSON-Endpunkte
  verlangen `Content-Type: application/json`. `SameSite=Lax` allein reicht nicht, weil andere Subdomains von
  thomas-guettler.de als „same-site“ gelten.
* Gegen Speicher-DoS: Laufende Passkey-Zeremonien sind auf 10 000 begrenzt (danach 503), WebAuthn-Bodys auf 64 KiB,
  JSON-Bodys auf 256 KiB. Server-Timeouts sind gesetzt. Abgelaufene Zeremonien und Logins räumt stündlich ein
  Janitor ab.
* Fehlerdetails aus go-webauthn landen nur im Server-Log, nicht in der API-Antwort.
* CSP `default-src 'self'`, keine Inline-Skripte oder -Styles.

### Live-Ansicht

Der Ersteller fragt `GET /api/texts/{id}/sessions` alle 3 s ab. Als „tippt“ gilt eine Sitzung mit einem Sync in den
letzten 60 s.

## API

| Methode | Pfad | |
|---|---|---|
| POST | `/api/register/begin` `{display_name}` · `/api/register/finish` | Passkey anlegen = Account anlegen |
| POST | `/api/login/begin` · `/api/login/finish` | Passkey-Login (ohne Benutzernamen) |
| POST | `/api/logout` | |
| GET | `/api/me` | |
| GET/POST | `/api/texts` | eigene Texte / neuen Text anlegen |
| GET | `/api/texts/{id}` | jeder angemeldete Nutzer (Teilen per Link) |
| GET | `/api/texts/{id}/sessions` | Live-Ansicht, nur für den Ersteller |
| PUT | `/api/sessions/{id}` | Sync |
