// Package mailer provides SMTP email notifications.
package mailer

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"
	"time"

	"github.com/YaoxuanZhang/bit-rot-detector/internal/domain"
)

const separatorWidth = 40

// Config holds SMTP configuration.
type Config struct {
	// Host is the SMTP server hostname (e.g. "mail.smtp2go.com").
	Host string

	// Port is the SMTP server port (typically 587 for STARTTLS).
	Port int

	// Username is the SMTP authentication username.
	Username string

	// Password is the SMTP authentication password.
	Password string

	// Sender is the RFC 5321 envelope sender address.
	Sender string

	// Recipient is the RFC 5321 envelope recipient address.
	Recipient string

	// NotifyOnSuccess controls whether an email is sent when the run
	// completes without errors or corruption.  Failure/corruption emails are
	// always sent regardless of this setting.
	NotifyOnSuccess bool
}

// Mailer sends email notifications.
type Mailer struct {
	cfg Config
}

// New creates a new Mailer.
func New(cfg Config) *Mailer {
	return &Mailer{cfg: cfg}
}

// SendUnifiedReport composes and sends the end-of-run report email.
func (m *Mailer) SendUnifiedReport(
	syncResults []SyncEntry,
	scrubResults []ScrubEntry,
	driveHealthResults []*domain.DriveHealth,
	errors []string,
	duration time.Duration,
) {
	hasBitRot := false
	for _, e := range scrubResults {
		if len(e.Result.FilesCorrupted) > 0 {
			hasBitRot = true
			break
		}
	}
	hasErrors := len(errors) > 0

	var status string
	switch {
	case hasBitRot:
		status = "CRITICAL"
	case hasErrors:
		status = "WARNING"
	default:
		status = "SUCCESS"
	}

	if status == "SUCCESS" && !m.cfg.NotifyOnSuccess {
		slog.Info("skipping email notification (success notifications disabled)")
		return
	}

	body := m.buildBody(status, syncResults, scrubResults, driveHealthResults, errors, duration)
	subject := m.buildSubject(status, syncResults, scrubResults, driveHealthResults)

	if err := m.send(subject, body); err != nil {
		slog.Error("failed to send email", "err", err)
	}
}

// SendTestEmail verifies SMTP connectivity.
func (m *Mailer) SendTestEmail() error {
	subject := "Bit Rot Detector – Test Email"
	body := fmt.Sprintf(`This is a test email from Bit Rot Detector.

If you received this, your SMTP configuration is working correctly.

  SMTP Host:  %s
  SMTP Port:  %d
  Sender:     %s
  Recipient:  %s
`, m.cfg.Host, m.cfg.Port, m.cfg.Sender, m.cfg.Recipient)

	return m.send(subject, body)
}

// SyncEntry associates a drive name with its [domain.SyncResult].
type SyncEntry struct {
	Drive  string
	Result *domain.SyncResult
}

// ScrubEntry associates a drive name with its [domain.ScrubResult].
type ScrubEntry struct {
	Drive  string
	Result *domain.ScrubResult
}

// ── private helpers ──────────────────────────────────────────────────────────

func (m *Mailer) send(subject, body string) error {
	addr := fmt.Sprintf("%s:%d", m.cfg.Host, m.cfg.Port)

	// Plain-text message with minimal headers.
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s",
		m.cfg.Sender, m.cfg.Recipient, subject, body)

	auth := smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)

	// Try STARTTLS first, then plain SMTP.
	client, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: m.cfg.Host}); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}

	if m.cfg.Username != "" {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err := client.Mail(m.cfg.Sender); err != nil {
		return fmt.Errorf("smtp MAIL FROM: %w", err)
	}
	if err := client.Rcpt(m.cfg.Recipient); err != nil {
		return fmt.Errorf("smtp RCPT TO: %w", err)
	}
	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}
	if _, err := fmt.Fprint(wc, msg); err != nil {
		return fmt.Errorf("smtp write body: %w", err)
	}
	return wc.Close()
}

func sep() string { return strings.Repeat("=", separatorWidth) }

func header(title string) string {
	return sep() + "\n" + title + "\n" + sep() + "\n"
}

