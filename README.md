# sysmin

`sysmin` is a Go tool that runs as a service on a Linux host and helps look after that host. It has the areas
Overview, Host, Activity and Settings, in a terminal interface and on the command line.

- Host reads users, groups and folder permissions. Changes run through `sudo sysmin host ...` after a
  confirmation, are recorded in a journal with the previous state, and can be undone. After a change to folder
  permissions, the affected containers are watched.
- Activity shows a log kept as a hash chain without a signature.
- Admins (members of the group `sudo`) reach the service through its socket. The installation requires the
  group `docker`.

A further area, Containers, is a possible direction; nothing is promised for it.

The project is at an early stage of development and is not ready for use.

## License

AGPL-3.0, see [LICENSE](LICENSE).
