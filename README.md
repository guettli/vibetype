# vibetype

Minimalistischer, schneller, offline-fähiger 10-Finger-Trainer mit Peer-to-Peer-Charakter:
jeder Nutzer kann Texte anlegen, per Link/Code teilen und sieht live, wer seinen Text gerade tippt.

* **Keine Rollen:** Jeder angemeldete Nutzer kann Texte erstellen und teilen. Der Ersteller eines Texts bekommt
  automatisch die Live-Ansicht aller Tipp-Sitzungen auf diesem Text.
* **Local-First:** Die Tipp-Logik läuft komplett im Browser. Ein Sync-Loop schickt den Fortschritt alle 3 s an den
  Server; offline wird gepuffert (`localStorage`), ein Service Worker hält die App-Shell vor.
* **Pausen zählen nicht:** Anschläge/min beziehen sich auf die aktive Tippzeit. Eine Lücke zwischen zwei Anschlägen
  zählt höchstens bis zur Pausenschwelle. Die liegt bei mindestens 3 Sekunden oder beim Vierfachen des eigenen
  Rhythmus, falls das mehr ist. Wer für jede Taste 5 Sekunden braucht, macht also keine „Pausen“ und wird nicht
  schöngerechnet. Eine Pause zum Lesen, Reden oder wegen der Schulglocke drückt dagegen nicht die Geschwindigkeit. Die
  Zusammenfassung zeigt Tippzeit und Pausenzeit getrennt, die Ergebnisse zeigen die Tippzeit. Der Client sendet sie als
  `active_ms`. Durchgänge älterer Versionen haben kein `active_ms`, ihre Anschläge/min enthalten Pausen und sind nicht
  vergleichbar. Deshalb vergleicht die Bestleistung nur gleich gemessene Durchgänge, und neu gemessene gehen beim ★ vor.
  Hat ein Durchgang Anschläge ohne Zeitmessung (alter Tab, zweiter Tab auf demselben Text), zählt für ihn die
  Gesamtdauer.
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
* **Konto:** Unter `/konto` (Klick auf den eigenen Namen) lässt sich der Anzeigename ändern. Dort kann man auch weitere
  Passkeys anlegen, etwa für das Handy oder den PC zu Hause, falls der Passkey nicht ohnehin über iCloud oder Google
  synchronisiert wird. Bereits registrierte Authenticators werden dabei ausgeschlossen. Passkeys lassen sich mit
  Bezeichnung, Anlage- und Nutzungsdatum auflisten und löschen, der letzte jedoch nicht, weil man sich sonst aussperrt.
  Hinzufügen und Löschen verlangen eine Anmeldung aus den letzten 10 Minuten. Sonst fragt die App den Passkey erneut ab
  und wiederholt die Aktion. So kann ein gestohlenes Sitzungs-Cookie nicht in einen dauerhaften Passkey des Angreifers
  verwandelt werden. Beim Löschen eines Passkeys werden alle anderen Anmeldungen des Kontos beendet.
* **Klassenraum-Modus:** Der Ersteller eines Texts sieht über der Live-Ansicht einen QR-Code mit dem Teilen-Link,
  dazu den Code und die URL. „Präsentieren“ schaltet diese Karte in Vollbild mit großer Schrift für den Beamer. Die
  Schüler scannen den QR-Code oder tippen den Code ein. Die Live-Ansicht ist nach Fortschritt sortiert, bei Gleichstand
  nach Anschlägen/min. Den QR-Code erzeugt der Server als SVG (`rsc.io/qr`), er bleibt bei jeder Größe scharf. Er enthält immer den konfigurierten `-origin`, also den
  Host, an den die Passkeys gebunden sind, auch wenn die Seite über einen anderen Hostnamen geöffnet wurde. Ohne
  Fullscreen-API (iPhone) überdeckt die Karte einfach das Fenster.
* **Lektionen:** Ein eingebauter Kurs mit 13 Lektionen für die deutsche QWERTZ-Tastatur. Er beginnt mit der
  Grundreihe (asdf jklö) und führt dann Lektion für Lektion neue Tasten ein: obere und untere Reihe, Großbuchstaben,
  Zahlen, Satz- und Sonderzeichen. Jede Lektion nutzt nur Tasten, die vorher schon eingeführt wurden. Das prüft
  [lessons_test.go](internal/lessons/lessons_test.go). Die Lektionen sind normale Texte (IDs `lek01`…`lek13`) eines
  Systemnutzers „vibetype“, der sich nicht anmelden kann. Der Server legt sie beim Start an bzw. aktualisiert sie.
  Ändert sich der Inhalt einer Lektion, löscht der Server beim Start die Sitzungen darauf (mit Warnung im Log). Der
  Browser merkt beim nächsten Laden, dass sich der Text geändert hat, setzt den lokalen Fortschritt zurück und sagt
  das. Lektionen, die aus dem Kurs entfernt wurden, verschwinden samt Sitzungen. Abgeschlossene Lektionen zeigt die
  Startseite mit ✓ und der Bestleistung. Nach dem Abschluss führt ein Link zur nächsten Lektion. Der Anzeigename
  „vibetype“ ist reserviert, damit kein Nutzer offiziell wirkt.
