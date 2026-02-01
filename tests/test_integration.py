#!/usr/bin/env python3
"""End-to-end email test - creates scenarios and sends real emails."""

import os
import shutil
import subprocess
import time
from pathlib import Path


def setup_test_directory():
    """Create a fresh test directory."""
    test_dir = Path("test_email_scenarios")

    # Clean up if exists
    if test_dir.exists():
        shutil.rmtree(test_dir)

    test_dir.mkdir()

    # Create canary
    (test_dir / ".bitrot-canary").touch()

    return test_dir


def run_bitrot(
    args: str,
    test_dir: Path = None,
    extra_env: dict = None,
    disable_notifications: bool = False,
):
    """Run bit-rot-detector with given args."""
    env = os.environ.copy()
    if test_dir:
        env["TARGET_DIRECTORY"] = str(test_dir.absolute())
    env["SCRUB_PERCENTAGE"] = "100"

    if disable_notifications:
        env["NOTIFY_ON_SUCCESS"] = "false"
    else:
        env["NOTIFY_ON_SUCCESS"] = "true"

    if extra_env:
        env.update(extra_env)

    result = subprocess.run(
        f"uv run bit-rot-detector {args}",
        shell=True,
        cwd=Path.cwd(),
        env=env,
        capture_output=True,
        text=True,
    )

    print(result.stdout)
    if result.stderr:
        print("STDERR:", result.stderr)

    return result.returncode


def scenario_1_test_email():
    """Scenario 1: Send test email."""
    print("\n" + "=" * 80)
    print("SCENARIO 1: Test Email")
    print("=" * 80)

    test_dir = setup_test_directory()

    print("\nSending test email...")
    run_bitrot("--test-email", test_dir)

    print("\nCheck your email for: 'Bit Rot Detector - Test Email'")
    input("\nPress Enter to continue to next scenario...")


def scenario_2_new_files():
    """Scenario 2: New files detected."""
    print("\n" + "=" * 80)
    print("SCENARIO 2: New Files Detected")
    print("=" * 80)

    test_dir = setup_test_directory()

    # Create some files
    print("\nCreating 10 new files...")
    for i in range(10):
        (test_dir / f"file_{i:03d}.txt").write_text(f"Content of file {i}\n" * 100)

    print("Running sync operation...")
    run_bitrot("--sync", test_dir)

    print("\nCheck your email for: 'Bit Rot Detector - 10 New Files Detected'")
    input("\nPress Enter to continue to next scenario...")


def scenario_3_modifications():
    """Scenario 3: File modifications detected."""
    print("\n" + "=" * 80)
    print("SCENARIO 3: File Modifications Detected")
    print("=" * 80)

    test_dir = setup_test_directory()

    # Create initial files
    print("\nCreating initial files...")
    for i in range(5):
        (test_dir / f"file_{i}.txt").write_text(f"Original content {i}\n" * 50)

    print("Running initial sync...")
    run_bitrot("--sync", test_dir, disable_notifications=True)

    # Wait a bit to ensure mtime changes
    time.sleep(2)

    # Modify some files
    print("\nModifying 3 files...")
    for i in range(3):
        (test_dir / f"file_{i}.txt").write_text(f"MODIFIED content {i}\n" * 75)

    print("Running sync to detect modifications...")
    run_bitrot("--sync", test_dir)

    print("\nCheck your email for: 'Bit Rot Detector - 0 New Files Detected'")
    print("   (Should show 3 files modified)")
    input("\nPress Enter to continue to next scenario...")


def scenario_4_file_moves():
    """Scenario 4: File moves detected."""
    print("\n" + "=" * 80)
    print("SCENARIO 4: File Moves Detected")
    print("=" * 80)

    test_dir = setup_test_directory()

    # Create initial files
    print("\nCreating initial files...")
    for i in range(5):
        (test_dir / f"file_{i}.txt").write_text(f"Content {i}\n" * 50)

    print("Running initial sync to add files to database...")
    run_bitrot("--sync", test_dir, disable_notifications=True)

    # Create subdirectory and move files
    print("\nMoving 3 files to subdirectory...")
    subdir = test_dir / "moved_files"
    subdir.mkdir()

    for i in range(3):
        (test_dir / f"file_{i}.txt").rename(subdir / f"file_{i}.txt")

    print("Running sync to detect moves...")
    run_bitrot("--sync", test_dir)

    print("\nCheck your email - should show 3 files moved")
    input("\nPress Enter to continue to next scenario...")


