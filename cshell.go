package cshell

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

const (
	VT100_UP    = 65
	VT100_DOWN  = 66
	VT100_RIGHT = 67
	VT100_LEFT  = 68
	ASCII_TAB   = 0x09
	ASCII_CTRLC = 0x03
	ASCII_CTRLD = 0x04
	ASCII_BEL   = 0x07
	ASCII_BS    = 0x08
	ASCII_CR    = 0x0D
	ASCII_LF    = 0x0A
	ASCII_ESC   = 0x1B
	ASCII_DEL   = 0x7F

	FlagHide   = 1 << 0
	FlagNoargs = 1 << 1
	FlagAlias  = 1 << 2
)

type CommandFunc = func(args []string) error
type Command struct {
	name  string
	desc  string
	fx    CommandFunc
	flags int
}

type Shell struct {
	Prompt string
	Echo   bool
	// AllowHistoryFiles enables the "save <file>" and "load <file>" subcommands
	// of the built-in history command.  It is disabled by default because the
	// file name comes from shell input, which otherwise gives anyone holding a
	// shell session an arbitrary file read (load) and file overwrite (save)
	// primitive against the process running the shell.
	AllowHistoryFiles bool
	// HistoryFileDir optionally restricts the files that the history save/load
	// subcommands may touch.  A relative name is resolved against it and a
	// resolved path that escapes the directory, including through a symlink, is
	// rejected.  Empty imposes no restriction beyond AllowHistoryFiles.
	HistoryFileDir string
	// MaxHistory bounds the number of command lines kept in the history, so that
	// a long lived session cannot grow it without limit; once it is exceeded the
	// oldest entries are dropped.  New defaults it to defaultMaxHistory, and 0
	// keeps every entry.
	MaxHistory int
	buffer     []byte
	pos        int
	len        int
	state      int
	out        io.Writer
	inp        io.Reader
	run        bool
	commands   []Command
	history    []string
	scrollback int
	savebuf    []byte
	savepos    int
	savelen    int
	// lastCR records that the previous input byte was a carriage return so that
	// a CRLF pair collapses into a single line terminator.
	lastCR bool
}

var ErrNoSuchCommand = errors.New("no such command")

// defaultMaxHistory is the history limit New installs.  A history entry is at
// most one input line long, so this bounds what a session accumulates.
const defaultMaxHistory = 1000

// writeOut sends p to the configured output, if there is one.  A shell created
// by New and used before SetIO, or given a nil writer, has nowhere to write;
// commands still run, they just stay quiet.
func (s *Shell) writeOut(p []byte) {
	if s.out == nil {
		return
	}
	s.out.Write(p)
}

// Printf sends output to the configured io.Writer
// Arguments are handled in the manner of fmt.Printf.
func (s *Shell) Printf(format string, v ...interface{}) {
	s.writeOut([]byte(fmt.Sprintf(format, v...)))
}

// Println sends output to the configured io.Writer
// Arguments are handled in the manner of fmt.Println.
func (s *Shell) Println(v ...interface{}) {
	s.writeOut([]byte(fmt.Sprintln(v...)))
}

// Print sends output to the configured io.Writer
// Arguments are handled in the manner of fmt.Print.
func (s *Shell) Print(v ...interface{}) {
	s.writeOut([]byte(fmt.Sprint(v...)))
}

// output a single character
func (s *Shell) putc(ch byte) {
	s.writeOut([]byte{ch})
}

// output a string
func (s *Shell) puts(str string) {
	s.writeOut([]byte(str))
}

// HexDump outputs a `hexdump -C` like dump of the buf argument
func (s *Shell) HexDump(buf []byte) {
	dump := hex.Dump(buf)
	for _, ch := range dump {
		if ch == '\n' {
			s.putc('\r')
		}
		s.putc(byte(ch))
	}
}

// output the command prompt
func (s *Shell) emitprompt() {
	if !s.Echo {
		return
	}
	if len(s.Prompt) > 0 {
		s.puts(s.Prompt)
		return
	}
	s.puts("cshell> ")
}

// redraw the current line
func (s *Shell) redraw() {
	s.putc(ASCII_CR)
	s.emitprompt()
	for i := 0; i < s.len; i++ {
		s.putc(s.buffer[i])
	}
}

func (s *Shell) erase() {
	s.putc(ASCII_CR)
	s.emitprompt()
	for i := 0; i < s.len; i++ {
		s.putc(' ')
	}
}

