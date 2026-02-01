"""Unit tests for mailer module."""

from unittest.mock import Mock, patch

import pytest

from bit_rot.mailer import EmailConfig, Mailer
from bit_rot.scanner import ScrubResult, SyncResult


class TestMailer:
    """Tests for Mailer class."""

    @pytest.fixture
    def mailer(self, mock_email_config: EmailConfig) -> Mailer:
        """Create a Mailer instance for testing."""
        return Mailer(mock_email_config)

    @patch("bit_rot.mailer.smtplib.SMTP")
    def test_send_notification_success(self, mock_smtp, mailer: Mailer):
        """Test successful email sending."""
        # Mock SMTP connection
        mock_server = Mock()
        mock_smtp.return_value.__enter__.return_value = mock_server

        result = mailer.send_notification("Test Subject", "Test Body")

        assert result is True
        mock_server.starttls.assert_called_once()
        mock_server.login.assert_called_once()
        mock_server.send_message.assert_called_once()

    @patch("bit_rot.mailer.smtplib.SMTP")
    def test_send_notification_smtp_failure(self, mock_smtp, mailer: Mailer):
        """Test email sending failure."""
        # Mock SMTP failure
        mock_smtp.side_effect = Exception("SMTP Error")

        result = mailer.send_notification("Test Subject", "Test Body")

        assert result is False

    def test_format_duration(self):
        """Test duration formatting."""
        assert Mailer._format_duration(45) == "45 seconds"
        assert Mailer._format_duration(125) == "2 minutes 5 seconds"
        assert Mailer._format_duration(3725) == "1 hours 2 minutes 5 seconds"

    def test_build_sync_section_single_drive(self):
        """Test building sync section for single drive."""
        sync_result = SyncResult(
            files_scanned=100,
            files_added=10,
            files_modified=5,
            files_moved=2,
            files_removed=1,
            errors=[],
        )

        lines = Mailer._build_sync_section(
            [("TestDrive", sync_result)], include_drive_header=True
        )

        # Should contain the results
        assert any("100" in line for line in lines)
        assert any("10" in line for line in lines)
        # Should not show totals for single drive
        assert not any("SYNC TOTALS" in line for line in lines)

    def test_build_sync_section_multiple_drives(self):
        """Test building sync section for multiple drives."""
        sync_result1 = SyncResult(
            files_scanned=100,
            files_added=10,
            files_modified=5,
            files_moved=2,
            files_removed=1,
            errors=[],
        )
        sync_result2 = SyncResult(
            files_scanned=200,
            files_added=20,
            files_modified=10,
            files_moved=4,
            files_removed=2,
            errors=[],
        )

        lines = Mailer._build_sync_section(
            [("WD_Elements", sync_result1), ("Seagate_Backup", sync_result2)],
            include_drive_header=True,
        )

        # Should show drive headers
        assert any("WD_Elements" in line for line in lines)
        assert any("Seagate_Backup" in line for line in lines)
        # Should show totals
        assert any("SYNC TOTALS" in line for line in lines)
        assert any("300" in line for line in lines)  # Total scanned

    def test_build_scrub_section(self):
        """Test building scrub section."""
        scrub_result = ScrubResult(
            files_validated=500,
            files_corrupted=[],
            errors=[],
        )

        lines = Mailer._build_scrub_section(
            [("TestDrive", scrub_result)], include_drive_header=True
        )

        assert any("500" in line for line in lines)
        assert any("SCRUB RESULTS" in line for line in lines)

    @patch("bit_rot.mailer.smtplib.SMTP")
    def test_send_unified_report_success_enabled(self, mock_smtp, mailer: Mailer):
        """Test unified report when success (and notification enabled)."""
        mock_server = Mock()
        mock_smtp.return_value.__enter__.return_value = mock_server

        # Ensure enabled
        mailer.config.notify_on_success = True

        mailer.send_unified_report(
            sync_results=[],
            scrub_results=[],
            drive_health_results=[],
            errors=[],
            duration_seconds=10.0,
        )

        mock_server.send_message.assert_called_once()

    @patch("bit_rot.mailer.smtplib.SMTP")
    def test_send_unified_report_success_disabled(self, mock_smtp, mailer: Mailer):
        """Test unified report skipped when success (and notification disabled)."""
        mock_server = Mock()
        mock_smtp.return_value.__enter__.return_value = mock_server

        # Disable success notifications
        mailer.config.notify_on_success = False

        mailer.send_unified_report(
            sync_results=[],
            scrub_results=[],
            drive_health_results=[],
            errors=[],
            duration_seconds=10.0,
        )

        # Should skip
        mock_smtp.assert_not_called()

    @patch("bit_rot.mailer.smtplib.SMTP")
    def test_send_unified_report_failure_always_sends(self, mock_smtp, mailer: Mailer):
        """Test unified report ALWAYS sends on failure even if success-notify disabled."""
        mock_server = Mock()
        mock_smtp.return_value.__enter__.return_value = mock_server

        # Disable success notifications
        mailer.config.notify_on_success = False

        # Simulate error
        mailer.send_unified_report(
            sync_results=[],
            scrub_results=[],
            drive_health_results=[],
            errors=["Critical DB failure"],
            duration_seconds=10.0,
        )

        # Should send because of error
        mock_server.send_message.assert_called_once()

    @patch("bit_rot.mailer.smtplib.SMTP")
    def test_send_unified_report_bit_rot_always_sends(self, mock_smtp, mailer: Mailer):
        """Test unified report ALWAYS sends on bit rot detection."""
        mock_server = Mock()
        mock_smtp.return_value.__enter__.return_value = mock_server

        # Disable success notifications
        mailer.config.notify_on_success = False

        scrub_result = ScrubResult(
            files_validated=10, files_corrupted=["bad_file.txt"], errors=[]
        )

        # Simulate bit rot
        mailer.send_unified_report(
            sync_results=[],
            scrub_results=[("Drive1", scrub_result)],
            drive_health_results=[],
            errors=[],
            duration_seconds=10.0,
        )

        # Should send because of bit rot
        mock_server.send_message.assert_called_once()
        call_args = mock_server.send_message.call_args
        message = call_args[0][0]
        assert "BIT ROT DETECTED" in message["Subject"]
