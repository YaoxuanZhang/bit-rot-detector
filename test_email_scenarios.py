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


def run_bitrot(args: str, test_dir: Path):
    """Run bit-rot-detector with given args."""
    env = os.environ.copy()
    env["TARGET_DIRECTORY"] = str(test_dir.absolute())
    
    result = subprocess.run(
        f"uv run bit-rot-detector {args}",
        shell=True,
        cwd=Path.cwd(),
        env=env,
        capture_output=True,
        text=True
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
    
    print("\n📧 Sending test email...")
    run_bitrot("--test-email", test_dir)
    
    print("\n✅ Check your email for: 'Bit Rot Detector - Test Email'")
    input("\nPress Enter to continue to next scenario...")


def scenario_2_new_files():
    """Scenario 2: New files detected."""
    print("\n" + "=" * 80)
    print("SCENARIO 2: New Files Detected")
    print("=" * 80)
    
    test_dir = setup_test_directory()
    
    # Create some files
    print("\n📁 Creating 10 new files...")
    for i in range(10):
        (test_dir / f"file_{i:03d}.txt").write_text(f"Content of file {i}\n" * 100)
    
    print("🔍 Running sync operation...")
    run_bitrot("--sync", test_dir)
    
    print("\n✅ Check your email for: 'Bit Rot Detector - 10 New Files Detected'")
    input("\nPress Enter to continue to next scenario...")


def scenario_3_modifications():
    """Scenario 3: File modifications detected."""
    print("\n" + "=" * 80)
    print("SCENARIO 3: File Modifications Detected")
    print("=" * 80)
    
    test_dir = setup_test_directory()
    
    # Create initial files
    print("\n📁 Creating initial files...")
    for i in range(5):
        (test_dir / f"file_{i}.txt").write_text(f"Original content {i}\n" * 50)
    
    print("🔍 Running initial sync...")
    run_bitrot("--sync", test_dir)
    
    # Wait a bit to ensure mtime changes
    time.sleep(2)
    
    # Modify some files
    print("\n✏️  Modifying 3 files...")
    for i in range(3):
        (test_dir / f"file_{i}.txt").write_text(f"MODIFIED content {i}\n" * 75)
    
    print("🔍 Running sync to detect modifications...")
    run_bitrot("--sync", test_dir)
    
    print("\n✅ Check your email for: 'Bit Rot Detector - 0 New Files Detected'")
    print("   (Should show 3 files modified)")
    input("\nPress Enter to continue to next scenario...")


def scenario_3b_file_moves():
    """Scenario 3b: File moves detected."""
    print("\n" + "=" * 80)
    print("SCENARIO 3B: File Moves Detected")
    print("=" * 80)
    
    test_dir = setup_test_directory()
    
    # Create initial files
    print("\n📁 Creating initial files...")
    for i in range(5):
        (test_dir / f"file_{i}.txt").write_text(f"Content {i}\n" * 50)
    
    print("🔍 Running initial sync to add files to database...")
    run_bitrot("--sync", test_dir)
    
    # Create subdirectory and move files
    print("\n📦 Moving 3 files to subdirectory...")
    subdir = test_dir / "moved_files"
    subdir.mkdir()
    
    for i in range(3):
        (test_dir / f"file_{i}.txt").rename(subdir / f"file_{i}.txt")
    
    print("🔍 Running sync to detect moves...")
    run_bitrot("--sync", test_dir)
    
    print("\n✅ Check your email - should show 3 files moved")
    input("\nPress Enter to continue to next scenario...")


def scenario_3c_file_removals():
    """Scenario 3c: File removals detected."""
    print("\n" + "=" * 80)
    print("SCENARIO 3C: File Removals Detected")
    print("=" * 80)
    
    test_dir = setup_test_directory()
    
    # Create initial files
    print("\n📁 Creating initial files...")
    for i in range(8):
        (test_dir / f"file_{i}.txt").write_text(f"Content {i}\n" * 50)
    
    print("🔍 Running initial sync...")
    run_bitrot("--sync", test_dir)
    
    # Remove some files
    print("\n🗑️  Removing 4 files...")
    for i in range(4):
        (test_dir / f"file_{i}.txt").unlink()
    
    print("🔍 Running sync to detect removals...")
    run_bitrot("--sync", test_dir)
    
    print("\n✅ Check your email - should show 4 files removed")
    input("\nPress Enter to continue to next scenario...")


def scenario_4_scrub_success():
    """Scenario 4: Successful scrub operation."""
    print("\n" + "=" * 80)
    print("SCENARIO 4: Successful Scrub Operation")
    print("=" * 80)
    
    test_dir = setup_test_directory()
    
    # Create files
    print("\n📁 Creating files...")
    for i in range(8):
        (test_dir / f"file_{i}.txt").write_text(f"Content {i}\n" * 100)
    
    print("🔍 Running initial sync...")
    run_bitrot("--sync", test_dir)
    
    print("🔍 Running scrub operation...")
    run_bitrot("--scrub", test_dir)
    
    print("\n✅ Check your email for: 'Bit Rot Detector - 8 Files Validated'")
    print("   (Should show 0 corrupted files)")
    input("\nPress Enter to continue to next scenario...")


def scenario_5_bit_rot():
    """Scenario 5: Bit rot detection."""
    print("\n" + "=" * 80)
    print("SCENARIO 5: BIT ROT DETECTION (Simulated)")
    print("=" * 80)
    
    test_dir = setup_test_directory()
    
    # Create files
    print("\n📁 Creating files...")
    for i in range(5):
        (test_dir / f"file_{i}.txt").write_text(f"Original content {i}\n" * 100)
    
    print("🔍 Running initial sync to hash files...")
    run_bitrot("--sync", test_dir)
    
    # Corrupt files by modifying WITHOUT updating mtime
    print("\n💥 Simulating bit rot (corrupting files without changing mtime)...")
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
    
    print("🔍 Running scrub to detect corruption...")
    run_bitrot("--scrub", test_dir)
    
    print("\n✅ Check your email for: '⚠️ BIT ROT DETECTED - 2 Corrupted Files!'")
    print("   (Should list the 2 corrupted files with full paths)")
    input("\nPress Enter to continue to next scenario...")


def scenario_6_canary_failure():
    """Scenario 6: Canary check failure."""
    print("\n" + "=" * 80)
    print("SCENARIO 6: Canary Check Failure")
    print("=" * 80)
    
    test_dir = setup_test_directory()
    
    # Create files
    print("\n📁 Creating files...")
    for i in range(3):
        (test_dir / f"file_{i}.txt").write_text(f"Content {i}\n" * 50)
    
    print("🔍 Running initial sync...")
    run_bitrot("--sync", test_dir)
    
    # Remove canary
    print("\n💥 Removing canary file to simulate unmounted drive...")
    (test_dir / ".bitrot-canary").unlink()
    
    print("🔍 Running sync (should fail canary check)...")
    run_bitrot("--sync", test_dir)
    
    print("\n✅ Check your email for: '⚠️ Bit Rot Detector - Critical Error'")
    print("   (Should mention canary check failed)")
    input("\nPress Enter to finish...")


def main():
    """Run all test scenarios."""
    print("\n")
    print("╔" + "═" * 78 + "╗")
    print("║" + " " * 15 + "BIT ROT DETECTOR - END-TO-END EMAIL TESTS" + " " * 22 + "║")
    print("╚" + "═" * 78 + "╝")
    
    print("\nThis will create test data and send REAL emails to your configured address.")
    print("Make sure your .env file has valid SMTP credentials!")
    
    # Check if .env exists
    if not Path(".env").exists():
        print("\n❌ ERROR: .env file not found!")
        print("Please create .env with your SMTP credentials first.")
        return
    
    response = input("\nReady to send test emails? (yes/no): ")
    if response.lower() != "yes":
        print("Cancelled.")
        return
    
    try:
        scenario_1_test_email()
        scenario_2_new_files()
        scenario_3_modifications()
        scenario_3b_file_moves()
        scenario_3c_file_removals()
        scenario_4_scrub_success()
        scenario_5_bit_rot()
        scenario_6_canary_failure()
        
        print("\n" + "=" * 80)
        print("✅ All test scenarios completed!")
        print("=" * 80)
        print("\nCheck your email inbox for all 8 notification types.")
        
        # Cleanup
        print("\n🧹 Cleaning up test directory...")
        shutil.rmtree("test_email_scenarios", ignore_errors=True)
        
    except KeyboardInterrupt:
        print("\n\n⚠️  Test interrupted by user.")
        shutil.rmtree("test_email_scenarios", ignore_errors=True)
    except Exception as e:
        print(f"\n\n❌ Error during testing: {e}")
        import traceback
        traceback.print_exc()


if __name__ == "__main__":
    main()
