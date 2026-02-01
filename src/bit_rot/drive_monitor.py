"""Drive health monitoring module for disk usage and SMART statistics."""

import logging
import shutil
import subprocess
from dataclasses import dataclass, field
from pathlib import Path
from typing import Optional

logger = logging.getLogger(__name__)


@dataclass
class DriveHealth:
    """Drive health and usage statistics."""

    drive_name: str
    total_space: int  # bytes
    used_space: int  # bytes
    free_space: int  # bytes
    temperature: Optional[int] = None  # Celsius
    smart_status: str = "UNKNOWN"  # "PASSED", "FAILED", "UNKNOWN"
    smart_errors: list[str] = field(default_factory=list)


def get_drive_health(drive_path: Path) -> DriveHealth:
    """Collect drive health statistics.

    Args:
        drive_path: Path to the drive

    Returns:
        DriveHealth object with usage and SMART stats
    """
    drive_name = drive_path.name

    # Get disk usage (cross-platform, always works)
    try:
        usage = shutil.disk_usage(drive_path)
        total_space = usage.total
        used_space = usage.used
        free_space = usage.free
    except Exception as e:
        logger.warning(f"[{drive_name}] Failed to get disk usage: {e}")
        # Return minimal health info if disk usage fails
        return DriveHealth(
            drive_name=drive_name,
            total_space=0,
            used_space=0,
            free_space=0,
            smart_errors=[f"Failed to get disk usage: {e}"],
        )

    # Try to get SMART data (optional, may require sudo)
    temperature = None
    smart_status = "UNKNOWN"
    smart_errors = []

    try:
        # Try to get SMART data using smartctl
        # This may fail if smartctl is not installed or needs sudo
        result = subprocess.run(
            ["smartctl", "-A", "-H", str(drive_path)],
            capture_output=True,
            text=True,
            timeout=5,
        )

        # Parse output even if return code is non-zero (smartctl returns non-zero for warnings)
        output = result.stdout

        # Extract temperature
        for line in output.split("\n"):
            # Common temperature attributes
            if "Temperature_Celsius" in line or "Airflow_Temperature_Cel" in line:
                parts = line.split()
                if len(parts) >= 10:
                    try:
                        temperature = int(parts[9])
                        break
                    except (ValueError, IndexError):
                        pass

        # Extract overall health status
        for line in output.split("\n"):
            if "SMART overall-health self-assessment test result:" in line:
                if "PASSED" in line:
                    smart_status = "PASSED"
                elif "FAILED" in line:
                    smart_status = "FAILED"
                break

    except FileNotFoundError:
        logger.debug(f"[{drive_name}] smartctl not found - skipping SMART monitoring")
        smart_errors.append("smartctl not installed")
    except subprocess.TimeoutExpired:
        logger.warning(f"[{drive_name}] smartctl timed out")
        smart_errors.append("smartctl timeout")
    except Exception as e:
        logger.debug(f"[{drive_name}] Failed to get SMART data: {e}")
        smart_errors.append(f"SMART unavailable: {e}")

    return DriveHealth(
        drive_name=drive_name,
        total_space=total_space,
        used_space=used_space,
        free_space=free_space,
        temperature=temperature,
        smart_status=smart_status,
        smart_errors=smart_errors,
    )


def format_bytes(bytes_value: int) -> str:
    """Format bytes to human-readable string.

    Args:
        bytes_value: Number of bytes

    Returns:
        Formatted string (e.g., "1.5 GB", "512 MB")
    """
    if bytes_value == 0:
        return "0 B"

    units = ["B", "KB", "MB", "GB", "TB", "PB"]
    unit_index = 0
    value = float(bytes_value)

    while value >= 1024 and unit_index < len(units) - 1:
        value /= 1024
        unit_index += 1

    # Format with appropriate precision
    if value >= 100:
        return f"{value:.0f} {units[unit_index]}"
    elif value >= 10:
        return f"{value:.1f} {units[unit_index]}"
    else:
        return f"{value:.2f} {units[unit_index]}"


def format_drive_health(health: DriveHealth) -> str:
    """Format drive health for display in email.

    Args:
        health: DriveHealth object

    Returns:
        Formatted string for email report
    """
    # Format disk usage
    used_str = format_bytes(health.used_space)
    total_str = format_bytes(health.total_space)

    if health.total_space > 0:
        used_pct = (health.used_space / health.total_space) * 100
        usage_info = f"{used_str} / {total_str} ({used_pct:.0f}%)"
    else:
        usage_info = "N/A"

    # Format temperature
    if health.temperature is not None:
        temp_info = f"{health.temperature}°C"
    else:
        temp_info = "N/A"

    # Format SMART status
    status_info = health.smart_status

    # Pad drive name for alignment (20 chars)
    drive_label = f"{health.drive_name}:".ljust(20)

    return (
        f"{drive_label} {usage_info:>35} | Temp: {temp_info:>5} | Health: {status_info}"
    )
