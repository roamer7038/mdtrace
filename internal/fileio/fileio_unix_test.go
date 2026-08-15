//go:build unix

package fileio

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestWriteRefusesNamedPipe は、名前付きパイプを書き先にできないことを確かめる。
//
// 通常のファイルとして開こうとすると、読み手が現れるまで返らない。
// 止まったまま返らないのは、断るより悪い。
func TestWriteRefusesNamedPipe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(path, 0o644); err != nil {
		t.Skipf("名前付きパイプを作れない: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- Write(path, []byte("本文\n")) }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("名前付きパイプを書き先にできてしまっている")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("書き込みが返らない（読み手を待って止まっている）")
	}
}

// TestWriteRefusesPipeThroughFD は、記述子ごしのパイプを書き先にできないことを
// 確かめる。実体までたどると存在しない道（pipe:[...]）になるので、
// 受け取った綴りの時点で見ないと分かりにくい失敗になる。
func TestWriteRefusesPipeThroughFD(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Skipf("パイプを作れない: %v", err)
	}
	defer r.Close()
	defer w.Close()

	path := fmt.Sprintf("/proc/self/fd/%d", w.Fd())
	if _, err := os.Stat(path); err != nil {
		t.Skipf("記述子の道が無い: %v", err)
	}
	err = Write(path, []byte("本文\n"))
	if err == nil {
		t.Fatal("パイプを書き先にできてしまっている")
	}
	if !strings.Contains(err.Error(), "通常のファイルではありません") {
		t.Errorf("原因が分からない: %v", err)
	}
}
