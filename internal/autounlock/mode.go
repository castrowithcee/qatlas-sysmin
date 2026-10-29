// Package autounlock lets the service open the vault after every start without the passphrase. The
// operator binds a copy of the vault's private key to this machine with systemd-creds; the service manager
// hands that credential to the service, which opens the vault with it. Without a binding the vault stays
// locked until someone enters the passphrase. The passphrase is still needed for every change of it.
package autounlock

// Mode is how the vault gets unlocked after a start.
type Mode string

// The unlock modes.
const (
	// ModeManual means the vault stays locked until the passphrase is entered. It is the default.
	ModeManual Mode = "manual"
	// ModeTPM2 means the key copy is sealed to this machine's TPM2.
	ModeTPM2 Mode = "tpm2"
	// ModeHost means the key copy is sealed with the host key kept on this machine's disk.
	ModeHost Mode = "host"
)

// Warning returns a short caution for the modes that unlock without the passphrase, and "" for manual.
func (m Mode) Warning() string {
	switch m {
	case ModeTPM2:
		return "the vault opens on its own on this machine; anyone who can run code as root here can read it"
	case ModeHost:
		return "the vault opens on its own on this machine and its host key is stored on disk, so a copy " +
			"of the disk can open it; anyone who can run code as root here can read it"
	}
	return ""
}

// Automatic reports whether the mode opens the vault without the passphrase.
func (m Mode) Automatic() bool { return m == ModeTPM2 || m == ModeHost }

// valid reports whether m is one of the three known modes.
func (m Mode) valid() bool { return m == ModeManual || m.Automatic() }
