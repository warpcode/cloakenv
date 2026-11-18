package utils

import "testing"

func BenchmarkSerializeYAMLValue(b *testing.B) {
	strVal := "hello_world_secret"
	intVal := 123456
	boolVal := true
	sliceVal := []any{"item1", "item2", "item3"}
	mapVal := map[string]any{
		"host": "localhost",
		"port": 8080,
		"tls":  true,
	}

	b.Run("StringScalar", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, _ = SerializeYAMLValue(strVal)
		}
	})

	b.Run("IntScalar", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, _ = SerializeYAMLValue(intVal)
		}
	})

	b.Run("BoolScalar", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, _ = SerializeYAMLValue(boolVal)
		}
	})

	b.Run("SliceValue", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, _ = SerializeYAMLValue(sliceVal)
		}
	})

	b.Run("MapValue", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, _ = SerializeYAMLValue(mapVal)
		}
	})
}