def scenario_5_file_removals():
    """Scenario 5: File removals detected."""
    print("\n" + "=" * 80)
    print("SCENARIO 5: File Removals Detected")
    print("=" * 80)

    test_dir = setup_test_directory()

    # Create initial files
    print("\nCreating initial files...")
    for i in range(8):
        (test_dir / f"file_{i}.txt").write_text(f"Content {i}\n" * 50)

    print("Running initial sync...")
    run_bitrot("--sync", test_dir, disable_notifications=True)

    # Remove some files
    print("\nRemoving 4 files...")
    for i in range(4):
        (test_dir / f"file_{i}.txt").unlink()

    print("Running sync to detect removals...")
    run_bitrot("--sync", test_dir)

    print("\nCheck your email - should show 4 files removed")
    input("\nPress Enter to continue to next scenario...")


def scenario_6_scrub_success():
    """Scenario 6: Successful scrub operation."""
    print("\n" + "=" * 80)
    print("SCENARIO 6: Successful Scrub Operation")
    print("=" * 80)

    test_dir = setup_test_directory()

    # Create files
    print("\nCreating files...")
    for i in range(8):
        (test_dir / f"file_{i}.txt").write_text(f"Content {i}\n" * 100)

    print("Running initial sync...")
    run_bitrot("--sync", test_dir, disable_notifications=True)

    print("Running scrub operation...")
    run_bitrot("--scrub", test_dir)

    print("\nCheck your email for: 'Bit Rot Detector - 8 Files Validated'")
    print("   (Should show 0 corrupted files)")
    input("\nPress Enter to continue to next scenario...")


def scenario_7_bit_rot():
    """Scenario 7: Bit rot detection."""
    print("\n" + "=" * 80)
    print("SCENARIO 7: BIT ROT DETECTION (Simulated)")
    print("=" * 80)

    test_dir = setup_test_directory()

    # Create files
    print("\nCreating files...")
    for i in range(5):
        (test_dir / f"file_{i}.txt").write_text(f"Original content {i}\n" * 100)

    print("Running initial sync to hash files...")
    run_bitrot("--sync", test_dir, disable_notifications=True)

    # Corrupt files by modifying WITHOUT updating mtime
    print("\nSimulating bit rot (corrupting files without changing mtime)...")
    import sqlite3

    # Get the database
    db_path = test_dir / "bitrot.db"

    # Corrupt 2 files by changing content but keeping same size
    for i in range(2):
        file_path = test_dir / f"file_{i}.txt"
        original_stat = file_path.stat()

        # Change content but keep same size
        file_path.write_text(f"CORRUPTED!!! {i}\n" * 100)

        # Restore original mtime to simulate bit rot
        os.utime(file_path, (original_stat.st_atime, original_stat.st_mtime))

    print("Running scrub to detect corruption...")
    run_bitrot("--scrub", test_dir)

    print("\nCheck your email for: 'BIT ROT DETECTED - 2 Corrupted Files!'")
    print("   (Should list the 2 corrupted files with full paths)")
    input("\nPress Enter to continue to next scenario...")


def scenario_8_canary_failure():
    """Scenario 8: Canary check failure."""
    print("\n" + "=" * 80)
    print("SCENARIO 8: Canary Check Failure")
    print("=" * 80)

    test_dir = setup_test_directory()

    # Create files
    print("\nCreating files...")
    for i in range(3):
        (test_dir / f"file_{i}.txt").write_text(f"Content {i}\n" * 50)

    print("Running initial sync...")
    run_bitrot("--sync", test_dir, disable_notifications=True)

    # Remove canary
    print("\nRemoving canary file to simulate unmounted drive...")
    (test_dir / ".bitrot-canary").unlink()

    print("Running sync (should fail canary check)...")
    run_bitrot("--sync", test_dir)

    print("\nCheck your email for: 'Bit Rot Detector - Critical Error'")
    print("   (Should mention canary check failed)")
    input("\nPress Enter to finish...")


