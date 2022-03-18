package cshell

import (
	"encoding/hex"
	"fmt"
	"io"
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
)

type CommandFunc = func(args []string) error
type Command struct {
	name string
	desc string
	fx   CommandFunc
}

type Shell struct {
	Prompt   string
	Echo     bool
	buffer   []byte
	pos      int
	len      int
	state    int
	out      io.Writer
	inp      io.Reader
	commands []Command
	history  []string
	hpos     int
}

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

// execute a complete line
func (s *Shell) execute() {

	//args := strings.Fields(string(s.buffer[:s.len]))
	//args, err := shellwords.Parse(string(s.buffer[:s.len]))
	args, err := ParseLine(string(s.buffer[:s.len]))
	if err != nil {
		return
	}

	if len(args) <= 0 {
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

	// default command
	for i, _ := range s.commands {
		c := &s.commands[i]
		if c.name == "*" {
			cmd = c
			break
		}
	}

	if cmd == nil {
		s.Printf("No such command: %s\r\n", args[0])
		return
	}
	err = cmd.fx(args)
	if err != nil {
		s.Printf("Command %s error: %v\r\n", args[0], err)
	}
}

func (s *Shell) Command(name string, desc string, fx CommandFunc) {
	if s == nil {
		return
	}
	s.commands = append(s.commands, Command{name: name, desc: desc, fx: fx})
}

func (s *Shell) Help(args []string) error {
	for _, cmd := range s.commands {
		s.Printf("%-10.10s %s\r\n", cmd.name, cmd.desc)
	}
	return nil
}

// input will process a single character generating the shell behavior.
func (s *Shell) input(ch byte) {
	switch s.state {
	case 2:
		switch ch {
		case VT100_UP:
			// history up
		case VT100_DOWN:
			// history down
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
		s.execute()
		s.len = 0
		s.pos = 0
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

// Run reads and writes from the io streams generating the
// command prompt and executing commands.
func (s *Shell) Run() {
	buf := make([]byte, 8)
	s.emitprompt()
	for {
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
		Prompt: "cshell> ",
		Echo:   true,
		buffer: make([]byte, 256),
	}
	s.Command("help", "Print commands", s.Help)
	return
}
