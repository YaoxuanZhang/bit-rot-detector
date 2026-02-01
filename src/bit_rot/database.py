"""Database module for managing file metadata with atomic transactions."""

import logging
import sqlite3
from contextlib import contextmanager
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Generator, Optional

logger = logging.getLogger(__name__)


@dataclass
class FileRecord:
    """Represents a file record in the database."""

    abs_path: str
    hash: str
    added_at: str
    last_seen: str
    last_scrubbed: Optional[str]
    scrub_count: int
    file_size: int
    mtime: float


class Database:
    """SQLite database manager with atomic transaction support."""

    def __init__(self, db_path: Path) -> None:
        """Initialize database connection and create schema if needed.

        Args:
            db_path: Path to SQLite database file

        Raises:
            sqlite3.DatabaseError: If database file is corrupted
        """
        self.db_path = db_path
        logger.info(f"Initializing database at {self.db_path}")

        try:
            self.conn = sqlite3.connect(str(db_path))
            self.conn.row_factory = sqlite3.Row

            # Test database integrity
            self.conn.execute("PRAGMA integrity_check").fetchone()

        except sqlite3.DatabaseError as e:
            logger.critical(f"Database corruption detected: {e}")
            logger.critical(f"Database file: {db_path}")
            logger.critical("Recommendation: Delete corrupted database and re-scan")
            raise

        self._staged_updates: list[tuple] = []
        self._staged_removals: list[str] = []

        self._create_schema()
        logger.info("Database schema initialized successfully")

    def _create_schema(self) -> None:
        """Create database schema if it doesn't exist."""
        self.conn.execute("""
            CREATE TABLE IF NOT EXISTS files (
                abs_path TEXT PRIMARY KEY,
                hash TEXT NOT NULL,
                added_at TEXT NOT NULL,
                last_seen TEXT NOT NULL,
                last_scrubbed TEXT,
                scrub_count INTEGER DEFAULT 0,
                file_size INTEGER NOT NULL,
                mtime REAL NOT NULL
            )
            """)
        self.conn.commit()
        logger.info("Database schema initialized successfully")

    @contextmanager
    def transaction(self) -> Generator[None, None, None]:
        """Context manager for atomic transactions.

        Yields:
            None

        Raises:
            Exception: If transaction fails, rolls back all changes
        """
        if not self.conn:
            raise RuntimeError("Database not initialized")

        self._staged_updates = []
        self._staged_removals = []

        try:
            logger.debug("Beginning transaction")
            yield
            self._commit_transaction()
            logger.info("Transaction committed successfully")
        except Exception as e:
            logger.error(f"Transaction failed, rolling back: {e}")
            self._staged_updates = []
            self._staged_removals = []
            raise

    def stage_file_update(
        self,
        abs_path: str,
        hash: str,
        file_size: int,
        mtime: float,
        added_at: Optional[str] = None,
        last_scrubbed: Optional[str] = None,
        scrub_count: Optional[int] = None,
    ) -> None:
        """Stage a file update for the next commit.

        Args:
            abs_path: Absolute path to the file
            hash: BLAKE3 hash of the file
            file_size: Size of the file in bytes
            mtime: File modification time (timestamp)
            added_at: Timestamp when file was first added (for new files)
            last_scrubbed: Timestamp of last scrub (for updates)
            scrub_count: Number of times file has been scrubbed (for updates)
        """
        now = datetime.now().isoformat()

        self._staged_updates.append(
            (
                abs_path,
                hash,
                added_at or now,
                now,
                last_scrubbed,
                scrub_count or 0,
                file_size,
                mtime,
            )
        )
        logger.debug(f"Staged update for: {abs_path}")

    def stage_file_removal(self, abs_path: str) -> None:
        """Stage a file removal for the next commit.

        Args:
            abs_path: Absolute path to the file to remove
        """
        self._staged_removals.append(abs_path)
        logger.debug(f"Staged removal for: {abs_path}")

    def _commit_transaction(self) -> None:
        """Commit all staged updates and removals atomically."""
        if not self.conn:
            raise RuntimeError("Database not initialized")

        cursor = self.conn.cursor()

        # Process updates (INSERT OR REPLACE)
        if self._staged_updates:
            logger.info(f"Committing {len(self._staged_updates)} file updates")
            cursor.executemany(
                """
                INSERT OR REPLACE INTO files 
                (abs_path, hash, added_at, last_seen, last_scrubbed, scrub_count, file_size, mtime)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                """,
                self._staged_updates,
            )

        # Process removals
        if self._staged_removals:
            logger.info(f"Committing {len(self._staged_removals)} file removals")
            cursor.executemany(
                "DELETE FROM files WHERE abs_path = ?",
                [(path,) for path in self._staged_removals],
            )

        self.conn.commit()
        self._staged_updates = []
        self._staged_removals = []

    def get_files_for_scrub(
        self, percentage: float, min_age_days: Optional[int] = None
    ) -> list[FileRecord]:
        """Get files for scrubbing based on percentage and age.

        Args:
            percentage: Percentage of files to scrub (0.1 to 100.0)
            min_age_days: Minimum days since last scrub (None for all files)

        Returns:
            List of FileRecord objects to scrub
        """
        if not self.conn:
            raise RuntimeError("Database not initialized")

        cursor = self.conn.cursor()

        # Build query with optional age filter
        query = "SELECT * FROM files"
        params = []

        if min_age_days is not None:
            cutoff = datetime.now().timestamp() - (min_age_days * 86400)
            cutoff_iso = datetime.fromtimestamp(cutoff).isoformat()
            query += " WHERE last_scrubbed IS NULL OR last_scrubbed < ?"
            params.append(cutoff_iso)

        query += " ORDER BY last_scrubbed ASC NULLS FIRST"

        cursor.execute(query, params)
        all_eligible = cursor.fetchall()

        # Calculate how many files to scrub
        total_eligible = len(all_eligible)
        num_to_scrub = max(1, int(total_eligible * (percentage / 100.0)))

        logger.info(
            f"[database] Selected {num_to_scrub} files for scrubbing "
            f"({percentage}% of {total_eligible} eligible files)"
        )

        return [
            FileRecord(
                abs_path=row["abs_path"],
                hash=row["hash"],
                added_at=row["added_at"],
                last_seen=row["last_seen"],
                last_scrubbed=row["last_scrubbed"],
                scrub_count=row["scrub_count"],
                file_size=row["file_size"],
                mtime=row["mtime"],
            )
            for row in all_eligible[:num_to_scrub]
        ]

    def get_all_files(self) -> dict[str, FileRecord]:
        """Get all files from the database.

        Returns:
            Dictionary mapping absolute paths to FileRecord objects
        """
        if not self.conn:
            raise RuntimeError("Database not initialized")

        cursor = self.conn.cursor()
        cursor.execute("SELECT * FROM files")

        return {
            row["abs_path"]: FileRecord(
                abs_path=row["abs_path"],
                hash=row["hash"],
                added_at=row["added_at"],
                last_seen=row["last_seen"],
                last_scrubbed=row["last_scrubbed"],
                scrub_count=row["scrub_count"],
                file_size=row["file_size"],
                mtime=row["mtime"],
            )
            for row in cursor.fetchall()
        }

    def update_scrub_status(self, abs_path: str) -> None:
        """Update last_scrubbed timestamp and increment scrub_count.

        Args:
            abs_path: Absolute path to the file
        """
        if not self.conn:
            raise RuntimeError("Database not initialized")

        now = datetime.now().isoformat()
        self.conn.execute(
            """
            UPDATE files 
            SET last_scrubbed = ?, scrub_count = scrub_count + 1
            WHERE abs_path = ?
            """,
            (now, abs_path),
        )
        self.conn.commit()
        logger.debug(f"Updated scrub status for: {abs_path}")

    def close(self) -> None:
        """Close the database connection."""
        if self.conn:
            self.conn.close()
            logger.debug("Database connection closed")
