//go:build !grammar_subset || grammar_subset_python || grammar_subset_bitbake || grammar_subset_mojo || grammar_subset_starlark

package grammars

// This state and encoding serve Python-derived external scanners. Keep this
// file separate from Python registration so a subset does not publish Python
// metadata when it only includes a derivative grammar.
type pyDelimiter byte

const (
	pyDelimSingleQuote pyDelimiter = 1 << 0
	pyDelimDoubleQuote pyDelimiter = 1 << 1
	pyDelimBackQuote   pyDelimiter = 1 << 2
	pyDelimRaw         pyDelimiter = 1 << 3
	pyDelimFormat      pyDelimiter = 1 << 4
	pyDelimTriple      pyDelimiter = 1 << 5
	pyDelimBytes       pyDelimiter = 1 << 6
)

func (d pyDelimiter) isFormat() bool { return d&pyDelimFormat != 0 }
func (d pyDelimiter) isRaw() bool    { return d&pyDelimRaw != 0 }
func (d pyDelimiter) isTriple() bool { return d&pyDelimTriple != 0 }
func (d pyDelimiter) isBytes() bool  { return d&pyDelimBytes != 0 }

func (d pyDelimiter) endChar() rune {
	switch {
	case d&pyDelimSingleQuote != 0:
		return '\''
	case d&pyDelimDoubleQuote != 0:
		return '"'
	case d&pyDelimBackQuote != 0:
		return '`'
	default:
		return 0
	}
}

type pythonScannerState struct {
	indents                  []uint16
	delimiters               []pyDelimiter
	insideInterpolatedString bool
}

func (s *pythonScannerState) syncInsideInterpolatedString() {
	s.insideInterpolatedString = false
	for _, d := range s.delimiters {
		if d.isFormat() {
			s.insideInterpolatedString = true
			return
		}
	}
}

func serializePythonScannerState(s *pythonScannerState, buf []byte) int {
	if len(buf) == 0 {
		return 0
	}
	s.syncInsideInterpolatedString()

	size := 0
	if s.insideInterpolatedString {
		buf[size] = 1
	}
	size++
	if size >= len(buf) {
		return size
	}

	delimCount := len(s.delimiters)
	if delimCount > 255 {
		delimCount = 255
	}
	buf[size] = byte(delimCount)
	size++
	if size >= len(buf) {
		return size
	}

	for i := 0; i < delimCount && size < len(buf); i++ {
		buf[size] = byte(s.delimiters[i])
		size++
	}

	// Skip indents[0] (sentinel), serialize from index 1.
	for i := 1; i < len(s.indents) && size+1 < len(buf); i++ {
		v := s.indents[i]
		buf[size] = byte(v & 0xFF)
		buf[size+1] = byte((v >> 8) & 0xFF)
		size += 2
	}

	return size
}

func deserializePythonScannerState(s *pythonScannerState, buf []byte) {
	s.delimiters = s.delimiters[:0]
	s.indents = s.indents[:0]
	s.indents = append(s.indents, 0)
	s.insideInterpolatedString = false

	if len(buf) == 0 {
		return
	}

	size := 0
	s.insideInterpolatedString = buf[size] != 0
	size++
	if size >= len(buf) {
		return
	}

	delimCount := int(buf[size])
	size++
	for i := 0; i < delimCount && size < len(buf); i++ {
		s.delimiters = append(s.delimiters, pyDelimiter(buf[size]))
		size++
	}
	s.syncInsideInterpolatedString()

	for size+1 < len(buf) {
		v := uint16(buf[size]) | uint16(buf[size+1])<<8
		s.indents = append(s.indents, v)
		size += 2
	}
}