* **Problemtasten:** Bei jedem Fehler merkt sich die App, welches Zeichen gerade dran war (`key_errors` in der
  Sitzung). Die Startseite zeigt die 5 Zeichen mit der höchsten Fehlerquote der letzten 30 Tage (Fehler ÷ Versuche,
  Versuche = korrekt getippte Vorkommen + Fehler). Bewertet wird ein Zeichen erst ab 10 Versuchen. Dazu kommt der
  Fingerhinweis für das gewählte Layout. „Übungstext für diese Tasten erstellen“ legt einen eigenen Text an: Pro
  Problemtaste gibt es zwei Zeilen mit Wörtern aus den Lektionen, die das Zeichen enthalten (ohne die Fingerübungs-Zeilen
  der Lektionen). Bei weniger als drei passenden Wörtern, etwa für Ziffern oder Sonderzeichen, kommen Drill-Gruppen
  dazu. Für die Leertaste gibt es Zeilen mit kurzen Wörtern, für Enter viele kurze Zeilen. Von den erzeugten
  Übungstexten bleiben pro Nutzer die neuesten 3 erhalten, ältere werden samt Sitzungen gelöscht. Sitzungen mit Fehlern,
  aber ohne Aufschlüsselung pro Zeichen (vor dieser Funktion oder von alten Clients), zählen nicht zur Quote, sonst
  würden sie die Quote verwässern.
* **Eigene Ergebnisse:** Die Startseite listet die letzten 200 eigenen Tipp-Durchgänge, auch nicht beendete (Datum,
  Text, Fortschritt, Anschläge/min, Fehler, Genauigkeit, Zeit). Die Bestleistung pro Text (beendet, höchste
  Anschläge/min) ist mit ★ markiert. Der Server berechnet sie über alle Durchgänge, nicht nur über die angezeigten.
  Nach dem Abschluss eines Texts zeigt eine Zusammenfassung das Ergebnis und vergleicht es mit der bisherigen
  Bestleistung. Dafür zählen die Durchgänge auf dem Server und die lokale Historie dieses Browsers, also auch noch
  nicht synchronisierte oder offline beendete Durchgänge.
* **Texte löschen:** Der Ersteller kann einen Text löschen. Dabei werden auch alle Tipp-Sitzungen aller Personen auf
  diesem Text gelöscht (mit Rückfrage). Wer den Text noch offline im Browser hat, bekommt beim nächsten Sync den
  Hinweis, dass der Text gelöscht wurde. Seine lokalen Daten zu diesem Text werden dann verworfen.
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
* `typing_sessions.key_errors` (JSON) zählt die Fehler pro Zeichen. Ältere Datenbanken bekommen die Spalte beim Start
  per `ALTER TABLE` nachgerüstet.
* `texts.kind` ist `''` für selbst geschriebene Texte und `practice` für erzeugte Übungstexte (nachgerüstet wie
  `key_errors`).
* `passkeys.label` und `passkeys.last_used_at` (wird bei jedem Login gesetzt) sind ebenfalls nachgerüstet.
* `auth_sessions` enthält die Login-Sitzungen. Gespeichert wird nur der SHA-256 des Cookie-Tokens.

### Texte

Beim Speichern wird normalisiert ([internal/textnorm](internal/textnorm/textnorm.go)): Unicode NFC, typografische
Anführungszeichen, Gedankenstriche und Auslassungspunkte werden zu ASCII, Leerraum wird zusammengefasst, Leerzeilen
fallen weg. Ein Zeilenumbruch entspricht genau einem Enter. Erlaubt sind danach nur Zeichen, die man auf einer
deutschen oder US-Tastatur tippen kann (druckbares ASCII, Latin-1 wie ä ö ü ß é § °, dazu €). Texte mit anderen
Zeichen (Emoji, CJK, …) werden mit einer Fehlermeldung abgelehnt, die die betroffenen Zeichen nennt. Positionen zählen in Unicode-Codepoints (Go-Runes = `Array.from()` in JS). Share-Slugs haben 6 Zeichen ohne
verwechselbare Zeichen (kein 0/o, 1/l/i).

### Sync-Protokoll

`PUT /api/sessions/{client-uuid}` mit `{text_id, cursor_pos, error_count, cpm, started_at, finished_at, key_errors, active_ms}`.
`active_ms` ist optional oder `null` und muss zwischen 0 und der verstrichenen Zeit liegen (1 Minute Toleranz).
`key_errors` ist optional (ältere Clients senden es nicht). Jeder Schlüssel muss genau ein Zeichen sein, das im Text
vorkommt, jeder Wert muss zwischen 1 und `error_count` liegen, und die Summe darf `error_count` nicht übersteigen.

