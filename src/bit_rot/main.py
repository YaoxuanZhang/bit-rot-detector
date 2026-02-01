"""Main entry point for Bit Rot Detector CLI."""

import argparse
import logging
import os
import sys
from pathlib import Path

from dotenv import load_dotenv

from bit_rot.logging_config import setup_logging

from .database import Database
from .hasher import Hasher
from .mailer import EmailConfig, Mailer
from .scanner import Scanner

# Load environment variables
load_dotenv()


def load_config() -> tuple[Path, EmailConfig, float, str, int]:
    """Load configuration from environment variables.

    Returns:
        Tuple of (target_directory, email_config, scrub_percentage, scrub_frequency, log_retention_days)

    Raises:
        ValueError: If required configuration is missing
    """
    target_dir = os.getenv("TARGET_DIRECTORY")
    if not target_dir:
        raise ValueError("TARGET_DIRECTORY not set in environment")

    target_path = Path(target_dir)
    if not target_path.exists():
        raise ValueError(f"TARGET_DIRECTORY does not exist: {target_dir}")

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

    return target_path, email_config, scrub_percentage, scrub_frequency, log_retention_days


def run_sync(target_path: Path, db: Database, scanner: Scanner, mailer: Mailer) -> bool:
    """Run sync operation.

    Args:
        target_path: Target directory to scan
        db: Database instance
        scanner: Scanner instance
        mailer: Mailer instance

    Returns:
        True if successful, False otherwise
    """
    logger = logging.getLogger(__name__)
    logger.info("========== STARTING SYNC OPERATION ==========")

    try:
        # Canary check
        if not scanner.check_canary(target_path):
            error_msg = f"Canary check failed: .bitrot-canary not found in {target_path}"
            logger.critical(f"{error_msg}")
            mailer.send_error_notification(error_msg)
            return False

        # Perform sync with atomic transaction
        with db.transaction():
            result = scanner.sync_directory(target_path, db)

        logger.info(
            f"Sync operation completed - "
            f"Scanned: {result.files_scanned}, Added: {result.files_added}, "
            f"Modified: {result.files_modified}, Moved: {result.files_moved}, "
            f"Removed: {result.files_removed}"
        )

        # Send notification
        mailer.send_sync_notification(
            files_added=result.files_added,
            files_modified=result.files_modified,
            files_moved=result.files_moved,
            files_removed=result.files_removed,
            files_scanned=result.files_scanned,
            errors=result.errors,
        )

        return True

    except Exception as e:
        error_msg = f"Sync operation failed: {e}"
        logger.critical(f"{error_msg}", exc_info=True)
        mailer.send_error_notification(error_msg)
        return False


def run_scrub(
    db: Database,
    scanner: Scanner,
    mailer: Mailer,
    scrub_percentage: float,
    scrub_frequency: str,
) -> bool:
    """Run scrub operation.

    Args:
        db: Database instance
        scanner: Scanner instance
        mailer: Mailer instance
        scrub_percentage: Percentage of files to scrub
        scrub_frequency: Scrub frequency (daily/weekly/monthly)

    Returns:
        True if successful, False otherwise
    """
    logger = logging.getLogger(__name__)
    logger.info("========== STARTING SCRUB OPERATION ==========")

    try:
        result = scanner.scrub_files(db, scrub_percentage, scrub_frequency)

        logger.info(
            f"Scrub operation completed - "
            f"Validated: {result.files_validated}, Corrupted: {len(result.files_corrupted)}"
        )

        # Send notification
        mailer.send_scrub_notification(
            files_validated=result.files_validated,
            files_corrupted=result.files_corrupted,
            errors=result.errors,
        )

        # Return False if corruption detected
        return len(result.files_corrupted) == 0

    except Exception as e:
        error_msg = f"Scrub operation failed: {e}"
        logger.critical(f"{error_msg}", exc_info=True)
        mailer.send_error_notification(error_msg)
        return False


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
        target_path, email_config, scrub_percentage, scrub_frequency, log_retention_days = load_config()
        
        # Setup logging with rotation
        setup_logging(log_retention_days)
        logger = logging.getLogger(__name__)
        
        logger.info("Loading configuration from environment")
        logger.info(f"Target directory: {target_path}")
        logger.info(f"Scrub settings: {scrub_percentage}%, {scrub_frequency}")
        logger.info(f"Log retention: {log_retention_days} days")

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

        # Initialize database (schema created in __init__)
        db_path = target_path / "bitrot.db"
        db = Database(db_path)

        # Initialize scanner
        hasher = Hasher()
        scanner = Scanner(hasher)

        # Determine what to run
        run_sync_op = args.sync or not args.scrub
        run_scrub_op = args.scrub or not args.sync

        success = True

        # Run sync
        if run_sync_op:
            if not run_sync(target_path, db, scanner, mailer):
                success = False

        # Run scrub
        if run_scrub_op:
            if not run_scrub(db, scanner, mailer, scrub_percentage, scrub_frequency):
                success = False

        # Cleanup
        db.close()

        if success:
            logger.info("========== ALL OPERATIONS COMPLETED SUCCESSFULLY ==========")
            return 0
        else:
            logger.warning("========== OPERATIONS COMPLETED WITH ERRORS ==========")
            return 1

    except ValueError as e:
        logger.critical(f"Configuration error: {e}")
        return 1
    except Exception as e:
        logger.critical(f"Unexpected error: {e}", exc_info=True)
        return 1


if __name__ == "__main__":
    sys.exit(main())
