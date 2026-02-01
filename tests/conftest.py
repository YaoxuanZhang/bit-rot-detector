"""Pytest configuration and shared fixtures."""

import tempfile
from pathlib import Path
from typing import Generator

import pytest

from bit_rot.config import Config
from bit_rot.mailer import EmailConfig


@pytest.fixture
def temp_dir() -> Generator[Path, None, None]:
    """Provide a temporary directory for testing.

    Yields:
        Path to temporary directory (automatically cleaned up)
    """
    with tempfile.TemporaryDirectory() as tmp:
        yield Path(tmp)


@pytest.fixture
def mock_email_config() -> EmailConfig:
    """Provide a mock email configuration for testing.

    Returns:
        EmailConfig with test settings
    """
    return EmailConfig(
        smtp_host="test.smtp.com",
        smtp_port=587,
        smtp_username="test_user",
        smtp_password="test_pass",
        sender="test@example.com",
        recipient="recipient@example.com",
        notify_sync_success=True,
        notify_scrub_success=True,
        notify_critical_failures=True,
    )


@pytest.fixture
def mock_config(temp_dir: Path, mock_email_config: EmailConfig) -> Config:
    """Provide a mock application configuration for testing.

    Args:
        temp_dir: Temporary directory fixture
        mock_email_config: Mock email configuration fixture

    Returns:
        Config with test settings
    """
    # Create test directory
    test_target = temp_dir / "test_target"
    test_target.mkdir()

    return Config(
        target_paths=[test_target],
        email_config=mock_email_config,
        scrub_percentage=1.0,
        scrub_frequency="daily",
        log_retention_days=7,
        max_workers=2,
    )
