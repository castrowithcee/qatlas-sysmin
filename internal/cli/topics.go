package cli

import "github.com/castrowithcee/qatlas-kit/helptopics"

// helpTopics are the guides in the order a newcomer reads them.
var helpTopics = []helptopics.Topic{
	{Name: "qsysmin", Short: "What qsysmin is and what it is for", Text: qsysminTopic},
	{Name: "host-admin", Short: "Read and change users, groups and folder permissions of the host", Text: hostAdminTopic},
}

const qsysminTopic = `qsysmin is a Go tool that runs as a service on a Linux host and helps look after that host. It has the areas Overview, Host, Activity and Settings.

Host reads the local users, groups and folder permissions. Changes run through 'sudo qsysmin host ...' after a confirmation, are recorded in a journal with the previous state, and can be undone. After a change to folder permissions, the containers it affects are watched. Activity shows a log kept as a hash chain without a signature.

Admins, members of the group sudo, reach the running service through its socket. The installation requires the group docker.

A further area, Containers, is a possible direction; nothing is promised for it.

'qsysmin --help' lists the commands; 'qsysmin tui' opens the terminal interface; 'qsysmin host-admin' covers the host commands.`

const hostAdminTopic = `The host commands read the local users, groups and folder permissions. Changes (host apply, host undo) run through sudo after a confirmation, are recorded in a root-only journal with the previous state, and can be undone.

Commands:
  qsysmin host users
  qsysmin host user <name>
  qsysmin host groups
  qsysmin host group <name>
  qsysmin host folder <path>

Only root and members of the group sudo may use them. Everyone else gets one line and exit code 1.

A user or group is a system user or group when its ID lies outside 1000 to 60000, so root and nobody count as system accounts. A user is an admin in the group sudo, a docker user in the group docker, and enabled in the group qsysmin. A group lists the users whose primary group it is as members.

Whether an account is locked or expired and which SSH keys it has are only readable as root. Otherwise they are unknown: absent in JSON and 'unknown' in the other formats. Keys are shown as type, comment and SHA256 fingerprint, never as the key itself.

A folder or file shows owner, group and the level of owner, group and others: nothing, read, or read and write. For a directory read and write both need the right to enter it; write without read counts as nothing. It also shows whether ACLs are set, the special bits setuid, setgid and sticky, and whether the path is protected: /, /etc, /usr, /boot, /proc, /sys and everything below the last five. Links and .. are resolved before that check.`
