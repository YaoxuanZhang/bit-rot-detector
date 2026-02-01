"""Main entry point for Bit Rot Detector CLI."""

import argparse
import logging
import sys

from dotenv import load_dotenv

from bit_rot.logging_config import setup_logging

from .config import load_config
from .coordinator import process_drives_concurrently
from .hasher import Hasher
from .mailer import Mailer
from .scanner import Scanner

# Load environment variables
load_dotenv()


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

    # Setup logging first so logger is available for exception handlers
    # Use default retention if config loading fails
    setup_logging()
    logger = logging.getLogger(__name__)

    try:
        # Load configuration
        config = load_config()

        logger.info("Loading configuration from environment")
        logger.info(
            f"Target directories: {', '.join(str(p) for p in config.target_paths)}"
        )
        logger.info(
            f"Scrub settings: {config.scrub_percentage}%, {config.scrub_frequency}"
        )
        logger.info(f"Log retention: {config.log_retention_days} days")
        logger.info(f"Max workers: {config.max_workers}")

        # Initialize components
        mailer = Mailer(config.email_config)

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
        all_sync_results, all_scrub_results, all_drive_health, errors, duration = (
            process_drives_concurrently(
                target_paths=config.target_paths,
                scanner=scanner,
                run_sync_op=run_sync_op,
                run_scrub_op=run_scrub_op,
                scrub_percentage=config.scrub_percentage,
                scrub_frequency=config.scrub_frequency,
                max_workers=config.max_workers,
            )
        )

        # Always send unified report (one email per program run)
        mailer.send_unified_report(
            sync_results=all_sync_results,
            scrub_results=all_scrub_results,
            drive_health_results=all_drive_health,
            errors=errors,
            duration_seconds=duration,
        )

        # Determine exit code
        has_bit_rot = any(len(r.files_corrupted) > 0 for _, r in all_scrub_results)

        if errors:
            logger.warning(
                f"========== OPERATIONS COMPLETED WITH {len(errors)} ERROR(S) =========="
            )
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
