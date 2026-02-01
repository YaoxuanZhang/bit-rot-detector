"""Unit tests for hasher module."""

from pathlib import Path

import pytest

from bit_rot.hasher import Hasher


class TestHasher:
    """Tests for Hasher class."""

    @pytest.fixture
    def hasher(self) -> Hasher:
        """Create a Hasher instance for testing."""
        return Hasher()

    def test_hash_file_success(self, hasher: Hasher, temp_dir: Path):
        """Test successful file hashing."""
        # Create test file
        test_file = temp_dir / "test.txt"
        test_file.write_text("Hello, World!")

        # Hash the file
        file_hash = hasher.compute_hash(test_file)

        # Verify hash is not empty
        assert file_hash is not None
        assert len(file_hash) > 0
        # BLAKE3 hashes are hex strings
        assert all(c in "0123456789abcdef" for c in file_hash)

    def test_hash_file_consistency(self, hasher: Hasher, temp_dir: Path):
        """Test that hashing same file produces same hash."""
        test_file = temp_dir / "test.txt"
        test_file.write_text("Test content")

        hash1 = hasher.compute_hash(test_file)
        hash2 = hasher.compute_hash(test_file)

        assert hash1 == hash2

    def test_hash_file_different_content(self, hasher: Hasher, temp_dir: Path):
        """Test that different content produces different hashes."""
        file1 = temp_dir / "file1.txt"
        file2 = temp_dir / "file2.txt"

        file1.write_text("Content A")
        file2.write_text("Content B")

        hash1 = hasher.compute_hash(file1)
        hash2 = hasher.compute_hash(file2)

        assert hash1 != hash2

    def test_hash_file_nonexistent(self, hasher: Hasher, temp_dir: Path):
        """Test hashing nonexistent file raises error."""
        nonexistent = temp_dir / "nonexistent.txt"

        with pytest.raises(FileNotFoundError):
            hasher.compute_hash(nonexistent)

    def test_hash_file_large_file(self, hasher: Hasher, temp_dir: Path):
        """Test hashing larger file (tests chunked reading)."""
        # Create a 10MB file
        large_file = temp_dir / "large.bin"
        large_file.write_bytes(b"X" * (10 * 1024 * 1024))

        file_hash = hasher.compute_hash(large_file)

        assert file_hash is not None
        assert len(file_hash) > 0

    def test_hash_file_empty_file(self, hasher: Hasher, temp_dir: Path):
        """Test hashing empty file."""
        empty_file = temp_dir / "empty.txt"
        empty_file.write_text("")

        file_hash = hasher.compute_hash(empty_file)

        # Empty file should still produce a hash
        assert file_hash is not None
        assert len(file_hash) > 0

    def test_hash_file_binary_content(self, hasher: Hasher, temp_dir: Path):
        """Test hashing binary file."""
        binary_file = temp_dir / "binary.bin"
        binary_file.write_bytes(bytes(range(256)))

        file_hash = hasher.compute_hash(binary_file)

        assert file_hash is not None
        assert len(file_hash) > 0
