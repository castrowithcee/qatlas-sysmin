package tui

import "maps"

// The texts of the stack folder setup.
const (
	tHostStacksTitle        textID = "HostStacksTitle"
	tHostStacksOthers       textID = "HostStacksOthers"
	tHostStacksNothing      textID = "HostStacksNothing"
	tHostStacksNothingNote  textID = "HostStacksNothingNote"
	tHostStacksFolder       textID = "HostStacksFolder"
	tHostStacksRunning      textID = "HostStacksRunning"
	tHostStacksSetUpAlready textID = "HostStacksSetUpAlready"
	tHostStacksStays        textID = "HostStacksStays"
	tHostStacksMore         textID = "HostStacksMore"
	tHostStacksMoreCommands textID = "HostStacksMoreCommands"
)

func init() {
	maps.Copy(textEN, map[textID]string{
		tHostStacksTitle:        "Set up the stack folder %s for the service?",
		tHostStacksOthers:       "Other users except root have no access to these folders and files afterwards.",
		tHostStacksNothing:      "Nothing to set up in %s",
		tHostStacksNothingNote:  "The folder and its stacks are set up already. Nothing was changed.",
		tHostStacksFolder:       "folder",
		tHostStacksRunning:      "(running)",
		tHostStacksSetUpAlready: "set up already",
		tHostStacksStays:        "Stays: %s (%s)",
		tHostStacksMore:         "… and %d more entries, not shown",
		tHostStacksMoreCommands: "… and %d more commands",

		footerID("s set up stacks"): "s set up stacks",
	})
	maps.Copy(textDE, map[textID]string{
		tHostStacksTitle:        "Stack-Ordner %s für den Dienst einrichten?",
		tHostStacksOthers:       "Andere Benutzer außer root haben danach keinen Zugriff auf diese Ordner und Dateien.",
		tHostStacksNothing:      "In %s ist nichts einzurichten",
		tHostStacksNothingNote:  "Der Ordner und seine Stacks sind schon eingerichtet. Es wurde nichts geändert.",
		tHostStacksFolder:       "Ordner",
		tHostStacksRunning:      "(läuft)",
		tHostStacksSetUpAlready: "schon eingerichtet",
		tHostStacksStays:        "Bleibt: %s (%s)",
		tHostStacksMore:         "… und %d weitere Einträge, nicht gezeigt",
		tHostStacksMoreCommands: "… und %d weitere Befehle",

		footerID("s set up stacks"): "s Stacks einrichten",
	})
}
