package engine

import (
	"regexp"
	"testing"
)

func TestExpandTemplateCharacterization(t *testing.T) {
	tests := []struct {
		name     string
		regex    string
		template string
		src      string
		want     string
	}{
		{
			name:     "braced expansion",
			regex:    `(?P<group1>hello)`,
			template: `echo ${group1}`,
			src:      `hello world`,
			want:     `echo hello`,
		},
		{
			name:     "unbraced expansion",
			regex:    `(?P<group1>hello)`,
			template: `echo $group1`,
			src:      `hello world`,
			want:     `echo hello`,
		},
		{
			name:     "escaped literal dollar sequences",
			regex:    `(?P<group1>hello)`,
			template: `echo \$group1 $$`,
			src:      `hello world`,
			want:     `echo \$group1 $`,
		},
		{
			name:     "single-quoted template segments",
			regex:    `(?P<group1>hello)`,
			template: `echo '${group1}'`,
			src:      `hello world`,
			want:     `echo 'hello'`,
		},
		{
			name:     "double-quoted template segments",
			regex:    `(?P<group1>hello)`,
			template: `echo "${group1}"`,
			src:      `hello world`,
			want:     `echo "hello"`,
		},
		{
			name:     "named groups that do not participate in the match",
			regex:    `(?P<group1>hello)|(?P<group2>world)`,
			template: `echo ${group1} ${group2}`,
			src:      `world`,
			want:     `echo  world`,
		},
		{
			name:     "out-of-range or unmatched group indices",
			regex:    `(hello)`,
			template: `echo ${2} $3`,
			src:      `hello`,
			want:     `echo ${2} $3`,
		},
		{
			name:     "empty template",
			regex:    `(hello)`,
			template: ``,
			src:      `hello`,
			want:     ``,
		},
		{
			name:     "no-match case",
			regex:    `(foo)`,
			template: `echo $1`,
			src:      `hello`,
			want:     `echo $1`,
		},
		{
			name:     "nested quotes - double inside single",
			regex:    `(?P<g1>hello)`,
			template: `echo '"$g1"'`,
			src:      `hello world`,
			want:     `echo '"hello"'`,
		},
		{
			name:     "nested quotes - single inside double",
			regex:    `(?P<g1>hello)`,
			template: `echo "'$g1'"`,
			src:      `hello world`,
			want:     `echo "'hello'"`,
		},
		{
			name:     "escaped characters inside quotes",
			regex:    `(?P<g1>hello)`,
			template: `echo "\$g1" '\$g1'`,
			src:      `hello world`,
			want:     `echo "\$g1" '\hello'`,
		},
		{
			name:     "numeric groups unbraced",
			regex:    `(hello) (world)`,
			template: `echo $1 $2`,
			src:      `hello world`,
			want:     `echo hello world`,
		},
		{
			name:     "numeric groups braced",
			regex:    `(hello) (world)`,
			template: `echo ${1} ${2}`,
			src:      `hello world`,
			want:     `echo hello world`,
		},
		{
			name:     "braced with non-alphanumeric",
			regex:    `(?P<group1>hello)`,
			template: `echo ${group1!}`,
			src:      `hello world`,
			want:     `echo ${group1!}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := regexp.MustCompile(tt.regex)
			matchIndices := re.FindStringSubmatchIndex(tt.src)

			got := expandTemplate(re, tt.template, tt.src, matchIndices)
			if got != tt.want {
				t.Errorf("expandTemplate() = %q, want %q", got, tt.want)
			}
		})
	}
}
