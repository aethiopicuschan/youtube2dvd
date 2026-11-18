package main

import (
	stdcontext "context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	dvdMediaChecking        = "確認中…"
	dvdMediaDriveAbsent     = "ドライブ未検出"
	dvdMediaNoDisc          = "ディスク未挿入"
	dvdMediaWritable        = "書き込み可能"
	dvdMediaReadOnly        = "書き込み不可"
	dvdMediaInsertedUnknown = "挿入済み・判定不能"
	dvdMediaUnknown         = "判定不能"
)

func checkDVDMediaStatus() string {
	switch runtime.GOOS {
	case "darwin":
		return checkDVDMediaDarwin()
	case "linux":
		return checkDVDMediaLinux()
	case "windows":
		return checkDVDMediaWindows()
	default:
		return dvdMediaUnknown
	}
}

func checkDVDMediaDarwin() string {
	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), 8*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "drutil", "status").CombinedOutput()
	if err != nil && len(output) == 0 {
		return dvdMediaUnknown
	}
	return parseDrutilDVDMediaStatus(string(output))
}

func parseDrutilDVDMediaStatus(output string) string {
	inserted := false
	knownWritableState := false
	unknownWritableState := false
	for _, block := range strings.Split(output, "\n\n") {
		mediaType := ""
		writability := ""
		for _, line := range strings.Split(block, "\n") {
			key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
			if !ok {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "type":
				mediaType = strings.TrimSpace(strings.SplitN(value, "Name:", 2)[0])
			case "writability":
				writability = strings.ToLower(value)
			}
		}
		if mediaType == "" || strings.Contains(strings.ToLower(mediaType), "no media inserted") {
			continue
		}
		inserted = true
		if !strings.Contains(strings.ToUpper(mediaType), "DVD") {
			knownWritableState = true
			continue
		}
		if writability != "" {
			knownWritableState = true
			if strings.Contains(writability, "not writable") || strings.Contains(writability, "unwritable") {
				continue
			}
			for _, token := range strings.FieldsFunc(writability, func(r rune) bool {
				return r == ',' || r == ' ' || r == '\t'
			}) {
				if token == "blank" || token == "appendable" || token == "overwritable" || token == "writable" {
					return dvdMediaWritable
				}
			}
			continue
		}
		if strings.Contains(strings.ToUpper(mediaType), "ROM") {
			knownWritableState = true
		} else {
			unknownWritableState = true
		}
	}
	if !inserted {
		if strings.Contains(strings.ToLower(output), "no media inserted") {
			return dvdMediaNoDisc
		}
		return dvdMediaUnknown
	}
	if unknownWritableState {
		return dvdMediaInsertedUnknown
	}
	if knownWritableState {
		return dvdMediaReadOnly
	}
	return dvdMediaUnknown
}

func checkDVDMediaLinux() string {
	devices := linuxOpticalDevices()
	if len(devices) == 0 {
		return dvdMediaUnknown
	}
	foundDisc := false
	foundKnownDisc := false
	foundUnknownDisc := false
	queryFailed := false
	queriedDrives := 0
	for _, device := range devices {
		ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), 3*time.Second)
		output, err := exec.CommandContext(ctx, "udevadm", "info", "--query=property", "--name", device).Output()
		cancel()
		if err != nil {
			queryFailed = true
			continue
		}
		properties := parseUdevProperties(string(output))
		if properties["ID_CDROM"] != "1" {
			queryFailed = true
			continue
		}
		queriedDrives++
		if properties["ID_CDROM_MEDIA"] != "1" {
			continue
		}
		foundDisc = true
		writable, known := udevDVDMediaWritable(properties)
		if writable {
			return dvdMediaWritable
		}
		if known {
			foundKnownDisc = true
		} else {
			foundUnknownDisc = true
		}
	}
	if foundDisc && foundUnknownDisc {
		return dvdMediaInsertedUnknown
	}
	if foundKnownDisc && !queryFailed && queriedDrives == len(devices) {
		return dvdMediaReadOnly
	}
	if foundDisc {
		return dvdMediaInsertedUnknown
	}
	if !queryFailed && queriedDrives == len(devices) {
		return dvdMediaNoDisc
	}
	return dvdMediaUnknown
}

func linuxOpticalDevices() []string {
	entries, err := os.ReadDir("/sys/class/block")
	if err == nil {
		devices := make([]string, 0, len(entries))
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), "sr") {
				continue
			}
			device := filepath.Join("/dev", entry.Name())
			if _, err := os.Stat(device); err == nil {
				devices = append(devices, device)
			}
		}
		if len(devices) > 0 {
			return devices
		}
	}
	devices := make([]string, 0, 6)
	for _, device := range []string{"/dev/cdrom", "/dev/dvd", "/dev/sr0", "/dev/sr1", "/dev/sr2", "/dev/sr3"} {
		if _, err := os.Stat(device); err == nil {
			devices = append(devices, device)
		}
	}
	return devices
}

