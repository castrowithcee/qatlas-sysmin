// Package auditlog appends one entry per action sysmin takes to a daily file and links the entries with a
// SHA-256 hash chain that spans day boundaries: the first entry of a day carries the hash of the last entry
// of the last day that has one, so the chain keeps going across a day nothing happened on and a day file
// never has to exist just to carry the link.
//
// A day file is DIR/YYYY-MM-DD.jsonl, named by its UTC date; every entry it holds is one JSON object per
// line. Append takes an exclusive lock on the directory for its whole read-compute-write, so concurrent
// writers, goroutines or processes, never race the chain. A failed Append is reported to the caller, never
// written silently.
//
// Append writes each line with a write of its own, so a crash while it writes can leave the last line
// torn. The next Append does not fail on such a line: it writes a KindDamage marker right after it that
// names the SHA-256 of exactly its bytes, never the bytes themselves, and chains from the last line before
// it that parses. Verify accepts a line that does not parse only when such a marker follows it and holds
// as an entry, chain and check value included, and counts it as documented damage; any other such line
// breaks the chain. Only the last line of the newest day file is ever bridged this way.
//
// A logger given the log key (see Key and Logger.WithKey) also signs every entry it writes, markers
// included, with a check value in Entry.MAC; see Key.mac for its exact definition. A logger without one
// leaves MAC empty, which marks an entry unverified: Verify counts such an entry but never treats it as a
// break, so the chain runs on through signed and unsigned entries alike.
package auditlog
