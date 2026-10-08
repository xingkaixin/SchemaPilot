package sqlscript

import (
	"strings"
)

type Dialect int

const (
	Postgres Dialect = iota
	MySQL
)

type Statement struct {
	Index     int    `json:"index"`
	Text      string `json:"-"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
}

// Split cuts a script into executable statements. Comments and whitespace
// between statements are dropped; a region holding only comments is not a
// statement. MySQL scripts may switch delimiters with the client-side
// DELIMITER command, the way the mysql CLI does.
func Split(source string, dialect Dialect) []Statement {
	scanner := &splitter{source: source, dialect: dialect, delimiter: ";", line: 1}
	scanner.run()
	return scanner.statements
}

type splitter struct {
	source     string
	dialect    Dialect
	delimiter  string
	statements []Statement

	pos  int
	line int

	codeStart int
	codeLine  int
	codeEnd   int
	endLine   int
}

func (scanner *splitter) run() {
	scanner.resetStatement()
	for scanner.pos < len(scanner.source) {
		if scanner.dialect == MySQL && scanner.codeStart < 0 && scanner.atLineStart() && scanner.tryDelimiterCommand() {
			continue
		}
		if strings.HasPrefix(scanner.source[scanner.pos:], scanner.delimiter) {
			scanner.flush()
			scanner.pos += len(scanner.delimiter)
			continue
		}
		char := scanner.source[scanner.pos]
		switch {
		case char == '\n':
			scanner.line++
			scanner.pos++
		case char == ' ' || char == '\t' || char == '\r' || char == '\f':
			scanner.pos++
		case char == '-' && scanner.peek(1) == '-' && scanner.dashCommentAllowed():
			scanner.skipLineComment()
		case char == '#' && scanner.dialect == MySQL:
			scanner.skipLineComment()
		case char == '/' && scanner.peek(1) == '*':
			executable := scanner.dialect == MySQL && (scanner.peek(2) == '!' || scanner.peek(2) == '+')
			start, line := scanner.pos, scanner.line
			scanner.skipBlockComment()
			if executable {
				scanner.markCode(start, line)
			}
		default:
			start, line := scanner.pos, scanner.line
			scanner.skipToken()
			scanner.markCode(start, line)
		}
	}
	scanner.flush()
}

func (scanner *splitter) resetStatement() {
	scanner.codeStart = -1
	scanner.codeLine = 0
	scanner.codeEnd = 0
	scanner.endLine = 0
}

func (scanner *splitter) markCode(start, line int) {
	if scanner.codeStart < 0 {
		scanner.codeStart = start
		scanner.codeLine = line
	}
	scanner.codeEnd = scanner.pos
	scanner.endLine = scanner.line
}

func (scanner *splitter) flush() {
	if scanner.codeStart >= 0 {
		text := strings.TrimSpace(scanner.source[scanner.codeStart:scanner.codeEnd])
		if text != "" {
			scanner.statements = append(scanner.statements, Statement{
				Index:     len(scanner.statements) + 1,
				Text:      text,
				StartLine: scanner.codeLine,
				EndLine:   scanner.endLine,
			})
		}
	}
	scanner.resetStatement()
}

func (scanner *splitter) peek(offset int) byte {
	if scanner.pos+offset < len(scanner.source) {
		return scanner.source[scanner.pos+offset]
	}
	return 0
}

func (scanner *splitter) atLineStart() bool {
	for index := scanner.pos - 1; index >= 0; index-- {
		switch scanner.source[index] {
		case '\n':
			return true
		case ' ', '\t', '\r':
			continue
		default:
			return false
		}
	}
	return true
}

func (scanner *splitter) tryDelimiterCommand() bool {
	rest := scanner.source[scanner.pos:]
	const keyword = "delimiter"
	if len(rest) <= len(keyword) || !strings.EqualFold(rest[:len(keyword)], keyword) {
		return false
	}
	if next := rest[len(keyword)]; next != ' ' && next != '\t' {
		return false
	}
	lineEnd := strings.IndexByte(rest, '\n')
	if lineEnd < 0 {
		lineEnd = len(rest)
	}
	fields := strings.Fields(rest[len(keyword):lineEnd])
	if len(fields) == 0 {
		return false
	}
	scanner.delimiter = fields[0]
	scanner.pos += lineEnd
	return true
}

func (scanner *splitter) dashCommentAllowed() bool {
	if scanner.dialect != MySQL {
		return true
	}
	// MySQL only treats "--" as a comment when followed by whitespace.
	next := scanner.peek(2)
	return next == 0 || next == ' ' || next == '\t' || next == '\n' || next == '\r'
}

func (scanner *splitter) skipLineComment() {
	end := strings.IndexByte(scanner.source[scanner.pos:], '\n')
	if end < 0 {
		scanner.pos = len(scanner.source)
		return
	}
	scanner.pos += end
}

func (scanner *splitter) skipBlockComment() {
	depth := 0
	for scanner.pos < len(scanner.source) {
		if scanner.source[scanner.pos] == '/' && scanner.peek(1) == '*' {
			if depth == 0 || scanner.dialect == Postgres {
				depth++
			}
			scanner.pos += 2
			continue
		}
		if scanner.source[scanner.pos] == '*' && scanner.peek(1) == '/' {
			depth--
			scanner.pos += 2
			if depth == 0 {
				return
			}
			continue
		}
		scanner.advance()
	}
}

func (scanner *splitter) advance() {
	if scanner.source[scanner.pos] == '\n' {
		scanner.line++
	}
	scanner.pos++
}

// skipToken consumes one quoted literal, dollar-quoted body or plain
// character so that delimiters inside literals are never matched.
func (scanner *splitter) skipToken() {
	char := scanner.source[scanner.pos]
	switch {
	case char == '\'':
		scanner.skipQuoted('\'', scanner.backslashEscapes())
	case char == '"':
		scanner.skipQuoted('"', scanner.dialect == MySQL)
	case char == '`' && scanner.dialect == MySQL:
		scanner.skipQuoted('`', false)
	case char == '$' && scanner.dialect == Postgres:
		if tag, ok := scanner.dollarTag(); ok {
			scanner.skipDollarQuoted(tag)
			return
		}
		scanner.pos++
	default:
		scanner.pos++
	}
}

