package tui

import "maps"

// The texts of the Projects area. They join the catalogs when the package starts, so this area adds its words
// in one place.
const (
	tProjEmpty         textID = "ProjEmpty"
	tProjNoSel         textID = "ProjNoSel"
	tProjDetail        textID = "ProjDetail"
	tProjLoading       textID = "ProjLoading"
	tProjColName       textID = "ProjColName"
	tProjColSource     textID = "ProjColSource"
	tProjColCurrent    textID = "ProjColCurrent"
	tProjColAvailable  textID = "ProjColAvailable"
	tProjColMode       textID = "ProjColMode"
	tProjColState      textID = "ProjColState"
	tProjColLastRun    textID = "ProjColLastRun"
	tProjApproved      textID = "ProjApproved"
	tProjPending       textID = "ProjPending"
	tProjError         textID = "ProjError"
	tProjRunning       textID = "ProjRunning"
	tProjTabOverview   textID = "ProjTabOverview"
	tProjTabDetails    textID = "ProjTabDetails"
	tProjTabRaw        textID = "ProjTabRaw"
	tProjLblCheck      textID = "ProjLblCheck"
	tProjLblFailedStep textID = "ProjLblFailedStep"
	tProjLblDir        textID = "ProjLblDir"
	tProjLblTemplate   textID = "ProjLblTemplate"
	tProjLblEnv        textID = "ProjLblEnv"
	tProjLblCompose    textID = "ProjLblCompose"
	tProjLblPoll       textID = "ProjLblPoll"
	tProjLblRevision   textID = "ProjLblRevision"
	tProjCheckFailed   textID = "ProjCheckFailed"
	tProjNoRun         textID = "ProjNoRun"
	tProjNoFile        textID = "ProjNoFile"
	tProjNeedFile      textID = "ProjNeedFile"
	tProjSourceUnread  textID = "ProjSourceUnread"
	tProjUpToDate      textID = "ProjUpToDate"
	tProjNewer         textID = "ProjNewer"
	tProjRolledBack    textID = "ProjRolledBack"
	tProjProblems      textID = "ProjProblems"

	tProjChanged     textID = "ProjChanged"
	tProjGone        textID = "ProjGone"
	tProjNotApproved textID = "ProjNotApproved"
	tProjIsPaused    textID = "ProjIsPaused"
	tProjIsPinned    textID = "ProjIsPinned"
	tProjPaused      textID = "ProjPaused"
	tProjResumed     textID = "ProjResumed"
	tProjPinned      textID = "ProjPinned"
	tProjUnpinned    textID = "ProjUnpinned"
	tProjRunPending  textID = "ProjRunPending"
	tProjRunSkipped  textID = "ProjRunSkipped"
	tProjRunResult   textID = "ProjRunResult"
	tProjRunNothing  textID = "ProjRunNothing"
	tProjRemoved     textID = "ProjRemoved"
	tBusyRun         textID = "BusyRun"
	tBusySource      textID = "BusySource"
	tBusyLog         textID = "BusyLog"

	tProjRunTitle    textID = "ProjRunTitle"
	tProjRunNote     textID = "ProjRunNote"
	tProjDeleteTitle textID = "ProjDeleteTitle"
	tProjDeleteNote  textID = "ProjDeleteNote"
	tProjSourceTitle textID = "ProjSourceTitle"
	tProjSourceNote  textID = "ProjSourceNote"
	tProjSourceDone  textID = "ProjSourceDone"
	tProjLogTitle    textID = "ProjLogTitle"
	tProjLogCut      textID = "ProjLogCut"
	tProjLogNone     textID = "ProjLogNone"
	tProjLogGone     textID = "ProjLogGone"
	tSrcDenied       textID = "SrcDenied"
	tSrcNotFound     textID = "SrcNotFound"
	tSrcUnreachable  textID = "SrcUnreachable"
	tSrcTimeout      textID = "SrcTimeout"
	tSrcCredential   textID = "SrcCredential"
	tSrcInvalid      textID = "SrcInvalid"
)

// projectWords are the words of the area that are drawn as they come from the service or the files: they are
// the same in English, and translated below.
var projectWords = []string{"images", "git", "auto", "manual", "mixed", "paused", "pinned", "locked", "restore",
	"succeeded", "failed", "rolled-back", "rollback-failed"}