func formatDuration(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func (m *Mailer) buildBody(
	status string,
	syncResults []SyncEntry,
	scrubResults []ScrubEntry,
	driveHealthResults []*domain.DriveHealth,
	errors []string,
	duration time.Duration,
) string {
	var sb strings.Builder

	sb.WriteString(header("PROGRAM REPORT"))
	sb.WriteString(fmt.Sprintf("Status:    %s\n", status))
	sb.WriteString(fmt.Sprintf("Duration:  %s\n", formatDuration(duration)))
	sb.WriteString(fmt.Sprintf("Drives:    %d\n\n", len(driveHealthResults)))

	// Drive health.
	if len(driveHealthResults) > 0 {
		sb.WriteString(header("DRIVE HEALTH"))
		for _, h := range driveHealthResults {
			used := formatBytes(h.UsedSpace)
			total := formatBytes(h.TotalSpace)
			var pct float64
			if h.TotalSpace > 0 {
				pct = float64(h.UsedSpace) / float64(h.TotalSpace) * 100
			}
			temp := "N/A"
			if h.Temperature != nil {
				temp = fmt.Sprintf("%d°C", *h.Temperature)
			}
			sb.WriteString(fmt.Sprintf("  %-20s %s / %s (%.0f%%)  Temp: %s  SMART: %s\n",
				h.DriveName+":", used, total, pct, temp, h.SmartStatus))
		}
		sb.WriteString("\n")
	}

	// Critical alert.
	hasBitRot := false
	for _, e := range scrubResults {
		if len(e.Result.FilesCorrupted) > 0 {
			hasBitRot = true
			break
		}
	}
	if hasBitRot {
		sb.WriteString(header("⚠  CRITICAL ALERT – BIT ROT DETECTED"))
		for _, e := range scrubResults {
			if len(e.Result.FilesCorrupted) == 0 {
				continue
			}
			sb.WriteString(fmt.Sprintf("Drive: %s\n", e.Drive))
			for i, p := range e.Result.FilesCorrupted {
				sb.WriteString(fmt.Sprintf("  %4d. %s\n", i+1, p))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("Recommended actions:\n")
		sb.WriteString("  1. Restore corrupted files from your most recent backup\n")
		sb.WriteString("  2. Verify storage hardware integrity\n")
		sb.WriteString("  3. Run a full disk check (fsck / chkdsk)\n\n")
	}

	// Errors.
	if len(errors) > 0 {
		sb.WriteString(header("ERRORS"))
		for i, e := range errors {
			sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, e))
		}
		sb.WriteString("\n")
	}

	// Sync results.
	if len(syncResults) > 0 {
		sb.WriteString(header("SYNC RESULTS"))
		for _, e := range syncResults {
			if len(syncResults) > 1 {
				sb.WriteString(fmt.Sprintf("Drive: %s\n", e.Drive))
			}
			sb.WriteString(fmt.Sprintf("  Scanned:   %d\n", e.Result.FilesScanned))
			sb.WriteString(fmt.Sprintf("  Added:     %d\n", e.Result.FilesAdded))
			sb.WriteString(fmt.Sprintf("  Modified:  %d\n", e.Result.FilesModified))
			sb.WriteString(fmt.Sprintf("  Moved:     %d\n", e.Result.FilesMoved))
			sb.WriteString(fmt.Sprintf("  Removed:   %d\n", e.Result.FilesRemoved))
			if len(e.Result.Errors) > 0 {
				sb.WriteString(fmt.Sprintf("  Errors:    %d\n", len(e.Result.Errors)))
			}
			sb.WriteString("\n")
		}
	}

	// Scrub results.
	if len(scrubResults) > 0 {
		sb.WriteString(header("SCRUB RESULTS"))
		for _, e := range scrubResults {
			if len(scrubResults) > 1 {
				sb.WriteString(fmt.Sprintf("Drive: %s\n", e.Drive))
			}
			sb.WriteString(fmt.Sprintf("  Validated: %d\n", e.Result.FilesValidated))
			sb.WriteString(fmt.Sprintf("  Corrupted: %d\n", len(e.Result.FilesCorrupted)))
			if len(e.Result.Errors) > 0 {
				sb.WriteString(fmt.Sprintf("  Errors:    %d\n", len(e.Result.Errors)))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString(sep() + "\n")
	return sb.String()
}

func (m *Mailer) buildSubject(
	status string,
	syncResults []SyncEntry,
	scrubResults []ScrubEntry,
	driveHealthResults []*domain.DriveHealth,
) string {
	numDrives := len(driveHealthResults)

	switch status {
	case "CRITICAL":
		total := 0
		for _, e := range scrubResults {
			total += len(e.Result.FilesCorrupted)
		}
		return fmt.Sprintf("BIT ROT DETECTED – %d Corrupted File(s)", total)
	case "WARNING":
		return "Bit Rot Detector – FAILED"
	default:
		if numDrives > 1 {
			return fmt.Sprintf("Bit Rot Detector – %d Drives OK", numDrives)
		}
		hasSync := len(syncResults) > 0
		hasScrub := len(scrubResults) > 0
		switch {
		case hasSync && hasScrub:
			return "Bit Rot Detector – Sync + Scrub OK"
		case hasSync:
			n := 0
			if len(syncResults) > 0 {
				n = syncResults[0].Result.FilesScanned
			}
			return fmt.Sprintf("Bit Rot Detector – %d Files Synced", n)
		default:
			n := 0
			if len(scrubResults) > 0 {
				n = scrubResults[0].Result.FilesValidated
			}
			return fmt.Sprintf("Bit Rot Detector – %d Files Validated", n)
		}
	}
}
