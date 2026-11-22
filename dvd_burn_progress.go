package main

import (
	"strconv"
	"strings"
)

// drutilOutput follows what drutil writes to a terminal while burning.
// drutil redraws its progress, such as " [*****     ] 42% ", in place with
// backspaces instead of starting a new line, so the percentage has to be
// picked out of the stream as it arrives.
type drutilOutput struct {
	digits   []byte
	percent  int
	line     []rune
	lines    []string
	closing  bool
	onUpdate func(dvdExportProgressUpdate)
}

const drutilOutputMaxLines = 64

// Write takes the next bytes drutil wrote. It never fails.
func (o *drutilOutput) Write(data []byte) (int, error) {
	for _, value := range data {
		switch {
		case value >= '0' && value <= '9':
			if len(o.digits) < 3 {
				o.digits = append(o.digits, value)
			} else {
				// Too long for a percentage.
				o.digits = o.digits[:0]
			}
		case value == '%' && len(o.digits) > 0:
			// drutil shows 100% for a moment when a phase ends and then
			// continues from where the next phase stands, so only take
			// values that move forward; the caller reports the end.
			if percent, err := strconv.Atoi(string(o.digits)); err == nil && percent < 100 && percent > o.percent {
				o.percent = percent
				o.update(dvdExportProgressUpdate{hasPercent: true, progressPercent: percent})
			}
			o.digits = o.digits[:0]
		default:
			o.digits = o.digits[:0]
		}
	}
	for _, r := range string(data) {
		switch r {
		case '\b':
			if len(o.line) > 0 {
				o.line = o.line[:len(o.line)-1]
			}
		case '\r', '\n':
			o.endLine()
		default:
			o.line = append(o.line, r)
			o.checkPhase()
		}
	}
	return len(data), nil
}

func (o *drutilOutput) update(update dvdExportProgressUpdate) {
	if o.onUpdate != nil {
		o.onUpdate(update)
	}
}

func (o *drutilOutput) checkPhase() {
	if o.closing || len(o.line) > 32 {
		return
	}
	line := string(o.line)
	if strings.HasPrefix(line, "Closing") || strings.HasPrefix(line, "Finishing") {
		o.closing = true
		o.update(dvdExportProgressUpdate{message: "DVDをファイナライズしています…"})
	}
}

// endLine finishes the current line and remembers it. That includes a line
// that is about to be redrawn in place: it may be the last one before an
// error.
func (o *drutilOutput) endLine() {
	line := strings.TrimSpace(string(o.line))
	o.line = o.line[:0]
	if line == "" {
		return
	}
	if len(o.lines) == drutilOutputMaxLines {
		o.lines = append(o.lines[:0], o.lines[1:]...)
	}
	o.lines = append(o.lines, line)
}

// failureMessage describes why drutil failed, from the end of its output.
func (o *drutilOutput) failureMessage() string {
	o.endLine()
	const maxLines = 2
	var meaningful []string
	for _, line := range o.lines {
		// Leave out the progress display.
		if strings.HasSuffix(line, "%") || strings.HasPrefix(line, "[") {
			continue
		}
		meaningful = append(meaningful, line)
	}
	for index, line := range meaningful {
		if strings.Contains(line, "failed") {
			meaningful = meaningful[index:]
			break
		}
	}
	if len(meaningful) > maxLines && !strings.Contains(meaningful[0], "failed") {
		meaningful = meaningful[len(meaningful)-maxLines:]
	}
	return strings.Join(meaningful, " ")
}
