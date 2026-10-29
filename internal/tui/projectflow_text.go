package tui

import "maps"

// The texts of the guided project setup. They join the catalogs when the package starts, like the texts of
// the Projects area.
const (
	tFlowTitleNew    textID = "FlowTitleNew"
	tFlowTitleEdit   textID = "FlowTitleEdit"
	tFlowStepStart   textID = "FlowStepStart"
	tFlowStepSource  textID = "FlowStepSource"
	tFlowStepTarget  textID = "FlowStepTarget"
	tFlowStepSteps   textID = "FlowStepSteps"
	tFlowStepRules   textID = "FlowStepRules"
	tFlowStepChecks  textID = "FlowStepChecks"
	tFlowStepNotify  textID = "FlowStepNotify"
	tFlowStepSummary textID = "FlowStepSummary"

	tFlowLblName           textID = "FlowLblName"
	tFlowLblScenario       textID = "FlowLblScenario"
	tFlowLblSource         textID = "FlowLblSource"
	tFlowLblGitURL         textID = "FlowLblGitURL"
	tFlowLblCredential     textID = "FlowLblCredential"
	tFlowLblImageName      textID = "FlowLblImageName"
	tFlowLblImageRef       textID = "FlowLblImageRef"
	tFlowLblEnvVar         textID = "FlowLblEnvVar"
	tFlowLblServices       textID = "FlowLblServices"
	tFlowLblDir            textID = "FlowLblDir"
	tFlowLblEnv            textID = "FlowLblEnv"
	tFlowLblCompose        textID = "FlowLblCompose"
	tFlowLblComposeProject textID = "FlowLblComposeProject"
	tFlowLblPoll           textID = "FlowLblPoll"
	tFlowLblMode           textID = "FlowLblMode"
	tFlowLblMaxUpdate      textID = "FlowLblMaxUpdate"
	tFlowLblMajorSteps     textID = "FlowLblMajorSteps"
	tFlowLblTagFilter      textID = "FlowLblTagFilter"
	tFlowLblChangelogRepo  textID = "FlowLblChangelogRepo"
	tFlowLblChangelogCred  textID = "FlowLblChangelogCred"
	tFlowLblStepType       textID = "FlowLblStepType"
	tFlowLblAssign         textID = "FlowLblAssign"
	tFlowLblTag            textID = "FlowLblTag"
	tFlowLblRun            textID = "FlowLblRun"
	tFlowLblBackup         textID = "FlowLblBackup"
	tFlowLblVolume         textID = "FlowLblVolume"
	tFlowLblCommand        textID = "FlowLblCommand"
	tFlowLblKeep           textID = "FlowLblKeep"
	tFlowLblRestore        textID = "FlowLblRestore"
	tFlowLblURL            textID = "FlowLblURL"
	tFlowLblStatus         textID = "FlowLblStatus"
	tFlowLblContains       textID = "FlowLblContains"
	tFlowLblContainers     textID = "FlowLblContainers"
	tFlowLblChannels       textID = "FlowLblChannels"
	tFlowLblEvents         textID = "FlowLblEvents"
	tFlowLblHookKind       textID = "FlowLblHookKind"
	tFlowLblHookSecret     textID = "FlowLblHookSecret"

	tFlowHeadImage    textID = "FlowHeadImage"
	tFlowHeadStep     textID = "FlowHeadStep"
	tFlowHeadBackup   textID = "FlowHeadBackup"
	tFlowHeadCheck    textID = "FlowHeadCheck"
	tFlowAddImage     textID = "FlowAddImage"
	tFlowAddStep      textID = "FlowAddStep"
	tFlowAddHTTP      textID = "FlowAddHTTP"
	tFlowAddContainer textID = "FlowAddContainer"
	tFlowAdvanced     textID = "FlowAdvanced"
	tFlowSumImage     textID = "FlowSumImage"
	tFlowSumCompose   textID = "FlowSumCompose"

	tFlowDefault      textID = "FlowDefault"
	tFlowRequired     textID = "FlowRequired"
	tFlowAbsolute     textID = "FlowAbsolute"
	tFlowNumber       textID = "FlowNumber"
	tFlowDuration     textID = "FlowDuration"
	tFlowFix          textID = "FlowFix"
	tFlowRefused      textID = "FlowRefused"
	tFlowExists       textID = "FlowExists"
	tFlowSaved        textID = "FlowSaved"
	tFlowSavedPending textID = "FlowSavedPending"
	tFlowProblems     textID = "FlowProblems"
	tFlowProblemsIn   textID = "FlowProblemsIn"
	tBusyFlowSave     textID = "BusyFlowSave"

	tInfoFlowName           textID = "InfoFlowName"
	tInfoFlowScenario       textID = "InfoFlowScenario"
	tInfoFlowSource         textID = "InfoFlowSource"
	tInfoFlowGitURL         textID = "InfoFlowGitURL"
	tInfoFlowCredential     textID = "InfoFlowCredential"
	tInfoFlowImageName      textID = "InfoFlowImageName"
	tInfoFlowImageRef       textID = "InfoFlowImageRef"
	tInfoFlowEnvVar         textID = "InfoFlowEnvVar"
	tInfoFlowServices       textID = "InfoFlowServices"
	tInfoFlowAddImage       textID = "InfoFlowAddImage"
	tInfoFlowDir            textID = "InfoFlowDir"
	tInfoFlowEnv            textID = "InfoFlowEnv"
	tInfoFlowCompose        textID = "InfoFlowCompose"
	tInfoFlowComposeProject textID = "InfoFlowComposeProject"
	tInfoFlowPoll           textID = "InfoFlowPoll"
	tInfoFlowMode           textID = "InfoFlowMode"
	tInfoFlowMaxUpdate      textID = "InfoFlowMaxUpdate"
	tInfoFlowMajorSteps     textID = "InfoFlowMajorSteps"
	tInfoFlowTagFilter      textID = "InfoFlowTagFilter"
	tInfoFlowChangelogRepo  textID = "InfoFlowChangelogRepo"
	tInfoFlowChangelogCred  textID = "InfoFlowChangelogCred"
	tInfoFlowStepType       textID = "InfoFlowStepType"
	tInfoFlowAssign         textID = "InfoFlowAssign"
	tInfoFlowStepServices   textID = "InfoFlowStepServices"
	tInfoFlowTag            textID = "InfoFlowTag"
	tInfoFlowRun            textID = "InfoFlowRun"
	tInfoFlowAddStep        textID = "InfoFlowAddStep"
	tInfoFlowBackup         textID = "InfoFlowBackup"
	tInfoFlowVolume         textID = "InfoFlowVolume"
	tInfoFlowBackupCommand  textID = "InfoFlowBackupCommand"
	tInfoFlowKeep           textID = "InfoFlowKeep"
	tInfoFlowRestore        textID = "InfoFlowRestore"
	tInfoFlowURL            textID = "InfoFlowURL"
	tInfoFlowStatus         textID = "InfoFlowStatus"
	tInfoFlowContains       textID = "InfoFlowContains"
	tInfoFlowCheckServices  textID = "InfoFlowCheckServices"
	tInfoFlowContainers     textID = "InfoFlowContainers"
	tInfoFlowAddCheck       textID = "InfoFlowAddCheck"
	tInfoFlowChannels       textID = "InfoFlowChannels"
	tInfoFlowEvents         textID = "InfoFlowEvents"
	tInfoFlowHookKind       textID = "InfoFlowHookKind"
	tInfoFlowHookSecret     textID = "InfoFlowHookSecret"
	tInfoFlowAdvanced       textID = "InfoFlowAdvanced"
	tInfoFlowSummary        textID = "InfoFlowSummary"
)

