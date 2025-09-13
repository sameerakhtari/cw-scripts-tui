package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"
)

const defaultScript = "./domain-based-backup.sh"

type step int

const (
	stepEmail step = iota
	stepAPI
	stepDomains
	stepConfirm
	stepRunning
	stepDone
)

type lineMsg string
type tickMsg struct{}
type doneMsg struct{ err error }

type model struct {
	// config
	scriptPath string

	// inputs
	email   textinput.Model
	apiKey  textinput.Model
	domArea textarea.Model

	// parsed/normalized preview
	normalized []string

	// run state
	stage    step
	spinner  spinner.Model
	viewport viewport.Model
	lines    chan string
	done     chan error

	// subprocess
	ctx    context.Context
	cancel context.CancelFunc
	cmd    *exec.Cmd

	// output cache
	logBuf bytes.Buffer

	// styles
	titleStyle lipgloss.Style
	helpStyle  lipgloss.Style
	okStyle    lipgloss.Style
	errStyle   lipgloss.Style
}

func initialModel(scriptPath string) model {
	email := textinput.New()
	email.Placeholder = "you@example.com"
	email.Prompt = "Cloudways email: "
	email.Focus()

	if v := os.Getenv("CW_EMAIL"); v != "" {
		email.SetValue(v)
	}

	api := textinput.New()
	api.Prompt = "Cloudways API key: "
	api.EchoMode = textinput.EchoPassword
	api.EchoCharacter = '•'
	if v := os.Getenv("CW_API_KEY"); v != "" {
		api.SetValue(v)
	}

	dom := textarea.New()
	dom.Placeholder = "Paste domain(s) here (any format). Press Ctrl+D when done."
	dom.ShowLineNumbers = false
	dom.SetHeight(10)
	dom.CharLimit = 0
	if v := os.Getenv("CW_DOMAINS"); v != "" {
		dom.SetValue(v)
	}

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	vp := viewport.New(100, 18)
	vp.YPosition = 0

	return model{
		scriptPath: scriptPath,
		email:      email,
		apiKey:     api,
		domArea:    dom,
		stage:      stepEmail,
		spinner:    sp,
		viewport:   vp,
		lines:      make(chan string, 4096),
		done:       make(chan error, 1),

		titleStyle: lipgloss.NewStyle().Bold(true),
		helpStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		okStyle:    lipgloss.NewStyle().Foreground(lipgloss.Color("42")),
		errStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
	}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) View() string {
	switch m.stage {
	case stepEmail:
		return m.titleStyle.Render("CW Backup — Bubble Tea TUI") + "\n\n" +
			m.email.View() + "\n\n" +
			m.helpStyle.Render("Enter to continue, q to quit.")
	case stepAPI:
		return m.titleStyle.Render("CW Backup — Bubble Tea TUI") + "\n\n" +
			m.apiKey.View() + "\n\n" +
			m.helpStyle.Render("Enter to continue, q to quit, Esc to go back.")
	case stepDomains:
		return m.titleStyle.Render("CW Backup — Paste Domains") + "\n\n" +
			m.domArea.View() + "\n\n" +
			m.helpStyle.Render("Ctrl+D when done. q to quit, Esc to go back.")
	case stepConfirm:
		var list string
		if len(m.normalized) == 0 {
			list = m.errStyle.Render("No valid domains parsed.")
		} else {
			list = "  • " + strings.Join(m.normalized, "\n  • ")
		}
		return m.titleStyle.Render("Confirm") + "\n\n" +
			fmt.Sprintf("Email: %s\nDomains (%d):\n%s\n\n", m.email.Value(), len(m.normalized), list) +
			m.helpStyle.Render("[y] run  [n] edit domains  [b] back")
	case stepRunning:
		header := m.titleStyle.Render("Running backup… ") + m.spinner.View()
		return header + "\n\n" + m.viewport.View() + "\n\n" + m.helpStyle.Render("q/ctrl+c to cancel. PgUp/PgDn to scroll.")
	case stepDone:
		header := m.titleStyle.Render("Finished")
		return header + "\n\n" + m.viewport.View() + "\n\n" + m.helpStyle.Render("Press q to exit.")
	default:
		return ""
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.stage {
	case stepEmail:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "enter":
				if strings.TrimSpace(m.email.Value()) == "" {
					return m, nil
				}
				m.stage = stepAPI
				m.apiKey.Focus()
				return m, nil
			}
		}
		var cmd tea.Cmd
		m.email, cmd = m.email.Update(msg)
		return m, cmd

	case stepAPI:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "esc":
				m.stage = stepEmail
				m.email.Focus()
				return m, nil
			case "enter":
				if strings.TrimSpace(m.apiKey.Value()) == "" {
					return m, nil
				}
				m.stage = stepDomains
				m.domArea.Focus()
				return m, nil
			}
		}
		var cmd tea.Cmd
		m.apiKey, cmd = m.apiKey.Update(msg)
		return m, cmd

	case stepDomains:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "esc":
				m.stage = stepAPI
				m.apiKey.Focus()
				return m, nil
			case "ctrl+d":
				m.normalized = normalizeDomains(m.domArea.Value())
				m.stage = stepConfirm
				return m, nil
			}
		}
		var cmd tea.Cmd
		m.domArea, cmd = m.domArea.Update(msg)
		return m, cmd

	case stepConfirm:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "b":
				m.stage = stepAPI
				m.apiKey.Focus()
				return m, nil
			case "n":
				m.stage = stepDomains
				m.domArea.Focus()
				return m, nil
			case "y":
				if err := assertExecutable(m.scriptPath); err != nil {
					m.viewport.SetContent(m.errStyle.Render(err.Error()))
					m.stage = stepDone
					return m, nil
				}
				m.stage = stepRunning
				m.viewport.SetContent("")
				m.ctx, m.cancel = context.WithCancel(context.Background())
				return m, tea.Batch(m.startProcessCmd(), nextTick())
			}
		}
		return m, nil

	case stepRunning:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "q", "ctrl+c":
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Quit
			case "pgdown", " ", "j", "down":
				m.viewport.LineDown(3)
				return m, nil
			case "pgup", "k", "up":
				m.viewport.LineUp(3)
				return m, nil
			}
		case tickMsg:
			// drain lines
			for i := 0; i < 200; i++ {
				select {
				case ln := <-m.lines:
					m.appendLogLine(ln)
				default:
					i = 200
				}
			}
			select {
			case err := <-m.done:
				if err != nil && !errors.Is(err, context.Canceled) {
					m.appendLogLine(m.errStyle.Render("Process error: " + err.Error()))
				} else {
					m.appendLogLine(m.okStyle.Render("Done."))
				}
				m.stage = stepDone
				return m, nil
			default:
				return m, nextTick()
			}
		default:
			// spinner tick
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	case stepDone:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			if msg.String() == "q" || msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
		}
		return m, nil
	}
	return m, nil
}

