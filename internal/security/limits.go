package security

type Limits struct {
	DefaultTailLines int64
	MaxTailLines     int64
	MaxLogBytes      int64
}

func (l Limits) TailLines(requested int64) int64 {
	if requested <= 0 {
		return l.DefaultTailLines
	}
	if requested > l.MaxTailLines {
		return l.MaxTailLines
	}
	return requested
}

func (l Limits) TruncateLog(logs string) (string, bool) {
	if l.MaxLogBytes <= 0 || int64(len(logs)) <= l.MaxLogBytes {
		return logs, false
	}
	return logs[:l.MaxLogBytes], true
}
