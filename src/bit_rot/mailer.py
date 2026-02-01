"""Email notification module for SMTP2GO integration."""

import logging
import smtplib
from dataclasses import dataclass
from email.mime.multipart import MIMEMultipart
from email.mime.text import MIMEText
from typing import Optional

logger = logging.getLogger(__name__)


@dataclass
class EmailConfig:
    """Email configuration settings."""

    smtp_host: str
    smtp_port: int
    smtp_username: str
    smtp_password: str
    sender: str
    recipient: str
    notify_sync_success: bool
    notify_scrub_success: bool
    notify_critical_failures: bool


class Mailer:
    """SMTP2GO email notification manager."""

    def __init__(self, config: EmailConfig):
        """Initialize mailer with configuration.

        Args:
            config: Email configuration settings
        """
        self.config = config

    def send_notification(self, subject: str, body: str) -> bool:
        """Send email notification via SMTP2GO.

        Args:
            subject: Email subject line
            body: Email body content

        Returns:
            True if email sent successfully, False otherwise
        """
        try:
            logger.info(f"Sending email: {subject}")

            # Create message
            msg = MIMEMultipart()
            msg["From"] = self.config.sender
            msg["To"] = self.config.recipient
            msg["Subject"] = subject
            msg.attach(MIMEText(body, "plain"))

            # Connect to SMTP server with STARTTLS
            with smtplib.SMTP(self.config.smtp_host, self.config.smtp_port) as server:
                server.starttls()
                server.login(self.config.smtp_username, self.config.smtp_password)
                server.send_message(msg)

            logger.info("Email sent successfully")
            return True

        except smtplib.SMTPException as e:
            logger.error(f"SMTP error sending email: {e}")
            return False
        except Exception as e:
            logger.error(f"Unexpected error sending email: {e}")
            return False

    def send_test_email(self) -> bool:
        """Send a test email to verify SMTP configuration.

        Returns:
            True if test email sent successfully, False otherwise
        """
        subject = "Bit Rot Detector - Test Email"
        body = """This is a test email from Bit Rot Detector.

If you received this email, your SMTP configuration is working correctly!

Configuration:
- SMTP Host: {}
- SMTP Port: {}
- Sender: {}
- Recipient: {}
""".format(
            self.config.smtp_host,
            self.config.smtp_port,
            self.config.sender,
            self.config.recipient,
        )

        logger.info("Sending test email")
        return self.send_notification(subject, body)

    @staticmethod
    def _format_duration(duration_seconds: float) -> str:
        """Format duration in seconds to human-readable string.

        Args:
            duration_seconds: Duration in seconds

        Returns:
            Formatted duration string
        """
        hours = int(duration_seconds // 3600)
        minutes = int((duration_seconds % 3600) // 60)
        seconds = int(duration_seconds % 60)

        if hours > 0:
            return f"{hours} hours {minutes} minutes {seconds} seconds"
        elif minutes > 0:
            return f"{minutes} minutes {seconds} seconds"
        else:
            return f"{seconds} seconds"

    @staticmethod
    def _build_header(title: str) -> list[str]:
        """Build a header section for emails (24 chars for mobile).

        Args:
            title: Header title text

        Returns:
            List of header lines
        """
        return [
            "=" * 24,
            title,
            "=" * 24,
            "",
        ]

    @staticmethod
    def _build_separator() -> str:
        """Build a separator line for emails (24 chars for mobile).

        Returns:
            Separator string
        """
        return "=" * 24

    @staticmethod
    def _build_sync_section(
        sync_results: list[tuple[str, "SyncResult"]], include_drive_header: bool = True
    ) -> list[str]:
        """Build sync results section.

        Args:
            sync_results: List of (drive_name, SyncResult) tuples
            include_drive_header: Whether to include drive names in output

        Returns:
            List of formatted lines
        """
        from .scanner import SyncResult

        lines = []
        if not sync_results:
            return lines

        # Build header
        header_title = "SYNC RESULTS" + (
            " BY DRIVE" if include_drive_header and len(sync_results) > 1 else ""
        )
        lines.extend(Mailer._build_header(header_title))

        total_scanned = 0
        total_added = 0
        total_modified = 0
        total_moved = 0
        total_removed = 0
        total_errors = 0

        for drive_name, result in sync_results:
            if include_drive_header and len(sync_results) > 1:
                lines.append(f"Drive: {drive_name}")
            lines.append(f"  Files Scanned:  {result.files_scanned:,}")
            lines.append(f"  Added:          {result.files_added:,}")
            lines.append(f"  Modified:       {result.files_modified:,}")
            lines.append(f"  Moved:          {result.files_moved:,}")
            lines.append(f"  Removed:        {result.files_removed:,}")
            if len(result.errors) > 0:
                lines.append(f"  Errors:         {len(result.errors):,}")
            lines.append("")

            total_scanned += result.files_scanned
            total_added += result.files_added
            total_modified += result.files_modified
            total_moved += result.files_moved
            total_removed += result.files_removed
            total_errors += len(result.errors)

        # Only show totals when multiple drives
        if len(sync_results) > 1:
            lines.append("SYNC TOTALS:")
            lines.append(f"  Total Files:    {total_scanned:,}")
            lines.append(f"  Total Added:    {total_added:,}")
            lines.append(f"  Total Modified: {total_modified:,}")
            lines.append(f"  Total Moved:    {total_moved:,}")
            lines.append(f"  Total Removed:  {total_removed:,}")
            if total_errors > 0:
                lines.append(f"  Total Errors:   {total_errors:,}")
            lines.append("")

        return lines

    @staticmethod
    def _build_scrub_section(
        scrub_results: list[tuple[str, "ScrubResult"]],
        include_drive_header: bool = True,
    ) -> list[str]:
        """Build scrub results section.

        Args:
            scrub_results: List of (drive_name, ScrubResult) tuples
            include_drive_header: Whether to include drive names in output

        Returns:
            List of formatted lines
        """
        from .scanner import ScrubResult

        lines = []
        if not scrub_results:
            return lines

        # Build header
        header_title = "SCRUB RESULTS" + (
            " BY DRIVE" if include_drive_header and len(scrub_results) > 1 else ""
        )
        lines.extend(Mailer._build_header(header_title))

        total_validated = 0
        total_corrupted = 0
        total_errors = 0

        for drive_name, result in scrub_results:
            if include_drive_header and len(scrub_results) > 1:
                lines.append(f"Drive: {drive_name}")
            lines.append(f"  Validated:      {result.files_validated:,} files")
            lines.append(f"  Corrupted:      {len(result.files_corrupted):,}")
            if len(result.errors) > 0:
                lines.append(f"  Errors:         {len(result.errors):,}")
            lines.append("")

            total_validated += result.files_validated
            total_corrupted += len(result.files_corrupted)
            total_errors += len(result.errors)

        # Only show totals when multiple drives
        if len(scrub_results) > 1:
            lines.append("SCRUB TOTALS:")
            lines.append(f"  Total Validated: {total_validated:,}")
            lines.append(f"  Total Corrupted: {total_corrupted:,}")
            if total_corrupted == 0:
                lines.append(f"  Status:          [OK] ALL FILES VERIFIED SUCCESSFULLY")
            else:
                lines.append(f"  Status:          [!] CORRUPTION DETECTED")
            lines.append("")

        return lines

    def send_unified_report(
        self,
        sync_results: list[tuple[str, "SyncResult"]],
        scrub_results: list[tuple[str, "ScrubResult"]],
        drive_health_results: list["DriveHealth"],
        errors: list[str],
        duration_seconds: float,
    ) -> None:
        """Send unified report for all program operations.

        This is the ONLY report sent per program run, containing:
        - Drive health stats (usage, temperature, SMART status)
        - Bit rot detection (if any)
        - Errors (if any)
        - Sync results (across all drives)
        - Scrub results (across all drives)

        Args:
            sync_results: List of (drive_name, SyncResult) tuples
            scrub_results: List of (drive_name, ScrubResult) tuples
            drive_health_results: List of DriveHealth objects
            errors: List of error messages
            duration_seconds: Total operation duration in seconds
        """
        from .drive_monitor import format_drive_health

        # Determine overall status
        has_bit_rot = any(len(r.files_corrupted) > 0 for _, r in scrub_results)
        has_errors = len(errors) > 0
        has_sync = len(sync_results) > 0
        has_scrub = len(scrub_results) > 0

        if has_bit_rot:
            status = "CRITICAL"
        elif has_errors:
            status = "WARNING"
        else:
            status = "SUCCESS"

        # Determine operation type
        if has_sync and has_scrub:
            operation_type = "SYNC + SCRUB"
        elif has_sync:
            operation_type = "SYNC"
        elif has_scrub:
            operation_type = "SCRUB"
        else:
            operation_type = "UNKNOWN"

        # Build email body
        lines = []
        lines.extend(self._build_header("PROGRAM REPORT"))
        lines.append(f"Status:    {status}")
        lines.append(f"Operation: {operation_type}")
        lines.append(f"Duration:  {self._format_duration(duration_seconds)}")
        lines.append(f"Drives:    {len(drive_health_results)}")
        lines.append("")

        # Drive health section
        if drive_health_results:
            lines.extend(self._build_header("DRIVES"))
            for health in drive_health_results:
                lines.append(format_drive_health(health))
            lines.append("")

        # Critical alert section (bit rot)
        if has_bit_rot:
            lines.extend(self._build_header("CRITICAL ALERT"))
            lines.append("BIT ROT DETECTED!")
            lines.append("")

            # Collect all corrupted files across all drives
            for drive_name, scrub_result in scrub_results:
                if scrub_result.files_corrupted:
                    lines.append(f"Drive: {drive_name}")
                    for i, filepath in enumerate(scrub_result.files_corrupted, 1):
                        lines.append(f"  {i:4d}. {filepath}")
                    lines.append("")

            lines.append(self._build_separator())
            lines.append("RECOMMENDED ACTIONS")
            lines.append(self._build_separator())
            lines.append("")
            lines.append("1. Restore corrupted files from your most recent backup")
            lines.append("2. Verify the integrity of your storage hardware")
            lines.append("3. Check system logs for hardware errors")
            lines.append("4. Consider running a full disk check (e.g., fsck, chkdsk)")
            lines.append("")

        # Errors section
        if has_errors:
            lines.extend(self._build_header("ERRORS"))
            for i, error in enumerate(errors, 1):
                lines.append(f"{i}. {error}")
            lines.append("")

        # Sync results section
        if has_sync:
            lines.extend(
                self._build_sync_section(sync_results, include_drive_header=True)
            )

        # Scrub results section
        if has_scrub:
            lines.extend(
                self._build_scrub_section(scrub_results, include_drive_header=True)
            )

        lines.append(self._build_separator())

        body = "\n".join(lines)

        # Determine subject based on priority
        if has_bit_rot:
            total_corrupted = sum(len(r.files_corrupted) for _, r in scrub_results)
            subject = f"BIT ROT DETECTED - {total_corrupted} Corrupted Files!"
        elif has_errors:
            subject = f"Bit Rot Detector - FAILED with {len(errors)} Error(s)"
        else:
            num_drives = len(drive_health_results)
            if num_drives > 1:
                subject = (
                    f"Bit Rot Detector - {num_drives} Drives Processed Successfully"
                )
            else:
                if has_sync and has_scrub:
                    subject = "Bit Rot Detector - Sync + Scrub Completed"
                elif has_sync:
                    total_files = (
                        sync_results[0][1].files_scanned if sync_results else 0
                    )
                    subject = f"Bit Rot Detector - {total_files:,} Files Synced"
                else:
                    total_files = (
                        scrub_results[0][1].files_validated if scrub_results else 0
                    )
                    subject = f"Bit Rot Detector - {total_files:,} Files Validated"

        # Always send unified report
        self.send_notification(subject, body)
