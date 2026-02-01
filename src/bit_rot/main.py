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

    try:
        # Load configuration
        config = load_config()

        # Setup logging with rotation
        setup_logging(config.log_retention_days)
        logger = logging.getLogger(__name__)

        logger.info("Loading configuration from environment")
        logger.info(f"Target directories: {', '.join(str(p) for p in config.target_paths)}")
        logger.info(f"Scrub settings: {config.scrub_percentage}%, {config.scrub_frequency}")
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
        all_sync_results, all_scrub_results, errors, duration = process_drives_concurrently(
            target_paths=config.target_paths,
            scanner=scanner,
            mailer=mailer,
            run_sync_op=run_sync_op,
            run_scrub_op=run_scrub_op,
            scrub_percentage=config.scrub_percentage,
            scrub_frequency=config.scrub_frequency,
            max_workers=config.max_workers,
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