def scenario_9_multi_drive():
    """Scenario 9: Multiple drives with different results."""
    print("\n" + "=" * 80)
    print("SCENARIO 9: Multiple Drives (Sync + Scrub)")
    print("=" * 80)

    # Create two test directories
    test_dir1 = Path("test_email_scenarios/drive1")
    test_dir2 = Path("test_email_scenarios/drive2")

    # Clean up if exists
    if Path("test_email_scenarios").exists():
        shutil.rmtree("test_email_scenarios")

    test_dir1.mkdir(parents=True)
    test_dir2.mkdir(parents=True)

    # Create canaries
    (test_dir1 / ".bitrot-canary").touch()
    (test_dir2 / ".bitrot-canary").touch()

    # Create files on drive 1
    print("\nCreating files on Drive 1...")
    for i in range(8):
        (test_dir1 / f"doc_{i}.txt").write_text(f"Drive 1 content {i}\n" * 100)

    # Create files on drive 2
    print("Creating files on Drive 2...")
    for i in range(5):
        (test_dir2 / f"backup_{i}.txt").write_text(f"Drive 2 content {i}\n" * 100)

    print("Running initial sync on both drives...")
    extra_env = {"TARGET_DIRECTORY": f"{test_dir1.absolute()},{test_dir2.absolute()}"}

    run_bitrot("--sync", test_dir=None, extra_env=extra_env, disable_notifications=True)

    # Modify some files on drive 1
    print("\nModifying 3 files on Drive 1...")
    time.sleep(2)
    for i in range(3):
        (test_dir1 / f"doc_{i}.txt").write_text(f"MODIFIED Drive 1 content {i}\n" * 120)

    # Remove some files from drive 2
    print("Removing 2 files from Drive 2...")
    for i in range(2):
        (test_dir2 / f"backup_{i}.txt").unlink()

    print("Running sync + scrub on both drives...")
    run_bitrot("--sync --scrub", test_dir=None, extra_env=extra_env)

    print("\nCheck your email:")
    print("  - Subject should show combined totals from both drives")
    print("  - Body should show separate sections for each drive")
    print("  - Drive 1: 3 modified files, X files scrubbed")
    print("  - Drive 2: 2 removed files, Y files scrubbed")
    input("\nPress Enter to finish...")


def main():
    """Run selected test scenarios."""
    print("\n")
    print("=" * 80)
    print("         BIT ROT DETECTOR - END-TO-END EMAIL TESTS")
    print("=" * 80)

    print(
        "\nThis will create test data and send REAL emails to your configured address."
    )
    print("Make sure your .env file has valid SMTP credentials!")

    # Check if .env exists
    if not Path(".env").exists():
        print("\nERROR: .env file not found!")
        print("Please create .env with your SMTP credentials first.")
        return

    # Scenario menu
    scenarios = {
        "1": ("Test Email", scenario_1_test_email),
        "2": ("New Files Detected", scenario_2_new_files),
        "3": ("File Modifications", scenario_3_modifications),
        "4": ("File Moves", scenario_4_file_moves),
        "5": ("File Removals", scenario_5_file_removals),
        "6": ("Successful Scrub", scenario_6_scrub_success),
        "7": ("Bit Rot Detection", scenario_7_bit_rot),
        "8": ("Canary Failure", scenario_8_canary_failure),
        "9": ("Multi-Drive Sync+Scrub", scenario_9_multi_drive),
    }

    print("\nAvailable test scenarios:")
    print("-" * 80)
    for key, (name, _) in scenarios.items():
        print(f"  {key}. {name}")
    print("  A. Run ALL scenarios")
    print("  Q. Quit")
    print("-" * 80)

    selection = input(
        "\nSelect scenarios to run (comma-separated, e.g., 1,2,7 or A for all): "
    ).strip()

    if selection.upper() == "Q":
        print("Cancelled.")
        return

    # Determine which scenarios to run
    to_run = []
    if selection.upper() == "A":
        to_run = list(scenarios.values())
    else:
        selected = [s.strip() for s in selection.split(",")]
        for s in selected:
            if s in scenarios:
                to_run.append(scenarios[s])
            else:
                print(f"Warning: Invalid selection '{s}' ignored")

    if not to_run:
        print("No valid scenarios selected.")
        return

    print(f"\nReady to run {len(to_run)} scenario(s)?")
    response = input("Continue? (yes/no): ")
    if response.lower() != "yes":
        print("Cancelled.")
        return

    try:
        for name, scenario_func in to_run:
            scenario_func()

        print("\n" + "=" * 80)
        print(f"{len(to_run)} test scenario(s) completed!")
        print("=" * 80)
        print("\nCheck your email inbox for notifications.")

        # Cleanup
        print("\nCleaning up test directory...")
        shutil.rmtree("test_email_scenarios", ignore_errors=True)

    except KeyboardInterrupt:
        print("\n\nTest interrupted by user.")
        shutil.rmtree("test_email_scenarios", ignore_errors=True)
    except Exception as e:
        print(f"\n\nError during testing: {e}")
        import traceback

        traceback.print_exc()


if __name__ == "__main__":
    main()
