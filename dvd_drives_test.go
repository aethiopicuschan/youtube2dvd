package main

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

func TestSummarizeDVDDrives(t *testing.T) {
	writer := func(state string) dvdDrive { return dvdDrive{ID: "1", Name: "Writer", State: state} }
	reader := dvdDrive{ID: "2", Name: "Reader", State: dvdMediaDriveCannotWrite}
	tests := []struct {
		name      string
		drives    []dvdDrive
		wantDrive string
		wantDisc  string
	}{
		{name: "no drive", drives: nil, wantDrive: "未検出", wantDisc: dvdMediaDriveAbsent},
		{name: "read-only drive alone", drives: []dvdDrive{reader}, wantDrive: "検出済み", wantDisc: dvdMediaDriveCannotWrite},
		{name: "writer with blank beside reader", drives: []dvdDrive{reader, writer(dvdMediaWritable)}, wantDrive: "検出済み", wantDisc: dvdMediaWritable},
		{name: "empty writer beside reader", drives: []dvdDrive{reader, writer(dvdMediaNoDisc)}, wantDrive: "検出済み", wantDisc: dvdMediaNoDisc},
		{
			name:      "empty writer beside a writer holding a pressed DVD",
			drives:    []dvdDrive{{ID: "1", Name: "A", State: dvdMediaReadOnly}, {ID: "2", Name: "B", State: dvdMediaNoDisc}},
			wantDrive: "検出済み",
			wantDisc:  dvdMediaNoDisc,
		},
		{name: "unknown beats a pressed DVD", drives: []dvdDrive{writer(dvdMediaReadOnly), {ID: "3", State: dvdMediaUnknown}}, wantDrive: "検出済み", wantDisc: dvdMediaUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			drive, disc := summarizeDVDDrives(test.drives)
			if drive != test.wantDrive || disc != test.wantDisc {
				t.Errorf("summarizeDVDDrives() = %q, %q; want %q, %q", drive, disc, test.wantDrive, test.wantDisc)
			}
		})
	}
}

func TestDVDDrivesHintNamesTheDrive(t *testing.T) {
	drives := []dvdDrive{
		{ID: "1", Name: "F: HL-DT-ST DVD-ROM DU90N", State: dvdMediaDriveCannotWrite},
		{ID: "2", Name: "E: TSSTcorp DVD+-RW SN-208FB", State: dvdMediaNoDisc},
	}
	if hint := dvdDrivesHint(drives); !strings.Contains(hint, "E: TSSTcorp DVD+-RW SN-208FB") || strings.Contains(hint, "DU90N") {
		t.Errorf("hint = %q, want it to name only the empty writer", hint)
	}
	if hint := dvdDrivesHint(drives[:1]); hint != dvdDiscEnvironmentHint(dvdMediaDriveCannotWrite) {
		t.Errorf("hint for a read-only drive = %q", hint)
	}
}

func TestDVDDriveStatusLabel(t *testing.T) {
	two := []dvdDrive{{ID: "1"}, {ID: "2"}}
	if got := dvdDriveStatusLabel("検出済み", two); got != "検出済み（2台）" {
		t.Errorf("label = %q", got)
	}
	if got := dvdDriveStatusLabel("検出済み", two[:1]); got != "検出済み" {
		t.Errorf("label for one drive = %q", got)
	}
}

func TestFindDVDDriveNeedsIDAndName(t *testing.T) {
	drives := []dvdDrive{{ID: "1", Name: "Other drive", State: dvdMediaWritable}}
	if _, found := findDVDDrive(drives, dvdDrive{ID: "1", Name: "Chosen drive"}); found {
		t.Error("found a drive whose number now belongs to another drive")
	}
	if _, found := findDVDDrive(drives, dvdDrive{ID: "1", Name: "Other drive"}); !found {
		t.Error("did not find the drive")
	}
}

func TestDVDWriteDialogBurnTarget(t *testing.T) {
	first := dvdDrive{ID: "a", Name: "First", State: dvdMediaWritable}
	second := dvdDrive{ID: "b", Name: "Second", State: dvdMediaWritable}
	dialog := &dvdWriteDialog{drives: []dvdDrive{{ID: "r", Name: "Reader", State: dvdMediaDriveCannotWrite}, first, second}}

	if target, ok := dialog.burnTarget(); !ok || target != first {
		t.Fatalf("default target = %+v, %t; want the first writable drive", target, ok)
	}
	dialog.selectedDrive = second
	if target, _ := dialog.burnTarget(); target != second {
		t.Errorf("target = %+v, want the chosen drive", target)
	}
	// The chosen disc is taken out: fall back to the drive that can burn.
	dialog.drives[2].State = dvdMediaNoDisc
	if target, _ := dialog.burnTarget(); target != first {
		t.Errorf("target = %+v, want the remaining writable drive", target)
	}
	dialog.drives[1].State = dvdMediaNoDisc
	if _, ok := dialog.burnTarget(); ok {
		t.Error("found a target without any writable drive")
	}
}

