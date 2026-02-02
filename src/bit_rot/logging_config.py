"""Logging configuration for Bit Rot Detector."""

import logging
import sys
import threading
from datetime import datetime, timedelta
from pathlib import Path

# Thread-local storage for context
thread_local = threading.local()


class DirectoryContextFilter(logging.Filter):
    """Injects directory context into log records."""

    def filter(self, record):
        """Add directory context to the record if available."""
        if hasattr(thread_local, "directory"):
            record.directory = f" [{thread_local.directory}]"
        else:
            record.directory = ""
        return True


def setup_logging(log_retention_days: int = 7, log_level: str = "INFO") -> None:
    """Configure logging with refined format, dual output, and rotation.

    Args:
        log_retention_days: Number of days to keep log files
        log_level: Console log level (DEBUG, INFO, WARNING, ERROR, CRITICAL)
    """
    # Create logs directory
    logs_dir = Path("logs")
    logs_dir.mkdir(exist_ok=True)

    # Force UTF-8 encoding for console output (fixes Windows Unicode errors)
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            try:
                stream.reconfigure(encoding="utf-8")
            except Exception:
                pass

    # Generate timestamped log filename
    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    log_file = logs_dir / f"bitrot_{timestamp}.log"

    # Clean up old log files
    cleanup_old_logs(logs_dir, log_retention_days)

    # Include logger name (module) in format for debugging
    # Add directory context if available
    log_format = "[%(asctime)s] [%(levelname)s] [%(name)s]%(directory)s %(message)s"
    date_format = "%Y-%m-%d %H:%M:%S"

    # Create logger
    root_logger = logging.getLogger()
    root_logger.setLevel(logging.DEBUG)

    # Clear any existing handlers to avoid duplicates
    root_logger.handlers.clear()

    # Create context filter
    context_filter = DirectoryContextFilter()

    # Console handler - use configurable log level
    console_handler = logging.StreamHandler()
    console_handler.setLevel(getattr(logging, log_level))
    console_formatter = logging.Formatter(log_format, date_format)
    console_handler.setFormatter(console_formatter)
    console_handler.addFilter(context_filter)
    root_logger.addHandler(console_handler)

    # File handler (DEBUG and above) - timestamped file
    file_handler = logging.FileHandler(log_file, encoding="utf-8")
    file_handler.setLevel(logging.DEBUG)
    file_formatter = logging.Formatter(log_format, date_format)
    file_handler.setFormatter(file_formatter)
    file_handler.addFilter(context_filter)
    root_logger.addHandler(file_handler)

    logging.info(f"Logging to: {log_file}")
    logging.info(f"Console log level: {log_level}")


def cleanup_old_logs(logs_dir: Path, retention_days: int) -> None:
    """Remove log files older than retention period.

    Args:
        logs_dir: Directory containing log files
        retention_days: Number of days to keep logs
    """
    if retention_days <= 0:
        return

    cutoff_time = datetime.now() - timedelta(days=retention_days)
    deleted_count = 0

    for log_file in logs_dir.glob("bitrot_*.log"):
        try:
            file_mtime = datetime.fromtimestamp(log_file.stat().st_mtime)
            if file_mtime < cutoff_time:
                log_file.unlink()
                deleted_count += 1
        except (OSError, ValueError) as e:
            # Log error but continue cleanup
            logging.warning(f"Error cleaning up log file {log_file}: {e}")

    if deleted_count > 0:
        logging.info(f"Cleaned up {deleted_count} old log file(s)")
