"""Logging configuration for Bit Rot Detector."""

import logging
from datetime import datetime, timedelta
from pathlib import Path


def setup_logging(log_retention_days: int = 7) -> None:
    """Configure logging with refined format, dual output, and rotation.

    Args:
        log_retention_days: Number of days to keep log files
    """
    # Create logs directory
    logs_dir = Path("logs")
    logs_dir.mkdir(exist_ok=True)

    # Generate timestamped log filename
    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    log_file = logs_dir / f"bitrot_{timestamp}.log"

    # Clean up old log files
    cleanup_old_logs(logs_dir, log_retention_days)

    # Include logger name (module) in format for debugging
    log_format = "[%(asctime)s] [%(levelname)s] [%(name)s] %(message)s"
    date_format = "%Y-%m-%d %H:%M:%S"

    # Create logger
    root_logger = logging.getLogger()
    root_logger.setLevel(logging.DEBUG)

    # Clear any existing handlers to avoid duplicates
    root_logger.handlers.clear()

    # Console handler (INFO and above)
    console_handler = logging.StreamHandler()
    console_handler.setLevel(logging.INFO)
    console_formatter = logging.Formatter(log_format, date_format)
    console_handler.setFormatter(console_formatter)
    root_logger.addHandler(console_handler)

    # File handler (DEBUG and above) - timestamped file
    file_handler = logging.FileHandler(log_file)
    file_handler.setLevel(logging.DEBUG)
    file_formatter = logging.Formatter(log_format, date_format)
    file_handler.setFormatter(file_formatter)
    root_logger.addHandler(file_handler)

    logging.info(f"Logging to: {log_file}")


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
