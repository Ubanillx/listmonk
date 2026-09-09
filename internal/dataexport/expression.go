package dataexport

import (
	"fmt"
	"strings"
	"unicode"
)

// expression translates a restricted customer filter into bound parameters.
// No subqueries, function calls, comments, casts or foreign table identifiers
// are accepted. This preserves normal advanced customer filters without giving
// an organization manager access to arbitrary SQL in an export worker.
func (q *query) expression(input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "TRUE", nil
	}
	if len(input) > 4000 {
		return "", fmt.Errorf("高级筛选过长")
	}
	columns := map[string]bool{"id": true, "uuid": true, "email": true, "name": true, "customer_code": true, "status": true, "created_at": true, "updated_at": true, "attribs": true}
	keywords := map[string]bool{"and": true, "or": true, "not": true, "is": true, "null": true, "true": true, "false": true, "like": true, "ilike": true, "in": true, "between": true}
	var out []string
	depth := 0
	for i := 0; i < len(input); {
		ch := input[i]
		if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' {
			i++
			continue
		}
		if ch == '\'' {
			i++
			var literal strings.Builder
			closed := false
			for i < len(input) {
				if input[i] == '\'' {
					if i+1 < len(input) && input[i+1] == '\'' {
						literal.WriteByte('\'')
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				literal.WriteByte(input[i])
				i++
			}
			if !closed {
				return "", fmt.Errorf("高级筛选字符串未闭合")
			}
			out = append(out, q.arg(literal.String()))
			continue
		}
		if ch >= '0' && ch <= '9' {
			start := i
			for i < len(input) && ((input[i] >= '0' && input[i] <= '9') || input[i] == '.') {
				i++
			}
			out = append(out, q.arg(input[start:i]))
			continue
		}
		if unicode.IsLetter(rune(ch)) || ch == '_' {
			start := i
			for i < len(input) && (unicode.IsLetter(rune(input[i])) || unicode.IsDigit(rune(input[i])) || input[i] == '_' || input[i] == '.') {
				i++
			}
			word := strings.ToLower(input[start:i])
			col := strings.TrimPrefix(word, "customers.")
			if columns[col] {
				out = append(out, "c."+col)
			} else if keywords[word] {
				out = append(out, word)
			} else {
				return "", fmt.Errorf("高级导出筛选不支持 %q；请使用客户字段、比较、AND/OR 或 IN", word)
			}
			continue
		}
		matched := false
		for _, op := range []string{"->>", "->", "!~*", "!~", "~*", "!=", "<>", ">=", "<=", "=", ">", "<", "~", "(", ")", ","} {
			if strings.HasPrefix(input[i:], op) {
				if op == "(" {
					depth++
				}
				if op == ")" {
					depth--
					if depth < 0 {
						return "", fmt.Errorf("高级筛选括号不匹配")
					}
				}
				out = append(out, op)
				i += len(op)
				matched = true
				break
			}
		}
		if !matched {
			return "", fmt.Errorf("高级导出筛选包含不支持的语法")
		}
	}
	if depth != 0 {
		return "", fmt.Errorf("高级筛选括号不匹配")
	}
	return "(" + strings.Join(out, " ") + ")", nil
}
