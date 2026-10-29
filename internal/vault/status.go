package vault

// Status reports what a status display shows; no secret value ever appears in it.
type Status struct {
	State State
	Dir   string
	// Entries is the number of entries the vault holds: 0 for a vault that does not exist, or -1 when it is
	// locked, so the count cannot be known without the key.
	Entries int
	// Pending is the number of entries waiting in pending, added while the vault was locked.
	Pending int
}

// Status reports the vault's state without asking for a passphrase.
func (v *Vault) Status() (Status, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	state, err := v.state()
	if err != nil {
		return Status{}, err
	}
	st := Status{State: state, Dir: v.dir}
	switch state {
	case StateAbsent:
		return st, nil
	case StateLocked:
		st.Entries = -1
	case StateUnlocked:
		st.Entries = v.doc.live()
	}
	names, err := v.pendingFiles()
	if err != nil {
		return Status{}, err
	}
	st.Pending = len(names)
	return st, nil
}
