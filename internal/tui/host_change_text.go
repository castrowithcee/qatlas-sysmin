package tui

// The texts of the host changes: the confirmation, the result, the messages of a run that gave no answer, and
// the Changes page.
const (
	tSubChanges            textID = "SubChanges"
	tInfoHostChanges       textID = "InfoHostChanges"
	tHostColChange         textID = "HostColChange"
	tHostColResult         textID = "HostColResult"
	tHostLblUndo           textID = "HostLblUndo"
	tHostResApplied        textID = "HostResApplied"
	tHostResFailed         textID = "HostResFailed"
	tHostResRestored       textID = "HostResRestored"
	tHostResUndone         textID = "HostResUndone"
	tHostChangesHint       textID = "HostChangesHint"
	tHostChangesNone       textID = "HostChangesNone"
	tHostChangesStale      textID = "HostChangesStale"
	tHostNoUndo            textID = "HostNoUndo"
	tHostReversible        textID = "HostReversible"
	tHostIrreversible      textID = "HostIrreversible"
	tHostChangeRefused     textID = "HostChangeRefused"
	tHostUnknownAction     textID = "HostUnknownAction"
	tHostSecretUnsupported textID = "HostSecretUnsupported"
	tHostDone              textID = "HostDone"
	tHostDoneUndoable      textID = "HostDoneUndoable"
	tHostDoneFinal         textID = "HostDoneFinal"
	tHostFailed            textID = "HostFailed"
	tHostFailedNote        textID = "HostFailedNote"
	tHostFailedRestored    textID = "HostFailedRestored"
	tHostRestoredNote      textID = "HostRestoredNote"
	tHostCheckPassed       textID = "HostCheckPassed"
	tHostCheckFailed       textID = "HostCheckFailed"
	tHostAuditWarning      textID = "HostAuditWarning"
	tHostUndoNote          textID = "HostUndoNote"
	tHostUndoRefused       textID = "HostUndoRefused"
	tHostUndoRefusedNote   textID = "HostUndoRefusedNote"
	tHostChangeFailed      textID = "HostChangeFailed"
	tHostReadFailed        textID = "HostReadFailed"
	tHostErrNoSudo         textID = "HostErrNoSudo"
	tHostErrStart          textID = "HostErrStart"
	tHostErrDenied         textID = "HostErrDenied"
	tHostErrAbortedChange  textID = "HostErrAbortedChange"
	tHostErrAbortedRead    textID = "HostErrAbortedRead"
	tHostErrExitChange     textID = "HostErrExitChange"
	tHostErrExitRead       textID = "HostErrExitRead"
	tHostErrOutput         textID = "HostErrOutput"
	tHostReadTitle         textID = "HostReadTitle"
	tHostReadNote          textID = "HostReadNote"
	tHostReadHint          textID = "HostReadHint"
	tHostReadDone          textID = "HostReadDone"
	tHostErrNoCredential   textID = "HostErrNoCredential"
	tHostErrReported       textID = "HostErrReported"
	tHostPowerWarn         textID = "HostPowerWarn"
	tHostRefused           textID = "HostRefused"
	tHostRefusedNote       textID = "HostRefusedNote"
)

