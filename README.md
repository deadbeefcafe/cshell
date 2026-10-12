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

Commands:

* A command name may be several words long (`dev info`), in which case it matches
  only a line whose leading words are exactly those words.  The first word may be
  abbreviated (`de info`), the rest must be spelled out.  A name given in full
  always beats an abbreviation, and a line that matches several commands without
  naming any of them in full is refused instead of guessed at.  Single word names
  abbreviate as before (`hist` for `history`).  Matching used to be a plain
  `HasPrefix` on the whole name, which put unrelated names in each other's way and
  made multi-word names unreachable.
* Registering a command with a `nil` handler and running it reports an error
  instead of panicking, and a shell that was never given streams (`SetIO`) keeps
  quiet instead of panicking on its first write.

History:

* `New` limits the history to `defaultMaxHistory` (1000) entries and drops the
  oldest once it is exceeded, so a long lived session cannot grow memory without
  bound.  Set `MaxHistory` to `0` to keep everything.
* The `history` command registers `save <file>` and `load <file>`, and the file
  name comes from shell input.  Both subcommands are disabled unless a program
  opts in, otherwise any client holding a session gets an arbitrary file read
  (`load`) and file overwrite (`save`) primitive:

```go
sh.AllowHistoryFiles = true                 // otherwise: "history file access is disabled"
sh.HistoryFileDir = "/var/lib/mysh/history" // optional; confines save/load to this directory
```

With `HistoryFileDir` set, a relative name is resolved against that directory and
any name that resolves outside it, including through a symlink, is refused.
`Shell.SaveHistory` and `Shell.LoadHistory` stay unrestricted, since they are
called by the program rather than by shell input.
