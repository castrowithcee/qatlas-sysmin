package tui

// The texts of the forms that create and change host users.
const (
	tHostNewTitle         textID = "HostNewTitle"
	tHostNewHint          textID = "HostNewHint"
	tHostNewServiceHint   textID = "HostNewServiceHint"
	tHostFormKind         textID = "HostFormKind"
	tHostFormHome         textID = "HostFormHome"
	tHostKindPerson       textID = "HostKindPerson"
	tHostKindService      textID = "HostKindService"
	tHostServiceKeepFiles textID = "HostServiceKeepFiles"
	tHostEditTitle        textID = "HostEditTitle"
	tHostFormPassword     textID = "HostFormPassword"
	tHostFormPassword2    textID = "HostFormPassword2"
	tHostFormFurther      textID = "HostFormFurther"
	tHostFormKey          textID = "HostFormKey"
	tHostFormKeyOf        textID = "HostFormKeyOf"
	tHostFormWhat         textID = "HostFormWhat"
	tHostFormAddTo        textID = "HostFormAddTo"
	tHostFormRemoveFrom   textID = "HostFormRemoveFrom"
	tHostFormMust         textID = "HostFormMust"
	tHostFormNoGroups     textID = "HostFormNoGroups"
	tHostFormTicked       textID = "HostFormTicked"
	tHostFormNameEmpty    textID = "HostFormNameEmpty"
	tHostFormNameTaken    textID = "HostFormNameTaken"
	tHostFormPwEmpty      textID = "HostFormPwEmpty"
	tHostFormPwBad        textID = "HostFormPwBad"
	tHostFormPwMismatch   textID = "HostFormPwMismatch"
	tHostFormKeyBad       textID = "HostFormKeyBad"
	tHostFormKeyDup       textID = "HostFormKeyDup"
	tHostFormKeyMax       textID = "HostFormKeyMax"
	tHostFormKeys         textID = "HostFormKeys"
	tHostFormTickOne      textID = "HostFormTickOne"
	tHostFormKeysUnread   textID = "HostFormKeysUnread"
	tHostChgAddGroups     textID = "HostChgAddGroups"
	tHostChgRemoveGroups  textID = "HostChgRemoveGroups"
	tHostChgAdminOn       textID = "HostChgAdminOn"
	tHostChgAdminOff      textID = "HostChgAdminOff"
	tHostChgDockerOn      textID = "HostChgDockerOn"
	tHostChgDockerOff     textID = "HostChgDockerOff"
	tHostChgPassword      textID = "HostChgPassword"
	tHostChgAddKey        textID = "HostChgAddKey"
	tHostChgRemoveKey     textID = "HostChgRemoveKey"
	tHostChgEnable        textID = "HostChgEnable"
	tHostChgEnableHint    textID = "HostChgEnableHint"
	tHostChgKeyHint       textID = "HostChgKeyHint"
	tHostChgLock          textID = "HostChgLock"
	tHostChgUnlock        textID = "HostChgUnlock"
	tHostLockStays        textID = "HostLockStays"
	tHostSelfLock         textID = "HostSelfLock"
	tHostSelfDelete       textID = "HostSelfDelete"
	tHostDeleteTitle      textID = "HostDeleteTitle"
	tHostHomeKeep         textID = "HostHomeKeep"
	tHostHomeDelete       textID = "HostHomeDelete"
	tHostDeleteHint       textID = "HostDeleteHint"
	tHostDeleteHomeHint   textID = "HostDeleteHomeHint"
	tHostDeleteKeeps      textID = "HostDeleteKeeps"
	tHostDeleteFinal      textID = "HostDeleteFinal"
	tHostHomeSystem       textID = "HostHomeSystem"
	tHostHomeHolds        textID = "HostHomeHolds"
	tHostHomeLink         textID = "HostHomeLink"
	tHostHomeNotDir       textID = "HostHomeNotDir"
	tHostHomeOwner        textID = "HostHomeOwner"
	tHostHomeUnread       textID = "HostHomeUnread"
	tHostSelfAdmin        textID = "HostSelfAdmin"
	tHostLastAdmin        textID = "HostLastAdmin"
	tHostRiskRoot         textID = "HostRiskRoot"
	tHostRiskAccount      textID = "HostRiskAccount"
	tHostRiskGroup        textID = "HostRiskGroup"
)

