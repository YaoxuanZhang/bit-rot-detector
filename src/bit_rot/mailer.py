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
    def format_summary_report(
        sync_stats: Optional[dict] = None,
        scrub_stats: Optional[dict] = None,
    ) -> str:
        """Format summary statistics report.

        Args:
            sync_stats: Sync operation statistics
            scrub_stats: Scrub operation statistics

        Returns:
            Formatted summary report string
        """
        lines = ["\n" + "=" * 60, "SUMMARY STATISTICS", "=" * 60]

        if sync_stats:
            lines.extend(
                [
                    "\nSync Operation:",
                    f"  Total Scanned:  {sync_stats.get('scanned', 0):,}",
                    f"  Added:          {sync_stats.get('added', 0):,}",
                    f"  Modified:       {sync_stats.get('modified', 0):,}",
                    f"  Moved:          {sync_stats.get('moved', 0):,}",
                    f"  Removed:        {sync_stats.get('removed', 0):,}",
                    f"  Errors:         {sync_stats.get('errors', 0):,}",
                ]
            )

        if scrub_stats:
            lines.extend(
                [
                    "\nScrub Operation:",
                    f"  Validated:      {scrub_stats.get('validated', 0):,}",
                    f"  Corrupted:      {scrub_stats.get('corrupted', 0):,}",
                    f"  Errors:         {scrub_stats.get('errors', 0):,}",
                ]
            )

        lines.append("=" * 60)
        return "\n".join(lines)

    def send_sync_notification(
        self,
        files_added: int,
        files_modified: int,
        files_moved: int,
        files_removed: int,
        files_scanned: int,
        errors: list[str],
    ) -> None:
        """Send notification for sync operation.

        Args:
            files_added: Number of files added
            files_modified: Number of files modified
            files_moved: Number of files moved
            files_removed: Number of files removed
            files_scanned: Total files scanned
            errors: List of error messages
        """
        # Send if sync success notifications enabled and files were scanned
        if files_scanned > 0 and self.config.notify_sync_success:
            subject = f"Bit Rot Detector - {files_scanned} Files Synced"
            body = f"""Sync operation completed.

New Files Added: {files_added:,}
Files Modified: {files_modified:,}
Files Moved: {files_moved:,}
Files Removed: {files_removed:,}
Total Scanned: {files_scanned:,}
"""
            if errors:
                body += f"\nErrors encountered: {len(errors)}\n"
                body += "\n".join(errors[:10])  # Show first 10 errors

            body += self.format_summary_report(
                sync_stats={
                    "scanned": files_scanned,
                    "added": files_added,
                    "modified": files_modified,
                    "moved": files_moved,
                    "removed": files_removed,
                    "errors": len(errors),
                }
            )

            self.send_notification(subject, body)

    def send_scrub_notification(
        self,
        files_validated: int,
        files_corrupted: list[str],
        errors: list[str],
    ) -> None:
        """Send notification for scrub operation.

        Args:
            files_validated: Number of files validated
            files_corrupted: List of corrupted file paths
            errors: List of error messages
        """
        # Send if corrupted files found (critical) or if scrub success notifications enabled
        if files_corrupted:
            # CRITICAL: Bit rot detected
            subject = f"BIT ROT DETECTED - {len(files_corrupted)} Corrupted Files!"
            body = f"""CRITICAL ALERT: Bit rot has been detected in {len(files_corrupted)} file(s)!

IMMEDIATE ACTION REQUIRED: The following files have been corrupted and need to be restored from backup.

CORRUPTED FILES (Full Paths):
"""
            # List all corrupted files with full paths
            for i, filepath in enumerate(files_corrupted, 1):
                body += f"{i:4d}. {filepath}\n"

            body += f"\n\nSUMMARY:\n"
            body += f"  Total Corrupted: {len(files_corrupted):,}\n"
            body += f"  Files Validated: {files_validated:,}\n"

            if errors:
                body += f"\nAdditional errors encountered: {len(errors)}\n"
                body += "\n".join(errors[:5])  # Show first 5 errors

            body += self.format_summary_report(
                scrub_stats={
                    "validated": files_validated,
                    "corrupted": len(files_corrupted),
                    "errors": len(errors),
                }
            )
            
            body += f"\n\nRECOMMENDED ACTIONS:\n"
            body += "1. Restore corrupted files from your most recent backup\n"
            body += "2. Verify the integrity of your storage hardware\n"
            body += "3. Check system logs for hardware errors\n"
            body += "4. Consider running a full disk check (e.g., fsck, chkdsk)\n"

            self.send_notification(subject, body)

        elif self.config.notify_scrub_success and files_validated > 0:
            # Success notification
            subject = f"Bit Rot Detector - {files_validated} Files Validated"
            body = f"""Scrub operation completed successfully.

Files Validated: {files_validated:,}
Corrupted Files: 0
"""
            if errors:
                body += f"\nErrors encountered: {len(errors)}\n"

            body += self.format_summary_report(
                scrub_stats={
                    "validated": files_validated,
                    "corrupted": 0,
                    "errors": len(errors),
                }
            )

            self.send_notification(subject, body)

    def send_error_notification(self, error_message: str) -> None:
        """Send critical error notification.

        Args:
            error_message: Error message to send
        """
        if self.config.notify_critical_failures:
            subject = "Bit Rot Detector - Critical Failure"
            body = f"""A critical error occurred during execution:

{error_message}

Please check the logs for more details.
"""
            self.send_notification(subject, body)
