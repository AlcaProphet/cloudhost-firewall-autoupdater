package tag

import "testing"

func TestFormat(t *testing.T) {
	tests := []struct {
		name    string
		tag     string
		comment string
		want    string
	}{
		{"有备注", "auto-dns", "生产API", "[auto-dns] 生产API"},
		{"无备注", "auto-dns", "", "[auto-dns]"},
		{"自定义TAG", "my-tag", "测试", "[my-tag] 测试"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Format(tt.tag, tt.comment)
			if got != tt.want {
				t.Errorf("Format(%q, %q) = %q, want %q", tt.tag, tt.comment, got, tt.want)
			}
		})
	}
}

func TestHasPrefix(t *testing.T) {
	tests := []struct {
		name        string
		description string
		tag         string
		want        bool
	}{
		{"匹配", "[auto-dns] 生产API", "auto-dns", true},
		{"不匹配", "[other] 规则", "auto-dns", false},
		{"空前缀", "普通规则", "auto-dns", false},
		{"仅TAG", "[auto-dns]", "auto-dns", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasPrefix(tt.description, tt.tag)
			if got != tt.want {
				t.Errorf("HasPrefix(%q, %q) = %v, want %v", tt.description, tt.tag, got, tt.want)
			}
		})
	}
}

// TestHasPrefix_StrictNamespace 严格 TAG 所有权语法（Issue7 §2.2 / AGENTS §三）：
// 只有 description 精确等于 "[TAG]" 或以 "[TAG] "（右方一个空格）开头时才归属；
// "[TAG]foo" 不属于本命名空间，绝不能被当作可操作规则。
func TestHasPrefix_StrictNamespace(t *testing.T) {
	tests := []struct {
		name        string
		description string
		tag         string
		want        bool
	}{
		{"仅TAG", "[auto-dns]", "auto-dns", true},
		{"TAG加空格加备注", "[auto-dns] 生产 API", "auto-dns", true},
		{"TAG后仅一个空格", "[auto-dns] ", "auto-dns", true},
		{"TAG后多个空格", "[auto-dns]   生产 API", "auto-dns", true},
		{"紧贴后缀不归属", "[auto-dns]foo", "auto-dns", false},
		{"相似TAG不归属", "[auto-dns-old] foo", "auto-dns", false},
		{"TAG前有字符不归属", "x[auto-dns] foo", "auto-dns", false},
		{"缺少右括号不归属", "[auto-dns foo", "auto-dns", false},
		{"其它TAG不归属", "[other] foo", "auto-dns", false},
		{"普通规则不归属", "手动规则", "auto-dns", false},
		{"空描述不归属", "", "auto-dns", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasPrefix(tt.description, tt.tag); got != tt.want {
				t.Errorf("HasPrefix(%q, %q) = %v, want %v", tt.description, tt.tag, got, tt.want)
			}
		})
	}
}

// TestParse_StrictNamespace Parse 必须与所有权判定共用同一语法（Issue7 §2.2）。
func TestParse_StrictNamespace(t *testing.T) {
	tests := []struct {
		name        string
		description string
		wantComment string
		wantOk      bool
	}{
		{"TAG加备注", "[auto-dns] 生产 API", "生产 API", true},
		{"仅TAG", "[auto-dns]", "", true},
		{"TAG后仅空格", "[auto-dns] ", "", true},
		{"紧贴后缀不归属", "[auto-dns]foo", "", false},
		{"相似TAG不归属", "[auto-dns-old] foo", "", false},
		{"TAG前有字符不归属", "x[auto-dns] foo", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comment, ok := Parse(tt.description, "auto-dns")
			if comment != tt.wantComment || ok != tt.wantOk {
				t.Errorf("Parse(%q) = (%q, %v), want (%q, %v)", tt.description, comment, ok, tt.wantComment, tt.wantOk)
			}
		})
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name        string
		description string
		tag         string
		wantComment string
		wantOk      bool
	}{
		{"有备注", "[auto-dns] 生产API", "auto-dns", "生产API", true},
		{"无备注", "[auto-dns]", "auto-dns", "", true},
		{"不匹配", "[other] 规则", "auto-dns", "", false},
		{"多空格", "[auto-dns]   备注  ", "auto-dns", "备注", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comment, ok := Parse(tt.description, tt.tag)
			if comment != tt.wantComment || ok != tt.wantOk {
				t.Errorf("Parse(%q, %q) = (%q, %v), want (%q, %v)",
					tt.description, tt.tag, comment, ok, tt.wantComment, tt.wantOk)
			}
		})
	}
}