func parseUdevProperties(output string) map[string]string {
	properties := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			properties[key] = value
		}
	}
	return properties
}

var udevDVDWriteFeatures = []string{
	"DVD_R",
	"DVD_R_DL",
	"DVD_RW",
	"DVD_RW_DL",
	"DVD_RAM",
	"DVD_PLUS_R",
	"DVD_PLUS_R_DL",
	"DVD_PLUS_RW",
	"DVD_PLUS_RW_DL",
}

func udevDVDMediaWritable(properties map[string]string) (writable, known bool) {
	mediaDVD := false
	for key, value := range properties {
		if value == "1" && strings.HasPrefix(key, "ID_CDROM_MEDIA_DVD") {
			mediaDVD = true
			break
		}
	}
	if !mediaDVD {
		return false, true
	}

	mediaFeatures := make(map[string]bool)
	for _, feature := range udevDVDWriteFeatures {
		if properties["ID_CDROM_MEDIA_"+feature] == "1" {
			mediaFeatures[feature] = true
		}
	}
	if len(mediaFeatures) == 0 {
		return false, true
	}
	compatible := false
	for feature := range mediaFeatures {
		if udevDriveSupportsDVDFeature(properties, feature) {
			compatible = true
			break
		}
	}
	if !compatible {
		return false, true
	}

	switch strings.ToLower(properties["ID_CDROM_MEDIA_STATE"]) {
	case "blank", "appendable":
		return true, true
	case "complete":
		for feature := range mediaFeatures {
			if feature == "DVD_RW" || feature == "DVD_RW_DL" || feature == "DVD_RAM" || feature == "DVD_PLUS_RW" || feature == "DVD_PLUS_RW_DL" {
				// Udev reports completed rewritable media as complete. DVD-RAM may also
				// report complete when write-protected, so leave that case undecided.
				if feature == "DVD_RAM" {
					return false, false
				}
				return true, true
			}
		}
		return false, true
	case "other":
		return false, false
	default:
		return false, false
	}
}

func udevDriveSupportsDVDFeature(properties map[string]string, feature string) bool {
	if properties["ID_CDROM_"+feature] == "1" {
		return true
	}
	return feature == "DVD_R" && properties["ID_CDROM_DVD_R_DDR"] == "1"
}

func checkDVDMediaWindows() string {
	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), 8*time.Second)
	defer cancel()
	const script = `$ErrorActionPreference = 'Stop'
$cdromDrives = @()
$driveCount = 0
$hasMedia = $false
$wmiMediaStateKnown = $false
$hasWritable = $false
$mediaStateKnown = $false
$recorderCount = 0
try {
  $cdromDrives = @(Get-CimInstance Win32_CDROMDrive)
  $driveCount = $cdromDrives.Count
  foreach ($drive in $cdromDrives) {
    if ($null -ne $drive.MediaLoaded) {
      $wmiMediaStateKnown = $true
      if ([bool]$drive.MediaLoaded) { $hasMedia = $true }
    }
  }
  $master = New-Object -ComObject IMAPI2.MsftDiscMaster2
  foreach ($id in $master) {
    try {
      $recorder = New-Object -ComObject IMAPI2.MsftDiscRecorder2
      $recorder.InitializeDiscRecorder($id)
      $format = New-Object -ComObject IMAPI2.MsftDiscFormat2Data
      $format.Recorder = $recorder
      $recorderCount++
      $mediaType = [int]$format.CurrentPhysicalMediaType
      $mediaStatus = [int]$format.CurrentMediaStatus
      if ($mediaType -ne 0) {
        $hasMedia = $true
        $mediaStateKnown = $true
        $isDVD = (($mediaType -ge 4 -and $mediaType -le 11) -or $mediaType -eq 13)
        if ($isDVD -and $mediaType -ne 4) {
          $supported = [bool]$format.IsCurrentMediaSupported($recorder)
          $hasWritableState = (($mediaStatus -band 0x000F) -ne 0)
          $hasBlockingState = (($mediaStatus -band 0xFC00) -ne 0)
          if ($supported -and $hasWritableState -and -not $hasBlockingState) { $hasWritable = $true }
        }
      }
    } catch { }
  }
} catch { }
if ($hasWritable) { 'writable' }
elseif ($hasMedia -and ($mediaStateKnown -or $recorderCount -eq 0)) { 'not-writable' }
elseif ($hasMedia) { 'inserted-unknown' }
elseif ($driveCount -gt 0 -and -not $hasMedia -and ($mediaStateKnown -or $wmiMediaStateKnown)) { 'empty' }
else { 'unknown' }`
	output, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return dvdMediaUnknown
	}
	switch strings.TrimSpace(string(output)) {
	case "writable":
		return dvdMediaWritable
	case "not-writable":
		return dvdMediaReadOnly
	case "inserted-unknown":
		return dvdMediaInsertedUnknown
	case "empty":
		return dvdMediaNoDisc
	default:
		return dvdMediaUnknown
	}
}