// recalledHistory returns the history line the scrollback position currently
// points at, truncated to fit the input buffer.  It reports false when that
// position is no longer part of the history, which can happen if the history
// changed while a recall was in progress.
func (s *Shell) recalledHistory() (string, bool) {
	lh := len(s.history)
	if s.scrollback <= 0 || s.scrollback > lh {
		return "", false
	}
	line := s.history[lh-s.scrollback]
	if len(line) > len(s.buffer) {
		line = line[:len(s.buffer)]
	}
	return line, true
}

// setPosition makes the input buffer hold line with the cursor after it.
func (s *Shell) setPosition(line string) {
	s.pos = len(line)
	s.len = s.pos
	copy(s.buffer, line)
}

func ParseLine(line string) (args []string, err error) {
	var escaped, doubleQuoted, singleQuoted bool
	var tokenStarted bool
	buf := strings.Builder{}

	for _, r := range line {
		if escaped {
			buf.WriteRune(r)
			tokenStarted = true
			escaped = false
			continue
		}
		if r == '\\' {
			if singleQuoted {
				buf.WriteRune(r)
			} else {
				escaped = true
			}
			tokenStarted = true
			continue
		}
		if unicode.IsSpace(r) {
			if singleQuoted || doubleQuoted {
				buf.WriteRune(r)
				continue
			}
			if tokenStarted {
				args = append(args, buf.String())
				buf.Reset()
				tokenStarted = false
			}
			continue
		}
		switch r {
		case '"':
			if !singleQuoted {
				tokenStarted = true
				doubleQuoted = !doubleQuoted
				continue
			}
		case '\'':
			if !doubleQuoted {
				tokenStarted = true
				singleQuoted = !singleQuoted
				continue
			}
		}
		buf.WriteRune(r)
		tokenStarted = true
	}

	if tokenStarted {
		args = append(args, buf.String())
	}

	if escaped || doubleQuoted || singleQuoted {
		err = fmt.Errorf("quote parse error")
		return
	}

	return
}

// AddHistory adds a line to the history
func (s *Shell) AddHistory(line string) {
	s.addHistoryLine(line)
}

// addHistoryLine appends one line, dropping the oldest entries once MaxHistory
// is reached.  It reuses the backing array so that a bounded history does not
// allocate on every command.
func (s *Shell) addHistoryLine(line string) {
	s.history = append(s.history, line)
	if s.MaxHistory <= 0 || len(s.history) <= s.MaxHistory {
		return
	}
	drop := len(s.history) - s.MaxHistory
	copy(s.history, s.history[drop:])
	s.history = s.history[:s.MaxHistory]
}

// GetHistory returns a copy of the history
func (s *Shell) GetHistory() (h []string) {
	h = []string{}
	for _, line := range s.history {
		h = append(h, line)
	}
	return
}

func (s *Shell) ClearHistory() {
	s.history = []string{}
}

// DelHistory removes the last n lines from the history
func (s *Shell) DelHistory(n int) {
	if n <= 0 {
		return
	}
	l := len(s.history)
	if n > l {
		n = l
	}
	s.history = s.history[:l-n]
}

func (s *Shell) LoadHistory(filename string) (err error) {
	var file *os.File
	file, err = os.Open(filename)
	if err != nil {
		return
	}
	defer file.Close()
	history := []string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		history = append(history, scanner.Text())
	}
	if err = scanner.Err(); err != nil {
		return
	}
	if s.MaxHistory > 0 && len(history) > s.MaxHistory {
		history = history[len(history)-s.MaxHistory:]
	}
	s.history = history
	return
}

// ErrHistoryFilesDisabled is returned by the history save/load subcommands
// while Shell.AllowHistoryFiles is false.
var ErrHistoryFilesDisabled = errors.New("history file access is disabled, set Shell.AllowHistoryFiles to enable it")

func (s *Shell) SaveHistory(filename string) (err error) {
	if len(s.history) <= 0 {
		return
	}
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	for _, line := range s.history {
		_, err = io.WriteString(file, line+"\n")
		if err != nil {
			return err
		}
	}
	return
}

// historyFilePath validates a file name passed to the history save/load
// subcommands.  With AllowHistoryFiles false the subcommands are refused.  With
// HistoryFileDir set, a relative name is resolved against that directory and
// the resolved path must stay inside it.
func (s *Shell) historyFilePath(name string) (string, error) {
	if !s.AllowHistoryFiles {
		return "", ErrHistoryFilesDisabled
	}
	if s.HistoryFileDir == "" {
		return name, nil
	}
	base, err := filepath.Abs(s.HistoryFileDir)
	if err != nil {
		return "", fmt.Errorf("history dir: %v", err)
	}
	if resolved, err := filepath.EvalSymlinks(base); err == nil {
		base = resolved
	}
	if _, err := os.Stat(base); err != nil {
		return "", fmt.Errorf("history dir %s: %v", s.HistoryFileDir, err)
	}
	target := name
	if filepath.IsAbs(target) {
		target = filepath.Clean(target)
	} else {
		target = filepath.Join(base, target)
	}
	// A name may not escape the directory, and neither may any symlink it is
	// reached through, so resolve the deepest component that exists and check
	// that it lands inside base.
	path := target
	for {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path = resolved
			break
		}
		parent := filepath.Dir(path)
		if parent == path {
			break
		}
		path = parent
	}
	if !withinDir(path, base) {
		return "", fmt.Errorf("%s: outside of %s", name, s.HistoryFileDir)
	}
	return target, nil
}

