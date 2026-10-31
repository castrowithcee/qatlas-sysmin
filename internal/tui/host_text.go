package tui

import "maps"

// The texts of the Host area. The area names, the page names and the words of the account databases keep their
// English form where the Linux tools do.
const (
	tAreaHost          textID = "AreaHost"
	tSubUsers          textID = "SubUsers"
	tSubGroups         textID = "SubGroups"
	tSubFolders        textID = "SubFolders"
	tHostColAdmin      textID = "HostColAdmin"
	tHostColDocker     textID = "HostColDocker"
	tHostColGroups     textID = "HostColGroups"
	tHostColMembers    textID = "HostColMembers"
	tHostLblEnabled    textID = "HostLblEnabled"
	tHostLblHome       textID = "HostLblHome"
	tHostLblLocked     textID = "HostLblLocked"
	tHostYes           textID = "HostYes"
	tHostNo            textID = "HostNo"
	tHostAdminOff      textID = "HostAdminOff"
	tHostAdminOffShort textID = "HostAdminOffShort"
	tHostColSystem     textID = "HostColSystem"
	tHostSystem        textID = "HostSystem"
	tHostSystemRO      textID = "HostSystemRO"
	tHostRegular       textID = "HostRegular"
	tHostUnknown       textID = "HostUnknown"
	tHostNone          textID = "HostNone"
	tHostNoUsers       textID = "HostNoUsers"
	tHostNoGroups      textID = "HostNoGroups"
	tHostNoMembers     textID = "HostNoMembers"
	tHostNoKeys        textID = "HostNoKeys"
	tHostKeysUnknown   textID = "HostKeysUnknown"
	tHostFoldersSoon   textID = "HostFoldersSoon"
)

func init() {
	maps.Copy(textEN, map[textID]string{
		tAreaHost:          "Host",
		tSubUsers:          "Users",
		tSubGroups:         "Groups",
		tSubFolders:        "Folders",
		tHostColAdmin:      "Admin",
		tHostColDocker:     "Docker",
		tHostColGroups:     "Groups",
		tHostColMembers:    "Members",
		tHostLblEnabled:    "Enabled (sysmin)",
		tHostLblHome:       "Home",
		tHostLblLocked:     "Locked",
		tHostYes:           "yes",
		tHostNo:            "no",
		tHostAdminOff:      "yes · not enabled",
		tHostAdminOffShort: "yes · no sysmin",
		tHostColSystem:     "System",
		tHostSystem:        "system",
		tHostSystemRO:      "system (read-only)",
		tHostRegular:       "regular",
		tHostUnknown:       "unknown",
		tHostNone:          "none",
		tHostNoUsers:       "No users found.",
		tHostNoGroups:      "No groups found.",
		tHostNoMembers:     "No members.",
		tHostNoKeys:        "No SSH keys.",
		tHostKeysUnknown:   "SSH keys: unknown (only root can read them).",
		tHostFoldersSoon:   "Folders are not shown yet.",

		footerID("space hide system"): "space hide system",
		footerID("space show system"): "space show system",
	})
	maps.Copy(textDE, map[textID]string{
		tAreaHost:          "Host",
		tSubUsers:          "Benutzer",
		tSubGroups:         "Gruppen",
		tSubFolders:        "Ordner",
		tHostColAdmin:      "Admin",
		tHostColDocker:     "Docker",
		tHostColGroups:     "Gruppen",
		tHostColMembers:    "Mitglieder",
		tHostLblEnabled:    "Freigeschaltet (sysmin)",
		tHostLblHome:       "Home",
		tHostLblLocked:     "Gesperrt",
		tHostYes:           "ja",
		tHostNo:            "nein",
		tHostAdminOff:      "ja · nicht freigeschaltet",
		tHostAdminOffShort: "ja · ohne sysmin",
		tHostColSystem:     "System",
		tHostSystem:        "System",
		tHostSystemRO:      "System (nur lesbar)",
		tHostRegular:       "normal",
		tHostUnknown:       "unbekannt",
		tHostNone:          "keine",
		tHostNoUsers:       "Keine Benutzer gefunden.",
		tHostNoGroups:      "Keine Gruppen gefunden.",
		tHostNoMembers:     "Keine Mitglieder.",
		tHostNoKeys:        "Keine SSH-Schlüssel.",
		tHostKeysUnknown:   "SSH-Schlüssel: unbekannt (nur root kann sie lesen).",
		tHostFoldersSoon:   "Ordner werden noch nicht angezeigt.",

		footerID("space hide system"): "space System ausblenden",
		footerID("space show system"): "space System anzeigen",
	})
}
