package main

import (
	"bufio"
	"bytes"
	stdcontext "context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

func dvdBurnBackendMessage() string {
	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("drutil"); err != nil {
			return "macOSのディスク記録機能を利用できません。"
		}
	case "linux":
		if _, err := exec.LookPath("growisofs"); err != nil {
			return "DVDへの書き込みにはgrowisofsが必要です。dvd+rw-toolsをインストールしてください。"
		}
	case "windows":
		if _, err := windowsScriptHost(); err != nil {
			return "Windows Script Hostを利用できません。"
		}
	default:
		return "このOSでのDVD書き込みには対応していません。"
	}
	return ""
}

func dvdBurnISO(ctx stdcontext.Context, isoPath string, progress dvdExportProgressReporter) error {
	if ctx == nil {
		return fmt.Errorf("DVD書き込みのコンテキストがありません")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if checkDVDMediaStatus() != dvdMediaWritable {
		return fmt.Errorf("DVDディスクが書き込み可能ではありません。ディスクを確認してください")
	}
	info, err := os.Stat(isoPath)
	if err != nil {
		return fmt.Errorf("書き込むISOファイルを確認できません: %w", err)
	}
	const sectorSize int64 = 2048
	if info.Size() <= 0 || info.Size()%sectorSize != 0 {
		return fmt.Errorf("ISOファイルのサイズがDVDのセクター境界に合っていません")
	}
	if progress != nil {
		progress(dvdExportProgressUpdate{message: "映像を書き込んでいます…", hasPercent: true, progressPercent: 0})
	}

	switch runtime.GOOS {
	case "darwin":
		drive, err := writableMacDVDDrive(ctx)
		if err != nil {
			return err
		}
		return runDrutil(ctx, []string{"-drive", drive, "burn", "-noverify", isoPath}, progress)
	case "linux":
		device, err := writableLinuxDVDDevice(ctx)
		if err != nil {
			return err
		}
		return runDVDWriterCommand(ctx, "growisofs", []string{"-dvd-compat", "-Z", device + "=" + isoPath}, progress)
	case "windows":
		return burnDVDISOWindows(ctx, isoPath, progress)
	default:
		return fmt.Errorf("このOSでのDVD書き込みには対応していません")
	}
}

func writableMacDVDDrive(ctx stdcontext.Context) (string, error) {
	for index := 1; index <= 16; index++ {
		output, err := exec.CommandContext(ctx, "drutil", "-drive", strconv.Itoa(index), "status").CombinedOutput()
		if err != nil && len(output) == 0 {
			if index == 1 {
				return "", fmt.Errorf("DVDドライブの状態を確認できません: %w", err)
			}
			break
		}
		if parseDrutilDVDMediaStatus(string(output)) == dvdMediaWritable {
			return strconv.Itoa(index), nil
		}
	}
	return "", fmt.Errorf("書き込み可能なDVDドライブとディスクの組み合わせが見つかりません")
}

func writableLinuxDVDDevice(ctx stdcontext.Context) (string, error) {
	devices := linuxOpticalDevices()
	for _, device := range devices {
		output, err := exec.CommandContext(ctx, "udevadm", "info", "--query=property", "--name", device).Output()
		if err != nil {
			continue
		}
		properties := parseUdevProperties(string(output))
		if properties["ID_CDROM"] != "1" || properties["ID_CDROM_MEDIA"] != "1" {
			continue
		}
		if writable, known := udevDVDMediaWritable(properties); writable && known {
			return device, nil
		}
	}
	return "", fmt.Errorf("書き込み可能なDVDドライブとディスクの組み合わせが見つかりません")
}

func windowsScriptHost() (string, error) {
	if root := os.Getenv("SystemRoot"); root != "" {
		path := filepath.Join(root, "System32", "cscript.exe")
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return exec.LookPath("cscript.exe")
}

func burnDVDISOWindows(ctx stdcontext.Context, isoPath string, progress dvdExportProgressReporter) error {
	script, err := os.CreateTemp("", "youtube2dvd-burn-*.vbs")
	if err != nil {
		return fmt.Errorf("書き込み用スクリプトを作成できません（Windows）: %w", err)
	}
	scriptPath := script.Name()
	defer os.Remove(scriptPath)
	if _, err := io.WriteString(script, windowsDVDBurnScript); err != nil {
		_ = script.Close()
		return fmt.Errorf("書き込み用スクリプトを保存できません（Windows）: %w", err)
	}
	if err := script.Close(); err != nil {
		return fmt.Errorf("書き込み用スクリプトを閉じられません（Windows）: %w", err)
	}
	host, err := windowsScriptHost()
	if err != nil {
		return fmt.Errorf("スクリプトホストを利用できません（Windows Script Host）: %w", err)
	}
	return runDVDWriterCommand(ctx, host, []string{"//NoLogo", scriptPath, isoPath}, progress)
}

const windowsDVDBurnScript = `Option Explicit
On Error Resume Next

Const DVD_ROM = 4
Const DVD_RAM = 5
Const DVD_PLUS_R = 6
Const DVD_PLUS_RW = 7
Const DVD_PLUS_R_DL = 8
Const DVD_R = 9
Const DVD_RW = 10
Const DVD_R_DL = 11
Const DVD_PLUS_RW_DL = 13
Const MEDIA_OVERWRITE_ONLY = 1
Const MEDIA_BLANK = 2
Const MEDIA_APPENDABLE = 4
Const MEDIA_FINAL_SESSION = 8
Const MEDIA_BLOCKING_MASK = &HFC00
Const WRITE_ACTION_WRITING_DATA = 4

Dim master, recorder, writer, imageManager, fileSystem, isoFile, index, mediaType, mediaStatus
Dim isoPath, isoSize, sectors, freeSectors, supported, writableState
isoPath = WScript.Arguments(0)
Set master = CreateObject("IMAPI2.MsftDiscMaster2")
If Err.Number <> 0 Then Fail "Could not initialize IMAPI2: " & Err.Description

Set fileSystem = CreateObject("Scripting.FileSystemObject")
Set isoFile = fileSystem.GetFile(isoPath)
If Err.Number <> 0 Then Fail "Could not open the ISO file: " & Err.Description
isoSize = CDbl(isoFile.Size)
If isoSize <= 0 Or (isoSize - Fix(isoSize / 2048) * 2048) <> 0 Then Fail "The ISO size is not aligned to DVD sectors."
sectors = Fix(isoSize / 2048)

Set imageManager = CreateObject("IMAPI2FS.MsftIsoImageManager")
If Err.Number <> 0 Then Fail "Could not initialize the ISO image manager: " & Err.Description
imageManager.SetPath isoPath
If Err.Number <> 0 Then Fail "Could not read the ISO image: " & Err.Description
' Validate returns nothing; it reports a problem as an error.
imageManager.Validate
If Err.Number <> 0 Then Fail "The ISO image is not valid: " & Err.Description

For index = 0 To master.Count - 1
  Err.Clear
  Set recorder = CreateObject("IMAPI2.MsftDiscRecorder2")
  recorder.InitializeDiscRecorder master.Item(index)
  Set writer = CreateObject("IMAPI2.MsftDiscFormat2Data")
  writer.Recorder = recorder
  If Err.Number = 0 Then
    mediaType = writer.CurrentPhysicalMediaType
    mediaStatus = writer.CurrentMediaStatus
    If Err.Number = 0 Then
      supported = writer.IsCurrentMediaSupported(recorder)
      writableState = ((mediaStatus And (MEDIA_OVERWRITE_ONLY Or MEDIA_BLANK Or MEDIA_APPENDABLE Or MEDIA_FINAL_SESSION)) <> 0)
      If supported And writableState And ((mediaStatus And MEDIA_BLOCKING_MASK) = 0) Then
        If mediaType = DVD_ROM Then Fail "DVD-ROM media cannot be written."
        If Not ((mediaType >= DVD_RAM And mediaType <= DVD_R_DL) Or mediaType = DVD_PLUS_RW_DL) Then Fail "The inserted media is not a writable DVD."
        freeSectors = writer.FreeSectorsOnMedia
        If Err.Number <> 0 Then Fail "Could not check free DVD capacity: " & Err.Description
        If freeSectors < sectors Then Fail "The ISO file exceeds the free DVD capacity."
        writer.ClientName = "YouTube to DVD"
        writer.ForceMediaToBeClosed = True
        writer.ForceOverwrite = True
        WScript.ConnectObject writer, "BurnEvent_"
        writer.Write imageManager.Stream
        If Err.Number <> 0 Then Fail "IMAPI2 could not write the DVD: " & Err.Description
        WScript.Quit 0
      End If
    End If
  End If
Next
Fail "No writable DVD drive and disc combination was found."

Sub Fail(message)
  WScript.Echo "ERROR|" & message
  WScript.Quit 1
End Sub

Sub BurnEvent_Update(object, state)
  If state.CurrentAction = WRITE_ACTION_WRITING_DATA And state.SectorCount > 0 Then
    WScript.Echo "PROGRESS|" & CInt(100 * (state.LastWrittenLba - state.StartLba) / state.SectorCount)
  ElseIf state.CurrentAction = 5 Then
    WScript.Echo "MESSAGE|FINALIZING"
  End If
End Sub
`

func runDVDWriterCommand(ctx stdcontext.Context, command string, args []string, progress dvdExportProgressReporter) error {
	cmd := exec.CommandContext(ctx, command, args...)
	hideConsoleWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("DVD書き込みを開始できません: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("DVD書き込みの出力を読み取れません: %w", err)
	}
	if err := cmd.Start(); err != nil {
		if runtime.GOOS == "linux" && strings.Contains(err.Error(), "executable file not found") {
			return fmt.Errorf("DVDへの書き込みにはgrowisofsが必要です。dvd+rw-toolsをインストールしてください")
		}
		return fmt.Errorf("DVD書き込みを開始できません: %w", err)
	}

	var output strings.Builder
	var outputMu sync.Mutex
	readOutput := func(reader io.ReadCloser) {
		defer reader.Close()
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		scanner.Split(splitDVDWriterLines)
		for scanner.Scan() {
			line := scanner.Text()
			outputMu.Lock()
			if output.Len() < 1<<16 {
				output.WriteString(line)
				output.WriteByte('\n')
			}
			outputMu.Unlock()
			if strings.HasPrefix(line, "MESSAGE|") && progress != nil {
				message := strings.TrimPrefix(line, "MESSAGE|")
				if message == "FINALIZING" {
					message = "DVDをファイナライズしています…"
				}
				progress(dvdExportProgressUpdate{message: message})
			}
			if percent, ok := dvdWriterPercent(line); ok && progress != nil {
				if percent > 99 {
					percent = 99
				}
				progress(dvdExportProgressUpdate{hasPercent: true, progressPercent: percent})
			}
		}
	}
	var readers sync.WaitGroup
	readers.Add(2)
	go func() { defer readers.Done(); readOutput(stdout) }()
	go func() { defer readers.Done(); readOutput(stderr) }()
	readers.Wait()
	waitErr := cmd.Wait()
	if waitErr != nil {
		outputMu.Lock()
		message := strings.TrimSpace(output.String())
		outputMu.Unlock()
		if index := strings.LastIndex(message, "ERROR|"); index >= 0 {
			message = strings.TrimSpace(message[index+len("ERROR|"):])
		}
		if message == "" {
			message = waitErr.Error()
		}
		return fmt.Errorf("%s", message)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if progress != nil {
		progress(dvdExportProgressUpdate{hasPercent: true, progressPercent: 99})
	}
	return nil
}

func splitDVDWriterLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for i, value := range data {
		if value != '\n' && value != '\r' {
			continue
		}
		if value == '\r' && i+1 == len(data) && !atEOF {
			return 0, nil, nil
		}
		advance = i + 1
		if value == '\r' && advance < len(data) && data[advance] == '\n' {
			advance++
		}
		return advance, bytes.TrimSpace(data[:i]), nil
	}
	if atEOF && len(data) > 0 {
		return len(data), bytes.TrimSpace(data), nil
	}
	return 0, nil, nil
}

func dvdWriterPercent(line string) (int, bool) {
	line = strings.TrimSpace(line)
	if value, ok := strings.CutPrefix(line, "PROGRESS|"); ok {
		value = strings.TrimSpace(value)
		percent, err := strconv.Atoi(value)
		return percent, err == nil
	}
	percentIndex := strings.IndexByte(line, '%')
	if percentIndex <= 0 {
		return 0, false
	}
	fields := strings.Fields(line[:percentIndex])
	if len(fields) == 0 {
		return 0, false
	}
	percent, err := strconv.ParseFloat(fields[len(fields)-1], 64)
	if err != nil {
		return 0, false
	}
	return int(percent), true
}

func dvdBurnFailureMessage(err error) string {
	message := dvdExportFailureMessage(err)
	return strings.Replace(message, "ISOファイルの作成", "DVDへの書き込み", 1)
}
