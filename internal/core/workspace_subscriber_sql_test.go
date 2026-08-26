package core

import "testing"

func TestValidateWorkspaceSubscriberSQLExpression(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  bool
	}{
		{
			name:  "normal condition",
			query: "email = 'member@example.com' AND status = 'enabled'",
			want:  true,
		},
		{
			name:  "nested condition and escaped quote",
			query: "(name = 'Ada''s list' OR email ILIKE '%@example.com')",
			want:  true,
		},
		{
			name:  "separator inside literal",
			query: "name = 'semi;colon'",
			want:  true,
		},
		{
			name:  "unbalanced opening parenthesis",
			query: "(status = 'enabled'",
			want:  false,
		},
		{
			name:  "unbalanced closing parenthesis",
			query: "status = 'enabled')",
			want:  false,
		},
		{
			name:  "line comment",
			query: "status = 'enabled' -- hide rest",
			want:  false,
		},
		{
			name:  "block comment",
			query: "status = /* filter */ 'enabled'",
			want:  false,
		},
		{
			name:  "multiple statement",
			query: "status = 'enabled'; SELECT 1",
			want:  false,
		},
		{
			name:  "positional parameter",
			query: "id = $1",
			want:  false,
		},
		{
			name:  "unterminated literal",
			query: "name = 'Ada",
			want:  false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateWorkspaceSubscriberSQLExpression(test.query)
			if (err == nil) != test.want {
				t.Fatalf("validateWorkspaceSubscriberSQLExpression(%q) error = %v, want valid=%v", test.query, err, test.want)
			}
		})
	}
}
