package effects

import (
	"fmt"
	"net/url"
	"sync"

	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

// Manager coordinates mock HTTP and SMTP recording services during test execution.
type Manager struct {
	orderSeq   int64
	effects    []adapterkit.Effect
	mu         sync.Mutex
	httpServer *HTTPServer
	smtpServer *SMTPServer
}

// NewManager initializes and starts the mock recording listeners.
func NewManager() (*Manager, error) {
	m := &Manager{
		effects: make([]adapterkit.Effect, 0),
	}

	m.httpServer = NewHTTPServer(&m.orderSeq, &m.effects, &m.mu)

	smtp, err := NewSMTPServer(&m.orderSeq, &m.effects, &m.mu)
	if err != nil {
		m.httpServer.Close()
		return nil, fmt.Errorf("failed to start SMTP mock server: %w", err)
	}
	m.smtpServer = smtp

	return m, nil
}

// HTTPURL returns the base URL of the HTTP recording server.
func (m *Manager) HTTPURL() string {
	if m.httpServer == nil {
		return ""
	}
	return m.httpServer.URL()
}

// SMTPAddr returns the host:port of the SMTP recording server.
func (m *Manager) SMTPAddr() string {
	if m.smtpServer == nil {
		return ""
	}
	return m.smtpServer.Addr()
}

// SMTPPort returns the port integer of the SMTP recording server.
func (m *Manager) SMTPPort() int {
	if m.smtpServer == nil {
		return 0
	}
	return m.smtpServer.Port()
}

// Env returns standard environment variables to route application network calls to mock servers.
func (m *Manager) Env() map[string]string {
	env := make(map[string]string)
	if m.httpServer != nil {
		env["HTTP_PROXY"] = m.httpServer.URL()
		env["HTTPS_PROXY"] = m.httpServer.URL()
		env["MOCK_HTTP_URL"] = m.httpServer.URL()
		env["TARGET_HTTP_URL"] = m.httpServer.URL()
	}
	if m.smtpServer != nil {
		env["SMTP_HOST"] = "127.0.0.1"
		env["SMTP_PORT"] = fmt.Sprintf("%d", m.smtpServer.Port())
		env["SMTP_SERVER"] = m.smtpServer.Addr()
		env["MAIL_SERVER"] = "127.0.0.1"
		env["MAIL_PORT"] = fmt.Sprintf("%d", m.smtpServer.Port())
	}
	return env
}

// Effects returns a copy of all recorded side effects.
func (m *Manager) Effects() []adapterkit.Effect {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]adapterkit.Effect, len(m.effects))
	copy(out, m.effects)
	return out
}

// Reset clears the recorded side effects.
func (m *Manager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.effects = make([]adapterkit.Effect, 0)
	m.orderSeq = 0
}

// Close terminates all mock listeners.
func (m *Manager) Close() {
	if m.httpServer != nil {
		m.httpServer.Close()
	}
	if m.smtpServer != nil {
		m.smtpServer.Close()
	}
}

// NormalizeTarget normalizes an HTTP target URL by stripping ephemeral host/port if needed.
func NormalizeTarget(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	// Return normalized path + query
	res := u.Path
	if u.RawQuery != "" {
		res += "?" + u.RawQuery
	}
	if res == "" {
		return "/"
	}
	return res
}
