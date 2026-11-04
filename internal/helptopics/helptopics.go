// Package helptopics holds the help topics and terms of qsysmin. The command line, the manpage and the help
// of the terminal interface show the same topics, and the info of the interface explains a technical word by
// the same terms.
package helptopics

import (
	"github.com/castrowithcee/qatlas-kit/helptopics"
	"github.com/castrowithcee/qatlas-kit/i18n"
)

// TermAuditLog is the ID of the term that explains the audit log.
const TermAuditLog = "audit-log"

// Topics are the help topics in lang, in the order a newcomer reads them. Every language has the same topics
// under the same names; a language without texts of its own gets the English ones, which are plain ASCII.
func Topics(lang i18n.Lang) []helptopics.Topic {
	if lang == i18n.German {
		return topicsDE()
	}
	return topicsEN()
}

// Terms are the technical words the interface explains, in lang; a language without texts of its own gets the
// English ones.
func Terms(lang i18n.Lang) []helptopics.Term {
	if lang == i18n.German {
		return []helptopics.Term{{ID: TermAuditLog, Title: "Audit log", Text: "Jede Anfrage an den Dienst wird protokolliert, durch Hashes verkettet. Eine gebrochene Kette heißt: ein Eintrag wurde geändert oder entfernt. Das Lesen des Protokolls fügt Einträge hinzu."}}
	}
	return []helptopics.Term{{ID: TermAuditLog, Title: "Audit log", Text: "Every request to the service is logged, chained by hashes. A broken chain means an entry was changed or removed. Reading the log adds entries."}}
}

func topicsEN() []helptopics.Topic {
	return []helptopics.Topic{
		{Name: "qsysmin", Short: "What qsysmin is and what it is for", Text: `qsysmin is a Go tool that runs as a service on a Linux host and helps look after that host. It has the areas Overview, Host, Activity and Settings.

Host reads the local users, groups and folder permissions. Changes run through 'sudo qsysmin host ...' after a confirmation, are recorded in a journal with the previous state, and can be undone. Activity shows a log kept as a hash chain without a signature.

Admins, members of the group sudo, reach the running service through its socket. The installation requires the group docker.

'qsysmin --help' lists the commands; 'qsysmin tui' opens the terminal interface; 'qsysmin host-admin' covers the host commands.`},
		{Name: "host-admin", Short: "Read and change users, groups and folder permissions of the host", Text: `The host commands read the local users, groups and folder permissions. Changes (host apply, host undo) run through sudo after a confirmation, are recorded in a root-only journal with the previous state, and can be undone.

Commands:
  qsysmin host users
  qsysmin host user <name>
  qsysmin host groups
  qsysmin host group <name>
  qsysmin host folder <path>

Only root and members of the group sudo may use them. Everyone else gets one line and exit code 1.

A user or group is a system user or group when its ID lies outside 1000 to 60000, so root and nobody count as system accounts. A user is an admin in the group sudo, a docker user in the group docker, and enabled in the group qsysmin. A group lists the users whose primary group it is as members.

Whether an account is locked or expired and which SSH keys it has are only readable as root. Otherwise they are unknown: absent in JSON and 'unknown' in the other formats. Keys are shown as type, comment and SHA256 fingerprint, never as the key itself.

A folder or file shows owner, group and the level of owner, group and others: nothing, read, or read and write. For a directory read and write both need the right to enter it; write without read counts as nothing. It also shows whether ACLs are set, the special bits setuid, setgid and sticky, and whether the path is protected: /, /etc, /usr, /boot, /proc, /sys and everything below the last five. Links and .. are resolved before that check.`},
		{Name: "host-tui", Short: "Use the area Host of the terminal interface", Text: "Host shows the users, groups and folder permissions of this machine. Admin means sudo rights, Docker is practically root, and enabled means membership of the group qsysmin. Grey rows are system accounts and groups, whose ID lies outside 1000 to 60000; they sit in their own group, which starts collapsed. enter on the row of a group collapses or expands it, left collapses the group of the marked row and right expands it again. e and d work on root and system accounts like on any other account, and the confirmation is then red, because packages or services may depend on them; only taking admin from, locking or deleting yourself or the last admin is refused. Folders reads one path at a time: type it, tab completes, enter reads. A path marked protected is a system path. e changes the folder shown, after a confirmation that names the number of entries and, for a system path, the risk. n creates a person, whose initial password must be changed at the first login, or a service account without login (no password, no shell, no SSH keys, a home only on request); e changes the marked one (groups, Admin, Docker, password, SSH keys, lock), one change at a time. Lock is the normal way to stop someone: it blocks every login, also by SSH key, while running sessions stay open; Unlock releases the password and removes the end of the account. d deletes the marked user after asking whether its home goes too: keeping the home is the default and can be undone, which creates the account again with the same UID, groups and password; deleting the home and mail spool cannot be undone. A user with running processes is not deleted. On Groups, n creates a group, e adds and removes members of the marked one (several users at once, one confirmation; in sudo, adding also adds qsysmin and removing takes both; nobody leaves sudo or qsysmin themselves, and one admin stays), r renames it and d deletes it, each after a confirmation. A group that is the primary group of a user is not deleted, and sudo and qsysmin are neither renamed nor deleted. Deleting can be undone, which creates the group again with the same group number and members. Renaming is always red, because configurations such as sudoers address a group by its name and keep the old one; deleting root, docker and system groups is red, too, and so are member changes in them. Changes lists the latest changes made through qsysmin and undoes one with enter. What only root can read, such as the locked state and SSH keys of a user or the list of changes, is read through sudo when you press ctrl+r; sudo asks for the password, qsysmin never sees it."},
	}
}

