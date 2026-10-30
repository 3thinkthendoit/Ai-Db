package db

import "strings"

// splitRedisArgs splits a Redis command line, honoring "double" and 'single' quotes
// so keys with spaces can be passed safely.
func splitRedisArgs(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []string
	var b strings.Builder
	inDouble := false
	inSingle := false
	escape := false
	flush := func() {
		out = append(out, b.String())
		b.Reset()
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if escape {
			b.WriteByte(ch)
			escape = false
			continue
		}
		if inDouble {
			if ch == '\\' {
				escape = true
				continue
			}
			if ch == '"' {
				inDouble = false
				continue
			}
			b.WriteByte(ch)
			continue
		}
		if inSingle {
			if ch == '\\' {
				escape = true
				continue
			}
			if ch == '\'' {
				inSingle = false
				continue
			}
			b.WriteByte(ch)
			continue
		}
		switch ch {
		case '"':
			inDouble = true
		case '\'':
			inSingle = true
		case ' ', '\t', '\n', '\r':
			if b.Len() > 0 {
				flush()
			}
		default:
			b.WriteByte(ch)
		}
	}
	if b.Len() > 0 {
		flush()
	}
	return out
}
