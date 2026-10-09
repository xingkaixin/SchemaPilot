package sqlscript

import (
	"slices"
	"strings"
)

type Dialect int

const (
	Postgres Dialect = iota
	MySQL
	SQLServer
	Oracle
	SQLite
)

type Statement struct {
	Index     int    `json:"index"`
	Text      string `json:"-"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
}

// Split cuts a script into executable statements. Comments and whitespace
// between statements are dropped; a region holding only comments is not a
// statement. Client-side conventions of each dialect's own CLI apply: MySQL
// scripts may switch delimiters with DELIMITER, SQL Server scripts may end a
// batch with GO, and Oracle scripts end PL/SQL units with a lone slash.
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

	// words holds the leading keywords of the current statement.
	words []string
	// block statements run until a separator line; the delimiter inside
	// them belongs to the body (PL/SQL units, T-SQL routines).
	block bool
	// nested statements count BEGIN/CASE … END so that delimiters inside
	// a compound body do not end them.
	nested bool
	depth  int
}

func (scanner *splitter) run() {
	scanner.resetStatement()
	for scanner.pos < len(scanner.source) {
		if scanner.dialect == MySQL && scanner.codeStart < 0 && scanner.atLineStart() && scanner.tryDelimiterCommand() {
			continue
		}
		if scanner.trySeparatorLine() {
			continue
		}
		if strings.HasPrefix(scanner.source[scanner.pos:], scanner.delimiter) {
			if scanner.block || scanner.depth > 0 {
				start, line := scanner.pos, scanner.line
				scanner.pos += len(scanner.delimiter)
				scanner.markCode(start, line)
				continue
			}
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
			word := scanner.skipToken()
			scanner.markCode(start, line)
			if word != "" {
				scanner.keyword(word)
			}
		}
	}
	scanner.flush()
}

func (scanner *splitter) resetStatement() {
	scanner.codeStart = -1
	scanner.codeLine = 0
	scanner.codeEnd = 0
	scanner.endLine = 0
	scanner.words = scanner.words[:0]
	scanner.block = false
	scanner.nested = scanner.dialect == SQLServer
	scanner.depth = 0
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

// trySeparatorLine handles a line holding only the client-side batch
// separator: GO for SQL Server, a slash for Oracle.
func (scanner *splitter) trySeparatorLine() bool {
	var separator string
	switch scanner.dialect {
	case SQLServer:
		separator = "go"
	case Oracle:
		separator = "/"
	default:
		return false
	}
	rest := scanner.source[scanner.pos:]
	if len(rest) < len(separator) || !strings.EqualFold(rest[:len(separator)], separator) || !scanner.atLineStart() {
		return false
	}
	lineEnd := strings.IndexByte(rest, '\n')
	if lineEnd < 0 {
		lineEnd = len(rest)
	}
	if strings.TrimSpace(rest[len(separator):lineEnd]) != "" {
		return false
	}
	scanner.flush()
	scanner.pos += lineEnd
	return true
}

func (scanner *splitter) keyword(word string) {
	if len(scanner.words) < 6 {
		scanner.words = append(scanner.words, word)
		scanner.classify()
	}
	if !scanner.nested {
		return
	}
	switch word {
	case "BEGIN":
		if !scanner.beginsTransaction() {
			scanner.depth++
		}
	case "CASE":
		scanner.depth++
	case "END":
		if scanner.depth > 0 {
			scanner.depth--
		}
	}
}

func (scanner *splitter) classify() {
	words := scanner.words
	switch scanner.dialect {
	case Oracle:
		if words[0] == "DECLARE" || words[0] == "BEGIN" {
			scanner.block = true
		}
		switch createdObject(words, []string{"CREATE"}, "OR", "REPLACE", "EDITIONABLE", "NONEDITIONABLE") {
		case "PROCEDURE", "FUNCTION", "PACKAGE", "TRIGGER", "TYPE":
			scanner.block = true
		}
	case SQLServer:
		switch createdObject(words, []string{"CREATE", "ALTER"}, "OR", "ALTER") {
		case "PROC", "PROCEDURE", "FUNCTION", "TRIGGER":
			scanner.block = true
		}
	case SQLite:
		if createdObject(words, []string{"CREATE"}, "TEMP", "TEMPORARY") == "TRIGGER" {
			scanner.nested = true
		}
	}
}

// createdObject returns the object type of a statement that starts with
// one of verbs, skipping modifiers such as OR REPLACE.
func createdObject(words []string, verbs []string, modifiers ...string) string {
	if !slices.Contains(verbs, words[0]) {
		return ""
	}
	for _, word := range words[1:] {
		if !slices.Contains(modifiers, word) {
			return word
		}
	}
	return ""
}

// beginsTransaction tells BEGIN TRAN and friends apart from a T-SQL block.
func (scanner *splitter) beginsTransaction() bool {
	if scanner.dialect != SQLServer {
		return false
	}
	rest := strings.TrimLeft(scanner.source[scanner.pos:], " \t\r\n")
	end := 0
	for end < len(rest) && isLetter(rest[end]) {
		end++
	}
	switch strings.ToUpper(rest[:end]) {
	case "TRAN", "TRANSACTION", "DISTRIBUTED", "DIALOG", "CONVERSATION":
		return true
	}
	return false
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
			if depth == 0 || scanner.dialect == Postgres || scanner.dialect == SQLServer {
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

// skipToken consumes one quoted literal, dollar-quoted body, keyword or
// plain character so that delimiters inside literals are never matched. It
// returns the upper-cased word when the dialect tracks keywords.
func (scanner *splitter) skipToken() string {
	char := scanner.source[scanner.pos]
	wordStart := scanner.pos == 0 || !isIdentifierByte(scanner.source[scanner.pos-1])
	switch {
	case char == '\'':
		scanner.skipQuoted('\'', scanner.backslashEscapes())
	case char == '"':
		scanner.skipQuoted('"', scanner.dialect == MySQL)
	case char == '`' && (scanner.dialect == MySQL || scanner.dialect == SQLite):
		scanner.skipQuoted('`', false)
	case char == '[' && (scanner.dialect == SQLServer || scanner.dialect == SQLite):
		scanner.skipQuoted(']', false)
	case char == '$' && scanner.dialect == Postgres:
		if tag, ok := scanner.dollarTag(); ok {
			scanner.skipDollarQuoted(tag)
			return ""
		}
		scanner.pos++
	case (char == 'q' || char == 'Q') && scanner.dialect == Oracle && wordStart && scanner.peek(1) == '\'' && scanner.peek(2) != 0:
		scanner.skipAlternativeQuoted()
	case isLetter(char) && wordStart && scanner.dialect != Postgres && scanner.dialect != MySQL:
		start := scanner.pos
		for scanner.pos < len(scanner.source) && isIdentifierByte(scanner.source[scanner.pos]) {
			scanner.pos++
		}
		return strings.ToUpper(scanner.source[start:scanner.pos])
	default:
		scanner.pos++
	}
	return ""
}

// skipAlternativeQuoted consumes an Oracle q'[...]' literal, which ends at
// the closing counterpart of its opening character followed by a quote.
func (scanner *splitter) skipAlternativeQuoted() {
	closing := scanner.peek(2)
	switch closing {
	case '[':
		closing = ']'
	case '(':
		closing = ')'
	case '{':
		closing = '}'
	case '<':
		closing = '>'
	}
	scanner.pos += 3
	for scanner.pos < len(scanner.source) {
		if scanner.source[scanner.pos] == closing && scanner.peek(1) == '\'' {
			scanner.pos += 2
			return
		}
		scanner.advance()
	}
}

func (scanner *splitter) backslashEscapes() bool {
	if scanner.dialect == MySQL {
		return true
	}
	if scanner.dialect != Postgres {
		return false
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
