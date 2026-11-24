package migrate

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	delimiterRe = regexp.MustCompile(`(?is)^\s*DELIMITER\b`)
	// 存储过程/函数/触发器/事件体里的 BEGIN/END、IF/LOOP 等不是可靠的分号边界。
	// 这类文件必须由数据库原生客户端执行，迁移运行器明确拒绝，避免静默切坏。
	routineRe = regexp.MustCompile("(?is)^\\s*CREATE\\s+(?:OR\\s+REPLACE\\s+)?(?:DEFINER\\s*=\\s*(?:CURRENT_USER(?:\\(\\s*\\))?|(?:`(?:``|[^`])*`|'(?:''|[^'])*'|\"(?:\"\"|[^\"])*\"|[^\\s]+)(?:\\s*@\\s*(?:`(?:``|[^`])*`|'(?:''|[^'])*'|\"(?:\"\"|[^\"])*\"|[^\\s]+))?)\\s+)?(?:PROCEDURE|FUNCTION|TRIGGER|EVENT)\\b")
)

// Split 是保留给旧调用方的无引擎版本：按通用 SQL 词法规则分词。
// 遇到无法可靠静态分词的语法时返回 nil；需要错误的调用方应使用 SplitForEngine。
func Split(sql string) []string {
	stmts, err := SplitForEngine("", sql)
	if err != nil {
		return nil
	}
	return stmts
}

// SplitForEngine 按数据库引擎的词法规则移除注释并按分号切分 SQL。
// 引号与 Postgres dollar quote 内的分号保留；不支持的过程体语法返回错误。
func SplitForEngine(engine, sql string) ([]string, error) {
	sql = strings.TrimPrefix(sql, "\uFEFF")
	e := strings.ToLower(strings.TrimSpace(engine))
	switch e {
	case "", "mysql", "mariadb", "postgres":
	default:
		return nil, fmt.Errorf("migrate: 不支持的 SQL 分词引擎 %q", engine)
	}
	mysqlLike := e == "" || e == "mysql" || e == "mariadb"
	postgresLike := e == "" || e == "postgres"

	var (
		out     []string
		current strings.Builder
		start   = true // 当前语句尚未出现非空白、非注释字符
	)
	for i := 0; i < len(sql); {
		c := sql[i]
		if start && hasKeywordAt(sql, i, "DELIMITER") {
			return nil, fmt.Errorf("migrate: DELIMITER 是客户端指令且会改变过程体边界，迁移文件不支持，请改写为普通 DDL 后用数据库原生客户端执行")
		}

		switch {
		case isSQLSpace(c):
			current.WriteByte(c)
			i++
		case c == '#' && mysqlLike:
			i = skipLineComment(sql, i+1)
			current.WriteByte(' ')
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-' && lineCommentAt(sql, i, mysqlLike):
			i = skipLineComment(sql, i+2)
			current.WriteByte(' ')
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			var err error
			if mysqlLike && executableCommentAt(sql, i) {
				var end int
				end, err = scanExecutableComment(sql, i, e)
				if err == nil {
					// /*!...*/（含 MariaDB 的 /*M!...*/）和 /*+...*/ 会执行或影响优化，不能当普通 comment 丢弃。
					current.WriteString(sql[i:end])
					start = false
				}
				i = end
			} else {
				i, err = skipBlockComment(sql, i, postgresLike, &current)
			}
			if err != nil {
				return nil, err
			}
		case c == '\'' || c == '"' || (c == '`' && mysqlLike):
			end, err := scanQuoted(sql, i, c, e)
			if err != nil {
				return nil, err
			}
			current.WriteString(sql[i:end])
			start = false
			i = end
		case c == '$' && postgresLike:
			tag, ok := dollarTagAt(sql, i)
			if !ok {
				current.WriteByte(c)
				start = false
				i++
				continue
			}
			body := i + len(tag)
			rel := strings.Index(sql[body:], tag)
			if rel < 0 {
				return nil, fmt.Errorf("migrate: Postgres dollar quote %s 未闭合", tag)
			}
			end := body + rel + len(tag)
			current.WriteString(sql[i:end])
			start = false
			i = end
		case c == ';':
			if err := appendStatement(&out, current.String()); err != nil {
				return nil, err
			}
			current.Reset()
			start = true
			i++
		default:
			current.WriteByte(c)
			start = false
			i++
		}
	}
	if err := appendStatement(&out, current.String()); err != nil {
		return nil, err
	}
	return out, nil
}

