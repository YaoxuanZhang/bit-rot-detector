"""Scanner module for directory traversal and file integrity verification."""

import logging
import os
import threading
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Optional

from .database import Database, FileRecord
from .hasher import Hasher

logger = logging.getLogger(__name__)


@dataclass
class SyncResult:
    """Results from a sync operation."""

    files_scanned: int
    files_added: int
    files_modified: int
    files_moved: int
    files_removed: int
    errors: list[str]


@dataclass
class ScrubResult:
    """Results from a scrub operation."""

    files_validated: int
    files_corrupted: list[str]
    errors: list[str]


class Scanner:
    """Directory scanner with move detection and scrubbing capabilities."""

    def __init__(self, hasher: Hasher, stop_event: Optional[threading.Event] = None):
        """Initialize scanner.

        Args:
            hasher: Hasher instance for computing file hashes
            stop_event: Event to signal cancellation
        """
        self.hasher = hasher
        self.stop_event = stop_event

    @staticmethod
    def check_canary(root_path: Path) -> bool:
        """Verify that the canary file exists.

        Args:
            root_path: Root directory to check

        Returns:
            True if canary exists, False otherwise
        """
        canary_path = root_path / ".bitrot-canary"
        exists = canary_path.exists()

        if exists:
            logger.info(f"Canary check passed: {canary_path}")
        else:
            logger.critical(f"Canary check FAILED: {canary_path} not found")

        return exists

    @staticmethod
    def _ensure_long_path(path: Path) -> Path:
        """Ensure path uses long path prefix on Windows to bypass 260 char limit.

        Args:
            path: Path to check

        Returns:
            Path with long path prefix if on Windows
        """
        if os.name == "nt":
            abs_path = str(path.resolve())
            if not abs_path.startswith("\\\\?\\"):
                return Path(f"\\\\?\\{abs_path}")
        return path

    def sync_directory(self, root_path: Path, db: Database) -> SyncResult:
        """Sync directory with database (Phases 1-3).

        Phase 1: Walk directory and record file metadata
        Phase 2: Detect moved files
        Phase 3: Detect deleted files

        Args:
            root_path: Root directory to scan
            db: Database instance

        Returns:
            SyncResult with operation statistics
        """
        # Ensure we can handle long paths on Windows
        root_path = self._ensure_long_path(root_path)

        logger.info(f"Starting sync operation for: {root_path}")
        session_start = datetime.now().isoformat()

        files_scanned = 0
        files_added = 0
        files_modified = 0
        files_moved = 0
        files_removed = 0
        errors = []

        # Get existing files from database
        db_files = db.get_all_files()
        logger.info(f"Loaded {len(db_files)} existing files from database")

        # Phase 1: Walk directory tree (don't follow symlinks to avoid loops)
        logger.info("Phase 1: Walking directory tree")
        current_files: dict[str, tuple[int, float]] = {}  # path -> (size, mtime)

        # System folders to skip (Windows-specific)
        system_folders = {
            "$RECYCLE.BIN",
            "System Volume Information",
            "Windows",
            "Program Files",
            "Program Files (x86)",
            "ProgramData",
            "Recovery",
            "$Recycle.Bin",  # Case variation
        }

        for root, dirs, files in os.walk(root_path, followlinks=False):
            if self.stop_event and self.stop_event.is_set():
                logger.info("Scanning interrupted")
                break

            # Skip system folders (modify dirs in-place to prevent descending)
            dirs[:] = [d for d in dirs if d not in system_folders]

            # Skip the database file itself if it's in the current directory
            if "bitrot.db" in files:
                files.remove("bitrot.db")

            for filename in files:
                if self.stop_event and self.stop_event.is_set():
                    break
                filepath = Path(root) / filename
                try:
                    stat = filepath.stat()
                    current_files[str(filepath.absolute())] = (
                        stat.st_size,
                        stat.st_mtime,
                    )
                    files_scanned += 1

                    if files_scanned % 1000 == 0:
                        logger.info(f"Scanned {files_scanned} files...")

                except (PermissionError, OSError) as e:
                    error_msg = f"Error accessing {filepath}: {e}"
                    logger.warning(f"{error_msg}")
                    errors.append(error_msg)

        logger.info(f"Phase 1 complete: Scanned {files_scanned} files")

        # Phase 2: Process files and detect moves
        logger.info("Phase 2: Processing files and detecting moves")

        # Track which DB files we've seen
        seen_db_paths = set()

        for current_path, (size, mtime) in current_files.items():
            if self.stop_event and self.stop_event.is_set():
                logger.info("Scanning interrupted (Phase 2)")
                break

            if current_path in db_files:
                # File exists in same location - check if modified
                record = db_files[current_path]

                # Compare size and mtime to detect modifications
                size_changed = size != record.file_size
                mtime_changed = abs(mtime - record.mtime) > 0.001  # 1ms tolerance

                if size_changed or mtime_changed:
                    # File was modified - re-compute hash
                    try:
                        logger.info(
                            f"Detected modification: {current_path} "
                            f"(size: {record.file_size} -> {size}, "
                            f"mtime changed: {mtime_changed})"
                        )
                        new_hash = self.hasher.compute_hash(Path(current_path))
                        db.stage_file_update(
                            abs_path=current_path,
                            hash=new_hash,
                            file_size=size,
                            mtime=mtime,
                            added_at=record.added_at,
                            last_scrubbed=None,  # Reset scrub status on modification
                            scrub_count=0,  # Reset scrub count on modification
                        )
                        files_modified += 1
                    except (PermissionError, OSError) as e:
                        error_msg = f"Error hashing modified file {current_path}: {e}"
                        logger.warning(f"{error_msg}")
                        errors.append(error_msg)
                else:
                    # File unchanged - just update last_seen
                    db.stage_file_update(
                        abs_path=current_path,
                        hash=record.hash,
                        file_size=size,
                        mtime=mtime,
                        added_at=record.added_at,
                        last_scrubbed=record.last_scrubbed,
                        scrub_count=record.scrub_count,
                    )

                seen_db_paths.add(current_path)
            else:
                # New file or potentially moved file
                # Check if a file with matching size/mtime exists in DB at different path
                moved_from = self._find_moved_file(
                    current_path, size, mtime, db_files, seen_db_paths, current_files
                )

                if moved_from:
                    # Size and mtime already matched in _find_moved_file
                    # No need to rehash - the file is uniquely identified
                    old_record = db_files[moved_from]
                    logger.info(f"Detected move: {moved_from} -> {current_path}")
                    db.stage_file_update(
                        abs_path=current_path,
                        hash=old_record.hash,  # Reuse existing hash
                        file_size=size,
                        mtime=mtime,
                        added_at=old_record.added_at,
                        last_scrubbed=old_record.last_scrubbed,
                        scrub_count=old_record.scrub_count,
                    )
                    # Mark old path for removal
                    db.stage_file_removal(moved_from)
                    seen_db_paths.add(moved_from)
                    files_moved += 1
                else:
                    # New file - compute hash and add
                    try:
                        file_hash = self.hasher.compute_hash(Path(current_path))
                        self._add_new_file(current_path, file_hash, size, mtime, db)
                        files_added += 1

                        if files_added % 100 == 0:
                            logger.info(f"Added {files_added} new files...")

                    except (PermissionError, OSError) as e:
                        error_msg = f"Error hashing {current_path}: {e}"
                        logger.warning(f"{error_msg}")
                        errors.append(error_msg)

        logger.info(
            f"Phase 2 complete: Added {files_added} files, "
            f"Modified {files_modified} files, Moved {files_moved} files"
        )

        # Phase 3: Detect deleted files
        logger.info("Phase 3: Detecting deleted files")

        for db_path in db_files:
            if self.stop_event and self.stop_event.is_set():
                break
            if db_path not in current_files and db_path not in seen_db_paths:
                logger.info(f"Detected deletion: {db_path}")
                db.stage_file_removal(db_path)
                files_removed += 1

        logger.info(f"Phase 3 complete: Removed {files_removed} files")

        result = SyncResult(
            files_scanned=files_scanned,
            files_added=files_added,
            files_modified=files_modified,
            files_moved=files_moved,
            files_removed=files_removed,
            errors=errors,
        )

        logger.info(
            f"Sync complete - Scanned: {files_scanned}, "
            f"Added: {files_added}, Modified: {files_modified}, Moved: {files_moved}, "
            f"Removed: {files_removed}, Errors: {len(errors)}"
        )

        return result

    def _find_moved_file(
        self,
        current_path: str,
        size: int,
        mtime: float,
        db_files: dict[str, FileRecord],
        seen_paths: set[str],
        current_files: dict[str, tuple[int, float]],
    ) -> Optional[str]:
        """Find a file in the database with matching size and mtime.

        IMPORTANT: Only returns a match if the old location no longer exists
        in the current scan. This prevents false move detection when duplicate
        files exist (e.g., two identical files, moving one shouldn't be detected
        as a move if the original still exists).

        Args:
            current_path: Current path of the file
            size: File size in bytes
            mtime: File modification time
            db_files: Dictionary of database files
            seen_paths: Set of paths already processed
            current_files: Dictionary of current files from scan

        Returns:
            Path of the matching file in DB, or None if not found
        """
        for db_path, record in db_files.items():
            if self.stop_event and self.stop_event.is_set():
                break

            if db_path in seen_paths:
                continue

            # CRITICAL: Only consider it a move if old location no longer exists
            if db_path in current_files:
                # Old location still exists - this is a duplicate, not a move
                continue

            # Check if size matches and path is different
            if record.file_size == size and db_path != current_path:
                # Check mtime (allow 1 second tolerance for filesystem differences)
                try:
                    db_mtime = datetime.fromisoformat(record.last_seen).timestamp()
                    if abs(db_mtime - mtime) < 1.0:
                        return db_path
                except (ValueError, OSError):
                    continue

        return None

    def _add_new_file(
        self, path: str, file_hash: str, size: int, mtime: float, db: Database
    ) -> None:
        """Add a new file to the database.

        Args:
            path: Absolute path to the file
            file_hash: BLAKE3 hash of the file
            size: File size in bytes
            mtime: File modification time
            db: Database instance
        """
        logger.debug(f"Adding new file: {path}")
        db.stage_file_update(abs_path=path, hash=file_hash, file_size=size, mtime=mtime)

    def scrub_files(
        self, db: Database, percentage: float, frequency: str
    ) -> ScrubResult:
        """Scrub files to detect bit rot (Phase 4).

        Args:
            db: Database instance
            percentage: Percentage of files to scrub (0.1 to 100.0)
            frequency: Scrub frequency (daily/weekly/monthly)

        Returns:
            ScrubResult with validation statistics
        """
        logger.info(f"Starting scrub operation ({percentage}%, {frequency} frequency)")

        # Determine minimum age based on frequency
        min_age_days = None
        if frequency == "weekly":
            min_age_days = 7
        elif frequency == "monthly":
            min_age_days = 30

        # Get files to scrub
        files_to_scrub = db.get_files_for_scrub(percentage, min_age_days)
        logger.info(f"Selected {len(files_to_scrub)} files for scrubbing")

        files_validated = 0
        files_corrupted = []
        errors = []

        for i, record in enumerate(files_to_scrub, 1):
            if self.stop_event and self.stop_event.is_set():
                logger.info("Scrubbing interrupted")
                break

            try:
                filepath = Path(record.abs_path)

                # Check if file still exists
                if not filepath.exists():
                    logger.warning(f"File no longer exists: {filepath}")
                    errors.append(f"File not found: {filepath}")
                    continue

                # Compute current hash
                current_hash = self.hasher.compute_hash(filepath)

                # Compare with stored hash
                if current_hash == record.hash:
                    # Hash matches - file is intact
                    db.update_scrub_status(record.abs_path)
                    files_validated += 1

                    if files_validated % 100 == 0:
                        logger.info(
                            f"Validated {files_validated}/{len(files_to_scrub)} files..."
                        )
                else:
                    # Hash mismatch - BIT ROT DETECTED!
                    error_msg = (
                        f"BIT ROT DETECTED: {filepath}\n"
                        f"  Expected: {record.hash}\n"
                        f"  Got:      {current_hash}"
                    )
                    files_corrupted.append(record.abs_path)

            except (PermissionError, OSError) as e:
                error_msg = f"Error scrubbing {record.abs_path}: {e}"
                logger.warning(f"{error_msg}")
                errors.append(error_msg)

        result = ScrubResult(
            files_validated=files_validated,
            files_corrupted=files_corrupted,
            errors=errors,
        )

        logger.info(
            f"Scrub complete - Validated: {files_validated}, "
            f"Corrupted: {len(files_corrupted)}, Errors: {len(errors)}"
        )

        return result
