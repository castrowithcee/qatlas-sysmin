# Qatlas Sysmin

Qatlas Sysmin, binary `qsysmin`, is a Go tool that runs as a service on a Linux host and helps look after that host. It has the areas
Overview, Host, Activity and Settings, in a terminal interface and on the command line.

- Host reads users, groups and folder permissions. Changes run through `sudo qsysmin host ...` after a
  confirmation, are recorded in a journal with the previous state, and can be undone.
- Activity shows a log kept as a hash chain without a signature.
- Admins (members of the group `sudo`) reach the service through its socket.

The project is at an early stage of development and is not ready for use.

## Switching from sysmin

The tool was called `sysmin` before. An installation under the old name cannot be updated: run
`sudo sysmin uninstall --purge` first, which also removes its data, then run the installer for `qsysmin`.
The installer refuses to continue while the old installation is present.

## License

AGPL-3.0, see [LICENSE](LICENSE).