* Die Queue (`pending` in `localStorage`) hält pro Sitzung nur den letzten Stand (latest wins). Sie bleibt auch
  nach langer Offline-Phase klein.
* Alle Offline-Daten (Texte, Sitzungen, Queue) liegen unter `vt:u:<user-id>:`. Auf einem geteilten PC im Kursraum
  sieht oder synchronisiert ein Account nie die Daten eines anderen. Beim Abmelden wird zuerst synchronisiert; ist
  dann noch etwas offen, fragt die App nach. Danach werden die Daten des Nutzers gelöscht.
* Der Server ist idempotent und monoton: `cursor_pos` und `error_count` können nicht sinken, `finished_at` wird nur
  einmal gesetzt. Doppelte oder vertauschte Requests verfälschen den Fortschritt deshalb nicht. `cpm` übernimmt der
  Server nur aus Requests, deren `cursor_pos` nicht kleiner als der gespeicherte Wert ist. `updated_at` ist immer
  Serverzeit.
* Der Server prüft: Sitzung gehört dem Nutzer und dem Text, `cursor_pos ≤ Textlänge`, `cpm` nicht negativ (Werte über 3000 kappt der Server auf 3000 und loggt sie, damit der Fortschritt nicht verloren geht. Der Client kappt genauso und weist in der Zusammenfassung darauf hin), `started_at`
  liegt nicht in der Zukunft, `finished_at` nur am Textende und nach `started_at`, höchstens 50 Sitzungen pro Nutzer
  und Text. Bei mehr löscht der Server die ältesten, die beste beendete Sitzung bleibt aber immer erhalten. So kann man
  eine Lektion beliebig oft wiederholen.
* Ein 404 beim Sync bedeutet, dass der Text gelöscht wurde. Die App verwirft dann alle lokalen Daten zu diesem Text und
  sagt das dem Nutzer. Löscht jemand den Text, während ein Sync läuft, prüft der Server das innerhalb der Transaktion
  und antwortet ebenfalls mit 404.
* Fehlerverhalten: Netzwerkfehler sowie 502/503/504 vom Reverse-Proxy (Backend startet neu) → „offline“, erneuter
  Versuch. 401 → Hinweis „neu anmelden“, Stand bleibt erhalten. Andere 5xx und 429 → Fehlermeldung, erneuter Versuch. Jeder Request hat ein Timeout von 15 s und zählt danach als offline. Gleichzeitige Sync-Aufrufe werden zusammengefasst, nicht gestapelt.
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

Der Ersteller fragt `GET /api/texts/{id}/sessions` alle 3 s ab (Anzeige sortiert nach Fortschritt). Als „tippt“ gilt eine Sitzung mit einem Sync in den
letzten 60 s.

## API

| Methode | Pfad | |
|---|---|---|
| POST | `/api/register/begin` `{display_name}` · `/api/register/finish` | Passkey anlegen = Account anlegen |
| POST | `/api/login/begin` · `/api/login/finish` | Passkey-Login (ohne Benutzernamen) |
| POST | `/api/logout` | |
| PUT | `/api/me` `{display_name}` | Anzeigename ändern |
| GET | `/api/me/passkeys` | eigene Passkeys (`id`, `label`, `created_at`, `last_used_at`) |
| POST | `/api/me/passkeys/begin` `{label}` · `/api/me/passkeys/finish` | weiteren Passkey anlegen |
| DELETE | `/api/me/passkeys/{id}` | Passkey löschen; 409 beim letzten; beendet alle anderen Anmeldungen |

Änderungen an Passkeys verlangen eine Anmeldung aus den letzten 10 Minuten, sonst kommt `403 {"code": "reauth_required"}`.
| GET | `/api/me` | |
| GET/POST | `/api/texts` | eigene Texte / neuen Text anlegen |
| GET | `/api/texts/{id}` | jeder angemeldete Nutzer (Teilen per Link); bei Lektionen zusätzlich `next_lesson` |
| GET | `/api/texts/{id}/sessions` | Live-Ansicht, nur für den Ersteller |
| GET | `/api/lessons` | eingebaute Lektionen in Kursreihenfolge (`id`, `title`, `length`) |
| GET | `/api/me/weak-keys` | Problemtasten: `keys` (`char`, `errors`, `attempts`, `rate`), `days`, `min_attempts` |
| POST | `/api/me/practice` | erstellt einen Übungstext für die Problemtasten (201) oder 409, solange es keine gibt |
| GET | `/api/texts/{id}/qr.svg` | QR-Code des Teilen-Links (`<origin>/t/{id}`) als SVG |
| DELETE | `/api/texts/{id}` | nur der Ersteller; löscht auch alle Sitzungen auf dem Text |
| GET | `/api/me/results[?text_id=…]` | eigene Sitzungen mit Texttitel und -länge, neueste zuerst (max. 200), dazu `best` (pro Text die beste beendete Sitzung über alle Sitzungen: `id`, `text_id`, `cpm`) |
| PUT | `/api/sessions/{id}` | Sync |
