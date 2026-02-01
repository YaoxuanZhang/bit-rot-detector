"""Drive processing coordination for multi-drive operations."""

import logging
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path
from typing import Optional

from .database import Database
from .drive_monitor import DriveHealth, get_drive_health
from .scanner import Scanner, ScrubResult, SyncResult

logger = logging.getLogger(__name__)


def process_drive(
    target_path: Path,
    scanner: Scanner,
    run_sync_op: bool,
    run_scrub_op: bool,
    scrub_percentage: float,
    scrub_frequency: str,
) -> tuple[str, Optional[SyncResult], Optional[ScrubResult], Optional[str]]:
    """Process a single drive (sync and/or scrub).

    Args:
        target_path: Path to the drive to process
        scanner: Scanner instance
        run_sync_op: Whether to run sync operation
        run_scrub_op: Whether to run scrub operation
        scrub_percentage: Percentage of files to scrub
        scrub_frequency: Scrub frequency setting

    Returns:
        Tuple of (drive_name, drive_health, sync_result, scrub_result, error_message)
    """
    drive_name = target_path.name
    logger.info(f"[{drive_name}] Starting processing")

    # Collect drive health first (before canary check)
    drive_health = get_drive_health(target_path)

    db = None
    sync_result = None
    scrub_result = None

    try:
        # Canary check
        if not scanner.check_canary(target_path):
            error_msg = f"[{drive_name}] Canary check failed: .bitrot-canary not found in {target_path}"
            logger.critical(error_msg)
            return (drive_name, drive_health, None, None, error_msg)

        # Initialize database
        db_path = target_path / "bitrot.db"
        db = Database(db_path)

        # Sync operation
        if run_sync_op:
            logger.info(f"[{drive_name}] Starting sync operation")
            with db.transaction():
                sync_result = scanner.sync_directory(target_path, db, drive_name)
            logger.info(
                f"[{drive_name}] Sync completed - Scanned: {sync_result.files_scanned}, "
                f"Added: {sync_result.files_added}, Modified: {sync_result.files_modified}, "
                f"Moved: {sync_result.files_moved}, Removed: {sync_result.files_removed}"
            )

        # Scrub operation
        if run_scrub_op:
            logger.info(f"[{drive_name}] Starting scrub operation")
            scrub_result = scanner.scrub_files(
                db, scrub_percentage, scrub_frequency, drive_name
            )
            logger.info(
                f"[{drive_name}] Scrub completed - Validated: {scrub_result.files_validated}, "
                f"Corrupted: {len(scrub_result.files_corrupted)}"
            )

        logger.info(f"[{drive_name}] Processing completed successfully")
        return (drive_name, drive_health, sync_result, scrub_result, None)

    except Exception as e:
        error_msg = f"[{drive_name}] Processing failed: {e}"
        logger.critical(error_msg, exc_info=True)
        return (drive_name, drive_health, None, None, error_msg)
    finally:
        if db:
            db.close()


def process_drives_concurrently(
    target_paths: list[Path],
    scanner: Scanner,
    run_sync_op: bool,
    run_scrub_op: bool,
    scrub_percentage: float,
    scrub_frequency: str,
    max_workers: int,
) -> tuple[
    list[tuple[str, SyncResult]],
    list[tuple[str, ScrubResult]],
    list[DriveHealth],
    list[str],
    float,
]:
    """Process multiple drives concurrently.

    Args:
        target_paths: List of drive paths to process
        scanner: Scanner instance
        run_sync_op: Whether to run sync operation
        run_scrub_op: Whether to run scrub operation
        scrub_percentage: Percentage of files to scrub
        scrub_frequency: Scrub frequency setting
        max_workers: Maximum number of concurrent workers

    Returns:
        Tuple of (sync_results, scrub_results, drive_health_results, errors, duration_seconds)
    """
    start_time = time.time()
    all_sync_results = []
    all_scrub_results = []
    all_drive_health = []
    errors = []

    # Determine worker count (allocate one worker per drive up to MAX_WORKERS)
    num_workers = min(max_workers, len(target_paths))
    logger.info(f"Processing {len(target_paths)} drive(s) with {num_workers} worker(s)")

    # Process drives concurrently
    with ThreadPoolExecutor(max_workers=num_workers) as executor:
        # Submit all drive processing tasks
        futures = {
            executor.submit(
                process_drive,
                path,
                scanner,
                run_sync_op,
                run_scrub_op,
                scrub_percentage,
                scrub_frequency,
            ): path
            for path in target_paths
        }

        # Collect results as they complete
        for future in as_completed(futures):
            drive_name, drive_health, sync_result, scrub_result, error = future.result()

            # Always collect drive health
            all_drive_health.append(drive_health)

            if error:
                errors.append(error)
            else:
                if sync_result:
                    all_sync_results.append((drive_name, sync_result))
                if scrub_result:
                    all_scrub_results.append((drive_name, scrub_result))
                    # Log bit rot detection
                    if scrub_result.files_corrupted:
                        logger.critical(
                            f"BIT ROT DETECTED on {drive_name}: {len(scrub_result.files_corrupted)} corrupted files"
                        )

    duration = time.time() - start_time
    return all_sync_results, all_scrub_results, all_drive_health, errors, duration
