package tui

import "maps"

// The texts of the forms and messages that create, rename and delete host groups.
const (
	tHostGroupNewTitle    textID = "HostGroupNewTitle"
	tHostGroupNewHint     textID = "HostGroupNewHint"
	tHostGroupRenameTitle textID = "HostGroupRenameTitle"
	tHostGroupRenameHint  textID = "HostGroupRenameHint"
	tHostFormNewName      textID = "HostFormNewName"
	tHostFormGroupBad     textID = "HostFormGroupBad"
	tHostFormGroupTaken   textID = "HostFormGroupTaken"
	tHostGroupKeepName    textID = "HostGroupKeepName"
	tHostGroupKeepDelete  textID = "HostGroupKeepDelete"
	tHostGroupPrimary     textID = "HostGroupPrimary"
	tHostRiskRenameGroup  textID = "HostRiskRenameGroup"
	tHostRiskGroupRoot    textID = "HostRiskGroupRoot"
	tHostRiskGroupDocker  textID = "HostRiskGroupDocker"
)

func init() {
	maps.Copy(textEN, map[textID]string{
		tHostGroupNewTitle:    "New group",
		tHostGroupNewHint:     "The system gives the group a regular group number.",
		tHostGroupRenameTitle: "Rename %s",
		tHostGroupRenameHint:  "Configurations such as sudoers address a group by its name; they keep the old name.",
		tHostFormNewName:      "New name",
		tHostFormGroupBad:     "Lower-case letters, digits, _ and -, starting with a letter or _, up to 32 characters.",
		tHostFormGroupTaken:   "This group exists already.",
		tHostGroupKeepName:    "%s keeps its name: administrator rights and access to sysmin depend on it.",
		tHostGroupKeepDelete:  "%s is never deleted: administrator rights and access to sysmin depend on it.",
		tHostGroupPrimary:     "%s is the primary group of %s and cannot be deleted.",
		tHostRiskRenameGroup:  "Configurations such as sudoers address a group by its name and keep the old one; they stop working until they are changed too.",
		tHostRiskGroupRoot:    "The group root belongs to the root account and to system files.",
		tHostRiskGroupDocker:  "Members of the group docker use Docker, which is practically root.",
	})
	maps.Copy(textDE, map[textID]string{
		tHostGroupNewTitle:    "Neue Gruppe",
		tHostGroupNewHint:     "Das System gibt der Gruppe eine reguläre Gruppennummer.",
		tHostGroupRenameTitle: "%s umbenennen",
		tHostGroupRenameHint:  "Konfigurationen wie sudoers sprechen eine Gruppe über ihren Namen an; sie behalten den alten Namen.",
		tHostFormNewName:      "Neuer Name",
		tHostFormGroupBad:     "Kleinbuchstaben, Ziffern, _ und -, am Anfang ein Buchstabe oder _, bis zu 32 Zeichen.",
		tHostFormGroupTaken:   "Diese Gruppe gibt es schon.",
		tHostGroupKeepName:    "%s behält ihren Namen: Adminrechte und der Zugang zu sysmin hängen daran.",
		tHostGroupKeepDelete:  "%s wird nie gelöscht: Adminrechte und der Zugang zu sysmin hängen daran.",
		tHostGroupPrimary:     "%s ist die Hauptgruppe von %s und kann nicht gelöscht werden.",
		tHostRiskRenameGroup:  "Konfigurationen wie sudoers sprechen eine Gruppe über ihren Namen an und behalten den alten; sie wirken nicht mehr, bis sie ebenfalls geändert sind.",
		tHostRiskGroupRoot:    "Die Gruppe root gehört zum Konto root und zu Systemdateien.",
		tHostRiskGroupDocker:  "Mitglieder der Gruppe docker nutzen Docker, und das ist praktisch root.",
	})
}
