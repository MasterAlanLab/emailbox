// Package query 只存放 sqlc 的命名 SQL，本文件是对这些 .sql 文件的守卫。
package query

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// sqlc 在计算查询边界时把字节偏移当成了字符偏移：只要 .sql 文件里出现多字节字符
// （比如中文注释），生成的 SQL 常量就会被**静默截断**，运行时报
// "SQL logic error: incomplete input"。截断量正好等于多出来的字节数，
// 所以问题往往在离注释很远的另一条查询上爆炸，极难定位。
//
// 因此 db/query 下必须全部是 ASCII。需要中文说明就写在 pkg/repo 的对应方法上。
func TestQueryFilesAreASCII(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		matches, err := filepath.Glob(filepath.Join(dialect, "*.sql"))
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) == 0 {
			t.Fatalf("%s 目录下没有找到任何 .sql 文件", dialect)
		}
		for _, path := range matches {
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for lineNo, line := range strings.Split(string(content), "\n") {
				if idx := firstNonASCII(line); idx >= 0 {
					r, _ := utf8.DecodeRuneInString(line[idx:])
					t.Errorf("%s:%d 含非 ASCII 字符 %q，会导致 sqlc 静默截断 SQL；请把说明移到 pkg/repo",
						path, lineNo+1, r)
					break
				}
			}
		}
	}
}

func firstNonASCII(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] > 127 {
			return i
		}
	}
	return -1
}

// sqlc 会**静默地**把它不认识的构造原样留在生成的 SQL 里：
// `json_each(sqlc.arg(x))` 与 `ORDER BY CASE WHEN sqlc.arg(x) ...` 都出现过，
// 生成的常量里带着字面量 "sqlc.arg(...)"，到运行时才报语法错误。
// 这里直接检查生成产物：任何 SQL 常量都不该包含 sqlc 指令的残留。
func TestGeneratedSQLHasNoLeftoverDirectives(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		matches, err := filepath.Glob(filepath.Join("..", "generated", dialect, "*.sql.go"))
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) == 0 {
			t.Fatalf("%s 目录下没有找到生成代码", dialect)
		}
		for _, path := range matches {
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, stmt := range sqlConstants(string(content)) {
				for _, directive := range []string{"sqlc.arg", "sqlc.narg", "sqlc.slice", "sqlc.embed"} {
					if strings.Contains(stmt, directive) {
						t.Errorf("%s 的生成 SQL 里残留了 %s，该查询在运行时会报语法错误:\n%s",
							path, directive, stmt)
					}
				}
			}
		}
	}
}

// sqlConstants 取出生成文件里所有反引号包裹的 SQL 常量。
func sqlConstants(source string) []string {
	var out []string
	rest := source
	for {
		start := strings.Index(rest, "= `")
		if start < 0 {
			return out
		}
		rest = rest[start+3:]
		end := strings.Index(rest, "`")
		if end < 0 {
			return out
		}
		out = append(out, rest[:end])
		rest = rest[end+1:]
	}
}
