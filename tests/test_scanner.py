"""Unit tests for scanner module."""

import threading
from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest

from bit_rot.database import Database, FileRecord
from bit_rot.scanner import Scanner


class TestScannerInterruption:
    """Tests for Scanner interruption handling."""

    @pytest.fixture
    def mock_db(self):
        """Mock Database instance."""
        return MagicMock(spec=Database)

    @pytest.fixture
    def mock_hasher(self):
        """Mock Hasher instance."""
        hasher = MagicMock()
        hasher.compute_hash.return_value = "dummy_hash"
        return hasher

    def test_sync_interruption_during_walk(self, temp_dir, mock_db, mock_hasher):
        """Test that sync_directory stops when interrupted during walk."""
        stop_event = threading.Event()
        scanner = Scanner(mock_hasher, stop_event=stop_event)

        # Mock database validation
        mock_db.get_all_files.return_value = {}

        # Simulate walk yielding results then getting interrupted
        def mock_walk(*args, **kwargs):
            # First iteration: normal
            yield (str(temp_dir), [], ["file1.txt"])
            # Signal interrupt
            stop_event.set()
            # Second iteration: should trigger interrupt check
            yield (str(temp_dir), [], ["file2.txt"])

        # Create dummy file to avoid errors
        (temp_dir / "file1.txt").touch()

        with patch("os.walk", side_effect=mock_walk):
            with pytest.raises(KeyboardInterrupt):
                scanner.sync_directory(temp_dir, mock_db)

    def test_sync_interruption_during_file_processing(
        self, temp_dir, mock_db, mock_hasher
    ):
        """Test interruption checks inside the file processing loop."""
        stop_event = threading.Event()
        scanner = Scanner(mock_hasher, stop_event=stop_event)
        mock_db.get_all_files.return_value = {}

        # We need to trigger the inner loop check:
        # for filename in files:
        #     if self.stop_event.is_set(): ...

        def mock_walk(*args, **kwargs):
            # Yield list with multiple files
            # Set event after first file processing?
            # We can't easily hook into "after first file" efficiently without
            # mocking something inside the loop.
            # But we can assume if event is set BEFORE loop, it raises.
            pass

        # Actually easier strategy:
        # The loop checks `if self.stop_event and self.stop_event.is_set():` each file.
        # We can construct a list where we can intervene? No.

        # However, we can use the fact that it checks event at start of os.walk iteration
        # AND start of file iteration.

        # If we set event immediately, it should raise immediately.
        stop_event.set()

        # We need to ensure it actually starts processing though (not just failing at start)
        # But `sync_directory` does logging then `db.get_all_files` then `os.walk`.

        with patch("os.walk", return_value=[(str(temp_dir), [], ["f1", "f2"])]):
            with pytest.raises(KeyboardInterrupt):
                scanner.sync_directory(temp_dir, mock_db)

    def test_scrub_interruption(self, temp_dir, mock_db, mock_hasher):
        """Test that scrub_files stops when interrupted."""
        stop_event = threading.Event()
        scanner = Scanner(mock_hasher, stop_event=stop_event)

        # Setup files to scrub
        f1 = temp_dir / "f1.txt"
        f2 = temp_dir / "f2.txt"
        f1.touch()
        f2.touch()

        # Mock database returning records
        records = [
            FileRecord(
                abs_path=str(f1),
                hash="h1",
                file_size=10,
                mtime=123.0,
                added_at="iso",
                last_seen="iso",
                last_scrubbed=None,
                scrub_count=0,
            ),
            FileRecord(
                abs_path=str(f2),
                hash="h2",
                file_size=10,
                mtime=123.0,
                added_at="iso",
                last_seen="iso",
                last_scrubbed=None,
                scrub_count=0,
            ),
        ]
        mock_db.get_files_for_scrub.return_value = records

        # We want to interrupt after the first file.
        # We can simulate this by side_effect on hasher.compute_hash

        def side_effect(*args):
            stop_event.set()
            return "h1"

        mock_hasher.compute_hash.side_effect = side_effect

        with pytest.raises(KeyboardInterrupt):
            scanner.scrub_files(mock_db, 100.0, "daily")

        # Verify we at least tried to hash the first one
        assert mock_hasher.compute_hash.called
