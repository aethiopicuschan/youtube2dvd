package main

import "testing"

func TestDiscBurningReportHasDVDWriter(t *testing.T) {
	// As printed by macOS for a USB DVD writer with a blank DVD-R inserted.
	const usbWriter = `Disc Burning:

    TSSTcorp DVD+-RW SN-208FB:

      Firmware Revision: D100
      Interconnect: USB
      Burn Support: Yes (Generic Drive Support)
      Profile Path: None
      Cache: 512 KB
      Reads DVD: Yes
      CD-Write: -R, -RW
      DVD-Write: -R, -R DL, -RAM, -RW, +R, +R DL, +RW
      Write Strategies: CD-TAO, CD-SAO, CD-Raw, DVD-DAO
      Media:
          Type: DVD-R
          ID: CMC MAG. AM3
          Blank: Yes
          Erasable: No
          Overwritable: Yes
          Appendable: Yes
          Write Speeds: 3x, 4x, 6x, 8x
`
	tests := []struct {
		name   string
		report string
		want   bool
	}{
		{name: "USB DVD writer", report: usbWriter, want: true},
		{name: "no drive", report: "", want: false},
		{name: "heading only", report: "Disc Burning:\n\n", want: false},
		{
			name:   "CD writer that only reads DVDs",
			report: "Disc Burning:\n\n    CD-RW Drive:\n\n      Burn Support: Yes (Apple Shipping Drive)\n      Reads DVD: Yes\n      CD-Write: -R, -RW\n",
			want:   false,
		},
		{
			name:   "DVD-Write says no",
			report: "    Drive:\n      CD-Write: -R, -RW\n      DVD-Write: No\n",
			want:   false,
		},
		{
			name:   "DVD-Write is empty",
			report: "    Drive:\n      DVD-Write:\n",
			want:   false,
		},
		{
			name:   "second drive writes DVDs",
			report: "    Reader:\n      Reads DVD: Yes\n\n    Writer:\n      dvd-write: -R, +R\r\n",
			want:   true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := discBurningReportHasDVDWriter(test.report); got != test.want {
				t.Errorf("discBurningReportHasDVDWriter() = %t, want %t", got, test.want)
			}
		})
	}
}
