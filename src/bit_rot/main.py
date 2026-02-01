"""Main entry point for Bit Rot Detector CLI."""

import argparse
import logging
import os
import sys
from pathlib import Path

from dotenv import load_dotenv

from bit_rot.logging_config import setup_logging

from .coordinator import process_drives_concurrently
from .hasher import Hasher
from .mailer import EmailConfig, Mailer
from .scanner import Scanner

# Load environment variables
load_dotenv()


def load_config() -> tuple[list[Path], EmailConfig, float, str, int, int]:
    """Load configuration from environment variables.

    Returns:
        Tuple of (target_paths, email_config, scrub_percentage, scrub_frequency, log_retention_days, max_workers)

    Raises:
        ValueError: If required configuration is missing
    """
    target_dirs_str = os.getenv("TARGET_DIRECTORY")
    if not target_dirs_str:
        raise ValueError("TARGET_DIRECTORY not set in environment")

    # Parse comma-separated paths
    target_dirs = [d.strip() for d in target_dirs_str.split(",") if d.strip()]
    if not target_dirs:
        raise ValueError("TARGET_DIRECTORY is empty")

    # Validate and convert to Path objects
    target_paths = []
    for target_dir in target_dirs:
        target_path = Path(target_dir)
        if not target_path.exists():
            raise ValueError(f"Target directory does not exist: {target_dir}")
        target_paths.append(target_path)

    # Remove duplicates while preserving order
    seen = set()
    unique_paths = []
    for path in target_paths:
        abs_path = path.resolve()
        if abs_path not in seen:
            seen.add(abs_path)
            unique_paths.append(path)
    target_paths = unique_paths

    # Email configuration
    email_config = EmailConfig(
        smtp_host=os.getenv("SMTP_HOST", "mail.smtp2go.com"),
        smtp_port=int(os.getenv("SMTP_PORT", "587")),
        smtp_username=os.getenv("SMTP_USERNAME", ""),
        smtp_password=os.getenv("SMTP_PASSWORD", ""),
        sender=os.getenv("SMTP_SENDER", ""),
        recipient=os.getenv("SMTP_RECIPIENT", ""),
        notify_sync_success=os.getenv("NOTIFY_SYNC_SUCCESS", "false").lower() == "true",
        notify_scrub_success=os.getenv("NOTIFY_SCRUB_SUCCESS", "false").lower() == "true",
        notify_critical_failures=os.getenv("NOTIFY_CRITICAL_FAILURES", "true").lower() == "true",
    )

    # Scrub configuration
    scrub_percentage = float(os.getenv("SCRUB_PERCENTAGE", "1.0"))
    if not 0.1 <= scrub_percentage <= 100.0:
        raise ValueError(f"SCRUB_PERCENTAGE must be between 0.1 and 100.0, got {scrub_percentage}")

    scrub_frequency = os.getenv("SCRUB_FREQUENCY", "daily").lower()
    if scrub_frequency not in ["daily", "weekly", "monthly"]:
        raise ValueError(f"SCRUB_FREQUENCY must be daily/weekly/monthly, got {scrub_frequency}")

    # Log retention configuration
    log_retention_days = int(os.getenv("LOG_RETENTION_DAYS", "7"))

    # Worker configuration
    max_workers = int(os.getenv("MAX_WORKERS", "4"))
    if max_workers < 1:
        raise ValueError(f"MAX_WORKERS must be at least 1, got {max_workers}")

    return target_paths, email_config, scrub_percentage, scrub_frequency, log_retention_days, max_workers


def main() -> int:
    """Main entry point for CLI.

    Returns:
        Exit code (0 for success, 1 for failure)
    """
    # Parse arguments first (before logging setup)
    parser = argparse.ArgumentParser(
        description="Bit Rot Detector - Production-grade file corruption detection"
    )
    parser.add_argument(
        "--sync",
        action="store_true",
        help="Run sync operation only (directory walk, move/deletion detection)",
    )
    parser.add_argument(
        "--scrub",
        action="store_true",
        help="Run scrub operation only (re-verify file hashes)",
    )
    parser.add_argument(
        "--test-email",
        action="store_true",
        help="Send test email to verify SMTP configuration",
    )

    args = parser.parse_args()

    try:
        # Load configuration
        (
            target_paths,
            email_config,
            scrub_percentage,
            scrub_frequency,
            log_retention_days,
            max_workers,
        ) = load_config()

        # Setup logging with rotation
        setup_logging(log_retention_days)
        logger = logging.getLogger(__name__)

        logger.info("Loading configuration from environment")
        logger.info(f"Target directories: {', '.join(str(p) for p in target_paths)}")
        logger.info(f"Scrub settings: {scrub_percentage}%, {scrub_frequency}")
        logger.info(f"Log retention: {log_retention_days} days")
        logger.info(f"Max workers: {max_workers}")

        # Initialize components
        mailer = Mailer(email_config)

        # Test email mode
        if args.test_email:
            logger.info("========== TESTING EMAIL CONFIGURATION ==========")
            if mailer.send_test_email():
                logger.info("Test email sent successfully!")
                return 0
            else:
                logger.error("Failed to send test email")
                return 1

        # Initialize scanner
        hasher = Hasher()
        scanner = Scanner(hasher)

        # Determine what to run
        run_sync_op = args.sync or not args.scrub
        run_scrub_op = args.scrub or not args.sync

        # Process drives concurrently
        all_sync_results, all_scrub_results, errors, duration = process_drives_concurrently(
            target_paths=target_paths,
            scanner=scanner,
            mailer=mailer,
            run_sync_op=run_sync_op,
            run_scrub_op=run_scrub_op,
            scrub_percentage=scrub_percentage,
            scrub_frequency=scrub_frequency,
            max_workers=max_workers,
        )

        # Send consolidated email if no errors and no bit rot
        has_bit_rot = any(len(r.files_corrupted) > 0 for _, r in all_scrub_results)

        if not errors and not has_bit_rot and (all_sync_results or all_scrub_results):
            # Send consolidated report
            if run_sync_op and run_scrub_op and all_sync_results and all_scrub_results:
                # Both operations ran - send consolidated report
                logger.info("Sending consolidated report")
                mailer.send_consolidated_report(all_sync_results, all_scrub_results, duration)
            else:
                # Only one operation ran - send individual notifications
                for drive_name, sync_result in all_sync_results:
                    mailer.send_sync_notification(
                        files_added=sync_result.files_added,
                        files_modified=sync_result.files_modified,
                        files_moved=sync_result.files_moved,
                        files_removed=sync_result.files_removed,
                        files_scanned=sync_result.files_scanned,
                        errors=sync_result.errors,
                    )
                for drive_name, scrub_result in all_scrub_results:
                    if not scrub_result.files_corrupted:  # Only send if not already sent as critical
                        mailer.send_scrub_notification(
                            files_validated=scrub_result.files_validated,
                            files_corrupted=scrub_result.files_corrupted,
                            errors=scrub_result.errors,
                        )

        if errors:
            logger.warning(f"========== OPERATIONS COMPLETED WITH {len(errors)} ERROR(S) ==========")
            return 1
        elif has_bit_rot:
            logger.critical("========== BIT ROT DETECTED ==========")
            return 1
        else:
            logger.info("========== ALL OPERATIONS COMPLETED SUCCESSFULLY ==========")
            return 0

    except ValueError as e:
        logger.critical(f"Configuration error: {e}")
        return 1
    except Exception as e:
        logger.critical(f"Unexpected error: {e}", exc_info=True)
        return 1


if __name__ == "__main__":
    sys.exit(main())
