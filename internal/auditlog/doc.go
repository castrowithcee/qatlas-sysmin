// Package auditlog is qsysmin's part of the action log: the entry type, the actions it can record, and the
// validation of both. Appending, the hash chain, the lock, retention, torn-line markers, reading and
// checking are the kit's auditlog package; this package fixes how qsysmin uses it.
//
// Entries carry no signature. A signature that an earlier version wrote in the mac member of an entry is
// not checked: the entry counts as signed but unchecked, and the chain over its stored bytes covers it.
package auditlog
