# vibetype

Minimalistischer, schneller, offline-fähiger 10-Finger-Trainer mit Peer-to-Peer-Charakter:
jeder Nutzer kann Texte anlegen, per Link/Code teilen und sieht live, wer seinen Text gerade tippt.

* **Keine Rollen:** Jeder angemeldete Nutzer kann Texte erstellen und teilen. Der Ersteller eines Texts bekommt
  automatisch die Live-Ansicht aller Tipp-Sitzungen auf diesem Text.
* **Local-First:** Die Tipp-Logik läuft komplett im Browser. Ein Sync-Loop schickt den Fortschritt alle 3 s an den
  Server; offline wird gepuffert (`localStorage`), ein Service Worker hält die App-Shell vor.
* **Stop-on-Error:** Bei einem Tippfehler bleibt der Cursor stehen und das Zeichen „wackelt“ rot, bis die richtige
  Taste kommt.
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

## Tests

```sh
go test ./...                       # Normalisierung, API, Sync-Semantik, Rechte
node --test web/test/*.test.mjs     # Tipp-Engine (Stop-on-Error, CPM, Genauigkeit)
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
fallen weg. Jedes Zeichen ist damit auf einer normalen Tastatur tippbar, ein Zeilenumbruch entspricht genau einem
Enter. Positionen zählen in Unicode-Codepoints (Go-Runes = `Array.from()` in JS). Share-Slugs haben 6 Zeichen ohne
verwechselbare Zeichen (kein 0/o, 1/l/i).

### Sync-Protokoll

`PUT /api/sessions/{client-uuid}` mit `{text_id, cursor_pos, error_count, cpm, started_at, finished_at}`.

* Die Queue (`vt:pending` in `localStorage`) hält pro Sitzung nur den letzten Stand (latest wins). Sie bleibt auch
  nach langer Offline-Phase klein.
* Der Server ist idempotent und monoton: `cursor_pos` und `error_count` können nicht sinken, `finished_at` wird nur
  einmal gesetzt. Reihenfolge und Wiederholung von Requests sind deshalb egal. `updated_at` ist immer Serverzeit.
* Der Server prüft: Sitzung gehört dem Nutzer und dem Text, `cursor_pos ≤ Textlänge`, `finished_at` nur am Textende.
* Fehlerverhalten: Netzwerkfehler → „offline“, erneuter Versuch. 401 → Hinweis „neu anmelden“, Stand bleibt erhalten.
  5xx → Fehlermeldung, erneuter Versuch. Andere 4xx → Fehlermeldung, der Stand wird verworfen (ein erneuter Versuch
  kann nicht helfen).

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
