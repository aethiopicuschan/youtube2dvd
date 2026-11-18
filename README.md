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
- 書き込み可能なDVDディスク

「ダウンロード」タブから動画を保存し、「書き込み」タブで動画をダブルクリックしてキューに追加します。書き込み方法を選ぶボタンを押し、DVDメニューのタイトルを入力して「DVDに書き込む」を選択してください。ダウンロード済みフォルダにある動画もキューに追加できます。

DVDへの書き込みは、アプリが対応ドライブと書き込み可能なディスクを検出した場合に利用できます。OSごとに次の書き込み機能を使います。

- macOS: 標準の`drutil`
- Windows: Windows標準のIMAPI2
- Linux: `growisofs`（`dvd+rw-tools`パッケージに含まれます）

### ISOファイルを作成したい場合

YouTube動画を取得する場合はインターネット環境が必要です。ダウンロード済みの動画から作成する場合は不要です。書き込み方法を選ぶボタンを押し、DVDメニューのタイトルを入力して「ISOファイルとして保存する」を選択してください。

## 補足

ダウンロードされた動画は、OSのダウンロードフォルダ内の `youtube2dvd` フォルダに保存されます。フォルダがない場合は自動で作成されます。同じ名前のファイルがある場合は上書きせず、ファイル名に連番を付けて保存します。
したがって、同フォルダに手動で動画ファイルを追加することによっても、このツールで扱うことができます。ただし、対応しているフォーマットはMP4とWebMのみです。
