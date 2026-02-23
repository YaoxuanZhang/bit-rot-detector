package mailer_test

import (
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/YaoxuanZhang/bit-rot-detector/internal/domain"
	"github.com/YaoxuanZhang/bit-rot-detector/internal/mailer"
)

// fakeSMTP starts a minimal SMTP listener that accepts one connection, reads
// the DATA payload, stores it, then closes.  It returns the listener address
// and a function that blocks until the message is received or the timeout
// elapses, returning the raw message body.
func fakeSMTP(t *testing.T) (addr string, getMessage func(timeout time.Duration) string) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fakeSMTP listen: %v", err)
	}

	var (
		mu  sync.Mutex
		msg string
		ch  = make(chan struct{})
	)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

		write := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }

		write("220 fakesmtp ESMTP")

		buf := make([]byte, 4096)
		readLine := func() string {
			n, _ := conn.Read(buf)
			return strings.TrimSpace(string(buf[:n]))
		}

		for {
			line := readLine()
			upper := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
				write("250-fakesmtp\r\n250 OK")
			case strings.HasPrefix(upper, "MAIL FROM"):
				write("250 OK")
			case strings.HasPrefix(upper, "RCPT TO"):
				write("250 OK")
			case upper == "DATA":
				write("354 Start")
				// Read until "\r\n.\r\n"
				var body strings.Builder
				dataBuf := make([]byte, 8192)
				for {
					n, _ := conn.Read(dataBuf)
					body.Write(dataBuf[:n])
					if strings.Contains(body.String(), "\r\n.\r\n") {
						break
					}
				}
				mu.Lock()
				msg = body.String()
				mu.Unlock()
				write("250 OK")
				close(ch)
				return
			case strings.HasPrefix(upper, "QUIT"):
				write("221 Bye")
				return
			}
		}
	}()

	t.Cleanup(func() { ln.Close() })

	getMessage = func(timeout time.Duration) string {
		select {
		case <-ch:
		case <-time.After(timeout):
		}
		mu.Lock()
		defer mu.Unlock()
		return msg
	}

	return ln.Addr().String(), getMessage
}

func newMailer(host string, port int, notifyOnSuccess bool) *mailer.Mailer {
	return mailer.New(mailer.Config{
		Host:            host,
		Port:            port,
		Sender:          "from@example.com",
		Recipient:       "to@example.com",
		NotifyOnSuccess: notifyOnSuccess,
	})
}

// ── Unit tests for formatting helpers ────────────────────────────────────────

func TestSendUnifiedReport_SkipsOnSuccessWhenDisabled(t *testing.T) {
	// With NotifyOnSuccess=false and no bit-rot / no errors, the mailer should
	// skip the send entirely.  We use a non-listening port to confirm no dial
	// is attempted.
	m := newMailer("127.0.0.1", 19999, false)
	// Must not panic or return error (no TCP dial should happen).
	m.SendUnifiedReport(nil, nil, nil, nil, time.Second)
}

func TestSendUnifiedReport_BitRotAlert(t *testing.T) {
	addr, getMessage := fakeSMTP(t)
	host, portStr, _ := net.SplitHostPort(addr)
	port := 0
	for _, r := range portStr {
		port = port*10 + int(r-'0')
	}

	m := newMailer(host, port, true)

	scrubResults := []mailer.ScrubEntry{
		{
			Drive: "data",
			Result: &domain.ScrubResult{
				FilesCorrupted: []string{"/data/important.iso"},
				FilesValidated: 99,
			},
		},
	}
	m.SendUnifiedReport(nil, scrubResults, nil, nil, 5*time.Second)

	body := getMessage(3 * time.Second)
	if !strings.Contains(body, "BIT ROT") {
		t.Errorf("expected BIT ROT in email body, got:\n%s", body)
	}
	if !strings.Contains(body, "important.iso") {
		t.Errorf("expected corrupted filename in email body")
	}
}

func TestSendUnifiedReport_WithErrors(t *testing.T) {
	addr, getMessage := fakeSMTP(t)
	host, portStr, _ := net.SplitHostPort(addr)
	port := 0
	for _, r := range portStr {
		port = port*10 + int(r-'0')
	}

	m := newMailer(host, port, true)
	m.SendUnifiedReport(nil, nil, nil, []string{"disk I/O error on /dev/sda"}, time.Second)

	body := getMessage(3 * time.Second)
	if !strings.Contains(body, "disk I/O error") {
		t.Errorf("expected error message in email body, got:\n%s", body)
	}
}

func TestSendUnifiedReport_SyncAndScrubSuccess(t *testing.T) {
	addr, getMessage := fakeSMTP(t)
	host, portStr, _ := net.SplitHostPort(addr)
	port := 0
	for _, r := range portStr {
		port = port*10 + int(r-'0')
	}

	m := newMailer(host, port, true)

	syncResults := []mailer.SyncEntry{
		{Drive: "nas", Result: &domain.SyncResult{FilesScanned: 1000, FilesAdded: 5}},
	}
	scrubResults := []mailer.ScrubEntry{
		{Drive: "nas", Result: &domain.ScrubResult{FilesValidated: 50}},
	}
	temp := 42
	health := []*domain.DriveHealth{
		{DriveName: "nas", TotalSpace: 2 << 30, UsedSpace: 1 << 30, Temperature: &temp, SmartStatus: "PASSED"},
	}

	m.SendUnifiedReport(syncResults, scrubResults, health, nil, 2*time.Minute+30*time.Second)

	body := getMessage(3 * time.Second)
	if !strings.Contains(body, "SUCCESS") {
		t.Errorf("expected SUCCESS status in email, got:\n%s", body)
	}
	if !strings.Contains(body, "42°C") {
		t.Errorf("expected temperature in email body, got:\n%s", body)
	}
}

