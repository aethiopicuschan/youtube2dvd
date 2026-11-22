package main

import (
	"fmt"
	"strings"
)

// dvdDrive is one optical drive and the disc in it.
type dvdDrive struct {
	// ID addresses the drive when burning: the drutil drive number on
	// macOS, the device path on Linux and the IMAPI recorder ID on Windows.
	ID string
	// Name is shown to the user, such as "E: TSSTcorp DVD+-RW SN-208FB".
	Name string
	// State is one of the dvdMedia* states for this drive alone.
	State string
}

// dvdDriveStatePriority orders the states of drives by how close they are
// to burning a DVD. The home screen reports the drive that is closest, so
// that an empty writer is not hidden behind a pressed DVD in another drive.
var dvdDriveStatePriority = []string{
	dvdMediaWritable,
	dvdMediaNoDisc,
	dvdMediaInsertedUnknown,
	dvdMediaUnknown,
	dvdMediaReadOnly,
	dvdMediaDriveCannotWrite,
}

func dvdDriveStateRank(state string) int {
	for rank, candidate := range dvdDriveStatePriority {
		if state == candidate {
			return rank
		}
	}
	return len(dvdDriveStatePriority)
}

// summarizeDVDDrives returns what the home screen shows for the drives: the
// drive status and the disc status of the drive closest to burning.
func summarizeDVDDrives(drives []dvdDrive) (driveState, discState string) {
	if len(drives) == 0 {
		return "未検出", dvdMediaDriveAbsent
	}
	best := drives[0].State
	for _, drive := range drives[1:] {
		if dvdDriveStateRank(drive.State) < dvdDriveStateRank(best) {
			best = drive.State
		}
	}
	return "検出済み", best
}

// writableDVDDrives returns the drives that hold a disc that can be burned.
func writableDVDDrives(drives []dvdDrive) []dvdDrive {
	var writable []dvdDrive
	for _, drive := range drives {
		if drive.State == dvdMediaWritable {
			writable = append(writable, drive)
		}
	}
	return writable
}

// dvdDriveNames lists the names of the drives in a given state, for hints.
func dvdDriveNames(drives []dvdDrive, state string) string {
	var names []string
	for _, drive := range drives {
		if drive.State == state && drive.Name != "" {
			names = append(names, drive.Name)
		}
	}
	return strings.Join(names, "、")
}

// dvdDrivesHint explains what keeps the drives from burning a DVD, naming
// the drives it is about where that helps.
func dvdDrivesHint(drives []dvdDrive) string {
	_, disc := summarizeDVDDrives(drives)
	names := dvdDriveNames(drives, disc)
	switch disc {
	case dvdMediaNoDisc:
		if names != "" {
			return fmt.Sprintf("書き込み対応のドライブ（%s）にディスクが入っていないため、DVDへの書き込みはできません。書き込み可能なDVDを入れてください。", names)
		}
	case dvdMediaReadOnly:
		if names != "" {
			return fmt.Sprintf("ドライブ（%s）のディスクは書き込みできないため、書き込み可能なDVDを入れてください。", names)
		}
	}
	return dvdDiscEnvironmentHint(disc)
}

// dvdDriveStatusLabel is the drive status shown on the home screen, with the
// number of drives when there is more than one.
func dvdDriveStatusLabel(driveState string, drives []dvdDrive) string {
	if driveState == "検出済み" && len(drives) > 1 {
		return fmt.Sprintf("%s（%d台）", driveState, len(drives))
	}
	return driveState
}

// findDVDDrive finds want among the drives connected now. The ID and the
// name must both match, so that a drutil drive number that now belongs to
// another drive is not taken for the one the user chose.
func findDVDDrive(drives []dvdDrive, want dvdDrive) (dvdDrive, bool) {
	for _, drive := range drives {
		if drive.ID == want.ID && drive.Name == want.Name {
			return drive, true
		}
	}
	return dvdDrive{}, false
}
