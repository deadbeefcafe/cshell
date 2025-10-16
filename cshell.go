package cshell

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
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
	Prompt     string
	Echo       bool
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
}

var ErrNoSuchCommand = errors.New("no such command")

// Printf sends output to the configured io.Writer
// Arguments are handled in the manner of fmt.Printf.
func (s *Shell) Printf(format string, v ...interface{}) {
	s.out.Write([]byte(fmt.Sprintf(format, v...)))
}

// Println sends output to the configured io.Writer
// Arguments are handled in the manner of fmt.Println.
func (s *Shell) Println(v ...interface{}) {
	s.out.Write([]byte(fmt.Sprintln(v...)))
}

// Print sends output to the configured io.Writer
// Arguments are handled in the manner of fmt.Print.
func (s *Shell) Print(v ...interface{}) {
	s.out.Write([]byte(fmt.Sprint(v...)))
}

// output a single character
func (s *Shell) putc(ch byte) {
	s.out.Write([]byte{ch})
}

// output a string
func (s *Shell) puts(str string) {
	s.out.Write([]byte(str))
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

func ParseLine(line string) (args []string, err error) {
	var escaped, doubleQuoted, singleQuoted bool
	buf := strings.Builder{}

	for _, r := range line {
		if escaped {
			buf.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			if singleQuoted {
				buf.WriteRune(r)
			} else {
				escaped = true
			}
			continue
		}
		if unicode.IsSpace(r) {
			if singleQuoted || doubleQuoted {
				buf.WriteRune(r)
				continue
			}
			if buf.Len() > 0 {
				args = append(args, buf.String())
				buf.Reset()
			}
			continue
		}
		switch r {
		case '"':
			if !singleQuoted {
				doubleQuoted = !doubleQuoted
				continue
			}
		case '\'':
			if !doubleQuoted {
				singleQuoted = !singleQuoted
				continue
			}
		}
		buf.WriteRune(r)
	}

	if buf.Len() > 0 {
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
	s.history = append(s.history, line)
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
	s.history = []string{}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		s.history = append(s.history, scanner.Text())
	}
	return
}

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

func (s *Shell) ExecuteLine(line string) (err error) {
	if line == "" {
		return
	}

	histLen := len(s.history)
	if line[0] == '!' {
		if line == "!!" {
			// repeat last command
			if histLen > 0 {
				line = s.history[histLen-1]
			}
		} else if n, err := strconv.Atoi(line[1:]); err == nil {
			if n > 0 && n <= histLen {
				line = s.history[n-1]
			}
		} else if x := s.SearchHistory(line[1:]); x != "" {
			line = x
		} else {
			s.Printf("%s: not found\r\n", line)
			return nil
		}
	}

	line = strings.TrimLeft(line, " \t")

	// lines prefixed with @ are not added to the history
	addhist := true
	if line[0] == '@' {
		addhist = false
		line = line[1:]
	}

	if addhist {
		if histLen <= 0 || s.history[histLen-1] != line {
			s.history = append(s.history, line)
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

	var cmd *Command
	for i, _ := range s.commands {
		c := &s.commands[i]
		if c.name == args[0] {
			cmd = c
			break
		}
		if strings.HasPrefix(c.name, args[0]) {
			if cmd != nil {
				s.Printf("Error: multiple matches\r\n")
				return
			}
			cmd = c
		}
	}

	if cmd == nil {
		// default command
		for i, _ := range s.commands {
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
	s.commands = append(s.commands, Command{name: name, desc: desc, fx: fx})
}

func (s *Shell) CommandOneArg(name string, desc string, fx CommandFunc) {
	if s == nil {
		return
	}
	s.commands = append(s.commands, Command{name: name, desc: desc, fx: fx, flags: FlagNoargs})
}

func (s *Shell) SetFlags(name string, flags int) error {
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
				if err := s.SaveHistory(args[2]); err != nil {
					s.Printf("Save %s: %v\r\n", args[2], err)
				}
			}
			return nil
		case "load":
			if len(args) > 2 {
				if err := s.LoadHistory(args[2]); err != nil {
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
			line := s.history[lh-s.scrollback]
			s.pos = len(line)
			s.len = s.pos
			for i := 0; i < s.pos; i++ {
				s.buffer[i] = line[i]
			}
			s.redraw()
		case VT100_DOWN:
			if s.scrollback <= 0 {

			}
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
			s.erase()
			lh := len(s.history)
			line := s.history[lh-s.scrollback]
			s.pos = len(line)
			s.len = s.pos
			for i := 0; i < s.pos; i++ {
				s.buffer[i] = line[i]
			}
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
	case ch == ASCII_CR:
		s.putc(ASCII_CR)
		s.putc(ASCII_LF)
		//s.execute()
		s.ExecuteLine(string(s.buffer[:s.len]))
		s.len = 0
		s.pos = 0
		s.scrollback = 0
		s.emitprompt()
	case ch == ASCII_DEL:
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
			s.len = 0
			s.pos = 0
		}
	case ch == ASCII_TAB:
		// XXX do completion here
		ch = ' '
		fallthrough
		//case ch >= 0x20 && ch < 0x7f:
	default:
		if s.pos < len(s.buffer) {
			if s.pos == s.len {
				s.putc(ch)
				s.buffer[s.pos] = ch
				s.pos++
				s.len++
			} else {
				s.len++
				for i := s.len; i > s.pos; i-- {
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
	s.emitprompt()
	s.run = true
	for s.run {
		n, err := s.inp.Read(buf)
		//fmt.Printf("XXXXXXX n=%d buf=<%s>\n\n", n, string(buf))
		if err != nil {
			fmt.Printf("read input error: %v\n", err)
			return
		}
		for i := 0; i < n; i++ {
			if buf[i] == ASCII_CTRLD {
				s.puts("\r\n")
				return
			}
			//fmt.Printf("n=%d %3d 0x%2.2x %c\r\n", n, buf[i], buf[i], buf[i])
			s.input(buf[i])
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
		Prompt:  "cshell> ",
		Echo:    true,
		buffer:  make([]byte, 512),
		savebuf: make([]byte, 512),
	}
	s.Command("help", "Print commands", s.Help)
	s.Command("history", "[n|del [n]|-c|clear|save <file>|load <file>] Command history", s.History)
	return
}