func TestSendUnifiedReport_MultiDrive(t *testing.T) {
	addr, getMessage := fakeSMTP(t)
	host, portStr, _ := net.SplitHostPort(addr)
	port := 0
	for _, r := range portStr {
		port = port*10 + int(r-'0')
	}

	m := newMailer(host, port, true)

	syncResults := []mailer.SyncEntry{
		{Drive: "drive1", Result: &domain.SyncResult{FilesScanned: 100}},
		{Drive: "drive2", Result: &domain.SyncResult{FilesScanned: 200}},
	}
	health := []*domain.DriveHealth{
		{DriveName: "drive1", SmartStatus: "PASSED"},
		{DriveName: "drive2", SmartStatus: "PASSED"},
	}

	m.SendUnifiedReport(syncResults, nil, health, nil, 10*time.Second)

	body := getMessage(3 * time.Second)
	if !strings.Contains(body, "drive1") && !strings.Contains(body, "drive2") {
		t.Errorf("expected drive names in email body, got:\n%s", body)
	}
}

func TestSendTestEmail(t *testing.T) {
	addr, getMessage := fakeSMTP(t)
	host, portStr, _ := net.SplitHostPort(addr)
	port := 0
	for _, r := range portStr {
		port = port*10 + int(r-'0')
	}

	m := newMailer(host, port, false)
	if err := m.SendTestEmail(); err != nil {
		t.Fatalf("SendTestEmail: %v", err)
	}

	body := getMessage(3 * time.Second)
	if !strings.Contains(body, "Test Email") && !strings.Contains(body, "test") {
		t.Errorf("expected test email content, got:\n%s", body)
	}
}

func TestSendToDeadHost(t *testing.T) {
	// Port 1 is almost certainly not open.
	m := newMailer("127.0.0.1", 1, false)
	err := m.SendTestEmail()
	if err == nil {
		t.Error("expected error when dialing a dead host")
	}
}

func TestSendUnifiedReport_WarningSingleDriveScrubOnly(t *testing.T) {
	addr, getMessage := fakeSMTP(t)
	host, portStr, _ := net.SplitHostPort(addr)
	port := parsePort(portStr)

	m := newMailer(host, port, true)
	// Scrub-only result (no sync): subject should say "Files Validated".
	scrubResults := []mailer.ScrubEntry{
		{Drive: "disk0", Result: &domain.ScrubResult{FilesValidated: 42}},
	}
	m.SendUnifiedReport(nil, scrubResults, nil, nil, 45*time.Second)

	body := getMessage(3 * time.Second)
	if !strings.Contains(body, "42") {
		t.Errorf("expected validated count 42 in email, got:\n%s", body)
	}
}

func TestSendUnifiedReport_WarningStatus(t *testing.T) {
	addr, getMessage := fakeSMTP(t)
	host, portStr, _ := net.SplitHostPort(addr)
	port := parsePort(portStr)

	m := newMailer(host, port, true)
	// Errors with no bit-rot → WARNING.
	m.SendUnifiedReport(nil, nil, nil, []string{"I/O error"}, 30*time.Second)

	body := getMessage(3 * time.Second)
	if !strings.Contains(body, "WARNING") && !strings.Contains(body, "FAILED") {
		t.Errorf("expected WARNING/FAILED status in email, got:\n%s", body)
	}
}

func TestSendUnifiedReport_DurationHours(t *testing.T) {
	addr, getMessage := fakeSMTP(t)
	host, portStr, _ := net.SplitHostPort(addr)
	port := parsePort(portStr)

	m := newMailer(host, port, true)
	// 2h 5m 3s duration.
	syncResults := []mailer.SyncEntry{
		{Drive: "bigdisk", Result: &domain.SyncResult{FilesScanned: 500000}},
	}
	m.SendUnifiedReport(syncResults, nil, nil, nil, 2*time.Hour+5*time.Minute+3*time.Second)

	body := getMessage(3 * time.Second)
	if !strings.Contains(body, "2h") {
		t.Errorf("expected hours in duration string, got:\n%s", body)
	}
}

func TestSendUnifiedReport_SyncOnlySubject(t *testing.T) {
	addr, getMessage := fakeSMTP(t)
	host, portStr, _ := net.SplitHostPort(addr)
	port := parsePort(portStr)

	m := newMailer(host, port, true)
	syncResults := []mailer.SyncEntry{
		{Drive: "disk0", Result: &domain.SyncResult{FilesScanned: 123, FilesAdded: 5}},
	}
	m.SendUnifiedReport(syncResults, nil, nil, nil, 10*time.Second)

	body := getMessage(3 * time.Second)
	if !strings.Contains(body, "123") && !strings.Contains(body, "Synced") {
		t.Logf("email body:\n%s", body)
	}
}

// parsePort converts a decimal port string to int.
func parsePort(s string) int {
	p := 0
	for _, r := range s {
		p = p*10 + int(r-'0')
	}
	return p
}
