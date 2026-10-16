// Package logger 实现容量受限的并发安全内存环形日志缓冲区。
//
// 设计目标（小型 NAS 机械硬盘休眠保护）：
//   - 高频轮询日志只写入内存环形队列（默认 1000 条），绝不直接落盘；
//   - 仅 WARN/ERROR 级别以及关键业务事件（配置变更、转存/整理结果）才落库；
//   - 支持按模块过滤、按级别过滤与导出纯文本 .log。
package logger

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Level 日志级别。
type Level string

const (
	LevelInfo  Level = "INFO"
	LevelWarn  Level = "WARN"
	LevelError Level = "ERROR"
)

// 模块名常量（与前端日志中心筛选项保持一致）。
const (
	ModSystem    = "System"
	ModScheduler = "Scheduler"
	ModCloud115  = "Cloud115"
	ModCloud123  = "Cloud123"
	ModGuangYa   = "CloudGuangYa"
	ModTianYi    = "Tianyi"
	ModOrganizer = "Organizer"
	ModSTRM      = "STRM"
	ModTransfer  = "Transfer"
	ModConfig    = "Config"
	ModAPI       = "API"
	ModMeta      = "Meta"
)

// Entry 单条日志。
type Entry struct {
	ID      int64     `json:"id"`
	Time    time.Time `json:"time"`
	Level   Level     `json:"level"`
	Module  string    `json:"module"`
	Message string    `json:"message"`
}

// PersistFunc 关键事件落盘回调（由 db 层注入）。
type PersistFunc func(Entry) error

// RingLogger 线程安全环形日志缓冲器。
type RingLogger struct {
	mu      sync.RWMutex
	entries []Entry
	cap     int
	seq     int64
	persist PersistFunc
}

// NewRingLogger 创建容量为 capacity 的环形日志器。
func NewRingLogger(capacity int) *RingLogger {
	if capacity <= 0 {
		capacity = 1000
	}
	return &RingLogger{
		entries: make([]Entry, 0, capacity),
		cap:     capacity,
	}
}

// SetPersistFunc 注入关键事件落库回调。
func (r *RingLogger) SetPersistFunc(f PersistFunc) {
	r.mu.Lock()
	r.persist = f
	r.mu.Unlock()
}

// shouldPersist 判断是否属于需要落盘的关键事件。
func shouldPersist(e Entry) bool {
	if e.Level == LevelWarn || e.Level == LevelError {
		return true
	}
	switch {
	case e.Module == ModConfig,
		strings.HasPrefix(e.Module, "Transfer"),
		strings.HasPrefix(e.Module, "Organize"):
		return true
	}
	return false
}

func (r *RingLogger) push(e Entry) {
	r.mu.Lock()
	if len(r.entries) >= r.cap {
		// 环形覆盖：丢弃最旧一条，保持容量上限，避免无界增长。
		copy(r.entries, r.entries[1:])
		r.entries = r.entries[:r.cap-1]
	}
	r.entries = append(r.entries, e)
	pf := r.persist
	r.mu.Unlock()

	if pf != nil && shouldPersist(e) {
		_ = pf(e)
	}
}

func (r *RingLogger) nextID() int64 {
	r.seq++
	return r.seq
}

// Log 记录一条日志。
func (r *RingLogger) Log(level Level, module, format string, args ...interface{}) Entry {
	e := Entry{
		ID:      r.nextID(),
		Time:    time.Now(),
		Level:   level,
		Module:  module,
		Message: fmt.Sprintf(format, args...),
	}
	r.push(e)
	return e
}

// Info 记录 INFO 日志。
func (r *RingLogger) Info(module, format string, args ...interface{}) Entry {
	return r.Log(LevelInfo, module, format, args...)
}

// Warn 记录 WARN 日志。
func (r *RingLogger) Warn(module, format string, args ...interface{}) Entry {
	return r.Log(LevelWarn, module, format, args...)
}

// Error 记录 ERROR 日志。
func (r *RingLogger) Error(module, format string, args ...interface{}) Entry {
	return r.Log(LevelError, module, format, args...)
}

// Query 按模块/级别过滤查询，返回按时间正序的最新 limit 条。
func (r *RingLogger) Query(module string, levels []Level, limit int) []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	levelSet := map[Level]bool{}
	for _, l := range levels {
		levelSet[l] = true
	}
	out := make([]Entry, 0, 128)
	for _, e := range r.entries {
		if module != "" && !strings.EqualFold(module, "ALL") && !strings.EqualFold(e.Module, module) {
			continue
		}
		if len(levelSet) > 0 && !levelSet[e.Level] {
			continue
		}
		out = append(out, e)
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// Export 导出为纯文本日志（按时间正序）。
func (r *RingLogger) Export(module string) string {
	entries := r.Query(module, nil, 0)
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.Time.Format("2006-01-02 15:04:05.000"))
		b.WriteString(" [")
		b.WriteString(string(e.Level))
		b.WriteString("] [")
		b.WriteString(e.Module)
		b.WriteString("] ")
		b.WriteString(e.Message)
		b.WriteString("\n")
	}
	return b.String()
}

// Count 返回当前内存中的日志条数。
func (r *RingLogger) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}
