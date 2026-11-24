package effects

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// SMTPServer runs a mock SMTP listener on loopback to capture email side effects.
type SMTPServer struct {
	listener net.Listener
	orderSeq *int64
	effects  *[]adapterkit.Effect
	mu       *sync.Mutex
	closed   int32
}

// NewSMTPServer initializes and starts a mock SMTP server on an ephemeral loopback port.
func NewSMTPServer(orderSeq *int64, effects *[]adapterkit.Effect, mu *sync.Mutex) (*SMTPServer, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to bind mock SMTP listener: %w", err)
	}

	s := &SMTPServer{
		listener: l,
		orderSeq: orderSeq,
		effects:  effects,
		mu:       mu,
	}

	go s.acceptLoop()
	return s, nil
}

// Addr returns the host:port address of the mock SMTP server.
func (s *SMTPServer) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Port returns the port integer.
func (s *SMTPServer) Port() int {
	if s.listener == nil {
		return 0
	}
	addr, ok := s.listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0
	}
	return addr.Port
}

// Close stops the mock SMTP server.
func (s *SMTPServer) Close() {
	if atomic.CompareAndSwapInt32(&s.closed, 0, 1) {
		if s.listener != nil {
			_ = s.listener.Close()
		}
	}
}

func (s *SMTPServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if atomic.LoadInt32(&s.closed) == 1 {
				return
			}
			continue
		}
		go s.handleConnection(conn)
	}
}

func (s *SMTPServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	// SMTP greeting
	_, _ = writer.WriteString("220 127.0.0.1 Go-Anything Mock SMTP Service Ready\r\n")
	_ = writer.Flush()

	var envelopeFrom string
	var envelopeTo []string
	var inData bool
	var dataLines []string

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}

		trimmed := strings.TrimRight(line, "\r\n")

		if inData {
			if trimmed == "." {
				// End of DATA block
				inData = false
				s.recordEmail(envelopeFrom, envelopeTo, dataLines)

				_, _ = writer.WriteString("250 2.0.0 OK: message queued\r\n")
				_ = writer.Flush()

				envelopeFrom = ""
				envelopeTo = nil
				dataLines = nil
				continue
			}
			dataLines = append(dataLines, trimmed)
			continue
		}

		upper := strings.ToUpper(trimmed)
		switch {
		case strings.HasPrefix(upper, "HELO") || strings.HasPrefix(upper, "EHLO"):
			_, _ = writer.WriteString("250-127.0.0.1 Hello\r\n250 8BITMIME\r\n")
			_ = writer.Flush()

		case strings.HasPrefix(upper, "MAIL FROM:"):
			envelopeFrom = extractEmail(trimmed[10:])
			_, _ = writer.WriteString("250 2.1.0 Sender OK\r\n")
			_ = writer.Flush()

		case strings.HasPrefix(upper, "RCPT TO:"):
			envelopeTo = append(envelopeTo, extractEmail(trimmed[8:]))
			_, _ = writer.WriteString("250 2.1.5 Recipient OK\r\n")
			_ = writer.Flush()

		case upper == "DATA":
			inData = true
			_, _ = writer.WriteString("354 Start mail input; end with <CRLF>.<CRLF>\r\n")
			_ = writer.Flush()

		case upper == "RSET":
			envelopeFrom = ""
			envelopeTo = nil
			dataLines = nil
			_, _ = writer.WriteString("250 2.0.0 Reset state\r\n")
			_ = writer.Flush()

		case upper == "NOOP":
			_, _ = writer.WriteString("250 2.0.0 OK\r\n")
			_ = writer.Flush()

		case upper == "QUIT":
			_, _ = writer.WriteString("221 2.0.0 Bye\r\n")
			_ = writer.Flush()
			return

		default:
			_, _ = writer.WriteString("500 5.5.1 Command unrecognized\r\n")
			_ = writer.Flush()
		}
	}
}

func extractEmail(s string) string {
	s = strings.TrimSpace(s)
	start := strings.Index(s, "<")
	end := strings.Index(s, ">")
	if start != -1 && end > start {
		return strings.TrimSpace(s[start+1 : end])
	}
	// Fallback: take first whitespace-delimited word
	fields := strings.Fields(s)
	if len(fields) > 0 {
		return strings.Trim(fields[0], "<>")
	}
	return s
}

func (s *SMTPServer) recordEmail(from string, to []string, lines []string) {
	fullContent := strings.Join(lines, "\r\n")
	hash := sha256.Sum256([]byte(fullContent))
	bodyHash := hex.EncodeToString(hash[:])

	subject := ""
	for _, l := range lines {
		if strings.HasPrefix(strings.ToLower(l), "subject:") {
			subject = strings.TrimSpace(l[8:])
			break
		}
	}

	order := int(atomic.AddInt64(s.orderSeq, 1))

	eff := adapterkit.Effect{
		Type:         "smtp",
		Order:        order,
		Target:       s.Addr(),
		EnvelopeFrom: from,
		EnvelopeTo:   to,
		Subject:      subject,
		BodyHash:     bodyHash,
		Count:        1,
	}

	s.mu.Lock()
	*s.effects = append(*s.effects, eff)
	s.mu.Unlock()
}
