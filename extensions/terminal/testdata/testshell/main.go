// Command testshell is the shell the tests of the terminal extension run in a pseudo-terminal (SPEC §10.25 "Tests"): a small
// program that echoes what is typed, runs a few commands that draw with escape sequences, uses the alternate screen, tracks the
// mouse and reports the size of its terminal. It reads the terminal in raw mode, so every byte a test types arrives as it is.
// Raw mode turns the terminal's ECHO off and the shell echoes what is typed itself, as a shell with a line editor does; with
// the argument --tty-echo the shell leaves ECHO on and lets the terminal echo, as a program that reads its input in cooked
// mode does (a terminal that echoes is one whose input is not hidden, decision 185).
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func main() {
	ttyEcho := len(os.Args) > 1 && os.Args[1] == "--tty-echo"
	fd := int(os.Stdin.Fd())
	if old, err := unix.IoctlGetTermios(fd, unix.TCGETS); err == nil {
		raw := *old
		raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
		raw.Oflag &^= unix.OPOST
		raw.Lflag &^= unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
		if !ttyEcho {
			raw.Lflag &^= unix.ECHO
		}
		raw.Cc[unix.VMIN], raw.Cc[unix.VTIME] = 1, 0
		_ = unix.IoctlSetTermios(fd, unix.TCSETS, &raw)
	}
	out := os.Stdout
	say := func(format string, args ...any) { fmt.Fprintf(out, format, args...) }
	say("ready\r\n$ ")
	var line []byte
	buf := make([]byte, 4096)
	var mouse, keys bool
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			return
		}
		if mouse {
			say("mouse:%x\r\n", buf[:n])
			continue
		}
		if keys {
			if n == 1 && buf[0] == 'q' {
				keys = false
				say("\r\n$ ")
				continue
			}
			say("keys:%x\r\n", buf[:n])
			continue
		}
		for _, b := range buf[:n] {
			switch b {
			case '\r', '\n':
				say("\r\n")
				cmd := strings.TrimSpace(string(line))
				line = line[:0]
				switch fields := strings.Fields(cmd); {
				case len(fields) == 0:
				case fields[0] == "echo":
					say("%s\r\n", strings.TrimSpace(strings.TrimPrefix(cmd, "echo")))
				case fields[0] == "colors":
					say("\x1b[31mred\x1b[0m \x1b[1;32mgreen\x1b[0m \x1b[4;34munderlined blue\x1b[0m \x1b[7minverse\x1b[0m \x1b[38;2;255;128;0mtruecolor\x1b[0m\r\n")
				case fields[0] == "alt":
					say("\x1b[?1049h\x1b[2J\x1b[1;1HALT SCREEN\x1b[3;5Hinside the alternate screen")
					time.Sleep(50 * time.Millisecond)
					continue
				case fields[0] == "main":
					say("\x1b[?1049l")
				case fields[0] == "mouse":
					say("\x1b[?1000h\x1b[?1006h")
					mouse = true
					continue
				case fields[0] == "keys":
					keys = true
					continue
				case fields[0] == "size":
					if ws, err := unix.IoctlGetWinsize(int(out.Fd()), unix.TIOCGWINSZ); err == nil {
						say("size %d %d\r\n", ws.Row, ws.Col)
					}
				case fields[0] == "flood" && len(fields) == 2:
					count, _ := strconv.Atoi(fields[1])
					var sb strings.Builder
					for i := 1; i <= count; i++ {
						fmt.Fprintf(&sb, "line %d\r\n", i)
						if sb.Len() > 32<<10 {
							_, _ = out.WriteString(sb.String())
							sb.Reset()
						}
					}
					_, _ = out.WriteString(sb.String())
				case fields[0] == "title" && len(fields) > 1:
					say("\x1b]0;%s\x07", strings.Join(fields[1:], " "))
				case fields[0] == "bell":
					say("\a")
				case fields[0] == "exit":
					code := 0
					if len(fields) > 1 {
						code, _ = strconv.Atoi(fields[1])
					}
					os.Exit(code)
				default:
					say("unknown: %s\r\n", cmd)
				}
				say("$ ")
			case 0x7f:
				if len(line) > 0 {
					line = line[:len(line)-1]
					if !ttyEcho {
						say("\b \b")
					}
				}
			case 0x04:
				return
			default:
				line = append(line, b)
				if !ttyEcho {
					_, _ = out.Write([]byte{b})
				}
			}
		}
	}
}
