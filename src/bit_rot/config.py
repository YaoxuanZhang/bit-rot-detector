"""Configuration module for Bit Rot Detector."""

import os
from dataclasses import dataclass
from pathlib import Path

from .mailer import EmailConfig


@dataclass
class Config:
    """Application configuration settings."""

    target_paths: list[Path]
    email_config: EmailConfig
    scrub_percentage: float
    scrub_frequency: str
    log_retention_days: int
    max_workers: int
    log_level: str


def load_config() -> Config:
    """Load configuration from environment variables.

    Returns:
        Config object with all application settings

    Raises:
        ValueError: If required configuration is missing or invalid
    """
    # Parse target directories
    target_dirs_str = os.getenv("TARGET_DIRECTORY")
    if not target_dirs_str:
        raise ValueError("TARGET_DIRECTORY not set in environment")

    # Parse comma-separated paths
    target_dirs = [d.strip() for d in target_dirs_str.split(",") if d.strip()]

    if not target_dirs:
        raise ValueError("TARGET_DIRECTORY is empty")

    # Validate and convert to Path objects
    target_paths = []
    for target_dir in target_dirs:
        target_path = Path(target_dir)
        if not target_path.exists():
            raise ValueError(f"Target directory does not exist: {target_dir}")
        target_paths.append(target_path)

    # Remove duplicates while preserving order
    seen = set()
    unique_paths = []
    for path in target_paths:
        abs_path = path.resolve()
        if abs_path not in seen:
            seen.add(abs_path)
            unique_paths.append(path)
    target_paths = unique_paths

    # Email configuration
    email_config = EmailConfig(
        smtp_host=os.getenv("SMTP_HOST", "mail.smtp2go.com"),
        smtp_port=int(os.getenv("SMTP_PORT", "587")),
        smtp_username=os.getenv("SMTP_USERNAME", ""),
        smtp_password=os.getenv("SMTP_PASSWORD", ""),
        sender=os.getenv("SMTP_SENDER", ""),
        recipient=os.getenv("SMTP_RECIPIENT", ""),
        notify_on_success=os.getenv("NOTIFY_ON_SUCCESS", "true").lower() == "true",
    )

    # Scrub configuration
    scrub_percentage = float(os.getenv("SCRUB_PERCENTAGE", "1.0"))
    if not 0.1 <= scrub_percentage <= 100.0:
        raise ValueError(
            f"SCRUB_PERCENTAGE must be between 0.1 and 100.0, got {scrub_percentage}"
        )

    scrub_frequency = os.getenv("SCRUB_FREQUENCY", "daily").lower()
    if scrub_frequency not in ["daily", "weekly", "monthly"]:
        raise ValueError(
            f"SCRUB_FREQUENCY must be daily/weekly/monthly, got {scrub_frequency}"
        )

    # Log retention configuration
    log_retention_days = int(os.getenv("LOG_RETENTION_DAYS", "7"))

    # Worker configuration
    max_workers = int(os.getenv("MAX_WORKERS", "4"))
    if max_workers < 1:
        raise ValueError(f"MAX_WORKERS must be at least 1, got {max_workers}")

    # Log level configuration
    log_level = os.getenv("LOG_LEVEL", "INFO").upper()
    valid_log_levels = ["DEBUG", "INFO", "WARNING", "ERROR", "CRITICAL"]
    if log_level not in valid_log_levels:
        raise ValueError(
            f"LOG_LEVEL must be one of {', '.join(valid_log_levels)}, got {log_level}"
        )

    return Config(
        target_paths=target_paths,
        email_config=email_config,
        scrub_percentage=scrub_percentage,
        scrub_frequency=scrub_frequency,
        log_retention_days=log_retention_days,
        max_workers=max_workers,
        log_level=log_level,
    )
