package tui

import "maps"

// The texts of the Host area. The area names, the page names and the words of the account databases keep their
// English form where the Linux tools do.
const (
	tAreaHost            textID = "AreaHost"
	tSubUsers            textID = "SubUsers"
	tSubGroups           textID = "SubGroups"
	tSubFolders          textID = "SubFolders"
	tHostColAdmin        textID = "HostColAdmin"
	tHostColDocker       textID = "HostColDocker"
	tHostColGroups       textID = "HostColGroups"
	tHostColMembers      textID = "HostColMembers"
	tHostLblEnabled      textID = "HostLblEnabled"
	tHostLblHome         textID = "HostLblHome"
	tHostLblLocked       textID = "HostLblLocked"
	tHostYes             textID = "HostYes"
	tHostNo              textID = "HostNo"
	tHostAdminOff        textID = "HostAdminOff"
	tHostAdminOffShort   textID = "HostAdminOffShort"
	tHostColSystem       textID = "HostColSystem"
	tHostSystem          textID = "HostSystem"
	tHostSystemRO        textID = "HostSystemRO"
	tHostRegular         textID = "HostRegular"
	tHostUnknown         textID = "HostUnknown"
	tHostNone            textID = "HostNone"
	tHostNoUsers         textID = "HostNoUsers"
	tHostNoGroups        textID = "HostNoGroups"
	tHostNoMembers       textID = "HostNoMembers"
	tHostNoKeys          textID = "HostNoKeys"
	tHostKeysUnknown     textID = "HostKeysUnknown"
	tHostPathLabel       textID = "HostPathLabel"
	tHostPathPlaceholder textID = "HostPathPlaceholder"
	tHostFolderHint      textID = "HostFolderHint"
	tHostFolderNotFound  textID = "HostFolderNotFound"
	tHostFolderDenied    textID = "HostFolderDenied"
	tHostOwnerOf         textID = "HostOwnerOf"
	tHostGroupOf         textID = "HostGroupOf"
	tHostOthers          textID = "HostOthers"
	tHostAccNothing      textID = "HostAccNothing"
	tHostAccRead         textID = "HostAccRead"
	tHostAccReadWrite    textID = "HostAccReadWrite"
	tHostKindFolder      textID = "HostKindFolder"
	tHostKindFile        textID = "HostKindFile"
	tHostKindOther       textID = "HostKindOther"
	tHostResolved        textID = "HostResolved"
	tHostMode            textID = "HostMode"
	tHostACL             textID = "HostACL"
	tHostACLSet          textID = "HostACLSet"
	tHostSpecial         textID = "HostSpecial"
	tHostSetuid          textID = "HostSetuid"
	tHostSetuidDir       textID = "HostSetuidDir"
	tHostSetgid          textID = "HostSetgid"
	tHostSetgidDir       textID = "HostSetgidDir"
	tHostSticky          textID = "HostSticky"
	tHostStickyDir       textID = "HostStickyDir"
	tHostProtected       textID = "HostProtected"
	tHostProtectedNote   textID = "HostProtectedNote"
	tInfoHostUsers       textID = "InfoHostUsers"
	tInfoHostGroups      textID = "InfoHostGroups"
	tInfoHostFolders     textID = "InfoHostFolders"
	tHelpHost            textID = "HelpHost"
	tHostSysAccounts     textID = "HostSysAccounts"
	tHostSysGroups       textID = "HostSysGroups"
	tSectionHint         textID = "SectionHint"
)