func TestDrutilDriveName(t *testing.T) {
	const status = " Vendor   Product           Rev \n TSSTcorp DVD+-RW SN-208FB  D100\n\n           Type: DVD-R                Name: /dev/disk6\n"
	if got := drutilDriveName(status); got != "TSSTcorp DVD+-RW SN-208FB" {
		t.Errorf("name = %q", got)
	}
	if got := drutilDriveName("\n Could not find a valid device.\n"); got != "" {
		t.Errorf("name without a drive = %q", got)
	}
}

func TestParseWindowsDrives(t *testing.T) {
	// As the script printed it for the two drives of a test machine.
	const output = "DRIVE\t\\\\?\\scsi#cdrom&ven_hl-dt-st&prod_dvd-rom_du90n#4&24d69727&0&000100#{53f56308-b6bf-11d0-94f2-00a0c91efb8b}\tF: HL-DT-ST DVD-ROM DU90N\tdrive-read-only\r\n" +
		"DRIVE\t\\\\?\\usbstor#cdrom&ven_tsstcorp&prod_dvd+-rw_sn-208fb&rev_d100#5331334c3659414733303138&0#{53f56308-b6bf-11d0-94f2-00a0c91efb8b}\tE: TSSTcorp DVD+-RW SN-208FB\twritable\r\n" +
		"WARNING: unrelated\r\n" +
		"DRIVE\t\t\tunknown\r\n"
	drives := parseWindowsDrives(output)
	want := []dvdDrive{
		{ID: `\\?\scsi#cdrom&ven_hl-dt-st&prod_dvd-rom_du90n#4&24d69727&0&000100#{53f56308-b6bf-11d0-94f2-00a0c91efb8b}`, Name: "F: HL-DT-ST DVD-ROM DU90N", State: dvdMediaDriveCannotWrite},
		{ID: `\\?\usbstor#cdrom&ven_tsstcorp&prod_dvd+-rw_sn-208fb&rev_d100#5331334c3659414733303138&0#{53f56308-b6bf-11d0-94f2-00a0c91efb8b}`, Name: "E: TSSTcorp DVD+-RW SN-208FB", State: dvdMediaWritable},
		{ID: "", Name: "ドライブ 3", State: dvdMediaUnknown},
	}
	if !slices.Equal(drives, want) {
		t.Errorf("drives = %+v\nwant %+v", drives, want)
	}
}

func TestLinuxDVDDriveFromUdev(t *testing.T) {
	base := func(extra map[string]string) map[string]string {
		properties := map[string]string{"ID_CDROM": "1", "ID_CDROM_DVD": "1", "ID_VENDOR": "HL-DT-ST", "ID_MODEL": "DVD-ROM_DU90N"}
		maps.Copy(properties, extra)
		return properties
	}
	writer := map[string]string{"ID_CDROM_DVD_R": "1", "ID_CDROM_DVD_PLUS_R": "1", "ID_VENDOR": "TSSTcorp", "ID_MODEL": "DVD+-RW_SN-208FB"}
	tests := []struct {
		name       string
		properties map[string]string
		want       string
	}{
		{name: "read-only drive with a blank disc", properties: base(nil), want: dvdMediaDriveCannotWrite},
		{name: "writer without a disc", properties: base(writer), want: dvdMediaNoDisc},
		{
			name: "writer with a blank DVD-R",
			properties: base(merge(writer, map[string]string{
				"ID_CDROM_MEDIA": "1", "ID_CDROM_MEDIA_DVD_R": "1", "ID_CDROM_MEDIA_STATE": "blank",
			})),
			want: dvdMediaWritable,
		},
		{
			name: "writer with a pressed DVD",
			properties: base(merge(writer, map[string]string{
				"ID_CDROM_MEDIA": "1", "ID_CDROM_MEDIA_DVD": "1", "ID_CDROM_MEDIA_STATE": "complete",
			})),
			want: dvdMediaReadOnly,
		},
		{name: "no capability flags at all", properties: map[string]string{"ID_CDROM": "1"}, want: dvdMediaNoDisc},
		{name: "not an optical drive", properties: map[string]string{}, want: dvdMediaUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := linuxDVDDriveFromUdev("/dev/sr0", test.properties).State; got != test.want {
				t.Errorf("state = %q, want %q", got, test.want)
			}
		})
	}
	if name := linuxDVDDriveFromUdev("/dev/sr1", base(writer)).Name; name != "/dev/sr1 TSSTcorp DVD+-RW SN-208FB" {
		t.Errorf("name = %q", name)
	}
}

func merge(sources ...map[string]string) map[string]string {
	merged := make(map[string]string)
	for _, m := range sources {
		maps.Copy(merged, m)
	}
	return merged
}
