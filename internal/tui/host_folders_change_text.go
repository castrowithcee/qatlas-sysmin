package tui

import "maps"

// The texts of the form that changes a folder.
const (
	tHostFolderChangeTitle textID = "HostFolderChangeTitle"
	tHostFolderOwner       textID = "HostFolderOwner"
	tHostFolderGroup       textID = "HostFolderGroup"
	tHostFolderOwnerMay    textID = "HostFolderOwnerMay"
	tHostFolderGroupMay    textID = "HostFolderGroupMay"
	tHostFolderOthersMay   textID = "HostFolderOthersMay"
	tHostFolderBelow       textID = "HostFolderBelow"
	tHostFolderSystemHint  textID = "HostFolderSystemHint"
	tHostFolderSystemWarn  textID = "HostFolderSystemWarn"
	tHostFolderOnlyDir     textID = "HostFolderOnlyDir"
	tHostFolderCount       textID = "HostFolderCount"
	tHostFolderTooMany     textID = "HostFolderTooMany"
	tHostFolderCountFailed textID = "HostFolderCountFailed"
)

func init() {
	maps.Copy(textEN, map[textID]string{
		tHostFolderChangeTitle: "Change %s",
		tHostFolderOwner:       "Owner",
		tHostFolderGroup:       "Group",
		tHostFolderOwnerMay:    "Owner may",
		tHostFolderGroupMay:    "Group may",
		tHostFolderOthersMay:   "Others may",
		tHostFolderBelow:       "Everything below it",
		tHostFolderSystemHint:  "This is a system path. Changing it can break the host.",
		tHostFolderSystemWarn:  "This is a system path: wrong owners or permissions here can stop services and block logins on this host.",
		tHostFolderOnlyDir:     "Only a folder can be changed here, not a file.",
		tHostFolderCount:       "%d entries are changed: the folder and everything below it.",
		tHostFolderTooMany:     "%s has more than %d entries. Change a smaller folder. Nothing was changed.",
		tHostFolderCountFailed: "The entries below the folder cannot be counted: %s. Nothing was changed.",

		footerID("e change"): "e change",
	})
	maps.Copy(textDE, map[textID]string{
		tHostFolderChangeTitle: "%s ändern",
		tHostFolderOwner:       "Besitzer",
		tHostFolderGroup:       "Gruppe",
		tHostFolderOwnerMay:    "Besitzer darf",
		tHostFolderGroupMay:    "Gruppe darf",
		tHostFolderOthersMay:   "Andere dürfen",
		tHostFolderBelow:       "Alles darunter",
		tHostFolderSystemHint:  "Das ist ein Systempfad. Eine Änderung kann den Host beschädigen.",
		tHostFolderSystemWarn:  "Das ist ein Systempfad: Falsche Besitzer oder Rechte können Dienste stoppen und Anmeldungen auf diesem Host blockieren.",
		tHostFolderOnlyDir:     "Hier lässt sich nur ein Ordner ändern, keine Datei.",
		tHostFolderCount:       "%d Einträge werden geändert: der Ordner und alles darunter.",
		tHostFolderTooMany:     "%s hat mehr als %d Einträge. Ändere einen kleineren Ordner. Es wurde nichts geändert.",
		tHostFolderCountFailed: "Die Einträge unter dem Ordner lassen sich nicht zählen: %s. Es wurde nichts geändert.",

		footerID("e change"): "e ändern",
	})
}
