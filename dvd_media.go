package main

import (
	stdcontext "context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
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
	// dvdMediaDriveCannotWrite is for a drive that only reads discs, such as
	// a DVD-ROM drive: no disc in it can be written.
	dvdMediaDriveCannotWrite = "書き込み非対応ドライブ"
)

// listDVDDrives returns the optical drives connected now and the state of
// the disc in each. A drive whose state cannot be read is listed with
// dvdMediaUnknown rather than left out.
func listDVDDrives() []dvdDrive {
	switch runtime.GOOS {
	case "darwin":
		return listDVDDrivesDarwin()
	case "linux":
		return listDVDDrivesLinux()
	case "windows":
		return listDVDDrivesWindows()
	default:
		return nil
	}
}

// listDVDDrivesDarwin asks drutil about one drive after another. drutil
// lists the drives that can burn some kind of disc; one that cannot burn
// DVDs is reported as such.
func listDVDDrivesDarwin() []dvdDrive {
	var drives []dvdDrive
	for index := 1; index <= 16; index++ {
		number := strconv.Itoa(index)
		status, err := runDrutilQuery(number, "status")
		if err != nil && status == "" {
			break
		}
		if strings.Contains(status, "Could not find a valid device") {
			break
		}
		drive := dvdDrive{ID: number, Name: drutilDriveName(status), State: parseDrutilDVDMediaStatus(status)}
		if info, _ := runDrutilQuery(number, "info"); drutilReportsDVDWriteSupport(info) && !discBurningReportHasDVDWriter(info) {
			drive.State = dvdMediaDriveCannotWrite
		}
		if drive.Name == "" {
			drive.Name = "ドライブ " + number
		}
		drives = append(drives, drive)
	}
	return drives
}

func runDrutilQuery(drive, verb string) (string, error) {
	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), 8*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "drutil", "-drive", drive, verb).CombinedOutput()
	return string(output), err
}

// drutilReportsDVDWriteSupport tells whether a "drutil info" report has the
// DVD-Write line at all, that is whether it says anything about DVD writing.
func drutilReportsDVDWriteSupport(info string) bool {
	for line := range strings.SplitSeq(info, "\n") {
		key, _, found := strings.Cut(line, ":")
		if found && strings.EqualFold(strings.TrimSpace(key), "DVD-Write") {
			return true
		}
	}
	return false
}

// drutilDriveName reads the vendor and product of the drive from the table
// at the top of drutil's output:
//
//	Vendor   Product           Rev
//	TSSTcorp DVD+-RW SN-208FB  D100
func drutilDriveName(output string) string {
	lines := strings.Split(output, "\n")
	for index, line := range lines {
		if index+1 >= len(lines) || !strings.Contains(line, "Vendor") || !strings.Contains(line, "Product") {
			continue
		}
		values := strings.TrimRight(lines[index+1], " \r")
		if revision := strings.Index(line, "Rev"); revision > 0 && revision < len(values) {
			values = values[:revision]
		}
		return strings.Join(strings.Fields(values), " ")
	}
	return ""
}

