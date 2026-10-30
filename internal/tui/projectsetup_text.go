package tui

import "maps"

// The texts of the start of a project: adopting a running stack, creating a credential on the way, and
// editing or duplicating a stored project. They join the catalogs when the package starts.
const (
	tPrjOriginTitle textID = "PrjOriginTitle"
	tPrjOriginNote  textID = "PrjOriginNote"
	tPrjOriginNew   textID = "PrjOriginNew"
	tPrjOriginAdopt textID = "PrjOriginAdopt"

	tPrjAdoptPrompt textID = "PrjAdoptPrompt"
	tPrjAdoptHint   textID = "PrjAdoptHint"
	tPrjAdoptDir    textID = "PrjAdoptDir"
	tPrjAdoptName   textID = "PrjAdoptName"
	tPrjBadName     textID = "PrjBadName"
	tBusyAdopt      textID = "BusyAdopt"
	tPrjNoDir       textID = "PrjNoDir"
	tPrjNoAccess    textID = "PrjNoAccess"
	tPrjNoDocker    textID = "PrjNoDocker"
	tPrjAdoptTitle  textID = "PrjAdoptTitle"
	tPrjAdoptOK     textID = "PrjAdoptOK"
	tPrjAdoptBlock  textID = "PrjAdoptBlock"
	tPrjMore        textID = "PrjMore"
	tPrjNoCurrent   textID = "PrjNoCurrent"

	tCredPickTitle textID = "CredPickTitle"
	tCredPickNote  textID = "CredPickNote"
	tCredNone      textID = "CredNone"
	tCredNew       textID = "CredNew"
	tBusyCreds     textID = "BusyCreds"
	tCredTitle     textID = "CredTitle"
	tCredName      textID = "CredName"
	tCredKind      textID = "CredKind"
	tCredBadName   textID = "CredBadName"
	tCredExists    textID = "CredExists"
	tCredFill      textID = "CredFill"
	tBusyCredSave  textID = "BusyCredSave"
	tCredSaved     textID = "CredSaved"

	tFlowNoChannels textID = "FlowNoChannels"
	tBusyProjectGet textID = "BusyProjectGet"
	tProjFileBad    textID = "ProjFileBad"
)

func init() {
	maps.Copy(textEN, map[textID]string{
		tPrjOriginTitle: "Start a project",
		tPrjOriginNote:  "Set up a new project or take over a stack that runs already.",
		tPrjOriginNew:   "New project",
		tPrjOriginAdopt: "Adopt running stack",

		tPrjAdoptPrompt: "Adopt running stack",
		tPrjAdoptHint:   "Only reads the stack; nothing is saved.",
		tPrjAdoptDir:    "Directory",
		tPrjAdoptName:   "Name (optional)",
		tPrjBadName:     "Use lower-case letters, digits and hyphens.",
		tBusyAdopt:      "Reading the stack",
		tPrjNoDir:       "That directory does not exist.",
		tPrjNoAccess:    "The service user sysmin may not pass %[1]s. Next: sudo setfacl -m u:sysmin:x %[1]s",
		tPrjNoDocker:    "Docker cannot be asked for the running containers.",
		tPrjAdoptTitle:  "Adopt %s",
		tPrjAdoptOK:     "Taking over fills in the setup; nothing is saved before the summary.",
		tPrjAdoptBlock:  "Blocking problems keep this stack from being taken over.",
		tPrjMore:        "+%d more",
		tPrjNoCurrent:   "version unknown",

		tCredPickTitle: "Credential",
		tCredPickNote:  "Pick one from the vault or create a new one.",
		tCredNone:      "none",
		tCredNew:       "New credential...",
		tBusyCreds:     "Reading credentials",
		tCredTitle:     "New credential",
		tCredName:      "Name",
		tCredKind:      "Kind",
		tCredBadName:   "Use lower-case letters, digits, dots, underscores and hyphens.",
		tCredExists:    "A credential with this name exists already.",
		tCredFill:      "Fill in every field.",
		tBusyCredSave:  "Saving credential %s",
		tCredSaved:     "Credential %s saved.",

		tFlowNoChannels: "No channels yet; they are added under Library.",
		tBusyProjectGet: "Reading %s",
		tProjFileBad:    "The project file cannot be read.",

		footerID("e edit"):          "e edit",
		footerID("p duplicate"):     "p duplicate",
		footerID("enter take over"): "enter take over",
		footerID("enter next"):      "enter next",
		footerID("←/→ kind"):        "←/→ kind",
	})
	maps.Copy(textDE, map[textID]string{
		tPrjOriginTitle: "Projekt beginnen",
		tPrjOriginNote:  "Ein neues Projekt anlegen oder einen laufenden Stack übernehmen.",
		tPrjOriginNew:   "Neues Projekt",
		tPrjOriginAdopt: "Laufenden Stack übernehmen",

		tPrjAdoptPrompt: "Laufenden Stack übernehmen",
		tPrjAdoptHint:   "Liest den Stack nur; nichts wird gespeichert.",
		tPrjAdoptDir:    "Verzeichnis",
		tPrjAdoptName:   "Name (optional)",
		tPrjBadName:     "Kleinbuchstaben, Ziffern und Bindestriche verwenden.",
		tBusyAdopt:      "Der Stack wird gelesen",
		tPrjNoDir:       "Dieses Verzeichnis gibt es nicht.",
		tPrjNoAccess:    "Der Dienstbenutzer sysmin darf %[1]s nicht durchqueren. Nächster Schritt: sudo setfacl -m u:sysmin:x %[1]s",
		tPrjNoDocker:    "Docker kann nicht nach den laufenden Containern gefragt werden.",
		tPrjAdoptTitle:  "%s übernehmen",
		tPrjAdoptOK:     "Das Übernehmen füllt den Ablauf vor; gespeichert wird erst in der Zusammenfassung.",
		tPrjAdoptBlock:  "Blockierende Probleme verhindern das Übernehmen dieses Stacks.",
		tPrjMore:        "+%d weitere",
		tPrjNoCurrent:   "Version unbekannt",

		tCredPickTitle: "Credential",
		tCredPickNote:  "Eines aus dem Tresor wählen oder ein neues anlegen.",
		tCredNone:      "keins",
		tCredNew:       "Neues Credential...",
		tBusyCreds:     "Credentials werden gelesen",
		tCredTitle:     "Neues Credential",
		tCredName:      "Name",
		tCredKind:      "Art",
		tCredBadName:   "Kleinbuchstaben, Ziffern, Punkte, Unterstriche und Bindestriche verwenden.",
		tCredExists:    "Ein Credential mit diesem Namen gibt es schon.",
		tCredFill:      "Alle Felder ausfüllen.",
		tBusyCredSave:  "Credential %s wird gespeichert",
		tCredSaved:     "Credential %s gespeichert.",

		tFlowNoChannels: "Noch keine Kanäle; sie werden unter Library angelegt.",
		tBusyProjectGet: "%s wird gelesen",
		tProjFileBad:    "Die Projektdatei lässt sich nicht lesen.",

		footerID("e edit"):          "e bearbeiten",
		footerID("p duplicate"):     "p duplizieren",
		footerID("enter take over"): "enter übernehmen",
		footerID("enter next"):      "enter weiter",
		footerID("←/→ kind"):        "←/→ Art",
	})
}