func init() {
	maps.Copy(textEN, map[textID]string{
		tAreaHost:            "Host",
		tSubUsers:            "Users",
		tSubGroups:           "Groups",
		tSubFolders:          "Folders",
		tHostColAdmin:        "Admin",
		tHostColDocker:       "Docker",
		tHostColGroups:       "Groups",
		tHostColMembers:      "Members",
		tHostLblEnabled:      "Enabled (sysmin)",
		tHostLblHome:         "Home",
		tHostLblLocked:       "Locked",
		tHostYes:             "yes",
		tHostNo:              "no",
		tHostAdminOff:        "yes · not enabled",
		tHostAdminOffShort:   "yes · no sysmin",
		tHostColSystem:       "System",
		tHostSystem:          "system",
		tHostSystemRO:        "system (read-only)",
		tHostRegular:         "regular",
		tHostUnknown:         "unknown",
		tHostNone:            "none",
		tHostNoUsers:         "No users found.",
		tHostNoGroups:        "No groups found.",
		tHostNoMembers:       "No members.",
		tHostNoKeys:          "No SSH keys.",
		tHostKeysUnknown:     "SSH keys: unknown (only root can read them).",
		tHostPathLabel:       "Path",
		tHostPathPlaceholder: "/srv/…",
		tHostFolderHint:      "Type a path and press enter to see who may read and write it.",
		tHostFolderNotFound:  "%s does not exist.",
		tHostFolderDenied:    "No permission to read %s.",
		tHostOwnerOf:         "Owner %s",
		tHostGroupOf:         "Group %s",
		tHostOthers:          "Others",
		tHostAccNothing:      "nothing",
		tHostAccRead:         "read",
		tHostAccReadWrite:    "read and write",
		tHostKindFolder:      "folder",
		tHostKindFile:        "file",
		tHostKindOther:       "other",
		tHostResolved:        "Resolves to",
		tHostMode:            "Mode",
		tHostACL:             "ACL",
		tHostACLSet:          "set: further rights apply that the levels above do not show",
		tHostSpecial:         "Special",
		tHostSetuid:          "setuid: runs with the rights of its owner",
		tHostSetuidDir:       "setuid: set, but has no effect on a folder",
		tHostSetgid:          "setgid: runs with the rights of its group",
		tHostSetgidDir:       "setgid: new entries take the group of this folder",
		tHostSticky:          "sticky: set, but has no effect on a file",
		tHostStickyDir:       "sticky: only the owner of an entry may remove it",
		tHostProtected:       "Protected",
		tHostProtectedNote:   "a system path; sysmin does not change its permissions",
		tInfoHostUsers:       "Admin: the user may become root with sudo. Docker: the user may use Docker, which is practically root on this host. Enabled: the user is a member of the group sysmin; an admin who is also enabled may use sysmin. n creates a user, e changes the marked one, d deletes it; every change asks first, runs through sudo and can be undone under Changes, except deleting a home.",
		tInfoHostGroups:      "The groups of this host with their members. n creates a group, r renames and d deletes the marked one; sudo and sysmin are never renamed or deleted.",
		tInfoHostFolders:     "Shows who may read and write a folder or file: owner, group and others, in plain words. e changes the folder shown: owner, group and the three levels, also for everything below it; the change asks first, runs through sudo and can be undone under Changes.",
		tHelpHost:            "Host shows the users, groups and folder permissions of this machine. Admin means sudo rights, Docker is practically root, and enabled means membership of the group sysmin. Grey rows are system accounts and groups, whose ID lies outside 1000 to 60000; they sit in their own section, which starts collapsed. enter or space on a heading collapses or expands it. e and d work on root and system accounts like on any other account, and the confirmation is then red, because packages or services may depend on them; only taking admin from, locking or deleting yourself or the last admin is refused. Folders reads one path at a time: type it, tab completes, enter reads. A path marked protected is a system path. e changes the folder shown, after a confirmation that names the number of entries and, for a system path, the risk; if a stack stops being healthy, the change is taken back. n creates a person, whose initial password must be changed at the first login, or a service account without login (no password, no shell, no SSH keys, a home only on request); e changes the marked one (groups, Admin, Docker, password, SSH keys, lock), one change at a time. Lock is the normal way to stop someone: it blocks every login, also by SSH key, while running sessions stay open; Unlock releases the password and removes the end of the account. d deletes the marked user after asking whether its home goes too: keeping the home is the default and can be undone, which creates the account again with the same UID, groups and password; deleting the home and mail spool cannot be undone. A user with running processes is not deleted. On Groups, n creates a group, r renames the marked one and d deletes it, each after a confirmation. A group that is the primary group of a user is not deleted, and sudo and sysmin are neither renamed nor deleted. Deleting can be undone, which creates the group again with the same group number and members. Renaming is always red, because configurations such as sudoers address a group by its name and keep the old one; deleting root, docker and system groups is red, too. Changes lists the latest changes made through sysmin and undoes one with enter. What only root can read, such as the locked state and SSH keys of a user or the list of changes, is read through sudo when you press ctrl+r; sudo asks for the password, sysmin never sees it.",

		footerID("enter read"):      "enter read",
		footerID("enter edit path"): "enter edit path",

		footerID("enter collapse"): "enter collapse",
		footerID("enter expand"):   "enter expand",
		footerID("space tick"):     "space tick",
		tHostSysAccounts:           "System accounts",
		tHostSysGroups:             "System groups",
		tSectionHint:               "Press enter or space to collapse or expand this section.",
	})
	maps.Copy(textDE, map[textID]string{
		tAreaHost:            "Host",
		tSubUsers:            "Benutzer",
		tSubGroups:           "Gruppen",
		tSubFolders:          "Ordner",
		tHostColAdmin:        "Admin",
		tHostColDocker:       "Docker",
		tHostColGroups:       "Gruppen",
		tHostColMembers:      "Mitglieder",
		tHostLblEnabled:      "Freigeschaltet (sysmin)",
		tHostLblHome:         "Home",
		tHostLblLocked:       "Gesperrt",
		tHostYes:             "ja",
		tHostNo:              "nein",
		tHostAdminOff:        "ja · nicht freigeschaltet",
		tHostAdminOffShort:   "ja · ohne sysmin",
		tHostColSystem:       "System",
		tHostSystem:          "System",
		tHostSystemRO:        "System (nur lesbar)",
		tHostRegular:         "normal",
		tHostUnknown:         "unbekannt",
		tHostNone:            "keine",
		tHostNoUsers:         "Keine Benutzer gefunden.",
		tHostNoGroups:        "Keine Gruppen gefunden.",
		tHostNoMembers:       "Keine Mitglieder.",
		tHostNoKeys:          "Keine SSH-Schlüssel.",
		tHostKeysUnknown:     "SSH-Schlüssel: unbekannt (nur root kann sie lesen).",
		tHostPathLabel:       "Pfad",
		tHostPathPlaceholder: "/srv/…",
		tHostFolderHint:      "Pfad eingeben und Enter drücken, um zu sehen, wer lesen und schreiben darf.",
		tHostFolderNotFound:  "%s existiert nicht.",
		tHostFolderDenied:    "Keine Berechtigung, %s zu lesen.",
		tHostOwnerOf:         "Besitzer %s",
		tHostGroupOf:         "Gruppe %s",
		tHostOthers:          "Andere",
		tHostAccNothing:      "nichts",
		tHostAccRead:         "lesen",
		tHostAccReadWrite:    "lesen und schreiben",
		tHostKindFolder:      "Ordner",
		tHostKindFile:        "Datei",
		tHostKindOther:       "anderes",
		tHostResolved:        "Führt zu",
		tHostMode:            "Modus",
		tHostACL:             "ACL",
		tHostACLSet:          "gesetzt: weitere Rechte gelten, die die Stufen oben nicht zeigen",
		tHostSpecial:         "Besonders",
		tHostSetuid:          "setuid: läuft mit den Rechten seines Besitzers",
		tHostSetuidDir:       "setuid: gesetzt, wirkt aber bei einem Ordner nicht",
		tHostSetgid:          "setgid: läuft mit den Rechten seiner Gruppe",
		tHostSetgidDir:       "setgid: neue Einträge erhalten die Gruppe dieses Ordners",
		tHostSticky:          "sticky: gesetzt, wirkt aber bei einer Datei nicht",
		tHostStickyDir:       "sticky: nur der Besitzer eines Eintrags darf ihn entfernen",
		tHostProtected:       "Geschützt",
		tHostProtectedNote:   "ein Systempfad; sysmin ändert seine Rechte nicht",
		tInfoHostUsers:       "Admin: Der Benutzer kann mit sudo root werden. Docker: Der Benutzer darf Docker nutzen, was auf diesem Host praktisch root bedeutet. Freigeschaltet: Der Benutzer ist Mitglied der Gruppe sysmin; ein freigeschalteter Admin darf sysmin nutzen. n legt einen Benutzer an, e ändert den markierten, d löscht ihn; jede Änderung fragt vorher, läuft über sudo und lässt sich unter Änderungen rückgängig machen, außer ein gelöschtes Home.",
		tInfoHostGroups:      "Die Gruppen dieses Hosts mit ihren Mitgliedern. n legt eine Gruppe an, r benennt die markierte um und d löscht sie; sudo und sysmin werden nie umbenannt oder gelöscht.",
		tInfoHostFolders:     "Zeigt in einfachen Worten, wer einen Ordner oder eine Datei lesen und schreiben darf: Besitzer, Gruppe und andere. e ändert den angezeigten Ordner: Besitzer, Gruppe und die drei Stufen, auch für alles darunter; die Änderung fragt vorher, läuft über sudo und lässt sich unter Änderungen rückgängig machen.",
		tHelpHost:            "Host zeigt die Benutzer, Gruppen und Ordnerrechte dieses Rechners. Admin heißt sudo-Rechte, Docker ist praktisch root, und freigeschaltet heißt Mitglied der Gruppe sysmin. Graue Zeilen sind Systemkonten und Systemgruppen, deren ID außerhalb von 1000 bis 60000 liegt; sie stehen in einem eigenen Abschnitt, der eingeklappt beginnt. Enter oder space auf einer Überschrift klappt ihn ein oder aus. e und d wirken auf root und Systemkonten wie auf jedes andere Konto, die Rückfrage ist dann rot, weil Pakete oder Dienste davon abhängen können; abgelehnt wird nur, sich selbst oder dem letzten Admin den Admin zu entziehen, ihn zu sperren oder zu löschen. Ordner liest einen Pfad nach dem anderen: eintippen, tab ergänzt, Enter liest. Ein als geschützt markierter Pfad ist ein Systempfad. e ändert den angezeigten Ordner, nach einer Bestätigung, die die Zahl der Einträge und bei einem Systempfad das Risiko nennt; wird ein Stack nicht mehr gesund, wird die Änderung zurückgenommen. n legt eine Person an, deren Startpasswort beim ersten Login geändert werden muss, oder ein Dienstkonto ohne Login (kein Passwort, keine Shell, keine SSH-Schlüssel, ein Home nur auf Wunsch); e ändert den markierten (Gruppen, Admin, Docker, Passwort, SSH-Schlüssel, Sperre), jeweils eine Änderung. Sperren ist der Normalfall, wenn jemand geht: Es verhindert jeden Login, auch per SSH-Schlüssel, laufende Sitzungen bleiben offen; Entsperren gibt das Passwort frei und hebt das Kontoende auf. d löscht den markierten Benutzer und fragt vorher, ob sein Home mitgeht: Das Home zu behalten ist der Standard und lässt sich rückgängig machen, wobei das Konto mit derselben UID, denselben Gruppen und demselben Passwort neu entsteht; Home und Mail-Spool mitzulöschen lässt sich nicht rückgängig machen. Ein Benutzer mit laufenden Prozessen wird nicht gelöscht. Unter Gruppen legt n eine Gruppe an, r benennt die markierte um und d löscht sie, jeweils nach einer Bestätigung. Eine Gruppe, die die Hauptgruppe eines Benutzers ist, wird nicht gelöscht, und sudo und sysmin werden weder umbenannt noch gelöscht. Löschen lässt sich rückgängig machen, wobei die Gruppe mit derselben Gruppennummer und denselben Mitgliedern neu entsteht. Umbenennen fragt immer rot, weil Konfigurationen wie sudoers eine Gruppe über ihren Namen ansprechen und den alten behalten; auch root, docker und Systemgruppen zu löschen fragt rot. Änderungen listet die letzten Änderungen über sysmin und macht eine mit Enter rückgängig. Was nur root lesen kann, etwa Sperrstatus und SSH-Schlüssel eines Benutzers oder die Liste der Änderungen, wird mit ctrl+r über sudo gelesen; sudo fragt nach dem Passwort, sysmin sieht es nie.",

		footerID("enter read"):      "enter lesen",
		footerID("enter edit path"): "enter Pfad ändern",

		footerID("enter collapse"): "enter einklappen",
		footerID("enter expand"):   "enter ausklappen",
		tHostSysAccounts:           "Systemkonten",
		tHostSysGroups:             "Systemgruppen",
		tSectionHint:               "Enter oder space klappt diesen Abschnitt ein oder aus.",
		footerID("space tick"):     "space wählen",
	})
}