func parseDrutilDVDMediaStatus(output string) string {
	inserted := false
	knownWritableState := false
	unknownWritableState := false
	for block := range strings.SplitSeq(output, "\n\n") {
		mediaType := ""
		writability := ""
		for line := range strings.SplitSeq(block, "\n") {
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

func listDVDDrivesLinux() []dvdDrive {
	devices := linuxOpticalDevices()
	drives := make([]dvdDrive, 0, len(devices))
	for _, device := range devices {
		ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), 3*time.Second)
		output, err := exec.CommandContext(ctx, "udevadm", "info", "--query=property", "--name", device).Output()
		cancel()
		if err != nil {
			drives = append(drives, dvdDrive{ID: device, Name: device, State: dvdMediaUnknown})
			continue
		}
		drives = append(drives, linuxDVDDriveFromUdev(device, parseUdevProperties(string(output))))
	}
	return drives
}

// linuxDVDDriveFromUdev reads the state of the drive at device from its udev
// properties.
func linuxDVDDriveFromUdev(device string, properties map[string]string) dvdDrive {
	drive := dvdDrive{ID: device, Name: device}
	if model := strings.Join(strings.Fields(strings.ReplaceAll(properties["ID_VENDOR"]+" "+properties["ID_MODEL"], "_", " ")), " "); model != "" {
		drive.Name = device + " " + model
	}
	if properties["ID_CDROM"] != "1" {
		drive.State = dvdMediaUnknown
		return drive
	}
	// A drive that reads DVDs but has no DVD write feature cannot burn, and
	// it does not even see a blank disc: udev would report no media.
	if properties["ID_CDROM_DVD"] == "1" && !udevDriveWritesDVD(properties) {
		drive.State = dvdMediaDriveCannotWrite
		return drive
	}
	if properties["ID_CDROM_MEDIA"] != "1" {
		drive.State = dvdMediaNoDisc
		return drive
	}
	switch writable, known := udevDVDMediaWritable(properties); {
	case writable:
		drive.State = dvdMediaWritable
	case known:
		drive.State = dvdMediaReadOnly
	default:
		drive.State = dvdMediaInsertedUnknown
	}
	return drive
}

func udevDriveWritesDVD(properties map[string]string) bool {
	for _, feature := range udevDVDWriteFeatures {
		if udevDriveSupportsDVDFeature(properties, feature) {
			return true
		}
	}
	return false
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
	for line := range strings.SplitSeq(output, "\n") {
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

// windowsDrivesScript lists the drives through IMAPI, one line per drive:
// "DRIVE", the recorder ID, the name and the state, separated by tabs. A
// drive that only reads, such as a DVD-ROM drive, is refused as the recorder
// of a data format; it does not see a blank disc either.
const windowsDrivesScript = `$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Console]::OutputEncoding = [Text.Encoding]::UTF8
function Report($id, $name, $state) { "DRIVE` + "`" + `t$id` + "`" + `t$name` + "`" + `t$state" }
try {
  $master = New-Object -ComObject IMAPI2.MsftDiscMaster2
  $ids = @($master)
} catch {
  # Without IMAPI, report the drives Windows knows of, without their state.
  try {
    foreach ($drive in @(Get-CimInstance Win32_CDROMDrive)) { Report '' ("$($drive.Drive) $($drive.Caption)".Trim()) 'unknown' }
  } catch { }
  return
}
foreach ($id in $ids) {
  $name = ''
  try {
    $recorder = New-Object -ComObject IMAPI2.MsftDiscRecorder2
    $recorder.InitializeDiscRecorder($id)
    $parts = @()
    $letter = @($recorder.VolumePathNames) | Select-Object -First 1
    if ($letter) { $parts += $letter.TrimEnd('\') }
    $parts += $recorder.VendorId.Trim()
    $parts += $recorder.ProductId.Trim()
    $name = (@($parts | Where-Object { $_ }) -join ' ')
    $format = New-Object -ComObject IMAPI2.MsftDiscFormat2Data
    if (-not [bool]$format.IsRecorderSupported($recorder)) { Report $id $name 'drive-read-only'; continue }
    $format.Recorder = $recorder
    $type = 0
    try {
      $type = [int]$format.CurrentPhysicalMediaType
    } catch {
      # E_IMAPI_RECORDER_MEDIA_NO_MEDIA
      if ($_.Exception.HResult -ne -1062600190) { throw }
    }
    if ($type -eq 0) { Report $id $name 'empty'; continue }
    $isDVD = (($type -ge 4 -and $type -le 11) -or $type -eq 13)
    if (-not $isDVD -or $type -eq 4) { Report $id $name 'not-writable'; continue }
    $status = [int]$format.CurrentMediaStatus
    $supported = [bool]$format.IsCurrentMediaSupported($recorder)
    if ($supported -and (($status -band 0x000F) -ne 0) -and (($status -band 0xFC00) -eq 0)) {
      Report $id $name 'writable'
    } else {
      Report $id $name 'not-writable'
    }
  } catch {
    Report $id $name 'unknown'
  }
}`

func listDVDDrivesWindows() []dvdDrive {
	// Starting PowerShell and the disc recording service can take several
	// seconds, more so while a drive spins up a disc.
	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", windowsDrivesScript)
	hideConsoleWindow(cmd)
	output, err := cmd.Output()
	if err != nil && len(output) == 0 {
		return nil
	}
	return parseWindowsDrives(string(output))
}

// parseWindowsDrives reads the output of windowsDrivesScript.
func parseWindowsDrives(output string) []dvdDrive {
	var drives []dvdDrive
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(fields) != 4 || fields[0] != "DRIVE" {
			continue
		}
		drive := dvdDrive{ID: fields[1], Name: strings.TrimSpace(fields[2])}
		switch fields[3] {
		case "writable":
			drive.State = dvdMediaWritable
		case "not-writable":
			drive.State = dvdMediaReadOnly
		case "empty":
			drive.State = dvdMediaNoDisc
		case "drive-read-only":
			drive.State = dvdMediaDriveCannotWrite
		default:
			drive.State = dvdMediaUnknown
		}
		if drive.Name == "" {
			drive.Name = "ドライブ " + strconv.Itoa(len(drives)+1)
		}
		drives = append(drives, drive)
	}
	return drives
}
