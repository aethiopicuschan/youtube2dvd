package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ncruces/zenity"
)

func chooseISOOutputPath() (string, error) {
	directory, err := downloadDirectory()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", fmt.Errorf("保存先フォルダーを作成できません: %w", err)
	}

	defaultPath := uniqueDownloadPath(directory, "youtube2dvd.iso")
	selectedPath, err := zenity.SelectFileSave(
		zenity.Title("ISOファイルの保存先を選択"),
		zenity.Filename(defaultPath),
		zenity.ConfirmOverwrite(),
		zenity.FileFilters{{Name: "ISOファイル (*.iso)", Patterns: []string{"*.iso"}, CaseFold: true}},
	)
	if err != nil {
		return "", err
	}
	selectedPath = strings.TrimSpace(selectedPath)
	if selectedPath == "" {
		return "", fmt.Errorf("保存先が選択されていません")
	}
	if !strings.EqualFold(filepath.Ext(selectedPath), ".iso") {
		if filepath.Ext(selectedPath) != "" {
			return "", fmt.Errorf("ISOファイル名の末尾は .iso にしてください")
		}
		selectedPath += ".iso"
		if _, err := os.Stat(selectedPath); err == nil {
			if err := zenity.Question(
				fmt.Sprintf("「%s」はすでにあります。上書きしますか？", filepath.Base(selectedPath)),
				zenity.Title("上書きの確認"),
				zenity.OKLabel("上書き"),
				zenity.CancelLabel("キャンセル"),
			); err != nil {
				return "", err
			}
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}

	return filepath.Abs(selectedPath)
}