func init() {
	maps.Copy(textEN, map[textID]string{
		tFlowTitleNew:    "New project",
		tFlowTitleEdit:   "Edit project",
		tFlowStepStart:   "Start",
		tFlowStepSource:  "Source",
		tFlowStepTarget:  "Target",
		tFlowStepSteps:   "Steps",
		tFlowStepRules:   "Version rule",
		tFlowStepChecks:  "Checks",
		tFlowStepNotify:  "Notify",
		tFlowStepSummary: "Summary",

		tFlowLblName:           "Name",
		tFlowLblScenario:       "Scenario",
		tFlowLblSource:         "Source",
		tFlowLblGitURL:         "Repository",
		tFlowLblCredential:     "Credential",
		tFlowLblImageName:      "Name",
		tFlowLblImageRef:       "Reference",
		tFlowLblEnvVar:         "Env variable",
		tFlowLblServices:       "Services",
		tFlowLblDir:            "Directory",
		tFlowLblEnv:            "Env file",
		tFlowLblCompose:        "Compose file",
		tFlowLblComposeProject: "Compose project",
		tFlowLblPoll:           "Poll interval",
		tFlowLblMode:           "Mode",
		tFlowLblMaxUpdate:      "Max update",
		tFlowLblMajorSteps:     "Major steps",
		tFlowLblTagFilter:      "Tag filter",
		tFlowLblChangelogRepo:  "Changelog repo",
		tFlowLblChangelogCred:  "Changelog credential",
		tFlowLblStepType:       "Type",
		tFlowLblAssign:         "Assignments",
		tFlowLblTag:            "Tag",
		tFlowLblRun:            "Command",
		tFlowLblBackup:         "Backup",
		tFlowLblVolume:         "Volume",
		tFlowLblCommand:        "Command",
		tFlowLblKeep:           "Keep",
		tFlowLblRestore:        "Restore command",
		tFlowLblURL:            "URL",
		tFlowLblStatus:         "Status",
		tFlowLblContains:       "Contains",
		tFlowLblContainers:     "Containers",
		tFlowLblChannels:       "Channels",
		tFlowLblEvents:         "Events",
		tFlowLblHookKind:       "Webhook",
		tFlowLblHookSecret:     "Secret credential",

		tFlowHeadImage:    "Image %d",
		tFlowHeadStep:     "Step %d",
		tFlowHeadBackup:   "Backup",
		tFlowHeadCheck:    "Check %d · %s",
		tFlowAddImage:     "Add image",
		tFlowAddStep:      "Add step",
		tFlowAddHTTP:      "Add HTTP check",
		tFlowAddContainer: "Add container check",
		tFlowAdvanced:     "Advanced",
		tFlowSumImage:     "Image %s",
		tFlowSumCompose:   "Compose files",

		tFlowDefault:      "%s (default)",
		tFlowRequired:     "Required.",
		tFlowAbsolute:     "Use an absolute path.",
		tFlowNumber:       "Use a whole number.",
		tFlowDuration:     "Use a duration such as 30m or 6h.",
		tFlowFix:          "Fix the marked rows first.",
		tFlowRefused:      "The service found %d problem(s); nothing was saved.",
		tFlowExists:       "A project with this name exists already.",
		tFlowSaved:        "Project %s saved and approved.",
		tFlowSavedPending: "Project %s saved; it waits for approval.",
		tFlowProblems:     "Problems",
		tFlowProblemsIn:   "Problems in: %s",
		tBusyFlowSave:     "Saving %s",

		tInfoFlowName:           "The name of the project file: lower-case letters, digits and hyphens.",
		tInfoFlowScenario:       "The steps an update runs: a built-in template, or steps of the project's own.",
		tInfoFlowSource:         "Where new versions come from: container images, or the tags of a git repository.",
		tInfoFlowGitURL:         "The repository whose tags are the versions, as https://, ssh:// or user@host:path, without a password.",
		tInfoFlowCredential:     "A vault credential for the registry or repository, picked from the list or created there; none for public sources.",
		tInfoFlowImageName:      "A short name for the image within the project.",
		tInfoFlowImageRef:       "The repository in the registry, without tag or digest, such as registry.example.com/team/app.",
		tInfoFlowEnvVar:         "The variable in the env file that carries the tag of the image.",
		tInfoFlowServices:       "The compose services that run the image, separated by commas.",
		tInfoFlowAddImage:       "Adds a further image to the project.",
		tInfoFlowDir:            "The absolute directory of the project on this host; tab completes.",
		tInfoFlowEnv:            "The absolute path of the env file an update writes the new tags to.",
		tInfoFlowCompose:        "An absolute compose file, passed in this order; empty uses the default search of Docker Compose.",
		tInfoFlowComposeProject: "The compose project name; empty uses the default of Docker Compose.",
		tInfoFlowPoll:           "How often to look for new versions, such as 30m or 6h; empty uses the setting of the service.",
		tInfoFlowMode:           "manual offers new versions; auto also applies them within the max update.",
		tInfoFlowMaxUpdate:      "The largest step an automatic update takes: patch, minor or major.",
		tInfoFlowMajorSteps:     "Whether an update over several major versions goes directly or one major version at a time.",
		tInfoFlowTagFilter:      "An RE2 regular expression a tag must match to count as a version.",
		tInfoFlowChangelogRepo:  "The GitHub repository whose releases are the release notes, as owner/repo.",
		tInfoFlowChangelogCred:  "A secret credential with a GitHub token for reading the releases, picked from the list or created there.",
		tInfoFlowStepType:       "The building block the step runs.",
		tInfoFlowAssign:         "KEY=value assignments for the env file, separated by commas.",
		tInfoFlowStepServices:   "The compose services of the step, separated by commas; empty means all.",
		tInfoFlowTag:            "The tag to check out, usually {version}.",
		tInfoFlowRun:            "A shell command line; placeholders are quoted automatically.",
		tInfoFlowAddStep:        "Adds a further step at the end.",
		tInfoFlowBackup:         "What a backup step saves: a Docker volume, a command of its own, or nothing.",
		tInfoFlowVolume:         "The Docker volume a backup copies.",
		tInfoFlowBackupCommand:  "The shell command that writes the backup.",
		tInfoFlowKeep:           "How many backups are kept.",
		tInfoFlowRestore:        "The shell command that restores a backup.",
		tInfoFlowURL:            "The address the check asks after the update.",
		tInfoFlowStatus:         "The HTTP status the check expects; empty means 200.",
		tInfoFlowContains:       "Text the answer must contain.",
		tInfoFlowCheckServices:  "The compose services that must run after the update, separated by commas.",
		tInfoFlowContainers:     "Further containers that must run, separated by commas.",
		tInfoFlowAddCheck:       "Adds a check that runs after the update; a failed check rolls the update back.",
		tInfoFlowChannels:       "The notification channels that receive the events; space switches one on or off.",
		tInfoFlowEvents:         "The events that are sent to the channels.",
		tInfoFlowHookKind:       "Lets a push from outside trigger a check, signed the way of the chosen host.",
		tInfoFlowHookSecret:     "The secret credential that signs the webhook, picked from the list or created there.",
		tInfoFlowAdvanced:       "Rows that are rarely needed; they open by themselves where a value or a problem stands.",
		tInfoFlowSummary:        "The project as it will be saved; the service checks it before it stores it.",

		footerID("g/n new"):                  "g/n new",
		footerID("enter choose"):             "enter choose",
		footerID("left/right switch"):        "left/right switch",
		footerID("space toggle"):             "space toggle",
		footerID("enter add"):                "enter add",
		footerID("enter close"):              "enter close",
		footerID("ctrl+d remove"):            "ctrl+d remove",
		footerID("ctrl+n/ctrl+p suggestion"): "ctrl+n/ctrl+p suggestion",
		footerID("{save} next"):              "{save} next",
		footerID("up/down field"):            "up/down field",
		wordID(flowCustom):                   "custom steps",
		wordID(flowNone):                     "none",
		wordID(flowVolume):                   "volume",
		wordID(flowCommand):                  "command",
	})
	maps.Copy(textDE, map[textID]string{
		tFlowTitleNew:    "Neues Projekt",
		tFlowTitleEdit:   "Projekt bearbeiten",
		tFlowStepStart:   "Start",
		tFlowStepSource:  "Quelle",
		tFlowStepTarget:  "Ziel",
		tFlowStepSteps:   "Schritte",
		tFlowStepRules:   "Versionsregel",
		tFlowStepChecks:  "Prüfungen",
		tFlowStepNotify:  "Benachrichtigung",
		tFlowStepSummary: "Zusammenfassung",

		tFlowLblName:           "Name",
		tFlowLblScenario:       "Szenario",
		tFlowLblSource:         "Quelle",
		tFlowLblGitURL:         "Repository",
		tFlowLblCredential:     "Credential",
		tFlowLblImageName:      "Name",
		tFlowLblImageRef:       "Referenz",
		tFlowLblEnvVar:         "Env-Variable",
		tFlowLblServices:       "Services",
		tFlowLblDir:            "Verzeichnis",
		tFlowLblEnv:            "Env-Datei",
		tFlowLblCompose:        "Compose-Datei",
		tFlowLblComposeProject: "Compose-Projekt",
		tFlowLblPoll:           "Abfrageintervall",
		tFlowLblMode:           "Modus",
		tFlowLblMaxUpdate:      "Max. Update",
		tFlowLblMajorSteps:     "Major-Schritte",
		tFlowLblTagFilter:      "Tag-Filter",
		tFlowLblChangelogRepo:  "Changelog-Repo",
		tFlowLblChangelogCred:  "Changelog-Credential",
		tFlowLblStepType:       "Typ",
		tFlowLblAssign:         "Zuweisungen",
		tFlowLblTag:            "Tag",
		tFlowLblRun:            "Befehl",
		tFlowLblBackup:         "Backup",
		tFlowLblVolume:         "Volume",
		tFlowLblCommand:        "Befehl",
		tFlowLblKeep:           "Behalten",
		tFlowLblRestore:        "Wiederherstellen",
		tFlowLblURL:            "URL",
		tFlowLblStatus:         "Status",
		tFlowLblContains:       "Enthält",
		tFlowLblContainers:     "Container",
		tFlowLblChannels:       "Kanäle",
		tFlowLblEvents:         "Ereignisse",
		tFlowLblHookKind:       "Webhook",
		tFlowLblHookSecret:     "Secret-Credential",

		tFlowHeadImage:    "Image %d",
		tFlowHeadStep:     "Schritt %d",
		tFlowHeadBackup:   "Backup",
		tFlowHeadCheck:    "Prüfung %d · %s",
		tFlowAddImage:     "Image hinzufügen",
		tFlowAddStep:      "Schritt hinzufügen",
		tFlowAddHTTP:      "HTTP-Prüfung hinzufügen",
		tFlowAddContainer: "Container-Prüfung hinzufügen",
		tFlowAdvanced:     "Advanced",
		tFlowSumImage:     "Image %s",
		tFlowSumCompose:   "Compose-Dateien",

		tFlowDefault:      "%s (Standard)",
		tFlowRequired:     "Pflichtfeld.",
		tFlowAbsolute:     "Einen absoluten Pfad angeben.",
		tFlowNumber:       "Eine ganze Zahl angeben.",
		tFlowDuration:     "Eine Dauer wie 30m oder 6h angeben.",
		tFlowFix:          "Zuerst die markierten Zeilen korrigieren.",
		tFlowRefused:      "Der Dienst fand %d Problem(e); nichts wurde gespeichert.",
		tFlowExists:       "Ein Projekt mit diesem Namen gibt es schon.",
		tFlowSaved:        "Projekt %s gespeichert und freigegeben.",
		tFlowSavedPending: "Projekt %s gespeichert; es wartet auf Freigabe.",
		tFlowProblems:     "Probleme",
		tFlowProblemsIn:   "Probleme in: %s",
		tBusyFlowSave:     "%s wird gespeichert",

		tInfoFlowName:           "Der Name der Projektdatei: Kleinbuchstaben, Ziffern und Bindestriche.",
		tInfoFlowScenario:       "Die Schritte eines Updates: eine eingebaute Vorlage oder eigene Schritte des Projekts.",
		tInfoFlowSource:         "Woher neue Versionen kommen: Container-Images oder die Tags eines Git-Repositorys.",
		tInfoFlowGitURL:         "Das Repository, dessen Tags die Versionen sind, als https://, ssh:// oder user@host:path, ohne Passwort.",
		tInfoFlowCredential:     "Ein Credential im Tresor für Registry oder Repository, aus der Liste gewählt oder dort angelegt; keins für öffentliche Quellen.",
		tInfoFlowImageName:      "Ein kurzer Name des Images im Projekt.",
		tInfoFlowImageRef:       "Das Repository in der Registry ohne Tag oder Digest, etwa registry.example.com/team/app.",
		tInfoFlowEnvVar:         "Die Variable der Env-Datei, die den Tag des Images trägt.",
		tInfoFlowServices:       "Die Compose-Services, die das Image ausführen, durch Kommas getrennt.",
		tInfoFlowAddImage:       "Fügt dem Projekt ein weiteres Image hinzu.",
		tInfoFlowDir:            "Das absolute Verzeichnis des Projekts auf diesem Host; tab vervollständigt.",
		tInfoFlowEnv:            "Der absolute Pfad der Env-Datei, in die ein Update die neuen Tags schreibt.",
		tInfoFlowCompose:        "Eine absolute Compose-Datei, in dieser Reihenfolge übergeben; leer nutzt die Standardsuche von Docker Compose.",
		tInfoFlowComposeProject: "Der Name des Compose-Projekts; leer nutzt den Standard von Docker Compose.",
		tInfoFlowPoll:           "Wie oft nach neuen Versionen gesucht wird, etwa 30m oder 6h; leer nutzt die Einstellung des Dienstes.",
		tInfoFlowMode:           "manual bietet neue Versionen an; auto spielt sie bis zum max. Update auch ein.",
		tInfoFlowMaxUpdate:      "Der größte Schritt eines automatischen Updates: patch, minor oder major.",
		tInfoFlowMajorSteps:     "Ob ein Update über mehrere Major-Versionen direkt geht oder eine Major-Version nach der anderen.",
		tInfoFlowTagFilter:      "Ein regulärer Ausdruck (RE2), zu dem ein Tag passen muss, um als Version zu gelten.",
		tInfoFlowChangelogRepo:  "Das GitHub-Repository, dessen Releases die Release Notes sind, als owner/repo.",
		tInfoFlowChangelogCred:  "Ein Secret-Credential mit GitHub-Token zum Lesen der Releases, aus der Liste gewählt oder dort angelegt.",
		tInfoFlowStepType:       "Der Baustein, den der Schritt ausführt.",
		tInfoFlowAssign:         "KEY=value-Zuweisungen für die Env-Datei, durch Kommas getrennt.",
		tInfoFlowStepServices:   "Die Compose-Services des Schritts, durch Kommas getrennt; leer heißt alle.",
		tInfoFlowTag:            "Der auszucheckende Tag, meist {version}.",
		tInfoFlowRun:            "Eine Shell-Befehlszeile; Platzhalter werden automatisch gequotet.",
		tInfoFlowAddStep:        "Fügt am Ende einen weiteren Schritt hinzu.",
		tInfoFlowBackup:         "Was ein Backup-Schritt sichert: ein Docker-Volume, ein eigener Befehl oder nichts.",
		tInfoFlowVolume:         "Das Docker-Volume, das ein Backup kopiert.",
		tInfoFlowBackupCommand:  "Der Shell-Befehl, der das Backup schreibt.",
		tInfoFlowKeep:           "Wie viele Backups behalten werden.",
		tInfoFlowRestore:        "Der Shell-Befehl, der ein Backup wiederherstellt.",
		tInfoFlowURL:            "Die Adresse, die die Prüfung nach dem Update abfragt.",
		tInfoFlowStatus:         "Der erwartete HTTP-Status; leer heißt 200.",
		tInfoFlowContains:       "Text, den die Antwort enthalten muss.",
		tInfoFlowCheckServices:  "Die Compose-Services, die nach dem Update laufen müssen, durch Kommas getrennt.",
		tInfoFlowContainers:     "Weitere Container, die laufen müssen, durch Kommas getrennt.",
		tInfoFlowAddCheck:       "Fügt eine Prüfung nach dem Update hinzu; scheitert sie, wird das Update zurückgerollt.",
		tInfoFlowChannels:       "Die Benachrichtigungskanäle, die die Ereignisse erhalten; space schaltet einen um.",
		tInfoFlowEvents:         "Die Ereignisse, die an die Kanäle gehen.",
		tInfoFlowHookKind:       "Lässt einen Push von außen eine Prüfung auslösen, signiert nach Art des gewählten Hosts.",
		tInfoFlowHookSecret:     "Das Secret-Credential, das den Webhook signiert, aus der Liste gewählt oder dort angelegt.",
		tInfoFlowAdvanced:       "Selten nötige Zeilen; sie öffnen sich von selbst, wo ein Wert oder ein Problem steht.",
		tInfoFlowSummary:        "Das Projekt, wie es gespeichert wird; der Dienst prüft es, bevor er es ablegt.",

		footerID("g/n new"):                  "g/n neu",
		footerID("enter choose"):             "enter wählen",
		footerID("left/right switch"):        "left/right wechseln",
		footerID("space toggle"):             "space umschalten",
		footerID("enter add"):                "enter hinzufügen",
		footerID("enter close"):              "enter schließen",
		footerID("ctrl+d remove"):            "ctrl+d entfernen",
		footerID("ctrl+n/ctrl+p suggestion"): "ctrl+n/ctrl+p Vorschlag",
		footerID("{save} next"):              "{save} weiter",
		footerID("up/down field"):            "up/down Feld",
		wordID(flowCustom):                   "eigene Schritte",
		wordID(flowNone):                     "keins",
		wordID(flowVolume):                   "Volume",
		wordID(flowCommand):                  "Befehl",
	})
}
