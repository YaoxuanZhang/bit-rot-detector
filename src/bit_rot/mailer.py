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
        sync_results: list[tuple[str, "SyncResult"]],
        include_drive_header: bool = True
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
            
        lines.append("=" * 64)
        lines.append("SYNC RESULTS" + (" BY DRIVE" if include_drive_header and len(sync_results) > 1 else ""))
        lines.append("=" * 64)
        lines.append("")
        
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
        include_drive_header: bool = True
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
            
        lines.append("=" * 64)
        lines.append("SCRUB RESULTS" + (" BY DRIVE" if include_drive_header and len(scrub_results) > 1 else ""))
        lines.append("=" * 64)
        lines.append("")
        
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
        # Determine operation type
        has_sync = len(sync_results) > 0
        has_scrub = len(scrub_results) > 0
        
        if has_sync and has_scrub:
            operation_type = "SYNC + SCRUB"
        elif has_sync:
            operation_type = "SYNC"
        else:
            operation_type = "SCRUB"
        
        # Build email body using builder functions
        lines = []
        lines.append("╔" + "═" * 62 + "╗")
        lines.append("║" + " " * 10 + "BIT ROT DETECTOR - CONSOLIDATED REPORT" + " " * 13 + "║")
        lines.append("╚" + "═" * 62 + "╝")
        lines.append("")
        lines.append(f"Operation: {operation_type}")
        lines.append(f"Drives Processed: {max(len(sync_results), len(scrub_results))}")
        lines.append(f"Duration: {self._format_duration(duration_seconds)}")
        lines.append("")
        
        # Use builder functions for sections
        lines.extend(self._build_sync_section(sync_results, include_drive_header=True))
        lines.extend(self._build_scrub_section(scrub_results, include_drive_header=True))
        
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
        from .scanner import SyncResult

        # Send if sync success notifications enabled and files were scanned
        if files_scanned > 0 and self.config.notify_sync_success:
            subject = f"Bit Rot Detector - {files_scanned:,} Files Synced"
            
            # Create a SyncResult for the builder function
            sync_result = SyncResult(
                files_scanned=files_scanned,
                files_added=files_added,
                files_modified=files_modified,
                files_moved=files_moved,
                files_removed=files_removed,
                errors=errors,
            )
            
            # Build body using existing section builder
            lines = []
            lines.extend(self._build_header("BIT ROT - SYNC"))            
            # Use existing builder function
            lines.extend(self._build_sync_section([("Drive", sync_result)], include_drive_header=False))
            
            # Add error details if present
            if errors:
                lines.append("=" * 64)
                lines.append("ERROR DETAILS")
                lines.append("=" * 64)
                lines.append("")
                for i, error in enumerate(errors[:10], 1):  # Show first 10 errors
                    lines.append(f"{i}. {error}")
                lines.append("")
            
            lines.append("=" * 64)
            body = "\n".join(lines)

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
        from .scanner import ScrubResult

        # Send if corrupted files found (critical) or if scrub success notifications enabled
        if files_corrupted:
            # CRITICAL: Bit rot detected
            subject = f"BIT ROT DETECTED - {len(files_corrupted)} Corrupted Files!"
            
            lines = []
            lines.append("╔" + "═" * 62 + "╗")
            lines.append("║" + " " * 10 + "CRITICAL ALERT - BIT ROT DETECTED" + " " * 18 + "║")
            lines.append("╚" + "═" * 62 + "╝")
            lines.append("")
            lines.append(f"IMMEDIATE ACTION REQUIRED: {len(files_corrupted)} file(s) corrupted!")
            lines.append("")
            lines.append("=" * 64)
            lines.append("CORRUPTED FILES (Full Paths)")
            lines.append("=" * 64)
            lines.append("")
            
            # List all corrupted files with full paths
            for i, filepath in enumerate(files_corrupted, 1):
                lines.append(f"{i:4d}. {filepath}")
            
            lines.append("")
            lines.append(Mailer._build_separator())
            lines.append("SUMMARY")
            lines.append(Mailer._build_separator())
            lines.append("")
            lines.append(f"  Total Corrupted: {len(files_corrupted):,}")
            lines.append(f"  Files Validated: {files_validated:,}")
            
            # Add error details if present
            if errors:
                lines.append("")
                lines.append(f"  Additional Errors: {len(errors):,}")
                lines.append("")
                lines.append(Mailer._build_separator())
                lines.append("ERROR DETAILS")
                lines.append(Mailer._build_separator())
                lines.append("")
                for i, error in enumerate(errors[:5], 1):  # Show first 5 errors
                    lines.append(f"{i}. {error}")
            
            lines.append("")
            lines.append(Mailer._build_separator())
            lines.append("RECOMMENDED ACTIONS")
            lines.append(Mailer._build_separator())
            lines.append("")
            lines.append("1. Restore corrupted files from your most recent backup")
            lines.append("2. Verify the integrity of your storage hardware")
            lines.append("3. Check system logs for hardware errors")
            lines.append("4. Consider running a full disk check (e.g., fsck, chkdsk)")
            lines.append("")
            lines.append("=" * 64)
            
            body = "\n".join(lines)
            self.send_notification(subject, body)

        elif self.config.notify_scrub_success and files_validated > 0:
            # Success notification
            subject = f"Bit Rot Detector - {files_validated:,} Files Validated"
            
            # Create a ScrubResult for the builder function
            scrub_result = ScrubResult(
                files_validated=files_validated,
                files_corrupted=[],  # Empty for success case
                errors=errors,
            )
            
            # Build body using existing section builder
            lines = []
            lines.extend(self._build_header("BIT ROT - SCRUB"))            
            # Use existing builder function
            lines.extend(self._build_scrub_section([("Drive", scrub_result)], include_drive_header=False))
            
            # Add error details if present
            if errors:
                lines.append("=" * 64)
                lines.append("ERROR DETAILS")
                lines.append("=" * 64)
                lines.append("")
                for i, error in enumerate(errors[:10], 1):  # Show first 10 errors
                    lines.append(f"{i}. {error}")
                lines.append("")
            
            lines.append("=" * 64)
            body = "\n".join(lines)

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