func (m *model) appendLogLine(s string) {
	if s == "" {
		return
	}
	if m.logBuf.Len() > 0 {
		m.logBuf.WriteByte('\n')
	}
	m.logBuf.WriteString(s)
	m.viewport.SetContent(m.logBuf.String())
	m.viewport.GotoBottom()
}

func nextTick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *model) startProcessCmd() tea.Cmd {
	cmd := exec.CommandContext(m.ctx, "/bin/bash", m.scriptPath)

	stdin, _ := cmd.StdinPipe()
	pipeR, pipeW := io.Pipe()
	cmd.Stdout = pipeW
	cmd.Stderr = pipeW

	// stream output
	go func() {
		sc := bufio.NewScanner(pipeR)
		buf := make([]byte, 0, 128*1024)
		sc.Buffer(buf, 10*1024*1024)
		for sc.Scan() {
			m.lines <- sc.Text()
		}
		_ = pipeR.Close()
	}()

	rawDomains := m.domArea.Value()

	return func() tea.Msg {
		if err := cmd.Start(); err != nil {
			return doneMsg{err: err}
		}

		// The bash script expects: email, api key, domains... then EOF
		_, _ = io.WriteString(stdin, strings.TrimSpace(m.email.Value())+"\n")
		_, _ = io.WriteString(stdin, strings.TrimSpace(m.apiKey.Value())+"\n")
		if !strings.HasSuffix(rawDomains, "\n") {
			rawDomains += "\n"
		}
		_, _ = io.WriteString(stdin, rawDomains)
		_ = stdin.Close()

		err := cmd.Wait()
		_ = pipeW.Close()
		m.cmd = cmd
		m.cancel = nil
		return doneMsg{err: err}
	}
}

func normalizeDomains(s string) []string {
	s = strings.ToLower(s)
	repls := []string{"\t", " ", ",", " ", ";", " ", "|", " ", "\r", " ", "https://", "", "http://", "", "/", " "}
	r := strings.NewReplacer(repls...)
	s = r.Replace(s)

	re := regexp.MustCompile(`([a-z0-9-]+\.)+[a-z]{2,}`)
	seen := map[string]struct{}{}
	var out []string
	for _, tok := range strings.Fields(s) {
		if m := re.FindString(tok); m != "" {
			m = strings.TrimPrefix(m, "www.")
			if _, ok := seen[m]; !ok {
				seen[m] = struct{}{}
				out = append(out, m)
			}
		}
	}
	return out
}

func assertExecutable(p string) error {
	if p == "" {
		return fmt.Errorf("script path is empty")
	}
	fi, err := os.Stat(p)
	if err != nil {
		return fmt.Errorf("script not found at %s: %w", p, err)
	}
	if fi.IsDir() {
		return fmt.Errorf("%s is a directory", p)
	}
	if fi.Mode()&0111 == 0 {
		_ = os.Chmod(p, 0755)
	}
	return nil
}

func main() {
	// script path via env, arg, or default
	script := os.Getenv("CW_BACKUP_SCRIPT")
	if script == "" {
		if len(os.Args) > 1 {
			script = os.Args[1]
		} else {
			script = defaultScript
		}
	}
	if !filepath.IsAbs(script) {
		if abs, err := filepath.Abs(script); err == nil {
			script = abs
		}
	}

	p := tea.NewProgram(initialModel(script), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
}
