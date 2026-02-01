"""Unit tests for database module."""

from datetime import datetime
from pathlib import Path

import pytest

from bit_rot.database import Database, FileRecord


class TestDatabase:
    """Tests for Database class."""

    @pytest.fixture
    def db(self, temp_dir: Path) -> Database:
        """Create a test database instance."""
        db_path = temp_dir / "test.db"
        return Database(db_path)

    def test_database_creation(self, temp_dir: Path):
        """Test database file is created."""
        db_path = temp_dir / "test.db"
        db = Database(db_path)

        assert db_path.exists()
        db.close()

    def test_stage_and_commit_file(self, db: Database):
        """Test staging and committing a file."""
        with db.transaction():
            db.stage_file_update(
                abs_path="/test/file.txt",
                hash="abc123",
                file_size=1024,
                mtime=123456.0,
            )

        # Verify file was added
        files = db.get_all_files()
        assert "/test/file.txt" in files
        assert files["/test/file.txt"].hash == "abc123"
        assert files["/test/file.txt"].file_size == 1024

    def test_transaction_rollback(self, db: Database):
        """Test transaction rollback on error."""
        try:
            with db.transaction():
                db.stage_file_update(
                    abs_path="/test/file.txt",
                    hash="abc123",
                    file_size=1024,
                    mtime=123456.0,
                )
                # Simulate error
                raise Exception("Test error")
        except Exception:
            pass

        # Verify file was not added (rolled back)
        files = db.get_all_files()
        assert "/test/file.txt" not in files

    def test_update_existing_file(self, db: Database):
        """Test updating an existing file."""
        # Add initial file
        with db.transaction():
            db.stage_file_update(
                abs_path="/test/file.txt",
                hash="abc123",
                file_size=1024,
                mtime=123456.0,
            )

        # Update the file
        with db.transaction():
            db.stage_file_update(
                abs_path="/test/file.txt",
                hash="def456",  # New hash
                file_size=2048,  # New size
                mtime=123457.0,
            )

        # Verify file was updated
        files = db.get_all_files()
        assert files["/test/file.txt"].hash == "def456"
        assert files["/test/file.txt"].file_size == 2048

    def test_stage_file_removal(self, db: Database):
        """Test removing a file."""
        # Add file
        with db.transaction():
            db.stage_file_update(
                abs_path="/test/file.txt",
                hash="abc123",
                file_size=1024,
                mtime=123456.0,
            )

        # Remove file
        with db.transaction():
            db.stage_file_removal("/test/file.txt")

        # Verify file was removed
        files = db.get_all_files()
        assert "/test/file.txt" not in files

    def test_get_files_for_scrub(self, db: Database):
        """Test getting files for scrubbing."""
        # Add some files
        with db.transaction():
            for i in range(10):
                db.stage_file_update(
                    abs_path=f"/test/file{i}.txt",
                    hash=f"hash{i}",
                    file_size=1024,
                    mtime=123456.0,
                )

        # Get 50% of files for scrubbing
        scrub_files = db.get_files_for_scrub(percentage=50.0)

        # Should return approximately 5 files
        assert 4 <= len(scrub_files) <= 6

    def test_update_scrub_status(self, db: Database):
        """Test updating scrub status."""
        # Add file
        with db.transaction():
            db.stage_file_update(
                abs_path="/test/file.txt",
                hash="abc123",
                file_size=1024,
                mtime=123456.0,
            )

        # Update scrub status
        db.update_scrub_status("/test/file.txt")

        # Verify scrub count increased
        files = db.get_all_files()
        assert files["/test/file.txt"].scrub_count == 1
        assert files["/test/file.txt"].last_scrubbed is not None

    def test_get_all_files_empty_database(self, db: Database):
        """Test getting all files from empty database."""
        files = db.get_all_files()
        assert len(files) == 0
        assert isinstance(files, dict)

    def test_preserve_scrub_history(self, db: Database):
        """Test that scrub history is preserved during updates."""
        # Add file and scrub it
        with db.transaction():
            db.stage_file_update(
                abs_path="/test/file.txt",
                hash="abc123",
                file_size=1024,
                mtime=123456.0,
            )

        db.update_scrub_status("/test/file.txt")

        # Update file (e.g., moved)
        with db.transaction():
            db.stage_file_update(
                abs_path="/test/file.txt",
                hash="abc123",  # Same hash
                file_size=1024,
                mtime=123456.0,
                scrub_count=1,  # Preserve scrub count
            )

        # Verify scrub history preserved
        files = db.get_all_files()
        assert files["/test/file.txt"].scrub_count == 1
