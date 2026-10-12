# cshell
A go package implementing a simple command line shell

## Behavior notes

Input handling:

* A command line may be terminated by `CR`, by `LF`, or by a `CR` `LF` pair,
  which is collapsed into one terminator.  Earlier versions only recognized `CR`,
  so input coming from a pipe or an LF-only client never executed.
* Control characters that carry no shell meaning (`BEL`, `FF`, `^A`, `^U`, ...)
  are dropped instead of being inserted into the command line and echoed to the
  terminal.  They therefore no longer reach command handlers through `args`.
* `BS` (`^H`) deletes the previous character, like `DEL`, since terminals use
  either one for backspace.

The `history` command registers `save <file>` and `load <file>` by default, and
the file name comes from shell input.  Both subcommands are disabled unless a
program opts in, otherwise any client holding a session gets an arbitrary file
read (`load`) and file overwrite (`save`) primitive:

```go
sh.AllowHistoryFiles = true                 // otherwise: "history file access is disabled"
sh.HistoryFileDir = "/var/lib/mysh/history" // optional; confines save/load to this directory
```

With `HistoryFileDir` set, a relative name is resolved against that directory
and any name that resolves outside it, including through a symlink, is refused.
`Shell.SaveHistory` and `Shell.LoadHistory` remain unrestricted, since they are
called directly by the program rather than by shell input.
