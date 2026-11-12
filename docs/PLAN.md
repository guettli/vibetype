# vibetype – MVP Architektur- & Umsetzungsplan

Ein minimalistisches, schnelles und offline-fähiges Programm zum Lernen des 10-Finger-Tastsystems mit
Peer-to-Peer-Charakter (jeder Nutzer kann Texte erstellen, teilen und live beobachten).

## 1. Kernkonzept & Paradigmen

* **Keine Rollen-Trennung (Peer-to-Peer):** Es gibt keine expliziten "Lehrer"- oder "Schüler"-Accounts. Jeder
  authentifizierte Nutzer kann Texte anlegen, per Link/Code teilen und erhält automatisch eine Live-Ansicht über alle
  Personen, die diesen Text gerade tippen.
* **Local-First & Autark:** Die Tipp-Logik läuft zu 100 % lokal im Browser (0 ms Netzwerk-Latenz beim Tippen). Ein
  Hintergrund-Prozess synchronisiert den Fortschritt alle N Sekunden (z. B. N = 3) mit dem Server.
* **Stop-on-Error Mechanik:** Bei einem Tippfehler blockiert der Cursor an der aktuellen Position. Es erfolgt ein
  sofortiges visuelles Signal (z. B. kurzes rotes Aufblitzen / Shake-Effekt des Zeichens), bis die korrekte Taste
  gedrückt wird.

## 2. Tech-Stack

* **Domain:** vibetype.thomas-guettler.de (wichtig für eine saubere WebAuthn rpId-Bindung)
* **Backend:** Go (Golang)
  * Single Binary Deployment (inklusive eingebetteter statischer Frontend-Assets via `embed.FS`).
  * Datenbank: SQLite im WAL-Mode (`modernc.org/sqlite`, CGO-frei) für hohe nebenläufige Lese-/Schreib-Performance
    beim N-Sekunden-Sync.
  * Auth: WebAuthn / Passkeys via `github.com/go-webauthn/webauthn` (Passwortlos, Session-Cookie über HTTP-Only).
* **Frontend:** Preact + HTM (oder Alpine.js / Vanilla JS)
  * Kein Build-Step nötig (ES Modules direkt im Browser, < 5 KB Footprint).
  * Local Storage / IndexedDB: Speichert geladene Texte sowie die Queue der noch nicht synchronisierten
    Tipp-Zustände, falls das WLAN im Kursraum kurz ausfällt.
  * CSS: Minimalistisches Custom CSS oder PicoCSS für saubere Typografie (Monospace-Font für den Tipp-Bereich).

## 3. Datenmodell (SQLite MVP)

1. `users`: `id` (TEXT, UUID/ULID, PK), `display_name` (TEXT), `created_at` (DATETIME)
2. `passkeys`: `id` (TEXT, Credential ID), `user_id` (FK), `public_key` (BLOB), `sign_count` (INTEGER),
   `created_at` (DATETIME)
3. `texts`: `id` (TEXT, kurzer Share-Slug wie `a7x9k2`, PK), `creator_id` (FK → users.id), `title` (TEXT),
   `content` (TEXT, normalisiert), `created_at` (DATETIME)
4. `typing_sessions`: `id` (TEXT, Client-generierte UUID, PK), `text_id` (FK), `user_id` (FK), `cursor_pos`
   (INTEGER), `error_count` (INTEGER), `cpm` (INTEGER), `started_at`, `updated_at` (letzter Sync),
   `finished_at` (NULLABLE)

## 4. Architektur: Autarker Betrieb & N-Sekunden-Sync

### 4.1 Tipp-Engine im Browser

1. Sobald ein Text über `/t/{text_id}` geöffnet wird, speichert der Browser ihn im `localStorage`.
2. Ein globaler `keydown`-Listener fängt alle Eingaben ab (Ignorieren von Modifier-Keys wie Shift, Alt, Control).
3. Vergleich:
   * Korrekt: `cursor_pos++`, Zeitstempel für CPM-Berechnung aktualisieren, dirty-Flag für den Sync setzen.
   * Falsch: `error_count++`, CSS-Klasse `.error-flash` auf dem aktuellen Zeichen triggern, dirty-Flag setzen.
     Cursor bleibt stehen.

### 4.2 Sync-Loop (Client → Server)

Umsetzung und Ergänzungen: siehe [README](../README.md#sync-protokoll).
