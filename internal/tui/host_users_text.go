package tui

import "maps"

// The texts of the forms that create and change host users.
const (
	tHostNewTitle        textID = "HostNewTitle"
	tHostNewHint         textID = "HostNewHint"
	tHostEditTitle       textID = "HostEditTitle"
	tHostFormPassword    textID = "HostFormPassword"
	tHostFormPassword2   textID = "HostFormPassword2"
	tHostFormFurther     textID = "HostFormFurther"
	tHostFormKey         textID = "HostFormKey"
	tHostFormKeyOf       textID = "HostFormKeyOf"
	tHostFormWhat        textID = "HostFormWhat"
	tHostFormAddTo       textID = "HostFormAddTo"
	tHostFormRemoveFrom  textID = "HostFormRemoveFrom"
	tHostFormMust        textID = "HostFormMust"
	tHostFormNoGroups    textID = "HostFormNoGroups"
	tHostFormTicked      textID = "HostFormTicked"
	tHostFormNameEmpty   textID = "HostFormNameEmpty"
	tHostFormNameTaken   textID = "HostFormNameTaken"
	tHostFormPwEmpty     textID = "HostFormPwEmpty"
	tHostFormPwBad       textID = "HostFormPwBad"
	tHostFormPwMismatch  textID = "HostFormPwMismatch"
	tHostFormKeyBad      textID = "HostFormKeyBad"
	tHostFormKeyDup      textID = "HostFormKeyDup"
	tHostFormKeyMax      textID = "HostFormKeyMax"
	tHostFormKeys        textID = "HostFormKeys"
	tHostFormTickOne     textID = "HostFormTickOne"
	tHostFormKeysUnread  textID = "HostFormKeysUnread"
	tHostChgAddGroups    textID = "HostChgAddGroups"
	tHostChgRemoveGroups textID = "HostChgRemoveGroups"
	tHostChgAdminOn      textID = "HostChgAdminOn"
	tHostChgAdminOff     textID = "HostChgAdminOff"
	tHostChgDockerOn     textID = "HostChgDockerOn"
	tHostChgDockerOff    textID = "HostChgDockerOff"
	tHostChgPassword     textID = "HostChgPassword"
	tHostChgAddKey       textID = "HostChgAddKey"
	tHostChgRemoveKey    textID = "HostChgRemoveKey"
	tHostChgEnable       textID = "HostChgEnable"
	tHostChgEnableHint   textID = "HostChgEnableHint"
	tHostChgKeyHint      textID = "HostChgKeyHint"
	tHostSelfAdmin       textID = "HostSelfAdmin"
	tHostLastAdmin       textID = "HostLastAdmin"
	tHostRiskRoot        textID = "HostRiskRoot"
	tHostRiskAccount     textID = "HostRiskAccount"
	tHostRiskGroup       textID = "HostRiskGroup"
)