func withinDir(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

func (s *Shell) SearchHistory(match string) (line string) {
	histLen := len(s.history)
	if histLen <= 0 {
		return
	}
	for x := histLen - 1; x >= 0; x-- {
		if strings.HasPrefix(s.history[x], match) {
			return s.history[x]
		}
	}
	return
}

// canonicalName folds a command name so that it can only match whole words of a
// command line: leading and trailing space is dropped and runs of space inside
// the name collapse into one.
func canonicalName(name string) string {
	return strings.Join(strings.Fields(name), " ")
}

// lookupCommand resolves the command that a parsed command line runs.  A
// command name may consist of more than one word, in which case all of its
// words must match the leading words of the line, and any command may be
// abbreviated by a prefix of its first word.  An exact match takes precedence
// over an abbreviation.  The second result reports that more than one command
// matched and none of them was named in full, so the choice would be a guess.
func (s *Shell) lookupCommand(args []string) (*Command, bool) {
	if len(args) == 0 || args[0] == "" {
		return nil, false
	}
	var (
		exact     *Command
		abbrev    *Command
		ambiguous bool
	)
	for i := range s.commands {
		c := &s.commands[i]
		name := strings.Fields(c.name)
		if len(name) == 0 || len(name) > len(args) {
			continue
		}
		// The first word may be abbreviated, the rest must be spelled out.
		if name[0] != args[0] && !strings.HasPrefix(name[0], args[0]) {
			continue
		}
		ok := true
		for j := 1; j < len(name); j++ {
			if name[j] != args[j] {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		if len(name) == len(args) && name[0] == args[0] {
			// named in full; the first registration wins
			if exact == nil {
				exact = c
			}
			continue
		}
		if exact != nil {
			continue
		}
		if abbrev != nil {
			ambiguous = true
		} else {
			abbrev = c
		}
	}
	if exact != nil {
		return exact, false
	}
	return abbrev, abbrev != nil && ambiguous
}

func (s *Shell) ExecuteLine(line string) (err error) {
	line = strings.TrimLeft(line, " \t")
	if line == "" {
		return
	}

	histLen := len(s.history)
	if line[0] == '!' {
		if line == "!!" {
			// repeat last command
			if histLen > 0 {
				line = s.history[histLen-1]
			} else {
				s.Printf("%s: not found\r\n", line)
				return nil
			}
		} else if n, err := strconv.Atoi(line[1:]); err == nil {
			if n > 0 && n <= histLen {
				line = s.history[n-1]
			} else {
				s.Printf("%s: not found\r\n", line)
				return nil
			}
		} else if x := s.SearchHistory(line[1:]); x != "" {
			line = x
		} else {
			s.Printf("%s: not found\r\n", line)
			return nil
		}
	}

	line = strings.TrimLeft(line, " \t")
	if line == "" {
		return
	}

	// lines prefixed with @ are not added to the history
	addhist := true
	if line[0] == '@' {
		addhist = false
		line = strings.TrimLeft(line[1:], " \t")
		if line == "" {
			return
		}
	}

	if addhist {
		if last := len(s.history); last <= 0 || s.history[last-1] != line {
			s.addHistoryLine(line)
		}
	}

	// lines prefixed with # are comments.
	if line[0] == '#' {
		return
	}

	args, err := ParseLine(line)
	if err != nil || len(args) == 0 {
		return
	}

	cmd, ambiguous := s.lookupCommand(args)
	if ambiguous {
		s.Printf("Error: multiple matches\r\n")
		return
	}

	if cmd == nil {
		// default command
		for i := range s.commands {
			c := &s.commands[i]
			if c.name == "*" {
				cmd = c
				break
			}
		}
	}

	if cmd == nil {
		s.Printf("No such command: %s\r\n", args[0])
		return
	}
	if cmd.fx == nil {
		// Registered with no handler, which is a configuration error the
		// embedding program has to fix; do not panic the session over it.
		s.Printf("Command %s: no handler registered\r\n", args[0])
		return
	}
	if cmd.flags&FlagNoargs != 0 {
		args = []string{line}
	}
	err = cmd.fx(args)
	if err != nil {
		s.Printf("Command %s error: %v\r\n", args[0], err)
	}

	return
}

func (s *Shell) Command(name string, desc string, fx CommandFunc) {
	if s == nil {
		return
	}
	s.commands = append(s.commands, Command{name: canonicalName(name), desc: desc, fx: fx})
}

func (s *Shell) CommandOneArg(name string, desc string, fx CommandFunc) {
	if s == nil {
		return
	}
	s.commands = append(s.commands, Command{name: canonicalName(name), desc: desc, fx: fx, flags: FlagNoargs})
}

func (s *Shell) SetFlags(name string, flags int) error {
	name = canonicalName(name)
	for i := range s.commands {
		c := &s.commands[i]
		if c.name == name {
			c.flags = flags
			return nil
		}
	}
	return ErrNoSuchCommand
}

func (s *Shell) History(args []string) error {
	var l int = len(s.history)
	var start int = 0
	if len(args) > 1 {
		switch args[1] {
		case "clear", "-c":
			s.ClearHistory()
			return nil
		case "save":
			if len(args) > 2 {
				if path, err := s.historyFilePath(args[2]); err != nil {
					s.Printf("Save %s: %v\r\n", args[2], err)
				} else if err := s.SaveHistory(path); err != nil {
					s.Printf("Save %s: %v\r\n", args[2], err)
				}
			}
			return nil
		case "load":
			if len(args) > 2 {
				if path, err := s.historyFilePath(args[2]); err != nil {
					s.Printf("Load %s: %v\r\n", args[2], err)
				} else if err := s.LoadHistory(path); err != nil {
					s.Printf("Load %s: %v\r\n", args[2], err)
				}
			}
			return nil
		case "del":
			if len(args) > 2 {
				if n, err := strconv.Atoi(args[2]); err == nil {
					s.DelHistory(n + 1)
				}
			} else {
				s.DelHistory(2) // remove current command (history) plus previous
			}
			return nil
		default:
			if i, err := strconv.Atoi(args[1]); err == nil && i > 0 {
				if i < l {
					start = l - i
				}
			}

		}
	}

	//s.Printf("XXX args=%#v\r\n", args)
	for i := start; i < len(s.history); i++ {
		s.Printf("%6d  %s\r\n", i+1, s.history[i])
	}
	return nil
}

func (s *Shell) Help(args []string) error {

	longest := 0
	if longest < 10 {
		longest = 10
	}
	for _, cmd := range s.commands {
		l := len(cmd.name)
		if l > longest {
			longest = l
		}
	}

	for _, cmd := range s.commands {
		if cmd.flags&FlagHide != 0 {
			continue
		}
		s.Printf("%-[2]*.[3]*[1]s  %[4]s\r\n", cmd.name, longest, longest, cmd.desc)
	}
	return nil
}

// input will process a single character generating the shell behavior.
func (s *Shell) input(ch byte) {
	lastCR := s.lastCR
	s.lastCR = false
	switch s.state {
	case 2:
		switch ch {
		case VT100_UP:
			lh := len(s.history)
			if lh <= 0 || s.scrollback >= lh {
				s.putc(ASCII_BEL)
				break
			}
			if s.scrollback == 0 {
				s.savepos = s.pos
				s.savelen = s.len
				copy(s.savebuf, s.buffer)
			}
			s.erase()
			s.scrollback++
			line, ok := s.recalledHistory()
			if !ok {
				s.scrollback--
				s.putc(ASCII_BEL)
				break
			}
			s.setPosition(line)
			s.redraw()
		case VT100_DOWN:
			if s.scrollback <= 0 {
				s.putc(ASCII_BEL)
				s.scrollback = 0
				break
			}
			s.scrollback--
			if s.scrollback == 0 {
				s.erase()
				s.pos = s.savepos
				s.len = s.savelen
				copy(s.buffer, s.savebuf)
				s.redraw()
				break
			}
			line, ok := s.recalledHistory()
			if !ok {
				// the history changed underneath the recall, there is nothing
				// left to step back to
				s.scrollback = 0
				s.putc(ASCII_BEL)
				break
			}
			s.erase()
			s.setPosition(line)
			s.redraw()
		case VT100_RIGHT:
			if s.pos < s.len {
				s.putc(s.buffer[s.pos])
				s.pos++
			} else {
				s.putc(ASCII_BEL)
			}
		case VT100_LEFT:
			if s.pos > 0 {
				s.pos--
				s.putc(ASCII_BS)
			} else {
				s.putc(ASCII_BEL)
			}
		}
		s.state = 0
		return
	case 1:

		if ch == '[' {
			s.state = 2
			return
		} else {
			s.state = 0
		}
		return
	default:
		s.state = 0
	}

	switch {
	case ch == ASCII_CR || ch == ASCII_LF:
		// A line may be terminated by CR, by LF, or by a CR immediately followed
		// by LF, in which case the LF is discarded.
		if ch == ASCII_LF && lastCR {
			return
		}
		if ch == ASCII_CR {
			s.lastCR = true
		}
		s.putc(ASCII_CR)
		s.putc(ASCII_LF)
		//s.execute()
		s.ExecuteLine(string(s.buffer[:s.len]))
		s.len = 0
		s.pos = 0
		s.scrollback = 0
		s.emitprompt()
	case ch == ASCII_DEL || ch == ASCII_BS:
		if s.pos <= 0 {
			s.putc(ASCII_BEL)
		} else {
			if s.pos == s.len {
				s.putc(ASCII_BS)
				s.putc(' ')
				s.putc(ASCII_BS)
				s.len--
				s.pos--
			} else {
				s.len--
				s.pos--
				for i := s.pos; i < s.len; i++ {
					s.buffer[i] = s.buffer[i+1]
				}
				s.redraw()
				s.putc(' ')
				for i := s.pos; i < s.len+1; i++ {
					s.putc(ASCII_BS)
				}
			}
		}
	case ch == ASCII_ESC:
		s.state = 1
	case ch == ASCII_CTRLC:
		if s.Echo {
			s.puts("\r\n")
			s.emitprompt()
		}
		s.len = 0
		s.pos = 0
		s.scrollback = 0
	case ch == ASCII_TAB:
		// XXX do completion here
		ch = ' '
		fallthrough
	default:
		// The control characters not handled above are dropped rather than
		// echoed and stored.  Writing them to the output lets remote input drive
		// the terminal (BEL, BS, form feed, ...), and storing them in the line
		// buffer would hand raw control bytes to commands in their args.
		if ch < 0x20 {
			return
		}
		if s.len < len(s.buffer) {
			if s.pos == s.len {
				s.putc(ch)
				s.buffer[s.pos] = ch
				s.pos++
				s.len++
			} else {
				s.len++
				for i := s.len - 1; i > s.pos; i-- {
					s.buffer[i] = s.buffer[i-1]
				}
				s.buffer[s.pos] = ch
				s.pos++
				s.redraw()
				for i := s.pos + 1; i < s.len+1; i++ {
					s.putc(ASCII_BS)
				}
			}
		} else {
			s.putc(ASCII_BEL)
		}
	}
}

func (s *Shell) Terminate() {
	s.run = false
}

// Run reads and writes from the io streams generating the
// command prompt and executing commands.
func (s *Shell) Run() {
	buf := make([]byte, 8)
	s.lastCR = false
	if s.inp == nil {
		// nothing to read, Run would otherwise panic on its first Read
		return
	}
	s.emitprompt()
	s.run = true
	for s.run {
		n, err := s.inp.Read(buf)
		//fmt.Printf("XXXXXXX n=%d buf=<%s>\n\n", n, string(buf))
		for i := 0; i < n; i++ {
			if buf[i] == ASCII_CTRLD {
				s.puts("\r\n")
				return
			}
			//fmt.Printf("n=%d %3d 0x%2.2x %c\r\n", n, buf[i], buf[i], buf[i])
			s.input(buf[i])
		}
		if err != nil {
			if err != io.EOF {
				s.Printf("read input error: %v\n", err)
			}
			return
		}
	}
}

// SetIO assigns input and output sources.  They should conform to the io.Reader
// and io.Writer interface.  The io.Reader should block until a characters are ready.
func (s *Shell) SetIO(r io.Reader, w io.Writer) {
	s.inp = r
	s.out = w
}

// SetEcho changes the echo state for the command prompt.
func (s *Shell) SetEcho(echo bool) {
	s.Echo = echo
}

// SetPrompt changes the shell command prompt.
func (s *Shell) SetPrompt(prompt string) {
	s.Prompt = prompt
}

// New creates a new command prompt.
func New() (s *Shell) {
	s = &Shell{
		Prompt:     "cshell> ",
		Echo:       true,
		MaxHistory: defaultMaxHistory,
		buffer:     make([]byte, 512),
		savebuf:    make([]byte, 512),
	}
	s.Command("help", "Print commands", s.Help)
	s.Command("history", "[n|del [n]|-c|clear|save <file>|load <file>] Command history", s.History)
	return
}
