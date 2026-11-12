package repeater

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// ErrNotAllowed is returned for any command outside the read-only allowlist.
var ErrNotAllowed = errors.New("command not allowed (read-only allowlist)")

var allowed = map[string]bool{
	"ver": true, "get name": true, "get radio": true,
	"stats-core": true, "stats-radio": true, "stats-packets": true, "neighbors": true,
}

const (
	queryTimeout = 2 * time.Second
	quietGap     = 250 * time.Millisecond
)

// Client sends allowlisted CLI commands and collects their replies.
type Client struct {
	w     io.Writer
	lines chan string
	done  chan struct{}
	mu    sync.Mutex // one query at a time
	err   error
}

// NewClient starts a reader goroutine on rw.
func NewClient(rw io.ReadWriter) *Client {
	c := &Client{w: rw, lines: make(chan string, 256), done: make(chan struct{})}
	go func() {
		sc := bufio.NewScanner(rw)
		for sc.Scan() {
			select {
			case c.lines <- strings.TrimRight(sc.Text(), "\r"):
			default: // nobody listening; drop
			}
		}
		c.err = sc.Err()
		if c.err == nil {
			c.err = io.EOF
		}
		close(c.done)
	}()
	return c
}

// Query sends an allowlisted read-only command and returns the reply payload
// (text after "-> " plus any continuation lines until the line goes quiet).
func (c *Client) Query(cmd string) (string, error) {
	if !allowed[cmd] {
		return "", ErrNotAllowed
	}
	return c.do(cmd)
}

// Exec sends any single-line command. Callers must confirm risky commands
// (see IsReadOnly) with the user before calling it.
func (c *Client) Exec(cmd string) (string, error) {
	cmd = strings.TrimSpace(cmd)
	if err := ValidateCommand(cmd); err != nil {
		return "", err
	}
	return c.do(cmd)
}

// MaxCommandLen bounds manual commands (the firmware CLI buffer is small).
const MaxCommandLen = 160

// ValidateCommand rejects empty, overlong and control-character commands, so a
// line can never smuggle a second command or terminal escapes.
func ValidateCommand(cmd string) error {
	if cmd == "" {
		return errors.New("empty command")
	}
	if len(cmd) > MaxCommandLen {
		return fmt.Errorf("command longer than %d characters", MaxCommandLen)
	}
	for _, r := range cmd {
		if r < 0x20 || r == 0x7f {
			return errors.New("command must be a single line without control characters")
		}
	}
	return nil
}

// Redact hides secrets in a command and its output before they are logged
// or shown to other viewers.
func Redact(cmd, out string) (string, string) {
	f := strings.Fields(cmd)
	switch {
	case len(f) >= 2 && f[0] == "password":
		cmd = "password ***"
	case len(f) >= 3 && f[0] == "set" && strings.Contains(f[1], "password"):
		cmd = "set " + f[1] + " ***"
	case len(f) >= 2 && f[0] == "get" && strings.Contains(f[1], "password"):
		if strings.HasPrefix(out, "> ") {
			out = "> ***"
		} else if out != "" {
			out = "***"
		}
	}
	return cmd, out
}

var readOnlyExact = map[string]bool{
	"ver": true, "clock": true, "neighbors": true, "log": true,
	"stats-core": true, "stats-radio": true, "stats-packets": true,
}

// IsReadOnly reports whether cmd is known not to change repeater state.
// Unknown commands are treated as risky.
func IsReadOnly(cmd string) bool {
	cmd = strings.Join(strings.Fields(cmd), " ")
	return readOnlyExact[cmd] || strings.HasPrefix(cmd, "get ")
}

func (c *Client) do(cmd string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.done:
		return "", fmt.Errorf("serial closed: %w", c.err)
	default:
	}
	for drained := false; !drained; {
		select {
		case <-c.lines:
		default:
			drained = true
		}
	}
	if _, err := io.WriteString(c.w, cmd+"\r"); err != nil {
		return "", err
	}
	deadline := time.NewTimer(queryTimeout)
	defer deadline.Stop()
	var out []string
	var quiet <-chan time.Time
	echoed := false
	for {
		select {
		case l := <-c.lines:
			if !echoed {
				// Only a reply after our own echo counts; earlier "-> " lines
				// are late replies to a previous, timed-out command.
				echoed = strings.HasSuffix(strings.TrimSpace(l), cmd)
				continue
			}
			if out == nil {
				i := strings.Index(l, "-> ")
				if i < 0 {
					continue // unsolicited noise
				}
				out = []string{strings.TrimSpace(l[i+3:])}
			} else if t := strings.TrimSpace(l); t != "" {
				out = append(out, t)
			}
			quiet = time.After(quietGap)
		case <-quiet:
			return strings.Join(out, "\n"), nil
		case <-deadline.C:
			if out != nil {
				return strings.Join(out, "\n"), nil
			}
			return "", fmt.Errorf("%s: timeout", cmd)
		case <-c.done:
			if out != nil {
				return strings.Join(out, "\n"), nil
			}
			return "", fmt.Errorf("serial closed: %w", c.err)
		}
	}
}

// CommonCommands are MeshCore repeater CLI commands offered for completion.
var CommonCommands = []string{
	"ver", "clock", "clock sync", "neighbors", "log", "log start", "log stop", "log erase",
	"stats-core", "stats-radio", "stats-packets", "advert", "reboot",
	"get name", "get radio", "get tx", "get freq", "get af", "get repeat", "get lat", "get lon",
	"get advert.interval", "get flood.advert.interval", "get flood.max", "get rxdelay", "get txdelay",
	"get direct.txdelay", "get guest.password", "get allow.read.only",
	"set name ", "set radio ", "set tx ", "set freq ", "set af ", "set repeat ", "set lat ", "set lon ",
	"set advert.interval ", "set flood.advert.interval ", "set flood.max ", "set rxdelay ", "set txdelay ",
	"password ", "erase",
}
