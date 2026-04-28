package watch

import "time"

type BufferConfig struct {
	MaxLines int
	MaxAge   time.Duration
	Now      func() time.Time
}

type Buffer struct {
	cfg     BufferConfig
	lines   [][]byte
	firstAt time.Time
}

func NewBuffer(cfg BufferConfig) *Buffer {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Buffer{cfg: cfg}
}

func (b *Buffer) Add(line []byte) {
	if len(b.lines) == 0 {
		b.firstAt = b.cfg.Now()
	}
	b.lines = append(b.lines, append([]byte(nil), line...))
}

func (b *Buffer) Len() int { return len(b.lines) }

func (b *Buffer) ShouldFlush() bool {
	if len(b.lines) == 0 {
		return false
	}
	if len(b.lines) >= b.cfg.MaxLines {
		return true
	}
	if b.cfg.Now().Sub(b.firstAt) >= b.cfg.MaxAge {
		return true
	}
	return false
}

func (b *Buffer) Drain() [][]byte {
	out := b.lines
	b.lines = nil
	b.firstAt = time.Time{}
	return out
}