func topicsDE() []helptopics.Topic {
	return []helptopics.Topic{
		{Name: "qsysmin", Short: "Was qsysmin ist und wofür es da ist", Text: `qsysmin ist ein Go-Werkzeug, das als Dienst auf einem Linux-Host läuft und hilft, diesen Host zu betreuen. Es hat die Bereiche Overview, Host, Activity und Settings.

Host liest die lokalen Benutzer, Gruppen und Ordnerrechte. Änderungen laufen nach einer Rückfrage über 'sudo qsysmin host ...', werden mit dem vorherigen Stand in einem Journal festgehalten und lassen sich rückgängig machen. Activity zeigt ein Protokoll, das als Hash-Kette ohne Signatur geführt wird.

Admins, Mitglieder der Gruppe sudo, erreichen den laufenden Dienst über seinen Socket. Die Installation setzt die Gruppe docker voraus.

'qsysmin --help' listet die Befehle; 'qsysmin tui' öffnet die Terminal-Oberfläche; 'qsysmin host-admin' beschreibt die Host-Befehle.`},
		{Name: "host-admin", Short: "Benutzer, Gruppen und Ordnerrechte des Hosts lesen und ändern", Text: `Die Host-Befehle lesen die lokalen Benutzer, Gruppen und Ordnerrechte. Änderungen (host apply, host undo) laufen nach einer Rückfrage über sudo, werden mit dem vorherigen Stand in einem Journal festgehalten, das nur root lesen kann, und lassen sich rückgängig machen.

Befehle:
  qsysmin host users
  qsysmin host user <name>
  qsysmin host groups
  qsysmin host group <name>
  qsysmin host folder <path>

Nur root und Mitglieder der Gruppe sudo dürfen sie nutzen. Alle anderen erhalten eine Zeile und den Exit-Code 1.

Ein Benutzer oder eine Gruppe ist ein Systembenutzer oder eine Systemgruppe, wenn die ID außerhalb von 1000 bis 60000 liegt; root und nobody zählen also als Systemkonten. Ein Benutzer ist Admin in der Gruppe sudo, Docker-Benutzer in der Gruppe docker und freigeschaltet in der Gruppe qsysmin. Eine Gruppe führt die Benutzer als Mitglieder, deren Hauptgruppe sie ist.

Ob ein Konto gesperrt oder abgelaufen ist und welche SSH-Schlüssel es hat, kann nur root lesen. Sonst ist es unbekannt: in JSON fehlt es, in den anderen Formaten steht 'unknown'. Schlüssel erscheinen als Typ, Kommentar und SHA256-Fingerprint, nie als Schlüssel selbst.

Ein Ordner oder eine Datei zeigt Besitzer, Gruppe und die Stufe von Besitzer, Gruppe und anderen: nichts, lesen oder lesen und schreiben. Bei einem Ordner brauchen lesen und schreiben beide das Recht, ihn zu betreten; schreiben ohne lesen zählt als nichts. Dazu zeigt er, ob ACLs gesetzt sind, die Sonderbits setuid, setgid und sticky und ob der Pfad geschützt ist: /, /etc, /usr, /boot, /proc, /sys und alles unter den letzten fünf. Links und .. werden vor dieser Prüfung aufgelöst.`},
		{Name: "host-tui", Short: "Den Bereich Host der Terminal-Oberfläche nutzen", Text: "Host zeigt die Benutzer, Gruppen und Ordnerrechte dieses Rechners. Admin heißt sudo-Rechte, Docker ist praktisch root, und freigeschaltet heißt Mitglied der Gruppe qsysmin. Graue Zeilen sind Systemkonten und Systemgruppen, deren ID außerhalb von 1000 bis 60000 liegt; sie stehen in einer eigenen Gruppe, die eingeklappt beginnt. Enter auf der Zeile einer Gruppe klappt sie ein oder aus, left klappt die Gruppe der markierten Zeile ein und right wieder aus. e und d wirken auf root und Systemkonten wie auf jedes andere Konto, die Rückfrage ist dann rot, weil Pakete oder Dienste davon abhängen können; abgelehnt wird nur, sich selbst oder dem letzten Admin den Admin zu entziehen, ihn zu sperren oder zu löschen. Ordner liest einen Pfad nach dem anderen: eintippen, tab ergänzt, Enter liest. Ein als geschützt markierter Pfad ist ein Systempfad. e ändert den angezeigten Ordner, nach einer Bestätigung, die die Zahl der Einträge und bei einem Systempfad das Risiko nennt. n legt eine Person an, deren Startpasswort beim ersten Login geändert werden muss, oder ein Dienstkonto ohne Login (kein Passwort, keine Shell, keine SSH-Schlüssel, ein Home nur auf Wunsch); e ändert den markierten (Gruppen, Admin, Docker, Passwort, SSH-Schlüssel, Sperre), jeweils eine Änderung. Sperren ist der Normalfall, wenn jemand geht: Es verhindert jeden Login, auch per SSH-Schlüssel, laufende Sitzungen bleiben offen; Entsperren gibt das Passwort frei und hebt das Kontoende auf. d löscht den markierten Benutzer und fragt vorher, ob sein Home mitgeht: Das Home zu behalten ist der Standard und lässt sich rückgängig machen, wobei das Konto mit derselben UID, denselben Gruppen und demselben Passwort neu entsteht; Home und Mail-Spool mitzulöschen lässt sich nicht rückgängig machen. Ein Benutzer mit laufenden Prozessen wird nicht gelöscht. Unter Gruppen legt n eine Gruppe an, e fügt der markierten Mitglieder hinzu und entfernt welche (mehrere Benutzer auf einmal, eine Rückfrage; in sudo kommt beim Hinzufügen qsysmin dazu und beim Entfernen gehen beide; niemand verlässt sudo oder qsysmin selbst, und ein Admin bleibt), r benennt sie um und d löscht sie, jeweils nach einer Bestätigung. Eine Gruppe, die die Hauptgruppe eines Benutzers ist, wird nicht gelöscht, und sudo und qsysmin werden weder umbenannt noch gelöscht. Löschen lässt sich rückgängig machen, wobei die Gruppe mit derselben Gruppennummer und denselben Mitgliedern neu entsteht. Umbenennen fragt immer rot, weil Konfigurationen wie sudoers eine Gruppe über ihren Namen ansprechen und den alten behalten; auch root, docker und Systemgruppen zu löschen fragt rot, ebenso Mitgliederänderungen darin. Änderungen listet die letzten Änderungen über qsysmin und macht eine mit Enter rückgängig. Was nur root lesen kann, etwa Sperrstatus und SSH-Schlüssel eines Benutzers oder die Liste der Änderungen, wird mit ctrl+r über sudo gelesen; sudo fragt nach dem Passwort, qsysmin sieht es nie."},
	}
}
