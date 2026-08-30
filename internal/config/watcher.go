// fsnotify 기반 설정 파일 핫 리로드(디바운스 포함)를 담당한다.
package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const debounceInterval = 500 * time.Millisecond

// Watcher는 설정 파일 변경을 감시하고 디바운스 후 콜백을 호출한다.
type Watcher struct {
	fw       *fsnotify.Watcher
	onChange func(*AppConfig)
	path     string
	done     chan struct{}
	closed   sync.Once
}

// StartWatching은 설정 파일의 디렉토리를 감시하기 시작한다.
// 에디터가 파일을 교체(rename)하는 방식을 고려해 디렉토리 단위로 감시한다.
func StartWatching(path string, onChange func(*AppConfig)) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := fw.Add(filepath.Dir(path)); err != nil {
		fw.Close()
		return nil, err
	}
	w := &Watcher{fw: fw, onChange: onChange, path: path, done: make(chan struct{})}
	go w.loop()
	return w, nil
}

// loop은 이벤트를 받아 디바운스한 뒤 설정을 다시 로드하고 콜백을 호출한다.
func (w *Watcher) loop() {
	var timer *time.Timer
	var fired chan struct{}
	for {
		select {
		case <-w.done:
			return
		case ev, ok := <-w.fw.Events:
			if !ok {
				return
			}
			if filepath.Base(ev.Name) != filepath.Base(w.path) {
				continue
			}
			// 저장 완료 이벤트만 처리 (원자적 교체: Rename/Create+Chmod)
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Chmod) == 0 {
				continue
			}
			if timer != nil {
				timer.Stop()
			}
			fired = make(chan struct{})
			timer = time.AfterFunc(debounceInterval, func() {
				cfg, err := Load(w.path)
				if err != nil {
					slog.Warn("설정 핫 리로드 실패", "path", w.path, "err", err)
					return
				}
				close(fired)
				w.onChange(cfg)
			})
			_ = fired
		case _, ok := <-w.fw.Errors:
			if !ok {
				return
			}
			slog.Warn("설정 감시 오류")
		}
	}
}

// Stop은 감시를 중지하고 자원을 해제한다.
func (w *Watcher) Stop() {
	w.closed.Do(func() {
		close(w.done)
		w.fw.Close()
	})
}

// Exists는 설정 파일 존재 여부를 반환한다.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
