// package editor implements a full-feature line editor.
package editor

import (
	"os"
	"fmt"
	"bufio"
	"unicode"
	"unicode/utf8"
	"./tty"
)

type cell struct {
	rune
	width byte
}

type buffer [][]cell

type Editor struct {
	savedTermios *tty.Termios
	file *os.File
	oldBuf, buf buffer
	line, col, width int
}

type LineRead struct {
	Line string
	Eof bool
	Err error
}

var savedTermios *tty.Termios

func Init(fd int) (*Editor, error) {
	term, err := tty.NewTermiosFromFd(fd)
	if err != nil {
		return nil, fmt.Errorf("Can't get terminal attribute: %s", err)
	}

	editor := &Editor{
		savedTermios: term.Copy(),
		file: os.NewFile(uintptr(fd), "<line editor terminal>"),
		oldBuf: [][]cell{[]cell{}},
	}

	term.SetIcanon(false)
	term.SetEcho(false)
	term.SetMin(1)
	term.SetTime(0)

	err = term.ApplyToFd(fd)
	if err != nil {
		return nil, fmt.Errorf("Can't set up terminal attribute: %s", err)
	}

	fmt.Fprint(editor.file, "\033[?7l")
	return editor, nil
}

func (ed *Editor) Cleanup() error {
	fmt.Fprint(ed.file, "\033[?7h")

	fd := int(ed.file.Fd())
	err := ed.savedTermios.ApplyToFd(fd)
	if err != nil {
		return fmt.Errorf("Can't restore terminal attribute of stdin: %s", err)
	}
	ed.savedTermios = nil
	return nil
}

func (ed *Editor) beep() {
}

func (ed *Editor) tip(s string) {
	fmt.Fprintf(ed.file, "\n%s\033[A", s)
}

func (ed *Editor) tipf(format string, a ...interface{}) {
	ed.tip(fmt.Sprintf(format, a...))
}

func (ed *Editor) clearTip() {
	fmt.Fprintf(ed.file, "\n\033[K\033[A")
}

func (ed *Editor) startBuffer() {
	ed.line = 0
	ed.col = 0
	ed.width = int(tty.GetWinsize(int(ed.file.Fd())).Col)
	ed.buf = [][]cell{make([]cell, ed.width)}
}

func (ed *Editor) commitBuffer() error {
	newlines := len(ed.oldBuf) - 1
	if newlines > 0 {
		fmt.Fprintf(ed.file, "\033[%dA", newlines)
	}
	fmt.Fprintf(ed.file, "\r\033[J")

	for _, line := range ed.buf {
		for _, c := range line {
			_, err := ed.file.WriteString(string(c.rune))
			if err != nil {
				return err
			}
		}
	}
	ed.oldBuf = ed.buf
	return nil
}

func (ed *Editor) appendToLine(c cell) {
	ed.buf[ed.line] = append(ed.buf[ed.line], c)
	ed.col += int(c.width)
}

func (ed *Editor) newline() {
	ed.buf[ed.line] = append(ed.buf[ed.line], cell{rune: '\n'})
	ed.buf = append(ed.buf, make([]cell, ed.width))
	ed.line++
	ed.col = 0
}

func (ed *Editor) write(r rune) {
	wd := wcwidth(r)
	c := cell{r, byte(wd)}

	if ed.col + wd > ed.width {
		ed.newline()
		ed.appendToLine(c)
	} else if ed.col + wd == ed.width {
		ed.appendToLine(c)
		ed.newline()
	} else {
		ed.appendToLine(c)
	}
}

func (ed *Editor) refresh(prompt, text string) error {
	ed.startBuffer()

	for _, r := range prompt {
		ed.write(r)
	}

	var indent int
	if ed.col * 2 < ed.width {
		indent = ed.col
	}

	for _, r := range text {
		ed.write(r)
		if ed.col == 0 {
			// XXX doesn't work on overflowed runes
			for i := 0; i < indent; i++ {
				ed.write(' ')
			}
		}
	}

	return ed.commitBuffer()
}

func (ed *Editor) ReadLine(prompt string) (lr LineRead) {
	stdin := bufio.NewReaderSize(ed.file, 0)
	line := ""

	for {
		err := ed.refresh(prompt, line)
		if err != nil {
			return LineRead{Err: err}
		}

		r, _, err := stdin.ReadRune()
		if err != nil {
			return LineRead{Err: err}
		}

		switch {
		case r == '\n':
			ed.clearTip()
			fmt.Fprintln(ed.file)
			return LineRead{Line: line}
		case r == 0x7f: // Backspace
			if l := len(line); l > 0 {
				_, w := utf8.DecodeLastRuneInString(line)
				line = line[:l-w]
			} else {
				ed.beep()
			}
		case r == 0x15: // ^U
			line = ""
		case r == 0x4 && len(line) == 0: // ^D
			return LineRead{Eof: true}
		case r == 0x2: // ^B
			fmt.Fprintf(ed.file, "\033[D")
		case r == 0x6: // ^F
			fmt.Fprintf(ed.file, "\033[C")
		case unicode.IsGraphic(r):
			line += string(r)
		default:
			ed.tipf("Non-graphic: %#x", r)
		}
	}

	panic("unreachable")
}
