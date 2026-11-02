package tui

import "maps"

// The texts of setting up the access an adoption lacks.
const (
	tPrjAccessOffer   textID = "PrjAccessOffer"
	tPrjAccessLeft    textID = "PrjAccessLeft"
	tPrjAccessDone    textID = "PrjAccessDone"
	tPrjAccessNothing textID = "PrjAccessNothing"
	tPrjAccessTitle   textID = "PrjAccessTitle"
	tPrjAccessStack   textID = "PrjAccessStack"
	tPrjAccessParents textID = "PrjAccessParents"
	tPrjAccessOthers  textID = "PrjAccessOthers"
	tPrjAccessRefused textID = "PrjAccessRefused"
	tPrjAccessFailed  textID = "PrjAccessFailed"
)

func init() {
	maps.Copy(textEN, map[textID]string{
		tPrjAccessOffer:   "Access is missing. Press y to set it up for the service.",
		tPrjAccessLeft:    "The setup covers the stack folder and the folders above it only; what is listed here stays missing.",
		tPrjAccessDone:    "Access was set up. You can undo this under Host > Changes.",
		tPrjAccessNothing: "There is nothing to set up for the stack folder: it is set up already.",
		tPrjAccessTitle:   "Set up access?",
		tPrjAccessStack:   "%s is set up for the service: root:sysmin, compose files read, env file write, data folders untouched.",
		tPrjAccessParents: "The service may pass, not list, these parent folders:",
		tPrjAccessOthers:  "Other users except root have no access to the stack folder and its files afterwards.",
		tPrjAccessRefused: "Access cannot be set up: %s",
		tPrjAccessFailed:  "Setting up access failed: %s",

		footerID("y set up access"): "y set up access",
	})
	maps.Copy(textDE, map[textID]string{
		tPrjAccessOffer:   "Zugriff fehlt. Mit y für den Dienst einrichten.",
		tPrjAccessLeft:    "Die Einrichtung deckt nur den Stack-Ordner und die Ordner darüber ab; was hier aufgeführt ist, fehlt weiter.",
		tPrjAccessDone:    "Zugriff eingerichtet. Du kannst das unter Host > Änderungen rückgängig machen.",
		tPrjAccessNothing: "Für den Stack-Ordner ist nichts einzurichten: Er ist schon eingerichtet.",
		tPrjAccessTitle:   "Zugriff einrichten?",
		tPrjAccessStack:   "%s wird für den Dienst eingerichtet: root:sysmin, Compose-Dateien lesen, Env-Datei schreiben, Datenordner unberührt.",
		tPrjAccessParents: "Der Dienst darf diese übergeordneten Ordner durchqueren, nicht auflisten:",
		tPrjAccessOthers:  "Andere Benutzer außer root haben danach keinen Zugriff auf den Stack-Ordner und seine Dateien.",
		tPrjAccessRefused: "Zugriff lässt sich nicht einrichten: %s",
		tPrjAccessFailed:  "Das Einrichten des Zugriffs ist fehlgeschlagen: %s",

		footerID("y set up access"): "y Zugriff einrichten",
	})
}