// hostChangeTexts are the texts of the host changes, in English and German.
func hostChangeTexts() (en, de map[textID]string) {
	en = map[textID]string{
		tSubChanges:            "Changes",
		tInfoHostChanges:       "The latest changes made to this host through qsysmin, from the journal that only root can read. Enter on a change that can be undone asks to undo it. Reading asks for your sudo password.",
		tHostColChange:         "Change",
		tHostColResult:         "Result",
		tHostLblUndo:           "Undo",
		tHostResApplied:        "applied",
		tHostResFailed:         "failed",
		tHostResRestored:       "restored",
		tHostResUndone:         "undone",
		tHostChangesHint:       "Press %s to read the latest changes as root. sudo asks for your password.",
		tHostChangesNone:       "No changes yet.",
		tHostChangesStale:      "This list is older than your last change. Press %s to read it again.",
		tHostNoUndo:            "This change cannot be undone.",
		tHostReversible:        "You can undo this under Host > Changes.",
		tHostIrreversible:      "This cannot be undone: %s",
		tHostChangeRefused:     "This change cannot be made",
		tHostUnknownAction:     "There is no action %q.",
		tHostSecretUnsupported: "This action needs a secret input, which only its own form can give. Nothing was changed.",
		tHostDone:              "Done: %s",
		tHostDoneUndoable:      "The change is in the journal. You can undo it under Host > Changes.",
		tHostDoneFinal:         "This change cannot be undone.",
		tHostFailed:            "Failed: %s",
		tHostFailedNote:        "The change did not succeed.",
		tHostFailedRestored:    "Failed, previous state restored: %s",
		tHostRestoredNote:      "The change or its check failed and the previous state is back.",
		tHostCheckPassed:       "Check: passed.",
		tHostCheckFailed:       "Check failed: %s",
		tHostAuditWarning:      "Warning: %s",
		tHostUndoNote:          "This takes back: %s",
		tHostUndoRefused:       "This change cannot be undone",
		tHostUndoRefusedNote:   "%s was not changed back: it is not reversible, was undone already, or did not succeed.",
		tHostChangeFailed:      "The change did not run",
		tHostReadFailed:        "Reading as root did not work",
		tHostErrNoSudo:         "sudo is not installed on this host, and a change needs root. Nothing was changed.",
		tHostErrStart:          "sudo could not be started: %s. Nothing was changed.",
		tHostErrDenied:         "sudo did not run it: the password was wrong or this account may not use sudo. Nothing was changed.",
		tHostErrAbortedChange:  "The run was interrupted before it reported a result. Read Host > Changes to see what was done.",
		tHostErrAbortedRead:    "The run was interrupted. Nothing was changed.",
		tHostErrExitChange:     "The command ended with exit code %d and no report. Read Host > Changes to see what was done.",
		tHostErrExitRead:       "The command ended with exit code %d and no answer. Nothing was changed.",
		tHostErrOutput:         "The answer of qsysmin could not be read. Nothing was changed.",
		tHostReadTitle:         "Read %s as root?",
		tHostReadNote:          "Reads the locked state and the SSH keys of this user, which only root can see. Nothing is changed. sudo asks for your password.",
		tHostReadHint:          "Press %s to read the fields only root can see.",
		tHostReadDone:          "Read %s as root.",
		tHostErrNoCredential:   "sudo no longer holds your password and cannot ask again here (for example because its timeout is zero). Nothing was changed.",
		tHostErrReported:       "qsysmin answered without a report: %s. Read Host > Changes to see whether anything was done.",
		tHostRefused:           "Not changed: %s",
		tHostRefusedNote:       "A rule of the host refused this change before anything was done.",
		tHostPowerWarn:         "Admin and Docker rights allow practically everything on this host.",

		footerID("y apply"):             "y apply",
		footerID("y undo"):              "y undo",
		footerID("y read as root"):      "y read as root",
		footerID("enter undo"):          "enter undo",
		footerID("ctrl+r read as root"): "ctrl+r read as root",
	}
	de = map[textID]string{
		tSubChanges:            "Änderungen",
		tInfoHostChanges:       "Die letzten Änderungen, die über qsysmin an diesem Host gemacht wurden, aus dem Journal, das nur root lesen kann. Enter auf einer Änderung, die sich zurücknehmen lässt, fragt nach dem Rückgängigmachen. Zum Lesen wird dein sudo-Passwort gefragt.",
		tHostColChange:         "Änderung",
		tHostColResult:         "Ergebnis",
		tHostLblUndo:           "Rückgängig",
		tHostResApplied:        "ausgeführt",
		tHostResFailed:         "fehlgeschlagen",
		tHostResRestored:       "zurückgestellt",
		tHostResUndone:         "rückgängig gemacht",
		tHostChangesHint:       "Drücke %s, um die letzten Änderungen als root zu lesen. sudo fragt nach deinem Passwort.",
		tHostChangesNone:       "Noch keine Änderungen.",
		tHostChangesStale:      "Diese Liste ist älter als deine letzte Änderung. Drücke %s, um sie neu zu lesen.",
		tHostNoUndo:            "Diese Änderung lässt sich nicht rückgängig machen.",
		tHostReversible:        "Du kannst das unter Host > Änderungen rückgängig machen.",
		tHostIrreversible:      "Das lässt sich nicht rückgängig machen: %s",
		tHostChangeRefused:     "Diese Änderung ist nicht möglich",
		tHostUnknownAction:     "Es gibt keine Aktion %q.",
		tHostSecretUnsupported: "Diese Aktion braucht eine geheime Eingabe, die nur ihr eigenes Formular geben kann. Es wurde nichts geändert.",
		tHostDone:              "Erledigt: %s",
		tHostDoneUndoable:      "Die Änderung steht im Journal. Du kannst sie unter Host > Änderungen rückgängig machen.",
		tHostDoneFinal:         "Diese Änderung lässt sich nicht rückgängig machen.",
		tHostFailed:            "Fehlgeschlagen: %s",
		tHostFailedNote:        "Die Änderung ist nicht gelungen.",
		tHostFailedRestored:    "Fehlgeschlagen, vorheriger Zustand zurückgestellt: %s",
		tHostRestoredNote:      "Die Änderung oder ihre Prüfung ist fehlgeschlagen; der vorherige Zustand ist wieder da.",
		tHostCheckPassed:       "Prüfung: bestanden.",
		tHostCheckFailed:       "Prüfung fehlgeschlagen: %s",
		tHostAuditWarning:      "Warnung: %s",
		tHostUndoNote:          "Das nimmt zurück: %s",
		tHostUndoRefused:       "Das lässt sich nicht rückgängig machen",
		tHostUndoRefusedNote:   "%s wurde nicht zurückgenommen: nicht umkehrbar, schon rückgängig gemacht oder nicht gelungen.",
		tHostChangeFailed:      "Die Änderung wurde nicht ausgeführt",
		tHostReadFailed:        "Das Lesen als root hat nicht geklappt",
		tHostErrNoSudo:         "sudo ist auf diesem Host nicht installiert, und eine Änderung braucht root. Es wurde nichts geändert.",
		tHostErrStart:          "sudo ließ sich nicht starten: %s. Es wurde nichts geändert.",
		tHostErrDenied:         "sudo hat es nicht ausgeführt: Das Passwort war falsch oder dieses Konto darf sudo nicht nutzen. Es wurde nichts geändert.",
		tHostErrAbortedChange:  "Der Lauf wurde abgebrochen, bevor er ein Ergebnis meldete. Unter Host > Änderungen steht, was geschehen ist.",
		tHostErrAbortedRead:    "Der Lauf wurde abgebrochen. Es wurde nichts geändert.",
		tHostErrExitChange:     "Der Befehl endete mit Exit-Code %d ohne Bericht. Unter Host > Änderungen steht, was geschehen ist.",
		tHostErrExitRead:       "Der Befehl endete mit Exit-Code %d ohne Antwort. Es wurde nichts geändert.",
		tHostErrOutput:         "Die Antwort von qsysmin ließ sich nicht lesen. Es wurde nichts geändert.",
		tHostReadTitle:         "%s als root lesen?",
		tHostReadNote:          "Liest den Sperrstatus und die SSH-Schlüssel dieses Benutzers, die nur root sehen kann. Es wird nichts geändert. sudo fragt nach deinem Passwort.",
		tHostReadHint:          "Drücke %s, um die Felder zu lesen, die nur root sieht.",
		tHostReadDone:          "%s als root gelesen.",
		tHostErrNoCredential:   "sudo hält dein Passwort nicht mehr und kann hier nicht erneut fragen (zum Beispiel wegen eines Timeouts von null). Es wurde nichts geändert.",
		tHostErrReported:       "qsysmin hat ohne Bericht geantwortet: %s. Unter Host > Änderungen steht, ob etwas geschehen ist.",
		tHostRefused:           "Nicht geändert: %s",
		tHostRefusedNote:       "Eine Regel des Hosts hat diese Änderung abgelehnt, bevor etwas geschah.",
		tHostPowerWarn:         "Admin- und Docker-Rechte erlauben praktisch alles auf diesem Host.",

		footerID("y apply"):             "y ausführen",
		footerID("y undo"):              "y rückgängig",
		footerID("y read as root"):      "y als root lesen",
		footerID("enter undo"):          "enter rückgängig",
		footerID("ctrl+r read as root"): "ctrl+r als root lesen",
	}
	return en, de
}
