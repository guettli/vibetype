// Package lessons contains the built-in beginner course for the German
// QWERTZ layout. Each lesson introduces a few keys and only uses keys of the
// lessons before it (enforced by lessons_test.go).
package lessons

// Lesson is one built-in exercise.
type Lesson struct {
	ID      string // stable text id, also used in /t/{id}
	Title   string
	NewKeys string // keys introduced by this lesson; "⇧" = capitals of all keys learned so far
	// DrillLines is the number of leading lines of Content that are finger
	// drills ("fj dk sl"), not words; practice texts take words from the rest.
	DrillLines int
	Content    string
}

// SystemUserID owns the lesson texts. It has no passkey and cannot log in.
const (
	SystemUserID   = "system"
	SystemUserName = "vibetype"
)

// All lessons in course order.
var All = []Lesson{
	{
		ID: "lek01", Title: "Lektion 1: Grundreihe asdf jklö", NewKeys: "asdfjklö ",
		DrillLines: 4,
		Content: `asdf jklö asdf jklö
fff jjj ddd kkk sss lll aaa ööö
fj dk sl aö fj dk sl aö
fdsa jklö fdsa jklö
das als all lass fall
ja das fass saal dass falls
ja das all als lass fall`,
	},
	{
		ID: "lek02", Title: "Lektion 2: g und h", NewKeys: "gh",
		DrillLines: 2,
		Content: `fg jh fg jh fgf jhj fgf jhj
gag hah gag hah
gas glas hals half hass
lag sag fahl kahl gas
das glas als lass hals
das glas lag da`,
	},
	{
		ID: "lek03", Title: "Lektion 3: e und i", NewKeys: "ei",
		DrillLines: 1,
		Content: `de ki de ki ded kik ded kik
die sie lies fiel lief
alle elf idee kiel kies
hilfe seife eile heide seide
sie sah die fee
sie lief leise als die geige fiel
die fee lief leise`,
	},
	{
		ID: "lek04", Title: "Lektion 4: r und u", NewKeys: "ru",
		DrillLines: 1,
		Content: `fr ju fr ju frf juj frf juj
rad uhr rufe raus kurs
regel ruhig frage erde lauf
kauf grau faul hause fuhr
der hase lief raus
sie rief ihr ruhig aus der luke
aus der erde rief er hilfe`,
	},
	{
		ID: "lek05", Title: "Lektion 5: t und z", NewKeys: "tz",
		DrillLines: 1,
		Content: `ft jz ft jz ftf jzj ftf jzj
tag zeit satz katze kurz
salz jetzt tasse zug zahl
herz stadt letzte zahl tat
der tag hat zeit
jetzt ist die katze ruhig
der hase sitzt still`,
	},
	{
		ID: "lek06", Title: "Lektion 6: o und w", NewKeys: "ow",
		DrillLines: 1,
		Content: `lo sw lo sw lol sws lol sws
wort wolke oder wasser wald
wo rot hose oft zwei
wolf dort sofort wetter wo
ist das wetter dort gut
wo ist der wolf heute
sofort ruft er zwei worte`,
	},
	{
		ID: "lek07", Title: "Lektion 7: n, m, Komma und Punkt", NewKeys: "nm,.",
		DrillLines: 1,
		Content: `jn jm k, l. jn jm k, l.
mann name morgen nein nun
immer mond nase man mit
am morgen, am mittag, um zehn.
nein, nun ist es zu warm.
immer wenn der mond da ist, ist es still.
der mann nimmt die tasse, dann geht er.`,
	},
	{
		ID: "lek08", Title: "Lektion 8: Großbuchstaben", NewKeys: "⇧",
		DrillLines: 1,
		Content: `Aa Ss Dd Ff Jj Kk Ll Öö
Anna Hans Ella Tom Otto Ute
Der Mond. Die Sonne. Ein Tag.
Am Morgen ist es kalt, am Mittag warm.
Hans und Ella gehen in den Wald.
Otto ruft Anna, dann rennt er heim.
Ute sieht den Mond. Er ist rund und hell.`,
	},
	{
		ID: "lek09", Title: "Lektion 9: c, v, b, x, y", NewKeys: "cvbxy",
		DrillLines: 1,
		Content: `dc fv fb sx ay dc fv fb sx ay
ich mich dich auch noch nach
buch bach sechs vier viel
vogel bett liebe axt text hexe yeti
Ich lese ein Buch am Bach.
Viele Vögel sitzen am Bach.
Der Text ist lang, aber sehr schön.`,
	},
	{
		ID: "lek10", Title: "Lektion 10: p, ü, ä, q, ß", NewKeys: "püäqß",
		DrillLines: 1,
		Content: `öp öü öä aq öß öp öü öä aq öß
über für müde spät quelle
quer straße fuß groß weiß
platz papier äpfel ärger süß
Für mich ist die Straße zu groß.
Die Quelle ist weiß und kühl.
Später essen wir süße Äpfel im Park.`,
	},
	{
		ID: "lek11", Title: "Lektion 11: Zahlen", NewKeys: "1234567890",
		DrillLines: 3,
		Content: `1 2 3 4 5 6 7 8 9 0
12 34 56 78 90 10 20 30
11 22 33 44 55 66 77 88 99 00
Ein Jahr hat 365 Tage und 12 Monate.
Der Kurs beginnt um 8 Uhr.
Wir sind 25 Kinder in 3 Gruppen.
Der Zug fährt um 14.30 Uhr ab Gleis 7.`,
	},
	{
		ID: "lek12", Title: "Lektion 12: Satzzeichen", NewKeys: `!?"'-():;/`,
		DrillLines: 0,
		Content: `Wie geht es dir? Gut, danke!
"Hallo", sagte sie. 'Hallo', sagte er.
Es ist 10:30 Uhr; wir sind da.
Das Haus (das alte) ist leer.
Das ist ein Wort - und noch eins.
Kommst du mit? Ja! Nein? Doch!
Die Datei liegt in kurs/eins/text.`,
	},
	{
		ID: "lek13", Title: "Lektion 13: Sonderzeichen und AltGr", NewKeys: "&%$€@#+*=_<>",
		DrillLines: 0,
		Content: `50 % von 20 € sind 10 €.
Ein Buch kostet 12 $ oder 11 €.
Schreib an info@example.org.
Das Thema ist #vibetype.
Rechne: 3 + 4 = 7 und 2 * 6 = 12.
3 < 5 und 9 > 7.
Max & Moritz schreiben kurs_eins.`,
	},
}

// Next returns the id of the lesson after id, or "" if id is the last lesson or not a lesson.
func Next(id string) string {
	for i, l := range All {
		if l.ID == id && i+1 < len(All) {
			return All[i+1].ID
		}
	}
	return ""
}
