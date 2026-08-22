package utils

import "testing"

func BenchmarkFormatKey(b *testing.B) {
	benchmarks := []struct {
		name  string
		input string
	}{
		{"AlreadyFormatted", "DATABASE_URL"},
		{"NeedsTransformation", "my.database-connection-url_1"},
		{"ComplexSpecialChars", "special$#@char__name--test"},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = FormatKey(bm.input)
			}
		})
	}
}
