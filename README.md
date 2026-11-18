# YouTube To DVD

[![License: MIT](https://img.shields.io/badge/License-MIT-brightgreen?style=flat-square)](/LICENSE)

指定されたYouTubeの動画をDVDに焼くためのツールです。

---

## インストール

### Golang環境が存在している場合

```sh
go install github.com/aethiopicuschan/youtube2dvd@latest
```

### Golang環境が存在しない場合

[リリースページ](https://github.com/aethiopicuschan/youtube2dvd/releases)から環境に合わせたバイナリをダウンロードしてください。

## 使い方

### DVDに書き込みたい場合

以下が必要です。

- インターネット環境
- DVD書き込み対応のドライブ
- DVD-R/RWディスク

「ダウンロード」タブからYouTube動画をダウンロードし、「書き込み」タブからDVDに書き込むことができます。その際、「書き込む」ボタンをクリックしてください。

### ISOファイルを作成したい場合

以下が必要です。

- インターネット環境

基本的にはDVDに書き込む場合と同じですが、「書き出す」ボタンをクリックしてください。

## 補足

ダウンロードされた動画は、OSのダウンロードフォルダ内の `youtube2dvd` フォルダに保存されます。フォルダがない場合は自動で作成されます。同じ名前のファイルがある場合は上書きせず、ファイル名に連番を付けて保存します。
したがって、同フォルダに手動で動画ファイルを追加することによっても、このツールで扱うことができます。ただし、対応しているフォーマットはMP4とWebMのみです。