func (scanner *splitter) backslashEscapes() bool {
	if scanner.dialect == MySQL {
		return true
	}
	// PostgreSQL E'...' strings accept backslash escapes.
	if scanner.pos == 0 {
		return false
	}
	prefix := scanner.source[scanner.pos-1]
	if prefix != 'E' && prefix != 'e' {
		return false
	}
	return scanner.pos < 2 || !isIdentifierByte(scanner.source[scanner.pos-2])
}

func (scanner *splitter) skipQuoted(quote byte, backslash bool) {
	scanner.pos++
	for scanner.pos < len(scanner.source) {
		char := scanner.source[scanner.pos]
		switch {
		case backslash && char == '\\':
			scanner.pos++
			if scanner.pos < len(scanner.source) {
				scanner.advance()
			}
		case char == quote:
			if scanner.peek(1) == quote {
				scanner.pos += 2
				continue
			}
			scanner.pos++
			return
		default:
			scanner.advance()
		}
	}
}

func (scanner *splitter) dollarTag() (string, bool) {
	if scanner.pos > 0 && isIdentifierByte(scanner.source[scanner.pos-1]) {
		return "", false
	}
	end := scanner.pos + 1
	for end < len(scanner.source) && scanner.source[end] != '$' {
		char := scanner.source[end]
		isFirst := end == scanner.pos+1
		if !(char == '_' || isLetter(char) || char >= 0x80 || (!isFirst && isDigit(char))) {
			return "", false
		}
		end++
	}
	if end >= len(scanner.source) {
		return "", false
	}
	return scanner.source[scanner.pos : end+1], true
}

func (scanner *splitter) skipDollarQuoted(tag string) {
	scanner.pos += len(tag)
	closing := strings.Index(scanner.source[scanner.pos:], tag)
	if closing < 0 {
		closing = len(scanner.source) - scanner.pos
	}
	scanner.line += strings.Count(scanner.source[scanner.pos:scanner.pos+closing], "\n")
	scanner.pos += closing + len(tag)
	if scanner.pos > len(scanner.source) {
		scanner.pos = len(scanner.source)
	}
}

func isIdentifierByte(char byte) bool {
	return char == '_' || char == '$' || isLetter(char) || isDigit(char) || char >= 0x80
}

func isLetter(char byte) bool {
	return (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
}

func isDigit(char byte) bool {
	return char >= '0' && char <= '9'
}
