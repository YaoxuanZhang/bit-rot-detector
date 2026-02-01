"""Unit tests for configuration module."""

import os
from pathlib import Path

import pytest

from bit_rot.config import Config, load_config


class TestConfig:
    """Tests for Config dataclass and load_config function."""

    def test_load_config_success(self, temp_dir: Path, monkeypatch):
        """Test successful configuration loading from environment variables."""
        # Create test directory
        test_target = temp_dir / "test_drive"
        test_target.mkdir()

        # Set environment variables
        monkeypatch.setenv("TARGET_DIRECTORY", str(test_target))
        monkeypatch.setenv("SMTP_HOST", "mail.smtp2go.com")
        monkeypatch.setenv("SMTP_PORT", "587")
        monkeypatch.setenv("SMTP_USERNAME", "user@example.com")
        monkeypatch.setenv("SMTP_PASSWORD", "password123")
        monkeypatch.setenv("SMTP_SENDER", "sender@example.com")
        monkeypatch.setenv("SMTP_RECIPIENT", "recipient@example.com")
        monkeypatch.setenv("NOTIFY_ON_SUCCESS", "true")
        monkeypatch.setenv("SCRUB_PERCENTAGE", "5.0")
        monkeypatch.setenv("SCRUB_FREQUENCY", "weekly")
        monkeypatch.setenv("LOG_RETENTION_DAYS", "14")
        monkeypatch.setenv("MAX_WORKERS", "8")

        # Load config
        config = load_config()

        # Assertions
        assert len(config.target_paths) == 1
        assert config.target_paths[0] == test_target
        assert config.email_config.smtp_host == "mail.smtp2go.com"
        assert config.email_config.smtp_port == 587
        assert config.email_config.notify_on_success is True
        assert config.scrub_percentage == 5.0
        assert config.scrub_frequency == "weekly"
        assert config.log_retention_days == 14
        assert config.max_workers == 8

    def test_load_config_missing_target_directory(self, monkeypatch):
        """Test that missing TARGET_DIRECTORY raises ValueError."""
        monkeypatch.delenv("TARGET_DIRECTORY", raising=False)

        with pytest.raises(ValueError, match="TARGET_DIRECTORY not set"):
            load_config()

    def test_load_config_invalid_target_directory(self, monkeypatch):
        """Test that non-existent target directory raises ValueError."""
        monkeypatch.setenv("TARGET_DIRECTORY", "/nonexistent/path")

        with pytest.raises(ValueError, match="does not exist"):
            load_config()

    def test_load_config_invalid_scrub_percentage(self, temp_dir: Path, monkeypatch):
        """Test that invalid scrub percentage raises ValueError."""
        test_target = temp_dir / "test_drive"
        test_target.mkdir()

        monkeypatch.setenv("TARGET_DIRECTORY", str(test_target))
        monkeypatch.setenv("SCRUB_PERCENTAGE", "150.0")  # Invalid: > 100

        with pytest.raises(ValueError, match="SCRUB_PERCENTAGE must be between"):
            load_config()

    def test_load_config_invalid_scrub_frequency(self, temp_dir: Path, monkeypatch):
        """Test that invalid scrub frequency raises ValueError."""
        test_target = temp_dir / "test_drive"
        test_target.mkdir()

        monkeypatch.setenv("TARGET_DIRECTORY", str(test_target))
        monkeypatch.setenv("SCRUB_FREQUENCY", "hourly")  # Invalid

        with pytest.raises(ValueError, match="SCRUB_FREQUENCY must be"):
            load_config()

    def test_load_config_invalid_max_workers(self, temp_dir: Path, monkeypatch):
        """Test that invalid max workers raises ValueError."""
        test_target = temp_dir / "test_drive"
        test_target.mkdir()

        monkeypatch.setenv("TARGET_DIRECTORY", str(test_target))
        monkeypatch.setenv("MAX_WORKERS", "0")  # Invalid: < 1

        with pytest.raises(ValueError, match="MAX_WORKERS must be at least 1"):
            load_config()

    def test_load_config_multiple_target_directories(self, temp_dir: Path, monkeypatch):
        """Test loading multiple target directories."""
        # Create two test directories
        target1 = temp_dir / "drive1"
        target2 = temp_dir / "drive2"
        target1.mkdir()
        target2.mkdir()

        monkeypatch.setenv("TARGET_DIRECTORY", f"{target1},{target2}")

        config = load_config()

        assert len(config.target_paths) == 2
        assert target1 in config.target_paths
        assert target2 in config.target_paths

    def test_load_config_duplicate_paths(self, temp_dir: Path, monkeypatch):
        """Test that duplicate paths are removed."""
        target = temp_dir / "drive"
        target.mkdir()

        # Specify same path twice
        monkeypatch.setenv("TARGET_DIRECTORY", f"{target},{target}")

        config = load_config()

        # Should only have one path after deduplication
        assert len(config.target_paths) == 1
        assert config.target_paths[0] == target

    def test_load_config_defaults(self, temp_dir: Path, monkeypatch):
        """Test that default values are used when env vars not set."""
        test_target = temp_dir / "test_drive"
        test_target.mkdir()

        # Set only required env vars
        monkeypatch.setenv("TARGET_DIRECTORY", str(test_target))

        # Clear optional env vars
        for key in [
            "SMTP_HOST",
            "SMTP_PORT",
            "SMTP_USERNAME",
            "SMTP_PASSWORD",
            "SMTP_SENDER",
            "SMTP_RECIPIENT",
            "NOTIFY_ON_SUCCESS",
            "NOTIFY_SYNC_SUCCESS",  # Old logic cleaning
            "NOTIFY_SCRUB_SUCCESS",  # Old logic cleaning
            "NOTIFY_CRITICAL_FAILURES",  # Old logic cleaning
            "SCRUB_PERCENTAGE",
            "SCRUB_FREQUENCY",
            "LOG_RETENTION_DAYS",
            "MAX_WORKERS",
        ]:
            monkeypatch.delenv(key, raising=False)

        config = load_config()

        # Check defaults
        assert config.email_config.smtp_host == "mail.smtp2go.com"
        assert config.email_config.smtp_port == 587
        assert config.email_config.notify_on_success is True  # Default is True
        assert config.scrub_percentage == 1.0
        assert config.scrub_frequency == "daily"
        assert config.log_retention_days == 7
        assert config.max_workers == 4
