// Package engine deploys the approved state of projects. A run checks the version sources, deploys the
// targets that the version rules allow without a human, all images of the run together in one plan, and
// rolls every image of the run back to its previous version when a step after the backup fails and something
// was changed before or by that step (see config.Effect). A rollback uses the same building blocks with the
// versions swapped, leaves out the backup and never restores data.
// When the rollback fails as well, the project is paused with an error.
//
// Versions above the update cap, and every version under the manual mode, are kept as available updates and
// reported once per version. A paused or pinned project is not checked, and while the vault is locked
// nothing is deployed.
//
// At most one run per project is active; a trigger while it runs is dropped, not queued. The state of every
// project survives a restart; each run writes its own log file and one audit entry.
package engine
