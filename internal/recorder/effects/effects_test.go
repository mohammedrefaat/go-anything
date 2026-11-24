package effects_test

import (
	"bytes"
	"io"
	"net/http"
	"net/smtp"
	"testing"

	"github.com/mohammedrefat/go-anything/internal/recorder/effects"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEffectsManager_HTTPRecording(t *testing.T) {
	mgr, err := effects.NewManager()
	require.NoError(t, err)
	defer mgr.Close()

	// Make outbound HTTP request to mock server
	resp, err := http.Post(mgr.HTTPURL()+"/api/v1/notify", "application/json", bytes.NewBufferString(`{"user_id": 42}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	effs := mgr.Effects()
	require.Len(t, effs, 1)

	eff := effs[0]
	assert.Equal(t, "http", eff.Type)
	assert.Equal(t, "POST", eff.Method)
	assert.Equal(t, "/api/v1/notify", eff.Target)
	assert.NotEmpty(t, eff.BodyHash)
	assert.Equal(t, 1, eff.Order)
}

func TestEffectsManager_SMTPRecording(t *testing.T) {
	mgr, err := effects.NewManager()
	require.NoError(t, err)
	defer mgr.Close()

	// Send an email via mock SMTP server
	addr := mgr.SMTPAddr()
	from := "sender@example.com"
	to := []string{"recipient@example.com"}
	msg := []byte("From: sender@example.com\r\n" +
		"To: recipient@example.com\r\n" +
		"Subject: Verification Code\r\n" +
		"\r\n" +
		"Your code is 123456.\r\n")

	err = smtp.SendMail(addr, nil, from, to, msg)
	require.NoError(t, err)

	effs := mgr.Effects()
	require.Len(t, effs, 1)

	eff := effs[0]
	assert.Equal(t, "smtp", eff.Type)
	assert.Equal(t, "sender@example.com", eff.EnvelopeFrom)
	assert.Equal(t, []string{"recipient@example.com"}, eff.EnvelopeTo)
	assert.Equal(t, "Verification Code", eff.Subject)
	assert.NotEmpty(t, eff.BodyHash)
}

func TestEffectsManager_MultipleEffectsOrder(t *testing.T) {
	mgr, err := effects.NewManager()
	require.NoError(t, err)
	defer mgr.Close()

	// 1. HTTP call
	resp, err := http.Get(mgr.HTTPURL() + "/status")
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	// 2. SMTP send
	msg := []byte("Subject: Alert\r\n\r\nAlert body\r\n")
	require.NoError(t, smtp.SendMail(mgr.SMTPAddr(), nil, "a@b.com", []string{"c@d.com"}, msg))

	// 3. Another HTTP call
	resp2, err := http.Get(mgr.HTTPURL() + "/done")
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp2.Body)
	resp2.Body.Close()

	effs := mgr.Effects()
	require.Len(t, effs, 3)
	assert.Equal(t, 1, effs[0].Order)
	assert.Equal(t, "http", effs[0].Type)
	assert.Equal(t, 2, effs[1].Order)
	assert.Equal(t, "smtp", effs[1].Type)
	assert.Equal(t, 3, effs[2].Order)
	assert.Equal(t, "http", effs[2].Type)
}
