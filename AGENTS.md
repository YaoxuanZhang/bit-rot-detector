# Agent Guide for Bit Rot Detector

This document is designed to help AI agents understand and work with the Bit Rot Detector codebase.

## Project Overview

Bit Rot Detector is a utility for verifying file integrity using BLAKE3 hashing. It is designed to run on a schedule (cron) to detect silent file corruption (bit rot).

## Key Technologies

- **Language**: Python 3.11+
- **Dependency Management**: `uv`
- **Testing**: `pytest`
- **Database**: SQLite (`bitrot.db` in target directories)
- **Hashing**: BLAKE3

## Repository Structure

- `src/bit_rot/`: Source code package
  - `__init__.py`: Package initialization
  - `main.py`: Entry point and CLI logic
  - `scanner.py`: File scanning and hashing logic
  - `database.py`: SQLite interactions
  - `mailer.py`: Email notification logic
  - `config.py`: Configuration handling
  - `coordinator.py`: Orchestrates multi-drive scanning
  - `drive_monitor.py`: Hardware monitoring (disk usage, SMART)
  - `hasher.py`: BLAKE3 hashing utilities
  - `logging_config.py`: Logging setup
- `tests/`: Test suite
  - `conftest.py`: Pytest fixtures and configuration
  - `test_integration.py`: End-to-end integration tests
  - `test_config.py`: Configuration unit tests
  - `test_database.py`: Database unit tests
  - `test_hasher.py`: Hashing unit tests
  - `test_mailer.py`: Mailer unit tests
- `logs/`: Directory for log files
- `htmlcov/`: Coverage reports

## Development Workflow

### Dependency Management

This project uses [uv](https://github.com/astral-sh/uv).

- **Install dependencies**: `uv sync`
- **Add dependency**: `uv add <package>`
- **Add dev dependency**: `uv add --dev <package>`

### Running the Application

Use `uv run` to execute the application in the virtual environment.

```bash
uv run bit-rot-detector [args]
```

### Testing

Always run tests before submitting changes.

```bash
# Run all tests
uv run pytest

# Run with coverage
uv run pytest --cov=src/bit_rot
```

## Common Tasks for Agents

### Adding a New Feature

1.  **Plan**: Analyze requirements and existing code.
2.  **Test**: Create a reproduction case or a new test in `tests/`.
3.  **Implement**: Modify code in `src/bit_rot/`.
4.  **Verify**: Run `uv run pytest` to ensure no regressions.

### Fixing a Bug

1.  **Reproduce**: Create a failing test case in `tests/`.
2.  **Debug**: Use logging or print statements to trace the issue.
3.  **Fix**: Apply the fix.
4.  **Verify**: Ensure the test passes and no other tests fail.

## Configuration

Configuration is loaded from environment variables (`.env`). See `.env.example` for reference. Key variables:

- `TARGET_DIRECTORY`: Path to monitor
- `SMTP_*`: Email settings
- `SCRUB_PERCENTAGE`: Fraction of files to re-check
- `MAX_WORKERS`: Concurrency level

## Beware

- **Symlinks**: The tool intentionally ignores symlinks to avoid infinite loops.
- **Canary File**: The `.bitrot-canary` file is critical. Do not remove checks for it.
- **Database Schema**: Changes to `src/bit_rot/database.py` schema must be handled carefully (migrations are not essentially complex but ensure backward compatibility if possible).

## Style Guide

- Follow PEP 8.
- Use type hints.
- Write docstrings for new functions and classes.
