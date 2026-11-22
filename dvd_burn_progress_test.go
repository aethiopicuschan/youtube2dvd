package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The fixture is what drutil wrote to its terminal during a test burn of a
// 27 MB image to a DVD-R.
func TestDrutilOutputFollowsRecordedBurn(t *testing.T) {
	recorded, err := os.ReadFile(filepath.Join("testdata", "drutil_burn_output.bin"))
	if err != nil {
		t.Fatal(err)
	}
	for _, chunkSize := range []int{1, 7, 4096, len(recorded)} {
		var percents []int
		var messages []string
		output := &drutilOutput{onUpdate: func(update dvdExportProgressUpdate) {
			if update.hasPercent {
				percents = append(percents, update.progressPercent)
			}
			if update.message != "" {
				messages = append(messages, update.message)
			}
		}}
		for start := 0; start < len(recorded); start += chunkSize {
			_, _ = output.Write(recorded[start:min(start+chunkSize, len(recorded))])
		}

		if len(percents) < 10 || percents[0] != 4 || percents[len(percents)-1] != 97 {
			t.Errorf("chunk size %d: percents = %v, want them to run from 4 to 97", chunkSize, percents)
		}
		if !slices.IsSorted(percents) || len(slices.Compact(slices.Clone(percents))) != len(percents) {
			t.Errorf("chunk size %d: percents = %v, want each one higher than the last", chunkSize, percents)
		}
		if !slices.Equal(messages, []string{"DVDをファイナライズしています…"}) {
			t.Errorf("chunk size %d: messages = %q, want the finalizing message once", chunkSize, messages)
		}
	}
}

func TestDrutilOutputIgnoresOtherNumbers(t *testing.T) {
	var percents []int
	output := &drutilOutput{onUpdate: func(update dvdExportProgressUpdate) {
		if update.hasPercent {
			percents = append(percents, update.progressPercent)
		}
	}}
	_, _ = output.Write([]byte("Burning Image to Disc: /tmp/50%/disc 2026.iso\r\n\r\nWriting track 1 ... [*  ] 1234% % 5 % [** ] 30% \b \b 100% [***] 20% [***] 31% "))

	// "50%" in the path counts; nothing can tell it from progress.
	if want := []int{50}; !slices.Equal(percents, want) {
		t.Errorf("percents = %v, want %v", percents, want)
	}
}

func TestDrutilOutputFailureMessage(t *testing.T) {
	output := &drutilOutput{}
	_, _ = output.Write([]byte("Burning Image to Disc: /tmp/disc.iso\r\n\r\nPreparing... done.\b \b\b \bWriting track 1 ... [**   ] 12% \b \b\r      \rBurn failed: The device failed to respond properly, unable to recover or retry.\r\n   (0x80020022)\r\n"))
	want := "Burn failed: The device failed to respond properly, unable to recover or retry. (0x80020022)"
	if got := output.failureMessage(); got != want {
		t.Errorf("failure message = %q, want %q", got, want)
	}

	empty := &drutilOutput{}
	if got := empty.failureMessage(); got != "" {
		t.Errorf("failure message without output = %q, want none", got)
	}
}