func init() {
	maps.Copy(textEN, map[textID]string{
		tHostNewTitle:        "New user",
		tHostNewHint:         "The user must change the initial password at the first login. Enter in the SSH key field adds the key.",
		tHostEditTitle:       "Change %s",
		tHostFormPassword:    "Password",
		tHostFormPassword2:   "Repeat password",
		tHostFormFurther:     "Further groups",
		tHostFormKey:         "SSH key",
		tHostFormKeyOf:       "Key",
		tHostFormWhat:        "Change",
		tHostFormAddTo:       "Add to groups",
		tHostFormRemoveFrom:  "Remove from",
		tHostFormMust:        "Change at login",
		tHostFormNoGroups:    "no group to choose",
		tHostFormTicked:      "%d of %d chosen",
		tHostFormNameEmpty:   "Enter a name.",
		tHostFormNameTaken:   "This user exists already.",
		tHostFormPwEmpty:     "Enter a password.",
		tHostFormPwBad:       "The password may have up to %d characters, without control characters.",
		tHostFormPwMismatch:  "The two passwords differ.",
		tHostFormKeyBad:      "Not a public key in authorized_keys format (one line, without options).",
		tHostFormKeyDup:      "This key is in the list already.",
		tHostFormKeyMax:      "At most %d keys.",
		tHostFormKeys:        "SSH keys (%d): %s",
		tHostFormTickOne:     "Choose at least one group.",
		tHostFormKeysUnread:  "The SSH keys are not read yet. Saving asks to read them as root first.",
		tHostChgAddGroups:    "Add to groups",
		tHostChgRemoveGroups: "Remove from groups",
		tHostChgAdminOn:      "Make admin",
		tHostChgAdminOff:     "Take admin away",
		tHostChgDockerOn:     "Allow Docker",
		tHostChgDockerOff:    "Withdraw Docker",
		tHostChgPassword:     "Set password",
		tHostChgAddKey:       "Add SSH key",
		tHostChgRemoveKey:    "Remove SSH key",
		tHostChgEnable:       "Enable for sysmin",
		tHostChgEnableHint:   "This admin is not in the group sysmin yet and cannot use sysmin. Saving adds the membership.",
		tHostChgKeyHint:      "Paste one public key line (authorized_keys format, without options).",
		tHostSelfAdmin:       "You cannot take admin rights from yourself.",
		tHostLastAdmin:       "%s is the last admin with sysmin access.",
		tHostRiskRoot:        "root is the account with all rights on this host.",
		tHostRiskAccount:     "Packages or services may depend on this system account.",
		tHostRiskGroup:       "Packages or services may depend on this system group.",
	})
	maps.Copy(textDE, map[textID]string{
		tHostNewTitle:        "Neuer Benutzer",
		tHostNewHint:         "Der Benutzer muss das Startpasswort beim ersten Login ändern. Enter im Feld SSH-Schlüssel fügt den Schlüssel hinzu.",
		tHostEditTitle:       "%s ändern",
		tHostFormPassword:    "Passwort",
		tHostFormPassword2:   "Passwort wiederholen",
		tHostFormFurther:     "Weitere Gruppen",
		tHostFormKey:         "SSH-Schlüssel",
		tHostFormKeyOf:       "Schlüssel",
		tHostFormWhat:        "Änderung",
		tHostFormAddTo:       "Zu Gruppen hinzufügen",
		tHostFormRemoveFrom:  "Entfernen aus",
		tHostFormMust:        "Beim Login ändern",
		tHostFormNoGroups:    "keine Gruppe zur Auswahl",
		tHostFormTicked:      "%d von %d gewählt",
		tHostFormNameEmpty:   "Gib einen Namen ein.",
		tHostFormNameTaken:   "Diesen Benutzer gibt es schon.",
		tHostFormPwEmpty:     "Gib ein Passwort ein.",
		tHostFormPwBad:       "Das Passwort darf bis zu %d Zeichen haben, ohne Steuerzeichen.",
		tHostFormPwMismatch:  "Die beiden Passwörter sind verschieden.",
		tHostFormKeyBad:      "Kein öffentlicher Schlüssel im Format authorized_keys (eine Zeile, ohne Optionen).",
		tHostFormKeyDup:      "Dieser Schlüssel steht schon in der Liste.",
		tHostFormKeyMax:      "Höchstens %d Schlüssel.",
		tHostFormKeys:        "SSH-Schlüssel (%d): %s",
		tHostFormTickOne:     "Wähle mindestens eine Gruppe.",
		tHostFormKeysUnread:  "Die SSH-Schlüssel sind noch nicht gelesen. Speichern fragt zuerst, ob sie als root gelesen werden sollen.",
		tHostChgAddGroups:    "Zu Gruppen hinzufügen",
		tHostChgRemoveGroups: "Aus Gruppen entfernen",
		tHostChgAdminOn:      "Zum Admin machen",
		tHostChgAdminOff:     "Admin entziehen",
		tHostChgDockerOn:     "Docker erlauben",
		tHostChgDockerOff:    "Docker entziehen",
		tHostChgPassword:     "Passwort setzen",
		tHostChgAddKey:       "SSH-Schlüssel hinzufügen",
		tHostChgRemoveKey:    "SSH-Schlüssel entfernen",
		tHostChgEnable:       "Für sysmin freischalten",
		tHostChgEnableHint:   "Dieser Admin ist noch nicht in der Gruppe sysmin und kann sysmin nicht nutzen. Speichern fügt die Mitgliedschaft hinzu.",
		tHostChgKeyHint:      "Füge eine Zeile mit einem öffentlichen Schlüssel ein (Format authorized_keys, ohne Optionen).",
		tHostSelfAdmin:       "Du kannst dir die Adminrechte nicht selbst nehmen.",
		tHostLastAdmin:       "%s ist der letzte Admin mit sysmin-Zugang.",
		tHostRiskRoot:        "root ist das Konto mit allen Rechten auf diesem Host.",
		tHostRiskAccount:     "Pakete oder Dienste können von diesem Systemkonto abhängen.",
		tHostRiskGroup:       "Pakete oder Dienste können von dieser Systemgruppe abhängen.",
	})
}
