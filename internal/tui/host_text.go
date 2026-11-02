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
		tInfoHostUsers:       "Admin: the user may become root with sudo. Docker: the user may use Docker, which is practically root on this host. Enabled: the user is a member of the group sysmin; an admin who is also enabled may use sysmin. n creates a user, e changes the marked one; every change asks first, runs through sudo and can be undone under Changes.",
		tInfoHostGroups:      "The groups of this host with their members. System groups are read-only here.",
		tInfoHostFolders:     "Shows who may read and write a folder or file: owner, group and others, in plain words. e changes the folder shown: owner, group and the three levels, also for everything below it; the change asks first, runs through sudo and can be undone under Changes.",
		tHelpHost:            "Host shows the users, groups and folder permissions of this machine. Admin means sudo rights, Docker is practically root, and enabled means membership of the group sysmin. Grey rows are system accounts and groups, whose ID lies outside 1000 to 60000; they sit in their own section, which starts collapsed. enter or space on a heading collapses or expands it. e changes root and system accounts like any other account, and the confirmation is then red, because packages or services may depend on them; only taking admin from yourself or from the last admin is refused. Folders reads one path at a time: type it, tab completes, enter reads. A path marked protected is a system path. e changes the folder shown, after a confirmation that names the number of entries and, for a system path, the risk; if a stack stops being healthy, the change is taken back. n creates a user whose initial password must be changed at the first login; e changes the marked one (groups, Admin, Docker, password, SSH keys), one change at a time. Changes lists the latest changes made through sysmin and undoes one with enter. What only root can read, such as the locked state and SSH keys of a user or the list of changes, is read through sudo when you press ctrl+r; sudo asks for the password, sysmin never sees it.",

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
		tInfoHostUsers:       "Admin: Der Benutzer kann mit sudo root werden. Docker: Der Benutzer darf Docker nutzen, was auf diesem Host praktisch root bedeutet. Freigeschaltet: Der Benutzer ist Mitglied der Gruppe sysmin; ein freigeschalteter Admin darf sysmin nutzen. n legt einen Benutzer an, e ändert den markierten; jede Änderung fragt vorher, läuft über sudo und lässt sich unter Änderungen rückgängig machen.",
		tInfoHostGroups:      "Die Gruppen dieses Hosts mit ihren Mitgliedern. Systemgruppen sind hier nur lesbar.",
		tInfoHostFolders:     "Zeigt in einfachen Worten, wer einen Ordner oder eine Datei lesen und schreiben darf: Besitzer, Gruppe und andere. e ändert den angezeigten Ordner: Besitzer, Gruppe und die drei Stufen, auch für alles darunter; die Änderung fragt vorher, läuft über sudo und lässt sich unter Änderungen rückgängig machen.",
		tHelpHost:            "Host zeigt die Benutzer, Gruppen und Ordnerrechte dieses Rechners. Admin heißt sudo-Rechte, Docker ist praktisch root, und freigeschaltet heißt Mitglied der Gruppe sysmin. Graue Zeilen sind Systemkonten und Systemgruppen, deren ID außerhalb von 1000 bis 60000 liegt; sie stehen in einem eigenen Abschnitt, der eingeklappt beginnt. Enter oder space auf einer Überschrift klappt ihn ein oder aus. e ändert root und Systemkonten wie jedes andere Konto, die Rückfrage ist dann rot, weil Pakete oder Dienste davon abhängen können; abgelehnt wird nur, sich selbst oder dem letzten Admin den Admin zu entziehen. Ordner liest einen Pfad nach dem anderen: eintippen, tab ergänzt, Enter liest. Ein als geschützt markierter Pfad ist ein Systempfad. e ändert den angezeigten Ordner, nach einer Bestätigung, die die Zahl der Einträge und bei einem Systempfad das Risiko nennt; wird ein Stack nicht mehr gesund, wird die Änderung zurückgenommen. n legt einen Benutzer an, dessen Startpasswort beim ersten Login geändert werden muss; e ändert den markierten (Gruppen, Admin, Docker, Passwort, SSH-Schlüssel), jeweils eine Änderung. Änderungen listet die letzten Änderungen über sysmin und macht eine mit Enter rückgängig. Was nur root lesen kann, etwa Sperrstatus und SSH-Schlüssel eines Benutzers oder die Liste der Änderungen, wird mit ctrl+r über sudo gelesen; sudo fragt nach dem Passwort, sysmin sieht es nie.",

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
