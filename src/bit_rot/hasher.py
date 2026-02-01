"""BLAKE3 hashing module for file integrity verification."""

import logging
from pathlib import Path

import blake3

logger = logging.getLogger(__name__)

# Chunk size for reading files (64KB)
CHUNK_SIZE = 65536


class Hasher:
    """BLAKE3 hash computation with chunked file reading."""

    def __init__(self, stop_event=None):
        """Initialize hasher.

        Args:
            stop_event: Optional threading.Event to signal cancellation
        """
        self.stop_event = stop_event

    def compute_hash(self, filepath: Path) -> str:
        """Compute BLAKE3 hash of a file.

        Args:
            filepath: Path to the file to hash

        Returns:
            Hexadecimal hash string

        Raises:
            PermissionError: If file cannot be read due to permissions
            OSError: If file cannot be read due to I/O errors
            KeyboardInterrupt: If operation is cancelled via stop_event
        """
        try:
            hasher = blake3.blake3()

            with open(filepath, "rb") as f:
                while chunk := f.read(CHUNK_SIZE):
                    if self.stop_event and self.stop_event.is_set():
                        raise KeyboardInterrupt("Hashing interrupted")
                    hasher.update(chunk)

            hash_value = hasher.hexdigest()
            logger.debug(f"Computed hash for {filepath}: {hash_value[:16]}...")
            return hash_value

        except PermissionError as e:
            logger.warning(f"Permission denied reading {filepath}: {e}")
            raise
        except OSError as e:
            logger.warning(f"I/O error reading {filepath}: {e}")
            raise
