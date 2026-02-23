// Package monitor detects whether a filesystem mount point is backed by a
// rotational (HDD) or non-rotational (SSD/NVMe) device and recommends an
// appropriate worker-pool concurrency level.
package monitor

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DriveType indicates whether a storage device is rotational or solid-state.
type DriveType int

const (
	DriveTypeUnknown     DriveType = iota
	DriveTypeRotational            // HDD – limit to 1 worker per physical disk
	DriveTypeNonRotational         // SSD/NVMe – scale to all available cores
)

// Info holds the detected device characteristics for a mount point.
type Info struct {
	Type       DriveType
	DeviceName string
	Workers    int
}

// Detect inspects the block-device queue for the filesystem that contains
// mountPoint and returns recommended concurrency information.
//
// On Linux it reads /sys/block/<dev>/queue/rotational.
// On other operating systems it returns DriveTypeUnknown with runtime.NumCPU()
// workers (safe default for unknown media).
func Detect(mountPoint string) Info {
	if runtime.GOOS != "linux" {
		w := runtime.NumCPU()
		slog.Info("drive type detection not supported on this OS; using NumCPU workers",
			"os", runtime.GOOS, "workers", w)
		return Info{Type: DriveTypeUnknown, Workers: w}
	}

	devName, err := deviceForPath(mountPoint)
	if err != nil {
		slog.Warn("could not determine device for path; defaulting to NumCPU workers",
			"path", mountPoint, "err", err)
		return Info{Type: DriveTypeUnknown, Workers: runtime.NumCPU()}
	}

	rotational, err := readRotational(devName)
	if err != nil {
		slog.Warn("could not read rotational flag; defaulting to NumCPU workers",
			"dev", devName, "err", err)
		return Info{Type: DriveTypeUnknown, DeviceName: devName, Workers: runtime.NumCPU()}
	}

	if rotational {
		slog.Info("HDD detected – limiting to 1 worker to prevent head thrashing", "dev", devName)
		return Info{Type: DriveTypeRotational, DeviceName: devName, Workers: 1}
	}

	w := runtime.NumCPU()
	slog.Info("SSD/NVMe detected – scaling to NumCPU workers", "dev", devName, "workers", w)
	return Info{Type: DriveTypeNonRotational, DeviceName: devName, Workers: w}
}

// deviceForPath returns the kernel block-device name (e.g. "sda", "nvme0n1")
// for the filesystem that contains path.  It resolves through symlinks and
// strips partition suffixes so that /sys/block/<dev> is a valid directory.
func deviceForPath(path string) (string, error) {
	// Resolve to an absolute, symlink-free path.
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		resolved = path
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("abs path: %w", err)
	}

	// /proc/mounts lists mount points; we walk up the directory tree and look
	// for the deepest matching entry.
	mounts, err := parseMounts()
	if err != nil {
		return "", err
	}

	bestDev := ""
	bestMount := ""
	for dev, mp := range mounts {
		if strings.HasPrefix(resolved, mp) && len(mp) > len(bestMount) {
			bestDev = dev
			bestMount = mp
		}
	}
	if bestDev == "" {
		return "", fmt.Errorf("no mount point found for %s", resolved)
	}

	// Strip partition number (sda1 -> sda, nvme0n1p1 -> nvme0n1).
	devName := baseDevice(bestDev)
	return devName, nil
}

// parseMounts reads /proc/mounts and returns a map of device -> mountpoint.
func parseMounts() (map[string]string, error) {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return nil, fmt.Errorf("read /proc/mounts: %w", err)
	}

	result := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		dev := fields[0]
		mp := fields[1]
		// Only consider real block devices (start with /dev/).
		if !strings.HasPrefix(dev, "/dev/") {
			continue
		}
		// Keep the actual device name (strip /dev/).
		result[filepath.Base(dev)] = mp
	}
	return result, nil
}

// baseDevice strips trailing partition digits or 'p<n>' suffixes.
//   - sda1   -> sda
//   - nvme0n1p1 -> nvme0n1
//   - sda    -> sda
func baseDevice(dev string) string {
	// NVMe partition suffix: <base>p<digits>
	if idx := strings.LastIndex(dev, "p"); idx > 0 {
		suffix := dev[idx+1:]
		if isAllDigits(suffix) && strings.Contains(dev[:idx], "nvme") {
			if _, err := os.Stat(filepath.Join("/sys/block", dev[:idx])); err == nil {
				return dev[:idx]
			}
		}
	}

	// Generic: strip trailing digits (sda1 -> sda)
	i := len(dev) - 1
	for i >= 0 && dev[i] >= '0' && dev[i] <= '9' {
		i--
	}
	candidate := dev[:i+1]
	if candidate != "" && candidate != dev {
		if _, err := os.Stat(filepath.Join("/sys/block", candidate)); err == nil {
			return candidate
		}
	}
	return dev
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// readRotational reads /sys/block/<dev>/queue/rotational and returns true for HDD.
func readRotational(devName string) (bool, error) {
	sysPath := filepath.Join("/sys/block", devName, "queue", "rotational")
	data, err := os.ReadFile(sysPath)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", sysPath, err)
	}
	val := strings.TrimSpace(string(data))
	return val == "1", nil
}