func init() {
	maps.Copy(textEN, map[textID]string{
		tProjEmpty:         "No projects yet.",
		tProjNoSel:         "No project selected.",
		tProjDetail:        "Project",
		tProjLoading:       "loading",
		tProjColName:       "Name",
		tProjColSource:     "Source",
		tProjColCurrent:    "Current",
		tProjColAvailable:  "Available",
		tProjColMode:       "Mode",
		tProjColState:      "State",
		tProjColLastRun:    "Last run",
		tProjApproved:      "approved",
		tProjPending:       "pending",
		tProjError:         "error",
		tProjRunning:       "running",
		tProjTabOverview:   "Overview",
		tProjTabDetails:    "Details",
		tProjTabRaw:        "Raw",
		tProjLblCheck:      "Last check",
		tProjLblFailedStep: "Failed step",
		tProjLblDir:        "Directory",
		tProjLblTemplate:   "Template",
		tProjLblEnv:        "Env file",
		tProjLblCompose:    "Compose",
		tProjLblPoll:       "Poll interval",
		tProjLblRevision:   "Revision",
		tProjCheckFailed:   "failed",
		tProjNoRun:         "no run yet",
		tProjNoFile:        "The project file cannot be read.",
		tProjNeedFile:      "The project is not loaded yet. Press ctrl+r.",
		tProjSourceUnread:  "cannot be read",
		tProjUpToDate:      "%s (up to date)",
		tProjNewer:         "%s → %s (%d newer)",
		tProjRolledBack:    "%s was rolled back",
		tProjProblems:      "%d problem(s)",
		tProjChanged:       "The project changed meanwhile. Reload with ctrl+r.",
		tProjGone:          "The project does not exist any more.",
		tProjNotApproved:   "The project is not approved yet.",
		tProjIsPaused:      "The project is paused.",
		tProjIsPinned:      "The project is pinned.",
		tProjPaused:        "%s paused.",
		tProjResumed:       "%s resumed.",
		tProjPinned:        "%s pinned.",
		tProjUnpinned:      "%s unpinned.",
		tProjRunPending:    "%s: the run is still going.",
		tProjRunSkipped:    "%s: not run (%s).",
		tProjRunResult:     "%s: run %s.",
		tProjRunNothing:    "%s: nothing to deploy.",
		tProjRemoved:       "%s removed.",
		tBusyRun:           "Running %s",
		tBusySource:        "Testing the sources of %s",
		tBusyLog:           "Reading the log of %s",
		tProjRunTitle:      "Run %s now?",
		tProjRunNote:       "Checks the sources and deploys what the version rules allow.",
		tProjDeleteTitle:   "Delete %s?",
		tProjDeleteNote:    "The project file is removed and the project no longer runs.",
		tProjSourceTitle:   "Sources of %s",
		tProjSourceNote:    "Versions read from the registries and repositories.",
		tProjSourceDone:    "Source test of %s finished.",
		tProjLogTitle:      "Log %s · %s",
		tProjLogCut:        "The log is cut: its end is shown.",
		tProjLogNone:       "This project has no run yet.",
		tProjLogGone:       "The log of the run is gone.",
		tSrcDenied:         "access denied",
		tSrcNotFound:       "not found",
		tSrcUnreachable:    "unreachable",
		tSrcTimeout:        "timed out",
		tSrcCredential:     "credential problem",
		tSrcInvalid:        "invalid source",

		footerID("space mark"):                  "space mark",
		footerID("enter detail"):                "enter detail",
		footerID("d delete"):                    "d delete",
		footerID("o log"):                       "o log",
		footerID("left/right tab"):              "left/right tab",
		footerID("{back} list"):                 "{back} list",
		footerID("pgup/pgdown page"):            "pgup/pgdown page",
		footerID("home/end ends"):               "home/end ends",
		footerID("y run"):                       "y run",
		footerID("n/{back} cancel"):             "n/{back} cancel",
		footerID("run/pause/pin via {command}"): "run/pause/pin via {command}",
	})
	maps.Copy(textDE, map[textID]string{
		tProjEmpty:         "Noch keine Projekte.",
		tProjNoSel:         "Kein Projekt gewählt.",
		tProjDetail:        "Projekt",
		tProjLoading:       "lädt",
		tProjColName:       "Name",
		tProjColSource:     "Quelle",
		tProjColCurrent:    "Aktuell",
		tProjColAvailable:  "Verfügbar",
		tProjColMode:       "Modus",
		tProjColState:      "Zustand",
		tProjColLastRun:    "Letzter Lauf",
		tProjApproved:      "freigegeben",
		tProjPending:       "wartet",
		tProjError:         "Fehler",
		tProjRunning:       "läuft",
		tProjTabOverview:   "Übersicht",
		tProjTabDetails:    "Details",
		tProjTabRaw:        "Raw",
		tProjLblCheck:      "Letzte Prüfung",
		tProjLblFailedStep: "Fehlerschritt",
		tProjLblDir:        "Verzeichnis",
		tProjLblTemplate:   "Vorlage",
		tProjLblEnv:        "Env-Datei",
		tProjLblCompose:    "Compose",
		tProjLblPoll:       "Intervall",
		tProjLblRevision:   "Revision",
		tProjCheckFailed:   "fehlgeschlagen",
		tProjNoRun:         "noch kein Lauf",
		tProjNoFile:        "Die Projektdatei ist nicht lesbar.",
		tProjNeedFile:      "Das Projekt ist noch nicht geladen. ctrl+r lädt neu.",
		tProjSourceUnread:  "nicht lesbar",
		tProjUpToDate:      "%s (aktuell)",
		tProjNewer:         "%s → %s (%d neuer)",
		tProjRolledBack:    "%s wurde zurückgerollt",
		tProjProblems:      "%d Problem(e)",
		tProjChanged:       "Das Projekt hat sich inzwischen geändert. ctrl+r lädt neu.",
		tProjGone:          "Das Projekt gibt es nicht mehr.",
		tProjNotApproved:   "Das Projekt ist noch nicht freigegeben.",
		tProjIsPaused:      "Das Projekt ist pausiert.",
		tProjIsPinned:      "Das Projekt ist angeheftet.",
		tProjPaused:        "%s pausiert.",
		tProjResumed:       "%s fortgesetzt.",
		tProjPinned:        "%s angeheftet.",
		tProjUnpinned:      "%s gelöst.",
		tProjRunPending:    "%s: Der Lauf dauert noch.",
		tProjRunSkipped:    "%s: nicht gelaufen (%s).",
		tProjRunResult:     "%s: Lauf %s.",
		tProjRunNothing:    "%s: nichts zu aktualisieren.",
		tProjRemoved:       "%s gelöscht.",
		tBusyRun:           "%s läuft",
		tBusySource:        "Quellen von %s werden geprüft",
		tBusyLog:           "Log von %s wird gelesen",
		tProjRunTitle:      "%s jetzt ausführen?",
		tProjRunNote:       "Prüft die Quellen und aktualisiert, was die Versionsregeln erlauben.",
		tProjDeleteTitle:   "%s löschen?",
		tProjDeleteNote:    "Die Projektdatei wird entfernt, das Projekt läuft nicht mehr.",
		tProjSourceTitle:   "Quellen von %s",
		tProjSourceNote:    "Versionen aus Registries und Repositories.",
		tProjSourceDone:    "Quellentest von %s beendet.",
		tProjLogTitle:      "Log %s · %s",
		tProjLogCut:        "Das Log ist gekürzt: sein Ende wird gezeigt.",
		tProjLogNone:       "Dieses Projekt hatte noch keinen Lauf.",
		tProjLogGone:       "Das Log des Laufs ist nicht mehr da.",
		tSrcDenied:         "Zugriff verweigert",
		tSrcNotFound:       "nicht gefunden",
		tSrcUnreachable:    "nicht erreichbar",
		tSrcTimeout:        "Zeitüberschreitung",
		tSrcCredential:     "Credential-Problem",
		tSrcInvalid:        "ungültige Quelle",

		footerID("space mark"):                  "space markieren",
		footerID("enter detail"):                "enter Detail",
		footerID("d delete"):                    "d löschen",
		footerID("o log"):                       "o Log",
		footerID("left/right tab"):              "left/right Reiter",
		footerID("{back} list"):                 "{back} Liste",
		footerID("pgup/pgdown page"):            "pgup/pgdown Seite",
		footerID("home/end ends"):               "home/end Enden",
		footerID("y run"):                       "y ausführen",
		footerID("n/{back} cancel"):             "n/{back} abbrechen",
		footerID("run/pause/pin via {command}"): "run/pause/pin über {command}",
	})
	for _, w := range projectWords {
		textEN[wordID(w)] = w
	}
	maps.Copy(textDE, map[textID]string{
		wordID("images"): "Images", wordID("git"): "git", wordID("auto"): "auto", wordID("manual"): "manuell",
		wordID("mixed"): "gemischt", wordID("paused"): "pausiert", wordID("pinned"): "angeheftet",
		wordID("locked"): "gesperrt", wordID("restore"): "Wiederherstellung", wordID("succeeded"): "erfolgreich",
		wordID("failed"): "fehlgeschlagen", wordID("rolled-back"): "zurückgerollt",
		wordID("rollback-failed"): "Rollback fehlgeschlagen",
	})
}
