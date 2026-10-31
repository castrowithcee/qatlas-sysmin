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
		tInfoHostUsers:       "Admin: the user may become root with sudo. Docker: the user may use Docker, which is practically root on this host. Enabled: the user is a member of the group sysmin; an admin who is also enabled may use sysmin.",
		tInfoHostGroups:      "The groups of this host with their members. System groups are read-only here.",
		tInfoHostFolders:     "Shows who may read and write a folder or file: owner, group and others, in plain words. Nothing is changed.",
		tHelpHost:            "Host shows the users, groups and folder permissions of this machine, read-only. Admin means sudo rights, Docker is practically root, and enabled means membership of the group sysmin. Grey rows are system accounts; space hides them. Folders reads one path at a time: type it, tab completes, enter reads. A path marked protected is a system path.",

		footerID("enter read"):      "enter read",
		footerID("enter edit path"): "enter edit path",

		footerID("space hide system"): "space hide system",
		footerID("space show system"): "space show system",
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
		tInfoHostUsers:       "Admin: Der Benutzer kann mit sudo root werden. Docker: Der Benutzer darf Docker nutzen, was auf diesem Host praktisch root bedeutet. Freigeschaltet: Der Benutzer ist Mitglied der Gruppe sysmin; ein freigeschalteter Admin darf sysmin nutzen.",
		tInfoHostGroups:      "Die Gruppen dieses Hosts mit ihren Mitgliedern. Systemgruppen sind hier nur lesbar.",
		tInfoHostFolders:     "Zeigt in einfachen Worten, wer einen Ordner oder eine Datei lesen und schreiben darf: Besitzer, Gruppe und andere. Es wird nichts geändert.",
		tHelpHost:            "Host zeigt die Benutzer, Gruppen und Ordnerrechte dieses Rechners, nur lesend. Admin heißt sudo-Rechte, Docker ist praktisch root, und freigeschaltet heißt Mitglied der Gruppe sysmin. Graue Zeilen sind Systemkonten; space blendet sie aus. Ordner liest einen Pfad nach dem anderen: eintippen, tab ergänzt, Enter liest. Ein als geschützt markierter Pfad ist ein Systempfad.",

		footerID("enter read"):      "enter lesen",
		footerID("enter edit path"): "enter Pfad ändern",

		footerID("space hide system"): "space System ausblenden",
		footerID("space show system"): "space System anzeigen",
	})
}
