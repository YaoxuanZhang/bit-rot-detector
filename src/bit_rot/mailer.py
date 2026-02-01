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

    def send_consolidated_report(
        self,
        sync_results: list[tuple[str, "SyncResult"]],
        scrub_results: list[tuple[str, "ScrubResult"]],
        duration_seconds: float,
    ) -> None:
        """Send consolidated report for multi-drive operations.

        Args:
            sync_results: List of (drive_name, SyncResult) tuples
            scrub_results: List of (drive_name, ScrubResult) tuples
            duration_seconds: Total operation duration in seconds
        """
        from .scanner import SyncResult, ScrubResult
        
        # Format duration
        hours = int(duration_seconds // 3600)
        minutes = int((duration_seconds % 3600) // 60)
        seconds = int(duration_seconds % 60)
        
        if hours > 0:
            duration_str = f"{hours} hours {minutes} minutes {seconds} seconds"
        elif minutes > 0:
            duration_str = f"{minutes} minutes {seconds} seconds"
        else:
            duration_str = f"{seconds} seconds"
        
        # Determine operation type
        has_sync = len(sync_results) > 0
        has_scrub = len(scrub_results) > 0
        
        if has_sync and has_scrub:
            operation_type = "SYNC + SCRUB"
        elif has_sync:
            operation_type = "SYNC"
        else:
            operation_type = "SCRUB"
        
        # Build email body
        lines = []
        lines.append("╔" + "═" * 62 + "╗")
        lines.append("║" + " " * 10 + "BIT ROT DETECTOR - CONSOLIDATED REPORT" + " " * 13 + "║")
        lines.append("╚" + "═" * 62 + "╝")
        lines.append("")
        lines.append(f"Operation: {operation_type}")
        lines.append(f"Drives Processed: {max(len(sync_results), len(scrub_results))}")
        lines.append(f"Duration: {duration_str}")
        lines.append("")
        
        # Sync results section
        if sync_results:
            lines.append("=" * 64)
            lines.append("SYNC RESULTS BY DRIVE")
            lines.append("=" * 64)
            lines.append("")
            
            total_scanned = 0
            total_added = 0
            total_modified = 0
            total_moved = 0
            total_removed = 0
            total_errors = 0
            
            for drive_name, result in sync_results:
                lines.append(f"Drive: {drive_name}")
                lines.append(f"  Files Scanned:  {result.files_scanned:,}")
                lines.append(f"  Added:          {result.files_added:,}")
                lines.append(f"  Modified:       {result.files_modified:,}")
                lines.append(f"  Moved:          {result.files_moved:,}")
                lines.append(f"  Removed:        {result.files_removed:,}")
                lines.append(f"  Errors:         {len(result.errors):,}")
                lines.append("")
                
                total_scanned += result.files_scanned
                total_added += result.files_added
                total_modified += result.files_modified
                total_moved += result.files_moved
                total_removed += result.files_removed
                total_errors += len(result.errors)
            
            lines.append("SYNC TOTALS:")
            lines.append(f"  Total Files:    {total_scanned:,}")
            lines.append(f"  Total Added:    {total_added:,}")
            lines.append(f"  Total Modified: {total_modified:,}")
            lines.append(f"  Total Moved:    {total_moved:,}")
            lines.append(f"  Total Removed:  {total_removed:,}")
            if total_errors > 0:
                lines.append(f"  Total Errors:   {total_errors:,}")
            lines.append("")
        
        # Scrub results section
        if scrub_results:
            lines.append("=" * 64)
            lines.append("SCRUB RESULTS BY DRIVE")
            lines.append("=" * 64)
            lines.append("")
            
            total_validated = 0
            total_corrupted = 0
            
            for drive_name, result in scrub_results:
                lines.append(f"Drive: {drive_name}")
                lines.append(f"  Validated:      {result.files_validated:,} files")
                lines.append(f"  Corrupted:      {len(result.files_corrupted):,}")
                if len(result.errors) > 0:
                    lines.append(f"  Errors:         {len(result.errors):,}")
                lines.append("")
                
                total_validated += result.files_validated
                total_corrupted += len(result.files_corrupted)
            
            lines.append("SCRUB TOTALS:")
            lines.append(f"  Total Validated: {total_validated:,}")
            lines.append(f"  Total Corrupted: {total_corrupted:,}")
            if total_corrupted == 0:
                lines.append(f"  Status:          ✓ ALL FILES VERIFIED SUCCESSFULLY")
            else:
                lines.append(f"  Status:          ⚠ CORRUPTION DETECTED")
            lines.append("")
        
        lines.append("=" * 64)
        
        body = "\n".join(lines)
        
        # Determine subject
        num_drives = max(len(sync_results), len(scrub_results))
        if num_drives > 1:
            subject = f"Bit Rot Detector - {num_drives} Drives Processed Successfully"
        else:
            if has_sync and has_scrub:
                subject = "Bit Rot Detector - Sync + Scrub Completed"
            elif has_sync:
                total_files = sync_results[0][1].files_scanned if sync_results else 0
                subject = f"Bit Rot Detector - {total_files:,} Files Synced"
            else:
                total_files = scrub_results[0][1].files_validated if scrub_results else 0
                subject = f"Bit Rot Detector - {total_files:,} Files Validated"
        
        # Send email if notifications are enabled
        should_send = False
        if has_sync and self.config.notify_sync_success:
            should_send = True
        if has_scrub and self.config.notify_scrub_success:
            should_send = True
        
        if should_send:
            self.send_notification(subject, body)
        else:
            logger.info("Consolidated report not sent (notifications disabled)")

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
