package tui

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
	tHostSysAccounts     textID = "HostSysAccounts"
	tHostSysGroups       textID = "HostSysGroups"
	tGroupRowHint        textID = "GroupRowHint"
)

// hostTexts are the texts of the Host area, in English and German.
func hostTexts() (en, de map[textID]string) {
	en = map[textID]string{
		tAreaHost:            "Host",
		tSubUsers:            "Users",
		tSubGroups:           "Groups",
		tSubFolders:          "Folders",
		tHostColAdmin:        "Admin",
		tHostColDocker:       "Docker",
		tHostColGroups:       "Groups",
		tHostColMembers:      "Members",
		tHostLblEnabled:      "Enabled (qsysmin)",
		tHostLblHome:         "Home",
		tHostLblLocked:       "Locked",
		tHostYes:             "yes",
		tHostNo:              "no",
		tHostAdminOff:        "yes · not enabled",
		tHostAdminOffShort:   "yes · no qsysmin",
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
		tHostProtectedNote:   "a system path; qsysmin does not change its permissions",
		tInfoHostUsers:       "Admin: the user may become root with sudo. Docker: the user may use Docker, which is practically root on this host. Enabled: the user is a member of the group qsysmin; an admin who is also enabled may use qsysmin. n creates a user, e changes the marked one, d deletes it; every change asks first, runs through sudo and can be undone under Changes, except deleting a home.",
		tInfoHostGroups:      "The groups of this host with their members. n creates a group, e changes its members, r renames and d deletes the marked one; sudo and qsysmin are never renamed or deleted.",
		tInfoHostFolders:     "Shows who may read and write a folder or file: owner, group and others, in plain words. e changes the folder shown: owner, group and the three levels, also for everything below it. Every change asks first, runs through sudo and can be undone under Changes.",

		tHostSysAccounts: "System accounts",
		tHostSysGroups:   "System groups",
		tGroupRowHint:    "Press {enter} to collapse or expand this group.",
	}
	de = map[textID]string{
		tAreaHost:            "Host",
		tSubUsers:            "Benutzer",
		tSubGroups:           "Gruppen",
		tSubFolders:          "Ordner",
		tHostColAdmin:        "Admin",
		tHostColDocker:       "Docker",
		tHostColGroups:       "Gruppen",
		tHostColMembers:      "Mitglieder",
		tHostLblEnabled:      "Freigeschaltet (qsysmin)",
		tHostLblHome:         "Home",
		tHostLblLocked:       "Gesperrt",
		tHostYes:             "ja",
		tHostNo:              "nein",
		tHostAdminOff:        "ja · nicht freigeschaltet",
		tHostAdminOffShort:   "ja · ohne qsysmin",
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
		tHostProtectedNote:   "ein Systempfad; qsysmin ändert seine Rechte nicht",
		tInfoHostUsers:       "Admin: Der Benutzer kann mit sudo root werden. Docker: Der Benutzer darf Docker nutzen, was auf diesem Host praktisch root bedeutet. Freigeschaltet: Der Benutzer ist Mitglied der Gruppe qsysmin; ein freigeschalteter Admin darf qsysmin nutzen. n legt einen Benutzer an, e ändert den markierten, d löscht ihn; jede Änderung fragt vorher, läuft über sudo und lässt sich unter Änderungen rückgängig machen, außer ein gelöschtes Home.",
		tInfoHostGroups:      "Die Gruppen dieses Hosts mit ihren Mitgliedern. n legt eine Gruppe an, e ändert ihre Mitglieder, r benennt die markierte um und d löscht sie; sudo und qsysmin werden nie umbenannt oder gelöscht.",
		tInfoHostFolders:     "Zeigt in einfachen Worten, wer einen Ordner oder eine Datei lesen und schreiben darf: Besitzer, Gruppe und andere. e ändert den angezeigten Ordner: Besitzer, Gruppe und die drei Stufen, auch für alles darunter. Jede Änderung fragt vorher, läuft über sudo und lässt sich unter Änderungen rückgängig machen.",

		tHostSysAccounts: "Systemkonten",
		tHostSysGroups:   "Systemgruppen",
		tGroupRowHint:    "{enter} klappt diese Gruppe ein oder aus.",
	}
	return en, de
}