func appendStatement(out *[]string, stmt string) error {
	stmt = strings.TrimSpace(stmt)
	if stmt == "" {
		return nil
	}
	if delimiterRe.MatchString(stmt) {
		return fmt.Errorf("migrate: DELIMITER 是客户端指令且会改变过程体边界，迁移文件不支持，请改写为普通 DDL 后用数据库原生客户端执行")
	}
	if routineRe.MatchString(stmt) {
		return fmt.Errorf("migrate: 存储过程/函数/触发器/事件体无法可靠静态分词，迁移文件不支持，请改写为普通 DDL 后用数据库原生客户端执行")
	}
	*out = append(*out, stmt)
	return nil
}

func hasKeywordAt(sql string, i int, keyword string) bool {
	if len(sql)-i < len(keyword) || !strings.EqualFold(sql[i:i+len(keyword)], keyword) {
		return false
	}
	end := i + len(keyword)
	if end < len(sql) && isIdentByte(sql[end]) {
		return false
	}
	return true
}

func isSQLSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

func isIdentByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '$' || c >= 0x80
}

func isIdentStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c >= 0x80
}

func lineCommentAt(sql string, i int, mysqlLike bool) bool {
	// MySQL/MariaDB 的 "--" 后必须有空白或控制字符；Postgres 不要求。
	return !mysqlLike || i+2 == len(sql) || isSQLSpace(sql[i+2])
}

func executableCommentAt(sql string, i int) bool {
	if i+2 >= len(sql) {
		return false
	}
	if sql[i+2] == '!' || sql[i+2] == '+' {
		return true
	}
	return i+3 < len(sql) && sql[i+2] == 'M' && sql[i+3] == '!'
}

func scanExecutableComment(sql string, start int, engine string) (int, error) {
	// 整体保留可执行注释，内部引号里的 */ 不是结束标记；内部体由数据库解释，分词器不擅自切开。
	for i := start + 2; i < len(sql); i++ {
		if sql[i] == '*' && i+1 < len(sql) && sql[i+1] == '/' {
			return i + 2, nil
		}
		if sql[i] == '\'' || sql[i] == '"' || sql[i] == '`' {
			end, err := scanQuoted(sql, i, sql[i], engine)
			if err != nil {
				return 0, fmt.Errorf("migrate: 可执行注释中的引号未闭合（起始字节 %d）: %w", start, err)
			}
			i = end - 1
		}
	}
	return 0, fmt.Errorf("migrate: 可执行注释未闭合（起始字节 %d）", start)
}

func skipLineComment(sql string, i int) int {
	for i < len(sql) && sql[i] != '\n' && sql[i] != '\r' {
		i++
	}
	return i
}

func skipBlockComment(sql string, i int, nested bool, current *strings.Builder) (int, error) {
	begin := i
	depth := 1
	i += 2
	for i < len(sql) {
		switch {
		case nested && i+1 < len(sql) && sql[i] == '/' && sql[i+1] == '*':
			depth++
			i += 2
		case i+1 < len(sql) && sql[i] == '*' && sql[i+1] == '/':
			depth--
			i += 2
			if depth == 0 {
				current.WriteByte(' ')
				return i, nil
			}
		default:
			if sql[i] == '\n' || sql[i] == '\r' {
				current.WriteByte(sql[i])
			}
			i++
		}
	}
	return 0, fmt.Errorf("migrate: 块注释未闭合（起始字节 %d）", begin)
}

func scanQuoted(sql string, start int, quote byte, engine string) (int, error) {
	backslash := engine == "" || engine == "mysql" || engine == "mariadb"
	if engine == "postgres" && quote == '\'' && start > 0 && (sql[start-1] == 'e' || sql[start-1] == 'E') &&
		(start == 1 || !isIdentByte(sql[start-2])) {
		backslash = true
	}
	for i := start + 1; i < len(sql); i++ {
		if sql[i] == quote {
			if i+1 < len(sql) && sql[i+1] == quote {
				i++
				continue
			}
			return i + 1, nil
		}
		if backslash && sql[i] == '\\' && i+1 < len(sql) {
			i++
		}
	}
	return 0, fmt.Errorf("migrate: 引号 %q 未闭合（起始字节 %d）", quote, start)
}

func dollarTagAt(sql string, i int) (string, bool) {
	if i > 0 && isIdentByte(sql[i-1]) {
		return "", false
	}
	if i+1 >= len(sql) {
		return "", false
	}
	if sql[i+1] == '$' {
		return "$$", true
	}
	if !isIdentStart(sql[i+1]) {
		return "", false
	}
	for j := i + 2; j < len(sql); j++ {
		if sql[j] == '$' {
			return sql[i : j+1], true
		}
		if !isIdentByte(sql[j]) || sql[j] == '$' {
			return "", false
		}
	}
	return "", false
}