// hostUserTexts are the texts of the host user forms, in English and German.
func hostUserTexts() (en, de map[textID]string) {
	en = map[textID]string{
		tHostNewTitle:         "New user",
		tHostNewHint:          "The user must change the initial password at the first login. Enter in the SSH key field adds the key.",
		tHostNewServiceHint:   "A service account has no login: no password, no shell, no SSH keys. Use it for a service or container with an identity of its own.",
		tHostFormKind:         "Kind",
		tHostFormHome:         "Home",
		tHostKindPerson:       "Person",
		tHostKindService:      "Service account",
		tHostServiceKeepFiles: "Undoing it deletes the account and a home qsysmin created; files of the account outside its home stay.",
		tHostEditTitle:        "Change %s",
		tHostFormPassword:     "Password",
		tHostFormPassword2:    "Repeat password",
		tHostFormFurther:      "Further groups",
		tHostFormKey:          "SSH key",
		tHostFormKeyOf:        "Key",
		tHostFormWhat:         "Change",
		tHostFormAddTo:        "Add to groups",
		tHostFormRemoveFrom:   "Remove from",
		tHostFormMust:         "Change at login",
		tHostFormNoGroups:     "no group to choose",
		tHostFormTicked:       "%d of %d chosen",
		tHostFormNameEmpty:    "Enter a name.",
		tHostFormNameTaken:    "This user exists already.",
		tHostFormPwEmpty:      "Enter a password.",
		tHostFormPwBad:        "The password may have up to %d characters, without control characters.",
		tHostFormPwMismatch:   "The two passwords differ.",
		tHostFormKeyBad:       "Not a public key in authorized_keys format (one line, without options).",
		tHostFormKeyDup:       "This key is in the list already.",
		tHostFormKeyMax:       "At most %d keys.",
		tHostFormKeys:         "SSH keys (%d): %s",
		tHostFormTickOne:      "Choose at least one group.",
		tHostFormKeysUnread:   "The SSH keys are not read yet. Saving asks to read them as root first.",
		tHostChgAddGroups:     "Add to groups",
		tHostChgRemoveGroups:  "Remove from groups",
		tHostChgAdminOn:       "Make admin",
		tHostChgAdminOff:      "Take admin away",
		tHostChgDockerOn:      "Allow Docker",
		tHostChgDockerOff:     "Withdraw Docker",
		tHostChgPassword:      "Set password",
		tHostChgAddKey:        "Add SSH key",
		tHostChgRemoveKey:     "Remove SSH key",
		tHostChgEnable:        "Enable for qsysmin",
		tHostChgEnableHint:    "This admin is not in the group qsysmin yet and cannot use qsysmin. Saving adds the membership.",
		tHostChgKeyHint:       "Paste one public key line (authorized_keys format, without options).",
		tHostChgLock:          "Lock",
		tHostChgUnlock:        "Unlock",
		tHostLockStays:        "Running sessions stay open until they end.",
		tHostSelfLock:         "You cannot lock yourself.",
		tHostSelfDelete:       "You cannot delete yourself.",
		tHostDeleteTitle:      "Delete %s",
		tHostHomeKeep:         "Keep",
		tHostHomeDelete:       "Delete too",
		tHostDeleteHint:       "The account goes; its home and files stay, and the delete can be undone. Lock is the normal way to stop someone.",
		tHostDeleteHomeHint:   "The home and the mail spool are deleted with the account. This cannot be undone.",
		tHostDeleteKeeps:      "A user with running processes is not deleted.",
		tHostDeleteFinal:      "This cannot be undone: the home %s and the mail spool are deleted with all their files.",
		tHostHomeSystem:       "%s is a system path.",
		tHostHomeHolds:        "%s holds the home of %s.",
		tHostHomeLink:         "%s is a link.",
		tHostHomeNotDir:       "%s is not a directory.",
		tHostHomeOwner:        "%s belongs to %s.",
		tHostHomeUnread:       "What else %s holds could not be checked: %s.",
		tHostSelfAdmin:        "You cannot take admin rights from yourself.",
		tHostLastAdmin:        "%s is the last admin with qsysmin access.",
		tHostRiskRoot:         "root is the account with all rights on this host.",
		tHostRiskAccount:      "Packages or services may depend on this system account.",
		tHostRiskGroup:        "Packages or services may depend on this system group.",
	}
	de = map[textID]string{
		tHostNewTitle:         "Neuer Benutzer",
		tHostNewHint:          "Der Benutzer muss das Startpasswort beim ersten Login ändern. Enter im Feld SSH-Schlüssel fügt den Schlüssel hinzu.",
		tHostNewServiceHint:   "Ein Dienstkonto hat keinen Login: kein Passwort, keine Shell, keine SSH-Schlüssel. Es dient einem Dienst oder Container mit eigener Identität.",
		tHostFormKind:         "Art",
		tHostFormHome:         "Home",
		tHostKindPerson:       "Person",
		tHostKindService:      "Dienstkonto",
		tHostServiceKeepFiles: "Rückgängig löscht das Konto und ein von qsysmin angelegtes Home; Dateien des Kontos außerhalb seines Homes bleiben.",
		tHostEditTitle:        "%s ändern",
		tHostFormPassword:     "Passwort",
		tHostFormPassword2:    "Passwort wiederholen",
		tHostFormFurther:      "Weitere Gruppen",
		tHostFormKey:          "SSH-Schlüssel",
		tHostFormKeyOf:        "Schlüssel",
		tHostFormWhat:         "Änderung",
		tHostFormAddTo:        "Zu Gruppen hinzufügen",
		tHostFormRemoveFrom:   "Entfernen aus",
		tHostFormMust:         "Beim Login ändern",
		tHostFormNoGroups:     "keine Gruppe zur Auswahl",
		tHostFormTicked:       "%d von %d gewählt",
		tHostFormNameEmpty:    "Gib einen Namen ein.",
		tHostFormNameTaken:    "Diesen Benutzer gibt es schon.",
		tHostFormPwEmpty:      "Gib ein Passwort ein.",
		tHostFormPwBad:        "Das Passwort darf bis zu %d Zeichen haben, ohne Steuerzeichen.",
		tHostFormPwMismatch:   "Die beiden Passwörter sind verschieden.",
		tHostFormKeyBad:       "Kein öffentlicher Schlüssel im Format authorized_keys (eine Zeile, ohne Optionen).",
		tHostFormKeyDup:       "Dieser Schlüssel steht schon in der Liste.",
		tHostFormKeyMax:       "Höchstens %d Schlüssel.",
		tHostFormKeys:         "SSH-Schlüssel (%d): %s",
		tHostFormTickOne:      "Wähle mindestens eine Gruppe.",
		tHostFormKeysUnread:   "Die SSH-Schlüssel sind noch nicht gelesen. Speichern fragt zuerst, ob sie als root gelesen werden sollen.",
		tHostChgAddGroups:     "Zu Gruppen hinzufügen",
		tHostChgRemoveGroups:  "Aus Gruppen entfernen",
		tHostChgAdminOn:       "Zum Admin machen",
		tHostChgAdminOff:      "Admin entziehen",
		tHostChgDockerOn:      "Docker erlauben",
		tHostChgDockerOff:     "Docker entziehen",
		tHostChgPassword:      "Passwort setzen",
		tHostChgAddKey:        "SSH-Schlüssel hinzufügen",
		tHostChgRemoveKey:     "SSH-Schlüssel entfernen",
		tHostChgEnable:        "Für qsysmin freischalten",
		tHostChgEnableHint:    "Dieser Admin ist noch nicht in der Gruppe qsysmin und kann qsysmin nicht nutzen. Speichern fügt die Mitgliedschaft hinzu.",
		tHostChgKeyHint:       "Füge eine Zeile mit einem öffentlichen Schlüssel ein (Format authorized_keys, ohne Optionen).",
		tHostChgLock:          "Sperren",
		tHostChgUnlock:        "Entsperren",
		tHostLockStays:        "Laufende Sitzungen bleiben offen, bis sie enden.",
		tHostSelfLock:         "Du kannst dich nicht selbst sperren.",
		tHostSelfDelete:       "Du kannst dich nicht selbst löschen.",
		tHostDeleteTitle:      "%s löschen",
		tHostHomeKeep:         "Behalten",
		tHostHomeDelete:       "Auch löschen",
		tHostDeleteHint:       "Das Konto geht; Home und Dateien bleiben, und das Löschen lässt sich rückgängig machen. Sperren ist der Normalfall, wenn jemand geht.",
		tHostDeleteHomeHint:   "Home und Mail-Spool werden mit dem Konto gelöscht. Das lässt sich nicht rückgängig machen.",
		tHostDeleteKeeps:      "Ein Benutzer mit laufenden Prozessen wird nicht gelöscht.",
		tHostDeleteFinal:      "Das lässt sich nicht rückgängig machen: Das Home %s und der Mail-Spool werden mit allen Dateien gelöscht.",
		tHostHomeSystem:       "%s ist ein Systempfad.",
		tHostHomeHolds:        "%s enthält das Home von %s.",
		tHostHomeLink:         "%s ist ein Link.",
		tHostHomeNotDir:       "%s ist kein Verzeichnis.",
		tHostHomeOwner:        "%s gehört %s.",
		tHostHomeUnread:       "Was %s sonst enthält, ließ sich nicht prüfen: %s.",
		tHostSelfAdmin:        "Du kannst dir die Adminrechte nicht selbst nehmen.",
		tHostLastAdmin:        "%s ist der letzte Admin mit qsysmin-Zugang.",
		tHostRiskRoot:         "root ist das Konto mit allen Rechten auf diesem Host.",
		tHostRiskAccount:      "Pakete oder Dienste können von diesem Systemkonto abhängen.",
		tHostRiskGroup:        "Pakete oder Dienste können von dieser Systemgruppe abhängen.",
	}
	return en, de
}
